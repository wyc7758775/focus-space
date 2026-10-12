-- Public/default collection is server-owned. Keep all existing articles and
-- user state, but stop every curated source from using the device collector.

CREATE TEMP TABLE plaud_gateway_public_sources (
    id TEXT PRIMARY KEY
);

INSERT INTO plaud_gateway_public_sources(id) VALUES
    ('wechat-aiera'),
    ('wechat-jiqizhixin'),
    ('wechat-geekpark'),
    ('wechat-qbitai'),
    ('wechat-ali-tech'),
    ('wechat-chaping'),
    ('wechat-paperweekly'),
    ('bilibili-tech-aesthetics'),
    ('bilibili-yingshijufeng'),
    ('bilibili-geekerwan'),
    ('bilibili-hardware-tea'),
    ('bilibili-machine-heart'),
    ('bilibili-qbit'),
    ('bilibili-geekpark'),
    ('bilibili-he-tongxue'),
    ('bilibili-li-mu-ai'),
    ('bilibili-ai-tech-review'),
    ('youtube-anthropic'),
    ('youtube-google-deepmind'),
    ('youtube-nvidia'),
    ('youtube-fireship'),
    ('youtube-two-minute-papers'),
    ('youtube-ai-explained'),
    ('youtube-computerphile'),
    ('youtube-lex-fridman'),
    ('elon-musk-x'),
    ('sam-altman-x'),
    ('andrej-karpathy-x'),
    ('demis-hassabis-x'),
    ('dario-amodei-x'),
    ('donald-trump-x'),
    ('jensen-huang-x'),
    ('satya-nadella-x'),
    ('tibo-sottiaux-x'),
    ('openai-news'),
    ('sspai'),
    ('eurlex-legislation'),
    ('arxiv-ai'),
    ('china-policy-library'),
    ('cninfo-disclosures'),
    ('crossref-research'),
    ('epo-publication-server'),
    ('govinfo-federal-register'),
    ('hacker-news'),
    ('sec-xbrl-filings'),
    ('sse-periodic-reports'),
    ('hkex-regulatory-announcements');

UPDATE feed_sources
SET collection_mode = 'source_gateway',
    url = 'source-gateway://' || id,
    etag = '',
    last_modified = '',
    next_sync_at = '',
    sync_interval_seconds = 1800,
    last_error = '',
    gateway_status = CASE WHEN gateway_status = '' THEN 'initializing' ELSE gateway_status END,
    gateway_has_more = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM plaud_gateway_public_sources);

-- The subscription API is user-scoped. Queue the existing local choices so
-- optional maintained sources become readable without relying on a one-shot
-- best-effort request during upgrade.
CREATE TABLE IF NOT EXISTS source_gateway_subscription_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    operation_id TEXT NOT NULL UNIQUE,
    source_id TEXT NOT NULL REFERENCES feed_sources(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_source_gateway_subscription_outbox_due
    ON source_gateway_subscription_outbox(next_retry_at, id);

INSERT OR IGNORE INTO source_gateway_subscription_outbox(
    operation_id, source_id, enabled, created_at, updated_at
)
SELECT 'plaud-sub:' || source.id || ':' || COALESCE(subscription.enabled, 0),
       source.id,
       COALESCE(subscription.enabled, 0),
       CURRENT_TIMESTAMP,
       CURRENT_TIMESTAMP
FROM plaud_gateway_public_sources source
LEFT JOIN feed_subscriptions subscription ON subscription.source_id = source.id;

-- These seven public defaults did not pass the maintained catalog gate. They
-- remain recoverable in SQLite, but are no longer presented or scheduled as a
-- working source. No news/preferences/full-text/context rows are deleted.
CREATE TEMP TABLE plaud_retired_public_sources (
    id TEXT PRIMARY KEY
);

INSERT INTO plaud_retired_public_sources(id) VALUES
    ('cnipa-patent-data'),
    ('wipo-patentscope'),
    ('wechat-ai-paper'),
    ('wechat-fnos'),
    ('wechat-huxiu'),
    ('wechat-founder-park'),
    ('wechat-liuxiaopai');

UPDATE feed_sources
SET is_default = 0,
    collection_mode = 'agent',
    url = 'source-retired://' || id,
    hidden = 1,
    auto_extract = 0,
    status = CASE
        WHEN EXISTS (SELECT 1 FROM news WHERE news.source_id = feed_sources.id AND news.deleted_at = '')
            THEN 'partial'
        ELSE 'unavailable'
    END,
    etag = '',
    last_modified = '',
    next_sync_at = '',
    last_error = '该公开来源未通过云端维护目录校验，已停止采集',
    gateway_status = '',
    gateway_cursor = '',
    gateway_has_more = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM plaud_retired_public_sources);

UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '公共来源已切换到云端统一采集'
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (
      SELECT id FROM plaud_gateway_public_sources
      UNION ALL
      SELECT id FROM plaud_retired_public_sources
  );

DROP TABLE plaud_retired_public_sources;
DROP TABLE plaud_gateway_public_sources;

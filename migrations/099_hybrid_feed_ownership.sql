-- Hybrid feed ownership: ordinary credential-free Feed URLs are collected by
-- Plaud, while only the 16 compatibility sources stay on Source Gateway.
-- This migration changes source routing only. Article history, full text,
-- preferences, subscriptions, Focus relations and pagination cursors remain.

CREATE TEMP TABLE plaud_hybrid_gateway_sources (id TEXT PRIMARY KEY);
INSERT INTO plaud_hybrid_gateway_sources(id) VALUES
    ('wechat-aiera'),
    ('wechat-jiqizhixin'),
    ('wechat-geekpark'),
    ('wechat-qbitai'),
    ('bilibili-tech-aesthetics'),
    ('bilibili-yingshijufeng'),
    ('bilibili-geekerwan'),
    ('bilibili-hardware-tea'),
    ('youtube-anthropic'),
    ('youtube-google-deepmind'),
    ('youtube-nvidia'),
    ('youtube-fireship'),
    ('elon-musk-x'),
    ('sam-altman-x'),
    ('andrej-karpathy-x'),
    ('demis-hassabis-x');

UPDATE feed_sources
SET collection_mode = 'source_gateway',
    url = 'source-gateway://' || id,
    sync_interval_seconds = 1800,
    etag = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE etag END,
    last_modified = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE last_modified END,
    next_sync_at = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE next_sync_at END,
    last_error = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE last_error END,
    gateway_status = CASE WHEN gateway_status = '' THEN 'initializing' ELSE gateway_status END,
    gateway_has_more = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN 1 ELSE gateway_has_more END,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM plaud_hybrid_gateway_sources);

CREATE TEMP TABLE plaud_hybrid_local_feeds (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    url TEXT NOT NULL,
    sync_interval_seconds INTEGER NOT NULL
);
INSERT INTO plaud_hybrid_local_feeds(id, kind, url, sync_interval_seconds) VALUES
    ('openai-news', 'rss', 'https://openai.com/news/rss.xml', 7200),
    ('sspai', 'rss', 'https://sspai.com/feed', 7200),
    ('arxiv-ai', 'atom', 'https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending', 21600),
    ('hkex-regulatory-announcements', 'rss', 'https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK', 21600);

UPDATE feed_sources
SET is_default = 1,
    kind = (SELECT local.kind FROM plaud_hybrid_local_feeds local WHERE local.id = feed_sources.id),
    collection_mode = 'app',
    url = (SELECT local.url FROM plaud_hybrid_local_feeds local WHERE local.id = feed_sources.id),
    sync_interval_seconds = (SELECT local.sync_interval_seconds FROM plaud_hybrid_local_feeds local WHERE local.id = feed_sources.id),
    etag = '',
    last_modified = '',
    next_sync_at = '',
    last_error = '',
    gateway_status = '',
    gateway_has_more = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM plaud_hybrid_local_feeds);

UPDATE feed_catalog_entries
SET feed_format = (SELECT local.kind FROM plaud_hybrid_local_feeds local WHERE local.id = feed_catalog_entries.source_id),
    validation_status = 'verified',
    catalog_origin = 'zettlab-maintained-local-feed'
WHERE source_id IN (SELECT id FROM plaud_hybrid_local_feeds);

CREATE TEMP TABLE plaud_hybrid_decommissioned_sources (id TEXT PRIMARY KEY);
INSERT INTO plaud_hybrid_decommissioned_sources(id) VALUES
    ('wechat-ali-tech'),
    ('wechat-chaping'),
    ('wechat-paperweekly'),
    ('bilibili-machine-heart'),
    ('bilibili-qbit'),
    ('bilibili-geekpark'),
    ('bilibili-he-tongxue'),
    ('bilibili-li-mu-ai'),
    ('bilibili-ai-tech-review'),
    ('youtube-two-minute-papers'),
    ('youtube-ai-explained'),
    ('youtube-computerphile'),
    ('youtube-lex-fridman'),
    ('dario-amodei-x'),
    ('donald-trump-x'),
    ('jensen-huang-x'),
    ('satya-nadella-x'),
    ('tibo-sottiaux-x'),
    ('eurlex-legislation'),
    ('china-policy-library'),
    ('cninfo-disclosures'),
    ('crossref-research'),
    ('epo-publication-server'),
    ('govinfo-federal-register'),
    ('hacker-news'),
    ('sec-xbrl-filings'),
    ('sse-periodic-reports');

CREATE TEMP TABLE plaud_hybrid_previously_retired_sources (id TEXT PRIMARY KEY);
INSERT INTO plaud_hybrid_previously_retired_sources(id) VALUES
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
    etag = '',
    last_modified = '',
    next_sync_at = '',
    last_error = '该维护来源已停止采集，历史内容仍可离线查看',
    gateway_status = '',
    gateway_has_more = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id FROM plaud_hybrid_decommissioned_sources
    UNION ALL
    SELECT id FROM plaud_hybrid_previously_retired_sources
);

-- These rows are delivery bookkeeping, not user subscriptions. Only Gateway
-- owned sources may remain in the cloud outbox.
DELETE FROM source_gateway_subscription_outbox
WHERE source_id NOT IN (SELECT id FROM plaud_hybrid_gateway_sources);

UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '该维护来源已停止采集，保留历史缓存',
    next_retry_at = ''
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (
      SELECT id FROM plaud_hybrid_decommissioned_sources
      UNION ALL
      SELECT id FROM plaud_hybrid_previously_retired_sources
  );

DROP TABLE plaud_hybrid_previously_retired_sources;
DROP TABLE plaud_hybrid_decommissioned_sources;
DROP TABLE plaud_hybrid_local_feeds;
DROP TABLE plaud_hybrid_gateway_sources;

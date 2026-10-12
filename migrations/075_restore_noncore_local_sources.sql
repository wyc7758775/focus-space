-- Source Gateway V1 only serves the 16 gateway_enabled core sources. Migration
-- 074 moved every historical default source to that path, leaving the other
-- subscriptions with no collector. Restore those non-core sources to their
-- previous bounded app collector while preserving articles and preferences.

CREATE TEMP TABLE plaud_noncore_source_restore (
    id TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    sync_interval_seconds INTEGER NOT NULL
);

INSERT INTO plaud_noncore_source_restore(id, url, sync_interval_seconds) VALUES
    ('arxiv-ai', 'https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending', 21600),
    ('bilibili-ai-tech-review', 'https://space.bilibili.com/216720985/video', 3600),
    ('bilibili-geekpark', 'https://space.bilibili.com/13260662/video', 3600),
    ('bilibili-he-tongxue', 'https://space.bilibili.com/163637592/video', 3600),
    ('bilibili-li-mu-ai', 'https://space.bilibili.com/1567748478/video', 3600),
    ('bilibili-machine-heart', 'https://space.bilibili.com/73414544/video', 3600),
    ('bilibili-qbit', 'https://space.bilibili.com/673779175/video', 3600),
    ('china-policy-library', 'https://www.gov.cn/zhengce/', 21600),
    ('cninfo-disclosures', 'https://www.cninfo.com.cn/new/disclosure', 21600),
    ('cnipa-patent-data', 'https://ggfw.cnipa.gov.cn', 21600),
    ('crossref-research', 'https://api.crossref.org/works?query=artificial%20intelligence&rows=50&sort=published&order=desc', 21600),
    ('dario-amodei-x', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FDarioAmodei', 3600),
    ('donald-trump-x', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FrealDonaldTrump', 3600),
    ('epo-publication-server', 'https://data.epo.org/publication-server/rest/v1.2/publication-dates', 21600),
    ('eurlex-legislation', 'https://eur-lex.europa.eu/EN/display-feed.rss?rssId=162', 21600),
    ('govinfo-federal-register', 'https://www.govinfo.gov/rss/fr.xml', 21600),
    ('hacker-news', 'https://hn.algolia.com/api/v1/search_by_date?tags=story&query=AI&hitsPerPage=50', 7200),
    ('hkex-regulatory-announcements', 'https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK', 21600),
    ('jensen-huang-x', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FJensenHuang', 3600),
    ('openai-news', 'https://openai.com/news/rss.xml', 7200),
    ('satya-nadella-x', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fsatyanadella', 3600),
    ('sec-xbrl-filings', 'https://www.sec.gov/Archives/edgar/xbrl-inline.rss.xml', 21600),
    ('sse-periodic-reports', 'https://www.sse.com.cn/disclosure/listedinfo/regular/', 21600),
    ('sspai', 'https://sspai.com/feed', 7200),
    ('tibo-sottiaux-x', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fthsottiaux', 3600),
    ('wechat-ai-paper', 'https://rsshub.rssforever.com/wechat/sogou/AI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper', 21600),
    ('wechat-ali-tech', 'https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml', 21600),
    ('wechat-chaping', 'https://wechat2rss.xlab.app/feed/8d839de8dd3290a1f1be7a94423cccb30c1b087d.xml', 21600),
    ('wechat-fnos', 'https://rsshub.rssforever.com/wechat/sogou/%E9%A3%9E%E7%89%9BFnOS', 21600),
    ('wechat-founder-park', 'https://rsshub.rssforever.com/wechat/sogou/Founder%20Park', 21600),
    ('wechat-huxiu', 'https://rsshub.rssforever.com/wechat/sogou/%E7%A1%AC%E6%B0%AA', 21600),
    ('wechat-liuxiaopai', 'https://rsshub.rssforever.com/wechat/sogou/%E5%88%98%E5%B0%8F%E6%8E%92', 21600),
    ('wechat-paperweekly', 'https://wechat2rss.xlab.app/feed/3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml', 21600),
    ('wipo-patentscope', 'https://patentscope.wipo.int/search/en/search.jsf', 21600),
    ('youtube-ai-explained', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCNJ1Ymd5yFuUPtn21xtRbbw', 3600),
    ('youtube-computerphile', 'https://www.youtube.com/feeds/videos.xml?channel_id=UC9-y-6csu5WGm29I7JiwpnA', 3600),
    ('youtube-lex-fridman', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCSHZKyawb77ixDdsGog4iWA', 3600),
    ('youtube-two-minute-papers', 'https://www.youtube.com/feeds/videos.xml?channel_id=UCbfYPyITQ-7l4upoX8nvctg', 3600);

-- During the gateway-only window a user could add the old public URL again.
-- idx_feed_sources_url is unique, so remember that duplicate and retire only
-- its collector before restoring the stable default row. The duplicate row,
-- its articles and every item preference remain intact for recovery/audit.
CREATE TEMP TABLE plaud_noncore_source_duplicates (
    duplicate_id TEXT PRIMARY KEY,
    default_id TEXT NOT NULL
);

INSERT INTO plaud_noncore_source_duplicates(duplicate_id, default_id)
SELECT duplicate.id, restore.id
FROM feed_sources duplicate
JOIN plaud_noncore_source_restore restore ON restore.url = duplicate.url
WHERE duplicate.id <> restore.id;

-- Map overlapping history to the stable row before moving the rest. Match the
-- collector identity first and fall back to the canonical URL, mirroring the
-- runtime upsert contract.
CREATE TEMP TABLE plaud_noncore_news_duplicates (
    duplicate_news_id TEXT PRIMARY KEY,
    canonical_news_id TEXT NOT NULL,
    default_id TEXT NOT NULL
);

INSERT INTO plaud_noncore_news_duplicates(duplicate_news_id, canonical_news_id, default_id)
SELECT duplicate_news_id, canonical_news_id, default_id
FROM (
    SELECT duplicate_news.id AS duplicate_news_id,
           duplicate_source.default_id AS default_id,
           COALESCE(
               (SELECT canonical.id
                FROM news canonical
                WHERE canonical.source_id = duplicate_source.default_id
                  AND duplicate_news.external_id <> ''
                  AND canonical.external_id = duplicate_news.external_id
                ORDER BY canonical.updated_at DESC, canonical.id
                LIMIT 1),
               (SELECT canonical.id
                FROM news canonical
                WHERE canonical.source_id = duplicate_source.default_id
                  AND duplicate_news.url <> ''
                  AND canonical.url = duplicate_news.url
                ORDER BY canonical.updated_at DESC, canonical.id
                LIMIT 1),
               ''
           ) AS canonical_news_id
    FROM news duplicate_news
    JOIN plaud_noncore_source_duplicates duplicate_source
      ON duplicate_source.duplicate_id = duplicate_news.source_id
) candidates
WHERE canonical_news_id <> '';

INSERT INTO news_preferences(news_id, dismissed, updated_at)
SELECT duplicate_news.canonical_news_id, preference.dismissed, preference.updated_at
FROM plaud_noncore_news_duplicates duplicate_news
JOIN news_preferences preference ON preference.news_id = duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(news_id) DO UPDATE SET
    dismissed = MAX(news_preferences.dismissed, excluded.dismissed),
    updated_at = MAX(news_preferences.updated_at, excluded.updated_at);

DELETE FROM news_preferences
WHERE news_id IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

INSERT INTO item_preferences(item_key, is_read, starred, read_later, reading_progress, updated_at)
SELECT 'news:' || duplicate_news.canonical_news_id,
       preference.is_read, preference.starred, preference.read_later,
       preference.reading_progress, preference.updated_at
FROM plaud_noncore_news_duplicates duplicate_news
JOIN item_preferences preference ON preference.item_key = 'news:' || duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(item_key) DO UPDATE SET
    is_read = MAX(item_preferences.is_read, excluded.is_read),
    starred = MAX(item_preferences.starred, excluded.starred),
    read_later = MAX(item_preferences.read_later, excluded.read_later),
    reading_progress = MAX(item_preferences.reading_progress, excluded.reading_progress),
    updated_at = MAX(item_preferences.updated_at, excluded.updated_at);

DELETE FROM item_preferences
WHERE item_key IN (
    SELECT 'news:' || duplicate_news_id FROM plaud_noncore_news_duplicates
);

INSERT INTO stable_context_items(
    owner_id, profile_id, item_key, state, added_by, added_at, removed_at,
    updated_at, content_hash, source_type, source_id, source_name,
    original_url, title, published_at, content_format, content_snapshot
)
SELECT context_item.owner_id, context_item.profile_id,
       'news:' || duplicate_news.canonical_news_id,
       context_item.state, context_item.added_by, context_item.added_at,
       context_item.removed_at, context_item.updated_at, context_item.content_hash,
       context_item.source_type, duplicate_news.default_id, context_item.source_name,
       context_item.original_url, context_item.title, context_item.published_at,
       context_item.content_format, context_item.content_snapshot
FROM plaud_noncore_news_duplicates duplicate_news
JOIN stable_context_items context_item
  ON context_item.item_key = 'news:' || duplicate_news.duplicate_news_id
WHERE 1
ON CONFLICT(owner_id, profile_id, item_key) DO UPDATE SET
    state = CASE
        WHEN stable_context_items.state = 'active' OR excluded.state = 'active' THEN 'active'
        ELSE 'removed'
    END,
    added_by = CASE
        WHEN stable_context_items.state <> 'active' AND excluded.state = 'active' THEN excluded.added_by
        ELSE stable_context_items.added_by
    END,
    added_at = MIN(stable_context_items.added_at, excluded.added_at),
    removed_at = CASE
        WHEN stable_context_items.state = 'active' OR excluded.state = 'active' THEN ''
        ELSE MAX(stable_context_items.removed_at, excluded.removed_at)
    END,
    updated_at = MAX(stable_context_items.updated_at, excluded.updated_at),
    content_hash = CASE WHEN stable_context_items.content_hash = '' THEN excluded.content_hash ELSE stable_context_items.content_hash END,
    source_id = excluded.source_id,
    source_name = CASE WHEN stable_context_items.source_name = '' THEN excluded.source_name ELSE stable_context_items.source_name END,
    original_url = CASE WHEN stable_context_items.original_url = '' THEN excluded.original_url ELSE stable_context_items.original_url END,
    title = CASE WHEN stable_context_items.title = '' THEN excluded.title ELSE stable_context_items.title END,
    published_at = CASE WHEN stable_context_items.published_at = '' THEN excluded.published_at ELSE stable_context_items.published_at END,
    content_snapshot = CASE WHEN stable_context_items.content_snapshot = '' THEN excluded.content_snapshot ELSE stable_context_items.content_snapshot END;

DELETE FROM stable_context_items
WHERE item_key IN (
    SELECT 'news:' || duplicate_news_id FROM plaud_noncore_news_duplicates
);

INSERT OR IGNORE INTO entry_full_texts(
    entry_id, content_text, content_format, fetched_at, status, error_message,
    source_url, updated_at, content_hash
)
SELECT duplicate_news.canonical_news_id, full_text.content_text,
       full_text.content_format, full_text.fetched_at, full_text.status,
       full_text.error_message, full_text.source_url, full_text.updated_at,
       full_text.content_hash
FROM plaud_noncore_news_duplicates duplicate_news
JOIN entry_full_texts full_text ON full_text.entry_id = duplicate_news.duplicate_news_id;

DELETE FROM entry_full_texts
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

INSERT OR IGNORE INTO article_ai_overviews(
    entry_id, content_hash, status, summary, key_points_json, impact,
    generated_at, error_code, prompt_version
)
SELECT duplicate_news.canonical_news_id, overview.content_hash, overview.status,
       overview.summary, overview.key_points_json, overview.impact,
       overview.generated_at, overview.error_code, overview.prompt_version
FROM plaud_noncore_news_duplicates duplicate_news
JOIN article_ai_overviews overview ON overview.entry_id = duplicate_news.duplicate_news_id;

DELETE FROM article_ai_overviews
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

UPDATE reader_digest_receipts
SET entry_id = COALESCE((
        SELECT duplicate_news.canonical_news_id
        FROM plaud_noncore_news_duplicates duplicate_news
        WHERE duplicate_news.duplicate_news_id = reader_digest_receipts.entry_id
    ), entry_id),
    source_id = COALESCE((
        SELECT duplicate_news.default_id
        FROM plaud_noncore_news_duplicates duplicate_news
        WHERE duplicate_news.duplicate_news_id = reader_digest_receipts.entry_id
    ), source_id)
WHERE entry_id IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

UPDATE news
SET deleted_at = CASE WHEN deleted_at = '' THEN CURRENT_TIMESTAMP ELSE deleted_at END,
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

-- Non-overlapping history can keep its stable news id, so every item key,
-- full-text row and context membership remains valid without rewriting.
UPDATE news
SET source_id = (
        SELECT duplicate_source.default_id
        FROM plaud_noncore_source_duplicates duplicate_source
        WHERE duplicate_source.duplicate_id = news.source_id
    ),
    source = COALESCE((
        SELECT canonical_source.name
        FROM plaud_noncore_source_duplicates duplicate_source
        JOIN feed_sources canonical_source ON canonical_source.id = duplicate_source.default_id
        WHERE duplicate_source.duplicate_id = news.source_id
    ), source),
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_noncore_source_duplicates)
  AND id NOT IN (SELECT duplicate_news_id FROM plaud_noncore_news_duplicates);

UPDATE stable_context_items
SET source_id = COALESCE((
        SELECT news.source_id FROM news WHERE 'news:' || news.id = stable_context_items.item_key
    ), source_id),
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_noncore_source_duplicates)
  AND item_key IN (
    SELECT 'news:' || id
    FROM news
    WHERE source_id IN (SELECT default_id FROM plaud_noncore_source_duplicates)
);

-- Preserve the user's subscription placement and overrides on the stable
-- default source before disabling the redundant collector.
UPDATE feed_subscriptions
SET enabled = MAX(enabled, COALESCE((
        SELECT duplicate_subscription.enabled
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), enabled)),
    folder_id = CASE WHEN folder_id = '' THEN COALESCE((
        SELECT duplicate_subscription.folder_id
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), '') ELSE folder_id END,
    title_override = CASE WHEN title_override = '' THEN COALESCE((
        SELECT duplicate_subscription.title_override
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), '') ELSE title_override END,
    pinned = MAX(pinned, COALESCE((
        SELECT duplicate_subscription.pinned
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), pinned)),
    muted = MAX(muted, COALESCE((
        SELECT duplicate_subscription.muted
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), muted)),
    sort_order = CASE WHEN sort_order = 0 THEN COALESCE((
        SELECT duplicate_subscription.sort_order
        FROM plaud_noncore_source_duplicates duplicate
        JOIN feed_subscriptions duplicate_subscription ON duplicate_subscription.source_id = duplicate.duplicate_id
        WHERE duplicate.default_id = feed_subscriptions.source_id
    ), 0) ELSE sort_order END,
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT default_id FROM plaud_noncore_source_duplicates);

INSERT OR IGNORE INTO feed_subscription_tags(source_id, tag_id, created_at)
SELECT duplicate_source.default_id, tag.tag_id, tag.created_at
FROM plaud_noncore_source_duplicates duplicate_source
JOIN feed_subscription_tags tag ON tag.source_id = duplicate_source.duplicate_id;

INSERT OR IGNORE INTO source_pack_members(pack_id, source_id, sort_order)
SELECT member.pack_id, duplicate_source.default_id, member.sort_order
FROM plaud_noncore_source_duplicates duplicate_source
JOIN source_pack_members member ON member.source_id = duplicate_source.duplicate_id;

UPDATE feed_sources
SET url = 'source-merged://' || id,
    collection_mode = 'agent',
    hidden = 1,
    status = 'partial',
    next_sync_at = '',
    last_error = '同一公开来源已由默认订阅接管',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN (SELECT duplicate_id FROM plaud_noncore_source_duplicates);

UPDATE feed_subscriptions
SET enabled = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE source_id IN (SELECT duplicate_id FROM plaud_noncore_source_duplicates);

UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '重复来源已合并到默认订阅'
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (SELECT duplicate_id FROM plaud_noncore_source_duplicates);

UPDATE feed_sources
SET collection_mode = 'app',
    url = (SELECT restore.url FROM plaud_noncore_source_restore restore WHERE restore.id = feed_sources.id),
    sync_interval_seconds = (SELECT restore.sync_interval_seconds FROM plaud_noncore_source_restore restore WHERE restore.id = feed_sources.id),
    etag = '',
    last_modified = '',
    next_sync_at = '',
    last_error = '',
    gateway_status = '',
    gateway_cursor = '',
    gateway_has_more = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE is_default = 1
  AND collection_mode = 'source_gateway'
  AND id IN (SELECT id FROM plaud_noncore_source_restore);

UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '非核心来源已恢复本地受限采集'
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (SELECT id FROM plaud_noncore_source_restore);

DROP TABLE plaud_noncore_news_duplicates;
DROP TABLE plaud_noncore_source_duplicates;
DROP TABLE plaud_noncore_source_restore;

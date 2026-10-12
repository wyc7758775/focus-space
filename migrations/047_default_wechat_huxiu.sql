-- Add the verified public WeChat feed for 硬氪.
-- This migration is additive and must not reset an existing subscription's
-- enabled, folder, sort, visibility, or source-health state on replay.
CREATE TEMP TABLE _default_wechat_huxiu_source (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    source_sort INTEGER NOT NULL,
    popularity INTEGER NOT NULL
);

INSERT INTO _default_wechat_huxiu_source
    (id, name, url, description, icon_text, icon_color, tags_json,
     source_sort, popularity)
VALUES
    ('wechat-huxiu', '硬氪',
     'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E7%A1%AC%E6%B0%AA',
     '科技产业、创业投资与商业趋势', '硬', '#0f766e',
     '["科技公众号","创业","商业"]', 120, 90);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'json', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 21600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _default_wechat_huxiu_source;

-- If the user already added this public feed, update only curated metadata
-- and keep the existing source health and runtime state intact.
UPDATE feed_sources
SET kind='json',
    name=(SELECT name FROM _default_wechat_huxiu_source d WHERE d.url=feed_sources.url),
    description=(SELECT description FROM _default_wechat_huxiu_source d WHERE d.url=feed_sources.url),
    is_default=1,
    collection_mode='app',
    sync_interval_seconds=21600,
    backfill_days=30,
    retention_days=90,
    retention_limit=1000,
    auto_extract=1,
    updated_at=CURRENT_TIMESTAMP
WHERE url IN (SELECT url FROM _default_wechat_huxiu_source);

INSERT OR IGNORE INTO subscription_folders
    (id, name, sort_order, created_at, updated_at)
VALUES ('folder-wechat-tech', '科技公众号', 60, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT f.id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
       'folder-wechat-tech', d.source_sort
FROM _default_wechat_huxiu_source d
JOIN feed_sources f ON f.url=d.url;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, icon_url,
     tags_json, feed_format, recommended, popularity, validation_status,
     validated_at, catalog_origin)
SELECT f.id, 'https://mp.weixin.qq.com/', 'new-media', 'zh-CN', d.icon_text,
       d.icon_color, '', d.tags_json, 'rss', 1, d.popularity, 'verified',
       '2026-08-14T00:00:00Z', 'zettlab-maintained-public-wechat-rss-v1'
FROM _default_wechat_huxiu_source d
JOIN feed_sources f ON f.url=d.url
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url,
    category=excluded.category,
    language=excluded.language,
    icon_text=excluded.icon_text,
    icon_color=excluded.icon_color,
    icon_url=excluded.icon_url,
    tags_json=excluded.tags_json,
    feed_format=excluded.feed_format,
    recommended=excluded.recommended,
    popularity=excluded.popularity,
    validation_status=excluded.validation_status,
    validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'wechat-tech', f.id, d.source_sort
FROM _default_wechat_huxiu_source d
JOIN feed_sources f ON f.url=d.url
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _default_wechat_huxiu_source;

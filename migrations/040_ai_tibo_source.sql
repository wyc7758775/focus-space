-- Add the OpenAI/Codex public account to the Agent-owned AI key-people pack.
-- This migration is additive and must not reset an existing user's subscription
-- or presentation preferences when an installed dashboard is upgraded.
CREATE TEMP TABLE _ai_tibo_source (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    handle TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    site_url TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    source_sort INTEGER NOT NULL
);

INSERT INTO _ai_tibo_source
    (id, name, handle, url, description, site_url, icon_text, icon_color, tags_json, source_sort)
VALUES
    ('tibo-sottiaux-x', 'Tibo', 'thsottiaux',
     'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fthsottiaux',
     'OpenAI/Codex 公开动态与产品更新', 'https://x.com/thsottiaux', 'T', '#10a37f',
     '["关键人物","AI","Codex"]', 90);

INSERT INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'api', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 3600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _ai_tibo_source
WHERE 1
ON CONFLICT(id) DO UPDATE SET
    kind=excluded.kind, name=excluded.name, url=excluded.url,
    description=excluded.description, is_default=excluded.is_default,
    collection_mode=excluded.collection_mode,
    sync_interval_seconds=excluded.sync_interval_seconds,
    backfill_days=excluded.backfill_days, retention_days=excluded.retention_days,
    retention_limit=excluded.retention_limit, auto_extract=excluded.auto_extract,
    hidden=CASE WHEN feed_sources.hidden IN (0, 1) THEN feed_sources.hidden ELSE excluded.hidden END,
    updated_at=CURRENT_TIMESTAMP;

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-key-people', '关键人物', 50, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'folder-key-people', source_sort
FROM _ai_tibo_source;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at,
     catalog_origin)
SELECT id, site_url, 'social-media', 'en', icon_text, icon_color, tags_json,
       'json', 1, 0, 'verified', '2026-08-12T00:00:00Z',
       'zettlab-maintained-public-aggregator-v1'
FROM _ai_tibo_source
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url, category=excluded.category,
    language=excluded.language, icon_text=excluded.icon_text,
    icon_color=excluded.icon_color, tags_json=excluded.tags_json,
    feed_format=excluded.feed_format, recommended=excluded.recommended,
    validation_status=excluded.validation_status,
    validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin;

INSERT INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('key-people', 'AI 关键人物', 'AI 公司、算力平台与技术路线关键人物', 'social-media', '人', 80)
ON CONFLICT(id) DO UPDATE SET
    name=excluded.name, description=excluded.description,
    category=excluded.category, icon_text=excluded.icon_text,
    sort_order=excluded.sort_order;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'key-people', id, source_sort FROM _ai_tibo_source
WHERE 1
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _ai_tibo_source;

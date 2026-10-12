-- Fixed public-aggregator sources for high-signal public figures. Source IDs
-- and source URLs are stable so repeated migrations preserve subscriptions and
-- all previously collected entries.
CREATE TEMP TABLE _folo_key_people_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    site_url TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    source_sort INTEGER NOT NULL
);

INSERT INTO _folo_key_people_sources
    (id, name, url, description, site_url, icon_text, icon_color, tags_json, source_sort)
VALUES
    ('elon-musk-x', 'Elon Musk', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Felonmusk', 'Elon Musk 的公开动态聚合', 'https://x.com/elonmusk', 'M', '#2563eb', '["关键人物","公开动态","科技"]', 10),
    ('donald-trump-x', 'Donald Trump', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FrealDonaldTrump', 'Donald Trump 的公开动态聚合', 'https://x.com/realDonaldTrump', 'T', '#dc2626', '["关键人物","公开动态","政策"]', 20);

INSERT INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'api', name, url, description, 1, 'unavailable', 0, 0, '',
       '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 3600, CURRENT_TIMESTAMP,
       30, 90, 1000, 1, 0, '', ''
FROM _folo_key_people_sources
WHERE 1
ON CONFLICT(id) DO UPDATE SET
    kind=excluded.kind, name=excluded.name, url=excluded.url,
    description=excluded.description, is_default=excluded.is_default,
    collection_mode=excluded.collection_mode,
    sync_interval_seconds=excluded.sync_interval_seconds,
    backfill_days=excluded.backfill_days, retention_days=excluded.retention_days,
    retention_limit=excluded.retention_limit, auto_extract=excluded.auto_extract,
    hidden=excluded.hidden, updated_at=CURRENT_TIMESTAMP;

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-key-people', '关键人物', 50, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'folder-key-people', source_sort
FROM _folo_key_people_sources;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at,
     catalog_origin)
SELECT id, site_url, 'social-media', 'en', icon_text, icon_color, tags_json,
       'json', 1, 0, 'verified', '2026-08-11T00:00:00Z',
       'zettlab-maintained-public-aggregator-v1'
FROM _folo_key_people_sources
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET site_url=excluded.site_url,
    category=excluded.category, language=excluded.language,
    icon_text=excluded.icon_text, icon_color=excluded.icon_color,
    tags_json=excluded.tags_json, feed_format=excluded.feed_format,
    recommended=excluded.recommended, validation_status=excluded.validation_status,
    validated_at=excluded.validated_at, catalog_origin=excluded.catalog_origin;

INSERT INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('key-people', '关键人物', '重要公开人物的公开动态', 'social-media', '人', 80)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description,
    category=excluded.category, icon_text=excluded.icon_text, sort_order=excluded.sort_order;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'key-people', id, source_sort FROM _folo_key_people_sources
WHERE 1
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _folo_key_people_sources;

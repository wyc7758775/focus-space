-- Bilibili/Folo adapters return a source history window, not only today's
-- uploads. Do not discard that window during the first sync because the old
-- 30-day backfill made valid June/July videos look like an empty source.
UPDATE feed_sources
SET backfill_days=3650,
    retention_days=3650,
    retention_limit=10000,
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id LIKE 'bilibili-%';

UPDATE feed_catalog_entries
SET catalog_origin='zettlab-bilibili-folo-and-api-v2'
WHERE source_id LIKE 'bilibili-%';

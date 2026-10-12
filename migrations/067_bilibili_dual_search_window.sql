-- The anonymous public collector now merges click- and publish-date windows.
-- Reset the cursor once so installations already on migration 066 receive
-- both windows without deleting previously stored videos.
UPDATE feed_sources
SET records_seen=0,
    records_used=0,
    last_sync_at='',
    last_error='',
    status='unavailable',
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id LIKE 'bilibili-%';

UPDATE feed_catalog_entries
SET catalog_origin='zettlab-bilibili-dual-search-window-v4'
WHERE source_id LIKE 'bilibili-%';

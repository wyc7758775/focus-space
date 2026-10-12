-- Re-run Bilibili sources after switching from the relevance search/all page
-- to the larger video-search window. Existing rows remain in place; only the
-- sync cursor is reset so the new collector can merge the wider result set.
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
SET catalog_origin='zettlab-bilibili-search-type-window-v3'
WHERE source_id LIKE 'bilibili-%';

-- Re-run the first history backfill for Bilibili sources that were previously
-- marked synced but persisted no entries because the old 30-day cutoff filtered
-- their provider history. Keep existing news rows; only reset the sync cursor.
UPDATE feed_sources
SET records_seen=0,
    records_used=0,
    last_sync_at='',
    last_error='',
    status='unavailable',
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id LIKE 'bilibili-%';

-- Bilibili history is now fetched one provider page per user scroll. Reset
-- the cursor once so existing sources start at the newest public window.
UPDATE feed_sources
SET records_seen=0,
    records_used=0,
    last_sync_at='',
    last_error='',
    last_modified='',
    status='unavailable',
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id LIKE 'bilibili-%';

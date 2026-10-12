-- The SEC RSS endpoint is frequently blocked, while the official anonymous
-- full-text index supports stable 100-row offsets. Use that typed API directly
-- and continue the bounded history window across sync rounds.

UPDATE feed_sources
SET url = 'https://efts.sec.gov/LATEST/search-index',
    backfill_days = 3650,
    retention_days = 3650,
    retention_limit = 1000,
    etag = '',
    last_modified = '',
    backfill_cursor = '',
    last_sync_at = '',
    next_sync_at = '',
    last_error = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'sec-xbrl-filings'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET site_url = 'https://www.sec.gov/edgar/search/',
    feed_format = 'json',
    validation_status = 'verified',
    validated_at = CURRENT_TIMESTAMP
WHERE source_id = 'sec-xbrl-filings';

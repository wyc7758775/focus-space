-- The legacy GovInfo RSS endpoint is intermittently empty. The official
-- Federal Register API exposes a bounded 20-item cursor page and 50 pages.

UPDATE feed_sources
SET url = 'https://www.federalregister.gov/api/v1/documents.json?per_page=20&order=newest',
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
WHERE id = 'govinfo-federal-register'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET site_url = 'https://www.federalregister.gov/',
    feed_format = 'json',
    validation_status = 'verified',
    validated_at = CURRENT_TIMESTAMP
WHERE source_id = 'govinfo-federal-register';

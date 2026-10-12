-- The static disclosure JSON only exposes a small current window. The official
-- bulletin query provides anonymous, typed annual-report history in 30-item pages.

UPDATE feed_sources
SET url = 'https://query.sse.com.cn/security/stock/queryCompanyBulletin.do',
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
WHERE id = 'sse-periodic-reports'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET site_url = 'https://www.sse.com.cn/disclosure/listedinfo/regular/',
    feed_format = 'json',
    validation_status = 'verified',
    validated_at = CURRENT_TIMESTAMP
WHERE source_id = 'sse-periodic-reports';

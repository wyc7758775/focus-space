-- The SEC monthly RSS embeds every filing attachment and can take minutes to
-- transfer. Requeue the source for the compact official quarterly XBRL index.
-- Existing entries, preferences, stable context and subscription state stay
-- intact; the collector reuses the released accession:form identity.
UPDATE feed_sources
SET backfill_days = 3650,
    retention_days = 3650,
    retention_limit = 1000,
    etag = '',
    last_modified = '',
    backfill_cursor = '',
    last_sync_at = '',
    next_sync_at = '',
    last_error = '',
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE id = 'sec-xbrl-filings'
  AND url = 'https://efts.sec.gov/LATEST/search-index'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET site_url = 'https://www.sec.gov/data-research/structured-data/structured-disclosure-rss-feeds',
    feed_format = 'idx',
    validation_status = 'verified',
    validated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE source_id = 'sec-xbrl-filings';

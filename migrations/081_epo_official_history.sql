-- EPO's official publication server exposes the full publication-date index.
-- Resume history by immutable date URLs while keeping each sync bounded.

UPDATE feed_sources
SET url = 'https://data.epo.org/publication-server/rest/v1.2/publication-dates',
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
WHERE id = 'epo-publication-server'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET feed_format = 'html', validation_status = 'verified', validated_at = CURRENT_TIMESTAMP
WHERE source_id = 'epo-publication-server';

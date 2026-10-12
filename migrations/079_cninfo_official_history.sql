-- CNINFO's disclosure page is only a script shell. Its anonymous official
-- query endpoint returns 30 typed announcements per page, including PDF URLs.

UPDATE feed_sources
SET url = 'https://www.cninfo.com.cn/new/hisAnnouncement/query',
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
WHERE id = 'cninfo-disclosures'
  AND collection_mode = 'app';

UPDATE feed_catalog_entries
SET feed_format = 'json', validation_status = 'verified', validated_at = CURRENT_TIMESTAMP
WHERE source_id = 'cninfo-disclosures';

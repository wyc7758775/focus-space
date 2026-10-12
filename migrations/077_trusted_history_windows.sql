-- Two fixed official sources already return a bounded multi-year archive in a
-- single response. Requeue them with a larger disk retention window while the
-- parser keeps unknown/user-provided feeds at the existing 100-item limit.

UPDATE feed_sources
SET backfill_days = 5000,
    retention_days = 5000,
    retention_limit = 1500,
    etag = '',
    last_modified = '',
    last_sync_at = '',
    next_sync_at = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN ('openai-news', 'china-policy-library')
  AND collection_mode = 'app';

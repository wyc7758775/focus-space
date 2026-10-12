-- EUR-Lex publishes a complete 100-item official RSS window in one response.
-- Requeue it so installs previously capped at 20 retain the full window and
-- can accumulate older items across later refreshes without unbounded memory.

UPDATE feed_sources
SET backfill_days = 3650,
    retention_days = 3650,
    retention_limit = 1000,
    etag = '',
    last_modified = '',
    last_sync_at = '',
    next_sync_at = '',
    last_error = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'eurlex-legislation'
  AND collection_mode = 'app';

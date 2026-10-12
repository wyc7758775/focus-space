-- Fixed public WeChat feeds often expose a small historical window whose
-- entries are older than 30 days. Keep that bounded window instead of
-- successfully fetching it and then discarding every item at selection time.

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
WHERE id IN (
    'wechat-ai-paper',
    'wechat-ali-tech',
    'wechat-chaping',
    'wechat-fnos',
    'wechat-founder-park',
    'wechat-huxiu',
    'wechat-liuxiaopai',
    'wechat-paperweekly'
)
  AND collection_mode = 'app';

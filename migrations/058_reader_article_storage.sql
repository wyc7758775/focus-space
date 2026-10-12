-- Full article projections are durable AC reader data.  Do not delete them on
-- upgrade: the reader handler refreshes sanitized projections on demand and
-- keeps the newest bounded set in entry_full_texts.

-- Make the curated 硬氪 source run in the first due batch after an upgrade.
-- The public Folo/RSSHub endpoint is already pinned in migration 047; this
-- only clears the stale "not yet synced" state without touching subscription
-- ownership or folder placement.
UPDATE feed_sources
SET status = 'unavailable',
    last_error = '等待首次同步',
    next_sync_at = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'wechat-huxiu';

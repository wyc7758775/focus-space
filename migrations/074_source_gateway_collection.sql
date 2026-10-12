-- Switch curated default sources to the Source Gateway collection path.
--
-- Product contract (2026-08-17 Plaud Source Gateway V1):
--   * Default sources keep their existing stable source_id values.
--   * Default sources no longer carry a third-party fetch URL; content now
--     arrives through the device-local Source Gateway proxy instead of direct
--     app-side HTTP collection.
--   * Old sync cursors, ETag and next_sync_at state are cleared so no stale
--     direct-fetch schedule survives the switch, but the `news` table and every
--     user preference are preserved verbatim.  Favorites, read-later and
--     knowledge-base memberships live in item_preferences / stable_context_items
--     and are never touched here.
--   * Gateway entries merge with existing rows by (source_id, external_id) and
--     fall back to (source_id, url) so upgraded installs do not show duplicates.
--
-- This migration is additive and reversible by app version rollback: it never
-- deletes news rows, preferences, context memberships or full-text cache.

ALTER TABLE news ADD COLUMN gateway_entry_id TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_sources ADD COLUMN gateway_status TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_sources ADD COLUMN gateway_cursor TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_sources ADD COLUMN gateway_has_more INTEGER NOT NULL DEFAULT 0 CHECK (gateway_has_more IN (0, 1));

CREATE INDEX IF NOT EXISTS idx_news_gateway_entry
    ON news(gateway_entry_id)
    WHERE gateway_entry_id <> '';

UPDATE feed_sources
SET collection_mode = 'source_gateway',
    -- idx_feed_sources_url is UNIQUE, so the retired fetch URL is replaced by a
    -- unique non-public marker, never a third-party address.  validPublicFeedURL
    -- rejects the scheme, which makes any residual direct-fetch path fail closed.
    url = 'source-gateway://' || id,
    etag = '',
    last_modified = '',
    next_sync_at = '',
    sync_interval_seconds = 1800,
    status = CASE
        WHEN EXISTS (SELECT 1 FROM news n WHERE n.source_id = feed_sources.id AND n.deleted_at = '')
            THEN 'ok'
        ELSE 'unavailable'
    END,
    last_error = '',
    gateway_status = 'initializing',
    gateway_has_more = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE is_default = 1 AND collection_mode <> 'source_gateway';

-- A pending direct-fetch request must not survive the switch: the gateway sync
-- path owns these sources from now on and enqueues its own requests.
UPDATE sync_requests
SET status = 'superseded',
    completed_at = CURRENT_TIMESTAMP,
    message = '默认来源已切换到云端来源网关'
WHERE status IN ('pending', 'retry_wait')
  AND source_id IN (SELECT id FROM feed_sources WHERE collection_mode = 'source_gateway');

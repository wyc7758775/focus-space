-- Tibo is a maintained X source again. The server-side RSSHub adapter owns
-- platform credentials and media normalization; the device keeps only the
-- normalized Source Gateway projection and existing user state.
UPDATE feed_sources
SET is_default = 1,
    kind = 'api',
    collection_mode = 'source_gateway',
    url = 'source-gateway://tibo-sottiaux-x',
    hidden = 0,
    auto_extract = 1,
    sync_interval_seconds = 1800,
    etag = '',
    last_modified = '',
    next_sync_at = '',
    last_error = '',
    gateway_status = 'initializing',
    gateway_cursor = '',
    gateway_has_more = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'tibo-sottiaux-x';

UPDATE feed_catalog_entries
SET category = 'social-media',
    language = 'en',
    feed_format = 'json',
    recommended = 1,
    validation_status = 'verified',
    validated_at = '2026-08-27T00:00:00Z',
    catalog_origin = 'zettlab-source-gateway-v4'
WHERE source_id = 'tibo-sottiaux-x';

INSERT OR IGNORE INTO source_gateway_subscription_outbox(
    operation_id, source_id, enabled, created_at, updated_at
)
SELECT 'plaud-sub:tibo-sottiaux-x:' || COALESCE(subscription.enabled, 0),
       'tibo-sottiaux-x', COALESCE(subscription.enabled, 0),
       CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM feed_sources source
LEFT JOIN feed_subscriptions subscription ON subscription.source_id = source.id
WHERE source.id = 'tibo-sottiaux-x';

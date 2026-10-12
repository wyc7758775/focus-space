-- X sources are collected from direct RSS mirrors. Keep the full mirror
-- window during first sync instead of applying the old 30-day/1000-row
-- defaults that were tuned for the Folo JSON page.
UPDATE feed_sources
SET backfill_days=3650,
    retention_days=3650,
    retention_limit=10000,
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id IN (
    'elon-musk-x',
    'donald-trump-x',
    'sam-altman-x',
    'dario-amodei-x',
    'demis-hassabis-x',
    'jensen-huang-x',
    'satya-nadella-x',
    'andrej-karpathy-x',
    'tibo-sottiaux-x'
);

UPDATE feed_catalog_entries
SET feed_format='rss',
    catalog_origin='zettlab-maintained-x-rss-mirrors-v1'
WHERE source_id IN (
    'elon-musk-x',
    'donald-trump-x',
    'sam-altman-x',
    'dario-amodei-x',
    'demis-hassabis-x',
    'jensen-huang-x',
    'satya-nadella-x',
    'andrej-karpathy-x',
    'tibo-sottiaux-x'
);

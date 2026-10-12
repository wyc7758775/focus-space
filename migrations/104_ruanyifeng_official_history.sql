-- The public Atom window currently exposes only three recent posts. Keep the
-- official weekly archive inside the same source and allow its bounded history
-- to survive the first-sync cutoff.
UPDATE feed_sources
SET backfill_days=3650,
    retention_days=3650,
    retention_limit=1000,
    etag='',
    last_modified='',
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id='top-rss-002-836ccfdca641'
  AND url='https://www.ruanyifeng.com/blog/atom.xml';

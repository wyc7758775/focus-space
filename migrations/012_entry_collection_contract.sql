-- Standard entry identity and bounded public-source collection settings.
-- All additions are nullable-compatible with the previous dashboard schema;
-- existing rows are backfilled with their legacy id as external_id.
ALTER TABLE feed_sources ADD COLUMN collection_mode TEXT NOT NULL DEFAULT 'app';
ALTER TABLE feed_sources ADD COLUMN sync_interval_seconds INTEGER NOT NULL DEFAULT 7200;
ALTER TABLE feed_sources ADD COLUMN next_sync_at TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_sources ADD COLUMN backfill_days INTEGER NOT NULL DEFAULT 30;
ALTER TABLE feed_sources ADD COLUMN retention_days INTEGER NOT NULL DEFAULT 90;
ALTER TABLE feed_sources ADD COLUMN retention_limit INTEGER NOT NULL DEFAULT 1000;
ALTER TABLE feed_sources ADD COLUMN auto_extract INTEGER NOT NULL DEFAULT 1 CHECK (auto_extract IN (0, 1));
ALTER TABLE feed_sources ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1));
ALTER TABLE feed_sources ADD COLUMN etag TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_sources ADD COLUMN last_modified TEXT NOT NULL DEFAULT '';

ALTER TABLE news ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN fetched_at TEXT NOT NULL DEFAULT '';

UPDATE news SET external_id = id WHERE source_id <> '' AND external_id = '';
UPDATE feed_sources
SET collection_mode = CASE WHEN collection_mode = '' THEN 'app' ELSE collection_mode END,
    sync_interval_seconds = CASE WHEN sync_interval_seconds < 300 OR sync_interval_seconds > 604800 THEN 7200 ELSE sync_interval_seconds END,
    backfill_days = CASE WHEN backfill_days < 0 OR backfill_days > 3650 THEN 30 ELSE backfill_days END,
    retention_days = CASE WHEN retention_days < 1 OR retention_days > 3650 THEN 90 ELSE retention_days END,
    retention_limit = CASE WHEN retention_limit < 10 OR retention_limit > 10000 THEN 1000 ELSE retention_limit END,
    next_sync_at = CASE WHEN EXISTS (SELECT 1 FROM feed_subscriptions s WHERE s.source_id = feed_sources.id AND s.enabled = 1) AND next_sync_at = '' THEN CURRENT_TIMESTAMP ELSE next_sync_at END;

CREATE UNIQUE INDEX IF NOT EXISTS idx_news_source_external
    ON news(source_id, external_id)
    WHERE source_id <> '' AND external_id <> '';
CREATE INDEX IF NOT EXISTS idx_news_source_fetched
    ON news(source_id, fetched_at DESC, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_feed_sources_due
    ON feed_sources(next_sync_at, status, collection_mode);

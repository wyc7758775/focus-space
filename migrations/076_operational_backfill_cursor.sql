-- Keep bounded local-collector history progress separate from HTTP validators.
-- Existing rows are preserved; the three offset-paged sources are queued once
-- so installs that previously stopped after the first 60 rows resume history.

ALTER TABLE feed_sources ADD COLUMN backfill_cursor TEXT NOT NULL DEFAULT '';

UPDATE feed_sources
SET last_sync_at = '',
    next_sync_at = '',
    backfill_cursor = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN ('arxiv-ai', 'crossref-research', 'hacker-news')
  AND collection_mode = 'app';

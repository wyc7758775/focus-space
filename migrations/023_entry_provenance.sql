-- Preserve the evidence contract for every normalized public-source entry.
-- Empty defaults keep older Agent and dashboard payloads compatible.
ALTER TABLE news ADD COLUMN permission_scope TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN source_level TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN failure_reason TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_news_provenance
    ON news(source_id, source_level, fetched_at DESC);

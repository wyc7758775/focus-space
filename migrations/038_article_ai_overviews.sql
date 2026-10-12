-- Agent-produced article overviews are keyed to the exact content revision.
-- A changed content_hash never reuses a stale overview.
ALTER TABLE app_settings ADD COLUMN auto_generate_article_overviews INTEGER NOT NULL DEFAULT 1
    CHECK (auto_generate_article_overviews IN (0, 1));

CREATE TABLE IF NOT EXISTS article_ai_overviews (
    entry_id TEXT NOT NULL REFERENCES news(id) ON DELETE CASCADE,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'ready', 'failed')),
    summary TEXT NOT NULL DEFAULT '',
    key_points_json TEXT NOT NULL DEFAULT '[]',
    impact TEXT NOT NULL DEFAULT '',
    generated_at TEXT NOT NULL,
    error_code TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (entry_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_article_ai_overviews_generated
    ON article_ai_overviews(generated_at DESC);

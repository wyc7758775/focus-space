ALTER TABLE feed_catalog_entries ADD COLUMN icon_url TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS source_icon_cache (
    source_id TEXT PRIMARY KEY REFERENCES feed_sources(id) ON DELETE CASCADE,
    icon_url TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    body BLOB NOT NULL DEFAULT X'',
    fetched_at TEXT NOT NULL DEFAULT '',
    retry_after TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    CHECK (length(icon_url) <= 1000),
    CHECK (length(content_type) <= 64),
    CHECK (length(body) <= 262144),
    CHECK (length(last_error) <= 500)
);

CREATE INDEX IF NOT EXISTS idx_source_icon_cache_fetched
ON source_icon_cache(fetched_at DESC, source_id);

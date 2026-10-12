CREATE TABLE IF NOT EXISTS entry_full_texts (
    entry_id TEXT PRIMARY KEY REFERENCES news(id) ON DELETE CASCADE,
    content_text TEXT NOT NULL DEFAULT '',
    content_format TEXT NOT NULL DEFAULT 'text',
    fetched_at TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unavailable',
    error_message TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_entry_full_text_status
    ON entry_full_texts(status, updated_at DESC);

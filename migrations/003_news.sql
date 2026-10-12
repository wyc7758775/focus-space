CREATE TABLE IF NOT EXISTS news (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    url TEXT NOT NULL,
    source TEXT NOT NULL,
    published_at TEXT NOT NULL,
    summary_text TEXT NOT NULL,
    content_format TEXT NOT NULL,
    safe_html TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS news_preferences (
    news_id TEXT PRIMARY KEY,
    dismissed INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_news_published_at ON news(published_at DESC, updated_at DESC);

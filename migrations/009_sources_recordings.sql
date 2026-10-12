CREATE TABLE IF NOT EXISTS feed_sources (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'rss',
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),
    status TEXT NOT NULL DEFAULT 'unavailable',
    records_seen INTEGER NOT NULL DEFAULT 0,
    records_used INTEGER NOT NULL DEFAULT 0,
    last_sync_at TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feed_subscriptions (
    source_id TEXT PRIMARY KEY REFERENCES feed_sources(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    subscribed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, created_at, updated_at)
VALUES
    ('openai-news', 'rss', 'OpenAI News', 'https://openai.com/news/rss.xml', 'OpenAI 官方新闻更新', 1, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('hacker-news', 'rss', 'Hacker News', 'https://news.ycombinator.com/rss', '技术社区最新讨论', 1, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('sspai', 'rss', '少数派', 'https://sspai.com/feed', '中文科技与效率内容', 1, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions(source_id, enabled, subscribed_at, updated_at)
SELECT id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM feed_sources
WHERE is_default = 1;

ALTER TABLE news ADD COLUMN source_id TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN origin TEXT NOT NULL DEFAULT 'agent';

CREATE INDEX IF NOT EXISTS idx_news_source_published
    ON news(source_id, published_at DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS recordings (
    record_id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    started_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL DEFAULT '',
    speaker_count INTEGER NOT NULL DEFAULT 0,
    audio_ref TEXT NOT NULL DEFAULT '',
    transcript TEXT NOT NULL DEFAULT '',
    transcript_truncated INTEGER NOT NULL DEFAULT 0 CHECK (transcript_truncated IN (0, 1)),
    segments_json TEXT NOT NULL DEFAULT '[]',
    segments_truncated INTEGER NOT NULL DEFAULT 0 CHECK (segments_truncated IN (0, 1)),
    speaker_names_json TEXT NOT NULL DEFAULT '{}',
    summary TEXT NOT NULL DEFAULT '',
    per_speaker_summary TEXT NOT NULL DEFAULT '',
    report TEXT NOT NULL DEFAULT '',
    action_ids_json TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS idx_recordings_updated
    ON recordings(updated_at DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS recording_preferences (
    recording_id TEXT PRIMARY KEY REFERENCES recordings(record_id) ON DELETE CASCADE,
    is_read INTEGER NOT NULL DEFAULT 0 CHECK (is_read IN (0, 1)),
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    playback_seconds REAL NOT NULL DEFAULT 0 CHECK (playback_seconds >= 0 AND playback_seconds <= 86400),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS recording_actions (
    recording_id TEXT NOT NULL REFERENCES recordings(record_id) ON DELETE CASCADE,
    action_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (recording_id, action_id)
);

CREATE TABLE IF NOT EXISTS sync_requests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    requested_at TEXT NOT NULL,
    completed_at TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sync_requests_requested
    ON sync_requests(requested_at DESC);

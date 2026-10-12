-- Device-local application defaults shared by the standalone UI and Agent
-- typed AppHost operations. There is one settings row per installed app;
-- owner/profile isolation is supplied by AppHost process identity.
CREATE TABLE IF NOT EXISTS app_settings (
    id TEXT PRIMARY KEY CHECK (id = 'default'),
    brief_enabled INTEGER NOT NULL DEFAULT 1 CHECK (brief_enabled IN (0, 1)),
    brief_hour INTEGER NOT NULL DEFAULT 8 CHECK (brief_hour BETWEEN 0 AND 23),
    brief_minute INTEGER NOT NULL DEFAULT 0 CHECK (brief_minute BETWEEN 0 AND 59),
    brief_weekdays TEXT NOT NULL DEFAULT '1,2,3,4,5',
    timezone TEXT NOT NULL DEFAULT 'device',
    auto_extract_recordings INTEGER NOT NULL DEFAULT 1 CHECK (auto_extract_recordings IN (0, 1)),
    notify_overdue INTEGER NOT NULL DEFAULT 1 CHECK (notify_overdue IN (0, 1)),
    notify_high_risk INTEGER NOT NULL DEFAULT 1 CHECK (notify_high_risk IN (0, 1)),
    notify_blockers INTEGER NOT NULL DEFAULT 1 CHECK (notify_blockers IN (0, 1)),
    notify_source_failures INTEGER NOT NULL DEFAULT 1 CHECK (notify_source_failures IN (0, 1)),
    notify_urgent INTEGER NOT NULL DEFAULT 1 CHECK (notify_urgent IN (0, 1)),
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO app_settings(id, updated_at) VALUES('default', CURRENT_TIMESTAMP);

-- Additive fields keep older snapshots readable while allowing idempotent
-- Agent writes, conflict review and recoverable soft deletion.
ALTER TABLE news ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1);
ALTER TABLE news ADD COLUMN confidence TEXT NOT NULL DEFAULT 'unverified';
ALTER TABLE news ADD COLUMN conflict_state TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN evidence_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE news ADD COLUMN deleted_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_news_active_source_published
    ON news(deleted_at, source_id, published_at DESC, updated_at DESC);

CREATE TABLE IF NOT EXISTS entry_conflicts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id TEXT NOT NULL,
    external_id TEXT NOT NULL,
    current_revision INTEGER NOT NULL,
    expected_revision INTEGER NOT NULL,
    current_json TEXT NOT NULL,
    incoming_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_entry_conflicts_open
    ON entry_conflicts(source_id, external_id, resolved_at, created_at DESC);

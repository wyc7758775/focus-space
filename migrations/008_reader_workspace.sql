ALTER TABLE evidence ADD COLUMN source_type TEXT NOT NULL DEFAULT '';

ALTER TABLE news ADD COLUMN view_type TEXT NOT NULL DEFAULT 'article';
ALTER TABLE news ADD COLUMN author TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN thumbnail_url TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN media_url TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN media_mime TEXT NOT NULL DEFAULT '';
ALTER TABLE news ADD COLUMN duration_seconds INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS item_preferences (
    item_key TEXT PRIMARY KEY,
    is_read INTEGER NOT NULL DEFAULT 0 CHECK (is_read IN (0, 1)),
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS reader_preferences (
    id TEXT PRIMARY KEY CHECK (id = 'default'),
    sidebar_width INTEGER NOT NULL CHECK (sidebar_width >= 220 AND sidebar_width <= 360),
    timeline_width INTEGER NOT NULL CHECK (timeline_width >= 360 AND timeline_width <= 640),
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO reader_preferences(id, sidebar_width, timeline_width, updated_at)
VALUES('default', 256, 450, CURRENT_TIMESTAMP);

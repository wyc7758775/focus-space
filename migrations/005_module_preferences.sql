CREATE TABLE IF NOT EXISTS module_preferences (
    module_id TEXT PRIMARY KEY,
    hidden INTEGER NOT NULL DEFAULT 0 CHECK (hidden IN (0, 1)),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS module_layouts (
    module_id TEXT PRIMARY KEY,
    height_px INTEGER NOT NULL CHECK (height_px >= 160 AND height_px <= 960),
    span INTEGER NOT NULL CHECK (span IN (1, 2)),
    updated_at TEXT NOT NULL
);

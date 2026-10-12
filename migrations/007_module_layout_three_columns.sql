CREATE TABLE module_layouts_next (
    module_id TEXT PRIMARY KEY,
    height_px INTEGER NOT NULL CHECK (height_px >= 160 AND height_px <= 960),
    span INTEGER NOT NULL CHECK (span IN (1, 2, 3)),
    updated_at TEXT NOT NULL
);

INSERT INTO module_layouts_next(module_id, height_px, span, updated_at)
SELECT module_id, height_px, span, updated_at
FROM module_layouts
WHERE span IN (1, 2, 3);

DROP TABLE module_layouts;
ALTER TABLE module_layouts_next RENAME TO module_layouts;

-- This device-local switch neither grants vault access nor widens the
-- connector scope selected outside the generated application.
ALTER TABLE app_settings ADD COLUMN obsidian_enabled INTEGER NOT NULL DEFAULT 1
    CHECK (obsidian_enabled IN (0, 1));

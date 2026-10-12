CREATE TABLE IF NOT EXISTS action_preferences (
  action_id TEXT PRIMARY KEY,
  completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0, 1)),
  deleted INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0, 1)),
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS signal_preferences (
  signal_id TEXT PRIMARY KEY,
  dismissed INTEGER NOT NULL DEFAULT 1 CHECK (dismissed IN (0, 1)),
  updated_at TEXT NOT NULL
);

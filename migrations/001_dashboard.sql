CREATE TABLE IF NOT EXISTS dashboard_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  generated_at TEXT NOT NULL,
  source_status TEXT NOT NULL,
  source_message TEXT NOT NULL DEFAULT '',
  records_seen INTEGER NOT NULL DEFAULT 0,
  records_used INTEGER NOT NULL DEFAULT 0,
  payload_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS evidence (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  locator TEXT NOT NULL DEFAULT '',
  observed_at TEXT NOT NULL DEFAULT '',
  quote TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS actions (
  id TEXT PRIMARY KEY,
  task TEXT NOT NULL,
  owner TEXT NOT NULL DEFAULT '负责人待确认',
  due_at TEXT NOT NULL DEFAULT '',
  source_ids TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'proposed',
  confirmation_status TEXT NOT NULL DEFAULT 'draft',
  priority TEXT NOT NULL DEFAULT 'medium'
);

CREATE TABLE IF NOT EXISTS metrics (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  period TEXT NOT NULL DEFAULT '',
  comparison TEXT NOT NULL DEFAULT '',
  source_ids TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'unverified'
);

CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'unverified',
  summary TEXT NOT NULL DEFAULT '',
  blockers TEXT NOT NULL DEFAULT '[]',
  source_ids TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS signals (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  source_ids TEXT NOT NULL DEFAULT '[]',
  confidence TEXT NOT NULL DEFAULT 'unverified',
  status TEXT NOT NULL DEFAULT 'unverified'
);

CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  question TEXT NOT NULL,
  options TEXT NOT NULL DEFAULT '[]',
  recommendation TEXT NOT NULL DEFAULT '',
  source_ids TEXT NOT NULL DEFAULT '[]',
  assumptions TEXT NOT NULL DEFAULT '[]',
  risks TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'draft'
);

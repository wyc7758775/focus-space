-- User-controlled Reader focus topics and bounded analysis accounting.
-- All data is device-local and additive; existing Reader preferences and
-- dashboard projections remain valid when these tables are empty.
CREATE TABLE IF NOT EXISTS reader_focus_topics (
    id TEXT PRIMARY KEY,
    label TEXT NOT NULL,
    terms_json TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    watch_since TEXT NOT NULL,
    bootstrap_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (bootstrap_status IN ('pending', 'skipped', 'active', 'done')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS reader_focus_policy (
    id TEXT PRIMARY KEY CHECK (id = 'default'),
    mode TEXT NOT NULL DEFAULT 'recommend_only'
        CHECK (mode IN ('recommend_only', 'auto_new', 'auto_bootstrap')),
    daily_article_limit INTEGER NOT NULL DEFAULT 10
        CHECK (daily_article_limit BETWEEN 1 AND 10),
    daily_token_budget INTEGER NOT NULL DEFAULT 8000
        CHECK (daily_token_budget BETWEEN 1000 AND 12000),
    per_run_limit INTEGER NOT NULL DEFAULT 5
        CHECK (per_run_limit BETWEEN 1 AND 5),
    bootstrap_limit INTEGER NOT NULL DEFAULT 5
        CHECK (bootstrap_limit IN (3, 5, 10)),
    full_text_mode TEXT NOT NULL DEFAULT 'summary'
        CHECK (full_text_mode IN ('summary', 'explicit_full_text')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO reader_focus_policy
    (id, mode, daily_article_limit, daily_token_budget, per_run_limit,
     bootstrap_limit, full_text_mode, revision, updated_at)
VALUES
    ('default', 'recommend_only', 10, 8000, 5, 5, 'summary', 1, CURRENT_TIMESTAMP);

CREATE TABLE IF NOT EXISTS reader_focus_runs (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('bootstrap', 'daily')),
    run_day TEXT NOT NULL,
    topic_ids_json TEXT NOT NULL,
    candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (candidate_count >= 0),
    processed_count INTEGER NOT NULL DEFAULT 0 CHECK (processed_count >= 0),
    estimated_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (estimated_input_tokens >= 0),
    estimated_output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (estimated_output_tokens >= 0),
    actual_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (actual_input_tokens >= 0),
    actual_output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (actual_output_tokens >= 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'completed', 'partial', 'failed')),
    idempotency_key TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reader_focus_topics_enabled
    ON reader_focus_topics(enabled, watch_since);
CREATE INDEX IF NOT EXISTS idx_reader_focus_runs_day
    ON reader_focus_runs(run_day, created_at DESC);

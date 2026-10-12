-- Focus Space is an additive, device-local evidence projection.  Existing
-- Reader, dashboard, and action tables remain valid when no focus exists.
CREATE TABLE IF NOT EXISTS focus_spaces (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    title TEXT NOT NULL,
    goal TEXT NOT NULL,
    decision_question TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'monitor'
        CHECK (kind IN ('monitor', 'decision')),
    horizon_mode TEXT NOT NULL DEFAULT 'open'
        CHECK (horizon_mode IN ('rolling', 'fixed', 'open')),
    horizon_start TEXT NOT NULL DEFAULT '',
    horizon_end TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'archived')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (id, owner_id, profile_id)
);

CREATE INDEX IF NOT EXISTS idx_focus_spaces_scope
    ON focus_spaces(owner_id, profile_id, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS focus_space_sources (
    focus_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    source_kind TEXT NOT NULL
        CHECK (source_kind IN ('meeting', 'rss', 'obsidian')),
    source_id TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (focus_id, source_kind, source_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_space_sources_scope
    ON focus_space_sources(owner_id, profile_id, focus_id, enabled);

CREATE TABLE IF NOT EXISTS focus_analysis_runs (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    focus_revision INTEGER NOT NULL CHECK (focus_revision >= 1),
    evidence_set_hash TEXT NOT NULL,
    analysis_profile TEXT NOT NULL DEFAULT 'evidence-v1',
    prompt_version TEXT NOT NULL DEFAULT 'focus-v1',
    scope TEXT NOT NULL CHECK (scope IN ('bootstrap', 'incremental', 'question')),
    status TEXT NOT NULL CHECK (status IN ('claimed', 'applied', 'partial', 'failed', 'superseded')),
    candidate_count INTEGER NOT NULL DEFAULT 0 CHECK (candidate_count >= 0),
    processed_count INTEGER NOT NULL DEFAULT 0 CHECK (processed_count >= 0),
    estimated_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (estimated_input_tokens >= 0),
    estimated_output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (estimated_output_tokens >= 0),
    actual_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (actual_input_tokens >= 0),
    actual_output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (actual_output_tokens >= 0),
    idempotency_key TEXT NOT NULL,
    selector_key TEXT NOT NULL DEFAULT '',
    expires_at TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (owner_id, profile_id, idempotency_key),
    UNIQUE (id, owner_id, profile_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_analysis_runs_focus
    ON focus_analysis_runs(owner_id, profile_id, focus_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_focus_analysis_runs_active_selector
    ON focus_analysis_runs(owner_id, profile_id, selector_key)
    WHERE selector_key <> '' AND status = 'claimed';

CREATE TABLE IF NOT EXISTS focus_analysis_evidence (
    run_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('meeting', 'rss', 'obsidian')),
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    locator TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    excerpt TEXT NOT NULL DEFAULT '',
    access_scope TEXT NOT NULL DEFAULT '',
    rank INTEGER NOT NULL DEFAULT 0 CHECK (rank >= 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY (run_id, evidence_id),
    FOREIGN KEY (run_id, owner_id, profile_id)
        REFERENCES focus_analysis_runs(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_analysis_evidence_source
    ON focus_analysis_evidence(source_kind, source_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS focus_result_feedback (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    result_kind TEXT NOT NULL CHECK (result_kind IN ('signal', 'action', 'decision')),
    result_id TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    status TEXT NOT NULL CHECK (status IN ('draft', 'accepted', 'rejected', 'snoozed', 'edited')),
    patch_json TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, focus_id, result_kind, result_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_result_feedback_focus
    ON focus_result_feedback(owner_id, profile_id, focus_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS focus_analysis_idempotency (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    response_json TEXT NOT NULL DEFAULT '',
    response_status INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_focus_analysis_idempotency_updated
    ON focus_analysis_idempotency(updated_at DESC);

-- Optional provenance columns on the existing projection tables.  They are
-- intentionally nullable-by-default so old dashboard payloads and old
-- clients continue to write exactly as before.
ALTER TABLE evidence ADD COLUMN focus_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence ADD COLUMN analysis_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE evidence ADD COLUMN evidence_version TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN focus_id TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN analysis_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN result_rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE actions ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN evidence_version TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN supporting_evidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE actions ADD COLUMN counterevidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE actions ADD COLUMN confidence TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN freshness TEXT NOT NULL DEFAULT '';
ALTER TABLE actions ADD COLUMN validation_items TEXT NOT NULL DEFAULT '[]';
ALTER TABLE signals ADD COLUMN focus_id TEXT NOT NULL DEFAULT '';
ALTER TABLE signals ADD COLUMN analysis_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE signals ADD COLUMN result_rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE signals ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
ALTER TABLE signals ADD COLUMN evidence_version TEXT NOT NULL DEFAULT '';
ALTER TABLE signals ADD COLUMN supporting_evidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE signals ADD COLUMN freshness TEXT NOT NULL DEFAULT '';
ALTER TABLE signals ADD COLUMN validation_items TEXT NOT NULL DEFAULT '[]';
ALTER TABLE decisions ADD COLUMN focus_id TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN analysis_run_id TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN result_rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE decisions ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN evidence_version TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN supporting_evidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE decisions ADD COLUMN counterevidence TEXT NOT NULL DEFAULT '[]';
ALTER TABLE decisions ADD COLUMN confidence TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN freshness TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN validation_items TEXT NOT NULL DEFAULT '[]';

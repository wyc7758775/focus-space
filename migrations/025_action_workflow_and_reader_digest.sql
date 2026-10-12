-- User-owned action review/scheduling state is kept beside the Agent
-- projection.  The projection may be refreshed, but this sidecar is never
-- replaced by a dashboard write.
CREATE TABLE IF NOT EXISTS action_workflows (
    action_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    agent_fingerprint TEXT NOT NULL DEFAULT '',
    user_edited INTEGER NOT NULL DEFAULT 0 CHECK (user_edited IN (0, 1)),
    has_task_override INTEGER NOT NULL DEFAULT 0 CHECK (has_task_override IN (0, 1)),
    task_override TEXT NOT NULL DEFAULT '',
    has_owner_override INTEGER NOT NULL DEFAULT 0 CHECK (has_owner_override IN (0, 1)),
    owner_override TEXT NOT NULL DEFAULT '',
    has_due_at_override INTEGER NOT NULL DEFAULT 0 CHECK (has_due_at_override IN (0, 1)),
    due_at_override TEXT NOT NULL DEFAULT '',
    has_priority_override INTEGER NOT NULL DEFAULT 0 CHECK (has_priority_override IN (0, 1)),
    priority_override TEXT NOT NULL DEFAULT '',
    confirmation_status TEXT NOT NULL DEFAULT 'draft'
        CHECK (confirmation_status IN ('draft', 'accepted', 'rejected')),
    schedule_state TEXT NOT NULL DEFAULT 'none'
        CHECK (schedule_state IN ('none', 'pending', 'scheduled', 'failed')),
    request_id TEXT NOT NULL DEFAULT '',
    candidate_id TEXT NOT NULL DEFAULT '',
    client_request_id TEXT NOT NULL DEFAULT '',
    job_id TEXT NOT NULL DEFAULT '',
    next_run_at TEXT NOT NULL DEFAULT '',
    replayed INTEGER NOT NULL DEFAULT 0 CHECK (replayed IN (0, 1)),
    error_code TEXT NOT NULL DEFAULT '',
    error_summary TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_action_workflows_updated
    ON action_workflows(updated_at DESC);

CREATE TABLE IF NOT EXISTS action_mutation_requests (
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    response_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_action_mutation_requests_created
    ON action_mutation_requests(created_at DESC);

-- Reader digest receipts are compact, owner-local deduplication evidence.
-- Keeping them in SQLite means a dashboard payload can contain only the
-- current bounded batch while subsequent runs still skip old content_hashes.
CREATE TABLE IF NOT EXISTS reader_digest_receipts (
    content_hash TEXT PRIMARY KEY,
    source_id TEXT NOT NULL DEFAULT '',
    entry_id TEXT NOT NULL DEFAULT '',
    processed_at TEXT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('drafted', 'no_candidate', 'skipped'))
);

CREATE INDEX IF NOT EXISTS idx_reader_digest_receipts_processed
    ON reader_digest_receipts(processed_at DESC);

-- Persist bounded Reader focus claims so concurrent maintenance reads cannot
-- hand the same article to more than one model invocation. Claims are local
-- sidecar state; the existing reader_digest operation contract is unchanged.
ALTER TABLE reader_focus_runs
    ADD COLUMN selector_key TEXT NOT NULL DEFAULT '';

ALTER TABLE reader_focus_runs
    ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';

ALTER TABLE reader_focus_runs
    ADD COLUMN failure_code TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS reader_focus_claim_items (
    run_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    topic_ids_json TEXT NOT NULL,
    estimated_input_tokens INTEGER NOT NULL DEFAULT 0
        CHECK (estimated_input_tokens >= 0),
    estimated_output_tokens INTEGER NOT NULL DEFAULT 0
        CHECK (estimated_output_tokens >= 0),
    state TEXT NOT NULL DEFAULT 'claimed'
        CHECK (state IN ('claimed', 'completed', 'failed', 'expired')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, entry_id, content_hash),
    FOREIGN KEY (run_id) REFERENCES reader_focus_runs(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reader_focus_claim_active_selector
    ON reader_focus_runs(selector_key)
    WHERE selector_key <> '' AND status = 'pending';

CREATE UNIQUE INDEX IF NOT EXISTS idx_reader_focus_claim_active_entry
    ON reader_focus_claim_items(entry_id, content_hash)
    WHERE state = 'claimed';

CREATE INDEX IF NOT EXISTS idx_reader_focus_claim_items_run
    ON reader_focus_claim_items(run_id, state, updated_at DESC);

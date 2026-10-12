-- Bounded article structure for paragraph-level citations.  All columns are
-- additive so pre-existing reader rows remain valid and can be refreshed on
-- demand without a backfill.
ALTER TABLE entry_full_texts ADD COLUMN blocks_json TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_full_texts ADD COLUMN blocks_version TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_full_texts ADD COLUMN blocks_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_full_texts ADD COLUMN blocks_truncated INTEGER NOT NULL DEFAULT 0
    CHECK (blocks_truncated IN (0, 1));

-- Structured locator data supplements the legacy URL/string locator.
ALTER TABLE focus_analysis_evidence ADD COLUMN source_locator_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE focus_analysis_evidence ADD COLUMN entry_id TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_analysis_evidence ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_analysis_evidence ADD COLUMN block_id TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_analysis_evidence ADD COLUMN quote TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_analysis_evidence ADD COLUMN text_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_analysis_evidence ADD COLUMN evidence_role TEXT NOT NULL DEFAULT 'support';

-- Result citations are kept separate from the bounded result JSON.  This lets
-- the reader resolve a single paragraph without copying article content into
-- a Focus snapshot, while owner/profile scope prevents cross-profile leaks.
CREATE TABLE IF NOT EXISTS focus_result_citations (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    result_kind TEXT NOT NULL CHECK (result_kind IN ('signal', 'action', 'decision')),
    result_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    evidence_id TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    entry_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    block_id TEXT NOT NULL DEFAULT '',
    quote TEXT NOT NULL DEFAULT '',
    text_hash TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'support'
        CHECK (role IN ('support', 'challenge', 'qualify', 'gap')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, result_kind, result_id, ordinal),
    FOREIGN KEY (run_id, owner_id, profile_id)
        REFERENCES focus_analysis_runs(id, owner_id, profile_id) ON DELETE CASCADE,
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_result_citations_entry
    ON focus_result_citations(owner_id, profile_id, entry_id, content_hash, block_id);

CREATE INDEX IF NOT EXISTS idx_focus_result_citations_evidence
    ON focus_result_citations(owner_id, profile_id, evidence_id, created_at DESC);

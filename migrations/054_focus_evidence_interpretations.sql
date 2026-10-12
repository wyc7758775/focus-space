-- Target-specific interpretation cache.  This is deliberately separate from
-- article_ai_overviews: the same evidence can mean something different for
-- two goals, revisions, prompts, or analysis profiles.
CREATE TABLE IF NOT EXISTS focus_evidence_interpretations (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    focus_revision INTEGER NOT NULL CHECK (focus_revision >= 1),
    evidence_id TEXT NOT NULL,
    evidence_revision TEXT NOT NULL,
    evidence_set_hash TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL,
    analysis_profile TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    interpretation_json TEXT NOT NULL DEFAULT '{}'
        CHECK (length(interpretation_json) <= 32768),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (
        owner_id, profile_id, focus_id, focus_revision, evidence_id,
        evidence_revision, prompt_version, analysis_profile
    ),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_evidence_interpretations_current
    ON focus_evidence_interpretations(
        owner_id, profile_id, focus_id, focus_revision,
        evidence_set_hash, updated_at DESC
    );

CREATE INDEX IF NOT EXISTS idx_focus_evidence_interpretations_retention
    ON focus_evidence_interpretations(owner_id, profile_id, updated_at DESC);

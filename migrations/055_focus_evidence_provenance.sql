-- Keep the existing evidence projection compatible while recording the
-- ordered run and prompt that produced a Focus result. This is additive and
-- intentionally separate from the already published Focus Space migration.
ALTER TABLE evidence ADD COLUMN rank INTEGER NOT NULL DEFAULT 0;
ALTER TABLE evidence ADD COLUMN prompt_version TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_evidence_focus_provenance
    ON evidence(focus_id, analysis_run_id, rank);

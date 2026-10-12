-- Bind a completed focus run to the complete validated apply payload.
-- Empty values are retained for rows written before this migration; runtime
-- treats those rows as untrusted and will not authorize a new replay against
-- them.
ALTER TABLE reader_focus_runs
    ADD COLUMN payload_fingerprint TEXT NOT NULL DEFAULT '';

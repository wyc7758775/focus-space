-- Focus Space V3 adds board/lifecycle metadata without changing the existing
-- Focus, analysis, or Reader contracts. Existing Focus rows remain in the
-- accumulating/slow-burn lane until real decision state moves them forward.
ALTER TABLE focus_spaces ADD COLUMN category TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_spaces ADD COLUMN decision_mode TEXT NOT NULL DEFAULT 'slow_burn'
    CHECK (decision_mode IN ('sprint', 'slow_burn'));
ALTER TABLE focus_spaces ADD COLUMN workflow_state TEXT NOT NULL DEFAULT 'accumulating'
    CHECK (workflow_state IN ('accumulating', 'deciding', 'executing', 'done', 'archived'));
ALTER TABLE focus_spaces ADD COLUMN review_at TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_spaces ADD COLUMN owner_hint TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_focus_spaces_v3_board
    ON focus_spaces(owner_id, profile_id, workflow_state, decision_mode, updated_at DESC);

-- Keep bootstrap decisions independent when more than one focus topic exists.
-- This migration is additive so databases that already applied 048 can upgrade
-- without changing existing Reader data.
ALTER TABLE reader_focus_topics
    ADD COLUMN bootstrap_mode TEXT NOT NULL DEFAULT 'none'
        CHECK (bootstrap_mode IN ('none', 'analyze', 'future_only'));

-- Recover an early 048 database that only had the aggregate policy mode.
UPDATE reader_focus_topics
SET bootstrap_mode = CASE
    WHEN bootstrap_status = 'active' AND (SELECT mode FROM reader_focus_policy WHERE id = 'default') = 'auto_bootstrap' THEN 'analyze'
    WHEN bootstrap_status IN ('active', 'done') AND (SELECT mode FROM reader_focus_policy WHERE id = 'default') = 'auto_new' THEN 'future_only'
    ELSE 'none'
END;

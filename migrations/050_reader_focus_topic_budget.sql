-- Persist bootstrap quota and consumption per topic. The aggregate policy
-- value remains as the compatibility default for old clients and new topics.
ALTER TABLE reader_focus_topics
    ADD COLUMN bootstrap_limit INTEGER NOT NULL DEFAULT 5
        CHECK (bootstrap_limit IN (3, 5, 10));

ALTER TABLE reader_focus_topics
    ADD COLUMN bootstrap_processed_count INTEGER NOT NULL DEFAULT 0
        CHECK (bootstrap_processed_count >= 0);

-- Existing 048/049 topics used the policy-level quota. Preserve that choice
-- when upgrading instead of silently resetting every topic to five articles.
UPDATE reader_focus_topics
SET bootstrap_limit = COALESCE(
    (SELECT bootstrap_limit FROM reader_focus_policy WHERE id = 'default'),
    5
);

-- Runs already written before this migration carry their topic IDs. Seed the
-- durable per-topic counter so run retention cannot reopen an authorized
-- bootstrap budget. Ignore malformed legacy JSON rather than failing startup.
UPDATE reader_focus_topics
SET bootstrap_processed_count = COALESCE((
    SELECT SUM(r.processed_count)
    FROM reader_focus_runs r
    JOIN json_each(CASE WHEN json_valid(r.topic_ids_json) THEN r.topic_ids_json ELSE '[]' END) ids
        ON ids.value = reader_focus_topics.id
    WHERE r.scope = 'bootstrap'
      AND r.status IN ('completed', 'partial')
), 0);

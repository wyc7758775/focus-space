-- Article overview generation is a durable background job. The public status
-- remains pending/ready/failed for backward compatibility; stage exposes the
-- recoverable state machine to newer clients.
ALTER TABLE article_ai_overviews
    ADD COLUMN stage TEXT NOT NULL DEFAULT 'queued'
    CHECK (stage IN ('queued', 'preparing_source', 'generating', 'validating', 'retry_wait', 'ready', 'failed'));

ALTER TABLE article_ai_overviews
    ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0);

ALTER TABLE article_ai_overviews
    ADD COLUMN next_retry_at TEXT NOT NULL DEFAULT '';

ALTER TABLE article_ai_overviews
    ADD COLUMN lease_until TEXT NOT NULL DEFAULT '';

ALTER TABLE article_ai_overviews
    ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';

UPDATE article_ai_overviews
SET stage = CASE status
    WHEN 'ready' THEN 'ready'
    WHEN 'failed' THEN 'failed'
    ELSE 'queued'
END,
updated_at = generated_at;

CREATE INDEX IF NOT EXISTS idx_article_ai_overviews_jobs
    ON article_ai_overviews(status, stage, next_retry_at, updated_at);

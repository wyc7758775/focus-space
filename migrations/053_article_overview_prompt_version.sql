-- Article overview cache entries are tied to the prompt contract as well as
-- the article revision. A prompt change must not silently reuse an old
-- conclusion for the same content hash.
ALTER TABLE article_ai_overviews
    ADD COLUMN prompt_version TEXT NOT NULL DEFAULT 'article-overview-v1';

CREATE INDEX IF NOT EXISTS idx_article_ai_overviews_prompt
    ON article_ai_overviews(entry_id, content_hash, prompt_version);

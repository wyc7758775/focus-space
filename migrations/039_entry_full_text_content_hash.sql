-- Bind the reader projection to the exact feed revision that produced it.
-- Legacy rows keep an empty hash and are treated as stale until refreshed.
ALTER TABLE entry_full_texts ADD COLUMN content_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_entry_full_text_revision
    ON entry_full_texts(entry_id, content_hash, status);

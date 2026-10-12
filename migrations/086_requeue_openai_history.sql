-- Existing installs may have already applied 077 while retaining validators
-- from the old 100-item window. Force one unconditional archive refresh so a
-- 304 response cannot permanently preserve the truncated local history.

UPDATE feed_sources
SET etag = '',
    last_modified = '',
    last_sync_at = '',
    next_sync_at = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'openai-news'
  AND collection_mode = 'app';

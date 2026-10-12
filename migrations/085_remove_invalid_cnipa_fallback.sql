-- Builds before the verified CNIPA collector guard stored statistics and
-- administrative reports as patent records. Hide only those legacy fallback
-- rows; user-retained items remain available through favorites or context.

UPDATE news
SET deleted_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE source_id = 'cnipa-patent-data'
  AND origin = 'app-fetch'
  AND source_level = 'official_fallback'
  AND deleted_at = ''
  AND NOT EXISTS (
      SELECT 1
      FROM item_preferences preferences
      WHERE preferences.item_key = 'news:' || news.id
        AND (preferences.starred = 1 OR preferences.read_later = 1)
  )
  AND NOT EXISTS (
      SELECT 1
      FROM stable_context_items context_item
      WHERE context_item.item_key = 'news:' || news.id
        AND context_item.state = 'active'
  );

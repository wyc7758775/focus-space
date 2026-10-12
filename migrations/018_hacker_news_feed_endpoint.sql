UPDATE feed_sources
SET url = 'https://hnrss.org/frontpage',
    status = 'unavailable',
    last_error = '',
    next_sync_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'hacker-news'
  AND url = 'https://news.ycombinator.com/rss'
  AND NOT EXISTS (
    SELECT 1 FROM feed_sources existing
    WHERE existing.url = 'https://hnrss.org/frontpage'
      AND existing.id <> 'hacker-news'
  );

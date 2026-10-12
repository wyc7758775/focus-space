-- Requeue the two exact BestBlogs-backed WeChat sources for the bounded,
-- metadata-only historical import introduced after migration 088. Existing
-- news, preferences, rich full text, overviews and digest state stay intact.
UPDATE feed_sources
SET backfill_cursor = '',
    next_sync_at = '',
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE (id = 'wechat-founder-park'
       AND url = 'https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml')
   OR (id = 'wechat-liuxiaopai'
       AND url = 'https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml');

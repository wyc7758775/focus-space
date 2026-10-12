-- Requeue every exact, identity-pinned WeChat feed for the metadata-only
-- 1000-entry history window. Existing rows, full text and user state remain
-- untouched; the runtime inserts only missing stable article identities.
UPDATE feed_sources
SET backfill_days = 3650,
    retention_days = 3650,
    retention_limit = 1000,
    backfill_cursor = '',
    next_sync_at = '',
    updated_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
WHERE (id = 'wechat-aiera' AND url = 'source-gateway://wechat-aiera')
   OR (id = 'wechat-ali-tech' AND url = 'https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml')
   OR (id = 'wechat-chaping' AND url = 'https://wechat2rss.xlab.app/feed/8d839de8dd3290a1f1be7a94423cccb30c1b087d.xml')
   OR (id = 'wechat-founder-park' AND url = 'https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml')
   OR (id = 'wechat-geekpark' AND url = 'source-gateway://wechat-geekpark')
   OR (id = 'wechat-jiqizhixin' AND url = 'source-gateway://wechat-jiqizhixin')
   OR (id = 'wechat-liuxiaopai' AND url = 'https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml')
   OR (id = 'wechat-paperweekly' AND url = 'https://wechat2rss.xlab.app/feed/3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml')
   OR (id = 'wechat-qbitai' AND url = 'source-gateway://wechat-qbitai');

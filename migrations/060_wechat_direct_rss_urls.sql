-- WeChat public-account feeds should be read from their pinned public RSS
-- routes directly. The old rows wrapped those same routes in Folo, which
-- truncated the visible result set and made the dashboard depend on an
-- unrelated proxy. Keep the source IDs and all user state unchanged.
CREATE TEMP TABLE _wechat_direct_rss_urls (
    id TEXT PRIMARY KEY,
    legacy_url TEXT NOT NULL,
    direct_url TEXT NOT NULL
);

INSERT INTO _wechat_direct_rss_urls (id, legacy_url, direct_url)
VALUES
    ('wechat-founder-park', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FFounder%2520Park', 'https://rsshub.rssforever.com/wechat/sogou/Founder%20Park'),
    ('wechat-fnos', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E9%A3%9E%E7%89%9BFnOS', 'https://rsshub.rssforever.com/wechat/sogou/%E9%A3%9E%E7%89%9BFnOS'),
    ('wechat-liuxiaopai', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E5%88%98%E5%B0%8F%E6%8E%92', 'https://rsshub.rssforever.com/wechat/sogou/%E5%88%98%E5%B0%8F%E6%8E%92'),
    ('wechat-ai-paper', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FAI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper', 'https://rsshub.rssforever.com/wechat/sogou/AI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper'),
    ('wechat-ali-tech', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2F6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml', 'https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml'),
    ('wechat-paperweekly', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2F3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml', 'https://wechat2rss.xlab.app/feed/3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml'),
    ('wechat-huxiu', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E7%A1%AC%E6%B0%AA', 'https://rsshub.rssforever.com/wechat/sogou/%E7%A1%AC%E6%B0%AA');

-- Only rewrite the exact URLs shipped by migrations 046/047. If a user has
-- already replaced a source with a custom URL, leave that choice untouched.
UPDATE feed_sources
SET url=(SELECT direct_url FROM _wechat_direct_rss_urls d WHERE d.id=feed_sources.id),
    kind='rss',
    updated_at=CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM _wechat_direct_rss_urls)
  AND url IN (SELECT legacy_url FROM _wechat_direct_rss_urls);

DROP TABLE _wechat_direct_rss_urls;

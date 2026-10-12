-- Additional Chinese technology publications verified against public RSS.
-- Keep this migration additive and preference-preserving: replay must not
-- re-enable an explicit unsubscribe or overwrite user folder/sort state.
CREATE TEMP TABLE _default_wechat_research_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    icon_url TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    tags_json TEXT NOT NULL,
    source_sort INTEGER NOT NULL,
    popularity INTEGER NOT NULL
);

INSERT INTO _default_wechat_research_sources
    (id, name, url, description, icon_url, icon_text, icon_color,
     tags_json, source_sort, popularity)
VALUES
    ('wechat-founder-park', 'Founder Park', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FFounder%2520Park', '创业者、AI 创新与全球技术创业动态', '', 'F', '#2563eb', '["科技公众号","创业","AI"]', 60, 92),
    ('wechat-fnos', '飞牛FnOS', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E9%A3%9E%E7%89%9BFnOS', 'NAS、家庭云和本地 AI 的产品与实践', '', '飞', '#16a34a', '["科技公众号","NAS","本地 AI"]', 70, 86),
    ('wechat-liuxiaopai', '刘小排', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E5%88%98%E5%B0%8F%E6%8E%92', 'AI 创业、产品实践与个人效率', '', '刘', '#ea580c', '["科技公众号","创业","效率"]', 80, 84),
    ('wechat-ai-paper', 'AI算法科研paper', 'https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FAI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper', '人工智能算法、论文和科研工具', '', '论', '#7c3aed', '["科技公众号","AI","论文"]', 90, 90),
    ('wechat-ali-tech', '阿里技术', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2F6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml', '阿里巴巴技术团队的工程实践、智能体与研发方法', 'https://wx.qlogo.cn/mmhead/1CHHx9Yq4nEbuS4GZwcj0kG9LoYStAh1Tcsxj0F5OpI5C2lxDaZ8ogSmdMTemVpot79NyyVNMqc/0', '阿', '#ff6a00', '["科技公众号","工程","AI"]', 100, 90),
    ('wechat-paperweekly', 'PaperWeekly', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2F3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml', '机器学习、人工智能论文与研究进展', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM4XfZEiaIxTZFPcSMe0laHNlkWJfvNVMcFY7PrIPmKrQjg/0', 'P', '#475569', '["科技公众号","AI","论文"]', 110, 88);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'json', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 21600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _default_wechat_research_sources;

-- A user may have added the same public feed before this curated entry was
-- shipped. Reuse that row without changing health, scheduling, or preference
-- state, while making the curated metadata visible.
UPDATE feed_sources
SET kind='json',
    name=(SELECT name FROM _default_wechat_research_sources d WHERE d.url=feed_sources.url),
    description=(SELECT description FROM _default_wechat_research_sources d WHERE d.url=feed_sources.url),
    is_default=1,
    collection_mode='app',
    sync_interval_seconds=21600,
    backfill_days=30,
    retention_days=90,
    retention_limit=1000,
    auto_extract=1,
    updated_at=CURRENT_TIMESTAMP
WHERE url IN (SELECT url FROM _default_wechat_research_sources);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT f.id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
       'folder-wechat-tech', d.source_sort
FROM _default_wechat_research_sources d
JOIN feed_sources f ON f.url=d.url;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, icon_url,
     tags_json, feed_format, recommended, popularity, validation_status,
     validated_at, catalog_origin)
SELECT f.id, 'https://mp.weixin.qq.com/', 'new-media', 'zh-CN', d.icon_text,
       d.icon_color, d.icon_url, d.tags_json, 'rss', 1, d.popularity, 'verified',
       '2026-08-14T00:00:00Z', 'zettlab-maintained-public-wechat-rss-v1'
FROM _default_wechat_research_sources d
JOIN feed_sources f ON f.url=d.url
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url,
    category=excluded.category,
    language=excluded.language,
    icon_text=excluded.icon_text,
    icon_color=excluded.icon_color,
    icon_url=excluded.icon_url,
    tags_json=excluded.tags_json,
    feed_format=excluded.feed_format,
    recommended=excluded.recommended,
    popularity=excluded.popularity,
    validation_status=excluded.validation_status,
    validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'wechat-tech', f.id, d.source_sort
FROM _default_wechat_research_sources d
JOIN feed_sources f ON f.url=d.url
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _default_wechat_research_sources;

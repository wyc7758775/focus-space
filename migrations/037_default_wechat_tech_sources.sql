-- Default Chinese technology publications exposed through bounded public RSS.
-- Stable source IDs keep collected entries and user preferences intact. The
-- subscription insert is intentionally ignore-only so an explicit unsubscribe
-- or folder/sort change is never reset by a migration replay.
CREATE TEMP TABLE _default_wechat_tech_sources (
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

INSERT INTO _default_wechat_tech_sources
    (id, name, url, description, icon_url, icon_text, icon_color,
     tags_json, source_sort, popularity)
VALUES
    ('wechat-qbitai', '量子位', 'https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml', '人工智能、模型、机器人与科技产业前沿', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM6PqxX9Yx8MibJ3nial8ialvVrRs3PxhCJm7eWgR7vFrowLA/0', '量', '#00997f', '["科技公众号","AI","前沿科技"]', 10, 100),
    ('wechat-jiqizhixin', '机器之心', 'https://wechat2rss.xlab.app/feed/51e92aad2728acdd1fda7314be32b16639353001.xml', '人工智能研究、模型进展与产业动态', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM75UiawQgcdqOcmtYS7Jibug9J7dskxkNicGiadtdKl7mLyiaw/0', '机', '#2563eb', '["科技公众号","AI","研究"]', 20, 98),
    ('wechat-geekpark', '极客公园', 'https://wechat2rss.xlab.app/feed/1a5aec98e71c707c8ca092bc2c255b9d4bac477d.xml', '科技产品、创业公司与商业趋势', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM6dnlEsZXOPTtkZpEYHJ3nvUviaGTbLGUj4VRaq3vra2icQ/0', '极', '#111827', '["科技公众号","产品","商业"]', 30, 96),
    ('wechat-aiera', '新智元', 'https://wechat2rss.xlab.app/feed/ede30346413ea70dbef5d485ea5cbb95cca446e7.xml', '人工智能研究、模型公司与行业观察', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM5Ge0ZibsJqTzd6HdTSHcydlic4TnsmpJicUrIlicD1L9ficFw/0', '新', '#7c3aed', '["科技公众号","AI","产业"]', 40, 94),
    ('wechat-chaping', '差评', 'https://wechat2rss.xlab.app/feed/8d839de8dd3290a1f1be7a94423cccb30c1b087d.xml', '消费科技、互联网产品与数字生活观察', 'https://wx.qlogo.cn/mmhead/Q3auHgzwzM6xLSHsmQn0ic8G4pgHC54jfUw9WiajH8CctzZibLzx4MMBg/0', '差', '#f97316', '["科技公众号","消费科技","互联网"]', 50, 92);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'rss', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 21600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _default_wechat_tech_sources;

-- Reuse a source that the user may already have discovered under another ID.
-- Do not touch health, counters, scheduling state, visibility or preferences.
UPDATE feed_sources
SET kind='rss',
    name=(SELECT name FROM _default_wechat_tech_sources d WHERE d.url=feed_sources.url),
    description=(SELECT description FROM _default_wechat_tech_sources d WHERE d.url=feed_sources.url),
    is_default=1,
    collection_mode='app',
    sync_interval_seconds=21600,
    backfill_days=30,
    retention_days=90,
    retention_limit=1000,
    auto_extract=1,
    updated_at=CURRENT_TIMESTAMP
WHERE url IN (SELECT url FROM _default_wechat_tech_sources);

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-wechat-tech', '科技公众号', 60, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT f.id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
       'folder-wechat-tech', d.source_sort
FROM _default_wechat_tech_sources d
JOIN feed_sources f ON f.url=d.url;

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, icon_url,
     tags_json, feed_format, recommended, popularity, validation_status,
     validated_at, catalog_origin)
SELECT f.id, 'https://mp.weixin.qq.com/', 'new-media', 'zh-CN', d.icon_text,
       d.icon_color, d.icon_url, d.tags_json, 'rss', 1, d.popularity, 'verified',
       '2026-08-12T00:00:00Z', 'zettlab-maintained-public-wechat-rss-v1'
FROM _default_wechat_tech_sources d
JOIN feed_sources f ON f.url=d.url
WHERE 1
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

INSERT INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('wechat-tech', '中文科技公众号', 'AI、科技产品与互联网产业的中文深度内容', 'new-media', '微', 90)
ON CONFLICT(id) DO UPDATE SET
    name=excluded.name,
    description=excluded.description,
    category=excluded.category,
    icon_text=excluded.icon_text,
    sort_order=excluded.sort_order;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'wechat-tech', f.id, d.source_sort
FROM _default_wechat_tech_sources d
JOIN feed_sources f ON f.url=d.url
WHERE 1
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _default_wechat_tech_sources;

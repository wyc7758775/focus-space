-- Direct Bilibili public-search sources. These are not RSSHub/Folo feeds:
-- the dashboard calls Bilibili's official anonymous search endpoint, pins the
-- returned uploader MID, and keeps only playable BV video entries.
CREATE TEMP TABLE _bilibili_ai_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    keyword TEXT NOT NULL,
    mid INTEGER NOT NULL,
    url TEXT NOT NULL,
    description TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    source_sort INTEGER NOT NULL
);

INSERT INTO _bilibili_ai_sources
    (id, name, keyword, mid, url, description, icon_text, icon_color, source_sort)
VALUES
    ('bilibili-geekerwan', '极客湾Geekerwan', '极客湾', 25876945, 'https://space.bilibili.com/25876945/video', '科技数码、手机、芯片与 AI 硬件实测', '极', '#2563eb', 10),
    ('bilibili-tech-aesthetics', '科技美学', '科技美学', 3766866, 'https://space.bilibili.com/3766866/video', '数码产品、系统与 AI 体验', '科', '#7c3aed', 20),
    ('bilibili-yingshijufeng', '影视飓风', '影视飓风', 946974, 'https://space.bilibili.com/946974/video', '影像技术、创作工具与 AI 视频实践', '影', '#ef4444', 30),
    ('bilibili-machine-heart', '机器之心官方', '机器之心', 73414544, 'https://space.bilibili.com/73414544/video', '人工智能研究、模型与产业动态', '机', '#0f766e', 40),
    ('bilibili-qbit', '量子位', '量子位', 673779175, 'https://space.bilibili.com/673779175/video', 'AI、机器人、芯片与前沿科技报道', '量', '#0891b2', 50),
    ('bilibili-geekpark', '极客公园', '极客公园', 13260662, 'https://space.bilibili.com/13260662/video', '科技产品、创新公司与 AI 趋势', '园', '#1d4ed8', 60),
    ('bilibili-hardware-tea', '硬件茶谈', '硬件茶谈', 14871346, 'https://space.bilibili.com/14871346/video', '硬件评测、装机与计算平台科普', '硬', '#b45309', 70),
    ('bilibili-he-tongxue', '老师好我叫何同学', '老师好我叫何同学', 163637592, 'https://space.bilibili.com/163637592/video', '科技产品、创意硬件与技术实验', '何', '#16a34a', 80),
    ('bilibili-li-mu-ai', '跟李沐学AI', '跟李沐学AI', 1567748478, 'https://space.bilibili.com/1567748478/video', '开源项目、机器学习与 AI 实践', '沐', '#dc2626', 90),
    ('bilibili-ai-tech-review', 'AI科技评论', 'AI科技评论', 216720985, 'https://space.bilibili.com/216720985/video', '人工智能研究、产品与产业评论', '评', '#9333ea', 100);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'api', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 3600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _bilibili_ai_sources;

UPDATE feed_sources
SET kind='api', name=(SELECT name FROM _bilibili_ai_sources WHERE id=feed_sources.id),
    url=(SELECT url FROM _bilibili_ai_sources WHERE id=feed_sources.id),
    description=(SELECT description FROM _bilibili_ai_sources WHERE id=feed_sources.id),
    is_default=1, collection_mode='app', sync_interval_seconds=3600,
    backfill_days=30, retention_days=90, retention_limit=1000,
    auto_extract=1, hidden=0, next_sync_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
WHERE id IN (SELECT id FROM _bilibili_ai_sources);

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-bilibili-ai', 'B 站科技 AI', 47, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'folder-bilibili-ai', source_sort
FROM _bilibili_ai_sources;

UPDATE feed_subscriptions
SET enabled=1, folder_id='folder-bilibili-ai',
    sort_order=(SELECT source_sort FROM _bilibili_ai_sources WHERE id=feed_subscriptions.source_id),
    updated_at=CURRENT_TIMESTAMP
WHERE source_id IN (SELECT id FROM _bilibili_ai_sources);

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at,
     catalog_origin)
SELECT id, url, 'ai', 'zh-CN', icon_text, icon_color,
       '["B站","AI","科技","视频"]', 'bilibili', 1, 90, 'verified',
       '2026-08-16T00:00:00Z', 'zettlab-direct-bilibili-search-v1'
FROM _bilibili_ai_sources
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url, category=excluded.category,
    language=excluded.language, icon_text=excluded.icon_text,
    icon_color=excluded.icon_color, tags_json=excluded.tags_json,
    feed_format=excluded.feed_format, recommended=excluded.recommended,
    validation_status=excluded.validation_status, validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin;

INSERT OR IGNORE INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('ai-video', 'AI 视频', '一键关注 AI 研究、产品与科技访谈视频', 'ai', '▶', 15);

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'ai-video', id, source_sort + 100
FROM _bilibili_ai_sources
WHERE 1
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _bilibili_ai_sources;

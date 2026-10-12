-- Agent-owned AI video sources. The feed URL is a pinned YouTube channel
-- Feed wrapped by the bounded Folo reader endpoint; the Go collector verifies
-- the channel ID and title before persisting any entry.
CREATE TEMP TABLE _youtube_ai_sources (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    channel_id TEXT NOT NULL,
    url TEXT NOT NULL,
    site_url TEXT NOT NULL,
    description TEXT NOT NULL,
    icon_text TEXT NOT NULL,
    icon_color TEXT NOT NULL,
    source_sort INTEGER NOT NULL
);

INSERT INTO _youtube_ai_sources
    (id, name, channel_id, url, site_url, description, icon_text, icon_color, source_sort)
VALUES
    ('youtube-google-deepmind', 'Google DeepMind', 'UCP7jMXSY2xbc3KCAE0MHQ-A', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCP7jMXSY2xbc3KCAE0MHQ-A', 'https://www.youtube.com/channel/UCP7jMXSY2xbc3KCAE0MHQ-A', 'Google DeepMind 的研究、模型与机器人视频', 'D', '#4285f4', 10),
    ('youtube-anthropic', 'Anthropic', 'UCrDwWp7EBBv4NwvScIpBDOA', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCrDwWp7EBBv4NwvScIpBDOA', 'https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA', 'Anthropic 的 AI 研究、安全与产品视频', 'A', '#d97757', 20),
    ('youtube-nvidia', 'NVIDIA', 'UCHuiy8bXnmK5nisYHUd1J5g', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCHuiy8bXnmK5nisYHUd1J5g', 'https://www.youtube.com/channel/UCHuiy8bXnmK5nisYHUd1J5g', 'NVIDIA 的 AI、算力与开发者视频', 'N', '#76b900', 30),
    ('youtube-two-minute-papers', 'Two Minute Papers', 'UCbfYPyITQ-7l4upoX8nvctg', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCbfYPyITQ-7l4upoX8nvctg', 'https://www.youtube.com/channel/UCbfYPyITQ-7l4upoX8nvctg', '用短视频解释最新 AI 论文与研究成果', '2', '#7c3aed', 40),
    ('youtube-ai-explained', 'AI Explained', 'UCNJ1Ymd5yFuUPtn21xtRbbw', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCNJ1Ymd5yFuUPtn21xtRbbw', 'https://www.youtube.com/channel/UCNJ1Ymd5yFuUPtn21xtRbbw', 'AI 模型、趋势与产业影响解读', 'E', '#0f766e', 50),
    ('youtube-computerphile', 'Computerphile', 'UC9-y-6csu5WGm29I7JiwpnA', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUC9-y-6csu5WGm29I7JiwpnA', 'https://www.youtube.com/channel/UC9-y-6csu5WGm29I7JiwpnA', '计算机科学、算法与 AI 基础知识', 'C', '#2563eb', 60),
    ('youtube-fireship', 'Fireship', 'UCsBjURrPoezykLs9EqgamOA', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCsBjURrPoezykLs9EqgamOA', 'https://www.youtube.com/channel/UCsBjURrPoezykLs9EqgamOA', '开发者工具、AI 与科技趋势速览', 'F', '#f97316', 70),
    ('youtube-lex-fridman', 'Lex Fridman Podcast', 'UCSHZKyawb77ixDdsGog4iWA', 'https://api.folo.is/feeds?url=https%3A%2F%2Fwww.youtube.com%2Ffeeds%2Fvideos.xml%3Fchannel_id%3DUCSHZKyawb77ixDdsGog4iWA', 'https://www.youtube.com/channel/UCSHZKyawb77ixDdsGog4iWA', 'AI、科学与技术领袖访谈', 'L', '#111827', 80);

INSERT INTO feed_sources
    (id, kind, name, url, description, is_default, status, records_seen,
     records_used, last_sync_at, last_error, created_at, updated_at,
     collection_mode, sync_interval_seconds, next_sync_at, backfill_days,
     retention_days, retention_limit, auto_extract, hidden, etag, last_modified)
SELECT id, 'youtube', name, url, description, 1, 'unavailable', 0, 0, '',
       '等待首次同步', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'app', 3600,
       CURRENT_TIMESTAMP, 30, 90, 1000, 1, 0, '', ''
FROM _youtube_ai_sources
WHERE 1
ON CONFLICT(id) DO UPDATE SET
    kind=excluded.kind, name=excluded.name, url=excluded.url,
    description=excluded.description, is_default=1,
    collection_mode='app', sync_interval_seconds=3600,
    backfill_days=30, retention_days=90, retention_limit=1000,
    auto_extract=1, updated_at=CURRENT_TIMESTAMP;

INSERT OR IGNORE INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES ('folder-ai-video', 'AI 视频', 45, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_subscriptions
    (source_id, enabled, subscribed_at, updated_at, folder_id, sort_order)
SELECT id, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'folder-ai-video', source_sort
FROM _youtube_ai_sources;

UPDATE feed_subscriptions
SET folder_id='folder-ai-video', sort_order=(SELECT source_sort FROM _youtube_ai_sources WHERE id=feed_subscriptions.source_id)
WHERE source_id IN (SELECT id FROM _youtube_ai_sources);

INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at,
     catalog_origin, icon_url)
SELECT id, site_url, 'ai', 'en', icon_text, icon_color,
       '["AI","科技","视频"]', 'youtube', 1, 90, 'verified',
       '2026-08-12T00:00:00Z', 'zettlab-agent-owned-youtube-v1',
       'https://icons.folo.is/youtube.com'
FROM _youtube_ai_sources
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET
    site_url=excluded.site_url, category=excluded.category,
    language=excluded.language, icon_text=excluded.icon_text,
    icon_color=excluded.icon_color, tags_json=excluded.tags_json,
    feed_format=excluded.feed_format, recommended=excluded.recommended,
    validation_status=excluded.validation_status, validated_at=excluded.validated_at,
    catalog_origin=excluded.catalog_origin,
    icon_url=CASE WHEN feed_catalog_entries.icon_url='' THEN excluded.icon_url ELSE feed_catalog_entries.icon_url END;

INSERT INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES ('ai-video', 'AI 视频', '一键关注 AI 研究、产品与科技访谈视频', 'ai', '▶', 15)
ON CONFLICT(id) DO UPDATE SET
    name=excluded.name, description=excluded.description,
    category=excluded.category, icon_text=excluded.icon_text,
    sort_order=excluded.sort_order;

INSERT INTO source_pack_members(pack_id, source_id, sort_order)
SELECT 'ai-video', id, source_sort FROM _youtube_ai_sources
WHERE 1
ON CONFLICT(pack_id, source_id) DO UPDATE SET sort_order=excluded.sort_order;

DROP TABLE _youtube_ai_sources;

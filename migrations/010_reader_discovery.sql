CREATE TABLE IF NOT EXISTS feed_catalog_entries (
    source_id TEXT PRIMARY KEY REFERENCES feed_sources(id),
    site_url TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'other',
    language TEXT NOT NULL DEFAULT 'und',
    icon_text TEXT NOT NULL DEFAULT '',
    icon_color TEXT NOT NULL DEFAULT '#64748b',
    tags_json TEXT NOT NULL DEFAULT '[]',
    feed_format TEXT NOT NULL DEFAULT 'rss',
    recommended INTEGER NOT NULL DEFAULT 0 CHECK (recommended IN (0, 1)),
    popularity INTEGER NOT NULL DEFAULT 0 CHECK (popularity >= 0),
    validation_status TEXT NOT NULL DEFAULT 'unverified',
    validated_at TEXT NOT NULL DEFAULT '',
    catalog_origin TEXT NOT NULL DEFAULT 'zettlab-maintained-public-rss-v1'
);

CREATE TABLE IF NOT EXISTS subscription_folders (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

ALTER TABLE feed_subscriptions ADD COLUMN folder_id TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_subscriptions ADD COLUMN title_override TEXT NOT NULL DEFAULT '';
ALTER TABLE feed_subscriptions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0 CHECK (pinned IN (0, 1));
ALTER TABLE feed_subscriptions ADD COLUMN muted INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0, 1));
ALTER TABLE feed_subscriptions ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS subscription_tags (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    color TEXT NOT NULL DEFAULT '#64748b',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feed_subscription_tags (
    source_id TEXT NOT NULL REFERENCES feed_sources(id),
    tag_id TEXT NOT NULL REFERENCES subscription_tags(id),
    created_at TEXT NOT NULL,
    PRIMARY KEY (source_id, tag_id)
);

CREATE TABLE IF NOT EXISTS source_packs (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT 'other',
    icon_text TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS source_pack_members (
    pack_id TEXT NOT NULL REFERENCES source_packs(id),
    source_id TEXT NOT NULL REFERENCES feed_sources(id),
    sort_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (pack_id, source_id)
);

CREATE TABLE IF NOT EXISTS feed_discovery_candidates (
    id TEXT PRIMARY KEY,
    input_url TEXT NOT NULL,
    normalized_url TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    site_url TEXT NOT NULL DEFAULT '',
    icon_url TEXT NOT NULL DEFAULT '',
    feed_format TEXT NOT NULL DEFAULT 'rss',
    status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    preview_json TEXT NOT NULL DEFAULT '[]',
    discovered_at TEXT NOT NULL,
    confirmed_at TEXT NOT NULL DEFAULT ''
);

ALTER TABLE item_preferences ADD COLUMN read_later INTEGER NOT NULL DEFAULT 0 CHECK (read_later IN (0, 1));
ALTER TABLE item_preferences ADD COLUMN reading_progress REAL NOT NULL DEFAULT 0 CHECK (reading_progress >= 0 AND reading_progress <= 1);
ALTER TABLE reader_preferences ADD COLUMN view_mode TEXT NOT NULL DEFAULT 'comfortable';
ALTER TABLE sync_requests ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0);
ALTER TABLE sync_requests ADD COLUMN next_retry_at TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_feed_sources_url ON feed_sources(url);
CREATE INDEX IF NOT EXISTS idx_feed_catalog_discover ON feed_catalog_entries(category, recommended DESC, popularity DESC);
CREATE INDEX IF NOT EXISTS idx_feed_subscriptions_order ON feed_subscriptions(pinned DESC, sort_order ASC, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_feed_discovery_input ON feed_discovery_candidates(input_url, discovered_at DESC);

INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, created_at, updated_at)
VALUES
    ('github-blog', 'rss', 'GitHub Blog', 'https://github.blog/feed/', '开发者平台、工程实践与开源动态', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('cloudflare-blog', 'rss', 'Cloudflare Blog', 'https://blog.cloudflare.com/rss/', '互联网基础设施、安全与性能', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('mozilla-hacks', 'rss', 'Mozilla Hacks', 'https://hacks.mozilla.org/feed/', '开放 Web 与浏览器工程', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('the-verge', 'rss', 'The Verge', 'https://www.theverge.com/rss/index.xml', '消费科技与数字文化', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('bbc-world', 'rss', 'BBC World', 'https://feeds.bbci.co.uk/news/world/rss.xml', '国际新闻', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('npr-technology', 'rss', 'NPR Technology', 'https://feeds.npr.org/1019/rss.xml', '科技与社会', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('nature', 'rss', 'Nature', 'https://www.nature.com/nature.rss', '自然科学研究进展', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
    ('mit-technology-review', 'rss', 'MIT Technology Review', 'https://www.technologyreview.com/feed/', '新兴技术与产业影响', 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);

INSERT OR IGNORE INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json, feed_format, recommended, popularity, validation_status, validated_at)
VALUES
    ('openai-news', 'https://openai.com/news/', 'ai', 'en', 'O', '#111827', '["AI","研究","产品"]', 'rss', 1, 100, 'verified', '2026-08-07T08:15:00Z'),
    ('hacker-news', 'https://news.ycombinator.com/', 'technology', 'en', 'Y', '#f97316', '["技术","创业","社区"]', 'rss', 1, 98, 'verified', '2026-08-07T08:15:00Z'),
    ('sspai', 'https://sspai.com/', 'productivity', 'zh-CN', '少', '#dc2626', '["效率","数字生活","中文"]', 'rss', 1, 96, 'verified', '2026-08-07T08:15:00Z'),
    ('github-blog', 'https://github.blog/', 'technology', 'en', 'G', '#24292f', '["开发","开源","工程"]', 'rss', 1, 92, 'verified', '2026-08-07T08:15:00Z'),
    ('cloudflare-blog', 'https://blog.cloudflare.com/', 'technology', 'en', 'C', '#f59e0b', '["基础设施","安全","网络"]', 'rss', 1, 88, 'verified', '2026-08-07T08:15:00Z'),
    ('mozilla-hacks', 'https://hacks.mozilla.org/', 'technology', 'en', 'M', '#7c3aed', '["Web","浏览器","开放标准"]', 'rss', 0, 80, 'verified', '2026-08-07T08:15:00Z'),
    ('the-verge', 'https://www.theverge.com/', 'technology', 'en', 'V', '#db2777', '["消费科技","文化","产品"]', 'rss', 0, 84, 'verified', '2026-08-07T08:15:00Z'),
    ('bbc-world', 'https://www.bbc.com/news/world', 'world', 'en', 'B', '#b91c1c', '["国际","新闻"]', 'rss', 1, 86, 'verified', '2026-08-07T08:15:00Z'),
    ('npr-technology', 'https://www.npr.org/sections/technology/', 'technology', 'en', 'N', '#2563eb', '["科技","社会"]', 'rss', 0, 76, 'verified', '2026-08-07T08:15:00Z'),
    ('nature', 'https://www.nature.com/nature/', 'science', 'en', 'N', '#0f766e', '["科学","研究"]', 'rss', 1, 90, 'verified', '2026-08-07T08:15:00Z'),
    ('mit-technology-review', 'https://www.technologyreview.com/', 'ai', 'en', 'T', '#0891b2', '["AI","产业","研究"]', 'rss', 1, 91, 'verified', '2026-08-07T08:15:00Z');

INSERT OR IGNORE INTO source_packs(id, name, description, category, icon_text, sort_order)
VALUES
    ('ai-engineering', 'AI 与工程', 'AI 研究、开发者社区与基础设施的精选来源', 'ai', 'AI', 10),
    ('science-world', '科学与世界', '科学研究、国际新闻与科技社会观察', 'science', '世', 20),
    ('chinese-productivity', '中文效率', '适合知识工作者的中文效率内容', 'productivity', '效', 30);

INSERT OR IGNORE INTO source_pack_members(pack_id, source_id, sort_order)
VALUES
    ('ai-engineering', 'openai-news', 10),
    ('ai-engineering', 'hacker-news', 20),
    ('ai-engineering', 'github-blog', 30),
    ('ai-engineering', 'cloudflare-blog', 40),
    ('ai-engineering', 'mit-technology-review', 50),
    ('science-world', 'nature', 10),
    ('science-world', 'bbc-world', 20),
    ('science-world', 'npr-technology', 30),
    ('chinese-productivity', 'sspai', 10);

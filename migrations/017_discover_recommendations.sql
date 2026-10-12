-- Keep the Discover taxonomy useful without silently subscribing the user.
-- Every URL below is a public RSS/Atom endpoint maintained by its publisher.
-- Joining back by URL also makes this migration safe when a user subscribed to
-- the same feed before the curated entry was added and therefore owns a
-- generated source ID instead of the catalog ID below.
WITH recommendations(id, name, url, description) AS (
    VALUES
        ('meta-newsroom', 'Meta Newsroom', 'https://about.fb.com/news/feed/', '社交平台、产品与生态动态'),
        ('smashing-magazine', 'Smashing Magazine', 'https://www.smashingmagazine.com/feed/', '设计、前端与用户体验实践'),
        ('nasa-live-missions', 'NASA Live & Missions', 'https://www.nasa.gov/news-release/feed/', '发射、任务与直播安排更新'),
        ('nasa-podcast', 'NASA Podcast', 'https://www.nasa.gov/feeds/podcasts/houston-we-have-a-podcast', 'NASA 访谈、任务故事与音频节目'),
        ('nasa-image-of-the-day', 'NASA Image of the Day', 'https://www.nasa.gov/feeds/iotd-feed/', 'NASA 每日图片与背景说明'),
        ('myanimelist-news', 'MyAnimeList News', 'https://myanimelist.net/rss/news.xml', '动画、漫画与行业资讯'),
        ('github-changelog', 'GitHub Changelog', 'https://github.blog/changelog/feed/', 'GitHub 产品发布、改进与退役公告'),
        ('google-shopping-blog', 'Google Shopping Blog', 'https://blog.google/products/shopping/rss/', '购物产品、消费发现与商家动态'),
        ('playstation-blog', 'PlayStation Blog', 'https://blog.playstation.com/feed/', 'PlayStation 游戏、更新与开发者内容'),
        ('project-gutenberg-new-books', 'Project Gutenberg New Books', 'https://www.gutenberg.org/cache/epub/feeds/today.rss', 'Project Gutenberg 每日新增与更新电子书'),
        ('sec-press-releases', 'U.S. SEC Press Releases', 'https://www.sec.gov/news/pressreleases.rss', '美国证券监管与市场公告')
)
INSERT OR IGNORE INTO feed_sources
    (id, kind, name, url, description, is_default, status, created_at, updated_at)
SELECT id, 'rss', name, url, description, 0, 'unavailable', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM recommendations;

WITH recommendations(url, site_url, category, language, icon_text, icon_color, tags_json) AS (
    VALUES
        ('https://about.fb.com/news/feed/', 'https://about.fb.com/news/', 'social-media', 'en', 'M', '#2563eb', '["社交媒体","产品","平台"]'),
        ('https://www.smashingmagazine.com/feed/', 'https://www.smashingmagazine.com/', 'design', 'en', 'S', '#dc2626', '["设计","前端","UX"]'),
        ('https://www.nasa.gov/news-release/feed/', 'https://www.nasa.gov/live/', 'live', 'en', 'N', '#1d4ed8', '["直播","太空","任务"]'),
        ('https://www.nasa.gov/feeds/podcasts/houston-we-have-a-podcast', 'https://www.nasa.gov/podcasts/houston-we-have-a-podcast/', 'multimedia', 'en', 'N', '#0f172a', '["播客","音频","太空"]'),
        ('https://www.nasa.gov/feeds/iotd-feed/', 'https://www.nasa.gov/image-of-the-day/', 'picture', 'en', 'N', '#4338ca', '["图片","科学","太空"]'),
        ('https://myanimelist.net/rss/news.xml', 'https://myanimelist.net/news', 'anime', 'en', 'M', '#2563eb', '["动画","漫画","行业"]'),
        ('https://github.blog/changelog/feed/', 'https://github.blog/changelog/', 'program-update', 'en', 'G', '#24292f', '["更新","开发","GitHub"]'),
        ('https://blog.google/products/shopping/rss/', 'https://blog.google/products/shopping/', 'shopping', 'en', 'G', '#16a34a', '["购物","产品","商家"]'),
        ('https://blog.playstation.com/feed/', 'https://blog.playstation.com/', 'game', 'en', 'P', '#2563eb', '["游戏","PlayStation","更新"]'),
        ('https://www.gutenberg.org/cache/epub/feeds/today.rss', 'https://www.gutenberg.org/ebooks/feeds.html', 'reading', 'en', 'P', '#92400e', '["阅读","电子书","公共领域"]'),
        ('https://www.sec.gov/news/pressreleases.rss', 'https://www.sec.gov/newsroom/press-releases', 'finance', 'en', 'S', '#0f766e', '["金融","监管","市场"]')
)
INSERT INTO feed_catalog_entries
    (source_id, site_url, category, language, icon_text, icon_color, tags_json,
     feed_format, recommended, popularity, validation_status, validated_at, catalog_origin)
SELECT f.id, r.site_url, r.category, r.language, r.icon_text, r.icon_color, r.tags_json,
       'rss', 1, 0, 'verified', '2026-08-08T16:00:00Z', 'zettlab-maintained-public-rss-v2'
FROM recommendations r
JOIN feed_sources f ON f.url = r.url
WHERE 1
ON CONFLICT(source_id) DO UPDATE SET
    site_url = excluded.site_url,
    category = excluded.category,
    language = excluded.language,
    icon_text = excluded.icon_text,
    icon_color = excluded.icon_color,
    tags_json = excluded.tags_json,
    feed_format = excluded.feed_format,
    recommended = excluded.recommended,
    validation_status = excluded.validation_status,
    validated_at = excluded.validated_at,
    catalog_origin = excluded.catalog_origin;

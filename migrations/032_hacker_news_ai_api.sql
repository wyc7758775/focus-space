-- The curated RSS endpoint is intermittently unavailable. Keep the stable
-- source id and user subscription state while switching collection to the
-- bounded Hacker News search API parsed by the application.
UPDATE feed_sources
SET kind = 'api',
    url = 'https://hn.algolia.com/api/v1/search_by_date?tags=story&query=AI&hitsPerPage=50',
    name = 'Hacker News AI',
    description = 'Hacker News 的 AI、模型与智能体文章',
    status = 'unavailable',
    last_error = '',
    next_sync_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'hacker-news'
  AND url IN (
    'https://news.ycombinator.com/rss',
    'https://hnrss.org/frontpage',
    'https://hnrss.org/newest?q=AI+OR+LLM+OR+GPT+OR+Claude+OR+Gemini+OR+Anthropic+OR+OpenAI'
  );

UPDATE feed_catalog_entries
SET site_url = 'https://news.ycombinator.com/newest',
    category = 'ai',
    tags_json = '["AI","LLM","模型","智能体"]',
    feed_format = 'api',
    recommended = 1,
    validation_status = 'verified',
    validated_at = '2026-08-11T00:00:00Z',
    catalog_origin = 'zettlab-maintained-public-api-v1'
WHERE source_id = 'hacker-news';

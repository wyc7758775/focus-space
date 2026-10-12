-- Keep the stable source id and all user-owned subscription state while
-- replacing the generic front page with an AI-specific Hacker News feed.
UPDATE feed_sources
SET name = 'Hacker News AI',
    url = 'https://hnrss.org/newest?q=AI+OR+LLM+OR+GPT+OR+Claude+OR+Gemini+OR+Anthropic+OR+OpenAI',
    description = 'Hacker News 的 AI、模型与智能体文章',
    status = 'unavailable',
    last_error = '',
    next_sync_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = 'hacker-news'
  AND url IN ('https://news.ycombinator.com/rss', 'https://hnrss.org/frontpage')
  AND NOT EXISTS (
    SELECT 1 FROM feed_sources existing
    WHERE existing.url = 'https://hnrss.org/newest?q=AI+OR+LLM+OR+GPT+OR+Claude+OR+Gemini+OR+Anthropic+OR+OpenAI'
      AND existing.id <> 'hacker-news'
  );

UPDATE feed_catalog_entries
SET site_url = 'https://news.ycombinator.com/newest',
    category = 'ai',
    tags_json = '["AI","LLM","模型","智能体"]',
    feed_format = 'rss',
    recommended = 1,
    validation_status = 'verified',
    validated_at = '2026-08-11T00:00:00Z',
    catalog_origin = 'zettlab-maintained-public-rss-v3'
WHERE source_id = 'hacker-news';

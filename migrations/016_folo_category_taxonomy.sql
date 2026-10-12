-- Align the maintained public recommendations with the complete Discover
-- category taxonomy. This changes presentation only; subscriptions, entries
-- and user state keep their existing IDs.
UPDATE feed_catalog_entries SET category = 'programming' WHERE source_id = 'openai-news';
UPDATE feed_catalog_entries SET category = 'bbs' WHERE source_id = 'hacker-news';
UPDATE feed_catalog_entries SET category = 'new-media' WHERE source_id = 'sspai';
UPDATE feed_catalog_entries SET category = 'programming' WHERE source_id = 'github-blog';
UPDATE feed_catalog_entries SET category = 'blog' WHERE source_id = 'cloudflare-blog';
UPDATE feed_catalog_entries SET category = 'programming' WHERE source_id = 'mozilla-hacks';
UPDATE feed_catalog_entries SET category = 'new-media' WHERE source_id = 'the-verge';
UPDATE feed_catalog_entries SET category = 'traditional-media' WHERE source_id IN ('bbc-world', 'npr-technology');
UPDATE feed_catalog_entries SET category = 'journal' WHERE source_id = 'nature';
UPDATE feed_catalog_entries SET category = 'new-media' WHERE source_id = 'mit-technology-review';

UPDATE source_packs SET category = 'programming' WHERE id = 'ai-engineering';
UPDATE source_packs SET category = 'journal' WHERE id = 'science-world';
UPDATE source_packs SET category = 'new-media' WHERE id = 'chinese-productivity';

ALTER TABLE entry_full_texts ADD COLUMN content_html TEXT NOT NULL DEFAULT '';
ALTER TABLE entry_full_texts ADD COLUMN image_urls_json TEXT NOT NULL DEFAULT '[]';

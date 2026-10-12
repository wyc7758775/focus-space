-- Persist the exact image index that represents an article lead image.
-- -1 means the thumbnail/OG image is outside the rendered article body.
ALTER TABLE entry_full_texts ADD COLUMN lead_image_index INTEGER NOT NULL DEFAULT -1;

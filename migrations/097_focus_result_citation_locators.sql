-- Add stable source and media locators to result citations.
--
-- These fields supplement the immutable citation row; the legacy article
-- identifiers and paragraph fields remain unchanged so older clients and
-- databases continue to resolve citations. Empty defaults keep historical
-- rows readable until their next analysis run writes structured locators.
ALTER TABLE focus_result_citations
    ADD COLUMN locator TEXT NOT NULL DEFAULT '';

ALTER TABLE focus_result_citations
    ADD COLUMN source_locator_json TEXT NOT NULL DEFAULT '{}';

ALTER TABLE focus_result_citations
    ADD COLUMN asset_id TEXT NOT NULL DEFAULT '';

ALTER TABLE focus_result_citations
    ADD COLUMN mime TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_focus_result_citations_asset
    ON focus_result_citations(owner_id, profile_id, asset_id)
    WHERE asset_id <> '';

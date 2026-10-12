-- Folo account portraits are authoritative for fixed X sources. Remove a
-- generic aggregator/site favicon left by an older icon-fetch race; the next
-- normal source sync repopulates the verified pbs.twimg.com portrait.
UPDATE feed_catalog_entries
SET icon_url = ''
WHERE source_id IN (
    SELECT id FROM feed_sources
    WHERE kind = 'api' AND url LIKE 'https://api.folo.is/feeds?%'
)
AND icon_url <> ''
AND lower(icon_url) NOT LIKE 'https://pbs.twimg.com/%';

DELETE FROM source_icon_cache
WHERE source_id IN (
    SELECT id FROM feed_sources
    WHERE kind = 'api' AND url LIKE 'https://api.folo.is/feeds?%'
)
AND lower(icon_url) NOT LIKE 'https://pbs.twimg.com/%';

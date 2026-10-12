-- Rebuild the two official history windows whose earlier collectors left
-- persistent bad state on upgraded devices. Keep every row and preference:
-- the next bounded sync replaces only unprotected rows outside retention.

-- Early EPO builds stored publication dates as YYYYMMDD. Normalize them before
-- retention ordering so the latest 1000-row window can evict older records.
UPDATE news
SET published_at = substr(published_at, 1, 4) || '-' || substr(published_at, 5, 2) || '-' || substr(published_at, 7, 2) || 'T00:00:00Z',
    updated_at = CURRENT_TIMESTAMP
WHERE source_id = 'epo-publication-server'
  AND origin = 'app-fetch'
  AND length(published_at) = 8
  AND published_at NOT GLOB '*[^0-9]*'
  AND date(substr(published_at, 1, 4) || '-' || substr(published_at, 5, 2) || '-' || substr(published_at, 7, 2)) IS NOT NULL;

-- SEC archive directories remove accession hyphens, but the index filename
-- keeps them. Rewrite valid old app-fetched links deterministically, including
-- favorites and stable-context rows, without touching authored projections.
UPDATE news
SET url = 'https://www.sec.gov/Archives/edgar/data/' ||
          CAST(substr(external_id, 1, 10) AS INTEGER) || '/' ||
          replace(substr(external_id, 1, 20), '-', '') || '/' ||
          substr(external_id, 1, 20) || '-index.html',
    updated_at = CURRENT_TIMESTAMP
WHERE source_id = 'sec-xbrl-filings'
  AND origin = 'app-fetch'
  AND length(external_id) >= 20
  AND (length(external_id) = 20 OR substr(external_id, 21, 1) = ':')
  AND substr(external_id, 11, 1) = '-'
  AND substr(external_id, 14, 1) = '-'
  AND replace(substr(external_id, 1, 20), '-', '') NOT GLOB '*[^0-9]*'
  AND CAST(substr(external_id, 1, 10) AS INTEGER) > 0;

-- Drop stale cursors and validators. EPO now reads the complete latest window
-- in one pass; SEC revalidates its newest pages while repaired older links stay
-- usable immediately.
UPDATE feed_sources
SET etag = '',
    last_modified = '',
    backfill_cursor = '',
    last_sync_at = '',
    next_sync_at = '',
    last_error = '',
    updated_at = CURRENT_TIMESTAMP
WHERE id IN ('epo-publication-server', 'sec-xbrl-filings')
  AND collection_mode = 'app';

-- Move the fixed operational registry into the dashboard's bounded Go
-- collector. Existing entries and user state remain untouched; only sources
-- that still use the retired Agent collection path are migrated.
UPDATE feed_sources
SET collection_mode = 'app',
    status = CASE
        WHEN records_used = 0 AND last_error = '等待 Agent 采集' THEN 'partial'
        ELSE status
    END,
    last_error = CASE
        WHEN records_used = 0 AND last_error = '等待 Agent 采集' THEN '等待应用首次同步'
        ELSE last_error
    END,
    next_sync_at = CASE
        WHEN EXISTS (
            SELECT 1 FROM feed_subscriptions s
            WHERE s.source_id = feed_sources.id AND s.enabled = 1
        ) THEN CURRENT_TIMESTAMP
        ELSE next_sync_at
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE collection_mode = 'agent'
  AND id IN (
      'sec-xbrl-filings',
      'hkex-regulatory-announcements',
      'cninfo-disclosures',
      'sse-periodic-reports',
      'govinfo-federal-register',
      'eurlex-legislation',
      'china-policy-library',
      'arxiv-ai',
      'crossref-research',
      'epo-publication-server',
      'cnipa-patent-data',
      'wipo-patentscope'
  );

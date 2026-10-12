-- Keep the Agent-owned YouTube sources useful on first open. The provider
-- endpoint is bounded and currently returns its available recent page; the
-- wider local window prevents the app from discarding older entries that are
-- present in that response.
UPDATE feed_sources
SET backfill_days=365,
    retention_days=365,
    retention_limit=1000,
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE kind='youtube'
  AND id IN (
    'youtube-google-deepmind', 'youtube-anthropic', 'youtube-nvidia',
    'youtube-two-minute-papers', 'youtube-ai-explained',
    'youtube-computerphile', 'youtube-fireship', 'youtube-lex-fridman'
  );

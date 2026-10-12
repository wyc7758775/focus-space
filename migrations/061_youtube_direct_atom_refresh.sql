-- Migration 059 moved the default YouTube sources away from the Folo
-- wrapper, but an already-synced database could keep the old four-item
-- result until its normal interval elapsed. Queue one immediate direct Atom
-- refresh without changing subscription, folder, read, or starred state.
UPDATE feed_sources
SET next_sync_at=CURRENT_TIMESTAMP,
    etag='',
    last_modified='',
    updated_at=CURRENT_TIMESTAMP
WHERE id IN (
    'youtube-google-deepmind', 'youtube-anthropic', 'youtube-nvidia',
    'youtube-two-minute-papers', 'youtube-ai-explained',
    'youtube-computerphile', 'youtube-fireship', 'youtube-lex-fridman'
)
  AND url LIKE 'https://www.youtube.com/feeds/videos.xml?channel_id=%';

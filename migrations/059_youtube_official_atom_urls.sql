-- The original YouTube seed used the Folo wrapper, whose response currently
-- contains only four entries and no continuation cursor. Store the official
-- channel Atom URLs so previews and future syncs use the complete bounded feed.
UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCP7jMXSY2xbc3KCAE0MHQ-A',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-google-deepmind';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCrDwWp7EBBv4NwvScIpBDOA',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-anthropic';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCHuiy8bXnmK5nisYHUd1J5g',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-nvidia';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCbfYPyITQ-7l4upoX8nvctg',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-two-minute-papers';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCNJ1Ymd5yFuUPtn21xtRbbw',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-ai-explained';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UC9-y-6csu5WGm29I7JiwpnA',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-computerphile';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCsBjURrPoezykLs9EqgamOA',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-fireship';

UPDATE feed_sources
SET url='https://www.youtube.com/feeds/videos.xml?channel_id=UCSHZKyawb77ixDdsGog4iWA',
    updated_at=CURRENT_TIMESTAMP
WHERE id='youtube-lex-fridman';

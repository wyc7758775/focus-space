-- RSSHub's Sogou route for 硬氪 is currently unavailable. Use the verified
-- public historical column as a paged source; the next page is carried in
-- feed_sources.last_modified by the app collector.
UPDATE feed_sources
SET url='https://www.jintiankansha.me/column/vCiIX6UYpo?page=1',
    status='unavailable',
    records_seen=0,
    records_used=0,
    last_sync_at='',
    last_error='',
    etag='',
    last_modified='',
    next_sync_at=CURRENT_TIMESTAMP,
    updated_at=CURRENT_TIMESTAMP
WHERE id='wechat-huxiu';

UPDATE feed_catalog_entries
SET site_url='https://www.jintiankansha.me/column/vCiIX6UYpo',
    feed_format='rss',
    validation_status='verified',
    catalog_origin='zettlab-wechat-history-column-v1'
WHERE source_id='wechat-huxiu';

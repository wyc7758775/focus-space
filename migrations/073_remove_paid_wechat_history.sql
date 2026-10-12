-- Fully revert the historical aggregator experiment. Its public index may
-- replace original article URLs with a VIP paywall, so neither the source nor
-- any cached aggregator entry is allowed to remain in the reader.
UPDATE feed_sources
SET url='https://rsshub.rssforever.com/wechat/sogou/%E7%A1%AC%E6%B0%AA',
    status='unavailable', last_error='', last_modified='', updated_at=CURRENT_TIMESTAMP
WHERE id='wechat-huxiu';

UPDATE feed_catalog_entries
SET site_url='https://mp.weixin.qq.com/', feed_format='rss', validation_status='verified', catalog_origin='zettlab-maintained-public-wechat-rss-v1'
WHERE source_id IN ('wechat-huxiu','wechat-jiqizhixin','wechat-aiera','wechat-geekpark','wechat-qbitai');

DELETE FROM entry_full_texts
WHERE entry_id IN (
  SELECT id FROM news
  WHERE source_id LIKE 'wechat-%'
    AND (url LIKE 'https://www.jintiankansha.me/%' OR url LIKE 'https://www.jintiankansha.com/%')
);

DELETE FROM news
WHERE source_id LIKE 'wechat-%'
  AND (url LIKE 'https://www.jintiankansha.me/%' OR url LIKE 'https://www.jintiankansha.com/%');

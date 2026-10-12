-- The public history index can show titles but may replace article links with
-- a VIP paywall. Restore the verified direct RSS sources for accounts whose
-- original links are not publicly available; never persist the paywall page
-- as article content.
UPDATE feed_sources SET url='https://wechat2rss.xlab.app/feed/51e92aad2728acdd1fda7314be32b16639353001.xml', status='ok', last_error='', last_modified='', updated_at=CURRENT_TIMESTAMP WHERE id='wechat-jiqizhixin';
UPDATE feed_sources SET url='https://wechat2rss.xlab.app/feed/ede30346413ea70dbef5d485ea5cbb95cca446e7.xml', status='ok', last_error='', last_modified='', updated_at=CURRENT_TIMESTAMP WHERE id='wechat-aiera';
UPDATE feed_sources SET url='https://wechat2rss.xlab.app/feed/1a5aec98e71c707c8ca092bc2c255b9d4bac477d.xml', status='ok', last_error='', last_modified='', updated_at=CURRENT_TIMESTAMP WHERE id='wechat-geekpark';
UPDATE feed_sources SET url='https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml', status='ok', last_error='', last_modified='', updated_at=CURRENT_TIMESTAMP WHERE id='wechat-qbitai';

UPDATE feed_catalog_entries SET catalog_origin='zettlab-maintained-public-wechat-rss-v1', validation_status='verified' WHERE source_id IN ('wechat-jiqizhixin','wechat-aiera','wechat-geekpark','wechat-qbitai');

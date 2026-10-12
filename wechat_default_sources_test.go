package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

var defaultWechatTechSourceIDs = []string{
	"wechat-qbitai",
	"wechat-jiqizhixin",
	"wechat-geekpark",
	"wechat-aiera",
	"wechat-chaping",
	"wechat-founder-park",
	"wechat-fnos",
	"wechat-liuxiaopai",
	"wechat-ai-paper",
	"wechat-ali-tech",
	"wechat-paperweekly",
	"wechat-huxiu",
}

var gatewayWechatTechSourceIDs = map[string]bool{
	"wechat-qbitai": true, "wechat-jiqizhixin": true,
	"wechat-geekpark": true, "wechat-aiera": true,
}

var retiredWechatTechSourceIDs = map[string]bool{
	"wechat-ai-paper": true, "wechat-fnos": true,
	"wechat-huxiu": true, "wechat-founder-park": true,
	"wechat-liuxiaopai": true, "wechat-ali-tech": true,
	"wechat-chaping": true, "wechat-paperweekly": true,
}

func TestDefaultWechatTechSourcesPreserveUserPreferencesFlow(t *testing.T) {
	db = openTestDB(t)

	for _, sourceID := range defaultWechatTechSourceIDs {
		var sourceURL, kind, collectionMode, folderID, iconURL, origin, packID string
		var isDefault, enabled, interval, hidden int
		err := db.QueryRow(`
	SELECT f.url,f.kind,f.is_default,f.collection_mode,f.sync_interval_seconds,f.hidden,
	       s.enabled,s.folder_id,c.icon_url,c.catalog_origin,p.pack_id
FROM feed_sources f
JOIN feed_subscriptions s ON s.source_id=f.id
JOIN feed_catalog_entries c ON c.source_id=f.id
JOIN source_pack_members p ON p.source_id=f.id
WHERE f.id=? AND p.pack_id='wechat-tech'`, sourceID).Scan(
			&sourceURL, &kind, &isDefault, &collectionMode, &interval, &hidden,
			&enabled, &folderID, &iconURL, &origin, &packID,
		)
		if err != nil {
			t.Fatalf("read %s: %v", sourceID, err)
		}
		validCollector := collectionMode == "source_gateway" && sourceURL == "source-gateway://"+sourceID && interval == 1800 && isDefault == 1 && hidden == 0
		if retiredWechatTechSourceIDs[sourceID] {
			validCollector = collectionMode == "agent" && sourceURL == "source-retired://"+sourceID && isDefault == 0 && hidden == 1
		}
		if !validCollector || kind != "rss" ||
			enabled != 1 || folderID != "folder-wechat-tech" ||
			(iconURL != "" && !strings.HasPrefix(iconURL, "https://wx.qlogo.cn/")) ||
			origin != "zettlab-maintained-public-wechat-rss-v1" || packID != "wechat-tech" {
			t.Fatalf("unexpected default source %s: url=%q default=%d hidden=%d mode=%q interval=%d enabled=%d folder=%q icon=%q origin=%q pack=%q",
				sourceID, sourceURL, isDefault, hidden, collectionMode, interval, enabled,
				folderID, iconURL, origin, packID)
		}
	}

	if _, err := db.Exec(`UPDATE feed_subscriptions SET enabled=0,folder_id='',sort_order=999 WHERE source_id='wechat-qbitai'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET hidden=1,status='ok',records_seen=7 WHERE id='wechat-qbitai'`); err != nil {
		t.Fatal(err)
	}
	migration, err := appFS.ReadFile("migrations/037_default_wechat_tech_sources.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatalf("replay migration: %v", err)
	}
	var enabled, hidden, sortOrder, recordsSeen int
	var folderID, status string
	if err := db.QueryRow(`
SELECT s.enabled,s.folder_id,s.sort_order,f.hidden,f.status,f.records_seen
FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id
WHERE f.id='wechat-qbitai'`).Scan(
		&enabled, &folderID, &sortOrder, &hidden, &status, &recordsSeen,
	); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 || folderID != "" || sortOrder != 999 || hidden != 1 || status != "ok" || recordsSeen != 7 {
		t.Fatalf("migration replay overwrote user state: enabled=%d folder=%q sort=%d hidden=%d status=%q records=%d",
			enabled, folderID, sortOrder, hidden, status, recordsSeen)
	}
}

func TestWechatTechFeedArticleIsReadableInReaderFlow(t *testing.T) {
	db = openTestDB(t)
	feedURL := "https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml"
	body := `<rss xmlns:content="http://purl.org/rss/1.0/modules/content/" version="2.0"><channel>
<title>量子位</title><image><url>https://wx.qlogo.cn/mmhead/example/0</url></image>
<item><guid>wechat-entry-1</guid><title>AI 产业关键进展</title>
<link>https://mp.weixin.qq.com/s?__biz=example&amp;mid=1&amp;idx=1</link>
<pubDate>Wed, 12 Aug 2026 10:00:00 +0000</pubDate>
<content:encoded><![CDATA[<p>这是可以在看板内直接阅读的微信公众号正文第一段。</p><p>正文第二段包含 CEO 需要关注的技术趋势和行动线索。</p>]]></content:encoded>
</item></channel></rss>`
	feed, err := parseFetchedFeed(feedFetchResponse{
		Body: []byte(body), ContentType: "application/rss+xml", FinalURL: feedURL,
	}, feedURL)
	if err != nil {
		t.Fatal(err)
	}
	if feed.IconURL != "https://wx.qlogo.cn/mmhead/example/0" || len(feed.Items) != 1 {
		t.Fatalf("wechat feed projection = %#v", feed)
	}
	source, err := feedSourceByID("wechat-qbitai")
	if err != nil {
		t.Fatal(err)
	}
	items := selectFetchedFeedItems(source, feed, time.Time{})
	if len(items) != 1 || !strings.HasPrefix(items[0].URL, "https://mp.weixin.qq.com/s?") ||
		!strings.Contains(items[0].SummaryText, "直接阅读") ||
		!strings.Contains(items[0].SummaryText, "行动线索") {
		t.Fatalf("wechat article is not readable: %#v", items)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, items[0], "2026-08-12T10:01:00Z"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	content := callJSON(t, entryContentHandler, http.MethodGet,
		"/api/entries/content?id="+items[0].ID, "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), "正文第二段") ||
		!strings.Contains(content.Body.String(), `"status":"ok"`) ||
		!strings.Contains(content.Body.String(), `"cached":true`) ||
		!strings.Contains(content.Body.String(), `"content_html":`) {
		t.Fatalf("feed-provided reader content = %d %s", content.Code, content.Body.String())
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/sources/preview?source_id=wechat-qbitai", nil)
	sourcePreviewHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}
	var preview struct {
		Origin string     `json:"preview_origin"`
		Items  []NewsItem `json:"preview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Origin != "local_sqlite" || len(preview.Items) != 1 ||
		!strings.Contains(preview.Items[0].SummaryText, "正文第二段") ||
		!strings.HasPrefix(preview.Items[0].URL, "https://mp.weixin.qq.com/") {
		t.Fatalf("reader preview = %#v", preview)
	}
}

func TestAdditionalWechatResearchSourcesHaveStableCuratedMetadata(t *testing.T) {
	db = openTestDB(t)

	expected := map[string]struct {
		name string
	}{
		"wechat-ali-tech":    {name: "阿里技术"},
		"wechat-paperweekly": {name: "PaperWeekly"},
		"wechat-jiqizhixin":  {name: "机器之心"},
		"wechat-aiera":       {name: "新智元"},
		"wechat-geekpark":    {name: "极客公园"},
		"wechat-qbitai":      {name: "量子位"},
		"wechat-huxiu":       {name: "硬氪"},
	}
	for id, want := range expected {
		var name, sourceURL, folderID, packID, validation, origin string
		var enabled, isDefault int
		err := db.QueryRow(`
SELECT f.name,f.url,f.is_default,s.enabled,s.folder_id,
       p.pack_id,c.validation_status,c.catalog_origin
FROM feed_sources f
JOIN feed_subscriptions s ON s.source_id=f.id
JOIN source_pack_members p ON p.source_id=f.id AND p.pack_id='wechat-tech'
JOIN feed_catalog_entries c ON c.source_id=f.id
WHERE f.id=?`, id).Scan(&name, &sourceURL, &isDefault, &enabled, &folderID, &packID, &validation, &origin)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		validCollector := sourceURL == "source-gateway://"+id && isDefault == 1 && enabled == 1
		if retiredWechatTechSourceIDs[id] {
			validCollector = sourceURL == "source-retired://"+id && isDefault == 0
		}
		if name != want.name || !validCollector ||
			folderID != "folder-wechat-tech" || packID != "wechat-tech" ||
			validation != "verified" || origin != "zettlab-maintained-public-wechat-rss-v1" {
			t.Fatalf("unexpected curated metadata for %s: name=%q url=%q default=%d enabled=%d folder=%q pack=%q validation=%q origin=%q",
				id, name, sourceURL, isDefault, enabled, folderID, packID, validation, origin)
		}
	}
}

func TestVerifiedWeChatFeedsMigrationUpgradeFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "088_")
	previousDB := db
	db = conn
	t.Cleanup(func() { db = previousDB })
	founderURL := weChatVerifiedDirectFeedURLs["wechat-founder-park"]
	liuURL := weChatVerifiedDirectFeedURLs["wechat-liuxiaopai"]
	older := "2026-08-19T00:00:00Z"
	now := "2026-08-20T00:00:00Z"

	if _, err := conn.Exec(`UPDATE feed_sources SET etag='old-etag',last_modified='old-modified',backfill_cursor='old-cursor',last_sync_at='2026-08-20T00:00:00Z',next_sync_at='2026-08-22T00:00:00Z',last_error='old-error',records_seen=7 WHERE id='wechat-founder-park'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_subscriptions SET enabled=0,folder_id='',title_override='',pinned=0,sort_order=0 WHERE source_id='wechat-founder-park'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,external_id) VALUES
('founder-old','历史文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=old','Founder Park','2026-08-01T00:00:00Z','摘要','text','',?,'wechat-founder-park','founder-old'),
('founder-canonical-overlap','规范重叠文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=shared','Founder Park','2026-08-02T00:00:00Z','规范摘要','text','',?,'wechat-founder-park','shared-founder-entry'),
	('founder-canonical-url-overlap','规范 URL 重叠文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=url-shared','Founder Park','2026-08-03T00:00:00Z','规范摘要','text','',?,'wechat-founder-park','canonical-url-entry'),
	('founder-canonical-reactivate','已隐藏规范文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=reactivate','Founder Park','2026-08-04T00:00:00Z','旧摘要','text','',?,'wechat-founder-park','reactivate-entry'),
	('founder-canonical-multi','多重复规范文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=multi','Founder Park','2026-08-05T00:00:00Z','规范摘要','text','',?,'wechat-founder-park','canonical-multi-entry')`, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE news SET deleted_at=? WHERE id='founder-canonical-reactivate'`, older); err != nil {
		t.Fatal(err)
	}

	if _, err := conn.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES('user-founder-copy','rss','Founder Park copy',?,'',0,'ok',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, founderURL); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES('user-founder-copy',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_subscriptions SET folder_id='folder-wechat-tech',title_override='Founder 自定义',pinned=1,sort_order=37 WHERE source_id='user-founder-copy'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,external_id) VALUES
('founder-copy-history','重复源独有历史','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=copy','Founder Park copy','2026-08-04T00:00:00Z','摘要','text','',?,'user-founder-copy','founder-copy-history'),
('founder-copy-overlap','重复源重叠文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=duplicate-shared','Founder Park copy','2026-08-02T00:00:00Z','重复摘要','text','',?,'user-founder-copy','shared-founder-entry'),
	('founder-copy-url-overlap','重复源 URL 重叠文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=url-shared','Founder Park copy','2026-08-03T00:00:00Z','重复摘要','text','',?,'user-founder-copy',''),
	('founder-copy-reactivate','可见重复源文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=reactivate-copy','Founder Park copy','2026-08-04T00:00:00Z','新摘要','text','',?,'user-founder-copy','reactivate-entry'),
	('founder-copy-multi-stale','多重复旧文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=multi','Founder Park copy','2026-08-05T00:00:00Z','旧摘要','text','',?,'user-founder-copy',''),
	('founder-copy-multi-exact','多重复准确文章','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=multi','Founder Park copy','2026-08-05T00:00:00Z','准确摘要','text','',?,'user-founder-copy','')`, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE news SET content_hash=CASE
WHEN id = 'founder-canonical-overlap' THEN 'shared-content-hash'
WHEN id = 'founder-copy-overlap' THEN 'duplicate-content-hash'
WHEN id IN ('founder-canonical-url-overlap','founder-copy-url-overlap') THEN 'url-content-hash'
WHEN id = 'founder-copy-history' THEN 'history-content-hash'
WHEN id IN ('founder-canonical-multi','founder-copy-multi-exact') THEN 'multi-current-hash'
WHEN id = 'founder-copy-multi-stale' THEN 'multi-old-hash'
ELSE content_hash END
WHERE id IN ('founder-canonical-overlap','founder-copy-overlap','founder-canonical-url-overlap','founder-copy-url-overlap','founder-copy-history','founder-canonical-multi','founder-copy-multi-exact','founder-copy-multi-stale')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO news_preferences(news_id,dismissed,updated_at) VALUES
('founder-canonical-overlap',0,?),('founder-copy-overlap',1,?)`, older, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES
('news:founder-canonical-overlap',1,0,0,0.25,?),
('news:founder-copy-overlap',0,1,1,0.75,?),
('news:founder-copy-history',0,1,0,0,?)`, older, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO stable_context_items(
owner_id,profile_id,item_key,state,added_by,added_at,removed_at,updated_at,content_hash,source_type,source_id,source_name,original_url,title,published_at,content_format,content_snapshot)
VALUES
('owner','profile','news:founder-canonical-overlap','removed','favorite',?, ?,?,'','rss','wechat-founder-park','','','','','text',''),
('owner','profile','news:founder-copy-overlap','active','manual_confirm',?,'',?,'duplicate-context-hash','rss','user-founder-copy','Founder Park copy','https://mp.weixin.qq.com/s?mid=shared','重复源重叠文章','2026-08-02T00:00:00Z','html','<p>上下文正文</p>'),
('owner','profile','news:founder-copy-history','active','favorite',?,'',?,'history-context-hash','rss','user-founder-copy','Founder Park copy','https://mp.weixin.qq.com/s?mid=copy','重复源独有历史','2026-08-04T00:00:00Z','text','独有正文')`, older, older, older, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,source_url,updated_at,content_hash,content_html,image_urls_json,lead_image_index) VALUES
('founder-canonical-overlap','','text',?,'unavailable','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=shared',?,'shared-content-hash','','[]',-1),
('founder-copy-overlap','重叠全文','html',?,'ok','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=duplicate-shared',?,'duplicate-content-hash','<article>重叠富文本</article>','["https://mmbiz.qpic.cn/lead.jpg","https://mmbiz.qpic.cn/body.jpg"]',1),
('founder-canonical-url-overlap','规范较新全文','html',?,'ok','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=url-shared',?,'url-content-hash','<article>规范较新富文本</article>','["https://mmbiz.qpic.cn/canonical.jpg"]',0),
('founder-copy-url-overlap','重复较旧全文','html',?,'ok','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=url-shared',?,'url-content-hash','<article>重复较旧富文本</article>','["https://mmbiz.qpic.cn/duplicate.jpg"]',0),
('founder-copy-multi-stale','多重复旧全文','html',?,'stale','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=multi',?,'multi-old-hash','<article>多重复旧富文本</article>','["https://mmbiz.qpic.cn/multi-old.jpg"]',0),
('founder-copy-multi-exact','多重复准确全文','html',?,'ok','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=multi',?,'multi-current-hash','<article>多重复准确富文本</article>','["https://mmbiz.qpic.cn/multi-current.jpg"]',0),
('founder-copy-history','独有全文','text',?,'ok','https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=copy',?,'history-content-hash','<p>独有富文本</p>','["https://mmbiz.qpic.cn/history.jpg"]',0)`, older, older, now, now, now, now, older, older, now, now, older, older, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,status,summary,key_points_json,impact,generated_at,error_code,prompt_version) VALUES
('founder-canonical-overlap','overview-hash','failed','','[]','',?,'old_failure','article-overview-v1'),
('founder-copy-overlap','overview-hash','ready','重叠文章概览','["要点"]','影响',?,'','article-overview-v1')`, older, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO reader_digest_receipts(content_hash,source_id,entry_id,processed_at,outcome) VALUES
('digest-hash','user-founder-copy','founder-copy-overlap',?,'drafted'),
('digest-history-hash','user-founder-copy','founder-copy-history',?,'no_candidate')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO reader_focus_runs(
id,scope,run_day,topic_ids_json,candidate_count,status,idempotency_key,created_at,updated_at,selector_key,expires_at)
VALUES
('canonical-claim-run','daily','2026-08-20','[]',1,'pending','reader-focus-claim:canonical',?,?,'topic:canonical','2026-08-21T00:00:00Z'),
('duplicate-claim-run','daily','2026-08-20','[]',1,'pending','reader-focus-claim:duplicate',?,?,'topic:duplicate','2026-08-21T00:00:00Z'),
('url-claim-run','daily','2026-08-20','[]',1,'pending','reader-focus-claim:url',?,?,'topic:url','2026-08-21T00:00:00Z')`, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO reader_focus_claim_items(
run_id,entry_id,content_hash,topic_ids_json,estimated_input_tokens,estimated_output_tokens,state,created_at,updated_at)
VALUES
('canonical-claim-run','founder-canonical-overlap','shared-content-hash','[]',100,20,'claimed',?,?),
('duplicate-claim-run','founder-copy-overlap','shared-content-hash','[]',100,20,'claimed',?,?),
('url-claim-run','founder-copy-url-overlap','url-content-hash','[]',100,20,'claimed',?,?)`, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO focus_spaces(id,owner_id,profile_id,title,goal,created_at,updated_at)
VALUES('focus-wechat','owner','profile','微信关注','跟踪更新',?,?)`, older, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO focus_space_sources(
focus_id,owner_id,profile_id,source_kind,source_id,enabled,revision,created_at,updated_at)
VALUES
('focus-wechat','owner','profile','rss','wechat-founder-park',0,2,?,?),
('focus-wechat','owner','profile','rss','user-founder-copy',1,3,?,?)`, older, older, now, now); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "088_verified_wechat_feeds.sql")
	// The current reader handler consumes the additive V3 block and Chat-link
	// ownership columns. Layer those two forward-compatible migrations onto the
	// historical 088 fixture without applying later source-policy migrations.
	applyMigrationFile(t, conn, "093_article_blocks_and_citations.sql")
	applyMigrationFile(t, conn, "098_focus_chat_link_scope.sql")

	var sourceURL, kind, mode, etag, modified, cursor, lastSync, nextSync, lastError string
	var recordsSeen int
	if err := conn.QueryRow(`SELECT url,kind,collection_mode,etag,last_modified,backfill_cursor,last_sync_at,next_sync_at,last_error,records_seen FROM feed_sources WHERE id='wechat-founder-park'`).Scan(&sourceURL, &kind, &mode, &etag, &modified, &cursor, &lastSync, &nextSync, &lastError, &recordsSeen); err != nil {
		t.Fatal(err)
	}
	if sourceURL != founderURL || kind != "rss" || mode != "app" || etag != "" || modified != "" || cursor != "" || lastSync != "" || nextSync != "" || lastError != "" || recordsSeen != 7 {
		t.Fatalf("migrated founder source url=%q kind=%q mode=%q etag=%q modified=%q cursor=%q last=%q next=%q error=%q records=%d", sourceURL, kind, mode, etag, modified, cursor, lastSync, nextSync, lastError, recordsSeen)
	}
	if err := conn.QueryRow(`SELECT url FROM feed_sources WHERE id='wechat-liuxiaopai'`).Scan(&sourceURL); err != nil || sourceURL != liuURL {
		t.Fatalf("migrated liuxiaopai url=%q err=%v", sourceURL, err)
	}

	var enabled, pinned, sortOrder int
	var folderID, titleOverride string
	if err := conn.QueryRow(`SELECT enabled,folder_id,title_override,pinned,sort_order FROM feed_subscriptions WHERE source_id='wechat-founder-park'`).Scan(&enabled, &folderID, &titleOverride, &pinned, &sortOrder); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 || folderID != "folder-wechat-tech" || titleOverride != "Founder 自定义" || pinned != 1 || sortOrder != 37 {
		t.Fatalf("merged subscription enabled=%d folder=%q title=%q pinned=%d sort=%d", enabled, folderID, titleOverride, pinned, sortOrder)
	}
	var duplicateURL, duplicateMode string
	var duplicateHidden, duplicateEnabled int
	if err := conn.QueryRow(`SELECT f.url,f.collection_mode,f.hidden,s.enabled FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id WHERE f.id='user-founder-copy'`).Scan(&duplicateURL, &duplicateMode, &duplicateHidden, &duplicateEnabled); err != nil {
		t.Fatal(err)
	}
	if duplicateURL != "source-merged://user-founder-copy" || duplicateMode != "agent" || duplicateHidden != 1 || duplicateEnabled != 0 {
		t.Fatalf("retired duplicate url=%q mode=%q hidden=%d enabled=%d", duplicateURL, duplicateMode, duplicateHidden, duplicateEnabled)
	}
	var historySourceID, historySourceName, historyDeletedAt string
	if err := conn.QueryRow(`SELECT source_id,source,deleted_at FROM news WHERE id='founder-copy-history'`).Scan(&historySourceID, &historySourceName, &historyDeletedAt); err != nil {
		t.Fatal(err)
	}
	if historySourceID != "wechat-founder-park" || historySourceName != "Founder Park" || historyDeletedAt != "" {
		t.Fatalf("non-overlap history source=%q name=%q deleted=%q", historySourceID, historySourceName, historyDeletedAt)
	}
	var retainedHistorySidecars int
	if err := conn.QueryRow(`SELECT
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:founder-copy-history' AND starred=1) +
(SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:founder-copy-history' AND source_id='wechat-founder-park' AND state='active') +
(SELECT COUNT(*) FROM entry_full_texts WHERE entry_id='founder-copy-history' AND content_text='独有全文')`).Scan(&retainedHistorySidecars); err != nil {
		t.Fatal(err)
	}
	if retainedHistorySidecars != 3 {
		t.Fatalf("non-overlap history sidecars retained=%d", retainedHistorySidecars)
	}

	var externalOverlapVisible, urlOverlapVisible, reactivatedOverlapVisible, multiOverlapVisible int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='wechat-founder-park' AND external_id='shared-founder-entry' AND deleted_at=''`).Scan(&externalOverlapVisible); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='wechat-founder-park' AND url='https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA%3D%3D&mid=url-shared' AND deleted_at=''`).Scan(&urlOverlapVisible); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE id='founder-canonical-reactivate' AND source_id='wechat-founder-park' AND deleted_at=''`).Scan(&reactivatedOverlapVisible); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE id='founder-canonical-multi' AND source_id='wechat-founder-park' AND deleted_at=''`).Scan(&multiOverlapVisible); err != nil {
		t.Fatal(err)
	}
	var deletedOverlapCopies int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE id IN ('founder-copy-overlap','founder-copy-url-overlap','founder-copy-reactivate','founder-copy-multi-stale','founder-copy-multi-exact') AND deleted_at<>''`).Scan(&deletedOverlapCopies); err != nil {
		t.Fatal(err)
	}
	if externalOverlapVisible != 1 || urlOverlapVisible != 1 || reactivatedOverlapVisible != 1 || multiOverlapVisible != 1 || deletedOverlapCopies != 5 {
		t.Fatalf("deduplicated overlap external=%d url=%d reactivated=%d multi=%d deleted_copies=%d", externalOverlapVisible, urlOverlapVisible, reactivatedOverlapVisible, multiOverlapVisible, deletedOverlapCopies)
	}

	var dismissed, duplicateNewsPreference int
	if err := conn.QueryRow(`SELECT dismissed FROM news_preferences WHERE news_id='founder-canonical-overlap'`).Scan(&dismissed); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news_preferences WHERE news_id='founder-copy-overlap'`).Scan(&duplicateNewsPreference); err != nil {
		t.Fatal(err)
	}
	var isRead, starred, readLater, duplicateItemPreference int
	var progress float64
	if err := conn.QueryRow(`SELECT is_read,starred,read_later,reading_progress FROM item_preferences WHERE item_key='news:founder-canonical-overlap'`).Scan(&isRead, &starred, &readLater, &progress); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM item_preferences WHERE item_key='news:founder-copy-overlap'`).Scan(&duplicateItemPreference); err != nil {
		t.Fatal(err)
	}
	if dismissed != 1 || duplicateNewsPreference != 0 || isRead != 1 || starred != 1 || readLater != 1 || progress != 0.75 || duplicateItemPreference != 0 {
		t.Fatalf("merged preferences dismissed=%d duplicate_news=%d read=%d starred=%d later=%d progress=%v duplicate_item=%d",
			dismissed, duplicateNewsPreference, isRead, starred, readLater, progress, duplicateItemPreference)
	}

	var contextState, contextAddedBy, contextRemovedAt, contextHash, contextSourceID, contextTitle string
	if err := conn.QueryRow(`SELECT state,added_by,removed_at,content_hash,source_id,title FROM stable_context_items
WHERE owner_id='owner' AND profile_id='profile' AND item_key='news:founder-canonical-overlap'`).Scan(
		&contextState, &contextAddedBy, &contextRemovedAt, &contextHash, &contextSourceID, &contextTitle,
	); err != nil {
		t.Fatal(err)
	}
	var duplicateContext int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:founder-copy-overlap'`).Scan(&duplicateContext); err != nil {
		t.Fatal(err)
	}
	if contextState != "active" || contextAddedBy != "manual_confirm" || contextRemovedAt != "" || contextHash != "duplicate-context-hash" || contextSourceID != "wechat-founder-park" || contextTitle != "重复源重叠文章" || duplicateContext != 0 {
		t.Fatalf("merged context state=%q added_by=%q removed=%q hash=%q source=%q title=%q duplicate=%d",
			contextState, contextAddedBy, contextRemovedAt, contextHash, contextSourceID, contextTitle, duplicateContext)
	}

	var fullText, fullHTML, imageURLs, fullTextStatus, fullTextHash, overviewStatus, overviewSummary, digestSourceID, digestEntryID string
	var leadImageIndex int
	if err := conn.QueryRow(`SELECT content_text,content_html,image_urls_json,lead_image_index,status,content_hash FROM entry_full_texts WHERE entry_id='founder-canonical-overlap'`).Scan(&fullText, &fullHTML, &imageURLs, &leadImageIndex, &fullTextStatus, &fullTextHash); err != nil {
		t.Fatal(err)
	}
	var canonicalNewerText, canonicalNewerHTML, canonicalNewerStatus, canonicalNewerHash string
	if err := conn.QueryRow(`SELECT content_text,content_html,status,content_hash FROM entry_full_texts WHERE entry_id='founder-canonical-url-overlap'`).Scan(&canonicalNewerText, &canonicalNewerHTML, &canonicalNewerStatus, &canonicalNewerHash); err != nil {
		t.Fatal(err)
	}
	var multiText, multiHTML, multiStatus, multiHash string
	if err := conn.QueryRow(`SELECT content_text,content_html,status,content_hash FROM entry_full_texts WHERE entry_id='founder-canonical-multi'`).Scan(&multiText, &multiHTML, &multiStatus, &multiHash); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT status,summary FROM article_ai_overviews WHERE entry_id='founder-canonical-overlap' AND content_hash='overview-hash'`).Scan(&overviewStatus, &overviewSummary); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT source_id,entry_id FROM reader_digest_receipts WHERE content_hash='digest-hash'`).Scan(&digestSourceID, &digestEntryID); err != nil {
		t.Fatal(err)
	}
	var historyDigestSourceID, historyDigestEntryID string
	if err := conn.QueryRow(`SELECT source_id,entry_id FROM reader_digest_receipts WHERE content_hash='digest-history-hash'`).Scan(&historyDigestSourceID, &historyDigestEntryID); err != nil {
		t.Fatal(err)
	}
	var duplicateDerivedRows int
	if err := conn.QueryRow(`SELECT
(SELECT COUNT(*) FROM entry_full_texts WHERE entry_id IN ('founder-copy-overlap','founder-copy-url-overlap','founder-copy-multi-stale','founder-copy-multi-exact')) +
(SELECT COUNT(*) FROM article_ai_overviews WHERE entry_id='founder-copy-overlap')`).Scan(&duplicateDerivedRows); err != nil {
		t.Fatal(err)
	}
	if fullText != "重叠全文" || fullHTML != "<article>重叠富文本</article>" || imageURLs != `["https://mmbiz.qpic.cn/lead.jpg","https://mmbiz.qpic.cn/body.jpg"]` || leadImageIndex != 1 || fullTextStatus != "stale" || fullTextHash != "" ||
		canonicalNewerText != "规范较新全文" || canonicalNewerHTML != "<article>规范较新富文本</article>" || canonicalNewerStatus != "ok" || canonicalNewerHash != "url-content-hash" ||
		multiText != "多重复准确全文" || multiHTML != "<article>多重复准确富文本</article>" || multiStatus != "ok" || multiHash != "multi-current-hash" ||
		overviewStatus != "ready" || overviewSummary != "重叠文章概览" || digestSourceID != "wechat-founder-park" || digestEntryID != "founder-canonical-overlap" ||
		historyDigestSourceID != "wechat-founder-park" || historyDigestEntryID != "founder-copy-history" || duplicateDerivedRows != 0 {
		t.Fatalf("merged derived rows full_text=%q html=%q images=%q lead=%d status/hash=%q/%q canonical=%q/%q/%q/%q multi=%q/%q/%q/%q overview=%q/%q digest=%q/%q history_digest=%q/%q duplicate=%d",
			fullText, fullHTML, imageURLs, leadImageIndex, fullTextStatus, fullTextHash,
			canonicalNewerText, canonicalNewerHTML, canonicalNewerStatus, canonicalNewerHash,
			multiText, multiHTML, multiStatus, multiHash,
			overviewStatus, overviewSummary, digestSourceID, digestEntryID, historyDigestSourceID, historyDigestEntryID, duplicateDerivedRows)
	}

	content := callJSON(t, entryContentHandler, http.MethodGet,
		"/api/entries/content?id=founder-canonical-overlap", "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), "重叠富文本") ||
		!strings.Contains(content.Body.String(), `"status":"stale"`) ||
		!strings.Contains(content.Body.String(), `"cached":true`) {
		t.Fatalf("migrated rich content is not readable: status=%d body=%s", content.Code, content.Body.String())
	}

	var affectedClaimRuns, affectedClaimItems int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM reader_focus_runs WHERE id IN ('duplicate-claim-run','url-claim-run')`).Scan(&affectedClaimRuns); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM reader_focus_claim_items WHERE run_id IN ('duplicate-claim-run','url-claim-run')`).Scan(&affectedClaimItems); err != nil {
		t.Fatal(err)
	}
	var canonicalClaimEntry, canonicalClaimState string
	if err := conn.QueryRow(`SELECT entry_id,state FROM reader_focus_claim_items WHERE run_id='canonical-claim-run'`).Scan(&canonicalClaimEntry, &canonicalClaimState); err != nil {
		t.Fatal(err)
	}
	if affectedClaimRuns != 0 || affectedClaimItems != 0 || canonicalClaimEntry != "founder-canonical-overlap" || canonicalClaimState != "claimed" {
		t.Fatalf("released claims runs=%d items=%d canonical=%q/%q",
			affectedClaimRuns, affectedClaimItems, canonicalClaimEntry, canonicalClaimState)
	}

	var focusEnabled, focusRevision, duplicateFocusSources int
	if err := conn.QueryRow(`SELECT enabled,revision FROM focus_space_sources
WHERE focus_id='focus-wechat' AND source_kind='rss' AND source_id='wechat-founder-park'`).Scan(&focusEnabled, &focusRevision); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM focus_space_sources WHERE source_id='user-founder-copy'`).Scan(&duplicateFocusSources); err != nil {
		t.Fatal(err)
	}
	if focusEnabled != 1 || focusRevision != 3 || duplicateFocusSources != 0 {
		t.Fatalf("merged focus source enabled=%d revision=%d duplicate=%d", focusEnabled, focusRevision, duplicateFocusSources)
	}
}

func TestVerifiedWeChatFeedsCollectAndPersistHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-founder-park", "wechat-liuxiaopai")
	tests := []struct {
		id        string
		name      string
		feedTitle string
		biz       []string
	}{
		{id: "wechat-founder-park", name: "Founder Park", feedTitle: "Founder Park", biz: []string{"Mzg5NTc0MjgwMw==", "MzY5ODQwMTkxNA=="}},
		{id: "wechat-liuxiaopai", name: "刘小排", feedTitle: "刘小排r", biz: []string{"MzI1MTUxNzgxMA=="}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			source, err := feedSourceByID(test.id)
			if err != nil {
				t.Fatal(err)
			}
			links := make([]string, 0, 75)
			for index := 0; index < 75; index++ {
				biz := test.biz[index%len(test.biz)]
				links = append(links, "https://mp.weixin.qq.com/s?__biz="+biz+"&mid=22475"+strconv.Itoa(10000+index)+"&idx=1&sn=verified"+strconv.Itoa(index))
			}
			fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
				return feedFetchResponse{Body: verifiedWeChatRSSFixture(test.feedTitle, links...), FinalURL: raw}, nil
			}
			result, handled, err := collectWeChatDiscoveredSourceContextWithFetcher(context.Background(), source, fetcher, time.Second, time.Second)
			if err != nil || !handled || result.Status != "ok" {
				t.Fatalf("handled=%v result=%#v err=%v", handled, result, err)
			}
			items := selectFetchedFeedItems(source, result.Feed, time.Time{})
			if len(items) != 75 {
				t.Fatalf("selected items=%d", len(items))
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if err := upsertFetchedNewsTx(tx, item, "2026-08-21T00:00:00Z"); err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, test.id).Scan(&count); err != nil || count != 75 {
				t.Fatalf("persisted count=%d err=%v", count, err)
			}
		})
	}
}

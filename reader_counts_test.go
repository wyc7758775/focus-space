package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func seedReaderCountEntry(t *testing.T, id, sourceID, sourceType, url string) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO news(id,title,url,source,source_id,source_type,view_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, id, url, sourceID, sourceID, sourceType, "article", "2026-09-01T00:00:00Z", id+" 摘要", "text", "", "hash-"+id, "2026-09-01T00:00:00Z")
	if err != nil {
		t.Fatalf("seed reader entry %s: %v", id, err)
	}
}

func seedCatalogSourceForContentTypeTest(t *testing.T, id, kind, url, feedFormat, tagsJSON string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO feed_sources
(id,kind,name,url,description,is_default,status,created_at,updated_at)
VALUES(?,?,?,?,?,0,'ready',?,?)`, id, kind, id, url, "catalog fixture", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatalf("seed feed source %s: %v", id, err)
	}
	if _, err := db.Exec(`INSERT INTO feed_catalog_entries
(source_id,site_url,category,language,icon_text,icon_color,tags_json,feed_format,recommended,popularity,validation_status,validated_at,catalog_origin)
VALUES(?,?,?,?,?,?,?, ?,0,0,'verified',?, 'test')`, id, url, "technology", "en", "T", "#64748b", tagsJSON, feedFormat, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatalf("seed catalog source %s: %v", id, err)
	}
}

func seedReaderCountSource(t *testing.T, id string) {
	t.Helper()
	if _, err := db.Exec(`INSERT OR IGNORE INTO feed_sources
(id,kind,name,url,description,is_default,status,created_at,updated_at)
VALUES(?,?,?,?,'',0,'ok',?,?)`, id, "rss", id, "https://example.com/"+id, "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatalf("seed reader source %s: %v", id, err)
	}
}

func TestSourceCatalogLightProjectionUsesBoundedCatalogTypeHints(t *testing.T) {
	db = openTestDB(t)
	seedCatalogSourceForContentTypeTest(t, "catalog-youtube-source", "youtube", "https://www.youtube.com/channel/test", "youtube", `["AI","视频"]`)
	// A row with a video view type must not be needed for the lightweight
	// projection; the catalog hint alone should classify this source.
	items, err := queryFeedSourcesForCatalog(20, "catalog-youtube-source", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("source rows = %d, want 1", len(items))
	}
	if items[0].EntryCount != 0 || items[0].UnreadCount != 0 {
		t.Fatalf("light counts = %d/%d, want zero placeholders", items[0].EntryCount, items[0].UnreadCount)
	}
	if !containsString(items[0].ContentTypes, "video") {
		t.Fatalf("catalog content types = %v, want video hint", items[0].ContentTypes)
	}
}

func TestSourceCatalogSourceIDProjectionReturnsOnlyRequestedSource(t *testing.T) {
	db = openTestDB(t)
	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id=github-blog&include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("scoped source status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 || payload.Sources[0].ID != "github-blog" {
		t.Fatalf("scoped source rows = %+v, want exactly github-blog", payload.Sources)
	}
	if payload.Sources[0].EntryCount != 0 || payload.Sources[0].UnreadCount != 0 {
		t.Fatalf("light scoped counts = %d/%d, want zero placeholders", payload.Sources[0].EntryCount, payload.Sources[0].UnreadCount)
	}
}

func TestSourceCatalogCountsRemainReadableBeforeFocusChatScopeMigration(t *testing.T) {
	// A device can be observed between migration files during an upgrade (and
	// historical migration tests intentionally exercise that state). The source
	// directory must keep working before migration 098 creates the ownership
	// table used by the modern visibility predicate.
	conn := openUpgradeTestDB(t, "098_")
	// 生产启动顺序是 迁移链 → reconcileSourceGatewaySchema 幂等补列 → 对外服务；
	// 本测模拟"可被查询的旧版本库"，需先复刻同一套补列，否则现代目录查询会
	// 引用启动期才补的列（如 feed_sources.gateway_last_success_at）。
	if err := reconcileSourceGatewaySchema(conn); err != nil {
		t.Fatalf("reconcile schema: %v", err)
	}
	previousDB := db
	db = conn
	t.Cleanup(func() { db = previousDB })

	items, err := queryFeedSourcesForCatalog(20, "openai-news", true, false)
	if err != nil {
		t.Fatalf("source catalog query before migration 098: %v", err)
	}
	if len(items) != 1 || items[0].ID != "openai-news" {
		t.Fatalf("source rows = %+v, want openai-news", items)
	}
}

func TestSourceCatalogLightProjectionDoesNotInferObservedTypesWithoutFilter(t *testing.T) {
	db = openTestDB(t)
	seedCatalogSourceForContentTypeTest(t, "catalog-rss-source", "rss", "https://example.com/feed.xml", "rss", `[]`)
	if _, err := db.Exec(`INSERT INTO news
(id,title,url,source,source_id,source_type,view_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "catalog-video-row", "video row", "https://example.com/video", "catalog-rss-source", "catalog-rss-source", "rss", "video", "2026-09-01T00:00:00Z", "summary", "text", "", "hash-catalog-video-row", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	items, err := queryFeedSourcesForCatalog(20, "catalog-rss-source", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || containsString(items[0].ContentTypes, "video") {
		t.Fatalf("light observed types = %+v, want catalog-only article hint", items)
	}
}

func TestSourceCatalogContentTypeFilterKeepsAuthoritativeObservedTypes(t *testing.T) {
	db = openTestDB(t)
	seedCatalogSourceForContentTypeTest(t, "filter-rss-source", "rss", "https://example.com/filter.xml", "rss", `[]`)
	if _, err := db.Exec(`INSERT INTO news
(id,title,url,source,source_id,source_type,view_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "filter-video-row", "video row", "https://example.com/filter-video", "filter-rss-source", "filter-rss-source", "rss", "video", "2026-09-01T00:00:00Z", "summary", "text", "", "hash-filter-video-row", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?include_counts=0&content_type=video", "")
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, source := range payload.Sources {
		if source.ID == "filter-rss-source" {
			if source.EntryCount != 0 || source.UnreadCount != 0 {
				t.Fatalf("filtered light counts = %d/%d, want zero placeholders", source.EntryCount, source.UnreadCount)
			}
			return
		}
	}
	t.Fatalf("observed video source missing from content_type filter: %s", result.Body.String())
}

func TestReaderCountsHandlerReturnsIndependentVisibleCounts(t *testing.T) {
	db = openTestDB(t)
	oldScope := appDataScope
	appDataScope = dataScope{OwnerID: "owner-test", ProfileID: "profile-test"}
	t.Cleanup(func() { appDataScope = oldScope })

	seedReaderCountEntry(t, "count-article", "count-source", "", "https://example.com/article")
	seedReaderCountEntry(t, "count-video", "count-source", "youtube", "https://youtube.com/watch?v=count-video")
	seedReaderCountEntry(t, "count-dismissed", "count-source", "", "https://example.com/dismissed")
	seedReaderCountEntry(t, "count-focus", "focus-chat-links", "", "https://example.com/focus")
	seedReaderCountEntry(t, "count-other-focus", "focus-chat-links", "", "https://example.com/other-focus")

	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES(?,?,?,?,?,?)`, "news:count-video", 1, 0, 0, 0, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news_preferences(news_id,dismissed,updated_at) VALUES(?,?,?)`, "count-dismissed", 1, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "owner-test", "profile-test", "count-focus", "https://example.com/focus", "hash-count-focus", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	result := callJSON(t, readerCountsHandler, http.MethodGet, "/api/reader/counts", "")
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", result.Code, result.Body.String())
	}
	var response readerCountsResponse
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.TotalEntries != 3 || response.UnreadEntries != 2 || response.VideoEntries != 1 || response.RSSEntries != 2 || response.YouTubeEntries != 1 {
		t.Fatalf("aggregate counts = %+v, want total=3 unread=2 video=1 rss=2 youtube=1", response)
	}
	if len(response.SourceCounts) != 2 {
		t.Fatalf("source counts = %+v, want two visible sources", response.SourceCounts)
	}
	var foundCountSource, foundFocusSource readerSourceCounts
	for _, source := range response.SourceCounts {
		switch source.SourceID {
		case "count-source":
			foundCountSource = source
		case "focus-chat-links":
			foundFocusSource = source
		}
	}
	if foundCountSource.TotalEntries != 2 || foundCountSource.UnreadEntries != 1 || foundCountSource.VideoEntries != 1 {
		t.Fatalf("count source = %+v", foundCountSource)
	}
	if foundFocusSource.TotalEntries != 1 || foundFocusSource.UnreadEntries != 1 {
		t.Fatalf("focus source = %+v", foundFocusSource)
	}
}

func TestSourceCatalogCountsMatchReaderCountsVisibilityScope(t *testing.T) {
	db = openTestDB(t)
	oldScope := appDataScope
	appDataScope = dataScope{OwnerID: "catalog-owner", ProfileID: "catalog-profile"}
	t.Cleanup(func() { appDataScope = oldScope })

	seedReaderCountSource(t, "catalog-count-source")
	seedReaderCountSource(t, "focus-chat-links")
	seedReaderCountEntry(t, "catalog-visible", "catalog-count-source", "", "https://example.com/catalog-visible")
	seedReaderCountEntry(t, "catalog-read", "catalog-count-source", "", "https://example.com/catalog-read")
	seedReaderCountEntry(t, "catalog-dismissed", "catalog-count-source", "", "https://example.com/catalog-dismissed")
	seedReaderCountEntry(t, "catalog-focus-visible", "focus-chat-links", "", "https://example.com/catalog-focus-visible")
	seedReaderCountEntry(t, "catalog-focus-foreign", "focus-chat-links", "", "https://example.com/catalog-focus-foreign")

	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES(?,?,?,?,?,?)`,
		"news:catalog-read", 1, 0, 0, 0, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news_preferences(news_id,dismissed,updated_at) VALUES(?,?,?)`,
		"catalog-dismissed", 1, "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"catalog-owner", "catalog-profile", "catalog-focus-visible", "https://example.com/catalog-focus-visible", "hash-catalog-focus-visible", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"other-owner", "other-profile", "catalog-focus-foreign", "https://example.com/catalog-focus-foreign", "hash-catalog-focus-foreign", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	readerRows, _, err := queryReaderSourceCountsWithContext(context.Background())
	if err != nil {
		t.Fatalf("reader source counts: %v", err)
	}
	want := make(map[string]readerSourceCounts, len(readerRows))
	for _, row := range readerRows {
		want[row.SourceID] = row
	}

	// Exercise the source_id path as well as the full catalog path.  The
	// visibility predicate has owner/profile placeholders inside SELECT
	// subqueries, so this catches both scope leaks and argument-order regressions.
	for _, sourceID := range []string{"", "catalog-count-source", "focus-chat-links"} {
		catalogRows, err := queryFeedSourcesForCatalog(100, sourceID, true, false)
		if err != nil {
			t.Fatalf("catalog source %q: %v", sourceID, err)
		}
		for _, source := range catalogRows {
			if sourceID != "" && source.ID != sourceID {
				t.Fatalf("filtered catalog returned %q for %q", source.ID, sourceID)
			}
			expected := want[source.ID]
			if source.EntryCount != expected.TotalEntries || source.UnreadCount != expected.UnreadEntries {
				t.Fatalf("source %q catalog counts=%d/%d reader=%d/%d; catalog must use the same visible scope", source.ID, source.EntryCount, source.UnreadCount, expected.TotalEntries, expected.UnreadEntries)
			}
		}
	}

	// The foreign focus link has no visible row of its own; the catalog still
	// exposes its source, but its count must be an explicit zero rather than the
	// shared news row leaking across profiles.
	var focusSource *FeedSource
	rows, err := queryFeedSourcesForCatalog(100, "focus-chat-links", true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("focus source rows=%d, want one", len(rows))
	}
	focusSource = &rows[0]
	if focusSource.EntryCount != 1 || focusSource.UnreadCount != 1 {
		t.Fatalf("focus source counts=%d/%d, want visible scoped row 1/1", focusSource.EntryCount, focusSource.UnreadCount)
	}
}

func TestReaderCountsHandlerSerializesZeroValues(t *testing.T) {
	db = openTestDB(t)
	result := callJSON(t, readerCountsHandler, http.MethodGet, "/api/reader/counts", "")
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", result.Code, result.Body.String())
	}
	var response readerCountsResponse
	if err := json.Unmarshal(result.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.TotalEntries != 0 || response.UnreadEntries != 0 || response.VideoEntries != 0 || response.RSSEntries != 0 || response.YouTubeEntries != 0 || response.SourceCounts == nil {
		t.Fatalf("zero response = %+v, body = %s", response, result.Body.String())
	}
}

func TestReaderCountsClassifiesVideoSourceIDPrefixesLikeEntryList(t *testing.T) {
	db = openTestDB(t)
	for _, fixture := range []struct {
		id       string
		sourceID string
	}{
		{id: "prefix-youtube", sourceID: "youtube-channel"},
		{id: "prefix-bilibili", sourceID: "bilibili-channel"},
		{id: "prefix-b23", sourceID: "b23-channel"},
	} {
		seedReaderCountEntry(t, fixture.id, fixture.sourceID, "rss", "https://example.com/"+fixture.id)
	}
	aggregate, err := queryReaderAggregateCounts()
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.TotalEntries != 3 || aggregate.VideoEntries != 3 || aggregate.RSSEntries != 0 || aggregate.YouTubeEntries != 1 {
		t.Fatalf("prefix aggregate = %+v, want total=3 video=3 rss=0 youtube=1", aggregate)
	}
	sources, err := queryReaderSourceCounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 3 {
		t.Fatalf("prefix source counts = %+v, want 3 rows", sources)
	}
	for _, source := range sources {
		if source.TotalEntries != 1 || source.VideoEntries != 1 {
			t.Fatalf("source %q = %+v, want one video", source.SourceID, source)
		}
	}
}

func TestReaderCountsUsesCatalogProviderFormatForVideoGallery(t *testing.T) {
	db = openTestDB(t)
	seedReaderCountSource(t, "catalog-youtube-only")
	if _, err := db.Exec(`INSERT INTO feed_catalog_entries
(source_id,site_url,category,language,icon_text,icon_color,tags_json,feed_format,recommended,popularity,validation_status,validated_at,catalog_origin)
VALUES(?,?,?,?,?,?,?, ?,0,0,'verified',?, 'test')`,
		"catalog-youtube-only", "https://example.com/channel", "technology", "en", "Y", "#64748b", "[]", "youtube", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	seedReaderCountEntry(t, "catalog-provider-entry", "catalog-youtube-only", "rss", "https://example.com/watch/entry")

	aggregate, err := queryReaderAggregateCounts()
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.VideoEntries != 1 || aggregate.YouTubeEntries != 1 || aggregate.RSSEntries != 0 {
		t.Fatalf("catalog provider aggregate = %+v, want one YouTube video and no RSS row", aggregate)
	}
	sources, err := queryReaderSourceCounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].SourceID != "catalog-youtube-only" || sources[0].VideoEntries != 1 {
		t.Fatalf("catalog provider source counts = %+v, want one video source", sources)
	}
}

func TestReaderCountsKeepsGenericMediaOutOfProviderVideoGallery(t *testing.T) {
	db = openTestDB(t)
	seedReaderCountEntry(t, "explicit-view-type", "ordinary-source", "rss", "https://example.com/explicit-view-type")
	if _, err := db.Exec(`UPDATE news SET view_type='video' WHERE id=?`, "explicit-view-type"); err != nil {
		t.Fatal(err)
	}
	seedReaderCountEntry(t, "explicit-media-mime", "ordinary-source", "rss", "https://example.com/explicit-media-mime")
	if _, err := db.Exec(`UPDATE news SET media_mime='video/mp4' WHERE id=?`, "explicit-media-mime"); err != nil {
		t.Fatal(err)
	}
	seedReaderCountEntry(t, "plain-article", "ordinary-source", "rss", "https://example.com/plain-article")

	aggregate, err := queryReaderAggregateCounts()
	if err != nil {
		t.Fatal(err)
	}
	if aggregate.TotalEntries != 3 || aggregate.VideoEntries != 0 || aggregate.RSSEntries != 3 {
		t.Fatalf("generic media aggregate = %+v, want total=3 video=0 rss=3", aggregate)
	}
	sources, err := queryReaderSourceCounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].VideoEntries != 0 {
		t.Fatalf("generic media source counts = %+v, want one source with zero provider videos", sources)
	}
}

func TestSourceListCanSkipExpensivePerSourceCounts(t *testing.T) {
	db = openTestDB(t)
	seedReaderCountEntry(t, "source-count-entry", "openai-news", "", "https://example.com/source-count")
	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, source := range payload.Sources {
		if source.ID == "openai-news" {
			if source.EntryCount != 0 || source.UnreadCount != 0 {
				t.Fatalf("light source counts = %+v, want zero placeholders", source)
			}
			return
		}
	}
	t.Fatal("openai-news source missing")
}

func TestDashboardReadOptionsSkipNewsAndSources(t *testing.T) {
	db = openTestDB(t)
	payload := `{"schema_version":1,"generated_at":"2026-09-01T00:00:00Z","source":{"kind":"mixed","status":"ok"},"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[],"news":[{"id":"dashboard-news","title":"dashboard news","url":"https://example.com/dashboard-news","source":"test","published_at":"2026-09-01T00:00:00Z","summary_text":"summary","content_format":"text","safe_html":""}],"decisions":[]}`
	if result := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", payload); result.Code != http.StatusOK {
		t.Fatalf("dashboard write status = %d, body = %s", result.Code, result.Body.String())
	}
	result := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard?include_news=0&include_sources=0&include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("dashboard read status = %d, body = %s", result.Code, result.Body.String())
	}
	var dashboard struct {
		Dashboard
		NewsIncluded    bool `json:"news_included"`
		SourcesIncluded bool `json:"sources_included"`
		CountsIncluded  bool `json:"counts_included"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.News) != 0 {
		t.Fatalf("news = %+v, want empty", dashboard.News)
	}
	if dashboard.Sources != nil {
		t.Fatalf("sources = %+v, want omitted/nil", dashboard.Sources)
	}
	if dashboard.NewsIncluded || dashboard.SourcesIncluded || dashboard.CountsIncluded {
		t.Fatalf("projection flags = news:%v sources:%v counts:%v, want all false", dashboard.NewsIncluded, dashboard.SourcesIncluded, dashboard.CountsIncluded)
	}
	// include_counts=0 must not echo the stale persisted aggregate values.
	if dashboard.TotalEntries != 0 || dashboard.UnreadEntries != 0 || dashboard.VideoEntries != 0 {
		t.Fatalf("lightweight counts = total:%d unread:%d video:%d, want explicit zeroes", dashboard.TotalEntries, dashboard.UnreadEntries, dashboard.VideoEntries)
	}
}

func TestDashboardWithoutSnapshotHydratesLocalProjections(t *testing.T) {
	db = openTestDB(t)
	seedReaderCountSource(t, "cold-install-source")
	seedReaderCountEntry(t, "cold-install-entry", "cold-install-source", "rss", "https://example.com/cold-install")
	if _, err := db.Exec(`INSERT INTO actions(id,task,owner,due_at,source_ids,status,confirmation_status,priority) VALUES(?,?,?,?,?,?,?,?)`,
		"cold-action", "核对来源同步", "产品研究", "2026-09-15", `[]`, "draft", "draft", "high"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO signals(id,title,detail,source_ids,confidence,status) VALUES(?,?,?,?,?,?)`,
		"cold-signal", "来源已恢复", "本地目录已有内容", `[]`, "medium", "draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO decisions(id,question,options,recommendation,source_ids,assumptions,risks,status) VALUES(?,?,?,?,?,?,?,?)`,
		"cold-decision", "是否继续同步？", `["继续","暂停"]`, "继续", `[]`, `[]`, `[]`, "draft"); err != nil {
		t.Fatal(err)
	}

	result := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if result.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", result.Code, result.Body.String())
	}
	var dashboard Dashboard
	if err := json.Unmarshal(result.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if len(dashboard.Actions) != 1 || dashboard.Actions[0].ID != "cold-action" {
		t.Fatalf("fallback actions = %+v, want cold-action", dashboard.Actions)
	}
	if len(dashboard.Signals) != 1 || dashboard.Signals[0].ID != "cold-signal" {
		t.Fatalf("fallback signals = %+v, want cold-signal", dashboard.Signals)
	}
	if len(dashboard.Decisions) != 1 || dashboard.Decisions[0].ID != "cold-decision" {
		t.Fatalf("fallback decisions = %+v, want cold-decision", dashboard.Decisions)
	}
	if len(dashboard.Sources) == 0 || dashboard.Source.Status == "unavailable" || dashboard.TotalEntries != 1 {
		t.Fatalf("fallback source projection = status:%q sources:%d total:%d", dashboard.Source.Status, len(dashboard.Sources), dashboard.TotalEntries)
	}
}

func TestDashboardNewsUsesReaderFocusLinkVisibilityScope(t *testing.T) {
	db = openTestDB(t)
	previousScope := appDataScope
	appDataScope = dataScope{OwnerID: "dashboard-owner", ProfileID: "dashboard-profile"}
	t.Cleanup(func() { appDataScope = previousScope })

	seedReaderCountEntry(t, "dashboard-focus-visible", "focus-chat-links", "", "https://example.com/focus-visible")
	seedReaderCountEntry(t, "dashboard-focus-foreign", "focus-chat-links", "", "https://example.com/focus-foreign")
	seedReaderCountEntry(t, "dashboard-ordinary", "ordinary-source", "", "https://example.com/ordinary")
	if _, err := db.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"dashboard-owner", "dashboard-profile", "dashboard-focus-visible", "https://example.com/focus-visible", "hash-dashboard-focus-visible", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
		"another-owner", "another-profile", "dashboard-focus-foreign", "https://example.com/focus-foreign", "hash-dashboard-focus-foreign", "2026-09-01T00:00:00Z", "2026-09-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	result := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if result.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", result.Code, result.Body.String())
	}
	var dashboard Dashboard
	if err := json.Unmarshal(result.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	visible := make(map[string]bool, len(dashboard.News))
	for _, item := range dashboard.News {
		visible[item.ID] = true
	}
	if !visible["dashboard-focus-visible"] || !visible["dashboard-ordinary"] || visible["dashboard-focus-foreign"] {
		t.Fatalf("dashboard news visibility = %+v, want visible focus + ordinary and no foreign focus", visible)
	}
	if dashboard.TotalEntries != 2 {
		t.Fatalf("dashboard total_entries = %d, want 2", dashboard.TotalEntries)
	}
}

func TestDashboardReadOptionsKeepUnknownDirectQueryKeysCompatible(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/dashboard?legacy_cache=1", nil)
	options, err := parseDashboardReadOptions(request)
	if err != nil {
		t.Fatalf("unknown direct query key rejected: %v", err)
	}
	if !options.IncludeNews || !options.IncludeSources || !options.IncludeCounts {
		t.Fatalf("defaults changed by unknown query key: %+v", options)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/dashboard?include_counts=0", nil)
	options, err = parseDashboardReadOptions(request)
	if err != nil {
		t.Fatalf("include_counts=0 rejected: %v", err)
	}
	if options.IncludeCounts {
		t.Fatalf("include_counts=0 did not disable counts: %+v", options)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/dashboard?include_counts=0&include_counts=1", nil)
	if _, err = parseDashboardReadOptions(request); err == nil {
		t.Fatal("duplicate include_counts was accepted")
	}
}

func TestReaderCountsHandlerRejectsQueryAndMutation(t *testing.T) {
	db = openTestDB(t)
	query := callJSON(t, readerCountsHandler, http.MethodGet, "/api/reader/counts?unexpected=1", "")
	if query.Code != http.StatusBadRequest {
		t.Fatalf("unexpected query status = %d, body = %s", query.Code, query.Body.String())
	}
	mutation := callJSON(t, readerCountsHandler, http.MethodPost, "/api/reader/counts", "{}")
	if mutation.Code != http.StatusMethodNotAllowed {
		t.Fatalf("mutation status = %d, body = %s", mutation.Code, mutation.Body.String())
	}
}

func TestReaderCountsQueriesHonorCanceledContext(t *testing.T) {
	db = openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := queryReaderAggregateCountsWithContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("aggregate error = %v, want context.Canceled", err)
	}
	if _, _, err := queryReaderSourceCountsWithContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("source error = %v, want context.Canceled", err)
	}
}

func TestReaderCountsQueriesUseBoundedTimeoutAndSourceProjection(t *testing.T) {
	if readerCountsQueryTimeout <= 0 || readerCountsQueryTimeout > 5*time.Second {
		t.Fatalf("reader count timeout = %s, want (0, 5s]", readerCountsQueryTimeout)
	}
	if readerCountsMaxSourceRows <= 0 || readerCountsMaxSourceRows > 4096 {
		t.Fatalf("reader source row cap = %d, want a positive bounded cap", readerCountsMaxSourceRows)
	}

	db = openTestDB(t)
	for index := 0; index < readerCountsMaxSourceRows+3; index++ {
		id := fmt.Sprintf("bounded-count-%04d", index)
		seedReaderCountEntry(t, id, "source-"+id, "", "https://example.com/"+id)
	}

	ctx, cancel := context.WithTimeout(context.Background(), readerCountsQueryTimeout)
	defer cancel()
	sources, truncated, err := queryReaderSourceCountsWithContext(ctx)
	if err != nil {
		t.Fatalf("bounded source query failed: %v", err)
	}
	if !truncated {
		t.Fatal("source query did not report truncation past the hard cap")
	}
	if len(sources) != readerCountsMaxSourceRows {
		t.Fatalf("source rows = %d, want cap %d", len(sources), readerCountsMaxSourceRows)
	}
}

func TestReaderCountsQueryErrorMapsDeadlineToRetryableStatus(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/reader/counts", nil)
	response := httptest.NewRecorder()
	writeReaderCountsQueryError(response, request, context.DeadlineExceeded)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("deadline status = %d, body = %s; want %d", response.Code, response.Body.String(), http.StatusServiceUnavailable)
	}
}

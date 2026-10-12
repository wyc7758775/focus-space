package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestEntryWriteSourceHealthAndScopedReaderFlow(t *testing.T) {
	db = openTestDB(t)
	target := `{"source_id":"wipo-patentscope","source_type":"rss","external_id":"wipo-target","title":"target-outside-dashboard-window","url":"https://www.wipo.int/pressroom/en/articles/2026/article_target.html","source":"WIPO PATENTSCOPE","published_at":"2020-01-01T00:00:00Z","summary_text":"bounded fallback evidence","fetched_at":"2026-08-10T13:00:00Z","permission_scope":"public_read","source_level":"official_fallback","failure_reason":"primary source unavailable"}`
	for attempt := 0; attempt < 2; attempt++ {
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", target)
		if result.Code != http.StatusOK {
			t.Fatalf("target write %d status = %d, body = %s", attempt, result.Code, result.Body.String())
		}
	}

	var status, lastError string
	var recordsSeen, recordsUsed int
	if err := db.QueryRow(`SELECT status,records_seen,records_used,last_error FROM feed_sources WHERE id='wipo-patentscope'`).Scan(&status, &recordsSeen, &recordsUsed, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "partial" || recordsSeen != 1 || recordsUsed != 1 || lastError != "primary source unavailable" {
		t.Fatalf("agent source projection status=%s seen=%d used=%d error=%q", status, recordsSeen, recordsUsed, lastError)
	}

	for i := 0; i < 100; i++ {
		payload := fmt.Sprintf(`{"source_id":"github-blog","source_type":"rss","external_id":"recent-%03d","title":"recent-%03d","url":"https://github.blog/recent-%03d","source":"GitHub Blog","published_at":"2026-08-10T12:00:00Z","summary_text":"recent item"}`, i, i, i)
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("recent write %d status = %d, body = %s", i, result.Code, result.Body.String())
		}
	}

	dashboard := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", dashboard.Code, dashboard.Body.String())
	}
	if strings.Contains(dashboard.Body.String(), "target-outside-dashboard-window") {
		t.Fatal("target unexpectedly remained inside the bounded dashboard window")
	}
	scoped := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=wipo-patentscope&limit=5&cursor=0", "")
	if scoped.Code != http.StatusOK || !strings.Contains(scoped.Body.String(), "target-outside-dashboard-window") || !strings.Contains(scoped.Body.String(), `"source_id":"wipo-patentscope"`) {
		t.Fatalf("scoped entry status = %d, body = %s", scoped.Code, scoped.Body.String())
	}
}

func TestEntryListDefaultsToFiftyItemCursorPage(t *testing.T) {
	db = openTestDB(t)
	for i := 0; i < 55; i++ {
		payload := fmt.Sprintf(`{"source_id":"github-blog","source_type":"rss","external_id":"page-%03d","title":"page-%03d","url":"https://github.blog/page-%03d","published_at":"2026-08-10T12:%02d:00Z","summary_text":"paged item"}`, i, i, i, i%60)
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("entry write %d status = %d, body = %s", i, result.Code, result.Body.String())
		}
	}

	result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog", "")
	if result.Code != http.StatusOK {
		t.Fatalf("entry page status = %d, body = %s", result.Code, result.Body.String())
	}
	var page struct {
		Entries   []StandardEntry `json:"entries"`
		Next      string          `json:"next_cursor"`
		Token     string          `json:"next_cursor_token"`
		Total     int             `json:"total_entries"`
		PageLimit int             `json:"limit"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 50 || page.Next != "50" || page.Token == "" || page.Total != 55 || page.PageLimit != 50 {
		t.Fatalf("default page = entries %d, next %q, limit %d; want 50, 50, 50", len(page.Entries), page.Next, page.PageLimit)
	}
	// A keyset page remains stable when a newer row is inserted between reads.
	newer := callJSON(t, entryHandler, http.MethodPost, "/api/entries", `{"source_id":"github-blog","source_type":"rss","external_id":"page-new","title":"page-new","url":"https://github.blog/page-new","published_at":"2026-08-11T12:00:00Z","summary_text":"newer item"}`)
	if newer.Code != http.StatusOK {
		t.Fatalf("newer entry write status = %d, body = %s", newer.Code, newer.Body.String())
	}
	second := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog&limit=50&cursor="+page.Token, "")
	if second.Code != http.StatusOK {
		t.Fatalf("keyset page status = %d, body = %s", second.Code, second.Body.String())
	}
	var nextPage struct {
		Entries []StandardEntry `json:"entries"`
		Total   int             `json:"total_entries"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &nextPage); err != nil {
		t.Fatal(err)
	}
	if len(nextPage.Entries) != 5 || nextPage.Total != 56 {
		t.Fatalf("keyset page = entries %d total %d; want 5 and 56", len(nextPage.Entries), nextPage.Total)
	}
	for _, item := range nextPage.Entries {
		if item.ExternalID == "page-new" {
			t.Fatal("keyset continuation unexpectedly included a row newer than its boundary")
		}
	}
}

func TestEntryListKeysetPaginationKeepsIdenticalTimestampRowsFlow(t *testing.T) {
	db = openTestDB(t)
	for i := 0; i < 55; i++ {
		payload := fmt.Sprintf(`{"source_id":"github-blog","source_type":"rss","external_id":"tie-%03d","title":"tie-%03d","url":"https://github.blog/tie-%03d","published_at":"2026-08-10T12:00:00Z","summary_text":"tied page item"}`, i, i, i)
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("entry write %d status = %d, body = %s", i, result.Code, result.Body.String())
		}
	}
	if _, err := db.Exec(`UPDATE news SET updated_at='2026-08-10T13:00:00Z' WHERE source_id='github-blog'`); err != nil {
		t.Fatal(err)
	}

	seen := make(map[string]struct{}, 55)
	cursor := "0"
	for pageIndex := 0; pageIndex < 10; pageIndex++ {
		result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog&limit=20&cursor="+cursor, "")
		if result.Code != http.StatusOK {
			t.Fatalf("page %d status = %d, body = %s", pageIndex, result.Code, result.Body.String())
		}
		var page struct {
			Entries []StandardEntry `json:"entries"`
			Token   string          `json:"next_cursor_token"`
			Total   int             `json:"total_entries"`
		}
		if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 55 {
			t.Fatalf("page %d total = %d, want 55", pageIndex, page.Total)
		}
		for _, entry := range page.Entries {
			if _, duplicate := seen[entry.ID]; duplicate {
				t.Fatalf("entry %q repeated on page %d", entry.ID, pageIndex)
			}
			seen[entry.ID] = struct{}{}
		}
		if page.Token == "" {
			if len(seen) != 55 {
				t.Fatalf("keyset walk returned %d unique tied rows, want 55", len(seen))
			}
			return
		}
		cursor = page.Token
	}
	t.Fatal("keyset pagination did not terminate within 10 pages")
}

func TestEntryListVideoFilterIncludesOnlyYouTubeAndBilibiliFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT OR IGNORE INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES('bilibili-test','bilibili','Bilibili 测试','https://www.bilibili.com/video/','',0,'ok',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, sourceID := range []string{"youtube-prefix", "bilibili-prefix", "b23-prefix"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,'',0,'ok',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, sourceID, "rss", sourceID, "https://example.com/"+sourceID); err != nil {
			t.Fatalf("seed source %s: %v", sourceID, err)
		}
	}
	fixtures := []string{
		`{"source_id":"youtube-anthropic","source_type":"youtube","view_type":"video","external_id":"yt-1","title":"YouTube video","url":"https://www.youtube.com/watch?v=abcdefghijk","published_at":"2026-08-10T12:03:00Z","summary_text":"video"}`,
		`{"source_id":"bilibili-test","source_type":"bilibili","view_type":"video","external_id":"bili-1","title":"Bilibili video","url":"https://www.bilibili.com/video/BV1xx411c7mD","published_at":"2026-08-10T12:02:00Z","summary_text":"video"}`,
		`{"source_id":"youtube-prefix","source_type":"rss","view_type":"article","external_id":"yt-prefix-1","title":"YouTube prefix video","url":"https://example.com/yt-prefix-1","published_at":"2026-08-10T12:01:30Z","summary_text":"video"}`,
		`{"source_id":"bilibili-prefix","source_type":"rss","view_type":"article","external_id":"bili-prefix-1","title":"Bilibili prefix video","url":"https://example.com/bili-prefix-1","published_at":"2026-08-10T12:01:20Z","summary_text":"video"}`,
		`{"source_id":"b23-prefix","source_type":"rss","view_type":"article","external_id":"b23-prefix-1","title":"B23 prefix video","url":"https://example.com/b23-prefix-1","published_at":"2026-08-10T12:01:10Z","summary_text":"video"}`,
		`{"source_id":"github-blog","source_type":"rss","view_type":"article","external_id":"media-1","title":"Media attachment","url":"https://example.com/post","media_url":"https://cdn.example.com/video.mp4","media_mime":"video/mp4","published_at":"2026-08-10T12:01:00Z","summary_text":"video"}`,
		`{"source_id":"github-blog","source_type":"rss","view_type":"article","external_id":"article-1","title":"Plain article","url":"https://example.com/article","published_at":"2026-08-10T12:00:00Z","summary_text":"article"}`,
	}
	for index, payload := range fixtures {
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("fixture %d write status = %d, body = %s", index, result.Code, result.Body.String())
		}
	}
	result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?view_type=video&limit=50", "")
	if result.Code != http.StatusOK {
		t.Fatalf("video filter status = %d, body = %s", result.Code, result.Body.String())
	}
	var page struct {
		Entries []StandardEntry `json:"entries"`
		Total   int             `json:"total_entries"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 5 || page.Total != 5 {
		t.Fatalf("video filter returned %d entries / total %d, want 5 / 5", len(page.Entries), page.Total)
	}
	for _, entry := range page.Entries {
		if entry.ExternalID == "article-1" || entry.ExternalID == "media-1" {
			t.Fatal("non-YouTube/Bilibili content leaked into video filter")
		}
	}

	youtubeResult := callJSON(t, entryHandler, http.MethodGet, "/api/entries?view_type=youtube&limit=50", "")
	if youtubeResult.Code != http.StatusOK {
		t.Fatalf("youtube filter status = %d, body = %s", youtubeResult.Code, youtubeResult.Body.String())
	}
	var youtubePage struct {
		Entries []StandardEntry `json:"entries"`
		Total   int             `json:"total_entries"`
	}
	if err := json.Unmarshal(youtubeResult.Body.Bytes(), &youtubePage); err != nil {
		t.Fatal(err)
	}
	if len(youtubePage.Entries) != 2 || youtubePage.Total != 2 {
		t.Fatalf("youtube filter returned %+v / total %d, want yt-1 and yt-prefix-1 / 2", youtubePage.Entries, youtubePage.Total)
	}

	rssResult := callJSON(t, entryHandler, http.MethodGet, "/api/entries?view_type=rss&limit=50", "")
	if rssResult.Code != http.StatusOK {
		t.Fatalf("rss filter status = %d, body = %s", rssResult.Code, rssResult.Body.String())
	}
	var rssPage struct {
		Entries []StandardEntry `json:"entries"`
		Total   int             `json:"total_entries"`
	}
	if err := json.Unmarshal(rssResult.Body.Bytes(), &rssPage); err != nil {
		t.Fatal(err)
	}
	if len(rssPage.Entries) != 2 || rssPage.Total != 2 {
		t.Fatalf("rss filter returned %d entries / total %d, want 2 / 2", len(rssPage.Entries), rssPage.Total)
	}
	for _, entry := range rssPage.Entries {
		if entry.ExternalID == "yt-prefix-1" || entry.ExternalID == "bili-prefix-1" || entry.ExternalID == "b23-prefix-1" {
			t.Fatalf("video prefix leaked into rss filter: %+v", entry)
		}
	}
}

func TestEntryListRSSFilterPaginatesWithoutVideoRows(t *testing.T) {
	db = openTestDB(t)
	for i := 0; i < 60; i++ {
		seedReaderCountEntry(t, fmt.Sprintf("rss-dense-%03d", i), "rss-dense", "rss", fmt.Sprintf("https://example.com/rss-dense/%03d", i))
		seedReaderCountEntry(t, fmt.Sprintf("video-dense-%03d", i), "youtube-dense", "rss", fmt.Sprintf("https://example.com/video-dense/%03d", i))
	}

	seen := make(map[string]struct{}, 60)
	cursor := "0"
	for pageIndex := 0; pageIndex < 10; pageIndex++ {
		path := "/api/entries?view_type=rss&limit=10&cursor=" + cursor
		result := callJSON(t, entryHandler, http.MethodGet, path, "")
		if result.Code != http.StatusOK {
			t.Fatalf("rss page %d status = %d, body = %s", pageIndex, result.Code, result.Body.String())
		}
		var page struct {
			Entries []StandardEntry `json:"entries"`
			Token   string          `json:"next_cursor_token"`
			Total   int             `json:"total_entries"`
		}
		if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Total != 60 {
			t.Fatalf("rss page %d total = %d, want 60", pageIndex, page.Total)
		}
		for _, entry := range page.Entries {
			if strings.HasPrefix(entry.ExternalID, "video-dense-") {
				t.Fatalf("video row leaked into rss page %d: %+v", pageIndex, entry)
			}
			if _, duplicate := seen[entry.ID]; duplicate {
				t.Fatalf("rss row %q repeated on page %d", entry.ID, pageIndex)
			}
			seen[entry.ID] = struct{}{}
		}
		if page.Token == "" {
			break
		}
		cursor = page.Token
	}
	if len(seen) != 60 {
		t.Fatalf("rss pagination returned %d unique rows, want 60", len(seen))
	}
}

func TestAppSettingsRoundTripAndValidationUnit(t *testing.T) {
	db = openTestDB(t)

	get := callJSON(t, settingsHandler, http.MethodGet, "/api/settings", "")
	if get.Code != http.StatusOK {
		t.Fatalf("default settings status = %d, body = %s", get.Code, get.Body.String())
	}
	var defaults AppSettings
	if err := json.Unmarshal(get.Body.Bytes(), &defaults); err != nil {
		t.Fatal(err)
	}
	if !defaults.BriefEnabled || defaults.BriefHour != 8 || defaults.BriefMinute != 0 || defaults.Timezone != "device" || !defaults.AutoExtractRecordings || !defaults.AutoGenerateArticleOverviews {
		t.Fatalf("unexpected defaults: %#v", defaults)
	}
	if !defaults.ObsidianEnabled {
		t.Fatalf("Obsidian source should be enabled by default: %#v", defaults)
	}

	patch := callJSON(t, settingsHandler, http.MethodPatch, "/api/settings", `{"brief_hour":7,"brief_minute":30,"brief_weekdays":[0,2,4],"auto_generate_article_overviews":false,"obsidian_enabled":false,"notify_urgent":false}`)
	if patch.Code != http.StatusOK {
		t.Fatalf("settings patch status = %d, body = %s", patch.Code, patch.Body.String())
	}
	var updated AppSettings
	if err := json.Unmarshal(patch.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.BriefHour != 7 || updated.BriefMinute != 30 || len(updated.BriefWeekdays) != 3 || updated.AutoGenerateArticleOverviews || updated.ObsidianEnabled || updated.NotifyUrgent {
		t.Fatalf("settings patch not applied: %#v", updated)
	}

	invalid := callJSON(t, settingsHandler, http.MethodPatch, "/api/settings", `{"brief_hour":24}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, body = %s", invalid.Code, invalid.Body.String())
	}
	unknown := callJSON(t, settingsHandler, http.MethodPatch, "/api/settings", `{"unexpected":true}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown settings status = %d, body = %s", unknown.Code, unknown.Body.String())
	}
}

func TestCapabilityContractListsClosedOperationNames(t *testing.T) {
	db = openTestDB(t)
	result := callJSON(t, capabilityHandler, http.MethodGet, "/api/capabilities", "")
	if result.Code != http.StatusOK {
		t.Fatalf("capability status = %d, body = %s", result.Code, result.Body.String())
	}
	body := result.Body.String()
	for _, expected := range []string{
		`"recording.action_link"`, `"recording.action_unlink"`,
		`"folder.create"`, `"folder.update"`, `"folder.delete"`,
		`"tag.create"`, `"tag.update"`, `"tag.delete"`,
		`"reader_digest.read"`, `"reader_digest.apply"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("capability contract is missing %s", expected)
		}
	}
	for _, forbidden := range []string{`"recording.action"`, `"folder.write"`, `"tag.write"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("capability contract exposes non-closed name %s", forbidden)
		}
	}
}

func TestMorningBriefValidationRejectsUnboundedOrUnknownStatus(t *testing.T) {
	db = openTestDB(t)
	base := `{"schema_version":1,"generated_at":"2026-08-08T08:00:00Z","source":{"kind":"mixed","status":"ok"},"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[],"news":[],"decisions":[]}`
	for _, suffix := range []string{
		`,"morning_brief":{"id":"b1","title":"CEO Morning Brief","generated_at":"2026-08-08T08:00:00Z","summary":"ok","status":"unknown"}}`,
		`,"morning_brief":{"id":"b1","title":"CEO Morning Brief","generated_at":"2026-08-08T08:00:00Z","summary":"","status":"ready"}}`,
	} {
		payload := strings.TrimSuffix(base, "}") + suffix
		result := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", payload)
		if result.Code != http.StatusBadRequest {
			t.Fatalf("invalid morning brief status = %d, body = %s", result.Code, result.Body.String())
		}
	}
}

func TestEntryCollectionIdempotencyConflictAndRecoveryFlow(t *testing.T) {
	db = openTestDB(t)

	request := `{"source_id":"github-blog","source_type":"rss","external_id":"article-1","title":"第一条","url":"https://example.com/article-1","source":"Example","summary_text":"摘要","content_format":"text","permission_scope":"public_read","source_level":"official","failure_reason":"","confidence":"high","evidence":["recording:r-1"]}`
	first := callJSON(t, entryHandler, http.MethodPost, "/api/entries", request)
	if first.Code != http.StatusOK {
		t.Fatalf("first entry status = %d, body = %s", first.Code, first.Body.String())
	}
	var entry StandardEntry
	if err := json.Unmarshal(first.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Revision != 1 || entry.ExternalID != "article-1" || entry.Confidence != "high" || entry.PermissionScope != "public_read" || entry.SourceLevel != "official" || entry.FailureReason != "" {
		t.Fatalf("unexpected first entry: %#v", entry)
	}

	repeat := callJSON(t, entryHandler, http.MethodPost, "/api/entries", request)
	if repeat.Code != http.StatusOK {
		t.Fatalf("repeat entry status = %d, body = %s", repeat.Code, repeat.Body.String())
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='github-blog' AND external_id='article-1'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("idempotency created %d rows", rows)
	}

	list := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog&external_id=article-1&full=1&limit=1", "")
	if list.Code != http.StatusOK || !containsJSONField(list.Body.String(), "safe_html") || !strings.Contains(list.Body.String(), `"permission_scope":"public_read"`) || !strings.Contains(list.Body.String(), `"source_level":"official"`) {
		t.Fatalf("entry list status = %d, body = %s", list.Code, list.Body.String())
	}

	state := callJSON(t, entryHandler, http.MethodPatch, "/api/entries", `{"id":"`+entry.ID+`","expected_revision":1,"starred":true,"read_later":true}`)
	if state.Code != http.StatusOK {
		t.Fatalf("entry state status = %d, body = %s", state.Code, state.Body.String())
	}
	var stateEntry StandardEntry
	if err := json.Unmarshal(state.Body.Bytes(), &stateEntry); err != nil {
		t.Fatal(err)
	}
	if !stateEntry.Starred || !stateEntry.ReadLater || stateEntry.Revision != 1 {
		t.Fatalf("state update unexpectedly changed entry: %#v", stateEntry)
	}

	conflict := callJSON(t, entryHandler, http.MethodPost, "/api/entries", `{"source_id":"github-blog","source_type":"rss","external_id":"article-1","title":"过期修改","expected_revision":99}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	var conflictState string
	if err := db.QueryRow(`SELECT conflict_state FROM news WHERE id=?`, entry.ID).Scan(&conflictState); err != nil {
		t.Fatal(err)
	}
	if conflictState != "needs_review" {
		t.Fatalf("conflict state = %q", conflictState)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM entry_conflicts WHERE source_id='github-blog' AND external_id='article-1'`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("conflict evidence rows = %d", rows)
	}

	deleteResult := callJSON(t, entryHandler, http.MethodPatch, "/api/entries", `{"id":"`+entry.ID+`","expected_revision":1,"deleted":true}`)
	if deleteResult.Code != http.StatusOK {
		t.Fatalf("soft delete status = %d, body = %s", deleteResult.Code, deleteResult.Body.String())
	}
	active := callJSON(t, entryHandler, http.MethodGet, "/api/entries?external_id=article-1", "")
	if active.Code != http.StatusOK || strings.Contains(active.Body.String(), `"external_id":"article-1"`) {
		t.Fatalf("soft-deleted entry still active: %s", active.Body.String())
	}
	recoverResult := callJSON(t, entryHandler, http.MethodPatch, "/api/entries", `{"id":"`+entry.ID+`","deleted":false}`)
	if recoverResult.Code != http.StatusOK {
		t.Fatalf("recover status = %d, body = %s", recoverResult.Code, recoverResult.Body.String())
	}
	active = callJSON(t, entryHandler, http.MethodGet, "/api/entries?external_id=article-1", "")
	if active.Code != http.StatusOK || !strings.Contains(active.Body.String(), `"external_id":"article-1"`) {
		t.Fatalf("recovered entry not active: %s", active.Body.String())
	}
	dismiss := callJSON(t, newsPreferenceHandler, http.MethodDelete, "/api/news", `{"id":"`+entry.ID+`"}`)
	if dismiss.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d, body = %s", dismiss.Code, dismiss.Body.String())
	}
	dismissedPage := callJSON(t, entryHandler, http.MethodGet, "/api/entries?external_id=article-1", "")
	if dismissedPage.Code != http.StatusOK || strings.Contains(dismissedPage.Body.String(), `"external_id":"article-1"`) {
		t.Fatalf("dismissed entry returned from typed page: %s", dismissedPage.Body.String())
	}
}

func containsJSONField(body, field string) bool {
	var value map[string]any
	if json.Unmarshal([]byte(body), &value) == nil {
		if _, ok := value[field]; ok {
			return true
		}
		if entries, ok := value["entries"].([]any); ok {
			for _, raw := range entries {
				if item, ok := raw.(map[string]any); ok {
					if _, found := item[field]; found {
						return true
					}
				}
			}
		}
	}
	return false
}

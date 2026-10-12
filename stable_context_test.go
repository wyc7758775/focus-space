package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"focus-space/internal/bridges"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func useStableContextScope(t *testing.T, owner, profile string) {
	t.Helper()
	previous := appDataScope
	appDataScope = dataScope{OwnerID: owner, ProfileID: profile}
	t.Cleanup(func() { appDataScope = previous })
}

func TestResolveDataScopeUsesHostIdentityOrAppLocalFallback(t *testing.T) {
	t.Run("legacy host uses app-local scope", func(t *testing.T) {
		t.Setenv("ZAPP_OWNER_ID", "")
		t.Setenv("ZAPP_PROFILE_ID", "")
		got, err := resolveDataScope()
		if err != nil {
			t.Fatal(err)
		}
		if got.OwnerID != "app-local" || got.ProfileID != "app-local" {
			t.Fatalf("scope = %+v", got)
		}
	})

	t.Run("missing owner", func(t *testing.T) {
		t.Setenv("ZAPP_OWNER_ID", "")
		t.Setenv("ZAPP_PROFILE_ID", "profile-a")
		if _, err := resolveDataScope(); err == nil {
			t.Fatal("missing owner scope was accepted")
		}
	})

	t.Run("missing profile", func(t *testing.T) {
		t.Setenv("ZAPP_OWNER_ID", "agent-a")
		t.Setenv("ZAPP_PROFILE_ID", "")
		if _, err := resolveDataScope(); err == nil {
			t.Fatal("missing profile scope was accepted")
		}
	})

	t.Run("host identity is preserved", func(t *testing.T) {
		t.Setenv("ZAPP_OWNER_ID", "agent:owner-a")
		t.Setenv("ZAPP_PROFILE_ID", "owner-a")
		got, err := resolveDataScope()
		if err != nil {
			t.Fatal(err)
		}
		if got.OwnerID != "agent:owner-a" || got.ProfileID != "owner-a" {
			t.Fatalf("scope = %+v", got)
		}
	})

	t.Run("malformed identity is rejected", func(t *testing.T) {
		t.Setenv("ZAPP_OWNER_ID", "agent/owner-a")
		t.Setenv("ZAPP_PROFILE_ID", "owner-a")
		if _, err := resolveDataScope(); err == nil {
			t.Fatal("malformed owner scope was accepted")
		}
	})
}

func insertTestNews(t *testing.T, conn *sql.DB, id, title, publishedAt, summary string) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO news(id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,updated_at)
VALUES(?,?,?,?,?,'test','rss','article','',?,?,'text','','','','',0,?)`,
		id, title, "https://example.com/"+id, "测试来源", "test-source", publishedAt, summary, publishedAt)
	if err != nil {
		t.Fatal(err)
	}
}

func TestStableContextRequiresConfirmationAndScopesRetrieval(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-a", "profile-a")
	insertTestNews(t, db, "N-CONTEXT", "季度路线图", "2026-08-07T08:00:00Z", "确认桌面端交付范围")

	unconfirmed := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"news:N-CONTEXT","added_by":"context_action"}`)
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed status = %d, body = %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	confirmed := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"news:N-CONTEXT","added_by":"context_action","confirmed":true}`)
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"state":"active"`) {
		t.Fatalf("confirmed status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}

	result := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=路线图&top_k=1&max_tokens=128", "")
	for _, expected := range []string{
		`"item_key":"news:N-CONTEXT"`,
		`"content_is_untrusted":true`,
		`"original_url":"https://example.com/N-CONTEXT"`,
		`"token_budget":128`,
	} {
		if !strings.Contains(result.Body.String(), expected) {
			t.Fatalf("retrieval missing %s: %s", expected, result.Body.String())
		}
	}

	appDataScope = dataScope{OwnerID: "owner-b", ProfileID: "profile-b"}
	isolated := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=路线图", "")
	if strings.Contains(isolated.Body.String(), "N-CONTEXT") {
		t.Fatalf("cross-scope context leaked: %s", isolated.Body.String())
	}
	appDataScope = dataScope{OwnerID: "owner-a", ProfileID: "profile-a"}

	removed := callJSON(t, stableContextHandler, http.MethodDelete, "/api/context", `{"item_key":"news:N-CONTEXT"}`)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"state":"removed"`) {
		t.Fatalf("remove status = %d, body = %s", removed.Code, removed.Body.String())
	}
	afterRemove := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=路线图", "")
	if strings.Contains(afterRemove.Body.String(), "N-CONTEXT") {
		t.Fatalf("removed context was still retrieved: %s", afterRemove.Body.String())
	}
	membership := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?membership=1", "")
	if !strings.Contains(membership.Body.String(), `"state":"removed"`) {
		t.Fatalf("soft-removed membership was not recoverable: %s", membership.Body.String())
	}
}

func TestLiveObsidianItemKeyValidationUnit(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		want     string
		ok       bool
		validKey bool
	}{
		{name: "nested markdown note", key: "evidence:live-obsidian:Projects/路线图.md", want: "Projects/路线图.md", ok: true, validKey: true},
		{name: "plain text note", key: "evidence:live-obsidian:Projects/路线图.txt", want: "Projects/路线图.txt", ok: true, validKey: true},
		{name: "pdf asset", key: "evidence:live-obsidian:Projects/简报.pdf", want: "Projects/简报.pdf", ok: true, validKey: true},
		{name: "image asset", key: "evidence:live-obsidian:Projects/封面.webp", want: "Projects/封面.webp", ok: true, validKey: true},
		{name: "unsupported file", key: "evidence:live-obsidian:Projects/archive.zip", validKey: false},
		{name: "traversal", key: "evidence:live-obsidian:../secret.md", validKey: false},
		{name: "absolute path", key: "evidence:live-obsidian:/secret.md", validKey: false},
		{name: "ordinary evidence", key: "evidence:stable-id", validKey: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, ok := liveObsidianPathFromItemKey(tt.key)
			if ok != tt.ok || path != tt.want || validStableItemKey(tt.key) != tt.validKey {
				t.Fatalf("key=%q path=%q ok=%v, want path=%q ok=%v valid=%v", tt.key, path, ok, tt.want, tt.ok, tt.validKey)
			}
		})
	}
}

func TestLiveObsidianStableContextFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-obsidian", "profile-obsidian")
	deviceAvailable := true
	readRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "obsidian-token" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/device-data/v1/obsidian/read" || r.URL.Query().Get("path") != "Projects/路线图.md" {
			http.NotFound(w, r)
			return
		}
		readRequests++
		if !deviceAvailable {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"path":    "Projects/路线图.md",
			"content": "# 季度路线图\n\n确认桌面端交付范围。",
		})
	}))
	defer server.Close()
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", server.URL+"/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "obsidian-token")

	key := "evidence:live-obsidian:Projects/路线图.md"
	accepted := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"evidence:live-obsidian:Projects/路线图.md","added_by":"context_action","confirmed":true}`)
	if accepted.Code != http.StatusOK || !strings.Contains(accepted.Body.String(), `"state":"active"`) {
		t.Fatalf("accept status = %d, body = %s", accepted.Code, accepted.Body.String())
	}
	if readRequests != 1 {
		t.Fatalf("device read count = %d, want 1", readRequests)
	}
	var stored Evidence
	if err := db.QueryRow(`SELECT id, title, locator, source_type, content, content_format FROM evidence WHERE id = 'live-obsidian:Projects/路线图.md'`).Scan(
		&stored.ID, &stored.Title, &stored.Locator, &stored.SourceType, &stored.Content, &stored.ContentFormat,
	); err != nil {
		t.Fatal(err)
	}
	if stored.SourceType != "obsidian" || stored.ContentFormat != "markdown" || !strings.Contains(stored.Content, "交付范围") {
		t.Fatalf("materialized evidence = %+v", stored)
	}

	search := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=交付范围&source_type=obsidian", "")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"item_key":"`+key+`"`) || !strings.Contains(search.Body.String(), `"source_name":"Obsidian"`) {
		t.Fatalf("search status = %d, body = %s", search.Code, search.Body.String())
	}

	removed := callJSON(t, stableContextHandler, http.MethodDelete, "/api/context", `{"item_key":"evidence:live-obsidian:Projects/路线图.md"}`)
	if removed.Code != http.StatusOK || !strings.Contains(removed.Body.String(), `"state":"removed"`) {
		t.Fatalf("remove status = %d, body = %s", removed.Code, removed.Body.String())
	}
	deviceAvailable = false
	restored := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"evidence:live-obsidian:Projects/路线图.md","added_by":"context_action","confirmed":true}`)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"state":"active"`) {
		t.Fatalf("cached restore status = %d, body = %s", restored.Code, restored.Body.String())
	}
	if readRequests != 2 {
		t.Fatalf("cached restore should make one bounded retry, reads = %d", readRequests)
	}

	pdf := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"evidence:live-obsidian:Projects/确认简报.pdf","added_by":"context_action","confirmed":true}`)
	if pdf.Code != http.StatusOK || !strings.Contains(pdf.Body.String(), `"content_format":"text"`) {
		t.Fatalf("pdf accept status = %d, body = %s", pdf.Code, pdf.Body.String())
	}
	var pdfFormat, pdfContent string
	if err := db.QueryRow(`SELECT content_format, content FROM evidence WHERE id = 'live-obsidian:Projects/确认简报.pdf'`).Scan(&pdfFormat, &pdfContent); err != nil {
		t.Fatal(err)
	}
	if pdfFormat != "pdf" || pdfContent != "" || readRequests != 2 {
		t.Fatalf("pdf evidence format=%q content=%q reads=%d", pdfFormat, pdfContent, readRequests)
	}
}

func TestSignalCounterevidenceEntersStableContextAfterConfirmationFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-signal", "profile-signal")
	payload := `{"schema_version":1,"generated_at":"2026-08-10T12:00:00Z","source":{"kind":"mixed","status":"ok","records_seen":1,"records_used":1},"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[{"id":"SIG-CONTEXT","title":"单点市场变化","detail":"一个来源观察到变化","source_ids":["feed-1/entry-1"],"confidence":"low","counterevidence":["样本量仍有限","尚无第二来源"],"status":"partial"}],"news":[],"decisions":[]}`
	write := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", payload)
	if write.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", write.Code, write.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT counterevidence FROM signals WHERE id='SIG-CONTEXT'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `["样本量仍有限","尚无第二来源"]` {
		t.Fatalf("counterevidence was not persisted: %s", stored)
	}
	read := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"counterevidence":["样本量仍有限","尚无第二来源"]`) {
		t.Fatalf("counterevidence was not returned: %s", read.Body.String())
	}

	accepted := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"signal:SIG-CONTEXT","added_by":"context_action","confirmed":true}`)
	if accepted.Code != http.StatusOK {
		t.Fatalf("context status = %d, body = %s", accepted.Code, accepted.Body.String())
	}
	var snapshot string
	if err := db.QueryRow(`SELECT content_snapshot FROM stable_context_items WHERE owner_id=? AND profile_id=? AND item_key='signal:SIG-CONTEXT'`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot, "样本量仍有限") || !strings.Contains(snapshot, "尚无第二来源") {
		t.Fatalf("accepted context lost counterevidence: %s", snapshot)
	}
}

func TestStableContextSearchIsBoundedAndFindsOlderMatches(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-search", "profile-search")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 220; i++ {
		title := fmt.Sprintf("普通内容 %03d", i)
		content := strings.Repeat("普通摘要 ", 80)
		if i == 0 {
			title = "needle 历史决策"
			content = strings.Repeat("needle 相关证据 ", 80)
		}
		updated := fmt.Sprintf("2026-08-07T%02d:%02d:%02dZ", (i/3600)%24, (i/60)%60, i%60)
		_, err = tx.Exec(`
INSERT INTO stable_context_items(owner_id,profile_id,item_key,state,added_by,added_at,removed_at,updated_at,content_hash,source_type,source_id,source_name,original_url,title,published_at,content_format,content_snapshot)
VALUES(?,?,?,'active','context_action',?,'',?,?,?,?,?,?,?,?, 'text',?)`,
			appDataScope.OwnerID, appDataScope.ProfileID, fmt.Sprintf("news:N-%03d", i), updated,
			updated, fmt.Sprintf("hash-%03d", i), "rss", "source", "测试来源", "https://example.com/item", title, updated, content)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	result := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=needle&top_k=5&max_tokens=128", "")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"item_key":"news:N-000"`) {
		t.Fatalf("older relevant context was not retrieved: %s", result.Body.String())
	}
	var payload struct {
		Items           []StableContextItem `json:"items"`
		EstimatedTokens int                 `json:"estimated_tokens"`
		TokenBudget     int                 `json:"token_budget"`
	}
	if err := jsonDecode(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) > 5 || payload.EstimatedTokens > payload.TokenBudget || payload.TokenBudget != 128 {
		t.Fatalf("retrieval was not bounded: %+v", payload)
	}
}

func TestFavoriteDoesNotEnterStableContextAndStillControlsRetention(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-retention", "profile-retention")
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,status,retention_days,retention_limit,created_at,updated_at)
		VALUES('test-source','rss','测试来源','https://example.com/test-source.xml','ok',10000,500,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	insertTestNews(t, db, "N-OLD", "需要长期保留", "2000-01-01T00:00:00Z", "旧收藏的证据正文")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("N-RECENT-%03d", i)
		published := fmt.Sprintf("2026-08-07T%02d:%02d:%02dZ", (i/3600)%24, (i/60)%60, i%60)
		_, err = tx.Exec(`
INSERT INTO news(id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,updated_at)
VALUES(?,?,?,?,?,'test','rss','article','',?,?,'text','','','','',0,?)`,
			id, id, "https://example.com/"+id, "测试来源", "test-source", published, "近期摘要", published)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	star := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"news:N-OLD","starred":true}`)
	if star.Code != http.StatusOK {
		t.Fatalf("star status = %d, body = %s", star.Code, star.Body.String())
	}
	prunePreferences()
	var oldRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE id = 'N-OLD'`).Scan(&oldRows); err != nil || oldRows != 1 {
		t.Fatalf("active favorite was pruned: rows=%d err=%v", oldRows, err)
	}

	unstar := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"news:N-OLD","starred":false}`)
	if unstar.Code != http.StatusOK {
		t.Fatalf("unstar status = %d, body = %s", unstar.Code, unstar.Body.String())
	}
	pruneSourceNews("test-source")
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE id = 'N-OLD'`).Scan(&oldRows); err != nil || oldRows != 0 {
		t.Fatalf("removed favorite stayed in stream retention: rows=%d err=%v", oldRows, err)
	}
	var contexts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE owner_id = ? AND profile_id = ? AND item_key = 'news:N-OLD'`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&contexts); err != nil {
		t.Fatal(err)
	}
	if contexts != 0 {
		t.Fatalf("favorite unexpectedly entered stable context: rows=%d", contexts)
	}
}

func TestPreferencePruningPreservesPerSourceHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,status,retention_days,retention_limit,created_at,updated_at)
		VALUES('history-source','rss','历史来源','https://example.com/history.xml','ok',5000,1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 600; index++ {
		id := fmt.Sprintf("HISTORY-%03d", index)
		published := time.Date(2026, time.August, 20, 0, 0, index, 0, time.UTC).Format(time.RFC3339)
		if _, err := tx.Exec(`INSERT INTO news(id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,updated_at)
			VALUES(?,?,?,?,?,'test','rss','article','',?,?,'text','','','','',0,?)`, id, id, "https://example.com/history/"+id, "历史来源", "history-source", published, "历史摘要", published); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	prunePreferences()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='history-source'`).Scan(&count); err != nil || count != 600 {
		t.Fatalf("preference pruning erased source history: count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET retention_limit=550 WHERE id='history-source'`); err != nil {
		t.Fatal(err)
	}
	pruneSourceNews("history-source")
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='history-source'`).Scan(&count); err != nil || count != 550 {
		t.Fatalf("source retention was not bounded: count=%d err=%v", count, err)
	}
}

func TestRecordingFavoriteDoesNotEnterStableContext(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-recording", "profile-recording")
	_, err := db.Exec(`
INSERT INTO recordings(record_id,title,status,duration_seconds,started_at,created_at,updated_at,speaker_count,audio_ref,transcript,transcript_truncated,segments_json,segments_truncated,speaker_names_json,summary,per_speaker_summary,report,action_ids_json)
VALUES('R-1','产品周会','done',600,'2026-08-07T08:00:00Z','','2026-08-07T09:00:00Z',2,'_zettlab/recordings/R-1/audio','确认发布范围',0,'[]',0,'{}','形成两个行动','','证据报告','[]')`)
	if err != nil {
		t.Fatal(err)
	}
	result := callJSON(t, recordingPreferenceHandler, http.MethodPatch, "/api/recordings/preferences", `{"recording_id":"R-1","starred":true}`)
	if result.Code != http.StatusOK {
		t.Fatalf("recording favorite status = %d, body = %s", result.Code, result.Body.String())
	}
	var contexts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE owner_id = ? AND profile_id = ? AND item_key = 'recording:R-1'`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&contexts); err != nil {
		t.Fatal(err)
	}
	if contexts != 0 {
		t.Fatalf("recording favorite unexpectedly entered stable context: rows=%d", contexts)
	}
}

func TestStableContextSearchSplitsChineseAndMixedQueries(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-terms", "profile-terms")
	insertTestNews(t, db, "N-CJK", "季度路线图", "2026-08-07T08:00:00Z", "桌面端交付范围与负责人")
	confirmed := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"news:N-CJK","added_by":"context_action","confirmed":true}`)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	for _, query := range []string{"请查季度路线图", "桌面端负责人", "quarterly 路线图"} {
		result := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q="+url.QueryEscape(query), "")
		if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"item_key":"news:N-CJK"`) {
			t.Fatalf("query %q missed context: %s", query, result.Body.String())
		}
	}
}

func TestManualStableContextSurvivesFavoriteRemoval(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-manual", "profile-manual")
	insertTestNews(t, db, "N-MANUAL", "手动保留的证据", "2026-08-07T00:00:00Z", "手动加入的上下文内容")
	added := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"news:N-MANUAL","added_by":"context_action","confirmed":true}`)
	if added.Code != http.StatusOK {
		t.Fatalf("manual context status = %d, body = %s", added.Code, added.Body.String())
	}
	star := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"news:N-MANUAL","starred":true}`)
	if star.Code != http.StatusOK {
		t.Fatalf("favorite status = %d, body = %s", star.Code, star.Body.String())
	}
	unstar := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"news:N-MANUAL","starred":false}`)
	if unstar.Code != http.StatusOK {
		t.Fatalf("unfavorite status = %d, body = %s", unstar.Code, unstar.Body.String())
	}
	var state, addedBy string
	if err := db.QueryRow(`SELECT state, added_by FROM stable_context_items WHERE owner_id = ? AND profile_id = ? AND item_key = 'news:N-MANUAL'`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&state, &addedBy); err != nil {
		t.Fatal(err)
	}
	if state != contextStateActive || addedBy != contextAddedByAction {
		t.Fatalf("manual context was removed or relabeled: state=%s added_by=%s", state, addedBy)
	}
}

func TestStableContextMigrationIsAdditiveAndDoesNotBackfill(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/migration.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON; CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(appFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil || version > 11 {
			continue
		}
		body, err := appFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := conn.Exec(`INSERT INTO schema_version(version, applied_at) VALUES(?, '2026-08-07T00:00:00Z')`, version); err != nil {
			t.Fatal(err)
		}
	}
	insertTestNews(t, conn, "N-HISTORICAL", "历史收藏", "2026-08-06T00:00:00Z", "不自动迁入")
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:N-HISTORICAL',0,1,0,0,'2026-08-06T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(conn); err != nil {
		t.Fatal(err)
	}
	var version, contexts, historical, counterevidenceColumns int
	if err := conn.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM stable_context_items`).Scan(&contexts); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE id = 'N-HISTORICAL'`).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('signals') WHERE name = 'counterevidence'`).Scan(&counterevidenceColumns); err != nil {
		t.Fatal(err)
	}
	var payloadFingerprintColumns int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('reader_focus_runs') WHERE name = 'payload_fingerprint'`).Scan(&payloadFingerprintColumns); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err := conn.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	var operationalMode string
	if err := conn.QueryRow(`SELECT collection_mode FROM feed_sources WHERE id='sec-xbrl-filings'`).Scan(&operationalMode); err != nil {
		t.Fatal(err)
	}
	var mutationTable string
	if err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='app_data_mutation_requests'`).Scan(&mutationTable); err != nil {
		t.Fatal(err)
	}
	var focusTables int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('reader_focus_topics','reader_focus_policy','reader_focus_runs')`).Scan(&focusTables); err != nil {
		t.Fatal(err)
	}
	var focusSpaceTables int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('focus_spaces','focus_analysis_runs','focus_evidence_interpretations')`).Scan(&focusSpaceTables); err != nil {
		t.Fatal(err)
	}
	// 期望版本随迁移链动态推导（此前硬编码 105，链每次增长都会假红）
	wantVersion := 0
	for _, name := range names {
		if v, err := migrationVersion(name); err == nil && v > wantVersion {
			wantVersion = v
		}
	}
	if version != wantVersion || focusTables != 3 || focusSpaceTables != 3 || contexts != 0 || historical != 1 || counterevidenceColumns != 1 || payloadFingerprintColumns != 1 || integrity != "ok" || operationalMode != "agent" || mutationTable != "app_data_mutation_requests" {
		t.Fatalf("migration result version=%d contexts=%d historical=%d counterevidence_columns=%d payload_fingerprint_columns=%d integrity=%s", version, contexts, historical, counterevidenceColumns, payloadFingerprintColumns, integrity)
	}
}

func TestInvalidCNIPAFallbackMigrationPreservesUserRetainedItemsFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "085_")
	now := "2026-08-21T00:00:00Z"
	for _, row := range []struct {
		id, sourceID, origin, sourceLevel string
	}{
		{id: "cnipa-remove", sourceID: "cnipa-patent-data", origin: "app-fetch", sourceLevel: "official_fallback"},
		{id: "cnipa-starred", sourceID: "cnipa-patent-data", origin: "app-fetch", sourceLevel: "official_fallback"},
		{id: "cnipa-read-later", sourceID: "cnipa-patent-data", origin: "app-fetch", sourceLevel: "official_fallback"},
		{id: "cnipa-context", sourceID: "cnipa-patent-data", origin: "app-fetch", sourceLevel: "official_fallback"},
		{id: "cnipa-official", sourceID: "cnipa-patent-data", origin: "app-fetch", sourceLevel: "official"},
		{id: "other-fallback", sourceID: "wipo-patentscope", origin: "app-fetch", sourceLevel: "official_fallback"},
	} {
		if _, err := conn.Exec(`INSERT INTO news(id,title,url,source,source_id,origin,source_type,published_at,summary_text,content_format,safe_html,source_level,updated_at) VALUES(?,?,?,?,?,?,?,?,'','text','',?,?)`, row.id, row.id, "https://example.com/"+row.id, row.sourceID, row.sourceID, row.origin, "web", now, row.sourceLevel, now); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES
('news:cnipa-starred',0,1,0,0,?),
('news:cnipa-read-later',0,0,1,0,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO stable_context_items(owner_id,profile_id,item_key,state,added_by,added_at,updated_at,content_hash) VALUES('owner','profile','news:cnipa-context','active','manual_confirm',?,?, 'hash')`, now, now); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "085_remove_invalid_cnipa_fallback.sql")
	for id, wantDeleted := range map[string]bool{
		"cnipa-remove":     true,
		"cnipa-starred":    false,
		"cnipa-read-later": false,
		"cnipa-context":    false,
		"cnipa-official":   false,
		"other-fallback":   false,
	} {
		var deletedAt string
		if err := conn.QueryRow(`SELECT deleted_at FROM news WHERE id=?`, id).Scan(&deletedAt); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if gotDeleted := deletedAt != ""; gotDeleted != wantDeleted {
			t.Fatalf("id=%s deleted=%v want=%v", id, gotDeleted, wantDeleted)
		}
	}
}

func TestOfficialHistoryRepairMigrationPreservesRetainedItemsFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "087_")
	now := "2026-08-21T00:00:00Z"
	if _, err := conn.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES
('epo-legacy','EP4791183NWA1','EPO legacy','https://data.epo.org/publication-server/rest/v1.2/patents/EP4791183NWA1/document.html','EPO','epo-publication-server','app-fetch','api','20260819','','text','','epo-hash',?),
('sec-legacy','0000094845-26-000041:8-K','SEC legacy','https://www.sec.gov/Archives/edgar/data/94845/000009484526000041/000009484526000041-index.html','SEC','sec-xbrl-filings','app-fetch','api','2026-08-20T00:00:00Z','','text','','sec-hash',?),
('sec-authored','0000094845-26-000042','SEC authored','https://example.com/authored','SEC','sec-xbrl-filings','agent','api','2026-08-20T00:00:00Z','','text','','authored-hash',?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at)
VALUES('news:epo-legacy',0,1,0,0,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO stable_context_items(owner_id,profile_id,item_key,state,added_by,added_at,updated_at,content_hash)
VALUES('owner','profile','news:sec-legacy','active','manual_confirm',?,?, 'context-hash')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_sources SET etag='stale',last_modified='stale',backfill_cursor='stale',last_sync_at=?,next_sync_at=?,last_error='old error'
WHERE id IN ('epo-publication-server','sec-xbrl-filings')`, now, now); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "087_repair_official_history_windows.sql")

	var epoPublished, secURL, authoredURL string
	if err := conn.QueryRow(`SELECT published_at FROM news WHERE id='epo-legacy'`).Scan(&epoPublished); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT url FROM news WHERE id='sec-legacy'`).Scan(&secURL); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT url FROM news WHERE id='sec-authored'`).Scan(&authoredURL); err != nil {
		t.Fatal(err)
	}
	var protected, unclearedSources, foreignKeyErrors int
	if err := conn.QueryRow(`SELECT
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:epo-legacy' AND starred=1) +
(SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:sec-legacy' AND state='active')`).Scan(&protected); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM feed_sources
WHERE id IN ('epo-publication-server','sec-xbrl-filings')
  AND (etag<>'' OR last_modified<>'' OR backfill_cursor<>'' OR last_sync_at<>'' OR next_sync_at<>'' OR last_error<>'')`).Scan(&unclearedSources); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	wantSECURL := "https://www.sec.gov/Archives/edgar/data/94845/000009484526000041/0000094845-26-000041-index.html"
	if epoPublished != "2026-08-19T00:00:00Z" || secURL != wantSECURL || authoredURL != "https://example.com/authored" || protected != 2 || unclearedSources != 0 || foreignKeyErrors != 0 {
		t.Fatalf("epo=%q sec=%q authored=%q protected=%d uncleared=%d fk=%d", epoPublished, secURL, authoredURL, protected, unclearedSources, foreignKeyErrors)
	}
}

func TestOfficialHistoryRepairMigrationRepairsAllCachedSECLinksFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "087_")
	tx, err := conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 1000; index++ {
		cik := fmt.Sprintf("%010d", index)
		accession := fmt.Sprintf("%s-26-%06d", cik, index)
		cleanAccession := strings.ReplaceAll(accession, "-", "")
		wrongURL := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%d/%s/%s-index.html", index, cleanAccession, cleanAccession)
		if _, err := tx.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES(?,?,?,?,?,'sec-xbrl-filings','app-fetch','api','2026-08-20T00:00:00Z','','text','',?,?)`,
			fmt.Sprintf("sec-cache-%04d", index), accession+":8-K", accession, wrongURL, "SEC", "hash-"+accession, "2026-08-21T00:00:00Z"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "087_repair_official_history_windows.sql")
	rows, err := conn.Query(`SELECT external_id,url FROM news WHERE source_id='sec-xbrl-filings' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	repaired := 0
	for rows.Next() {
		var externalID, rawURL string
		if err := rows.Scan(&externalID, &rawURL); err != nil {
			t.Fatal(err)
		}
		accession := strings.SplitN(externalID, ":", 2)[0]
		cleanCIK := strings.TrimLeft(accession[:10], "0")
		want := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s-index.html", cleanCIK, strings.ReplaceAll(accession, "-", ""), accession)
		if rawURL != want {
			t.Fatalf("external_id=%q url=%q want=%q", externalID, rawURL, want)
		}
		repaired++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if repaired != 1000 {
		t.Fatalf("repaired=%d want=1000", repaired)
	}
	var integrity string
	var foreignKeyErrors int
	if err := conn.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" || foreignKeyErrors != 0 {
		t.Fatalf("integrity=%q foreign_key_errors=%d", integrity, foreignKeyErrors)
	}
}

func TestOperationalSourceMigrationSeedsAppCollectedSubscriptions(t *testing.T) {
	db = openTestDB(t)
	localFeeds := map[string]string{
		"hkex-regulatory-announcements": "https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK",
		"arxiv-ai":                      "https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending",
	}
	gatewaySources := map[string]bool{"elon-musk-x": true, "tibo-sottiaux-x": true}
	sourceIDs := []string{
		"sec-xbrl-filings", "hkex-regulatory-announcements", "cninfo-disclosures", "sse-periodic-reports",
		"govinfo-federal-register", "eurlex-legislation", "china-policy-library", "arxiv-ai",
		"crossref-research", "epo-publication-server", "cnipa-patent-data", "wipo-patentscope",
		"elon-musk-x", "donald-trump-x", "tibo-sottiaux-x",
	}
	for _, sourceID := range sourceIDs {
		var mode, sourceURL, folderID, origin string
		var enabled, isDefault, hidden int
		err := db.QueryRow(`
	SELECT f.collection_mode, f.url, f.is_default, f.hidden,
	       s.enabled, s.folder_id, c.catalog_origin
	FROM feed_sources f
	JOIN feed_subscriptions s ON s.source_id=f.id
	JOIN feed_catalog_entries c ON c.source_id=f.id
	WHERE f.id=?`, sourceID).Scan(&mode, &sourceURL, &isDefault, &hidden, &enabled, &folderID, &origin)
		if err != nil {
			t.Fatalf("source %s: %v", sourceID, err)
		}
		wantMode := "agent"
		wantURL := "source-retired://" + sourceID
		wantDefault, wantHidden := 0, 1
		if localURL, ok := localFeeds[sourceID]; ok {
			wantMode, wantURL = "app", localURL
			wantDefault, wantHidden = 1, 0
		} else if gatewaySources[sourceID] {
			wantMode, wantURL = "source_gateway", "source-gateway://"+sourceID
			wantDefault, wantHidden = 1, 0
		}
		if mode != wantMode || sourceURL != wantURL || isDefault != wantDefault || hidden != wantHidden || enabled != 1 || folderID == "" || (origin != "zettlab-maintained-official-work-sources-v1" && origin != "zettlab-maintained-public-aggregator-v1" && origin != "zettlab-maintained-x-rss-mirrors-v1" && origin != "zettlab-maintained-local-feed" && origin != "zettlab-source-gateway-v4") {
			t.Fatalf("source %s mode=%s url=%s default=%d hidden=%d enabled=%d folder=%s origin=%s", sourceID, mode, sourceURL, isDefault, hidden, enabled, folderID, origin)
		}
	}
	var provenanceColumns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('news') WHERE name IN ('permission_scope','source_level','failure_reason')`).Scan(&provenanceColumns); err != nil {
		t.Fatal(err)
	}
	if provenanceColumns != 3 {
		t.Fatalf("provenance columns = %d", provenanceColumns)
	}
}

func TestCuratedRecommendationMigrationPreservesExistingSubscriptionFlow(t *testing.T) {
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/catalog-migration.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON; CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(appFS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil || version > 16 {
			continue
		}
		body, err := appFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
		if _, err := conn.Exec(`INSERT INTO schema_version(version, applied_at) VALUES(?, '2026-08-08T00:00:00Z')`, version); err != nil {
			t.Fatal(err)
		}
	}

	const feedURL = "https://blog.playstation.com/feed/"
	if _, err := conn.Exec(`
INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at)
VALUES('source-existing','rss','Existing PlayStation',?,'用户先前添加',0,'ok','2026-08-08T00:00:00Z','2026-08-08T00:00:00Z')`, feedURL); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
INSERT INTO feed_catalog_entries(source_id,category,recommended,catalog_origin)
VALUES('source-existing','other',0,'app-public-fetch')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`
INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at)
VALUES('source-existing',1,'2026-08-08T00:00:00Z','2026-08-08T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(conn); err != nil {
		t.Fatal(err)
	}
	var sourceID, category, origin string
	var recommended, enabled, duplicateURLs int
	if err := conn.QueryRow(`
SELECT f.id,c.category,c.recommended,c.catalog_origin,s.enabled
FROM feed_sources f
JOIN feed_catalog_entries c ON c.source_id=f.id
JOIN feed_subscriptions s ON s.source_id=f.id
WHERE f.url=?`, feedURL).Scan(&sourceID, &category, &recommended, &origin, &enabled); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE url=?`, feedURL).Scan(&duplicateURLs); err != nil {
		t.Fatal(err)
	}
	if sourceID != "source-existing" || category != "game" || recommended != 1 || origin != "zettlab-maintained-public-rss-v2" || enabled != 1 || duplicateURLs != 1 {
		t.Fatalf("existing source changed unexpectedly: id=%s category=%s recommended=%d origin=%s enabled=%d urls=%d", sourceID, category, recommended, origin, enabled, duplicateURLs)
	}
}

func TestStableContextSurvivesDatabaseReopenFlow(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
	if err := os.MkdirAll("data", 0o755); err != nil {
		t.Fatal(err)
	}
	useStableContextScope(t, "owner-reopen", "profile-reopen")

	open := func() {
		var openErr error
		db, openErr = openDB()
		if openErr != nil {
			t.Fatal(openErr)
		}
		if openErr = migrate(db); openErr != nil {
			t.Fatal(openErr)
		}
	}
	open()
	insertTestNews(t, db, "N-REOPEN", "跨重启内容", "2026-08-07T08:00:00Z", "重启后仍可检索")
	star := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"news:N-REOPEN","starred":true}`)
	if star.Code != http.StatusOK {
		t.Fatalf("star status = %d, body = %s", star.Code, star.Body.String())
	}
	confirmed := callJSON(t, stableContextHandler, http.MethodPost, "/api/context", `{"item_key":"news:N-REOPEN","added_by":"context_action","confirmed":true}`)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("context status = %d, body = %s", confirmed.Code, confirmed.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	open()
	t.Cleanup(func() { _ = db.Close() })
	result := callJSON(t, stableContextHandler, http.MethodGet, "/api/context?q=跨重启", "")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "N-REOPEN") {
		t.Fatalf("stable context did not survive reopen: %s", result.Body.String())
	}
}

func jsonDecode(body []byte, target any) error {
	return json.NewDecoder(bytes.NewReader(body)).Decode(target)
}

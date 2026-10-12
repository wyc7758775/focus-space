package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/app.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedMigrationNames(t) {
		applyMigrationFile(t, conn, name)
	}
	// Match the production startup path: older/intermediate migration sets may
	// have recorded the version without materializing every gateway column.
	// Keeping the repair in the fixture prevents tests from depending on a
	// schema shape that the running application would immediately reconcile.
	if err := reconcileSourceGatewaySchema(conn); err != nil {
		t.Fatalf("reconcile source gateway schema: %v", err)
	}
	return conn
}

func sortedMigrationNames(t *testing.T) []string {
	t.Helper()
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
	return names
}

func applyMigrationFile(t *testing.T, conn *sql.DB, name string) {
	t.Helper()
	body, readErr := appFS.ReadFile("migrations/" + name)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if _, execErr := conn.Exec(string(body)); execErr != nil {
		t.Fatalf("apply %s: %v", name, execErr)
	}
}

// enableLegacyPublicCollectorFixture keeps parser/history tests independent
// from the production ownership policy. Migration 092 routes every maintained
// public default through Source Gateway; tests that exercise the retained
// parser implementation must opt into an explicit local fixture.
func enableLegacyPublicCollectorFixture(t *testing.T, sourceIDs ...string) {
	t.Helper()
	for _, sourceID := range sourceIDs {
		rawURL := ""
		if spec, ok := operationalSourceSpecs[sourceID]; ok && len(spec.endpoints) > 0 {
			rawURL = spec.endpoints[0]
		}
		if rawURL == "" {
			for candidate, policy := range weChatVerifiedDirectFeedPolicies {
				if policy.SourceID == sourceID {
					rawURL = candidate
					break
				}
			}
		}
		if rawURL == "" {
			for _, source := range defaultFeedSources {
				if source.ID == sourceID {
					rawURL = source.URL
					break
				}
			}
		}
		if rawURL == "" {
			t.Fatalf("no legacy collector fixture URL for %s", sourceID)
		}
		if _, err := db.Exec(`UPDATE feed_sources
SET collection_mode='app',url=?,hidden=0,auto_extract=1,gateway_status='',gateway_cursor='',gateway_has_more=0,next_sync_at=''
WHERE id=?`, rawURL, sourceID); err != nil {
			t.Fatalf("enable legacy collector fixture %s: %v", sourceID, err)
		}
	}
}

func syncLegacyPublicCollectorFixture(ctx context.Context, sourceID string) error {
	source, err := feedSourceByID(sourceID)
	if err != nil {
		return err
	}
	return syncOperationalSourceFixtureWithinBudget(ctx, ctx, source)
}

// openUpgradeTestDB replays only the migrations before the given filename
// prefix (for example "074_"), simulating a device database that predates the
// migration under test.  The caller seeds old-shape rows and then applies the
// new migration explicitly via applyMigrationFile.
func openUpgradeTestDB(t *testing.T, pendingPrefix string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+t.TempDir()+"/app.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedMigrationNames(t) {
		if name >= pendingPrefix {
			break
		}
		applyMigrationFile(t, conn, name)
	}
	return conn
}

func callJSON(t *testing.T, handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	result := httptest.NewRecorder()
	handler(result, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
	return result
}

func staticContains(source, fragment string) bool {
	return strings.Contains(strings.Join(strings.Fields(source), ""), strings.Join(strings.Fields(fragment), ""))
}

// indexPageCorpus 返回页面语料：index.html 与从中外置的 static/js/focus-app.js 拼接，
// 与抽取前的内联形态等价；前端文件重组时只需更新此处。
func indexPageCorpus(t *testing.T) []byte {
	t.Helper()
	index, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	inline, err := appFS.ReadFile("static/js/focus-app.js")
	if err != nil {
		t.Fatal(err)
	}
	return append(index, inline...)
}

func TestDashboardFlowPersistsAndReadsStructuredSnapshot(t *testing.T) {
	db = openTestDB(t)
	payload := `{
  "schema_version": 1,
  "generated_at": "2026-08-06T10:00:00Z",
  "source": {"kind":"profile_meetings","status":"ok","records_seen":1,"records_used":1,"last_read_at":"2026-08-06T10:00:00Z","message":"","inputs":[{"id":"meetings","kind":"meeting","label":"当前会议","status":"ok","records_seen":1,"records_used":1,"last_read_at":"2026-08-06T10:00:00Z","message":""},{"id":"feed-tech","kind":"rss","label":"技术源","status":"partial","records_seen":3,"records_used":2,"last_read_at":"2026-08-06T09:00:00Z","message":"一条源暂不可用"}]},
  "evidence": [{"id":"S1","title":"周会","locator":"00:01:20","observed_at":"2026-08-06T09:00:00Z","quote":"确认下周交付"}],
  "actions": [{"id":"A1","task":"确认交付范围","owner":"unconfirmed","due_at":"","source_ids":["S1"],"status":"draft","confirmation_status":"draft","priority":"medium"}],
  "metrics": [], "projects": [],
  "signals": [{"id":"SG1","title":"行业新闻","detail":"一条可隐藏的新闻","source_ids":["S1"],"confidence":"high","status":"new"}],
  "morning_brief": {"id":"brief-2026-08-06","title":"CEO Morning Brief","generated_at":"2026-08-06T08:00:00Z","summary":"今天优先推进交付确认。","priorities":["确认交付范围"],"risks":["来源部分不可用"],"source_ids":["S1"],"status":"partial"},
  "news": [{"id":"N1","title":"RSS 更新","url":"https://example.com/news/1","source":"示例源","source_type":"rss","view_type":"video","author":"编辑部","published_at":"2026-08-06T09:30:00Z","summary_text":"纯文本降级摘要","content_format":"html","safe_html":"<p><strong>安全摘要</strong></p>","thumbnail_url":"https://example.com/thumb.jpg","media_url":"https://example.com/video.mp4","media_mime":"video/mp4","duration_seconds":90}],
  "decisions": []
}`

	post := httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewBufferString(payload))
	postResult := httptest.NewRecorder()
	dashboardHandler(postResult, post)
	if postResult.Code != http.StatusOK {
		t.Fatalf("POST status = %d, body = %s", postResult.Code, postResult.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/dashboard", nil)
	getResult := httptest.NewRecorder()
	dashboardHandler(getResult, get)
	if getResult.Code != http.StatusOK || !strings.Contains(getResult.Body.String(), `"task":"确认交付范围"`) || !strings.Contains(getResult.Body.String(), `"id":"feed-tech"`) || !strings.Contains(getResult.Body.String(), `"view_type":"video"`) || !strings.Contains(getResult.Body.String(), `"CEO Morning Brief"`) {
		t.Fatalf("GET status = %d, body = %s", getResult.Code, getResult.Body.String())
	}

	itemPatch := httptest.NewRecorder()
	itemPreferenceHandler(itemPatch, httptest.NewRequest(http.MethodPatch, "/api/items", bytes.NewBufferString(`{"key":"news:N1","read":true,"starred":true}`)))
	if itemPatch.Code != http.StatusOK {
		t.Fatalf("PATCH item status = %d, body = %s", itemPatch.Code, itemPatch.Body.String())
	}
	readerPatch := httptest.NewRecorder()
	readerPreferenceHandler(readerPatch, httptest.NewRequest(http.MethodPatch, "/api/reader", bytes.NewBufferString(`{"sidebar_width":280,"timeline_width":520}`)))
	if readerPatch.Code != http.StatusOK {
		t.Fatalf("PATCH reader status = %d, body = %s", readerPatch.Code, readerPatch.Body.String())
	}
	itemsResult := httptest.NewRecorder()
	itemPreferenceHandler(itemsResult, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if !strings.Contains(itemsResult.Body.String(), `"key":"news:N1"`) || !strings.Contains(itemsResult.Body.String(), `"starred":true`) {
		t.Fatalf("item preference not readable: %s", itemsResult.Body.String())
	}
	readerResult := httptest.NewRecorder()
	readerPreferenceHandler(readerResult, httptest.NewRequest(http.MethodGet, "/api/reader", nil))
	if !strings.Contains(readerResult.Body.String(), `"sidebar_width":280`) || !strings.Contains(readerResult.Body.String(), `"timeline_width":520`) {
		t.Fatalf("reader preference not readable: %s", readerResult.Body.String())
	}
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs`).Scan(&runs); err != nil || runs != 1 {
		t.Fatalf("dashboard_runs = %d, err = %v", runs, err)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/actions", bytes.NewBufferString(`{"id":"A1","completed":true}`))
	patchResult := httptest.NewRecorder()
	actionPreferenceHandler(patchResult, patch)
	if patchResult.Code != http.StatusOK {
		t.Fatalf("PATCH action status = %d, body = %s", patchResult.Code, patchResult.Body.String())
	}

	dismiss := httptest.NewRequest(http.MethodDelete, "/api/signals", bytes.NewBufferString(`{"id":"SG1"}`))
	dismissResult := httptest.NewRecorder()
	signalPreferenceHandler(dismissResult, dismiss)
	if dismissResult.Code != http.StatusOK {
		t.Fatalf("DELETE signal status = %d, body = %s", dismissResult.Code, dismissResult.Body.String())
	}

	dismissNews := httptest.NewRequest(http.MethodDelete, "/api/news", bytes.NewBufferString(`{"id":"N1"}`))
	dismissNewsResult := httptest.NewRecorder()
	newsPreferenceHandler(dismissNewsResult, dismissNews)
	if dismissNewsResult.Code != http.StatusOK {
		t.Fatalf("DELETE news status = %d, body = %s", dismissNewsResult.Code, dismissNewsResult.Body.String())
	}

	hideModule := httptest.NewRequest(http.MethodPatch, "/api/modules", bytes.NewBufferString(`{"id":"news","hidden":true}`))
	hideModuleResult := httptest.NewRecorder()
	modulePreferenceHandler(hideModuleResult, hideModule)
	if hideModuleResult.Code != http.StatusOK {
		t.Fatalf("PATCH module status = %d, body = %s", hideModuleResult.Code, hideModuleResult.Body.String())
	}

	resizeModule := httptest.NewRequest(http.MethodPatch, "/api/layout", bytes.NewBufferString(`{"id":"news","height_px":420,"span":3}`))
	resizeModuleResult := httptest.NewRecorder()
	moduleLayoutHandler(resizeModuleResult, resizeModule)
	if resizeModuleResult.Code != http.StatusOK {
		t.Fatalf("PATCH layout status = %d, body = %s", resizeModuleResult.Code, resizeModuleResult.Body.String())
	}

	getResult = httptest.NewRecorder()
	dashboardHandler(getResult, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if !strings.Contains(getResult.Body.String(), `"status":"completed"`) || strings.Contains(getResult.Body.String(), `"id":"SG1"`) || strings.Contains(getResult.Body.String(), `"id":"N1"`) || !strings.Contains(getResult.Body.String(), `"hidden_modules":["news"]`) || !strings.Contains(getResult.Body.String(), `"news":{"height_px":420,"span":3}`) {
		t.Fatalf("preferences not applied: %s", getResult.Body.String())
	}

	refreshResult := httptest.NewRecorder()
	dashboardHandler(refreshResult, httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewBufferString(payload)))
	if refreshResult.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshResult.Code, refreshResult.Body.String())
	}
	getResult = httptest.NewRecorder()
	dashboardHandler(getResult, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if !strings.Contains(getResult.Body.String(), `"status":"completed"`) || strings.Contains(getResult.Body.String(), `"id":"SG1"`) || strings.Contains(getResult.Body.String(), `"id":"N1"`) || !strings.Contains(getResult.Body.String(), `"hidden_modules":["news"]`) || !strings.Contains(getResult.Body.String(), `"news":{"height_px":420,"span":3}`) {
		t.Fatalf("preferences lost after refresh: %s", getResult.Body.String())
	}

	deleteAction := httptest.NewRequest(http.MethodDelete, "/api/actions", bytes.NewBufferString(`{"id":"A1"}`))
	deleteResult := httptest.NewRecorder()
	actionPreferenceHandler(deleteResult, deleteAction)
	if deleteResult.Code != http.StatusOK {
		t.Fatalf("DELETE action status = %d, body = %s", deleteResult.Code, deleteResult.Body.String())
	}
	getResult = httptest.NewRecorder()
	dashboardHandler(getResult, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if strings.Contains(getResult.Body.String(), `"id":"A1"`) {
		t.Fatalf("deleted action still visible: %s", getResult.Body.String())
	}
}

func TestDashboardRejectsUnknownOrTrailingJSON(t *testing.T) {
	for _, payload := range []string{
		`{"schema_version":1,"generated_at":"x","source":{"kind":"profile_meetings","status":"ok"},"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[],"decisions":[],"html":"<script>"}`,
		`{"schema_version":1,"generated_at":"x","source":{"kind":"profile_meetings","status":"ok"},"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[],"decisions":[]} {}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewBufferString(payload))
		result := httptest.NewRecorder()
		dashboardHandler(result, req)
		if result.Code != http.StatusBadRequest {
			t.Fatalf("payload %q status = %d", payload, result.Code)
		}
	}
}

func TestSignalCounterevidenceValidationUnit(t *testing.T) {
	valid := Signal{
		ID:              "SIG-BOUNDED",
		Title:           "有边界的信号",
		Detail:          "单一来源只能形成低置信度信号",
		SourceIDs:       []string{"feed-1/entry-1"},
		Confidence:      "low",
		Counterevidence: []string{"尚无第二来源"},
		Status:          "partial",
	}
	if err := validateSignals([]Signal{valid}); err != nil {
		t.Fatalf("valid counterevidence rejected: %v", err)
	}

	tooMany := valid
	tooMany.ID = "SIG-TOO-MANY"
	tooMany.Counterevidence = make([]string, 11)
	for index := range tooMany.Counterevidence {
		tooMany.Counterevidence[index] = "缺口"
	}
	if err := validateSignals([]Signal{tooMany}); err == nil {
		t.Fatal("unbounded counterevidence count was accepted")
	}

	tooLong := valid
	tooLong.ID = "SIG-TOO-LONG"
	tooLong.Counterevidence = []string{strings.Repeat("证", 401)}
	if err := validateSignals([]Signal{tooLong}); err == nil {
		t.Fatal("unbounded counterevidence text was accepted")
	}
}

func TestSignalCounterevidenceReaderDetailContract(t *testing.T) {
	body, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, expected := range []string{"entry.item?.counterevidence", "data-signal-counterevidence", "反向证据与缺口"} {
		if !staticContains(page, expected) {
			t.Fatalf("reader signal detail is missing %q", expected)
		}
	}
}

func TestObsidianEvidenceContentContract(t *testing.T) {
	valid := Evidence{
		ID: "note-1", Title: "项目笔记", Locator: "Projects/plan.md", SourceType: "obsidian",
		Content: "# 计划\n\n![[diagram.png]]", ContentFormat: "markdown",
	}
	if err := validateEvidence([]Evidence{valid}); err != nil {
		t.Fatalf("valid Obsidian evidence rejected: %v", err)
	}
	if err := validateEvidence([]Evidence{{ID: "note-text", Title: "文本", Locator: "Projects/notes.txt", SourceType: "obsidian", Content: "plain text", ContentFormat: "text"}}); err != nil {
		t.Fatalf("valid Obsidian text rejected: %v", err)
	}
	if err := validateEvidence([]Evidence{{ID: "note-pdf", Title: "简报", Locator: "Projects/brief.pdf", SourceType: "obsidian", ContentFormat: "pdf"}}); err != nil {
		t.Fatalf("valid Obsidian PDF rejected: %v", err)
	}
	if err := validateEvidence([]Evidence{{ID: "note-2", Title: "越界", Locator: "/etc/passwd", SourceType: "obsidian", Content: "# x", ContentFormat: "markdown"}}); err == nil {
		t.Fatal("absolute Obsidian locator accepted")
	}
	if err := validateEvidence([]Evidence{{ID: "note-2b", Title: "中间越界", Locator: "Projects/../secret.md", SourceType: "obsidian", Content: "# x", ContentFormat: "markdown"}}); err == nil {
		t.Fatal("nested parent Obsidian locator accepted")
	}
	if err := validateEvidence([]Evidence{{ID: "note-3", Title: "格式错误", Locator: "plan.md", SourceType: "obsidian", Content: "# x", ContentFormat: "html"}}); err == nil {
		t.Fatal("non-Markdown Obsidian content accepted")
	}
	page, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"renderObsidianMarkdown", "renderObsidianDetail", "renderObsidianPDF", "renderObsidianImage", "obsidianImageURL", "obsidian/asset", "!\\[\\[", "function toggleObsidianFolder", "renderTimeline();", "obsidianDetails.get(entry.locator)", `class="obsidian-tree-summary"`, `class="obsidian-tree-row obsidian-note-row`, `class="obsidian-folder-icon"`} {
		if !strings.Contains(string(page), fragment) {
			t.Fatalf("Obsidian renderer missing %q", fragment)
		}
	}
	styles, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{".obsidian-tree-row.selected::after", ".obsidian-folder-icon", ".obsidian-note-heading h2", `.detail-panel.obsidian-detail-panel`, ".obsidian-pdf-toolbar", ".obsidian-image-detail"} {
		if !strings.Contains(string(styles), fragment) {
			t.Fatalf("Obsidian prototype styling missing %q", fragment)
		}
	}
	toggleStart := strings.Index(string(page), "function toggleObsidianFolder")
	toggleEnd := -1
	if toggleStart >= 0 {
		toggleEnd = strings.Index(string(page)[toggleStart:], "window.__toggleReaderObsidianFolder")
	}
	if toggleStart < 0 || toggleEnd < 0 || !strings.Contains(string(page)[toggleStart:toggleStart+toggleEnd], "renderTimeline();") {
		t.Fatal("Obsidian folder toggle must rerender the middle tree")
	}
	for _, fragment := range []string{
		"obsidianLoadedFolders: new Set()",
		"obsidianFolderInflight: new Map()",
		"function loadLiveObsidianFolder(path, force = false)",
		`loadLiveObsidianFolder("", force)`,
		"await loadLiveObsidianFolder(normalized)",
	} {
		if !strings.Contains(string(page), fragment) {
			t.Fatalf("Obsidian lazy folder loader missing %q", fragment)
		}
	}
	if strings.Contains(string(page), "while (queue.length") {
		t.Fatal("Obsidian initial load must not recursively walk the complete vault")
	}
}

func TestObsidianRendererBridgeContract(t *testing.T) {
	pageBytes := indexPageCorpus(t)
	readerBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page, reader := string(pageBytes), string(readerBytes)
	if !strings.Contains(reader, "window.__renderObsidianMarkdown = renderObsidianMarkdown;") {
		t.Fatal("Obsidian Markdown renderer must be exported for the inline detail script")
	}
	for _, expected := range []string{
		`const renderObsidianEvidence = (raw, notePath) =>`,
		`typeof renderer === "function"`,
		`return renderer(raw, notePath);`,
		`catch (error)`,
		`esc(String(raw ?? "").slice(0, 32000))`,
		`renderObsidianEvidence(item.content, item.locator)`,
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("Obsidian detail renderer bridge is missing %q", expected)
		}
	}
	if strings.Contains(page, "renderObsidianMarkdown(item.content, item.locator)") {
		t.Fatal("inline detail script must not call the reader IIFE's private renderer")
	}
}

func TestDashboardRefreshKeepsOmittedProjectionItems(t *testing.T) {
	db = openTestDB(t)
	first := `{"schema_version":1,"generated_at":"2026-08-07T08:00:00Z","source":{"kind":"profile_meetings","status":"ok","records_seen":1,"records_used":1},"evidence":[{"id":"E-OLD","title":"旧证据","locator":"00:01","observed_at":"2026-08-07T07:00:00Z","quote":"保留"}],"actions":[{"id":"A-OLD","task":"旧行动","owner":"unconfirmed","due_at":"","source_ids":["E-OLD"],"status":"draft","confirmation_status":"draft","priority":"medium"}],"metrics":[],"projects":[],"signals":[],"news":[],"decisions":[]}`
	second := `{"schema_version":1,"generated_at":"2026-08-07T09:00:00Z","source":{"kind":"profile_meetings","status":"partial","records_seen":1,"records_used":1},"evidence":[],"actions":[{"id":"A-NEW","task":"新行动","owner":"unconfirmed","due_at":"","source_ids":[],"status":"draft","confirmation_status":"draft","priority":"high"}],"metrics":[],"projects":[],"signals":[],"news":[],"decisions":[]}`
	for _, payload := range []string{first, second} {
		result := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("dashboard refresh status = %d, body = %s", result.Code, result.Body.String())
		}
	}
	result := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	body := result.Body.String()
	if !strings.Contains(body, `"id":"A-OLD"`) || !strings.Contains(body, `"id":"A-NEW"`) || !strings.Contains(body, `"id":"E-OLD"`) {
		t.Fatalf("omitted projection items were lost: %s", body)
	}
	var actionRows, evidenceRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&actionRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM evidence`).Scan(&evidenceRows); err != nil {
		t.Fatal(err)
	}
	if actionRows != 2 || evidenceRows != 1 {
		t.Fatalf("refresh rewrote projection rows: actions=%d evidence=%d", actionRows, evidenceRows)
	}
}

func TestDashboardTotalEntriesUsesDatabaseCountBeyondNewsProjectionLimit(t *testing.T) {
	db = openTestDB(t)
	for i := 0; i < maxItems+1; i++ {
		id := fmt.Sprintf("news-%03d", i)
		publishedAt := fmt.Sprintf("2026-08-07T08:%02d:00Z", i%60)
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, "标题 "+id, "https://example.com/"+id, "测试来源", publishedAt, "摘要", "text", "", publishedAt); err != nil {
			t.Fatal(err)
		}
	}

	result := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if result.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, body = %s", result.Code, result.Body.String())
	}
	var dashboard Dashboard
	if err := json.Unmarshal(result.Body.Bytes(), &dashboard); err != nil {
		t.Fatal(err)
	}
	if dashboard.TotalEntries != maxItems+1 {
		t.Fatalf("total_entries = %d, want %d", dashboard.TotalEntries, maxItems+1)
	}
	if len(dashboard.News) != maxItems {
		t.Fatalf("news projection length = %d, want %d", len(dashboard.News), maxItems)
	}
}

func TestDashboardNewsProjectionExcludesDeletedDismissedAndCarriesUpdatedAt(t *testing.T) {
	db = openTestDB(t)
	insert := func(id, updatedAt string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, id, "https://example.com/"+id, "测试来源", updatedAt, "摘要", "text", "", updatedAt); err != nil {
			t.Fatal(err)
		}
	}
	insert("active-news", "2026-08-07T10:00:00Z")
	insert("deleted-news", "2026-08-07T09:00:00Z")
	insert("dismissed-news", "2026-08-07T08:00:00Z")
	if _, err := db.Exec(`UPDATE news SET deleted_at=? WHERE id=?`, "2026-08-07T10:01:00Z", "deleted-news"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news_preferences(news_id,dismissed,updated_at) VALUES(?,?,?)`, "dismissed-news", 1, "2026-08-07T10:02:00Z"); err != nil {
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
	if dashboard.TotalEntries != 1 || len(dashboard.News) != 1 || dashboard.News[0].ID != "active-news" {
		t.Fatalf("dashboard news projection = total %d, items %#v", dashboard.TotalEntries, dashboard.News)
	}
	if dashboard.News[0].UpdatedAt != "2026-08-07T10:00:00Z" {
		t.Fatalf("dashboard updated_at = %q", dashboard.News[0].UpdatedAt)
	}
}

func TestModuleLayoutRejectsOutOfRangeValues(t *testing.T) {
	db = openTestDB(t)
	for _, payload := range []string{
		`{"id":"news","height_px":159,"span":1}`,
		`{"id":"news","height_px":961,"span":1}`,
		`{"id":"news","height_px":320,"span":4}`,
		`{"id":"news","height_px":320}`,
		`{"id":"news","span":1}`,
		`{"id":"unknown","height_px":320,"span":1}`,
	} {
		result := httptest.NewRecorder()
		moduleLayoutHandler(result, httptest.NewRequest(http.MethodPatch, "/api/layout", bytes.NewBufferString(payload)))
		if result.Code != http.StatusBadRequest {
			t.Fatalf("payload %s status = %d, body = %s", payload, result.Code, result.Body.String())
		}
	}
}

func TestReaderColumnResizeStaysResponsiveAndAccessible(t *testing.T) {
	body := indexPageCorpus(t)
	page := string(body)
	for _, expected := range []string{
		"grid-template-columns:var(--sidebar-width) 8px minmax(320px,var(--timeline-width)) 8px minmax(0,1fr)",
		".splitter:hover::after,.splitter:focus-visible::after",
		"id=\"sidebar-splitter\"",
		"./api/reader",
		"@media (prefers-reduced-motion:reduce)",
		"function startColumnResize",
		"const detailMinimum=300",
		"function entryDate(entry)",
		"morning_brief",
	} {
		if !staticContains(page, expected) {
			t.Errorf("dashboard resize motion is missing %q", expected)
		}
	}
	for _, forbidden := range []string{"transition:height", "transition:width", "transition:all"} {
		if staticContains(page, forbidden) {
			t.Errorf("dashboard resize motion must stay pointer-responsive; found %q", forbidden)
		}
	}
}

func TestReaderSourceScopeAndCollapseContractUnit(t *testing.T) {
	index := indexPageCorpus(t)
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, script, css := string(index), string(parity), string(styles)
	for _, expected := range []string{
		`class="splitter-slot"`,
		`id="sidebar-splitter-slot"`,
		`id="timeline-splitter-slot"`,
		`if(!document.hidden)void refresh()`,
		`window.__cacheReaderDetailEntry?.(entry)`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("reader collapse structure is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`void switchEntryScope(requestedScope)`,
		`source_id: target`,
		`Number.isFinite(Number(source.unread_count))`,
		`Math.max(0, Number(source.unread_count))`,
		`source.unread_count`,
		`const selectedKey = state.selected;`,
		`const selectedScope = entryScopeForView();`,
		`const scopedSnapshot =`,
		`await baseRefresh({ deferRender: true });`,
		`parity.entryScopeSnapshots.set(selectedScope, scopedSnapshot);`,
		`parity.entryScope = selectedScope;`,
		`restoreEntryScopeSnapshot(selectedScope);`,
		`The fresh dashboard snapshot is authoritative.`,
		`parity.entryCursor = "0";`,
		`const dashboardNewsIncluded = state.dashboard?.news_included !== false;`,
		`if (selectedStillPresent || (selectedKey && !dashboardNewsIncluded))`,
		`else if (selectedKey) state.selected = null;`,
		`new IntersectionObserver(`,
		`!makeEntries(state.dashboard || {}).some((entry) => entry.key === state.selected)`,
		`installPanelCollapseControls();`,
		`enforcePanelCollapseWidths();`,
		`entry.item?.counterevidence`,
		`反向证据与缺口`,
		`installSignalCounterevidence(entry);`,
	} {
		if !staticContains(script, expected) {
			t.Errorf("reader scoped loading or collapse behavior is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.reader-shell.sidebar-collapsed .sidebar`,
		`.reader-shell.sidebar-collapsed .sidebar{padding:0}`,
		`.reader-shell.timeline-collapsed{grid-template-columns:var(--sidebar-width) 8px 0 8px minmax(0,1fr)}`,
		`.reader-shell>.splitter-slot`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader collapse styles are missing %q", expected)
		}
	}
}

func TestFeedSourceSidebarReportsUnreadCountSeparatelyFromTotal(t *testing.T) {
	db = openTestDB(t)

	for index := 0; index < 3; index++ {
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", fmt.Sprintf(`{
  "source_id":"github-blog","source_type":"rss","external_id":"unread-count-%d",
  "title":"Unread count test %d","url":"https://example.com/unread-count/%d",
  "summary_text":"test","content_format":"text"
}`, index, index, index))
		if result.Code != http.StatusOK {
			t.Fatalf("entry %d status = %d, body = %s", index, result.Code, result.Body.String())
		}
	}
	read := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog&limit=1", "")
	if read.Code != http.StatusOK {
		t.Fatalf("entry list status = %d, body = %s", read.Code, read.Body.String())
	}
	var page struct {
		Entries []StandardEntry `json:"entries"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 {
		t.Fatalf("entry list length = %d, want 1", len(page.Entries))
	}
	marked := callJSON(t, entryHandler, http.MethodPatch, "/api/entries", fmt.Sprintf(`{"id":%q,"read":true}`, page.Entries[0].ID))
	if marked.Code != http.StatusOK {
		t.Fatalf("mark read status = %d, body = %s", marked.Code, marked.Body.String())
	}
	allEntries := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=github-blog&limit=50", "")
	if allEntries.Code != http.StatusOK {
		t.Fatalf("all entry list status = %d, body = %s", allEntries.Code, allEntries.Body.String())
	}
	var allPage struct {
		Entries []StandardEntry `json:"entries"`
	}
	if err := json.Unmarshal(allEntries.Body.Bytes(), &allPage); err != nil {
		t.Fatal(err)
	}
	if len(allPage.Entries) != 3 {
		t.Fatalf("scoped list returned %d entries, want all 3 regardless of unread state", len(allPage.Entries))
	}

	sources, err := queryFeedSources(200, "github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("source count = %d, want 1", len(sources))
	}
	if sources[0].EntryCount != 3 || sources[0].UnreadCount != 2 {
		t.Fatalf("source counts = total %d, unread %d; want total 3, unread 2", sources[0].EntryCount, sources[0].UnreadCount)
	}
}

func TestQueryFeedSourcesConcurrentFlowDoesNotExhaustSQLitePool(t *testing.T) {
	db = openTestDB(t)
	db.SetMaxOpenConns(4)

	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := queryFeedSources(200, "")
			results <- err
		}()
	}

	for i := 0; i < 4; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("concurrent source query failed: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent source queries exhausted the SQLite connection pool")
		}
	}
}

func TestReaderDetailLoadingSkeletonContract(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, css := string(parity), string(styles)
	for _, expected := range []string{
		`function detailLoadingMarkup()`,
		`detailCache: new Map()`,
		`function cacheDetailEntry(entry)`,
		`detailCacheLimit: 4`,
		`window.__cacheReaderDetailEntry = cacheDetailEntry`,
		`function restoreCachedSelectedEntry()`,
		`restoreCachedSelectedEntry();`,
		`function detailRenderFingerprint(entry)`,
		`panel.dataset.detailFingerprint === fingerprint`,
		`panel.dataset.detailFingerprint === "loading"`,
		`function timelineRenderFingerprint(entries, requestedScope, mode = "ready")`,
		`timeline?.dataset.renderFingerprint === fingerprint`,
		`data-detail-loading`,
		`role="status" aria-live="polite" aria-busy="true"`,
		`detail-loading-overview`,
		`const selectedScrollTop = selectedKey ? $("detail-panel")?.scrollTop || 0 : 0;`,
		`parity.detailLoading = true`,
		`panel.setAttribute("aria-busy", "true")`,
		`panel.innerHTML = detailLoadingMarkup()`,
	} {
		if !staticContains(script, expected) {
			t.Errorf("reader detail loading skeleton is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.detail-loading{`,
		`.detail-skeleton{`,
		`.detail-loading-spinner`,
		`@keyframes detail-skeleton-pulse`,
		`@media(prefers-reduced-motion:reduce){.detail-skeleton,.detail-loading-spinner{animation:none}}`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader detail loading styles are missing %q", expected)
		}
	}
}

func TestReaderTimelineLoadingSkeletonContract(t *testing.T) {
	page := indexPageCorpus(t)
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styles, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	pageText, script, css := string(page), string(parity), string(styles)
	for _, expected := range []string{
		`data-timeline-loading`,
		`function timelineSkeletonCardMarkup(kind)`,
		`function timelineLoadingMarkup(mode = "scope")`,
		`timelineLoadingMarkup("scope")`,
		`timelineLoadingMarkup("more")`,
		`aria-busy="true"`,
	} {
		if !strings.Contains(pageText+"\n"+script, expected) {
			t.Errorf("timeline loading skeleton is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.timeline-loading{`,
		`.timeline-skeleton-card.is-article`,
		`.timeline-skeleton-card.is-social`,
		`.timeline-skeleton-card.is-video`,
		`@keyframes timeline-skeleton-shimmer`,
		`@media(prefers-reduced-motion:reduce){.timeline-skeleton-line,.timeline-skeleton-media{animation:none`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("timeline loading styles are missing %q", expected)
		}
	}
}

func TestReaderPaginationUsesAuthoritativeCursorAndDropsStaleScopeFlow(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(parity)
	for _, expected := range []string{
		`parity.entryCursor = "0";`,
		`const requestToken = parity.entryRequestToken;`,
		`const requestScope = parity.entryScope;`,
		`requestToken !== parity.entryRequestToken`,
		`requestScope !== parity.entryScope`,
		`parity.entryLoading = false;`,
		`renderTimeline();`,
		`result.next_cursor_token || result.next_cursor`,
		`removeAttribute("data-render-fingerprint")`,
	} {
		if !staticContains(script, expected) {
			t.Errorf("reader pagination safety is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`news.length >= parity.entryPageSize ? String(news.length) : "0"`,
		`baseCount >= parity.entryPageSize`,
	} {
		if staticContains(script, forbidden) {
			t.Errorf("reader pagination still trusts snapshot length as cursor: %q", forbidden)
		}
	}
}

func TestReaderLongScrollKeepsBoundedProjectionAndSelectionAnchor(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(parity)
	for _, expected := range []string{
		`const readerNewsStateLimit = 500;`,
		`const readerProtectedNewsLimit = 500;`,
		`const readerNewsTotalLimit = readerNewsStateLimit + readerProtectedNewsLimit;`,
		`const readerEntryPageLimit = 200;`,
		`function entryRemotePageRemaining(totalEntries)`,
		`function sortReaderNews(items)`,
		`rightUpdated - leftUpdated`,
		`updated_at: item.updated_at || ""`,
		`revision: Number(item.revision) || 0`,
		`function mergeReaderEntry(existing, incoming)`,
		`newRevision > oldRevision`,
		`function readerNewsIsProtected(item)`,
		`function readerNewsWindowStatus(items)`,
		`function preserveSelectedReaderNews(sorted, bounded)`,
		`function boundReaderNews(items)`,
		`function rebindReaderNewsProjection()`,
		`contextMembershipsReady`,
		`state.dashboard.news = boundReaderNews(state.dashboard.news);`,
		`parity.entryBaseNews = boundReaderNews(parity.entryBaseNews);`,
		`const context = value(6);`,
		`if (context) {`,
		`parity.entryBaseNews = boundReaderNews`,
		`state.dashboard.news = parity.entryBaseNews.map`,
		`const protectedItems = sorted.filter(readerNewsIsProtected);`,
		`const ordinaryItems = sorted.filter((item) => !readerNewsIsProtected(item));`,
		`const bounded = boundReaderNews([...merged.values()]);`,
		`const mergeResult = mergePagedEntries(result.entries || []);`,
		`!mergeResult.windowsFull`,
		`parity.contextMutationKeys`,
		`state.dashboard.news.length < readerNewsTotalLimit`,
		`parity.entryPagesRead >= readerEntryPageLimit`,
		`parity.entryPagesRead += 1;`,
		`parity.entryPagesRead < readerEntryPageLimit`,
		`entryRemotePageRemaining(result.total_entries)`,
		`entryTotalForView(state.view)`,
		`parity.entryPagesRead = 1;`,
		`const aggregateTotal = entryTotalForView(state.view);`,
		`parity.entryPagesRead = 0;`,
		`Number.isFinite(aggregateTotal)`,
		`const selectedID = selectedKey.startsWith("news:")`,
		`if (readerNewsIsProtected(selected))`,
	} {
		if !staticContains(script, expected) {
			t.Errorf("bounded reader projection is missing %q", expected)
		}
	}
	mergeStart := strings.Index(script, `function mergePagedEntries(items)`)
	loadStart := strings.Index(script, `async function loadMoreEntries()`)
	if mergeStart < 0 || loadStart <= mergeStart {
		t.Fatal("reader merge/load-more blocks are missing")
	}
	merge := script[mergeStart:loadStart]
	if strings.Contains(merge, `state.dashboard.news = [...merged.values()].sort(`) {
		t.Fatal("reader merge must not retain an unbounded sorted map")
	}
	if !strings.Contains(merge, `return {`) || !strings.Contains(merge, `truncated:`) {
		t.Fatal("reader merge must report when the bounded window is full")
	}
}

func TestCloudReaderDefersDeviceProjectionUntilItsViewIsOpened(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(parity)
	for _, expected := range []string{
		`function liveDeviceDataView(view = state.view)`,
		`"source:my-recordings"`,
		`"source:device-meetings"`,
		`"source:obsidian"`,
		`if (!force && !liveDeviceDataView(view)) return;`,
		`scheduleLiveDeviceDataRefresh(false, false, view)`,
		`scheduleLiveDeviceDataRefresh(false, true);`,
		`if (!force && liveDeviceData.loadedAt > 0)`,
		`liveDeviceData.loadedAt = 0;`,
	} {
		if !staticContains(script, expected) {
			t.Errorf("lazy device projection is missing %q", expected)
		}
	}
	if strings.Contains(script, `scheduleLiveDeviceDataRefresh(true, true);`) {
		t.Fatal("cloud startup must not force a recursive meeting/Obsidian scan")
	}
}

func TestReaderScopedRefreshKeepsOpenArticleInTypedProjection(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(parity)
	refreshStart := strings.Index(script, `refresh = async function refreshParityState`)
	refreshEnd := strings.Index(script[refreshStart:], `persistReader = function`)
	if refreshStart < 0 || refreshEnd < 0 {
		t.Fatal("reader refresh implementation is missing")
	}
	refresh := script[refreshStart : refreshStart+refreshEnd]
	capture := strings.Index(refresh, `const scopedSnapshot =`)
	baseRefresh := strings.Index(refresh, `await baseRefresh({ deferRender: true });`)
	reset := strings.Index(refresh, `resetEntryPagination();`)
	restore := strings.Index(refresh, `restoreEntryScopeSnapshot(selectedScope);`)
	presence := strings.Index(refresh, `const selectedStillPresent =`)
	if capture < 0 || baseRefresh <= capture || reset <= baseRefresh || restore <= reset || presence <= restore {
		t.Fatalf("source snapshot must be captured before refresh and restored before the selected-row check")
	}
}

func TestReaderRequestTimeoutIsBoundedUnit(t *testing.T) {
	page := indexPageCorpus(t)
	for _, expected := range []string{
		`const timeoutMs = clamp(Number(options.timeoutMs) || 8000, 1000, 30000)`,
		`const controller = new AbortController()`,
		`controller.abort()`,
		`window.clearTimeout(timer)`,
		`if (timedOut) throw new Error("请求超时，请重试")`,
	} {
		if !staticContains(string(page), expected) {
			t.Errorf("bounded request timeout is missing %q", expected)
		}
	}
}

func TestReaderBootstrapPartiallyDegradesInsteadOfLoadingForeverFlow(t *testing.T) {
	page := indexPageCorpus(t)
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	surface := string(page) + "\n" + string(parity)
	for _, expected := range []string{
		`const results = await Promise.allSettled([`,
		`failedCount === results.length`,
		`内容载入超时，请点击刷新重试`,
		`部分内容暂时未载入，可继续浏览或点击刷新重试`,
		`部分来源设置暂时未载入，可继续浏览或重试`,
		`parity.loading = false`,
		`state.dashboard = dashboard`,
	} {
		if !staticContains(surface, expected) {
			t.Errorf("partial bootstrap degradation is missing %q", expected)
		}
	}
}

func TestReaderFullTextLeadImageAndMediaBoundsFlow(t *testing.T) {
	parityBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	parity, styles := string(parityBytes), string(stylesBytes)
	for _, expected := range []string{
		`function safeEntryHTML(raw, entryID, omitLeadImage = false)`,
		`function normalizeLeadImageIndex(value)`,
		`const articleRenderers = Object.freeze({`,
		`function articleRendererFor(entry, content)`,
		`function renderArticleContent(entry, content)`,
		`host === "mp.weixin.qq.com"`,
		`host === "wechat2rss.xlab.app"`,
		`host.endsWith(".mmbiz.qpic.cn")`,
		`Every article-body image goes through the indexed device proxy`,
		`./api/entries/content/image?id=`,
		`resolved.pathname.startsWith(`,
		`/agent/v1/api/source-gateway/entries/`,
		`if (xAccountForItem(entry.item)) return;`,
		`omitIndex >= 0 && imageIndex === omitIndex`,
		`content?.lead_image_index`,
		`if (hasHTML) return articleRenderers.html;`,
	} {
		if !strings.Contains(parity, expected) {
			t.Errorf("reader full-text lead-image flow is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.detail-kicker svg{width:14px;height:14px;flex:none}`,
		`.detail-panel .detail-cover`,
		`object-fit:contain`,
		`max-height:360px`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("reader full-text media bounds are missing %q", expected)
		}
	}
	pageBytes := indexPageCorpus(t)
	page := string(pageBytes)
	for _, expected := range []string{
		`item.view_type === "video" && item.media_url`,
		`item.view_type === "audio" && item.media_url`,
		`/(?:youtube\.com|youtu\.be)\//i.test(String(item.url || ""))`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("existing media renderer is missing %q", expected)
		}
	}
}

func TestReaderMobileNavigationKeepsFiltersReachable(t *testing.T) {
	pageBytes := indexPageCorpus(t)
	parityBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, parity, css := string(pageBytes), string(parityBytes), string(cssBytes)
	for _, expected := range []string{
		`menu:'<svg`,
		`id="mobile-nav-toggle"`,
		`aria-controls="sidebar"`,
		`id="mobile-nav-backdrop"`,
		`function setMobileNavigation`,
		`document.body.classList.toggle("mobile-nav-open", enabled)`,
	} {
		if !staticContains(page+parity, expected) {
			t.Errorf("mobile navigation is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`@media(max-width:640px)`,
		`body.mobile-nav-open .mobile-nav-backdrop`,
		`body.mobile-nav-open .sidebar`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("mobile navigation styles are missing %q", expected)
		}
	}
}

func TestRecordingFavoriteRefreshesStableContextMembership(t *testing.T) {
	body := indexPageCorpus(t)
	page := string(body)
	if !staticContains(page, `Object.prototype.hasOwnProperty.call(payload, "starred")) await refresh()`) {
		t.Error("recording favorite must refresh stable context membership after persistence")
	}
}

func TestReaderDiscoveryAndSubscriptionManagementFlow(t *testing.T) {
	db = openTestDB(t)

	catalog := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?q=github", "")
	if catalog.Code != http.StatusOK || !strings.Contains(catalog.Body.String(), `"id":"github-blog"`) || !strings.Contains(catalog.Body.String(), `"catalog_origin":"zettlab-maintained-public-rss-v1"`) {
		t.Fatalf("catalog status = %d, body = %s", catalog.Code, catalog.Body.String())
	}
	pack := callJSON(t, sourcePackHandler, http.MethodPost, "/api/packs", `{"pack_id":"ai-engineering"}`)
	if pack.Code != http.StatusOK || !strings.Contains(pack.Body.String(), `"hacker-news"`) || !strings.Contains(pack.Body.String(), `"arxiv-ai"`) {
		t.Fatalf("pack status = %d, body = %s", pack.Code, pack.Body.String())
	}

	folder := callJSON(t, folderHandler, http.MethodPost, "/api/folders", `{"name":"工程资讯","sort_order":10}`)
	if folder.Code != http.StatusOK {
		t.Fatalf("folder status = %d, body = %s", folder.Code, folder.Body.String())
	}
	var folderBody SubscriptionFolder
	if err := json.Unmarshal(folder.Body.Bytes(), &folderBody); err != nil {
		t.Fatal(err)
	}
	tag := callJSON(t, tagHandler, http.MethodPost, "/api/tags", `{"name":"重点","color":"#2563eb"}`)
	if tag.Code != http.StatusOK {
		t.Fatalf("tag status = %d, body = %s", tag.Code, tag.Body.String())
	}
	var tagBody SubscriptionTag
	if err := json.Unmarshal(tag.Body.Bytes(), &tagBody); err != nil {
		t.Fatal(err)
	}

	patchSubscription := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"github-blog","folder_id":"`+folderBody.ID+`","pinned":true,"muted":true,"sort_order":20}`)
	if patchSubscription.Code != http.StatusOK {
		t.Fatalf("subscription status = %d, body = %s", patchSubscription.Code, patchSubscription.Body.String())
	}
	assignTag := callJSON(t, tagHandler, http.MethodPatch, "/api/tags", `{"id":"`+tagBody.ID+`","source_id":"github-blog","enabled":true}`)
	if assignTag.Code != http.StatusOK {
		t.Fatalf("tag assignment status = %d, body = %s", assignTag.Code, assignTag.Body.String())
	}
	source := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?q=github", "")
	for _, expected := range []string{`"folder_name":"工程资讯"`, `"pinned":true`, `"muted":true`, `"重点"`} {
		if !strings.Contains(source.Body.String(), expected) {
			t.Fatalf("managed source missing %s: %s", expected, source.Body.String())
		}
	}

	bulk := callJSON(t, bulkSubscriptionHandler, http.MethodPost, "/api/subscriptions/bulk", `{"source_ids":["github-blog","cloudflare-blog"],"operation":"mute"}`)
	if bulk.Code != http.StatusOK || !strings.Contains(bulk.Body.String(), `"count":2`) {
		t.Fatalf("bulk status = %d, body = %s", bulk.Code, bulk.Body.String())
	}
}

func TestTopicPackOneClickSubscriptionFlow(t *testing.T) {
	db = openTestDB(t)

	result := callJSON(t, sourcePackHandler, http.MethodGet, "/api/packs", "")
	if result.Code != http.StatusOK {
		t.Fatalf("pack list status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Packs []SourcePack `json:"packs"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Packs) != 10 {
		t.Fatalf("topic pack count = %d, want 10", len(payload.Packs))
	}
	expected := map[string]int{
		"ai-engineering":           5,
		"engineering-development":  5,
		"science-innovation":       6,
		"global-tech-business":     6,
		"policy-regulation":        7,
		"chinese-tech-policy":      6,
		"operational-intelligence": 12,
		"key-people":               9,
		"wechat-tech":              12,
		"ai-video":                 18,
	}
	for _, pack := range payload.Packs {
		want, ok := expected[pack.ID]
		if !ok {
			t.Errorf("unexpected topic pack %q", pack.ID)
			continue
		}
		if len(pack.Sources) != want {
			t.Errorf("topic pack %q source count = %d, want %d", pack.ID, len(pack.Sources), want)
		}
		for _, source := range pack.Sources {
			if source.IconText == "" || !validHexColor(source.IconColor) {
				t.Errorf("topic pack %q source %q has incomplete icon: %q %q", pack.ID, source.ID, source.IconText, source.IconColor)
			}
		}
	}

	subscribe := callJSON(t, sourcePackHandler, http.MethodPost, "/api/packs", `{"pack_id":"engineering-development"}`)
	if subscribe.Code != http.StatusOK || !strings.Contains(subscribe.Body.String(), `"github-changelog"`) || !strings.Contains(subscribe.Body.String(), `"smashing-magazine"`) {
		t.Fatalf("one-click subscribe status = %d, body = %s", subscribe.Code, subscribe.Body.String())
	}
	var subscribed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM source_pack_members m JOIN feed_subscriptions s ON s.source_id=m.source_id WHERE m.pack_id='engineering-development' AND s.enabled=1`).Scan(&subscribed); err != nil {
		t.Fatal(err)
	}
	if subscribed != expected["engineering-development"] {
		t.Fatalf("subscribed topic sources = %d, want %d", subscribed, expected["engineering-development"])
	}
}

func TestFeedSourceWithoutCatalogIconGetsStableFallback(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES('custom-source','rss','自定义行业源','https://example.com/custom.xml','',0,'unavailable',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	first, err := feedSourceByID("custom-source")
	if err != nil {
		t.Fatal(err)
	}
	second, err := feedSourceByID("custom-source")
	if err != nil {
		t.Fatal(err)
	}
	if first.IconText != "自" || !validHexColor(first.IconColor) {
		t.Fatalf("fallback icon = %q %q", first.IconText, first.IconColor)
	}
	if first.IconText != second.IconText || first.IconColor != second.IconColor {
		t.Fatalf("fallback icon changed between reads: %#v %#v", first, second)
	}
}

func TestTopicPackAndSourceIconPresentation(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, styles := string(pageBytes), string(stylesBytes)
	for _, expected := range []string{
		`<h3>专题订阅</h3>`,
		`一键添加`,
		`pack-source-icons`,
		`sourceAvatar(source, "sidebar-source-avatar")`,
		`remoteIcon || brandFallback || projectFallback`,
		`data-fallback-src=`,
		`./api/sources/icon?id=`,
		`loading="lazy"`,
		`event.target.matches(".source-avatar-image")`,
		`event.target.src = fallback`,
		`youtubeBrandIconURL`,
		`data-brand-fallback-src=`,
		`data-retry-src=`,
		`event.target.classList.add("is-loaded")`,
		`"ai", "programming"`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("topic subscription UI is missing %q", expected)
		}
	}
	for _, expected := range []string{".pack-source-avatar", ".source-item .sidebar-source-avatar", ".source-avatar-image", ".source-avatar-image.is-loaded", ".source-avatar-image[hidden]", ".pack-item.complete"} {
		if !strings.Contains(styles, expected) {
			t.Errorf("topic subscription styles are missing %q", expected)
		}
	}
}

func TestYouTubeVideoPresentationKeepsMediaAboveCopyAndAvatarFallbackBounded(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, styles := string(pageBytes), string(stylesBytes)
	for _, expected := range []string{
		`<span class="youtube-entry-media">`,
		`<span class="youtube-entry-copy">`,
		`thumbnailFallbackURL`,
		`thumbnailDirectURL`,
		`youtubeBrandIconURL`,
		`data-brand-fallback-src=`,
		`isVideoCreator || source?.has_icon`,
		`video-creator-avatar`,
		`视频缩略图加载中`,
		`panel.querySelector(".detail-body")?.remove();`,
		`!xDetail && !isYouTube && ["news", "evidence"].includes(entry.kind)`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("YouTube presentation is missing %q", expected)
		}
	}
	fingerprintGuard := strings.Index(page, `if (panel.dataset.detailFingerprint === fingerprint && panel.childElementCount)`)
	classReset := strings.LastIndex(page, `panel.classList.remove("x-detail-panel", "youtube-detail-panel"`)
	if fingerprintGuard < 0 || classReset < 0 || fingerprintGuard > classReset {
		t.Fatal("detail fingerprint guard must run before resetting presentation classes")
	}
	if strings.Index(page, `<span class="youtube-entry-media">`) > strings.Index(page, `<span class="youtube-entry-copy">`) {
		t.Fatal("YouTube media must be rendered before copy")
	}
	for _, expected := range []string{
		`.youtube-entry-main{display:flex!important;flex-direction:column`,
		`.youtube-entry-media{position:relative;display:block;width:100%`,
		`aspect-ratio:16/9`,
		`.youtube-thumbnail-placeholder[hidden]`,
		`.youtube-detail-poster{position:absolute`,
		`.youtube-detail-play{position:absolute`,
		`.youtube-detail-status[hidden]{display:none}`,
		`.youtube-detail-frame[data-player-state="ready"] iframe`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("YouTube layout is missing %q", expected)
		}
	}
}

func TestYouTubeThumbnailFallbackDoesNotSpeculateMissingLocalMedia(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	start := strings.Index(page, `function youtubeVideoForItem(item) {`)
	end := strings.Index(page[start:], `function safeVideoLink(raw) {`)
	if start < 0 || end < 0 {
		t.Fatal("YouTube thumbnail resolver is missing")
	}
	resolver := page[start : start+end]
	for _, expected := range []string{
		`const thumbnailDirectURL = localAppMediaURL(`,
		`item.thumbnail_url`,
		`thumbnailFallbackURL: ` + "`https://img.youtube.com/vi/${id}/hqdefault.jpg`",
		`thumbnailDirectURL,`,
	} {
		if !strings.Contains(resolver, expected) {
			t.Errorf("YouTube thumbnail resolver is missing %q", expected)
		}
	}
	if strings.Contains(resolver, "`./api/media?id=${encodeURIComponent(text(item.id))}&kind=thumbnail`") {
		t.Fatal("missing YouTube covers must not speculate a local /api/media thumbnail")
	}
	for _, expected := range []string{
		`nextURL !== current`,
		`secondURL !== current`,
		`secondURL !== nextURL`,
		`event.target.matches(".entry-media-wrap img")`,
		`event.target.closest(".entry-media-wrap")?.remove()`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("thumbnail fallback deduplication is missing %q", expected)
		}
	}
}

func TestVideoEntriesOpenFloatingPlayerWithoutDetailPanel(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, styles := string(pageBytes), string(stylesBytes)
	indexBytes := indexPageCorpus(t)
	index := string(indexBytes)
	for _, expected := range []string{
		`function openFloatingVideoPlayer(entry)`,
		`data-floating-video-player`,
		`function installFloatingVideoSelection()`,
		`event.stopImmediatePropagation()`,
		`!videoGallery`,
		`openFloatingVideoPlayer(next);`,
		`const baseSelectEntry = selectEntry;`,
		`installFloatingVideoSelection();`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("floating video flow is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.video-floating-player{position:fixed`,
		`.video-floating-card{width:min(960px,calc(100vw - 80px))`,
		`place-items:center`,
		`.video-floating-frame iframe,.video-floating-frame video`,
		`.video-floating-close`,
		`@media(max-width:640px){.video-floating-player`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("floating video layout is missing %q", expected)
		}
	}
	videoGalleryGuard := strings.Index(page, `parity.autoSelectEntry &&`)
	if videoGalleryGuard < 0 || !strings.Contains(page[videoGalleryGuard:videoGalleryGuard+180], `!videoGallery`) {
		t.Fatal("video gallery must not auto-open the first item in the detail panel")
	}
	if !strings.Contains(index, `document.body.classList.remove("detail-open");`) {
		t.Fatal("switching a dynamically rendered video source must clear the previous detail-open state")
	}
}

func TestReaderSourcesHydrateOnlyAfterSelection(t *testing.T) {
	page, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(page)
	for _, expected := range []string{
		"const lazySourceLoadAt = new Map();",
		"const sourceMetadataLoads = new Map();",
		"./api/sources?source_id=",
		"async function resolveSourceMetadata(sourceID, force = false)",
		"function loadSourceOnDemand(sourceID)",
		"void syncSource(id);",
		"if (view.startsWith(\"source:\")) loadSourceOnDemand(view.slice(7));",
		"const preparing = gatewaySourcePreparing(source);",
		"hydrated ? statusKind(source.status) : \"idle\"",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("lazy source hydration contract is missing %q", expected)
		}
	}
}

func TestReaderBootstrapUsesAuthoritativeSourceAndAggregateCounts(t *testing.T) {
	page := indexPageCorpus(t)
	script := string(page)
	for _, expected := range []string{
		"function responseBody(value)",
		"const projectedCount = Number(source.entry_count)",
		"[\"rss\", \"rss_entries\"]",
		"[\"video\", \"video_entries\"]",
		"[\"youtube\", \"youtube_entries\"]",
		"sourceBilibiliCount",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("reader bootstrap source/count contract is missing %q", expected)
		}
	}
}

func TestReaderSelectionKeepsTimelineAndSourceDOMStable(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, expected := range []string{
		`function syncTimelineSelection(timeline, entries = [])`,
		`timeline.querySelectorAll(".entry.selected")`,
		`syncTimelineSelection(timeline, entries);`,
		`sourceList.dataset.renderFingerprint === sourceFingerprint`,
		`sourceList.innerHTML = sourceMarkup`,
		`panel.querySelector(".detail-cover")?.remove()`,
		`panel.querySelector(".detail-media")?.remove()`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("stable reader selection is missing %q", expected)
		}
	}
	fingerprintStart := strings.Index(page, `function timelineRenderFingerprint(`)
	fingerprintEnd := strings.Index(page, `renderTimeline = function renderParityTimeline()`)
	if fingerprintStart < 0 || fingerprintEnd <= fingerprintStart {
		t.Fatal("timeline fingerprint block is missing")
	}
	if strings.Contains(page[fingerprintStart:fingerprintEnd], `state.selected`) {
		t.Fatal("timeline fingerprint must not rebuild the list for selection-only changes")
	}
}

func TestReaderLoadedEntryMetaReconcilesAfterPaginatedTimelineRender(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	helperStart := strings.Index(page, `function syncLoadedEntryMeta(entries = filteredEntries())`)
	renderStart := strings.Index(page, `renderTimeline = function renderParityTimeline()`)
	if helperStart < 0 || renderStart < 0 {
		t.Fatal("loaded entry metadata reconciliation is missing")
	}
	for _, expected := range []string{
		`readerFocusCopy(`,
		`已加载 ${loaded} / 共 ${total} 条`,
		`syncLoadedEntryMeta();`,
		`syncLoadedEntryMeta(entries);`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("loaded entry metadata contract is missing %q", expected)
		}
	}
}

func TestReaderReadMutationKeepsStableViewsAndVideoDetail(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	fingerprintStart := strings.Index(page, `function timelineRenderFingerprint(`)
	fingerprintEnd := strings.Index(page, `renderTimeline = function renderParityTimeline()`)
	if fingerprintStart < 0 || fingerprintEnd <= fingerprintStart {
		t.Fatal("timeline fingerprint block is missing")
	}
	fingerprint := page[fingerprintStart:fingerprintEnd]
	for _, expected := range []string{
		`node.classList.toggle("read", Boolean(pref(entry).read))`,
		`syncTimelineSelection(timeline, entries);`,
	} {
		if !strings.Contains(page, expected) && !strings.Contains(fingerprint, expected) {
			t.Errorf("read mutation stability contract is missing %q", expected)
		}
	}
	detailStart := strings.Index(page, `function detailRenderFingerprint(`)
	detailEnd := strings.Index(page, `renderDetail = function renderParityDetail()`)
	if detailStart < 0 || detailEnd <= detailStart {
		t.Fatal("detail fingerprint block is missing")
	}
	if strings.Contains(page[detailStart:detailEnd], `preference.read ? "read" : "unread"`) {
		t.Fatal("read preference must not rebuild the selected detail or YouTube iframe")
	}
}

func TestReaderUnreadViewRemovedFromNavigation(t *testing.T) {
	indexBytes := indexPageCorpus(t)
	parityBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	i18nBytes, err := appFS.ReadFile("static/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	index, parity, i18n := string(indexBytes), string(parityBytes), string(i18nBytes)
	for _, fragment := range []string{
		`data-view="unread"`,
		`data-count="unread"`,
		`state.view === "unread"`,
		`["unread", "未读", "inbox"]`,
		`["all", "rss", "unread", "starred", "later", "context"]`,
		`"unread": 0`,
	} {
		if strings.Contains(index, fragment) || strings.Contains(parity, fragment) {
			t.Fatalf("Unread navigation/view logic must be removed: found %q", fragment)
		}
	}
	if strings.Contains(i18n, `unread: "未读"`) || strings.Contains(i18n, `unread: "Unread"`) {
		t.Fatal("Unread navigation label must be removed from i18n")
	}
	for _, fragment := range []string{
		`node.classList.toggle("read", Boolean(pref(entry).read))`,
		`body: JSON.stringify({ key: entry.key, read: true })`,
	} {
		if !strings.Contains(parity, fragment) {
			t.Fatalf("Read-state behavior must remain available after removing the view: missing %q", fragment)
		}
	}
}

func TestYouTubeDetailEmbedUsesClickToPlayAndBoundedFallback(t *testing.T) {
	pageBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(pageBytes)
	for _, expected := range []string{
		`const youtubeEmbedTimeoutMS = 8000;`,
		`data-youtube-detail-play`,
		`data-youtube-detail-status role="status" aria-live="polite" hidden`,
		`function playYouTubeDetailVideo(button)`,
		`frame.dataset.playerState = "loading";`,
		`frame.setAttribute("aria-busy", "true");`,
		`iframe.remove();`,
		`frame.dataset.playerState = "fallback";`,
		`frame.dataset.playerState = "ready";`,
		`button.focus({ preventScroll: true });`,
		`视频暂时无法连接`,
		`在 YouTube 打开`,
	} {
		if !staticContains(page, expected) {
			t.Errorf("recoverable YouTube detail is missing %q", expected)
		}
	}
	markupStart := strings.Index(page, `function youtubeDetailMarkup(item) {`)
	markupEnd := strings.Index(page, `function showYouTubeDetailStatus(`)
	if markupStart < 0 || markupEnd <= markupStart {
		t.Fatal("YouTube detail markup block is missing")
	}
	if strings.Contains(page[markupStart:markupEnd], `<iframe`) {
		t.Fatal("YouTube detail must not start a third-party iframe before user intent")
	}
	playerStart := strings.Index(page, `function playYouTubeDetailVideo(button) {`)
	playerEnd := strings.Index(page, `function installYouTubeDetailPresentation(`)
	if playerStart < 0 || playerEnd <= playerStart {
		t.Fatal("YouTube player flow block is missing")
	}
	player := page[playerStart:playerEnd]
	timeoutStart := strings.Index(player, `window.setTimeout(() => {`)
	loadStart := strings.Index(player, `iframe.addEventListener(`)
	appendStart := strings.Index(player, `frame.appendChild(iframe);`)
	if timeoutStart < 0 || loadStart <= timeoutStart || appendStart <= loadStart {
		t.Fatal("YouTube player must arm timeout and load recovery before attaching the iframe")
	}
}

func TestFocusV3ActionBoardFillsTheAvailableViewportWithoutOuterScroll(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{
		`workspace.classList.toggle("is-action-view", active && state.view === "action")`,
		`.reader-shell.product-v3-shell.focus-v3-active{grid-template-columns:var(--sidebar-width) minmax(0,1fr)!important}`,
		`.reader-shell.product-v3-shell.focus-v3-active>.focus-v3-product-splitter{display:none!important}`,
		`.focus-v3-workspace.is-action-view{display:flex;min-height:0;flex-direction:column;overflow:hidden}`,
		`.focus-v3-action-board{width:100%;min-width:0;min-height:0;flex:1;display:grid;grid-template-columns:repeat(4,minmax(210px,1fr));align-items:stretch;gap:6px;overflow-x:auto;overflow-y:hidden}`,
		`.focus-v3-action-board>section>div{min-height:0;flex:1;display:grid;align-content:start;gap:6px;overflow-y:auto;`,
	} {
		if !strings.Contains(script+styles, expected) {
			t.Errorf("viewport-filling action board contract is missing %q", expected)
		}
	}
	if strings.Contains(styles, `.focus-v3-action-board{display:grid;grid-template-columns:repeat(4,minmax(240px,1fr));align-items:start;gap:10px;min-width:1040px}`) {
		t.Fatal("action board must not retain the fixed-width legacy layout")
	}
	if strings.Contains(styles, `.reader-shell.product-v3-shell.focus-v3-active{grid-template-columns:238px 5px minmax(0,1fr)!important`) {
		t.Fatal("Focus workspace must not retain the Reader splitter column")
	}
}

func TestFocusV3ProductionShellUsesRealOperationsAndPreservesReaderRoutes(t *testing.T) {
	pageBytes := indexPageCorpus(t)
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, script, styles := string(pageBytes), string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{"我的焦点", "data-view=\"meeting\"", "data-view=\"obsidian\""} {
		if !strings.Contains(page, expected) {
			t.Errorf("production shell is missing %q", expected)
		}
	}
	for _, expected := range []string{"focus_space.get", "focus_space.update", "focus_analysis.get", "data-focus-lane", "data-focus-panel", "data-focus-feedback"} {
		if !strings.Contains(script, expected) {
			t.Errorf("Focus V3 real operation/interaction is missing %q", expected)
		}
	}
	for _, expected := range []string{".reader-shell.focus-v3-active", ".focus-v3-board", ".focus-v3-drawer", ".focus-v3-create-dialog", ".focus-v3-evidence-list button strong,.focus-v3-evidence-list button span{display:-webkit-box;min-width:0;overflow:hidden;overflow-wrap:anywhere;-webkit-box-orient:vertical;-webkit-line-clamp:2}", "overflow-x:hidden;overflow-y:auto;overscroll-behavior-x:none;box-sizing:border-box"} {
		if !strings.Contains(styles, expected) {
			t.Errorf("Focus V3 production style is missing %q", expected)
		}
	}
}

func TestFocusBoardKeepsEmptyModeDropRow(t *testing.T) {
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, expected := range []string{
		".focus-v3-board-cell.empty{min-height:0;padding:6px;border:1px dashed var(--line);background:var(--surface-soft)}",
		".focus-v3-board-cell.drop-target{min-height:0",
		".focus-v3-board-lane{display:grid;grid-template-rows:auto minmax(0,1fr);min-height:0;gap:6px}",
		"grid-template-rows:auto repeat(2,minmax(0,1fr))",
	} {
		if !strings.Contains(styles, expected) {
			t.Fatalf("Focus board must keep a draggable row for empty modes: missing %q", expected)
		}
	}
}

func TestFocusBoardSplitsModesAndScrollsWithinLanes(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{
		`"is-focus-board-view"`,
		`active && state.view === "today" && parity.focusViewMode === "board"`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("Focus board must opt into viewport-filling layout: missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.reader-shell.product-v3-shell.focus-v3-active>.focus-v3-workspace.is-focus-board-view{display:flex;flex-direction:column;overflow:hidden}`,
		`.focus-v3-board-wrap{display:flex;flex:1;min-height:0`,
		`grid-template-rows:auto repeat(2,minmax(0,1fr))`,
		`.focus-v3-board-lane{display:grid;grid-template-rows:auto minmax(0,1fr);min-height:0`,
		`.focus-v3-board-wrap>.focus-v3-board{overflow:hidden}`,
		`.focus-v3-board-lane{overflow:hidden}`,
		`.focus-v3-board-cells{grid-auto-rows:minmax(100%,max-content);overflow-y:auto;overflow-x:hidden;overscroll-behavior:contain;align-content:start}`,
		`focus-v3-board-cell{position:relative;display:grid;gap:5px;align-content:start;min-width:0;min-height:0;overflow:visible;overscroll-behavior:contain`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("Focus board must keep scrolling inside each mode lane: missing %q", expected)
		}
	}
}

func TestReaderDirectoryNavigationPreservesScrollPosition(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		"obsidianDirectoryScrollTops: new Map()",
		"function rememberObsidianDirectoryScroll",
		"function restoreObsidianDirectoryScroll",
		`data-obsidian-directory-view="${esc(`,
		"rememberObsidianDirectoryScroll();",
		"rememberObsidianDirectoryScroll(timeline);",
		"restoreObsidianDirectoryScroll(timeline, state.view);",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("reader directory navigation scroll preservation missing %q", expected)
		}
	}
}

func TestFoloXPresentationKeepsPortraitsPlayableVideoAndFallback(t *testing.T) {
	pageBytes := indexPageCorpus(t)
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, script, styles := string(pageBytes), string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{
		"media-src 'self' blob: https://video.twimg.com",
		`"elon-musk-x": "elonmusk"`,
		`"donald-trump-x": "realDonaldTrump"`,
		`function xProfileHandle(raw)`,
		`function xSourceIsX(source)`,
		`function xPostTextFromContent(value, entry)`,
		`function xEntryFallbackText(entry)`,
		`function xAuthorAvatarMarkup(entry, source)`,
		`function syncXFeedProfileHeader(source)`,
		`data-x-feed-profile`,
		`function xFeedMediaIndices(value)`,
		`function applyXFeedMedia(entry, value)`,
		`function observeXFeedFullText(entries)`,
		`function hydrateXFeedEntry(entry)`,
		`document.body.classList.toggle("x-feed", xFeed)`,
		`!xFeed &&`,
		`entry?.kind === "news" && xAccountForItem(entry.item)`,
		`data-x-needs-fulltext=`,
		`target.hostname.toLowerCase() !== "video.twimg.com"`,
		`localAppMediaURL(item.thumbnail_url, item.id, "thumbnail")`,
		`localAppMediaURL(item.media_url, item.id, "media")`,
		`class="x-entry-video" controls playsinline preload="none"`,
		`function showXVideoFallback`,
		`function installXDetailPresentation`,
		`class="x-detail-post" aria-label="X 动态正文"`,
		`AI 概览`,
		`if (xAccountForItem(entry.item)) return;`,
		`classList.remove("x-detail-panel", "youtube-detail-panel"`,
		`const markup = youtubeDetailMarkup(item);`,
		`panel.insertAdjacentHTML("afterbegin", markup);`,
		`youtube-detail-tabs`,
		`data-youtube-detail-tab="transcript"`,
		`if (!xDetail && !isYouTube && ["news", "evidence"].includes(entry.kind))`,
		`在 X 查看`,
		`sourceAvatar(source,`,
	} {
		if !staticContains(page+"\n"+script, expected) {
			t.Errorf("Folo/X presentation is missing %q", expected)
		}
	}
	for _, expected := range []string{
		".x-entry-avatar .x-author-avatar",
		"border-radius:50%",
		".x-entry-head",
		"min-width:64px",
		".x-entry-head>span:first-of-type",
		".x-entry-text",
		".x-entry-media",
		"aspect-ratio:16/9",
		".x-media-fallback[hidden]",
		".x-detail-identity",
		".x-detail-post p",
		"font-size:16px",
		"font-weight:400",
		".x-detail-summary",
		`body.x-feed .reader-shell{grid-template-columns:var(--sidebar-width) 8px minmax(0,1fr) 0 0!important}`,
		`body.x-feed .x-entry{display:grid;grid-template-columns:48px minmax(0,1fr) 34px;max-width:760px`,
		`body.x-feed .detail-panel{display:none!important}`,
		`.x-entry-foot`,
		`.x-entry-rich-media`,
		`body.x-feed .x-feed-profile{display:grid;grid-template-columns:48px minmax(0,1fr) auto`,
		`body.x-feed .timeline-head>.timeline-title-row,body.x-feed .timeline-head>.timeline-meta,body.x-feed .timeline-head>.search-row{display:none}`,
		`html[data-theme] body.product-v3-shell.x-feed .x-feed-profile{width:min(100%,760px);margin-inline:auto}`,
		`html[data-theme] body.product-v3-shell.x-feed .x-entry{width:min(100%,760px);max-width:760px;min-height:0;margin:0 auto;padding:22px 0`,
		`html[data-theme] body.product-v3-shell.x-feed .date-label,html[data-theme] body.product-v3-shell.x-feed .entry-page-more{width:min(100%,760px);margin-left:auto;margin-right:auto;text-align:center}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("Folo/X styles are missing %q", expected)
		}
	}
}

func TestProductV3VideoGalleryConsumesFullWorkspaceWidth(t *testing.T) {
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	styles := string(stylesBytes)
	for _, expected := range []string{
		`body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active){grid-template-columns:var(--sidebar-width) 5px minmax(0,1fr) 0 0!important}`,
		`body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active)>#timeline-splitter-slot,body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active)>.detail-panel{display:none!important}`,
		`body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active)>.timeline-panel{grid-column:3;min-width:0;width:auto;border-right:0}`,
		`.timeline-scroll.video-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));grid-auto-rows:max-content;align-content:start;gap:10px 2px;padding:10px 16px 32px;background:var(--canvas)}`,
		`.video-grid .video-entry{display:flex!important;flex-direction:column;align-self:start;min-width:0;height:auto!important;min-height:0!important`,
		`.video-grid .youtube-entry .video-entry-title,.video-grid .video-entry-title{display:-webkit-box!important;min-width:0;min-height:2.7em;max-height:2.84em;overflow:hidden;color:var(--ink);font-size:13px;line-height:1.42;font-weight:720;-webkit-box-orient:vertical;-webkit-line-clamp:2}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("product V3 full-width video gallery style is missing %q", expected)
		}
	}
}

func TestFocusV3NavigationClearsReaderProjectionLayoutStateFlow(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)
	start := strings.Index(script, "function renderFocusV3Workspace() {")
	if start < 0 {
		t.Fatal("Focus V3 workspace renderer block is missing")
	}
	endOffset := strings.Index(script[start:], "function focusDetailSections(")
	if endOffset < 0 {
		t.Fatal("Focus V3 workspace renderer block is missing")
	}
	block := script[start : start+endOffset]
	if !strings.Contains(script, `function invalidateReaderProjectionRequests()`) {
		t.Fatal("reader projection invalidation helper is missing")
	}
	helperStart := strings.Index(script, `function invalidateReaderProjectionRequests() {`)
	if helperStart < 0 {
		t.Fatal("reader projection invalidation helper block is missing")
	}
	helperEnd := strings.Index(script[helperStart:], `function restoreEntryScopeSnapshot(`)
	if helperEnd < 0 {
		t.Fatal("reader projection invalidation helper block is missing")
	}
	helper := script[helperStart : helperStart+helperEnd]
	for _, expected := range []string{
		`function rememberAggregateEntrySnapshot()`,
		`function restoreAggregateEntrySnapshot()`,
		`parity.entryAggregateSnapshot = {`,
		`state.dashboard.news = snapshot.news.map`,
		`if (!previousScope && target) rememberAggregateEntrySnapshot();`,
		`if (!restoreAggregateEntrySnapshot()) {`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("bounded aggregate reader snapshot is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`state.dashboard.news = aggregateNews`,
		`const restoredAggregate = restoreAggregateEntrySnapshot();`,
		`parity.entryCursor = "0"`,
		`parity.entryPagesRead = 0`,
		`parity.entryHasMore =`,
		`parity.entryObserver?.disconnect()`,
		`parity.xFeedObserver?.disconnect()`,
		`parity.xFeedHydrationQueue.length = 0`,
		`parity.xFeedHydrationQueued.clear()`,
	} {
		if !strings.Contains(helper, expected) {
			t.Errorf("reader projection invalidation must restore aggregate state %q", expected)
		}
	}
	for _, expected := range []string{
		`const wasFocusActive = Boolean(`,
		`if (!wasFocusActive) invalidateReaderProjectionRequests();`,
		`document.body.classList.remove("x-feed", "video-gallery")`,
		`shell?.classList.remove("x-feed", "video-gallery")`,
		`$("timeline")?.classList.remove("video-grid")`,
		`document.body.classList.add("product-v3-shell")`,
		`shell?.classList.add("product-v3-shell")`,
	} {
		if !strings.Contains(block, expected) {
			t.Errorf("Focus navigation state cleanup is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`parity.entryLoadingOwnerToken = requestToken`,
		`parity.entryLoadingOwnerToken === requestToken`,
		`focusV3Views.has(state.view)`,
		`parity.entryObserver !== observer`,
		`parity.xFeedObserver !== observer`,
		`parity.xFeedObserver?.takeRecords?.()`,
		`parity.videoInitialRequestStarted = false`,
		`parity.entryObserver?.disconnect()`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("reader request ownership guard is missing %q", expected)
		}
	}
	if strings.Index(block, `document.body.classList.remove("x-feed", "video-gallery")`) > strings.Index(block, `document.body.classList.toggle("focus-v3-active", active)`) {
		t.Fatal("reader projection classes must be cleared before Focus layout is applied")
	}
	for _, expected := range []string{
		`body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active)`,
		`body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active)>.timeline-panel`,
		`@media(max-width:820px){.reader-shell.product-v3-shell{height:auto;min-height:100dvh;grid-template-columns:1fr!important;overflow:visible}body.product-v3-shell.video-gallery:not(.detail-open) .reader-shell:not(.focus-v3-active){grid-template-columns:1fr!important}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("video gallery layout must exclude active Focus workspace: %q", expected)
		}
	}
	if strings.Contains(styles, `.reader-shell.product-v3-shell.video-gallery:not(.detail-open){`) {
		t.Fatal("video gallery selector must not override the Focus two-column layout")
	}
}

func TestReaderWorkspacePolishKeepsControlsAccessibleAndBounded(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{
		`role="tabpanel"`,
		`aria-selected="true"`,
		`aria-selected="false"`,
		`function onModalKeydown(event)`,
		`event.key === "Escape"`,
		`restoreModalFocus();`,
		`["ArrowLeft", "ArrowRight", "Home", "End"]`,
		`const scrollTop = panel?.scrollTop || 0;`,
		`panel.scrollTop = scrollTop;`,
		`aria-busy="${busy}"`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("reader interaction polish is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.splitter-slot>.splitter::after`,
		`.entry.read{opacity:1}`,
		`.entry.selected{border-color:#9cb8ed`,
		`.pack-action svg{width:14px;height:14px`,
		`.pack-action[aria-busy="true"]{cursor:wait}`,
		`.toast,.error-bar{z-index:40}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("reader visual polish is missing %q", expected)
		}
	}
}

func TestHackerNewsCuratedFeedRetiresWithoutChangingStableIdentity(t *testing.T) {
	db = openTestDB(t)
	var name, feedURL, description, mode string
	if err := db.QueryRow(`SELECT name,url,description,collection_mode FROM feed_sources WHERE id='hacker-news'`).Scan(&name, &feedURL, &description, &mode); err != nil {
		t.Fatal(err)
	}
	if name != "Hacker News AI" || feedURL != "source-retired://hacker-news" || !strings.Contains(description, "AI") || mode != "agent" {
		t.Fatalf("hacker-news source = %q %q %q %q", name, feedURL, description, mode)
	}
	found := false
	for _, source := range defaultFeedSources {
		if source.ID == "hacker-news" {
			found = source.Name == name
		}
	}
	if !found {
		t.Fatal("default Hacker News AI source must keep the stable id")
	}
}

func TestReaderSubscriptionFlowWaitsForSyncAndExplainsEmptySource(t *testing.T) {
	body, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	for _, expected := range []string{
		"if (enabled) void monitorSourceSync(sourceID)",
		"await refreshSourceMetadataAndCounts(true)",
		"await reloadSourceScopeAfterSync(sourceID)",
		"首次同步完成后，文章会自动出现在这里",
		"订阅已保留并会稍后重试",
		"data-retry-source",
		"部分来源可用",
		"source.title_override || source.name",
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("reader subscription flow missing %q", expected)
		}
	}
}

func TestCuratedDiscoveryCatalogCoversEveryConcreteCategoryFlow(t *testing.T) {
	db = openTestDB(t)

	expectedCategories := []string{
		"social-media", "new-media", "traditional-media", "blog",
		"ai", "programming", "design", "live", "multimedia", "picture", "anime",
		"program-update", "shopping", "game", "reading", "journal", "finance",
	}
	for _, category := range expectedCategories {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE category=? AND recommended=1`, category).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Errorf("category %q has no curated recommendation", category)
		}
	}

	var silentlySubscribed int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM feed_subscriptions s
JOIN feed_sources f ON f.id=s.source_id
WHERE f.url IN (
    'https://about.fb.com/news/feed/',
    'https://www.smashingmagazine.com/feed/',
    'https://www.nasa.gov/news-release/feed/',
    'https://www.nasa.gov/feeds/podcasts/houston-we-have-a-podcast',
    'https://www.nasa.gov/feeds/iotd-feed/',
    'https://myanimelist.net/rss/news.xml',
    'https://github.blog/changelog/feed/',
    'https://blog.google/products/shopping/rss/',
    'https://blog.playstation.com/feed/',
    'https://www.gutenberg.org/cache/epub/feeds/today.rss',
    'https://www.sec.gov/news/pressreleases.rss'
)`).Scan(&silentlySubscribed); err != nil {
		t.Fatal(err)
	}
	if silentlySubscribed != 0 {
		t.Fatalf("curated recommendations were subscribed without user choice: %d", silentlySubscribed)
	}

	game := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?category=game", "")
	if game.Code != http.StatusOK || !strings.Contains(game.Body.String(), `"id":"playstation-blog"`) || !strings.Contains(game.Body.String(), `"category":"game"`) {
		t.Fatalf("game discovery status = %d, body = %s", game.Code, game.Body.String())
	}
}

func TestDiscoveryConfirmationPersistsPublicFeedFlow(t *testing.T) {
	db = openTestDB(t)

	pending := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", `{"input_url":"https://example.org/articles"}`)
	if pending.Code != http.StatusAccepted || !strings.Contains(pending.Body.String(), `"status":"pending"`) {
		t.Fatalf("discovery request status = %d, body = %s", pending.Code, pending.Body.String())
	}
	now := "2026-08-07T08:00:00Z"
	verified := FeedDiscovery{
		ID:            "discovery-example",
		InputURL:      "https://example.org/articles",
		NormalizedURL: "https://example.org/articles",
		FeedURL:       "https://example.org/feed.xml",
		Title:         "Example Feed",
		SiteURL:       "https://example.org/",
		FeedFormat:    "rss",
		Status:        "verified",
		DiscoveredAt:  now,
		Preview: []NewsItem{{
			ID: "preview-1", Title: "Preview item", URL: "https://example.org/one", Source: "Example Feed",
			SourceType: "rss", PublishedAt: now, SummaryText: "Summary", ContentFormat: "text", SafeHTML: "<p>Summary</p>",
		}},
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = upsertDiscoveriesTx(tx, []FeedDiscovery{verified}, now); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	list := callJSON(t, discoveryHandler, http.MethodGet, "/api/discoveries", "")
	if strings.Contains(list.Body.String(), `"status":"pending"`) || !strings.Contains(list.Body.String(), `"id":"discovery-example"`) {
		t.Fatalf("verified discovery did not replace pending item: %s", list.Body.String())
	}
	confirm := callJSON(t, discoveryHandler, http.MethodPatch, "/api/discoveries", `{"id":"discovery-example"}`)
	if confirm.Code != http.StatusOK || !strings.Contains(confirm.Body.String(), `"collection_mode":"app"`) || !strings.Contains(confirm.Body.String(), `"subscribed":true`) {
		t.Fatalf("confirm status = %d, body = %s", confirm.Code, confirm.Body.String())
	}
	var sources, subscriptions, news, requests int
	for query, target := range map[string]*int{
		`SELECT COUNT(*) FROM feed_sources WHERE id=?`:              &sources,
		`SELECT COUNT(*) FROM feed_subscriptions WHERE source_id=?`: &subscriptions,
		`SELECT COUNT(*) FROM news WHERE source_id=?`:               &news,
		`SELECT COUNT(*) FROM sync_requests WHERE source_id=?`:      &requests,
	} {
		if err := db.QueryRow(query, sourceIDForURL(verified.FeedURL)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if sources != 1 || subscriptions != 1 || news != 1 || requests != 1 {
		t.Fatalf("confirmed discovery state: sources=%d subscriptions=%d news=%d requests=%d", sources, subscriptions, news, requests)
	}
}

func TestDirectSourceCreateUsesURLDerivedLocalIdentityFlow(t *testing.T) {
	db = openTestDB(t)
	_, err := upsertFeedSource(sourceMutationRequest{
		Name: "Example Engineering", URL: "https://example.org/feed.xml", Kind: "rss",
	})
	if err != nil {
		t.Fatalf("source create error=%v", err)
	}
	var sources, catalogs, subscriptions, requests int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE url='https://example.org/feed.xml'`).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE catalog_origin='user-added-public-source'`).Scan(&catalogs); err != nil {
		t.Fatal(err)
	}
	sourceID := sourceIDForURL("https://example.org/feed.xml")
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_subscriptions WHERE source_id=?`, sourceID).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id=?`, sourceID).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if sources != 1 || catalogs != 1 || subscriptions != 0 || requests != 0 {
		t.Fatalf("created source state: sources=%d catalogs=%d subscriptions=%d requests=%d", sources, catalogs, subscriptions, requests)
	}
}

func TestDiscoveryInputClassificationAndKeywordSearchAreSideEffectFree(t *testing.T) {
	db = openTestDB(t)
	tests := []struct {
		name       string
		input      string
		kind       string
		normalized string
	}{
		{name: "keyword", input: "人工智能", kind: discoveryInputKeyword},
		{name: "website", input: "https://example.com/news", kind: discoveryInputWebsite, normalized: "https://example.com/news"},
		{name: "feed", input: "https://example.com/feed.xml", kind: discoveryInputFeed, normalized: "https://example.com/feed.xml"},
		{name: "rsshub shorthand", input: "rsshub:/github/trending/daily", kind: discoveryInputRSSHub, normalized: "https://rsshub.app/github/trending/daily"},
		{name: "rsshub url", input: "https://rsshub.app/github/trending/daily", kind: discoveryInputRSSHub, normalized: "https://rsshub.app/github/trending/daily"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := classifyDiscoveryInput(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Kind != tt.kind || tt.normalized != "" && result.Normalized != tt.normalized {
				t.Fatalf("classify %q = %#v", tt.input, result)
			}
		})
	}
	for _, input := range []string{"example.com/feed", "rsshub:/../private", "http://127.0.0.1/feed"} {
		if _, err := classifyDiscoveryInput(input); err == nil {
			t.Fatalf("unsafe or ambiguous input %q was accepted", input)
		}
	}

	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_discovery_candidates`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	search := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", `{"input":"AI"}`)
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"input_kind":"keyword"`) || !strings.Contains(search.Body.String(), `"id":"openai-news"`) || !strings.Contains(search.Body.String(), `"requires_agent":false`) {
		t.Fatalf("keyword search status = %d, body = %s", search.Code, search.Body.String())
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_discovery_candidates`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("keyword search wrote discovery rows: before=%d after=%d", before, after)
	}

	rsshub := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", `{"input":"rsshub:/github/trending/daily"}`)
	if rsshub.Code != http.StatusBadRequest || !strings.Contains(rsshub.Body.String(), "维护目录") {
		t.Fatalf("RSSHub request status = %d, body = %s", rsshub.Code, rsshub.Body.String())
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_discovery_candidates`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("URL discovery wrote rows: before=%d after=%d", before, after)
	}
}

func TestSourceCatalogFiltersAndPreviewUseOnlyLocalData(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-07T08:00:00Z"
	if _, err := db.Exec(`INSERT INTO news
(id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "video-1", "本地视频预览", "https://github.blog/video", "GitHub Blog", "github-blog", "test", "rss", "video", "", now, "摘要", "text", "", "https://example.com/cover.jpg", "https://example.com/video.mp4", "video/mp4", 60, now); err != nil {
		t.Fatal(err)
	}

	chinese := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?language=zh-cn&content_type=article", "")
	if chinese.Code != http.StatusOK || !strings.Contains(chinese.Body.String(), `"id":"sspai"`) || strings.Contains(chinese.Body.String(), `"id":"openai-news"`) {
		t.Fatalf("language filter status = %d, body = %s", chinese.Code, chinese.Body.String())
	}
	video := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?content_type=video&sort=activity", "")
	if video.Code != http.StatusOK || !strings.Contains(video.Body.String(), `"id":"github-blog"`) || !strings.Contains(video.Body.String(), `"popularity_available":false`) || !strings.Contains(video.Body.String(), `"popularity":0`) {
		t.Fatalf("content filter status = %d, body = %s", video.Code, video.Body.String())
	}
	invalid := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?content_type=unknown", "")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid content filter status = %d, body = %s", invalid.Code, invalid.Body.String())
	}

	preview := callJSON(t, sourcePreviewHandler, http.MethodGet, "/api/sources/preview?source_id=github-blog", "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"title":"本地视频预览"`) || !strings.Contains(preview.Body.String(), `"preview_origin":"local_sqlite"`) || !strings.Contains(preview.Body.String(), `"public_popularity_available":false`) {
		t.Fatalf("preview status = %d, body = %s", preview.Code, preview.Body.String())
	}
}

func TestReadLaterBulkAndViewModePersist(t *testing.T) {
	db = openTestDB(t)
	for _, key := range []string{"news:item-1", "evidence:item-2"} {
		result := callJSON(t, itemPreferenceHandler, http.MethodPatch, "/api/items", `{"key":"`+key+`","read_later":true,"reading_progress":0.5}`)
		if result.Code != http.StatusOK {
			t.Fatalf("item preference status = %d, body = %s", result.Code, result.Body.String())
		}
	}
	bulk := callJSON(t, bulkItemPreferenceHandler, http.MethodPatch, "/api/items/bulk", `{"keys":["news:item-1","evidence:item-2"],"read":true}`)
	if bulk.Code != http.StatusOK || !strings.Contains(bulk.Body.String(), `"count":2`) {
		t.Fatalf("bulk items status = %d, body = %s", bulk.Code, bulk.Body.String())
	}
	reader := callJSON(t, readerPreferenceHandler, http.MethodPatch, "/api/reader", `{"sidebar_width":280,"timeline_width":480,"view_mode":"compact"}`)
	if reader.Code != http.StatusOK {
		t.Fatalf("reader status = %d, body = %s", reader.Code, reader.Body.String())
	}
	items := callJSON(t, itemPreferenceHandler, http.MethodGet, "/api/items", "")
	if strings.Count(items.Body.String(), `"read":true`) != 2 || strings.Count(items.Body.String(), `"read_later":true`) != 2 || strings.Count(items.Body.String(), `"reading_progress":0.5`) != 2 {
		t.Fatalf("item preferences not persisted: %s", items.Body.String())
	}
	readerGet := callJSON(t, readerPreferenceHandler, http.MethodGet, "/api/reader", "")
	if !strings.Contains(readerGet.Body.String(), `"view_mode":"compact"`) {
		t.Fatalf("view mode not persisted: %s", readerGet.Body.String())
	}
}

func TestNewsPreferenceSurvivesDatabaseReopen(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })
	if err := os.MkdirAll("data", 0o755); err != nil {
		t.Fatal(err)
	}

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
	payload := `{"schema_version":1,"generated_at":"2026-08-06T12:00:00Z","source":{"kind":"profile_meetings","status":"ok"},"news":[{"id":"N-PERSIST","title":"持久化新闻","url":"https://example.com/persist","source":"测试源","source_type":"api","published_at":"2026-08-06T11:00:00Z","summary_text":"降级摘要","content_format":"markdown","safe_html":"<p>安全正文</p>"}]}`

	open()
	post := httptest.NewRecorder()
	dashboardHandler(post, httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewBufferString(payload)))
	if post.Code != http.StatusOK {
		t.Fatalf("initial POST status = %d, body = %s", post.Code, post.Body.String())
	}
	dismiss := httptest.NewRecorder()
	newsPreferenceHandler(dismiss, httptest.NewRequest(http.MethodDelete, "/api/news", bytes.NewBufferString(`{"id":"N-PERSIST"}`)))
	if dismiss.Code != http.StatusOK {
		t.Fatalf("dismiss status = %d, body = %s", dismiss.Code, dismiss.Body.String())
	}
	hideModule := httptest.NewRecorder()
	modulePreferenceHandler(hideModule, httptest.NewRequest(http.MethodPatch, "/api/modules", bytes.NewBufferString(`{"id":"news","hidden":true}`)))
	if hideModule.Code != http.StatusOK {
		t.Fatalf("hide module status = %d, body = %s", hideModule.Code, hideModule.Body.String())
	}
	resizeModule := httptest.NewRecorder()
	moduleLayoutHandler(resizeModule, httptest.NewRequest(http.MethodPatch, "/api/layout", bytes.NewBufferString(`{"id":"news","height_px":360,"span":2}`)))
	if resizeModule.Code != http.StatusOK {
		t.Fatalf("resize module status = %d, body = %s", resizeModule.Code, resizeModule.Body.String())
	}
	itemPatch := httptest.NewRecorder()
	itemPreferenceHandler(itemPatch, httptest.NewRequest(http.MethodPatch, "/api/items", bytes.NewBufferString(`{"key":"news:N-PERSIST","read":true,"starred":true}`)))
	if itemPatch.Code != http.StatusOK {
		t.Fatalf("item patch status = %d, body = %s", itemPatch.Code, itemPatch.Body.String())
	}
	readerPatch := httptest.NewRecorder()
	readerPreferenceHandler(readerPatch, httptest.NewRequest(http.MethodPatch, "/api/reader", bytes.NewBufferString(`{"sidebar_width":300,"timeline_width":560}`)))
	if readerPatch.Code != http.StatusOK {
		t.Fatalf("reader patch status = %d, body = %s", readerPatch.Code, readerPatch.Body.String())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	open()
	t.Cleanup(func() { _ = db.Close() })
	refresh := httptest.NewRecorder()
	dashboardHandler(refresh, httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewBufferString(payload)))
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh POST status = %d, body = %s", refresh.Code, refresh.Body.String())
	}
	get := httptest.NewRecorder()
	dashboardHandler(get, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if strings.Contains(get.Body.String(), `"id":"N-PERSIST"`) || !strings.Contains(get.Body.String(), `"hidden_modules":["news"]`) || !strings.Contains(get.Body.String(), `"news":{"height_px":360,"span":2}`) {
		t.Fatalf("dismissed news returned after reopen: %s", get.Body.String())
	}
	items := httptest.NewRecorder()
	itemPreferenceHandler(items, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if !strings.Contains(items.Body.String(), `"key":"news:N-PERSIST"`) || !strings.Contains(items.Body.String(), `"starred":true`) {
		t.Fatalf("item preference lost after reopen: %s", items.Body.String())
	}
	reader := httptest.NewRecorder()
	readerPreferenceHandler(reader, httptest.NewRequest(http.MethodGet, "/api/reader", nil))
	if !strings.Contains(reader.Body.String(), `"sidebar_width":300`) || !strings.Contains(reader.Body.String(), `"timeline_width":560`) {
		t.Fatalf("reader preference lost after reopen: %s", reader.Body.String())
	}
}

// 中间态 074 修复（2026-08-21 设备实测）：测试期中间包把 schema_version
// 标到 74 但只落了部分列，完整版被版本号跳过导致阅读器 500。修复器必须
// 幂等补齐列/表并订正数据。
func TestMigrateRepairsPartial074StateFlow(t *testing.T) {
	for _, recordedVersion := range []int{74, 75} {
		t.Run(fmt.Sprintf("schema-%d", recordedVersion), func(t *testing.T) {
			conn := openUpgradeTestDB(t, "074_")
			if _, err := conn.Exec(`CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`INSERT INTO schema_version(version, applied_at) VALUES (?, '2026-08-21T03:51:16Z')`, recordedVersion); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`UPDATE feed_sources SET collection_mode='app',url='source-gateway://wechat-geekpark' WHERE id='wechat-geekpark'`); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('wechat-geekpark','pending','2026-08-21T03:51:16Z')`); err != nil {
				t.Fatal(err)
			}

			if err := migrate(conn); err != nil {
				t.Fatalf("migrate failed on partial-074 state: %v", err)
			}

			for _, col := range []string{"gateway_status", "gateway_cursor", "gateway_has_more", "gateway_enabled"} {
				var n int
				if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('feed_sources') WHERE name=?`, col).Scan(&n); err != nil || n != 1 {
					t.Fatalf("feed_sources.%s missing after repair (n=%d, err=%v)", col, n, err)
				}
			}
			var n int
			if err := conn.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('news') WHERE name='gateway_entry_id'`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("news.gateway_entry_id missing (n=%d, err=%v)", n, err)
			}
			if err := conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='source_gateway_subscription_outbox'`).Scan(&n); err != nil || n != 1 {
				t.Fatalf("outbox table missing (n=%d, err=%v)", n, err)
			}
			var enabled int
			var mode, sourceURL string
			if err := conn.QueryRow(`SELECT gateway_enabled,collection_mode,url FROM feed_sources WHERE id='wechat-geekpark'`).Scan(&enabled, &mode, &sourceURL); err != nil || enabled != 1 || mode != "source_gateway" || sourceURL != "source-gateway://wechat-geekpark" {
				t.Fatalf("gateway_enabled=%d mode=%q url=%q err=%v", enabled, mode, sourceURL, err)
			}
			var st string
			if err := conn.QueryRow(`SELECT status FROM sync_requests WHERE source_id='wechat-geekpark' ORDER BY id DESC LIMIT 1`).Scan(&st); err != nil || st != "superseded" {
				t.Fatalf("sync_request status=%q err=%v", st, err)
			}
			if err := migrate(conn); err != nil {
				t.Fatalf("second migrate not idempotent: %v", err)
			}
		})
	}
}

func TestSourceGatewayStartupReconcilePreservesSyncStateFlow(t *testing.T) {
	conn := openTestDB(t)
	if err := reconcileSourceGatewaySchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_sources SET
		sync_interval_seconds=777,
		next_sync_at='2999-01-01T00:00:00Z',
		last_error='gateway timeout',
		gateway_status='degraded',
		gateway_cursor='cursor-final',
		gateway_has_more=0,
		etag='gateway-etag',
		last_modified='gateway-last-modified',
		updated_at='2026-08-22T00:00:00Z'
		WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := reconcileSourceGatewaySchema(conn); err != nil {
			t.Fatalf("reconcile attempt %d: %v", attempt+1, err)
		}
	}

	var mode, sourceURL, nextSync, lastError, gatewayStatus, cursor, etag, lastModified, updatedAt string
	var interval, hasMore, enabled int
	if err := conn.QueryRow(`SELECT collection_mode,url,sync_interval_seconds,next_sync_at,last_error,
		gateway_status,gateway_cursor,gateway_has_more,gateway_enabled,etag,last_modified,updated_at
		FROM feed_sources WHERE id='youtube-anthropic'`).Scan(
		&mode, &sourceURL, &interval, &nextSync, &lastError, &gatewayStatus, &cursor,
		&hasMore, &enabled, &etag, &lastModified, &updatedAt,
	); err != nil {
		t.Fatal(err)
	}
	if mode != "source_gateway" || sourceURL != "source-gateway://youtube-anthropic" || enabled != 1 {
		t.Fatalf("ownership changed: mode=%q url=%q enabled=%d", mode, sourceURL, enabled)
	}
	if interval != 777 || nextSync != "2999-01-01T00:00:00Z" || lastError != "gateway timeout" ||
		gatewayStatus != "degraded" || cursor != "cursor-final" || hasMore != 0 ||
		etag != "gateway-etag" || lastModified != "gateway-last-modified" || updatedAt != "2026-08-22T00:00:00Z" {
		t.Fatalf("startup reset gateway state: interval=%d next=%q error=%q status=%q cursor=%q has_more=%d etag=%q modified=%q updated=%q",
			interval, nextSync, lastError, gatewayStatus, cursor, hasMore, etag, lastModified, updatedAt)
	}
}

func TestDeviceMeetingAudioReferenceValidationUnit(t *testing.T) {
	for _, ref := range []string{
		"_zettlab/recordings/meeting-1/audio",
		"_zettlab/device-data/v1/meetings/meeting-1/audio",
		"/_zettlab/device-data/v1/meetings/meeting-1/audio",
	} {
		if !validAudioRef(ref) {
			t.Fatalf("expected valid audio ref %q", ref)
		}
	}
	for _, ref := range []string{
		"_zettlab/device-data/v1/meetings/../audio",
		"_zettlab/device-data/v1/meetings/meeting-1/audio?token=x",
		"_zettlab/device-data/v1/meetings/a/b/audio",
	} {
		if validAudioRef(ref) {
			t.Fatalf("expected invalid audio ref %q", ref)
		}
	}
}

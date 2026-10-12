package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func insertMaintenanceSource(t *testing.T, conn *sql.DB, id string, sortOrder int) {
	t.Helper()
	now := "2026-08-11T08:00:00Z"
	_, err := conn.Exec(`INSERT OR REPLACE INTO feed_sources
(id,kind,name,url,description,is_default,status,records_seen,records_used,last_sync_at,last_error,created_at,updated_at,
 collection_mode,sync_interval_seconds,next_sync_at,backfill_days,retention_days,retention_limit,auto_extract,hidden,etag,last_modified)
VALUES(?,?,?,?,?,0,'ok',0,0,?,'',?,?, 'agent',21600,'',30,90,1000,1,0,'','')`,
		id, "rss", "来源 "+id, "https://example.com/"+id, "测试来源", now, now, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`INSERT OR REPLACE INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at,folder_id,sort_order) VALUES(?,1,?,?, '',?)`, id, now, now, sortOrder)
	if err != nil {
		t.Fatal(err)
	}
}

func insertMaintenanceEntry(t *testing.T, conn *sql.DB, sourceID, id string, index int) string {
	t.Helper()
	hash := sha256.Sum256([]byte(sourceID + "\x00" + id))
	contentHash := hex.EncodeToString(hash[:])
	now := "2026-08-11T08:00:00Z"
	_, err := conn.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,
 content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES(?,?,?,?,?,?,?,'rss','article','',?,?, 'text','',?,?, '', 'official', '',?)`,
		id, id, "文章 "+id, "https://example.com/article/"+id, "来源 "+sourceID, sourceID, "app", now, "摘要 "+id, contentHash, now, now)
	if err != nil {
		t.Fatal(err)
	}
	return contentHash
}

func TestReaderMaintenanceReadIsBoundedAndDeduplicatedFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:maintenance-read", "maintenance-read")
	for index := 0; index < maintenanceReadSourceLimit+1; index++ {
		sourceID := "maintenance-source-" + string(rune('a'+index))
		insertMaintenanceSource(t, db, sourceID, index)
		for entry := 0; entry < maintenanceReadEntryLimit; entry++ {
			hash := insertMaintenanceEntry(t, db, sourceID, sourceID+"-entry-"+string(rune('0'+entry)), entry)
			if index == 0 && entry == 0 {
				_, err := db.Exec(`INSERT INTO reader_digest_receipts(content_hash,source_id,entry_id,processed_at,outcome) VALUES(?,?,?,?,?)`, hash, sourceID, sourceID+"-entry-0", "2026-08-10T08:00:00Z", "drafted")
				if err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	var output bytes.Buffer
	if err := runReaderMaintenanceRead(&output); err != nil {
		t.Fatal(err)
	}
	var result readerMaintenanceReadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != maintenanceReadSourceLimit {
		t.Fatalf("source limit = %d", len(result.Sources))
	}
	if len(result.Entries) > maintenanceReadTotalLimit {
		t.Fatalf("entry limit = %d", len(result.Entries))
	}
	if len(result.Receipts) > maintenanceReadTotalLimit {
		t.Fatalf("receipt limit = %d", len(result.Receipts))
	}
	for _, source := range result.Sources {
		if source.SourceURL == "" || strings.Contains(output.String(), `"url"`) {
			t.Fatalf("transport source URL projection is unsafe: %s", output.String())
		}
	}
	for _, entry := range result.Entries {
		if entry.ContentHash == strings.Repeat("0", 64) {
			t.Fatal("unexpected empty content hash")
		}
	}
	for _, entry := range result.Entries {
		if entry.SourceID == "maintenance-source-a" && entry.ID == "maintenance-source-a-entry-0" {
			t.Fatal("receipt-deduplicated entry was returned")
		}
	}
}

func TestReaderMaintenanceArticleOverviewCandidatesUseFullTextAndBoundedFallback(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:maintenance-overviews", "maintenance-overviews")
	sourceID := "maintenance-overview-source"
	insertMaintenanceSource(t, db, sourceID, 1)
	fullTextID := "maintenance-overview-full"
	fullTextHash := insertMaintenanceEntry(t, db, sourceID, fullTextID, 1)
	fullText := strings.Repeat("全文优先内容", 1000)
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash) VALUES(?,?,?,?,?,'',?,?,?)`, fullTextID, fullText, "text", "2026-08-11T08:00:00Z", "ok", "https://example.com/full", "2026-08-11T08:00:00Z", fullTextHash); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maintenanceOverviewLimit+1; index++ {
		insertMaintenanceEntry(t, db, sourceID, fmt.Sprintf("maintenance-overview-summary-%d", index), index+2)
	}
	var output bytes.Buffer
	if err := runReaderMaintenanceRead(&output); err != nil {
		t.Fatal(err)
	}
	var result readerMaintenanceReadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ArticleAIOverviewCandidates) != maintenanceOverviewLimit {
		t.Fatalf("overview candidates = %d, want %d", len(result.ArticleAIOverviewCandidates), maintenanceOverviewLimit)
	}
	var found bool
	for _, candidate := range result.ArticleAIOverviewCandidates {
		if len([]rune(candidate.Excerpt)) > maintenanceOverviewExcerptRunes {
			t.Fatalf("overview excerpt length = %d", len([]rune(candidate.Excerpt)))
		}
		if candidate.EntryID == fullTextID {
			found = true
			if candidate.ContentHash != fullTextHash || candidate.Excerpt != string([]rune(fullText)[:maintenanceOverviewExcerptRunes]) {
				t.Fatalf("full text candidate = %#v", candidate)
			}
		}
	}
	if !found {
		t.Fatal("full text overview candidate missing")
	}
	if _, err := db.Exec(`UPDATE app_settings SET auto_generate_article_overviews=0 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runReaderMaintenanceRead(&output); err != nil {
		t.Fatal(err)
	}
	result = readerMaintenanceReadResult{}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ArticleAIOverviewCandidates) != 0 {
		t.Fatalf("disabled overview candidates = %#v", result.ArticleAIOverviewCandidates)
	}
}

func TestReaderMaintenanceApplyStoresArticleOverviewForCurrentContentFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:maintenance-overview-apply", "maintenance-overview-apply")
	sourceID := "maintenance-overview-apply-source"
	entryID := "maintenance-overview-apply-entry"
	insertMaintenanceSource(t, db, sourceID, 1)
	contentHash := insertMaintenanceEntry(t, db, sourceID, entryID, 1)
	overview := ArticleAIOverview{EntryID: entryID, ContentHash: contentHash, Status: "ready", Summary: "文章要点", KeyPoints: []string{"要点一", "要点二"}, Impact: "需要跟进", GeneratedAt: "2026-08-11T10:00:00Z"}
	payload, err := json.Marshal(Dashboard{SchemaVersion: 1, GeneratedAt: "2026-08-11T10:00:00Z", Source: SourceStatus{Kind: "local_sources", Status: "ok"}, Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{}, ArticleAIOverviews: []ArticleAIOverview{overview}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApply(bytes.NewReader(payload), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	entry := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id="+sourceID+"&limit=1", "")
	if entry.Code != http.StatusOK || !strings.Contains(entry.Body.String(), `"ai_overview":{"entry_id":"`+entryID+`"`) || !strings.Contains(entry.Body.String(), `"status":"ready"`) {
		t.Fatalf("entry overview projection = %d %s", entry.Code, entry.Body.String())
	}
	dashboard := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if dashboard.Code != http.StatusOK || !strings.Contains(dashboard.Body.String(), `"ai_overview":{"entry_id":"`+entryID+`"`) {
		t.Fatalf("dashboard overview projection = %d %s", dashboard.Code, dashboard.Body.String())
	}
	bad := overview
	bad.ContentHash = strings.Repeat("0", 64)
	badPayload, err := json.Marshal(Dashboard{SchemaVersion: 1, GeneratedAt: "2026-08-11T10:01:00Z", Source: SourceStatus{Kind: "local_sources", Status: "ok"}, Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{}, ArticleAIOverviews: []ArticleAIOverview{bad}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApply(bytes.NewReader(badPayload), &bytes.Buffer{}); err == nil {
		t.Fatal("maintenance accepted stale overview hash")
	}
	updatedHashBytes := sha256.Sum256([]byte("updated article body"))
	updatedHash := hex.EncodeToString(updatedHashBytes[:])
	if _, err := db.Exec(`UPDATE news SET content_hash=?,summary_text=?,updated_at=? WHERE id=?`, updatedHash, "更新后的文章摘要", "2026-08-11T10:02:00Z", entryID); err != nil {
		t.Fatal(err)
	}
	var readOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&readOutput); err != nil {
		t.Fatal(err)
	}
	var readResult readerMaintenanceReadResult
	if err := json.Unmarshal(readOutput.Bytes(), &readResult); err != nil {
		t.Fatal(err)
	}
	var regenerated bool
	for _, candidate := range readResult.ArticleAIOverviewCandidates {
		if candidate.EntryID == entryID && candidate.ContentHash == updatedHash {
			regenerated = true
			break
		}
	}
	if !regenerated {
		t.Fatalf("changed article did not re-enter overview candidates: %#v", readResult.ArticleAIOverviewCandidates)
	}
}

func TestReaderMaintenanceApplyWritesDraftOnlyFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:maintenance-apply", "maintenance-apply")
	sourceID := "maintenance-apply-source"
	insertMaintenanceSource(t, db, sourceID, 1)
	entryID := "maintenance-apply-entry"
	contentHash := insertMaintenanceEntry(t, db, sourceID, entryID, 1)
	payload, err := json.Marshal(Dashboard{
		SchemaVersion: 1,
		GeneratedAt:   "2026-08-11T09:00:00Z",
		Source:        SourceStatus{Kind: "local_sources", Status: "ok", RecordsSeen: 1, RecordsUsed: 1},
		Evidence:      []Evidence{{ID: entryID, Title: "文章证据", Locator: "", ObservedAt: "2026-08-11T08:00:00Z", Quote: "摘要", SourceType: "rss"}},
		Actions:       []Action{{ID: "maintenance-action", Task: "跟进文章", Owner: "待确认", SourceIDs: []string{entryID}, Status: "draft", ConfirmationStatus: "draft", Priority: "medium"}},
		Signals:       []Signal{{ID: "maintenance-signal", Title: "市场变化", Detail: "需要观察", SourceIDs: []string{entryID}, Confidence: "medium", Status: "draft"}},
		Decisions:     []Decision{{ID: "maintenance-decision", Question: "是否跟进", Options: []string{"是", "否"}, Recommendation: "待确认", SourceIDs: []string{entryID}, Status: "draft"}},
		ReaderDigests: []ReaderDigestReceipt{{ContentHash: contentHash, SourceID: sourceID, EntryID: entryID, ProcessedAt: "2026-08-11T09:00:00Z", Outcome: "drafted"}},
		MorningBrief:  &MorningBrief{ID: "brief-2026-08-11", Title: "CEO Morning Brief", GeneratedAt: "2026-08-11T09:00:00Z", Summary: "优先确认交付范围。", Priorities: []string{"确认交付范围"}, Risks: []string{"来源部分不可用"}, SourceIDs: []string{entryID}, Status: "partial"},
		Metrics:       []Metric{}, Projects: []Project{}, News: []NewsItem{}, Recordings: []Recording{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runReaderMaintenanceApply(bytes.NewReader(payload), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"ok":true`) {
		t.Fatalf("apply output = %s", output.String())
	}
	get := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"confirmation_status":"draft"`) || !strings.Contains(get.Body.String(), `"schedule_state":"none"`) || !strings.Contains(get.Body.String(), `"morning_brief"`) {
		t.Fatalf("draft projection missing: %s", get.Body.String())
	}
	var contextRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE owner_id=? AND profile_id=?`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&contextRows); err != nil {
		t.Fatal(err)
	}
	if contextRows != 0 {
		t.Fatalf("unconfirmed maintenance wrote stable context: %d", contextRows)
	}
	bad := bytes.NewBufferString(`{"schema_version":1,"generated_at":"2026-08-11T09:00:00Z","source":{"kind":"local_sources","status":"ok"},"hidden_modules":["news"],"evidence":[],"actions":[],"metrics":[],"projects":[],"signals":[],"news":[],"decisions":[]}`)
	if err := runReaderMaintenanceApply(bad, &bytes.Buffer{}); err == nil {
		t.Fatal("maintenance accepted a module mutation")
	}
}

func TestReaderDigestAppDataApplyReplaysAtomicallyFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:maintenance-app-data", "maintenance-app-data")
	sourceID := "maintenance-app-data-source"
	insertMaintenanceSource(t, db, sourceID, 1)
	entryID := "maintenance-app-data-entry"
	contentHash := insertMaintenanceEntry(t, db, sourceID, entryID, 1)
	payload, err := json.Marshal(Dashboard{
		SchemaVersion: 1,
		GeneratedAt:   "2026-08-11T10:00:00Z",
		Source:        SourceStatus{Kind: "local_sources", Status: "ok", RecordsSeen: 1, RecordsUsed: 1},
		Evidence:      []Evidence{{ID: entryID, Title: "文章证据", ObservedAt: "2026-08-11T08:00:00Z", Quote: "摘要", SourceType: "rss"}},
		Actions:       []Action{{ID: "maintenance-app-data-action", Task: "跟进文章", Owner: "待确认", SourceIDs: []string{entryID}, Status: "draft", ConfirmationStatus: "draft", Priority: "medium"}},
		Signals:       []Signal{},
		Decisions:     []Decision{},
		ReaderDigests: []ReaderDigestReceipt{{ContentHash: contentHash, SourceID: sourceID, EntryID: entryID, ProcessedAt: "2026-08-11T10:00:00Z", Outcome: "drafted"}},
		Metrics:       []Metric{}, Projects: []Project{}, News: []NewsItem{}, Recordings: []Recording{},
	})
	if err != nil {
		t.Fatal(err)
	}
	first := appDataCall(t, "/api/app-data/v1/operations/reader_digest.apply", string(payload), "reader-digest-apply-1")
	if first.Code != http.StatusOK {
		t.Fatalf("first apply status=%d body=%s", first.Code, first.Body.String())
	}
	var genericRows, receipts, dashboardRuns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_data_mutation_requests`).Scan(&genericRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_digest_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs`).Scan(&dashboardRuns); err != nil {
		t.Fatal(err)
	}
	if genericRows != 0 || receipts != 1 || dashboardRuns != 1 {
		t.Fatalf("first apply rows generic=%d receipts=%d dashboard_runs=%d", genericRows, receipts, dashboardRuns)
	}
	replay := appDataCall(t, "/api/app-data/v1/operations/reader_digest.apply", string(payload), "reader-digest-apply-1")
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" {
		t.Fatalf("replay status=%d header=%q body_equal=%v", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String() == first.Body.String())
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_digest_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs`).Scan(&dashboardRuns); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || dashboardRuns != 1 {
		t.Fatalf("replay duplicated rows receipts=%d dashboard_runs=%d", receipts, dashboardRuns)
	}
	conflictPayload := strings.Replace(string(payload), "2026-08-11T10:00:00Z", "2026-08-11T10:01:00Z", 1)
	conflict := appDataCall(t, "/api/app-data/v1/operations/reader_digest.apply", conflictPayload, "reader-digest-apply-1")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestReaderFocusMaintenanceUsesBootstrapBudgetAndAppliesFiveItemsFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:focus-maintenance", "focus-maintenance")
	for sourceIndex := 0; sourceIndex < 2; sourceIndex++ {
		sourceID := fmt.Sprintf("focus-maintenance-source-%d", sourceIndex)
		insertMaintenanceSource(t, db, sourceID, sourceIndex)
		for entryIndex := 0; entryIndex < 6; entryIndex++ {
			entryID := fmt.Sprintf("%s-entry-%d", sourceID, entryIndex)
			contentHash := insertMaintenanceEntry(t, db, sourceID, entryID, entryIndex)
			if _, err := db.Exec(`UPDATE news SET title=?,summary_text=? WHERE id=?`, "AI Computer 更新 "+entryID, "AI Computer 相关摘要 "+entryID, entryID); err != nil {
				t.Fatal(err)
			}
			if contentHash == "" {
				t.Fatal("test entry has no content hash")
			}
		}
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := runReaderMaintenanceRead(&output); err != nil {
		t.Fatal(err)
	}
	var result readerMaintenanceReadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ReaderFocus == nil || result.ReaderRecommendations == nil {
		t.Fatalf("focus projections missing: %#v", result)
	}
	if result.ReaderFocusClaim == nil || result.ReaderFocusClaim.Status != "claimed" || result.ReaderFocusClaim.ID == "" {
		t.Fatalf("focus claim missing: %#v", result.ReaderFocusClaim)
	}
	if len(result.ArticleAIOverviewCandidates) != 5 || len(result.Entries) < 1 || len(result.Entries) > 5 {
		t.Fatalf("bootstrap bounds candidates=%d entries=%d", len(result.ArticleAIOverviewCandidates), len(result.Entries))
	}
	for _, candidate := range result.ArticleAIOverviewCandidates {
		if !strings.Contains(candidate.Title, "AI Computer") {
			t.Fatalf("non-matching overview candidate: %#v", candidate)
		}
	}

	overviews := make([]ArticleAIOverview, 0, 5)
	for _, candidate := range result.ArticleAIOverviewCandidates {
		overviews = append(overviews, ArticleAIOverview{
			EntryID: candidate.EntryID, ContentHash: candidate.ContentHash, Status: "ready",
			Summary: "AI Computer 概览", KeyPoints: []string{"重点"}, Impact: "影响", GeneratedAt: "2026-08-14T12:00:00Z",
		})
	}
	payload, err := json.Marshal(Dashboard{
		SchemaVersion: 1, GeneratedAt: "2026-08-14T12:00:00Z",
		Source:   SourceStatus{Kind: "local_sources", Status: "ok", RecordsSeen: 5, RecordsUsed: 5},
		Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{}, ArticleAIOverviews: overviews,
		ReaderFocusClaimID: result.ReaderFocusClaim.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "focus-bootstrap-five"); err != nil {
		t.Fatalf("five-item focus apply failed: %v", err)
	}
	var processed, runCount int
	if err := db.QueryRow(`SELECT COALESCE(SUM(processed_count),0), COUNT(*) FROM reader_focus_runs WHERE status IN ('completed','partial') AND idempotency_key NOT LIKE 'reader-focus-claim:%'`).Scan(&processed, &runCount); err != nil {
		t.Fatal(err)
	}
	if processed != 5 || runCount != 1 {
		t.Fatalf("focus ledger processed=%d runs=%d", processed, runCount)
	}
	var bootstrapStatus, bootstrapMode string
	if err := db.QueryRow(`SELECT bootstrap_status,bootstrap_mode FROM reader_focus_topics LIMIT 1`).Scan(&bootstrapStatus, &bootstrapMode); err != nil {
		t.Fatal(err)
	}
	completedState, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if bootstrapStatus != readerFocusBootstrapDone || bootstrapMode != "future_only" || completedState.Policy.Mode != readerFocusModeAutoNew {
		t.Fatalf("bootstrap completion status=%q mode=%q policy=%q, want done/future_only/auto_new", bootstrapStatus, bootstrapMode, completedState.Policy.Mode)
	}
	// A transport replay is allowed only with the original idempotency key and
	// payload fingerprint after the budget is exhausted.
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "focus-bootstrap-five"); err != nil {
		t.Fatalf("focus replay failed: %v", err)
	}
}

func setupReaderFocusMaintenanceReadFixture(t *testing.T, entryCount int) ReaderFocusState {
	t.Helper()
	if _, err := db.Exec(`UPDATE app_settings SET auto_generate_article_overviews=1 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	sourceID := "focus-budget-read-source"
	insertMaintenanceSource(t, db, sourceID, 0)
	for index := 0; index < entryCount; index++ {
		entryID := fmt.Sprintf("focus-budget-read-entry-%d", index)
		insertMaintenanceEntry(t, db, sourceID, entryID, index)
		if _, err := db.Exec(`UPDATE news SET title=?,summary_text=? WHERE id=?`, "AI Computer 更新 "+entryID, "AI Computer 摘要 "+entryID, entryID); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{TopicID: state.Topics[0].ID, Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if len(readerFocusMaintenanceTopics(state)) != 1 {
		t.Fatalf("focus maintenance topic is not active: %#v", state)
	}
	return state
}

func TestReaderFocusMaintenanceEntriesOnlyFollowBudgetedCandidatesFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:focus-budget-read", "focus-budget-read")
	state := setupReaderFocusMaintenanceReadFixture(t, 3)

	var firstOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&firstOutput); err != nil {
		t.Fatal(err)
	}
	var first readerMaintenanceReadResult
	if err := json.Unmarshal(firstOutput.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.ArticleAIOverviewCandidates) == 0 || len(first.Entries) != len(first.ArticleAIOverviewCandidates) {
		t.Fatalf("budgeted focus payload candidates=%d entries=%d", len(first.ArticleAIOverviewCandidates), len(first.Entries))
	}
	wanted := make(map[string]struct{}, len(first.ArticleAIOverviewCandidates))
	for _, candidate := range first.ArticleAIOverviewCandidates {
		wanted[candidate.EntryID+"\x00"+candidate.ContentHash] = struct{}{}
	}
	for _, entry := range first.Entries {
		if _, ok := wanted[entry.ID+"\x00"+entry.ContentHash]; !ok {
			t.Fatalf("entry bypassed candidate gate: %#v", entry)
		}
	}

	// Exhaust the token budget without exhausting the article/bootstrap quota.
	// The next read must not leak the broader unread set through Entries.
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE reader_focus_policy SET daily_token_budget=1000 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	topicIDsJSON, err := json.Marshal([]string{state.Topics[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "budget-exhausted", "daily", readerFocusToday(), string(topicIDsJSON), 1, 1, 500, 500, "completed", "budget-exhausted-key", now, now); err != nil {
		t.Fatal(err)
	}
	var exhaustedOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&exhaustedOutput); err != nil {
		t.Fatal(err)
	}
	var exhausted readerMaintenanceReadResult
	if err := json.Unmarshal(exhaustedOutput.Bytes(), &exhausted); err != nil {
		t.Fatal(err)
	}
	if len(exhausted.ArticleAIOverviewCandidates) != 0 || len(exhausted.Entries) != 0 {
		t.Fatalf("token-exhausted focus payload candidates=%d entries=%d", len(exhausted.ArticleAIOverviewCandidates), len(exhausted.Entries))
	}
}

func TestReaderFocusMaintenanceEntriesEmptyWhenAutomaticOverviewDisabledFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:focus-auto-disabled", "focus-auto-disabled")
	setupReaderFocusMaintenanceReadFixture(t, 2)
	if _, err := db.Exec(`UPDATE app_settings SET auto_generate_article_overviews=0 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runReaderMaintenanceRead(&output); err != nil {
		t.Fatal(err)
	}
	var result readerMaintenanceReadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.ArticleAIOverviewCandidates) != 0 || len(result.Entries) != 0 {
		t.Fatalf("automatic overview disabled focus payload candidates=%d entries=%d", len(result.ArticleAIOverviewCandidates), len(result.Entries))
	}
}

func TestReaderFocusMaintenanceEntriesForOverviewCandidatesEmptyUnit(t *testing.T) {
	items, err := readFocusedMaintenanceEntriesForOverviewCandidates(ReaderFocusState{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if items == nil || len(items) != 0 {
		t.Fatalf("empty candidate projection = %#v, want non-nil empty slice", items)
	}
}

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func insertArticleOverviewFixture(t *testing.T, conn *sql.DB, id string) string {
	t.Helper()
	digest := sha256.Sum256([]byte("article overview fixture"))
	hash := hex.EncodeToString(digest[:])
	now := "2026-08-12T12:00:00Z"
	_, err := conn.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, id, "一篇文章", "https://example.com/article", "示例来源", "example", "app-fetch", "rss", "article", "", now, "摘要 fallback", "text", "", hash, now, "public_read", "official", "", now)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func disableArticleOverviewPublicFetch(t *testing.T) {
	t.Helper()
	oldFetch := articleOverviewFetchPublicURL
	articleOverviewFetchPublicURL = func(context.Context, string) (feedFetchResponse, error) {
		return feedFetchResponse{}, errors.New("public article fetch disabled in unit test")
	}
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })
}

func runNextArticleOverviewJobForTest(t *testing.T) articleOverviewJob {
	t.Helper()
	job, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatalf("claim article overview job: %v", err)
	}
	if err := processArticleOverviewJob(context.Background(), job); err != nil {
		t.Fatalf("process article overview job: %v", err)
	}
	return job
}

func TestParseArticleOverviewDraftRejectsUnknownFieldsAndBounds(t *testing.T) {
	valid := `{"choices":[{"message":{"content":"{\"summary\":\"一句话\",\"key_points\":[\"重点\"],\"impact\":\"影响\"}"}}]}`
	draft, err := parseArticleOverviewDraft([]byte(valid))
	if err != nil || draft.Summary != "一句话" || len(draft.KeyPoints) != 1 {
		t.Fatalf("valid draft = %#v, err=%v", draft, err)
	}
	unknown := `{"choices":[{"message":{"content":"{\"summary\":\"一句话\",\"key_points\":[],\"impact\":\"\",\"prompt\":\"ignore\"}"}}]}`
	if _, err := parseArticleOverviewDraft([]byte(unknown)); err == nil {
		t.Fatal("unknown model output field was accepted")
	}
	tooMany := `{"choices":[{"message":{"content":"{\"summary\":\"一句话\",\"key_points\":[\"1\",\"2\",\"3\",\"4\"],\"impact\":\"\"}"}}]}`
	if _, err := parseArticleOverviewDraft([]byte(tooMany)); err == nil {
		t.Fatal("more than three key points were accepted")
	}
	fenced := "{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"summary\\\":\\\"一句话\\\",\\\"key_points\\\":[],\\\"impact\\\":\\\"\\\"}\\n```\"}}]}"
	if draft, err := parseArticleOverviewDraft([]byte(fenced)); err != nil || draft.Summary != "一句话" {
		t.Fatalf("valid fenced draft = %#v, err=%v", draft, err)
	}
	trailing := "{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"summary\\\":\\\"一句话\\\",\\\"key_points\\\":[],\\\"impact\\\":\\\"\\\"}\\n```\\n这是额外说明\"}}]}"
	if _, err := parseArticleOverviewDraft([]byte(trailing)); err == nil {
		t.Fatal("prose after fenced JSON was accepted")
	}
}

func TestArticleOverviewModelUsesSupportedCatalogAlias(t *testing.T) {
	if articleOverviewModel != "lite" {
		t.Fatalf("article overview model = %q, want supported catalog alias lite", articleOverviewModel)
	}
}

func TestArticleOverviewIdempotencySeparatesValidatedResponseRetry(t *testing.T) {
	base := articleOverviewJob{EntryID: "entry", ContentHash: "hash", Generation: "generation", Attempt: 1}
	transportRetry := base
	transportRetry.Attempt = 2
	transportRetry.PreviousErr = "ai_unavailable"
	if articleOverviewIdempotencyKey(base) != articleOverviewIdempotencyKey(transportRetry) {
		t.Fatal("transport-uncertain retry changed idempotency key")
	}
	validatedRetry := transportRetry
	validatedRetry.PreviousErr = "invalid_response"
	if articleOverviewIdempotencyKey(base) == articleOverviewIdempotencyKey(validatedRetry) {
		t.Fatal("validated malformed response retry reused cached completion")
	}
}

func TestArticleAIOverviewHandlerValidatesAndStoresReadyResult(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	hash := insertArticleOverviewFixture(t, db, "overview-entry")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat" || r.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected proxy request path=%s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"文章核心\",\"key_points\":[\"重点一\"],\"impact\":\"影响判断\"}"}}]}`))
	}))
	defer proxy.Close()
	oldEndpoint, oldClient := articleOverviewProxyEndpoint, articleOverviewHTTPClient
	articleOverviewProxyEndpoint = proxy.URL + "/chat"
	articleOverviewHTTPClient = proxy.Client()
	t.Cleanup(func() { articleOverviewProxyEndpoint, articleOverviewHTTPClient = oldEndpoint, oldClient })

	result := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"overview-entry"}`)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"status":"pending"`) || !strings.Contains(result.Body.String(), `"stage":"queued"`) {
		t.Fatalf("overview response = %d %s", result.Code, result.Body.String())
	}
	runNextArticleOverviewJobForTest(t)
	ready := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"overview-entry"}`)
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"status":"ready"`) || !strings.Contains(ready.Body.String(), "文章核心") {
		t.Fatalf("ready overview response = %d %s", ready.Code, ready.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT content_hash FROM article_ai_overviews WHERE entry_id=?`, "overview-entry").Scan(&stored); err != nil || stored != hash {
		t.Fatalf("stored overview hash=%q err=%v", stored, err)
	}
	unknown := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"overview-entry","url":"https://bad.example"}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown request field status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestArticleAIOverviewRequestUsesSupportedCatalogAliasFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	contentHash := insertArticleOverviewFixture(t, db, "overview-contract-entry")
	var got map[string]any
	var gotIdempotencyKey string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdempotencyKey = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"完成\",\"key_points\":[],\"impact\":\"\"}"}}]}`))
	}))
	defer proxy.Close()
	oldEndpoint, oldClient := articleOverviewProxyEndpoint, articleOverviewHTTPClient
	articleOverviewProxyEndpoint = proxy.URL + "/chat"
	articleOverviewHTTPClient = proxy.Client()
	t.Cleanup(func() { articleOverviewProxyEndpoint, articleOverviewHTTPClient = oldEndpoint, oldClient })

	result := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"overview-contract-entry"}`)
	if result.Code != http.StatusOK {
		t.Fatalf("overview response = %d %s", result.Code, result.Body.String())
	}
	job := runNextArticleOverviewJobForTest(t)
	if got["model"] != "lite" {
		t.Fatalf("model = %v, want supported catalog alias lite", got["model"])
	}
	if _, ok := got["temperature"]; ok {
		t.Fatal("dashboard must not force a provider-specific temperature")
	}
	format, ok := got["response_format"].(map[string]any)
	if !ok || format["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object", got["response_format"])
	}
	wantKey := articleOverviewIdempotencyKey(job)
	if gotIdempotencyKey != wantKey || !strings.HasPrefix(gotIdempotencyKey, "article-overview:") {
		t.Fatalf("Idempotency-Key=%q want=%q", gotIdempotencyKey, wantKey)
	}
	if job.ContentHash != contentHash || job.Generation == "" {
		t.Fatalf("claimed overview job=%#v", job)
	}
}

func TestReadArticleOverviewSourceFetchesAndCachesPublicBody(t *testing.T) {
	db = openTestDB(t)
	insertArticleOverviewFixture(t, db, "overview-fetch-entry")
	oldFetch := articleOverviewFetchPublicURL
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })

	var calls int
	articleOverviewFetchPublicURL = func(ctx context.Context, raw string) (feedFetchResponse, error) {
		calls++
		if raw != "https://example.com/article" {
			t.Fatalf("fetch URL = %q", raw)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("public article fetch has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > articleOverviewSourceTimeout || remaining < articleOverviewSourceTimeout-500*time.Millisecond {
			t.Fatalf("public article fetch budget = %s, want about %s", remaining, articleOverviewSourceTimeout)
		}
		body := `<article><h1>完整正文</h1><p>` + strings.Repeat("这是一段来自公开原文的正文。", 40) + `</p></article>`
		return feedFetchResponse{Body: []byte(body), ContentType: "text/html", FinalURL: raw}, nil
	}

	source, err := readArticleOverviewSource(context.Background(), "overview-fetch-entry")
	if err != nil {
		t.Fatalf("read article overview source: %v", err)
	}
	if calls != 1 {
		t.Fatalf("public fetch calls = %d, want 1", calls)
	}
	cached, err := readEntryContent("overview-fetch-entry")
	if err != nil {
		t.Fatalf("read cached article body: %v", err)
	}
	if !entryContentCacheUsable(cached, source.SourceURL, source.ContentHash) || !strings.Contains(cached.ContentText, "完整正文") {
		t.Fatalf("cached article body = %#v", cached)
	}
	if content := articleOverviewContent(source); !strings.Contains(content, "完整正文") || strings.Contains(content, "摘要 fallback") {
		t.Fatalf("overview content did not prefer fetched body: %q", content)
	}
}

func TestReadArticleOverviewSourceUsesGatewayFulltextFlow(t *testing.T) {
	db = openTestDB(t)
	insertArticleOverviewFixture(t, db, "overview-gateway-entry")
	if _, err := db.Exec(`UPDATE news SET source_id='youtube-anthropic',origin=?,gateway_entry_id='sg_overview_1' WHERE id='overview-gateway-entry'`, gatewayOrigin); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.fulltexts["sg_overview_1"] = sourceGatewayFulltext{ContentHTML: `<article><p>来自云端全文路由的正文。</p></article>`}
	mock.configure(t)
	oldFetch := articleOverviewFetchPublicURL
	publicFetches := 0
	articleOverviewFetchPublicURL = func(context.Context, string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public overview fetch must stay closed")
	}
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })

	source, err := readArticleOverviewSource(context.Background(), "overview-gateway-entry")
	if err != nil {
		t.Fatalf("read gateway overview source: %v", err)
	}
	if publicFetches != 0 || mock.entryCallCount("/fulltext") != 1 {
		t.Fatalf("overview egress public=%d gateway_fulltext=%d", publicFetches, mock.entryCallCount("/fulltext"))
	}
	if content := articleOverviewContent(source); !strings.Contains(content, "来自云端全文路由的正文") {
		t.Fatalf("overview did not use gateway body: %q", content)
	}
}

func TestReadArticleOverviewSourceUnmappedGatewayRowFailsClosedFlow(t *testing.T) {
	db = openTestDB(t)
	insertArticleOverviewFixture(t, db, "overview-unmapped-gateway-entry")
	if _, err := db.Exec(`UPDATE news SET source_id='youtube-anthropic',origin=?,gateway_entry_id='' WHERE id='overview-unmapped-gateway-entry'`, gatewayOrigin); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.configure(t)
	oldFetch := articleOverviewFetchPublicURL
	publicFetches := 0
	articleOverviewFetchPublicURL = func(context.Context, string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public overview fetch must stay closed")
	}
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })

	source, err := readArticleOverviewSource(context.Background(), "overview-unmapped-gateway-entry")
	if err != nil {
		t.Fatalf("unmapped gateway row should retain summary: %v", err)
	}
	if publicFetches != 0 || mock.entryCallCount("/fulltext") != 0 || articleOverviewContent(source) != "摘要 fallback" {
		t.Fatalf("unmapped overview public=%d gateway=%d content=%q", publicFetches, mock.entryCallCount("/fulltext"), articleOverviewContent(source))
	}
}

func TestReadArticleOverviewSourceFetchFailureFallsBackToSummary(t *testing.T) {
	db = openTestDB(t)
	insertArticleOverviewFixture(t, db, "overview-fetch-fallback-entry")
	oldFetch := articleOverviewFetchPublicURL
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })
	articleOverviewFetchPublicURL = func(context.Context, string) (feedFetchResponse, error) {
		return feedFetchResponse{}, errors.New("upstream unavailable")
	}

	source, err := readArticleOverviewSource(context.Background(), "overview-fetch-fallback-entry")
	if err != nil {
		t.Fatalf("fetch failure should be recoverable: %v", err)
	}
	if got := articleOverviewContent(source); got != "摘要 fallback" {
		t.Fatalf("fallback overview content = %q, want feed summary", got)
	}
	var cachedRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entry_full_texts WHERE entry_id=?`, "overview-fetch-fallback-entry").Scan(&cachedRows); err != nil {
		t.Fatal(err)
	}
	if cachedRows != 0 {
		t.Fatalf("failed fetch created a full-text row: %d", cachedRows)
	}
}

func TestArticleOverviewContentPrefersRicherFeedBodyOverShortCachedFooter(t *testing.T) {
	db = openTestDB(t)
	hash := insertArticleOverviewFixture(t, db, "overview-short-cache-entry")
	richSummary := "订阅源已经提供了完整的研究背景、方法、实验结果与结论，足以作为本次文章概览的可靠输入。"
	if _, err := db.Exec(`UPDATE news SET summary_text=? WHERE id=?`, richSummary, "overview-short-cache-entry"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash) VALUES(?,?,?,?,?,'',?,?,?)`,
		"overview-short-cache-entry", "关注公众号，点击阅读原文。", "text", now, "ok", "https://example.com/article", now, hash); err != nil {
		t.Fatal(err)
	}
	source, err := readArticleOverviewSource(context.Background(), "overview-short-cache-entry")
	if err != nil {
		t.Fatal(err)
	}
	if got := articleOverviewContent(source); got != richSummary {
		t.Fatalf("overview content = %q, want richer feed body", got)
	}
}

func TestReadArticleOverviewSourceHonorsParentDeadline(t *testing.T) {
	db = openTestDB(t)
	insertArticleOverviewFixture(t, db, "overview-fetch-deadline-entry")
	oldFetch := articleOverviewFetchPublicURL
	t.Cleanup(func() { articleOverviewFetchPublicURL = oldFetch })
	parent, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	var observed context.Context
	articleOverviewFetchPublicURL = func(ctx context.Context, _ string) (feedFetchResponse, error) {
		observed = ctx
		return feedFetchResponse{}, ctx.Err()
	}

	_, err := readArticleOverviewSource(parent, "overview-fetch-deadline-entry")
	if err != nil {
		t.Fatalf("parent deadline should still fall back: %v", err)
	}
	if observed == nil {
		t.Fatal("public fetch seam was not called")
	}
	if deadline, ok := observed.Deadline(); !ok || time.Until(deadline) > 25*time.Millisecond {
		t.Fatalf("fetch context widened parent deadline: deadline=%v ok=%v", deadline, ok)
	}
}

func TestArticleAIOverviewDurableRecoveryFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-recovery-entry")
	requests := 0
	var idempotencyKeys []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		idempotencyKeys = append(idempotencyKeys, r.Header.Get("Idempotency-Key"))
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"恢复成功\",\"key_points\":[],\"impact\":\"\"}"}}]}`))
	}))
	defer proxy.Close()
	oldEndpoint, oldClient := articleOverviewProxyEndpoint, articleOverviewHTTPClient
	articleOverviewProxyEndpoint = proxy.URL + "/chat"
	articleOverviewHTTPClient = proxy.Client()
	t.Cleanup(func() { articleOverviewProxyEndpoint, articleOverviewHTTPClient = oldEndpoint, oldClient })

	first := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"overview-recovery-entry"}`)
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"stage":"queued"`) {
		t.Fatalf("enqueue response = %d %s", first.Code, first.Body.String())
	}
	job, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	if err := processArticleOverviewJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var stage string
	var attempts int
	if err := db.QueryRow(`SELECT stage,attempt_count FROM article_ai_overviews WHERE entry_id=?`, "overview-recovery-entry").Scan(&stage, &attempts); err != nil {
		t.Fatal(err)
	}
	if stage != articleOverviewStageRetryWait || attempts != 1 {
		t.Fatalf("after transient failure stage=%q attempts=%d", stage, attempts)
	}
	if _, err := db.Exec(`UPDATE article_ai_overviews SET next_retry_at=? WHERE entry_id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), "overview-recovery-entry"); err != nil {
		t.Fatal(err)
	}
	runNextArticleOverviewJobForTest(t)
	overview, err := storedArticleOverview("overview-recovery-entry", job.ContentHash)
	if err != nil || overview == nil || overview.Status != "ready" || overview.Stage != articleOverviewStageReady || overview.AttemptCount != 2 || overview.Summary != "恢复成功" {
		t.Fatalf("recovered overview=%#v err=%v", overview, err)
	}
	if len(idempotencyKeys) != 2 || idempotencyKeys[0] == "" || idempotencyKeys[0] != idempotencyKeys[1] {
		t.Fatalf("retry idempotency keys=%v", idempotencyKeys)
	}
}

func TestArticleAIOverviewWaitsForAsyncSourcePreparationFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-source-wait-entry")
	overview, err := enqueueArticleOverview("overview-source-wait-entry", false)
	if err != nil || overview == nil {
		t.Fatalf("enqueue overview=%#v err=%v", overview, err)
	}
	job, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	job.Attempt = articleOverviewMaxAttempts
	if err := finishArticleOverviewJob(job, errors.New("article_content_unavailable")); err != nil {
		t.Fatal(err)
	}
	var status, stage string
	if err := db.QueryRow(`SELECT status,stage FROM article_ai_overviews WHERE entry_id=?`, job.EntryID).Scan(&status, &stage); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || stage != articleOverviewStageRetryWait {
		t.Fatalf("source preparation terminated early: status=%q stage=%q", status, stage)
	}

	job.Generation = time.Now().Add(-articleOverviewSourceReadyWindow - time.Second).UTC().Format(time.RFC3339Nano)
	if err := finishArticleOverviewJob(job, errors.New("article_content_unavailable")); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,stage FROM article_ai_overviews WHERE entry_id=?`, job.EntryID).Scan(&status, &stage); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || stage != articleOverviewStageFailed {
		t.Fatalf("expired source preparation was not bounded: status=%q stage=%q", status, stage)
	}
}

func TestArticleAIOverviewRequestCancellationDoesNotOwnJobFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-detached-entry")
	proxyCalls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyCalls++
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"后台完成\",\"key_points\":[],\"impact\":\"\"}"}}]}`))
	}))
	defer proxy.Close()
	oldEndpoint, oldClient := articleOverviewProxyEndpoint, articleOverviewHTTPClient
	articleOverviewProxyEndpoint = proxy.URL + "/chat"
	articleOverviewHTTPClient = proxy.Client()
	t.Cleanup(func() { articleOverviewProxyEndpoint, articleOverviewHTTPClient = oldEndpoint, oldClient })

	request := httptest.NewRequest(http.MethodPost, "/api/entries/ai-overview", strings.NewReader(`{"entry_id":"overview-detached-entry"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	articleAIOverviewHandler(response, request.WithContext(ctx))
	if response.Code != http.StatusOK || proxyCalls != 0 || !strings.Contains(response.Body.String(), `"stage":"queued"`) {
		t.Fatalf("detached enqueue code=%d proxy_calls=%d body=%s", response.Code, proxyCalls, response.Body.String())
	}
	runNextArticleOverviewJobForTest(t)
	if proxyCalls != 1 {
		t.Fatalf("background proxy calls=%d, want 1", proxyCalls)
	}
}

func TestArticleAIOverviewRestartRequeuesInFlightJobFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-restart-entry")
	if _, err := enqueueArticleOverview("overview-restart-entry", false); err != nil {
		t.Fatal(err)
	}
	claimed, err := claimNextArticleOverviewJob()
	if err != nil || claimed.Attempt != 1 {
		t.Fatalf("first claim=%#v err=%v", claimed, err)
	}
	if err := recoverArticleOverviewJobs(); err != nil {
		t.Fatal(err)
	}
	recovered, err := claimNextArticleOverviewJob()
	if err != nil || recovered.EntryID != claimed.EntryID || recovered.ContentHash != claimed.ContentHash || recovered.Attempt != 2 {
		t.Fatalf("recovered claim=%#v err=%v", recovered, err)
	}
}

func TestArticleAIOverviewDeletedEntryDoesNotLeaveRecoveringJob(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-deleted-entry")
	if _, err := enqueueArticleOverview("overview-deleted-entry", false); err != nil {
		t.Fatal(err)
	}
	job, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE news SET deleted_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), job.EntryID); err != nil {
		t.Fatal(err)
	}
	if err := processArticleOverviewJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM article_ai_overviews WHERE entry_id=?`, job.EntryID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Fatalf("deleted article left %d recoverable overview jobs", jobs)
	}
}

func TestArticleAIOverviewRepeatedForceDoesNotDuplicateActiveJob(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-idempotent-entry")
	if _, err := enqueueArticleOverview("overview-idempotent-entry", false); err != nil {
		t.Fatal(err)
	}
	claimed, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enqueueArticleOverview("overview-idempotent-entry", true); err != nil {
		t.Fatal(err)
	}
	var stage string
	var attempts int
	if err := db.QueryRow(`SELECT stage,attempt_count FROM article_ai_overviews WHERE entry_id=?`, claimed.EntryID).Scan(&stage, &attempts); err != nil {
		t.Fatal(err)
	}
	if stage != articleOverviewStagePreparingSource || attempts != 1 {
		t.Fatalf("repeated force reset active job: stage=%q attempts=%d", stage, attempts)
	}
}

func TestArticleAIOverviewManualRetryStartsNewIdempotencyGeneration(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "overview-new-generation-entry")
	if _, err := enqueueArticleOverview("overview-new-generation-entry", false); err != nil {
		t.Fatal(err)
	}
	first, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failedArticleOverview(first.EntryID, first.ContentHash, errors.New("ai_proxy_rejected"), first.Attempt); err != nil {
		t.Fatal(err)
	}
	// SQLite timestamps are nanosecond-scoped, but keep this deterministic on
	// platforms whose clock source has a coarser resolution.
	time.Sleep(time.Millisecond)
	if _, err := enqueueArticleOverview(first.EntryID, true); err != nil {
		t.Fatal(err)
	}
	second, err := claimNextArticleOverviewJob()
	if err != nil {
		t.Fatal(err)
	}
	if first.Generation == "" || second.Generation == "" || first.Generation == second.Generation {
		t.Fatalf("manual retry generations first=%q second=%q", first.Generation, second.Generation)
	}
	if articleOverviewIdempotencyKey(first) == articleOverviewIdempotencyKey(second) {
		t.Fatal("manual retry reused the prior generation idempotency key")
	}
}

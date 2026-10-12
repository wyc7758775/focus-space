package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/bridges"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Source Gateway V1 flow tests.  The gateway server is mocked as a loopback
// httptest server speaking the spec §6.4/§7 contract; no test leaves the
// loopback interface or touches real credentials.

type gatewayMockServer struct {
	t            *testing.T
	mu           sync.Mutex
	server       *httptest.Server
	catalog      []sourceGatewayCatalogSource
	subs         []sourceGatewaySubscription
	pages        map[string][][]sourceGatewayEntry // sourceID -> ordered pages
	fulltexts    map[string]sourceGatewayFulltext
	media        map[string][]byte // "entryID/index"
	mediaMIME    string
	subsAsArray  bool
	requests     []string
	failWith     int
	failFulltext bool
	blockEntries bool
}

func newGatewayMockServer(t *testing.T) *gatewayMockServer {
	t.Helper()
	mock := &gatewayMockServer{t: t, pages: map[string][][]sourceGatewayEntry{}, fulltexts: map[string]sourceGatewayFulltext{}, media: map[string][]byte{}, mediaMIME: "image/jpeg"}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "test-gateway-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_FORBIDDEN", "error": "unauthorized"})
			return
		}
		mock.mu.Lock()
		mock.requests = append(mock.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		mock.mu.Unlock()
		if mock.failWith != 0 {
			w.WriteHeader(mock.failWith)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_UPSTREAM_UNAVAILABLE", "error": "upstream down"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/catalog"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"catalog_version": "test-v1", "sources": mock.catalog,
			}})
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/subscriptions/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ok": true}})
		case strings.HasSuffix(r.URL.Path, "/subscriptions"):
			if mock.subsAsArray {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": mock.subs})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
					"subscriptions": mock.subs,
				}})
			}
		case strings.Contains(r.URL.Path, "/fulltext"):
			mock.serveFulltext(w, r)
		case strings.Contains(r.URL.Path, "/media/"):
			mock.serveMedia(w, r)
		case strings.HasSuffix(r.URL.Path, "/entries"):
			mock.serveEntries(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(mock.server.Close)
	return mock
}

func (m *gatewayMockServer) serveFulltext(w http.ResponseWriter, r *http.Request) {
	if m.failFulltext {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_UPSTREAM_UNAVAILABLE", "error": "fulltext down"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	gatewayID := parts[len(parts)-2]
	content, ok := m.fulltexts[gatewayID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_ENTRY_NOT_FOUND", "error": "missing"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"content_text": content.ContentText, "content_html": content.ContentHTML,
		"content_format": content.ContentFormat, "degraded": content.Degraded,
		"images": content.Images,
	}})
}

func (m *gatewayMockServer) serveMedia(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// .../entries/<id>/media/<index>
	key := parts[len(parts)-3] + "/" + parts[len(parts)-1]
	body, ok := m.media[key]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_MEDIA_UNAVAILABLE", "error": "missing"})
		return
	}
	w.Header().Set("Content-Type", m.mediaMIME)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", `"gateway-media"`)
	w.Header().Set("Last-Modified", "Wed, 27 Aug 2026 12:00:00 GMT")
	status := http.StatusOK
	if r.Header.Get("Range") == "bytes=0-3" && len(body) >= 4 {
		body = body[:4]
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", "bytes 0-3/8")
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (m *gatewayMockServer) entryCallCount(marker string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, request := range m.requests {
		if strings.Contains(request, marker) {
			count++
		}
	}
	return count
}

func (m *gatewayMockServer) serveEntries(w http.ResponseWriter, r *http.Request) {
	if m.blockEntries {
		<-r.Context().Done()
		return
	}
	sourceID := r.URL.Query().Get("source_id")
	cursor := r.URL.Query().Get("cursor")
	pages := m.pages[sourceID]
	index := 0
	if cursor != "" {
		parsed, err := fmt.Sscanf(cursor, "cursor-%d", &index)
		if err != nil || parsed != 1 || index < 0 || index >= len(pages) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_CURSOR_INVALID", "error": "bad cursor"})
			return
		}
	}
	if len(pages) == 0 || index >= len(pages) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"entries": []any{}, "has_more": false}})
		return
	}
	page := pages[index]
	next := ""
	hasMore := index+1 < len(pages)
	if hasMore {
		next = fmt.Sprintf("cursor-%d", index+1)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"entries": page, "next_cursor": next, "has_more": hasMore,
	}})
}

func (m *gatewayMockServer) configure(t *testing.T) {
	t.Helper()
	t.Setenv("ZAPP_SOURCE_GATEWAY_BASE_URL", m.server.URL+"/source-gateway")
	t.Setenv("ZAPP_SOURCE_GATEWAY_TOKEN", "test-gateway-token")
	sourceGatewayState = gatewayRuntime{}
}

func gatewayTestEntry(sourceID, externalID, title, url, hash, published string) sourceGatewayEntry {
	return sourceGatewayEntry{
		ID:          "sg_" + sourceID + "_" + externalID,
		SourceID:    sourceID,
		ExternalID:  externalID,
		Title:       title,
		URL:         url,
		PublishedAt: published,
		SummaryText: "摘要 " + title,
		ContentHash: hash,
		SourceLevel: "official",
	}
}

func TestGatewayEntryIDsRemainArticleUniqueWhenExternalIDCaseDiffersFlow(t *testing.T) {
	db = openTestDB(t)
	source := mustFeedSource(t, "youtube-anthropic")
	upper := gatewayTestEntry(source.ID, "VideoAbC", "Upper", "https://example.com/upper", "hash-upper", "2026-08-23T10:00:00Z")
	lower := gatewayTestEntry(source.ID, "videoabc", "Lower", "https://example.com/lower", "hash-lower", "2026-08-23T09:00:00Z")
	upper.ID = "sg_entry_case_upper"
	lower.ID = "sg_entry_case_lower"

	changed, err := upsertGatewayEntriesPage(source, []sourceGatewayEntry{upper, lower})
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 {
		t.Fatalf("changed entries = %d, want 2", changed)
	}

	seenNewsIDs := make(map[string]bool, 2)
	for _, entry := range []sourceGatewayEntry{upper, lower} {
		var newsID, feedSourceID, gatewayEntryID string
		err := db.QueryRow(`SELECT id,source_id,gateway_entry_id FROM news WHERE source_id=? AND external_id=?`, source.ID, entry.ExternalID).
			Scan(&newsID, &feedSourceID, &gatewayEntryID)
		if err != nil {
			t.Fatal(err)
		}
		if newsID != gatewayNewsID(source.ID, entry.ExternalID) || feedSourceID != source.ID || gatewayEntryID != entry.ID {
			t.Fatalf("entry %q mapped to news=%q source=%q gateway=%q", entry.ExternalID, newsID, feedSourceID, gatewayEntryID)
		}
		seenNewsIDs[newsID] = true
	}
	if len(seenNewsIDs) != 2 {
		t.Fatalf("case-distinct articles share news ids: %#v", seenNewsIDs)
	}
	if got, want := gatewayNewsID(source.ID, "save-me"), stableObjectID("entry", source.ID+"|save-me"); got != want {
		t.Fatalf("lowercase legacy id changed: got %q want %q", got, want)
	}
}

func TestUpsertGatewayNewsRejectsInvalidArticleID(t *testing.T) {
	db = openTestDB(t)
	source := mustFeedSource(t, "youtube-anthropic")
	entry := gatewayTestEntry(source.ID, "video-1", "Invalid", "https://example.com/invalid", "hash-invalid", "2026-08-23T08:00:00Z")
	entry.ID = ""
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := upsertGatewayNewsTx(tx, source, entry, nowUTC()); err == nil {
		t.Fatal("expected invalid gateway article id to be rejected")
	}
}

func TestSourceGatewayBridgeConfigValidationUnit(t *testing.T) {
	if bridge := newSourceGatewayBridge("", "token", nil); bridge.err == nil {
		t.Fatal("empty base URL must fail closed")
	}
	if bridge := newSourceGatewayBridge("http://127.0.0.1:9090/api/v1/source-gateway", "", nil); bridge.err == nil {
		t.Fatal("missing token must fail closed")
	}
	if bridge := newSourceGatewayBridge("http://8.8.8.8/api/v1/source-gateway", "token", nil); bridge.err == nil {
		t.Fatal("non-loopback host must be rejected")
	}
	if bridge := newSourceGatewayBridge("https://127.0.0.1/api/v1/source-gateway", "token", nil); bridge.err == nil {
		t.Fatal("non-http scheme must be rejected")
	}
	if bridge := newSourceGatewayBridge("http://127.0.0.1:9090/api/v1/other", "token", nil); bridge.err == nil {
		t.Fatal("base URL without the source-gateway suffix must be rejected")
	}
	if bridge := newSourceGatewayBridge("http://user@127.0.0.1:9090/x/source-gateway", "token", nil); bridge.err == nil {
		t.Fatal("userinfo must be rejected")
	}
	bridge := newSourceGatewayBridge("http://127.0.0.1:19090/api/v1/source-gateway", " token ", nil)
	if bridge.err != nil || bridge.token != "token" {
		t.Fatalf("valid loopback config rejected: %v", bridge.err)
	}
}

func TestSourceGatewayStatusMapsBusinessEnvelopeAndScoped404Unit(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{
			name:   "local upstream unavailable keeps http 200 as an error",
			status: http.StatusOK,
			body:   `{"code":93003,"data":{},"msg":"云端来源暂不可用，显示上次内容"}`,
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "local auth required keeps http 200 as an auth error",
			status: http.StatusOK,
			body:   `{"code":93002,"data":{},"msg":"云端授权缺失或已失效，请重新登录"}`,
			want:   errSourceGatewayAuth,
		},
		{
			name:   "local invalid params is not an empty success",
			status: http.StatusOK,
			body:   `{"code":"93001","data":{}}`,
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "cloud semantic entry not found",
			status: http.StatusNotFound,
			body:   `{"status":"error","error":"SRC_ENTRY_NOT_FOUND","code":"SRC0004"}`,
			want:   errSourceGatewayNotFound,
		},
		{
			name:   "cloud numeric entry not found",
			status: http.StatusNotFound,
			body:   `{"status":"error","code":"SRC0004","message":"missing"}`,
			want:   errSourceGatewayNotFound,
		},
		{
			name:   "cloud cursor error uses semantic numeric alias",
			status: http.StatusBadRequest,
			body:   `{"status":"error","error":"SRC_CURSOR_INVALID","code":"SRC0005"}`,
			want:   errSourceGatewayInvalidCursor,
		},
		{
			name:   "app host generic 404 is unavailable not source missing",
			status: http.StatusNotFound,
			body:   `{"error":"source gateway is unavailable"}`,
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "bare 404 preserves source gateway not found contract",
			status: http.StatusNotFound,
			body:   "",
			want:   errSourceGatewayNotFound,
		},
		{
			name:   "future nonzero local code cannot become empty success",
			status: http.StatusOK,
			body:   `{"code":93005,"data":{}}`,
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "idempotency conflict is a business error",
			status: http.StatusOK,
			body:   `{"status":"error","error":"SRC_IDEMPOTENCY_CONFLICT","code":"SRC0010"}`,
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "plain app host unavailable 404",
			status: http.StatusNotFound,
			body:   "source gateway is unavailable",
			want:   errSourceGatewayUnavailable,
		},
		{
			name:   "standard cloud success code zero",
			status: http.StatusOK,
			body:   `{"status":"success","code":"0","data":{}}`,
			want:   nil,
		},
		{
			name:   "standard local success numeric zero",
			status: http.StatusOK,
			body:   `{"code":0,"data":{}}`,
			want:   nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapSourceGatewayStatus([]byte(tc.body), tc.status)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("status mapping = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tc.want) {
				t.Fatalf("status mapping = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSourceGatewayHTTP200BusinessErrorDoesNotDecodeAsEmptyDataFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":93003,"data":{},"msg":"云端来源暂不可用，显示上次内容"}`))
	}))
	defer server.Close()
	bridge := newSourceGatewayBridge(server.URL+sourceGatewayPathSuffix, "test-token", server.Client())
	var data struct {
		Sources []sourceGatewayCatalogSource `json:"sources"`
	}
	err := bridge.getJSON(context.Background(), "/catalog", "", sourceGatewayMaxPageBytes, &data)
	if !errors.Is(err, errSourceGatewayUnavailable) {
		t.Fatalf("getJSON error = %v, want unavailable", err)
	}
	if data.Sources != nil {
		t.Fatalf("business error must not decode an empty success payload: %#v", data)
	}
}

func TestSourceGatewayMediaHTTP200BusinessErrorFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"code":93004,"data":{},"msg":"媒体内容不可用"}`))
	}))
	defer server.Close()
	bridge := newSourceGatewayBridge(server.URL+sourceGatewayPathSuffix, "test-token", server.Client())
	_, err := bridge.mediaWithRange(context.Background(), "entry-1", 0, http.MethodGet, "thumbnail", "")
	if !errors.Is(err, errSourceGatewayUnavailable) {
		t.Fatalf("media error = %v, want unavailable", err)
	}
}

func TestGatewayRuntimeReaderHistoryCooldownUnit(t *testing.T) {
	if sourceGatewayReaderHistoryTimeout < 10*time.Second || sourceGatewayReaderHistoryTimeout > 12*time.Second {
		t.Fatalf("reader history timeout must stay bounded at 10-12s, got %s", sourceGatewayReaderHistoryTimeout)
	}
	if sourceGatewayReaderHistoryCooldown != 30*time.Second {
		t.Fatalf("reader history cooldown = %s", sourceGatewayReaderHistoryCooldown)
	}
	runtime := gatewayRuntime{}
	now := time.Now()
	if runtime.readerHistoryCoolingDown(now) {
		t.Fatal("fresh runtime must not be cooling down")
	}
	runtime.recordReaderHistoryFailure(now)
	if !runtime.readerHistoryCoolingDown(now.Add(time.Second)) {
		t.Fatal("failure must start a bounded cooldown")
	}
	runtime.clearReaderHistoryCooldown()
	if runtime.readerHistoryCoolingDown(now) {
		t.Fatal("successful pull must clear the cooldown")
	}
}

func TestSourceGatewayBridgeDerivesFromDeviceDataEnvUnit(t *testing.T) {
	t.Setenv("ZAPP_SOURCE_GATEWAY_BASE_URL", "")
	t.Setenv("ZAPP_SOURCE_GATEWAY_TOKEN", "")
	// Older local-server builds included a trailing slash in this value.
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", "http://127.0.0.1:9090/api/v1/internal/app-runtime/plaud-action-dashboard/device-data/v1/")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "device-token")
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		t.Fatalf("derived bridge: %v", bridge.err)
	}
	if got := bridge.baseURL.String(); got != "http://127.0.0.1:9090/api/v1/internal/app-runtime/plaud-action-dashboard/source-gateway" {
		t.Fatalf("derived base URL = %q", got)
	}
	if bridge.token != "device-token" {
		t.Fatal("derived bridge must reuse the device-data token")
	}
}

func TestSourceGatewayBridgeUsesLegacyTokenWithDedicatedBaseURLUnit(t *testing.T) {
	t.Setenv("ZAPP_SOURCE_GATEWAY_BASE_URL", "http://127.0.0.1:19090/api/v1/internal/app-runtime/plaud-action-dashboard/source-gateway/")
	t.Setenv("ZAPP_SOURCE_GATEWAY_TOKEN", "")
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "legacy-device-token")
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		t.Fatalf("mixed-version bridge: %v", bridge.err)
	}
	if got := bridge.baseURL.String(); got != "http://127.0.0.1:19090/api/v1/internal/app-runtime/plaud-action-dashboard/source-gateway" {
		t.Fatalf("dedicated base URL = %q", got)
	}
	if bridge.token != "legacy-device-token" {
		t.Fatalf("bridge token = %q, want legacy fallback", bridge.token)
	}
}

func TestSourceGatewaySubscriptionsAcceptArrayEnvelopeUnit(t *testing.T) {
	mock := newGatewayMockServer(t)
	mock.subs = []sourceGatewaySubscription{
		{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"},
		{SourceID: "wechat-qbitai", Enabled: false, Status: "paused"},
	}
	mock.subsAsArray = true
	mock.configure(t)

	subscriptions, err := newSourceGatewayBridgeFromEnv().subscriptions(context.Background())
	if err != nil {
		t.Fatalf("subscriptions array: %v", err)
	}
	if len(subscriptions) != 2 || subscriptions[0].SourceID != "youtube-anthropic" || subscriptions[1].Enabled {
		t.Fatalf("subscriptions = %#v", subscriptions)
	}
}

func TestSourceGatewaySyncHeadPersistsHistoryCursorFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	pages := make([][]sourceGatewayEntry, sourceGatewayMaxPagesRun+2)
	for index := range pages {
		externalID := fmt.Sprintf("history-%d", index)
		pages[index] = []sourceGatewayEntry{gatewayTestEntry(
			"youtube-anthropic",
			externalID,
			fmt.Sprintf("历史文章 %d", index),
			"https://www.youtube.com/watch?v="+externalID,
			"hash-"+externalID,
			fmt.Sprintf("2026-08-%02dT10:00:00Z", 20-index),
		)}
	}
	mock.pages["youtube-anthropic"] = pages
	mock.configure(t)
	source := mustFeedSource(t, "youtube-anthropic")

	if err := syncGatewaySourceHead(context.Background(), newSourceGatewayBridgeFromEnv(), source); err != nil {
		t.Fatalf("head sync: %v", err)
	}
	var cursor string
	var hasMore, rows int
	if err := db.QueryRow(`SELECT gateway_cursor,gateway_has_more FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, source.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	wantCursor := fmt.Sprintf("cursor-%d", sourceGatewayMaxPagesRun)
	if cursor != wantCursor || hasMore != 1 || rows != sourceGatewayMaxPagesRun {
		t.Fatalf("after capped head sync cursor=%q hasMore=%d rows=%d, want %q/1/%d", cursor, hasMore, rows, wantCursor, sourceGatewayMaxPagesRun)
	}

	pulled, err := pullGatewayHistoryPage(context.Background(), source.ID)
	if err != nil || pulled != 1 {
		t.Fatalf("history continuation = %d, %v", pulled, err)
	}
	if err := db.QueryRow(`SELECT gateway_cursor,gateway_has_more FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	wantCursor = fmt.Sprintf("cursor-%d", sourceGatewayMaxPagesRun+1)
	if cursor != wantCursor || hasMore != 1 {
		t.Fatalf("continued cursor=%q hasMore=%d, want %q/1", cursor, hasMore, wantCursor)
	}
}

func TestSourceGatewaySyncPagesAndDeduplicatesFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	page1 := make([]sourceGatewayEntry, 0, 50)
	for i := 0; i < 50; i++ {
		page1 = append(page1, gatewayTestEntry("youtube-anthropic", fmt.Sprintf("v%03d", i), fmt.Sprintf("视频 %d", i), fmt.Sprintf("https://www.youtube.com/watch?v=%03d", i), fmt.Sprintf("hash-%d", i), "2026-08-16T10:00:00Z"))
	}
	page2 := []sourceGatewayEntry{
		gatewayTestEntry("youtube-anthropic", "v050", "视频 50", "https://www.youtube.com/watch?v=050", "hash-50", "2026-08-15T10:00:00Z"),
		// A foreign-source entry in a scoped page must never be written.
		gatewayTestEntry("youtube-nvidia", "foreign", "不该写入", "https://www.youtube.com/watch?v=foreign", "hash-x", "2026-08-15T09:00:00Z"),
	}
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{page1, page2}
	mock.configure(t)

	if err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "youtube-anthropic")); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='youtube-anthropic'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 51 {
		t.Fatalf("entries after warmup = %d, want 51 (50+1, foreign skipped)", count)
	}
	var status, gatewayStatus, gatewayCursor string
	var hasMore int
	if err := db.QueryRow(`SELECT status,gateway_status,gateway_cursor,gateway_has_more FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&status, &gatewayStatus, &gatewayCursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	if status != "ok" || gatewayStatus != "healthy" || hasMore != 0 {
		t.Fatalf("source state = %q/%q hasMore=%d", status, gatewayStatus, hasMore)
	}
	var gatewayID string
	if err := db.QueryRow(`SELECT gateway_entry_id FROM news WHERE source_id='youtube-anthropic' AND external_id='v000'`).Scan(&gatewayID); err != nil {
		t.Fatal(err)
	}
	if gatewayID != "sg_youtube-anthropic_v000" {
		t.Fatalf("gateway entry id = %q", gatewayID)
	}

	// A steady-state re-sync must page exactly once, change nothing and never
	// duplicate rows.
	mock.requests = nil
	if err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "youtube-anthropic")); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='youtube-anthropic'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 51 {
		t.Fatalf("entries after re-sync = %d, want 51", count)
	}
	entryRequests := 0
	for _, request := range mock.requests {
		if strings.Contains(request, "/entries") {
			entryRequests++
		}
	}
	if entryRequests != 1 {
		t.Fatalf("steady-state sync issued %d entry pages, want 1: %#v", entryRequests, mock.requests)
	}
}

func mustFeedSource(t *testing.T, id string) FeedSource {
	t.Helper()
	source, err := feedSourceByID(id)
	if err != nil {
		t.Fatalf("feed source %s: %v", id, err)
	}
	return source
}

func TestSourceGatewayMergesWithLegacyDirectFetchRowFlow(t *testing.T) {
	db = openTestDB(t)
	// Legacy row from the retired direct collector: different external id, same
	// canonical article URL, with user state attached.
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('entry-legacy','legacy-guid-1','旧标题','https://example.com/post/1','量子位','wechat-qbitai','app-fetch','rss','article','','2026-08-10T00:00:00Z','旧摘要','text','','','','',0,'old-hash','2026-08-10T00:00:00Z','public_read','official','','2026-08-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:entry-legacy',1,1,0,0.5,'2026-08-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "wechat-qbitai", Name: "量子位", Group: "wechat", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "wechat-qbitai", Enabled: true, Status: "ready"}}
	mock.pages["wechat-qbitai"] = [][]sourceGatewayEntry{{
		gatewayTestEntry("wechat-qbitai", "gateway-ext-1", "新标题", "https://example.com/post/1", "new-hash", "2026-08-10T00:00:00Z"),
	}}
	mock.configure(t)

	if err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "wechat-qbitai")); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='wechat-qbitai'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("merge by URL must not duplicate rows, got %d", count)
	}
	var title, gatewayID, origin string
	var starred, read int
	var progress float64
	if err := db.QueryRow(`SELECT n.title,n.gateway_entry_id,n.origin,COALESCE(p.starred,0),COALESCE(p.is_read,0),COALESCE(p.reading_progress,0)
FROM news n LEFT JOIN item_preferences p ON p.item_key='news:'||n.id WHERE n.id='entry-legacy'`).Scan(&title, &gatewayID, &origin, &starred, &read, &progress); err != nil {
		t.Fatal(err)
	}
	if title != "新标题" || gatewayID != "sg_wechat-qbitai_gateway-ext-1" {
		t.Fatalf("merged row title=%q gateway=%q", title, gatewayID)
	}
	if starred != 1 || read != 1 || progress != 0.5 {
		t.Fatalf("user state lost in merge: starred=%d read=%d progress=%v", starred, read, progress)
	}
	if origin == "agent" {
		t.Fatalf("automated gateway refresh must not flip a public-fetch row into an agent projection: %q", origin)
	}
}

func TestSourceGatewayUnavailableKeepsLastKnownGoodFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('entry-cached','ext-1','已缓存文章','https://example.com/a','Anthropic','youtube-anthropic','source-gateway','text','article','','2026-08-10T00:00:00Z','缓存摘要','text','','','','',0,'h1','2026-08-10T00:00:00Z','public_read','official','','2026-08-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "degraded"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "degraded"}}
	mock.configure(t)
	mock.failWith = http.StatusServiceUnavailable
	originalFetch := operationalSourceFetch
	publicFetches := 0
	operationalSourceFetch = func(_ context.Context, _, _, _ string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public collector must not run while the gateway is unavailable")
	}
	t.Cleanup(func() { operationalSourceFetch = originalFetch })

	source := mustFeedSource(t, "youtube-anthropic")
	err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), source)
	if err == nil {
		t.Fatal("unavailable gateway must report an error")
	}
	var status, lastError string
	if err := db.QueryRow(`SELECT status,last_error FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&status, &lastError); err != nil {
		t.Fatal(err)
	}
	// 离线行为（spec §7）：本地有最后一次成功数据时不解释为"没有内容"。
	if status != "partial" || !strings.Contains(lastError, "云端来源暂不可用，显示上次内容") {
		t.Fatalf("offline state = %q %q", status, lastError)
	}
	list := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "已缓存文章") {
		t.Fatalf("cached entries must stay visible while the gateway is down: %d %s", list.Code, list.Body.String())
	}
	if publicFetches != 0 {
		t.Fatalf("gateway outage triggered %d public collector calls", publicFetches)
	}
}

func TestSourceGatewayNotConfiguredKeepsLastKnownGoodWithoutPublicFetchFlow(t *testing.T) {
	db = openTestDB(t)
	for _, key := range []string{sourceGatewayBaseURLEnv, sourceGatewayTokenEnv, "ZAPP_DEVICE_DATA_BASE_URL", "ZAPP_DEVICE_DATA_TOKEN"} {
		t.Setenv(key, "")
	}
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('entry-cached-missing-gateway','ext-cached','已缓存文章','https://example.com/cached','Anthropic','youtube-anthropic','source-gateway','text','article','','2026-08-10T00:00:00Z','缓存摘要','text','','','','',0,'cached-hash','2026-08-10T00:00:00Z','public_read','official','','2026-08-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	originalFetch := operationalSourceFetch
	t.Cleanup(func() { operationalSourceFetch = originalFetch })
	publicFetches := 0
	operationalSourceFetch = func(_ context.Context, _, _, _ string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public collector must not run when the gateway is not configured")
	}
	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('youtube-anthropic','pending','2026-08-22T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	if err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "youtube-anthropic")); err == nil {
		t.Fatal("missing gateway configuration must report an unavailable source")
	}
	var mode, sourceURL, status, gatewayStatus, lastError, requestStatus string
	var entries int
	if err := db.QueryRow(`SELECT collection_mode,url,status,gateway_status,last_error FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&mode, &sourceURL, &status, &gatewayStatus, &lastError); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='youtube-anthropic' AND deleted_at=''`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM sync_requests WHERE source_id='youtube-anthropic' ORDER BY id DESC LIMIT 1`).Scan(&requestStatus); err != nil {
		t.Fatal(err)
	}
	if publicFetches != 0 || entries != 1 || mode != "source_gateway" || sourceURL != "source-gateway://youtube-anthropic" || status != "partial" || gatewayStatus != "unavailable" || requestStatus != "retry_wait" || !strings.Contains(lastError, gatewayUnavailableNotice) {
		t.Fatalf("public_fetches=%d entries=%d mode=%q url=%q status=%q gateway=%q request=%q error=%q", publicFetches, entries, mode, sourceURL, status, gatewayStatus, requestStatus, lastError)
	}
	list := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "已缓存文章") {
		t.Fatalf("cached entries must stay visible without gateway configuration: %d %s", list.Code, list.Body.String())
	}
}

func TestSourceGatewayAuthFailureIsTerminalFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	t.Setenv("ZAPP_SOURCE_GATEWAY_TOKEN", "wrong-token")
	originalFetch := operationalSourceFetch
	publicFetches := 0
	operationalSourceFetch = func(_ context.Context, _, _, _ string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public collector must not run after an authentication failure")
	}
	t.Cleanup(func() { operationalSourceFetch = originalFetch })

	err := syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "youtube-anthropic"))
	if err == nil {
		t.Fatal("401 must surface an auth failure")
	}
	var gatewayStatus, lastError string
	if err := db.QueryRow(`SELECT gateway_status,last_error FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&gatewayStatus, &lastError); err != nil {
		t.Fatal(err)
	}
	if gatewayStatus != "auth" || !strings.Contains(lastError, "重新登录") || publicFetches != 0 {
		t.Fatalf("auth state = %q %q public_fetches=%d", gatewayStatus, lastError, publicFetches)
	}
}

func TestSourceGatewayReconcileCreatesCatalogRowsFlow(t *testing.T) {
	db = openTestDB(t)
	// Production startup runs the idempotent 074 repair after migrations. Keep
	// this flow fixture on the same schema so it exercises the explicit
	// gateway_enabled/gateway_has_more materialization for catalog-only rows.
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{
		{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy", CachedEntryCount: 321, LastSuccessAt: "2026-09-15T10:20:30Z"},
		{ID: "wechat-geekpark", Name: "极客公园", Group: "wechat", Status: "degraded"},
		{ID: "new-maintained-source", Name: "新维护来源", Group: "wechat", Status: "healthy"},
	}
	mock.subs = []sourceGatewaySubscription{
		{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"},
		{SourceID: "wechat-geekpark", Enabled: true, Status: "degraded"},
		{SourceID: "new-maintained-source", Enabled: true, Status: "initializing"},
	}
	mock.configure(t)

	if err := reconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var status, gatewayLastSuccess string
	var gatewayCachedCount int
	if err := db.QueryRow(`SELECT gateway_status,gateway_last_success_at,gateway_cached_entry_count FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&status, &gatewayLastSuccess, &gatewayCachedCount); err != nil {
		t.Fatal(err)
	}
	if status != "healthy" {
		t.Fatalf("healthy catalog status = %q", status)
	}
	if gatewayLastSuccess != "2026-09-15T10:20:30Z" || gatewayCachedCount != 321 {
		t.Fatalf("gateway freshness = %q/%d", gatewayLastSuccess, gatewayCachedCount)
	}
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='wechat-geekpark'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "degraded" {
		t.Fatalf("degraded catalog status = %q", status)
	}
	var mode, name string
	var isDefault, enabled, gatewayEnabled, gatewayHasMore int
	if err := db.QueryRow(`SELECT f.collection_mode,f.name,f.is_default,s.enabled,f.gateway_enabled,f.gateway_has_more FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id WHERE f.id='new-maintained-source'`).Scan(&mode, &name, &isDefault, &enabled, &gatewayEnabled, &gatewayHasMore); err != nil {
		t.Fatal(err)
	}
	if mode != "source_gateway" || name != "新维护来源" || isDefault != 0 || enabled != 1 || gatewayEnabled != 1 || gatewayHasMore != 1 {
		t.Fatalf("new catalog row = %q %q default=%d enabled=%d gateway=%d hasMore=%d", mode, name, isDefault, enabled, gatewayEnabled, gatewayHasMore)
	}
	// An existing default that the cloud no longer serves is marked blocked but
	// keeps its local rows and subscription.
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='youtube-google-deepmind'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "blocked" {
		t.Fatalf("source outside the maintained catalog = %q, want blocked", status)
	}
}

func TestSourceGatewayStartupReconcileMaterializesMissingSubscriptionsFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	// Reproduce the broken install state: migrations left the maintained source
	// directory in place, but its local subscription projection was not
	// materialized.  The reader's LEFT JOIN then reports subscribed=false and
	// the on-demand source path refuses to enqueue a first sync.
	if _, err := db.Exec(`DELETE FROM feed_subscriptions WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	// The schema repair can leave durable local-toggle operations from the
	// fixture's migration history.  They are unrelated to this metadata
	// bootstrap assertion; drain/outbox behavior has its own flow coverage.
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)

	started := time.Now()
	runSourceGatewayStartupReconcile(context.Background())
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("successful metadata bootstrap took %s; it must stay off the HTTP startup path", elapsed)
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("startup reconcile enabled=%d, want materialized subscription", enabled)
	}
	mock.mu.Lock()
	requests := append([]string(nil), mock.requests...)
	mock.mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[0], "/subscriptions") || !strings.Contains(requests[1], "/catalog") {
		t.Fatalf("startup bootstrap must perform bounded metadata reads only (subscriptions before catalog): %#v", requests)
	}
	for _, request := range requests {
		if strings.Contains(request, "/entries") {
			t.Fatalf("metadata bootstrap must not hydrate article entries: %#v", requests)
		}
	}

	// A user can explicitly unsubscribe between two reconciles.  Reset the
	// process cooldown to model a later retry and verify the materialization
	// path remains insert-only rather than silently re-enabling that choice.
	if _, err := db.Exec(`UPDATE feed_subscriptions SET enabled=0, updated_at=CURRENT_TIMESTAMP WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	sourceGatewayState = gatewayRuntime{}
	runSourceGatewayStartupReconcile(context.Background())
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("startup reconcile must preserve an explicit local unsubscribe")
	}
}

func TestSourceGatewayStartupReconcileNoopWithoutBridgeUnit(t *testing.T) {
	for _, key := range []string{sourceGatewayBaseURLEnv, sourceGatewayTokenEnv, "ZAPP_DEVICE_DATA_BASE_URL", "ZAPP_DEVICE_DATA_TOKEN"} {
		t.Setenv(key, "")
	}
	started := time.Now()
	runSourceGatewayStartupReconcile(context.Background())
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("unconfigured gateway bootstrap blocked for %s", elapsed)
	}
}

func TestSourceCatalogScopedReadRepairsMissingGatewaySubscriptionFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM feed_subscriptions WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)

	// This is the request-triggered fallback used when a user clicks a source
	// before the asynchronous startup worker has completed. It must materialize
	// the row in the same request, while keeping the response scoped to one
	// source and avoiding an article entries fetch.
	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id=youtube-anthropic&include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("scoped source status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 || !payload.Sources[0].Subscribed {
		t.Fatalf("scoped source projection = %#v, want one subscribed source", payload.Sources)
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("request-triggered subscription enabled=%d, want 1", enabled)
	}
	mock.mu.Lock()
	requests := append([]string(nil), mock.requests...)
	mock.mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[0], "/subscriptions") || !strings.Contains(requests[1], "/catalog") {
		t.Fatalf("scoped source repair requests = %#v, want subscriptions then catalog", requests)
	}
	for _, request := range requests {
		if strings.Contains(request, "/entries") {
			t.Fatalf("scoped source metadata repair must not hydrate entries: %#v", requests)
		}
	}
}

func TestSourceCatalogScopedReadRepairsMissingGatewaySourceRowFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	// A fresh install can lose the entire local feed_sources row (not only the
	// subscription projection). A source-scoped request must be able to
	// materialize it from the user-scoped gateway subscription response.
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	const sourceID = "first-install-source"
	var existing int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id=?`, sourceID).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatal("test source unexpectedly exists before reconciliation")
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: sourceID, Name: "首装来源", Group: "rss", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: sourceID, Enabled: true, Status: "ready"}}
	mock.configure(t)

	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id="+sourceID+"&include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("missing source row status = %d, body = %s", result.Code, result.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 || payload.Sources[0].ID != sourceID || !payload.Sources[0].Subscribed {
		t.Fatalf("materialized source projection = %#v, want one subscribed source", payload.Sources)
	}
	var mode, sourceURL, gatewayStatus string
	var hasMore int
	if err := db.QueryRow(`SELECT collection_mode,url,gateway_status,gateway_has_more FROM feed_sources WHERE id=?`, sourceID).Scan(&mode, &sourceURL, &gatewayStatus, &hasMore); err != nil {
		t.Fatal(err)
	}
	if mode != "source_gateway" || sourceURL != "source-gateway://"+sourceID || gatewayStatus != "healthy" || hasMore != 1 {
		t.Fatalf("materialized source row = mode=%q url=%q gateway=%q has_more=%d", mode, sourceURL, gatewayStatus, hasMore)
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id=?`, sourceID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("materialized subscription enabled=%d, want 1", enabled)
	}
	mock.mu.Lock()
	requests := append([]string(nil), mock.requests...)
	mock.mu.Unlock()
	if len(requests) != 2 || !strings.Contains(requests[0], "/subscriptions") || !strings.Contains(requests[1], "/catalog") {
		t.Fatalf("missing source row repair requests = %#v, want subscriptions then catalog", requests)
	}
	for _, request := range requests {
		if strings.Contains(request, "/entries") {
			t.Fatalf("source metadata repair must not hydrate entries: %#v", requests)
		}
	}
}

func TestSourceCatalogScopedReadDoesNotReturnFalseEmptyOnReconcileFailureFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	// Keep the source row but remove only its subscription projection, which is
	// the state that previously made ensureSourceGatewaySubscriptionProjection
	// swallow a gateway timeout and return HTTP 200 with an empty/disabled row.
	if _, err := db.Exec(`DELETE FROM feed_subscriptions WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.failWith = http.StatusServiceUnavailable
	mock.configure(t)

	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id=youtube-anthropic&include_counts=0", "")
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("reconciliation failure status = %d, body = %s; a timeout must not become a successful empty source", result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), `"sources"`) {
		t.Fatalf("reconciliation failure must not return a source payload: %s", result.Body.String())
	}
}

func TestSourceCatalogScopedReadMissingGatewayRowDoesNotReturnFalseEmptyOnReconcileFailureFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.failWith = http.StatusServiceUnavailable
	mock.configure(t)

	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id=missing-first-install-source&include_counts=0", "")
	if result.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing-row reconciliation failure status = %d, body = %s; a timeout must not become HTTP 200 empty", result.Code, result.Body.String())
	}
	if strings.Contains(result.Body.String(), `"sources"`) {
		t.Fatalf("missing-row reconciliation failure must not return a source payload: %s", result.Body.String())
	}
}

func TestSourceCatalogScopedReadPreservesExplicitGatewayUnsubscribeUnit(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_subscriptions SET enabled=0 WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)

	result := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?source_id=youtube-anthropic&include_counts=0", "")
	if result.Code != http.StatusOK {
		t.Fatalf("scoped source status = %d, body = %s", result.Code, result.Body.String())
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("request-triggered repair overwrote explicit unsubscribe: enabled=%d", enabled)
	}
}

func TestSourceGatewayEmptyCatalogPreservesProjectionFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_status='healthy',status='ok' WHERE id IN ('youtube-anthropic','youtube-google-deepmind')`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	// The proxy answered successfully but returned no catalog. This is a
	// transient/incomplete projection, not evidence that every local source is
	// blocked.
	mock.catalog = nil
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)
	err := reconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv())
	if !errors.Is(err, errSourceGatewayEmptyCatalog) {
		t.Fatalf("empty catalog error = %v, want retryable empty-catalog error", err)
	}
	mock.mu.Lock()
	requests := append([]string(nil), mock.requests...)
	mock.mu.Unlock()
	if len(requests) < 2 || !strings.Contains(requests[0], "/subscriptions") || !strings.Contains(requests[1], "/catalog") {
		t.Fatalf("empty catalog must initialize/read subscriptions before catalog validation: %#v", requests)
	}
	var firstStatus, secondStatus string
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='youtube-google-deepmind'`).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "healthy" || secondStatus != "healthy" {
		t.Fatalf("empty catalog mass-blocked local projection: first=%q second=%q", firstStatus, secondStatus)
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled == 0 {
		t.Fatal("empty catalog must not alter the existing subscription")
	}
}

func TestSourceGatewayNeedsInitialHeadTreatsUnsyncedRowsAsInitialUnit(t *testing.T) {
	tests := []struct {
		name   string
		source FeedSource
		want   bool
	}{
		{
			name: "records seen without successful sync still needs head",
			source: FeedSource{
				CollectionMode: "source_gateway",
				Status:         "ok",
				GatewayStatus:  "healthy",
				RecordsSeen:    17,
				EntryCount:     0,
			},
			want: true,
		},
		{
			name: "successful empty sync is stable",
			source: FeedSource{
				CollectionMode: "source_gateway",
				Status:         "ok",
				GatewayStatus:  "healthy",
				RecordsSeen:    17,
				EntryCount:     0,
				LastSyncAt:     "2026-09-02T00:00:00Z",
			},
			want: false,
		},
		{
			name: "cached entries do not need initial head",
			source: FeedSource{
				CollectionMode: "source_gateway",
				Status:         "ok",
				GatewayStatus:  "healthy",
				EntryCount:     3,
			},
			want: false,
		},
		{
			name: "auth remains terminal",
			source: FeedSource{
				CollectionMode: "source_gateway",
				GatewayStatus:  "auth",
				EntryCount:     0,
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sourceGatewayNeedsInitialHead(test.source); got != test.want {
				t.Fatalf("sourceGatewayNeedsInitialHead(%#v) = %t, want %t", test.source, got, test.want)
			}
		})
	}
}

func TestSourceGatewayInitializingSubscriptionHiddenFromCatalogStaysRetryableFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	// Simulate a cold source whose catalog worker has not produced a feed yet.
	// The server still returns the user's initializing subscription, while the
	// default catalog intentionally omits that source until it is healthy.
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_status='unavailable', status='unavailable', gateway_cursor='', gateway_has_more=0, last_sync_at='', records_seen=0 WHERE id='youtube-google-deepmind'`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{
		{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"},
		{SourceID: "youtube-google-deepmind", Enabled: true, Status: "initializing"},
	}
	mock.pages["youtube-google-deepmind"] = [][]sourceGatewayEntry{{
		gatewayTestEntry("youtube-google-deepmind", "cold-1", "冷启动文章", "https://www.youtube.com/watch?v=cold-1", "cold-hash-1", "2026-08-30T00:00:00Z"),
	}}
	mock.configure(t)
	if err := reconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var gatewayStatus string
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='youtube-google-deepmind'`).Scan(&gatewayStatus); err != nil {
		t.Fatal(err)
	}
	if gatewayStatus != "initializing" {
		t.Fatalf("hidden initializing subscription status = %q, want initializing", gatewayStatus)
	}
	source := mustFeedSource(t, "youtube-google-deepmind")
	if !sourceGatewayNeedsInitialHead(source) {
		t.Fatalf("initializing hidden source must remain eligible for its first head: %#v", source)
	}
	pulled, err := pullGatewayHistoryPage(context.Background(), source.ID)
	if err != nil || pulled != 1 {
		t.Fatalf("initializing hidden source head pull = %d, %v", pulled, err)
	}
	if got := mock.entryCallCount("source_id=youtube-google-deepmind"); got != 1 {
		t.Fatalf("initializing hidden source entries calls = %d, want 1", got)
	}
}

func TestSourceGatewayEmptySubscriptionProjectionDoesNotMassBlockFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_status='healthy',status='ok' WHERE id='youtube-google-deepmind'`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	// Empty subscriptions can be a cold proxy response. Keep the prior source
	// projection until a complete subscription response arrives.
	mock.subs = nil
	mock.configure(t)
	if err := reconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv()); err != nil {
		t.Fatalf("empty subscription projection should be non-destructive: %v", err)
	}
	var status string
	if err := db.QueryRow(`SELECT gateway_status FROM feed_sources WHERE id='youtube-google-deepmind'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "blocked" {
		t.Fatal("empty subscription response must not mass-block sources")
	}
}

func TestSourceGatewayReconcileFailureUsesShortRetryFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.failWith = http.StatusServiceUnavailable
	mock.configure(t)
	sourceGatewayState = gatewayRuntime{}
	err := maybeReconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv(), false)
	if err == nil {
		t.Fatal("gateway failure must be returned to the scheduler")
	}
	sourceGatewayState.mu.Lock()
	retryAt, reconciledAt := sourceGatewayState.reconcileRetryAt, sourceGatewayState.reconciledAt
	sourceGatewayState.mu.Unlock()
	if retryAt.IsZero() || !retryAt.After(time.Now()) {
		t.Fatalf("failed reconcile must schedule a short retry, retryAt=%s", retryAt)
	}
	if !reconciledAt.IsZero() {
		t.Fatalf("failed reconcile must not be recorded as successful: reconciledAt=%s", reconciledAt)
	}
}

func TestSourceGatewayHistoryPullResumesFromCursorFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{
		{gatewayTestEntry("youtube-anthropic", "p0", "新页", "https://www.youtube.com/watch?v=p0", "hp0", "2026-08-16T00:00:00Z")},
		{gatewayTestEntry("youtube-anthropic", "p1", "历史页", "https://www.youtube.com/watch?v=p1", "hp1", "2026-08-01T00:00:00Z")},
	}
	mock.configure(t)
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_cursor='cursor-0', gateway_has_more=1 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}

	pulled, err := pullGatewayHistoryPage(context.Background(), "youtube-anthropic")
	if err != nil || pulled != 1 {
		t.Fatalf("history pull = %d, %v", pulled, err)
	}
	var cursor string
	var hasMore int
	if err := db.QueryRow(`SELECT gateway_cursor,gateway_has_more FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&cursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	if cursor != "cursor-1" || hasMore != 1 {
		t.Fatalf("cursor=%q hasMore=%d after first pull", cursor, hasMore)
	}
	pulled, err = pullGatewayHistoryPage(context.Background(), "youtube-anthropic")
	if err != nil || pulled != 1 {
		t.Fatalf("second history pull = %d, %v", pulled, err)
	}
	if err := db.QueryRow(`SELECT gateway_cursor,gateway_has_more FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&cursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	if cursor != "" || hasMore != 0 {
		t.Fatalf("walk must terminate at the last page: cursor=%q hasMore=%d", cursor, hasMore)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='youtube-anthropic'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("history rows = %d, want 2", count)
	}
}

func TestSourceGatewayHistoryPullRecoversFromInvalidCursorFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{
		{gatewayTestEntry("youtube-anthropic", "p0", "新页", "https://www.youtube.com/watch?v=p0", "hp0", "2026-08-16T00:00:00Z")},
	}
	mock.configure(t)
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_cursor='cursor-99', gateway_has_more=1 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	pulled, err := pullGatewayHistoryPage(context.Background(), "youtube-anthropic")
	if err != nil || pulled != 1 {
		t.Fatalf("invalid cursor must reset and refetch from the head: %d, %v", pulled, err)
	}
	var cursor string
	if err := db.QueryRow(`SELECT gateway_cursor FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if cursor != "" {
		t.Fatalf("single-page walk completes with an empty cursor, got %q", cursor)
	}
}

func TestSourceGatewayStatusHandlerFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	result := callJSON(t, sourceGatewayStatusHandler, http.MethodGet, "/api/source-gateway/status", "")
	if result.Code != http.StatusOK {
		t.Fatalf("status = %d", result.Code)
	}
	var payload struct {
		Data struct {
			Configured bool           `json:"configured"`
			Available  bool           `json:"available"`
			Message    string         `json:"message"`
			Sources    map[string]int `json:"sources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Data.Configured || !payload.Data.Available || payload.Data.Message != "" {
		t.Fatalf("fresh gateway state = %#v", payload.Data)
	}
	if payload.Data.Sources["initializing"] == 0 {
		t.Fatalf("default sources start as initializing after the switch: %#v", payload.Data.Sources)
	}

	// After a failed sync attempt the status surfaces the offline copy.
	mock.failWith = http.StatusServiceUnavailable
	_ = syncGatewaySourceWithinBudget(context.Background(), context.Background(), mustFeedSource(t, "youtube-anthropic"))
	result = callJSON(t, sourceGatewayStatusHandler, http.MethodGet, "/api/source-gateway/status", "")
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Data.Available || !strings.Contains(payload.Data.Message, "显示上次内容") {
		t.Fatalf("degraded gateway state = %#v", payload.Data)
	}
}

// Compatibility defaults are server-owned, standard feeds stay local, and
// removed catalog sources keep their SQLite history without being scheduled.
func TestSourceGatewayMigrationPreservesLocalDataFlow(t *testing.T) {
	db = openTestDB(t)
	var defaults, switched, local, retired, outbox int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE is_default=1`).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE is_default=1 AND collection_mode='source_gateway' AND url='source-gateway://' || id AND etag='' AND next_sync_at='' AND gateway_status='initializing'`).Scan(&switched); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE is_default=1 AND collection_mode='app' AND url LIKE 'https://%' AND next_sync_at=''`).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources
		WHERE is_default=0 AND collection_mode='agent' AND hidden=1 AND url='source-retired://' || id
		  AND id IN (
		      SELECT value FROM json_each(?)
		      UNION ALL
		      SELECT value FROM json_each(?)
		  )`, sourceGatewayDecommissionedSourceIDsJSON, sourceGatewayRetiredSourceIDsJSON).Scan(&retired); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if defaults != 40 || switched != 17 || local != 4 || retired != 33 || outbox != 17 {
		t.Fatalf("public collection paths: defaults=%d gateway=%d local=%d retired=%d outbox=%d", defaults, switched, local, retired, outbox)
	}
	var stranded int
	if err := db.QueryRow(`SELECT COUNT(*)
	FROM feed_sources f
	JOIN feed_subscriptions s ON s.source_id=f.id
	LEFT JOIN feed_catalog_entries c ON c.source_id=f.id
	WHERE s.enabled=1 AND f.is_default=1
	  AND NOT (
	      (f.collection_mode='source_gateway' AND f.id IN (SELECT value FROM json_each(?)))
	      OR (f.collection_mode='app' AND f.id IN (SELECT value FROM json_each(?)))
	      OR (f.collection_mode='app' AND c.catalog_origin='weekend-project-space/top-rss-list')
	  )`, sourceGatewayMaintainedSourceIDsJSON, localStandardFeedSourceIDsJSON).Scan(&stranded); err != nil {
		t.Fatal(err)
	}
	if stranded != 0 {
		t.Fatalf("enabled subscriptions without a collector: %d", stranded)
	}
}

func TestNonCoreSourceRestoreUpgradeFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "074_")
	coreIDs := map[string]bool{
		"wechat-aiera": true, "wechat-jiqizhixin": true, "wechat-geekpark": true, "wechat-qbitai": true,
		"bilibili-tech-aesthetics": true, "bilibili-yingshijufeng": true, "bilibili-geekerwan": true, "bilibili-hardware-tea": true,
		"youtube-anthropic": true, "youtube-google-deepmind": true, "youtube-nvidia": true, "youtube-fireship": true,
		"elon-musk-x": true, "sam-altman-x": true, "andrej-karpathy-x": true, "demis-hassabis-x": true,
	}
	type collectorConfig struct {
		url      string
		interval int
	}
	before := map[string]collectorConfig{}
	rows, err := conn.Query(`SELECT id,url,sync_interval_seconds FROM feed_sources WHERE is_default=1 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, sourceURL string
		var interval int
		if err := rows.Scan(&id, &sourceURL, &interval); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if !coreIDs[id] {
			before[id] = collectorConfig{url: sourceURL, interval: interval}
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(before) != 38 {
		t.Fatalf("pre-074 non-core defaults = %d, want 38", len(before))
	}
	if _, err := conn.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('entry-noncore','ext-noncore','升级前文章','https://example.com/old','OpenAI News','openai-news','gateway','rss','article','','2026-08-01T00:00:00Z','摘要','text','','','','',0,'noncore-hash','2026-08-01T00:00:00Z','public_read','official','','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:entry-noncore',0,1,1,0,'2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, conn, "074_source_gateway_collection.sql")
	if _, err := conn.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('openai-news','retry_wait','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "075_restore_noncore_local_sources.sql")
	for id, want := range before {
		var mode, sourceURL string
		var interval int
		if err := conn.QueryRow(`SELECT collection_mode,url,sync_interval_seconds FROM feed_sources WHERE id=?`, id).Scan(&mode, &sourceURL, &interval); err != nil {
			t.Fatal(err)
		}
		if mode != "app" || sourceURL != want.url || interval != want.interval {
			t.Errorf("restored %s = mode=%q url=%q interval=%d, want app %q %d", id, mode, sourceURL, interval, want.url, want.interval)
		}
	}

	var mode, sourceURL, nextSync, lastError, gatewayStatus string
	if err := conn.QueryRow(`SELECT collection_mode,url,next_sync_at,last_error,gateway_status FROM feed_sources WHERE id='openai-news'`).Scan(&mode, &sourceURL, &nextSync, &lastError, &gatewayStatus); err != nil {
		t.Fatal(err)
	}
	if mode != "app" || sourceURL != "https://openai.com/news/rss.xml" || nextSync != "" || lastError != "" || gatewayStatus != "" {
		t.Fatalf("restored source = mode=%q url=%q next=%q error=%q gateway=%q", mode, sourceURL, nextSync, lastError, gatewayStatus)
	}

	applyMigrationFile(t, conn, "092_server_first_public_sources.sql")
	applyMigrationFile(t, conn, "099_hybrid_feed_ownership.sql")
	if err := conn.QueryRow(`SELECT collection_mode,url,next_sync_at,last_error,gateway_status FROM feed_sources WHERE id='openai-news'`).Scan(&mode, &sourceURL, &nextSync, &lastError, &gatewayStatus); err != nil {
		t.Fatal(err)
	}
	if mode != "app" || sourceURL != "https://openai.com/news/rss.xml" || nextSync != "" || lastError != "" || gatewayStatus != "" {
		t.Fatalf("hybrid local source = mode=%q url=%q next=%q error=%q gateway=%q", mode, sourceURL, nextSync, lastError, gatewayStatus)
	}
	var retiredMode, retiredURL string
	var retiredDefault, retiredHidden int
	if err := conn.QueryRow(`SELECT collection_mode,url,is_default,hidden FROM feed_sources WHERE id='wechat-huxiu'`).Scan(&retiredMode, &retiredURL, &retiredDefault, &retiredHidden); err != nil {
		t.Fatal(err)
	}
	if retiredMode != "agent" || retiredURL != "source-retired://wechat-huxiu" || retiredDefault != 0 || retiredHidden != 1 {
		t.Fatalf("retired source = mode=%q url=%q default=%d hidden=%d", retiredMode, retiredURL, retiredDefault, retiredHidden)
	}
	var title, requestStatus string
	var starred, readLater int
	if err := conn.QueryRow(`SELECT n.title,COALESCE(p.starred,0),COALESCE(p.read_later,0) FROM news n LEFT JOIN item_preferences p ON p.item_key='news:'||n.id WHERE n.id='entry-noncore'`).Scan(&title, &starred, &readLater); err != nil {
		t.Fatal(err)
	}
	if title != "升级前文章" || starred != 1 || readLater != 1 {
		t.Fatalf("restored source lost local data: %q starred=%d later=%d", title, starred, readLater)
	}
	if err := conn.QueryRow(`SELECT status FROM sync_requests WHERE source_id='openai-news' ORDER BY id DESC LIMIT 1`).Scan(&requestStatus); err != nil {
		t.Fatal(err)
	}
	if requestStatus != "superseded" {
		t.Fatalf("stale gateway request = %q, want superseded", requestStatus)
	}
}

// 升级路径：先跑到 073 的旧库，再应用 074，验证旧文章与用户偏好原样保留。
func TestSourceGatewayMigrationUpgradePathPreservesNewsFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "074_")
	if _, err := conn.Exec(`UPDATE feed_sources SET etag='etag-1', last_modified='Mon, 01 Jan 2026', next_sync_at='2026-08-17T00:00:00Z' WHERE id='wechat-qbitai'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('entry-upgrade','ext-up','升级前文章','https://example.com/up','量子位','wechat-qbitai','app-fetch','rss','article','','2026-08-01T00:00:00Z','摘要','text','','','','',0,'up-hash','2026-08-01T00:00:00Z','public_read','official','','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:entry-upgrade',1,1,1,0.25,'2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at) VALUES('entry-upgrade','正文快照','text','2026-08-01T00:00:00Z','ok','','https://example.com/up','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('wechat-qbitai','pending','2026-08-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, conn, "074_source_gateway_collection.sql")

	var mode, url, etag, nextSync, gatewayStatus string
	if err := conn.QueryRow(`SELECT collection_mode,url,etag,next_sync_at,gateway_status FROM feed_sources WHERE id='wechat-qbitai'`).Scan(&mode, &url, &etag, &nextSync, &gatewayStatus); err != nil {
		t.Fatal(err)
	}
	if mode != "source_gateway" || url != "source-gateway://wechat-qbitai" || etag != "" || nextSync != "" || gatewayStatus != "initializing" {
		t.Fatalf("switched source = %q url=%q etag=%q next=%q gw=%q", mode, url, etag, nextSync, gatewayStatus)
	}
	var title, content string
	var starred, readLater int
	if err := conn.QueryRow(`SELECT n.title,COALESCE(p.starred,0),COALESCE(p.read_later,0),COALESCE(f.content_text,'') FROM news n
LEFT JOIN item_preferences p ON p.item_key='news:'||n.id
LEFT JOIN entry_full_texts f ON f.entry_id=n.id WHERE n.id='entry-upgrade'`).Scan(&title, &starred, &readLater, &content); err != nil {
		t.Fatal(err)
	}
	if title != "升级前文章" || starred != 1 || readLater != 1 || content != "正文快照" {
		t.Fatalf("upgrade wiped local data: %q starred=%d later=%d content=%q", title, starred, readLater, content)
	}
	var requestStatus string
	if err := conn.QueryRow(`SELECT status FROM sync_requests WHERE source_id='wechat-qbitai'`).Scan(&requestStatus); err != nil {
		t.Fatal(err)
	}
	if requestStatus != "superseded" {
		t.Fatalf("pending direct-fetch sync request = %q, want superseded", requestStatus)
	}
}

func TestSourceGatewayReconcileDoesNotStompLocalUnsubscribeFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)
	// 用户已在本地退订：reconcile 不得把 enabled 改回去。
	if _, err := db.Exec(`UPDATE feed_subscriptions SET enabled=0, updated_at=CURRENT_TIMESTAMP WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if err := reconcileSourceGateway(context.Background(), newSourceGatewayBridgeFromEnv()); err != nil {
		t.Fatal(err)
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("reconcile must not overwrite a local unsubscribe")
	}
}

func TestSourceGatewayLocalToggleForwardsToCloudFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.configure(t)
	forwardGatewaySubscription("youtube-anthropic", false)
	var putFound bool
	for _, request := range mock.requests {
		if strings.HasPrefix(request, "PUT ") && strings.Contains(request, "/subscriptions/youtube-anthropic") {
			putFound = true
		}
	}
	if !putFound {
		t.Fatalf("local unsubscribe must be forwarded to the cloud subscription API: %#v", mock.requests)
	}
	// 非网关来源不转发。
	mock.requests = nil
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,is_default,status,collection_mode,created_at,updated_at) VALUES('user-feed-x','rss','自添加','https://example.com/feed',0,'ok','app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	forwardGatewaySubscription("user-feed-x", false)
	for _, request := range mock.requests {
		if strings.HasPrefix(request, "PUT ") {
			t.Fatalf("user-added source must not hit the gateway: %s", request)
		}
	}
}

func TestSourceGatewaySubscriptionOutboxRetriesFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "outbox-token" || r.Method != http.MethodPut {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "SRC_UPSTREAM_UNAVAILABLE"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ok": true}})
	}))
	defer server.Close()
	bridge := newSourceGatewayBridge(server.URL+sourceGatewayPathSuffix, "outbox-token", server.Client())

	if err := enqueueSourceGatewaySubscription("openai-news", true); err != nil {
		t.Fatal(err)
	}
	if err := drainSourceGatewaySubscriptionOutbox(context.Background(), bridge, 1); err != nil {
		t.Fatal(err)
	}
	var attempt int
	var nextRetry, lastError string
	if err := db.QueryRow(`SELECT attempt_count,next_retry_at,last_error FROM source_gateway_subscription_outbox WHERE source_id='openai-news'`).Scan(&attempt, &nextRetry, &lastError); err != nil {
		t.Fatal(err)
	}
	if attempt != 1 || nextRetry == "" || lastError != "unavailable" {
		t.Fatalf("retry state attempt=%d next=%q error=%q", attempt, nextRetry, lastError)
	}
	if _, err := db.Exec(`UPDATE source_gateway_subscription_outbox SET next_retry_at='' WHERE source_id='openai-news'`); err != nil {
		t.Fatal(err)
	}
	if err := drainSourceGatewaySubscriptionOutbox(context.Background(), bridge, 1); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id='openai-news'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || pending != 0 {
		t.Fatalf("outbox requests=%d pending=%d", requests, pending)
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSensitiveFeedURLCredentialPolicyUnit(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "signature", raw: "https://example.com/feed?signature=value"},
		{name: "short signature", raw: "https://example.com/feed?sig=value"},
		{name: "AWS signature", raw: "https://example.com/feed?X-Amz-Signature=value"},
		{name: "Google credential", raw: "https://example.com/feed?X-Goog-Credential=value"},
		{name: "segmented access key", raw: "https://example.com/feed?access_key=value"},
		{name: "camel access token", raw: "https://example.com/feed?accessToken=value"},
		{name: "AWS access key id", raw: "https://example.com/feed?AccessKeyId=value"},
		{name: "signal is benign", raw: "https://example.com/feed?signal=strong", want: true},
		{name: "design is benign", raw: "https://example.com/feed?design=compact", want: true},
		{name: "assignment is benign", raw: "https://example.com/feed?assignment=weekly", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validPublicFeedURL(test.raw); got != test.want {
				t.Fatalf("validPublicFeedURL(%q)=%v want=%v", test.raw, got, test.want)
			}
		})
	}
}

func TestIsGatewayManagedEntryUnit(t *testing.T) {
	tests := []struct {
		name           string
		gatewayEntryID string
		collectionMode string
		want           bool
	}{
		{name: "ordinary local entry", collectionMode: "app"},
		{name: "mapped gateway entry", gatewayEntryID: "sg_entry_1", collectionMode: "app", want: true},
		{name: "legacy gateway source row", collectionMode: "source_gateway", want: true},
		{name: "whitespace is empty", gatewayEntryID: "  ", collectionMode: "  "},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isGatewayManagedEntry(test.gatewayEntryID, test.collectionMode); got != test.want {
				t.Fatalf("isGatewayManagedEntry(%q,%q)=%v want=%v", test.gatewayEntryID, test.collectionMode, got, test.want)
			}
		})
	}
}

func TestPublicAndPrivateLocalSourceCreateFailClosedFlow(t *testing.T) {
	db = openTestDB(t)
	var sourcesBefore, requestsBefore int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources`).Scan(&sourcesBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests`).Scan(&requestsBefore); err != nil {
		t.Fatal(err)
	}

	createdResponse := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"https://example.com/feed.xml"}`)
	if createdResponse.Code != http.StatusOK {
		t.Fatalf("ordinary public source status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	var created FeedSource
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.ID, "feed-") || created.CollectionMode != "app" || created.URL != "https://example.com/feed.xml" {
		t.Fatalf("ordinary public source=%#v", created)
	}

	rejected := []struct {
		name   string
		body   string
		secret string
	}{
		{name: "gateway mode bypass", body: `{"url":"https://example.com/feed.xml","collection_mode":"source_gateway"}`},
		{name: "private local remains closed", body: `{"url":"https://example.com/private.xml?signature=do-not-leak","collection_mode":"private_local"}`, secret: "do-not-leak"},
	}
	for _, test := range rejected {
		t.Run(test.name, func(t *testing.T) {
			response := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.secret != "" && strings.Contains(response.Body.String(), test.secret) {
				t.Fatalf("secret leaked in response: %s", response.Body.String())
			}
		})
	}

	var sourcesAfter, requestsAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources`).Scan(&sourcesAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests`).Scan(&requestsAfter); err != nil {
		t.Fatal(err)
	}
	if sourcesAfter != sourcesBefore+1 || requestsAfter != requestsBefore {
		t.Fatalf("create state: sources=%d/%d requests=%d/%d", sourcesBefore, sourcesAfter, requestsBefore, requestsAfter)
	}
}

func TestGatewaySourceSettingsUpdatePreservesServerFirstIdentityFlow(t *testing.T) {
	db = openTestDB(t)
	before, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if before.CollectionMode != "source_gateway" || before.URL != "source-gateway://youtube-anthropic" {
		t.Fatalf("unexpected gateway fixture: mode=%q url=%q", before.CollectionMode, before.URL)
	}

	response := callJSON(t, sourceHandler, http.MethodPatch, "/api/sources", `{
			"id":"youtube-anthropic",
		"sync_interval_seconds":3600,
		"retention_days":365,
		"retention_limit":1500,
		"auto_extract":false,
		"hidden":true
	}`)
	if response.Code != http.StatusOK {
		t.Fatalf("settings update status=%d body=%s", response.Code, response.Body.String())
	}

	after, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if after.URL != before.URL || after.Name != before.Name || after.Kind != before.Kind || after.CollectionMode != "source_gateway" {
		t.Fatalf("gateway identity changed: before=%#v after=%#v", before, after)
	}
	if after.SyncIntervalSecs != 3600 || after.RetentionDays != 365 || after.RetentionLimit != 1500 || after.AutoExtract || !after.Hidden {
		t.Fatalf("gateway settings not saved: %#v", after)
	}
	var requests int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id='youtube-anthropic'`).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("settings update queued local collection: %d requests", requests)
	}

	identityMutation := callJSON(t, sourceHandler, http.MethodPatch, "/api/sources", `{
			"id":"youtube-anthropic",
		"url":"https://example.com/feed.xml",
		"collection_mode":"app"
	}`)
	if identityMutation.Code != http.StatusBadRequest {
		t.Fatalf("identity mutation status=%d body=%s", identityMutation.Code, identityMutation.Body.String())
	}
	unchanged, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.URL != before.URL || unchanged.CollectionMode != "source_gateway" {
		t.Fatalf("rejected identity mutation changed source: %#v", unchanged)
	}
}

func TestUpdateGatewaySourceSettingsUnit(t *testing.T) {
	db = openTestDB(t)
	current, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	appMode := "app"
	tooFrequent := 299
	tests := []struct {
		name string
		req  sourceMutationRequest
	}{
		{name: "URL mutation", req: sourceMutationRequest{URL: "https://example.com/feed.xml"}},
		{name: "kind mutation", req: sourceMutationRequest{Kind: "atom"}},
		{name: "name mutation", req: sourceMutationRequest{Name: "Other source"}},
		{name: "description mutation", req: sourceMutationRequest{Description: "Other description"}},
		{name: "mode mutation", req: sourceMutationRequest{CollectionMode: &appMode}},
		{name: "invalid interval", req: sourceMutationRequest{SyncIntervalSecs: &tooFrequent}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, updateErr := updateGatewaySourceSettings(current, test.req)
			if updateErr == nil {
				t.Fatal("expected gateway settings update to fail closed")
			}
		})
	}

	after, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if after.URL != current.URL || after.Kind != current.Kind || after.Name != current.Name ||
		after.Description != current.Description || after.CollectionMode != current.CollectionMode ||
		after.SyncIntervalSecs != current.SyncIntervalSecs {
		t.Fatalf("rejected gateway updates changed source: before=%#v after=%#v", current, after)
	}
}

func TestLegacyLocalCollectionPreservesOfflineStateWithoutNetworkFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM sync_requests WHERE source_id='hacker-news'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news
(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,content_hash,fetched_at,updated_at)
VALUES('server-first-cached-entry','缓存文章','https://example.com/cached','Hacker News AI','hacker-news','2026-08-20T00:00:00Z','缓存摘要','text','','cached-hash','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at)
VALUES('news:server-first-cached-entry',1,1,1,0.75,'2026-08-20T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at,title_override,pinned,muted,sort_order)
VALUES('hacker-news',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'我的 HN',1,0,7)
ON CONFLICT(source_id) DO UPDATE SET enabled=1,title_override='我的 HN',pinned=1,sort_order=7,updated_at=CURRENT_TIMESTAMP`); err != nil {
		t.Fatal(err)
	}
	if _, err := enqueueSyncRequest("hacker-news"); err != nil {
		t.Fatal(err)
	}

	previousOperationalFetch := operationalSourceFetch
	previousLocalFetch := localCustomFeedFetch
	requests := 0
	operationalSourceFetch = func(context.Context, string, string, string) (feedFetchResponse, error) {
		requests++
		return feedFetchResponse{}, errors.New("network must not be used")
	}
	localCustomFeedFetch = func(context.Context, string, string, string) (feedFetchResponse, error) {
		requests++
		return feedFetchResponse{}, errors.New("network must not be used")
	}
	t.Cleanup(func() {
		operationalSourceFetch = previousOperationalFetch
		localCustomFeedFetch = previousLocalFetch
	})
	if err := syncRequestedSourceContext(context.Background(), "hacker-news"); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("legacy source performed %d network requests", requests)
	}

	var status, lastError, sourceURL, titleOverride, requestStatus string
	var newsCount, read, starred, readLater, enabled, pinned, sortOrder int
	if err := db.QueryRow(`SELECT status,last_error,url FROM feed_sources WHERE id='hacker-news'`).Scan(&status, &lastError, &sourceURL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(MAX(p.is_read),0),COALESCE(MAX(p.starred),0),COALESCE(MAX(p.read_later),0)
FROM news n LEFT JOIN item_preferences p ON p.item_key='news:' || n.id
WHERE n.id='server-first-cached-entry'`).Scan(&newsCount, &read, &starred, &readLater); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT enabled,title_override,pinned,sort_order FROM feed_subscriptions WHERE source_id='hacker-news'`).Scan(&enabled, &titleOverride, &pinned, &sortOrder); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM sync_requests WHERE source_id='hacker-news' ORDER BY id DESC LIMIT 1`).Scan(&requestStatus); err != nil {
		t.Fatal(err)
	}
	if status != "partial" || lastError != "该来源由 Agent/Connector 采集" || sourceURL != "source-retired://hacker-news" || newsCount != 1 || read != 1 || starred != 1 || readLater != 1 {
		t.Fatalf("offline state changed: status=%q error=%q url=%q news=%d prefs=%d/%d/%d", status, lastError, sourceURL, newsCount, read, starred, readLater)
	}
	if enabled != 1 || titleOverride != "我的 HN" || pinned != 1 || sortOrder != 7 || requestStatus != "agent_required" {
		t.Fatalf("subscription/request state changed: enabled=%d title=%q pinned=%d sort=%d request=%q", enabled, titleOverride, pinned, sortOrder, requestStatus)
	}

	entries := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=hacker-news&limit=10", "")
	if entries.Code != http.StatusOK || !strings.Contains(entries.Body.String(), "server-first-cached-entry") {
		t.Fatalf("cached entry unavailable: status=%d body=%s", entries.Code, entries.Body.String())
	}
}

func TestLegacySourcePreviewUsesSQLiteAndAgentListRedactsSignedURLFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,is_default,status,collection_mode,created_at,updated_at)
VALUES
('legacy-preview','rss','Legacy Preview','https://example.com/preview',0,'partial','app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('legacy-empty','rss','Legacy Empty','https://example.com/empty',0,'unavailable','hybrid',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
('legacy-signed','rss','Legacy Signed','https://example.com/private?X-Amz-Signature=do-not-leak',0,'partial','app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO news(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,content_hash,fetched_at,updated_at)
VALUES('legacy-preview-entry','本地预览','https://example.com/post','Legacy Preview','legacy-preview','2026-08-20T00:00:00Z','缓存摘要','text','','preview-hash','2026-08-20T00:00:00Z','2026-08-20T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	preview := callJSON(t, sourcePreviewHandler, http.MethodGet, "/api/sources/preview?source_id=legacy-preview", "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"preview_origin":"local_sqlite"`) || !strings.Contains(preview.Body.String(), "legacy-preview-entry") {
		t.Fatalf("cached preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	empty := callJSON(t, sourcePreviewHandler, http.MethodGet, "/api/sources/preview?source_id=legacy-empty", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"preview_error":""`) || !strings.Contains(empty.Body.String(), `"preview_origin":"local_sqlite"`) || strings.Contains(empty.Body.String(), `"preview_origin":"public_fetch"`) {
		t.Fatalf("empty preview status=%d body=%s", empty.Code, empty.Body.String())
	}

	listed := appDataCall(t, "/api/app-data/v1/operations/source.list?q=Legacy+Signed", "", "")
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), "do-not-leak") {
		t.Fatalf("signed source list status=%d body=%s", listed.Code, listed.Body.String())
	}
	var payload struct {
		Sources []map[string]any `json:"sources"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) != 1 || payload.Sources[0]["id"] != "legacy-signed" {
		t.Fatalf("signed source projection=%#v", payload.Sources)
	}
	if _, ok := payload.Sources[0]["url"]; ok {
		t.Fatalf("private url field leaked: %#v", payload.Sources[0])
	}
	if _, ok := payload.Sources[0]["source_url"]; ok {
		t.Fatalf("private source_url field leaked: %#v", payload.Sources[0])
	}
}

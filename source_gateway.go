package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"focus-space/internal/bridges"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Source Gateway integration (2026-08-17 Plaud Source Gateway V1).
//
// The dashboard never talks to the cloud Source Gateway directly.  It only
// calls the device-local restricted proxy exposed by zettlab-local-server over
// loopback, using the same AppHost-injected credential pattern as the device
// data bridge:
//
//	ZAPP_SOURCE_GATEWAY_BASE_URL  loopback http URL whose path ends with
//	                              "/source-gateway" (app-runtime scoped)
//	ZAPP_SOURCE_GATEWAY_TOKEN     app-scoped token sent as X-Zettlab-App-Data-Token
//
// When the dedicated variables are absent (older local-server), the client
// falls back to deriving the base URL from ZAPP_DEVICE_DATA_BASE_URL by
// replacing the trailing "/device-data/v1" with "/source-gateway" and reusing
// ZAPP_DEVICE_DATA_TOKEN.  There is no runtime switch back to direct fetching:
// when the gateway is unavailable the reader serves the last successful local
// snapshot and reports "云端来源暂不可用，显示上次内容".
const (
	sourceGatewayBaseURLEnv  = "ZAPP_SOURCE_GATEWAY_BASE_URL"
	sourceGatewayTokenEnv    = "ZAPP_SOURCE_GATEWAY_TOKEN"
	sourceGatewayPathSuffix  = "/source-gateway"
	sourceGatewayPageLimit   = 50
	sourceGatewayMaxPagesRun = 4
	// History pulls walk one page per reader-driven request so a scroll never
	// turns into an unbounded catch-up inside a UI request.
	sourceGatewayHistoryPagesPerPull = 1
	// A reader request may be the first request after an app install.  The
	// loopback proxy can be cold while local-server starts its upstream bridge;
	// two seconds made a valid first page look like an empty source and the
	// subsequent cooldown hid it for another 30 seconds.  Keep the request
	// bounded, but give both the initial head and one history page enough time
	// to complete.
	sourceGatewayReaderHistoryTimeout  = 10 * time.Second
	sourceGatewayReaderHistoryCooldown = 30 * time.Second
	// Reconciliation is metadata-only and must not consume the whole source
	// fetch budget.  A failed/slow catalog request is retried later while the
	// requested source can still fetch its head page.
	sourceGatewayReconcileTimeout       = 3 * time.Second
	sourceGatewayReconcileRetryInterval = 5 * time.Second
	sourceGatewayListTimeout            = 20 * time.Second
	sourceGatewayFulltextTimeout        = 30 * time.Second
	sourceGatewayMediaTimeout           = 60 * time.Second
	sourceGatewayMaxPageBytes           = 4 << 20
	sourceGatewayMaxFulltextBytes       = 2 << 20
	sourceGatewayMaxMediaBytes          = 16 << 20
	sourceGatewayMaxPlayableMediaBytes  = 128 << 20
	sourceGatewayReconcileInterval      = 10 * time.Minute
	sourceGatewaySubscriptionBatch      = 50
	sourceGatewaySubscriptionWorkers    = 4
	sourceGatewaySchemaVersion          = "source_gateway.v1"
	// A newly installed dashboard can have the local source directory but no
	// materialized subscription projection yet.  Reconcile that metadata in a
	// bounded background pass so FOCUS_SPACE_LAZY_SYNC remains truly lazy for
	// article bodies while the first navigation request still sees usable
	// subscription state.  The overall budget is deliberately finite: an old
	// local-server without the gateway must not hold process shutdown open.
	sourceGatewayStartupReconcileAttempts = 3
	sourceGatewayStartupReconcileBudget   = 20 * time.Second
	// gatewayOrigin marks rows written by the Source Gateway sync.  Like
	// "app-fetch" it is an automated public-content origin, so later gateway
	// refreshes may update the row without tripping the user-projection
	// conflict guard in upsertFetchedNewsTx.
	gatewayOrigin            = "source-gateway"
	gatewayUnavailableNotice = "云端来源暂不可用，显示上次内容"
	gatewayAuthNotice        = "云端登录态已失效，请重新登录后重试"
	// The app-runtime proxy keeps its established HTTP-200 envelope for local
	// failures.  Keep these values here (rather than importing local-server)
	// because the dashboard is shipped as a self-contained Agent package and
	// must remain compatible with both the old and new proxy binaries.
	sourceGatewayLocalInvalidParamsCode       = "93001"
	sourceGatewayLocalAuthRequiredCode        = "93002"
	sourceGatewayLocalUpstreamUnavailableCode = "93003"
	sourceGatewayLocalMediaUnavailableCode    = "93004"
)

var (
	errSourceGatewayNotConfigured = errors.New("source gateway is not configured")
	errSourceGatewayUnavailable   = errors.New("source gateway is temporarily unavailable")
	errSourceGatewayAuth          = errors.New("source gateway credentials were rejected")
	errSourceGatewayInvalidCursor = errors.New("source gateway cursor is invalid")
	errSourceGatewayNotFound      = errors.New("source gateway entry is unavailable")
	errSourceGatewayEmptyCatalog  = errors.New("source gateway returned an empty catalog")
)

type sourceGatewayBridge struct {
	baseURL *url.URL
	token   string
	client  *http.Client
	err     error
}

func newSourceGatewayBridgeFromEnv() *sourceGatewayBridge {
	baseURL := strings.TrimSpace(os.Getenv(sourceGatewayBaseURLEnv))
	token := strings.TrimSpace(os.Getenv(sourceGatewayTokenEnv))
	// Older AppHost builds only inject the device-data bridge endpoint and
	// token. Read those values independently of the dedicated variables: a
	// mixed-version supervisor may provide a new base URL while still exposing
	// only the legacy credential.
	deviceBase := strings.TrimRight(strings.TrimSpace(os.Getenv("ZAPP_DEVICE_DATA_BASE_URL")), "/")
	legacyToken := strings.TrimSpace(os.Getenv("ZAPP_DEVICE_DATA_TOKEN"))
	if baseURL == "" {
		// Older AppHost builds only inject the device-data bridge endpoint.  The
		// source gateway proxy hangs off the same app-runtime scope, so derive
		// the sibling path instead of inventing a second credential channel.
		if strings.HasSuffix(deviceBase, "/device-data/v1") {
			baseURL = strings.TrimSuffix(deviceBase, "/device-data/v1") + sourceGatewayPathSuffix
		}
	}
	if token == "" {
		// Keep the legacy token fallback even when a dedicated base URL is
		// present. During rolling upgrades AppHost can expose the new endpoint
		// before it starts injecting the new token name.
		token = legacyToken
	}
	// Every operation installs its own context deadline. A global client
	// timeout would abort ranged playback at the list-request limit.
	return newSourceGatewayBridge(baseURL, token, &http.Client{})
}

func newSourceGatewayBridge(rawBaseURL, token string, client *http.Client) *sourceGatewayBridge {
	bridge := &sourceGatewayBridge{token: strings.TrimSpace(token), client: client}
	if strings.TrimSpace(rawBaseURL) == "" || bridge.token == "" {
		bridge.err = errSourceGatewayNotConfigured
		return bridge
	}
	baseURL, err := url.Parse(strings.TrimRight(rawBaseURL, "/"))
	if err != nil || baseURL.Scheme != "http" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" || !bridges.IsDeviceDataLoopbackHost(baseURL.Hostname()) || !strings.HasSuffix(baseURL.Path, sourceGatewayPathSuffix) {
		bridge.err = errors.New("source gateway bridge configuration is invalid")
		return bridge
	}
	if client == nil {
		client = &http.Client{}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	bridge.baseURL = baseURL
	bridge.client = &clientCopy
	return bridge
}

// sourceGatewayCatalogSource mirrors the versioned catalog entry returned by
// GET /catalog (see the V1 spec §6.4).  Unknown fields are ignored so the
// server can additively extend the payload.
type sourceGatewayCatalogSource struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Group            string `json:"group"`
	Status           string `json:"status"`
	CachedEntryCount int    `json:"cached_entry_count"`
	WindowPolicy     string `json:"window_policy"`
	LastSuccessAt    string `json:"last_success_at"`
}

type sourceGatewaySubscription struct {
	SourceID string `json:"source_id"`
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"`
}

// sourceGatewaySubscriptionsPayload accepts both subscription response shapes
// used by deployed local proxies. Newer proxies return an object with a
// subscriptions field; older proxies returned the array directly in the
// envelope's data field. Keeping the compatibility decoder at this boundary
// lets the rest of the reconciliation code use one bounded representation.
type sourceGatewaySubscriptionsPayload struct {
	Subscriptions []sourceGatewaySubscription `json:"subscriptions"`
}

func (p *sourceGatewaySubscriptionsPayload) UnmarshalJSON(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("subscriptions payload is empty")
	}
	if trimmed[0] == '[' {
		return json.Unmarshal(trimmed, &p.Subscriptions)
	}
	if trimmed[0] != '{' {
		return errors.New("subscriptions payload must be an object or array")
	}
	type subscriptionsPayloadAlias sourceGatewaySubscriptionsPayload
	var object subscriptionsPayloadAlias
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return err
	}
	p.Subscriptions = object.Subscriptions
	return nil
}

type sourceGatewayEntry struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	ExternalID    string `json:"external_id"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	Author        string `json:"author"`
	PublishedAt   string `json:"published_at"`
	SummaryText   string `json:"summary_text"`
	ContentFormat string `json:"content_format"`
	ThumbnailURL  string `json:"thumbnail_url"`
	MediaURL      string `json:"media_url"`
	MediaMIME     string `json:"media_mime"`
	ContentHash   string `json:"content_hash"`
	FetchedAt     string `json:"fetched_at"`
	SourceLevel   string `json:"source_level"`
}

type sourceGatewayEntriesPage struct {
	Entries    []sourceGatewayEntry
	NextCursor string
	HasMore    bool
}

type sourceGatewayFulltext struct {
	ContentText   string
	ContentHTML   string
	ContentFormat string
	Images        []string
	Degraded      bool
}

// gatewayRuntime is the fixed-size process-local view of gateway health used
// by the status endpoint and reader history cooldown. It never stores source
// IDs or payloads, so repeated catalog changes cannot grow resident memory.
type gatewayRuntime struct {
	mu                   sync.Mutex
	lastAttemptAt        string
	lastSuccessAt        string
	lastError            string
	reconciledAt         time.Time
	reconcileRetryAt     time.Time
	reconciling          bool
	reconcileDone        chan struct{}
	readerHistoryRetryAt time.Time
}

var sourceGatewayState gatewayRuntime

func (g *gatewayRuntime) recordSuccess() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := nowUTC()
	g.lastAttemptAt = now
	g.lastSuccessAt = now
	g.lastError = ""
}

func (g *gatewayRuntime) recordFailure(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastAttemptAt = nowUTC()
	g.lastError = gatewayErrorClass(err)
}

func (g *gatewayRuntime) readerHistoryCoolingDown(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.readerHistoryRetryAt.After(now)
}

func (g *gatewayRuntime) recordReaderHistoryFailure(now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.readerHistoryRetryAt = now.Add(sourceGatewayReaderHistoryCooldown)
}

func (g *gatewayRuntime) clearReaderHistoryCooldown() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.readerHistoryRetryAt = time.Time{}
}

func gatewayErrorClass(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errSourceGatewayNotConfigured):
		return "not_configured"
	case errors.Is(err, errSourceGatewayAuth):
		return "auth"
	case errors.Is(err, errSourceGatewayInvalidCursor):
		return "invalid_cursor"
	case errors.Is(err, errSourceGatewayNotFound):
		return "not_found"
	default:
		return "unavailable"
	}
}

// sourceGatewayNeedsInitialHead distinguishes a never-hydrated source from a
// source whose bounded cloud walk really reached the end.  gateway_has_more is
// intentionally only a continuation bit, so its zero value cannot by itself
// mean "do not fetch": older installs and catalog-only rows may have an empty
// cache with that value.  A successful empty response records last_sync_at and
// an OK/healthy state, which keeps subsequent reader requests local.
func sourceGatewayNeedsInitialHead(source FeedSource) bool {
	if source.CollectionMode != "source_gateway" || source.GatewayHasMore != 0 || strings.TrimSpace(source.GatewayCursor) != "" {
		return false
	}
	gatewayStatus := strings.ToLower(strings.TrimSpace(source.GatewayStatus))
	if gatewayStatus == "auth" || gatewayStatus == "blocked" {
		return false
	}
	// New rows are unavailable/initializing until their first successful head
	// response.  EntryCount is the authoritative local-cache count (rather than
	// records_seen, which can include a prior failed attempt).
	if source.EntryCount == 0 && strings.ToLower(strings.TrimSpace(source.Status)) != "ok" {
		return true
	}
	// records_seen is an attempt counter, not proof that a usable head was
	// persisted.  Older installs can therefore have records_seen > 0 while
	// still having no local entries and no successful sync timestamp.  Treat
	// that state as an initial read as well; otherwise the source can remain a
	// permanent empty projection after a failed first install.
	if source.EntryCount == 0 && strings.TrimSpace(source.LastSyncAt) == "" {
		return true
	}
	return gatewayStatus == "" || gatewayStatus == "initializing"
}

// do issues one bounded request against the local proxy.  There are no
// client-side retries: zettlab-local-server already owns 401/403 mapping and
// bounded 429/5xx retry with Retry-After, and the sync scheduler owns the
// next attempt.  The token never appears in URLs, logs or response bodies.
func (b *sourceGatewayBridge) do(ctx context.Context, method, suffix, query string, maxBytes int64) ([]byte, int, error) {
	if b == nil || b.err != nil || b.baseURL == nil || b.client == nil {
		return nil, 0, errSourceGatewayNotConfigured
	}
	target := *b.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + suffix
	target.RawQuery = query
	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, 0, errSourceGatewayUnavailable
	}
	request.Header.Set(bridges.DeviceDataTokenHeader, b.token)
	request.Header.Set("Accept", "application/json")
	response, err := b.client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errSourceGatewayUnavailable, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("%w: response read failed", errSourceGatewayUnavailable)
	}
	if int64(len(body)) > maxBytes {
		return nil, 0, fmt.Errorf("%w: response too large", errSourceGatewayUnavailable)
	}
	return body, response.StatusCode, nil
}

// getJSON unwraps the standard {"data": ...} envelope and maps the Source
// Gateway error codes (spec §6.5) onto the small local error taxonomy.
func (b *sourceGatewayBridge) getJSON(ctx context.Context, suffix, query string, maxBytes int64, dst any) error {
	body, status, err := b.do(ctx, http.MethodGet, suffix, query, maxBytes)
	if err != nil {
		sourceGatewayState.recordFailure(err)
		return err
	}
	if err := mapSourceGatewayStatus(body, status); err != nil {
		sourceGatewayState.recordFailure(err)
		return err
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || len(envelope.Data) == 0 {
		err := fmt.Errorf("%w: malformed response", errSourceGatewayUnavailable)
		sourceGatewayState.recordFailure(err)
		return err
	}
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		err := fmt.Errorf("%w: malformed data", errSourceGatewayUnavailable)
		sourceGatewayState.recordFailure(err)
		return err
	}
	sourceGatewayState.recordSuccess()
	return nil
}

// sourceGatewayResponseEnvelope is deliberately tolerant at the wire
// boundary. Cloud Source Gateway responses use string codes (and expose the
// semantic SRC_* value in `error`), while the local app-runtime proxy uses
// integer business codes in an HTTP-200 envelope. Keeping Code/Error as
// RawMessage lets one client understand both generations without accepting an
// arbitrary object as a code.
type sourceGatewayResponseEnvelope struct {
	Status string          `json:"status"`
	Code   json.RawMessage `json:"code"`
	Error  json.RawMessage `json:"error"`
	Data   json.RawMessage `json:"data"`
}

func decodeSourceGatewayResponse(body []byte) (sourceGatewayResponseEnvelope, bool) {
	var envelope sourceGatewayResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return sourceGatewayResponseEnvelope{}, false
	}
	return envelope, true
}

// sourceGatewayScalarCode accepts only a JSON string or number. In
// particular, an error object from an unrelated endpoint cannot accidentally
// become a Source Gateway code and change the fallback classification.
func sourceGatewayScalarCode(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

// mapSourceGatewayCode maps both the semantic Source Gateway constants and
// their numeric cloud/local aliases. The semantic value is preferred by
// sourceGatewayResponseError because the cloud `code` field is an opaque
// SRC000x identifier while `error` carries SRC_ENTRY_NOT_FOUND, etc.
func mapSourceGatewayCode(code string) error {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch code {
	case "SRC_FORBIDDEN", "SRC0009", sourceGatewayLocalAuthRequiredCode:
		// The cloud identity is rejected. Retrying without a fresh login only
		// produces the same answer, so surface this as a terminal auth state.
		return errSourceGatewayAuth
	case "SRC_CURSOR_INVALID", "SRC0005":
		return errSourceGatewayInvalidCursor
	case "SRC_ENTRY_NOT_FOUND", "SRC_INVALID_SOURCE", "SRC0002", "SRC0004":
		return errSourceGatewayNotFound
	case "SRC_NOT_READY", "SRC_SOURCE_BLOCKED", "SRC_RATE_LIMITED", "SRC_UPSTREAM_UNAVAILABLE", "SRC_MEDIA_UNAVAILABLE", "SRC_IDEMPOTENCY_CONFLICT", "SRC0001", "SRC0003", "SRC0006", "SRC0007", "SRC0008", "SRC0010", sourceGatewayLocalInvalidParamsCode, sourceGatewayLocalUpstreamUnavailableCode, sourceGatewayLocalMediaUnavailableCode:
		return errSourceGatewayUnavailable
	default:
		return nil
	}
}

// sourceGatewayBusinessCodeIsNonZero reports a local app-runtime failure that
// uses a future numeric code not yet known by this package. The local proxy
// deliberately keeps HTTP 200 for business failures; accepting an unknown
// non-zero code would turn a failed catalog into a false empty success. A
// zero code is the only success code in the local and cloud envelopes.
func sourceGatewayBusinessCodeIsNonZero(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" || code == "0" {
		return false
	}
	_, err := strconv.ParseInt(code, 10, 64)
	return err == nil
}

// sourceGatewayBridgeUnavailable404 identifies the generic 404 emitted by
// AppHost when the app capability, scoped token, credential or region is not
// available. It must not be confused with the Source Gateway's typed 404
// (SRC_INVALID_SOURCE/SRC_ENTRY_NOT_FOUND), which is a real not-found result.
func sourceGatewayBridgeUnavailable404(body []byte) bool {
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		for _, value := range []string{payload.Error, payload.Message} {
			normalized := strings.ToLower(strings.TrimSpace(value))
			if normalized == "source gateway is unavailable" || normalized == "source gateway unavailable" {
				return true
			}
		}
	}
	// Keep compatibility with an older proxy that returned the plain phrase
	// instead of JSON. Do not classify arbitrary HTML/text 404 pages this way.
	normalized := strings.ToLower(strings.TrimSpace(string(body)))
	return normalized == "source gateway is unavailable" || normalized == "source gateway unavailable"
}

// sourceGatewayResponseError inspects an envelope even when the HTTP status
// is 2xx. local-server intentionally returns HTTP 200 for business failures,
// so checking status first would decode `{data:{}}` as a successful empty
// catalog/entries page and permanently hide the outage behind a false zero.
func sourceGatewayResponseError(body []byte) (error, bool) {
	envelope, ok := decodeSourceGatewayResponse(body)
	if !ok {
		return nil, false
	}
	// Cloud uses `error` for the semantic constant; inspect it before the
	// numeric/opaque `code` field. This also handles local envelopes where code
	// is an integer and error is absent.
	for _, raw := range []json.RawMessage{envelope.Error, envelope.Code} {
		code := sourceGatewayScalarCode(raw)
		if mapped := mapSourceGatewayCode(code); mapped != nil {
			return mapped, true
		}
		if sourceGatewayBusinessCodeIsNonZero(code) {
			return errSourceGatewayUnavailable, true
		}
	}
	status := strings.ToLower(strings.TrimSpace(envelope.Status))
	if status == "error" || status == "failed" {
		return errSourceGatewayUnavailable, true
	}
	return nil, false
}

func sourceGatewayResponseHasCode(body []byte, wanted ...string) bool {
	envelope, ok := decodeSourceGatewayResponse(body)
	if !ok {
		return false
	}
	want := make(map[string]struct{}, len(wanted))
	for _, code := range wanted {
		want[strings.ToUpper(strings.TrimSpace(code))] = struct{}{}
	}
	for _, raw := range []json.RawMessage{envelope.Error, envelope.Code} {
		if _, ok := want[strings.ToUpper(sourceGatewayScalarCode(raw))]; ok {
			return true
		}
	}
	return false
}

func mapSourceGatewayStatus(body []byte, status int) error {
	// Business envelopes are authoritative, including HTTP-200 local proxy
	// failures. A recognized error must never be mistaken for an empty success.
	if mapped, recognized := sourceGatewayResponseError(body); recognized {
		return mapped
	}
	if status >= 200 && status < 300 {
		return nil
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return errSourceGatewayAuth
	}
	if status == http.StatusNotFound {
		// AppHost deliberately returns a generic 404 for an unavailable/missing
		// app capability. Only a typed Source Gateway not-found code means that
		// the requested source/entry is genuinely absent; treating every 404 as
		// not-found would mark all cached sources blocked when an old proxy or
		// wrong app scope is being used. A bare 404 remains the cloud contract's
		// entry/source-not-found response; only the explicit AppHost phrase above
		// is treated as an unavailable capability.
		if sourceGatewayBridgeUnavailable404(body) {
			return errSourceGatewayUnavailable
		}
		return errSourceGatewayNotFound
	}
	return errSourceGatewayUnavailable
}

func (b *sourceGatewayBridge) catalog(ctx context.Context) ([]sourceGatewayCatalogSource, string, error) {
	var data struct {
		CatalogVersion string                       `json:"catalog_version"`
		Sources        []sourceGatewayCatalogSource `json:"sources"`
	}
	if err := b.getJSON(ctx, "/catalog", "", sourceGatewayMaxPageBytes, &data); err != nil {
		return nil, "", err
	}
	return data.Sources, data.CatalogVersion, nil
}

func (b *sourceGatewayBridge) subscriptions(ctx context.Context) ([]sourceGatewaySubscription, error) {
	var data sourceGatewaySubscriptionsPayload
	if err := b.getJSON(ctx, "/subscriptions", "", sourceGatewayMaxPageBytes, &data); err != nil {
		return nil, err
	}
	return data.Subscriptions, nil
}

func (b *sourceGatewayBridge) entries(ctx context.Context, sourceID, cursor string, limit int) (sourceGatewayEntriesPage, error) {
	if limit <= 0 || limit > sourceGatewayPageLimit {
		limit = sourceGatewayPageLimit
	}
	query := url.Values{}
	query.Set("limit", fmt.Sprintf("%d", limit))
	if strings.TrimSpace(sourceID) != "" {
		query.Set("source_id", sourceID)
	}
	if strings.TrimSpace(cursor) != "" {
		query.Set("cursor", cursor)
	}
	var data struct {
		Entries    []sourceGatewayEntry `json:"entries"`
		NextCursor string               `json:"next_cursor"`
		HasMore    bool                 `json:"has_more"`
	}
	if err := b.getJSON(ctx, "/entries", query.Encode(), sourceGatewayMaxPageBytes, &data); err != nil {
		return sourceGatewayEntriesPage{}, err
	}
	return sourceGatewayEntriesPage{Entries: data.Entries, NextCursor: data.NextCursor, HasMore: data.HasMore}, nil
}

func validGatewayEntryID(id string) bool {
	if !validText(id, 128, true) || strings.ContainsAny(id, "/\\\x00\r\n?#") {
		return false
	}
	return url.PathEscape(id) == id
}

// isGatewayManagedEntry also fences legacy rows that predate
// news.gateway_entry_id. Their source ownership is authoritative: until a
// later list sync maps the row to an opaque gateway id, the app must keep its
// local cache and fail closed instead of reading the recorded public URL.
func isGatewayManagedEntry(gatewayEntryID, collectionMode string) bool {
	return strings.TrimSpace(gatewayEntryID) != "" || strings.TrimSpace(collectionMode) == "source_gateway"
}

// fulltext fetches one article body on demand (spec §8.4: the list sync never
// fetches bodies).  The server side is idempotent and bounded; the response is
// parsed tolerantly so additive fields from newer gateways are ignored.
func (b *sourceGatewayBridge) fulltext(ctx context.Context, gatewayEntryID string) (sourceGatewayFulltext, error) {
	if !validGatewayEntryID(gatewayEntryID) {
		return sourceGatewayFulltext{}, errSourceGatewayNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, sourceGatewayFulltextTimeout)
	defer cancel()
	body, status, err := b.do(ctx, http.MethodPost, "/entries/"+url.PathEscape(gatewayEntryID)+"/fulltext", "", sourceGatewayMaxFulltextBytes)
	if err != nil {
		sourceGatewayState.recordFailure(err)
		return sourceGatewayFulltext{}, err
	}
	if err := mapSourceGatewayStatus(body, status); err != nil {
		sourceGatewayState.recordFailure(err)
		return sourceGatewayFulltext{}, err
	}
	var envelope struct {
		Data struct {
			ContentText   string          `json:"content_text"`
			ContentHTML   string          `json:"content_html"`
			ContentFormat string          `json:"content_format"`
			Degraded      bool            `json:"degraded"`
			Images        json.RawMessage `json:"images"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		err := fmt.Errorf("%w: malformed fulltext", errSourceGatewayUnavailable)
		sourceGatewayState.recordFailure(err)
		return sourceGatewayFulltext{}, err
	}
	result := sourceGatewayFulltext{
		ContentText:   limitText(envelope.Data.ContentText, maxEntryFullTextRunes*4),
		ContentHTML:   envelope.Data.ContentHTML,
		ContentFormat: envelope.Data.ContentFormat,
		Degraded:      envelope.Data.Degraded,
	}
	if len(envelope.Data.ContentHTML) > maxEntryFullHTMLBytes {
		result.ContentHTML = envelope.Data.ContentHTML[:maxEntryFullHTMLBytes]
	}
	result.Images = parseGatewayImages(envelope.Data.Images)
	sourceGatewayState.recordSuccess()
	return result, nil
}

// parseGatewayImages accepts both a plain URL list and an object list with
// url/mime fields; the array order defines the media index space used by
// GET /entries/:id/media/:index.
func parseGatewayImages(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var plain []string
	if json.Unmarshal(raw, &plain) == nil {
		return boundedGatewayImages(plain)
	}
	var objects []struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &objects) != nil {
		return nil
	}
	urls := make([]string, 0, len(objects))
	for _, object := range objects {
		urls = append(urls, object.URL)
	}
	return boundedGatewayImages(urls)
}

func boundedGatewayImages(urls []string) []string {
	result := make([]string, 0, minInt(len(urls), maxEntryArticleImages))
	for _, raw := range urls {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		result = append(result, value)
		if len(result) >= maxEntryArticleImages {
			break
		}
	}
	return result
}

// gatewayMediaResponse carries one fixed-index attachment from the
// local-server media route. The caller owns closing Body.
type gatewayMediaResponse struct {
	Body         io.ReadCloser
	ContentType  string
	Length       int64
	StatusCode   int
	ContentRange string
	AcceptRanges string
	ETag         string
	LastModified string
}

type gatewayMediaBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *gatewayMediaBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// media fetches one entry image by its recorded index.  No URL is ever taken
// from the caller or from article HTML: the index space comes from the gateway
// record itself, so the route cannot be turned into an open proxy.
func (b *sourceGatewayBridge) media(ctx context.Context, gatewayEntryID string, index int, method string) (gatewayMediaResponse, error) {
	return b.mediaWithRange(ctx, gatewayEntryID, index, method, "thumbnail", "")
}

// mediaWithRange fetches an image or playable attachment by its recorded
// index. No caller-provided URL is accepted, and only one validated byte range
// can cross the app-runtime boundary.
func (b *sourceGatewayBridge) mediaWithRange(ctx context.Context, gatewayEntryID string, index int, method, kind, rangeHeader string) (gatewayMediaResponse, error) {
	if !validGatewayEntryID(gatewayEntryID) || index < 0 || index >= maxEntryArticleImages {
		return gatewayMediaResponse{}, errSourceGatewayNotFound
	}
	if method != http.MethodGet && method != http.MethodHead {
		return gatewayMediaResponse{}, errors.New("unsupported media method")
	}
	if kind != "thumbnail" && kind != "media" {
		return gatewayMediaResponse{}, errors.New("unsupported media kind")
	}
	rangeHeader = strings.TrimSpace(rangeHeader)
	if rangeHeader != "" && !validMediaRangeHeader(rangeHeader) {
		return gatewayMediaResponse{}, errors.New("unsupported media range")
	}
	ctx, cancel := context.WithTimeout(ctx, sourceGatewayMediaTimeout)
	if b == nil || b.err != nil || b.baseURL == nil || b.client == nil {
		cancel()
		return gatewayMediaResponse{}, errSourceGatewayNotConfigured
	}
	target := *b.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/entries/" + url.PathEscape(gatewayEntryID) + "/media/" + strconv.Itoa(index)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		cancel()
		return gatewayMediaResponse{}, errSourceGatewayUnavailable
	}
	request.Header.Set(bridges.DeviceDataTokenHeader, b.token)
	if kind == "thumbnail" {
		request.Header.Set("Accept", "image/*")
	} else {
		request.Header.Set("Accept", "audio/*, video/*")
	}
	if rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	response, err := b.client.Do(request)
	if err != nil {
		cancel()
		sourceGatewayState.recordFailure(fmt.Errorf("%w: %v", errSourceGatewayUnavailable, err))
		return gatewayMediaResponse{}, errSourceGatewayUnavailable
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		defer response.Body.Close()
		defer cancel()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		err := mapSourceGatewayStatus(body, response.StatusCode)
		// A typed 404 for a media index means the optional image is absent. Keep
		// the existing not-found contract so list thumbnails can clear a stale
		// projection; 93004/5xx remains an unavailable/degraded result.
		if response.StatusCode == http.StatusNotFound && sourceGatewayResponseHasCode(body, "SRC_MEDIA_UNAVAILABLE", "SRC0008") {
			err = errSourceGatewayNotFound
		}
		sourceGatewayState.recordFailure(err)
		return gatewayMediaResponse{}, err
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	// The app-runtime proxy keeps local business failures in an HTTP-200 JSON
	// envelope (9300x). Do not interpret that envelope as a media stream just
	// because the transport status is 200; otherwise the caller sees a MIME
	// error and the source cache is silently treated as healthy.
	if strings.HasSuffix(contentType, "+json") || contentType == "application/json" {
		defer response.Body.Close()
		defer cancel()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		if mapped, recognized := sourceGatewayResponseError(body); recognized {
			sourceGatewayState.recordFailure(mapped)
			return gatewayMediaResponse{}, mapped
		}
		err := fmt.Errorf("%w: media endpoint returned JSON", errSourceGatewayUnavailable)
		sourceGatewayState.recordFailure(err)
		return gatewayMediaResponse{}, err
	}
	if !allowedMediaContentType(contentType, kind) {
		_ = response.Body.Close()
		cancel()
		return gatewayMediaResponse{}, errors.New("云端媒体类型不受支持")
	}
	maxBytes := int64(sourceGatewayMaxMediaBytes)
	if kind == "media" {
		maxBytes = int64(sourceGatewayMaxPlayableMediaBytes)
	}
	if response.ContentLength > maxBytes {
		_ = response.Body.Close()
		cancel()
		return gatewayMediaResponse{}, errors.New("云端媒体超过大小上限")
	}
	sourceGatewayState.recordSuccess()
	return gatewayMediaResponse{
		Body:         &gatewayMediaBody{ReadCloser: response.Body, cancel: cancel},
		ContentType:  contentType,
		Length:       response.ContentLength,
		StatusCode:   response.StatusCode,
		ContentRange: strings.TrimSpace(response.Header.Get("Content-Range")),
		AcceptRanges: strings.TrimSpace(response.Header.Get("Accept-Ranges")),
		ETag:         strings.TrimSpace(response.Header.Get("ETag")),
		LastModified: strings.TrimSpace(response.Header.Get("Last-Modified")),
	}, nil
}

// gatewayNewsItem converts one gateway entry into the local reader row shape.
// The entry keeps its original article URL for reference and citation, but
// images and full text are only ever read back through the local proxy.
func gatewayNewsItem(source FeedSource, entry sourceGatewayEntry) NewsItem {
	externalID := strings.TrimSpace(entry.ExternalID)
	if externalID == "" {
		externalID = strings.TrimSpace(entry.URL)
	}
	item := NewsItem{
		ID:              gatewayNewsID(source.ID, externalID),
		ExternalID:      externalID,
		Title:           limitText(entry.Title, 240),
		URL:             strings.TrimSpace(entry.URL),
		Source:          firstNonEmpty(source.Name, entry.Author),
		SourceID:        source.ID,
		Origin:          gatewayOrigin,
		SourceType:      firstNonEmpty(entry.ContentFormat, source.Kind, "text"),
		Author:          limitText(entry.Author, 120),
		PublishedAt:     normalizePublishedAt(entry.PublishedAt),
		SummaryText:     limitText(entry.SummaryText, 2000),
		ContentFormat:   "text",
		ThumbnailURL:    strings.TrimSpace(entry.ThumbnailURL),
		MediaURL:        strings.TrimSpace(entry.MediaURL),
		MediaMIME:       strings.TrimSpace(entry.MediaMIME),
		ContentHash:     strings.TrimSpace(entry.ContentHash),
		FetchedAt:       firstNonEmpty(entry.FetchedAt, nowUTC()),
		PermissionScope: "public_read",
		SourceLevel:     firstNonEmpty(entry.SourceLevel, "official"),
	}
	if item.PublishedAt == "" {
		item.PublishedAt = nowUTC()
	}
	if item.ViewType == "" && looksLikeVideoEntry(item) {
		item.ViewType = "video"
	}
	return item
}

// gatewayNewsID preserves the existing ID shape for lowercase feed identities
// while keeping opaque external IDs case-sensitive. YouTube/Bilibili IDs and
// URL paths may differ only by case and must remain distinct articles.
func gatewayNewsID(sourceID, externalID string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(sourceID + "|" + externalID)))
	return "entry-" + hex.EncodeToString(digest[:8])
}

func looksLikeVideoEntry(item NewsItem) bool {
	lower := strings.ToLower(item.URL + " " + item.MediaMIME)
	return strings.Contains(lower, "youtube.com/") || strings.Contains(lower, "youtu.be/") ||
		strings.Contains(lower, "bilibili.com/video") || strings.Contains(lower, "b23.tv/") ||
		strings.HasPrefix(strings.ToLower(item.MediaMIME), "video/")
}

// upsertGatewayNewsTx merges one gateway entry with the existing reader data.
// Identity is (source_id, external_id); a row collected by the retired direct
// fetch path is additionally matched by (source_id, url) so upgrading users do
// not see duplicates and keep their read/star/context state on the same row.
func upsertGatewayNewsTx(tx *sql.Tx, source FeedSource, entry sourceGatewayEntry, now string) (bool, error) {
	if !validGatewayEntryID(strings.TrimSpace(entry.ID)) {
		return false, errors.New("云端条目缺少有效文章标识")
	}
	item := gatewayNewsItem(source, entry)
	if item.ExternalID == "" {
		return false, errors.New("云端条目缺少稳定标识")
	}
	var matchedExternalID string
	err := tx.QueryRow(`SELECT external_id FROM news WHERE source_id=? AND external_id=?`, item.SourceID, item.ExternalID).Scan(&matchedExternalID)
	if errors.Is(err, sql.ErrNoRows) && item.URL != "" {
		// The previous direct collector may have stored the same article under a
		// different external id (RSS guid vs. gateway external id).  Merge by
		// canonical URL and keep the row's existing identity so preferences,
		// knowledge-base memberships and reading progress stay attached.
		err = tx.QueryRow(`SELECT external_id FROM news WHERE source_id=? AND url=? ORDER BY published_at DESC, id LIMIT 1`, item.SourceID, item.URL).Scan(&matchedExternalID)
		if err == nil && matchedExternalID != "" {
			item.ExternalID = matchedExternalID
			item.ID = stableObjectID("entry", item.SourceID+"|"+matchedExternalID)
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	var previousHash, previousSummary, previousThumbnail string
	if matchedExternalID != "" {
		_ = tx.QueryRow(`SELECT content_hash,summary_text,thumbnail_url FROM news WHERE source_id=? AND external_id=?`, item.SourceID, item.ExternalID).Scan(&previousHash, &previousSummary, &previousThumbnail)
		if (strings.TrimSpace(item.SummaryText) == "" || containsBlockedArticleMarker(item.SummaryText)) &&
			strings.TrimSpace(previousSummary) != "" && !containsBlockedArticleMarker(previousSummary) {
			item.SummaryText = previousSummary
			if previousThumbnail != "" {
				item.ThumbnailURL = previousThumbnail
			}
		}
	}
	if err := upsertFetchedNewsTx(tx, item, now); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE news SET gateway_entry_id=? WHERE source_id=? AND external_id=?`, entry.ID, item.SourceID, item.ExternalID); err != nil {
		return false, err
	}
	return previousHash != item.ContentHash, nil
}

// markGatewaySourceOutcome records a gateway sync outcome without ever
// deleting local content: the reader keeps serving the last successful
// snapshot while the source reports the degraded state.  gatewayStatus is the
// cloud-side state projection (unavailable/auth/blocked), not the fetch
// display status.
func markGatewaySourceOutcome(sourceID, gatewayStatus, message string) error {
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	now := nowUTC()
	next := time.Now().UTC().Add(feedSyncRetryBase).Format(time.RFC3339)
	status := "unavailable"
	var entries int
	if countErr := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, sourceID).Scan(&entries); countErr == nil && entries > 0 {
		// With a usable local snapshot the source stays in a last-known-good
		// display state; only the error text explains that data may be stale.
		status = "partial"
	}
	_, err := db.Exec(`UPDATE feed_sources SET status=?, gateway_status=?, last_error=?, last_sync_at=?, next_sync_at=?, updated_at=? WHERE id=?`, status, gatewayStatus, limitText(message, 500), now, next, now, sourceID)
	if err != nil {
		return err
	}
	_, _ = db.Exec(`UPDATE sync_requests SET status='retry_wait', completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at=? WHERE source_id=? AND status IN ('pending','retry_wait')`, now, limitText(message, 500), next, sourceID)
	return errors.New(message)
}

// syncGatewaySourceWithinBudget is the source_gateway branch of the regular
// feed sync worker.  It reuses the same queue, per-source slots, timeout and
// sync_requests bookkeeping as every other collection mode; only the fetch
// itself moves from direct public HTTP to the bounded local proxy.
func syncGatewaySourceWithinBudget(parent, ctx context.Context, source FeedSource) error {
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return markGatewaySourceOutcome(source.ID, "unavailable", gatewayUnavailableNotice)
	}
	// Catalog/subscription reconciliation is best effort.  Bound it separately
	// so a cold or unavailable metadata endpoint cannot consume the entire
	// per-source fetch budget and prevent the requested source head from being
	// hydrated.
	reconcileCtx, cancelReconcile := context.WithTimeout(ctx, sourceGatewayReconcileTimeout)
	if err := maybeReconcileSourceGateway(reconcileCtx, bridge, false); err != nil && !errors.Is(err, errSourceGatewayNotConfigured) {
		log.Printf("来源网关目录协调失败: %v", err)
	}
	cancelReconcile()
	return syncGatewaySourceHead(ctx, bridge, source)
}

// reconcileSourceGatewayWithinBudget performs one metadata-only reconciliation
// with the same short deadline used by a source fetch.  Keeping bridge
// discovery here makes the startup and periodic paths share the exact
// compatibility behavior for old AppHost-injected environment variables.
func reconcileSourceGatewayWithinBudget(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return bridge.err
	}
	ctx, cancel := context.WithTimeout(parent, sourceGatewayReconcileTimeout)
	defer cancel()
	return maybeReconcileSourceGateway(ctx, bridge, false)
}

// ensureSourceGatewaySubscriptionProjection repairs the projection needed by
// a source-scoped reader request.  A cold install can be missing either the
// feed_sources row itself or its subscription row.  Both states must be
// distinguished from an explicit disabled subscription, and a failed
// reconciliation must be returned to the HTTP caller instead of silently
// turning into a successful empty directory response. The ordinary directory
// request remains a cheap local read; only a source-scoped lookup waits for the
// short gateway deadline.
func ensureSourceGatewaySubscriptionProjection(parent context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db == nil {
		return nil
	}
	var collectionMode string
	err := db.QueryRow(`SELECT collection_mode FROM feed_sources WHERE id=?`, sourceID).Scan(&collectionMode)
	sourceMissing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !sourceMissing {
		return fmt.Errorf("%w: read source projection: %v", errSourceGatewayUnavailable, err)
	}
	if !sourceMissing && strings.TrimSpace(collectionMode) != "source_gateway" {
		return nil
	}
	needsSubscription := false
	if !sourceMissing {
		var hasSubscription int
		if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM feed_subscriptions WHERE source_id=?)`, sourceID).Scan(&hasSubscription); err != nil {
			return fmt.Errorf("%w: read source subscription projection: %v", errSourceGatewayUnavailable, err)
		}
		// An existing row, including enabled=0, is an intentional local choice.
		// Only a completely absent subscription row needs server reconciliation.
		needsSubscription = hasSubscription == 0
	}
	if !sourceMissing && !needsSubscription {
		return nil
	}
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return fmt.Errorf("%w: source projection cannot be repaired: %v", errSourceGatewayUnavailable, bridge.err)
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, sourceGatewayReconcileTimeout)
	defer cancel()
	// Startup and the minute scheduler use the same singleflight guard.  If
	// either one is already reconciling, wait for that exact pass before reading
	// the source row; otherwise a click in the tiny cold-start window could
	// still observe subscribed=false and skip its first sync forever.
	waitForSourceGatewayReconcile(ctx)
	// A missing source row is outside the normal ten-minute reconciliation
	// cooldown: force one metadata pass so a newly returned server subscription
	// can materialize the row during this request.
	err = maybeReconcileSourceGateway(ctx, bridge, sourceMissing)
	// If another caller won the guard between the first wait and maybe's lock,
	// wait once more for that pass before the handler performs its local SELECT.
	if err == nil {
		waitForSourceGatewayReconcile(ctx)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%w: source projection reconciliation deadline: %v", errSourceGatewayUnavailable, ctx.Err())
	}
	if err != nil {
		if errors.Is(err, errSourceGatewayNotConfigured) {
			return fmt.Errorf("%w: source projection gateway is not configured", errSourceGatewayUnavailable)
		}
		return err
	}
	var afterMode string
	err = db.QueryRow(`SELECT collection_mode FROM feed_sources WHERE id=?`, sourceID).Scan(&afterMode)
	if errors.Is(err, sql.ErrNoRows) {
		// A successful, complete reconciliation that did not return this source
		// means the client supplied a stale/unknown source ID.  Do not emit a
		// misleading 200 with an empty sources array.
		return errSourceGatewayNotFound
	}
	if err != nil {
		return fmt.Errorf("%w: verify source projection: %v", errSourceGatewayUnavailable, err)
	}
	if strings.TrimSpace(afterMode) != "source_gateway" {
		return nil
	}
	var hasSubscription int
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM feed_subscriptions WHERE source_id=?)`, sourceID).Scan(&hasSubscription); err != nil {
		return fmt.Errorf("%w: verify source subscription projection: %v", errSourceGatewayUnavailable, err)
	}
	if hasSubscription == 0 {
		// The metadata pass completed but did not establish a subscription. This
		// can be a valid server-side unsubscribe, but it is not safe to claim the
		// source is ready for a first article read. Return a retryable response so
		// the client never caches a false empty source during a partial projection.
		return fmt.Errorf("%w: source subscription projection is incomplete", errSourceGatewayUnavailable)
	}
	return nil
}

func waitForSourceGatewayReconcile(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	sourceGatewayState.mu.Lock()
	done := sourceGatewayState.reconcileDone
	inFlight := sourceGatewayState.reconciling
	sourceGatewayState.mu.Unlock()
	if !inFlight || done == nil {
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// runSourceGatewayStartupReconcile repairs the local metadata projection after
// a fresh install (or an upgrade from a version that only persisted
// feed_sources).  It is intentionally asynchronous from HTTP startup and
// never hydrates article bodies.  Transient gateway/local-server startup races
// get a small, bounded retry window; explicit local unsubscribe state is
// preserved by reconcileSourceGateway's INSERT ... ON CONFLICT DO NOTHING.
func runSourceGatewayStartupReconcile(parent context.Context) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, sourceGatewayStartupReconcileBudget)
	defer cancel()
	for attempt := 0; attempt < sourceGatewayStartupReconcileAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		err := reconcileSourceGatewayWithinBudget(ctx)
		if err == nil || errors.Is(err, errSourceGatewayNotConfigured) {
			return
		}
		if attempt+1 == sourceGatewayStartupReconcileAttempts {
			log.Printf("来源网关首装目录协调失败，稍后由定时/按需同步重试: %v", err)
			return
		}
		timer := time.NewTimer(sourceGatewayReconcileRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				// The timer may have fired concurrently with cancellation.  Drain
				// without blocking: shutdown must not wait on a value already
				// consumed by the select.
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
		}
	}
}

// syncGatewaySourceHead walks the newest gateway pages and stops as soon as a
// full page produces no content changes, so steady-state syncs cost exactly
// one page.  Warm installs page deeper (bounded) until they reach known rows.
func syncGatewaySourceHead(ctx context.Context, bridge *sourceGatewayBridge, source FeedSource) error {
	cursor := ""
	pages := 0
	seen := 0
	changed := 0
	hasMore := false
	for pages < sourceGatewayMaxPagesRun {
		page, err := bridge.entries(ctx, source.ID, cursor, sourceGatewayPageLimit)
		if err != nil {
			return markGatewaySyncError(source, err)
		}
		pages++
		seen += len(page.Entries)
		pageChanged, err := upsertGatewayEntriesPage(source, page.Entries)
		if err != nil {
			return err
		}
		changed += pageChanged
		// Keep the continuation token from the last page we actually read. A
		// warm-install run is intentionally capped; if it reaches that cap, the
		// reader-driven history pull must resume from this cursor instead of
		// starting at the head and re-reading the same pages forever.
		cursor = strings.TrimSpace(page.NextCursor)
		hasMore = page.HasMore && cursor != ""
		if !hasMore || (pageChanged == 0 && len(page.Entries) > 0) {
			break
		}
	}
	if !hasMore {
		cursor = ""
	}
	now := nowUTC()
	next := time.Now().UTC().Add(time.Duration(source.SyncIntervalSecs) * time.Second).Format(time.RFC3339)
	feedSyncWriteMu.Lock()
	_, err := db.Exec(`UPDATE feed_sources SET status='ok', gateway_status=CASE WHEN gateway_status IN ('','initializing','unavailable') THEN 'healthy' ELSE gateway_status END, records_seen=records_seen+?, records_used=records_used+?, last_sync_at=?, last_error='', next_sync_at=?, gateway_cursor=?, gateway_has_more=?, updated_at=? WHERE id=?`,
		seen, changed, now, next, cursor, boolInt(hasMore), now, source.ID)
	if err == nil {
		_, err = db.Exec(`UPDATE sync_requests SET status='ok', completed_at=?, message=?, attempt_count=attempt_count+1, next_retry_at='' WHERE source_id=? AND status IN ('pending','retry_wait')`, now, messageForGatewaySync(changed, pages), source.ID)
	}
	feedSyncWriteMu.Unlock()
	if err != nil {
		return err
	}
	pruneSourceNews(source.ID)
	return nil
}

func messageForGatewaySync(changed, pages int) string {
	if changed > 0 {
		return fmt.Sprintf("已从云端来源更新 %d 条", changed)
	}
	if pages > 0 {
		return "内容未变化"
	}
	return "已同步"
}

func markGatewaySyncError(source FeedSource, err error) error {
	if errors.Is(err, errSourceGatewayAuth) {
		return markGatewaySourceOutcome(source.ID, "auth", gatewayAuthNotice)
	}
	if errors.Is(err, errSourceGatewayNotFound) {
		return markGatewaySourceOutcome(source.ID, "blocked", "云端目录暂未提供该来源，已保留本地内容")
	}
	return markGatewaySourceOutcome(source.ID, "unavailable", gatewayUnavailableNotice)
}

func upsertGatewayEntriesPage(source FeedSource, entries []sourceGatewayEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	now := nowUTC()
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	changed := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.SourceID) != "" && entry.SourceID != source.ID {
			// The proxy is expected to scope entries to the requested source;
			// never let a malformed response write into another source's feed.
			continue
		}
		updated, err := upsertGatewayNewsTx(tx, source, entry, now)
		if err != nil {
			return changed, err
		}
		if updated {
			changed++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// pullGatewayHistoryPage continues the per-source walk into older cloud pages.
// The reader triggers it when local pages are exhausted but the source reports
// more cloud history; each call fetches one bounded page and persists the
// cursor so the next scroll continues exactly where this one stopped.
func pullGatewayHistoryPage(ctx context.Context, sourceID string) (int, error) {
	source, err := feedSourceByID(sourceID)
	if err != nil {
		return 0, err
	}
	initialHead := sourceGatewayNeedsInitialHead(source)
	if source.CollectionMode != "source_gateway" || (source.GatewayHasMore == 0 && !initialHead) {
		return 0, nil
	}
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return 0, bridge.err
	}
	cursor := source.GatewayCursor
	page, err := bridge.entries(ctx, sourceID, cursor, sourceGatewayPageLimit)
	if errors.Is(err, errSourceGatewayInvalidCursor) && cursor != "" {
		// Cursors are HMAC-signed and may expire.  Restart the walk at the head;
		// unchanged pages are cheap no-ops and the walk reconverges.
		if _, resetErr := db.Exec(`UPDATE feed_sources SET gateway_cursor='', updated_at=? WHERE id=?`, nowUTC(), sourceID); resetErr != nil {
			return 0, resetErr
		}
		page, err = bridge.entries(ctx, sourceID, "", sourceGatewayPageLimit)
	}
	if err != nil {
		return 0, err
	}
	inserted, err := upsertGatewayEntriesPage(source, page.Entries)
	if err != nil {
		return 0, err
	}
	nextCursor, hasMore := page.NextCursor, page.HasMore
	if !hasMore {
		nextCursor = ""
	}
	feedSyncWriteMu.Lock()
	now := nowUTC()
	if initialHead {
		nextSync := time.Now().UTC().Add(time.Duration(source.SyncIntervalSecs) * time.Second).Format(time.RFC3339)
		_, err = db.Exec(`UPDATE feed_sources
SET gateway_cursor=?, gateway_has_more=?, status='ok',
    gateway_status=CASE WHEN gateway_status IN ('','initializing','unavailable') THEN 'healthy' ELSE gateway_status END,
    records_seen=records_seen+?, records_used=records_used+?,
    last_sync_at=?, last_error='', next_sync_at=?, updated_at=?
WHERE id=?`, nextCursor, boolInt(hasMore), len(page.Entries), inserted, now, nextSync, now, sourceID)
	} else {
		_, err = db.Exec(`UPDATE feed_sources SET gateway_cursor=?, gateway_has_more=?, updated_at=? WHERE id=?`, nextCursor, boolInt(hasMore), now, sourceID)
	}
	feedSyncWriteMu.Unlock()
	if err != nil {
		return 0, err
	}
	if inserted > 0 {
		pruneSourceNews(sourceID)
	}
	return len(page.Entries), nil
}

// maybeReconcileSourceGateway refreshes the local projection of the cloud
// catalog and the user's gateway subscriptions.  It runs at most once per
// interval per process and never blocks the reader on failure.
func maybeReconcileSourceGateway(ctx context.Context, bridge *sourceGatewayBridge, force bool) error {
	now := time.Now()
	sourceGatewayState.mu.Lock()
	if sourceGatewayState.reconciling {
		sourceGatewayState.mu.Unlock()
		return nil
	}
	if !force && ((!sourceGatewayState.reconciledAt.IsZero() && now.Sub(sourceGatewayState.reconciledAt) < sourceGatewayReconcileInterval) || sourceGatewayState.reconcileRetryAt.After(now)) {
		sourceGatewayState.mu.Unlock()
		return nil
	}
	sourceGatewayState.reconciling = true
	done := make(chan struct{})
	sourceGatewayState.reconcileDone = done
	sourceGatewayState.mu.Unlock()
	err := reconcileSourceGateway(ctx, bridge)
	sourceGatewayState.mu.Lock()
	sourceGatewayState.reconciling = false
	if sourceGatewayState.reconcileDone == done {
		sourceGatewayState.reconcileDone = nil
	}
	if err == nil {
		sourceGatewayState.reconciledAt = time.Now()
		sourceGatewayState.reconcileRetryAt = time.Time{}
	} else {
		// Do not turn a transient catalog outage into a ten-minute stale
		// projection.  Retry soon, while still preventing a request storm.
		sourceGatewayState.reconcileRetryAt = time.Now().Add(sourceGatewayReconcileRetryInterval)
	}
	sourceGatewayState.mu.Unlock()
	close(done)
	return err
}

func reconcileSourceGateway(ctx context.Context, bridge *sourceGatewayBridge) error {
	// Upgrade and local-toggle subscription changes are durable. Drain them
	// before reading the server projection so optional maintained sources become
	// readable in the same reconcile pass. Individual HTTP failures are persisted
	// with backoff and do not block healthy catalog/cache reconciliation.
	if err := drainSourceGatewaySubscriptionOutbox(ctx, bridge, sourceGatewaySubscriptionBatch); err != nil &&
		!errors.Is(err, errSourceGatewayNotConfigured) {
		return err
	}
	// The server lazily materializes a user's core subscription projection when
	// this endpoint is first read. Do this before catalog validation: a cold
	// install can legitimately return an empty catalog while that projection is
	// being initialized, and skipping this request leaves the first scoped
	// entries read forbidden on the next attempt. The catalog is still the
	// authoritative directory; an empty response below remains retryable rather
	// than being treated as a successful empty directory.
	subscriptions, err := bridge.subscriptions(ctx)
	if err != nil {
		return err
	}
	sources, _, err := bridge.catalog(ctx)
	if err != nil {
		return err
	}
	// An authenticated but empty catalog is not a valid projection of the
	// maintained source directory. Treat it as a transient upstream failure;
	// otherwise the old mass-block query below would hide every local source
	// after one truncated/empty response.
	if len(sources) == 0 {
		err := fmt.Errorf("%w: catalog contains no sources", errSourceGatewayEmptyCatalog)
		sourceGatewayState.recordFailure(err)
		return err
	}
	type catalogInfo struct {
		status           string
		name             string
		group            string
		lastSuccessAt    string
		cachedEntryCount int
	}
	catalogByID := make(map[string]catalogInfo, len(sources))
	for _, item := range sources {
		catalogByID[item.ID] = catalogInfo{
			status:           firstNonEmpty(item.Status, "healthy"),
			name:             item.Name,
			group:            item.Group,
			lastSuccessAt:    item.LastSuccessAt,
			cachedEntryCount: item.CachedEntryCount,
		}
	}
	now := nowUTC()
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, subscription := range subscriptions {
		sourceID := strings.TrimSpace(subscription.SourceID)
		if !validText(sourceID, 128, true) {
			continue
		}
		info, known := catalogByID[sourceID]
		if !known {
			// GetCatalog intentionally hides sources that are still initializing.
			// A subscription carrying that state is therefore expected to be absent
			// from the readable catalog; preserve initializing so the first head
			// request can retry instead of permanently classifying the source as
			// blocked.  Any other absent subscribed source remains blocked upstream.
			if strings.EqualFold(strings.TrimSpace(subscription.Status), "initializing") {
				info = catalogInfo{status: "initializing"}
			} else {
				info = catalogInfo{status: "blocked"}
			}
		} else if info.status == "blocked" {
			// Defensive: the default catalog view already excludes blocked
			// sources, but never let a blocked marker overwrite readable state.
			info.status = "blocked"
		}
		kind := catalogGroupToKind(info.group)
		name := limitText(info.name, 120)
		if name == "" {
			name = sourceID
		}
		// New maintained-directory sources arrive as non-default rows; existing
		// default rows keep their identity and preferences verbatim.  The URL is
		// a unique non-public marker: content only ever arrives via the gateway.
		if _, err := tx.Exec(`INSERT INTO feed_sources(id, kind, name, url, description, is_default, status, created_at, updated_at, collection_mode, sync_interval_seconds, next_sync_at, backfill_days, retention_days, retention_limit, auto_extract, hidden, gateway_status, gateway_enabled, gateway_has_more)
VALUES(?, ?, ?, 'source-gateway://' || ?, '', 0, 'unavailable', ?, ?, 'source_gateway', 1800, '', 30, 90, 1000, 1, 0, 'initializing', 1, 1)
ON CONFLICT(id) DO NOTHING`, sourceID, kind, name, sourceID, now, now); err != nil {
			return err
		}
		gatewayStatus := gatewayStatusFromCatalog(info.status, subscription.Status)
		if _, err := tx.Exec(`UPDATE feed_sources SET gateway_status=?, gateway_last_success_at=?, gateway_cached_entry_count=?, gateway_has_more=CASE WHEN ? IN ('healthy','degraded') AND gateway_status='blocked' THEN 1 ELSE gateway_has_more END, updated_at=? WHERE id=? AND collection_mode='source_gateway'`, gatewayStatus, info.lastSuccessAt, info.cachedEntryCount, gatewayStatus, now, sourceID); err != nil {
			return err
		}
		// Subscription state is owned by the server; folder/pinned/muted stay
		// local.  The enabled flag is applied only when the row is first
		// materialized locally — a later reconcile must not overwrite a local
		// toggle that has not been forwarded yet.
		if _, err := tx.Exec(`INSERT INTO feed_subscriptions(source_id, enabled, subscribed_at, updated_at) VALUES(?, ?, ?, ?)
ON CONFLICT(source_id) DO NOTHING`, sourceID, boolInt(subscription.Enabled), now, now); err != nil {
			return err
		}
	}
	// A gateway-managed source missing from both the catalog and the
	// subscription list is not "empty": keep its local snapshot and mark it so
	// the UI can say the cloud copy is currently unavailable.
	// Only a complete, non-empty subscription projection may classify rows that
	// are absent from the catalog as blocked.  An empty subscription response is
	// commonly produced by a cold/partially upgraded proxy; preserving the
	// previous statuses keeps existing content and subscriptions usable until a
	// trusted response arrives. Keep the union of catalog IDs and subscription
	// IDs: initializing subscriptions are intentionally hidden from the default
	// catalog, but must not be overwritten as blocked after the loop above.
	if len(subscriptions) > 0 {
		projectedIDs := make(map[string]struct{}, len(catalogByID)+len(subscriptions))
		for sourceID := range catalogByID {
			projectedIDs[sourceID] = struct{}{}
		}
		for _, subscription := range subscriptions {
			sourceID := strings.TrimSpace(subscription.SourceID)
			if validText(sourceID, 128, true) {
				projectedIDs[sourceID] = struct{}{}
			}
		}
		if _, err := tx.Exec(`UPDATE feed_sources SET gateway_status='blocked', updated_at=? WHERE collection_mode='source_gateway' AND gateway_status<>'blocked' AND id NOT IN (SELECT value FROM json_each(?))`, now, mustJSONKeys(projectedIDs)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// gatewayStatusFromCatalog merges the catalog health with the per-user
// subscription state into the local projection vocabulary
// (initializing/healthy/degraded/blocked).
func gatewayStatusFromCatalog(catalogStatus, subscriptionStatus string) string {
	switch strings.ToLower(strings.TrimSpace(catalogStatus)) {
	case "blocked":
		return "blocked"
	case "degraded":
		return "degraded"
	}
	switch strings.ToLower(strings.TrimSpace(subscriptionStatus)) {
	case "initializing":
		return "initializing"
	case "degraded":
		return "degraded"
	case "blocked":
		return "blocked"
	default:
		return "healthy"
	}
}

func catalogGroupToKind(group string) string {
	switch strings.ToLower(strings.TrimSpace(group)) {
	case "youtube":
		return "youtube"
	case "bilibili":
		return "bilibili"
	case "x", "twitter":
		return "rss"
	case "wechat":
		return "rss"
	default:
		return "rss"
	}
}

func mustJSONKeys(values map[string]struct{}) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}

// sourceGatewayStatusHandler powers the reader's offline indicator.  It never
// contacts the network; it reports the process-local record of the last
// gateway attempt plus the per-source catalog projection.
func sourceGatewayStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	bridge := newSourceGatewayBridgeFromEnv()
	sourceGatewayState.mu.Lock()
	lastAttempt, lastSuccess, lastError := sourceGatewayState.lastAttemptAt, sourceGatewayState.lastSuccessAt, sourceGatewayState.lastError
	sourceGatewayState.mu.Unlock()
	counts := map[string]int{}
	rows, err := db.Query(`SELECT COALESCE(NULLIF(gateway_status,''), 'initializing'), COUNT(*) FROM feed_sources WHERE collection_mode='source_gateway' GROUP BY 1`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var status string
			var count int
			if scanErr := rows.Scan(&status, &count); scanErr == nil {
				counts[status] = count
			}
		}
	}
	available := bridge.err == nil && lastError == ""
	degraded := bridge.err == nil && (lastError != "" || counts["degraded"] > 0 || counts["blocked"] > 0 || counts["unavailable"] > 0)
	message := ""
	switch {
	case bridge.err != nil || lastError == "auth":
		message = gatewayUnavailableNotice
		if lastError == "auth" {
			message = gatewayAuthNotice
		}
	case degraded:
		message = "部分云端来源暂不可用，显示上次内容"
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"configured":      bridge.err == nil,
			"available":       available,
			"degraded":        degraded,
			"message":         message,
			"last_attempt_at": lastAttempt,
			"last_success_at": lastSuccess,
			"sources":         counts,
			"schema_version":  sourceGatewaySchemaVersion,
		},
	})
}

// pullGatewayHistoryForReader is the reader-driven tail pull: one bounded
// cloud page per request, only for a single gateway-managed source filter.
func pullGatewayHistoryForReader(r *http.Request, sourceID string) (int, error) {
	source, err := feedSourceByID(sourceID)
	if err != nil {
		return 0, err
	}
	if source.CollectionMode != "source_gateway" || (source.GatewayHasMore == 0 && !sourceGatewayNeedsInitialHead(source)) {
		return 0, nil
	}
	if sourceGatewayState.readerHistoryCoolingDown(time.Now()) {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourceGatewayReaderHistoryTimeout)
	defer cancel()
	pulled, err := pullGatewayHistoryPage(ctx, sourceID)
	if err != nil {
		sourceGatewayState.recordReaderHistoryFailure(time.Now())
		return 0, err
	}
	sourceGatewayState.clearReaderHistoryCooldown()
	return pulled, nil
}

// putSubscription forwards one local subscribe/unsubscribe decision to the
// server, which owns the durable subscription mapping (spec §6.4).  The call
// is idempotent per (source, enabled) and best-effort: a failure is logged and
// the local choice stands until the next successful reconcile.
func (b *sourceGatewayBridge) putSubscription(ctx context.Context, sourceID string, enabled bool) error {
	if !validGatewayEntryID(sourceID) {
		return errSourceGatewayNotFound
	}
	if b == nil || b.err != nil || b.baseURL == nil || b.client == nil {
		return errSourceGatewayNotConfigured
	}
	target := *b.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/subscriptions/" + url.PathEscape(sourceID)
	ctx, cancel := context.WithTimeout(ctx, sourceGatewayListTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, target.String(), strings.NewReader(fmt.Sprintf(`{"enabled":%t}`, enabled)))
	if err != nil {
		return errSourceGatewayUnavailable
	}
	request.Header.Set(bridges.DeviceDataTokenHeader, b.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "plaud-sub:"+sourceID+fmt.Sprintf(":%t", enabled))
	response, err := b.client.Do(request)
	if err != nil {
		sourceGatewayState.recordFailure(fmt.Errorf("%w: %v", errSourceGatewayUnavailable, err))
		return errSourceGatewayUnavailable
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, sourceGatewayMaxPageBytes))
	if statusErr := mapSourceGatewayStatus(body, response.StatusCode); statusErr != nil {
		sourceGatewayState.recordFailure(statusErr)
		return statusErr
	}
	sourceGatewayState.recordSuccess()
	return nil
}

type sourceGatewaySubscriptionOutboxItem struct {
	id           int64
	sourceID     string
	enabled      bool
	attemptCount int
}

type sourceGatewaySubscriptionDelivery struct {
	item sourceGatewaySubscriptionOutboxItem
	err  error
}

func enqueueSourceGatewaySubscription(sourceID string, enabled bool) error {
	if !validGatewayEntryID(sourceID) {
		return errSourceGatewayNotFound
	}
	now := nowUTC()
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The outbox stores desired state, not an event log. Removing an older
	// opposite toggle prevents concurrent delivery from reordering user intent.
	if _, err := tx.Exec(`DELETE FROM source_gateway_subscription_outbox WHERE source_id=?`, sourceID); err != nil {
		return err
	}
	operationID := fmt.Sprintf("plaud-sub:%s:%t", sourceID, enabled)
	if _, err := tx.Exec(`INSERT INTO source_gateway_subscription_outbox(
operation_id,source_id,enabled,attempt_count,next_retry_at,last_error,created_at,updated_at
) VALUES(?,?,?,0,'','',?,?)`, operationID, sourceID, boolInt(enabled), now, now); err != nil {
		return err
	}
	return tx.Commit()
}

func sourceGatewaySubscriptionBackoff(attempt int) time.Duration {
	schedule := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 6 * time.Hour}
	if attempt <= 0 {
		return schedule[0]
	}
	if attempt > len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempt-1]
}

// drainSourceGatewaySubscriptionOutbox mirrors at most one bounded batch.
// Network calls run with fixed concurrency; SQLite mutations are applied in a
// single transaction after all calls finish, so no goroutine writes the DB.
func drainSourceGatewaySubscriptionOutbox(ctx context.Context, bridge *sourceGatewayBridge, limit int) error {
	if bridge == nil || bridge.err != nil {
		return errSourceGatewayNotConfigured
	}
	if limit <= 0 || limit > sourceGatewaySubscriptionBatch {
		limit = sourceGatewaySubscriptionBatch
	}
	rows, err := db.QueryContext(ctx, `SELECT id,source_id,enabled,attempt_count
FROM source_gateway_subscription_outbox
WHERE next_retry_at='' OR julianday(next_retry_at) <= julianday('now')
ORDER BY id ASC LIMIT ?`, limit)
	if err != nil {
		return err
	}
	items := make([]sourceGatewaySubscriptionOutboxItem, 0, limit)
	for rows.Next() {
		var item sourceGatewaySubscriptionOutboxItem
		var enabled int
		if err := rows.Scan(&item.id, &item.sourceID, &enabled, &item.attemptCount); err != nil {
			rows.Close()
			return err
		}
		item.enabled = enabled == 1
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	workerCount := sourceGatewaySubscriptionWorkers
	if workerCount > len(items) {
		workerCount = len(items)
	}
	jobs := make(chan sourceGatewaySubscriptionOutboxItem, len(items))
	deliveries := make(chan sourceGatewaySubscriptionDelivery, len(items))
	for _, item := range items {
		jobs <- item
	}
	close(jobs)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer workers.Done()
			for item := range jobs {
				deliveries <- sourceGatewaySubscriptionDelivery{
					item: item,
					err:  bridge.putSubscription(ctx, item.sourceID, item.enabled),
				}
			}
		}()
	}
	workers.Wait()
	close(deliveries)

	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	for delivery := range deliveries {
		if delivery.err == nil {
			if _, err := tx.Exec(`DELETE FROM source_gateway_subscription_outbox WHERE id=?`, delivery.item.id); err != nil {
				return err
			}
			continue
		}
		attempt := delivery.item.attemptCount + 1
		next := now.Add(sourceGatewaySubscriptionBackoff(attempt)).Format(time.RFC3339)
		if _, err := tx.Exec(`UPDATE source_gateway_subscription_outbox
SET attempt_count=?,next_retry_at=?,last_error=?,updated_at=? WHERE id=?`,
			attempt, next, gatewayErrorClass(delivery.err), now.Format(time.RFC3339), delivery.item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// forwardGatewaySubscription durably mirrors a local toggle to the cloud for
// gateway-managed sources only; user-added feeds stay device-local.
func forwardGatewaySubscription(sourceID string, enabled bool) {
	source, err := feedSourceByID(sourceID)
	if err != nil || source.CollectionMode != "source_gateway" {
		return
	}
	if err := enqueueSourceGatewaySubscription(sourceID, enabled); err != nil {
		log.Printf("来源网关订阅状态入队失败 source=%s: %v", sourceID, err)
		return
	}
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceGatewayListTimeout)
	defer cancel()
	if err := drainSourceGatewaySubscriptionOutbox(ctx, bridge, sourceGatewaySubscriptionBatch); err != nil {
		log.Printf("来源网关订阅状态转发失败 source=%s: %v", sourceID, err)
	}
}

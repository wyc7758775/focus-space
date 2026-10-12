package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type sourceIconRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn sourceIconRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func sourceIconResponse(status int, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "image/png")
	return &http.Response{
		StatusCode:    status,
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func pngIconFixture() []byte {
	return []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
}

func TestSourceIconCacheAndLocalEndpointFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url=? WHERE source_id='github-blog'`, "https://assets.example.com/icon.png"); err != nil {
		t.Fatal(err)
	}
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	calls := 0
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.String() != "https://assets.example.com/icon.png" {
				t.Fatalf("icon request URL = %q", req.URL.String())
			}
			return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
		})}
	}

	first, err := resolveSourceIcon(context.Background(), "github-blog")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveSourceIcon(context.Background(), "github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !bytes.Equal(first.Body, pngIconFixture()) || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("cache calls=%d first=%#v second=%#v", calls, first, second)
	}
	source, err := feedSourceByID("github-blog")
	if err != nil || !source.HasIcon {
		t.Fatalf("source icon projection = %#v, err=%v", source, err)
	}

	get := httptest.NewRecorder()
	sourceIconHandler(get, httptest.NewRequest(http.MethodGet, "/api/sources/icon?id=github-blog", nil))
	if get.Code != http.StatusOK || get.Header().Get("Content-Type") != "image/png" || !bytes.Equal(get.Body.Bytes(), pngIconFixture()) {
		t.Fatalf("icon GET status=%d headers=%v body=%q", get.Code, get.Header(), get.Body.Bytes())
	}
	etag := get.Header().Get("ETag")
	conditionalRequest := httptest.NewRequest(http.MethodGet, "/api/sources/icon?id=github-blog", nil)
	conditionalRequest.Header.Set("If-None-Match", etag)
	conditional := httptest.NewRecorder()
	sourceIconHandler(conditional, conditionalRequest)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 || calls != 1 {
		t.Fatalf("conditional status=%d body=%d calls=%d", conditional.Code, conditional.Body.Len(), calls)
	}
}

func TestSourceIconFetchRejectsUnsafeOversizeAndSVGUnit(t *testing.T) {
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })

	t.Run("private URL is rejected before HTTP", func(t *testing.T) {
		calls := 0
		sourceIconHTTPClientFactory = func() *http.Client {
			calls++
			return &http.Client{}
		}
		if _, _, err := fetchSourceIcon(context.Background(), "http://127.0.0.1/favicon.ico"); err == nil || calls != 0 {
			t.Fatalf("private icon err=%v client_calls=%d", err, calls)
		}
	})

	tests := []struct {
		name   string
		body   []byte
		length int64
	}{
		{name: "svg", body: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), length: -1},
		{name: "html", body: []byte(`<html><body>not an icon</body></html>`), length: -1},
		{name: "oversize", body: bytes.Repeat([]byte{0}, int(maxSourceIconBytes)+1), length: -1},
		{name: "declared oversize", body: pngIconFixture(), length: maxSourceIconBytes + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceIconHTTPClientFactory = func() *http.Client {
				return &http.Client{Transport: sourceIconRoundTripFunc(func(*http.Request) (*http.Response, error) {
					response := sourceIconResponse(http.StatusOK, tt.body)
					response.ContentLength = tt.length
					return response, nil
				})}
			}
			if _, _, err := fetchSourceIcon(context.Background(), "https://assets.example.com/icon"); err == nil {
				t.Fatal("unsafe icon was accepted")
			}
		})
	}
}

func TestSourceIconUsesBoundedProxyForTrustedXPortraitFlow(t *testing.T) {
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	requests := make([]string, 0, 2)
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.URL.String())
			if req.URL.Host == "pbs.twimg.com" {
				return nil, errors.New("direct portrait route unavailable")
			}
			if req.URL.Host != sourceIconProxyHost || req.URL.Query().Get("url") != "https://pbs.twimg.com/profile_images/tibo/current.jpg" || req.URL.Query().Get("width") != "96" || req.URL.Query().Get("height") != "96" {
				t.Fatalf("unexpected proxy request: %s", req.URL.String())
			}
			return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
		})}
	}

	body, contentType, err := fetchSourceIconCandidate(context.Background(), "https://pbs.twimg.com/profile_images/tibo/current.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || contentType != "image/png" || !bytes.Equal(body, pngIconFixture()) {
		t.Fatalf("requests=%#v type=%q body=%q", requests, contentType, body)
	}
}

func TestSourceIconDoesNotProxyArbitraryHostsUnit(t *testing.T) {
	if proxyURL, ok := sourceIconProxyURL("https://assets.example.com/icon.png"); ok || proxyURL != "" {
		t.Fatalf("arbitrary icon was proxied: %q", proxyURL)
	}
}

func TestSourceIconNewCandidateBypassesOldFailureCooldownUnit(t *testing.T) {
	now := time.Now().UTC()
	cache := sourceIconCacheEntry{
		IconURL:    "https://assets.example.com/old.png",
		RetryAfter: now.Add(time.Hour).Format(time.RFC3339),
	}
	if !sourceIconRetryMatches(cache, []string{"https://assets.example.com/old.png"}) {
		t.Fatal("same failed candidate should keep its cooldown")
	}
	if sourceIconRetryMatches(cache, []string{"https://assets.example.com/new.png"}) {
		t.Fatal("new candidate inherited stale cooldown")
	}
}

func TestSourceIconFallsBackToWebsiteDeclaredIconFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url=?,site_url=? WHERE source_id='github-blog'`,
		"https://assets.example.com/icon.svg", "https://example.com/blog"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET url='https://example.com/feed.xml' WHERE id='github-blog'`); err != nil {
		t.Fatal(err)
	}
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	requested := make([]string, 0, 4)
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = append(requested, req.URL.String())
			switch req.URL.String() {
			case "https://assets.example.com/icon.svg":
				return sourceIconResponse(http.StatusOK, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)), nil
			case "https://example.com/favicon.ico":
				return sourceIconResponse(http.StatusNotFound, []byte("missing")), nil
			case "https://example.com/blog":
				response := sourceIconResponse(http.StatusOK, []byte(`<html><head><link rel="shortcut icon" href="https://cdn.example.com/brand.ico"></head></html>`))
				response.ContentLength = maxSourceIconHTMLBytes + 1
				return response, nil
			case "https://cdn.example.com/brand.ico":
				return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
			default:
				return nil, errors.New("unexpected icon request")
			}
		})}
	}

	entry, err := resolveSourceIcon(context.Background(), "github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if entry.IconURL != "https://cdn.example.com/brand.ico" || len(requested) != 4 || requested[0] != "https://assets.example.com/icon.svg" || requested[1] != "https://example.com/favicon.ico" || requested[2] != "https://example.com/blog" || requested[3] != entry.IconURL {
		t.Fatalf("fallback entry=%#v requested=%#v", entry, requested)
	}
	var stored string
	if err := db.QueryRow(`SELECT icon_url FROM feed_catalog_entries WHERE source_id='github-blog'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != entry.IconURL {
		t.Fatalf("stored fallback = %q, want %q", stored, entry.IconURL)
	}
}

func TestSourceIconDiscoversFeedOriginAfterCanonicalSiteFailureFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url='',site_url='https://canonical.example.com/news' WHERE source_id='github-blog'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET url='https://feed.example.com/api/search?q=ai',collection_mode='agent' WHERE id='github-blog'`); err != nil {
		t.Fatal(err)
	}
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.String() {
			case "https://canonical.example.com/favicon.ico", "https://feed.example.com/favicon.ico":
				return sourceIconResponse(http.StatusNotFound, []byte("missing")), nil
			case "https://canonical.example.com/news":
				return sourceIconResponse(http.StatusServiceUnavailable, []byte("unavailable")), nil
			case "https://feed.example.com/":
				return sourceIconResponse(http.StatusOK, []byte(`<html><head><link rel="icon" href="/public/source.png"></head></html>`)), nil
			case "https://feed.example.com/public/source.png":
				return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
			default:
				return nil, errors.New("unexpected icon request")
			}
		})}
	}

	entry, err := resolveSourceIcon(context.Background(), "github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if entry.IconURL != "https://feed.example.com/public/source.png" {
		t.Fatalf("feed-origin icon = %#v", entry)
	}
}

func TestGatewaySourceIconFetchesOnlyExactCatalogURLFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url='https://assets.example.com/icon.png',site_url='https://example.com/' WHERE source_id='hacker-news'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET url='source-gateway://hacker-news',collection_mode='source_gateway' WHERE id='hacker-news'`); err != nil {
		t.Fatal(err)
	}
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	calls := 0
	sourceIconHTTPClientFactory = func() *http.Client {
		calls++
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://assets.example.com/icon.png" {
				t.Fatalf("gateway-managed source fetched unexpected URL %q", req.URL.String())
			}
			return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
		})}
	}

	resolved, err := resolveSourceIcon(context.Background(), "hacker-news")
	if err != nil || !bytes.Equal(resolved.Body, pngIconFixture()) || calls != 1 {
		t.Fatalf("gateway icon=%#v err=%v calls=%d", resolved, err, calls)
	}
	resolved, err = resolveSourceIcon(context.Background(), "hacker-news")
	if err != nil || !bytes.Equal(resolved.Body, pngIconFixture()) || calls != 1 {
		t.Fatalf("cached gateway icon=%#v err=%v calls=%d", resolved, err, calls)
	}
}

func TestGatewayVideoSourceFetchesPinnedCreatorAvatarFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url='' WHERE source_id='bilibili-yingshijufeng'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET collection_mode='source_gateway' WHERE id='bilibili-yingshijufeng'`); err != nil {
		t.Fatal(err)
	}
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	requested := ""
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			requested = req.URL.String()
			return sourceIconResponse(http.StatusOK, pngIconFixture()), nil
		})}
	}

	resolved, err := resolveSourceIcon(context.Background(), "bilibili-yingshijufeng")
	if err != nil {
		t.Fatal(err)
	}
	if requested != "https://unavatar.io/bilibili/946974" || resolved.IconURL != requested || !bytes.Equal(resolved.Body, pngIconFixture()) {
		t.Fatalf("creator avatar=%#v requested=%q", resolved, requested)
	}
}

func TestGatewayXSourceUsesPinnedCreatorAvatarUnit(t *testing.T) {
	if got := creatorAvatarCandidateURL("andrej-karpathy-x"); got != "https://unavatar.io/twitter/karpathy" {
		t.Fatalf("X avatar candidate=%q", got)
	}
	if got := creatorAvatarCandidateURL("unknown-x"); got != "" {
		t.Fatalf("unknown source must not receive an avatar candidate: %q", got)
	}
}

func TestYouTubeProviderLogoIsOnlyCreatorAvatarFallbackUnit(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`UPDATE feed_catalog_entries SET icon_url=? WHERE source_id='youtube-nvidia'`, youtubeProviderIconURL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET collection_mode='source_gateway' WHERE id='youtube-nvidia'`); err != nil {
		t.Fatal(err)
	}
	descriptor, err := sourceIconDescriptorByID("youtube-nvidia")
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Candidates) != 1 || descriptor.Candidates[0] != "https://unavatar.io/youtube/UCHuiy8bXnmK5nisYHUd1J5g" || len(descriptor.FallbackCandidates) != 1 || descriptor.FallbackCandidates[0] != youtubeProviderIconURL {
		t.Fatalf("youtube descriptor=%#v", descriptor)
	}
}

func TestSourceIconFailureDoesNotAffectFeedSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "govinfo-federal-register")
	previousFetch := operationalSourceFetch
	operationalSourceFetch = operationalFixtureFetcher
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	previousFactory := sourceIconHTTPClientFactory
	t.Cleanup(func() { sourceIconHTTPClientFactory = previousFactory })
	sourceIconHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: sourceIconRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("temporary icon outage")
		})}
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "govinfo-federal-register"); err != nil {
		t.Fatal(err)
	}
	icon := httptest.NewRecorder()
	sourceIconHandler(icon, httptest.NewRequest(http.MethodGet, "/api/sources/icon?id=govinfo-federal-register", nil))
	if icon.Code != http.StatusNotFound {
		t.Fatalf("failed icon status=%d body=%s", icon.Code, icon.Body.String())
	}
	var entries int
	var status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='govinfo-federal-register'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM feed_sources WHERE id='govinfo-federal-register'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if entries != operationalSourceLimit || status != "ok" {
		t.Fatalf("feed changed after icon failure: entries=%d status=%q", entries, status)
	}
}

func TestSourceFaviconURLUsesOriginRoot(t *testing.T) {
	if got := sourceFaviconURL("https://example.com/articles/one?ref=feed"); got != "https://example.com/favicon.ico" {
		t.Fatalf("favicon URL = %q", got)
	}
}

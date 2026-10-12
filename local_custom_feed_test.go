package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type localCustomFeedFixture struct {
	kind        string
	contentType string
	path        string
	body        func(string, string) []byte
}

type localCustomDiscoveryResponse struct {
	ID           string     `json:"id"`
	Status       string     `json:"status"`
	FeedFormat   string     `json:"feed_format"`
	ErrorMessage string     `json:"error_message"`
	Preview      []NewsItem `json:"preview"`
}

func installLocalCustomFeedFetcher(t *testing.T, fetch feedDiscoveryFetcher) {
	t.Helper()
	previous := localCustomFeedFetch
	localCustomFeedFetch = fetch
	t.Cleanup(func() { localCustomFeedFetch = previous })
}

func setLocalCustomFeedRuntime(t *testing.T, enabled bool) {
	t.Helper()
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = enabled
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })
}

func decodeLocalCustomFeedResponse[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var payload T
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status=%d body=%s: %v", response.Code, response.Body.String(), err)
	}
	return payload
}

func localCustomFeedFixtures() []localCustomFeedFixture {
	return []localCustomFeedFixture{
		{
			kind:        "rss",
			contentType: "application/rss+xml",
			path:        "/feed.xml",
			body: func(feedURL, published string) []byte {
				parsed, _ := time.Parse(time.RFC3339, published)
				return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>Local RSS</title><description>local custom rss</description>
<link>https://rss-custom.example/</link><item><guid>rss-item-1</guid><title>RSS cached item</title>
<link>https://rss-custom.example/posts/1</link><pubDate>%s</pubDate><description>RSS summary</description></item>
</channel></rss>`, parsed.Format(time.RFC1123Z)))
			},
		},
		{
			kind:        "atom",
			contentType: "application/atom+xml",
			path:        "/feed.atom",
			body: func(feedURL, published string) []byte {
				return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Local Atom</title>
<link rel="self" href="%s"/><link rel="alternate" href="https://atom-custom.example/"/>
<entry><id>atom-item-1</id><title>Atom cached item</title><link href="https://atom-custom.example/posts/1"/>
<updated>%s</updated><summary>Atom summary</summary></entry></feed>`, feedURL, published))
			},
		},
		{
			kind:        "json",
			contentType: "application/feed+json",
			path:        "/feed.json",
			body: func(feedURL, published string) []byte {
				return []byte(fmt.Sprintf(`{
  "version":"https://jsonfeed.org/version/1.1",
  "title":"Local JSON Feed",
  "home_page_url":"https://json-custom.example/",
  "feed_url":%q,
  "items":[{"id":"json-item-1","url":"https://json-custom.example/posts/1","title":"JSON cached item","content_text":"JSON summary","date_published":%q}]
}`, feedURL, published))
			},
		},
	}
}

func TestFoloPublicFeedParsesRenderableArticleImageAndVideo(t *testing.T) {
	feedURL := "https://api.folo.is/feeds?url=https%3A%2F%2Fexample.com%2Ffeed.xml"
	body := []byte(`{
  "code": 0,
  "data": {
    "feed": {
      "title": "Public Technology Feed",
      "description": "Latest public posts",
      "siteUrl": "https://example.com/",
      "image": "https://cdn.example.com/icon.png"
    },
    "entries": [{
      "id": "entry-1",
      "title": "A post with playable media",
      "url": "https://example.com/posts/1",
      "content": "The complete public article summary.",
      "author": "Example Author",
      "publishedAt": "2026-08-27T12:00:00Z",
      "media": [
        {"type":"photo","url":"https://cdn.example.com/photo.jpg"},
        {"type":"video","url":"https://cdn.example.com/video.mp4","preview_image_url":"https://cdn.example.com/video-preview.jpg","duration":"1:23"}
      ]
    }]
  }
}`)
	feed, err := parseFetchedFeed(feedFetchResponse{Body: body, ContentType: "application/json", FinalURL: feedURL}, feedURL)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "Public Technology Feed" || feed.SiteURL != "https://example.com/" || feed.IconURL != "https://cdn.example.com/icon.png" || feed.Format != "json" {
		t.Fatalf("feed metadata=%#v", feed)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items=%d", len(feed.Items))
	}
	item := feed.Items[0]
	if item.URL != "https://example.com/posts/1" || item.SummaryText != "The complete public article summary." || item.ThumbnailURL != "https://cdn.example.com/video-preview.jpg" || item.MediaURL != "https://cdn.example.com/video.mp4" || item.MediaMIME != "video/mp4" || item.DurationSecs != 83 || item.ViewType != "video" {
		t.Fatalf("item=%#v", item)
	}
}

func TestFoloPublicFeedSyncPersistsRenderableArticleFlow(t *testing.T) {
	db = openTestDB(t)
	setLocalCustomFeedRuntime(t, false)
	feedURL := "https://api.folo.is/feeds?url=https%3A%2F%2Fexample.com%2Ffeed.xml"
	installLocalCustomFeedFetcher(t, func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		if raw != feedURL {
			return feedFetchResponse{}, fmt.Errorf("unexpected URL %q", raw)
		}
		return feedFetchResponse{
			FinalURL:    feedURL,
			ContentType: "application/json",
			Body:        []byte(`{"code":0,"data":{"feed":{"title":"Folo synced feed","siteUrl":"https://example.com/"},"entries":[{"id":"entry-1","title":"Persisted article","url":"https://example.com/posts/1","description":"Readable body","publishedAt":"2026-08-27T12:00:00Z","media":[{"type":"photo","url":"https://cdn.example.com/photo.jpg"}]}]}}`),
		}, nil
	})
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,is_default,status,collection_mode,created_at,updated_at) VALUES('top-rss-flow','api','Catalog source',?,0,'unavailable','app',?,?)`, feedURL, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := enqueueSyncRequest("top-rss-flow"); err != nil {
		t.Fatal(err)
	}
	if err := syncRequestedSourceContext(context.Background(), "top-rss-flow"); err != nil {
		t.Fatal(err)
	}
	var title, summary, thumbnail, status string
	if err := db.QueryRow(`SELECT n.title,n.summary_text,n.thumbnail_url,f.status FROM news n JOIN feed_sources f ON f.id=n.source_id WHERE n.source_id='top-rss-flow'`).Scan(&title, &summary, &thumbnail, &status); err != nil {
		t.Fatal(err)
	}
	if title != "Persisted article" || summary != "Readable body" || thumbnail != "https://cdn.example.com/photo.jpg" || status != "ok" {
		t.Fatalf("title=%q summary=%q thumbnail=%q status=%q", title, summary, thumbnail, status)
	}
	response := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=top-rss-flow&limit=10", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Persisted article") || !strings.Contains(response.Body.String(), "photo.jpg") {
		t.Fatalf("entries status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLocalCustomPublicFeedCreateSync304AndOfflineFlow(t *testing.T) {
	for _, fixture := range localCustomFeedFixtures() {
		t.Run(fixture.kind, func(t *testing.T) {
			db = openTestDB(t)
			setLocalCustomFeedRuntime(t, false)

			feedURL := "https://" + fixture.kind + "-custom.example" + fixture.path
			published := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
			body := fixture.body(feedURL, published)
			fetchCalls := 0
			conditionalFetch := false
			offline := false
			installLocalCustomFeedFetcher(t, func(_ context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
				fetchCalls++
				if raw != feedURL {
					return feedFetchResponse{}, fmt.Errorf("unexpected local custom URL %q", raw)
				}
				if offline {
					return feedFetchResponse{}, errors.New("temporary local feed outage")
				}
				if etag != "" || lastModified != "" {
					conditionalFetch = true
					if etag != `"local-v1"` || lastModified != "Sat, 23 Aug 2026 12:00:00 GMT" {
						return feedFetchResponse{}, fmt.Errorf("unexpected cache validators etag=%q modified=%q", etag, lastModified)
					}
					return feedFetchResponse{
						FinalURL: feedURL, ETag: `"local-v1"`,
						LastModified: "Sat, 23 Aug 2026 12:00:00 GMT", NotModified: true,
					}, nil
				}
				return feedFetchResponse{
					Body: body, ContentType: fixture.contentType, FinalURL: feedURL,
					ETag: `"local-v1"`, LastModified: "Sat, 23 Aug 2026 12:00:00 GMT",
				}, nil
			})

			createBody, err := json.Marshal(map[string]any{
				"kind": fixture.kind, "name": "My " + strings.ToUpper(fixture.kind), "url": feedURL,
			})
			if err != nil {
				t.Fatal(err)
			}
			createdResponse := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", string(createBody))
			if createdResponse.Code != http.StatusOK {
				t.Fatalf("create status=%d body=%s", createdResponse.Code, createdResponse.Body.String())
			}
			created := decodeLocalCustomFeedResponse[FeedSource](t, createdResponse)
			if created.ID == "" || created.URL != feedURL || created.Kind != fixture.kind || created.CollectionMode != "app" || created.IsDefault {
				t.Fatalf("created source=%#v", created)
			}

			subscribed := callJSON(t, subscriptionHandler, http.MethodPost, "/api/subscriptions", fmt.Sprintf(`{"source_id":%q}`, created.ID))
			if subscribed.Code != http.StatusOK {
				t.Fatalf("subscribe status=%d body=%s", subscribed.Code, subscribed.Body.String())
			}
			if err := syncRequestedSourceContext(context.Background(), created.ID); err != nil {
				t.Fatalf("initial local sync: %v", err)
			}
			var entries int
			var storedETag, storedLastModified, status string
			if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, created.ID).Scan(&entries); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT etag,last_modified,status FROM feed_sources WHERE id=?`, created.ID).Scan(&storedETag, &storedLastModified, &status); err != nil {
				t.Fatal(err)
			}
			if entries != 1 || storedETag != `"local-v1"` || storedLastModified == "" || status != "ok" {
				t.Fatalf("initial cache entries=%d etag=%q modified=%q status=%q", entries, storedETag, storedLastModified, status)
			}

			if _, err := enqueueSyncRequest(created.ID); err != nil {
				t.Fatal(err)
			}
			if err := syncRequestedSourceContext(context.Background(), created.ID); err != nil {
				t.Fatalf("304 local sync: %v", err)
			}
			var entriesAfter304 int
			var syncMessage string
			if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, created.ID).Scan(&entriesAfter304); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT message FROM sync_requests WHERE source_id=? ORDER BY id DESC LIMIT 1`, created.ID).Scan(&syncMessage); err != nil {
				t.Fatal(err)
			}
			if !conditionalFetch || entriesAfter304 != entries || syncMessage != "内容未变化" {
				t.Fatalf("304 conditional=%v entries=%d/%d message=%q", conditionalFetch, entriesAfter304, entries, syncMessage)
			}

			offline = true
			if _, err := enqueueSyncRequest(created.ID); err != nil {
				t.Fatal(err)
			}
			if err := syncRequestedSourceContext(context.Background(), created.ID); err == nil {
				t.Fatal("offline sync unexpectedly succeeded")
			}
			fetchesBeforeRead := fetchCalls
			preview := callJSON(t, sourcePreviewHandler, http.MethodGet, "/api/sources/preview?source_id="+created.ID, "")
			if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"preview_origin":"local_sqlite"`) || !strings.Contains(preview.Body.String(), "cached item") {
				t.Fatalf("offline preview status=%d body=%s", preview.Code, preview.Body.String())
			}
			entriesResponse := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id="+created.ID+"&limit=10", "")
			if entriesResponse.Code != http.StatusOK || !strings.Contains(entriesResponse.Body.String(), "cached item") {
				t.Fatalf("offline entries status=%d body=%s", entriesResponse.Code, entriesResponse.Body.String())
			}
			if fetchCalls != fetchesBeforeRead {
				t.Fatalf("offline SQLite reads performed network fetches: before=%d after=%d", fetchesBeforeRead, fetchCalls)
			}
			if err := db.QueryRow(`SELECT status FROM feed_sources WHERE id=?`, created.ID).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != "partial" {
				t.Fatalf("last-known-good source status=%q, want partial", status)
			}

			forwardGatewaySubscription(created.ID, true)
			var outbox int
			if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id=?`, created.ID).Scan(&outbox); err != nil {
				t.Fatal(err)
			}
			if outbox != 0 {
				t.Fatalf("custom URL entered gateway outbox: %d", outbox)
			}
		})
	}
}

func TestLocalCustomFeedWebsiteDiscoveryConfirmFlow(t *testing.T) {
	db = openTestDB(t)
	setLocalCustomFeedRuntime(t, true)

	websiteURL := "https://discover-custom.example/"
	feedURL := "https://discover-custom.example/feed.atom"
	published := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	atomBody := localCustomFeedFixtures()[1].body(feedURL, published)
	requests := make([]string, 0, 2)
	installLocalCustomFeedFetcher(t, func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		requests = append(requests, raw)
		switch raw {
		case websiteURL:
			return feedFetchResponse{
				Body:        []byte(`<html><head><title>Discovery publisher</title><link rel="alternate" type="application/atom+xml" href="/feed.atom"></head></html>`),
				ContentType: "text/html", FinalURL: websiteURL,
			}, nil
		case feedURL:
			return feedFetchResponse{Body: atomBody, ContentType: "application/atom+xml", FinalURL: feedURL}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected discovery URL %q", raw)
		}
	})

	discovery := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", fmt.Sprintf(`{"input_url":%q}`, websiteURL))
	if discovery.Code != http.StatusAccepted {
		t.Fatalf("discovery status=%d body=%s", discovery.Code, discovery.Body.String())
	}
	discovered := decodeLocalCustomFeedResponse[localCustomDiscoveryResponse](t, discovery)
	if discovered.ID == "" || discovered.Status != "verified" || discovered.FeedFormat != "atom" || len(discovered.Preview) != 1 {
		t.Fatalf("discovery payload=%#v", discovered)
	}
	if len(requests) != 2 || requests[0] != websiteURL || requests[1] != feedURL {
		t.Fatalf("autodiscovery requests=%v", requests)
	}

	confirmedResponse := callJSON(t, discoveryHandler, http.MethodPatch, "/api/discoveries", fmt.Sprintf(`{"id":%q}`, discovered.ID))
	if confirmedResponse.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmedResponse.Code, confirmedResponse.Body.String())
	}
	confirmed := decodeLocalCustomFeedResponse[FeedSource](t, confirmedResponse)
	if confirmed.URL != feedURL || confirmed.Kind != "atom" || confirmed.CollectionMode != "app" || !confirmed.Subscribed {
		t.Fatalf("confirmed source=%#v", confirmed)
	}
	var cached, enabled, outbox int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=? AND deleted_at=''`, confirmed.ID).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id=?`, confirmed.ID).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	forwardGatewaySubscription(confirmed.ID, true)
	if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id=?`, confirmed.ID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if cached != 1 || enabled != 1 || outbox != 0 {
		t.Fatalf("confirm cache=%d enabled=%d gateway_outbox=%d", cached, enabled, outbox)
	}
}

func TestLocalCustomFeedSecurityBoundariesFlow(t *testing.T) {
	t.Run("signed and private inputs never fetch or persist", func(t *testing.T) {
		db = openTestDB(t)
		setLocalCustomFeedRuntime(t, true)
		fetches := 0
		installLocalCustomFeedFetcher(t, func(context.Context, string, string, string) (feedFetchResponse, error) {
			fetches++
			return feedFetchResponse{}, errors.New("must not fetch rejected input")
		})

		cases := []struct {
			name   string
			url    string
			secret string
		}{
			{name: "signed query", url: "https://custom.example/feed.xml?X-Amz-Signature=do-not-leak", secret: "do-not-leak"},
			{name: "userinfo", url: "https://user:password@custom.example/feed.xml", secret: "password"},
			{name: "private IPv4", url: "http://192.168.10.2/feed.xml"},
			{name: "CGNAT IPv4", url: "http://100.64.0.1/feed.xml"},
			{name: "benchmark IPv4", url: "http://198.18.0.1/feed.xml"},
			{name: "loopback IPv6", url: "http://[::1]/feed.xml"},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", fmt.Sprintf(`{"kind":"rss","name":"Rejected","url":%q}`, test.url))
				if created.Code != http.StatusBadRequest {
					t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
				}
				discovered := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", fmt.Sprintf(`{"input_url":%q}`, test.url))
				if discovered.Code != http.StatusBadRequest {
					t.Fatalf("discovery status=%d body=%s", discovered.Code, discovered.Body.String())
				}
				if test.secret != "" && (strings.Contains(created.Body.String(), test.secret) || strings.Contains(discovered.Body.String(), test.secret)) {
					t.Fatalf("secret leaked: create=%s discovery=%s", created.Body.String(), discovered.Body.String())
				}
			})
		}
		for _, mode := range []string{"source_gateway", "private_local"} {
			t.Run("mode "+mode, func(t *testing.T) {
				created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", fmt.Sprintf(
					`{"kind":"rss","name":"Rejected mode","url":"https://mode-custom.example/feed.xml","collection_mode":%q}`,
					mode,
				))
				if created.Code != http.StatusBadRequest {
					t.Fatalf("mode=%q create status=%d body=%s", mode, created.Code, created.Body.String())
				}
			})
		}
		var discoveries, customSources, outbox int
		if err := db.QueryRow(`SELECT COUNT(*) FROM feed_discovery_candidates`).Scan(&discoveries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id LIKE 'feed-%'`).Scan(&customSources); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id LIKE 'feed-%'`).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if fetches != 0 || discoveries != 0 || customSources != 0 || outbox != 0 {
			t.Fatalf("rejected inputs fetches=%d discoveries=%d sources=%d outbox=%d", fetches, discoveries, customSources, outbox)
		}
	})

	t.Run("redirect to private address fails closed", func(t *testing.T) {
		db = openTestDB(t)
		setLocalCustomFeedRuntime(t, true)
		publicURL := "https://redirect-custom.example/feed.xml"
		privateURL := "http://169.254.169.254/latest/meta-data"
		installLocalCustomFeedFetcher(t, func(context.Context, string, string, string) (feedFetchResponse, error) {
			return feedFetchResponse{
				Body:        localCustomFeedFixtures()[0].body(publicURL, time.Now().UTC().Format(time.RFC3339)),
				ContentType: "application/rss+xml", FinalURL: privateURL,
			}, nil
		})

		response := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", fmt.Sprintf(`{"input_url":%q}`, publicURL))
		if response.Code != http.StatusAccepted {
			t.Fatalf("redirect discovery status=%d body=%s", response.Code, response.Body.String())
		}
		payload := decodeLocalCustomFeedResponse[localCustomDiscoveryResponse](t, response)
		if payload.Status == "verified" || payload.ErrorMessage == "" {
			t.Fatalf("private redirect was accepted: %#v", payload)
		}
		if strings.Contains(response.Body.String(), privateURL) {
			t.Fatalf("private redirect URL leaked: %s", response.Body.String())
		}
		confirm := callJSON(t, discoveryHandler, http.MethodPatch, "/api/discoveries", fmt.Sprintf(`{"id":%q}`, payload.ID))
		if confirm.Code != http.StatusBadRequest {
			t.Fatalf("private redirect confirm status=%d body=%s", confirm.Code, confirm.Body.String())
		}
		var sources, outbox int
		if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id LIKE 'feed-%'`).Scan(&sources); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id LIKE 'feed-%'`).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if sources != 0 || outbox != 0 {
			t.Fatalf("private redirect sources=%d outbox=%d", sources, outbox)
		}
	})

	t.Run("oversize response fails closed", func(t *testing.T) {
		db = openTestDB(t)
		setLocalCustomFeedRuntime(t, true)
		publicURL := "https://oversize-custom.example/feed.xml"
		installLocalCustomFeedFetcher(t, func(context.Context, string, string, string) (feedFetchResponse, error) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Content-Type", "application/rss+xml")
			recorder.WriteHeader(http.StatusOK)
			_, _ = recorder.Write([]byte(strings.Repeat("x", feedFetchMaxBytes+1)))
			response := recorder.Result()
			response.Request = httptest.NewRequest(http.MethodGet, publicURL, nil)
			defer response.Body.Close()
			body, err := readPublicFeedResponseBody(response, publicURL)
			return feedFetchResponse{Body: body, ContentType: "application/rss+xml", FinalURL: publicURL}, err
		})

		response := callJSON(t, discoveryHandler, http.MethodPost, "/api/discoveries", fmt.Sprintf(`{"input_url":%q}`, publicURL))
		if response.Code != http.StatusAccepted {
			t.Fatalf("oversize discovery status=%d body=%s", response.Code, response.Body.String())
		}
		payload := decodeLocalCustomFeedResponse[localCustomDiscoveryResponse](t, response)
		if payload.Status == "verified" || !strings.Contains(payload.ErrorMessage, "大小上限") {
			t.Fatalf("oversize response=%#v", payload)
		}
		var sources, outbox int
		if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id LIKE 'feed-%'`).Scan(&sources); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM source_gateway_subscription_outbox WHERE source_id LIKE 'feed-%'`).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if sources != 0 || outbox != 0 {
			t.Fatalf("oversize response sources=%d outbox=%d", sources, outbox)
		}
	})
}

func TestSourceGatewayCoreFeedNeverUsesLocalCustomFetcherFlow(t *testing.T) {
	db = openTestDB(t)
	setLocalCustomFeedRuntime(t, false)
	t.Setenv("ZAPP_SOURCE_GATEWAY_BASE_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "")

	localFetches := 0
	installLocalCustomFeedFetcher(t, func(context.Context, string, string, string) (feedFetchResponse, error) {
		localFetches++
		return feedFetchResponse{}, errors.New("gateway source reached local custom fetcher")
	})

	source, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if source.CollectionMode != "source_gateway" {
		t.Fatalf("core source mode=%q url=%q", source.CollectionMode, source.URL)
	}
	_ = syncFeedSourceContext(context.Background(), source.ID)
	if localFetches != 0 {
		t.Fatalf("gateway source performed %d local custom fetches", localFetches)
	}
}

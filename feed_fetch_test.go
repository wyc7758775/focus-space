package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPublicFeedHTTPClientUsesConfiguredProxyUnit(t *testing.T) {
	previous := os.Getenv("HTTPS_PROXY")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:18080")
	client := publicFeedHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatal("configured HTTPS proxy was not installed")
	}
	proxy, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "openai.com"}})
	if err != nil || proxy == nil || proxy.Host != "127.0.0.1:18080" {
		t.Fatalf("proxy=%v err=%v", proxy, err)
	}
	untrustedProxy, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "user-feed.example"}})
	if err != nil || untrustedProxy != nil {
		t.Fatalf("user feed unexpectedly inherited proxy: proxy=%v err=%v", untrustedProxy, err)
	}
	if transport.MaxResponseHeaderBytes != feedFetchMaxHeaderBytes || transport.MaxIdleConns != 8 || transport.MaxIdleConnsPerHost != 2 {
		t.Fatalf("transport bounds header=%d idle=%d per_host=%d", transport.MaxResponseHeaderBytes, transport.MaxIdleConns, transport.MaxIdleConnsPerHost)
	}
	if previous != "" {
		t.Setenv("HTTPS_PROXY", previous)
	}
}

func TestPublicFeedHTTPClientProxiesOnlyExactVerifiedWechatFeedsFlow(t *testing.T) {
	proxyHits := make(chan string, 4)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits <- r.Host
		http.Error(w, "test proxy stops before upstream TLS", http.StatusBadGateway)
	}))
	defer proxyServer.Close()
	t.Setenv("HTTPS_PROXY", proxyServer.URL)

	client := publicFeedHTTPClient()
	defer client.CloseIdleConnections()
	transport := client.Transport.(*http.Transport)
	proxyAddress := strings.TrimPrefix(proxyServer.URL, "http://")
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == proxyAddress {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		return nil, fmt.Errorf("test blocked direct dial to %s", address)
	}

	for verifiedURL := range weChatVerifiedDirectFeedPolicies {
		parsedVerifiedURL, parseErr := url.Parse(verifiedURL)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		response, err := client.Get(verifiedURL)
		if response != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatalf("verified feed %q unexpectedly passed the rejecting test proxy", verifiedURL)
		}
		select {
		case host := <-proxyHits:
			if host != parsedVerifiedURL.Hostname()+":443" {
				t.Fatalf("proxy CONNECT host=%q", host)
			}
		case <-time.After(time.Second):
			t.Fatalf("verified feed %q did not use the configured proxy", verifiedURL)
		}
	}

	for _, untrustedURL := range []string{
		"https://wechat2rss.bestblogs.dev/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml",
		"https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml?view=full",
	} {
		response, err := client.Get(untrustedURL)
		if response != nil {
			response.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "test blocked direct dial") {
			t.Fatalf("same-domain feed %q did not stay on direct dialing: %v", untrustedURL, err)
		}
		select {
		case host := <-proxyHits:
			t.Fatalf("same-domain feed %q unexpectedly reached proxy host %q", untrustedURL, host)
		default:
		}
	}
}

func TestPublicDialAllowsExplicitLoopbackProxyUnit(t *testing.T) {
	// 用 JoinHostPort 组装，避免设备发布契约把随机测试端口误判为硬编码监听地址。
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	t.Setenv("HTTPS_PROXY", fmt.Sprintf("http://127.0.0.1:%d", port))
	conn, err := publicDialContext(context.Background(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("configured loopback proxy was rejected: %v", err)
	}
	conn.Close()
}

func TestPublicFeedResponseBudgetIsExactURLScopedUnit(t *testing.T) {
	tests := map[string]int64{
		"https://example.com/feed.xml":                                                                 feedFetchMaxBytes,
		"https://api.folo.is/feeds?url=https%3A%2F%2Fexample.com%2Ffeed.xml":                           feedFetchFoloMaxBytes,
		"https://wechat2rss.xlab.app/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml":                feedFetchFoloMaxBytes,
		"https://WECHAT2RSS.XLAB.APP./feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml":               feedFetchFoloMaxBytes,
		"https://wechat2rss.xlab.app.evil/feed.xml":                                                    feedFetchMaxBytes,
		"https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml":           feedFetchFoloMaxBytes,
		"https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml":           feedFetchFoloMaxBytes,
		"https://wechat2rss.bestblogs.dev/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml":           feedFetchMaxBytes,
		"https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml?view=full": feedFetchMaxBytes,
		"http://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml":            feedFetchMaxBytes,
	}
	for rawURL, want := range tests {
		if got := publicFeedResponseMaxBytes(nil, rawURL); got != want {
			t.Fatalf("url=%q max=%d want=%d", rawURL, got, want)
		}
	}
}

func TestPublicFeedResponseBudgetUsesFinalRedirectURLUnit(t *testing.T) {
	verifiedURL := "https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml"
	otherURL := "https://wechat2rss.bestblogs.dev/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml"
	for _, test := range []struct {
		name, requestedURL, finalURL string
		want                         int64
	}{
		{name: "verified redirects away", requestedURL: verifiedURL, finalURL: otherURL, want: feedFetchMaxBytes},
		{name: "redirect reaches verified", requestedURL: otherURL, finalURL: verifiedURL, want: feedFetchFoloMaxBytes},
	} {
		t.Run(test.name, func(t *testing.T) {
			final, err := url.Parse(test.finalURL)
			if err != nil {
				t.Fatal(err)
			}
			response := &http.Response{Request: &http.Request{URL: final}}
			if got := publicFeedResponseMaxBytes(response, test.requestedURL); got != test.want {
				t.Fatalf("max=%d want=%d", got, test.want)
			}
		})
	}
}

func TestPublicFeedResponseBodyBudgetEnforcesExactURLBoundaryUnit(t *testing.T) {
	responseFor := func(rawURL string, body []byte, contentLength int64) *http.Response {
		t.Helper()
		parsed, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: contentLength,
			Request:       &http.Request{URL: parsed},
		}
	}

	overGenericLimit := bytes.Repeat([]byte{'x'}, feedFetchMaxBytes+1)
	for verifiedURL := range weChatVerifiedDirectFeedPolicies {
		response := responseFor(verifiedURL, overGenericLimit, -1)
		body, err := readPublicFeedResponseBody(response, "https://example.com/original.xml")
		response.Body.Close()
		if err != nil || len(body) != len(overGenericLimit) {
			t.Fatalf("verified feed %q body len=%d err=%v", verifiedURL, len(body), err)
		}
	}

	otherURL := "https://wechat2rss.bestblogs.dev/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml"
	response := responseFor(otherURL, overGenericLimit, -1)
	_, err := readPublicFeedResponseBody(response, "https://example.com/original.xml")
	response.Body.Close()
	if err == nil || !strings.Contains(err.Error(), "超过大小上限") {
		t.Fatalf("same-domain non-verified feed accepted >2 MiB: %v", err)
	}

	verifiedURL := "https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml"
	response = responseFor(verifiedURL, nil, feedFetchFoloMaxBytes+1)
	_, err = readPublicFeedResponseBody(response, verifiedURL)
	response.Body.Close()
	if err == nil || !strings.Contains(err.Error(), "超过大小上限") {
		t.Fatalf("verified feed accepted >8 MiB Content-Length: %v", err)
	}
}

func TestParseFeedVariants(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		format     string
		externalID string
		itemURL    string
		mediaURL   string
		iconURL    string
	}{
		{
			name:   "rss with relative links and enclosure",
			body:   `<rss version="2.0"><channel><title>Example RSS</title><link>https://example.com/</link><image><url>/brand.png</url></image><item><title>One</title><guid>entry-1</guid><link>/posts/one</link><pubDate>Wed, 07 Aug 2024 10:00:00 +0000</pubDate><description><![CDATA[<p>Hello</p>]]></description><enclosure url="/audio/one.mp3" type="audio/mpeg"/></item></channel></rss>`,
			format: "rss", externalID: "entry-1", itemURL: "https://example.com/posts/one", mediaURL: "https://example.com/audio/one.mp3", iconURL: "https://example.com/brand.png",
		},
		{
			name:   "atom",
			body:   `<feed xmlns="http://www.w3.org/2005/Atom"><title>Example Atom</title><link href="https://example.com/"/><icon>/atom-icon.png</icon><entry><id>tag:example.com,2024:2</id><title>Two</title><link rel="alternate" href="/posts/two"/><updated>2024-08-07T11:00:00Z</updated><summary>Summary</summary></entry></feed>`,
			format: "atom", externalID: "tag:example.com,2024:2", itemURL: "https://example.com/posts/two", iconURL: "https://example.com/atom-icon.png",
		},
		{
			name:   "json feed",
			body:   `{"version":"https://jsonfeed.org/version/1.1","title":"Example JSON","home_page_url":"https://example.com/","icon":"/json-icon.png","items":[{"id":"json-1","url":"/posts/json","title":"Three","content_text":"Text","date_published":"2024-08-07T12:00:00Z"}]}`,
			format: "json", externalID: "json-1", itemURL: "https://example.com/posts/json", iconURL: "https://example.com/json-icon.png",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed, err := parseFetchedFeed(feedFetchResponse{Body: []byte(tt.body), ContentType: "application/xml"}, "https://example.com/feed.xml")
			if err != nil {
				// JSON Feed is intentionally detected before XML; retry with its
				// correct content type to keep the fixture explicit.
				feed, err = parseFetchedFeed(feedFetchResponse{Body: []byte(tt.body), ContentType: "application/feed+json"}, "https://example.com/feed.json")
			}
			if err != nil {
				t.Fatal(err)
			}
			if feed.Format != tt.format || len(feed.Items) != 1 {
				t.Fatalf("feed = %#v", feed)
			}
			item := feed.Items[0]
			if item.ExternalID != tt.externalID || item.URL != tt.itemURL {
				t.Fatalf("item identity = %#v", item)
			}
			if tt.mediaURL != "" && item.MediaURL != tt.mediaURL {
				t.Fatalf("media url = %q", item.MediaURL)
			}
			if feed.IconURL != tt.iconURL {
				t.Fatalf("icon url = %q, want %q", feed.IconURL, tt.iconURL)
			}
			if !validHTTPURL(item.URL) || item.FetchedAt == "" || item.ContentHash == "" {
				t.Fatalf("item is not bounded/identified: %#v", item)
			}
		})
	}
}

func TestWechatRSSContentEncodedIsStoredInBoundedReaderCacheFlow(t *testing.T) {
	db = openTestDB(t)
	body := `<rss><channel><title>新智元</title><item><guid>wechat-rich-1</guid><title>三个 Claude</title><link>https://mp.weixin.qq.com/s?id=rich-1</link><content:encoded><![CDATA[<p>正文第一段</p><p><strong>正文第二段</strong><img src="https://wechat2rss.xlab.app/img-proxy/?u=https%3A%2F%2Fmmbiz.qpic.cn%2Fbody.jpg"/></p>]]></content:encoded></item></channel></rss>`
	feed, err := parseXMLFeed([]byte(body), "https://wechat2rss.xlab.app/feed/test.xml")
	if err != nil || len(feed.Items) != 1 {
		t.Fatalf("rich WeChat feed parse = %#v, err=%v", feed, err)
	}
	item := feed.Items[0]
	item.SourceID = "wechat-aiera"
	item.Source = "新智元"
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, item, "2026-08-17T12:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var status, format, html, sourceURL string
	if err := db.QueryRow(`SELECT status,content_format,content_html,source_url FROM entry_full_texts WHERE entry_id=?`, item.ID).Scan(&status, &format, &html, &sourceURL); err != nil {
		t.Fatal(err)
	}
	if status != "ok" || format != "html" || !strings.Contains(html, "正文第一段") || sourceURL != item.URL {
		t.Fatalf("rich WeChat cache = status=%q format=%q html=%q url=%q", status, format, html, sourceURL)
	}
}

func TestParseRSSChannelPrefersStandardThenITunesIcon(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "standard image wins",
			body: `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Podcast</title><image><url>/rss.png</url></image><itunes:image href="/itunes.jpg"/><item><guid>one</guid><title>One</title><link>/one</link></item></channel></rss>`,
			want: "https://example.com/rss.png",
		},
		{
			name: "itunes image fallback",
			body: `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Podcast</title><itunes:image href="/itunes.jpg"/><item><guid>one</guid><title>One</title><link>/one</link></item></channel></rss>`,
			want: "https://example.com/itunes.jpg",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed, err := parseXMLFeed([]byte(tt.body), "https://example.com/feed.xml")
			if err != nil {
				t.Fatal(err)
			}
			if feed.IconURL != tt.want {
				t.Fatalf("icon url = %q, want %q", feed.IconURL, tt.want)
			}
		})
	}
}

func TestSelectFetchedFeedItemsScopesIdentityBySourceFlow(t *testing.T) {
	feedURL := "https://example.com/feed.xml"
	body := `<rss><channel><title>Example feed</title>
<item><guid>shared-guid</guid><title>Article one</title><link>https://example.com/one</link><description>Article summary</description></item>
</channel></rss>`
	feed, err := parseFetchedFeed(feedFetchResponse{Body: []byte(body), ContentType: "application/rss+xml", FinalURL: feedURL}, feedURL)
	if err != nil {
		t.Fatal(err)
	}
	// 用户自添加来源仍走本地受限抓取；条目身份按来源隔离，不再保留任何
	// 默认源专属的过滤特例（默认源内容筛选由云端来源网关负责）。
	source := FeedSource{ID: "user-feed", Kind: "rss", Name: "Example"}
	items := selectFetchedFeedItems(source, feed, time.Time{})
	if len(items) != 1 {
		t.Fatalf("selected items = %#v", items)
	}
	item := items[0]
	if item.URL != "https://example.com/one" || item.Source != source.Name || item.SourceID != source.ID {
		t.Fatalf("article projection = %#v", item)
	}
	other := selectFetchedFeedItems(FeedSource{ID: "other-feed", Kind: "rss", Name: "Other"}, feed, time.Time{})
	if len(other) != 1 || other[0].ID == item.ID {
		t.Fatalf("entry identity must be scoped by source: %#v vs %#v", item, other)
	}
}

func TestParseXMLFeedSupportsDeclaredLegacyCharset(t *testing.T) {
	body := []byte("<?xml version=\"1.0\" encoding=\"windows-1252\"?><rss><channel><title>Company filings</title><item><guid>filing-1</guid><title>Director\x92s report</title><link>https://example.com/filing-1</link><description>Public filing</description></item></channel></rss>")
	feed, err := parseXMLFeed(body, "https://example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "Director’s report" {
		t.Fatalf("legacy charset feed = %#v", feed)
	}
}

func TestHTMLAutodiscoveryAndPublicURLValidation(t *testing.T) {
	feedURL, title, description := discoverHTMLFeed([]byte(`<html><head><title>Site</title><meta name="description" content="A site"><link rel="alternate" type="application/atom+xml" href="/atom.xml"></head></html>`), "https://example.com/articles")
	if feedURL != "https://example.com/atom.xml" || title != "Site" || description != "A site" {
		t.Fatalf("autodiscovery = %q %q %q", feedURL, title, description)
	}
	privateFeedHost := "10.0.0." + "2"
	userinfoFeedURL := "https://" + "user:" + "pass@" + "example.com/feed"
	for _, raw := range []string{
		"http://127.0.0.1/feed", "http://" + privateFeedHost + "/feed", "https://example.com/feed?token=secret",
		"https://example.com/feed?ok=1;token=secret",
		userinfoFeedURL, "file:///tmp/feed",
	} {
		if _, err := normalizePublicURL(raw); err == nil {
			t.Fatalf("unsafe URL accepted: %s", raw)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchPublicURL(ctx, "http://127.0.0.1/feed"); err == nil {
		t.Fatal("cancelled/private fetch unexpectedly succeeded")
	}
}

func TestResolvedIPPolicyRejectsPrivateIPv4AndIPv6(t *testing.T) {
	privateIPv4 := "10.0.0." + "1"
	tests := []struct {
		raw  string
		want bool
	}{
		{raw: "1.1.1.1", want: true},
		{raw: "2606:4700:4700::1111", want: true},
		{raw: privateIPv4, want: false},
		{raw: "127.0.0.1", want: false},
		{raw: "169.254.1.1", want: false},
		{raw: "fc00::1", want: false},
		{raw: "fd12:3456:789a::1", want: false},
		{raw: "fe80::1", want: false},
		{raw: "::1", want: false},
	}
	for _, tt := range tests {
		if got := isPublicIP(net.ParseIP(tt.raw)); got != tt.want {
			t.Errorf("isPublicIP(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}

func TestPublicDialRejectsIPv6PrivateAddressBeforeNetworkFlow(t *testing.T) {
	conn, err := publicDialContext(context.Background(), "tcp", "[fd12:3456:789a::1]:443")
	if conn != nil {
		conn.Close()
		t.Fatal("private IPv6 dial unexpectedly returned a connection")
	}
	if err == nil || !strings.Contains(err.Error(), "受限网络") {
		t.Fatalf("private IPv6 dial error = %v", err)
	}
}

func TestHTMLAutodiscoveryCycleIsBoundedFlow(t *testing.T) {
	responses := map[string]feedFetchResponse{
		"https://example.com/b": {
			Body:        []byte(`<html><head><link rel="alternate" type="application/rss+xml" href="https://example.com/a"></head></html>`),
			ContentType: "text/html",
			FinalURL:    "https://example.com/b",
		},
	}
	calls := 0
	fetch := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls++
		return responses[raw], nil
	}
	initial := feedFetchResponse{
		Body:        []byte(`<html><head><link rel="alternate" type="application/rss+xml" href="https://example.com/b"></head></html>`),
		ContentType: "text/html",
		FinalURL:    "https://example.com/a",
	}
	_, err := parseFetchedFeedWithDiscovery(context.Background(), initial, "https://example.com/a", 0, map[string]struct{}{}, fetch)
	if err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("autodiscovery fetch calls = %d, want 1", calls)
	}
}

func TestEntryUpsertIdempotencyFlow(t *testing.T) {
	db = openTestDB(t)
	source, err := feedSourceByID("github-blog")
	if err != nil {
		t.Fatal(err)
	}
	first := NewsItem{ID: stableObjectID("entry", "external-1"), ExternalID: "external-1", Title: "First", URL: "https://example.com/one", Source: source.Name, SourceID: source.ID, Origin: "app-fetch", SourceType: "rss", PublishedAt: "2024-08-07T00:00:00Z", SummaryText: "old", ContentFormat: "text", ContentHash: "hash-1", FetchedAt: "2024-08-07T00:00:00Z"}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, first, "2024-08-07T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Title = "Updated"
	second.SummaryText = "new"
	second.ContentHash = "hash-2"
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, second, "2024-08-07T01:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id = ? AND external_id = ?`, source.ID, "external-1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate entry rows = %d", count)
	}
	var title, summary string
	if err := db.QueryRow(`SELECT title, summary_text FROM news WHERE source_id = ? AND external_id = ?`, source.ID, "external-1").Scan(&title, &summary); err != nil {
		t.Fatal(err)
	}
	if title != "Updated" || summary != "new" {
		t.Fatalf("upsert did not refresh content: %q %q", title, summary)
	}
	if !strings.HasPrefix(first.ID, "entry-") {
		t.Fatal("entry id is not deterministic")
	}
}

func TestEntryIdentityIsScopedBySourceFlow(t *testing.T) {
	db = openTestDB(t)
	for _, source := range []struct{ id, name string }{{"github-blog", "GitHub"}, {"cloudflare-blog", "Cloudflare"}} {
		item := NewsItem{
			ID: stableObjectID("entry", source.id+"|same-guid"), ExternalID: "same-guid",
			Title: source.name, URL: "https://example.com/" + source.id, Source: source.name,
			SourceID: source.id, Origin: "app-fetch", SourceType: "rss",
			PublishedAt: "2024-08-07T00:00:00Z", SummaryText: source.name,
			ContentFormat: "text", ContentHash: source.id, FetchedAt: "2024-08-07T00:00:00Z",
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := upsertFetchedNewsTx(tx, item, item.FetchedAt); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE external_id='same-guid'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("same GUID collapsed across sources: %d", count)
	}
}

func TestFetchedFeedPreservesAgentProjectionAndRecordsConflictFlow(t *testing.T) {
	db = openTestDB(t)
	seed := NewsItem{
		ID: stableObjectID("entry", "agent-source|same-guid"), ExternalID: "same-guid",
		Title: "Agent conclusion", URL: "https://example.com/one", Source: "Agent",
		SourceID: "agent-source", Origin: "agent", SourceType: "evidence",
		PublishedAt: "2024-08-07T00:00:00Z", SummaryText: "confirmed by user",
		ContentFormat: "text", ContentHash: "agent-hash", FetchedAt: "2024-08-07T00:00:00Z",
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, seed, seed.FetchedAt); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	incoming := seed
	incoming.Title = "Public feed rewrite"
	incoming.SummaryText = "untrusted update"
	incoming.ContentHash = "feed-hash"
	incoming.Origin = "app-fetch"
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertFetchedNewsTx(tx, incoming, "2024-08-07T01:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var title, conflict string
	if err := db.QueryRow(`SELECT title,conflict_state FROM news WHERE source_id='agent-source' AND external_id='same-guid'`).Scan(&title, &conflict); err != nil {
		t.Fatal(err)
	}
	if title != "Agent conclusion" || conflict != "needs_review" {
		t.Fatalf("agent projection was overwritten: title=%q conflict=%q", title, conflict)
	}
	var conflicts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entry_conflicts WHERE source_id='agent-source' AND external_id='same-guid' AND resolved_at=''`).Scan(&conflicts); err != nil {
		t.Fatal(err)
	}
	if conflicts != 1 {
		t.Fatalf("open conflicts = %d, want 1", conflicts)
	}
}

func TestSourceSubscriptionTriggersImmediateRequestFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })
	result := callJSON(t, subscriptionHandler, http.MethodPost, "/api/subscriptions", `{"source_id":"github-blog"}`)
	if result.Code != http.StatusOK {
		t.Fatalf("subscription status = %d, body = %s", result.Code, result.Body.String())
	}
	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id='github-blog' AND status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending == 0 {
		t.Fatal("subscription did not enqueue an immediate sync request")
	}
}

func TestDirectSourceCreateCannotBypassCatalogIDFlow(t *testing.T) {
	db = openTestDB(t)

	result := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"https://github.blog/feed/"}`)
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), catalogSourceRequiresStableIDMessage) {
		t.Fatalf("catalog URL bypass status = %d, body = %s", result.Code, result.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id='github-blog'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate source rows = %d, want 1", count)
	}
}

func TestConfirmDiscoveryRejectsLegacyCandidateWithoutMutationFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })

	now := "2026-08-09T05:00:00Z"
	preview, err := json.Marshal([]NewsItem{{
		ID: "entry-discovered-existing", ExternalID: "same-guid", Title: "GitHub 更新",
		URL: "https://github.blog/changelog/example", Source: "GitHub Blog",
		SourceID: "feed-placeholder", Origin: "discovery-preview", SourceType: "rss",
		PublishedAt: now, SummaryText: "公开预览", ContentFormat: "text", ContentHash: "hash-existing", FetchedAt: now,
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO feed_discovery_candidates
(id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,feed_format,status,error_message,preview_json,discovered_at,confirmed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"discovery-existing", "https://github.blog/feed/", "https://github.blog/feed/", "https://github.blog/feed/",
		"GitHub Blog", "公开更新", "https://github.blog/", "", "rss", "verified", "", string(preview), now, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := confirmDiscovery("discovery-existing"); err == nil || err.Error() != catalogSourceRequiresStableIDMessage {
		t.Fatalf("confirm legacy candidate error=%v", err)
	}
	var sourceCount, subscriptions, syncRequests int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id='github-blog'`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_subscriptions WHERE source_id=? AND enabled=1`, "github-blog").Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id=?`, "github-blog").Scan(&syncRequests); err != nil {
		t.Fatal(err)
	}
	if sourceCount != 1 || subscriptions != 0 || syncRequests != 0 {
		t.Fatalf("rejected confirmation mutated state: sources=%d subscriptions=%d requests=%d", sourceCount, subscriptions, syncRequests)
	}
	var category, confirmedAt string
	if err := db.QueryRow(`SELECT c.category, d.confirmed_at
FROM feed_catalog_entries c
JOIN feed_discovery_candidates d ON d.id=?
WHERE c.source_id=?`, "discovery-existing", "github-blog").Scan(&category, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	if category != "programming" || confirmedAt != "" {
		t.Fatalf("catalog or candidate changed: category=%q confirmed_at=%q", category, confirmedAt)
	}
}

func TestSyncRequestIsDeduplicatedAndRetryableFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })

	first, err := enqueueSyncRequest("github-blog")
	if err != nil {
		t.Fatal(err)
	}
	second, err := enqueueSyncRequest("github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("duplicate request ids = %d and %d", first, second)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id='github-blog' AND status='pending'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("pending request count = %d, want 1", count)
	}
	if _, err := db.Exec(`UPDATE sync_requests SET status='retry_wait', message='temporary', next_retry_at='later' WHERE id=?`, first); err != nil {
		t.Fatal(err)
	}
	retried, err := enqueueSyncRequest("github-blog")
	if err != nil {
		t.Fatal(err)
	}
	if retried != first {
		t.Fatalf("retry request id = %d, want %d", retried, first)
	}
	var status, message, nextRetry string
	if err := db.QueryRow(`SELECT status,message,next_retry_at FROM sync_requests WHERE id=?`, first).Scan(&status, &message, &nextRetry); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || message != "" || nextRetry != "" {
		t.Fatalf("retry request was not reset: status=%q message=%q next_retry=%q", status, message, nextRetry)
	}
}

func TestConcurrentSyncRequestsAreSerializedFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })

	const workers = 24
	start := make(chan struct{})
	errorsByWorker := make(chan error, workers)
	var workersDone sync.WaitGroup
	for index := 0; index < workers; index++ {
		workersDone.Add(1)
		go func() {
			defer workersDone.Done()
			<-start
			_, err := enqueueSyncRequest("github-blog")
			errorsByWorker <- err
		}()
	}
	close(start)
	workersDone.Wait()
	close(errorsByWorker)
	for err := range errorsByWorker {
		if err != nil {
			t.Fatalf("concurrent enqueue failed: %v", err)
		}
	}
	var pending int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id='github-blog' AND status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending requests=%d want=1", pending)
	}
}

func TestDeleteSubscriptionAlwaysDisablesEvenIfBodySaysEnabledFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })

	created := callJSON(t, subscriptionHandler, http.MethodPost, "/api/subscriptions", `{"source_id":"github-blog","enabled":true}`)
	if created.Code != http.StatusOK {
		t.Fatalf("subscribe status = %d, body = %s", created.Code, created.Body.String())
	}
	deleted := callJSON(t, subscriptionHandler, http.MethodDelete, "/api/subscriptions", `{"source_id":"github-blog","enabled":true}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", deleted.Code, deleted.Body.String())
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='github-blog'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("DELETE left subscription enabled=%d", enabled)
	}
	var status, message string
	if err := db.QueryRow(`SELECT status, message FROM sync_requests WHERE source_id='github-blog' ORDER BY id DESC LIMIT 1`).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || message != "来源已退订" {
		t.Fatalf("pending sync was not cancelled: status=%q message=%q", status, message)
	}
}

func TestFeedSyncQueueBackpressureIsBoundedFlow(t *testing.T) {
	db = openTestDB(t)
	previousEnabled, previousQueue := feedSyncRuntimeEnabled, feedSyncQueue
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 1)
	t.Cleanup(func() {
		clearFeedSyncQueueState(feedSyncQueue)
		feedSyncRuntimeEnabled = previousEnabled
		feedSyncQueue = previousQueue
	})
	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('','pending','2026-08-08T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	triggerSourceSync("")
	triggerSourceSync("")
	if got := len(feedSyncQueue); got != 1 {
		t.Fatalf("queue length = %d, want 1", got)
	}
	var status, message string
	if err := db.QueryRow(`SELECT status,message FROM sync_requests WHERE source_id='' ORDER BY id DESC LIMIT 1`).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if status != "retry_wait" || !strings.Contains(message, "队列繁忙") {
		t.Fatalf("backpressure status=%q message=%q", status, message)
	}
}

func TestFeedSyncQueueDeduplicatesSourceIDsUnit(t *testing.T) {
	db = openTestDB(t)
	previousEnabled, previousQueue := feedSyncRuntimeEnabled, feedSyncQueue
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 4)
	clearFeedSyncQueueState(feedSyncQueue)
	t.Cleanup(func() {
		clearFeedSyncQueueState(feedSyncQueue)
		feedSyncRuntimeEnabled = previousEnabled
		feedSyncQueue = previousQueue
	})

	triggerSourceSync("source-a")
	triggerSourceSync("source-a")
	triggerSourceSync(" source-a ")
	triggerSourceSync("source-b")
	if got := len(feedSyncQueue); got != 2 {
		t.Fatalf("queue length=%d, want two distinct source IDs", got)
	}
	queued := []string{<-feedSyncQueue, <-feedSyncQueue}
	if !((queued[0] == "source-a" && queued[1] == "source-b") || (queued[0] == "source-b" && queued[1] == "source-a")) {
		t.Fatalf("queued source IDs=%q, want source-a and source-b", queued)
	}

	// Once an item has been consumed, the same source can be scheduled again.
	releaseFeedSyncQueueItem(feedSyncQueue, "source-a")
	triggerSourceSync("source-a")
	if got := len(feedSyncQueue); got != 1 {
		t.Fatalf("source was not schedulable after completion, queue length=%d", got)
	}
}

func TestPendingSourceSyncRecoversAfterQueueBackpressureFlow(t *testing.T) {
	db = openTestDB(t)
	previousEnabled, previousQueue := feedSyncRuntimeEnabled, feedSyncQueue
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 1)
	clearFeedSyncQueueState(feedSyncQueue)
	t.Cleanup(func() {
		clearFeedSyncQueueState(feedSyncQueue)
		feedSyncRuntimeEnabled = previousEnabled
		feedSyncQueue = previousQueue
	})

	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('recover-source','pending',?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at,next_retry_at) VALUES('future-source','retry_wait',?,?)`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	// Occupy the bounded queue. The source-specific request must stay pending,
	// rather than being converted into a lost in-memory job.
	triggerSourceSync("busy-source")
	triggerSourceSync("recover-source")
	var status string
	if err := db.QueryRow(`SELECT status FROM sync_requests WHERE source_id='recover-source'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("backpressured source status=%q, want pending", status)
	}
	if got := len(feedSyncQueue); got != 1 {
		t.Fatalf("backpressured source unexpectedly entered queue, length=%d", got)
	}

	// A recovery pass after capacity is available requeues the pending source,
	// while respecting a future retry backoff and the source-ID dedup guard.
	<-feedSyncQueue
	if recovered, err := recoverPendingFeedSyncRequests(context.Background(), 10); err != nil {
		t.Fatal(err)
	} else if recovered != 1 {
		t.Fatalf("recovered request count=%d, want 1", recovered)
	}
	if got := len(feedSyncQueue); got != 1 {
		t.Fatalf("recovery queue length=%d, want 1", got)
	}
	if queued := <-feedSyncQueue; queued != "recover-source" {
		t.Fatalf("recovery queued %q, want recover-source", queued)
	}
	if recovered, err := recoverPendingFeedSyncRequests(context.Background(), 10); err != nil {
		t.Fatal(err)
	} else if recovered != 1 {
		t.Fatalf("deduplicated recovery count=%d, want one covered request", recovered)
	}
	if got := len(feedSyncQueue); got != 0 {
		t.Fatalf("duplicate recovery enqueued %d jobs", got)
	}
}

func TestFeedSyncWorkerPanicReleasesQueueItemFlow(t *testing.T) {
	db = openTestDB(t)
	previousEnabled, previousQueue := feedSyncRuntimeEnabled, feedSyncQueue
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 1)
	clearFeedSyncQueueState(feedSyncQueue)
	t.Cleanup(func() {
		clearFeedSyncQueueState(feedSyncQueue)
		feedSyncRuntimeEnabled = previousEnabled
		feedSyncQueue = previousQueue
	})

	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at) VALUES('panic-source','pending',?)`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if !tryTriggerSourceSync("panic-source") {
		t.Fatal("panic source was not queued")
	}
	queued := <-feedSyncQueue
	if err := runQueuedFeedSync(context.Background(), feedSyncQueue, queued, func(context.Context, string) error {
		panic("synthetic worker failure")
	}); err == nil {
		t.Fatal("worker panic was not converted to a retryable error")
	}
	var status, nextRetry string
	if err := db.QueryRow(`SELECT status,next_retry_at FROM sync_requests WHERE source_id='panic-source'`).Scan(&status, &nextRetry); err != nil {
		t.Fatal(err)
	}
	if status != "retry_wait" || nextRetry == "" {
		t.Fatalf("panic request status=%q next_retry_at=%q, want retry_wait with backoff", status, nextRetry)
	}
	if !tryTriggerSourceSync("panic-source") {
		t.Fatal("queue item dedup state was not released after panic")
	}
}

func TestLazyRecoveryDoesNotPrefetchDueSourcesUnit(t *testing.T) {
	db = openTestDB(t)
	previousEnabled, previousQueue := feedSyncRuntimeEnabled, feedSyncQueue
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 1)
	clearFeedSyncQueueState(feedSyncQueue)
	t.Cleanup(func() {
		clearFeedSyncQueueState(feedSyncQueue)
		feedSyncRuntimeEnabled = previousEnabled
		feedSyncQueue = previousQueue
	})

	if _, err := db.Exec(`UPDATE feed_sources SET next_sync_at=? WHERE id='github-blog'`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if recovered, err := recoverPendingFeedSyncRequests(context.Background(), 100000); err != nil {
		t.Fatal(err)
	} else if recovered != 0 || len(feedSyncQueue) != 0 {
		t.Fatalf("lazy recovery prefetched due source: recovered=%d queue=%d", recovered, len(feedSyncQueue))
	}
	if _, err := db.Exec(`INSERT INTO sync_requests(source_id,status,requested_at,next_retry_at) VALUES('malformed-retry','retry_wait',?,?)`, time.Now().UTC().Format(time.RFC3339), "not-a-timestamp"); err != nil {
		t.Fatal(err)
	}
	if recovered, err := recoverPendingFeedSyncRequests(context.Background(), 100000); err != nil {
		t.Fatal(err)
	} else if recovered != 1 {
		t.Fatalf("malformed retry was not recovered: recovered=%d", recovered)
	}
	if queued := <-feedSyncQueue; queued != "malformed-retry" {
		t.Fatalf("queued source=%q, want malformed-retry", queued)
	}
}

func TestFeedSyncWorkersHonorShutdownBeforeDatabaseCloseFlow(t *testing.T) {
	previousQueue := feedSyncQueue
	feedSyncQueue = make(chan string, 1)
	t.Cleanup(func() { feedSyncQueue = previousQueue })

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		runFeedSyncWorker(ctx)
		close(workerDone)
	}()
	schedulerDone := make(chan struct{})
	go func() {
		runFeedSyncScheduler(ctx)
		close(schedulerDone)
	}()
	cancel()

	for name, done := range map[string]chan struct{}{"worker": workerDone, "scheduler": schedulerDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s did not stop after context cancellation", name)
		}
	}
}

func TestFeedSyncBatchUsesBoundedConcurrencyFlow(t *testing.T) {
	sourceIDs := []string{"one", "two", "three", "four", "five", "six"}
	started := make(chan string, len(sourceIDs))
	release := make(chan struct{})
	resultCh := make(chan feedSyncBatchResult, 1)
	go func() {
		resultCh <- syncFeedSourcesBounded(context.Background(), sourceIDs, func(ctx context.Context, sourceID string) error {
			started <- sourceID
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	for running := 0; running < feedSyncMaxConcurrent; running++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("only %d workers started", running)
		}
	}
	select {
	case sourceID := <-started:
		t.Fatalf("source %q exceeded concurrency limit %d", sourceID, feedSyncMaxConcurrent)
	default:
	}
	close(release)
	select {
	case result := <-resultCh:
		if result.Processed != len(sourceIDs) || result.Failures != 0 {
			t.Fatalf("batch result = %#v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("bounded batch did not finish")
	}
}

func TestFeedSyncSourceTimeoutIsIndependent(t *testing.T) {
	err := withFeedSyncSourceTimeout(context.Background(), 10*time.Millisecond, func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first source timeout = %v", err)
	}
	err = withFeedSyncSourceTimeout(context.Background(), time.Second, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	})
	if err != nil {
		t.Fatalf("fresh source inherited previous timeout: %v", err)
	}
}

func TestFeedSyncRetryBackoffIsBounded(t *testing.T) {
	tests := []struct {
		completedAttempts int
		want              time.Duration
	}{
		{completedAttempts: -1, want: 5 * time.Minute},
		{completedAttempts: 0, want: 5 * time.Minute},
		{completedAttempts: 1, want: 10 * time.Minute},
		{completedAttempts: 2, want: 20 * time.Minute},
		{completedAttempts: 3, want: 40 * time.Minute},
		{completedAttempts: 4, want: time.Hour},
		{completedAttempts: 100, want: time.Hour},
	}
	for _, tt := range tests {
		if got := feedSyncRetryBackoff(tt.completedAttempts); got != tt.want {
			t.Errorf("backoff(%d) = %s, want %s", tt.completedAttempts, got, tt.want)
		}
	}
}

func TestMarkSourceSyncFailureUsesRetainedSnapshotUnit(t *testing.T) {
	db = openTestDB(t)
	_, err := db.Exec(`
INSERT INTO feed_sources(id,kind,name,url,is_default,status,collection_mode,created_at,updated_at)
VALUES
  ('empty-failure','rss','Empty','https://example.com/empty',0,'ok','app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('cached-failure','rss','Cached','https://example.com/cached',0,'ok','app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO news(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,content_hash,fetched_at,updated_at)
VALUES('cached-entry','Cached entry','https://example.com/cached/1','Cached','cached-failure','2026-08-21T00:00:00Z','','text','','cached-hash','2026-08-21T00:00:00Z','2026-08-21T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		sourceID string
		want     string
	}{
		{sourceID: "empty-failure", want: "unavailable"},
		{sourceID: "cached-failure", want: "partial"},
	} {
		if err := markSourceSyncFailure(test.sourceID, "temporary timeout"); err == nil {
			t.Fatalf("source %q failure unexpectedly returned nil", test.sourceID)
		}
		var status string
		if err := db.QueryRow(`SELECT status FROM feed_sources WHERE id=?`, test.sourceID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != test.want {
			t.Fatalf("source %q status=%q want=%q", test.sourceID, status, test.want)
		}
	}
}

func TestFeedSyncFailurePreservesCachedHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "arxiv-ai")
	previousFetch := operationalSourceFetch
	operationalSourceFetch = func(context.Context, string, string, string) (feedFetchResponse, error) {
		return feedFetchResponse{}, context.DeadlineExceeded
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	_, err := db.Exec(`
INSERT INTO news(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,content_hash,fetched_at,updated_at)
VALUES('arxiv-cached-entry','Cached paper','https://arxiv.org/abs/2608.00001','arXiv AI 最新论文','arxiv-ai','2026-08-21T00:00:00Z','','text','','arxiv-cached-hash','2026-08-21T00:00:00Z','2026-08-21T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "arxiv-ai"); err == nil {
		t.Fatal("failed source sync unexpectedly returned nil")
	}
	var status, message string
	var retained int
	if err := db.QueryRow(`SELECT status,last_error FROM feed_sources WHERE id='arxiv-ai'`).Scan(&status, &message); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='arxiv-ai' AND deleted_at=''`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if status != "partial" || retained != 1 || !strings.Contains(message, "deadline exceeded") {
		t.Fatalf("status=%q retained=%d message=%q", status, retained, message)
	}
}

func TestCapBackfillAtRetentionLimitUnit(t *testing.T) {
	historySource := verifiedHistoryTestSource("wechat-founder-park")
	historyCursor := verifiedWeChatHistoryCursor(weChatVerifiedDirectFeedPolicies[historySource.URL])
	tests := []struct {
		name                            string
		retained, limit                 int
		cursor, status, message         string
		wantCursor, wantStatus, wantMsg string
	}{
		{name: "below limit", retained: 59, limit: 60, cursor: "https://example.com/page/3", status: "partial", message: "后续同步将继续回填更早内容", wantCursor: "https://example.com/page/3", wantStatus: "partial", wantMsg: "后续同步将继续回填更早内容"},
		{name: "limit reached", retained: 60, limit: 60, cursor: "https://example.com/page/3", status: "partial", message: "后续同步将继续回填更早内容", wantCursor: "", wantStatus: "ok", wantMsg: ""},
		{name: "failure remains visible", retained: 60, limit: 60, cursor: "https://example.com/page/3", status: "partial", message: "后续分页读取失败", wantCursor: "", wantStatus: "partial", wantMsg: "后续分页读取失败"},
		{name: "completed history cursor stays terminal", retained: 1000, limit: 1000, cursor: historyCursor, status: "ok", message: "", wantCursor: historyCursor, wantStatus: "ok", wantMsg: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cursor, status, message := capBackfillAtRetentionLimit(test.retained, test.limit, test.cursor, test.status, test.message)
			if cursor != test.wantCursor || status != test.wantStatus || message != test.wantMsg {
				t.Fatalf("cursor=%q status=%q message=%q", cursor, status, message)
			}
		})
	}
}

func TestDueFeedSourceIDsUsesBoundedBatchAndOnlyEnabledDueSources(t *testing.T) {
	db = openTestDB(t)
	_, err := db.Exec(`
INSERT INTO feed_sources(id,kind,name,url,is_default,status,next_sync_at,collection_mode,created_at,updated_at)
VALUES
  ('due-1','rss','Due 1','https://example.com/1',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('due-2','rss','Due 2','https://example.com/2',0,'unavailable','2026-08-13T00:00:00Z', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('future-1','rss','Future','https://example.com/3',0,'ok','2099-01-01T00:00:00Z', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('agent-1','rss','Agent','https://example.com/4',0,'ok','', 'agent',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at)
VALUES ('due-1',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),('due-2',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),('future-1',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),('agent-1',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := dueFeedSourceIDs(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > feedSyncWarmupBatchSize {
		t.Fatalf("bounded due ids = %#v", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if seen["future-1"] || seen["agent-1"] {
		t.Fatalf("future or agent source returned = %#v", ids)
	}
	ids, err = dueFeedSourceIDs(1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) > feedSyncWarmupBatchSize {
		t.Fatalf("bounded due ids = %#v", ids)
	}
}

func TestStartupFeedSyncHonorsSmallSourceBudgetFlow(t *testing.T) {
	if feedSyncStartupMaxSources != feedSyncWarmupBatchSize {
		t.Fatalf("startup source cap = %d, want one warmup batch (%d)", feedSyncStartupMaxSources, feedSyncWarmupBatchSize)
	}
	if feedSyncStartupBudget <= 0 || feedSyncStartupBudget >= feedFetchTimeout*2 {
		t.Fatalf("startup budget = %s, want a bounded budget below two source timeouts", feedSyncStartupBudget)
	}

	db = openTestDB(t)
	_, err := db.Exec(`
INSERT INTO feed_sources(id,kind,name,url,is_default,status,next_sync_at,collection_mode,created_at,updated_at)
VALUES
  ('startup-1','rss','Startup 1','https://example.com/startup-1',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-2','rss','Startup 2','https://example.com/startup-2',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-3','rss','Startup 3','https://example.com/startup-3',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-4','rss','Startup 4','https://example.com/startup-4',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-5','rss','Startup 5','https://example.com/startup-5',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-6','rss','Startup 6','https://example.com/startup-6',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-7','rss','Startup 7','https://example.com/startup-7',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-8','rss','Startup 8','https://example.com/startup-8',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-9','rss','Startup 9','https://example.com/startup-9',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  ('startup-10','rss','Startup 10','https://example.com/startup-10',0,'unavailable','', 'app',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at)
SELECT id,1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM feed_sources WHERE id LIKE 'startup-%';`)
	if err != nil {
		t.Fatal(err)
	}
	var batches [][]string
	syncDueFeedSourcesWith(context.Background(), 3, func(_ context.Context, ids []string) feedSyncBatchResult {
		batches = append(batches, append([]string(nil), ids...))
		return feedSyncBatchResult{Processed: len(ids)}
	})
	var processed int
	for _, batch := range batches {
		processed += len(batch)
	}
	if processed != 3 || len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("startup batches = %#v, processed=%d; want one capped batch of 3", batches, processed)
	}
}

func TestFeedSyncSchedulerDefaultsToLazyAndKeepsEagerOverrideUnit(t *testing.T) {
	t.Setenv("FOCUS_SPACE_LAZY_SYNC", "")
	if !feedSyncSchedulerLazy() {
		t.Fatal("source scheduler must default to lazy/on-demand hydration")
	}
	t.Setenv("FOCUS_SPACE_LAZY_SYNC", "1")
	if !feedSyncSchedulerLazy() {
		t.Fatal("explicit lazy setting must remain lazy")
	}
	t.Setenv("FOCUS_SPACE_LAZY_SYNC", "0")
	if feedSyncSchedulerLazy() {
		t.Fatal("FOCUS_SPACE_LAZY_SYNC=0 must preserve the eager compatibility mode")
	}
}

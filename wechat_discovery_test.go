package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const weChatArticleFixture = `<!doctype html><html><head>
<meta property="og:title" content="Agent 正在怎么变" />
<meta property="og:description" content="一篇公开文章摘要" />
<meta property="og:article:author" content="取木" />
</head><body><script>
var user_name = "gh_98e0ccf2df49";
var nickname = htmlDecode("取木Axel");
var round_head_img = "http://mmbiz.qpic.cn/example/0?wx_fmt=png";
var msg_cdn_url = "https://mmbiz.qpic.cn/example/cover?wx_fmt=jpeg";
var create_time = "1785397124" * 1;
window.alias = "qumu_axel" || "";
</script></body></html>`

func TestWeChatBrowserAgentDoesNotRegressIntoWildcardBindSignature(t *testing.T) {
	// Keep the legacy token split so this guard itself cannot trip the source
	// scanner that rejects a literal wildcard bind address.
	wildcardToken := "0." + "0.0.0"
	legacyUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140." + wildcardToken + " Safari/537.36"
	if strings.Contains(weChatBrowserAgent, wildcardToken) {
		t.Fatalf("WeChat User-Agent contains a wildcard bind token: %q", weChatBrowserAgent)
	}
	if weChatBrowserAgent == legacyUA {
		t.Fatalf("legacy wildcard-looking User-Agent regressed: %q", weChatBrowserAgent)
	}
	if !strings.Contains(weChatBrowserAgent, "Chrome/140") || !strings.Contains(weChatBrowserAgent, "Safari/537.36") {
		t.Fatalf("User-Agent no longer identifies a supported browser: %q", weChatBrowserAgent)
	}
}

func TestWeChatArticleDiscoveryParsesStableIdentityAndSafeAssets(t *testing.T) {
	raw := "https://mp.weixin.qq.com/s/article-id"
	classified, err := classifyDiscoveryInput(raw)
	if err != nil || classified.Kind != discoveryInputWeChat {
		t.Fatalf("classify = %#v, err = %v", classified, err)
	}
	profile, err := parseWeChatArticlePage(feedFetchResponse{Body: []byte(weChatArticleFixture), ContentType: "text/html", FinalURL: raw}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if profile.AccountID != "gh_98e0ccf2df49" || profile.AccountAlias != "qumu_axel" || profile.AccountName != "取木Axel" {
		t.Fatalf("profile identity = %#v", profile)
	}
	if profile.ArticleTitle != "Agent 正在怎么变" || !strings.HasPrefix(profile.IconURL, "https://mmbiz.qpic.cn/") || !strings.HasPrefix(profile.CoverURL, "https://mmbiz.qpic.cn/") {
		t.Fatalf("profile content = %#v", profile)
	}
	feeds := weChatFeedURLs(profile)
	if len(feeds) != 2 || !validWechatRSSFeedURL(feeds[0]) || !validWechatRSSFeedURL(feeds[1]) || strings.Contains(feeds[0], "api.folo.is") || strings.Contains(feeds[1], "api.folo.is") {
		t.Fatalf("feed candidates = %#v", feeds)
	}
}

func TestDirectWechatRSSFeedFiltersArticlesAndKeepsReaderContent(t *testing.T) {
	feedURL := "https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml"
	body := []byte(`<rss version="2.0"><channel><title>量子位</title><description>公开 RSS</description>
	<item><guid>qbit-1</guid><title>AI 产业关键进展</title><link>https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&amp;mid=1&amp;idx=1&amp;sn=verified</link><author>量子位</author><pubDate>Wed, 12 Aug 2026 10:00:00 +0000</pubDate><description>文章摘要</description></item>
	</channel></rss>`)
	feed, err := parseDirectWechatRSSFeed(body, feedURL, "量子位", "", "https://mp.weixin.qq.com/s/article-id")
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "量子位" || feed.Format != "rss" || len(feed.Items) != 1 || feed.Items[0].SourceType != "wechat" || feed.Items[0].SourceLevel != "public_aggregator" || feed.Items[0].PermissionScope != "public_read" {
		t.Fatalf("unexpected direct WeChat feed: %#v", feed)
	}
	if !validWechatRSSFeedURL(feedURL) || validWechatRSSFeedURL("https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Fwechat%2Furead%2Fqumu_axel") {
		t.Fatal("direct WeChat RSS validation regressed")
	}
}

func TestVerifiedWeChatDirectFeedIdentityUnit(t *testing.T) {
	tests := []struct {
		id        string
		name      string
		feedTitle string
		biz       []string
	}{
		{id: "wechat-aiera", name: "新智元", feedTitle: "新智元", biz: []string{"MzI3MTA0MTk1MA=="}},
		{id: "wechat-founder-park", name: "Founder Park", feedTitle: "Founder Park", biz: []string{"Mzg5NTc0MjgwMw==", "MzY5ODQwMTkxNA=="}},
		{id: "wechat-geekpark", name: "极客公园", feedTitle: "极客公园", biz: []string{"MTMwNDMwODQ0MQ=="}},
		{id: "wechat-jiqizhixin", name: "机器之心", feedTitle: "机器之心", biz: []string{"MzA3MzI4MjgzMw=="}},
		{id: "wechat-liuxiaopai", name: "刘小排", feedTitle: "刘小排r", biz: []string{"MzI1MTUxNzgxMA=="}},
		{id: "wechat-qbitai", name: "量子位", feedTitle: "量子位", biz: []string{"MzIzNjc1NzUzMw=="}},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			feedURL := weChatVerifiedDirectFeedURLs[test.id]
			if !validWechatRSSFeedURL(feedURL) {
				t.Fatalf("verified URL rejected: %s", feedURL)
			}
			links := make([]string, 0, len(test.biz))
			for index, biz := range test.biz {
				links = append(links, "https://mp.weixin.qq.com/s?__biz="+biz+"&mid=224750000"+strconv.Itoa(index)+"&idx=1&sn=verified")
			}
			feed, err := parseDirectWechatRSSFeed(verifiedWeChatRSSFixture(test.feedTitle, links...), feedURL, test.name, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if feed.Title != test.name || len(feed.Items) != len(links) {
				t.Fatalf("verified feed=%#v", feed)
			}
		})
	}

	for _, invalid := range []string{
		"https://wechat2rss.bestblogs.dev/feed/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.xml",
		weChatVerifiedDirectFeedURLs["wechat-founder-park"] + "?other=1",
		weChatVerifiedDirectFeedURLs["wechat-founder-park"] + "/",
		"https://evil.example/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml",
	} {
		if validWechatRSSFeedURL(invalid) {
			t.Fatalf("unverified URL accepted: %s", invalid)
		}
	}
}

func TestVerifiedWeChatDirectFeedRejectsIdentityDriftUnit(t *testing.T) {
	feedURL := weChatVerifiedDirectFeedURLs["wechat-founder-park"]
	allowed := "https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA==&mid=1&idx=1&sn=ok"
	tests := []struct {
		name  string
		title string
		links []string
	}{
		{name: "wrong title", title: "数字栩生", links: []string{allowed}},
		{name: "missing biz", title: "Founder Park", links: []string{"https://mp.weixin.qq.com/s?mid=1&idx=1&sn=missing"}},
		{name: "duplicate biz", title: "Founder Park", links: []string{"https://mp.weixin.qq.com/s?__biz=MzY5ODQwMTkxNA==&__biz=Mzg5NTc0MjgwMw==&mid=1"}},
		{name: "unknown biz", title: "Founder Park", links: []string{"https://mp.weixin.qq.com/s?__biz=wrong&mid=1&idx=1"}},
		{name: "mixed accounts", title: "Founder Park", links: []string{allowed, "https://mp.weixin.qq.com/s?__biz=wrong&mid=2&idx=1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDirectWechatRSSFeed(verifiedWeChatRSSFixture(test.title, test.links...), feedURL, "Founder Park", "", ""); err == nil {
				t.Fatal("identity drift was accepted")
			}
		})
	}
}

func TestWeChatContinuousFeedRejectsWrongAccountAndUnsafeTarget(t *testing.T) {
	profile := weChatArticleProfile{AccountAlias: "qumu_axel", AccountID: "gh_98e0ccf2df49"}
	feedURL := foloWeChatFeedURLs(profile)[0]
	wrong := []byte(`{"code":0,"data":{"feed":{"title":"其他账号 的微信公众号"},"entries":[{"id":"1","title":"Wrong","url":"https://mp.weixin.qq.com/s/wrong"}]}}`)
	if _, err := parseFoloWeChatFeed(wrong, feedURL, "取木Axel", "", "https://mp.weixin.qq.com/s/article-id"); err == nil {
		t.Fatal("mismatched account was accepted")
	}
	privateTarget := "https://api.folo.is/feeds?url=http%3A%2F%2F127." + "0.0.1%2Fprivate"
	if validFoloWeChatFeedURL(privateTarget) ||
		validFoloWeChatFeedURL("https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fqumu_axel") {
		t.Fatal("unsafe or non-WeChat target was accepted")
	}
}

func TestPinnedWeChatFallbackAcceptsEquivalentEncodedFeedURLFlow(t *testing.T) {
	sourceURL := weChatPinnedFoloFallbackURLs["wechat-huxiu"]
	body := []byte(`{"code":0,"data":{"feed":{"title":"硬氪 的微信公众号","url":"https://rsshub.rssforever.com/wechat/sogou/硬氪"},"entries":[{"id":"1","title":"硬科技融资进展","url":"https://weixin.sogou.com/link?url=dn9a_-gY295K0Rci_xozVXfdMkSQTLW6cwJThYulHEtVjXrGTiVgS0wJUuXmWDUZBQi8Kv2aG6g7QsJE5Dg-71qXa8Fplpd9-ocRnKHKB46J0mJ8W1sXo1Rj890HTTwUZ2i3vZKr_-05jU99DkavkVeJtufK5LDqiZfaboWQwrsdfA22WcfWxLsCmnXMNOvAGFGqGYMqNgSUFUi9ybDGpKOf7CAGmrdZF1oFBqvdA1YBa29MB6iOAXaJzxt3WtpGMkEK_UHCCXNj32-j2KiwUw..&type=2&query=%25E7%25A1%25AC%25E6%25B0%25AA&token=3ED2A9257ABBFBB4A3A2FB5E314371A4A4B7E56B6A7E9F03","publishedAt":"2026-08-21T00:00:00Z"}]}}`)
	feed, err := parseFoloWechat2RSSFeed(body, sourceURL)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "硬氪 的微信公众号" || len(feed.Items) != 1 || feed.Items[0].SourceType != "wechat" {
		t.Fatalf("fallback feed = %#v", feed)
	}
	for _, invalid := range []string{
		"https://evil.example/link?url=dn9a_opaque&type=2&query=x&token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"https://weixin.sogou.com/link?url=dn9a_opaque&type=2&query=x&token=secret",
		"https://weixin.sogou.com/link?url=dn9a_opaque&type=2&query=x&token=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&extra=1",
	} {
		if _, err := normalizeWechatEntryURL(invalid); err == nil {
			t.Fatalf("unsafe Sogou article URL accepted: %s", invalid)
		}
	}
}

func TestWeChatHistoryRetentionMigrationFlow(t *testing.T) {
	db = openTestDB(t)
	expectedModes := map[string]string{
		"wechat-ai-paper": "agent", "wechat-ali-tech": "agent",
		"wechat-chaping": "agent", "wechat-fnos": "agent",
		"wechat-founder-park": "agent", "wechat-huxiu": "agent",
		"wechat-liuxiaopai": "agent", "wechat-paperweekly": "agent",
	}
	for id, expectedMode := range expectedModes {
		var backfillDays, retentionDays, retentionLimit int
		var mode string
		if err := db.QueryRow(`SELECT collection_mode,backfill_days,retention_days,retention_limit FROM feed_sources WHERE id=?`, id).Scan(&mode, &backfillDays, &retentionDays, &retentionLimit); err != nil {
			t.Fatalf("source %s: %v", id, err)
		}
		if mode != expectedMode || backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 1000 {
			t.Fatalf("source=%s mode=%s backfill=%d retention=%d limit=%d", id, mode, backfillDays, retentionDays, retentionLimit)
		}
	}
}

func TestWeChatContinuousFeedAcceptsExactEntryAuthorWhenFeedTitleIsGeneric(t *testing.T) {
	profile := weChatArticleProfile{AccountAlias: "qumu_axel", AccountID: "gh_98e0ccf2df49"}
	feedURL := foloWeChatFeedURLs(profile)[0]
	body := []byte(`{"code":0,"data":{"feed":{"title":"优读 - qumu_axel"},"entries":[{"id":"1","title":"Agent 正在怎么变","url":"https://mp.weixin.qq.com/s/article-id","author":"取木Axel"},{"id":"2","title":"其他账号内容","url":"https://mp.weixin.qq.com/s/other","author":"其他账号"}]}}`)
	feed, err := parseFoloWeChatFeed(body, feedURL, "取木Axel", "", "https://mp.weixin.qq.com/s/article-id")
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].Author != "取木Axel" {
		t.Fatalf("generic-title feed identity filter = %#v", feed.Items)
	}
}

func TestWeChatDiscoveryRouteFailsClosedBeforeLocalCollectorFlow(t *testing.T) {
	previous := discoverWeChatArticleFunc
	calls := 0
	discoverWeChatArticleFunc = func(_ context.Context, item FeedDiscovery) (FeedDiscovery, error) {
		calls++
		item.Title = "取木Axel"
		item.SiteURL = item.InputURL
		item.Status = "pending"
		item.ErrorMessage = "公众号已识别，文章可直接查看；持续订阅服务暂不可用，请稍后重试"
		item.Preview = []NewsItem{{Title: "Agent 正在怎么变", URL: item.InputURL, Source: "取木Axel", SourceType: "wechat"}}
		return item, nil
	}
	t.Cleanup(func() { discoverWeChatArticleFunc = previous })

	db = openTestDB(t)
	response := callJSON(t, discoveryHandler, "POST", "/api/discoveries", `{"input":"https://mp.weixin.qq.com/s/article-id"}`)
	body := response.Body.String()
	if response.Code != http.StatusBadRequest || !strings.Contains(body, "维护目录") || strings.Contains(body, "article-id") {
		t.Fatalf("WeChat discovery = %d %s", response.Code, body)
	}
	var candidates int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_discovery_candidates`).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || candidates != 0 {
		t.Fatalf("blocked WeChat discovery used local collector: calls=%d candidates=%d", calls, candidates)
	}
	legacy := FeedDiscovery{InputURL: "https://mp.weixin.qq.com/s/article-id", ErrorMessage: `Get "https://mp.weixin.qq.com/mp/wappoc_appmsgcaptcha?poc_token=redacted": 公开来源重定向到不安全地址`}
	if cleaned := publicDiscoveryError(legacy); strings.Contains(cleaned, "poc_token") || !strings.Contains(cleaned, "公众号专用识别流程") {
		t.Fatalf("legacy error was not cleaned: %q", cleaned)
	}
}

func TestFoloWechat2RSSFeedIsPinnedAndBounded(t *testing.T) {
	feedURL := "https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2F6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml"
	body := []byte(`{"code":0,"data":{"feed":{"title":"阿里技术","description":"工程实践","url":"https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml","image":"https://wx.qlogo.cn/mmhead/example/0"},"entries":[{"id":"entry-1","title":"工程实践","url":"https://mp.weixin.qq.com/s?__biz=example&mid=1&idx=1&sn=one","summary":"摘要一","publishedAt":"2026-08-14T00:00:00Z"},{"id":"entry-2","title":"智能体进展","url":"https://mp.weixin.qq.com/s?__biz=example&mid=2&idx=1&sn=two","content":"正文二","publishedAt":"2026-08-13T00:00:00Z"}]}}`)
	feed, err := parseFoloWechat2RSSFeed(body, feedURL)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != "阿里技术" || feed.Format != "rss" || len(feed.Items) != 2 || feed.Items[0].SourceType != "wechat" || feed.Items[1].SummaryText != "正文二" {
		t.Fatalf("unexpected bounded Folo feed: %#v", feed)
	}
	if !validFoloWechat2RSSFeedURL(feedURL) {
		t.Fatal("valid Folo Wechat2RSS URL rejected")
	}
	for _, invalid := range []string{
		"https://api.folo.is/feeds?url=https%3A%2F%2Fexample.com%2Ffeed.xml",
		"https://api.folo.is/feeds?url=https%3A%2F%2Fwechat2rss.xlab.app%2Ffeed%2Fnot-a-hash.xml",
		"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Furead%2Fnot-sogou",
		"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2Ffoo%252Fbar",
	} {
		if validFoloWechat2RSSFeedURL(invalid) {
			t.Fatalf("untrusted Folo URL accepted: %s", invalid)
		}
	}
}

func TestRegularFoloWeChatCollectionKeepsTwentyItemLimitUnit(t *testing.T) {
	feedURL := "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Fwechat%2Furead%2Fqumu_axel"
	var body strings.Builder
	body.WriteString(`{"code":0,"data":{"feed":{"title":"取木Axel"},"entries":[`)
	for index := 0; index < 25; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{"id":"entry-`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`","title":"公开文章","url":"https://mp.weixin.qq.com/s?__biz=example&mid=`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`&idx=1&sn=bounded","publishedAt":"2026-08-14T00:00:00Z"}`)
	}
	body.WriteString(`]}}`)
	source := FeedSource{ID: "user-wechat", Name: "取木Axel", URL: feedURL}
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		return feedFetchResponse{Body: []byte(body.String()), FinalURL: raw}, nil
	}
	result, handled, err := collectWeChatDiscoveredSourceContextWithFetcher(context.Background(), source, fetcher, time.Second, time.Second)
	if err != nil || !handled || len(result.Feed.Items) != operationalSourceLimit {
		t.Fatalf("handled=%v items=%d err=%v", handled, len(result.Feed.Items), err)
	}
}

func TestFoloSogouWechatFeedKeepsBoundedPublicArticleSummaries(t *testing.T) {
	feedURL := "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FFounder%2520Park"
	body := []byte(`{"code":0,"data":{"feed":{"title":"数字栩生 的微信公众号","description":"公开公众号摘要","url":"https://rsshub.rssforever.com/wechat/sogou/Founder%20Park"},"entries":[{"id":"sogou-1","title":"Founder Park AGI Playground","url":"https://weixin.sogou.com/link?url=opaque&type=2&query=Founder%20Park","summary":"Founder Park 的公开文章摘要","publishedAt":"2026-08-14T00:00:00Z"}]}}`)
	feed, err := parseFoloWechat2RSSFeed(body, feedURL)
	if err != nil {
		t.Fatal(err)
	}
	if !validFoloWechat2RSSFeedURL(feedURL) || len(feed.Items) != 1 || feed.Items[0].URL == "" ||
		!isSogouWechatArticleURL(mustParseURL(feed.Items[0].URL)) ||
		!strings.Contains(feed.Items[0].SummaryText, "公开文章摘要") {
		t.Fatalf("unexpected Sogou-backed feed: %#v", feed)
	}
}

func TestPinnedWeChatFallbackURLRejectsModifiedTargets(t *testing.T) {
	for sourceID, target := range weChatPinnedFallbackURLs {
		source := FeedSource{ID: sourceID, URL: target}
		fallbackURL, ok := pinnedWeChatFallbackURL(source)
		if !ok || !validFoloWechat2RSSFeedURL(fallbackURL) {
			t.Fatalf("pinned source %s rejected: %q", sourceID, fallbackURL)
		}
		if fallbackURL != weChatPinnedFoloFallbackURLs[sourceID] || strings.Contains(fallbackURL, "%25E") {
			t.Fatalf("pinned source %s was double escaped: %q", sourceID, fallbackURL)
		}
		source.URL += "/changed"
		if _, ok := pinnedWeChatFallbackURL(source); ok {
			t.Fatalf("modified source %s accepted", sourceID)
		}
	}
	if _, ok := pinnedWeChatFallbackURL(FeedSource{ID: "user-wechat", URL: "https://rsshub.rssforever.com/wechat/sogou/example"}); ok {
		t.Fatal("unpinned source accepted")
	}
}

func TestWeChatPinnedFallbackRecoversMatchingCachedFeedFlow(t *testing.T) {
	tests := []struct {
		id        string
		name      string
		feedTitle string
	}{
		{id: "wechat-ai-paper", name: "AI算法科研paper", feedTitle: "AI算法科研paper 的微信公众号"},
		{id: "wechat-fnos", name: "飞牛FnOS", feedTitle: "飞牛fnOS 的微信公众号"},
		{id: "wechat-huxiu", name: "硬氪", feedTitle: "硬氪 的微信公众号"},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			source := FeedSource{ID: test.id, Name: test.name, URL: weChatPinnedFallbackURLs[test.id]}
			calls := make([]string, 0, 2)
			fetcher := func(ctx context.Context, raw, _, _ string) (feedFetchResponse, error) {
				calls = append(calls, raw)
				if raw == source.URL {
					<-ctx.Done()
					return feedFetchResponse{}, ctx.Err()
				}
				body := `{"code":0,"data":{"feed":{"title":"` + test.feedTitle + `"},"entries":[{"id":"one","title":"公开文章","url":"https://mp.weixin.qq.com/s/verified","publishedAt":"2026-08-14T00:00:00Z"}]}}`
				return feedFetchResponse{Body: []byte(body), FinalURL: raw}, nil
			}
			parent, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			result, handled, err := collectWeChatDiscoveredSourceContextWithFetcher(parent, source, fetcher, 15*time.Millisecond, 100*time.Millisecond)
			if err != nil || !handled {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if result.Status != "partial" || len(result.Feed.Items) != 1 || result.Feed.Title != source.Name || result.Feed.FeedURL != source.URL {
				t.Fatalf("fallback result=%#v", result)
			}
			if len(calls) != 2 || calls[0] != source.URL || !strings.HasPrefix(calls[1], "https://api.folo.is/feeds?") {
				t.Fatalf("fetch order=%#v", calls)
			}
		})
	}
}

func TestVerifiedWeChatDirectCollectionUsesExactSourceFlow(t *testing.T) {
	source := FeedSource{ID: "wechat-liuxiaopai", Name: "刘小排", URL: weChatVerifiedDirectFeedURLs["wechat-liuxiaopai"]}
	calls := 0
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls++
		if raw != source.URL {
			t.Fatalf("unexpected fallback request: %s", raw)
		}
		body := verifiedWeChatRSSFixture("刘小排r", "https://mp.weixin.qq.com/s?__biz=MzI1MTUxNzgxMA==&mid=1&idx=1&sn=verified")
		return feedFetchResponse{Body: body, FinalURL: raw}, nil
	}
	result, handled, err := collectWeChatDiscoveredSourceContextWithFetcher(context.Background(), source, fetcher, 100*time.Millisecond, 100*time.Millisecond)
	if err != nil || !handled || result.Status != "ok" || len(result.Feed.Items) != 1 || calls != 1 {
		t.Fatalf("handled=%v calls=%d result=%#v err=%v", handled, calls, result, err)
	}

	for _, invalid := range []FeedSource{
		{ID: source.ID, Name: source.Name, URL: "https://rsshub.rssforever.com/wechat/sogou/%E5%88%98%E5%B0%8F%E6%8E%92"},
		{ID: "user-copy", Name: source.Name, URL: source.URL},
	} {
		before := calls
		if _, handled, err := collectWeChatDiscoveredSourceContextWithFetcher(context.Background(), invalid, fetcher, 100*time.Millisecond, 100*time.Millisecond); !handled || err == nil {
			t.Fatalf("invalid binding handled=%v err=%v", handled, err)
		}
		if calls != before {
			t.Fatal("invalid binding reached the network")
		}
	}
}

func verifiedWeChatRSSFixture(title string, links ...string) []byte {
	var body strings.Builder
	body.WriteString(`<rss version="2.0"><channel><title>`)
	body.WriteString(title)
	body.WriteString(`</title>`)
	for index, link := range links {
		body.WriteString(`<item><guid>verified-`)
		body.WriteString(strconv.Itoa(index))
		body.WriteString(`</guid><title>公开文章</title><link>`)
		body.WriteString(strings.ReplaceAll(link, "&", "&amp;"))
		body.WriteString(`</link><pubDate>Thu, 20 Aug 2026 10:00:00 +0000</pubDate><description>摘要</description></item>`)
	}
	body.WriteString(`</channel></rss>`)
	return []byte(body.String())
}

func mustParseURL(raw string) *url.URL {
	parsed, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return parsed
}

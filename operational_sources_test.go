package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func operationalFixtureResponse(raw, body string) feedFetchResponse {
	return feedFetchResponse{Body: []byte(body), ContentType: "application/rss+xml", FinalURL: raw}
}

func operationalFoloFixture(handle string) feedFetchResponse {
	body := `{"code":0,"data":{"feed":{"siteUrl":"https://x.com/` + handle + `","image":"https://pbs.twimg.com/profile_images/fixture/avatar.jpg"},"entries":[{"id":"fixture-` + handle + `","title":"Public update","url":"https://x.com/` + handle + `/status/1234567890","summary":"A bounded public fixture","author":"` + handle + `","authorAvatar":"https://pbs.twimg.com/profile_images/fixture/avatar_normal.jpg","publishedAt":"2026-08-11T05:20:07Z"}]}}`
	return feedFetchResponse{Body: []byte(body), ContentType: "application/json"}
}

func youtubeHistoryRSSFixture(spec operationalSourceSpec, count int) string {
	var body strings.Builder
	fmt.Fprintf(&body, `<rss version="2.0"><channel><title>%s - YouTube</title><link>https://www.youtube.com/channel/%s</link><description>YouTube channel %s</description>`, spec.youtubeTitle, spec.youtubeChannelID, spec.youtubeTitle)
	for index := 0; index < count; index++ {
		videoID := fmt.Sprintf("hist%07d", index)
		published := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC).Add(-time.Duration(index) * 24 * time.Hour).Format(time.RFC3339)
		fmt.Fprintf(&body, `<item><title>History %d</title><link>https://www.youtube.com/watch?v=%s</link><guid>https://www.youtube.com/watch?v=%s</guid><published>%s</published><author>%s</author><enclosure url="https://i.ytimg.com/vi/%s/sddefault.jpg" type="image/jpeg"></enclosure></item>`, index, videoID, videoID, published, spec.youtubeTitle, videoID)
	}
	body.WriteString(`</channel></rss>`)
	return body.String()
}

func TestDynamicFoloXSourceOnlyAcceptsAllowlistedRoutes(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		handle   string
		endpoint string
		ok       bool
	}{
		{
			name:     "folo wrapper",
			raw:      "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fdotey",
			handle:   "dotey",
			endpoint: "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fdotey",
			ok:       true,
		},
		{
			name:     "direct rsshub route",
			raw:      "https://rsshub.app/twitter/user/dotey",
			handle:   "dotey",
			endpoint: "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fdotey",
			ok:       true,
		},
		{name: "non twitter route", raw: "https://rsshub.app/youtube/channel/abc", ok: false},
		{name: "untrusted host", raw: "https://evil.example/twitter/user/dotey", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle, endpoint, ok := dynamicFoloXSource(tt.raw)
			if ok != tt.ok || handle != tt.handle || endpoint != tt.endpoint {
				t.Fatalf("handle=%q endpoint=%q ok=%v", handle, endpoint, ok)
			}
		})
	}
}

func TestDynamicFoloXSourceUsesPinnedParserForUserSubscription(t *testing.T) {
	previous := operationalSourceFetch
	operationalSourceFetch = operationalFixtureFetcher
	t.Cleanup(func() { operationalSourceFetch = previous })
	source := FeedSource{
		ID:   "twitter-dotey-user-source",
		Name: "Twitter @宝玉",
		URL:  "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Felonmusk",
	}
	result, handled, err := collectOperationalSourceContext(context.Background(), source)
	if err != nil || !handled || result.Status == "unavailable" || len(result.Feed.Items) != 1 {
		t.Fatalf("handled=%v err=%v result=%#v", handled, err, result)
	}
	if result.Feed.Items[0].URL != "https://x.com/elonmusk/status/1234567890" {
		t.Fatalf("unexpected dynamic X item: %#v", result.Feed.Items[0])
	}
}

func operationalFixtureFetcher(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
	switch {
	case strings.Contains(raw, "hn.algolia.com/api/v1/search_by_date"):
		return feedFetchResponse{Body: []byte(`{"hits":[{"objectID":"49260751","title":"A 10M-token context efficient agentic model","url":"https://example.com/agentic-model","author":"builder","created_at":"2026-08-11T16:31:33Z","story_text":"AI model research"}]}`), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "xbrl-inline.rss.xml"):
		return feedFetchResponse{}, errors.New("来源返回 HTTP 403")
	case strings.Contains(raw, "efts.sec.gov"):
		return feedFetchResponse{Body: []byte(`{"hits":{"hits":[{"_id":"0000123456-26-000001","_source":{"adsh":"0000123456-26-000001","form":"8-K","display_names":["Example Corp"],"ciks":["0000123456"],"file_date":"2026-08-10","file_description":"Current report"}}]}}`), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "hkex.com.hk"):
		return operationalFixtureResponse(raw, `<rss><channel><title>HKEX</title><item><guid>hkex-1</guid><title>监管公告</title><link>https://www.hkex.com.hk/announcement/1</link></item></channel></rss>`), nil
	case strings.Contains(raw, "cninfo.com.cn"):
		return feedFetchResponse{Body: []byte(`<html><head><title>巨潮公告</title></head><body><a href="/new/disclosure/detail?plate=sz&id=1">巨潮披露公告一</a></body></html>`), ContentType: "text/html", FinalURL: raw}, nil
	case strings.Contains(raw, "query.sse.com.cn/security/stock/queryCompanyBulletin.do"):
		request, ok := parseSSEHistoryRequest(raw, 1)
		if !ok {
			return feedFetchResponse{}, errors.New("fixture SSE request 无效: " + raw)
		}
		return feedFetchResponse{Body: []byte(sseHistoryFixture(request, 1, sseHistoryPageSize)), ContentType: "application/javascript", FinalURL: raw}, nil
	case strings.Contains(raw, "federalregister.gov/api/v1/documents"):
		return feedFetchResponse{Body: []byte(federalRegisterHistoryFixture(1, 1)), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "govinfo.gov/rss"):
		return operationalFixtureResponse(raw, `<rss><channel><title>Federal Register</title><item><guid>fr-1</guid><title>Federal notice</title><link>https://www.govinfo.gov/content/pkg/FR-2026-1/html/1.htm</link></item></channel></rss>`), nil
	case strings.Contains(raw, "eur-lex.europa.eu"):
		return operationalFixtureResponse(raw, `<rss><channel><title>EUR-Lex</title><item><guid>eu-1</guid><title>EU legislative update</title><link>https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=1</link></item></channel></rss>`), nil
	case strings.Contains(raw, "ZUIXINZHENGCE.json"):
		return feedFetchResponse{Body: []byte(`{"items":[{"TITLE":"政策文件","URL":"https://www.gov.cn/zhengce/1.htm","DOCRELPUBTIME":"2026-08-10","SUB_TITLE":"最新发布"}]}`), ContentType: "application/json", FinalURL: raw}, nil
	case raw == "https://openai.com/news/rss.xml":
		return feedFetchResponse{Body: []byte(openAIHistoryRSSFixture(1)), ContentType: "application/rss+xml", FinalURL: raw}, nil
	case strings.Contains(raw, "export.arxiv.org"):
		return operationalFixtureResponse(raw, `<feed xmlns="http://www.w3.org/2005/Atom"><title>arXiv</title><entry><id>http://arxiv.org/abs/1</id><title>AI paper</title><link href="https://arxiv.org/abs/1"/><summary>Evidence</summary></entry></feed>`), nil
	case strings.Contains(raw, "api.crossref.org/works"):
		doi := "10.1000/" + "example"
		doiURL := "https://doi.org/" + doi
		body := `{"message":{"items":[{"DOI":"` + doi + `","title":["Research result"],"URL":"` + doiURL + `","published":{"date-parts":[[2026,8,10]]},"author":[{"family":"Li"}],"abstract":"Abstract"}]}}`
		return feedFetchResponse{Body: []byte(body), ContentType: "application/json", FinalURL: raw}, nil
	case strings.HasSuffix(raw, "/publication-dates"):
		return feedFetchResponse{Body: []byte(`publication-dates/20260810/patents`), ContentType: "text/plain", FinalURL: raw}, nil
	case strings.Contains(raw, "/publication-dates/20260810/patents"):
		return feedFetchResponse{Body: []byte(`/patents/EP1234567`), ContentType: "text/plain", FinalURL: raw}, nil
	case strings.Contains(raw, "cnipa.gov.cn"):
		return feedFetchResponse{Body: []byte(`<html><head><title>国家知识产权局</title></head><body><a href="/art/2026/1.html">专利动态</a></body></html>`), ContentType: "text/html", FinalURL: raw}, nil
	case strings.Contains(raw, "patentscope.wipo.int"):
		return feedFetchResponse{Body: []byte(`<html><head><title>PATENTSCOPE</title></head><body><a href="/search/en/search.jsf#top">Search</a></body></html>`), ContentType: "text/html", FinalURL: raw}, nil
	case strings.Contains(raw, "wipo.int/pressroom/en/rss.xml"):
		return operationalFixtureResponse(raw, `<rss><channel><title>WIPO News</title><item><guid>wipo-1</guid><title>Patent news</title><link>https://www.wipo.int/pressroom/en/article/1</link></item></channel></rss>`), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "elonmusk"):
		return feedFetchResponse{Body: []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/elonmusk","image":"https://pbs.twimg.com/profile_images/elon/avatar.jpg"},"entries":[{"id":"folo-elon-1","title":"Launch update","url":"https://x.com/elonmusk/status/1234567890","summary":"A public update","author":"Elon Musk","authorAvatar":"https://pbs.twimg.com/profile_images/elon/avatar_normal.jpg","publishedAt":"2026-08-11T05:20:07.527Z","media":[{"url":"https://pbs.twimg.com/media/launch.jpg?name=orig","type":"photo","width":2048,"height":2048}]}]}}`), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "realDonaldTrump"):
		return feedFetchResponse{Body: []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://twitter.com/realDonaldTrump","image":"https://pbs.twimg.com/profile_images/trump/avatar.jpg"},"entries":[{"id":"folo-trump-1","title":"Statement","url":"https://twitter.com/realDonaldTrump/status/9876543210","summary":"A public statement","author":"Donald J. Trump","publishedAt":"2026-08-11T05:20:07.527Z","media":[{"url":"https://video.twimg.com/ext_tw_video/statement.mp4","type":"video","width":1920,"height":1080,"preview_image_url":"https://pbs.twimg.com/ext_tw_video_thumb/statement.jpg"}]}]}}`), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "thsottiaux"):
		return feedFetchResponse{Body: []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/thsottiaux","image":"https://pbs.twimg.com/profile_images/tibo/avatar.jpg"},"entries":[{"id":"folo-tibo-1","title":"Codex update","url":"https://x.com/thsottiaux/status/2087423996115681767","summary":"A Codex product update","author":"Tibo","authorAvatar":"https://pbs.twimg.com/profile_images/tibo/avatar_normal.jpg","publishedAt":"2026-08-12T06:20:37.669Z"}]}}`), ContentType: "application/json", FinalURL: raw}, nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "sama"):
		return operationalFoloFixture("sama"), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "DarioAmodei"):
		return operationalFoloFixture("DarioAmodei"), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "demishassabis"):
		return operationalFoloFixture("demishassabis"), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "JensenHuang"):
		return operationalFoloFixture("JensenHuang"), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "satyanadella"):
		return operationalFoloFixture("satyanadella"), nil
	case strings.Contains(raw, "api.folo.is/feeds") && strings.Contains(raw, "karpathy"):
		return operationalFoloFixture("karpathy"), nil
	case strings.Contains(raw, "youtube.com/feeds/videos.xml") || strings.Contains(raw, "youtube.com%2Ffeeds%2Fvideos.xml"):
		for _, spec := range operationalSourceSpecs {
			if spec.parser != "youtube" || !strings.Contains(raw, spec.youtubeChannelID) {
				continue
			}
			body := `<feed xmlns="http://www.w3.org/2005/Atom"><title>` + spec.youtubeTitle + `</title><link rel="alternate" href="https://www.youtube.com/channel/` + spec.youtubeChannelID + `"/><entry><id>yt:video:dQw4w9WgXcQ</id><title>AI research update</title><link rel="alternate" href="https://www.youtube.com/watch?v=dQw4w9WgXcQ"/><published>2026-08-12T06:20:37.669Z</published><updated>2026-08-12T06:20:37.669Z</updated><summary>A public AI video</summary></entry></feed>`
			return feedFetchResponse{Body: []byte(body), ContentType: "application/atom+xml", FinalURL: raw}, nil
		}
		return feedFetchResponse{}, errors.New("fixture YouTube channel 未覆盖: " + raw)
	case strings.Contains(raw, "jintiankansha.me/column/"):
		page := "1"
		if parsed, parseErr := url.Parse(raw); parseErr == nil && parsed.Query().Get("page") != "" {
			page = parsed.Query().Get("page")
		}
		next := ""
		if page == "1" {
			next = `<a id="p_next" href="?page=2">下一页</a>`
		}
		body := `<html><head><title>硬氪</title></head><body><span class="item_title"><a href="http://www.jintiankansha.me/t/abc12345">硬氪历史文章 ` + page + `</a></span>` + next + `</body></html>`
		return feedFetchResponse{Body: []byte(body), ContentType: "text/html", FinalURL: raw}, nil
	case strings.Contains(raw, "api.bilibili.com/x/web-interface/search/type") || strings.Contains(raw, "api.bilibili.com/x/web-interface/search/all/v2"):
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			return feedFetchResponse{}, parseErr
		}
		keyword := parsed.Query().Get("keyword")
		for _, spec := range operationalSourceSpecs {
			if spec.parser != "bilibili" || spec.bilibiliKeyword != keyword {
				continue
			}
			body := fmt.Sprintf(`{"code":0,"data":{"page":1,"next":2,"numPages":2,"result":[{"bvid":"BV1abc123456","mid":%d,"author":%q,"title":"科技 AI 更新","description":"公开视频摘要","pubdate":1786400000,"duration":"12:34"},{"bvid":"BV2abc123456","mid":%d,"author":%q,"title":"第二条视频","description":"第二条摘要","pubdate":1786300000,"duration":"1:02"},{"bvid":"BV3abc123456","mid":999,"title":"错误账号"}]}}`, spec.bilibiliMID, spec.bilibiliTitle, spec.bilibiliMID, spec.bilibiliTitle)
			return feedFetchResponse{Body: []byte(body), ContentType: "application/json", FinalURL: raw}, nil
		}
		return feedFetchResponse{}, errors.New("fixture Bilibili account 未覆盖: " + keyword)
	default:
		return feedFetchResponse{}, errors.New("fixture URL 未覆盖: " + raw)
	}
}

func TestYouTubeOperationalFeedPinsChannelAndProducesPlayableItems(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA","title":"Anthropic"},"entries":[{"id":"one","title":"Research","url":"https://www.youtube.com/watch?v=iF5IWjOWcA4","description":"AI research","publishedAt":"2026-08-12T00:00:00Z","duration":"12:34","media":[{"url":"https://i2.ytimg.com/vi/iF5IWjOWcA4/hqdefault.jpg","type":"photo"}]},{"id":"bad","title":"Wrong host","url":"https://example.com/watch?v=iF5IWjOWcA4"}]}}`)
	feed, err := parseYouTubeOperationalFeed(body, source, spec, operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.SiteURL != "https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA" || feed.IconURL != "https://icons.folo.is/youtube.com" || len(feed.Items) != 1 {
		t.Fatalf("feed=%#v", feed)
	}
	item := feed.Items[0]
	if item.ViewType != "video" || item.URL != "https://www.youtube.com/watch?v=iF5IWjOWcA4" || item.ThumbnailURL != "https://i2.ytimg.com/vi/iF5IWjOWcA4/hqdefault.jpg" || item.DurationSecs != 754 {
		t.Fatalf("item=%#v", item)
	}
	if _, ok := youtubeVideoID("https://www.youtube.com/watch?v=bad"); ok {
		t.Fatal("short or malformed YouTube IDs must be rejected")
	}
	if _, ok := youtubeVideoID("https://evil.example/watch?v=iF5IWjOWcA4"); ok {
		t.Fatal("untrusted YouTube host accepted")
	}
}

func TestBilibiliOperationalFeedPinsAccountAndProducesPlayableItems(t *testing.T) {
	spec := operationalSourceSpecs["bilibili-geekerwan"]
	source := FeedSource{ID: "bilibili-geekerwan", Name: spec.bilibiliTitle, URL: "https://space.bilibili.com/25876945/video"}
	body := []byte(fmt.Sprintf(`{"code":0,"data":{"page":1,"next":2,"numPages":2,"result":[{"result_type":"bili_user","data":[{"mid":25876945,"uname":"极客湾Geekerwan","upic":"//i0.hdslb.com/bfs/face/geekerwan.jpg","res":[{"bvid":"BV1abc123456","title":"极客湾 AI 芯片实测","description":"公开摘要","pubdate":1786400000,"duration":"12:34"}]}]},{"result_type":"video","data":[{"bvid":"BV2abc123456","mid":25876945,"author":"极客湾Geekerwan","title":"第二条视频","description":"第二条摘要","pubdate":1786300000,"duration":"1:02"},{"bvid":"BV3abc123456","mid":999,"title":"错误账号"}] } ]}}`))
	feed, err := parseBilibiliOperationalFeed(body, spec.endpoints[0], source, spec, operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Format != "bilibili" || feed.SiteURL != "https://space.bilibili.com/25876945/video" || feed.IconURL != "https://i0.hdslb.com/bfs/face/geekerwan.jpg" || len(feed.Items) != 2 {
		t.Fatalf("feed=%#v", feed)
	}
	for _, item := range feed.Items {
		if item.SourceType != "bilibili" || item.ViewType != "video" || item.PermissionScope != "public_read" || !strings.HasPrefix(item.URL, "https://www.bilibili.com/video/BV") || item.DurationSecs == 0 {
			t.Fatalf("item=%#v", item)
		}
	}
}

func TestBilibiliSearchTypeWindowFiltersAccountWithoutRelevancePageCap(t *testing.T) {
	spec := operationalSourceSpecs["bilibili-tech-aesthetics"]
	source := FeedSource{ID: "bilibili-tech-aesthetics", Name: spec.bilibiliTitle, URL: "https://space.bilibili.com/3766866/video"}
	body := []byte(`{"code":0,"data":{"page":1,"next":2,"numPages":20,"result":[{"bvid":"BV1abc123456","mid":3766866,"author":"科技美学","title":"<em class=\"keyword\">科技美学</em> 新品实测","description":"公开视频","pubdate":1786400000,"duration":"12:34"},{"bvid":"BV2abc123456","mid":999,"author":"其他账号","title":"同关键词但不是固定账号"}]}}`)
	feed, err := parseBilibiliOperationalFeed(body, spec.endpoints[0], source, spec, operationalBilibiliPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].Title != "科技美学 新品实测" || feed.Items[0].Author != "科技美学" {
		t.Fatalf("feed=%#v", feed)
	}
	policy := normalizedOperationalPaging(spec.paging, 10000)
	if policy.PageSize != 50 || policy.MaxPages != 20 || policy.MaxItems != 2000 {
		t.Fatalf("Bilibili policy=%#v", policy)
	}
}

func TestBilibiliFoloOperationalFeedPinsAccountAndProducesPlayableItems(t *testing.T) {
	spec := operationalSourceSpecs["bilibili-geekpark"]
	source := FeedSource{ID: "bilibili-geekpark", Name: spec.bilibiliTitle, URL: spec.endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://space.bilibili.com/13260662","title":"极客公园 的 bilibili 空间","image":"https://i2.hdslb.com/bfs/face/avatar.jpg"},"entries":[{"id":"one","title":"AI 硬件","url":"https://www.bilibili.com/video/BV1abc123456","summary":"公开视频","author":"极客公园","publishedAt":"2026-08-15T00:00:00Z","media":[{"url":"https://i1.hdslb.com/bfs/archive/cover.jpg","type":"photo"}]},{"id":"bad","title":"错误来源","url":"https://www.bilibili.com/video/BV1abc123456","summary":"duplicate"},{"id":"other","title":"错误站点","url":"https://example.com/video/BV1abc123456"}]}}`)
	feed, err := parseBilibiliFoloOperationalFeed(body, source, spec, operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Format != "bilibili" || feed.SiteURL != "https://space.bilibili.com/13260662" || feed.IconURL != "https://i2.hdslb.com/bfs/face/avatar.jpg" || len(feed.Items) != 1 {
		t.Fatalf("feed=%#v", feed)
	}
	if feed.Items[0].ExternalID != "BV1abc123456" || feed.Items[0].ViewType != "video" || feed.Items[0].ThumbnailURL != "https://i1.hdslb.com/bfs/archive/cover.jpg" || feed.Items[0].SourceLevel != "public_aggregator" {
		t.Fatalf("item=%#v", feed.Items[0])
	}
}

func TestBilibiliOperationalSourceFallsBackWhenSearchAPIIsBlocked(t *testing.T) {
	spec := operationalSourceSpecs["bilibili-geekerwan"]
	source := FeedSource{ID: "bilibili-geekerwan", Name: "极客湾Geekerwan", URL: "https://space.bilibili.com/25876945/video"}
	searchURL := spec.endpoints[0]
	fallbackURL := bilibiliFoloFeedURL(spec.bilibiliMID)
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		if raw == searchURL {
			return feedFetchResponse{}, errors.New("Bilibili 412")
		}
		if raw != fallbackURL {
			return feedFetchResponse{}, errors.New("unexpected fallback URL")
		}
		body := fmt.Sprintf(`{"code":0,"data":{"feed":{"siteUrl":"https://space.bilibili.com/%d/video","title":"极客湾Geekerwan"},"entries":[{"id":"folo-bv-1","title":"科技更新","url":"https://www.bilibili.com/video/BV1abc123456","description":"视频摘要","publishedAt":"2026-08-12T00:00:00Z","media":[{"url":"https://i0.hdslb.com/bfs/archive/cover.jpg","type":"photo"}]}]}}`, spec.bilibiliMID)
		return feedFetchResponse{Body: []byte(body), ContentType: "application/json", FinalURL: raw}, nil
	}
	result := collectBilibiliOperationalSource(context.Background(), source, spec, fetcher, "2026-08-12T00:00:00Z")
	if result.Status != "partial" || len(result.Feed.Items) != 1 || !strings.Contains(result.Message, "降级") {
		t.Fatalf("fallback result = %#v", result)
	}
}

func TestBilibiliOperationalFeedRejectsAccountMismatch(t *testing.T) {
	spec := operationalSourceSpecs["bilibili-geekerwan"]
	source := FeedSource{ID: "bilibili-geekerwan", Name: spec.bilibiliTitle, URL: "https://space.bilibili.com/25876945/video"}
	body := []byte(`{"code":0,"data":{"page":1,"result":[{"result_type":"bili_user","data":[{"mid":3766866,"uname":"科技美学","res":[{"bvid":"BV1abc123456","title":"错误账号"}]}]}]}}`)
	if _, err := parseBilibiliOperationalFeed(body, spec.endpoints[0], source, spec, operationalSourceLimit); err == nil {
		t.Fatal("Bilibili account mismatch must fail closed")
	}
}

func TestBilibiliMigrationSeedsTenVerifiedDefaultSourcesFlow(t *testing.T) {
	db = openTestDB(t)
	gateway := map[string]bool{
		"bilibili-geekerwan": true, "bilibili-tech-aesthetics": true,
		"bilibili-yingshijufeng": true, "bilibili-hardware-tea": true,
	}
	expected := []string{
		"bilibili-geekerwan",
		"bilibili-tech-aesthetics",
		"bilibili-yingshijufeng",
		"bilibili-machine-heart",
		"bilibili-qbit",
		"bilibili-geekpark",
		"bilibili-hardware-tea",
		"bilibili-he-tongxue",
		"bilibili-li-mu-ai",
		"bilibili-ai-tech-review",
	}
	for _, id := range expected {
		var kind, sourceURL, format, folder, mode string
		var isDefault, enabled, hidden int
		err := db.QueryRow(`SELECT f.kind,f.url,f.is_default,f.hidden,c.feed_format,s.enabled,s.folder_id,f.collection_mode FROM feed_sources f JOIN feed_catalog_entries c ON c.source_id=f.id JOIN feed_subscriptions s ON s.source_id=f.id WHERE f.id=?`, id).Scan(&kind, &sourceURL, &isDefault, &hidden, &format, &enabled, &folder, &mode)
		if err != nil {
			t.Fatalf("source %s: %v", id, err)
		}
		validOwnership := mode == "agent" && sourceURL == "source-retired://"+id && isDefault == 0 && hidden == 1
		if gateway[id] {
			validOwnership = mode == "source_gateway" && sourceURL == "source-gateway://"+id && isDefault == 1 && hidden == 0
		}
		if kind != "api" || !validOwnership || format != "bilibili" || enabled != 1 || folder != "folder-bilibili-ai" {
			t.Fatalf("source %s kind=%s url=%s mode=%s default=%d hidden=%d format=%s enabled=%d folder=%s", id, kind, sourceURL, mode, isDefault, hidden, format, enabled, folder)
		}
	}
}

func TestBilibiliMigrationKeepsHistoryWindowForProviderFeeds(t *testing.T) {
	db = openTestDB(t)
	var backfillDays, retentionDays, retentionLimit int
	if err := db.QueryRow(`SELECT backfill_days,retention_days,retention_limit FROM feed_sources WHERE id='bilibili-geekpark'`).Scan(&backfillDays, &retentionDays, &retentionLimit); err != nil {
		t.Fatal(err)
	}
	if backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 10000 {
		t.Fatalf("bilibili history window=%d/%d/%d", backfillDays, retentionDays, retentionLimit)
	}
}

func TestOperationalBackfillCursorValidation(t *testing.T) {
	tests := []struct {
		name   string
		source string
		valid  string
	}{
		{
			name:   "arxiv",
			source: "arxiv-ai",
			valid:  "https://export.arxiv.org/api/query?max_results=20&search_query=cat%3Acs.AI&sortBy=submittedDate&sortOrder=descending&start=20",
		},
		{
			name:   "hacker news",
			source: "hacker-news",
			valid:  "https://hn.algolia.com/api/v1/search_by_date?hitsPerPage=20&page=1&query=AI&tags=story",
		},
		{
			name:   "crossref",
			source: "crossref-research",
			valid:  "https://api.crossref.org/works?filter=until-pub-date%3A2026-08-20&offset=20&order=desc&query=artificial+intelligence&rows=20&select=DOI%2Ctitle%2CURL%2Cpublished%2Cauthor%2Ccontainer-title%2Cabstract&sort=published",
		},
		{
			name:   "federal register",
			source: "govinfo-federal-register",
			valid:  "https://www.federalregister.gov/api/v1/documents?format=json&order=newest&page=2&per_page=20&search_after_cursor=cursor_2",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := operationalSourceSpecs[test.source]
			policy := normalizedOperationalPaging(spec.paging, 1000)
			if got, ok := validOperationalBackfillCursor(test.valid, spec, policy); !ok || got == "" {
				t.Fatalf("valid cursor rejected: %q", test.valid)
			}
			invalid := []string{
				strings.Replace(test.valid, "https://", "https://evil.example/", 1),
				test.valid + "&unexpected=1",
				test.valid + "&" + map[string]string{"arxiv-ai": "start=40", "hacker-news": "page=2", "crossref-research": "offset=40", "govinfo-federal-register": "page=3"}[test.source],
			}
			for _, raw := range invalid {
				if _, ok := validOperationalBackfillCursor(raw, spec, policy); ok {
					t.Fatalf("unsafe cursor accepted: %q", raw)
				}
			}
		})
	}
	crossref := operationalSourceSpecs["crossref-research"]
	policy := normalizedOperationalPaging(crossref.paging, 1000)
	future := "https://api.crossref.org/works?filter=until-pub-date%3A2999-01-01&offset=20&order=desc&query=artificial+intelligence&rows=20&select=DOI%2Ctitle%2CURL%2Cpublished%2Cauthor%2Ccontainer-title%2Cabstract&sort=published"
	if _, ok := validOperationalBackfillCursor(future, crossref, policy); ok {
		t.Fatal("future Crossref cursor accepted")
	}
}

func TestPagedOperationalBackfillResumesAcrossSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "hacker-news")
	previousFetch := operationalSourceFetch
	pages := make([]int, 0, 6)
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() != "hn.algolia.com" {
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
		page, err := strconv.Atoi(parsed.Query().Get("page"))
		if err != nil {
			return feedFetchResponse{}, err
		}
		pages = append(pages, page)
		return feedFetchResponse{
			Body:        []byte(hackerNewsHistoryFixture(page, 10)),
			ContentType: "application/json",
			FinalURL:    raw,
		}, nil
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	for round := 0; round < 2; round++ {
		if err := syncLegacyPublicCollectorFixture(context.Background(), "hacker-news"); err != nil {
			t.Fatalf("sync round %d: %v", round+1, err)
		}
	}
	if got, want := fmt.Sprint(pages), "[0 1 2 0 3 4]"; got != want {
		t.Fatalf("page sequence=%s, want %s", got, want)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='hacker-news'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='hacker-news'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	parsedCursor, err := url.Parse(cursor)
	if err != nil || parsedCursor.Query().Get("page") != "5" {
		t.Fatalf("cursor=%q err=%v", cursor, err)
	}
	if entries != 100 || status != "partial" {
		t.Fatalf("entries=%d status=%q cursor=%q", entries, status, cursor)
	}
}

func TestPagedOperationalBackfillCompletesOrRetriesCursor(t *testing.T) {
	spec := operationalSourceSpecs["hacker-news"]
	policy := normalizedOperationalPaging(spec.paging, 1000)
	cursor := "https://hn.algolia.com/api/v1/search_by_date?hitsPerPage=20&page=3&query=AI&tags=story"
	source := FeedSource{
		ID:             "hacker-news",
		Name:           "Hacker News AI",
		URL:            hackerNewsAISearchURL,
		LastSyncAt:     time.Now().UTC().Format(time.RFC3339),
		RecordsSeen:    60,
		BackfillDays:   30,
		RetentionLimit: 1000,
		BackfillCursor: cursor,
	}
	if _, ok := validOperationalBackfillCursor(cursor, spec, policy); !ok {
		t.Fatal("test cursor is invalid")
	}

	t.Run("complete", func(t *testing.T) {
		var pages []int
		fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			parsed, _ := url.Parse(raw)
			page, _ := strconv.Atoi(parsed.Query().Get("page"))
			pages = append(pages, page)
			return feedFetchResponse{Body: []byte(hackerNewsHistoryFixture(page, 4)), ContentType: "application/json", FinalURL: raw}, nil
		}
		result := collectPagedOperationalSource(context.Background(), source, spec, fetcher, time.Now().UTC().Format(time.RFC3339))
		if got := fmt.Sprint(pages); got != "[0 3]" {
			t.Fatalf("pages=%s", got)
		}
		if !result.Backfill || !result.BackfillCursorChanged || result.BackfillCursor != "" || result.Status != "ok" {
			t.Fatalf("completed result=%#v", result)
		}
	})

	t.Run("retry failed history page", func(t *testing.T) {
		fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			parsed, _ := url.Parse(raw)
			page, _ := strconv.Atoi(parsed.Query().Get("page"))
			if page == 3 {
				return feedFetchResponse{}, errors.New("HTTP 503")
			}
			return feedFetchResponse{Body: []byte(hackerNewsHistoryFixture(page, 4)), ContentType: "application/json", FinalURL: raw}, nil
		}
		result := collectPagedOperationalSource(context.Background(), source, spec, fetcher, time.Now().UTC().Format(time.RFC3339))
		if !result.BackfillCursorChanged || result.BackfillCursor != cursor || result.Status != "partial" || len(result.Feed.Items) != operationalSourceLimit {
			t.Fatalf("retry result=%#v", result)
		}
	})
}

func TestTrustedHistoricalFeedLimitDoesNotAffectUnknownFeeds(t *testing.T) {
	body := openAIHistoryRSSFixture(130)
	ordinary, err := parseXMLFeed([]byte(body), "https://example.com/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := parseXMLFeedWithLimit([]byte(body), "https://openai.com/news/rss.xml", feedFetchTrustedMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	if len(ordinary.Items) != feedFetchMaxItems || len(trusted.Items) != 130 {
		t.Fatalf("ordinary=%d trusted=%d", len(ordinary.Items), len(trusted.Items))
	}
}

func TestTrustedHistoricalSourcesPersistBeyondLegacyCapsFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "openai-news", "china-policy-library")
	previousFetch := operationalSourceFetch
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		switch raw {
		case "https://openai.com/news/rss.xml":
			return feedFetchResponse{Body: []byte(openAIHistoryRSSFixture(130)), ContentType: "application/rss+xml", FinalURL: raw}, nil
		case "https://www.gov.cn/zhengce/zuixin/ZUIXINZHENGCE.json":
			return feedFetchResponse{Body: []byte(policyHistoryJSONFixture(130)), ContentType: "application/json", FinalURL: raw}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	for _, sourceID := range []string{"openai-news", "china-policy-library"} {
		if err := syncLegacyPublicCollectorFixture(context.Background(), sourceID); err != nil {
			t.Fatalf("sync %s: %v", sourceID, err)
		}
		var entries, retentionLimit int
		if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, sourceID).Scan(&entries); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT retention_limit FROM feed_sources WHERE id=?`, sourceID).Scan(&retentionLimit); err != nil {
			t.Fatal(err)
		}
		if entries != 130 || retentionLimit != feedFetchTrustedMaxItems {
			t.Fatalf("source=%s entries=%d retention=%d", sourceID, entries, retentionLimit)
		}
	}
}

func TestTrustedHistoricalMigrationClearsValidatorsBeforeOpenAIRefetchFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "086_")
	// 与 reader_counts 升级窗口测试同理：复刻生产启动期的幂等补列再查询。
	if err := reconcileSourceGatewaySchema(conn); err != nil {
		t.Fatalf("reconcile schema: %v", err)
	}
	previousDB := db
	db = conn
	t.Cleanup(func() { db = previousDB })
	if _, err := conn.Exec(`UPDATE feed_sources SET etag='legacy-etag',last_modified='legacy-modified',last_sync_at='2026-08-20T00:00:00Z',records_seen=100,records_used=100 WHERE id='openai-news'`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, conn, "086_requeue_openai_history.sql")

	previousFetch := operationalSourceFetch
	operationalSourceFetch = func(_ context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
		if raw != "https://openai.com/news/rss.xml" {
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
		if etag != "" || lastModified != "" {
			return feedFetchResponse{NotModified: true, FinalURL: raw, ETag: etag, LastModified: lastModified}, nil
		}
		return feedFetchResponse{Body: []byte(openAIHistoryRSSFixture(1143)), ContentType: "application/rss+xml", FinalURL: raw, ETag: "fresh-etag", LastModified: "fresh-modified"}, nil
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	if err := syncLegacyPublicCollectorFixture(context.Background(), "openai-news"); err != nil {
		t.Fatal(err)
	}
	var entries int
	var etag, lastModified string
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='openai-news' AND deleted_at=''`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT etag,last_modified FROM feed_sources WHERE id='openai-news'`).Scan(&etag, &lastModified); err != nil {
		t.Fatal(err)
	}
	if entries != 1143 || etag != "fresh-etag" || lastModified != "fresh-modified" {
		t.Fatalf("entries=%d validators=%q/%q", entries, etag, lastModified)
	}
}

func TestNoncoreRestoreMergesDuplicateUserURLWithoutLosingHistoryFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "075_")
	now := "2026-08-21T00:00:00Z"
	const sourceURL = "https://openai.com/news/rss.xml"
	if _, err := conn.Exec(`UPDATE feed_subscriptions SET folder_id='',title_override='',pinned=0,muted=0,sort_order=0 WHERE source_id='openai-news'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO feed_sources
(id,kind,name,url,description,is_default,status,collection_mode,created_at,updated_at)
VALUES('user-openai','rss','My AI feed',?,'user duplicate',0,'ok','app',?,?)`, sourceURL, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO feed_subscriptions
(source_id,enabled,subscribed_at,updated_at,folder_id,title_override,pinned,muted,sort_order)
VALUES('user-openai',1,?,?,'folder-user','My title',1,0,77)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,published_at,summary_text,content_format,safe_html,updated_at)
VALUES
('user-openai-entry','entry-1','kept','https://example.com/kept','My AI feed','user-openai','app-fetch','rss',?,'','text','',?),
('default-overlap','shared-entry','canonical','https://example.com/shared','OpenAI News','openai-news','app-fetch','rss',?,'','text','',?),
('user-overlap','shared-entry','duplicate','https://example.com/shared','My AI feed','user-openai','app-fetch','rss',?,'','text','',?)`, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at)
	VALUES('news:user-openai-entry',0,1,0,0,?),('news:user-overlap',0,1,1,0.5,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO stable_context_items(owner_id,profile_id,item_key,state,added_by,added_at,updated_at,content_hash,source_id)
VALUES('owner','profile','news:user-overlap','active','manual_confirm',?,?,'context-hash','user-openai')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,source_url,updated_at,content_hash)
VALUES('user-overlap','full text','text',?,'ok','https://example.com/shared',?,'full-hash')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO subscription_tags(id,name,color,created_at,updated_at)
VALUES('tag-user','User tag','#64748b',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO feed_subscription_tags(source_id,tag_id,created_at)
VALUES('user-openai','tag-user',?)`, now); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "075_restore_noncore_local_sources.sql")

	var defaultURL, defaultMode, folderID, titleOverride string
	var defaultEnabled, pinned, sortOrder int
	if err := conn.QueryRow(`SELECT f.url,f.collection_mode,s.enabled,s.folder_id,s.title_override,s.pinned,s.sort_order
FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id WHERE f.id='openai-news'`).Scan(&defaultURL, &defaultMode, &defaultEnabled, &folderID, &titleOverride, &pinned, &sortOrder); err != nil {
		t.Fatal(err)
	}
	var duplicateURL, duplicateMode string
	var duplicateHidden, duplicateEnabled, retained, visibleOverlap int
	if err := conn.QueryRow(`SELECT f.url,f.collection_mode,f.hidden,s.enabled
FROM feed_sources f JOIN feed_subscriptions s ON s.source_id=f.id WHERE f.id='user-openai'`).Scan(&duplicateURL, &duplicateMode, &duplicateHidden, &duplicateEnabled); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT
(SELECT COUNT(*) FROM news WHERE id='user-openai-entry' AND source_id='openai-news' AND deleted_at='') +
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:user-openai-entry' AND starred=1) +
(SELECT COUNT(*) FROM news WHERE id='user-overlap' AND deleted_at<>'') +
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:default-overlap' AND starred=1 AND read_later=1) +
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:user-overlap') +
(SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:default-overlap' AND state='active' AND source_id='openai-news') +
(SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:user-overlap') +
(SELECT COUNT(*) FROM entry_full_texts WHERE entry_id='default-overlap' AND content_text='full text') +
(SELECT COUNT(*) FROM feed_subscription_tags WHERE source_id='openai-news' AND tag_id='tag-user')`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='openai-news' AND url='https://example.com/shared' AND deleted_at=''`).Scan(&visibleOverlap); err != nil {
		t.Fatal(err)
	}
	if defaultURL != sourceURL || defaultMode != "app" || defaultEnabled != 1 || folderID != "folder-user" || titleOverride != "My title" || pinned != 1 || sortOrder != 77 ||
		duplicateURL != "source-merged://user-openai" || duplicateMode != "agent" || duplicateHidden != 1 || duplicateEnabled != 0 || retained != 7 || visibleOverlap != 1 {
		t.Fatalf("default=%q/%q enabled=%d folder=%q title=%q pinned=%d sort=%d duplicate=%q/%q hidden=%d enabled=%d retained=%d overlap=%d",
			defaultURL, defaultMode, defaultEnabled, folderID, titleOverride, pinned, sortOrder, duplicateURL, duplicateMode, duplicateHidden, duplicateEnabled, retained, visibleOverlap)
	}
}

func TestFederalRegisterBackfillResumesAcrossSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "govinfo-federal-register")
	previousFetch := operationalSourceFetch
	pages := make([]int, 0, 6)
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() != "www.federalregister.gov" {
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
		page := 1
		if rawPage := parsed.Query().Get("page"); rawPage != "" {
			page, err = strconv.Atoi(rawPage)
			if err != nil {
				return feedFetchResponse{}, err
			}
		}
		pages = append(pages, page)
		return feedFetchResponse{Body: []byte(federalRegisterHistoryFixture(page, 5)), ContentType: "application/json", FinalURL: raw}, nil
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	for round := 0; round < 2; round++ {
		if err := syncLegacyPublicCollectorFixture(context.Background(), "govinfo-federal-register"); err != nil {
			t.Fatalf("sync round %d: %v", round+1, err)
		}
	}
	if got, want := fmt.Sprint(pages), "[1 2 3 1 4 5]"; got != want {
		t.Fatalf("pages=%s, want %s", got, want)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='govinfo-federal-register'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='govinfo-federal-register'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if entries != 100 || cursor != "" || status != "ok" {
		t.Fatalf("entries=%d cursor=%q status=%q", entries, cursor, status)
	}
}

func TestFederalRegisterFailedInitialBackfillRetriesHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "govinfo-federal-register")
	if _, err := db.Exec(`UPDATE feed_sources SET status='unavailable',last_sync_at='2026-08-21T00:00:00Z',records_seen=40,records_used=0,last_error='context deadline exceeded',backfill_cursor='' WHERE id='govinfo-federal-register'`); err != nil {
		t.Fatal(err)
	}
	previousFetch := operationalSourceFetch
	pages := make([]int, 0, 3)
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		parsed, err := url.Parse(raw)
		if err != nil {
			return feedFetchResponse{}, err
		}
		page := 1
		if rawPage := parsed.Query().Get("page"); rawPage != "" {
			page, err = strconv.Atoi(rawPage)
			if err != nil {
				return feedFetchResponse{}, err
			}
		}
		pages = append(pages, page)
		return feedFetchResponse{Body: []byte(federalRegisterHistoryFixture(page, 10)), ContentType: "application/json", FinalURL: raw}, nil
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	if err := syncLegacyPublicCollectorFixture(context.Background(), "govinfo-federal-register"); err != nil {
		t.Fatal(err)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='govinfo-federal-register'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='govinfo-federal-register'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(pages); got != "[1 2 3]" || entries != 60 || status != "partial" || !strings.Contains(cursor, "page=4") {
		t.Fatalf("pages=%s entries=%d cursor=%q status=%q", got, entries, cursor, status)
	}
}

func TestEURLEXOfficialFeedKeepsHundredItemWindowFlow(t *testing.T) {
	spec := operationalSourceSpecs["eurlex-legislation"]
	source := FeedSource{ID: "eurlex-legislation", Name: "欧盟立法动态", URL: spec.endpoints[0], BackfillDays: 3650, RetentionLimit: 1000}
	var body strings.Builder
	body.WriteString(`<rss><channel><title>EUR-Lex</title><link>https://eur-lex.europa.eu/</link>`)
	for index := 0; index < operationalFeedWindowMaxItems; index++ {
		fmt.Fprintf(&body, `<item><guid>eurlex-%d</guid><title>EU act %d</title><link>https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:%d</link><pubDate>Thu, 20 Aug 2026 12:00:00 GMT</pubDate></item>`, index, index, index)
	}
	body.WriteString(`</channel></rss>`)
	result := collectOperationalSourceContextWithFetcher(context.Background(), source, spec, func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		return feedFetchResponse{Body: []byte(body.String()), ContentType: "application/rss+xml", FinalURL: raw}, nil
	})
	if result.Status != "ok" || len(result.Feed.Items) != operationalFeedWindowMaxItems {
		t.Fatalf("status=%q items=%d message=%q", result.Status, len(result.Feed.Items), result.Message)
	}
}

func openAIHistoryRSSFixture(count int) string {
	var body strings.Builder
	body.WriteString(`<rss><channel><title>OpenAI News</title><link>https://openai.com/news/</link>`)
	for index := 0; index < count; index++ {
		fmt.Fprintf(&body, `<item><guid>openai-%d</guid><title>Research update %d</title><link>https://openai.com/news/research-%d/</link><pubDate>Thu, 20 Aug 2026 12:%02d:00 GMT</pubDate><description>Research archive %d</description></item>`, index, index, index, index%60, index)
	}
	body.WriteString(`</channel></rss>`)
	return body.String()
}

func policyHistoryJSONFixture(count int) string {
	var body strings.Builder
	body.WriteString(`{"items":[`)
	for index := 0; index < count; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"TITLE":"政策文件 %d","URL":"https://www.gov.cn/zhengce/content/%d/content.html","DOCRELPUBTIME":"2026-08-20","SUB_TITLE":"政策摘要 %d"}`, index, index, index)
	}
	body.WriteString(`]}`)
	return body.String()
}

func federalRegisterHistoryFixture(page, pageCount int) string {
	var body strings.Builder
	nextURL := ""
	if page < pageCount {
		nextURL = fmt.Sprintf("https://www.federalregister.gov/api/v1/documents?format=json&order=newest&page=%d&per_page=20&search_after_cursor=cursor_%d", page+1, page+1)
	}
	fmt.Fprintf(&body, `{"count":100,"total_pages":%d,"next_page_url":%q,"results":[`, pageCount, nextURL)
	for index := 0; index < operationalSourceLimit; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		number := fmt.Sprintf("2026-%05d", page*operationalSourceLimit+index)
		fmt.Fprintf(&body, `{"document_number":%q,"title":%q,"abstract":%q,"html_url":%q,"publication_date":"2026-08-21","type":"Notice","agencies":[{"name":"Research Agency"}]}`, number, "Federal research "+number, "Public notice "+number, "https://www.federalregister.gov/documents/2026/08/21/"+number+"/federal-research")
	}
	body.WriteString(`]}`)
	return body.String()
}

func hackerNewsHistoryFixture(page, pageCount int) string {
	var body strings.Builder
	fmt.Fprintf(&body, `{"page":%d,"nbPages":%d,"hits":[`, page, pageCount)
	for index := 0; index < operationalSourceLimit; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		id := 100000 + page*operationalSourceLimit + index
		published := time.Now().UTC().Add(-time.Duration(id-100000) * time.Minute).Format(time.RFC3339)
		fmt.Fprintf(&body, `{"objectID":"%d","title":"AI history %d","url":"https://example.com/ai-history/%d","author":"reader","created_at":"%s","story_text":"AI research archive"}`, id, id, id, published)
	}
	body.WriteString(`]}`)
	return body.String()
}

func TestYouTubeOperationalFeedRejectsChannelMismatch(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://www.youtube.com/channel/UCHuiy8bXnmK5nisYHUd1J5g","title":"NVIDIA"},"entries":[{"id":"one","title":"Wrong","url":"https://www.youtube.com/watch?v=iF5IWjOWcA4"}]}}`)
	if _, err := parseYouTubeOperationalFeed(body, source, spec, operationalSourceLimit); err == nil {
		t.Fatal("channel mismatch must fail closed")
	}
}

func TestYouTubeOperationalFeedAcceptsOfficialAtomAndKeepsVideoMetadata(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0]}
	body := `<feed xmlns="http://www.w3.org/2005/Atom"><title>Anthropic</title><link rel="alternate" href="https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA"/><entry><id>yt:video:iF5IWjOWcA4</id><title>Research</title><link rel="alternate" href="https://www.youtube.com/watch?v=iF5IWjOWcA4"/><published>2026-08-12T00:00:00Z</published><updated>2026-08-12T00:00:00Z</updated><summary>AI research</summary></entry><entry><id>yt:video:dQw4w9WgXcQ</id><title>Systems</title><link rel="alternate" href="https://www.youtube.com/watch?v=dQw4w9WgXcQ"/><published>2026-08-11T00:00:00Z</published><updated>2026-08-11T00:00:00Z</updated><summary>Systems research</summary></entry></feed>`
	feed, err := parseYouTubeOperationalFeed([]byte(body), source, spec, operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Format != "youtube" || feed.SiteURL != "https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA" || len(feed.Items) != 2 {
		t.Fatalf("feed=%#v", feed)
	}
	for _, item := range feed.Items {
		if item.ViewType != "video" || item.SourceType != "youtube" || item.SourceLevel != "official" || item.ThumbnailURL == "" {
			t.Fatalf("item=%#v", item)
		}
	}
}

func TestYouTubeHistoryRSSPinsChannelTitleAndVideoURLs(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0]}
	body := strings.Replace(youtubeHistoryRSSFixture(spec, 2), `</channel>`, `<item><title>Untrusted</title><link>https://evil.example/watch?v=iF5IWjOWcA4</link><guid>bad</guid></item></channel>`, 1)
	feed, err := parseYouTubeOperationalFeed([]byte(body), source, spec, operationalYouTubeMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	if feed.SiteURL != "https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA" || len(feed.Items) != 2 {
		t.Fatalf("history feed=%#v", feed)
	}
	for _, item := range feed.Items {
		if item.SourceLevel != "public_aggregator" || item.ViewType != "video" || !strings.HasPrefix(item.URL, "https://www.youtube.com/watch?v=hist") {
			t.Fatalf("history item=%#v", item)
		}
	}

	for _, invalid := range []string{
		strings.Replace(body, "Anthropic - YouTube", "Anthropic Archive", 1),
		strings.Replace(body, spec.youtubeChannelID, "UCHuiy8bXnmK5nisYHUd1J5g", 1),
	} {
		if _, parseErr := parseYouTubeOperationalFeed([]byte(invalid), source, spec, operationalYouTubeMaxItems); parseErr == nil {
			t.Fatal("history feed identity mismatch must fail closed")
		}
	}
}

func TestYouTubeOfficialAtomDoesNotStopAtFourEntries(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0]}
	var body strings.Builder
	body.WriteString(`<feed xmlns="http://www.w3.org/2005/Atom"><title>Anthropic</title><link rel="alternate" href="https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA"/>`)
	for index, videoID := range []string{"iF5IWjOWcA4", "dQw4w9WgXcQ", "abcdefghijk", "lmnopqrstuv", "wxyzABCDEFG", "HIJKLMNopqr"} {
		fmt.Fprintf(&body, `<entry><id>yt:video:%s</id><title>Research %d</title><link rel="alternate" href="https://www.youtube.com/watch?v=%s"/><published>2026-08-%02dT00:00:00Z</published><summary>AI research</summary></entry>`, videoID, index+1, videoID, 12-index)
	}
	body.WriteString(`</feed>`)
	feed, err := parseYouTubeOperationalFeed([]byte(body.String()), source, spec, operationalMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 6 {
		t.Fatalf("official Atom feed was truncated at %d items", len(feed.Items))
	}
	for _, item := range feed.Items {
		if strings.Contains(item.ThumbnailURL, "folo.is") {
			t.Fatalf("direct YouTube thumbnail still uses Folo: %q", item.ThumbnailURL)
		}
	}
}

func TestYouTubeOperationalSourceFallsBackWhenOfficialAtomIsUnreachable(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0], ETag: "official-etag", LastModified: "official-last-modified", BackfillCursor: "keep-cursor"}
	var calls []string
	fetcher := func(_ context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		if raw == spec.endpoints[0] {
			if etag != source.ETag || lastModified != source.LastModified {
				t.Fatalf("official validators=%q/%q", etag, lastModified)
			}
			return feedFetchResponse{}, errors.New("YouTube Atom timeout")
		}
		if etag != "" || lastModified != "" {
			t.Fatalf("fallback inherited official validators=%q/%q", etag, lastModified)
		}
		return feedFetchResponse{Body: []byte(youtubeHistoryRSSFixture(spec, 5)), ContentType: "application/rss+xml", FinalURL: raw, ETag: "history-etag", LastModified: "history-last-modified"}, nil
	}
	result := collectYouTubeOperationalSource(context.Background(), source, spec, fetcher)
	if result.Status != "partial" || len(result.Feed.Items) != 5 {
		t.Fatalf("fallback result = %#v calls=%v", result, calls)
	}
	if len(calls) != 2 || calls[0] != spec.endpoints[0] || calls[1] != spec.endpoints[1] {
		t.Fatalf("fallback calls = %v, want official then history endpoint", calls)
	}
	if !strings.Contains(result.Message, "历史兼容") || result.Feed.ETag != source.ETag || result.Feed.LastModified != source.LastModified || result.BackfillCursorChanged {
		t.Fatalf("fallback message = %q", result.Message)
	}
}

func TestYouTubeOperationalSourceFallsThroughInvalidHistoryToFolo(t *testing.T) {
	spec := operationalSourceSpecs["youtube-anthropic"]
	source := FeedSource{ID: "youtube-anthropic", Name: "Anthropic", URL: spec.endpoints[0], ETag: "official-etag", LastModified: "official-last-modified"}
	var calls []string
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		switch raw {
		case spec.endpoints[0]:
			return feedFetchResponse{}, errors.New("YouTube Atom timeout")
		case spec.endpoints[1]:
			body := strings.Replace(youtubeHistoryRSSFixture(spec, 5), "Anthropic - YouTube", "Wrong Channel - YouTube", 1)
			return feedFetchResponse{Body: []byte(body), ContentType: "application/rss+xml", FinalURL: raw, ETag: "bad-history-etag"}, nil
		case spec.endpoints[2]:
			body := `{"code":0,"data":{"feed":{"siteUrl":"https://www.youtube.com/channel/UCrDwWp7EBBv4NwvScIpBDOA","title":"Anthropic"},"entries":[{"id":"fallback-1","title":"Fallback research","url":"https://www.youtube.com/watch?v=iF5IWjOWcA4","description":"AI research","publishedAt":"2026-08-12T00:00:00Z"}]}}`
			return feedFetchResponse{Body: []byte(body), ContentType: "application/json", FinalURL: raw, ETag: "folo-etag"}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	result := collectYouTubeOperationalSource(context.Background(), source, spec, fetcher)
	if result.Status != "partial" || len(result.Feed.Items) != 1 || !strings.Contains(result.Message, "已降级到兼容 feed") {
		t.Fatalf("fallback result=%#v", result)
	}
	if len(calls) != 3 || calls[0] != spec.endpoints[0] || calls[1] != spec.endpoints[1] || calls[2] != spec.endpoints[2] {
		t.Fatalf("fallback calls=%v", calls)
	}
	if result.Feed.ETag != source.ETag || result.Feed.LastModified != source.LastModified || result.BackfillCursorChanged {
		t.Fatalf("fallback polluted canonical state: %#v", result)
	}
}

func TestYouTubeHistoryFallbackPersistsFiftyEntriesFlow(t *testing.T) {
	db = openTestDB(t)
	const sourceID = "youtube-computerphile"
	spec := operationalSourceSpecs[sourceID]
	// The parser remains covered as an isolated compatibility fixture. The
	// production default is source_gateway after migration 092.
	if _, err := db.Exec(`UPDATE feed_sources SET collection_mode='app',url=?,etag='official-etag',last_modified='official-last-modified',backfill_cursor='keep-history-cursor' WHERE id=?`, spec.endpoints[0], sourceID); err != nil {
		t.Fatal(err)
	}
	previousFetch := operationalSourceFetch
	var calls []string
	operationalSourceFetch = func(_ context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		switch raw {
		case spec.endpoints[0]:
			if etag != "official-etag" || lastModified != "official-last-modified" {
				t.Fatalf("official validators=%q/%q", etag, lastModified)
			}
			return feedFetchResponse{}, errors.New("YouTube Atom timeout")
		case spec.endpoints[1]:
			if etag != "" || lastModified != "" {
				t.Fatalf("history validators=%q/%q", etag, lastModified)
			}
			return feedFetchResponse{Body: []byte(youtubeHistoryRSSFixture(spec, operationalYouTubeMaxItems)), ContentType: "application/rss+xml", FinalURL: raw, ETag: "history-etag", LastModified: "history-last-modified"}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	if err := syncLegacyPublicCollectorFixture(context.Background(), sourceID); err != nil {
		t.Fatal(err)
	}
	var entries, aggregatorEntries int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(CASE WHEN source_level='public_aggregator' THEN 1 ELSE 0 END) FROM news WHERE source_id=?`, sourceID).Scan(&entries, &aggregatorEntries); err != nil {
		t.Fatal(err)
	}
	var status, message, etag, lastModified, cursor string
	if err := db.QueryRow(`SELECT status,last_error,etag,last_modified,backfill_cursor FROM feed_sources WHERE id=?`, sourceID).Scan(&status, &message, &etag, &lastModified, &cursor); err != nil {
		t.Fatal(err)
	}
	if entries != operationalYouTubeMaxItems || aggregatorEntries != operationalYouTubeMaxItems || status != "partial" {
		t.Fatalf("entries=%d aggregator=%d status=%q", entries, aggregatorEntries, status)
	}
	if !strings.Contains(message, "历史兼容") || etag != "official-etag" || lastModified != "official-last-modified" || cursor != "keep-history-cursor" {
		t.Fatalf("message=%q validators=%q/%q cursor=%q", message, etag, lastModified, cursor)
	}
	if len(calls) != 2 || calls[0] != spec.endpoints[0] || calls[1] != spec.endpoints[1] {
		t.Fatalf("calls=%v", calls)
	}
}

func TestYouTubeSourceCatalogUsesGatewayWithoutPublicCollector(t *testing.T) {
	db = openTestDB(t)
	source, err := feedSourceByID("youtube-anthropic")
	if err != nil {
		t.Fatal(err)
	}
	want := "source-gateway://youtube-anthropic"
	if source.URL != want || source.CollectionMode != "source_gateway" {
		t.Fatalf("source URL/mode = %q/%q, want %q/source_gateway", source.URL, source.CollectionMode, want)
	}
	var stored string
	if err := db.QueryRow(`SELECT url FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != want {
		t.Fatalf("stored source URL = %q, want %q", stored, want)
	}
}

func TestYouTubeDirectAtomMigrationQueuesImmediateRefresh(t *testing.T) {
	// Upgrade path: a pre-V1 database still carries the retired direct-fetch
	// schedule; applying the Source Gateway migration must clear it and queue
	// the source for gateway sync without touching its rows or subscription.
	conn := openUpgradeTestDB(t, "074_")
	if _, err := conn.Exec(`UPDATE feed_sources SET next_sync_at='2999-01-01T00:00:00Z',etag='stale',last_modified='stale' WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, conn, "074_source_gateway_collection.sql")
	var nextSync, etag, lastModified, mode string
	if err := conn.QueryRow(`SELECT next_sync_at,etag,last_modified,collection_mode FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&nextSync, &etag, &lastModified, &mode); err != nil {
		t.Fatal(err)
	}
	if nextSync == "2999-01-01T00:00:00Z" || etag != "" || lastModified != "" || mode != "source_gateway" {
		t.Fatalf("gateway refresh was not queued: next=%q etag=%q modified=%q mode=%q", nextSync, etag, lastModified, mode)
	}
}

func TestParseHackerNewsSearchJSONKeepsOnlyReadableAIArticles(t *testing.T) {
	source := FeedSource{ID: "hacker-news", Name: "Hacker News AI", URL: hackerNewsAISearchURL}
	body := []byte(`{"hits":[
{"objectID":"101","title":"A production AI agent","url":"https://example.com/ai-agent","author":"alice","created_at":"2026-08-11T16:31:33Z","story_text":"Architecture notes"},
{"objectID":"102","title":"Ask HN: AI workflows?","url":"https://news.ycombinator.com/item?id=102","author":"bob","created_at":"2026-08-11T16:30:00Z"},
{"objectID":"103","title":"AI lending boom","url":"https://example.com/ahttps://example.com/a","author":"carol","created_at":"2026-08-11T16:29:00Z"},
{"objectID":"104","title":"A gardening article","url":"https://example.com/garden","author":"dan","created_at":"2026-08-11T16:28:00Z"},
{"objectID":"bad-id","title":"An LLM article","url":"https://example.com/llm","author":"eve","created_at":"2026-08-11T16:27:00Z"}
]}`)
	items, err := parseHackerNewsSearchJSON(body, source, operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%#v", items)
	}
	item := items[0]
	if item.ExternalID != "101" || item.URL != "https://example.com/ai-agent" || item.SourceType != "api" || item.SourceLevel != "public_aggregator" {
		t.Fatalf("item=%#v", item)
	}
}

func TestHackerNewsAISyncWritesBrowsableArticlesAndRetainsOnFailureFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "hacker-news")
	// Keep this historical fixture inside the source's retention window as the
	// wall clock advances; this test covers identity/upsert and failure
	// retention, not the default 30-day cutoff.
	if _, err := db.Exec(`UPDATE feed_sources SET backfill_days=365 WHERE id='hacker-news'`); err != nil {
		t.Fatal(err)
	}
	previousFetch := operationalSourceFetch
	operationalSourceFetch = operationalFixtureFetcher
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	const legacyID = "legacy-hacker-news-entry"
	const articleURL = "https://example.com/agentic-model"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, legacyID, "https://news.ycombinator.com/item?id=49260751", "旧 RSS 标题", articleURL, "Hacker News AI", "hacker-news", "app-fetch", "rss", "article", "builder", "2026-08-11T16:31:33Z", "旧摘要", "text", "", "", "", "", 0, "legacy-hash", "2026-08-11T16:32:00Z", "public_read", "public", "", "2026-08-11T16:32:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,updated_at) VALUES(?,1,1,'2026-08-11T16:32:00Z')`, "news:"+legacyID); err != nil {
		t.Fatal(err)
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "hacker-news"); err != nil {
		t.Fatal(err)
	}
	var entries int
	var storedID, externalID, storedURL, status string
	if err := db.QueryRow(`SELECT COUNT(*),MIN(id),MIN(external_id),MIN(url) FROM news WHERE source_id='hacker-news'`).Scan(&entries, &storedID, &externalID, &storedURL); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM feed_sources WHERE id='hacker-news'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	source, err := feedSourceByID("hacker-news")
	if err != nil {
		t.Fatal(err)
	}
	var starred int
	if err := db.QueryRow(`SELECT starred FROM item_preferences WHERE item_key=?`, "news:"+legacyID).Scan(&starred); err != nil {
		t.Fatal(err)
	}
	if entries != 1 || source.EntryCount != 1 || storedID != legacyID || externalID != "49260751" || storedURL != articleURL || starred != 1 || status != "ok" {
		t.Fatalf("entries=%d source_count=%d id=%q external_id=%q url=%q starred=%d status=%q", entries, source.EntryCount, storedID, externalID, storedURL, starred, status)
	}

	operationalSourceFetch = func(context.Context, string, string, string) (feedFetchResponse, error) {
		return feedFetchResponse{}, errors.New("来源返回 HTTP 503")
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "hacker-news"); err == nil {
		t.Fatal("expected local collector failure")
	}
	var retained int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='hacker-news'`).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if retained != entries {
		t.Fatalf("retained=%d, want %d", retained, entries)
	}
}

func TestParseFoloOperationalJSONAcceptsOnlyPinnedAccountsAndMediaHosts(t *testing.T) {
	source := FeedSource{ID: "elon-musk-x", Name: "Elon Musk", URL: operationalSourceSpecs["elon-musk-x"].endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/elonmusk","image":"https://pbs.twimg.com/profile_images/elon/avatar.jpg"},"entries":[
{"id":"one","title":"Photo","url":"https://x.com/elonmusk/status/111","summary":"photo summary","publishedAt":"2026-08-11T05:20:07Z","media":[{"url":"https://pbs.twimg.com/media/photo.jpg","type":"photo","width":1200,"height":1200},{"url":"https://evil.example/ignored.mp4","type":"video"}]},
{"id":"two","title":"Video","url":"https://twitter.com/elonmusk/status/222","summary":"video summary","authorAvatar":"https://pbs.twimg.com/profile_images/elon/entry-avatar.jpg","publishedAt":"2026-08-11T05:20:07Z","media":[{"url":"https://video.twimg.com/ext_tw_video/video.mp4","type":"video","width":1920,"height":1080,"preview_image_url":"https://pbs.twimg.com/ext_tw_video_thumb/video.jpg"},{"url":"https://evil.example/ignored.jpg","type":"photo"}]},
{"id":"three","title":"Other account","url":"https://x.com/notelon/status/333","summary":"must not persist","publishedAt":"2026-08-11T05:20:07Z"},
{"id":"four","title":"Bad status","url":"https://x.com/elonmusk/status/not-a-number","summary":"must not persist","publishedAt":"2026-08-11T05:20:07Z"}
]}}`)
	feed, err := parseFoloOperationalFeed(body, source, "elonmusk", operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	items := feed.Items
	if len(items) != 2 {
		t.Fatalf("items=%#v", items)
	}
	if feed.IconURL != "https://pbs.twimg.com/profile_images/elon/entry-avatar.jpg" || feed.SiteURL != "https://x.com/elonmusk" {
		t.Fatalf("feed icon/site=%q %q", feed.IconURL, feed.SiteURL)
	}
	if items[0].ExternalID != "111" || items[0].ThumbnailURL != "https://pbs.twimg.com/media/photo.jpg" || items[0].MediaURL != "" || items[0].SourceLevel != "public_aggregator" {
		t.Fatalf("photo item=%#v", items[0])
	}
	if items[1].ExternalID != "222" || items[1].ThumbnailURL != "https://pbs.twimg.com/ext_tw_video_thumb/video.jpg" || items[1].MediaURL != "https://video.twimg.com/ext_tw_video/video.mp4" || items[1].MediaMIME != "video/mp4" {
		t.Fatalf("video item=%#v", items[1])
	}
}

func TestFoloOperationalFeedRejectsUntrustedAvatarAndPreviewHosts(t *testing.T) {
	source := FeedSource{ID: "donald-trump-x", Name: "Donald Trump", URL: operationalSourceSpecs["donald-trump-x"].endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/realDonaldTrump","image":"https://images.example/avatar.jpg"},"entries":[{"id":"one","title":"Statement","url":"https://x.com/realDonaldTrump/status/111","authorAvatar":"https://images.example/entry-avatar.jpg","publishedAt":"2026-08-11T05:20:07Z","media":[{"url":"https://video.twimg.com/ext_tw_video/video.mp4","type":"video","preview_image_url":"https://images.example/video.jpg"}]}]}}`)
	feed, err := parseFoloOperationalFeed(body, source, "realDonaldTrump", operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.IconURL != "" || len(feed.Items) != 1 || feed.Items[0].ThumbnailURL != "" {
		t.Fatalf("untrusted media leaked into feed: %#v", feed)
	}
}

func TestFoloOperationalFeedUsesFeedPortraitWithoutEntryAvatar(t *testing.T) {
	source := FeedSource{ID: "dario-amodei-x", Name: "Dario Amodei", URL: operationalSourceSpecs["dario-amodei-x"].endpoints[0]}
	body := []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/DarioAmodei","image":"https://pbs.twimg.com/profile_images/dario/avatar.jpg"},"entries":[{"id":"one","title":"AI update","url":"https://x.com/DarioAmodei/status/111","publishedAt":"2026-08-11T05:20:07Z"}]}}`)
	feed, err := parseFoloOperationalFeed(body, source, "DarioAmodei", operationalSourceLimit)
	if err != nil {
		t.Fatal(err)
	}
	if feed.IconURL != "https://pbs.twimg.com/profile_images/dario/avatar.jpg" {
		t.Fatalf("feed portrait=%q", feed.IconURL)
	}
}

func TestXRSSOperationalFeedKeepsAllAvailablePostsAndPinsAccount(t *testing.T) {
	source := FeedSource{ID: "elon-musk-x", Name: "Elon Musk", URL: operationalSourceSpecs["elon-musk-x"].endpoints[0]}
	body := []byte(`<rss><channel><title>Twitter @Elon Musk</title><link>https://x.com/elonmusk</link><image><url>https://pbs.twimg.com/profile_images/elon/avatar.jpg</url></image>
<item><title>One</title><link>https://x.com/elonmusk/status/101</link><guid>https://twitter.com/elonmusk/status/101</guid><description>first</description></item>
<item><title>Two</title><link>https://x.com/elonmusk/status/102</link><guid>https://twitter.com/elonmusk/status/102</guid><description>second</description></item>
<item><title>Three</title><link>https://x.com/elonmusk/status/103</link><guid>https://twitter.com/elonmusk/status/103</guid><description>third</description></item>
<item><title>Four</title><link>https://x.com/elonmusk/status/104</link><guid>https://twitter.com/elonmusk/status/104</guid><description>fourth</description></item>
<item><title>Five</title><link>https://x.com/elonmusk/status/105</link><guid>https://twitter.com/elonmusk/status/105</guid><description>fifth</description></item>
<item><title>Other account</title><link>https://x.com/notelon/status/106</link><guid>https://twitter.com/notelon/status/106</guid><description>reject</description></item>
</channel></rss>`)
	feed, err := parseXRSSOperationalFeed(body, "https://rsshub.yfi.moe/twitter/user/elonmusk", source, "elonmusk", operationalXMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 5 || feed.SiteURL != "https://x.com/elonmusk" || feed.IconURL != "https://pbs.twimg.com/profile_images/elon/avatar.jpg" {
		t.Fatalf("feed=%#v", feed)
	}
	if feed.Items[0].ExternalID != "101" || feed.Items[4].ExternalID != "105" {
		t.Fatalf("items=%#v", feed.Items)
	}
}

func TestCollectXOperationalSourcePrefersMergedMirrorsOverFoloFallback(t *testing.T) {
	source := FeedSource{ID: "elon-musk-x", Name: "Elon Musk", URL: operationalSourceSpecs["elon-musk-x"].endpoints[0], RetentionLimit: 10000}
	spec := operationalSourceSpecs["elon-musk-x"]
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		switch {
		case strings.Contains(raw, "rsshub.yfi.moe"):
			return operationalFixtureResponse(raw, `<rss><channel><title>Twitter @Elon Musk</title><link>https://x.com/elonmusk</link><item><title>YFI one</title><link>https://x.com/elonmusk/status/201</link></item><item><title>Shared</title><link>https://x.com/elonmusk/status/203</link></item></channel></rss>`), nil
		case strings.Contains(raw, "rss.xxu.do"):
			return operationalFixtureResponse(raw, `<rss><channel><title>Twitter @Elon Musk</title><link>https://x.com/elonmusk</link><item><title>XXU two</title><link>https://x.com/elonmusk/status/202</link></item><item><title>Shared richer</title><link>https://x.com/elonmusk/status/203</link></item></channel></rss>`), nil
		default:
			return operationalFoloFixture(spec.foloHandle), nil
		}
	}
	result := collectXOperationalSource(context.Background(), source, spec, fetcher, time.Now().UTC().Format(time.RFC3339))
	if result.Status != "ok" || len(result.Feed.Items) != 3 {
		t.Fatalf("result=%#v", result)
	}
	ids := make(map[string]bool)
	for _, item := range result.Feed.Items {
		ids[item.ExternalID] = true
	}
	for _, id := range []string{"201", "202", "203"} {
		if !ids[id] {
			t.Fatalf("merged IDs=%v", ids)
		}
	}
}

func TestCollectXOperationalSourceBoundsSlowMirrorsAndReturnsFoloFallback(t *testing.T) {
	source := FeedSource{ID: "elon-musk-x", Name: "Elon Musk", URL: operationalSourceSpecs["elon-musk-x"].endpoints[0], RetentionLimit: 10000}
	spec := operationalSourceSpecs["elon-musk-x"]
	const (
		foloBudget   = 20 * time.Millisecond
		mirrorBudget = 30 * time.Millisecond
	)
	mirrors := xOperationalMirrorURLs(spec.foloHandle)
	calls := make([]string, 0, 3)
	mirrorWaits := make([]time.Duration, 0, len(mirrors))
	fetcher := func(ctx context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		if raw == spec.endpoints[0] {
			return operationalFoloFixture(spec.foloHandle), nil
		}
		started := time.Now()
		<-ctx.Done()
		mirrorWaits = append(mirrorWaits, time.Since(started))
		return feedFetchResponse{}, ctx.Err()
	}
	parent, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := collectXOperationalSourceWithBudgets(parent, source, spec, fetcher, time.Now().UTC().Format(time.RFC3339), foloBudget, mirrorBudget)
	elapsed := time.Since(started)

	if result.Status != "partial" || len(result.Feed.Items) != 1 || !strings.Contains(result.Message, "兼容读取") {
		t.Fatalf("fallback result=%#v", result)
	}
	if len(calls) != 3 || calls[0] != spec.endpoints[0] || calls[1] != mirrors[0] || calls[2] != mirrors[1] {
		t.Fatalf("fetch order=%v", calls)
	}
	if len(mirrorWaits) != 2 {
		t.Fatalf("mirror waits=%v", mirrorWaits)
	}
	for index, waited := range mirrorWaits {
		if waited < mirrorBudget/2 {
			t.Errorf("mirror %d reused an expired or unbounded context: waited %s", index, waited)
		}
	}
	if elapsed < mirrorBudget+mirrorBudget/2 || elapsed >= 250*time.Millisecond {
		t.Fatalf("bounded sequential mirror elapsed=%s, waits=%v", elapsed, mirrorWaits)
	}
}

func TestCollectXOperationalSourceFoloTimeoutPreservesFreshMirrorBudgets(t *testing.T) {
	source := FeedSource{ID: "elon-musk-x", Name: "Elon Musk", URL: operationalSourceSpecs["elon-musk-x"].endpoints[0], RetentionLimit: 10000}
	spec := operationalSourceSpecs["elon-musk-x"]
	const (
		foloBudget   = 25 * time.Millisecond
		mirrorBudget = 40 * time.Millisecond
	)
	mirrors := xOperationalMirrorURLs(spec.foloHandle)
	calls := make([]string, 0, 3)
	var foloWait time.Duration
	mirrorRemaining := make([]time.Duration, 0, len(mirrors))
	fetcher := func(ctx context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		if raw == spec.endpoints[0] {
			started := time.Now()
			<-ctx.Done()
			foloWait = time.Since(started)
			return feedFetchResponse{}, ctx.Err()
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			mirrorRemaining = append(mirrorRemaining, 0)
		} else {
			mirrorRemaining = append(mirrorRemaining, time.Until(deadline))
		}
		switch raw {
		case mirrors[0]:
			return operationalFixtureResponse(raw, `<rss><channel><title>Twitter @Elon Musk</title><link>https://x.com/elonmusk</link><item><title>Mirror one</title><link>https://x.com/elonmusk/status/301</link></item></channel></rss>`), nil
		case mirrors[1]:
			return operationalFixtureResponse(raw, `<rss><channel><title>Twitter @Elon Musk</title><link>https://x.com/elonmusk</link><item><title>Mirror two</title><link>https://x.com/elonmusk/status/302</link></item></channel></rss>`), nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	parent, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	result := collectXOperationalSourceWithBudgets(parent, source, spec, fetcher, time.Now().UTC().Format(time.RFC3339), foloBudget, mirrorBudget)
	elapsed := time.Since(started)

	if result.Status != "ok" || len(result.Feed.Items) != 2 {
		t.Fatalf("mirror result=%#v", result)
	}
	if len(calls) != 3 || calls[0] != spec.endpoints[0] || calls[1] != mirrors[0] || calls[2] != mirrors[1] {
		t.Fatalf("fetch order=%v", calls)
	}
	if foloWait < foloBudget/2 {
		t.Fatalf("Folo did not block until its budget: %s", foloWait)
	}
	if len(mirrorRemaining) != 2 {
		t.Fatalf("mirror deadlines=%v", mirrorRemaining)
	}
	for index, remaining := range mirrorRemaining {
		if remaining < mirrorBudget/2 || remaining > mirrorBudget*2 {
			t.Errorf("mirror %d did not receive a fresh bounded context: %s", index, remaining)
		}
	}
	if elapsed < foloBudget/2 || elapsed >= 250*time.Millisecond {
		t.Fatalf("bounded Folo timeout elapsed=%s", elapsed)
	}
}

func TestFoloKeyPeopleSyncPersistsPlayableVideoFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "donald-trump-x", "tibo-sottiaux-x")
	previousFetch := operationalSourceFetch
	operationalSourceFetch = operationalFixtureFetcher
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "elon-musk-x", Name: "Elon Musk", Group: "x", Status: "healthy"}}
	for _, item := range mock.catalog {
		mock.subs = append(mock.subs, sourceGatewaySubscription{SourceID: item.ID, Enabled: true, Status: "ready"})
	}
	mock.pages["elon-musk-x"] = [][]sourceGatewayEntry{{
		gatewayTestEntry("elon-musk-x", "s1", "AI 进展", "https://x.com/elonmusk/status/1", "x-h1", "2026-08-15T00:00:00Z"),
	}}
	mock.configure(t)

	for _, sourceID := range []string{"elon-musk-x", "donald-trump-x", "tibo-sottiaux-x"} {
		if err := syncLegacyPublicCollectorFixture(context.Background(), sourceID); err != nil {
			t.Fatalf("sync %s: %v", sourceID, err)
		}
	}

	// Production routes all three public sources through the gateway. This test
	// opts the latter two into an isolated parser fixture to preserve coverage of
	// their media metadata migration.
	var seededIcon int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE source_id='elon-musk-x'`).Scan(&seededIcon); err != nil {
		t.Fatal(err)
	}
	if seededIcon != 1 {
		t.Fatal("gateway sync must not remove the curated catalog row")
	}

	var mediaURL, previewURL, viewType string
	if err := db.QueryRow(`SELECT media_url,thumbnail_url,view_type FROM news WHERE source_id='donald-trump-x'`).Scan(&mediaURL, &previewURL, &viewType); err != nil {
		t.Fatal(err)
	}
	if mediaURL != "https://video.twimg.com/ext_tw_video/statement.mp4" || previewURL != "https://pbs.twimg.com/ext_tw_video_thumb/statement.jpg" || viewType != "video" {
		t.Fatalf("video=%q preview=%q view=%q", mediaURL, previewURL, viewType)
	}
}

func TestFoloSyncFailurePreservesExistingEntries(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "donald-trump-x")
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at) VALUES('existing-folo','100','existing','https://x.com/realDonaldTrump/status/100','Donald Trump','donald-trump-x','app-fetch','api','article','','2026-08-10T00:00:00Z','existing','text','','','','',0,'existing-hash','2026-08-10T00:00:00Z','public_read','public_aggregator','','2026-08-10T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	previous := operationalSourceFetch
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		if strings.Contains(raw, "api.folo.is/feeds") {
			return feedFetchResponse{Body: []byte(`{"code":0,"data":{"feed":{"siteUrl":"https://x.com/realDonaldTrump"},"entries":[{"id":"invalid","url":"https://x.com/notdonald/status/1"}]}}`)}, nil
		}
		return feedFetchResponse{}, errors.New("unexpected source")
	}
	t.Cleanup(func() { operationalSourceFetch = previous })
	if err := syncLegacyPublicCollectorFixture(context.Background(), "donald-trump-x"); err == nil {
		t.Fatal("expected Folo validation failure")
	}
	var entries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='donald-trump-x'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 1 {
		t.Fatalf("entries=%d; failed sync must retain existing content", entries)
	}
}

func TestFoloMigrationSeedsKeyPeopleDefaults(t *testing.T) {
	db = openTestDB(t)
	for _, sourceID := range []string{"elon-musk-x", "donald-trump-x", "tibo-sottiaux-x"} {
		var interval, backfillDays, retentionDays, retentionLimit int
		var folderID, category, packID string
		err := db.QueryRow(`
SELECT f.sync_interval_seconds, f.backfill_days, f.retention_days, f.retention_limit, s.folder_id, c.category, p.pack_id
FROM feed_sources f
JOIN feed_subscriptions s ON s.source_id=f.id
JOIN feed_catalog_entries c ON c.source_id=f.id
JOIN source_pack_members p ON p.source_id=f.id
		WHERE f.id=?`, sourceID).Scan(&interval, &backfillDays, &retentionDays, &retentionLimit, &folderID, &category, &packID)
		if err != nil {
			t.Fatalf("source %s: %v", sourceID, err)
		}
		wantInterval := 1800
		if interval != wantInterval || backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 10000 || folderID != "folder-key-people" || category != "social-media" || packID != "key-people" {
			t.Fatalf("source %s interval=%d backfill=%d retention_days=%d retention_limit=%d folder=%s category=%s pack=%s", sourceID, interval, backfillDays, retentionDays, retentionLimit, folderID, category, packID)
		}
	}
}

func TestOperationalSourceCollectorsCoverTheFixedRegistryFlow(t *testing.T) {
	previous := operationalSourceFetch
	previousCNInfo := cnInfoPageFetch
	previousSEC := secHistoryQuarterFetch
	operationalSourceFetch = operationalFixtureFetcher
	cnInfoPageFetch = func(_ context.Context, page, _ int) (feedFetchResponse, error) {
		return feedFetchResponse{Body: []byte(cnInfoHistoryFixture(page, 1)), ContentType: "application/json", FinalURL: cnInfoHistoryEndpoint}, nil
	}
	secHistoryQuarterFetch = func(_ context.Context, _ string, source FeedSource, _ int) (fetchedFeed, error) {
		item := makeOperationalNews(source, "0000123456-26-000001:8-K", "Example Corp · 8-K", "https://www.sec.gov/Archives/edgar/data/123456/000012345626000001/0000123456-26-000001-index.html", "2026-08-10T00:00:00Z", "8-K XBRL filing", "", "sec-xbrl", "official", "")
		return fetchedFeed{Title: source.Name, SiteURL: "https://www.sec.gov/", FeedURL: source.URL, Format: "idx", Items: []NewsItem{item}}, nil
	}
	t.Cleanup(func() {
		operationalSourceFetch = previous
		cnInfoPageFetch = previousCNInfo
		secHistoryQuarterFetch = previousSEC
	})
	for sourceID, spec := range operationalSourceSpecs {
		t.Run(sourceID, func(t *testing.T) {
			sourceName := sourceID
			if sourceID == "openai-news" {
				sourceName = "OpenAI News"
			}
			source := FeedSource{ID: sourceID, Name: sourceName, URL: spec.endpoints[0], Kind: "rss"}
			result, handled, err := collectOperationalSourceContext(context.Background(), source)
			if err != nil || !handled {
				t.Fatalf("handled=%v err=%v", handled, err)
			}
			if sourceID == "cnipa-patent-data" || sourceID == "wipo-patentscope" {
				if result.Status != "unavailable" || len(result.Feed.Items) != 0 {
					t.Fatalf("unverified patent fallback escaped: %#v", result)
				}
				return
			}
			if result.Status == "unavailable" || len(result.Feed.Items) == 0 {
				t.Fatalf("collector result = %#v", result)
			}
			for _, item := range result.Feed.Items {
				if item.PermissionScope != "public_read" || item.URL == "" || item.ContentHash == "" {
					t.Fatalf("unbounded item = %#v", item)
				}
			}
		})
	}
}

func TestPatentCollectorsRejectNewsAndStatisticsAsRecords(t *testing.T) {
	calls := make([]string, 0, 2)
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		calls = append(calls, raw)
		body := `<html><head><title>Official information</title></head><body><a href="/art/2026/1.html">Patent statistics</a><a href="https://www.wipo.int/pressroom/en/article/1">Patent news</a></body></html>`
		return feedFetchResponse{Body: []byte(body), ContentType: "text/html", FinalURL: raw}, nil
	}
	for _, sourceID := range []string{"cnipa-patent-data", "wipo-patentscope"} {
		spec := operationalSourceSpecs[sourceID]
		source := FeedSource{ID: sourceID, Name: sourceID, URL: spec.endpoints[0]}
		result := collectOperationalSourceContextWithFetcher(context.Background(), source, spec, fetcher)
		if result.Status != "unavailable" || len(result.Feed.Items) != 0 {
			t.Fatalf("source=%s result=%#v", sourceID, result)
		}
	}
	if len(calls) != 1 || calls[0] != operationalSourceSpecs["wipo-patentscope"].endpoints[0] {
		t.Fatalf("unexpected patent fallback calls: %v", calls)
	}
}

func TestOperationalSourceSyncWritesEntriesAndPreservesDashboardFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "govinfo-federal-register")
	previousFetch := operationalSourceFetch
	operationalSourceFetch = operationalFixtureFetcher
	t.Cleanup(func() { operationalSourceFetch = previousFetch })
	if _, err := db.Exec(`INSERT INTO dashboard_runs(generated_at,source_status,source_message,payload_json) VALUES('2026-08-10T00:00:00Z','ok','existing','{"actions":[{"id":"keep"}]}')`); err != nil {
		t.Fatal(err)
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "govinfo-federal-register"); err != nil {
		t.Fatal(err)
	}
	var entries, runs, used int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='govinfo-federal-register'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs WHERE source_message='existing'`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT records_used FROM feed_sources WHERE id='govinfo-federal-register'`).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if entries != operationalSourceLimit || runs != 1 || used != operationalSourceLimit {
		t.Fatalf("entries=%d dashboard_runs=%d records_used=%d", entries, runs, used)
	}
}

func TestPublicFeedRetryPolicyDoesNotRetryPermanentHTTP(t *testing.T) {
	if publicFetchRetryable(errors.New("来源返回 HTTP 403")) {
		t.Fatal("permanent HTTP error was marked retryable")
	}
	if !publicFetchRetryable(errors.New("来源返回 HTTP 503")) {
		t.Fatal("server HTTP error was not marked retryable")
	}
}

func TestPublicFeedRetryRecoversTransientFailureFlow(t *testing.T) {
	calls := 0
	fetch := func(_ context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
		calls++
		if calls == 1 {
			return feedFetchResponse{}, errors.New("temporary dial timeout")
		}
		return feedFetchResponse{FinalURL: raw, ETag: etag, LastModified: lastModified}, nil
	}
	response, err := fetchPublicURLWithRetryUsing(context.Background(), "https://example.com/feed", "etag", "modified", fetch)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || response.FinalURL != "https://example.com/feed" || response.ETag != "etag" {
		t.Fatalf("retry result calls=%d response=%#v", calls, response)
	}
}

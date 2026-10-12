package main

// The operational registry serves non-core defaults and explicitly added
// public sources. Core-source adapters remain here as migration reference for
// the server worker; source_gateway rows never route through these collectors.

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	htmlnode "golang.org/x/net/html"
)

const (
	operationalSourceLimit   = 20
	operationalMaxPages      = 3
	operationalMaxItems      = operationalSourceLimit * operationalMaxPages
	operationalMaxBytes      = 4 << 20
	operationalHTMLLinkLimit = operationalSourceLimit * 4
	operationalNodeTextLimit = 4000
	hackerNewsAISearchURL    = "https://hn.algolia.com/api/v1/search_by_date?tags=story&query=AI&hitsPerPage=50"
)

const (
	operationalPagingFeed       = "feed-next"
	operationalPagingArxiv      = "arxiv-offset"
	operationalPagingCrossref   = "crossref-offset"
	operationalPagingHackerNews = "hackernews-page"
	operationalPagingFederalReg = "federal-register-cursor"
	operationalPagingFolo       = "folo-continuation"
	operationalPagingFeedWindow = "feed-window"
	// X RSS mirrors return the complete currently available window in one RSS
	// document. They do not expose Folo's four-entry JSON page or a cursor.
	operationalPagingX        = "x-rss-window"
	operationalPagingBilibili = "bilibili-search-page"
	operationalPagingWechat   = "wechat-history-page"
)

const (
	// Keep the collector bounded for the board's 2 GB memory budget, while
	// removing the old 20/60-entry cap for X sources. The limit is applied to
	// the parsed RSS window, not to unread state; older entries stay in SQLite
	// and are served by the existing cursor API.
	operationalXPageSize = 10000
	operationalXMaxPages = 1
	operationalXMaxItems = operationalXPageSize * operationalXMaxPages

	// The old search/all endpoint is a relevance page, not a creator archive.
	// Use the larger public video-search window as the anonymous fallback.
	operationalBilibiliPageSize = 50
	operationalBilibiliMaxPages = 20
	// Merge two independent provider windows (click and publish-date). Each
	// window is capped at 1,000 by the public endpoint, so keep room for their
	// union before deduplication.
	operationalBilibiliMaxItems = operationalBilibiliPageSize * operationalBilibiliMaxPages * 2

	// Some fixed official RSS endpoints expose a larger complete window in one
	// response without a next-page cursor. Parse that bounded window in full.
	operationalFeedWindowPageSize = 100
	operationalFeedWindowMaxPages = 1
	operationalFeedWindowMaxItems = operationalFeedWindowPageSize

	// Fetch the small compatibility page first so it remains available even
	// when both RSS mirrors spend their full connection budgets. Mirror calls
	// stay sequential to keep peak response memory bounded on the 2 GB board.
	operationalXFoloBudget   = 5 * time.Second
	operationalXMirrorBudget = 3 * time.Second

	// YouTube's official Atom host is not reachable on every device network.
	// Keep each sequential attempt inside the source's 15-second budget and
	// cap the larger RSS history window so response memory stays bounded.
	operationalYouTubeMaxItems       = 50
	operationalYouTubeOfficialBudget = 5 * time.Second
	operationalYouTubeHistoryBudget  = 6 * time.Second
	operationalYouTubeFoloBudget     = 3 * time.Second
)

type operationalPagingPolicy struct {
	Kind     string
	PageSize int
	MaxPages int
	MaxItems int
	MaxBytes int
}

type operationalSourceSpec struct {
	parser           string
	endpoints        []string
	foloHandle       string
	youtubeChannelID string
	youtubeTitle     string
	bilibiliMID      int64
	bilibiliKeyword  string
	bilibiliTitle    string
	paging           operationalPagingPolicy
}

type operationalSourceResult struct {
	Feed                  fetchedFeed
	Status                string
	Message               string
	NotModified           bool
	Backfill              bool
	BackfillCursor        string
	BackfillCursorChanged bool
	NextSyncAfter         time.Duration
}

type operationalFetchFunc func(context.Context, string, string, string) (feedFetchResponse, error)

var operationalSourceFetch operationalFetchFunc = sseAwareOperationalFetch

var operationalSourceSpecs = map[string]operationalSourceSpec{
	"openai-news": {
		parser:    "trusted-feed",
		endpoints: []string{"https://openai.com/news/rss.xml"},
	},
	"hacker-news": {
		parser:    "hackernews",
		endpoints: []string{hackerNewsAISearchURL},
		paging:    boundedOperationalPaging(operationalPagingHackerNews),
	},
	"sec-xbrl-filings": {
		parser:    "sec",
		endpoints: []string{secHistoryEndpoint},
	},
	"hkex-regulatory-announcements": {
		parser:    "feed",
		endpoints: []string{"https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK"},
		paging:    boundedOperationalPaging(operationalPagingFeed),
	},
	"cninfo-disclosures": {
		parser:    "cninfo",
		endpoints: []string{cnInfoHistoryEndpoint},
	},
	"sse-periodic-reports": {
		parser:    "sse-history",
		endpoints: []string{sseHistoryEndpoint},
	},
	"govinfo-federal-register": {
		parser:    "federal-register",
		endpoints: []string{"https://www.federalregister.gov/api/v1/documents.json?per_page=20&order=newest"},
		paging:    boundedOperationalPaging(operationalPagingFederalReg),
	},
	"eurlex-legislation": {
		parser:    "feed",
		endpoints: []string{"https://eur-lex.europa.eu/EN/display-feed.rss?rssId=162"},
		paging: operationalPagingPolicy{
			Kind: operationalPagingFeedWindow, PageSize: operationalFeedWindowPageSize,
			MaxPages: operationalFeedWindowMaxPages, MaxItems: operationalFeedWindowMaxItems, MaxBytes: operationalMaxBytes,
		},
	},
	"china-policy-library": {
		parser:    "policy",
		endpoints: []string{"https://www.gov.cn/zhengce/zuixin/ZUIXINZHENGCE.json"},
	},
	"arxiv-ai": {
		parser:    "feed",
		endpoints: []string{"https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending"},
		paging:    boundedOperationalPaging(operationalPagingArxiv),
	},
	"crossref-research": {
		parser:    "crossref",
		endpoints: []string{"https://api.crossref.org/works"},
		paging:    boundedOperationalPaging(operationalPagingCrossref),
	},
	"epo-publication-server": {
		parser:    "epo",
		endpoints: []string{"https://data.epo.org/publication-server/rest/v1.2/publication-dates"},
	},
	"cnipa-patent-data": {
		parser:    "cnipa",
		endpoints: []string{"https://ggfw.cnipa.gov.cn"},
	},
	"wipo-patentscope": {
		parser:    "wipo",
		endpoints: []string{"https://patentscope.wipo.int/search/en/search.jsf"},
	},
	"elon-musk-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Felonmusk"},
		foloHandle: "elonmusk",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"donald-trump-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FrealDonaldTrump"},
		foloHandle: "realDonaldTrump",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"sam-altman-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fsama"},
		foloHandle: "sama",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"dario-amodei-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FDarioAmodei"},
		foloHandle: "DarioAmodei",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"demis-hassabis-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fdemishassabis"},
		foloHandle: "demishassabis",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"jensen-huang-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2FJensenHuang"},
		foloHandle: "JensenHuang",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"satya-nadella-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fsatyanadella"},
		foloHandle: "satyanadella",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"andrej-karpathy-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fkarpathy"},
		foloHandle: "karpathy",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	"tibo-sottiaux-x": {
		parser:     "folo",
		endpoints:  []string{"https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.app%2Ftwitter%2Fuser%2Fthsottiaux"},
		foloHandle: "thsottiaux",
		paging:     boundedOperationalPaging(operationalPagingFolo),
	},
	// YouTube channel feeds come from the official bounded Atom feed. The
	// channel ID and returned title are pinned so a provider response cannot
	// silently switch the user's subscription to a different channel.
	"youtube-google-deepmind":   youtubeOperationalSpec("UCP7jMXSY2xbc3KCAE0MHQ-A", "Google DeepMind"),
	"youtube-anthropic":         youtubeOperationalSpec("UCrDwWp7EBBv4NwvScIpBDOA", "Anthropic"),
	"youtube-nvidia":            youtubeOperationalSpec("UCHuiy8bXnmK5nisYHUd1J5g", "NVIDIA"),
	"youtube-two-minute-papers": youtubeOperationalSpec("UCbfYPyITQ-7l4upoX8nvctg", "Two Minute Papers"),
	"youtube-ai-explained":      youtubeOperationalSpec("UCNJ1Ymd5yFuUPtn21xtRbbw", "AI Explained"),
	"youtube-computerphile":     youtubeOperationalSpec("UC9-y-6csu5WGm29I7JiwpnA", "Computerphile"),
	"youtube-fireship":          youtubeOperationalSpec("UCsBjURrPoezykLs9EqgamOA", "Fireship"),
	"youtube-lex-fridman":       youtubeOperationalSpec("UCSHZKyawb77ixDdsGog4iWA", "Lex Fridman"),
	"bilibili-geekerwan":        bilibiliOperationalSpec(25876945, "极客湾", "极客湾Geekerwan"),
	"bilibili-tech-aesthetics":  bilibiliOperationalSpec(3766866, "科技美学", "科技美学"),
	"bilibili-yingshijufeng":    bilibiliOperationalSpec(946974, "影视飓风", "影视飓风"),
	"bilibili-machine-heart":    bilibiliOperationalSpec(73414544, "机器之心", "机器之心官方"),
	"bilibili-qbit":             bilibiliOperationalSpec(673779175, "量子位", "量子位"),
	"bilibili-geekpark":         bilibiliOperationalSpec(13260662, "极客公园", "极客公园"),
	"bilibili-hardware-tea":     bilibiliOperationalSpec(14871346, "硬件茶谈", "硬件茶谈"),
	"bilibili-he-tongxue":       bilibiliOperationalSpec(163637592, "老师好我叫何同学", "老师好我叫何同学"),
	"bilibili-li-mu-ai":         bilibiliOperationalSpec(1567748478, "跟李沐学AI", "跟李沐学AI"),
	"bilibili-ai-tech-review":   bilibiliOperationalSpec(216720985, "AI科技评论", "AI科技评论"),
}

func boundedOperationalPaging(kind string) operationalPagingPolicy {
	return operationalPagingPolicy{
		Kind:     kind,
		PageSize: operationalSourceLimit,
		MaxPages: operationalMaxPages,
		MaxItems: operationalMaxItems,
		MaxBytes: operationalMaxBytes,
	}
}

func xOperationalPaging() operationalPagingPolicy {
	return operationalPagingPolicy{
		Kind:     operationalPagingX,
		PageSize: operationalXPageSize,
		MaxPages: operationalXMaxPages,
		MaxItems: operationalXMaxItems,
		MaxBytes: operationalMaxBytes,
	}
}

func youtubeOperationalSpec(channelID, title string) operationalSourceSpec {
	feedURL := "https://www.youtube.com/feeds/videos.xml?channel_id=" + channelID
	return operationalSourceSpec{
		parser: "youtube",
		// The fixed RSS history window is tried before the smaller Folo page.
		// Neither fallback changes the canonical URL stored for the user.
		endpoints:        []string{feedURL, youtubeHistoryFeedURL(channelID), youtubeFoloFeedURL(feedURL)},
		youtubeChannelID: channelID,
		youtubeTitle:     title,
		paging:           boundedOperationalPaging(operationalPagingFeed),
	}
}

func youtubeHistoryFeedURL(channelID string) string {
	if !youtubeChannelIDRE.MatchString(channelID) {
		return ""
	}
	return "https://rss.peachyjoy.top/youtube/channel/" + channelID
}

func bilibiliOperationalSpec(mid int64, keyword, title string) operationalSourceSpec {
	searchURL := func(order string) string {
		return "https://api.bilibili.com/x/web-interface/search/type?keyword=" + url.QueryEscape(keyword) + "&search_type=video&order=" + url.QueryEscape(order) + "&page=1&page_size=" + strconv.Itoa(operationalBilibiliPageSize)
	}
	return operationalSourceSpec{
		parser:          "bilibili",
		endpoints:       []string{searchURL("click"), searchURL("pubdate")},
		bilibiliMID:     mid,
		bilibiliKeyword: keyword,
		bilibiliTitle:   title,
		paging: operationalPagingPolicy{
			Kind:     operationalPagingBilibili,
			PageSize: operationalBilibiliPageSize,
			MaxPages: operationalBilibiliMaxPages,
			MaxItems: operationalBilibiliMaxItems,
			MaxBytes: operationalMaxBytes,
		},
	}
}

func bilibiliFoloFeedURL(mid int64) string {
	route := fmt.Sprintf("https://rsshub.app/bilibili/user/video/%d", mid)
	return "https://api.folo.is/feeds?url=" + url.QueryEscape(route)
}

func youtubeFoloFeedURL(feedURL string) string {
	// Kept for compatibility with older local databases that may still expose
	// the aggregator URL in diagnostics; new subscriptions use the official
	// YouTube Atom endpoint above.
	return "https://api.folo.is/feeds?url=" + url.QueryEscape(feedURL)
}

var publicFetchHTTPStatusRE = regexp.MustCompile(`HTTP (\d{3})`)
var operationalEPODatesRE = regexp.MustCompile(`publication-dates/(\d{8})/patents`)
var operationalEPOPatentRE = regexp.MustCompile(`/patents/([A-Za-z0-9]+)`)
var operationalFoloXRouteRE = regexp.MustCompile(`^/(?:twitter|x)/user/([A-Za-z0-9_]{1,30})/?$`)
var operationalXHandleRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)
var secAccessionRE = regexp.MustCompile(`^[0-9]{10}-[0-9]{2}-[0-9]{6}$`)
var secCIKRE = regexp.MustCompile(`^[0-9]{1,10}$`)

func fetchPublicURLWithRetry(ctx context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
	return fetchPublicURLWithRetryUsing(ctx, raw, etag, lastModified, fetchPublicURLWithCache)
}

func fetchPublicURLWithRetryUsing(ctx context.Context, raw, etag, lastModified string, fetch operationalFetchFunc) (feedFetchResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, err := fetch(ctx, raw, etag, lastModified)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !publicFetchRetryable(err) || attempt == 1 {
			break
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return feedFetchResponse{}, ctx.Err()
		case <-timer.C:
		}
	}
	return feedFetchResponse{}, lastErr
}

func publicFetchRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	match := publicFetchHTTPStatusRE.FindStringSubmatch(err.Error())
	if len(match) == 2 {
		status := match[1]
		return status == "408" || status == "425" || status == "429" || strings.HasPrefix(status, "5")
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "temporary", "connection reset", "connection refused", "unexpected eof", "tls handshake"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func collectOperationalSourceContext(ctx context.Context, source FeedSource) (operationalSourceResult, bool, error) {
	if result, handled, err := collectWeChatDiscoveredSourceContext(ctx, source); handled {
		return result, true, err
	}
	spec, ok := operationalSourceSpecs[source.ID]
	if !ok {
		// User-added X subscriptions are still public Folo/RSSHub feeds, but
		// they do not have a fixed catalog ID.  Route only the narrowly
		// allowlisted Twitter/X RSSHub shape through the same pinned parser so
		// the response account and every status URL remain verified.
		if handle, endpoint, valid := dynamicFoloXSource(source.URL); valid {
			spec = operationalSourceSpec{
				parser:     "folo",
				endpoints:  []string{endpoint},
				foloHandle: handle,
				paging:     xOperationalPaging(),
			}
			return collectOperationalSourceContextWithFetcher(ctx, source, spec, operationalSourceFetch), true, nil
		}
		return operationalSourceResult{}, false, nil
	}
	return collectOperationalSourceContextWithFetcher(ctx, source, spec, operationalSourceFetch), true, nil
}

func xOperationalMirrorURLs(handle string) []string {
	handle = strings.TrimSpace(handle)
	if !operationalXHandleRE.MatchString(handle) {
		return nil
	}
	pathHandle := url.PathEscape(handle)
	return []string{
		"https://rsshub.yfi.moe/twitter/user/" + pathHandle,
		"https://rss.xxu.do/twitter/user/" + pathHandle,
	}
}

func dynamicFoloXSource(raw string) (handle, endpoint string, ok bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	feedURL := raw
	if host == "api.folo.is" && parsed.Path == "/feeds" {
		feedURL = parsed.Query().Get("url")
		if feedURL == "" {
			return "", "", false
		}
		parsedFeed, parseErr := url.Parse(feedURL)
		if parseErr != nil || parsedFeed.Scheme != "https" || parsedFeed.User != nil || parsedFeed.Port() != "" || parsedFeed.RawQuery != "" || parsedFeed.Fragment != "" {
			return "", "", false
		}
		parsed = parsedFeed
		host = strings.ToLower(parsed.Hostname())
	} else if host != "rsshub.app" && host != "rsshub.rssforever.com" {
		return "", "", false
	}
	if host != "rsshub.app" && host != "rsshub.rssforever.com" {
		return "", "", false
	}
	match := operationalFoloXRouteRE.FindStringSubmatch(parsed.Path)
	if len(match) != 2 {
		return "", "", false
	}
	if !strings.HasPrefix(strings.ToLower(feedURL), "https://api.folo.is/feeds?") {
		endpoint = youtubeFoloFeedURL(parsed.String())
	} else {
		endpoint = strings.TrimSpace(raw)
	}
	return match[1], endpoint, true
}

func collectOperationalSourceContextWithFetcher(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc) operationalSourceResult {
	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	switch spec.parser {
	case "trusted-feed":
		return collectTrustedHistoricalFeed(ctx, source, spec, fetcher)
	case "sec":
		return collectSECHistoryOperational(ctx, source, fetcher, fetchedAt)
	case "feed":
		var lastErr error
		for _, endpoint := range spec.endpoints {
			endpointSpec := spec
			endpointSpec.endpoints = []string{endpoint}
			result := collectPagedOperationalSource(ctx, source, endpointSpec, fetcher, fetchedAt)
			if result.Status != "unavailable" {
				return result
			}
			lastErr = errors.New(result.Message)
		}
		return operationalSourceResult{Status: "unavailable", Message: limitText("来源读取失败："+errorText(lastErr), 500)}
	case "sse-history":
		return collectSSEHistoryOperational(ctx, source, func(pageCtx context.Context, raw string) (feedFetchResponse, error) {
			return fetcher(pageCtx, raw, "", "")
		}, fetchedAt)
	case "policy":
		response, err := fetcher(ctx, spec.endpoints[0], "", "")
		if err != nil {
			return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
		}
		items, err := parsePolicyOperationalJSON(response.Body, source, fetchedAt, trustedHistoricalItemLimit(source))
		if err != nil {
			return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
		}
		return operationalFeedResultWithLimit(makeOperationalFeed(source, items, "api"), "ok", "", response, trustedHistoricalItemLimit(source))
	case "crossref":
		return collectPagedOperationalSource(ctx, source, spec, fetcher, fetchedAt)
	case "hackernews":
		return collectPagedOperationalSource(ctx, source, spec, fetcher, fetchedAt)
	case "federal-register":
		return collectPagedOperationalSource(ctx, source, spec, fetcher, fetchedAt)
	case "folo":
		return collectXOperationalSource(ctx, source, spec, fetcher, fetchedAt)
	case "youtube":
		return collectYouTubeOperationalSource(ctx, source, spec, fetcher)
	case "bilibili":
		return collectBilibiliOperationalSource(ctx, source, spec, fetcher, fetchedAt)
	case "wechat-history":
		return collectJintiankanshaWechatSource(ctx, source, spec, fetcher, fetchedAt)
	case "epo":
		return collectEPOHistoryOperational(ctx, source, spec, fetcher, fetchedAt)
	case "cninfo":
		return collectCNInfoOperational(ctx, source, cnInfoPageFetch)
	case "cnipa":
		return operationalSourceResult{Status: "unavailable", Message: limitText("CNIPA 匿名查询无法稳定返回完整专利历史；保留已有内容，不使用关键词片段冒充全量数据", 500)}
	case "wipo":
		primary := collectHTMLOperational(ctx, source, operationalSourceSpec{endpoints: spec.endpoints[:1]}, fetcher, fetchedAt, func(link string) bool {
			lower := strings.ToLower(link)
			return !strings.Contains(link, "#") && (strings.Contains(lower, "/publication/") || strings.Contains(lower, "/document/") || strings.Contains(lower, "/result/") || strings.Contains(lower, "?query="))
		}, "web", "ok", "")
		if primary.Status == "ok" {
			return primary
		}
		return operationalSourceResult{Status: "unavailable", Message: limitText("WIPO PATENTSCOPE 没有返回可匿名验证的专利记录；新闻 RSS 不作为专利数据降级", 500)}
	}
	return operationalSourceResult{Status: "unavailable", Message: "来源解析器未配置"}
}

func trustedHistoricalItemLimit(source FeedSource) int {
	limit := feedFetchTrustedMaxItems
	if source.RetentionLimit > 0 && source.RetentionLimit < limit {
		limit = source.RetentionLimit
	}
	return limit
}

func collectTrustedHistoricalFeed(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc) operationalSourceResult {
	if source.ID != "openai-news" || len(spec.endpoints) != 1 || source.URL != spec.endpoints[0] {
		return operationalSourceResult{Status: "unavailable", Message: "固定历史来源配置无效"}
	}
	response, err := fetcher(ctx, spec.endpoints[0], source.ETag, source.LastModified)
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	if response.NotModified {
		return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: response.ETag, LastModified: response.LastModified}}
	}
	finalURL, parseErr := url.Parse(firstNonEmpty(response.FinalURL, spec.endpoints[0]))
	if parseErr != nil || finalURL.Scheme != "https" || finalURL.User != nil || finalURL.Port() != "" || finalURL.RawQuery != "" || finalURL.Fragment != "" ||
		(finalURL.Hostname() != "openai.com" && finalURL.Hostname() != "www.openai.com") || finalURL.Path != "/news/rss.xml" {
		return operationalSourceResult{Status: "unavailable", Message: "固定历史来源重定向地址无效"}
	}
	feed, err := parseXMLFeedWithLimit(response.Body, spec.endpoints[0], trustedHistoricalItemLimit(source))
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	siteURL, siteErr := url.Parse(feed.SiteURL)
	if siteErr != nil || (siteURL.Hostname() != "openai.com" && siteURL.Hostname() != "www.openai.com") || !strings.EqualFold(strings.TrimSpace(feed.Title), source.Name) {
		return operationalSourceResult{Status: "unavailable", Message: "固定历史来源返回的站点身份不匹配"}
	}
	feed.FeedURL = source.URL
	return operationalFeedResultWithLimit(feed, "ok", "", response, trustedHistoricalItemLimit(source))
}

// collectXOperationalSource keeps the Folo JSON page only as a fallback. It is
// fetched first because its small independent budget must not be consumed by
// slow RSS mirrors. A valid mirror window still wins because Folo currently
// returns only a few entries and exposes no continuation cursor.
func collectXOperationalSource(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	return collectXOperationalSourceWithBudgets(ctx, source, spec, fetcher, fetchedAt, operationalXFoloBudget, operationalXMirrorBudget)
}

func collectXOperationalSourceWithBudgets(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string, foloBudget, mirrorBudget time.Duration) operationalSourceResult {
	policy := normalizedOperationalPaging(xOperationalPaging(), source.RetentionLimit)
	var fallbackFeed fetchedFeed
	var fallbackResponse feedFetchResponse
	fallbackNotModified := false
	fallbackError := ""
	if len(spec.endpoints) > 0 {
		foloCtx, cancel := context.WithTimeout(ctx, foloBudget)
		response, err := fetcher(foloCtx, spec.endpoints[0], source.ETag, source.LastModified)
		cancel()
		if err != nil {
			fallbackError = errorText(err)
		} else if response.NotModified {
			fallbackNotModified = true
			fallbackResponse = response
		} else {
			feed, parseErr := parseFoloOperationalFeed(response.Body, source, spec.foloHandle, policy.MaxItems)
			if parseErr != nil {
				fallbackError = errorText(parseErr)
			} else {
				fallbackFeed = feed
				fallbackResponse = response
			}
		}
	}

	mirrors := xOperationalMirrorURLs(spec.foloHandle)
	merged := fetchedFeed{}
	seen := make(map[string]struct{}, policy.MaxItems)
	var firstResponse feedFetchResponse
	var mirrorErrors []string
	for _, endpoint := range mirrors {
		mirrorCtx, cancel := context.WithTimeout(ctx, mirrorBudget)
		response, err := fetcher(mirrorCtx, endpoint, "", "")
		cancel()
		if err != nil {
			mirrorErrors = append(mirrorErrors, endpoint+": "+errorText(err))
			continue
		}
		parsed, parseErr := parseXRSSOperationalFeed(response.Body, response.FinalURL, source, spec.foloHandle, policy.MaxItems)
		if parseErr != nil {
			mirrorErrors = append(mirrorErrors, endpoint+": "+errorText(parseErr))
			continue
		}
		if firstResponse.FinalURL == "" {
			firstResponse = feedFetchResponse{FinalURL: response.FinalURL, ETag: response.ETag, LastModified: response.LastModified}
		}
		appendOperationalFeedPage(&merged, parsed, seen, policy.MaxItems)
	}
	if len(merged.Items) > 0 {
		merged.FeedURL = source.URL
		merged.Format = "rss"
		status, message := "ok", ""
		if len(mirrorErrors) > 0 {
			status = "partial"
			message = "X RSS 镜像部分不可用，已合并仍可访问的公开内容"
		}
		return operationalFeedResultWithLimit(merged, status, message, firstResponse, policy.MaxItems)
	}

	// Keep existing installations functional if both mirrors are temporarily
	// down. The compatibility page is never reported as a complete source.
	if fallbackNotModified {
		return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: fallbackResponse.ETag, LastModified: fallbackResponse.LastModified}}
	}
	if len(fallbackFeed.Items) > 0 {
		message := "X RSS 镜像暂不可用，已降级到兼容读取；该兼容源可能只返回最近少量内容"
		return operationalFeedResultWithLimit(fallbackFeed, "partial", message, fallbackResponse, policy.MaxItems)
	}
	if fallbackError != "" {
		mirrorErrors = append(mirrorErrors, "兼容读取："+fallbackError)
	}
	if len(mirrorErrors) == 0 {
		mirrorErrors = append(mirrorErrors, "X RSS 镜像没有返回可用条目")
	}
	return operationalSourceResult{Status: "unavailable", Message: limitText("X 公开来源读取失败："+strings.Join(mirrorErrors, "；"), 500)}
}

// collectBilibiliOperationalSource follows the same provider-first shape as
// Folo: try the provider's RSSHub feed through its feed adapter, then keep the
// official anonymous search API as a second source. The two responses are
// merged by BV id so an adapter outage does not turn a valid subscription into
// an empty source.
func collectBilibiliOperationalSource(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	policy := normalizedOperationalPaging(spec.paging, source.RetentionLimit)
	currentURL := strings.TrimSpace(source.LastModified)
	if !validBilibiliSearchURL(currentURL, spec) {
		currentURL = spec.endpoints[0]
	}
	response, err := fetcher(ctx, currentURL, "", "")
	if err != nil {
		fallbackURL := bilibiliFoloFeedURL(spec.bilibiliMID)
		fallbackResponse, fallbackErr := fetcher(ctx, fallbackURL, "", "")
		if fallbackErr == nil {
			if fallbackFeed, fallbackParseErr := parseBilibiliFoloOperationalFeed(fallbackResponse.Body, source, spec, policy.PageSize); fallbackParseErr == nil {
				result := operationalFeedResultWithLimit(fallbackFeed, "partial", "Bilibili 官方接口暂时不可达，已降级到兼容 feed", fallbackResponse, policy.PageSize)
				result.Feed.FeedURL = source.URL
				result.Feed.Format = "bilibili"
				return result
			}
		}
		return operationalSourceResult{Status: "unavailable", Message: limitText("Bilibili 当前页读取失败："+errorText(err), 500)}
	}
	feed, nextURL, parseErr := parseOperationalSourcePage(response, currentURL, source, spec, fetchedAt, policy, 0)
	if parseErr != nil && strings.Contains(currentURL, "order=click") && len(spec.endpoints) > 1 {
		// A keyword can have no matching creator rows in one ranking window
		// even though the publish-date window contains valid uploads.
		fallbackURL := spec.endpoints[1]
		fallbackResponse, fallbackErr := fetcher(ctx, fallbackURL, "", "")
		if fallbackErr == nil {
			if fallbackFeed, fallbackNext, fallbackParseErr := parseOperationalSourcePage(fallbackResponse, fallbackURL, source, spec, fetchedAt, policy, 0); fallbackParseErr == nil {
				response, feed, nextURL, parseErr = fallbackResponse, fallbackFeed, fallbackNext, nil
			}
		}
	}
	if parseErr != nil {
		fallbackURL := bilibiliFoloFeedURL(spec.bilibiliMID)
		fallbackResponse, fallbackErr := fetcher(ctx, fallbackURL, "", "")
		if fallbackErr == nil {
			if fallbackFeed, fallbackParseErr := parseBilibiliFoloOperationalFeed(fallbackResponse.Body, source, spec, policy.PageSize); fallbackParseErr == nil {
				result := operationalFeedResultWithLimit(fallbackFeed, "partial", "Bilibili 官方接口解析失败，已降级到兼容 feed", fallbackResponse, policy.PageSize)
				result.Feed.FeedURL = source.URL
				result.Feed.Format = "bilibili"
				return result
			}
		}
		return operationalSourceResult{Status: "unavailable", Message: limitText("Bilibili 当前页解析失败："+errorText(parseErr), 500)}
	}
	// After the click-sorted window, continue with the publish-date window.
	// This gives the two public windows a stable cursor without issuing all
	// forty requests inside one source timeout.
	if nextURL == "" && strings.Contains(currentURL, "order=click") && len(spec.endpoints) > 1 {
		nextURL = spec.endpoints[1]
	}
	status, message := "ok", ""
	if nextURL != "" {
		status = "partial"
		message = "已读取当前页；滑动到底部后继续读取更早视频"
	}
	result := operationalFeedResultWithLimit(feed, status, message, response, policy.PageSize)
	result.Feed.LastModified = nextURL
	result.Feed.FeedURL = source.URL
	result.Feed.Format = "bilibili"
	return result
}

func validBilibiliSearchURL(raw string, spec operationalSourceSpec) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "api.bilibili.com" || parsed.Path != "/x/web-interface/search/type" || parsed.Query().Get("keyword") != spec.bilibiliKeyword {
		return false
	}
	return parsed.Query().Get("search_type") == "video" && (parsed.Query().Get("order") == "click" || parsed.Query().Get("order") == "pubdate")
}

func collectYouTubeOperationalSource(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc) operationalSourceResult {
	return collectYouTubeOperationalSourceWithBudgets(ctx, source, spec, fetcher, operationalYouTubeOfficialBudget, operationalYouTubeHistoryBudget, operationalYouTubeFoloBudget)
}

func collectYouTubeOperationalSourceWithBudgets(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, officialBudget, historyBudget, foloBudget time.Duration) operationalSourceResult {
	if len(spec.endpoints) != 3 || strings.TrimSpace(spec.endpoints[0]) == "" || strings.TrimSpace(spec.endpoints[1]) == "" || strings.TrimSpace(spec.endpoints[2]) == "" {
		return operationalSourceResult{Status: "unavailable", Message: "YouTube 来源地址配置无效"}
	}
	limit := operationalYouTubeMaxItems
	if source.RetentionLimit > 0 && source.RetentionLimit < limit {
		limit = source.RetentionLimit
	}
	budgets := []time.Duration{officialBudget, historyBudget, foloBudget}
	errorsByEndpoint := make([]string, 0, len(spec.endpoints))
	for index, endpoint := range spec.endpoints {
		requestCtx, cancel := context.WithTimeout(ctx, budgets[index])
		etag, lastModified := "", ""
		if index == 0 {
			etag, lastModified = source.ETag, source.LastModified
		}
		response, err := fetcher(requestCtx, endpoint, etag, lastModified)
		cancel()
		if err != nil {
			errorsByEndpoint = append(errorsByEndpoint, errorText(err))
			continue
		}
		if response.NotModified {
			if index == 0 {
				return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: response.ETag, LastModified: response.LastModified}}
			}
			errorsByEndpoint = append(errorsByEndpoint, "兼容来源意外返回未修改状态")
			continue
		}
		feed, parseErr := parseYouTubeOperationalFeed(response.Body, source, spec, limit)
		if parseErr != nil {
			errorsByEndpoint = append(errorsByEndpoint, errorText(parseErr))
			continue
		}
		if index == 0 {
			return operationalFeedResultWithLimit(feed, "ok", "", response, limit)
		}
		message := "YouTube 官方 Atom 暂时不可用，已降级到历史兼容 feed"
		if index == 2 {
			message = "YouTube 官方 Atom 与历史兼容 feed 暂时不可用，已降级到兼容 feed"
		}
		// Keep the validators from the canonical Atom endpoint. Compatibility
		// response headers must never replace them, and this result does not
		// mutate the independent cross-sync backfill cursor.
		canonicalCache := feedFetchResponse{ETag: source.ETag, LastModified: source.LastModified}
		return operationalFeedResultWithLimit(feed, "partial", message, canonicalCache, limit)
	}
	return operationalSourceResult{Status: "unavailable", Message: limitText("YouTube 公开来源读取失败："+strings.Join(errorsByEndpoint, "；"), 500)}
}

func collectPagedOperationalSource(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	policy := normalizedOperationalPaging(spec.paging, source.RetentionLimit)
	cursorSupported := operationalSourceSupportsBackfillCursor(policy.Kind)
	resumeCursor, resumeValid := validOperationalBackfillCursor(source.BackfillCursor, spec, policy)
	backfill := operationalSourceNeedsBackfill(source, policy) || resumeValid || (cursorSupported && strings.TrimSpace(source.BackfillCursor) != "")
	pageLimit := 1
	if backfill {
		pageLimit = policy.MaxPages
	}
	currentURL := operationalFirstPageURL(spec, fetchedAt, policy)
	if currentURL == "" {
		return operationalSourceResult{Status: "unavailable", Message: "来源分页地址无效"}
	}

	feed := fetchedFeed{}
	seenItems := make(map[string]struct{}, policy.MaxItems)
	seenPages := make(map[string]struct{}, pageLimit)
	var firstResponse feedFetchResponse
	totalBytes := 0
	status, message := "ok", ""
	readingHistory := !resumeValid
	nextCursor := ""
	for page := 0; page < pageLimit && currentURL != "" && len(feed.Items) < policy.MaxItems; page++ {
		if _, duplicate := seenPages[currentURL]; duplicate {
			status, message = operationalPartialResult(feed, "来源分页出现循环")
			currentURL = ""
			break
		}
		seenPages[currentURL] = struct{}{}
		etag, lastModified := "", ""
		if page == 0 && !backfill && spec.parser == "feed" {
			etag, lastModified = source.ETag, source.LastModified
		}
		response, err := fetcher(ctx, currentURL, etag, lastModified)
		fallbackMessage := ""
		if err != nil && page == 0 && spec.parser == "youtube" && len(spec.endpoints) > 1 {
			// YouTube's public Atom host is frequently unreachable from a
			// device network even when the rest of the internet is available.
			// Try the bounded compatibility endpoint before marking the source
			// unavailable, while retaining the official URL as source identity.
			for _, fallbackURL := range spec.endpoints[1:] {
				if strings.TrimSpace(fallbackURL) == "" || fallbackURL == currentURL {
					continue
				}
				fallbackResponse, fallbackErr := fetcher(ctx, fallbackURL, "", "")
				if fallbackErr == nil {
					response, err, currentURL = fallbackResponse, nil, fallbackURL
					fallbackMessage = "YouTube 官方 Atom 暂时不可达，已降级到兼容 feed"
					break
				}
			}
		}
		if err != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页读取失败："+errorText(err))
			nextCursor = currentURL
			break
		}
		if page == 0 {
			firstResponse = feedFetchResponse{
				FinalURL:     response.FinalURL,
				ETag:         response.ETag,
				LastModified: response.LastModified,
			}
			if response.NotModified {
				return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: response.ETag, LastModified: response.LastModified}}
			}
		} else if response.NotModified {
			break
		}
		if len(response.Body) > policy.MaxBytes-totalBytes {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: "来源响应超过回填字节上限"}
			}
			status, message = operationalPartialResult(feed, "已达到来源回填字节上限")
			nextCursor = currentURL
			break
		}
		totalBytes += len(response.Body)
		pageFeed, nextURL, parseErr := parseOperationalSourcePage(response, currentURL, source, spec, fetchedAt, policy, page)
		if parseErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(parseErr), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页解析失败："+errorText(parseErr))
			nextCursor = currentURL
			break
		}
		appendOperationalFeedPage(&feed, pageFeed, seenItems, policy.MaxItems)
		if fallbackMessage != "" {
			status, message = "partial", fallbackMessage
		}
		if !backfill {
			currentURL = ""
			break
		}
		if resumeValid && !readingHistory {
			readingHistory = true
			currentURL = resumeCursor
			nextCursor = resumeCursor
			continue
		}
		if nextURL == "" || operationalPageReachedBackfillCutoff(source, pageFeed.Items) {
			currentURL = ""
			nextCursor = ""
			break
		}
		if cursorSupported {
			validatedNext, ok := validOperationalBackfillCursor(nextURL, spec, policy)
			if !ok {
				status, message = operationalPartialResult(feed, "来源返回了无效的历史分页地址，已停止回填")
				currentURL = ""
				nextCursor = ""
				break
			}
			nextURL = validatedNext
			nextCursor = nextURL
		}
		currentURL = nextURL
	}
	if len(feed.Items) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "来源没有返回可用条目"}
	}
	if cursorSupported && backfill && nextCursor != "" && status == "ok" {
		status = "partial"
		message = "已读取当前历史窗口；后续同步将继续回填更早内容"
	}
	result := operationalFeedResultWithLimit(feed, status, message, firstResponse, policy.MaxItems)
	if cursorSupported && backfill {
		result.Backfill = true
		result.BackfillCursor = nextCursor
		result.BackfillCursorChanged = true
	}
	return result
}

func normalizedOperationalPaging(policy operationalPagingPolicy, retentionLimit int) operationalPagingPolicy {
	pageSizeLimit, pageLimit, itemLimit := operationalSourceLimit, operationalMaxPages, operationalMaxItems
	if policy.Kind == operationalPagingX {
		pageSizeLimit, pageLimit, itemLimit = operationalXPageSize, operationalXMaxPages, operationalXMaxItems
	} else if policy.Kind == operationalPagingBilibili {
		pageSizeLimit, pageLimit, itemLimit = operationalBilibiliPageSize, operationalBilibiliMaxPages, operationalBilibiliMaxItems
	} else if policy.Kind == operationalPagingFeedWindow {
		pageSizeLimit, pageLimit, itemLimit = operationalFeedWindowPageSize, operationalFeedWindowMaxPages, operationalFeedWindowMaxItems
	}
	if policy.PageSize < 1 || policy.PageSize > pageSizeLimit {
		policy.PageSize = pageSizeLimit
	}
	if policy.MaxPages < 1 || policy.MaxPages > pageLimit {
		policy.MaxPages = pageLimit
	}
	if policy.MaxItems < 1 || policy.MaxItems > itemLimit {
		policy.MaxItems = itemLimit
	}
	if retentionLimit > 0 && retentionLimit < policy.MaxItems {
		policy.MaxItems = retentionLimit
	}
	if policy.MaxBytes < feedFetchMaxBytes || policy.MaxBytes > operationalMaxBytes {
		policy.MaxBytes = operationalMaxBytes
	}
	return policy
}

func operationalSourceNeedsBackfill(source FeedSource, policy operationalPagingPolicy) bool {
	if policy.Kind == "" || policy.MaxPages < 2 || source.BackfillDays <= 0 || policy.MaxItems <= policy.PageSize {
		return false
	}
	if policy.Kind == operationalPagingBilibili && (source.Status == "partial" || source.Status == "unavailable") {
		// Public Bilibili search is intermittently rate-limited. A partial pass
		// must be eligible for a later full retry instead of becoming a
		// permanently short source after the first blocked page.
		return true
	}
	if source.LastSyncAt == "" || source.RecordsSeen == 0 {
		return true
	}
	// A failed initial cursor sync can still increment RecordsSeen before the
	// next retry. Recover cursor-backed sources while they hold at most one
	// page; once that retry succeeds, the persisted cursor drives later rounds.
	if operationalSourceSupportsBackfillCursor(policy.Kind) && source.EntryCount <= policy.PageSize {
		return true
	}
	// Versions before bounded pagination could leave exactly one full page in
	// the database. Retain the narrower compatibility rule for non-cursor
	// collectors so a complete one-page source is not expanded every sync.
	return source.EntryCount == policy.PageSize && source.RecordsSeen <= policy.PageSize
}

func operationalSourceSupportsBackfillCursor(kind string) bool {
	return kind == operationalPagingArxiv || kind == operationalPagingCrossref || kind == operationalPagingHackerNews || kind == operationalPagingFederalReg
}

func validOperationalBackfillCursor(raw string, spec operationalSourceSpec, policy operationalPagingPolicy) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || !operationalSourceSupportsBackfillCursor(policy.Kind) || len(spec.endpoints) == 0 {
		return "", false
	}
	parsed, err := url.Parse(raw)
	base, baseErr := url.Parse(spec.endpoints[0])
	samePath := parsed.Path == base.Path
	if policy.Kind == operationalPagingFederalReg {
		samePath = parsed.Path == "/api/v1/documents"
	}
	if err != nil || baseErr != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Host != base.Host || !samePath {
		return "", false
	}
	values := parsed.Query()
	exact := func(expected map[string]string) bool {
		if len(values) != len(expected) {
			return false
		}
		for key, want := range expected {
			got := values[key]
			if len(got) != 1 || got[0] != want {
				return false
			}
		}
		return true
	}
	positiveMultiple := func(value string, multiple, maximum int) bool {
		n, parseErr := strconv.Atoi(value)
		return parseErr == nil && n > 0 && n <= maximum && n%multiple == 0
	}
	switch policy.Kind {
	case operationalPagingArxiv:
		if !positiveMultiple(values.Get("start"), policy.PageSize, 500000) || !exact(map[string]string{
			"search_query": "cat:cs.AI", "start": values.Get("start"), "max_results": strconv.Itoa(policy.PageSize),
			"sortBy": "submittedDate", "sortOrder": "descending",
		}) {
			return "", false
		}
	case operationalPagingHackerNews:
		page, parseErr := strconv.Atoi(values.Get("page"))
		if parseErr != nil || page <= 0 || page > 1000 || !exact(map[string]string{
			"tags": "story", "query": "AI", "page": strconv.Itoa(page), "hitsPerPage": strconv.Itoa(policy.PageSize),
		}) {
			return "", false
		}
	case operationalPagingCrossref:
		filter := values.Get("filter")
		dateText := strings.TrimPrefix(filter, "until-pub-date:")
		dateValue, dateErr := time.Parse("2006-01-02", dateText)
		if dateErr != nil || filter == dateText || dateValue.After(time.Now().UTC().Add(24*time.Hour)) ||
			!positiveMultiple(values.Get("offset"), policy.PageSize, 10000) || !exact(map[string]string{
			"query": "artificial intelligence", "rows": strconv.Itoa(policy.PageSize), "offset": values.Get("offset"),
			"sort": "published", "order": "desc", "filter": filter,
			"select": "DOI,title,URL,published,author,container-title,abstract",
		}) {
			return "", false
		}
	case operationalPagingFederalReg:
		page, parseErr := strconv.Atoi(values.Get("page"))
		cursor := values.Get("search_after_cursor")
		if parseErr != nil || page < 2 || page > 50 || len(cursor) < 8 || len(cursor) > 2048 || !federalRegisterCursorRE.MatchString(cursor) || !exact(map[string]string{
			"format": "json", "order": "newest", "page": strconv.Itoa(page), "per_page": strconv.Itoa(policy.PageSize), "search_after_cursor": cursor,
		}) {
			return "", false
		}
	default:
		return "", false
	}
	return parsed.String(), true
}

func operationalFirstPageURL(spec operationalSourceSpec, fetchedAt string, policy operationalPagingPolicy) string {
	if len(spec.endpoints) == 0 {
		return ""
	}
	switch policy.Kind {
	case operationalPagingCrossref:
		return crossrefOperationalURL(spec.endpoints[0], fetchedAt)
	case operationalPagingHackerNews:
		return operationalURLWithPage(spec.endpoints[0], "page", 0, "hitsPerPage", policy.PageSize)
	case operationalPagingArxiv:
		return operationalURLWithPage(spec.endpoints[0], "start", 0, "max_results", policy.PageSize)
	case operationalPagingBilibili:
		return operationalURLWithPage(spec.endpoints[0], "page", 1, "page_size", policy.PageSize)
	case operationalPagingWechat:
		return spec.endpoints[0]
	default:
		return spec.endpoints[0]
	}
}

func parseOperationalSourcePage(response feedFetchResponse, requestedURL string, source FeedSource, spec operationalSourceSpec, fetchedAt string, policy operationalPagingPolicy, page int) (fetchedFeed, string, error) {
	var feed fetchedFeed
	var err error
	switch spec.parser {
	case "feed":
		feed, err = parseOperationalFeed(response, requestedURL)
	case "crossref":
		var items []NewsItem
		items, err = parseCrossrefOperationalJSON(response.Body, source, fetchedAt, policy.PageSize)
		feed = makeOperationalFeed(source, items, "api")
	case "hackernews":
		var items []NewsItem
		items, err = parseHackerNewsSearchJSON(response.Body, source, policy.PageSize)
		feed = makeOperationalFeed(source, items, "api")
	case "federal-register":
		var items []NewsItem
		items, err = parseFederalRegisterOperationalJSON(response.Body, source, policy.PageSize)
		feed = makeOperationalFeed(source, items, "api")
	case "folo":
		feed, err = parseFoloOperationalFeed(response.Body, source, spec.foloHandle, policy.PageSize)
	case "youtube":
		feed, err = parseYouTubeOperationalFeed(response.Body, source, spec, policy.PageSize)
	case "bilibili":
		feed, err = parseBilibiliOperationalFeed(response.Body, requestedURL, source, spec, policy.PageSize)
	default:
		err = errors.New("来源分页解析器未配置")
	}
	if err != nil {
		return fetchedFeed{}, "", err
	}
	if len(feed.Items) > policy.PageSize {
		feed.Items = feed.Items[:policy.PageSize]
	}
	currentURL := firstNonEmpty(response.FinalURL, requestedURL)
	nextURL := operationalNextPageURL(policy.Kind, response.Body, currentURL, page, policy.PageSize, len(feed.Items))
	return feed, nextURL, nil
}

func appendOperationalFeedPage(target *fetchedFeed, page fetchedFeed, seen map[string]struct{}, limit int) {
	target.Title = firstNonEmpty(target.Title, page.Title)
	target.Description = firstNonEmpty(target.Description, page.Description)
	target.SiteURL = firstNonEmpty(target.SiteURL, page.SiteURL)
	target.FeedURL = firstNonEmpty(target.FeedURL, page.FeedURL)
	target.IconURL = firstNonEmpty(target.IconURL, page.IconURL)
	target.Format = firstNonEmpty(target.Format, page.Format)
	for _, item := range page.Items {
		key := firstNonEmpty(strings.TrimSpace(item.ExternalID), strings.TrimSpace(item.URL), strings.TrimSpace(item.ContentHash))
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		target.Items = append(target.Items, item)
		if len(target.Items) >= limit {
			break
		}
	}
}

func operationalPartialResult(feed fetchedFeed, reason string) (string, string) {
	return "partial", limitText(fmt.Sprintf("已保留 %d 条内容；%s", len(feed.Items), reason), 500)
}

func operationalPageReachedBackfillCutoff(source FeedSource, items []NewsItem) bool {
	if source.BackfillDays <= 0 {
		return false
	}
	cutoff := time.Now().UTC().Add(-time.Duration(source.BackfillDays) * 24 * time.Hour)
	for _, item := range items {
		if published, err := time.Parse(time.RFC3339, item.PublishedAt); err == nil && published.Before(cutoff) {
			return true
		}
	}
	return false
}

func operationalNextPageURL(kind string, body []byte, currentURL string, page, pageSize, parsedItems int) string {
	switch kind {
	case operationalPagingCrossref:
		start, total, perPage := crossrefPageState(body)
		if perPage < 1 {
			perPage = pageSize
		}
		nextOffset := start + perPage
		if parsedItems == 0 || total <= nextOffset {
			return ""
		}
		return operationalURLWithPage(currentURL, "offset", nextOffset, "rows", pageSize)
	case operationalPagingHackerNews:
		var response struct {
			Page    int `json:"page"`
			NbPages int `json:"nbPages"`
		}
		if json.Unmarshal(body, &response) != nil || response.Page+1 >= response.NbPages || parsedItems == 0 {
			return ""
		}
		return operationalURLWithPage(currentURL, "page", response.Page+1, "hitsPerPage", pageSize)
	case operationalPagingFederalReg:
		var response struct {
			NextPageURL string `json:"next_page_url"`
		}
		if json.Unmarshal(body, &response) != nil || parsedItems == 0 || response.NextPageURL == "" {
			return ""
		}
		return sameSourceContinuationURL(currentURL, response.NextPageURL)
	case operationalPagingArxiv:
		start, total, perPage := arxivPageState(body)
		if perPage < 1 {
			perPage = pageSize
		}
		nextStart := start + perPage
		if parsedItems == 0 || total <= nextStart {
			return ""
		}
		return operationalURLWithPage(currentURL, "start", nextStart, "max_results", pageSize)
	case operationalPagingFeed:
		return feedContinuationURL(body, currentURL)
	case operationalPagingFolo:
		return foloContinuationURL(body, currentURL, page)
	case operationalPagingBilibili:
		var response struct {
			Data struct {
				Page     int `json:"page"`
				Next     int `json:"next"`
				NumPages int `json:"numPages"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &response) != nil || parsedItems == 0 {
			return ""
		}
		currentPage := response.Data.Page
		if currentPage < 1 {
			currentPage = page + 1
		}
		nextPage := response.Data.Next
		if nextPage <= currentPage {
			nextPage = currentPage + 1
		}
		if response.Data.NumPages > 0 && nextPage > response.Data.NumPages {
			return ""
		}
		return operationalURLWithPage(currentURL, "page", nextPage, "page_size", pageSize)
	case operationalPagingWechat:
		if match := jintiankanshaNextRE.FindSubmatch(body); len(match) == 2 {
			candidate := resolvePublicLink(currentURL, string(match[1]))
			if validJintiankanshaColumnURL(candidate) {
				return candidate
			}
		}
		return ""
	default:
		return ""
	}
}

func operationalURLWithPage(raw, pageKey string, page int, sizeKey string, pageSize int) string {
	return operationalURLWithString(raw, pageKey, strconv.Itoa(page), sizeKey, strconv.Itoa(pageSize))
}

func operationalURLWithString(raw, key, value, sizeKey, sizeValue string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !validPublicFeedURL(parsed.String()) {
		return ""
	}
	values := parsed.Query()
	values.Set(key, value)
	if sizeKey != "" {
		values.Set(sizeKey, sizeValue)
	}
	parsed.RawQuery = values.Encode()
	if !validPublicFeedURL(parsed.String()) {
		return ""
	}
	return parsed.String()
}

func crossrefPageState(body []byte) (int, int, int) {
	var response struct {
		Message struct {
			TotalResults int `json:"total-results"`
			ItemsPerPage int `json:"items-per-page"`
			Query        struct {
				StartIndex int `json:"start-index"`
			} `json:"query"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &response) != nil {
		return 0, 0, 0
	}
	return response.Message.Query.StartIndex, response.Message.TotalResults, response.Message.ItemsPerPage
}

func arxivPageState(body []byte) (int, int, int) {
	var response struct {
		StartIndex   int `xml:"startIndex"`
		TotalResults int `xml:"totalResults"`
		ItemsPerPage int `xml:"itemsPerPage"`
	}
	if xml.Unmarshal(body, &response) != nil {
		return 0, 0, 0
	}
	return response.StartIndex, response.TotalResults, response.ItemsPerPage
}

func feedContinuationURL(body []byte, currentURL string) string {
	var document struct {
		NextURL string `json:"next_url"`
	}
	if json.Unmarshal(body, &document) == nil && document.NextURL != "" {
		return sameSourceContinuationURL(currentURL, document.NextURL)
	}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || !strings.EqualFold(start.Name.Local, "link") || !strings.EqualFold(xmlAttr(&start, "rel"), "next") {
			continue
		}
		return sameSourceContinuationURL(currentURL, xmlAttr(&start, "href"))
	}
}

func foloContinuationURL(body []byte, currentURL string, page int) string {
	var response struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	// Providers have returned the continuation envelope under several names
	// over time. Inspect only the bounded top-level data object and one nested
	// metadata object; never recursively walk arbitrary model/user JSON.
	containers := []map[string]json.RawMessage{response.Data}
	for _, key := range []string{"pagination", "pageInfo", "page_info", "meta", "links"} {
		var nested map[string]json.RawMessage
		if json.Unmarshal(response.Data[key], &nested) == nil && len(nested) > 0 {
			containers = append(containers, nested)
		}
	}
	for _, container := range containers {
		for _, key := range []string{"next", "nextPage", "next_page", "nextUrl", "next_url"} {
			var candidate string
			if json.Unmarshal(container[key], &candidate) == nil && candidate != "" {
				return sameSourceContinuationURL(currentURL, candidate)
			}
			var link struct {
				Href string `json:"href"`
				URL  string `json:"url"`
			}
			if json.Unmarshal(container[key], &link) == nil {
				if candidate = firstNonEmpty(link.Href, link.URL); candidate != "" {
					return sameSourceContinuationURL(currentURL, candidate)
				}
			}
		}
		for _, key := range []string{"nextCursor", "next_cursor", "cursor"} {
			var cursor string
			if json.Unmarshal(container[key], &cursor) == nil {
				if cursor = boundedContinuationToken(cursor); cursor != "" {
					return operationalURLWithString(currentURL, "cursor", cursor, "", "")
				}
			}
		}
		var nextPage int
		if json.Unmarshal(container["nextPage"], &nextPage) == nil && nextPage > page {
			return operationalURLWithPage(currentURL, "page", nextPage, "", 0)
		}
	}
	return ""
}

func boundedContinuationToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 || strings.ContainsAny(value, "\r\n\x00") {
		return ""
	}
	return value
}

func sameSourceContinuationURL(currentURL, candidate string) string {
	resolved := resolvePublicLink(currentURL, candidate)
	current, currentErr := url.Parse(currentURL)
	next, nextErr := url.Parse(resolved)
	if currentErr != nil || nextErr != nil || resolved == "" || !strings.EqualFold(current.Scheme, next.Scheme) || !strings.EqualFold(current.Host, next.Host) {
		return ""
	}
	return resolved
}

type hackerNewsSearchResponse struct {
	Hits []hackerNewsSearchHit `json:"hits"`
}

type hackerNewsSearchHit struct {
	ObjectID  string `json:"objectID"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	StoryText string `json:"story_text"`
}

var hackerNewsAIThemeRE = regexp.MustCompile(`(?i)(^|[^a-z0-9])(ai|llm|gpt(?:-[0-9]+)?|claude|gemini|openai|anthropic|agentic|artificial intelligence|machine learning|deep learning|large language model)([^a-z0-9]|$)`)
var federalRegisterCursorRE = regexp.MustCompile(`^[A-Za-z0-9_-]+={0,2}$`)
var federalRegisterDocumentNumberRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{2,63}$`)

func parseHackerNewsSearchJSON(body []byte, source FeedSource, limit int) ([]NewsItem, error) {
	var response hackerNewsSearchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, errors.New("Hacker News AI JSON 无法解析")
	}
	items := make([]NewsItem, 0, minInt(limit, len(response.Hits)))
	seen := make(map[string]struct{})
	for _, hit := range response.Hits {
		id := strings.TrimSpace(hit.ObjectID)
		searchable := cleanPlain(hit.Title + " " + hit.StoryText)
		link, ok := hackerNewsArticleURL(hit.URL)
		if !allASCIIDigits(id) || !hackerNewsAIThemeRE.MatchString(searchable) || !ok {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		item := makeOperationalNews(source, id, hit.Title, link, hit.CreatedAt, hit.StoryText, hit.Author, "api", "public_aggregator", "")
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return nil, errors.New("Hacker News AI 没有返回可浏览的 AI 文章")
	}
	return items, nil
}

func parseFederalRegisterOperationalJSON(body []byte, source FeedSource, limit int) ([]NewsItem, error) {
	var response struct {
		Results []struct {
			DocumentNumber  string `json:"document_number"`
			Title           string `json:"title"`
			Abstract        string `json:"abstract"`
			HTMLURL         string `json:"html_url"`
			PublicationDate string `json:"publication_date"`
			Type            string `json:"type"`
			Agencies        []struct {
				Name string `json:"name"`
			} `json:"agencies"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &response) != nil {
		return nil, errors.New("Federal Register JSON 无法解析")
	}
	items := make([]NewsItem, 0, minInt(limit, len(response.Results)))
	seen := make(map[string]struct{}, len(response.Results))
	for _, row := range response.Results {
		documentNumber := strings.TrimSpace(row.DocumentNumber)
		link, ok := validFederalRegisterDocumentURL(row.HTMLURL, documentNumber)
		if !ok || !federalRegisterDocumentNumberRE.MatchString(documentNumber) || strings.TrimSpace(row.Title) == "" {
			continue
		}
		if _, duplicate := seen[documentNumber]; duplicate {
			continue
		}
		seen[documentNumber] = struct{}{}
		agencies := make([]string, 0, minInt(3, len(row.Agencies)))
		for _, agency := range row.Agencies {
			if name := strings.TrimSpace(cleanPlain(agency.Name)); name != "" {
				agencies = append(agencies, name)
				if len(agencies) == 3 {
					break
				}
			}
		}
		item := makeOperationalNews(source, documentNumber, row.Title, link, row.PublicationDate, firstNonEmpty(row.Abstract, row.Type), strings.Join(agencies, ", "), "api", "official", "")
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return nil, errors.New("Federal Register API 没有返回可用文件")
	}
	return items, nil
}

func validFederalRegisterDocumentURL(raw, documentNumber string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() != "www.federalregister.gov" {
		return "", false
	}
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) < 6 || pathParts[0] != "documents" || pathParts[4] != documentNumber {
		return "", false
	}
	return parsed.String(), true
}

func hackerNewsArticleURL(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	if strings.Count(lower, "https://")+strings.Count(lower, "http://") != 1 {
		return "", false
	}
	normalized, err := normalizePublicURL(value)
	if err != nil || isHackerNewsDiscussionURL(normalized) {
		return "", false
	}
	return normalized, true
}

// Folo is an aggregator, so do not use the permissive generic JSON-feed
// parser here. The account and every stored public URL are pinned to the
// configured X/Twitter handle and media hosts before touching local storage.
type foloResponse struct {
	Code int      `json:"code"`
	Data foloData `json:"data"`
}

type foloData struct {
	Feed    foloFeed    `json:"feed"`
	Entries []foloEntry `json:"entries"`
}

type foloFeed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	SiteURL     string `json:"siteUrl"`
	URL         string `json:"url"`
	Image       string `json:"image"`
}

type foloEntry struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	URL               string          `json:"url"`
	Description       string          `json:"description"`
	Summary           string          `json:"summary"`
	Content           string          `json:"content"`
	Author            string          `json:"author"`
	AuthorAvatar      string          `json:"authorAvatar"`
	PublishedAt       string          `json:"publishedAt"`
	Duration          json.RawMessage `json:"duration"`
	DurationSecs      json.RawMessage `json:"duration_seconds"`
	DurationInSeconds json.RawMessage `json:"duration_in_seconds"`
	Media             []foloMedia     `json:"media"`
}

type foloMedia struct {
	URL               string          `json:"url"`
	Type              string          `json:"type"`
	Width             int             `json:"width"`
	Height            int             `json:"height"`
	PreviewImageURL   string          `json:"preview_image_url"`
	Duration          json.RawMessage `json:"duration"`
	DurationSecs      json.RawMessage `json:"duration_seconds"`
	DurationInSeconds json.RawMessage `json:"duration_in_seconds"`
}

// Folo deployments have returned duration as either a number of seconds or a
// clock-like string. Keep both forms optional so an older response remains
// readable and a malformed duration cannot reject the whole feed.
func parseFoloDuration(values ...json.RawMessage) int {
	for _, raw := range values {
		value := strings.TrimSpace(string(raw))
		if value == "" || value == "null" {
			continue
		}
		if strings.HasPrefix(value, `"`) {
			var textValue string
			if json.Unmarshal(raw, &textValue) == nil {
				if parsed := parseMediaDuration(textValue); parsed > 0 {
					return clampInt(parsed, 0, 86400)
				}
			}
			continue
		}
		if strings.HasPrefix(value, "{") {
			var object struct {
				Seconds          json.RawMessage `json:"seconds"`
				Duration         json.RawMessage `json:"duration"`
				DurationSeconds  json.RawMessage `json:"duration_seconds"`
				DurationInSecond json.RawMessage `json:"duration_in_seconds"`
			}
			if json.Unmarshal(raw, &object) == nil {
				if parsed := parseFoloDuration(object.Seconds, object.Duration, object.DurationSeconds, object.DurationInSecond); parsed > 0 {
					return parsed
				}
			}
			continue
		}
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed >= 0 && parsed <= 86400 {
			return int(parsed)
		}
	}
	return 0
}

func parseFoloOperationalJSON(body []byte, source FeedSource, handle string, limit int) ([]NewsItem, error) {
	feed, err := parseFoloOperationalFeed(body, source, handle, limit)
	if err != nil {
		return nil, err
	}
	return feed.Items, nil
}

func parseFoloOperationalFeed(body []byte, source FeedSource, handle string, limit int) (fetchedFeed, error) {
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fetchedFeed{}, errors.New("Folo JSON 无法解析")
	}
	if response.Code != 0 {
		return fetchedFeed{}, errors.New("Folo 返回非成功状态")
	}
	siteURL, _, accountOK := validFoloProfileURL(response.Data.Feed.SiteURL, handle, false)
	if !accountOK {
		return fetchedFeed{}, errors.New("Folo 返回的账号与固定来源不匹配")
	}
	if len(response.Data.Entries) == 0 {
		return fetchedFeed{}, errors.New("Folo 没有返回条目")
	}

	items := make([]NewsItem, 0, minInt(limit, len(response.Data.Entries)))
	seen := make(map[string]struct{})
	feedIconURL, _ := validFoloMediaURL(response.Data.Feed.Image, "pbs.twimg.com")
	authorIconURL := ""
	for _, entry := range response.Data.Entries {
		statusID, link, ok := validFoloStatusURL(entry.URL, handle)
		if !ok || strings.TrimSpace(entry.ID) == "" {
			continue
		}
		if _, exists := seen[statusID]; exists {
			continue
		}
		seen[statusID] = struct{}{}
		item := makeFetchedNews(statusID, firstNonEmpty(entry.Title, entry.Summary, entry.Description), link, source.Name, entry.Author, entry.PublishedAt, firstNonEmpty(entry.Summary, entry.Description, entry.Content), "", "", "", 0, source.URL)
		item.SourceType = "api"
		item.SourceLevel = "public_aggregator"
		if authorIconURL == "" {
			authorIconURL, _ = validFoloMediaURL(entry.AuthorAvatar, "pbs.twimg.com")
		}
		for _, media := range entry.Media {
			kind := strings.ToLower(strings.TrimSpace(media.Type))
			switch {
			case (kind == "photo" || kind == "image") && item.ThumbnailURL == "":
				if imageURL, ok := validFoloMediaURL(media.URL, "pbs.twimg.com"); ok {
					item.ThumbnailURL = imageURL
				}
			case kind == "video" && item.MediaURL == "":
				if videoURL, ok := validFoloMediaURL(media.URL, "video.twimg.com"); ok {
					item.MediaURL = videoURL
					item.MediaMIME = "video/mp4"
					item.ViewType = "video"
					if previewURL, previewOK := validFoloMediaURL(media.PreviewImageURL, "pbs.twimg.com"); previewOK {
						item.ThumbnailURL = previewURL
					}
				}
			}
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("Folo 没有返回符合固定账号规则的条目")
	}
	feed := makeOperationalFeed(source, items, "api")
	feed.SiteURL = siteURL
	// Entry authorAvatar tracks profile changes more reliably than the feed
	// record, which aggregators may cache for a long time.
	feed.IconURL = firstNonEmpty(authorIconURL, feedIconURL)
	return feed, nil
}

func parseXRSSOperationalFeed(body []byte, inputURL string, source FeedSource, handle string, limit int) (fetchedFeed, error) {
	var document rssDocument
	decoder := xml.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&document); err != nil {
		return fetchedFeed{}, errors.New("X RSS 无法解析")
	}
	if document.Channel.Title == "" && len(document.Channel.Items) == 0 {
		return fetchedFeed{}, errors.New("X RSS 没有返回条目")
	}
	feedSiteURL := resolvePublicLink(inputURL, document.Channel.Link)
	siteURL, _, accountOK := validFoloProfileURL(feedSiteURL, handle, false)
	if !accountOK {
		return fetchedFeed{}, errors.New("X RSS 返回的账号与固定来源不匹配")
	}
	if limit < 1 {
		limit = operationalXMaxItems
	}
	items := make([]NewsItem, 0, minInt(limit, len(document.Channel.Items)))
	seen := make(map[string]struct{}, len(document.Channel.Items))
	feedIconURL, _ := validFoloMediaURL(resolvePublicLink(inputURL, firstNonEmpty(document.Channel.ImageURL, document.Channel.ITunesImage)), "pbs.twimg.com")
	for _, raw := range document.Channel.Items {
		statusID, link, ok := validFoloStatusURL(firstNonEmpty(raw.Link, raw.GUID), handle)
		if !ok || statusID == "" {
			continue
		}
		if _, exists := seen[statusID]; exists {
			continue
		}
		seen[statusID] = struct{}{}
		item := newsItemFromRSS(raw, source.Name, source.URL)
		item.ID = stableObjectID("entry", statusID)
		item.ExternalID = statusID
		item.URL = link
		item.Source = source.Name
		item.SourceType = "api"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		if item.ThumbnailURL == "" || item.MediaURL == "" {
			imageURL, videoURL := xRSSDescriptionMedia(raw.Description)
			if item.ThumbnailURL == "" {
				item.ThumbnailURL = imageURL
			}
			if item.MediaURL == "" && videoURL != "" {
				item.MediaURL = videoURL
				item.MediaMIME = "video/mp4"
				item.ViewType = "video"
			}
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("X RSS 没有返回符合固定账号规则的条目")
	}
	feed := makeOperationalFeed(source, items, "rss")
	feed.Title = cleanPlain(document.Channel.Title)
	feed.Description = cleanPlain(document.Channel.Description)
	feed.SiteURL = siteURL
	feed.IconURL = feedIconURL
	feed.FeedURL = source.URL
	return feed, nil
}

var xRSSDescriptionMediaRE = regexp.MustCompile(`(?i)(?:src|href)=\"(https://(?:pbs\.twimg\.com|video\.twimg\.com)/[^\"]+)\"`)

func xRSSDescriptionMedia(description string) (imageURL, videoURL string) {
	for _, match := range xRSSDescriptionMediaRE.FindAllStringSubmatch(description, 8) {
		if len(match) != 2 {
			continue
		}
		candidate := strings.ReplaceAll(match[1], "&amp;", "&")
		if strings.HasPrefix(strings.ToLower(candidate), "https://video.twimg.com/") && videoURL == "" {
			videoURL, _ = validFoloMediaURL(candidate, "video.twimg.com")
			continue
		}
		if imageURL == "" {
			imageURL, _ = validFoloMediaURL(candidate, "pbs.twimg.com")
		}
	}
	return imageURL, videoURL
}

func validFoloAccountURL(raw, handle string) bool {
	_, _, ok := validFoloProfileURL(raw, handle, false)
	return ok
}

func validFoloStatusURL(raw, handle string) (string, string, bool) {
	profile, statusID, ok := validFoloProfileURL(raw, handle, true)
	return statusID, profile, ok
}

func validFoloProfileURL(raw, handle string, requireStatus bool) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "x.com" && host != "twitter.com" {
		return "", "", false
	}
	profilePath := "/" + handle
	if len(parsed.Path) < len(profilePath) || !strings.EqualFold(parsed.Path[:len(profilePath)], profilePath) {
		return "", "", false
	}
	if !requireStatus {
		return parsed.String(), "", len(parsed.Path) == len(profilePath)
	}
	statusPrefix := profilePath + "/status/"
	if len(parsed.Path) <= len(statusPrefix) || !strings.EqualFold(parsed.Path[:len(statusPrefix)], statusPrefix) {
		return "", "", false
	}
	statusID := parsed.Path[len(statusPrefix):]
	if !allASCIIDigits(statusID) {
		return "", "", false
	}
	return parsed.String(), statusID, true
}

func validFoloMediaURL(raw, expectedHost string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return "", false
	}
	if strings.ToLower(parsed.Hostname()) != expectedHost || parsed.Path == "" {
		return "", false
	}
	return parsed.String(), true
}

var youtubeChannelIDRE = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
var youtubeVideoIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func validYouTubeChannelURL(raw, channelID string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" && host != "m.youtube.com" {
		return "", false
	}
	channelPath := "/channel/" + channelID
	if parsed.Path != channelPath && parsed.Path != channelPath+"/" {
		return "", false
	}
	return "https://www.youtube.com/channel/" + channelID, true
}

func youtubeVideoID(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	var candidate string
	switch host {
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		if parsed.Path == "/watch" {
			candidate = parsed.Query().Get("v")
		} else if strings.HasPrefix(parsed.Path, "/shorts/") {
			candidate = strings.TrimPrefix(parsed.Path, "/shorts/")
		} else if strings.HasPrefix(parsed.Path, "/embed/") {
			candidate = strings.TrimPrefix(parsed.Path, "/embed/")
		}
	case "youtu.be":
		candidate = strings.TrimPrefix(parsed.Path, "/")
	default:
		return "", false
	}
	if !youtubeVideoIDRE.MatchString(candidate) {
		return "", false
	}
	return candidate, true
}

func youtubeThumbnailProxyURL(videoID string) string {
	if !youtubeVideoIDRE.MatchString(videoID) {
		return ""
	}
	// The official thumbnail is already a public HTTPS image. Do not route a
	// direct YouTube feed through an unrelated image proxy just because the
	// Atom entry omitted media:thumbnail.
	return "https://i.ytimg.com/vi/" + videoID + "/hqdefault.jpg"
}

func parseYouTubeOperationalFeed(body []byte, source FeedSource, spec operationalSourceSpec, limit int) (fetchedFeed, error) {
	if strings.HasPrefix(strings.TrimSpace(string(body)), "<") {
		return parseYouTubeAtomOperationalFeed(body, source, spec, limit)
	}
	return parseYouTubeFoloOperationalFeed(body, source, spec, limit)
}

func parseYouTubeAtomOperationalFeed(body []byte, source FeedSource, spec operationalSourceSpec, limit int) (fetchedFeed, error) {
	inputURL := firstNonEmpty(source.URL, "https://www.youtube.com/feeds/videos.xml?channel_id="+spec.youtubeChannelID)
	parsed, err := parseXMLFeed(body, inputURL)
	if err != nil {
		return fetchedFeed{}, errors.New("YouTube Atom Feed 无法解析")
	}
	channelURL, ok := validYouTubeChannelURL(parsed.SiteURL, spec.youtubeChannelID)
	isHistoryRSS := parsed.Format == "rss"
	expectedTitle := spec.youtubeTitle
	if isHistoryRSS {
		expectedTitle += " - YouTube"
	}
	if !ok || strings.TrimSpace(parsed.Title) != expectedTitle {
		return fetchedFeed{}, errors.New("YouTube 返回的频道与固定来源不匹配")
	}
	items := make([]NewsItem, 0, minInt(limit, len(parsed.Items)))
	seen := make(map[string]struct{}, len(parsed.Items))
	for _, raw := range parsed.Items {
		videoID, valid := youtubeVideoID(raw.URL)
		if !valid || videoID == "" {
			continue
		}
		if _, exists := seen[videoID]; exists {
			continue
		}
		seen[videoID] = struct{}{}
		thumbnail := ""
		if parsedThumbnail, thumbnailOK := validYouTubeThumbnailURL(raw.ThumbnailURL); thumbnailOK {
			thumbnail = parsedThumbnail
		}
		if thumbnail == "" {
			thumbnail = youtubeThumbnailProxyURL(videoID)
		}
		item := makeFetchedNews(
			videoID,
			raw.Title,
			"https://www.youtube.com/watch?v="+videoID,
			source.Name,
			raw.Author,
			raw.PublishedAt,
			raw.SummaryText,
			thumbnail,
			"",
			"video/youtube",
			raw.DurationSecs,
			source.URL,
		)
		item.SourceType = "youtube"
		item.SourceLevel = "official"
		if isHistoryRSS {
			item.SourceLevel = "public_aggregator"
		}
		item.PermissionScope = "public_read"
		item.ViewType = "video"
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("YouTube 没有返回可播放视频")
	}
	feed := makeOperationalFeed(source, items, "youtube")
	feed.SiteURL = channelURL
	feed.IconURL = firstNonEmpty(parsed.IconURL, "https://icons.folo.is/youtube.com")
	return feed, nil
}

func parseYouTubeFoloOperationalFeed(body []byte, source FeedSource, spec operationalSourceSpec, limit int) (fetchedFeed, error) {
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fetchedFeed{}, errors.New("YouTube Feed JSON 无法解析")
	}
	if response.Code != 0 {
		return fetchedFeed{}, errors.New("YouTube Feed 返回非成功状态")
	}
	channelURL, ok := validYouTubeChannelURL(response.Data.Feed.SiteURL, spec.youtubeChannelID)
	if !ok || !strings.EqualFold(strings.TrimSpace(response.Data.Feed.Title), spec.youtubeTitle) {
		return fetchedFeed{}, errors.New("YouTube 返回的频道与固定来源不匹配")
	}
	if len(response.Data.Entries) == 0 {
		return fetchedFeed{}, errors.New("YouTube 没有返回视频")
	}
	items := make([]NewsItem, 0, minInt(limit, len(response.Data.Entries)))
	seen := make(map[string]struct{}, len(response.Data.Entries))
	for _, entry := range response.Data.Entries {
		videoID, valid := youtubeVideoID(entry.URL)
		if !valid || strings.TrimSpace(entry.ID) == "" {
			continue
		}
		if _, exists := seen[videoID]; exists {
			continue
		}
		seen[videoID] = struct{}{}
		thumbnail := ""
		for _, media := range entry.Media {
			if strings.EqualFold(strings.TrimSpace(media.Type), "photo") {
				if parsed, mediaOK := validYouTubeThumbnailURL(media.URL); mediaOK {
					thumbnail = parsed
					break
				}
			}
		}
		if thumbnail == "" {
			thumbnail = youtubeThumbnailProxyURL(videoID)
		}
		duration := parseFoloDuration(entry.DurationSecs, entry.DurationInSeconds, entry.Duration)
		if duration == 0 {
			for _, media := range entry.Media {
				duration = parseFoloDuration(media.DurationSecs, media.DurationInSeconds, media.Duration)
				if duration > 0 {
					break
				}
			}
		}
		item := makeFetchedNews(videoID, firstNonEmpty(entry.Title, entry.Description), "https://www.youtube.com/watch?v="+videoID, source.Name, firstNonEmpty(entry.Author, response.Data.Feed.Title), entry.PublishedAt, firstNonEmpty(entry.Description, entry.Content), thumbnail, "", "video/youtube", duration, source.URL)
		item.SourceType = "youtube"
		item.SourceLevel = "public_aggregator"
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("YouTube 没有返回可播放视频")
	}
	return fetchedFeed{
		Title:   spec.youtubeTitle,
		SiteURL: channelURL,
		FeedURL: source.URL,
		IconURL: "https://icons.folo.is/youtube.com",
		Format:  "youtube",
		Items:   items,
	}, nil
}

type bilibiliSearchResponse struct {
	Code int `json:"code"`
	Data struct {
		Page     int                   `json:"page"`
		Next     int                   `json:"next"`
		NumPages int                   `json:"numPages"`
		Result   []bilibiliSearchGroup `json:"result"`
	} `json:"data"`
}

type bilibiliSearchEnvelope struct {
	Code int `json:"code"`
	Data struct {
		Page     int             `json:"page"`
		Next     int             `json:"next"`
		NumPages int             `json:"numPages"`
		Result   json.RawMessage `json:"result"`
	} `json:"data"`
}

type bilibiliSearchGroup struct {
	ResultType string          `json:"result_type"`
	Data       json.RawMessage `json:"data"`
}

type bilibiliSearchUser struct {
	MID       int64                 `json:"mid"`
	Uname     string                `json:"uname"`
	Upic      string                `json:"upic"`
	Videos    int                   `json:"videos"`
	Resources []bilibiliSearchVideo `json:"res"`
}

type bilibiliSearchVideo struct {
	BVID        string `json:"bvid"`
	MID         int64  `json:"mid"`
	Author      string `json:"author"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Desc        string `json:"desc"`
	Pic         string `json:"pic"`
	Pubdate     int64  `json:"pubdate"`
	Duration    string `json:"duration"`
}

var bilibiliVideoIDRE = regexp.MustCompile(`^BV[0-9A-Za-z]{8,32}$`)
var bilibiliVideoURLRE = regexp.MustCompile(`^/video/(BV[0-9A-Za-z]{8,32})/?$`)

func parseBilibiliOperationalFeed(body []byte, requestedURL string, source FeedSource, spec operationalSourceSpec, limit int) (fetchedFeed, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(requestedURL))
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Hostname() != "api.bilibili.com" || (parsedURL.Path != "/x/web-interface/search/type" && parsedURL.Path != "/x/web-interface/search/all/v2") || parsedURL.Query().Get("keyword") != spec.bilibiliKeyword {
		return fetchedFeed{}, errors.New("Bilibili 搜索地址与固定来源不匹配")
	}
	var response bilibiliSearchEnvelope
	if err := json.Unmarshal(body, &response); err != nil {
		return fetchedFeed{}, errors.New("Bilibili 搜索 JSON 无法解析")
	}
	if response.Code != 0 {
		return fetchedFeed{}, fmt.Errorf("Bilibili 搜索返回非成功状态（%d）", response.Code)
	}
	if limit < 1 {
		limit = operationalSourceLimit
	}
	items := make([]NewsItem, 0, minInt(limit, operationalSourceLimit))
	seen := make(map[string]struct{}, limit)
	channelName := spec.bilibiliTitle
	channelIcon := ""
	appendVideo := func(video bilibiliSearchVideo, accountName string) {
		if video.MID != 0 && video.MID != spec.bilibiliMID {
			return
		}
		videoID := strings.TrimSpace(video.BVID)
		if !bilibiliVideoIDRE.MatchString(videoID) {
			return
		}
		if _, exists := seen[videoID]; exists {
			return
		}
		seen[videoID] = struct{}{}
		title := cleanPlain(video.Title)
		if title == "" {
			return
		}
		published := ""
		if video.Pubdate > 0 {
			published = time.Unix(video.Pubdate, 0).UTC().Format(time.RFC3339)
		}
		item := makeFetchedNews(
			videoID,
			title,
			"https://www.bilibili.com/video/"+videoID,
			source.Name,
			firstNonEmpty(video.Author, accountName, channelName),
			published,
			cleanPlain(firstNonEmpty(video.Description, video.Desc)),
			bilibiliImageURL(video.Pic),
			"",
			"",
			parseMediaDuration(video.Duration),
			source.URL,
		)
		item.SourceType = "bilibili"
		item.SourceLevel = "official"
		item.PermissionScope = "public_read"
		item.ViewType = "video"
		items = append(items, item)
	}
	appendSearchTypeVideos := func(videos []bilibiliSearchVideo) {
		for _, video := range videos {
			appendVideo(video, channelName)
			if len(items) >= limit {
				break
			}
		}
	}
	var searchTypeVideos []bilibiliSearchVideo
	if len(response.Data.Result) > 0 && json.Unmarshal(response.Data.Result, &searchTypeVideos) == nil {
		appendSearchTypeVideos(searchTypeVideos)
	}
	if len(items) < limit {
		var groups []bilibiliSearchGroup
		if len(response.Data.Result) > 0 && json.Unmarshal(response.Data.Result, &groups) == nil {
			for _, group := range groups {
				switch group.ResultType {
				case "bili_user":
					var users []bilibiliSearchUser
					if json.Unmarshal(group.Data, &users) != nil {
						continue
					}
					for _, user := range users {
						if user.MID != spec.bilibiliMID {
							continue
						}
						channelName = firstNonEmpty(cleanPlain(user.Uname), channelName)
						channelIcon = bilibiliImageURL(user.Upic)
						for _, video := range user.Resources {
							video.MID = spec.bilibiliMID
							appendVideo(video, channelName)
							if len(items) >= limit {
								break
							}
						}
					}
				case "video":
					var videos []bilibiliSearchVideo
					if json.Unmarshal(group.Data, &videos) != nil {
						continue
					}
					for _, video := range videos {
						appendVideo(video, channelName)
						if len(items) >= limit {
							break
						}
					}
				}
				if len(items) >= limit {
					break
				}
			}
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("Bilibili 固定账号没有返回可播放视频")
	}
	feed := makeOperationalFeed(source, items, "bilibili")
	feed.Title = channelName
	feed.SiteURL = "https://space.bilibili.com/" + strconv.FormatInt(spec.bilibiliMID, 10) + "/video"
	feed.IconURL = channelIcon
	return feed, nil
}

func parseBilibiliFoloOperationalFeed(body []byte, source FeedSource, spec operationalSourceSpec, limit int) (fetchedFeed, error) {
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fetchedFeed{}, errors.New("Bilibili Folo JSON 无法解析")
	}
	if response.Code != 0 {
		return fetchedFeed{}, fmt.Errorf("Bilibili Folo 返回非成功状态（%d）", response.Code)
	}
	if !validBilibiliSpaceURL(response.Data.Feed.SiteURL, spec.bilibiliMID) {
		return fetchedFeed{}, errors.New("Bilibili Folo 返回的账号与固定来源不匹配")
	}
	if limit < 1 {
		limit = operationalSourceLimit
	}
	items := make([]NewsItem, 0, minInt(limit, len(response.Data.Entries)))
	seen := make(map[string]struct{}, len(response.Data.Entries))
	channelIcon := bilibiliImageURL(response.Data.Feed.Image)
	channelName := firstNonEmpty(response.Data.Feed.Title, spec.bilibiliTitle)
	for _, entry := range response.Data.Entries {
		videoID, link, ok := validBilibiliVideoURL(entry.URL)
		if !ok || videoID == "" {
			continue
		}
		if _, exists := seen[videoID]; exists {
			continue
		}
		seen[videoID] = struct{}{}
		item := makeFetchedNews(
			videoID,
			entry.Title,
			link,
			source.Name,
			firstNonEmpty(entry.Author, channelName),
			entry.PublishedAt,
			firstNonEmpty(entry.Summary, entry.Description, entry.Content),
			"",
			"",
			"",
			parseFoloDuration(entry.Duration, entry.DurationSecs, entry.DurationInSeconds),
			source.URL,
		)
		item.SourceType = "bilibili"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		item.ViewType = "video"
		for _, media := range entry.Media {
			if strings.EqualFold(strings.TrimSpace(media.Type), "photo") && item.ThumbnailURL == "" {
				item.ThumbnailURL = bilibiliImageURL(media.URL)
			}
		}
		if item.ThumbnailURL == "" {
			item.ThumbnailURL = channelIcon
		}
		items = append(items, item)
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("Bilibili Folo 没有返回可播放视频")
	}
	feed := makeOperationalFeed(source, items, "bilibili")
	feed.Title = channelName
	feed.SiteURL = response.Data.Feed.SiteURL
	feed.IconURL = channelIcon
	return feed, nil
}

func validBilibiliSpaceURL(raw string, mid int64) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Hostname() != "space.bilibili.com" {
		return false
	}
	want := "/" + strconv.FormatInt(mid, 10)
	return parsed.Path == want || parsed.Path == want+"/" || parsed.Path == want+"/video"
}

func validBilibiliVideoURL(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	if parsed.Hostname() != "www.bilibili.com" && parsed.Hostname() != "bilibili.com" {
		return "", "", false
	}
	match := bilibiliVideoURLRE.FindStringSubmatch(parsed.Path)
	if len(match) != 2 || !bilibiliVideoIDRE.MatchString(match[1]) {
		return "", "", false
	}
	return match[1], "https://www.bilibili.com/video/" + match[1], true
}

func bilibiliImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || parsed.Path == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "bpic.bilibili.com" && host != "archive.biliimg.com" && host != "i0.hdslb.com" && host != "i1.hdslb.com" && host != "i2.hdslb.com" {
		return ""
	}
	// Bilibili's anonymous metadata API still emits HTTP cover URLs. The same
	// trusted CDN hosts support HTTPS, so upgrade before the browser sees them.
	parsed.Scheme = "https"
	return parsed.String()
}

func validYouTubeThumbnailURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "i.ytimg.com" && host != "img.youtube.com" && host != "yt3.ggpht.com" && !strings.HasSuffix(host, ".ytimg.com") {
		return "", false
	}
	return parsed.String(), true
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func operationalFeedResult(feed fetchedFeed, status, message string, response feedFetchResponse) operationalSourceResult {
	return operationalFeedResultWithLimit(feed, status, message, response, operationalSourceLimit)
}

func operationalFeedResultWithLimit(feed fetchedFeed, status, message string, response feedFetchResponse, limit int) operationalSourceResult {
	if limit < 1 || limit > operationalXMaxItems {
		limit = operationalSourceLimit
	}
	if len(feed.Items) > limit {
		feed.Items = feed.Items[:limit]
	}
	for index := range feed.Items {
		feed.Items[index].PermissionScope = firstNonEmpty(feed.Items[index].PermissionScope, "public_read")
		feed.Items[index].SourceLevel = firstNonEmpty(feed.Items[index].SourceLevel, "official")
	}
	feed.ETag = response.ETag
	feed.LastModified = response.LastModified
	return operationalSourceResult{Feed: feed, Status: status, Message: limitText(message, 500)}
}

func makeOperationalFeed(source FeedSource, items []NewsItem, format string) fetchedFeed {
	return fetchedFeed{Title: source.Name, SiteURL: firstNonEmpty(source.SiteURL, source.URL), FeedURL: source.URL, Format: format, Items: items}
}

func parseOperationalFeed(response feedFetchResponse, inputURL string) (fetchedFeed, error) {
	if feed, err := parseXMLFeed(response.Body, firstNonEmpty(response.FinalURL, inputURL)); err == nil {
		return feed, nil
	}
	return parseJSONFeed(response.Body, firstNonEmpty(response.FinalURL, inputURL))
}

func makeOperationalNews(source FeedSource, externalID, title, rawURL, published, summary, author, sourceType, level, reason string) NewsItem {
	link := resolvePublicLink(source.URL, rawURL)
	if link == "" {
		link = source.URL
	}
	item := makeFetchedNews(firstNonEmpty(externalID, link, title), title, link, source.Name, author, published, summary, "", "", "", 0, source.URL)
	item.SourceType = firstNonEmpty(sourceType, source.Kind)
	item.PermissionScope = "public_read"
	item.SourceLevel = firstNonEmpty(level, "official")
	item.FailureReason = limitText(reason, 500)
	return item
}

func parseOperationalJSON(body []byte) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, errors.New("来源 JSON 无法解析")
	}
	return value, nil
}

func operationalRows(value any, keys ...string) []any {
	if rows, ok := value.([]any); ok {
		return rows
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if rows, ok := object[key].([]any); ok {
			return rows
		}
	}
	return nil
}

func operationalString(value any) string {
	switch typed := value.(type) {
	case string:
		return limitText(typed, 4000)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func operationalField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := operationalString(row[key]); value != "" {
			return value
		}
	}
	return ""
}

func operationalFirstArrayString(value any) string {
	values, ok := value.([]any)
	if !ok || len(values) == 0 {
		return ""
	}
	return operationalString(values[0])
}

func parseCrossrefOperationalJSON(body []byte, source FeedSource, fetchedAt string, limit int) ([]NewsItem, error) {
	root, err := parseOperationalJSON(body)
	if err != nil {
		return nil, err
	}
	message, _ := root["message"].(map[string]any)
	items := operationalRows(message["items"])
	result := make([]NewsItem, 0, minInt(limit, len(items)))
	for _, raw := range items {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := operationalFirstArrayString(row["title"])
		doi := operationalField(row, "DOI")
		link := operationalField(row, "URL")
		if link == "" && doi != "" {
			link = "https://doi.org/" + doi
		}
		container := operationalFirstArrayString(row["container-title"])
		abstract := operationalField(row, "abstract")
		if abstract == "" {
			abstract = container
		}
		date := crossrefDate(row["published"])
		author := ""
		if authors, ok := row["author"].([]any); ok && len(authors) > 0 {
			if first, ok := authors[0].(map[string]any); ok {
				author = operationalField(first, "family", "name")
			}
		}
		if title == "" && doi == "" {
			continue
		}
		result = append(result, makeOperationalNews(source, firstNonEmpty(doi, link), firstNonEmpty(title, doi), link, date, abstract, author, "api", "official", ""))
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Crossref 没有返回可用研究成果")
	}
	return result, nil
}

func crossrefDate(value any) string {
	object, _ := value.(map[string]any)
	parts, _ := object["date-parts"].([]any)
	if len(parts) == 0 {
		return ""
	}
	first, _ := parts[0].([]any)
	if len(first) == 0 {
		return ""
	}
	year := intFromJSON(first[0])
	month, day := 1, 1
	if len(first) > 1 {
		month = intFromJSON(first[1])
	}
	if len(first) > 2 {
		day = intFromJSON(first[2])
	}
	if year < 1 || month < 1 || day < 1 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02dT00:00:00Z", year, month, day)
}

func intFromJSON(value any) int {
	number, ok := value.(float64)
	if !ok {
		return 0
	}
	return int(number)
}

func parsePolicyOperationalJSON(body []byte, source FeedSource, fetchedAt string, limit int) ([]NewsItem, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, errors.New("政策接口 JSON 无法解析")
	}
	rows := operationalRows(value, "items", "data", "list")
	result := make([]NewsItem, 0, minInt(limit, len(rows)))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := operationalField(row, "TITLE", "title", "name")
		link := operationalField(row, "URL", "url", "link")
		if title == "" || link == "" {
			continue
		}
		result = append(result, makeOperationalNews(source, link, title, link, operationalField(row, "DOCRELPUBTIME", "publishTime", "date"), operationalField(row, "SUB_TITLE", "summary", "description"), "", "api", "official", ""))
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 {
		return nil, errors.New("政策接口没有返回可用文件")
	}
	return result, nil
}

func parseSSEOperationalJSON(body []byte, source FeedSource, fetchedAt string, limit int) ([]NewsItem, error) {
	root, err := parseOperationalJSON(body)
	if err != nil {
		return nil, err
	}
	rows := operationalRows(root["publishData"])
	result := make([]NewsItem, 0, minInt(limit, len(rows)))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		path := operationalField(row, "bulletinUrl")
		link := resolvePublicLink("https://www.sse.com.cn", path)
		if link == "" {
			continue
		}
		title := firstNonEmpty(operationalField(row, "bulletinTitle"), operationalField(row, "securityAbbr"))
		result = append(result, makeOperationalNews(source, firstNonEmpty(operationalField(row, "discloseId"), path), title, link, operationalField(row, "discloseDate"), operationalField(row, "securityAbbr"), "", "api", "official", ""))
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 {
		return nil, errors.New("上交所公告接口没有返回可用记录")
	}
	return result, nil
}

func parseSECOperationalJSON(body []byte, source FeedSource, fetchedAt string, limit int) ([]NewsItem, error) {
	root, err := parseOperationalJSON(body)
	if err != nil {
		return nil, err
	}
	hitsObject, _ := root["hits"].(map[string]any)
	hits := operationalRows(hitsObject["hits"])
	result := make([]NewsItem, 0, minInt(limit, len(hits)))
	for _, raw := range hits {
		hit, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		sourceRow, _ := hit["_source"].(map[string]any)
		accession := firstNonEmpty(operationalField(sourceRow, "adsh"), operationalField(hit, "_id"))
		form := operationalField(sourceRow, "form", "root_forms")
		cik := operationalFirstArrayString(sourceRow["ciks"])
		if !secAccessionRE.MatchString(accession) {
			continue
		}
		cleanAccession := strings.ReplaceAll(accession, "-", "")
		link := "https://www.sec.gov/edgar/search/"
		if secCIKRE.MatchString(cik) {
			cleanCIK := strings.TrimLeft(cik, "0")
			if cleanCIK != "" {
				link = fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s-index.html", cleanCIK, cleanAccession, accession)
			}
		}
		title := firstNonEmpty(operationalField(sourceRow, "file_description"), form, "SEC filing")
		if names, ok := sourceRow["display_names"].([]any); ok && len(names) > 0 {
			title = operationalString(names[0]) + " · " + title
		}
		result = append(result, makeOperationalNews(source, accession+":"+form, title, link, firstNonEmpty(operationalField(sourceRow, "file_date"), operationalField(sourceRow, "period_ending")), operationalField(sourceRow, "file_description"), "", "api", "official_fallback", "SEC RSS 不可用；已降级到 SEC 官方公开检索"))
		if len(result) >= limit {
			break
		}
	}
	if len(result) == 0 {
		return nil, errors.New("SEC 公共检索没有返回可用申报")
	}
	return result, nil
}

func secOperationalFallbackURL(fetchedAt string) string {
	end := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, fetchedAt); err == nil {
		end = parsed.UTC()
	}
	values := url.Values{}
	values.Set("q", "")
	values.Set("dateRange", "custom")
	values.Set("forms", "10-K,10-Q,8-K")
	values.Set("startdt", end.Add(-365*24*time.Hour).Format("2006-01-02"))
	values.Set("enddt", end.Format("2006-01-02"))
	values.Set("from", "0")
	values.Set("size", fmt.Sprint(operationalSourceLimit))
	return "https://efts.sec.gov/LATEST/search-index?" + values.Encode()
}

func crossrefOperationalURL(endpoint, fetchedAt string) string {
	end := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, fetchedAt); err == nil {
		end = parsed.UTC()
	}
	values := url.Values{}
	values.Set("query", "artificial intelligence")
	values.Set("rows", fmt.Sprint(operationalSourceLimit))
	values.Set("offset", "0")
	values.Set("sort", "published")
	values.Set("order", "desc")
	values.Set("filter", "until-pub-date:"+end.Format("2006-01-02"))
	values.Set("select", "DOI,title,URL,published,author,container-title,abstract")
	return endpoint + "?" + values.Encode()
}

func collectEPOOperational(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	datesResponse, err := fetcher(ctx, spec.endpoints[0], "", "")
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	dates := uniqueMatches(operationalEPODatesRE, string(datesResponse.Body))
	if len(dates) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "EPO 没有返回公开发布日期"}
	}
	date := dates[0]
	listURL := "https://data.epo.org/publication-server/rest/v1.2/publication-dates/" + date + "/patents"
	listResponse, err := fetcher(ctx, listURL, "", "")
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	patents := uniqueMatches(operationalEPOPatentRE, string(listResponse.Body))
	if len(patents) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "EPO 没有返回公开专利编号"}
	}
	items := make([]NewsItem, 0, minInt(operationalSourceLimit, len(patents)))
	for _, patent := range patents {
		link := "https://data.epo.org/publication-server/rest/v1.2/patents/" + patent + "/document.html"
		items = append(items, makeOperationalNews(source, patent, "EPO 公开专利 "+patent, link, date, "欧洲专利局公开专利发布数据", "", "api", "official", ""))
		if len(items) >= operationalSourceLimit {
			break
		}
	}
	return operationalFeedResult(makeOperationalFeed(source, items, "api"), "ok", "", listResponse)
}

func uniqueMatches(pattern *regexp.Regexp, value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, operationalSourceLimit)
	for _, match := range pattern.FindAllStringSubmatch(value, operationalSourceLimit*2) {
		if len(match) < 2 {
			continue
		}
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		result = append(result, match[1])
		if len(result) >= operationalSourceLimit {
			break
		}
	}
	return result
}

func collectHTMLOperational(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string, keep func(string) bool, sourceType, status, fallbackMessage string) operationalSourceResult {
	var lastErr error
	for _, endpoint := range spec.endpoints {
		response, err := fetcher(ctx, endpoint, "", "")
		if err != nil {
			lastErr = err
			continue
		}
		pageTitle, links := parseOperationalHTML(response.Body, response.FinalURL)
		items := make([]NewsItem, 0, operationalSourceLimit)
		seen := make(map[string]struct{})
		for _, link := range links {
			if link.title == "" || len([]rune(link.title)) < 4 || !keep(link.url) {
				continue
			}
			if _, ok := seen[link.url]; ok {
				continue
			}
			seen[link.url] = struct{}{}
			level := "official"
			if status == "partial" {
				level = "official_fallback"
			}
			items = append(items, makeOperationalNews(source, link.url, link.title, link.url, fetchedAt, pageTitle, "", sourceType, level, fallbackMessage))
			if len(items) >= operationalSourceLimit {
				break
			}
		}
		if len(items) > 0 {
			return operationalFeedResult(makeOperationalFeed(source, items, sourceType), status, fallbackMessage, response)
		}
		lastErr = errors.New("官方页面只返回导航或脚本壳，没有可用公开记录")
	}
	return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(lastErr), 500)}
}

type operationalHTMLLink struct {
	url   string
	title string
}

func parseOperationalHTML(body []byte, baseURL string) (string, []operationalHTMLLink) {
	root, err := htmlnode.Parse(strings.NewReader(string(body)))
	if err != nil {
		return "", nil
	}
	title := ""
	links := make([]operationalHTMLLink, 0, operationalSourceLimit)
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if len(links) >= operationalHTMLLinkLimit {
			return
		}
		if node.Type == htmlnode.ElementNode {
			switch strings.ToLower(node.Data) {
			case "script", "style", "noscript", "svg":
				return
			case "title":
				if title == "" {
					title = limitText(operationalNodeText(node), 240)
				}
			case "a":
				href := ""
				for _, attr := range node.Attr {
					if strings.EqualFold(attr.Key, "href") {
						href = attr.Val
						break
					}
				}
				link := resolvePublicLink(baseURL, href)
				if link != "" {
					links = append(links, operationalHTMLLink{url: link, title: limitText(operationalNodeText(node), 240)})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return title, links
}

func operationalNodeText(node *htmlnode.Node) string {
	var parts []string
	textLength := 0
	var walk func(*htmlnode.Node)
	walk = func(current *htmlnode.Node) {
		if textLength >= operationalNodeTextLimit {
			return
		}
		if current.Type == htmlnode.TextNode {
			remaining := operationalNodeTextLimit - textLength
			value := []rune(current.Data)
			if len(value) > remaining {
				value = value[:remaining]
			}
			parts = append(parts, string(value))
			textLength += len(value)
			return
		}
		if current.Type == htmlnode.ElementNode {
			switch strings.ToLower(current.Data) {
			case "script", "style", "noscript", "svg":
				return
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return cleanPlain(strings.Join(parts, " "))
}

func firstXMLText(body []byte, names ...string) string {
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[strings.ToLower(name)] = struct{}{}
	}
	decoder := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if _, ok := wanted[strings.ToLower(start.Name.Local)]; !ok {
			continue
		}
		var text string
		if decoder.DecodeElement(&text, &start) == nil {
			return cleanPlain(text)
		}
	}
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return limitText(err.Error(), 500)
}

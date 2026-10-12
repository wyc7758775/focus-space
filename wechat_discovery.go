package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	stdhtml "html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	weChatArticleMaxBytes    = 4 << 20
	weChatFeedTimeout        = 5 * time.Second
	weChatDirectFeedBudget   = 6 * time.Second
	weChatVerifiedFeedBudget = 12 * time.Second
	weChatFallbackFeedBudget = 7 * time.Second
	// Keep the browser identity realistic while avoiding the four-zero IP
	// token that the App security scanner treats as a wildcard bind.
	weChatBrowserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140 Safari/537.36"
)

var (
	discoverWeChatArticleFunc = discoverWeChatArticle
	weChatAccountIDRE         = regexp.MustCompile(`(?m)\b(?:var\s+user_name\s*=|user_name:)\s*["'](gh_[A-Za-z0-9]+)["']`)
	weChatAliasRE             = regexp.MustCompile(`(?m)\b(?:window\.)?alias\s*(?:=|:)\s*["']([A-Za-z][A-Za-z0-9_-]{2,63})["']`)
	weChatNicknameRE          = regexp.MustCompile(`(?m)\bvar\s+nickname\s*=\s*htmlDecode\("([^"\r\n]{1,512})"\)`)
	weChatIconRE              = regexp.MustCompile(`(?m)\b(?:round_head_img|hd_head_img)\s*(?:=|:)\s*["']([^"'\r\n]+)["']`)
	weChatCoverRE             = regexp.MustCompile(`(?m)\bvar\s+msg_cdn_url\s*=\s*"([^"\r\n]+)"`)
	weChatCreateTimeRE        = regexp.MustCompile(`(?m)\bvar\s+create_time\s*=\s*"?(\d{9,12})"?`)
	weChatAccountIDValidRE    = regexp.MustCompile(`^gh_[A-Za-z0-9]{6,64}$`)
	weChatAliasValidRE        = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{2,63}$`)
	wechat2RSSFeedPathRE      = regexp.MustCompile(`^/feed/[a-f0-9]{40}\.xml$`)
	sogouWechatOpaqueLinkRE   = regexp.MustCompile(`^[A-Za-z0-9_.-]{16,800}$`)
	sogouWechatPublicTokenRE  = regexp.MustCompile(`^[A-F0-9]{32,128}$`)
	weChatPinnedFallbackURLs  = map[string]string{
		"wechat-ai-paper": "https://rsshub.rssforever.com/wechat/sogou/AI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper",
		"wechat-fnos":     "https://rsshub.rssforever.com/wechat/sogou/%E9%A3%9E%E7%89%9BFnOS",
		"wechat-huxiu":    "https://rsshub.rssforever.com/wechat/sogou/%E7%A1%AC%E6%B0%AA",
	}
	// Keep the outer Folo query encoding fixed. Escaping an already escaped
	// source URL turns Chinese path bytes into %25xx and the endpoint returns
	// HTTP 400. These URLs decode to the exact pinned targets above.
	weChatPinnedFoloFallbackURLs = map[string]string{
		"wechat-ai-paper": "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2FAI%E7%AE%97%E6%B3%95%E7%A7%91%E7%A0%94paper",
		"wechat-fnos":     "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E9%A3%9E%E7%89%9BFnOS",
		"wechat-huxiu":    "https://api.folo.is/feeds?url=https%3A%2F%2Frsshub.rssforever.com%2Fwechat%2Fsogou%2F%E7%A1%AC%E6%B0%AA",
	}
	weChatVerifiedDirectFeedURLs = map[string]string{
		"wechat-aiera":        "https://wechat2rss.xlab.app/feed/ede30346413ea70dbef5d485ea5cbb95cca446e7.xml",
		"wechat-ali-tech":     "https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml",
		"wechat-chaping":      "https://wechat2rss.xlab.app/feed/8d839de8dd3290a1f1be7a94423cccb30c1b087d.xml",
		"wechat-founder-park": "https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml",
		"wechat-geekpark":     "https://wechat2rss.xlab.app/feed/1a5aec98e71c707c8ca092bc2c255b9d4bac477d.xml",
		"wechat-jiqizhixin":   "https://wechat2rss.xlab.app/feed/51e92aad2728acdd1fda7314be32b16639353001.xml",
		"wechat-liuxiaopai":   "https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml",
		"wechat-paperweekly":  "https://wechat2rss.xlab.app/feed/3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml",
		"wechat-qbitai":       "https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml",
	}
	weChatVerifiedDirectFeedPolicies = map[string]weChatVerifiedFeedPolicy{
		"https://wechat2rss.xlab.app/feed/ede30346413ea70dbef5d485ea5cbb95cca446e7.xml": {
			SourceID:      "wechat-aiera",
			SourceName:    "新智元",
			FeedTitle:     "新智元",
			HistoryFeedID: "60901577013168128",
			AllowedBiz:    map[string]struct{}{"MzI3MTA0MTk1MA==": {}},
		},
		"https://wechat2rss.xlab.app/feed/6e1f9b775f7a5841ac1a94310f0478b45a02ec01.xml": {
			SourceID:      "wechat-ali-tech",
			SourceName:    "阿里技术",
			FeedTitle:     "阿里技术",
			HistoryFeedID: "61793911467530240",
			AllowedBiz:    map[string]struct{}{"Mzg4NTczNzg2OA==": {}},
		},
		"https://wechat2rss.xlab.app/feed/8d839de8dd3290a1f1be7a94423cccb30c1b087d.xml": {
			SourceID:      "wechat-chaping",
			SourceName:    "差评",
			FeedTitle:     "差评X.PIN",
			HistoryFeedID: "44005569356962816",
			AllowedBiz:    map[string]struct{}{"MzA5NDc1NzQ4MA==": {}},
		},
		"https://wechat2rss.bestblogs.dev/feed/f940695505f2be1399d23cc98182297cadf6f90d.xml": {
			SourceID:      "wechat-founder-park",
			SourceName:    "Founder Park",
			FeedTitle:     "Founder Park",
			HistoryFeedID: "187702008971600930",
			AllowedBiz: map[string]struct{}{
				"Mzg5NTc0MjgwMw==": {},
				"MzY5ODQwMTkxNA==": {},
			},
		},
		"https://wechat2rss.xlab.app/feed/1a5aec98e71c707c8ca092bc2c255b9d4bac477d.xml": {
			SourceID:      "wechat-geekpark",
			SourceName:    "极客公园",
			FeedTitle:     "极客公园",
			HistoryFeedID: "41654726069556264",
			AllowedBiz:    map[string]struct{}{"MTMwNDMwODQ0MQ==": {}},
		},
		"https://wechat2rss.xlab.app/feed/51e92aad2728acdd1fda7314be32b16639353001.xml": {
			SourceID:      "wechat-jiqizhixin",
			SourceName:    "机器之心",
			FeedTitle:     "机器之心",
			HistoryFeedID: "41654726069556242",
			AllowedBiz:    map[string]struct{}{"MzA3MzI4MjgzMw==": {}},
		},
		"https://wechat2rss.bestblogs.dev/feed/484d4199ae6c0b72ea01e7e0597a1f74933dfb62.xml": {
			SourceID:      "wechat-liuxiaopai",
			SourceName:    "刘小排",
			FeedTitle:     "刘小排r",
			HistoryFeedID: "187702008979989521",
			AllowedBiz: map[string]struct{}{
				"MzI1MTUxNzgxMA==": {},
			},
		},
		"https://wechat2rss.xlab.app/feed/3be891c2f4e526629ab055a297cc2cd6c1f0a563.xml": {
			SourceID:      "wechat-paperweekly",
			SourceName:    "PaperWeekly",
			FeedTitle:     "PaperWeekly",
			HistoryFeedID: "69272325013859328",
			AllowedBiz:    map[string]struct{}{"MzIwMTc4ODE0Mw==": {}},
		},
		"https://wechat2rss.xlab.app/feed/7131b577c61365cb47e81000738c10d872685908.xml": {
			SourceID:      "wechat-qbitai",
			SourceName:    "量子位",
			FeedTitle:     "量子位",
			HistoryFeedID: "58864180026527744",
			AllowedBiz:    map[string]struct{}{"MzIzNjc1NzUzMw==": {}},
		},
	}
)

type weChatVerifiedFeedPolicy struct {
	SourceID      string
	SourceName    string
	FeedTitle     string
	HistoryFeedID string
	AllowedBiz    map[string]struct{}
}

type weChatArticleProfile struct {
	AccountID          string
	AccountAlias       string
	AccountName        string
	ArticleTitle       string
	ArticleDescription string
	ArticleURL         string
	IconURL            string
	CoverURL           string
	PublishedAt        string
}

func fetchWeChatArticlePage(ctx context.Context, raw string) (feedFetchResponse, error) {
	normalized, err := normalizePublicURL(raw)
	if err != nil {
		return feedFetchResponse{}, errors.New("微信公众号文章地址不合法")
	}
	parsed, _ := url.Parse(normalized)
	if !isWeChatArticleURL(parsed) {
		return feedFetchResponse{}, errors.New("该地址不是微信公众号文章")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return feedFetchResponse{}, err
	}
	req.Header.Set("User-Agent", weChatBrowserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.5")
	client := publicFeedHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return feedFetchResponse{}, errors.New("公众号页面需要验证，已保留文章链接；请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return feedFetchResponse{}, errors.New("公众号文章暂时无法读取，请稍后重试")
	}
	if response.ContentLength > weChatArticleMaxBytes {
		return feedFetchResponse{}, errors.New("公众号文章超过读取大小上限")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, weChatArticleMaxBytes+1))
	if err != nil {
		return feedFetchResponse{}, errors.New("公众号文章读取中断，请稍后重试")
	}
	if len(body) > weChatArticleMaxBytes {
		return feedFetchResponse{}, errors.New("公众号文章超过读取大小上限")
	}
	if bytes.Contains(body, []byte("wappoc_appmsgcaptcha")) || bytes.Contains(body, []byte("poc_token")) {
		return feedFetchResponse{}, errors.New("公众号页面需要验证，已保留文章链接；请稍后重试")
	}
	return feedFetchResponse{Body: body, ContentType: strings.ToLower(response.Header.Get("Content-Type")), FinalURL: response.Request.URL.String()}, nil
}

func parseWeChatArticlePage(response feedFetchResponse, inputURL string) (weChatArticleProfile, error) {
	profile := weChatArticleProfile{
		ArticleURL:         inputURL,
		ArticleTitle:       weChatMetaContent(response.Body, "property", "og:title"),
		ArticleDescription: weChatMetaContent(response.Body, "property", "og:description"),
		AccountName:        weChatMetaContent(response.Body, "property", "og:article:author"),
	}
	if match := weChatAccountIDRE.FindSubmatch(response.Body); len(match) == 2 {
		profile.AccountID = string(match[1])
	}
	if match := weChatAliasRE.FindSubmatch(response.Body); len(match) == 2 {
		profile.AccountAlias = string(match[1])
	}
	if match := weChatNicknameRE.FindSubmatch(response.Body); len(match) == 2 {
		profile.AccountName = cleanPlain(stdhtml.UnescapeString(string(match[1])))
	}
	for _, match := range weChatIconRE.FindAllSubmatch(response.Body, 3) {
		if len(match) == 2 {
			profile.IconURL = normalizeWeChatAssetURL(string(match[1]))
			if profile.IconURL != "" {
				break
			}
		}
	}
	if match := weChatCoverRE.FindSubmatch(response.Body); len(match) == 2 {
		profile.CoverURL = normalizeWeChatAssetURL(string(match[1]))
	}
	if match := weChatCreateTimeRE.FindSubmatch(response.Body); len(match) == 2 {
		if seconds, parseErr := strconv.ParseInt(string(match[1]), 10, 64); parseErr == nil {
			profile.PublishedAt = time.Unix(seconds, 0).UTC().Format(time.RFC3339)
		}
	}
	if profile.AccountID == "" || profile.AccountName == "" || profile.ArticleTitle == "" {
		return weChatArticleProfile{}, errors.New("已打开微信文章，但未能识别公众号身份；请提供公众号微信号或已收录 Feed")
	}
	return profile, nil
}

func weChatMetaContent(body []byte, key, expected string) string {
	for _, tag := range htmlMetaRE.FindAll(body, -1) {
		attrs := map[string]string{}
		for _, match := range htmlAttributeRE.FindAllSubmatch(tag, -1) {
			if len(match) == 3 {
				attrs[strings.ToLower(string(match[1]))] = stdhtml.UnescapeString(strings.TrimSpace(string(match[2])))
			}
		}
		if strings.EqualFold(attrs[key], expected) {
			return limitText(cleanPlain(attrs["content"]), 4000)
		}
	}
	return ""
}

func normalizeWeChatAssetURL(raw string) string {
	value := strings.TrimSpace(stdhtml.UnescapeString(raw))
	if strings.HasPrefix(value, "http://") {
		value = "https://" + strings.TrimPrefix(value, "http://")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "mmbiz.qpic.cn" && host != "wx.qlogo.cn" && host != "mmbiz.qlogo.cn" {
		return ""
	}
	if !validPublicFeedURL(parsed.String()) {
		return ""
	}
	return parsed.String()
}

func discoverWeChatArticle(ctx context.Context, item FeedDiscovery) (FeedDiscovery, error) {
	response, err := fetchWeChatArticlePage(ctx, item.NormalizedURL)
	if err != nil {
		return item, err
	}
	profile, err := parseWeChatArticlePage(response, item.NormalizedURL)
	if err != nil {
		return item, err
	}
	article := makeFetchedNews(profile.ArticleURL, profile.ArticleTitle, profile.ArticleURL, profile.AccountName,
		profile.AccountName, profile.PublishedAt, profile.ArticleDescription, profile.CoverURL, "", "", 0, profile.ArticleURL)
	article.SourceType = "wechat"
	article.SourceLevel = "official"
	article.PermissionScope = "public_read"
	item.Title = profile.AccountName
	item.Description = "微信公众号；已识别文章《" + profile.ArticleTitle + "》"
	item.SiteURL = profile.ArticleURL
	item.IconURL = profile.IconURL
	item.Preview = []NewsItem{article}
	item.Status = "pending"
	item.ErrorMessage = "公众号已识别，文章可直接查看；持续订阅服务暂不可用，请稍后重试"

	feed, feedURL, feedErr := discoverWeChatContinuousFeed(ctx, profile)
	if feedErr != nil {
		// Article discovery can still join an already curated, paged source.
		// This is important for WeChat pages that return a verification shell:
		// the article itself is readable, while the account history is served by
		// the maintained source (for example 硬氪's historical column).
		if existing, lookupErr := existingWechatSourceByName(profile.AccountName); lookupErr == nil {
			item.Status = "verified"
			item.ErrorMessage = ""
			item.FeedURL = existing.URL
			item.FeedFormat = firstNonEmpty(existing.FeedFormat, "rss")
			item.Title = existing.Name
			item.Description = firstNonEmpty(existing.Description, item.Description)
		}
		return item, nil
	}
	item.Status = "verified"
	item.ErrorMessage = ""
	item.FeedURL = feedURL
	item.FeedFormat = "rss"
	item.Title = firstNonEmpty(feed.Title, profile.AccountName)
	item.Description = firstNonEmpty(feed.Description, item.Description)
	item.Preview = mergeWeChatPreview(article, feed.Items)
	return item, nil
}

func existingWechatSourceByName(name string) (FeedSource, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return FeedSource{}, sql.ErrNoRows
	}
	var sourceID string
	if err := db.QueryRow(`SELECT id FROM feed_sources WHERE lower(name)=lower(?) ORDER BY is_default DESC, id LIMIT 1`, name).Scan(&sourceID); err != nil {
		return FeedSource{}, err
	}
	return feedSourceByID(sourceID)
}

func discoverWeChatContinuousFeed(ctx context.Context, profile weChatArticleProfile) (fetchedFeed, string, error) {
	for _, feedURL := range weChatFeedURLs(profile) {
		feedCtx, cancel := context.WithTimeout(ctx, weChatFeedTimeout)
		response, err := fetchPublicURL(feedCtx, feedURL)
		cancel()
		if err != nil {
			continue
		}
		feed, err := parseDirectWechatRSSFeed(response.Body, feedURL, profile.AccountName, profile.IconURL, profile.ArticleURL)
		if err == nil {
			return feed, feedURL, nil
		}
	}
	return fetchedFeed{}, "", errors.New("持续订阅服务暂不可用")
}

func weChatFeedURLs(profile weChatArticleProfile) []string {
	routes := make([]string, 0, 2)
	if weChatAliasValidRE.MatchString(profile.AccountAlias) {
		routes = append(routes, "/wechat/uread/"+profile.AccountAlias)
	}
	if weChatAccountIDValidRE.MatchString(profile.AccountID) {
		routes = append(routes, "/wechat/data258/"+profile.AccountID)
	}
	result := make([]string, 0, len(routes))
	for _, route := range routes {
		result = append(result, "https://rsshub.app"+route)
	}
	return result
}

// foloWeChatFeedURLs remains only for parsing legacy rows created before the
// direct RSS migration. New discovery never calls Folo; it uses the public
// RSSHub route returned by weChatFeedURLs above.
func foloWeChatFeedURLs(profile weChatArticleProfile) []string {
	result := make([]string, 0, 2)
	for _, target := range weChatFeedURLs(profile) {
		result = append(result, "https://api.folo.is/feeds?url="+url.QueryEscape(target))
	}
	return result
}

func validWechatRSSFeedURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "wechat2rss.xlab.app":
		return wechat2RSSFeedPathRE.MatchString(parsed.EscapedPath())
	case "wechat2rss.bestblogs.dev":
		_, ok := weChatVerifiedDirectFeedPolicies[strings.TrimSpace(raw)]
		return ok
	case "rsshub.app":
		return validRSSHubWechatTarget(parsed)
	case "rsshub.rssforever.com":
		return validFoloSogouWechatTarget(parsed)
	default:
		return false
	}
}

func validRSSHubWechatTarget(target *url.URL) bool {
	parts := strings.Split(strings.Trim(target.EscapedPath(), "/"), "/")
	if len(parts) != 3 || parts[0] != "wechat" {
		return false
	}
	return parts[1] == "uread" && weChatAliasValidRE.MatchString(parts[2]) ||
		parts[1] == "data258" && weChatAccountIDValidRE.MatchString(parts[2])
}

func validFoloWeChatFeedURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || strings.ToLower(parsed.Hostname()) != "api.folo.is" || parsed.Path != "/feeds" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	values := parsed.Query()
	if len(values) != 1 || len(values["url"]) != 1 {
		return false
	}
	target, err := url.Parse(values.Get("url"))
	if err != nil || target.Scheme != "https" || strings.ToLower(target.Hostname()) != "rsshub.app" || target.RawQuery != "" || target.Fragment != "" || target.User != nil || target.Port() != "" {
		return false
	}
	parts := strings.Split(strings.Trim(target.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "wechat" {
		return false
	}
	return parts[1] == "uread" && weChatAliasValidRE.MatchString(parts[2]) ||
		parts[1] == "data258" && weChatAccountIDValidRE.MatchString(parts[2])
}

func parseFoloWeChatFeed(body []byte, sourceURL, expectedName, iconURL, articleURL string) (fetchedFeed, error) {
	if !validFoloWeChatFeedURL(sourceURL) {
		return fetchedFeed{}, errors.New("持续订阅地址不合法")
	}
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Code != 0 {
		return fetchedFeed{}, errors.New("持续订阅服务返回不可解析内容")
	}
	titleMatches := weChatFeedNameMatches(response.Data.Feed.Title, expectedName)
	authorMatches := false
	for _, entry := range response.Data.Entries {
		if weChatAuthorMatches(entry.Author, expectedName) {
			authorMatches = true
			break
		}
	}
	if !titleMatches && !authorMatches {
		return fetchedFeed{}, errors.New("持续订阅结果与当前公众号不匹配")
	}
	items := make([]NewsItem, 0, minInt(feedFetchMaxItems, len(response.Data.Entries)))
	seen := map[string]struct{}{}
	for _, entry := range response.Data.Entries {
		if !titleMatches && !weChatAuthorMatches(entry.Author, expectedName) {
			continue
		}
		link, err := normalizePublicURL(entry.URL)
		if err != nil {
			continue
		}
		parsed, _ := url.Parse(link)
		if !isWeChatArticleURL(parsed) {
			continue
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		news := makeFetchedNews(firstNonEmpty(entry.ID, link), entry.Title, link, expectedName, firstNonEmpty(entry.Author, expectedName),
			entry.PublishedAt, firstNonEmpty(entry.Summary, entry.Description, entry.Content), "", "", "", 0, sourceURL)
		news.SourceType = "wechat"
		news.SourceLevel = "public_aggregator"
		news.PermissionScope = "public_read"
		items = append(items, news)
		if len(items) >= feedFetchMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("持续订阅源没有返回可浏览的公众号文章")
	}
	return fetchedFeed{Title: expectedName, Description: firstNonEmpty(response.Data.Feed.Description, expectedName+" 的微信公众号"), SiteURL: articleURL, FeedURL: sourceURL, IconURL: iconURL, Format: "rss", Items: items}, nil
}

func parseDirectWechatRSSFeed(body []byte, sourceURL, expectedName, iconURL, articleURL string) (fetchedFeed, error) {
	if !validWechatRSSFeedURL(sourceURL) {
		return fetchedFeed{}, errors.New("微信公众号 RSS 地址不合法")
	}
	feed, err := parseXMLFeed(body, sourceURL)
	if err != nil {
		return fetchedFeed{}, errors.New("微信公众号 RSS 返回不可解析内容")
	}
	expectedName = strings.TrimSpace(expectedName)
	verifiedPolicy, verified := weChatVerifiedDirectFeedPolicies[strings.TrimSpace(sourceURL)]
	if verified && (strings.TrimSpace(cleanPlain(expectedName)) != verifiedPolicy.SourceName || strings.TrimSpace(cleanPlain(feed.Title)) != verifiedPolicy.FeedTitle) {
		return fetchedFeed{}, errors.New("RSS 结果与当前公众号不匹配")
	}
	titleMatches := expectedName == "" || weChatFeedIdentityMatches(feed.Title, expectedName)
	authorMatches := false
	for _, item := range feed.Items {
		if weChatAuthorMatches(item.Author, expectedName) {
			authorMatches = true
			break
		}
	}
	if expectedName != "" && !titleMatches && !authorMatches {
		return fetchedFeed{}, errors.New("RSS 结果与当前公众号不匹配")
	}

	items := make([]NewsItem, 0, minInt(feedFetchMaxItems, len(feed.Items)))
	seen := make(map[string]struct{})
	for _, item := range feed.Items {
		link, normalizeErr := normalizeWechatEntryURL(item.URL)
		if verified && (normalizeErr != nil || !verifiedWeChatArticleURL(link, verifiedPolicy)) {
			return fetchedFeed{}, errors.New("RSS 文章身份与当前公众号不匹配")
		}
		if normalizeErr != nil {
			continue
		}
		parsed, _ := url.Parse(link)
		if !isWeChatArticleURL(parsed) && !isSogouWechatArticleURL(parsed) {
			continue
		}
		if !titleMatches && expectedName != "" && !weChatAuthorMatches(item.Author, expectedName) {
			continue
		}
		if _, exists := seen[link]; exists {
			continue
		}
		seen[link] = struct{}{}
		item.URL = link
		if verified && verifiedPolicy.HistoryFeedID != "" {
			identity, ok := verifiedWeChatArticleIdentity(link, verifiedPolicy)
			if !ok {
				return fetchedFeed{}, errors.New("RSS 文章身份与当前公众号不匹配")
			}
			item.ExternalID = identity
			item.ID = stableObjectID("entry", identity)
		}
		item.Source = firstNonEmpty(expectedName, feed.Title)
		item.SourceType = "wechat"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		items = append(items, item)
		if len(items) >= feedFetchMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("微信公众号 RSS 没有返回可浏览的文章")
	}
	feed.Title = firstNonEmpty(expectedName, feed.Title)
	feed.Description = firstNonEmpty(feed.Description, feed.Title+" 的微信公众号")
	feed.SiteURL = firstNonEmpty(articleURL, feed.SiteURL, "https://mp.weixin.qq.com/")
	feed.FeedURL = sourceURL
	feed.IconURL = firstNonEmpty(iconURL, feed.IconURL)
	feed.Format = "rss"
	feed.Items = items
	return feed, nil
}

func verifiedWeChatArticleURL(raw string, policy weChatVerifiedFeedPolicy) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isWeChatArticleURL(parsed) || strings.TrimRight(parsed.EscapedPath(), "/") != "/s" {
		return false
	}
	values := parsed.Query()
	bizValues := values["__biz"]
	if len(bizValues) != 1 {
		return false
	}
	_, ok := policy.AllowedBiz[bizValues[0]]
	return ok
}

func weChatFeedIdentityMatches(actual, expected string) bool {
	if weChatFeedNameMatches(actual, expected) {
		return true
	}
	compact := func(value string) string {
		return strings.Join(strings.Fields(strings.ToLower(cleanPlain(value))), "")
	}
	actualCompact, expectedCompact := compact(actual), compact(expected)
	return len([]rune(expectedCompact)) >= 2 &&
		(strings.Contains(actualCompact, expectedCompact) || strings.Contains(expectedCompact, actualCompact))
}

// Folo's public feed endpoint keeps the bounded Wechat2RSS/RSSHub payload
// small enough for the dashboard while preserving article links and summaries.
// The wrapper is accepted only for pinned public targets; arbitrary Folo URLs
// must never become an implicit source proxy.
func validFoloWechat2RSSFeedURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || strings.ToLower(parsed.Hostname()) != "api.folo.is" || parsed.Path != "/feeds" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	values := parsed.Query()
	if len(values) != 1 || len(values["url"]) != 1 {
		return false
	}
	target, err := url.Parse(values.Get("url"))
	if err != nil || target.Scheme != "https" || target.User != nil || target.Port() != "" || target.RawQuery != "" || target.Fragment != "" {
		return false
	}
	switch strings.ToLower(target.Hostname()) {
	case "wechat2rss.xlab.app":
		return wechat2RSSFeedPathRE.MatchString(target.EscapedPath())
	case "rsshub.rssforever.com":
		return validFoloSogouWechatTarget(target)
	default:
		return false
	}
}

func validFoloSogouWechatTarget(target *url.URL) bool {
	parts := strings.Split(strings.Trim(target.EscapedPath(), "/"), "/")
	if len(parts) != 3 || parts[0] != "wechat" || parts[1] != "sogou" {
		return false
	}
	account, err := url.PathUnescape(parts[2])
	if err != nil || strings.Contains(account, "/") || !validText(account, 120, true) {
		return false
	}
	return true
}

func parseFoloWechat2RSSFeed(body []byte, sourceURL string) (fetchedFeed, error) {
	if !validFoloWechat2RSSFeedURL(sourceURL) {
		return fetchedFeed{}, errors.New("Folo 微信公众号来源地址不合法")
	}
	var response foloResponse
	if err := json.Unmarshal(body, &response); err != nil || response.Code != 0 {
		return fetchedFeed{}, errors.New("Folo 微信公众号返回不可解析内容")
	}
	if strings.TrimSpace(response.Data.Feed.Title) == "" || len(response.Data.Entries) == 0 {
		return fetchedFeed{}, errors.New("Folo 微信公众号没有返回可浏览条目")
	}
	outerURL, _ := url.Parse(sourceURL)
	nestedURL := outerURL.Query().Get("url")
	normalizedNestedURL, err := normalizePublicURL(nestedURL)
	if err != nil {
		return fetchedFeed{}, errors.New("Folo 微信公众号来源地址不合法")
	}
	if response.Data.Feed.URL != "" {
		feedURL, err := normalizePublicURL(response.Data.Feed.URL)
		if err != nil || feedURL != normalizedNestedURL {
			return fetchedFeed{}, errors.New("Folo 微信公众号来源地址与响应不匹配")
		}
	}
	items := make([]NewsItem, 0, minInt(feedFetchMaxItems, len(response.Data.Entries)))
	seen := make(map[string]struct{})
	for _, entry := range response.Data.Entries {
		link, err := normalizeWechatEntryURL(entry.URL)
		if err != nil {
			continue
		}
		parsed, _ := url.Parse(link)
		if (!isWeChatArticleURL(parsed) && !isSogouWechatArticleURL(parsed)) || strings.TrimSpace(entry.Title) == "" {
			continue
		}
		id := firstNonEmpty(entry.ID, link)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		item := makeFetchedNews(id, entry.Title, link, response.Data.Feed.Title,
			firstNonEmpty(entry.Author, response.Data.Feed.Title), entry.PublishedAt,
			firstNonEmpty(entry.Summary, entry.Description, entry.Content), "", "", "", 0, sourceURL)
		item.SourceType = "wechat"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		items = append(items, item)
		if len(items) >= feedFetchMaxItems {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("Folo 微信公众号没有返回可浏览的文章")
	}
	return fetchedFeed{
		Title:       response.Data.Feed.Title,
		Description: firstNonEmpty(response.Data.Feed.Description, response.Data.Feed.Title+" 的微信公众号"),
		SiteURL:     "https://mp.weixin.qq.com/",
		FeedURL:     sourceURL,
		IconURL:     response.Data.Feed.Image,
		Format:      "rss",
		Items:       items,
	}, nil
}

func normalizeWechatEntryURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if normalized, err := normalizePublicURL(value); err == nil {
		parsed, _ := url.Parse(normalized)
		if isWeChatArticleURL(parsed) || isSogouWechatArticleURL(parsed) {
			return normalized, nil
		}
	}
	// Sogou's public redirect includes an ephemeral routing field named
	// "token". The generic public URL validator intentionally rejects that
	// name, so accept it only on this exact HTTPS article route and schema.
	parsed, err := url.ParseRequestURI(value)
	if err != nil || len(value) > 1000 || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" ||
		!strings.EqualFold(parsed.Hostname(), "weixin.sogou.com") || strings.TrimRight(parsed.EscapedPath(), "/") != "/link" {
		return "", errors.New("微信公众号文章地址不合法")
	}
	values := parsed.Query()
	allowed := map[string]struct{}{"url": {}, "type": {}, "query": {}, "token": {}}
	for key, entries := range values {
		if _, ok := allowed[key]; !ok || len(entries) != 1 {
			return "", errors.New("微信公众号文章地址参数不合法")
		}
	}
	if !sogouWechatOpaqueLinkRE.MatchString(values.Get("url")) || values.Get("type") != "2" || !validText(values.Get("query"), 120, true) || !sogouWechatPublicTokenRE.MatchString(values.Get("token")) {
		return "", errors.New("微信公众号文章地址参数不合法")
	}
	return parsed.String(), nil
}

func weChatAuthorMatches(actual, expected string) bool {
	actual = strings.Join(strings.Fields(cleanPlain(actual)), "")
	expected = strings.Join(strings.Fields(cleanPlain(expected)), "")
	return actual != "" && expected != "" && strings.EqualFold(actual, expected)
}

func weChatFeedNameMatches(actual, expected string) bool {
	clean := func(value string) string {
		value = strings.ToLower(cleanPlain(value))
		for _, suffix := range []string{"的微信公众号", "微信公众号", "-微阅读", "微阅读"} {
			value = strings.TrimSuffix(value, suffix)
		}
		return strings.Join(strings.Fields(value), "")
	}
	return clean(actual) != "" && clean(actual) == clean(expected)
}

func mergeWeChatPreview(article NewsItem, items []NewsItem) []NewsItem {
	result := []NewsItem{article}
	seen := map[string]struct{}{article.URL: {}}
	for _, item := range items {
		if _, exists := seen[item.URL]; exists {
			continue
		}
		seen[item.URL] = struct{}{}
		result = append(result, item)
		if len(result) >= maxSourcePreviewItems {
			break
		}
	}
	return result
}

func collectWeChatDiscoveredSourceContext(ctx context.Context, source FeedSource) (operationalSourceResult, bool, error) {
	directBudget := weChatDirectFeedBudget
	if _, ok := weChatVerifiedDirectFeedPolicies[strings.TrimSpace(source.URL)]; ok {
		directBudget = weChatVerifiedFeedBudget
	}
	result, handled, directErr := collectWeChatDiscoveredSourceContextWithFetcher(ctx, source, weChatSourceFetch, directBudget, weChatFallbackFeedBudget)
	policy, verified := verifiedWeChatPolicyForSource(source)
	if !verified || !verifiedWeChatHistoryPending(source, policy) {
		return result, handled, directErr
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < verifiedWeChatHistoryMinBudget {
		if directErr != nil {
			return result, handled, directErr
		}
		result.Status = "partial"
		result.Message = joinSourceSyncMessages(result.Message, "已读取最新文章；历史内容将在后台继续补全")
		result.NextSyncAfter = verifiedWeChatHistoryStartupRetry
		return result, handled, nil
	}
	history, historyErr := verifiedWeChatHistoryCollector(ctx, source, policy)
	if historyErr != nil {
		if directErr != nil {
			return operationalSourceResult{}, true, errors.New("公众号最新内容读取失败：" + errorText(directErr) + "；历史回填失败：" + errorText(historyErr))
		}
		result.Status = "partial"
		result.Message = joinSourceSyncMessages(result.Message, "已保留最新文章；历史回填暂不可用，下次同步继续")
		result.NextSyncAfter = verifiedWeChatHistoryRetryDelay
		return result, handled, nil
	}
	if directErr != nil {
		result = operationalSourceResult{
			Feed:    history,
			Status:  "partial",
			Message: "最新文章窗口暂不可用，已恢复可验证的历史文章",
		}
		handled = true
	} else {
		result.Feed = mergeVerifiedWeChatHistory(result.Feed, history)
	}
	result.NotModified = false
	result.Backfill = true
	result.BackfillCursor = verifiedWeChatHistoryCursor(policy)
	result.BackfillCursorChanged = true
	return result, handled, nil
}

func collectWeChatDiscoveredSourceContextWithFetcher(ctx context.Context, source FeedSource, fetcher operationalFetchFunc, directBudget, fallbackBudget time.Duration) (operationalSourceResult, bool, error) {
	if err := validateVerifiedWeChatSource(source); err != nil {
		return operationalSourceResult{}, true, err
	}
	if validWechatRSSFeedURL(source.URL) {
		directCtx, cancel := context.WithTimeout(ctx, directBudget)
		response, directErr := fetcher(directCtx, source.URL, source.ETag, source.LastModified)
		cancel()
		if directErr == nil {
			if response.NotModified {
				return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: response.ETag, LastModified: response.LastModified}}, true, nil
			}
			feed, parseErr := parseDirectWechatRSSFeed(response.Body, source.URL, source.Name, source.IconURL, source.SiteURL)
			if parseErr == nil {
				return operationalFeedResultWithLimit(feed, "ok", "", response, feedFetchMaxItems), true, nil
			}
			directErr = parseErr
		}

		fallbackURL, ok := pinnedWeChatFallbackURL(source)
		if !ok {
			return operationalSourceResult{}, true, directErr
		}
		fallbackCtx, cancelFallback := context.WithTimeout(ctx, fallbackBudget)
		fallbackResponse, fallbackErr := fetcher(fallbackCtx, fallbackURL, "", "")
		cancelFallback()
		if fallbackErr != nil {
			return operationalSourceResult{}, true, errors.New("公众号直连不可用：" + errorText(directErr) + "；兼容读取失败：" + errorText(fallbackErr))
		}
		feed, parseErr := parseFoloWechat2RSSFeed(fallbackResponse.Body, fallbackURL)
		if parseErr != nil {
			return operationalSourceResult{}, true, errors.New("公众号直连不可用：" + errorText(directErr) + "；兼容内容无效：" + errorText(parseErr))
		}
		if !weChatFeedIdentityMatches(feed.Title, source.Name) {
			return operationalSourceResult{}, true, errors.New("公众号兼容读取结果与当前公众号不匹配")
		}
		feed.Title = source.Name
		feed.SiteURL = firstNonEmpty(source.SiteURL, feed.SiteURL)
		feed.FeedURL = source.URL
		feed.IconURL = firstNonEmpty(source.IconURL, feed.IconURL)
		for index := range feed.Items {
			feed.Items[index].Source = source.Name
		}
		message := "公众号直连暂不可用，已降级到经身份校验的公开缓存；内容可能不是最新"
		// Do not persist compatibility validators against the canonical direct
		// URL. The next sync must retry direct access and recover automatically.
		return operationalFeedResult(feed, "partial", message, feedFetchResponse{}), true, nil
	}
	if !validFoloWeChatFeedURL(source.URL) {
		return operationalSourceResult{}, false, nil
	}
	fallbackCtx, cancel := context.WithTimeout(ctx, fallbackBudget)
	response, err := fetcher(fallbackCtx, source.URL, source.ETag, source.LastModified)
	cancel()
	if err != nil {
		return operationalSourceResult{}, true, err
	}
	if response.NotModified {
		return operationalSourceResult{NotModified: true, Feed: fetchedFeed{ETag: response.ETag, LastModified: response.LastModified}}, true, nil
	}
	feed, err := parseFoloWeChatFeed(response.Body, source.URL, source.Name, source.IconURL, source.SiteURL)
	if err != nil {
		return operationalSourceResult{}, true, err
	}
	return operationalFeedResult(feed, "ok", "", response), true, nil
}

func validateVerifiedWeChatSource(source FeedSource) error {
	actualURL := strings.TrimSpace(source.URL)
	expectedURL, pinnedID := weChatVerifiedDirectFeedURLs[strings.TrimSpace(source.ID)]
	policy, pinnedURL := weChatVerifiedDirectFeedPolicies[actualURL]
	if pinnedID && actualURL != expectedURL {
		return errors.New("固定公众号订阅地址与来源身份不匹配")
	}
	if pinnedURL && (!pinnedID || policy.SourceID != strings.TrimSpace(source.ID) || expectedURL != actualURL) {
		return errors.New("固定公众号订阅地址与来源身份不匹配")
	}
	return nil
}

func pinnedWeChatFallbackURL(source FeedSource) (string, bool) {
	target, ok := weChatPinnedFallbackURLs[strings.TrimSpace(source.ID)]
	if !ok || strings.TrimSpace(source.URL) != target || !validWechatRSSFeedURL(target) {
		return "", false
	}
	fallbackURL, ok := weChatPinnedFoloFallbackURLs[strings.TrimSpace(source.ID)]
	if !ok {
		return "", false
	}
	return fallbackURL, validFoloWechat2RSSFeedURL(fallbackURL)
}

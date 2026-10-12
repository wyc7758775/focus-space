package main

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

const jintiankanshaHardKeColumnURL = "https://www.jintiankansha.me/column/vCiIX6UYpo?page=1"
const jintiankanshaMachineHeartColumnURL = "https://www.jintiankansha.me/column/pJMG8ZXFLd?page=1"
const jintiankanshaNewAIColumnURL = "https://www.jintiankansha.me/column/tiKZRAcl3D?page=1"
const jintiankanshaGeekparkColumnURL = "https://www.jintiankansha.me/column/J5JYjmNsqA?page=1"
const jintiankanshaQbitColumnURL = "https://www.jintiankansha.me/column/8LA3hF4EoQ?page=1"

var jintiankanshaArticlePathRE = regexp.MustCompile(`^/t/[A-Za-z0-9_-]{6,64}$`)
var jintiankanshaNextRE = regexp.MustCompile(`(?i)<a[^>]+id=["']p_next["'][^>]+href=["']([^"']+)["']`)

// collectJintiankanshaWechatSource reads exactly one historical page per sync.
// The next page URL is carried in feed_sources.last_modified, which is an
// internal HTTP metadata field and remains invisible to clients. This keeps
// the first paint fast and lets the reader request older pages on demand.
func collectJintiankanshaWechatSource(ctx context.Context, source FeedSource, spec operationalSourceSpec, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	pageURL := strings.TrimSpace(source.LastModified)
	if !validJintiankanshaColumnURL(pageURL) {
		pageURL = spec.endpoints[0]
	}
	response, err := fetcher(ctx, pageURL, "", "")
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText("硬氪历史栏目暂时无法读取："+errorText(err), 500)}
	}
	feed, nextURL, parseErr := parseJintiankanshaWechatPage(response.Body, pageURL, source, fetchedAt)
	if parseErr != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(parseErr), 500)}
	}
	feed.ETag = response.ETag
	// Preserve the next page cursor in the existing internal metadata column.
	feed.LastModified = nextURL
	status, message := "ok", ""
	if nextURL != "" {
		status = "partial"
		message = "已读取当前页；滑动到底部后继续读取更早文章"
	}
	result := operationalFeedResultWithLimit(feed, status, message, response, operationalSourceLimit)
	result.Feed.LastModified = nextURL
	return result
}

func validJintiankanshaColumnURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || strings.ToLower(parsed.Hostname()) != "www.jintiankansha.me" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	if !regexp.MustCompile(`^/column/[A-Za-z0-9]{6,32}$`).MatchString(parsed.Path) {
		return false
	}
	page := parsed.Query().Get("page")
	return page == "" || allASCIIDigits(page)
}

func parseJintiankanshaWechatPage(body []byte, pageURL string, source FeedSource, fetchedAt string) (fetchedFeed, string, error) {
	if !validJintiankanshaColumnURL(pageURL) {
		return fetchedFeed{}, "", errors.New("硬氪历史栏目地址不合法")
	}
	title, links := parseOperationalHTML(body, pageURL)
	items := make([]NewsItem, 0, operationalSourceLimit)
	seen := make(map[string]struct{}, operationalSourceLimit)
	iconURL := ""
	for _, link := range links {
		parsed, err := url.Parse(strings.TrimSpace(link.url))
		if err != nil || strings.ToLower(parsed.Hostname()) != "www.jintiankansha.me" || !jintiankanshaArticlePathRE.MatchString(parsed.Path) {
			continue
		}
		canonical := "https://www.jintiankansha.me" + parsed.Path
		if _, exists := seen[canonical]; exists || strings.TrimSpace(link.title) == "" {
			continue
		}
		seen[canonical] = struct{}{}
		item := makeFetchedNews(parsed.Path, link.title, canonical, source.Name, source.Name, fetchedAt, title, iconURL, "", "", 0, source.URL)
		item.SourceType = "wechat"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		items = append(items, item)
		if len(items) >= operationalSourceLimit {
			break
		}
	}
	if len(items) == 0 {
		return fetchedFeed{}, "", errors.New("硬氪历史栏目没有返回文章")
	}
	nextURL := ""
	if match := jintiankanshaNextRE.FindSubmatch(body); len(match) == 2 {
		candidate := resolvePublicLink(pageURL, string(match[1]))
		if validJintiankanshaColumnURL(candidate) {
			nextURL = candidate
		}
	}
	parsedPage, _ := url.Parse(pageURL)
	return fetchedFeed{
		Title:       source.Name,
		Description: "硬氪微信公众号历史文章",
		SiteURL:     "https://www.jintiankansha.me" + parsedPage.Path,
		FeedURL:     source.URL,
		IconURL:     iconURL,
		Format:      "rss",
		Items:       items,
	}, nextURL, nil
}

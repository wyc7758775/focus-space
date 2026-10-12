package main

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	htmlnode "golang.org/x/net/html"
)

const (
	ruanYiFengSourceID         = "top-rss-002-836ccfdca641"
	ruanYiFengFeedURL          = "https://www.ruanyifeng.com/blog/atom.xml"
	ruanYiFengWeeklyArchiveURL = "https://www.ruanyifeng.com/blog/weekly/index.html"
	ruanYiFengArchiveItemLimit = 500
)

var (
	ruanYiFengWeeklyPathRE = regexp.MustCompile(`^/blog/[0-9]{4}/[0-9]{2}/weekly-issue-[0-9]+\.html$`)
	ruanYiFengDateRE       = regexp.MustCompile(`@[0-9]{4}\.[0-9]{2}\.[0-9]{2}$`)
)

func isRuanYiFengPublicSource(source FeedSource) bool {
	return source.ID == ruanYiFengSourceID && strings.TrimSpace(source.URL) == ruanYiFengFeedURL
}

// collectRuanYiFengPublicFeed combines the small current Atom window with the
// official weekly archive. Both endpoints are owned by the same public site;
// no third-party mirror or generated article is introduced. The archive walk
// is bounded so one sync cannot grow response memory without limit.
func collectRuanYiFengPublicFeed(ctx context.Context, source FeedSource) (feedCollectionResult, error) {
	fetcher := operationalFetchFunc(localCustomFeedFetch)
	feedResponse, feedErr := fetchPublicURLWithRetryUsing(ctx, source.URL, "", "", fetcher)
	if feedErr == nil {
		feedResponse, feedErr = validateLocalCustomFeedResponse(feedResponse, source.URL)
	}
	archiveResponse, archiveErr := fetchPublicURLWithRetryUsing(ctx, ruanYiFengWeeklyArchiveURL, "", "", fetcher)
	if archiveErr == nil {
		archiveErr = validateRuanYiFengArchiveResponse(archiveResponse)
	}

	var feed fetchedFeed
	if feedErr == nil {
		feed, feedErr = parseXMLFeedWithLimit(feedResponse.Body, source.URL, feedFetchTrustedMaxItems)
	}
	if archiveErr == nil {
		archiveItems, parseErr := parseRuanYiFengWeeklyArchive(archiveResponse.Body, source)
		if parseErr != nil {
			archiveErr = parseErr
		} else {
			feed = mergeRuanYiFengArchive(feed, archiveItems, source)
		}
	}

	if len(feed.Items) == 0 {
		return feedCollectionResult{}, errors.New(joinSourceSyncMessages(errorText(feedErr), errorText(archiveErr)))
	}
	feed.ETag, feed.LastModified = "", ""
	result := feedCollectionResult{response: feedResponse, feed: feed, status: "ok"}
	if feedErr != nil || archiveErr != nil {
		result.status = "partial"
		result.message = "官方最新 Feed 或历史归档暂时不可用，已保留本次成功读取的内容"
	}
	return result, nil
}

func validateRuanYiFengArchiveResponse(response feedFetchResponse) error {
	finalURL, err := url.Parse(firstNonEmpty(response.FinalURL, ruanYiFengWeeklyArchiveURL))
	if err != nil || finalURL.Scheme != "https" || finalURL.User != nil || finalURL.Port() != "" || finalURL.RawQuery != "" || finalURL.Fragment != "" {
		return errors.New("阮一峰官方历史归档重定向地址无效")
	}
	host := strings.ToLower(strings.TrimSuffix(finalURL.Hostname(), "."))
	if (host != "www.ruanyifeng.com" && host != "ruanyifeng.com") || finalURL.Path != "/blog/weekly/index.html" {
		return errors.New("阮一峰官方历史归档身份不匹配")
	}
	return nil
}

func parseRuanYiFengWeeklyArchive(body []byte, source FeedSource) ([]NewsItem, error) {
	root, err := htmlnode.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.New("阮一峰官方历史归档无法解析")
	}
	items := make([]NewsItem, 0, ruanYiFengArchiveItemLimit)
	seen := make(map[string]struct{}, ruanYiFengArchiveItemLimit)
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		if len(items) >= ruanYiFengArchiveItemLimit {
			return
		}
		if node.Type == htmlnode.ElementNode && strings.EqualFold(node.Data, "li") && hasHTMLClass(node, "module-list-item") {
			link, title := ruanYiFengArchiveLink(node)
			published := ruanYiFengArchiveDate(node)
			if link != "" && title != "" && published != "" {
				if _, exists := seen[link]; !exists {
					seen[link] = struct{}{}
					item := makeOperationalNews(source, link, title, link, published, "阮一峰科技爱好者周刊官方公开存档。", "阮一峰", "rss", "official", "")
					items = append(items, item)
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if len(items) < 200 {
		return nil, errors.New("阮一峰官方历史归档返回不足 200 篇")
	}
	return items, nil
}

func hasHTMLClass(node *htmlnode.Node, target string) bool {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, "class") {
			for _, value := range strings.Fields(attr.Val) {
				if value == target {
					return true
				}
			}
		}
	}
	return false
}

func ruanYiFengArchiveLink(node *htmlnode.Node) (string, string) {
	var resultURL, resultTitle string
	var walk func(*htmlnode.Node)
	walk = func(current *htmlnode.Node) {
		if resultURL != "" {
			return
		}
		if current.Type == htmlnode.ElementNode && strings.EqualFold(current.Data, "a") {
			href := ""
			for _, attr := range current.Attr {
				if strings.EqualFold(attr.Key, "href") {
					href = strings.TrimSpace(attr.Val)
					break
				}
			}
			parsed, err := url.Parse(href)
			if err == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Port() == "" && parsed.RawQuery == "" && parsed.Fragment == "" {
				host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
				if (host == "www.ruanyifeng.com" || host == "ruanyifeng.com") && ruanYiFengWeeklyPathRE.MatchString(parsed.Path) {
					resultURL = "https://www.ruanyifeng.com" + parsed.Path
					resultTitle = limitText(operationalNodeText(current), 240)
					return
				}
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return resultURL, resultTitle
}

func ruanYiFengArchiveDate(node *htmlnode.Node) string {
	encoded := ""
	var walk func(*htmlnode.Node)
	walk = func(current *htmlnode.Node) {
		if encoded != "" {
			return
		}
		for _, attr := range current.Attr {
			if strings.EqualFold(attr.Key, "data-cfemail") {
				encoded = strings.TrimSpace(attr.Val)
				return
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	decoded, err := decodeCloudflareEmail(encoded)
	if err != nil {
		return ""
	}
	dateText := strings.TrimPrefix(ruanYiFengDateRE.FindString(decoded), "@")
	parsed, err := time.ParseInLocation("2006.01.02", dateText, time.FixedZone("CST", 8*60*60))
	if err != nil {
		return ""
	}
	return parsed.UTC().Format(time.RFC3339)
}

func decodeCloudflareEmail(value string) (string, error) {
	bytesValue, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(bytesValue) < 2 {
		return "", errors.New("Cloudflare 日期编码无效")
	}
	key := bytesValue[0]
	decoded := make([]byte, len(bytesValue)-1)
	for index, value := range bytesValue[1:] {
		decoded[index] = value ^ key
	}
	return string(decoded), nil
}

func mergeRuanYiFengArchive(feed fetchedFeed, archive []NewsItem, source FeedSource) fetchedFeed {
	if feed.Title == "" {
		feed = makeOperationalFeed(source, nil, "atom")
	}
	seen := make(map[string]struct{}, len(feed.Items)+len(archive))
	for _, item := range feed.Items {
		seen[item.URL] = struct{}{}
	}
	for _, item := range archive {
		if _, exists := seen[item.URL]; exists {
			continue
		}
		seen[item.URL] = struct{}{}
		feed.Items = append(feed.Items, item)
	}
	feed.FeedURL = source.URL
	feed.SiteURL = "https://www.ruanyifeng.com/blog/"
	feed.Format = firstNonEmpty(feed.Format, "atom")
	return feed
}

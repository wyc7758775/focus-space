package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	cnInfoHistoryEndpoint = "https://www.cninfo.com.cn/new/hisAnnouncement/query"
	cnInfoHistoryPageSize = 30
	cnInfoHistoryMaxPages = 3
	cnInfoHistoryPageWait = 5 * time.Second
)

var cnInfoAnnouncementIDRE = regexp.MustCompile(`^[0-9]{6,32}$`)
var cnInfoPageFetch = fetchCNInfoHistoryPage

type cnInfoHistoryFetchFunc func(context.Context, int, int) (feedFetchResponse, error)

type cnInfoHistoryPage struct {
	Items      []NewsItem
	HasMore    bool
	TotalPages int
}

func fetchCNInfoHistoryPage(ctx context.Context, page, pageSize int) (feedFetchResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, err := fetchCNInfoHistoryPageOnce(ctx, page, pageSize)
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

func fetchCNInfoHistoryPageOnce(ctx context.Context, page, pageSize int) (feedFetchResponse, error) {
	if page < 1 || page > 1000 || pageSize != cnInfoHistoryPageSize {
		return feedFetchResponse{}, errors.New("CNINFO 分页参数无效")
	}
	form := url.Values{}
	form.Set("pageNum", strconv.Itoa(page))
	form.Set("pageSize", strconv.Itoa(pageSize))
	form.Set("column", "szse")
	form.Set("tabName", "fulltext")
	form.Set("plate", "")
	form.Set("stock", "")
	form.Set("searchkey", "")
	form.Set("secid", "")
	form.Set("category", "")
	form.Set("trade", "")
	form.Set("seDate", "")
	form.Set("sortName", "")
	form.Set("sortType", "")
	form.Set("isHLtitle", "true")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cnInfoHistoryEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return feedFetchResponse{}, err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("Origin", "https://www.cninfo.com.cn")
	req.Header.Set("Referer", "https://www.cninfo.com.cn/new/disclosure")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	client := publicFeedHTTPClient()
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return feedFetchResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return feedFetchResponse{}, fmt.Errorf("来源返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > feedFetchMaxBytes {
		return feedFetchResponse{}, errors.New("CNINFO 分页响应超过大小上限")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, feedFetchMaxBytes+1))
	if err != nil {
		return feedFetchResponse{}, err
	}
	if len(body) > feedFetchMaxBytes {
		return feedFetchResponse{}, errors.New("CNINFO 分页响应超过大小上限")
	}
	return feedFetchResponse{Body: body, ContentType: strings.ToLower(response.Header.Get("Content-Type")), FinalURL: cnInfoHistoryEndpoint}, nil
}

func collectCNInfoOperational(ctx context.Context, source FeedSource, fetcher cnInfoHistoryFetchFunc) operationalSourceResult {
	if source.ID != "cninfo-disclosures" || source.URL != cnInfoHistoryEndpoint || fetcher == nil {
		return operationalSourceResult{Status: "unavailable", Message: "CNINFO 固定来源配置无效"}
	}
	resumePage, resumeValid := cnInfoBackfillPage(source.BackfillCursor)
	backfill := source.LastSyncAt == "" || source.RecordsSeen == 0 || resumeValid || strings.TrimSpace(source.BackfillCursor) != ""
	pageLimit := 1
	if backfill {
		pageLimit = cnInfoHistoryMaxPages
	}
	currentPage := 1
	readingHistory := !resumeValid
	nextCursor := ""
	feed := fetchedFeed{Title: source.Name, SiteURL: "https://www.cninfo.com.cn/new/disclosure", FeedURL: source.URL, Format: "api"}
	seen := make(map[string]struct{}, cnInfoHistoryPageSize*pageLimit)
	status, message := "ok", ""
	for attempt := 0; attempt < pageLimit && currentPage > 0; attempt++ {
		pageCtx, cancel := context.WithTimeout(ctx, cnInfoHistoryPageWait)
		response, err := fetcher(pageCtx, currentPage, cnInfoHistoryPageSize)
		cancel()
		if err != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页读取失败："+errorText(err))
			nextCursor = cnInfoBackfillCursor(currentPage)
			break
		}
		page, parseErr := parseCNInfoHistoryPage(response.Body, source, cnInfoHistoryPageSize)
		if parseErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(parseErr), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页解析失败："+errorText(parseErr))
			nextCursor = cnInfoBackfillCursor(currentPage)
			break
		}
		appendOperationalFeedPage(&feed, makeOperationalFeed(source, page.Items, "api"), seen, cnInfoHistoryPageSize*pageLimit)
		if !backfill {
			break
		}
		if resumeValid && !readingHistory {
			readingHistory = true
			currentPage = resumePage
			nextCursor = cnInfoBackfillCursor(resumePage)
			continue
		}
		if !page.HasMore || currentPage >= page.TotalPages || operationalPageReachedBackfillCutoff(source, page.Items) {
			currentPage = 0
			nextCursor = ""
			break
		}
		currentPage++
		nextCursor = cnInfoBackfillCursor(currentPage)
	}
	if len(feed.Items) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "CNINFO API 没有返回可用公告"}
	}
	if backfill && nextCursor != "" && status == "ok" {
		status = "partial"
		message = "已读取当前历史窗口；后续同步将继续回填更早公告"
	}
	result := operationalFeedResultWithLimit(feed, status, message, feedFetchResponse{}, cnInfoHistoryPageSize*pageLimit)
	if backfill {
		result.Backfill = true
		result.BackfillCursor = nextCursor
		result.BackfillCursorChanged = true
	}
	return result
}

func parseCNInfoHistoryPage(body []byte, source FeedSource, limit int) (cnInfoHistoryPage, error) {
	var response struct {
		TotalPages    int  `json:"totalpages"`
		HasMore       bool `json:"hasMore"`
		Announcements []struct {
			ID        string `json:"announcementId"`
			Title     string `json:"announcementTitle"`
			Time      int64  `json:"announcementTime"`
			Adjunct   string `json:"adjunctUrl"`
			StockName string `json:"secName"`
			StockCode string `json:"secCode"`
		} `json:"announcements"`
	}
	if json.Unmarshal(body, &response) != nil {
		return cnInfoHistoryPage{}, errors.New("CNINFO 公告 JSON 无法解析")
	}
	items := make([]NewsItem, 0, minInt(limit, len(response.Announcements)))
	seen := make(map[string]struct{}, len(response.Announcements))
	for _, row := range response.Announcements {
		id := strings.TrimSpace(row.ID)
		link, ok := validCNInfoPDFURL(row.Adjunct, id)
		if !ok || !cnInfoAnnouncementIDRE.MatchString(id) || strings.TrimSpace(row.Title) == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		published := ""
		if row.Time > 0 {
			published = time.UnixMilli(row.Time).UTC().Format(time.RFC3339)
		}
		summary := strings.TrimSpace(strings.Join([]string{cleanPlain(row.StockName), strings.TrimSpace(row.StockCode)}, " "))
		items = append(items, makeOperationalNews(source, id, cleanPlain(row.Title), link, published, summary, "", "api", "official", ""))
		if len(items) >= limit {
			break
		}
	}
	if len(items) == 0 {
		return cnInfoHistoryPage{}, errors.New("CNINFO API 没有返回可用公告")
	}
	return cnInfoHistoryPage{Items: items, HasMore: response.HasMore, TotalPages: response.TotalPages}, nil
}

func validCNInfoPDFURL(raw, announcementID string) (string, bool) {
	resolved := resolvePublicLink("https://static.cninfo.com.cn/", raw)
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() != "static.cninfo.com.cn" {
		return "", false
	}
	if !strings.HasPrefix(parsed.Path, "/finalpage/") || !strings.HasSuffix(strings.ToUpper(parsed.Path), strings.ToUpper(announcementID)+".PDF") {
		return "", false
	}
	return parsed.String(), true
}

func cnInfoBackfillCursor(page int) string {
	if page < 2 || page > 1000 {
		return ""
	}
	return cnInfoHistoryEndpoint + "?page=" + strconv.Itoa(page) + "&page_size=" + strconv.Itoa(cnInfoHistoryPageSize)
}

func cnInfoBackfillPage(raw string) (int, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || parsed.Hostname() != "www.cninfo.com.cn" || parsed.Path != "/new/hisAnnouncement/query" {
		return 0, false
	}
	values := parsed.Query()
	if len(values) != 2 || len(values["page"]) != 1 || len(values["page_size"]) != 1 || values.Get("page_size") != strconv.Itoa(cnInfoHistoryPageSize) {
		return 0, false
	}
	page, parseErr := strconv.Atoi(values.Get("page"))
	return page, parseErr == nil && page >= 2 && page <= 1000
}

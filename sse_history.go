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
	"sync"
	"time"
)

const (
	sseHistoryEndpoint         = "https://query.sse.com.cn/security/stock/queryCompanyBulletin.do"
	sseHistoryReferer          = "https://www.sse.com.cn/disclosure/listedinfo/regular/"
	sseHistoryCallback         = "plaudDashboardCallback"
	sseHistorySecurityTypes    = "0101,120100,020100,020200,120200"
	sseHistoryPageSize         = 30
	sseHistoryMaxPages         = 3
	sseHistoryMaxPage          = 2000
	sseHistoryMaxResponseBytes = 512 << 10
	sseHistoryMaxRoundBytes    = sseHistoryMaxResponseBytes * sseHistoryMaxPages
	sseHistoryPageWait         = 12 * time.Second
)

var sseHistoryPDFPathRE = regexp.MustCompile(`^/disclosure/listedinfo/announcement/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9][A-Za-z0-9_.-]{0,200}\.[pP][dD][fF]$`)
var sseHistoryPageFetch = fetchSSEHistoryPage

type sseHistoryFetchFunc func(context.Context, string) (feedFetchResponse, error)

func sseAwareOperationalFetch(ctx context.Context, raw, etag, lastModified string) (feedFetchResponse, error) {
	if strings.HasPrefix(raw, sseHistoryEndpoint+"?") {
		return sseHistoryPageFetch(ctx, raw)
	}
	return fetchPublicURLWithRetry(ctx, raw, etag, lastModified)
}

type sseHistoryRequest struct {
	Page      int
	EndPage   int
	BeginDate string
	EndDate   string
}

type sseHistoryPage struct {
	Items     []NewsItem
	HasMore   bool
	PageCount int
}

type sseHistoryFetchResult struct {
	Response feedFetchResponse
	Err      error
}

func fetchSSEHistoryPage(ctx context.Context, raw string) (feedFetchResponse, error) {
	if _, ok := parseSSEHistoryRequest(raw, 1); !ok {
		return feedFetchResponse{}, errors.New("上交所分页地址无效")
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		response, err := fetchSSEHistoryPageOnce(ctx, raw)
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

func fetchSSEHistoryPageOnce(ctx context.Context, raw string) (feedFetchResponse, error) {
	requestState, ok := parseSSEHistoryRequest(raw, 1)
	if !ok || requestState.Page > sseHistoryMaxPage {
		return feedFetchResponse{}, errors.New("上交所分页参数无效")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return feedFetchResponse{}, err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "application/javascript, application/json, */*")
	req.Header.Set("Referer", sseHistoryReferer)
	client := publicFeedHTTPClient()
	client.Timeout = sseHistoryPageWait
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("上交所查询接口不允许重定向")
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return feedFetchResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return feedFetchResponse{}, fmt.Errorf("来源返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > sseHistoryMaxResponseBytes {
		return feedFetchResponse{}, errors.New("上交所分页响应超过大小上限")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, sseHistoryMaxResponseBytes+1))
	if err != nil {
		return feedFetchResponse{}, err
	}
	if len(body) > sseHistoryMaxResponseBytes {
		return feedFetchResponse{}, errors.New("上交所分页响应超过大小上限")
	}
	return feedFetchResponse{
		Body:        body,
		ContentType: strings.ToLower(response.Header.Get("Content-Type")),
		FinalURL:    raw,
	}, nil
}

func collectSSEHistoryOperational(ctx context.Context, source FeedSource, fetcher sseHistoryFetchFunc, fetchedAt string) operationalSourceResult {
	if source.ID != "sse-periodic-reports" || source.URL != sseHistoryEndpoint || fetcher == nil {
		return operationalSourceResult{Status: "unavailable", Message: "上交所固定来源配置无效"}
	}
	latest, ok := sseHistoryFirstRequest(fetchedAt)
	if !ok {
		return operationalSourceResult{Status: "unavailable", Message: "上交所历史日期窗口无效"}
	}
	resume, resumeValid := parseSSEHistoryCursor(source.BackfillCursor, 2)
	backfill := source.LastSyncAt == "" || source.RecordsSeen == 0 || resumeValid || strings.TrimSpace(source.BackfillCursor) != ""
	requests := []sseHistoryRequest{latest}
	if backfill {
		if resumeValid {
			history := resume
			history.EndPage = minInt(history.Page+1, sseHistoryMaxPage)
			requests = append(requests, history)
		} else {
			requests[0].EndPage = minInt(latest.Page+sseHistoryMaxPages-1, sseHistoryMaxPage)
		}
	}
	results := fetchSSEHistoryRequests(ctx, requests, fetcher)
	nextCursor := ""
	totalBytes := 0
	feed := fetchedFeed{Title: source.Name, SiteURL: sseHistoryReferer, FeedURL: source.URL, Format: "api"}
	seen := make(map[string]struct{}, sseHistoryPageSize*sseHistoryMaxPages)
	status, message := "ok", ""
	for index, request := range requests {
		currentURL := sseHistoryRequestURL(request)
		response, fetchErr := results[index].Response, results[index].Err
		if fetchErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(fetchErr), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页读取失败："+errorText(fetchErr))
			nextCursor = currentURL
			break
		}
		if len(response.Body) > sseHistoryMaxResponseBytes || totalBytes+len(response.Body) > sseHistoryMaxRoundBytes {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: "上交所分页响应超过大小上限"}
			}
			status, message = operationalPartialResult(feed, "已达到上交所历史回填字节上限")
			nextCursor = currentURL
			break
		}
		totalBytes += len(response.Body)
		page, parseErr := parseSSEHistoryPage(response.Body, source, request)
		if parseErr != nil {
			if len(feed.Items) == 0 {
				return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(parseErr), 500)}
			}
			status, message = operationalPartialResult(feed, "后续分页解析失败："+errorText(parseErr))
			nextCursor = currentURL
			break
		}
		appendOperationalFeedPage(&feed, makeOperationalFeed(source, page.Items, "api"), seen, sseHistoryPageSize*sseHistoryMaxPages)
		if !backfill {
			break
		}
		if resumeValid && index == 0 {
			nextCursor = sseHistoryRequestURL(resume)
			continue
		}
		if !page.HasMore || operationalPageReachedBackfillCutoff(source, page.Items) {
			nextCursor = ""
			break
		}
		next := request
		next.Page = request.EndPage + 1
		next.EndPage = next.Page
		nextCursor = sseHistoryRequestURL(next)
	}
	if len(feed.Items) == 0 {
		return operationalSourceResult{Status: "unavailable", Message: "上交所 API 没有返回可用定期报告"}
	}
	if backfill && nextCursor != "" && status == "ok" {
		status = "partial"
		message = "已读取当前历史窗口；后续同步将继续回填更早定期报告"
	}
	result := operationalFeedResultWithLimit(feed, status, message, feedFetchResponse{}, sseHistoryPageSize*sseHistoryMaxPages)
	if backfill {
		result.Backfill = true
		result.BackfillCursor = nextCursor
		result.BackfillCursorChanged = true
	}
	return result
}

func fetchSSEHistoryRequests(ctx context.Context, requests []sseHistoryRequest, fetcher sseHistoryFetchFunc) []sseHistoryFetchResult {
	results := make([]sseHistoryFetchResult, len(requests))
	var wait sync.WaitGroup
	for index, request := range requests {
		wait.Add(1)
		go func() {
			defer wait.Done()
			pageCtx, cancel := context.WithTimeout(ctx, sseHistoryPageWait)
			defer cancel()
			results[index].Response, results[index].Err = fetcher(pageCtx, sseHistoryRequestURL(request))
		}()
	}
	wait.Wait()
	return results
}

func sseHistoryFirstRequest(fetchedAt string) (sseHistoryRequest, bool) {
	end, err := time.Parse(time.RFC3339, strings.TrimSpace(fetchedAt))
	if err != nil {
		return sseHistoryRequest{}, false
	}
	end = end.UTC()
	request := sseHistoryRequest{
		Page:      1,
		BeginDate: end.AddDate(-10, 0, 0).Format("2006-01-02"),
		EndDate:   end.Format("2006-01-02"),
	}
	return request, validSSEHistoryDates(request.BeginDate, request.EndDate)
}

func sseHistoryRequestURL(request sseHistoryRequest) string {
	request = normalizedSSEHistoryRequest(request)
	pageCount := request.EndPage - request.Page + 1
	if request.Page < 1 || request.EndPage > sseHistoryMaxPage || pageCount < 1 || pageCount > sseHistoryMaxPages || !validSSEHistoryDates(request.BeginDate, request.EndDate) {
		return ""
	}
	values := url.Values{}
	values.Set("jsonCallBack", sseHistoryCallback)
	values.Set("isPagination", "true")
	values.Set("productId", "")
	values.Set("keyWord", "")
	values.Set("securityType", sseHistorySecurityTypes)
	values.Set("reportType2", "DQBG")
	values.Set("reportType", "YEARLY")
	values.Set("beginDate", request.BeginDate)
	values.Set("endDate", request.EndDate)
	values.Set("pageHelp.pageSize", strconv.Itoa(sseHistoryPageSize))
	values.Set("pageHelp.pageCount", "50")
	values.Set("pageHelp.pageNo", strconv.Itoa(request.Page))
	values.Set("pageHelp.beginPage", strconv.Itoa(request.Page))
	values.Set("pageHelp.cacheSize", strconv.Itoa(pageCount))
	values.Set("pageHelp.endPage", strconv.Itoa(request.EndPage))
	return sseHistoryEndpoint + "?" + values.Encode()
}

func parseSSEHistoryRequest(raw string, minimumPage int) (sseHistoryRequest, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || minimumPage < 1 {
		return sseHistoryRequest{}, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		strings.ToLower(parsed.Hostname()) != "query.sse.com.cn" || parsed.Path != "/security/stock/queryCompanyBulletin.do" {
		return sseHistoryRequest{}, false
	}
	values := parsed.Query()
	page, pageErr := strconv.Atoi(values.Get("pageHelp.pageNo"))
	endPage, endPageErr := strconv.Atoi(values.Get("pageHelp.endPage"))
	cacheSize, cacheSizeErr := strconv.Atoi(values.Get("pageHelp.cacheSize"))
	request := sseHistoryRequest{Page: page, EndPage: endPage, BeginDate: values.Get("beginDate"), EndDate: values.Get("endDate")}
	expected := map[string]string{
		"jsonCallBack":       sseHistoryCallback,
		"isPagination":       "true",
		"productId":          "",
		"keyWord":            "",
		"securityType":       sseHistorySecurityTypes,
		"reportType2":        "DQBG",
		"reportType":         "YEARLY",
		"beginDate":          request.BeginDate,
		"endDate":            request.EndDate,
		"pageHelp.pageSize":  strconv.Itoa(sseHistoryPageSize),
		"pageHelp.pageCount": "50",
		"pageHelp.pageNo":    strconv.Itoa(page),
		"pageHelp.beginPage": strconv.Itoa(page),
		"pageHelp.cacheSize": strconv.Itoa(cacheSize),
		"pageHelp.endPage":   strconv.Itoa(endPage),
	}
	if pageErr != nil || endPageErr != nil || cacheSizeErr != nil || page < minimumPage || endPage < page || endPage > sseHistoryMaxPage ||
		cacheSize < 1 || cacheSize > sseHistoryMaxPages || cacheSize != endPage-page+1 || len(values) != len(expected) || !validSSEHistoryDates(request.BeginDate, request.EndDate) {
		return sseHistoryRequest{}, false
	}
	for key, want := range expected {
		got := values[key]
		if len(got) != 1 || got[0] != want {
			return sseHistoryRequest{}, false
		}
	}
	return request, true
}

func parseSSEHistoryCursor(raw string, minimumPage int) (sseHistoryRequest, bool) {
	request, ok := parseSSEHistoryRequest(raw, minimumPage)
	return request, ok && request.EndPage == request.Page
}

func normalizedSSEHistoryRequest(request sseHistoryRequest) sseHistoryRequest {
	if request.EndPage == 0 {
		request.EndPage = request.Page
	}
	return request
}

func validSSEHistoryDates(beginText, endText string) bool {
	begin, beginErr := time.Parse("2006-01-02", beginText)
	end, endErr := time.Parse("2006-01-02", endText)
	return beginErr == nil && endErr == nil && beginText == begin.Format("2006-01-02") && endText == end.Format("2006-01-02") &&
		begin.Equal(end.AddDate(-10, 0, 0)) && !end.After(time.Now().UTC().Add(24*time.Hour))
}

func parseSSEHistoryPage(body []byte, source FeedSource, request sseHistoryRequest) (sseHistoryPage, error) {
	request = normalizedSSEHistoryRequest(request)
	if len(body) == 0 || len(body) > sseHistoryMaxResponseBytes {
		return sseHistoryPage{}, errors.New("上交所分页响应大小无效")
	}
	trimmed := strings.TrimSpace(string(body))
	prefix := sseHistoryCallback + "("
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, ")") {
		return sseHistoryPage{}, errors.New("上交所 JSONP 包装无效")
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), ")")
	var response struct {
		BeginDate    string `json:"beginDate"`
		EndDate      string `json:"endDate"`
		IsPagination string `json:"isPagination"`
		JSONCallBack string `json:"jsonCallBack"`
		ReportType   string `json:"reportType"`
		ReportType2  string `json:"reportType2"`
		SecurityType string `json:"securityType"`
		PageHelp     struct {
			BeginPage int `json:"beginPage"`
			CacheSize int `json:"cacheSize"`
			EndPage   int `json:"endPage"`
			PageCount int `json:"pageCount"`
			PageNo    int `json:"pageNo"`
			PageSize  int `json:"pageSize"`
			Data      []struct {
				AddDate      string `json:"ADDDATE"`
				BulletinType string `json:"BULLETIN_TYPE"`
				SecurityCode string `json:"SECURITY_CODE"`
				SecurityName string `json:"SECURITY_NAME"`
				SSEDate      string `json:"SSEDATE"`
				Title        string `json:"TITLE"`
				URL          string `json:"URL"`
			} `json:"data"`
		} `json:"pageHelp"`
	}
	if json.Unmarshal([]byte(payload), &response) != nil {
		return sseHistoryPage{}, errors.New("上交所公告 JSON 无法解析")
	}
	if response.JSONCallBack != sseHistoryCallback || response.IsPagination != "true" || response.ReportType != "YEARLY" || response.ReportType2 != "DQBG" ||
		response.SecurityType != sseHistorySecurityTypes || response.BeginDate != request.BeginDate || response.EndDate != request.EndDate ||
		response.PageHelp.PageNo != request.Page || response.PageHelp.BeginPage != request.Page || response.PageHelp.EndPage != request.EndPage ||
		response.PageHelp.CacheSize != request.EndPage-request.Page+1 || response.PageHelp.PageSize != sseHistoryPageSize || response.PageHelp.PageCount < 0 ||
		len(response.PageHelp.Data) > (request.EndPage-request.Page+1)*sseHistoryPageSize {
		return sseHistoryPage{}, errors.New("上交所公告分页身份不匹配")
	}
	itemLimit := (request.EndPage - request.Page + 1) * sseHistoryPageSize
	items := make([]NewsItem, 0, minInt(itemLimit, len(response.PageHelp.Data)))
	seen := make(map[string]struct{}, len(response.PageHelp.Data))
	for _, row := range response.PageHelp.Data {
		link, ok := validSSEHistoryPDFURL(row.URL)
		title := cleanPlain(row.Title)
		if !ok || title == "" {
			continue
		}
		if _, duplicate := seen[link]; duplicate {
			continue
		}
		seen[link] = struct{}{}
		summaryParts := make([]string, 0, 3)
		for _, value := range []string{row.SecurityName, row.SecurityCode, row.BulletinType} {
			if value = cleanPlain(value); value != "" {
				summaryParts = append(summaryParts, value)
			}
		}
		published := firstNonEmpty(row.SSEDate, row.AddDate)
		items = append(items, makeOperationalNews(source, link, title, link, published, strings.Join(summaryParts, " "), "", "api", "official", ""))
		if len(items) >= itemLimit {
			break
		}
	}
	if len(response.PageHelp.Data) > 0 && len(items) == 0 {
		return sseHistoryPage{}, errors.New("上交所 API 没有返回可用定期报告")
	}
	return sseHistoryPage{
		Items:     items,
		HasMore:   len(response.PageHelp.Data) > 0 && request.EndPage < response.PageHelp.PageCount,
		PageCount: response.PageHelp.PageCount,
	}, nil
}

func validSSEHistoryPDFURL(raw string) (string, bool) {
	resolved := resolvePublicLink("https://www.sse.com.cn", strings.TrimSpace(raw))
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		strings.ToLower(parsed.Hostname()) != "www.sse.com.cn" || !sseHistoryPDFPathRE.MatchString(parsed.Path) || strings.Contains(parsed.Path, "..") {
		return "", false
	}
	return parsed.String(), true
}

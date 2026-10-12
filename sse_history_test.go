package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEHistoryPagePinsOfficialPDFAndCursor(t *testing.T) {
	end := time.Now().UTC()
	request := sseHistoryRequest{Page: 1, BeginDate: end.AddDate(-10, 0, 0).Format("2006-01-02"), EndDate: end.Format("2006-01-02")}
	source := FeedSource{ID: "sse-periodic-reports", Name: "上交所定期报告", URL: sseHistoryEndpoint}
	body := strings.Replace(sseHistoryFixture(request, 2, sseHistoryPageSize),
		`/disclosure/listedinfo/announcement/c/new/2026-08-02/600000_20260802_X030.pdf`,
		`https://evil.example/disclosure/listedinfo/announcement/c/new/2026-08-02/600000_20260802_X030.pdf`, 1)
	page, err := parseSSEHistoryPage([]byte(body), source, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != sseHistoryPageSize-1 || !page.HasMore || page.PageCount != 2 {
		t.Fatalf("page=%#v", page)
	}
	for _, item := range page.Items {
		parsed, _ := url.Parse(item.URL)
		if parsed.Hostname() != "www.sse.com.cn" || !strings.HasPrefix(parsed.Path, "/disclosure/listedinfo/announcement/") || item.SourceLevel != "official" {
			t.Fatalf("item=%#v", item)
		}
	}
	cursorRequest := request
	cursorRequest.Page = 4
	cursorRequest.EndPage = 4
	cursor := sseHistoryRequestURL(cursorRequest)
	parsedCursor, ok := parseSSEHistoryRequest(cursor, 2)
	if !ok || parsedCursor != cursorRequest {
		t.Fatalf("cursor=%q parsed=%#v ok=%v", cursor, parsedCursor, ok)
	}
	invalidCursors := []string{
		cursor + "&extra=1",
		strings.Replace(cursor, "query.sse.com.cn", "evil.example", 1),
		cursor + "&pageHelp.pageNo=5",
		strings.Replace(cursor, "plaudDashboardCallback", "otherCallback", 1),
		strings.Replace(cursor, cursorRequest.BeginDate, end.AddDate(-9, 0, 0).Format("2006-01-02"), 1),
		strings.Replace(cursor, "pageHelp.pageNo=4", "pageHelp.pageNo=1", 1),
	}
	for _, invalid := range invalidCursors {
		if _, accepted := parseSSEHistoryRequest(invalid, 2); accepted {
			t.Fatalf("unsafe cursor accepted: %q", invalid)
		}
	}
	wrongCallback := strings.Replace(body, sseHistoryCallback+"(", "otherCallback(", 1)
	if _, err := parseSSEHistoryPage([]byte(wrongCallback), source, request); err == nil {
		t.Fatal("wrong JSONP callback was accepted")
	}
	for _, raw := range []string{
		"https://evil.example/disclosure/listedinfo/announcement/c/new/2026-08-01/a.pdf",
		"https://www.sse.com.cn/other/a.pdf",
		"https://www.sse.com.cn/disclosure/listedinfo/announcement/../secret.pdf",
	} {
		if _, accepted := validSSEHistoryPDFURL(raw); accepted {
			t.Fatalf("unsafe PDF accepted: %q", raw)
		}
	}
}

func TestSSEHistoryResumesAcrossSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "sse-periodic-reports")
	var sourceURL string
	var backfillDays, retentionDays, retentionLimit int
	if err := db.QueryRow(`SELECT url,backfill_days,retention_days,retention_limit FROM feed_sources WHERE id='sse-periodic-reports'`).Scan(&sourceURL, &backfillDays, &retentionDays, &retentionLimit); err != nil {
		t.Fatal(err)
	}
	if sourceURL != sseHistoryEndpoint || backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 1000 {
		t.Fatalf("migration url=%q backfill=%d retention=%d limit=%d", sourceURL, backfillDays, retentionDays, retentionLimit)
	}
	previousFetch := sseHistoryPageFetch
	pages := make([]int, 0, 6)
	var pagesMu sync.Mutex
	sseHistoryPageFetch = func(_ context.Context, raw string) (feedFetchResponse, error) {
		request, ok := parseSSEHistoryRequest(raw, 1)
		if !ok {
			return feedFetchResponse{}, fmt.Errorf("invalid request %q", raw)
		}
		pagesMu.Lock()
		for page := request.Page; page <= request.EndPage; page++ {
			pages = append(pages, page)
		}
		pagesMu.Unlock()
		return feedFetchResponse{Body: []byte(sseHistoryFixture(request, 5, sseHistoryPageSize)), ContentType: "application/javascript", FinalURL: raw}, nil
	}
	t.Cleanup(func() { sseHistoryPageFetch = previousFetch })

	for round := 0; round < 2; round++ {
		if err := syncLegacyPublicCollectorFixture(context.Background(), "sse-periodic-reports"); err != nil {
			t.Fatalf("sync round %d: %v", round+1, err)
		}
	}
	sort.Ints(pages)
	if got, want := fmt.Sprint(pages), "[1 1 2 3 4 5]"; got != want {
		t.Fatalf("pages=%s, want %s", got, want)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='sse-periodic-reports'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='sse-periodic-reports'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if entries != 150 || cursor != "" || status != "ok" {
		t.Fatalf("entries=%d cursor=%q status=%q", entries, cursor, status)
	}
}

func TestSSEHistoryFailureKeepsCursorFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "sse-periodic-reports")
	previousFetch := sseHistoryPageFetch
	pages := make([]int, 0, 5)
	var pagesMu sync.Mutex
	sseHistoryPageFetch = func(_ context.Context, raw string) (feedFetchResponse, error) {
		request, ok := parseSSEHistoryRequest(raw, 1)
		if !ok {
			return feedFetchResponse{}, fmt.Errorf("invalid request %q", raw)
		}
		pagesMu.Lock()
		for page := request.Page; page <= request.EndPage; page++ {
			pages = append(pages, page)
		}
		pagesMu.Unlock()
		if request.Page == 4 {
			return feedFetchResponse{}, errors.New("来源返回 HTTP 503")
		}
		return feedFetchResponse{Body: []byte(sseHistoryFixture(request, 10, sseHistoryPageSize)), ContentType: "application/javascript", FinalURL: raw}, nil
	}
	t.Cleanup(func() { sseHistoryPageFetch = previousFetch })

	if err := syncLegacyPublicCollectorFixture(context.Background(), "sse-periodic-reports"); err != nil {
		t.Fatal(err)
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), "sse-periodic-reports"); err != nil {
		t.Fatal(err)
	}
	sort.Ints(pages)
	if got, want := fmt.Sprint(pages), "[1 1 2 3 4 5]"; got != want {
		t.Fatalf("pages=%s, want %s", got, want)
	}
	var cursor, status string
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='sse-periodic-reports'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	request, ok := parseSSEHistoryRequest(cursor, 2)
	if !ok || request.Page != 4 || status != "partial" {
		t.Fatalf("cursor=%q request=%#v ok=%v status=%q", cursor, request, ok, status)
	}
}

func sseHistoryFixture(request sseHistoryRequest, pageCount, itemCount int) string {
	request = normalizedSSEHistoryRequest(request)
	if itemCount == sseHistoryPageSize && request.EndPage > request.Page {
		itemCount *= request.EndPage - request.Page + 1
	}
	rows := make([]map[string]any, 0, itemCount)
	for index := 0; index < itemCount; index++ {
		page := request.Page + index/sseHistoryPageSize
		day := page + 1
		id := strconv.Itoa(page*sseHistoryPageSize + index%sseHistoryPageSize)
		rows = append(rows, map[string]any{
			"ADDDATE":       fmt.Sprintf("2026-08-%02d 17:15:21", day),
			"BULLETIN_TYPE": "年报",
			"SECURITY_CODE": "600000",
			"SECURITY_NAME": "示例公司",
			"SSEDATE":       fmt.Sprintf("2026-08-%02d", day),
			"TITLE":         "年度报告 " + id,
			"URL":           fmt.Sprintf("/disclosure/listedinfo/announcement/c/new/2026-08-%02d/600000_202608%02d_X%03d.pdf", day, day, page*sseHistoryPageSize+index%sseHistoryPageSize),
		})
	}
	payload := map[string]any{
		"beginDate":    request.BeginDate,
		"endDate":      request.EndDate,
		"isPagination": "true",
		"jsonCallBack": sseHistoryCallback,
		"reportType":   "YEARLY",
		"reportType2":  "DQBG",
		"securityType": sseHistorySecurityTypes,
		"pageHelp": map[string]any{
			"beginPage": request.Page,
			"cacheSize": request.EndPage - request.Page + 1,
			"endPage":   request.EndPage,
			"pageCount": pageCount,
			"pageNo":    request.Page,
			"pageSize":  sseHistoryPageSize,
			"total":     pageCount * sseHistoryPageSize,
			"data":      rows,
		},
	}
	body, _ := json.Marshal(payload)
	return sseHistoryCallback + "(" + string(body) + ")"
}

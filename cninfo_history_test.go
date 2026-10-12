package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestCNInfoHistoryPagePinsOfficialPDFsAndCursor(t *testing.T) {
	source := FeedSource{ID: "cninfo-disclosures", Name: "巨潮资讯公告", URL: cnInfoHistoryEndpoint}
	body := strings.Replace(cnInfoHistoryFixture(1, 2), `]}`, `,{"announcementId":"9999999999","announcementTitle":"错误公告","announcementTime":1787270400000,"adjunctUrl":"https://evil.example/finalpage/9999999999.PDF","secName":"错误公司","secCode":"000000"}]}`, 1)
	page, err := parseCNInfoHistoryPage([]byte(body), source, cnInfoHistoryPageSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != cnInfoHistoryPageSize || !page.HasMore || page.TotalPages != 2 {
		t.Fatalf("page=%#v", page)
	}
	for _, item := range page.Items {
		parsed, _ := url.Parse(item.URL)
		if parsed.Hostname() != "static.cninfo.com.cn" || item.SourceLevel != "official" {
			t.Fatalf("item=%#v", item)
		}
	}
	cursor := cnInfoBackfillCursor(2)
	if pageNumber, ok := cnInfoBackfillPage(cursor); !ok || pageNumber != 2 {
		t.Fatalf("cursor=%q page=%d ok=%v", cursor, pageNumber, ok)
	}
	for _, invalid := range []string{cursor + "&extra=1", strings.Replace(cursor, "www.cninfo.com.cn", "evil.example", 1), cursor + "&page=3"} {
		if _, ok := cnInfoBackfillPage(invalid); ok {
			t.Fatalf("unsafe cursor accepted: %q", invalid)
		}
	}
}

func TestCNInfoHistoryResumesAcrossSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "cninfo-disclosures")
	previousFetch := cnInfoPageFetch
	pages := make([]int, 0, 6)
	cnInfoPageFetch = func(_ context.Context, page, pageSize int) (feedFetchResponse, error) {
		if pageSize != cnInfoHistoryPageSize {
			return feedFetchResponse{}, fmt.Errorf("page size=%d", pageSize)
		}
		pages = append(pages, page)
		return feedFetchResponse{Body: []byte(cnInfoHistoryFixture(page, 5)), ContentType: "application/json", FinalURL: cnInfoHistoryEndpoint}, nil
	}
	t.Cleanup(func() { cnInfoPageFetch = previousFetch })

	for round := 0; round < 2; round++ {
		if err := syncLegacyPublicCollectorFixture(context.Background(), "cninfo-disclosures"); err != nil {
			t.Fatalf("sync round %d: %v", round+1, err)
		}
	}
	if got, want := fmt.Sprint(pages), "[1 2 3 1 4 5]"; got != want {
		t.Fatalf("pages=%s, want %s", got, want)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='cninfo-disclosures'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='cninfo-disclosures'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if entries != 150 || cursor != "" || status != "ok" {
		t.Fatalf("entries=%d cursor=%q status=%q", entries, cursor, status)
	}
}

func TestCNInfoBackfillStopsAtRetentionLimitFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "cninfo-disclosures")
	if _, err := db.Exec(`UPDATE feed_sources SET retention_limit=60 WHERE id='cninfo-disclosures'`); err != nil {
		t.Fatal(err)
	}
	previousFetch := cnInfoPageFetch
	pages := make([]int, 0, 3)
	cnInfoPageFetch = func(_ context.Context, page, _ int) (feedFetchResponse, error) {
		pages = append(pages, page)
		return feedFetchResponse{Body: []byte(cnInfoHistoryFixture(page, 10)), ContentType: "application/json", FinalURL: cnInfoHistoryEndpoint}, nil
	}
	t.Cleanup(func() { cnInfoPageFetch = previousFetch })

	if err := syncLegacyPublicCollectorFixture(context.Background(), "cninfo-disclosures"); err != nil {
		t.Fatal(err)
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='cninfo-disclosures'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='cninfo-disclosures'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(pages); got != "[1 2 3]" || entries != 60 || cursor != "" || status != "ok" {
		t.Fatalf("pages=%s entries=%d cursor=%q status=%q", got, entries, cursor, status)
	}
}

func cnInfoHistoryFixture(page, pageCount int) string {
	var body strings.Builder
	fmt.Fprintf(&body, `{"totalAnnouncement":%d,"totalpages":%d,"hasMore":%t,"announcements":[`, pageCount*cnInfoHistoryPageSize, pageCount, page < pageCount)
	for index := 0; index < cnInfoHistoryPageSize; index++ {
		if index > 0 {
			body.WriteByte(',')
		}
		id := strconv.Itoa(1200000000 + page*cnInfoHistoryPageSize + index)
		fmt.Fprintf(&body, `{"announcementId":%q,"announcementTitle":%q,"announcementTime":1787270400000,"adjunctUrl":%q,"secName":%q,"secCode":%q}`, id, "年度报告 "+id, "finalpage/2026-08-21/"+id+".PDF", "示例公司", "600000")
	}
	body.WriteString(`]}`)
	return body.String()
}

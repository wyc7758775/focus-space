package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEPOHistoryDateURLPinsOfficialPublicationDateUnit(t *testing.T) {
	valid := epoHistoryDateURL("20260819")
	if valid != epoHistoryIndexEndpoint+"/20260819/patents" {
		t.Fatalf("valid date URL = %q", valid)
	}
	for _, date := range []string{"", "2026-08-19", "20261301", "../../patents"} {
		if raw := epoHistoryDateURL(date); raw != "" {
			t.Fatalf("unsafe date accepted: %q -> %q", date, raw)
		}
	}
	for _, raw := range []string{
		"http://data.epo.org/publication-server/rest/v1.2/publication-dates/20260819/patents",
		"https://data.epo.org.evil.example/publication-server/rest/v1.2/publication-dates/20260819/patents",
		valid + "?offset=100",
		valid + "#fragment",
	} {
		if validEPOHistoryResponseURL(raw, valid) {
			t.Fatalf("unsafe response URL accepted: %s", raw)
		}
	}
}

func TestEPOHistoryParsersPinOfficialLinksAndBoundLatestWindowUnit(t *testing.T) {
	dateBody := `<html><body>
<a href="https://evil.example/publication-server/rest/v1.2/publication-dates/20260820/patents">evil</a>
<a href="/publication-server/rest/v1.2/publication-dates/20260812/patents">older</a>
<a href="/publication-server/rest/v1.2/publication-dates/20260819/patents">latest</a>
<a href="/publication-server/rest/v1.2/publication-dates/20260819/patents">duplicate</a>
<a href="/publication-server/rest/v1.2/publication-dates/20260805/patents?redirect=1">query</a>
</body></html>`
	dates, err := parseEPOHistoryDates([]byte(dateBody), time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fmt.Sprint(dates), "[20260819 20260812]"; got != want {
		t.Fatalf("dates=%s want=%s", got, want)
	}

	var patentBody strings.Builder
	patentBody.WriteString(`<html><body><a href="https://evil.example/publication-server/rest/v1.2/patents/EP9999999NWA1">evil</a>`)
	for index := 0; index < epoHistoryPerDateLimit+5; index++ {
		id := fmt.Sprintf("EP%07dNWA1", 4_000_000+index)
		fmt.Fprintf(&patentBody, `<a href="/publication-server/rest/v1.2/patents/%s">%s</a>`, id, id)
	}
	patentBody.WriteString(`</body></html>`)
	patents, err := parseEPOHistoryPatents([]byte(patentBody.String()), epoHistoryPerDateLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(patents) != epoHistoryPerDateLimit || patents[0] != "EP4000000NWA1" || patents[len(patents)-1] != "EP4000999NWA1" {
		t.Fatalf("unexpected bounded patents: count=%d first=%q last=%q", len(patents), patents[0], patents[len(patents)-1])
	}
}

func TestEPOHistoryRefreshReplacesFullRetentionWindowFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "epo-publication-server")
	dates := epoHistoryTestDates(time.Now().UTC(), 3)
	phase := 0
	previousFetch := operationalSourceFetch
	requests := make([]string, 0, 4)
	operationalSourceFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		requests = append(requests, raw)
		if raw == epoHistoryIndexEndpoint {
			visibleDates := dates
			if phase == 0 {
				visibleDates = dates[1:]
			}
			return feedFetchResponse{Body: []byte(epoHistoryDatesFixture(visibleDates)), ContentType: "text/html", FinalURL: raw}, nil
		}
		switch raw {
		case epoHistoryDateURL(dates[0]):
			return feedFetchResponse{Body: []byte(epoHistoryPatentsFixture(0, 1200)), ContentType: "text/html", FinalURL: raw}, nil
		case epoHistoryDateURL(dates[1]):
			return feedFetchResponse{Body: []byte(epoHistoryPatentsFixture(1, 1000)), ContentType: "text/html", FinalURL: raw}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	t.Cleanup(func() { operationalSourceFetch = previousFetch })

	if err := syncLegacyPublicCollectorFixture(context.Background(), "epo-publication-server"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET backfill_cursor=? WHERE id='epo-publication-server'`, epoHistoryDateURL(dates[1])+"?offset=100"); err != nil {
		t.Fatal(err)
	}
	phase = 1
	if err := syncLegacyPublicCollectorFixture(context.Background(), "epo-publication-server"); err != nil {
		t.Fatal(err)
	}

	var entries int
	var oldest, newest, cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*),MIN(published_at),MAX(published_at) FROM news WHERE source_id='epo-publication-server' AND deleted_at=''`).Scan(&entries, &oldest, &newest); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='epo-publication-server'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	wantPublished := epoHistoryPublishedAt(dates[0])
	wantRequests := []string{epoHistoryIndexEndpoint, epoHistoryDateURL(dates[1]), epoHistoryIndexEndpoint, epoHistoryDateURL(dates[0])}
	if entries != 1000 || oldest != wantPublished || newest != wantPublished || cursor != "" || status != "ok" || fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("entries=%d oldest=%q newest=%q cursor=%q status=%q requests=%v", entries, oldest, newest, cursor, status, requests)
	}
}

func TestEPOHistoryFillsWindowFromOlderPublicationDatesFlow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	dates := epoHistoryTestDates(now, 3)
	source := FeedSource{ID: "epo-publication-server", Name: "EPO 欧洲专利发布", URL: epoHistoryIndexEndpoint, BackfillDays: 3650, RetentionLimit: 1000}
	requests := make([]string, 0, 3)
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		requests = append(requests, raw)
		switch raw {
		case epoHistoryIndexEndpoint:
			return feedFetchResponse{Body: []byte(epoHistoryDatesFixture(dates)), FinalURL: raw}, nil
		case epoHistoryDateURL(dates[0]):
			return feedFetchResponse{Body: []byte(epoHistoryPatentsFixture(0, 200)), FinalURL: raw}, nil
		case epoHistoryDateURL(dates[1]):
			return feedFetchResponse{Body: []byte(epoHistoryPatentsFixture(1, 900)), FinalURL: raw}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	result := collectEPOHistoryOperational(context.Background(), source, operationalSourceSpecs[source.ID], fetcher, now.Format(time.RFC3339))
	wantRequests := []string{epoHistoryIndexEndpoint, epoHistoryDateURL(dates[0]), epoHistoryDateURL(dates[1])}
	if result.Status != "ok" || len(result.Feed.Items) != 1000 || fmt.Sprint(requests) != fmt.Sprint(wantRequests) {
		t.Fatalf("status=%q items=%d requests=%v message=%q", result.Status, len(result.Feed.Items), requests, result.Message)
	}
}

func TestEPOHistoryLaterDateFailureKeepsBoundedPartialResultFlow(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	dates := epoHistoryTestDates(now, 3)
	source := FeedSource{
		ID:             "epo-publication-server",
		Name:           "EPO 欧洲专利发布",
		URL:            epoHistoryIndexEndpoint,
		BackfillDays:   3650,
		RetentionLimit: 1000,
		BackfillCursor: epoHistoryDateURL(dates[1]) + "?offset=100",
	}
	fetcher := func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		switch raw {
		case epoHistoryIndexEndpoint:
			return feedFetchResponse{Body: []byte(epoHistoryDatesFixture(dates)), FinalURL: raw}, nil
		case epoHistoryDateURL(dates[0]):
			return feedFetchResponse{Body: []byte(epoHistoryPatentsFixture(0, 200)), FinalURL: raw}, nil
		case epoHistoryDateURL(dates[1]):
			return feedFetchResponse{}, errors.New("HTTP 503")
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL: %s", raw)
		}
	}
	result := collectEPOHistoryOperational(context.Background(), source, operationalSourceSpecs[source.ID], fetcher, now.Format(time.RFC3339))
	if result.Status != "partial" || len(result.Feed.Items) != 200 || result.Backfill || !result.BackfillCursorChanged || result.BackfillCursor != "" {
		t.Fatalf("partial result=%#v", result)
	}
}

func TestEPOHistoryMigrationUsesOfficialBoundedWindow(t *testing.T) {
	db = openTestDB(t)
	var rawURL, cursor string
	var backfillDays, retentionDays, retentionLimit int
	if err := db.QueryRow(`SELECT url,backfill_days,retention_days,retention_limit,backfill_cursor FROM feed_sources WHERE id='epo-publication-server'`).Scan(&rawURL, &backfillDays, &retentionDays, &retentionLimit, &cursor); err != nil {
		t.Fatal(err)
	}
	if rawURL != "source-retired://epo-publication-server" || backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 1000 || cursor != "" {
		t.Fatalf("url=%q window=%d/%d/%d cursor=%q", rawURL, backfillDays, retentionDays, retentionLimit, cursor)
	}
}

func epoHistoryTestDates(now time.Time, count int) []string {
	dates := make([]string, 0, count)
	for index := 0; index < count; index++ {
		dates = append(dates, now.Add(-time.Duration(2+index*7)*24*time.Hour).Format("20060102"))
	}
	return dates
}

func epoHistoryDatesFixture(dates []string) string {
	var body strings.Builder
	body.WriteString("<html><body>")
	for _, date := range dates {
		fmt.Fprintf(&body, `<a href="/publication-server/rest/v1.2/publication-dates/%s/patents">%s</a>`, date, date)
	}
	body.WriteString("</body></html>")
	return body.String()
}

func epoHistoryPatentsFixture(dateIndex, count int) string {
	var body strings.Builder
	body.WriteString("<html><body>")
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("EP%07dNWA1", 4_000_000+dateIndex*1000+index)
		fmt.Fprintf(&body, `<a href="/publication-server/rest/v1.2/patents/%s">%s</a>`, id, id)
	}
	body.WriteString("</body></html>")
	return body.String()
}

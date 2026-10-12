package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type secHistoryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn secHistoryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestSECHistoryQuarterCursorPinsOfficialArchiveUnit(t *testing.T) {
	quarter := secHistoryQuarterAt(time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC))
	if quarter != "2026-Q3" || previousSECQuarter(quarter) != "2026-Q2" {
		t.Fatalf("quarter=%q previous=%q", quarter, previousSECQuarter(quarter))
	}
	wantURL := "https://www.sec.gov/Archives/edgar/full-index/2026/QTR3/xbrl.gz"
	if raw := secHistoryQuarterURL(quarter); raw != wantURL {
		t.Fatalf("url=%q want=%q", raw, wantURL)
	}
	if parsed, ok := secHistoryQuarterFromURL(wantURL); !ok || parsed != quarter {
		t.Fatalf("parsed=%q ok=%v", parsed, ok)
	}
	cursor := secHistoryCursor(quarter)
	if parsed, ok := validSECHistoryCursor(cursor); !ok || parsed != quarter {
		t.Fatalf("cursor=%q parsed=%q ok=%v", cursor, parsed, ok)
	}
	for _, invalid := range []string{
		cursor + ":0",
		strings.Replace(wantURL, "www.sec.gov", "evil.example", 1),
		wantURL + "?download=1",
		"https://www.sec.gov/Archives/edgar/full-index/2026/QTR5/xbrl.gz",
	} {
		if strings.HasPrefix(invalid, secHistoryCursorPrefix) {
			if _, ok := validSECHistoryCursor(invalid); ok {
				t.Fatalf("unsafe cursor accepted: %q", invalid)
			}
			continue
		}
		if _, ok := secHistoryQuarterFromURL(invalid); ok {
			t.Fatalf("unsafe URL accepted: %q", invalid)
		}
	}
}

func TestParseSECQuarterGZIPKeepsLatestBoundedWindowUnit(t *testing.T) {
	source := secHistoryTestSource()
	raw := secHistoryQuarterURL("2026-Q3")
	body := secHistoryGZIPFixture(t,
		"1000209|MEDALLION FINANCIAL CORP|10-Q|2026-08-05|edgar/data/1000209/0001193125-26-335042.txt",
		"1000191|RIVERSOURCE VARIABLE ACCOUNT 10|497|2026-08-14|edgar/data/1000191/0001193125-26-352552.txt",
		"1000191|RIVERSOURCE VARIABLE ACCOUNT 10 DUPLICATE|497|2026-08-14|edgar/data/1000191/0001193125-26-352552.txt",
		"1000228|HENRY SCHEIN INC|8-K|2026-07-30|edgar/data/1000228/0001193125-26-324857.txt",
		"invalid|Unsafe|8-K|2026-08-20|edgar/data/1/../unsafe.txt",
	)
	feed, err := parseSECQuarterGZIP(bytes.NewReader(body), source, raw, 2, secHistoryMaxCompressedBytes, secHistoryMaxDecompressedBytes)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Format != "idx" || len(feed.Items) != 2 {
		t.Fatalf("feed=%#v", feed)
	}
	if feed.Items[0].ExternalID != "0001193125-26-352552:497" || feed.Items[1].ExternalID != "0001193125-26-335042:10-Q" {
		t.Fatalf("items=%#v", feed.Items)
	}
	wantURL := "https://www.sec.gov/Archives/edgar/data/1000191/000119312526352552/0001193125-26-352552-index.html"
	if feed.Items[0].URL != wantURL || feed.Items[0].PermissionScope != "public_read" || feed.Items[0].SourceLevel != "official" {
		t.Fatalf("item=%#v want_url=%q", feed.Items[0], wantURL)
	}
}

func TestSECQuarterHistoryRetriesTransientResponseOnceUnit(t *testing.T) {
	previousFactory := secHistoryClientFactory
	var attempts atomic.Int32
	source := secHistoryTestSource()
	raw := secHistoryQuarterURL("2026-Q3")
	body := secHistoryGZIPFixture(t, "1000209|MEDALLION FINANCIAL CORP|10-Q|2026-08-05|edgar/data/1000209/0001193125-26-335042.txt")
	secHistoryClientFactory = func() *http.Client {
		return &http.Client{Transport: secHistoryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempt := attempts.Add(1)
			if request.URL.String() != raw || request.Header.Get("Accept") == "" {
				t.Fatalf("request=%s accept=%q", request.URL, request.Header.Get("Accept"))
			}
			if attempt == 1 {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("retry")), Request: request}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
		})}
	}
	t.Cleanup(func() { secHistoryClientFactory = previousFactory })

	feed, err := fetchSECQuarterHistory(context.Background(), raw, source, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || len(feed.Items) != 1 {
		t.Fatalf("attempts=%d items=%d", attempts.Load(), len(feed.Items))
	}
}

func TestSECHistoryUsesDedicatedSourceTimeoutUnit(t *testing.T) {
	if feedSyncTimeoutForSource("sec-xbrl-filings") != secHistorySourceTimeout {
		t.Fatalf("SEC timeout=%s", feedSyncTimeoutForSource("sec-xbrl-filings"))
	}
	if secHistorySourceTimeout < 2*secHistoryRequestTimeout {
		t.Fatalf("source timeout=%s cannot cover retries=%s", secHistorySourceTimeout, secHistoryRequestTimeout)
	}
	client := newSECHistoryHTTPClient()
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout != secHistoryRequestTimeout || client.Timeout != secHistoryRequestTimeout {
		t.Fatalf("client timeout=%s transport=%#v", client.Timeout, client.Transport)
	}
}

func TestSECHistoryResumesAcrossQuarterSyncFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "sec-xbrl-filings")
	previousFetch := secHistoryQuarterFetch
	reference := time.Now().UTC()
	current := secHistoryQuarterAt(reference)
	previous := previousSECQuarter(current)
	calls := make([]string, 0, 2)
	secHistoryQuarterFetch = func(_ context.Context, raw string, source FeedSource, limit int) (fetchedFeed, error) {
		calls = append(calls, raw)
		quarter, ok := secHistoryQuarterFromURL(raw)
		if !ok || limit != 1000 {
			return fetchedFeed{}, errors.New("invalid SEC test request")
		}
		year, number, _ := parseSECQuarter(quarter)
		published := time.Date(year, time.Month((number-1)*3+1), 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
		accession := fmt.Sprintf("0001000000-%02d-%06d", year%100, number)
		item := makeOperationalNews(source, accession+":8-K", quarter+" filing", "https://www.sec.gov/Archives/edgar/data/1000000/"+strings.ReplaceAll(accession, "-", "")+"/"+accession+"-index.html", published, "8-K XBRL filing", "", "sec-xbrl", "official", "")
		return fetchedFeed{Title: source.Name, SiteURL: "https://www.sec.gov/", FeedURL: source.URL, Format: "idx", Items: []NewsItem{item}}, nil
	}
	t.Cleanup(func() { secHistoryQuarterFetch = previousFetch })

	for round := 0; round < 2; round++ {
		if err := syncLegacyPublicCollectorFixture(context.Background(), "sec-xbrl-filings"); err != nil {
			t.Fatalf("round %d: %v", round+1, err)
		}
	}
	var entries int
	var cursor, status string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id='sec-xbrl-filings'`).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id='sec-xbrl-filings'`).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{secHistoryQuarterURL(current), secHistoryQuarterURL(previous)}
	if fmt.Sprint(calls) != fmt.Sprint(wantCalls) || entries != 2 || cursor != secHistoryCursor(previousSECQuarter(previous)) || status != "partial" {
		t.Fatalf("calls=%v entries=%d cursor=%q status=%q", calls, entries, cursor, status)
	}
}

func TestSECCurrentFallbackKeepsReleasedAccessionIdentityUnit(t *testing.T) {
	source := secHistoryTestSource()
	response := feedFetchResponse{Body: []byte(secHistoryRSSFixture()), FinalURL: secCurrentFeedEndpoint}
	feed, err := parseXMLFeedWithLimit(response.Body, secCurrentFeedEndpoint, feedFetchMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	feed, err = validateSECXBRLFeed(feed, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].ExternalID != "0000094845-26-000041:8-K" {
		t.Fatalf("items=%#v", feed.Items)
	}
}

func TestSECQuarterlyMigrationPreservesUserStateFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "091_")
	now := "2026-08-21T00:00:00Z"
	if _, err := conn.Exec(`INSERT INTO news
(id,external_id,title,url,source,source_id,origin,source_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at)
VALUES('sec-retained','0000094845-26-000041:8-K','SEC retained','https://www.sec.gov/Archives/edgar/data/94845/000009484526000041/0000094845-26-000041-index.html','SEC','sec-xbrl-filings','app-fetch','sec-xbrl','2026-08-20T00:00:00Z','','text','','sec-hash',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at)
VALUES('news:sec-retained',1,1,0,0,?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO stable_context_items(owner_id,profile_id,item_key,state,added_by,added_at,updated_at,content_hash)
VALUES('owner','profile','news:sec-retained','active','manual_confirm',?,?, 'context-hash')`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_sources SET last_sync_at=?,next_sync_at=?,last_error='timeout',records_seen=20 WHERE id='sec-xbrl-filings'`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE feed_subscriptions SET enabled=0,folder_id='folder-financial' WHERE source_id='sec-xbrl-filings'`); err != nil {
		t.Fatal(err)
	}

	applyMigrationFile(t, conn, "091_sec_quarterly_xbrl_history.sql")

	var lastSync, nextSync, lastError, format, folder string
	var backfillDays, retentionLimit, newsCount, preferred, contextual, enabled int
	if err := conn.QueryRow(`SELECT last_sync_at,next_sync_at,last_error,backfill_days,retention_limit FROM feed_sources WHERE id='sec-xbrl-filings'`).Scan(&lastSync, &nextSync, &lastError, &backfillDays, &retentionLimit); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT feed_format FROM feed_catalog_entries WHERE source_id='sec-xbrl-filings'`).Scan(&format); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT enabled,folder_id FROM feed_subscriptions WHERE source_id='sec-xbrl-filings'`).Scan(&enabled, &folder); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM news WHERE id='sec-retained'`).Scan(&newsCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM item_preferences WHERE item_key='news:sec-retained' AND is_read=1 AND starred=1`).Scan(&preferred); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE item_key='news:sec-retained' AND state='active'`).Scan(&contextual); err != nil {
		t.Fatal(err)
	}
	if lastSync != "" || nextSync != "" || lastError != "" || backfillDays != 3650 || retentionLimit != 1000 || format != "idx" || enabled != 0 || folder != "folder-financial" || newsCount != 1 || preferred != 1 || contextual != 1 {
		t.Fatalf("sync=%q next=%q error=%q backfill=%d limit=%d format=%q enabled=%d folder=%q news=%d preferred=%d context=%d", lastSync, nextSync, lastError, backfillDays, retentionLimit, format, enabled, folder, newsCount, preferred, contextual)
	}
}

func secHistoryTestSource() FeedSource {
	return FeedSource{ID: "sec-xbrl-filings", Name: "SEC XBRL 财报申报", URL: secHistoryEndpoint, Kind: "rss", BackfillDays: 3650, RetentionLimit: 1000}
}

func secHistoryGZIPFixture(t *testing.T, rows ...string) []byte {
	t.Helper()
	var raw strings.Builder
	raw.WriteString("Description:           XBRL Index of EDGAR Dissemination Feed\n")
	raw.WriteString("Last Data Received:    August 21, 2026\n\n")
	raw.WriteString(secHistoryIndexHeader + "\n")
	raw.WriteString(strings.Repeat("-", 80) + "\n")
	for _, row := range rows {
		raw.WriteString(row + "\n")
	}
	var compressed bytes.Buffer
	archive := gzip.NewWriter(&compressed)
	if _, err := archive.Write([]byte(raw.String())); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func secHistoryRSSFixture() string {
	return `<rss><channel><title>All XBRL Data Submitted to the SEC</title><link>https://www.sec.gov/spotlight/xbrl/filings-and-feeds.shtml</link><item><title>Example Corp (0000094845) (Filer)</title><link>https://www.sec.gov/Archives/edgar/data/94845/000009484526000041/0000094845-26-000041-index.html</link><guid>https://www.sec.gov/Archives/edgar/data/94845/000009484526000041/0000094845-26-000041-xbrl.zip</guid><description>8-K</description><pubDate>Thu, 20 Aug 2026 12:00:00 EDT</pubDate></item></channel></rss>`
}

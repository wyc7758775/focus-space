package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func ruanYiFengArchiveFixture(count int) string {
	var body strings.Builder
	body.WriteString(`<html><head><title>分类：周刊 - 阮一峰的网络日志</title></head><body><ul>`)
	for index := count; index >= 1; index-- {
		date := fmt.Sprintf("40@2026.08.%02d", (index%28)+1)
		encoded := encodeCloudflareFixture(date, byte(index%200+1))
		fmt.Fprintf(&body, `<li class="module-list-item"><a href="https://www.ruanyifeng.com/blog/2026/08/weekly-issue-%d.html">科技爱好者周刊（第 %d 期）</a><span data-cfemail="%s">x</span></li>`, index, index, encoded)
	}
	body.WriteString(`</ul></body></html>`)
	return body.String()
}

func encodeCloudflareFixture(value string, key byte) string {
	encoded := []byte{key}
	for _, current := range []byte(value) {
		encoded = append(encoded, current^key)
	}
	return fmt.Sprintf("%x", encoded)
}

func TestRuanYiFengOfficialArchiveParsesAtLeastTwoHundredFlow(t *testing.T) {
	source := FeedSource{ID: ruanYiFengSourceID, Name: "阮一峰的网络日志", URL: ruanYiFengFeedURL}
	items, err := parseRuanYiFengWeeklyArchive([]byte(ruanYiFengArchiveFixture(210)), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 210 || items[0].Title != "科技爱好者周刊（第 210 期）" || items[0].PublishedAt == "" {
		t.Fatalf("archive items=%d first=%#v", len(items), items[0])
	}
	if items[0].URL != "https://www.ruanyifeng.com/blog/2026/08/weekly-issue-210.html" {
		t.Fatalf("unexpected official URL: %s", items[0].URL)
	}
}

func TestRuanYiFengOfficialArchiveRejectsShortOrForeignHistory(t *testing.T) {
	source := FeedSource{ID: ruanYiFengSourceID, Name: "阮一峰的网络日志", URL: ruanYiFengFeedURL}
	if _, err := parseRuanYiFengWeeklyArchive([]byte(ruanYiFengArchiveFixture(199)), source); err == nil {
		t.Fatal("short archive must not masquerade as complete history")
	}
	foreign := strings.ReplaceAll(ruanYiFengArchiveFixture(210), "www.ruanyifeng.com", "example.com")
	if _, err := parseRuanYiFengWeeklyArchive([]byte(foreign), source); err == nil {
		t.Fatal("foreign archive links must fail closed")
	}
}

func TestRuanYiFengPublicFeedMergesCurrentAndOfficialHistoryFlow(t *testing.T) {
	oldFetch := localCustomFeedFetch
	t.Cleanup(func() { localCustomFeedFetch = oldFetch })
	localCustomFeedFetch = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		switch raw {
		case ruanYiFengFeedURL:
			return feedFetchResponse{Body: []byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"><title>阮一峰的网络日志</title><link href="https://www.ruanyifeng.com/blog/"/><entry><id>weekly-210</id><title>科技爱好者周刊（第 210 期）</title><link href="https://www.ruanyifeng.com/blog/2026/08/weekly-issue-210.html"/><updated>2026-08-28T00:00:00Z</updated><summary>当前 Feed 完整摘要</summary></entry></feed>`), ContentType: "application/atom+xml", FinalURL: raw}, nil
		case ruanYiFengWeeklyArchiveURL:
			return feedFetchResponse{Body: []byte(ruanYiFengArchiveFixture(210)), ContentType: "text/html", FinalURL: raw}, nil
		default:
			return feedFetchResponse{}, fmt.Errorf("unexpected URL %s", raw)
		}
	}
	source := FeedSource{ID: ruanYiFengSourceID, Name: "阮一峰的网络日志", URL: ruanYiFengFeedURL, RetentionLimit: 1000}
	result, err := collectRuanYiFengPublicFeed(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if result.status != "ok" || len(result.feed.Items) != 210 {
		t.Fatalf("status=%s items=%d message=%s", result.status, len(result.feed.Items), result.message)
	}
	if result.feed.Items[0].SummaryText != "当前 Feed 完整摘要" {
		t.Fatalf("current rich item was overwritten: %#v", result.feed.Items[0])
	}
	if parsed, err := time.Parse(time.RFC3339, result.feed.Items[1].PublishedAt); err != nil || parsed.IsZero() {
		t.Fatalf("history date invalid: %q %v", result.feed.Items[1].PublishedAt, err)
	}
}

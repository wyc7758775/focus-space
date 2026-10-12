package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type verifiedHistoryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn verifiedHistoryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func verifiedHistoryTestSource(sourceID string) FeedSource {
	feedURL := weChatVerifiedDirectFeedURLs[sourceID]
	policy := weChatVerifiedDirectFeedPolicies[feedURL]
	return FeedSource{
		ID:               sourceID,
		Name:             policy.SourceName,
		URL:              feedURL,
		Kind:             "wechat",
		CollectionMode:   "app",
		SyncIntervalSecs: 21600,
		BackfillDays:     3650,
		RetentionDays:    3650,
		RetentionLimit:   1000,
	}
}

func verifiedHistoryArticleURL(biz string, index int) string {
	return fmt.Sprintf("https://mp.weixin.qq.com/s?__biz=%s&mid=%d&idx=1&sn=history%d", biz, 2247500000+index, index)
}

func verifiedHistoryFixture(source FeedSource, policy weChatVerifiedFeedPolicy, links []string, mutate func(map[string]any, []map[string]any)) []byte {
	entries := make([]map[string]any, 0, len(links))
	for index, link := range links {
		entries = append(entries, map[string]any{
			"id":          fmt.Sprintf("remote-%d", index),
			"title":       fmt.Sprintf("历史文章 %d", index),
			"url":         link,
			"guid":        link,
			"publishedAt": time.Date(2026, time.August, 20-index%10, 10, 0, 0, index, time.UTC).Format(time.RFC3339Nano),
			"author":      source.Name,
			"content":     strings.Repeat("正文不会进入历史元数据 ", 128),
			"media":       []map[string]any{{"url": "https://mmbiz.qpic.cn/history/0"}},
		})
	}
	feed := map[string]any{
		"type":    "feed",
		"id":      policy.HistoryFeedID,
		"url":     source.URL,
		"siteUrl": source.URL,
		"title":   policy.FeedTitle,
	}
	if mutate != nil {
		mutate(feed, entries)
	}
	body, _ := json.Marshal(map[string]any{
		"code": 0,
		"data": map[string]any{
			"analytics": map[string]any{"ignored": []any{1, 2, 3}},
			"entries":   entries,
			"feed":      feed,
		},
	})
	return body
}

func verifiedHistoryOrderedFixture(source FeedSource, policy weChatVerifiedFeedPolicy, links []string, contentBytes int) []byte {
	feed, _ := json.Marshal(map[string]any{
		"type":    "feed",
		"id":      policy.HistoryFeedID,
		"url":     source.URL,
		"siteUrl": source.URL,
		"title":   policy.FeedTitle,
	})
	var body strings.Builder
	body.WriteString(`{"code":0,"data":{"feed":`)
	body.Write(feed)
	body.WriteString(`,"entries":[`)
	for index, link := range links {
		if index > 0 {
			body.WriteByte(',')
		}
		entry, _ := json.Marshal(map[string]any{
			"title":       fmt.Sprintf("历史文章 %d", index),
			"url":         link,
			"guid":        link,
			"publishedAt": time.Date(2026, time.August, 20-index%10, 10, 0, 0, index, time.UTC).Format(time.RFC3339Nano),
			"author":      source.Name,
			"content":     strings.Repeat("x", contentBytes),
		})
		body.Write(entry)
	}
	body.WriteString(`]}}`)
	return []byte(body.String())
}

func parseVerifiedHistoryFixture(t *testing.T, source FeedSource, links []string) fetchedFeed {
	t.Helper()
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	body := verifiedHistoryFixture(source, policy, links, nil)
	feed, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), verifiedWeChatHistoryMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	return feed
}

func TestVerifiedWeChatHistoryStreamParserIsBoundedAndMetadataOnlyUnit(t *testing.T) {
	source := verifiedHistoryTestSource("wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := []string{
		verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 1),
		verifiedHistoryArticleURL("MzY5ODQwMTkxNA==", 2),
	}
	body := verifiedHistoryFixture(source, policy, links, nil)
	feed, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), 2)
	if err != nil {
		t.Fatal(err)
	}
	if feed.Title != source.Name || len(feed.Items) != 2 {
		t.Fatalf("feed=%#v", feed)
	}
	for _, item := range feed.Items {
		if !item.historyMetadata || item.SummaryText != "" || item.SafeHTML != "" || !strings.HasPrefix(item.ExternalID, "wechat:") {
			t.Fatalf("history item loaded rich content or lost identity: %#v", item)
		}
	}
	if _, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)-1), 2); err == nil || !strings.Contains(err.Error(), "大小上限") {
		t.Fatalf("byte limit error=%v", err)
	}
	if _, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), 1); err == nil || !strings.Contains(err.Error(), "数量上限") {
		t.Fatalf("item limit error=%v", err)
	}
}

func TestVerifiedWeChatHistoryStopsReadingAfterRetentionWindowUnit(t *testing.T) {
	source := verifiedHistoryTestSource("wechat-qbitai")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	biz := "MzIzNjc1NzUzMw=="
	links := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		links = append(links, verifiedHistoryArticleURL(biz, index+1))
	}
	body := verifiedHistoryOrderedFixture(source, policy, links, 64<<10)
	reader := strings.NewReader(string(body))
	feed, err := parseVerifiedWeChatHistoryWithLimits(reader, source, policy, int64(len(body)+1), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 3 || reader.Len() == 0 {
		t.Fatalf("items=%d unread=%d total=%d", len(feed.Items), reader.Len(), len(body))
	}
}

func TestVerifiedWeChatHistoryKeepsNewestArticleRevisionUnit(t *testing.T) {
	source := verifiedHistoryTestSource("wechat-qbitai")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	older := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247900001&idx=1&sn=older"
	newer := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247900001&idx=1&sn=newer"
	body := verifiedHistoryFixture(source, policy, []string{older, newer}, func(_ map[string]any, entries []map[string]any) {
		entries[0]["publishedAt"] = "2026-08-19T00:00:00Z"
		entries[1]["publishedAt"] = "2026-08-20T00:00:00Z"
	})
	feed, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), verifiedWeChatHistoryMaxItems)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Items) != 1 || feed.Items[0].URL != newer || feed.Items[0].PublishedAt != "2026-08-20T00:00:00Z" {
		t.Fatalf("items=%#v", feed.Items)
	}
}

func TestVerifiedWeChatHistoryParserAcceptsProductionSizedFeedsUnit(t *testing.T) {
	tests := []struct {
		sourceID string
		count    int
		biz      []string
	}{
		{sourceID: "wechat-founder-park", count: 287, biz: []string{"Mzg5NTc0MjgwMw==", "MzY5ODQwMTkxNA=="}},
		{sourceID: "wechat-liuxiaopai", count: 215, biz: []string{"MzI1MTUxNzgxMA=="}},
		{sourceID: "wechat-qbitai", count: 1000, biz: []string{"MzIzNjc1NzUzMw=="}},
	}
	for _, test := range tests {
		t.Run(test.sourceID, func(t *testing.T) {
			source := verifiedHistoryTestSource(test.sourceID)
			policy := weChatVerifiedDirectFeedPolicies[source.URL]
			links := make([]string, 0, test.count)
			for index := 0; index < test.count; index++ {
				links = append(links, verifiedHistoryArticleURL(test.biz[index%len(test.biz)], index+1))
			}
			body := verifiedHistoryFixture(source, policy, links, nil)
			feed, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), verifiedWeChatHistoryMaxItems)
			if err != nil {
				t.Fatal(err)
			}
			if len(feed.Items) != test.count {
				t.Fatalf("parsed %d items, want %d", len(feed.Items), test.count)
			}
		})
	}
}

func TestVerifiedWeChatHistoryRejectsFeedAndArticleIdentityDriftUnit(t *testing.T) {
	source := verifiedHistoryTestSource("wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	valid := verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 1)
	tests := []struct {
		name   string
		mutate func(map[string]any, []map[string]any)
	}{
		{name: "wrong feed id", mutate: func(feed map[string]any, _ []map[string]any) { feed["id"] = "187702008971600931" }},
		{name: "wrong feed type", mutate: func(feed map[string]any, _ []map[string]any) { feed["type"] = "collection" }},
		{name: "wrong feed title", mutate: func(feed map[string]any, _ []map[string]any) { feed["title"] = "其他来源" }},
		{name: "wrong feed url", mutate: func(feed map[string]any, _ []map[string]any) { feed["url"] = "https://example.com/feed.xml" }},
		{name: "wrong biz", mutate: func(_ map[string]any, entries []map[string]any) {
			entries[0]["url"] = verifiedHistoryArticleURL("wrong", 1)
			entries[0]["guid"] = entries[0]["url"]
		}},
		{name: "missing mid", mutate: func(_ map[string]any, entries []map[string]any) {
			entries[0]["url"] = "https://mp.weixin.qq.com/s?__biz=Mzg5NTc0MjgwMw==&idx=1&sn=history"
			entries[0]["guid"] = entries[0]["url"]
		}},
		{name: "duplicate biz", mutate: func(_ map[string]any, entries []map[string]any) {
			entries[0]["url"] = valid + "&__biz=MzY5ODQwMTkxNA=="
			entries[0]["guid"] = entries[0]["url"]
		}},
		{name: "guid drift", mutate: func(_ map[string]any, entries []map[string]any) {
			entries[0]["guid"] = verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 2)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body := verifiedHistoryFixture(source, policy, []string{valid}, test.mutate)
			if _, err := parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), source, policy, int64(len(body)+1), 2); err == nil {
				t.Fatal("identity drift was accepted")
			}
		})
	}
}

func TestVerifiedWeChatHistoryEndpointAndCursorAreExactUnit(t *testing.T) {
	historySources := []string{
		"wechat-aiera",
		"wechat-ali-tech",
		"wechat-chaping",
		"wechat-founder-park",
		"wechat-geekpark",
		"wechat-jiqizhixin",
		"wechat-liuxiaopai",
		"wechat-paperweekly",
		"wechat-qbitai",
	}
	for _, sourceID := range historySources {
		source := verifiedHistoryTestSource(sourceID)
		policy := weChatVerifiedDirectFeedPolicies[source.URL]
		endpoint, ok := verifiedWeChatHistoryEndpoint(policy)
		if !ok || endpoint != "https://api.folo.is/feeds?id="+policy.HistoryFeedID+"&entriesLimit=-1" {
			t.Fatalf("endpoint=%q ok=%v", endpoint, ok)
		}
		if !verifiedWeChatHistoryPending(source, policy) || feedSyncTimeoutForSource(sourceID) != verifiedWeChatHistorySourceTimeout {
			t.Fatalf("source %s was not queued with the history budget", sourceID)
		}
		source.BackfillCursor = verifiedWeChatHistoryCursor(policy)
		if verifiedWeChatHistoryPending(source, policy) {
			t.Fatalf("completed source %s stayed pending", sourceID)
		}
	}
	if feedSyncTimeoutForSource("wechat-ai-paper") != feedFetchTimeout {
		t.Fatal("ordinary source timeout changed")
	}
}

func TestVerifiedWeChatHistoryClientUsesDedicatedHeaderTimeoutUnit(t *testing.T) {
	client := newVerifiedWeChatHistoryHTTPClient()
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("history transport = %T", client.Transport)
	}
	if client.Timeout != verifiedWeChatHistoryRequestTimeout || transport.ResponseHeaderTimeout != verifiedWeChatHistoryHeaderTimeout {
		t.Fatalf("client timeout=%s header timeout=%s", client.Timeout, transport.ResponseHeaderTimeout)
	}
}

func TestVerifiedWeChatHistoryRetriesTransientResponseOnceUnit(t *testing.T) {
	source := verifiedHistoryTestSource("wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	body := verifiedHistoryFixture(source, policy, []string{verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 1)}, nil)
	previousFactory := verifiedWeChatHistoryClientFactory
	previousSlot := verifiedWeChatHistorySlot
	verifiedWeChatHistorySlot = make(chan struct{}, 1)
	calls := 0
	verifiedWeChatHistoryClientFactory = func() *http.Client {
		return &http.Client{Transport: verifiedHistoryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			status, responseBody := http.StatusServiceUnavailable, "temporarily unavailable"
			if calls == 2 {
				status, responseBody = http.StatusOK, string(body)
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader(responseBody)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		})}
	}
	t.Cleanup(func() {
		verifiedWeChatHistoryClientFactory = previousFactory
		verifiedWeChatHistorySlot = previousSlot
	})

	feed, err := fetchVerifiedWeChatHistory(context.Background(), source, policy)
	if err != nil {
		t.Fatal(err)
	}
	if calls != verifiedWeChatHistoryMaxAttempts || len(feed.Items) != 1 {
		t.Fatalf("calls=%d items=%d", calls, len(feed.Items))
	}
}

func TestVerifiedWeChatHistoryQueueWaitDoesNotConsumeSourceBudgetFlow(t *testing.T) {
	previousSlot := verifiedWeChatHistorySlot
	verifiedWeChatHistorySlot = make(chan struct{}, 1)
	t.Cleanup(func() { verifiedWeChatHistorySlot = previousSlot })

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- withVerifiedWeChatHistorySourceSlot(context.Background(), "wechat-founder-park", func(context.Context) error {
			close(firstStarted)
			<-releaseFirst
			return nil
		})
	}()
	<-firstStarted

	secondStarted := make(chan time.Duration, 1)
	secondResult := make(chan error, 1)
	go func() {
		secondResult <- withVerifiedWeChatHistorySourceSlot(context.Background(), "wechat-liuxiaopai", func(parent context.Context) error {
			return withFeedSyncSourceTimeout(parent, 100*time.Millisecond, func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok {
					return errors.New("missing source deadline")
				}
				secondStarted <- time.Until(deadline)
				return nil
			})
		})
	}()

	startedBeforeRelease := false
	select {
	case <-secondStarted:
		startedBeforeRelease = true
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseFirst)
	remaining := <-secondStarted
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	if startedBeforeRelease || remaining < 80*time.Millisecond {
		t.Fatalf("started_before_release=%v remaining=%s", startedBeforeRelease, remaining)
	}
}

func installVerifiedHistoryTestCollectors(t *testing.T, direct operationalFetchFunc, history verifiedWeChatHistoryCollectorFunc) {
	t.Helper()
	previousDirect := weChatSourceFetch
	previousHistory := verifiedWeChatHistoryCollector
	weChatSourceFetch = direct
	verifiedWeChatHistoryCollector = history
	t.Cleanup(func() {
		weChatSourceFetch = previousDirect
		verifiedWeChatHistoryCollector = previousHistory
	})
}

func TestVerifiedWeChatHistorySyncPersistsMissingRowsWithoutDowngradingExistingContentFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-founder-park")
	source := mustFeedSource(t, "wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := make([]string, 0, 30)
	for index := 0; index < 30; index++ {
		biz := "Mzg5NTc0MjgwMw=="
		if index%10 == 0 {
			biz = "MzY5ODQwMTkxNA=="
		}
		links = append(links, verifiedHistoryArticleURL(biz, index+1))
	}
	now := "2026-08-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('existing-history-row','legacy-guid','已有富内容',?,'Founder Park','wechat-founder-park','app-fetch','wechat','article','Founder Park',?,'必须保留的摘要','text','','','','',0,'rich-hash',?,'public_read','public_aggregator','',?)`, links[20], now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash) VALUES('existing-history-row','必须保留的正文','text',?,'ok','',?,?,?)`, now, links[20], now, "rich-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:existing-history-row',1,1,1,0.75,?)`, now); err != nil {
		t.Fatal(err)
	}
	directLinks := links[:10]
	historyCalls := 0
	installVerifiedHistoryTestCollectors(t,
		func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			return feedFetchResponse{Body: verifiedWeChatRSSFixture(policy.FeedTitle, directLinks...), FinalURL: raw}, nil
		},
		func(_ context.Context, got FeedSource, _ weChatVerifiedFeedPolicy) (fetchedFeed, error) {
			historyCalls++
			return parseVerifiedHistoryFixture(t, got, links), nil
		},
	)
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	var count, duplicateURLs int
	var cursor, status, summary, fullText string
	var read, starred, later int
	if err := db.QueryRow(`SELECT COUNT(*),COUNT(*)-COUNT(DISTINCT url) FROM news WHERE source_id=? AND deleted_at=''`, source.ID).Scan(&count, &duplicateURLs); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor, &status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT summary_text FROM news WHERE id='existing-history-row'`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT content_text FROM entry_full_texts WHERE entry_id='existing-history-row'`).Scan(&fullText); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT is_read,starred,read_later FROM item_preferences WHERE item_key='news:existing-history-row'`).Scan(&read, &starred, &later); err != nil {
		t.Fatal(err)
	}
	if count != 30 || duplicateURLs != 0 || cursor != verifiedWeChatHistoryCursor(policy) || status != "ok" || summary != "必须保留的摘要" || fullText != "必须保留的正文" || read != 1 || starred != 1 || later != 1 {
		t.Fatalf("count=%d dup=%d cursor=%q status=%q summary=%q full=%q prefs=%d/%d/%d", count, duplicateURLs, cursor, status, summary, fullText, read, starred, later)
	}
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	if historyCalls != 1 {
		t.Fatalf("completed history fetched %d times", historyCalls)
	}
}

func TestVerifiedWeChatHistoryContinuesAfterDirectNotModifiedFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-liuxiaopai")
	source := mustFeedSource(t, "wechat-liuxiaopai")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := []string{
		verifiedHistoryArticleURL("MzI1MTUxNzgxMA==", 1),
		verifiedHistoryArticleURL("MzI1MTUxNzgxMA==", 2),
		verifiedHistoryArticleURL("MzI1MTUxNzgxMA==", 3),
	}
	installVerifiedHistoryTestCollectors(t,
		func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			return feedFetchResponse{FinalURL: raw, ETag: "same", NotModified: true}, nil
		},
		func(_ context.Context, got FeedSource, _ weChatVerifiedFeedPolicy) (fetchedFeed, error) {
			return parseVerifiedHistoryFixture(t, got, links), nil
		},
	)
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	var cursor string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, source.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if count != 3 || cursor != verifiedWeChatHistoryCursor(policy) {
		t.Fatalf("count=%d cursor=%q", count, cursor)
	}
}

func TestVerifiedWeChatHistoryFailureKeepsLatestRSSAndRetriesFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-founder-park")
	source := mustFeedSource(t, "wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := []string{
		verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 1),
		verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", 2),
	}
	installVerifiedHistoryTestCollectors(t,
		func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			return feedFetchResponse{Body: verifiedWeChatRSSFixture(policy.FeedTitle, links...), FinalURL: raw}, nil
		},
		func(context.Context, FeedSource, weChatVerifiedFeedPolicy) (fetchedFeed, error) {
			return fetchedFeed{}, errors.New("temporary history timeout")
		},
	)
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	var cursor, status, lastError, next string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, source.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status,last_error,next_sync_at FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor, &status, &lastError, &next); err != nil {
		t.Fatal(err)
	}
	nextTime, _ := time.Parse(time.RFC3339, next)
	if count != 2 || cursor != "" || status != "partial" || !strings.Contains(lastError, "下次同步继续") || time.Until(nextTime) > verifiedWeChatHistoryRetryDelay+time.Minute {
		t.Fatalf("count=%d cursor=%q status=%q error=%q next=%q", count, cursor, status, lastError, next)
	}
}

func TestVerifiedWeChatHistoryIdentityDriftLateInResponseDoesNotPartiallyPersistFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-founder-park")
	source := mustFeedSource(t, "wechat-founder-park")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := make([]string, 0, 287)
	for index := 0; index < 287; index++ {
		links = append(links, verifiedHistoryArticleURL("Mzg5NTc0MjgwMw==", index+1))
	}
	installVerifiedHistoryTestCollectors(t,
		func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
			return feedFetchResponse{FinalURL: raw, ETag: "same", NotModified: true}, nil
		},
		func(_ context.Context, got FeedSource, gotPolicy weChatVerifiedFeedPolicy) (fetchedFeed, error) {
			body := verifiedHistoryFixture(got, gotPolicy, links, func(_ map[string]any, entries []map[string]any) {
				entries[199]["url"] = verifiedHistoryArticleURL("wrong", 200)
				entries[199]["guid"] = entries[199]["url"]
			})
			return parseVerifiedWeChatHistoryWithLimits(strings.NewReader(string(body)), got, gotPolicy, int64(len(body)+1), verifiedWeChatHistoryMaxItems)
		},
	)
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	var stored int
	var cursor string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, source.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if stored != 0 || cursor != "" || verifiedWeChatHistoryPending(source, policy) == false {
		t.Fatalf("stored=%d cursor=%q pending=%v", stored, cursor, verifiedWeChatHistoryPending(source, policy))
	}
}

func TestVerifiedWeChatHistoryRecoversWhenDirectFeedFailsFlow(t *testing.T) {
	db = openTestDB(t)
	enableLegacyPublicCollectorFixture(t, "wechat-liuxiaopai")
	source := mustFeedSource(t, "wechat-liuxiaopai")
	policy := weChatVerifiedDirectFeedPolicies[source.URL]
	links := []string{
		verifiedHistoryArticleURL("MzI1MTUxNzgxMA==", 1),
		verifiedHistoryArticleURL("MzI1MTUxNzgxMA==", 2),
	}
	installVerifiedHistoryTestCollectors(t,
		func(context.Context, string, string, string) (feedFetchResponse, error) {
			return feedFetchResponse{}, errors.New("direct feed timeout")
		},
		func(_ context.Context, got FeedSource, _ weChatVerifiedFeedPolicy) (fetchedFeed, error) {
			return parseVerifiedHistoryFixture(t, got, links), nil
		},
	)
	if err := syncLegacyPublicCollectorFixture(context.Background(), source.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	var cursor, status, lastError string
	if err := db.QueryRow(`SELECT COUNT(*) FROM news WHERE source_id=?`, source.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT backfill_cursor,status,last_error FROM feed_sources WHERE id=?`, source.ID).Scan(&cursor, &status, &lastError); err != nil {
		t.Fatal(err)
	}
	if count != 2 || cursor != verifiedWeChatHistoryCursor(policy) || status != "partial" || !strings.Contains(lastError, "已恢复") {
		t.Fatalf("count=%d cursor=%q status=%q error=%q", count, cursor, status, lastError)
	}
}

func TestVerifiedWeChatHistoryWindowMigrationRequeuesWithoutDeletingStateFlow(t *testing.T) {
	body, err := appFS.ReadFile("migrations/090_verified_wechat_history_window.sql")
	if err != nil {
		t.Fatal(err)
	}
	migration := string(body)
	for _, forbidden := range []string{"DELETE FROM news", "DELETE FROM item_preferences", "DELETE FROM entry_full_texts", "DROP TABLE"} {
		if strings.Contains(strings.ToUpper(migration), strings.ToUpper(forbidden)) {
			t.Fatalf("migration contains destructive statement %q", forbidden)
		}
	}
	conn := openUpgradeTestDB(t, "090_")
	if _, err := conn.Exec(`
UPDATE feed_sources SET backfill_cursor='legacy-cursor',next_sync_at='2026-08-23T00:00:00Z'
WHERE id IN ('wechat-aiera','wechat-ali-tech','wechat-chaping','wechat-founder-park','wechat-geekpark','wechat-jiqizhixin','wechat-liuxiaopai','wechat-paperweekly','wechat-qbitai');
UPDATE feed_sources SET etag='keep-etag',last_modified='keep-modified' WHERE id='wechat-founder-park';
UPDATE feed_subscriptions SET folder_id='folder-wechat-tech',title_override='Founder 自定义',pinned=1,muted=1,sort_order=37 WHERE source_id='wechat-founder-park';
INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,external_id,origin,content_hash,fetched_at)
VALUES('history-state','保留文章','https://mp.weixin.qq.com/s?__biz=Mzg5NTc0MjgwMw%3D%3D&mid=1&idx=1&sn=state','Founder Park','2026-08-20T00:00:00Z','保留摘要','text','',CURRENT_TIMESTAMP,'wechat-founder-park','history-state','app-fetch','state-hash',CURRENT_TIMESTAMP);
INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES('news:history-state',1,1,1,0.75,CURRENT_TIMESTAMP);
INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash) VALUES('history-state','保留全文','text',CURRENT_TIMESTAMP,'ok','','https://mp.weixin.qq.com/s?__biz=Mzg5NTc0MjgwMw%3D%3D&mid=1&idx=1&sn=state',CURRENT_TIMESTAMP,'state-hash');
INSERT INTO article_ai_overviews(entry_id,content_hash,status,summary,key_points_json,impact,generated_at,error_code,prompt_version) VALUES('history-state','state-hash','ready','保留概览','["要点"]','保留影响',CURRENT_TIMESTAMP,'','article-overview-v1');
INSERT INTO reader_digest_receipts(content_hash,source_id,entry_id,processed_at,outcome) VALUES('state-hash','wechat-founder-park','history-state',CURRENT_TIMESTAMP,'drafted');
INSERT INTO focus_spaces(id,owner_id,profile_id,title,goal,created_at,updated_at) VALUES('focus-history','owner','profile','历史关注','保留状态',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO focus_space_sources(focus_id,owner_id,profile_id,source_kind,source_id,enabled,revision,created_at,updated_at) VALUES('focus-history','owner','profile','rss','wechat-founder-park',1,3,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`); err != nil {
		t.Fatal(err)
	}
	applyMigrationFile(t, conn, "090_verified_wechat_history_window.sql")
	for _, sourceID := range []string{
		"wechat-aiera",
		"wechat-ali-tech",
		"wechat-chaping",
		"wechat-founder-park",
		"wechat-geekpark",
		"wechat-jiqizhixin",
		"wechat-liuxiaopai",
		"wechat-paperweekly",
		"wechat-qbitai",
	} {
		var cursor, next string
		var backfillDays, retentionDays, retentionLimit int
		if err := conn.QueryRow(`SELECT backfill_cursor,next_sync_at,backfill_days,retention_days,retention_limit FROM feed_sources WHERE id=?`, sourceID).Scan(&cursor, &next, &backfillDays, &retentionDays, &retentionLimit); err != nil {
			t.Fatal(err)
		}
		if cursor != "" || next != "" || backfillDays != 3650 || retentionDays != 3650 || retentionLimit != 1000 {
			t.Fatalf("source=%s cursor=%q next=%q backfill=%d retention=%d/%d", sourceID, cursor, next, backfillDays, retentionDays, retentionLimit)
		}
	}
	var etag, modified, folder, title, summary, fullText string
	var pinned, muted, sortOrder, sidecars int
	if err := conn.QueryRow(`SELECT etag,last_modified FROM feed_sources WHERE id='wechat-founder-park'`).Scan(&etag, &modified); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT folder_id,title_override,pinned,muted,sort_order FROM feed_subscriptions WHERE source_id='wechat-founder-park'`).Scan(&folder, &title, &pinned, &muted, &sortOrder); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT summary_text FROM news WHERE id='history-state'`).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT content_text FROM entry_full_texts WHERE entry_id='history-state'`).Scan(&fullText); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(`SELECT
(SELECT COUNT(*) FROM item_preferences WHERE item_key='news:history-state' AND is_read=1 AND starred=1 AND read_later=1) +
(SELECT COUNT(*) FROM article_ai_overviews WHERE entry_id='history-state' AND status='ready' AND summary='保留概览') +
(SELECT COUNT(*) FROM reader_digest_receipts WHERE entry_id='history-state' AND outcome='drafted') +
(SELECT COUNT(*) FROM focus_space_sources WHERE focus_id='focus-history' AND source_id='wechat-founder-park' AND enabled=1 AND revision=3)`).Scan(&sidecars); err != nil {
		t.Fatal(err)
	}
	if etag != "keep-etag" || modified != "keep-modified" || folder != "folder-wechat-tech" || title != "Founder 自定义" || pinned != 1 || muted != 1 || sortOrder != 37 || summary != "保留摘要" || fullText != "保留全文" || sidecars != 4 {
		t.Fatalf("validators=%q/%q subscription=%q/%q/%d/%d/%d summary=%q full=%q sidecars=%d", etag, modified, folder, title, pinned, muted, sortOrder, summary, fullText, sidecars)
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Source Gateway V1 §8.4：正文按需 fulltext + 图片走 local-server media 路由。
// 这些测试全部使用 loopback mock 网关，不触网。

func seedGatewayEntry(t *testing.T, sourceID, externalID, gatewayID, url, hash string) string {
	t.Helper()
	id := stableObjectID("entry", sourceID+"|"+externalID)
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at,gateway_entry_id)
VALUES(?,?,?,?,'云端来源',?,'source-gateway','text','article','','2026-08-16T10:00:00Z','条目摘要','text','','https://img.example.com/lead.jpg','','',0,?, '2026-08-16T10:00:00Z','public_read','official','','2026-08-16T10:00:00Z',?)`,
		id, externalID, "云端文章 "+externalID, url, sourceID, hash, gatewayID); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGatewayEntryFulltextOnDemandAndCachedFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	entryID := seedGatewayEntry(t, "youtube-anthropic", "v1", "sg_full_1", "https://www.youtube.com/watch?v=v1", "gw-hash-1")
	mock.fulltexts["sg_full_1"] = sourceGatewayFulltext{
		ContentHTML: `<html><body><article><p>云端正文第一段，包含足够长的可读内容。</p><p>第二段。</p><img src="https://img.example.com/a.jpg"/><img src="https://img.example.com/b.jpg"/></article></body></html>`,
		Images:      []string{"https://img.example.com/a.jpg", "https://img.example.com/b.jpg"},
	}

	first := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"status":"ok"`) || !strings.Contains(first.Body.String(), "云端正文第一段") {
		t.Fatalf("fulltext = %d %s", first.Code, first.Body.String())
	}
	if !strings.Contains(first.Body.String(), `"image_count":2`) {
		t.Fatalf("gateway images must define the media index space: %s", first.Body.String())
	}
	if mock.entryCallCount("/fulltext") != 1 {
		t.Fatalf("fulltext calls = %d, want exactly 1", mock.entryCallCount("/fulltext"))
	}

	// 正文缓存写入现有 entry_full_texts；再次打开走本地缓存，不再请求云端。
	second := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"cached":true`) {
		t.Fatalf("cached read = %d %s", second.Code, second.Body.String())
	}
	if mock.entryCallCount("/fulltext") != 1 {
		t.Fatalf("cached open must not refetch: %d calls", mock.entryCallCount("/fulltext"))
	}
	var status, text string
	if err := db.QueryRow(`SELECT status,content_text FROM entry_full_texts WHERE entry_id=?`, entryID).Scan(&status, &text); err != nil {
		t.Fatal(err)
	}
	if status != "ok" || !strings.Contains(text, "云端正文第一段") {
		t.Fatalf("persisted full text = %q %q", status, text)
	}
}

func TestGatewayWechatChallengeRecoversVerifiedFeedRichContentFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	articleURL := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247914838&idx=1&sn=07603c6db9fa5b097ecfc3c48461df63"
	entryID := seedGatewayEntry(t, "wechat-qbitai", "wechat:MzIzNjc1NzUzMw==:2247914838:1", "sg_qbit_rich", articleURL, "qbit-rich-hash")
	secondURL := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247914838&idx=2&sn=d361f7666335e4a0c96b3212c8f274ed"
	secondEntryID := seedGatewayEntry(t, "wechat-qbitai", "gateway-opaque-second", "sg_qbit_second", secondURL, "qbit-second-hash")
	mock.fulltexts["sg_qbit_rich"] = sourceGatewayFulltext{
		ContentHTML: `<h2>环境异常</h2><p>当前环境异常，完成验证后即可继续访问。</p>`,
		ContentText: "环境异常 当前环境异常，完成验证后即可继续访问。",
	}
	imageURL := "https://wechat2rss.xlab.app/img-proxy/?k=cover&u=https%3A%2F%2Fmmbiz.qpic.cn%2Fmmbiz_png%2Fcover.jpg"
	secondImageURL := "https://wechat2rss.xlab.app/img-proxy/?k=second&u=https%3A%2F%2Fmmbiz.qpic.cn%2Fmmbiz_png%2Fsecond.jpg"
	feedBody := `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>量子位</title><item>` +
		`<guid>wechat-rich</guid><title>真实公众号文章</title><link>https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&amp;mid=2247914838&amp;idx=1&amp;sn=07603c6db9fa5b097ecfc3c48461df63</link>` +
		`<content:encoded><![CDATA[<p>正文第一段</p><h2>真实章节</h2><p><strong>重点正文</strong></p><p><img src="` + imageURL + `"/></p>]]></content:encoded></item>` +
		`<item><guid>wechat-second</guid><title>相邻文章</title><link>https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&amp;mid=2247914838&amp;idx=2&amp;sn=d361f7666335e4a0c96b3212c8f274ed</link>` +
		`<content:encoded><![CDATA[<p>相邻正文</p><p><img src="` + secondImageURL + `"/></p>]]></content:encoded></item></channel></rss>`
	previousFetch := entryContentFetchVerifiedWeChatFeed
	t.Cleanup(func() { entryContentFetchVerifiedWeChatFeed = previousFetch })
	entryContentFetchVerifiedWeChatFeed = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		if raw != weChatVerifiedDirectFeedURLs["wechat-qbitai"] {
			t.Fatalf("unexpected verified feed URL %q", raw)
		}
		return feedFetchResponse{Body: []byte(feedBody), FinalURL: raw, ContentType: "application/rss+xml"}, nil
	}

	content := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), `"content_format":"html"`) ||
		!strings.Contains(content.Body.String(), "真实章节") || !strings.Contains(content.Body.String(), "重点正文") ||
		!strings.Contains(content.Body.String(), `"image_count":1`) || strings.Contains(content.Body.String(), "环境异常") {
		t.Fatalf("verified rich fallback = %d %s", content.Code, content.Body.String())
	}

	previousMediaFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousMediaFactory })
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != imageURL && req.URL.String() != secondImageURL || req.Header.Get("Referer") != "https://mp.weixin.qq.com/" {
				t.Fatalf("unexpected recovered image request %s headers=%v", req.URL, req.Header)
			}
			header := make(http.Header)
			header.Set("Content-Type", "image/jpeg")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("JPEG")), ContentLength: 4}, nil
		})}
	}
	image := callJSON(t, entryContentImageHandler, http.MethodGet, "/api/entries/content/image?id="+entryID+"&index=0", "")
	if image.Code != http.StatusOK || image.Body.String() != "JPEG" {
		t.Fatalf("recovered body image = %d %q", image.Code, image.Body.String())
	}
	thumbnail := callJSON(t, mediaProxyHandler, http.MethodGet, "/api/media?id="+entryID+"&kind=thumbnail", "")
	if thumbnail.Code != http.StatusOK || thumbnail.Body.String() != "JPEG" {
		t.Fatalf("recovered thumbnail = %d %q", thumbnail.Code, thumbnail.Body.String())
	}
	secondThumbnail := callJSON(t, mediaProxyHandler, http.MethodGet, "/api/media?id="+secondEntryID+"&kind=thumbnail", "")
	if secondThumbnail.Code != http.StatusOK || secondThumbnail.Body.String() != "JPEG" {
		t.Fatalf("neighbour thumbnail = %d %q", secondThumbnail.Code, secondThumbnail.Body.String())
	}
}

func TestGatewayWechatBlockedSourceRecoversVerifiedFeedRichContentFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	mock.failFulltext = true
	articleURL := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247914838&idx=1&sn=07603c6db9fa5b097ecfc3c48461df63"
	entryID := seedGatewayEntry(t, "wechat-qbitai", "wechat:MzIzNjc1NzUzMw==:2247914838:1", "sg_qbit_blocked", articleURL, "qbit-blocked-hash")
	if _, err := db.Exec(`UPDATE news SET summary_text='环境异常 当前环境异常，完成验证后即可继续访问。' WHERE id=?`, entryID); err != nil {
		t.Fatal(err)
	}
	secondURL := "https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&mid=2247914838&idx=2&sn=d361f7666335e4a0c96b3212c8f274ed"
	secondEntryID := seedGatewayEntry(t, "wechat-qbitai", "gateway-opaque-second", "sg_qbit_second", secondURL, "qbit-second-hash")
	imageURL := "https://wechat2rss.xlab.app/img-proxy/?k=cover&u=https%3A%2F%2Fmmbiz.qpic.cn%2Fmmbiz_png%2Fcover.jpg"
	secondImageURL := "https://wechat2rss.xlab.app/img-proxy/?k=second&u=https%3A%2F%2Fmmbiz.qpic.cn%2Fmmbiz_png%2Fsecond.jpg"
	feedBody := `<rss version="1.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>量子位</title><item>` +
		`<guid>wechat-rich</guid><title>真实公众号文章</title><link>https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&amp;mid=2247914838&amp;idx=1&amp;sn=07603c6db9fa5b097ecfc3c48461df63</link>` +
		`<content:encoded><![CDATA[<p>网关不可用时恢复的真实正文</p><p><img src="` + imageURL + `"/></p>]]></content:encoded></item>` +
		`<item><guid>wechat-second</guid><title>相邻文章</title><link>https://mp.weixin.qq.com/s?__biz=MzIzNjc1NzUzMw==&amp;mid=2247914838&amp;idx=2&amp;sn=d361f7666335e4a0c96b3212c8f274ed</link>` +
		`<content:encoded><![CDATA[<p>相邻正文</p><p><img src="` + secondImageURL + `"/></p>]]></content:encoded></item></channel></rss>`
	previousFetch := entryContentFetchVerifiedWeChatFeed
	t.Cleanup(func() { entryContentFetchVerifiedWeChatFeed = previousFetch })
	entryContentFetchVerifiedWeChatFeed = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		if raw != weChatVerifiedDirectFeedURLs["wechat-qbitai"] {
			t.Fatalf("unexpected verified feed URL %q", raw)
		}
		return feedFetchResponse{Body: []byte(feedBody), FinalURL: raw, ContentType: "application/rss+xml"}, nil
	}

	content := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if content.Code != http.StatusOK || !strings.Contains(content.Body.String(), `"status":"stale"`) ||
		!strings.Contains(content.Body.String(), "网关不可用时恢复的真实正文") ||
		!strings.Contains(content.Body.String(), "显示可信订阅内容") {
		t.Fatalf("blocked gateway verified fallback = %d %s", content.Code, content.Body.String())
	}
	var targetSummary string
	if err := db.QueryRow(`SELECT summary_text FROM news WHERE id=?`, entryID).Scan(&targetSummary); err != nil {
		t.Fatal(err)
	}
	if containsBlockedArticleMarker(targetSummary) || !strings.Contains(targetSummary, "网关不可用时恢复的真实正文") {
		t.Fatalf("opened entry summary was not repaired: %q", targetSummary)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := upsertGatewayNewsTx(tx, FeedSource{ID: "wechat-qbitai", Name: "量子位", Kind: "wechat"}, sourceGatewayEntry{
		ID: "sg_qbit_blocked", ExternalID: "wechat:MzIzNjc1NzUzMw==:2247914838:1", URL: articleURL,
		Title: "真实公众号文章", SummaryText: "环境异常 当前环境异常，完成验证后即可继续访问。",
		ContentFormat: "text", ContentHash: "qbit-blocked-new-hash", ThumbnailURL: "https://img.example.com/challenge.jpg",
	}, nowUTC())
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if !changed {
		_ = tx.Rollback()
		t.Fatal("changed gateway hash must be recorded")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT summary_text FROM news WHERE id=?`, entryID).Scan(&targetSummary); err != nil {
		t.Fatal(err)
	}
	if containsBlockedArticleMarker(targetSummary) || !strings.Contains(targetSummary, "网关不可用时恢复的真实正文") {
		t.Fatalf("later gateway sync overwrote recovered summary: %q", targetSummary)
	}
	var currentThumbnail, neighbourThumbnail, recoveredSummary string
	if err := db.QueryRow(`SELECT thumbnail_url,summary_text FROM news WHERE id=?`, entryID).Scan(&currentThumbnail, &recoveredSummary); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT thumbnail_url FROM news WHERE id=?`, secondEntryID).Scan(&neighbourThumbnail); err != nil {
		t.Fatal(err)
	}
	if currentThumbnail != imageURL || neighbourThumbnail != secondImageURL {
		t.Fatalf("verified feed did not repair thumbnails: current=%q neighbour=%q", currentThumbnail, neighbourThumbnail)
	}
	if containsBlockedArticleMarker(recoveredSummary) || !strings.Contains(recoveredSummary, "网关不可用时恢复的真实正文") {
		t.Fatalf("verified feed did not repair blocked timeline summary: %q", recoveredSummary)
	}
}

func TestGatewayEntryFulltextFailureKeepsSummaryAndRetriesFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	entryID := seedGatewayEntry(t, "youtube-anthropic", "v2", "sg_full_2", "https://www.youtube.com/watch?v=v2", "gw-hash-2")
	mock.failFulltext = true

	failed := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if failed.Code != http.StatusOK || !strings.Contains(failed.Body.String(), `"status":"summary"`) ||
		!strings.Contains(failed.Body.String(), "条目摘要") || !strings.Contains(failed.Body.String(), "云端来源暂不可用，显示上次内容") {
		t.Fatalf("failure fallback = %d %s", failed.Code, failed.Body.String())
	}
	var cacheStatus string
	if err := db.QueryRow(`SELECT status FROM entry_full_texts WHERE entry_id=?`, entryID).Scan(&cacheStatus); err != nil {
		t.Fatal(err)
	}
	if cacheStatus != "unavailable" {
		t.Fatalf("failure must be recorded for the retry affordance, got %q", cacheStatus)
	}

	mock.failFulltext = false
	mock.fulltexts["sg_full_2"] = sourceGatewayFulltext{ContentText: "恢复后的云端正文"}
	retried := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID+"&refresh=1", "")
	if retried.Code != http.StatusOK || !strings.Contains(retried.Body.String(), "恢复后的云端正文") {
		t.Fatalf("retry = %d %s", retried.Code, retried.Body.String())
	}
}

func TestGatewayEntryFulltextAuthFailureFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	t.Setenv("ZAPP_SOURCE_GATEWAY_TOKEN", "wrong-token")
	entryID := seedGatewayEntry(t, "youtube-anthropic", "v3", "sg_full_3", "https://www.youtube.com/watch?v=v3", "gw-hash-3")
	result := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "重新登录") {
		t.Fatalf("auth failure must map to a re-login notice: %d %s", result.Code, result.Body.String())
	}
}

func TestGatewayEntryImagesUseLocalMediaRouteFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	entryID := seedGatewayEntry(t, "youtube-anthropic", "v4", "sg_img_1", "https://www.youtube.com/watch?v=v4", "gw-hash-4")
	mock.fulltexts["sg_img_1"] = sourceGatewayFulltext{
		ContentText: "正文",
		Images:      []string{"https://i.ytimg.com/vi/v4/a.jpg", "https://i.ytimg.com/vi/v4/b.jpg"},
	}
	mock.media["sg_img_1/0"] = []byte("\xff\xd8\xff-image-a")
	mock.media["sg_img_1/1"] = []byte("\xff\xd8\xff-image-b")

	content := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if content.Code != http.StatusOK {
		t.Fatalf("content = %d %s", content.Code, content.Body.String())
	}
	image := callJSON(t, entryContentImageHandler, http.MethodGet, "/api/entries/content/image?id="+entryID+"&index=1", "")
	if image.Code != http.StatusOK || image.Body.String() != "\xff\xd8\xff-image-b" {
		t.Fatalf("image = %d %q", image.Code, image.Body.String())
	}
	if image.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image content type = %q", image.Header().Get("Content-Type"))
	}
	// 索引空间只来自来源记录：越界索引是 404，不是任意 URL 代理。
	missing := callJSON(t, entryContentImageHandler, http.MethodGet, "/api/entries/content/image?id="+entryID+"&index=5", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("out-of-record image index = %d", missing.Code)
	}
	// 图片失败只影响对应图片：第二张 404 时正文与第一张仍可用。
	delete(mock.media, "sg_img_1/1")
	broken := callJSON(t, entryContentImageHandler, http.MethodGet, "/api/entries/content/image?id="+entryID+"&index=1", "")
	if broken.Code == http.StatusOK {
		t.Fatal("removed upstream image must not silently succeed")
	}
	again := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id="+entryID, "")
	if again.Code != http.StatusOK || !strings.Contains(again.Body.String(), "正文") {
		t.Fatalf("article content must survive an image failure: %d %s", again.Code, again.Body.String())
	}
}

func TestGatewayThumbnailUsesLocalMediaRouteFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	entryID := seedGatewayEntry(t, "youtube-anthropic", "v5", "sg_thumb_1", "https://www.youtube.com/watch?v=v5", "gw-hash-5")
	mock.media["sg_thumb_1/0"] = []byte("\xff\xd8\xff-thumb")

	thumb := callJSON(t, mediaProxyHandler, http.MethodGet, "/api/media?id="+entryID+"&kind=thumbnail", "")
	if thumb.Code != http.StatusOK || thumb.Body.String() != "\xff\xd8\xff-thumb" {
		t.Fatalf("thumbnail = %d %q", thumb.Code, thumb.Body.String())
	}
	// 不直接加载第三方缩略图 URL：唯一出站请求是对本地网关 media 路由。
	for _, request := range mock.requests {
		if !strings.Contains(request, "/source-gateway/") {
			t.Fatalf("unexpected non-gateway request: %s", request)
		}
	}
	if mock.entryCallCount("/media/0") != 1 {
		t.Fatalf("thumbnail must map to media index 0: %#v", mock.requests)
	}
}

func TestReaderTailPullFetchesOlderCloudPageFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{
		{gatewayTestEntry("youtube-anthropic", "older-1", "更早的文章", "https://www.youtube.com/watch?v=older1", "old-hash", "2026-07-01T00:00:00Z")},
	}
	mock.configure(t)
	// 本地窗口里只剩一条较新的文章；云端还有更多历史。
	seedGatewayEntry(t, "youtube-anthropic", "new-1", "sg_tail_1", "https://www.youtube.com/watch?v=new1", "new-hash")
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_cursor='cursor-0', gateway_has_more=1 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}

	page := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=1", "")
	if page.Code != http.StatusOK {
		t.Fatalf("page = %d %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	var payload struct {
		Entries []struct {
			Title string `json:"title"`
		} `json:"entries"`
		NextToken string `json:"next_cursor_token"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Entries) != 1 || payload.NextToken == "" {
		t.Fatalf("first page must stay bounded and keep a cursor: %s", body)
	}
	// 滚动到下一页：被淘汰出本地窗口的云端历史条目经尾部分页重新可见。
	page2 := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=1&cursor="+url.QueryEscape(payload.NextToken), "")
	if page2.Code != http.StatusOK || !strings.Contains(page2.Body.String(), "更早的文章") {
		t.Fatalf("evicted cloud history must be re-fetched on scroll: %d %s", page2.Code, page2.Body.String())
	}
	var cursor string
	var hasMore int
	if err := db.QueryRow(`SELECT gateway_cursor,gateway_has_more FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&cursor, &hasMore); err != nil {
		t.Fatal(err)
	}
	if cursor != "" || hasMore != 0 {
		t.Fatalf("single-page cloud history must terminate: cursor=%q hasMore=%d", cursor, hasMore)
	}

	// 云端到底后再次滚动不再发请求，也不把空页解释为错误。
	before := mock.entryCallCount("/entries")
	last := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=50", "")
	if last.Code != http.StatusOK || !strings.Contains(last.Body.String(), "更早的文章") {
		t.Fatalf("final local page = %d %s", last.Code, last.Body.String())
	}
	if mock.entryCallCount("/entries") != before {
		t.Fatal("no further cloud pull once gateway_has_more is exhausted")
	}
}

func TestReaderInitialGatewayHeadHydratesEmptyCacheFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{{
		gatewayTestEntry("youtube-anthropic", "initial-1", "首次加载的文章", "https://www.youtube.com/watch?v=initial1", "initial-hash", "2026-08-27T10:00:00Z"),
	}}
	mock.configure(t)
	// A fresh/partially upgraded install can have an empty local cache while
	// gateway_has_more still contains its zero value. Opening the source must
	// issue one bounded head request instead of treating zero as exhaustion.
	if _, err := db.Exec(`DELETE FROM news WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET status='unavailable',records_seen=0,records_used=0,last_sync_at='',last_error='',next_sync_at='',gateway_status='initializing',gateway_cursor='',gateway_has_more=0 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	mock.mu.Lock()
	mock.requests = nil
	mock.mu.Unlock()

	first := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "首次加载的文章") {
		t.Fatalf("initial gateway head status=%d body=%s", first.Code, first.Body.String())
	}
	if calls := mock.entryCallCount("/entries"); calls != 1 {
		t.Fatalf("initial empty cache must issue one head request, got %d", calls)
	}
	var status, gatewayStatus, cursor, lastSync string
	var hasMore, entryCount int
	if err := db.QueryRow(`SELECT status,gateway_status,gateway_cursor,gateway_has_more,last_sync_at,entry_count FROM (
		SELECT f.status,f.gateway_status,f.gateway_cursor,f.gateway_has_more,f.last_sync_at,
		       (SELECT COUNT(*) FROM news n WHERE n.source_id=f.id AND n.deleted_at='') AS entry_count
		FROM feed_sources f WHERE f.id='youtube-anthropic')`).Scan(&status, &gatewayStatus, &cursor, &hasMore, &lastSync, &entryCount); err != nil {
		t.Fatal(err)
	}
	if status != "ok" || gatewayStatus != "healthy" || cursor != "" || hasMore != 0 || lastSync == "" || entryCount != 1 {
		t.Fatalf("initial head state status=%q gateway=%q cursor=%q hasMore=%d lastSync=%q entries=%d", status, gatewayStatus, cursor, hasMore, lastSync, entryCount)
	}

	// Once the successful response records OK/healthy + last_sync_at, a second
	// read is served from SQLite and must not re-fetch an exhausted empty walk.
	mock.mu.Lock()
	mock.requests = nil
	mock.mu.Unlock()
	second := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "首次加载的文章") {
		t.Fatalf("cached follow-up status=%d body=%s", second.Code, second.Body.String())
	}
	if calls := mock.entryCallCount("/entries"); calls != 0 {
		t.Fatalf("successful exhausted head must not repeat, got %d requests", calls)
	}
}

func TestReaderInitialGatewayHeadRepairsMissingSubscriptionBeforeReadFlow(t *testing.T) {
	db = openTestDB(t)
	if err := reconcileSourceGatewaySchema(db); err != nil {
		t.Fatal(err)
	}
	// Simulate an install where the source catalog row has already been
	// persisted but the local subscription projection has not.  The reader
	// must repair that metadata in the same source-scoped request; otherwise a
	// first click would see an empty SQLite page and never start hydration.
	if _, err := db.Exec(`DELETE FROM feed_subscriptions WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM source_gateway_subscription_outbox`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM news WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET status='unavailable',records_seen=0,records_used=0,last_sync_at='',last_error='',next_sync_at='',gateway_status='initializing',gateway_cursor='',gateway_has_more=0 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.catalog = []sourceGatewayCatalogSource{{ID: "youtube-anthropic", Name: "Anthropic", Group: "youtube", Status: "healthy"}}
	mock.subs = []sourceGatewaySubscription{{SourceID: "youtube-anthropic", Enabled: true, Status: "ready"}}
	mock.pages["youtube-anthropic"] = [][]sourceGatewayEntry{{
		gatewayTestEntry("youtube-anthropic", "projection-repair-1", "修复投影后首篇文章", "https://www.youtube.com/watch?v=projection-repair-1", "projection-repair-hash", "2026-08-27T11:00:00Z"),
	}}
	mock.configure(t)

	result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "修复投影后首篇文章") {
		t.Fatalf("source-scoped read after projection repair = %d %s", result.Code, result.Body.String())
	}
	var enabled int
	if err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id='youtube-anthropic'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("source-scoped read did not materialize subscription, enabled=%d", enabled)
	}
	mock.mu.Lock()
	requests := append([]string(nil), mock.requests...)
	mock.mu.Unlock()
	if len(requests) != 3 || !strings.Contains(requests[0], "/subscriptions") || !strings.Contains(requests[1], "/catalog") || !strings.Contains(requests[2], "/entries") {
		t.Fatalf("source-scoped read request order = %#v, want metadata repair before entries", requests)
	}
}

func TestReaderEmptyGatewayFailureIsRetryableFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`DELETE FROM news WHERE source_id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feed_sources SET status='unavailable',records_seen=0,records_used=0,last_sync_at='',last_error='',next_sync_at='',gateway_status='initializing',gateway_cursor='',gateway_has_more=1 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.failWith = http.StatusServiceUnavailable
	mock.configure(t)

	result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if result.Code != http.StatusServiceUnavailable || !strings.Contains(result.Body.String(), "来源正在准备或暂时不可用") {
		t.Fatalf("empty gateway failure = %d %s, want retryable source error", result.Code, result.Body.String())
	}
	if calls := mock.entryCallCount("/entries"); calls != 1 {
		t.Fatalf("empty gateway failure must make one bounded read, got %d", calls)
	}
	// The retry cooldown suppresses another upstream request, but must not
	// downgrade the visible result to a successful empty page.
	retry := callJSON(t, entryHandler, http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=10", "")
	if retry.Code != http.StatusServiceUnavailable || !strings.Contains(retry.Body.String(), "来源正在准备或暂时不可用") {
		t.Fatalf("empty gateway cooldown = %d %s, want retryable source error", retry.Code, retry.Body.String())
	}
	if calls := mock.entryCallCount("/entries"); calls != 1 {
		t.Fatalf("cooldown read must not hammer gateway, got %d calls", calls)
	}
}

func TestReaderTailPullTimeoutReturnsCacheAndEntersCooldownFlow(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.blockEntries = true
	mock.configure(t)
	seedGatewayEntry(t, "youtube-anthropic", "cached", "sg_cached", "https://www.youtube.com/watch?v=cached", "cached-hash")
	if _, err := db.Exec(`UPDATE feed_sources SET gateway_cursor='cursor-0',gateway_has_more=1 WHERE id='youtube-anthropic'`); err != nil {
		t.Fatal(err)
	}

	request := func() (*httptest.ResponseRecorder, time.Duration) {
		result := httptest.NewRecorder()
		started := time.Now()
		entryHandler(result, httptest.NewRequest(http.MethodGet, "/api/entries?source_id=youtube-anthropic&limit=50", nil))
		return result, time.Since(started)
	}
	first, firstElapsed := request()
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "云端文章 cached") {
		t.Fatalf("timeout must preserve cached entries: status=%d body=%s", first.Code, first.Body.String())
	}
	if firstElapsed < sourceGatewayReaderHistoryTimeout/2 || firstElapsed > sourceGatewayReaderHistoryTimeout+time.Second {
		t.Fatalf("reader timeout elapsed=%s, configured=%s", firstElapsed, sourceGatewayReaderHistoryTimeout)
	}
	if calls := mock.entryCallCount("/entries"); calls != 1 {
		t.Fatalf("first tail pull requests=%d, want 1", calls)
	}

	second, secondElapsed := request()
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "云端文章 cached") {
		t.Fatalf("cooldown must preserve cached entries: status=%d body=%s", second.Code, second.Body.String())
	}
	if secondElapsed > 250*time.Millisecond {
		t.Fatalf("cooldown request took %s", secondElapsed)
	}
	if calls := mock.entryCallCount("/entries"); calls != 1 {
		t.Fatalf("cooldown created another transport request: %d", calls)
	}
}

func TestLegacyGatewayManagedEntryWithoutGatewayIDFailsClosedFlow(t *testing.T) {
	db = openTestDB(t)
	var mode string
	if err := db.QueryRow(`SELECT collection_mode FROM feed_sources WHERE id='youtube-anthropic'`).Scan(&mode); err != nil || mode != "source_gateway" {
		t.Fatalf("gateway source fixture mode=%q err=%v", mode, err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('legacy-gateway-entry','legacy-gateway-external','旧云端文章','https://example.com/legacy-article','Anthropic','youtube-anthropic','source-gateway','rss','article','','2026-08-16T10:00:00Z','旧云端摘要','text','','https://cdn.example.com/thumb.jpg','','',0,'legacy-gateway-hash','2026-08-16T10:00:00Z','public_read','official','','2026-08-16T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := persistEntryContent("legacy-gateway-entry", "https://example.com/legacy-article", "legacy-gateway-hash", extractedArticle{
		Text:      "上次成功保存的完整正文",
		Format:    "text",
		ImageURLs: []string{"https://cdn.example.com/body.jpg"},
	}, "2026-08-16T10:00:00Z"); err != nil {
		t.Fatal(err)
	}

	previousFetch := entryContentFetchPublicURL
	publicFetches := 0
	entryContentFetchPublicURL = func(context.Context, string, string, string) (feedFetchResponse, error) {
		publicFetches++
		return feedFetchResponse{}, errors.New("public fetch must stay closed")
	}
	t.Cleanup(func() { entryContentFetchPublicURL = previousFetch })
	previousMediaFactory := mediaHTTPClientFactory
	mediaFetches := 0
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
			mediaFetches++
			return nil, errors.New("public media fetch must stay closed")
		})}
	}
	t.Cleanup(func() { mediaHTTPClientFactory = previousMediaFactory })

	refreshed := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id=legacy-gateway-entry&refresh=1", "")
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), `"status":"stale"`) || !strings.Contains(refreshed.Body.String(), "上次成功保存的完整正文") {
		t.Fatalf("cached refresh status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
	image := callJSON(t, entryContentImageHandler, http.MethodGet, "/api/entries/content/image?id=legacy-gateway-entry&index=0", "")
	if image.Code != http.StatusBadGateway {
		t.Fatalf("unmapped gateway image status=%d body=%s", image.Code, image.Body.String())
	}
	if _, err := db.Exec(`DELETE FROM entry_full_texts WHERE entry_id='legacy-gateway-entry'`); err != nil {
		t.Fatal(err)
	}
	summary := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id=legacy-gateway-entry", "")
	if summary.Code != http.StatusOK || !strings.Contains(summary.Body.String(), `"status":"summary"`) || !strings.Contains(summary.Body.String(), "旧云端摘要") {
		t.Fatalf("summary fallback status=%d body=%s", summary.Code, summary.Body.String())
	}
	if publicFetches != 0 || mediaFetches != 0 {
		t.Fatalf("legacy gateway row escaped to public network: content=%d media=%d", publicFetches, mediaFetches)
	}
}

// 非网关条目（用户自添加来源、Chat 沉淀的单篇文章）保持原有本地抓取路径。
func TestNonGatewayEntryKeepsLocalFulltextPathFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES('chat-saved-1','chat-1','Chat 沉淀文章','https://example.com/article','Chat','', 'agent','text','article','','2026-08-16T10:00:00Z','本地摘要','text','','','','',0,'chat-hash','2026-08-16T10:00:00Z','public_read','official','','2026-08-16T10:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.configure(t)
	previousFetch := entryContentFetchPublicURL
	publicFetches := 0
	entryContentFetchPublicURL = func(_ context.Context, raw, _, _ string) (feedFetchResponse, error) {
		publicFetches++
		if raw != "https://example.com/article" {
			t.Fatalf("single-article fetch URL=%q", raw)
		}
		return feedFetchResponse{Body: []byte(`<article><p>单篇公开正文</p></article>`), ContentType: "text/html", FinalURL: raw}, nil
	}
	t.Cleanup(func() { entryContentFetchPublicURL = previousFetch })
	// 没有 gateway_entry_id：即使网关可用，也不许把该条目路由到云端。
	result := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id=chat-saved-1", "")
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "单篇公开正文") || publicFetches != 1 {
		t.Fatalf("local path status=%d public_fetches=%d body=%s", result.Code, publicFetches, result.Body.String())
	}
	if mock.entryCallCount("/fulltext") != 0 {
		t.Fatal("non-gateway entry must not call the gateway fulltext route")
	}
}

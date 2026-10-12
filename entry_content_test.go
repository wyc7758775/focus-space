package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractEntryFullTextBoundsNoiseAndEntities(t *testing.T) {
	text := extractEntryFullText([]byte(`<html><head><script>alert(1)</script></head><body><article><h1>标题</h1><p>第一&nbsp;段</p><p>第二段</p></article><style>.x{}</style></body></html>`), "text/html")
	if strings.Contains(text, "alert") || strings.Contains(text, ".x") || !strings.Contains(text, "第一 段") || !strings.Contains(text, "第二段") {
		t.Fatalf("unexpected extracted text: %q", text)
	}
	if got := len([]rune(extractEntryFullText([]byte(strings.Repeat("x", maxEntryFullTextRunes+100)), "text/plain"))); got != maxEntryFullTextRunes {
		t.Fatalf("text length = %d, want %d", got, maxEntryFullTextRunes)
	}
}

func TestExtractEntryArticleKeepsStructureAndImagesWithoutPageChrome(t *testing.T) {
	body := []byte(`<html><head><meta property="og:image" content="/cover.jpg"></head><body>
<nav>首页 产品 登录</nav><article class="normal-article"><header>作者 分享 收藏</header>
<div class="article__main__content"><p>第一段 <strong>重点</strong></p><h2>章节标题</h2><p>第二段</p><img data-original="/inside.png" alt="室内照片"></div>
<footer>举报 微信扫码 评论区</footer></article></body></html>`)
	article := extractEntryArticle(body, "text/html", "https://example.com/posts/one")
	if !strings.Contains(article.Text, "第一段 重点") || !strings.Contains(article.Text, "章节标题") || strings.Contains(article.Text, "登录") || strings.Contains(article.Text, "微信扫码") {
		t.Fatalf("unexpected reader text: %q", article.Text)
	}
	if !strings.Contains(article.SafeHTML, "<p ") || !strings.Contains(article.SafeHTML, "<h2 ") || !strings.Contains(article.SafeHTML, `data-entry-image="0"`) || !strings.Contains(article.SafeHTML, `data-block-id="b_`) {
		t.Fatalf("reader HTML lost structure or image: %s", article.SafeHTML)
	}
	if article.LeadImageIndex != -1 {
		t.Fatalf("cover outside article must not be treated as lead body image: %d", article.LeadImageIndex)
	}
	if len(article.ImageURLs) != 1 || article.ImageURLs[0] != "https://example.com/inside.png" {
		t.Fatalf("reader images = %#v", article.ImageURLs)
	}
}

func TestEntryContentRendererFactoryPrefersWechatContentContainer(t *testing.T) {
	body := []byte(`<html><body><div id="js_article"><h1>页面壳标题</h1><div id="js_content"><p>微信正文</p><img data-src="https://cdn.example.com/wechat.jpg"></div><footer>页面壳页脚</footer></div></body></html>`)
	article := renderEntryContent(body, "text/html", "https://mp.weixin.qq.com/s?id=1", "https://mp.weixin.qq.com/s?id=1")
	if !strings.Contains(article.Text, "微信正文") || strings.Contains(article.Text, "页面壳标题") || strings.Contains(article.Text, "页面壳页脚") {
		t.Fatalf("wechat renderer selected wrong node: %q", article.Text)
	}
	if !strings.Contains(article.SafeHTML, `data-entry-image="0"`) || len(article.ImageURLs) != 1 {
		t.Fatalf("wechat renderer lost images: html=%q images=%#v", article.SafeHTML, article.ImageURLs)
	}
	if article.LeadImageIndex != -1 {
		t.Fatalf("wechat body without OG image should not report a lead index: %d", article.LeadImageIndex)
	}
}

func TestEntryContentRendererFactorySupportsMarkdownAndWechatCDNHosts(t *testing.T) {
	markdown := renderEntryContent([]byte("# 标题\n\n- **重点**\n\n正文"), "text/markdown; charset=utf-8", "https://example.com/note", "https://example.com/note")
	if markdown.Format != "markdown" || !strings.Contains(markdown.Text, "标题") || !strings.Contains(markdown.Text, "重点") {
		t.Fatalf("markdown renderer = %#v", markdown)
	}
	if !isWechatRendererHost("img01.mmbiz.qpic.cn") || isWechatRendererHost("example.com") {
		t.Fatal("wechat CDN host classification drifted")
	}
}

func TestRenderHTMLArticleDoesNotIncludeOGImageOutsideSelectedContent(t *testing.T) {
	body := []byte(`<html><head><meta property="og:image" content="https://cdn.example.com/cover.jpg"></head><body><article><p>正文</p><img src="https://cdn.example.com/body.jpg"></article></body></html>`)
	article := renderEntryContent(body, "text/html", "https://example.com/post", "https://example.com/post")
	if article.LeadImageIndex != -1 || len(article.ImageURLs) != 1 || article.ImageURLs[0] != "https://cdn.example.com/body.jpg" {
		t.Fatalf("OG image leaked into body image projection: %#v", article)
	}
}

func TestRenderHTMLArticleTracksOGImageInsideSelectedContent(t *testing.T) {
	body := []byte(`<html><head><meta property="og:image" content="https://cdn.example.com/body.jpg"></head><body><article><p>正文</p><img src="https://cdn.example.com/body.jpg"></article></body></html>`)
	article := renderEntryContent(body, "text/html", "https://example.com/post", "https://example.com/post")
	if article.LeadImageIndex != 0 || len(article.ImageURLs) != 1 || article.ImageURLs[0] != "https://cdn.example.com/body.jpg" || strings.Contains(article.SafeHTML, `data-entry-image="0"`) {
		t.Fatalf("OG image in body was not tracked consistently: %#v", article)
	}
}

func TestRenderHTMLArticleBoundsLargeTextNode(t *testing.T) {
	article := renderEntryContent([]byte("<article><p>"+strings.Repeat("x", maxEntryFullHTMLBytes*2)+"</p></article>"), "text/html", "https://example.com/post", "https://example.com/post")
	if len(article.SafeHTML) > maxEntryFullHTMLBytes {
		t.Fatalf("safe HTML exceeded byte bound: %d", len(article.SafeHTML))
	}
}

func TestEntryContentRendererFactorySniffsMislabelledHTMLAndKeepsPlainText(t *testing.T) {
	htmlArticle := renderEntryContent([]byte(`<article><h2>误报响应头</h2><p>仍按网页正文排版</p></article>`), "text/plain; charset=utf-8", "https://example.com/post", "https://example.com/post")
	if !strings.Contains(htmlArticle.SafeHTML, "<h2 ") || !strings.Contains(htmlArticle.SafeHTML, "<p ") || !strings.Contains(htmlArticle.SafeHTML, `data-block-id="b_`) || !strings.Contains(htmlArticle.Text, "仍按网页正文排版") {
		t.Fatalf("mislabelled HTML was not rendered as an article: %#v", htmlArticle)
	}

	plainArticle := renderEntryContent([]byte("第一行\n第二行 < 100"), "text/plain", "https://example.com/note", "https://example.com/note")
	if plainArticle.SafeHTML != "" || plainArticle.Text != "第一行 第二行 < 100" || len(plainArticle.ImageURLs) != 0 {
		t.Fatalf("plain text renderer changed its output contract: %#v", plainArticle)
	}
}

func TestEntryContentCacheRemainsUsableAfterRefreshFailure(t *testing.T) {
	cached := EntryContent{ContentText: "正文缓存", ContentHash: "hash-1", SourceURL: "https://mp.weixin.qq.com/s?id=1", Status: "unavailable"}
	if !entryContentCacheUsable(cached, cached.SourceURL, cached.ContentHash) {
		t.Fatal("cached content should remain readable after a failed refresh")
	}
	if entryContentCacheUsable(cached, cached.SourceURL, "hash-2") {
		t.Fatal("cache from a different entry revision must not be reused")
	}
}

func TestEntryContentFailedRefreshPersistsStaleWithoutDroppingProjection(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-12T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"wechat-stale", "wechat-stale", "微信文章", "https://mp.weixin.qq.com/s?id=1", "公众号", "wechat-test", "app-fetch", "wechat", "article", now, "摘要", "text", "", "hash-1", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_html,image_urls_json,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		"wechat-stale", "已有正文", `<p>已有正文</p><img data-entry-image="0">`, `[`+`"https://cdn.example.com/article.jpg"`+`]`, "html", now, "ok", "", "https://mp.weixin.qq.com/s?id=1", now, "hash-1"); err != nil {
		t.Fatal(err)
	}
	if err := writeEntryContentFailure("wechat-stale", "https://mp.weixin.qq.com/s?id=1", "hash-1", "challenge"); err != nil {
		t.Fatal(err)
	}
	var status, content, images string
	if err := db.QueryRow(`SELECT status,content_text,image_urls_json FROM entry_full_texts WHERE entry_id='wechat-stale'`).Scan(&status, &content, &images); err != nil {
		t.Fatal(err)
	}
	if status != "stale" || content != "已有正文" || !strings.Contains(images, "article.jpg") {
		t.Fatalf("failed refresh damaged projection: status=%q content=%q images=%q", status, content, images)
	}
}

func TestEntryContentImageUsesPersistedURLOnlyFlow(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-08T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"rich-entry", "rich-external", "Rich", "https://example.com/article", "Example", "example", "app-fetch", "rss", "article", "", now, "摘要", "text", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_html,image_urls_json,content_format,fetched_at,status,error_message,source_url,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"rich-entry", "正文", `<p>正文</p><img data-entry-image="0">`, `["https://cdn.example.com/image.jpg"]`, "html", now, "ok", "", "https://example.com/article", now); err != nil {
		t.Fatal(err)
	}
	previousFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://cdn.example.com/image.jpg" || req.Header.Get("Referer") != "https://example.com/" || req.Header.Get("User-Agent") == "" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Fatalf("unexpected image request: %s headers=%v", req.URL, req.Header)
			}
			header := make(http.Header)
			header.Set("Content-Type", "image/jpeg")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("JPEG")), ContentLength: 4}, nil
		})}
	}
	result := httptest.NewRecorder()
	entryContentImageHandler(result, httptest.NewRequest(http.MethodGet, "/api/entries/content/image?id=rich-entry&index=0", nil))
	if result.Code != http.StatusOK || result.Body.String() != "JPEG" || result.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("image response = %d headers=%v body=%q", result.Code, result.Header(), result.Body.String())
	}
	unsafe := httptest.NewRecorder()
	entryContentImageHandler(unsafe, httptest.NewRequest(http.MethodGet, "/api/entries/content/image?id=rich-entry&index=9", nil))
	if unsafe.Code != http.StatusNotFound {
		t.Fatalf("unexpected missing image response = %d %s", unsafe.Code, unsafe.Body.String())
	}
}

func TestEntryContentCachedAndUnsafeFlow(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-08T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"entry-content", "external-content", "Cached", "https://example.com/article", "Example", "github-blog", "app-fetch", "rss", "article", "", now, "摘要", "text", "", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		"entry-content", "正文缓存", "text", now, "ok", "", "https://example.com/article", now); err != nil {
		t.Fatal(err)
	}
	cached := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id=entry-content", "")
	if cached.Code != http.StatusOK || !strings.Contains(cached.Body.String(), `"cached":true`) || !strings.Contains(cached.Body.String(), "正文缓存") {
		t.Fatalf("cached content = %d %s", cached.Code, cached.Body.String())
	}
	if _, err := db.Exec(`UPDATE news SET url=? WHERE id=?`, "http://127.0.0.1/private", "entry-content"); err != nil {
		t.Fatal(err)
	}
	unsafe := callJSON(t, entryContentHandler, http.MethodGet, "/api/entries/content?id=entry-content&refresh=1", "")
	if unsafe.Code != http.StatusOK || !strings.Contains(unsafe.Body.String(), `"status":"stale"`) || !strings.Contains(unsafe.Body.String(), "公开来源") {
		t.Fatalf("unsafe fallback = %d %s", unsafe.Code, unsafe.Body.String())
	}
}

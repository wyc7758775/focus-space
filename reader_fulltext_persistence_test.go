package main

import (
	"strings"
	"testing"
)

func TestPersistEntryContentWritesRichProjectionForNextOpen(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-16T08:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,published_at,summary_text,content_format,safe_html,content_hash,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"persist-rich", "persist-rich", "文章", "https://example.com/article", "Example", "example", "app-fetch", "rss", "article", now, "摘要", "text", "", "hash-1", now); err != nil {
		t.Fatal(err)
	}
	article := extractedArticle{
		Text:           "第一段\n第二段",
		SafeHTML:       "<p>第一段</p><p>第二段</p>",
		ImageURLs:      []string{"https://cdn.example.com/article.jpg"},
		LeadImageIndex: 0,
		Format:         "html",
	}
	if err := persistEntryContent("persist-rich", "https://example.com/article", "hash-1", article, now); err != nil {
		t.Fatal(err)
	}
	var html, images, status, hash string
	if err := db.QueryRow(`SELECT content_html,image_urls_json,status,content_hash FROM entry_full_texts WHERE entry_id=?`, "persist-rich").Scan(&html, &images, &status, &hash); err != nil {
		t.Fatal(err)
	}
	if status != "ok" || hash != "hash-1" || !strings.Contains(html, "<p>第二段</p>") || !strings.Contains(images, "article.jpg") {
		t.Fatalf("persisted projection = status=%q hash=%q html=%q images=%q", status, hash, html, images)
	}
}

func TestReaderAutomaticallyHydratesAndCachesOpenedFullText(t *testing.T) {
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(parity)
	for _, fragment := range []string{
		"indexedDB.open(fullTextBrowserDBName, 1)",
		"readBrowserFullText(entry)",
		"writeBrowserFullText(entry, result)",
		"loadFullText(entry, true)",
		"fullTextBrowserCacheLimit = 16",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("reader full-text cache contract missing %q", fragment)
		}
	}
	if strings.Contains(source, "不会保存到设备") {
		t.Fatal("reader still advertises non-persistent full-text behavior")
	}
}

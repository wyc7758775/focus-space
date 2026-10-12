package main

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
)

// TestSourceOPMLExportImportFlow pins the OPML interop contract end to end:
// export groups subscribed feeds by folder with title overrides respected,
// import reuses existing rows (never rewriting curated sources), creates
// missing folders, keeps an explicit folder choice, and reports honest
// imported/skipped counts.
func TestSourceOPMLExportImportFlow(t *testing.T) {
	db = openTestDB(t)

	folder := callJSON(t, folderHandler, http.MethodPost, "/api/folders", `{"name":"设计","sort_order":10}`)
	if folder.Code != http.StatusOK {
		t.Fatalf("create folder status = %d, body = %s", folder.Code, folder.Body.String())
	}
	var createdFolder struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(folder.Body.Bytes(), &createdFolder); err != nil {
		t.Fatal(err)
	}

	created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"https://example.com/design.xml","name":"设计周刊"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("create source status = %d, body = %s", created.Code, created.Body.String())
	}
	subscribed := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"`+sourceIDForURL("https://example.com/design.xml")+`","enabled":true,"folder_id":"`+createdFolder.ID+`","title_override":"设计周刊（改名）"}`)
	if subscribed.Code != http.StatusOK {
		t.Fatalf("subscribe status = %d, body = %s", subscribed.Code, subscribed.Body.String())
	}
	loose := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"https://example.com/loose.xml","name":"独立博客"}`)
	if loose.Code != http.StatusOK {
		t.Fatalf("create loose source status = %d, body = %s", loose.Code, loose.Body.String())
	}
	if result := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"`+sourceIDForURL("https://example.com/loose.xml")+`","enabled":true}`); result.Code != http.StatusOK {
		t.Fatalf("subscribe loose status = %d, body = %s", result.Code, result.Body.String())
	}

	exported := callJSON(t, sourceOPMLHandler, http.MethodGet, "/api/sources/opml", "")
	if exported.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", exported.Code, exported.Body.String())
	}
	payload := exported.Body.String()
	var document opmlDocument
	if err := xml.Unmarshal([]byte(payload), &document); err != nil {
		t.Fatalf("export is not valid OPML: %v\n%s", err, payload)
	}
	folderNode, looseNode := opmlOutline{}, opmlOutline{}
	for _, node := range document.Body.Outlines {
		if node.XMLURL == "" && node.Text == "设计" {
			folderNode = node
		}
		if node.XMLURL == "https://example.com/loose.xml" {
			looseNode = node
		}
	}
	if folderNode.Text != "设计" || len(folderNode.Outlines) != 1 || folderNode.Outlines[0].XMLURL != "https://example.com/design.xml" {
		t.Fatalf("folder outline missing or malformed: %#v", folderNode)
	}
	if folderNode.Outlines[0].Text != "设计周刊（改名）" {
		t.Fatalf("export must respect the title override, got %q", folderNode.Outlines[0].Text)
	}
	if looseNode.XMLURL == "" {
		t.Fatalf("loose feed missing from export body: %#v", document.Body.Outlines)
	}

	importPayload := `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0"><head><title>import</title></head><body>
<outline text="设计"><outline type="rss" text="新设计源" xmlUrl="https://example.com/new-design.xml"/></outline>
<outline text="设计"><outline type="rss" text="重复设计源" xmlUrl="https://example.com/design.xml"/></outline>
<outline type="rss" text="导入独立源" xmlUrl="https://example.com/imported-loose.xml"/>
<outline type="rss" text="内网地址" xmlUrl="http://127.0.0.1/feed.xml"/>
<outline type="rss" text="同文档重复" xmlUrl="https://example.com/imported-loose.xml"/>
</body></opml>`
	imported := callJSON(t, sourceOPMLHandler, http.MethodPost, "/api/sources/opml", importPayload)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status = %d, body = %s", imported.Code, imported.Body.String())
	}
	var report opmlImportReport
	if err := json.Unmarshal(imported.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	// imported: new-design + imported-loose.  skipped: already-subscribed
	// design.xml, localhost URL, in-document duplicate.
	if report.Imported != 2 || report.Skipped != 3 {
		t.Fatalf("import report = %+v, want imported=2 skipped=3", report)
	}
	if report.Folders != 0 {
		t.Fatalf("existing folder reused, want folders=0 got %d", report.Folders)
	}

	var folderCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_folders WHERE name = '设计'`).Scan(&folderCount); err != nil || folderCount != 1 {
		t.Fatalf("folder design count = %d err = %v, want exactly 1", folderCount, err)
	}
	var newDesignFolder string
	if err := db.QueryRow(`SELECT s.folder_id FROM feed_subscriptions s JOIN feed_sources f ON f.id = s.source_id WHERE f.url = 'https://example.com/new-design.xml'`).Scan(&newDesignFolder); err != nil {
		t.Fatalf("imported feed missing folder assignment: %v", err)
	}
	if newDesignFolder != createdFolder.ID {
		t.Fatalf("imported feed folder = %q, want existing folder %q", newDesignFolder, createdFolder.ID)
	}
	// Re-importing the same document is fully idempotent: everything is
	// already subscribed, nothing new is created.
	again := callJSON(t, sourceOPMLHandler, http.MethodPost, "/api/sources/opml", importPayload)
	if again.Code != http.StatusOK {
		t.Fatalf("re-import status = %d, body = %s", again.Code, again.Body.String())
	}
	var secondReport opmlImportReport
	if err := json.Unmarshal(again.Body.Bytes(), &secondReport); err != nil {
		t.Fatal(err)
	}
	if secondReport.Imported != 0 || secondReport.Skipped != 5 {
		t.Fatalf("re-import report = %+v, want imported=0 skipped=5", secondReport)
	}

	// A brand-new folder name in the document creates the folder once.
	freshFolderDoc := `<opml version="2.0"><body><outline text="新分组"><outline type="rss" text="甲" xmlUrl="https://example.com/a.xml"/><outline type="rss" text="乙" xmlUrl="https://example.com/b.xml"/></outline></body></opml>`
	freshResult := callJSON(t, sourceOPMLHandler, http.MethodPost, "/api/sources/opml", freshFolderDoc)
	if freshResult.Code != http.StatusOK {
		t.Fatalf("fresh folder import status = %d, body = %s", freshResult.Code, freshResult.Body.String())
	}
	var freshReport opmlImportReport
	if err := json.Unmarshal(freshResult.Body.Bytes(), &freshReport); err != nil {
		t.Fatal(err)
	}
	if freshReport.Imported != 2 || freshReport.Folders != 1 {
		t.Fatalf("fresh folder report = %+v, want imported=2 folders=1", freshReport)
	}
	var freshFolderCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_folders WHERE name = '新分组'`).Scan(&freshFolderCount); err != nil || freshFolderCount != 1 {
		t.Fatalf("新分组 folder count = %d err = %v, want 1", freshFolderCount, err)
	}
}

// TestSourceOPMLImportRejectsMalformedDocuments guards the parse boundary:
// broken XML and documents without any outline are client errors, and the
// endpoint refuses unsupported methods.
func TestSourceOPMLImportRejectsMalformedDocuments(t *testing.T) {
	db = openTestDB(t)
	if bad := callJSON(t, sourceOPMLHandler, http.MethodPost, "/api/sources/opml", `<opml><body><outline`); bad.Code != http.StatusBadRequest {
		t.Fatalf("malformed xml status = %d, want 400", bad.Code)
	}
	if empty := callJSON(t, sourceOPMLHandler, http.MethodPost, "/api/sources/opml", `<opml version="2.0"><body></body></opml>`); empty.Code != http.StatusBadRequest {
		t.Fatalf("empty body status = %d, want 400", empty.Code)
	}
	if method := callJSON(t, sourceOPMLHandler, http.MethodDelete, "/api/sources/opml", ""); method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete status = %d, want 405", method.Code)
	}
}

// TestSourceOPMLFeedOutlineShapeUnit pins the per-feed outline mapping so the
// export stays parseable by every mainstream reader.
func TestSourceOPMLFeedOutlineShapeUnit(t *testing.T) {
	outline := opmlFeedOutline(FeedSource{
		Name:         "原始名",
		TitleOverride: "改名后",
		URL:          "https://example.com/feed.xml",
		SiteURL:      "https://example.com",
		Description:  "说明 & <xml>",
	})
	encoded, err := xml.Marshal(outline)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(encoded)
	for _, expected := range []string{
		`text="改名后"`,
		`xmlUrl="https://example.com/feed.xml"`,
		`htmlUrl="https://example.com"`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("outline %s missing %s", rendered, expected)
		}
	}
	if strings.Contains(rendered, "原始名") {
		t.Fatalf("title override must win: %s", rendered)
	}
	if !strings.Contains(rendered, "&lt;xml&gt;") {
		t.Fatalf("description must be xml-escaped: %s", rendered)
	}
}

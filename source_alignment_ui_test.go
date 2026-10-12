package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestSourceAlignmentPackMarkers pins the 订阅源对齐 surface: the
// subscription manager search and unsubscribed section, drag-to-folder, and
// OPML import/export entries. Markers are behavior anchors, not coverage of
// the JS itself.
func TestSourceAlignmentPackMarkers(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)

	for _, expected := range []string{
		// Per-feed quick actions at document level, with the modal-scope
		// exclusion so onModalClick and this delegate never double-fire.
		`function onSourceAlignmentClick(`,
		`closest?.("#discover-modal")`,
		`data-open-site-source=`,
		`data-copy-source-url=`,
		// Manager search: pure filter shared by the list and the unsubscribed
		// section, document-level input delegation with focus restore.
		`function managerSourceMatchesQuery(`,
		`id="manager-source-search"`,
		`data-unsubscribed-section`,
		// 已退订 semantics: only feeds with a disabled subscription row are
		// listed, never the untouched catalog.
		`source.formerly_subscribed`,
		// Drag-to-folder: sidebar items are drag sources, folder group
		// triggers are drop targets, default group clears the folder.
		`function installSourceDragToFolder(`,
		`data-drag-source-id=`,
		`data-folder-drop=`,
		`folder:${text(source.folder_id)}`,
		// OPML: import posts the raw file through the same endpoint the
		// export link downloads, capped client-side at the server limit.
		// The row lives in the generated discover-tab template (rebuildModal
		// replaces the modal DOM, static markup would never show) and the
		// listeners are document-delegated for the same reason.
		`function installOPMLImport(`,
		`id="opml-import-button"`,
		`id="opml-export-button"`,
		`id="opml-file-input"`,
		`./api/sources/opml`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("source alignment pack is missing %q", expected)
		}
	}
	// 来源范围视图不得再渲染订阅页头：4 列视频网格会把它压进单个网格单元
	// （源名逐字换行成竖排、大片空白，被用户当成多余卡片）。来源级操作
	// 全部保留在订阅管理器行内。
	for _, forbidden := range []string{
		`function sourceScopeHeaderMarkup(`,
		`data-source-scope-header=`,
		`.source-scope-header{`,
	} {
		if strings.Contains(script, forbidden) || strings.Contains(css, forbidden) {
			t.Errorf("source scope header must stay out of the timeline: %q", forbidden)
		}
	}
	if got := strings.Count(script, `data-sync-source=`); got < 1 {
		t.Errorf("per-feed sync action wired %d times, want >= 1 (manager row)", got)
	}
	if got := strings.Count(script, `./api/sources/opml`); got < 2 {
		t.Errorf("opml endpoint referenced %d times, want >= 2 (import POST + export link)", got)
	}

	indexBytes, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexBytes)
	for _, expected := range []string{
		`?v=20260928-graph-tidy4`,
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("index.html source alignment is missing %q", expected)
		}
	}

	for _, expected := range []string{
		`.manager-unsubscribed{`,
		`[data-folder-drop].drag-over`,
		`.opml-row{`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader css is missing %q", expected)
		}
	}
}

// TestFormerlySubscribedProjection pins the 已退订 semantics: a feed the user
// subscribed to and then unsubscribed keeps formerly_subscribed=1, while a
// never-subscribed catalog row must not appear as formerly subscribed.  The
// unsubscribed list in the manager is built on this flag, not on
// "every catalog row the user never chose".
func TestFormerlySubscribedProjection(t *testing.T) {
	db = openTestDB(t)

	unsubURL := "https://example.com/formerly.xml"
	if created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"`+unsubURL+`","name":"退订过的源"}`); created.Code != http.StatusOK {
		t.Fatalf("create formerly source status = %d, body = %s", created.Code, created.Body.String())
	}
	if sub := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"`+sourceIDForURL(unsubURL)+`","enabled":true}`); sub.Code != http.StatusOK {
		t.Fatalf("subscribe status = %d, body = %s", sub.Code, sub.Body.String())
	}
	if unsub := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"`+sourceIDForURL(unsubURL)+`","enabled":false}`); unsub.Code != http.StatusOK {
		t.Fatalf("unsubscribe status = %d, body = %s", unsub.Code, unsub.Body.String())
	}
	neverURL := "https://example.com/never-subscribed.xml"
	if created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"`+neverURL+`","name":"从未订阅的源"}`); created.Code != http.StatusOK {
		t.Fatalf("create never source status = %d, body = %s", created.Code, created.Body.String())
	}
	activeURL := "https://example.com/active.xml"
	if created := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"`+activeURL+`","name":"订阅中的源"}`); created.Code != http.StatusOK {
		t.Fatalf("create active source status = %d, body = %s", created.Code, created.Body.String())
	}
	if sub := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", `{"source_id":"`+sourceIDForURL(activeURL)+`","enabled":true}`); sub.Code != http.StatusOK {
		t.Fatalf("subscribe active status = %d, body = %s", sub.Code, sub.Body.String())
	}

	// applySources caps the projection at 100 rows and the curated catalog
	// in the test schema is larger, which would push the freshly-added rows
	// out.  Query through the same projection with the full bounded limit.
	sources, err := queryFeedSources(maxFeedSourceQueryLimit, "")
	if err != nil {
		t.Fatal(err)
	}
	byURL := make(map[string]FeedSource, len(sources))
	for _, source := range sources {
		byURL[source.URL] = source
	}
	if source := byURL[unsubURL]; source.Subscribed || !source.FormerlySubscribed {
		t.Fatalf("unsubscribed feed projected as subscribed=%v formerly=%v, want false/true", source.Subscribed, source.FormerlySubscribed)
	}
	if source := byURL[neverURL]; source.Subscribed || source.FormerlySubscribed {
		t.Fatalf("never-subscribed feed projected as subscribed=%v formerly=%v, want false/false", source.Subscribed, source.FormerlySubscribed)
	}
	if source := byURL[activeURL]; !source.Subscribed || source.FormerlySubscribed {
		t.Fatalf("active feed projected as subscribed=%v formerly=%v, want true/false", source.Subscribed, source.FormerlySubscribed)
	}
}

package main

import (
	"strings"
	"testing"
)

func TestReaderArticleSelectionOnlyUpdatesCardAndDetail(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`function selectNewsEntryLocally(entry)`,
		`readerEntryCard(entry)?.classList.add("selected")`,
		`renderDetail();`,
		`if (entry?.kind === "news") return selectNewsEntryLocally(entry);`,
		`function markReaderEntryReadLocally(entry)`,
		`function installLocalReaderDetailNavigation()`,
		`event.stopImmediatePropagation();`,
		`renderOverview();`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("local Reader update is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`document.body.classList.remove("detail-open");\n      render();\n      openFloatingVideoPlayer(entry);`,
		`document.body.classList.remove("detail-open");\n      render();\n      if (!pref(entry).read)`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("Reader selection still repaints the whole shell: %q", forbidden)
		}
	}
}

func TestSourceNavigationDoesNotEnqueueNetworkSync(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		"Navigation is read-only. Source sync is owned by explicit refresh",
		"暂无本地快照",
		"点击刷新后，服务端会在后台同步",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("source snapshot-first behavior is missing %q", expected)
		}
	}
	start := strings.Index(script, "async function loadSourceOnDemand")
	end := strings.Index(script[start:], "async function subscribePack")
	if start < 0 || end < 0 {
		t.Fatal("source navigation function boundaries are missing")
	}
	body := script[start : start+end]
	if strings.Contains(body, "enqueueSourceSyncOnce(id)") {
		t.Fatal("opening a source must not enqueue a network sync")
	}
}

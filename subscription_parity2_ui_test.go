package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestSubscriptionParity2Markers pins the 订阅管理对齐 Folo 第二批 surface:
// per-feed rename, per-feed mark-all-read with bounded batching and undo,
// sync-status surfacing, manager sort modes, and folder rename/delete.
func TestSubscriptionParity2Markers(t *testing.T) {
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
		// Rename: modal rows and any timeline-side rows share the same
		// delegated actions; empty value restores the default title.
		`function beginRenameSource(`,
		`function saveRenameSource(`,
		`function cancelRenameSource(`,
		`data-rename-source=`,
		`data-rename-save=`,
		`data-rename-cancel`,
		`id="rename-source-input"`,
		// Saving the default name back must CLEAR the override (patch empty
		// string), not skip the patch — otherwise the old title_override keeps
		// masking the default name forever.
		`const override = value === text(source.name) ? "" : value;`,
		`title_override: override`,
		// Per-source mark-all-read: bounded collection (6 pages × 50 keys,
		// the server caps ./api/entries limit at 50), ≤200-key bulk batches,
		// undo restores exactly the collected keys.
		`function markSourceReadCollect(`,
		`function markSourceRead(`,
		`function undoMarkSourceRead(`,
		`data-read-source=`,
		`source_id: sourceID`,
		`keys.slice(index, index + 200)`,
		`read: false,`,
		// Status line: sync errors must be visible with the error detail in
		// a tooltip; unread/entry/last-sync give the management actions
		// context.  No fake loading states.
		`function managerRelativeTime(`,
		`source-status-error`,
		`readerFocusCopy("从未同步", "never")`,
		`最近同步`,
		// Manager sort: 手动 stays the default (sort_order is the persisted
		// truth), name/unread/sync are read-only render orderings.
		`function managerSortedSources(`,
		`id="manager-sort-mode"`,
		`["manual", "name", "unread", "sync"].includes(mode)`,
		// Folder rename/delete: same /api/folders contract as create;
		// delete is two-click armed (backend moves feeds to 未分组) and the
		// confirm click ignores same-click handler echoes (modal + document
		// dual dispatch) via the arm timestamp.
		`async function renameFolder(`,
		`async function deleteFolder(`,
		`folderDeleteArmedAt`,
		`Date.now() - parity.folderDeleteArmedAt < 400`,
		`data-rename-folder`,
		`data-delete-folder`,
		`id="folder-manage-select"`,
		`id="folder-rename-name"`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("subscription parity pack 2 is missing %q", expected)
		}
	}

	// The list-level mark-all-read must batch too: the server rejects bulk
	// bodies over 200 keys, so an unbatched single PATCH fails whole-list
	// reads at 201 entries.
	head := `if (keys.length)`
	if strings.Contains(script, head) {
		t.Errorf("markVisibleRead still has an unbatched single bulk PATCH (%q)", head)
	}

	for _, expected := range []string{
		`.source-meta-line{`,
		`.source-status-error`,
		`.manager-rename-row`,
		`.folder-manage-row`,
		`.folder-delete-btn`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader css is missing %q", expected)
		}
	}
}

// TestFolderRenameDeleteContract pins the /api/folders rename + delete
// contract the new manager UI rides on: rename keeps the id, delete moves
// subscriptions back to 未分组 instead of orphaning them.
func TestFolderRenameDeleteContract(t *testing.T) {
	db = openTestDB(t)

	createdFolder := callJSON(t, folderHandler, http.MethodPost, "/api/folders", `{"name":"月度报告"}`)
	if createdFolder.Code != http.StatusOK {
		t.Fatalf("create folder status = %d, body = %s", createdFolder.Code, createdFolder.Body.String())
	}
	var folder SubscriptionFolder
	if err := json.Unmarshal(createdFolder.Body.Bytes(), &folder); err != nil {
		t.Fatal(err)
	}

	renamed := callJSON(t, folderHandler, http.MethodPatch, "/api/folders", `{"id":"`+folder.ID+`","name":"季度报告"}`)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename folder status = %d, body = %s", renamed.Code, renamed.Body.String())
	}

	listed := callJSON(t, folderHandler, http.MethodGet, "/api/folders", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list folders status = %d", listed.Code)
	}
	body := listed.Body.String()
	if !strings.Contains(body, "季度报告") || strings.Contains(body, `"月度报告"`) {
		t.Fatalf("folder rename did not stick: %s", body)
	}

	// A subscribed feed in the folder survives delete by moving to 未分组.
	if src := callJSON(t, sourceHandler, http.MethodPost, "/api/sources", `{"url":"https://example.com/folder-delete.xml","name":"分组删除样本"}`); src.Code != http.StatusOK {
		t.Fatalf("create source status = %d, body = %s", src.Code, src.Body.String())
	}
	subscribeBody := `{"source_id":"` + sourceIDForURL("https://example.com/folder-delete.xml") + `","enabled":true,"folder_id":"` + folder.ID + `"}`
	if sub := callJSON(t, subscriptionHandler, http.MethodPatch, "/api/subscriptions", subscribeBody); sub.Code != http.StatusOK {
		t.Fatalf("subscribe into folder status = %d, body = %s", sub.Code, sub.Body.String())
	}

	deleted := callJSON(t, folderHandler, http.MethodDelete, "/api/folders", `{"id":"`+folder.ID+`"}`)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete folder status = %d, body = %s", deleted.Code, deleted.Body.String())
	}

	var folderID string
	if err := db.QueryRow(`SELECT folder_id FROM feed_subscriptions WHERE source_id = ?`, sourceIDForURL("https://example.com/folder-delete.xml")).Scan(&folderID); err != nil {
		t.Fatal(err)
	}
	if folderID != "" {
		t.Fatalf("folder delete left subscription folder_id = %q, want empty", folderID)
	}
}

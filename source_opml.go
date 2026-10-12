package main

// OPML 2.0 interop for the subscription list.  Export walks the same
// applySources projection the reader sidebar uses, so folder grouping and
// title overrides match what the user sees.  Import reuses the public-source
// upsert and the subscription mutation paths: no second state mechanism, and
// curated catalog rows are subscribed to, never rewritten.

import (
	"database/sql"
	"encoding/xml"
	"errors"
	"focus-space/internal/app"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// sourceOPMLMaxBytes bounds one uploaded outline document.  A normal
// subscription export is a few hundred kilobytes; anything larger is refused
// instead of parsed.
const sourceOPMLMaxBytes = 2 << 20

type opmlOutline struct {
	XMLName     xml.Name      `xml:"outline"`
	Text        string        `xml:"text,attr"`
	Title       string        `xml:"title,attr,omitempty"`
	Type        string        `xml:"type,attr,omitempty"`
	XMLURL      string        `xml:"xmlUrl,attr,omitempty"`
	HTMLURL     string        `xml:"htmlUrl,attr,omitempty"`
	Description string        `xml:"description,attr,omitempty"`
	Outlines    []opmlOutline `xml:"outline,omitempty"`
}

type opmlHead struct {
	Title       string `xml:"title"`
	DateCreated string `xml:"dateCreated"`
}

type opmlBody struct {
	Outlines []opmlOutline `xml:"outline"`
}

type opmlDocument struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    opmlHead `xml:"head"`
	Body    opmlBody `xml:"body"`
}

func sourceOPMLHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sourceOPMLExport(w, r)
	case http.MethodPost:
		sourceOPMLImport(w, r)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func opmlFeedOutline(source FeedSource) opmlOutline {
	title := strings.TrimSpace(source.TitleOverride)
	if title == "" {
		title = source.Name
	}
	return opmlOutline{
		Text:        title,
		Title:       title,
		Type:        "rss",
		XMLURL:      source.URL,
		HTMLURL:     source.SiteURL,
		Description: source.Description,
	}
}

func sourceOPMLExport(w http.ResponseWriter, _ *http.Request) {
	var dashboard Dashboard
	if err := applySources(&dashboard); err != nil {
		app.HTTPErr(w, err)
		return
	}
	folderOrder := make([]string, 0, 8)
	grouped := make(map[string][]FeedSource)
	for _, source := range dashboard.Sources {
		if !source.Subscribed {
			continue
		}
		folder := strings.TrimSpace(source.FolderName)
		if _, seen := grouped[folder]; !seen {
			folderOrder = append(folderOrder, folder)
		}
		grouped[folder] = append(grouped[folder], source)
	}
	document := opmlDocument{Version: "2.0"}
	document.Head.Title = "focus-space 订阅"
	document.Head.DateCreated = time.Now().UTC().Format(time.RFC1123Z)
	for _, folder := range folderOrder {
		if folder == "" {
			for _, feed := range grouped[folder] {
				document.Body.Outlines = append(document.Body.Outlines, opmlFeedOutline(feed))
			}
			continue
		}
		node := opmlOutline{Text: folder, Title: folder}
		for _, feed := range grouped[folder] {
			node.Outlines = append(node.Outlines, opmlFeedOutline(feed))
		}
		document.Body.Outlines = append(document.Body.Outlines, node)
	}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subscriptions.opml"`)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(encoded)
}

type opmlImportReport struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Folders  int `json:"folders"`
}

func sourceOPMLImport(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, sourceOPMLMaxBytes))
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, "OPML 文档过大或读取失败")
		return
	}
	var document opmlDocument
	if err := xml.Unmarshal(body, &document); err != nil || len(document.Body.Outlines) == 0 {
		app.UserErr(w, http.StatusBadRequest, "OPML 文档无法解析")
		return
	}

	folderIDs := loadExistingFolderIDs()
	report := opmlImportReport{}
	seen := make(map[string]bool)

	var walk func(outlines []opmlOutline, folderName string)
	walk = func(outlines []opmlOutline, folderName string) {
		for _, node := range outlines {
			rawURL := strings.TrimSpace(node.XMLURL)
			if rawURL == "" {
				nested := strings.TrimSpace(node.Title)
				if nested == "" {
					nested = strings.TrimSpace(node.Text)
				}
				walk(node.Outlines, nested)
				continue
			}
			normalized, normalizeErr := normalizePublicURL(rawURL)
			if normalizeErr != nil {
				report.Skipped++
				continue
			}
			if seen[normalized] {
				report.Skipped++
				continue
			}
			seen[normalized] = true

			sourceID, resolveErr := resolveOPMLFeedSource(normalized, node)
			if resolveErr != nil {
				report.Skipped++
				continue
			}
			if sourceAlreadySubscribed(sourceID) {
				report.Skipped++
				continue
			}

			folderID := ""
			if name := strings.TrimSpace(folderName); name != "" {
				folderID, resolveErr = ensureOPMLFolder(folderIDs, name, &report.Folders)
				if resolveErr != nil {
					report.Skipped++
					continue
				}
			}
			if subscribeErr := subscribeOPMLSource(sourceID, folderID); subscribeErr != nil {
				report.Skipped++
				continue
			}
			report.Imported++
			// Some exporters nest feeds under feed nodes; treat them as
			// siblings of the same folder rather than dropping them.
			if len(node.Outlines) > 0 {
				walk(node.Outlines, folderName)
			}
		}
	}
	walk(document.Body.Outlines, "")

	app.WriteJSON(w, http.StatusOK, report)
}

// sourceAlreadySubscribed keeps import counts honest: an outline whose feed
// is already enabled counts as skipped, not imported.
func sourceAlreadySubscribed(sourceID string) bool {
	var enabled int
	err := db.QueryRow(`SELECT enabled FROM feed_subscriptions WHERE source_id = ?`, sourceID).Scan(&enabled)
	return err == nil && enabled == 1
}

// resolveOPMLFeedSource returns the source ID for one outline.  An address
// that already exists (user-added or curated catalog row) is reused as-is so
// an import never rewrites curated rows; unknown addresses go through the
// regular public-source upsert.
func resolveOPMLFeedSource(normalizedURL string, node opmlOutline) (string, error) {
	var existingID string
	err := db.QueryRow(`SELECT id FROM feed_sources WHERE url = ?`, normalizedURL).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	name := strings.TrimSpace(node.Title)
	if name == "" {
		name = strings.TrimSpace(node.Text)
	}
	source, err := upsertFeedSource(sourceMutationRequest{
		Kind:        "rss",
		Name:        name,
		URL:         normalizedURL,
		Description: strings.TrimSpace(node.Description),
	})
	if err != nil {
		return "", err
	}
	return source.ID, nil
}

// loadExistingFolderIDs snapshots folder names to stable IDs so repeated
// imports (and duplicate folder titles) resolve to one row.
func loadExistingFolderIDs() map[string]string {
	folderIDs := make(map[string]string)
	rows, err := db.Query(`SELECT id, name FROM subscription_folders`)
	if err != nil {
		return folderIDs
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			folderIDs[strings.ToLower(strings.TrimSpace(name))] = id
		}
	}
	return folderIDs
}

func ensureOPMLFolder(folderIDs map[string]string, name string, created *int) (string, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if id, seen := folderIDs[key]; seen {
		return id, nil
	}
	id := stableObjectID("folder", name)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES(?,?,0,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
		id, name, now, now); err != nil {
		return "", err
	}
	folderIDs[key] = id
	*created++
	return id, nil
}

// subscribeOPMLSource enables the subscription and assigns the imported
// folder only when the user has not chosen one already, mirroring the
// subscription mutation handler's column semantics.
func subscribeOPMLSource(sourceID, folderID string) error {
	current, err := subscriptionState(sourceID)
	fresh := errors.Is(err, sql.ErrNoRows)
	if err != nil && !fresh {
		return err
	}
	if current.FolderID == "" && folderID != "" && folderExists(folderID) {
		current.FolderID = folderID
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`
INSERT INTO feed_subscriptions(source_id, enabled, subscribed_at, updated_at, folder_id, title_override, pinned, muted, sort_order)
VALUES(?,1,?,?,?,?,?,?,?)
ON CONFLICT(source_id) DO UPDATE SET enabled=1, updated_at=excluded.updated_at,
folder_id=excluded.folder_id, title_override=excluded.title_override, pinned=excluded.pinned,
muted=excluded.muted, sort_order=excluded.sort_order`,
		sourceID, now, now, current.FolderID, current.TitleOverride,
		boolInt(current.Pinned), boolInt(current.Muted), current.SortOrder); err != nil {
		return err
	}
	if fresh {
		if _, syncErr := enqueueSyncRequest(sourceID); syncErr != nil {
			// The subscription row is already written; a failed enqueue only
			// delays the first fetch, so log and keep importing.
			log.Printf("OPML 导入创建首次同步请求失败 source=%s: %v", sourceID, syncErr)
		}
	}
	return nil
}

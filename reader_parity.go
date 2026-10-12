package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxDiscoveryCandidates = 20
	maxSourcePreviewItems  = 5
)

const (
	discoveryInputKeyword = "keyword"
	discoveryInputWebsite = "website_url"
	discoveryInputFeed    = "feed_url"
	discoveryInputRSSHub  = "rsshub_route"
	discoveryInputWeChat  = "wechat_article"
)

func validCollectionMode(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "app", "agent", "hybrid", "source_gateway":
		return true
	default:
		return false
	}
}

func validCollectionSettings(interval, backfillDays, retentionDays, retentionLimit int) bool {
	return interval >= 300 && interval <= 604800 &&
		backfillDays >= 0 && backfillDays <= 3650 &&
		retentionDays >= 1 && retentionDays <= 3650 &&
		retentionLimit >= 10 && retentionLimit <= 10000
}

type FeedDiscovery struct {
	ID            string     `json:"id"`
	InputURL      string     `json:"input_url"`
	NormalizedURL string     `json:"normalized_url"`
	FeedURL       string     `json:"feed_url,omitempty"`
	Title         string     `json:"title,omitempty"`
	Description   string     `json:"description,omitempty"`
	SiteURL       string     `json:"site_url,omitempty"`
	IconURL       string     `json:"icon_url,omitempty"`
	FeedFormat    string     `json:"feed_format,omitempty"`
	Status        string     `json:"status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	Preview       []NewsItem `json:"preview,omitempty"`
	DiscoveredAt  string     `json:"discovered_at"`
	ConfirmedAt   string     `json:"confirmed_at,omitempty"`
}

type SubscriptionFolder struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Count     int    `json:"count"`
}

type SubscriptionTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count"`
}

type SourcePack struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Category    string       `json:"category"`
	IconText    string       `json:"icon_text"`
	Sources     []FeedSource `json:"sources"`
}

type subscriptionStateValue struct {
	FolderID      string
	TitleOverride string
	Pinned        bool
	Muted         bool
	SortOrder     int
}

type sourceQueryOptions struct {
	// SourceID narrows the metadata projection to one source.  The reader uses
	// this bounded lookup when a user clicks a source before the full catalog
	// request has settled; it must never turn that click into an all-source
	// refresh.
	SourceID       string
	Query          string
	Category       string
	Language       string
	ContentType    string
	Sort           string
	OnlySubscribed bool
	IncludeCounts  bool
}

type classifiedDiscoveryInput struct {
	Kind       string `json:"input_kind"`
	Raw        string `json:"input"`
	Normalized string `json:"normalized_input,omitempty"`
	Query      string `json:"query,omitempty"`
	Message    string `json:"message"`
}

// The maintained public catalog currently contains several hundred entries.
// Keep the response bounded for the 2 GB device while allowing the complete
// catalog to be browsed and searched instead of silently truncating at 200.
const maxFeedSourceQueryLimit = 1000

func queryFeedSources(limit int, sourceID string) ([]FeedSource, error) {
	return queryFeedSourcesWithCountsAndContentTypes(limit, sourceID, true, true)
}

func queryFeedSourcesWithCounts(limit int, sourceID string, includeCounts bool) ([]FeedSource, error) {
	return queryFeedSourcesWithCountsAndContentTypes(limit, sourceID, includeCounts, true)
}

// queryFeedSourcesForCatalog is the projection used by the lightweight source
// directory request.  The directory needs catalog metadata immediately, but
// it does not need to scan the entire news table just to infer types for every
// source.  Callers that request a content_type filter must set
// includeContentTypes so the observed-entry query remains authoritative.
func queryFeedSourcesForCatalog(limit int, sourceID string, includeCounts, includeContentTypes bool) ([]FeedSource, error) {
	return queryFeedSourcesWithCountsAndContentTypes(limit, sourceID, includeCounts, includeContentTypes)
}

func queryFeedSourcesWithCountsAndContentTypes(limit int, sourceID string, includeCounts, includeContentTypes bool) ([]FeedSource, error) {
	if limit <= 0 || limit > maxFeedSourceQueryLimit {
		limit = 100
	}
	countColumns := `0, 0`
	// Keep the source catalog's counts on the same visibility contract as
	// /api/reader/counts and /api/entries.  In particular, dismissed rows and
	// Chat-imported rows belonging to another owner/profile must not inflate a
	// source's total.  The shared predicate contains two placeholders for the
	// active data scope; its arguments are appended below in SELECT order.
	countArgs := make([]any, 0, 4)
	if includeCounts {
		visiblePredicate, visibleArgs := readerVisibleEntryPredicateForDB()
		countColumns = `
       COALESCE((SELECT COUNT(*) FROM news n
                 WHERE n.source_id = f.id AND ` + visiblePredicate + `), 0),
       COALESCE((SELECT COUNT(*) FROM news n
                 LEFT JOIN item_preferences ip ON ip.item_key = 'news:' || n.id
                 WHERE n.source_id = f.id AND ` + visiblePredicate + `
                   AND COALESCE(ip.is_read, 0) = 0
				   ), 0)`
		countArgs = append(countArgs, visibleArgs...)
		countArgs = append(countArgs, visibleArgs...)
	}
	query := `
SELECT f.id, f.kind, f.name, f.url, f.description, f.is_default,
       f.status, f.records_seen, f.records_used,
       ` + countColumns + `,
       f.last_sync_at, COALESCE(f.gateway_last_success_at, ''), COALESCE(f.gateway_cached_entry_count, 0), f.last_error,
       COALESCE(s.enabled, 0), CASE WHEN s.source_id IS NOT NULL AND COALESCE(s.enabled, 0) = 0 THEN 1 ELSE 0 END,
       COALESCE(c.site_url, ''), COALESCE(c.category, 'other'),
       COALESCE(c.language, 'und'), COALESCE(c.icon_text, ''), COALESCE(c.icon_color, '#64748b'),
       COALESCE(c.icon_url, ''), CASE WHEN COALESCE(c.icon_url, '') <> '' OR COALESCE(c.site_url, '') <> '' OR EXISTS(
           SELECT 1 FROM source_icon_cache icon WHERE icon.source_id = f.id AND length(icon.body) > 0
       ) THEN 1 ELSE 0 END,
       COALESCE(c.tags_json, '[]'), COALESCE(c.feed_format, f.kind),
       COALESCE(c.recommended, 0), COALESCE(c.popularity, 0),
       COALESCE(c.validation_status, 'unverified'), COALESCE(c.validated_at, ''),
       COALESCE(c.catalog_origin, ''), COALESCE(s.folder_id, ''),
       COALESCE(s.title_override, ''), COALESCE(s.pinned, 0), COALESCE(s.muted, 0),
       COALESCE(s.sort_order, 0), COALESCE(folder.name, ''),
       COALESCE(f.collection_mode, 'app'), COALESCE(f.sync_interval_seconds, 7200),
       COALESCE(f.next_sync_at, ''), COALESCE(f.backfill_days, 30),
       COALESCE(f.retention_days, 90), COALESCE(f.retention_limit, 1000),
       COALESCE(f.auto_extract, 1), COALESCE(f.hidden, 0),
	   COALESCE(f.etag, ''), COALESCE(f.last_modified, ''), COALESCE(f.backfill_cursor, ''),
       COALESCE(f.gateway_status, ''), COALESCE(f.gateway_cursor, ''), COALESCE(f.gateway_has_more, 0),
       COALESCE((SELECT json_group_array(t.name)
                 FROM feed_subscription_tags st
                 JOIN subscription_tags t ON t.id = st.tag_id
                 WHERE st.source_id = f.id), '[]')
FROM feed_sources f
LEFT JOIN feed_subscriptions s ON s.source_id = f.id
LEFT JOIN feed_catalog_entries c ON c.source_id = f.id
LEFT JOIN subscription_folders folder ON folder.id = s.folder_id`
	args := make([]any, 0, len(countArgs)+2)
	args = append(args, countArgs...)
	if sourceID != "" {
		query += " WHERE f.id = ?"
		args = append(args, sourceID)
	}
	query += ` ORDER BY COALESCE(s.enabled, 0) DESC, COALESCE(s.pinned, 0) DESC,
COALESCE(s.sort_order, 0) ASC, f.is_default DESC, COALESCE(c.recommended, 0) DESC,
f.name ASC, f.id ASC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]FeedSource, 0, limit)
	for rows.Next() {
		var item FeedSource
		var isDefault, subscribed, formerlySubscribed, recommended, pinned, muted, autoExtract, hidden, hasIcon int
		var catalogTagsJSON, tagsJSON string
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.Name, &item.URL, &item.Description, &isDefault,
			&item.Status, &item.RecordsSeen, &item.RecordsUsed, &item.EntryCount, &item.UnreadCount, &item.LastSyncAt, &item.GatewayLastSuccessAt, &item.GatewayCachedCount, &item.LastError,
			&subscribed, &formerlySubscribed, &item.SiteURL, &item.Category, &item.Language, &item.IconText,
			&item.IconColor, &item.IconURL, &hasIcon, &catalogTagsJSON, &item.FeedFormat, &recommended, &item.Popularity,
			&item.ValidationStatus, &item.ValidatedAt, &item.CatalogOrigin, &item.FolderID,
			&item.TitleOverride, &pinned, &muted, &item.SortOrder, &item.FolderName,
			&item.CollectionMode, &item.SyncIntervalSecs, &item.NextSyncAt, &item.BackfillDays,
			&item.RetentionDays, &item.RetentionLimit, &autoExtract, &hidden, &item.ETag, &item.LastModified, &item.BackfillCursor,
			&item.GatewayStatus, &item.GatewayCursor, &item.GatewayHasMore,
			&tagsJSON,
		); err != nil {
			return nil, err
		}
		item.IsDefault = isDefault == 1
		item.Subscribed = subscribed == 1
		item.FormerlySubscribed = formerlySubscribed == 1
		item.Recommended = recommended == 1
		item.Pinned = pinned == 1
		item.Muted = muted == 1
		item.AutoExtract = autoExtract == 1
		item.Hidden = hidden == 1
		item.HasIcon = hasIcon == 1
		ensureSourceIcon(&item)
		// The legacy catalog column has no auditable public source. Keep the wire
		// field for compatibility, but never expose its seeded values as heat.
		item.Popularity = 0
		_ = json.Unmarshal([]byte(catalogTagsJSON), &item.CatalogTags)
		_ = json.Unmarshal([]byte(tagsJSON), &item.Tags)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Release the query connection before the content-type lookup below. The
	// dashboard allows several source reads in parallel and the SQLite pool is
	// intentionally small; holding this rows cursor while issuing another
	// query can otherwise exhaust all four connections and deadlock readers.
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if includeContentTypes {
		if err := attachSourceContentTypes(items); err != nil {
			return nil, err
		}
	} else {
		// Keep the catalog useful (badges and facets still render) without
		// turning the initial directory request into a full news-table scan.
		attachCatalogContentTypes(items)
	}
	return items, nil
}

var sourceIconPalette = [...]string{
	"#2563eb", "#0f766e", "#b45309", "#7c3aed", "#be123c", "#0369a1", "#4d7c0f", "#c2410c",
}

func ensureSourceIcon(source *FeedSource) {
	if strings.TrimSpace(source.IconText) == "" {
		source.IconText = firstRune(firstNonEmpty(source.Name, source.ID, "RSS"))
		source.IconColor = sourceIconColor(source.ID)
		return
	}
	if !validHexColor(source.IconColor) {
		source.IconColor = sourceIconColor(source.ID)
	}
}

func sourceIconColor(sourceID string) string {
	digest := sha256.Sum256([]byte(sourceID))
	return sourceIconPalette[int(digest[0])%len(sourceIconPalette)]
}

func attachSourceContentTypes(items []FeedSource) error {
	observed := make(map[string]map[string]struct{}, len(items))
	rows, err := db.Query(`SELECT source_id, COALESCE(NULLIF(view_type, ''), 'article')
FROM news WHERE source_id <> '' GROUP BY source_id, view_type`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, rawType string
		if err := rows.Scan(&sourceID, &rawType); err != nil {
			return err
		}
		contentType := normalizeSourceContentType(rawType)
		if observed[sourceID] == nil {
			observed[sourceID] = make(map[string]struct{})
		}
		observed[sourceID][contentType] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range items {
		items[index].ContentTypes = contentTypesForSource(items[index], observed[items[index].ID])
	}
	return nil
}

// attachCatalogContentTypes intentionally relies only on bounded source-row
// metadata.  It is used for include_counts=0, where a full GROUP BY over news
// would defeat the endpoint's lightweight projection.  Actual entry types are
// still loaded whenever a caller asks for content_type filtering or uses the
// compatibility queryFeedSources helpers.
func attachCatalogContentTypes(items []FeedSource) {
	for index := range items {
		items[index].ContentTypes = contentTypesForSource(items[index], nil)
	}
}

func contentTypesForSource(source FeedSource, observed map[string]struct{}) []string {
	kinds := map[string]struct{}{"article": {}}
	for kind := range observed {
		kinds[kind] = struct{}{}
	}

	// Catalog metadata is stable and bounded, so it is safe to use as a hint
	// for the lightweight directory projection.  Keep the same explicit
	// vocabulary as normalizeSourceContentType and avoid treating arbitrary
	// API/JSON feeds as social or video without a provider signal.
	hints := make([]string, 0, 8+len(source.CatalogTags))
	hints = append(hints, source.ID, source.Kind, source.Category, source.URL, source.SiteURL, source.FeedFormat)
	hints = append(hints, source.CatalogTags...)
	hint := strings.ToLower(strings.Join(hints, " "))
	add := func(kind string) { kinds[kind] = struct{}{} }
	switch {
	case strings.Contains(hint, "youtube") || strings.Contains(hint, "youtu.be") ||
		strings.HasPrefix(strings.ToLower(source.ID), "youtube-"):
		add("video")
	case strings.Contains(hint, "bilibili") || strings.Contains(hint, "b23.tv") ||
		strings.HasPrefix(strings.ToLower(source.ID), "b23-"):
		add("video")
	}
	if strings.Contains(hint, "podcast") || strings.Contains(hint, "audio") || strings.Contains(hint, "音频") {
		add("audio")
	}
	if strings.Contains(hint, "social") || strings.Contains(hint, "社交") ||
		strings.Contains(strings.ToLower(source.Category), "social-media") ||
		strings.Contains(strings.ToLower(source.SiteURL), "x.com/") ||
		strings.Contains(strings.ToLower(source.SiteURL), "twitter.com/") {
		add("social")
	}
	if strings.Contains(hint, "photo") || strings.Contains(hint, "image") || strings.Contains(hint, "图片") {
		add("picture")
	}
	if strings.Contains(hint, "notification") || strings.Contains(hint, "alert") || strings.Contains(hint, "通知") {
		add("notification")
	}

	order := []string{"article", "social", "picture", "video", "audio", "notification"}
	result := make([]string, 0, len(order))
	for _, kind := range order {
		if _, ok := kinds[kind]; ok {
			result = append(result, kind)
		}
	}
	return result
}

func normalizeSourceContentType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "social", "social_media":
		return "social"
	case "picture", "image", "photo":
		return "picture"
	case "video":
		return "video"
	case "audio", "podcast":
		return "audio"
	case "notification", "alert":
		return "notification"
	default:
		return "article"
	}
}

func parseSourceQueryOptions(r *http.Request) (sourceQueryOptions, error) {
	values := r.URL.Query()
	options := sourceQueryOptions{
		SourceID:    strings.TrimSpace(values.Get("source_id")),
		Query:       strings.ToLower(strings.TrimSpace(values.Get("q"))),
		Category:    strings.ToLower(strings.TrimSpace(values.Get("category"))),
		Language:    strings.ToLower(strings.TrimSpace(values.Get("language"))),
		ContentType: strings.ToLower(strings.TrimSpace(values.Get("content_type"))),
		Sort:        strings.ToLower(strings.TrimSpace(values.Get("sort"))),
		// App Host and the standalone UI historically used `subscribed`; the
		// typed Agent operation uses the explicit `only_subscribed` spelling.
		// Accept both so old clients keep working while the new operation can
		// remain self-describing.
		OnlySubscribed: values.Get("subscribed") == "1" || values.Get("only_subscribed") == "1",
		IncludeCounts:  values.Get("include_counts") != "0",
	}
	if raw := values.Get("include_counts"); raw != "" && raw != "0" && raw != "1" {
		return sourceQueryOptions{}, errors.New("来源计数参数不合法")
	}
	if !validText(options.SourceID, 128, false) || !validText(options.Query, 120, false) || !validText(options.Category, 80, false) || !validText(options.Language, 32, false) {
		return sourceQueryOptions{}, errors.New("来源筛选条件过长")
	}
	if options.ContentType != "" && options.ContentType != "all" {
		allowed := map[string]bool{"article": true, "social": true, "picture": true, "video": true, "audio": true, "notification": true}
		if !allowed[options.ContentType] {
			return sourceQueryOptions{}, errors.New("内容类型筛选不受支持")
		}
	}
	if options.Sort == "" {
		options.Sort = "recommended"
	}
	if options.Sort != "recommended" && options.Sort != "activity" && options.Sort != "name" {
		return sourceQueryOptions{}, errors.New("来源排序方式不受支持")
	}
	return options, nil
}

func filterAndSortFeedSources(sources []FeedSource, options sourceQueryOptions) []FeedSource {
	filtered := make([]FeedSource, 0, len(sources))
	for _, source := range sources {
		if options.OnlySubscribed && !source.Subscribed || options.Category != "" && strings.ToLower(source.Category) != options.Category || options.Language != "" && strings.ToLower(source.Language) != options.Language {
			continue
		}
		if options.ContentType != "" && options.ContentType != "all" && !containsString(source.ContentTypes, options.ContentType) {
			continue
		}
		if options.Query != "" {
			haystack := strings.ToLower(strings.Join(append([]string{source.Name, source.Description, source.Category, source.Language}, source.CatalogTags...), " "))
			if !strings.Contains(haystack, options.Query) {
				continue
			}
		}
		filtered = append(filtered, source)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := filtered[i], filtered[j]
		switch options.Sort {
		case "activity":
			if left.LastSyncAt != right.LastSyncAt {
				return left.LastSyncAt > right.LastSyncAt
			}
			if left.RecordsSeen != right.RecordsSeen {
				return left.RecordsSeen > right.RecordsSeen
			}
		case "recommended":
			if left.Recommended != right.Recommended {
				return left.Recommended
			}
			if left.IsDefault != right.IsDefault {
				return left.IsDefault
			}
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})
	return filtered
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func subscriptionState(sourceID string) (subscriptionStateValue, error) {
	var value subscriptionStateValue
	var pinned, muted int
	err := db.QueryRow(`SELECT folder_id, title_override, pinned, muted, sort_order
FROM feed_subscriptions WHERE source_id = ?`, sourceID).Scan(
		&value.FolderID, &value.TitleOverride, &pinned, &muted, &value.SortOrder,
	)
	value.Pinned = pinned == 1
	value.Muted = muted == 1
	return value, err
}

func folderExists(id string) bool {
	if id == "" {
		return true
	}
	var found string
	return db.QueryRow(`SELECT id FROM subscription_folders WHERE id = ?`, id).Scan(&found) == nil
}

func stableObjectID(prefix, value string) string {
	digest := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(value))))
	return prefix + "-" + hex.EncodeToString(digest[:8])
}

func normalizePublicURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !validPublicFeedURL(value) {
		return "", errors.New("invalid public URL")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), nil
}

func folderHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ID        string `json:"id,omitempty"`
		Name      string `json:"name,omitempty"`
		SortOrder int    `json:"sort_order,omitempty"`
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT f.id, f.name, f.sort_order, COUNT(s.source_id)
FROM subscription_folders f LEFT JOIN feed_subscriptions s ON s.folder_id = f.id AND s.enabled = 1
GROUP BY f.id, f.name, f.sort_order ORDER BY f.sort_order, f.name`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		items := make([]SubscriptionFolder, 0, 16)
		for rows.Next() {
			var item SubscriptionFolder
			if err := rows.Scan(&item.ID, &item.Name, &item.SortOrder, &item.Count); err != nil {
				app.HTTPErr(w, err)
				return
			}
			items = append(items, item)
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"folders": items})
	case http.MethodPost, http.MethodPatch:
		var req request
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "分组数据格式错误")
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if !validText(req.Name, 80, true) || req.SortOrder < -100000 || req.SortOrder > 100000 {
			app.UserErr(w, http.StatusBadRequest, "分组字段不合法")
			return
		}
		if r.Method == http.MethodPost {
			req.ID = stableObjectID("folder", req.Name)
		} else if !validText(req.ID, 80, true) {
			app.UserErr(w, http.StatusBadRequest, "分组标识不合法")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := db.Exec(`INSERT INTO subscription_folders(id, name, sort_order, created_at, updated_at)
VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,
sort_order=excluded.sort_order, updated_at=excluded.updated_at`, req.ID, req.Name, req.SortOrder, now, now)
		if err != nil {
			app.UserErr(w, http.StatusConflict, "分组名称已存在")
			return
		}
		app.WriteJSON(w, http.StatusOK, SubscriptionFolder{ID: req.ID, Name: req.Name, SortOrder: req.SortOrder})
	case http.MethodDelete:
		var req request
		if err := app.DecodeStrictJSON(w, r, 2<<10, &req); err != nil || !validText(req.ID, 80, true) {
			app.UserErr(w, http.StatusBadRequest, "分组标识不合法")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`UPDATE feed_subscriptions SET folder_id = '', updated_at = ? WHERE folder_id = ?`, time.Now().UTC().Format(time.RFC3339), req.ID); err == nil {
			_, err = tx.Exec(`DELETE FROM subscription_folders WHERE id = ?`, req.ID)
		}
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func tagHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		ID       string `json:"id,omitempty"`
		Name     string `json:"name,omitempty"`
		Color    string `json:"color,omitempty"`
		SourceID string `json:"source_id,omitempty"`
		Enabled  *bool  `json:"enabled,omitempty"`
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT t.id, t.name, t.color, COUNT(st.source_id)
FROM subscription_tags t LEFT JOIN feed_subscription_tags st ON st.tag_id = t.id
GROUP BY t.id, t.name, t.color ORDER BY t.name`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		items := make([]SubscriptionTag, 0, 16)
		for rows.Next() {
			var item SubscriptionTag
			if err := rows.Scan(&item.ID, &item.Name, &item.Color, &item.Count); err != nil {
				app.HTTPErr(w, err)
				return
			}
			items = append(items, item)
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"tags": items})
	case http.MethodPost, http.MethodPatch:
		var req request
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "标签数据格式错误")
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.ID == "" {
			if !validText(req.Name, 60, true) {
				app.UserErr(w, http.StatusBadRequest, "标签名称不合法")
				return
			}
			req.ID = stableObjectID("tag", req.Name)
		}
		if !validText(req.ID, 80, true) || req.Color != "" && !validHexColor(req.Color) {
			app.UserErr(w, http.StatusBadRequest, "标签字段不合法")
			return
		}
		if req.Color == "" {
			req.Color = "#64748b"
		}
		now := time.Now().UTC().Format(time.RFC3339)
		if req.Name != "" {
			if _, err := db.Exec(`INSERT INTO subscription_tags(id, name, color, created_at, updated_at)
VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, color=excluded.color,
updated_at=excluded.updated_at`, req.ID, req.Name, req.Color, now, now); err != nil {
				app.UserErr(w, http.StatusConflict, "标签名称已存在")
				return
			}
		}
		if req.SourceID != "" {
			if _, err := feedSourceByID(req.SourceID); err != nil {
				app.UserErr(w, http.StatusNotFound, "来源不存在")
				return
			}
			enabled := req.Enabled == nil || *req.Enabled
			var err error
			if enabled {
				_, err = db.Exec(`INSERT OR IGNORE INTO feed_subscription_tags(source_id, tag_id, created_at) VALUES(?,?,?)`, req.SourceID, req.ID, now)
			} else {
				_, err = db.Exec(`DELETE FROM feed_subscription_tags WHERE source_id = ? AND tag_id = ?`, req.SourceID, req.ID)
			}
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
		app.WriteJSON(w, http.StatusOK, SubscriptionTag{ID: req.ID, Name: req.Name, Color: req.Color})
	case http.MethodDelete:
		var req request
		if err := app.DecodeStrictJSON(w, r, 2<<10, &req); err != nil || !validText(req.ID, 80, true) {
			app.UserErr(w, http.StatusBadRequest, "标签标识不合法")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer tx.Rollback()
		if req.SourceID != "" {
			_, err = tx.Exec(`DELETE FROM feed_subscription_tags WHERE source_id = ? AND tag_id = ?`, req.SourceID, req.ID)
		} else if _, err = tx.Exec(`DELETE FROM feed_subscription_tags WHERE tag_id = ?`, req.ID); err == nil {
			_, err = tx.Exec(`DELETE FROM subscription_tags WHERE id = ?`, req.ID)
		}
		if err != nil || tx.Commit() != nil {
			app.HTTPErr(w, errors.New("标签删除失败"))
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func validHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 24)
	return err == nil
}

func sourcePackHandler(w http.ResponseWriter, r *http.Request) {
	type request struct {
		PackID string `json:"pack_id"`
	}
	if r.Method == http.MethodGet {
		rows, err := db.Query(`SELECT id, name, description, category, icon_text FROM source_packs ORDER BY sort_order, name`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		packs := make([]SourcePack, 0, 8)
		packIDs := make([]string, 0, 8)
		for rows.Next() {
			var pack SourcePack
			if err := rows.Scan(&pack.ID, &pack.Name, &pack.Description, &pack.Category, &pack.IconText); err != nil {
				rows.Close()
				app.HTTPErr(w, err)
				return
			}
			packIDs = append(packIDs, pack.ID)
			packs = append(packs, pack)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			app.HTTPErr(w, err)
			return
		}
		if err := rows.Close(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		for index, packID := range packIDs {
			memberRows, err := db.Query(`SELECT source_id FROM source_pack_members WHERE pack_id = ? ORDER BY sort_order`, packID)
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			memberIDs := make([]string, 0, 20)
			for memberRows.Next() {
				var sourceID string
				if err := memberRows.Scan(&sourceID); err != nil {
					memberRows.Close()
					app.HTTPErr(w, err)
					return
				}
				memberIDs = append(memberIDs, sourceID)
			}
			if err := memberRows.Err(); err != nil {
				memberRows.Close()
				app.HTTPErr(w, err)
				return
			}
			memberRows.Close()
			for _, sourceID := range memberIDs {
				if source, err := feedSourceByID(sourceID); err == nil {
					packs[index].Sources = append(packs[index].Sources, source)
				}
			}
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"packs": packs})
		return
	}
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req request
	if err := app.DecodeStrictJSON(w, r, 2<<10, &req); err != nil || !validText(req.PackID, 80, true) {
		app.UserErr(w, http.StatusBadRequest, "订阅包格式错误")
		return
	}
	rows, err := db.Query(`SELECT source_id FROM source_pack_members WHERE pack_id = ? ORDER BY sort_order`, req.PackID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	ids := make([]string, 0, 20)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			app.HTTPErr(w, err)
			return
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		app.UserErr(w, http.StatusNotFound, "订阅包不存在")
		return
	}
	if err := setSubscriptions(ids, "subscribe", ""); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "source_ids": ids})
}

type bulkSubscriptionRequest struct {
	SourceIDs []string `json:"source_ids"`
	Operation string   `json:"operation"`
	FolderID  string   `json:"folder_id,omitempty"`
}

func bulkSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req bulkSubscriptionRequest
	if err := app.DecodeStrictJSON(w, r, 16<<10, &req); err != nil || len(req.SourceIDs) == 0 || len(req.SourceIDs) > 100 {
		app.UserErr(w, http.StatusBadRequest, "批量订阅格式错误")
		return
	}
	if req.FolderID != "" && !folderExists(req.FolderID) {
		app.UserErr(w, http.StatusBadRequest, "订阅分组不存在")
		return
	}
	if err := setSubscriptions(req.SourceIDs, req.Operation, req.FolderID); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(req.SourceIDs)})
}

func setSubscriptions(sourceIDs []string, operation, folderID string) error {
	allowed := map[string]bool{"subscribe": true, "unsubscribe": true, "pin": true, "unpin": true, "mute": true, "unmute": true, "move": true}
	if !allowed[operation] {
		return errors.New("批量操作不受支持")
	}
	seen := make(map[string]struct{}, len(sourceIDs))
	ids := make([]string, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		if !validText(id, 128, true) {
			return errors.New("来源标识不合法")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		if _, err := feedSourceByID(id); err != nil {
			return errors.New("来源不存在")
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range ids {
		if _, err = tx.Exec(`INSERT OR IGNORE INTO feed_subscriptions
(source_id, enabled, subscribed_at, updated_at, folder_id, title_override, pinned, muted, sort_order)
VALUES(?,0,?,?,'','',0,0,0)`, id, now, now); err != nil {
			return err
		}
		switch operation {
		case "subscribe", "unsubscribe":
			_, err = tx.Exec(`UPDATE feed_subscriptions SET enabled = ?, updated_at = ? WHERE source_id = ?`, boolInt(operation == "subscribe"), now, id)
		case "pin", "unpin":
			_, err = tx.Exec(`UPDATE feed_subscriptions SET pinned = ?, updated_at = ? WHERE source_id = ?`, boolInt(operation == "pin"), now, id)
		case "mute", "unmute":
			_, err = tx.Exec(`UPDATE feed_subscriptions SET muted = ?, updated_at = ? WHERE source_id = ?`, boolInt(operation == "mute"), now, id)
		case "move":
			_, err = tx.Exec(`UPDATE feed_subscriptions SET folder_id = ?, updated_at = ? WHERE source_id = ?`, folderID, now, id)
		}
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if operation == "subscribe" {
		for _, id := range ids {
			if _, err := enqueueSyncRequest(id); err != nil {
				return err
			}
		}
	} else if operation == "unsubscribe" {
		for _, id := range ids {
			if err := cancelPendingSyncRequests(id); err != nil {
				return err
			}
		}
	}
	if feedSyncRuntimeEnabled && (operation == "subscribe" || operation == "unsubscribe") {
		for _, id := range ids {
			go forwardGatewaySubscription(id, operation == "subscribe")
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func classifyDiscoveryInput(raw string) (classifiedDiscoveryInput, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return classifiedDiscoveryInput{}, errors.New("请输入关键词、网站地址、Feed 地址或 RSSHub route")
	}
	if len(value) > 1000 || strings.ContainsAny(value, "\x00\r\n") {
		return classifiedDiscoveryInput{}, errors.New("输入内容过长或包含不支持的字符")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "rsshub:") || strings.HasPrefix(value, "/") {
		normalized, err := normalizeRSSHubRoute(value)
		if err != nil {
			return classifiedDiscoveryInput{}, err
		}
		return classifiedDiscoveryInput{
			Kind:       discoveryInputRSSHub,
			Raw:        value,
			Normalized: normalized,
			Message:    "将通过公开 RSSHub route 尝试发现；公共实例可能限流，失败时可改用自托管实例或原始 Feed",
		}, nil
	}
	if parsed, err := url.ParseRequestURI(value); err == nil && parsed.IsAbs() {
		normalized, normalizeErr := normalizePublicURL(value)
		if normalizeErr != nil {
			return classifiedDiscoveryInput{}, errors.New("请输入不含凭据、私网地址或敏感查询参数的公开 http/https 地址")
		}
		kind := discoveryInputWebsite
		if isWeChatArticleURL(parsed) {
			kind = discoveryInputWeChat
		} else if strings.EqualFold(parsed.Hostname(), "rsshub.app") {
			kind = discoveryInputRSSHub
		} else if looksLikeFeedURL(parsed) {
			kind = discoveryInputFeed
		}
		message := "将请求当前 Agent 验证网站声明的 RSS/Atom，并在确认前显示预览"
		if kind == discoveryInputWeChat {
			message = "将识别微信公众号和文章预览；只有持续订阅源验证通过后才可订阅"
		} else if kind == discoveryInputFeed {
			message = "将请求当前 Agent 验证 Feed 格式和预览，确认后才订阅"
		} else if kind == discoveryInputRSSHub {
			message = "将尝试公开 RSSHub route；公共实例失败时不会影响已保存内容"
		}
		return classifiedDiscoveryInput{Kind: kind, Raw: value, Normalized: normalized, Message: message}, nil
	}
	if looksLikeURLWithoutScheme(value) {
		return classifiedDiscoveryInput{}, errors.New("网站或 Feed 地址需要以 http:// 或 https:// 开头")
	}
	if !validText(value, 120, true) {
		return classifiedDiscoveryInput{}, errors.New("搜索关键词不能超过 120 个字符")
	}
	return classifiedDiscoveryInput{
		Kind:    discoveryInputKeyword,
		Raw:     value,
		Query:   value,
		Message: "仅搜索本地维护的公开来源目录，不会创建订阅或联网",
	}, nil
}

func isWeChatArticleURL(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Hostname(), "mp.weixin.qq.com") {
		return false
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return path == "/s" || strings.HasPrefix(path, "/s/")
}

// Sogou's public WeChat route returns bounded article summaries and expiring
// redirect URLs. Keep those links distinct from direct mp.weixin pages so the
// reader never assumes the anti-crawler redirect is a full-text source.
func isSogouWechatArticleURL(parsed *url.URL) bool {
	if parsed == nil || !strings.EqualFold(parsed.Hostname(), "weixin.sogou.com") {
		return false
	}
	if strings.TrimRight(parsed.EscapedPath(), "/") != "/link" || parsed.Fragment != "" {
		return false
	}
	values := parsed.Query()
	return len(values["url"]) == 1 && strings.TrimSpace(values.Get("url")) != ""
}

func looksLikeFeedURL(parsed *url.URL) bool {
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".rss") || strings.HasSuffix(path, ".xml") || strings.HasSuffix(path, ".atom") || strings.HasSuffix(path, ".json") {
		return true
	}
	for _, marker := range []string{"/feed", "/feeds/", "/rss", "/atom", "/podcast"} {
		if strings.Contains(path, marker) {
			return true
		}
	}
	return false
}

func looksLikeURLWithoutScheme(value string) bool {
	if strings.ContainsAny(value, " \t") {
		return false
	}
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "www.") || strings.Contains(value, ".") && (strings.Contains(value, "/") || !strings.Contains(value, ":"))
}

func normalizeRSSHubRoute(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "rsshub://"):
		value = value[len("rsshub://"):]
	case strings.HasPrefix(lower, "rsshub:"):
		value = value[len("rsshub:"):]
	}
	value = strings.TrimLeft(value, "/")
	if value == "" || len(value) > 800 || strings.ContainsAny(value, "\x00\r\n\\") {
		return "", errors.New("RSSHub route 不能为空，示例：rsshub:/github/trending/daily")
	}
	pathPart := value
	if index := strings.IndexByte(pathPart, '?'); index >= 0 {
		pathPart = pathPart[:index]
	}
	decoded, err := url.PathUnescape(pathPart)
	if err != nil || strings.Contains(decoded, "..") || strings.Contains(pathPart, "//") {
		return "", errors.New("RSSHub route 包含不安全的路径片段")
	}
	candidate := "https://rsshub.app/" + value
	normalized, err := normalizePublicURL(candidate)
	if err != nil {
		return "", errors.New("RSSHub route 格式不正确或包含敏感参数")
	}
	return normalized, nil
}

func sourcePreviewHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	sourceID := strings.TrimSpace(r.URL.Query().Get("source_id"))
	if !validText(sourceID, 128, true) {
		app.UserErr(w, http.StatusBadRequest, "来源标识不合法")
		return
	}
	source, err := feedSourceByID(sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "来源不存在")
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	rows, err := db.Query(`SELECT id,external_id,title,url,source,source_id,origin,source_type,view_type,author,
published_at,updated_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at
FROM news WHERE source_id = ? ORDER BY published_at DESC, updated_at DESC LIMIT ?`, sourceID, maxSourcePreviewItems)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer rows.Close()
	items := make([]NewsItem, 0, maxSourcePreviewItems)
	for rows.Next() {
		var item NewsItem
		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Title, &item.URL, &item.Source, &item.SourceID, &item.Origin,
			&item.SourceType, &item.ViewType, &item.Author, &item.PublishedAt, &item.UpdatedAt, &item.SummaryText,
			&item.ContentFormat, &item.SafeHTML, &item.ThumbnailURL, &item.MediaURL, &item.MediaMIME,
			&item.DurationSecs, &item.ContentHash, &item.FetchedAt); err != nil {
			rows.Close()
			app.HTTPErr(w, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		app.HTTPErr(w, err)
		return
	}
	if err := rows.Close(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	previewOrigin := "local_sqlite"
	previewError := ""
	app.WriteJSON(w, http.StatusOK, map[string]any{
		"source":                      source,
		"preview":                     items,
		"preview_origin":              previewOrigin,
		"preview_error":               limitText(previewError, 500),
		"public_popularity_available": false,
	})
}

func validateDiscoveries(items []FeedDiscovery) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !validText(item.ID, 128, true) || !uniqueString(item.ID, seen) || !validPublicFeedURL(item.InputURL) || !validPublicFeedURL(item.NormalizedURL) || !validText(item.Title, 240, false) || !validText(item.Description, 1000, false) || !validOptionalPublicURL(item.SiteURL) || !validOptionalPublicURL(item.IconURL) || !validText(item.ErrorMessage, 500, false) || !validText(item.DiscoveredAt, 80, true) || !validText(item.ConfirmedAt, 80, false) {
			return errors.New("来源发现字段不合法")
		}
		if item.Status != "verified" && item.Status != "failed" && item.Status != "pending" {
			return errors.New("来源发现状态不合法")
		}
		if item.Status == "verified" && !validPublicFeedURL(item.FeedURL) {
			return errors.New("来源发现地址不合法")
		}
		if item.FeedURL != "" && !validPublicFeedURL(item.FeedURL) {
			return errors.New("来源发现地址不合法")
		}
		if item.FeedFormat != "" && item.FeedFormat != "rss" && item.FeedFormat != "atom" && item.FeedFormat != "json" && item.FeedFormat != "youtube" {
			return errors.New("来源发现格式不合法")
		}
		if len(item.Preview) > 5 || validateNews(item.Preview) != nil {
			return errors.New("来源预览不合法")
		}
	}
	return nil
}

func uniqueString(value string, seen map[string]struct{}) bool {
	if _, ok := seen[value]; ok {
		return false
	}
	seen[value] = struct{}{}
	return true
}

func validOptionalPublicURL(raw string) bool {
	return raw == "" || validPublicFeedURL(raw)
}

func upsertDiscoveriesTx(tx *sql.Tx, items []FeedDiscovery, now string) error {
	for _, item := range items {
		if item.Status != "pending" {
			if _, err := tx.Exec(`DELETE FROM feed_discovery_candidates
WHERE normalized_url = ? AND status = 'pending'`, item.NormalizedURL); err != nil {
				return err
			}
		}
		preview, err := json.Marshal(item.Preview)
		if err != nil {
			return err
		}
		discoveredAt := item.DiscoveredAt
		if discoveredAt == "" {
			discoveredAt = now
		}
		_, err = tx.Exec(`INSERT INTO feed_discovery_candidates
(id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,feed_format,status,error_message,preview_json,discovered_at,confirmed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET input_url=excluded.input_url, normalized_url=excluded.normalized_url,
feed_url=excluded.feed_url, title=excluded.title, description=excluded.description,
site_url=excluded.site_url, icon_url=excluded.icon_url, feed_format=excluded.feed_format,
status=excluded.status, error_message=excluded.error_message, preview_json=excluded.preview_json,
discovered_at=excluded.discovered_at`, item.ID, item.InputURL, item.NormalizedURL, item.FeedURL,
			item.Title, item.Description, item.SiteURL, item.IconURL, firstNonEmpty(item.FeedFormat, "rss"),
			item.Status, item.ErrorMessage, preview, discoveredAt, item.ConfirmedAt)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(`DELETE FROM feed_discovery_candidates WHERE id NOT IN
(SELECT id FROM feed_discovery_candidates ORDER BY discovered_at DESC LIMIT ?)`, maxDiscoveryCandidates)
	return err
}

func discoveryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		rows, err := db.Query(`SELECT id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,
feed_format,status,error_message,preview_json,discovered_at,confirmed_at
FROM feed_discovery_candidates ORDER BY discovered_at DESC LIMIT ?`, maxDiscoveryCandidates)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		items := make([]FeedDiscovery, 0, maxDiscoveryCandidates)
		for rows.Next() {
			var item FeedDiscovery
			var preview string
			if err := rows.Scan(&item.ID, &item.InputURL, &item.NormalizedURL, &item.FeedURL,
				&item.Title, &item.Description, &item.SiteURL, &item.IconURL, &item.FeedFormat,
				&item.Status, &item.ErrorMessage, &preview, &item.DiscoveredAt, &item.ConfirmedAt); err != nil {
				app.HTTPErr(w, err)
				return
			}
			item.ErrorMessage = publicDiscoveryError(item)
			_ = json.Unmarshal([]byte(preview), &item.Preview)
			items = append(items, item)
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"discoveries": items})
		return
	}
	if r.Method == http.MethodPost {
		var req struct {
			Input    string `json:"input,omitempty"`
			InputURL string `json:"input_url"`
		}
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "来源发现请求格式错误")
			return
		}
		classified, err := classifyDiscoveryInput(firstNonEmpty(req.Input, req.InputURL))
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if classified.Kind == discoveryInputKeyword {
			sources, queryErr := queryFeedSources(maxFeedSourceQueryLimit, "")
			if queryErr != nil {
				app.HTTPErr(w, queryErr)
				return
			}
			filtered := filterAndSortFeedSources(sources, sourceQueryOptions{Query: strings.ToLower(classified.Query), Sort: "recommended"})
			app.WriteJSON(w, http.StatusOK, map[string]any{
				"input_kind":     classified.Kind,
				"query":          classified.Query,
				"message":        classified.Message,
				"sources":        filtered,
				"requires_agent": false,
			})
			return
		}
		if classified.Kind == discoveryInputWeChat || classified.Kind == discoveryInputRSSHub {
			app.UserErr(w, http.StatusBadRequest, "该兼容来源由维护目录提供，请从来源目录选择")
			return
		}
		normalizedURL := classified.Normalized
		now := time.Now().UTC().Format(time.RFC3339)
		item := FeedDiscovery{
			ID:            stableObjectID("discovery-request", normalizedURL),
			InputURL:      normalizedURL,
			NormalizedURL: normalizedURL,
			FeedFormat:    "rss",
			Status:        "pending",
			DiscoveredAt:  now,
		}
		ctx, cancel := context.WithTimeout(r.Context(), feedFetchTimeout)
		defer cancel()
		var response feedFetchResponse
		var fetchErr error
		if feedSyncRuntimeEnabled {
			response, fetchErr = localCustomFeedFetch(ctx, normalizedURL, "", "")
		} else {
			fetchErr = errors.New("等待应用运行时验证公开来源")
		}
		if fetchErr == nil {
			response, fetchErr = validateLocalCustomFeedResponse(response, normalizedURL)
		}
		if fetchErr == nil {
			feed, parseErr := parseFetchedFeedWithContext(ctx, response, normalizedURL)
			if parseErr == nil {
				feedURL, normalizeErr := normalizePublicURL(firstNonEmpty(feed.FeedURL, response.FinalURL, normalizedURL))
				if normalizeErr != nil {
					parseErr = errors.New("来源发现地址不安全")
				} else if feed.Format != "rss" && feed.Format != "atom" && feed.Format != "json" {
					parseErr = errors.New("来源格式不是 RSS、Atom 或 JSON Feed")
				} else {
					item.Status = "verified"
					item.FeedURL = feedURL
					item.Title = feed.Title
					item.Description = feed.Description
					item.SiteURL = feed.SiteURL
					item.IconURL = firstNonEmpty(feed.IconURL, sourceFaviconURL(feed.SiteURL))
					item.FeedFormat = feed.Format
					for index := range feed.Items {
						feed.Items[index].SourceID = sourceIDForURL(item.FeedURL)
						feed.Items[index].Source = item.Title
						feed.Items[index].SourceType = item.FeedFormat
						if len(item.Preview) < maxSourcePreviewItems {
							item.Preview = append(item.Preview, feed.Items[index])
						}
					}
				}
			}
			if parseErr != nil {
				item.ErrorMessage = limitText(errorText(parseErr), 500)
			}
		} else {
			item.ErrorMessage = limitText(errorText(fetchErr), 500)
		}
		if item.Status != "verified" && item.ErrorMessage == "" {
			item.ErrorMessage = "公开来源尚未验证"
		}
		preview, marshalErr := json.Marshal(item.Preview)
		if marshalErr != nil {
			app.HTTPErr(w, marshalErr)
			return
		}
		_, err = db.Exec(`INSERT INTO feed_discovery_candidates
	(id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,feed_format,status,error_message,preview_json,discovered_at,confirmed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET input_url=excluded.input_url, normalized_url=excluded.normalized_url,
feed_url=excluded.feed_url, title=excluded.title, description=excluded.description, site_url=excluded.site_url,
icon_url=excluded.icon_url, feed_format=excluded.feed_format, status=excluded.status, error_message=excluded.error_message,
preview_json=excluded.preview_json, discovered_at=excluded.discovered_at, confirmed_at=''`, item.ID, item.InputURL, item.NormalizedURL,
			item.FeedURL, item.Title, item.Description, item.SiteURL, item.IconURL, item.FeedFormat, item.Status, item.ErrorMessage, preview, item.DiscoveredAt, "")
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusAccepted, map[string]any{
			"id":               item.ID,
			"input_url":        item.InputURL,
			"normalized_url":   item.NormalizedURL,
			"feed_url":         item.FeedURL,
			"feed_format":      item.FeedFormat,
			"status":           item.Status,
			"discovered_at":    item.DiscoveredAt,
			"input_kind":       classified.Kind,
			"normalized_input": classified.Normalized,
			"message":          classified.Message,
			"requires_agent":   item.Status != "verified",
			"preview":          item.Preview,
			"title":            item.Title,
			"description":      item.Description,
			"site_url":         item.SiteURL,
			"icon_url":         item.IconURL,
			"error_message":    item.ErrorMessage,
		})
		return
	}
	if r.Method != http.MethodPatch {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := app.DecodeStrictJSON(w, r, 2<<10, &req); err != nil || !validText(req.ID, 128, true) {
		app.UserErr(w, http.StatusBadRequest, "来源发现标识不合法")
		return
	}
	source, err := confirmDiscovery(req.ID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "待确认来源不存在")
		return
	}
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	app.WriteJSON(w, http.StatusOK, source)
}

func publicDiscoveryError(item FeedDiscovery) string {
	message := strings.TrimSpace(item.ErrorMessage)
	parsed, _ := url.Parse(strings.TrimSpace(item.InputURL))
	if isWeChatArticleURL(parsed) && (strings.Contains(message, "poc_token") || strings.Contains(message, "wappoc_appmsgcaptcha")) {
		return "公众号页面需要验证；请重新发现该文章，应用会使用公众号专用识别流程"
	}
	return message
}

func confirmDiscovery(id string) (FeedSource, error) {
	var item FeedDiscovery
	var previewJSON string
	err := db.QueryRow(`SELECT id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,
feed_format,status,error_message,preview_json,discovered_at,confirmed_at
FROM feed_discovery_candidates WHERE id = ?`, id).Scan(&item.ID, &item.InputURL, &item.NormalizedURL,
		&item.FeedURL, &item.Title, &item.Description, &item.SiteURL, &item.IconURL, &item.FeedFormat,
		&item.Status, &item.ErrorMessage, &previewJSON, &item.DiscoveredAt, &item.ConfirmedAt)
	if err != nil {
		return FeedSource{}, err
	}
	if item.Status != "verified" {
		return FeedSource{}, errors.New("该地址尚未通过来源验证")
	}
	return confirmPrivateLocalDiscovery(item, previewJSON)
}

// The historical function name is retained to avoid a wide symbol rename. It
// only persists an already verified, credential-free public Feed in app mode;
// private_local remains unsupported.
func confirmPrivateLocalDiscovery(item FeedDiscovery, previewJSON string) (FeedSource, error) {
	feedURL, err := normalizePublicURL(item.FeedURL)
	if err != nil {
		return FeedSource{}, errors.New("来源地址不合法")
	}
	name := strings.TrimSpace(item.Title)
	if name == "" {
		name = urlHost(feedURL)
	}
	if !validText(name, 120, true) {
		return FeedSource{}, errors.New("来源名称不合法")
	}
	sourceID := sourceIDForURL(feedURL)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return FeedSource{}, err
	}
	defer tx.Rollback()
	// A discovered URL can already exist in the curated catalog (or have been
	// added earlier with a user-facing ID). Reuse that row and all of its local
	// metadata/preferences instead of inserting a second source.
	var existingID string
	err = tx.QueryRow(`SELECT id FROM feed_sources WHERE url = ?`, feedURL).Scan(&existingID)
	if err == nil {
		if existingID != sourceIDForURL(feedURL) {
			return FeedSource{}, errors.New(catalogSourceRequiresStableIDMessage)
		}
		sourceID = existingID
	} else if !errors.Is(err, sql.ErrNoRows) {
		return FeedSource{}, err
	}
	kind := item.FeedFormat
	if kind != "atom" && kind != "json" {
		kind = "rss"
	}
	if existingID == "" {
		_, err = tx.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at,
	collection_mode,sync_interval_seconds,next_sync_at,backfill_days,retention_days,retention_limit,auto_extract,hidden)
	VALUES(?,?,?,?,?,0,'ok',?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET kind=excluded.kind,
	name=excluded.name, url=excluded.url, description=excluded.description, status=excluded.status,
	collection_mode=excluded.collection_mode, sync_interval_seconds=excluded.sync_interval_seconds,
	backfill_days=excluded.backfill_days, retention_days=excluded.retention_days, retention_limit=excluded.retention_limit,
	auto_extract=excluded.auto_extract, hidden=excluded.hidden, updated_at=excluded.updated_at`, sourceID, kind,
			name, feedURL, item.Description, now, now, "app", 7200, now, 30, 90, 1000, 1, 0)
		if err != nil {
			return FeedSource{}, err
		}
	}
	if _, err = tx.Exec(`INSERT INTO feed_catalog_entries
(source_id,site_url,category,language,icon_text,icon_color,icon_url,tags_json,feed_format,recommended,popularity,validation_status,validated_at,catalog_origin)
VALUES(?,?, 'other','und',?,'#64748b',?,'[]',?,0,0,'verified',?,'user-discovered-public-feed')
ON CONFLICT(source_id) DO UPDATE SET
site_url=CASE WHEN excluded.site_url <> '' THEN excluded.site_url ELSE feed_catalog_entries.site_url END,
icon_url=CASE WHEN excluded.icon_url <> '' THEN excluded.icon_url ELSE feed_catalog_entries.icon_url END,
feed_format=excluded.feed_format, validation_status='verified', validated_at=excluded.validated_at`, sourceID, item.SiteURL,
		firstRune(name), item.IconURL, kind, now); err != nil {
		return FeedSource{}, err
	}
	if _, err = tx.Exec(`INSERT INTO feed_subscriptions
(source_id,enabled,subscribed_at,updated_at,folder_id,title_override,pinned,muted,sort_order)
VALUES(?,1,?,?,'','',0,0,0) ON CONFLICT(source_id) DO UPDATE SET enabled=1, updated_at=excluded.updated_at`, sourceID, now, now); err != nil {
		return FeedSource{}, err
	}
	var preview []NewsItem
	_ = json.Unmarshal([]byte(previewJSON), &preview)
	for _, news := range preview {
		news.SourceID = sourceID
		if news.ID == "" {
			news.ID = stableObjectID("entry", sourceID+"|"+news.ExternalID)
		}
		news.Source = name
		news.SourceType = firstNonEmpty(news.SourceType, kind)
		if news.Origin == "" {
			news.Origin = "discovery-preview"
		}
		if err = upsertFetchedNewsTx(tx, news, now); err != nil {
			return FeedSource{}, err
		}
	}
	if _, err = tx.Exec(`UPDATE feed_discovery_candidates SET confirmed_at = ? WHERE id = ?`, now, item.ID); err != nil {
		return FeedSource{}, err
	}
	if err = tx.Commit(); err != nil {
		return FeedSource{}, err
	}
	if _, err := enqueueSyncRequest(sourceID); err != nil {
		return FeedSource{}, err
	}
	return feedSourceByID(sourceID)
}

func urlHost(raw string) string {
	parsed, _ := url.Parse(raw)
	return parsed.Hostname()
}

func firstRune(value string) string {
	for _, r := range strings.TrimSpace(value) {
		return string(r)
	}
	return "R"
}

func completeSyncRequestsTx(tx *sql.Tx, input InputStatus, now string) error {
	status := "completed"
	nextRetry := ""
	if input.Status == "unavailable" {
		status = "retry_wait"
		nextRetry = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	}
	_, err := tx.Exec(`UPDATE sync_requests SET status = ?, completed_at = ?, message = ?,
attempt_count = attempt_count + 1, next_retry_at = ? WHERE source_id = ? AND status IN ('pending','retry_wait')`,
		status, now, input.Message, nextRetry, input.ID)
	return err
}

func bulkItemPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req struct {
		Keys      []string `json:"keys"`
		Read      *bool    `json:"read,omitempty"`
		Starred   *bool    `json:"starred,omitempty"`
		ReadLater *bool    `json:"read_later,omitempty"`
	}
	if err := app.DecodeStrictJSON(w, r, 32<<10, &req); err != nil || len(req.Keys) == 0 || len(req.Keys) > 200 || req.Read == nil && req.Starred == nil && req.ReadLater == nil {
		app.UserErr(w, http.StatusBadRequest, "批量条目状态格式错误")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[string]struct{}, len(req.Keys))
	for _, key := range req.Keys {
		if !validItemKey(key) {
			app.UserErr(w, http.StatusBadRequest, "条目标识不合法")
			return
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var read, starred, readLater int
		var progress float64
		err := tx.QueryRow(`SELECT is_read, starred, read_later, reading_progress FROM item_preferences WHERE item_key = ?`, key).Scan(&read, &starred, &readLater, &progress)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			app.HTTPErr(w, err)
			return
		}
		if req.Read != nil {
			read = boolInt(*req.Read)
		}
		if req.Starred != nil {
			starred = boolInt(*req.Starred)
		}
		if req.ReadLater != nil {
			readLater = boolInt(*req.ReadLater)
		}
		if _, err = tx.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at)
VALUES(?,?,?,?,?,?) ON CONFLICT(item_key) DO UPDATE SET is_read=excluded.is_read,
starred=excluded.starred, read_later=excluded.read_later, updated_at=excluded.updated_at`,
			key, read, starred, readLater, progress, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(seen)})
}

// sourceDisplayPrefHandler stores the per-source timeline display override
// (auto/article/video/gallery). "gallery" is accepted from day one so the
// schema does not churn when the picture view lands; today it renders as the
// video grid.
func sourceDisplayPrefHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT source_id, view_mode FROM source_display_prefs`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		prefs := make(map[string]string)
		for rows.Next() {
			var id, mode string
			if err := rows.Scan(&id, &mode); err != nil {
				app.HTTPErr(w, err)
				return
			}
			prefs[id] = mode
		}
		if err := rows.Err(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, prefs)
	case http.MethodPut:
		var req struct {
			SourceID string `json:"source_id"`
			ViewMode string `json:"view_mode"`
		}
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil ||
			!validSourceDisplayID(req.SourceID) {
			app.UserErr(w, http.StatusBadRequest, "来源标识不合法")
			return
		}
		switch req.ViewMode {
		case "auto", "article", "video", "gallery":
		default:
			app.UserErr(w, http.StatusBadRequest, "视图模式不合法")
			return
		}
		// "auto" is the absence of an override: delete instead of storing rows so
		// the table only ever holds explicit user choices.
		if req.ViewMode == "auto" {
			if _, err := db.Exec(`DELETE FROM source_display_prefs WHERE source_id = ?`, req.SourceID); err != nil {
				app.HTTPErr(w, err)
				return
			}
			app.WriteJSON(w, http.StatusOK, map[string]any{"source_id": req.SourceID, "view_mode": "auto"})
			return
		}
		if _, err := db.Exec(`INSERT INTO source_display_prefs(source_id, view_mode, updated_at) VALUES(?,?,?)
ON CONFLICT(source_id) DO UPDATE SET view_mode=excluded.view_mode, updated_at=excluded.updated_at`,
			req.SourceID, req.ViewMode, time.Now().UTC().Format(time.RFC3339)); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"source_id": req.SourceID, "view_mode": req.ViewMode})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func formatSourceSummary(source FeedSource) string {
	return fmt.Sprintf("%s (%s)", source.Name, source.URL)
}

func sortedUnique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

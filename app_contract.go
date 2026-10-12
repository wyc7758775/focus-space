package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"focus-space/internal/app"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	appCapabilityVersion = 1
	maxEntryQueryLimit   = 50
	maxEntryQueryOffset  = 10000
	entryCursorPrefix    = "k1."
	maxEntryWriteContent = 96 << 10
)

type entryPageCursor struct {
	PublishedAt string `json:"published_at"`
	UpdatedAt   string `json:"updated_at"`
	ID          string `json:"id"`
	Offset      int    `json:"offset"`
}

func encodeEntryPageCursor(cursor entryPageCursor) string {
	body, _ := json.Marshal(cursor)
	return entryCursorPrefix + base64.RawURLEncoding.EncodeToString(body)
}

func decodeEntryPageCursor(raw string) (entryPageCursor, bool) {
	var cursor entryPageCursor
	if len(raw) <= len(entryCursorPrefix) || len(raw) > 768 || !strings.HasPrefix(raw, entryCursorPrefix) {
		return cursor, false
	}
	body, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(raw, entryCursorPrefix))
	if err != nil || len(body) > 512 || json.Unmarshal(body, &cursor) != nil {
		return entryPageCursor{}, false
	}
	if !validText(cursor.ID, 256, false) || len(cursor.PublishedAt) > 80 || len(cursor.UpdatedAt) > 80 || cursor.Offset < 0 || cursor.Offset > maxEntryQueryOffset {
		return entryPageCursor{}, false
	}
	return cursor, true
}

// AppSettings is the device-local policy shared by the standalone UI and
// Agent calls. "device" means the AppHost process timezone; no account or
// cloud preference is involved.
type AppSettings struct {
	BriefEnabled                 bool   `json:"brief_enabled"`
	BriefHour                    int    `json:"brief_hour"`
	BriefMinute                  int    `json:"brief_minute"`
	BriefWeekdays                []int  `json:"brief_weekdays"`
	Timezone                     string `json:"timezone"`
	AutoExtractRecordings        bool   `json:"auto_extract_recordings"`
	AutoGenerateArticleOverviews bool   `json:"auto_generate_article_overviews"`
	ObsidianEnabled              bool   `json:"obsidian_enabled"`
	NotifyOverdue                bool   `json:"notify_overdue"`
	NotifyHighRisk               bool   `json:"notify_high_risk"`
	NotifyBlockers               bool   `json:"notify_blockers"`
	NotifySourceFailures         bool   `json:"notify_source_failures"`
	NotifyUrgent                 bool   `json:"notify_urgent"`
	UpdatedAt                    string `json:"updated_at"`
}

type appSettingsMutation struct {
	BriefEnabled                 *bool   `json:"brief_enabled,omitempty"`
	BriefHour                    *int    `json:"brief_hour,omitempty"`
	BriefMinute                  *int    `json:"brief_minute,omitempty"`
	BriefWeekdays                []int   `json:"brief_weekdays,omitempty"`
	Timezone                     *string `json:"timezone,omitempty"`
	AutoExtractRecordings        *bool   `json:"auto_extract_recordings,omitempty"`
	AutoGenerateArticleOverviews *bool   `json:"auto_generate_article_overviews,omitempty"`
	ObsidianEnabled              *bool   `json:"obsidian_enabled,omitempty"`
	NotifyOverdue                *bool   `json:"notify_overdue,omitempty"`
	NotifyHighRisk               *bool   `json:"notify_high_risk,omitempty"`
	NotifyBlockers               *bool   `json:"notify_blockers,omitempty"`
	NotifySourceFailures         *bool   `json:"notify_source_failures,omitempty"`
	NotifyUrgent                 *bool   `json:"notify_urgent,omitempty"`
}

type StandardEntry struct {
	NewsItem
	Revision        int      `json:"revision"`
	Confidence      string   `json:"confidence"`
	ConflictState   string   `json:"conflict_state,omitempty"`
	Evidence        []string `json:"evidence,omitempty"`
	DeletedAt       string   `json:"deleted_at,omitempty"`
	Read            bool     `json:"read"`
	Starred         bool     `json:"starred"`
	ReadLater       bool     `json:"read_later"`
	ReadingProgress float64  `json:"reading_progress"`
}

type entryWriteRequest struct {
	SchemaVersion    int      `json:"schema_version,omitempty"`
	ID               string   `json:"id,omitempty"`
	SourceID         string   `json:"source_id"`
	SourceType       string   `json:"source_type"`
	ExternalID       string   `json:"external_id"`
	Title            string   `json:"title"`
	URL              string   `json:"url,omitempty"`
	Source           string   `json:"source,omitempty"`
	Origin           string   `json:"origin,omitempty"`
	ViewType         string   `json:"view_type,omitempty"`
	Author           string   `json:"author,omitempty"`
	PublishedAt      string   `json:"published_at,omitempty"`
	SummaryText      string   `json:"summary_text,omitempty"`
	Content          string   `json:"content,omitempty"`
	ContentFormat    string   `json:"content_format,omitempty"`
	SafeHTML         string   `json:"safe_html,omitempty"`
	ThumbnailURL     string   `json:"thumbnail_url,omitempty"`
	MediaURL         string   `json:"media_url,omitempty"`
	MediaMIME        string   `json:"media_mime,omitempty"`
	DurationSecs     int      `json:"duration_seconds,omitempty"`
	ContentHash      string   `json:"content_hash,omitempty"`
	FetchedAt        string   `json:"fetched_at,omitempty"`
	PermissionScope  string   `json:"permission_scope,omitempty"`
	SourceLevel      string   `json:"source_level,omitempty"`
	FailureReason    string   `json:"failure_reason,omitempty"`
	ExpectedRevision int      `json:"expected_revision,omitempty"`
	Confidence       string   `json:"confidence,omitempty"`
	Evidence         []string `json:"evidence,omitempty"`
}

type entryStateRequest struct {
	ID               string   `json:"id"`
	ExpectedRevision int      `json:"expected_revision,omitempty"`
	Deleted          *bool    `json:"deleted,omitempty"`
	Read             *bool    `json:"read,omitempty"`
	Starred          *bool    `json:"starred,omitempty"`
	ReadLater        *bool    `json:"read_later,omitempty"`
	ReadingProgress  *float64 `json:"reading_progress,omitempty"`
}

func readerCacheNamespace(owner, profile string) string {
	sum := sha256.Sum256([]byte("plaud-action-dashboard\x00" + owner + "\x00" + profile))
	return hex.EncodeToString(sum[:])
}

func capabilityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{
		"version":          appCapabilityVersion,
		"storage":          "device_sqlite",
		"cache_namespace":  readerCacheNamespace(appDataScope.OwnerID, appDataScope.ProfileID),
		"standalone":       true,
		"account_required": false,
		"features": map[string]bool{
			"focus_space_decision_v2": true,
			"focus_decision_qa_v1":    true,
			"focus_result_bundle_v1":  true,
			"focus_blueprint_v1":      true,
			"focus_work_product_v1":   true,
			"reader_saved_filter_v1":  true,
		},
		"decision_run_statuses": append([]string(nil), decisionRunStatuses...),
		"operations": []string{
			"dashboard.get", "dashboard.write", "settings.get", "settings.update",
			"action.feedback", "action.schedule.begin", "action.schedule.receipt",
			"source.list", "source.create", "source.update", "source.preview",
			"discovery.list", "discovery.create", "discovery.confirm",
			"subscription.list", "subscription.set", "subscription.bulk", "sync.list", "sync.request",
			"entry.list", "entry.write", "entry.state", "entry.bulk_state", "entry.content", "context.query", "context.confirm", "context.remove",
			"recording.list", "recording.get", "recording.state", "recording.action_link", "recording.action_unlink",
			"folder.list", "folder.create", "folder.update", "folder.delete", "tag.list", "tag.create", "tag.update", "tag.delete", "pack.list", "pack.subscribe",
			"reader_settings.get", "reader_settings.update", "reader_counts.get", "module.update", "layout.update",
			"reader_focus.get", "reader_focus.update", "reader_recommendation.read",
			"reader_digest.read", "reader_digest.apply",
			"focus_space.get", "focus_space.update", "focus_attachment.import", "focus_analysis.read", "focus_analysis.apply", "focus_analysis.get", "focus_result.feedback",
			"focus_blueprint.get", "focus_blueprint.preview", "focus_blueprint.apply", "focus_blueprint.rollback",
			"focus_work_product.get", "focus_work_product.feedback", "focus_work_product.link",
			"decision_run.start", "decision_run.get", "decision_run.answer", "decision_run.resume", "decision_run.cancel",
			"decision_revision.apply", "decision_revision.get", "decision_revision.compare", "decision_feedback.apply", "decision_review.apply",
		},
		"limits": map[string]int{"request_bytes": maxBodyBytes, "entry_page": maxEntryQueryLimit},
	})
}

func settingsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := readAppSettings()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, settings)
	case http.MethodPatch:
		current, err := readAppSettings()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		var req appSettingsMutation
		if err := app.DecodeStrictJSON(w, r, 8<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "应用设置格式错误")
			return
		}
		applySettingsMutation(&current, req)
		if !validAppSettings(current) {
			app.UserErr(w, http.StatusBadRequest, "应用设置不合法")
			return
		}
		current.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		_, err = db.Exec(`UPDATE app_settings SET brief_enabled=?,brief_hour=?,brief_minute=?,brief_weekdays=?,timezone=?,auto_extract_recordings=?,auto_generate_article_overviews=?,obsidian_enabled=?,notify_overdue=?,notify_high_risk=?,notify_blockers=?,notify_source_failures=?,notify_urgent=?,updated_at=? WHERE id='default'`,
			boolInt(current.BriefEnabled), current.BriefHour, current.BriefMinute, joinWeekdays(current.BriefWeekdays), current.Timezone,
			boolInt(current.AutoExtractRecordings), boolInt(current.AutoGenerateArticleOverviews), boolInt(current.ObsidianEnabled), boolInt(current.NotifyOverdue), boolInt(current.NotifyHighRisk), boolInt(current.NotifyBlockers), boolInt(current.NotifySourceFailures), boolInt(current.NotifyUrgent), current.UpdatedAt)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, current)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func readAppSettings() (AppSettings, error) {
	var value AppSettings
	var weekdays string
	var brief, extract, overviews, obsidian, overdue, risk, blockers, failures, urgent int
	err := db.QueryRow(`SELECT brief_enabled,brief_hour,brief_minute,brief_weekdays,timezone,auto_extract_recordings,auto_generate_article_overviews,obsidian_enabled,notify_overdue,notify_high_risk,notify_blockers,notify_source_failures,notify_urgent,updated_at FROM app_settings WHERE id='default'`).Scan(
		&brief, &value.BriefHour, &value.BriefMinute, &weekdays, &value.Timezone, &extract, &overviews, &obsidian, &overdue, &risk, &blockers, &failures, &urgent, &value.UpdatedAt)
	if err != nil {
		return AppSettings{}, err
	}
	value.BriefEnabled = brief == 1
	value.AutoExtractRecordings = extract == 1
	value.AutoGenerateArticleOverviews = overviews == 1
	value.ObsidianEnabled = obsidian == 1
	value.NotifyOverdue = overdue == 1
	value.NotifyHighRisk = risk == 1
	value.NotifyBlockers = blockers == 1
	value.NotifySourceFailures = failures == 1
	value.NotifyUrgent = urgent == 1
	value.BriefWeekdays = parseWeekdays(weekdays)
	return value, nil
}

func applySettingsMutation(value *AppSettings, req appSettingsMutation) {
	if req.BriefEnabled != nil {
		value.BriefEnabled = *req.BriefEnabled
	}
	if req.BriefHour != nil {
		value.BriefHour = *req.BriefHour
	}
	if req.BriefMinute != nil {
		value.BriefMinute = *req.BriefMinute
	}
	if req.BriefWeekdays != nil {
		value.BriefWeekdays = append([]int(nil), req.BriefWeekdays...)
	}
	if req.Timezone != nil {
		value.Timezone = strings.TrimSpace(*req.Timezone)
	}
	if req.AutoExtractRecordings != nil {
		value.AutoExtractRecordings = *req.AutoExtractRecordings
	}
	if req.AutoGenerateArticleOverviews != nil {
		value.AutoGenerateArticleOverviews = *req.AutoGenerateArticleOverviews
	}
	if req.ObsidianEnabled != nil {
		value.ObsidianEnabled = *req.ObsidianEnabled
	}
	if req.NotifyOverdue != nil {
		value.NotifyOverdue = *req.NotifyOverdue
	}
	if req.NotifyHighRisk != nil {
		value.NotifyHighRisk = *req.NotifyHighRisk
	}
	if req.NotifyBlockers != nil {
		value.NotifyBlockers = *req.NotifyBlockers
	}
	if req.NotifySourceFailures != nil {
		value.NotifySourceFailures = *req.NotifySourceFailures
	}
	if req.NotifyUrgent != nil {
		value.NotifyUrgent = *req.NotifyUrgent
	}
}

func validAppSettings(value AppSettings) bool {
	if value.BriefHour < 0 || value.BriefHour > 23 || value.BriefMinute < 0 || value.BriefMinute > 59 || len(value.BriefWeekdays) == 0 || len(value.BriefWeekdays) > 7 {
		return false
	}
	seen := map[int]struct{}{}
	for _, day := range value.BriefWeekdays {
		if day < 0 || day > 6 {
			return false
		}
		if _, ok := seen[day]; ok {
			return false
		}
		seen[day] = struct{}{}
	}
	if value.Timezone == "" || len(value.Timezone) > 64 {
		return false
	}
	for _, r := range value.Timezone {
		if unicode.IsControl(r) || !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/_+-", r)) {
			return false
		}
	}
	return true
}

func parseWeekdays(raw string) []int {
	result := make([]int, 0, 7)
	for _, part := range strings.Split(raw, ",") {
		if day, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && day >= 0 && day <= 6 {
			result = append(result, day)
		}
	}
	return result
}

func joinWeekdays(days []int) string {
	parts := make([]string, 0, len(days))
	for _, day := range days {
		parts = append(parts, strconv.Itoa(day))
	}
	return strings.Join(parts, ",")
}

func entryHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listEntries(w, r)
	case http.MethodPost:
		writeEntry(w, r)
	case http.MethodPatch:
		updateEntryState(w, r)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func listEntries(w http.ResponseWriter, r *http.Request) {
	allowed := map[string]struct{}{"source_id": {}, "external_id": {}, "q": {}, "limit": {}, "cursor": {}, "include_deleted": {}, "full": {}, "view_type": {}, "saved": {}}
	for key, values := range r.URL.Query() {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			app.UserErr(w, http.StatusBadRequest, "条目筛选条件不合法")
			return
		}
	}
	sourceID, externalID, query := strings.TrimSpace(r.URL.Query().Get("source_id")), strings.TrimSpace(r.URL.Query().Get("external_id")), strings.TrimSpace(r.URL.Query().Get("q"))
	viewType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view_type")))
	if !validText(sourceID, 128, false) || !validText(externalID, 256, false) || !validText(query, 240, false) {
		app.UserErr(w, http.StatusBadRequest, "条目筛选条件过长")
		return
	}
	if viewType != "" && viewType != "rss" && viewType != "video" && viewType != "youtube" && viewType != "picture" {
		app.UserErr(w, http.StatusBadRequest, "view_type 不合法")
		return
	}
	saved := r.URL.Query().Get("saved")
	if saved != "" && saved != "starred" && saved != "read_later" {
		app.UserErr(w, http.StatusBadRequest, "saved 不合法")
		return
	}
	limit, ok := boundedEntryQueryInt(r, "limit", maxEntryQueryLimit, 1, maxEntryQueryLimit)
	if !ok {
		app.UserErr(w, http.StatusBadRequest, "limit 不合法")
		return
	}
	rawCursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	offset := 0
	var pageCursor *entryPageCursor
	if strings.HasPrefix(rawCursor, entryCursorPrefix) {
		decoded, valid := decodeEntryPageCursor(rawCursor)
		if !valid {
			app.UserErr(w, http.StatusBadRequest, "cursor 不合法")
			return
		}
		pageCursor = &decoded
		offset = decoded.Offset
	} else {
		var ok bool
		offset, ok = boundedEntryQueryInt(r, "cursor", 0, 0, maxEntryQueryOffset)
		if !ok {
			app.UserErr(w, http.StatusBadRequest, "cursor 不合法")
			return
		}
	}
	includeDeleted := r.URL.Query().Get("include_deleted") == "1"
	full := r.URL.Query().Get("full") == "1"
	if full && limit > 10 {
		app.UserErr(w, http.StatusBadRequest, "全文查询每页最多 10 条")
		return
	}
	// A source-scoped read is the first request made after a cold install.  The
	// lightweight source directory is intentionally asynchronous, so the
	// local projection can still be missing its feed_sources/subscription rows
	// when the reader asks for the page directly.  Repair that projection before
	// querying SQLite; otherwise the request can return a false empty page and
	// the UI will conclude that the source has no articles.  Non-gateway rows
	// return immediately from the helper, while focus-chat-links remains a
	// private local scope with no gateway projection to repair.
	if sourceID != "" && sourceID != "focus-chat-links" {
		if projectionErr := ensureSourceGatewaySubscriptionProjection(r.Context(), sourceID); projectionErr != nil {
			status, message := sourceGatewayEntryReadHTTPError(projectionErr)
			log.Printf("来源条目读取触发目录协调失败 source=%s: %v", sourceID, projectionErr)
			app.UserErr(w, status, message)
			return
		}
	}

	// Keep the typed entry feed aligned with the dashboard projection: a user
	// dismissed item must not reappear merely because the UI asks for a later
	// page through the parity reader.
	where := []string{
		"1=1",
		"NOT EXISTS (SELECT 1 FROM news_preferences np WHERE np.news_id = n.id AND np.dismissed = 1)",
		// The Reader's news table is shared by all profiles. One-off links
		// imported from Chat are visible only through their durable scope row;
		// ordinary feed entries retain the existing shared Reader behavior.
		"(COALESCE(n.source_id,'') <> 'focus-chat-links' OR EXISTS (SELECT 1 FROM focus_chat_link_entries l WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=? AND l.source_url=n.url AND l.content_hash=n.content_hash))",
	}
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID}
	if saved != "" {
		// Column names come only from the validated fixed enum above.
		where = append(where, "EXISTS (SELECT 1 FROM item_preferences saved_pref WHERE saved_pref.item_key='news:' || n.id AND saved_pref."+saved+"=1)")
	}
	if !includeDeleted {
		where = append(where, "n.deleted_at = ''")
	}
	if sourceID != "" {
		where = append(where, "n.source_id = ?")
		args = append(args, sourceID)
	}
	if externalID != "" {
		where = append(where, "n.external_id = ?")
		args = append(args, externalID)
	}
	if query != "" {
		where = append(where, "(LOWER(n.title) LIKE ? OR LOWER(n.summary_text) LIKE ? OR LOWER(n.author) LIKE ?)")
		needle := "%" + strings.ToLower(strings.ReplaceAll(query, "%", "")) + "%"
		args = append(args, needle, needle, needle)
	}
	if viewType == "rss" {
		where = append(where, readerRSSEntryPredicate)
	} else if viewType == "video" {
		where = append(where, readerVideoEntryPredicate)
	} else if viewType == "youtube" {
		where = append(where, readerYouTubeEntryPredicate)
	} else if viewType == "picture" {
		where = append(where, readerPictureEntryPredicate)
	}
	baseWhere := append([]string(nil), where...)
	baseArgs := append([]any(nil), args...)
	if pageCursor != nil {
		where = append(where, "(n.published_at < ? OR (n.published_at = ? AND n.updated_at < ?) OR (n.published_at = ? AND n.updated_at = ? AND n.id < ?))")
		args = append(args, pageCursor.PublishedAt, pageCursor.PublishedAt, pageCursor.UpdatedAt, pageCursor.PublishedAt, pageCursor.UpdatedAt, pageCursor.ID)
	}
	queryArgs := []any{articleOverviewPromptVersion}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit+1)
	querySQL := `SELECT n.id,n.external_id,n.title,n.url,n.source,n.source_id,n.origin,n.source_type,n.view_type,n.author,n.published_at,n.updated_at,n.summary_text,n.content_format,n.safe_html,n.thumbnail_url,n.media_url,n.media_mime,n.duration_seconds,n.content_hash,n.fetched_at,n.permission_scope,n.source_level,n.failure_reason,n.revision,n.confidence,n.conflict_state,n.evidence_json,n.deleted_at,COALESCE(o.entry_id,''),COALESCE(o.content_hash,''),COALESCE(o.prompt_version,''),COALESCE(o.status,''),COALESCE(o.stage,''),COALESCE(o.attempt_count,0),COALESCE(o.summary,''),COALESCE(o.key_points_json,'[]'),COALESCE(o.impact,''),COALESCE(o.generated_at,''),COALESCE(o.error_code,''),COALESCE(p.is_read,0),COALESCE(p.starred,0),COALESCE(p.read_later,0),COALESCE(p.reading_progress,0) FROM news n LEFT JOIN item_preferences p ON p.item_key='news:' || n.id LEFT JOIN article_ai_overviews o ON o.entry_id=n.id AND o.content_hash=n.content_hash AND o.prompt_version=? WHERE ` + strings.Join(where, " AND ") + ` ORDER BY n.published_at DESC,n.updated_at DESC,n.id DESC LIMIT ?`
	if pageCursor == nil {
		querySQL += " OFFSET ?"
		queryArgs = append(queryArgs, offset)
	}
	runPage := func() ([]StandardEntry, map[string]string, error) {
		rows, err := db.Query(querySQL, queryArgs...)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		entries := make([]StandardEntry, 0, limit)
		updatedAtByID := make(map[string]string, limit+1)
		for rows.Next() {
			var item StandardEntry
			var evidenceRaw, overviewKeyPoints, updatedAt string
			var read, starred, later int
			var overview ArticleAIOverview
			if err := rows.Scan(&item.ID, &item.ExternalID, &item.Title, &item.URL, &item.Source, &item.SourceID, &item.Origin, &item.SourceType, &item.ViewType, &item.Author, &item.PublishedAt, &updatedAt, &item.SummaryText, &item.ContentFormat, &item.SafeHTML, &item.ThumbnailURL, &item.MediaURL, &item.MediaMIME, &item.DurationSecs, &item.ContentHash, &item.FetchedAt, &item.PermissionScope, &item.SourceLevel, &item.FailureReason, &item.Revision, &item.Confidence, &item.ConflictState, &evidenceRaw, &item.DeletedAt, &overview.EntryID, &overview.ContentHash, &overview.PromptVersion, &overview.Status, &overview.Stage, &overview.AttemptCount, &overview.Summary, &overviewKeyPoints, &overview.Impact, &overview.GeneratedAt, &overview.ErrorCode, &read, &starred, &later, &item.ReadingProgress); err != nil {
				return nil, nil, err
			}
			_ = json.Unmarshal([]byte(evidenceRaw), &item.Evidence)
			if overview.EntryID != "" {
				if err := json.Unmarshal([]byte(overviewKeyPoints), &overview.KeyPoints); err != nil {
					return nil, nil, err
				}
				item.AIOverview = &overview
			}
			item.Read, item.Starred, item.ReadLater = read == 1, starred == 1, later == 1
			if !full {
				item.SafeHTML = ""
			}
			item.UpdatedAt = updatedAt
			updatedAtByID[item.ID] = updatedAt
			entries = append(entries, item)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, err
		}
		return entries, updatedAtByID, nil
	}
	entries, updatedAtByID, err := runPage()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	var gatewayPullErr error
	if len(entries) <= limit && sourceID != "" && sourceID != "focus-chat-links" && query == "" && externalID == "" && !includeDeleted && viewType == "" {
		// 本地窗口已读完：网关管理的来源可以继续从云端分页读取更早历史，
		// 被淘汰出本地窗口的条目由此重新可见（spec §8.3）。每次滚动只拉取一页，
		// 失败时保持本地结果，不把云端故障解释成"没有内容"。
		if pulled, pullErr := pullGatewayHistoryForReader(r, sourceID); pullErr != nil {
			gatewayPullErr = pullErr
			log.Printf("来源网关历史分页失败 source=%s: %v", sourceID, pullErr)
		} else if pulled > 0 {
			entries, updatedAtByID, err = runPage()
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
	}
	// An empty source-scoped page is not a successful result when the first
	// gateway read failed. Returning an explicit retryable status prevents the
	// UI from treating a transport/auth outage as a real source with no
	// articles. Cached rows still take the normal 200 path above, preserving the
	// last-known-good offline experience.
	if len(entries) == 0 && gatewayPullErr != nil {
		status, message := sourceGatewayEntryReadHTTPError(gatewayPullErr)
		app.UserErr(w, status, message)
		return
	}
	if len(entries) == 0 {
		// A failed read enters a short cooldown to avoid hammering the gateway.
		// During that window pullGatewayHistoryForReader intentionally returns no
		// error; consult the persisted source state so a second click does not
		// turn the cooldown into a misleading successful empty page.
		if status, message, unavailable := sourceGatewayEmptyReadHTTPError(sourceID); unavailable {
			app.UserErr(w, status, message)
			return
		}
	}
	next, nextToken := "", ""
	if len(entries) > limit {
		boundary := entries[limit-1]
		entries = entries[:limit]
		next = strconv.Itoa(offset + limit)
		nextToken = encodeEntryPageCursor(entryPageCursor{
			PublishedAt: boundary.PublishedAt,
			UpdatedAt:   updatedAtByID[boundary.ID],
			ID:          boundary.ID,
			Offset:      offset + limit,
		})
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM news n WHERE `+strings.Join(baseWhere, " AND "), baseArgs...).Scan(&total); err != nil {
		app.HTTPErr(w, err)
		return
	}
	response := map[string]any{"entries": entries, "next_cursor": next, "limit": limit, "total_entries": total}
	if nextToken != "" {
		response["next_cursor_token"] = nextToken
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func sourceGatewayEntryReadHTTPError(err error) (int, string) {
	switch {
	case errors.Is(err, errSourceGatewayAuth):
		return http.StatusUnauthorized, "来源授权已失效，请重新登录后重试"
	case errors.Is(err, errSourceGatewayNotFound):
		return http.StatusNotFound, "来源暂时不存在，请刷新来源列表"
	default:
		return http.StatusServiceUnavailable, "来源正在准备或暂时不可用，请稍后重试"
	}
}

func sourceGatewayEmptyReadHTTPError(sourceID string) (int, string, bool) {
	if strings.TrimSpace(sourceID) == "" {
		return 0, "", false
	}
	source, err := feedSourceByID(sourceID)
	if err != nil || source.CollectionMode != "source_gateway" || source.EntryCount > 0 {
		return 0, "", false
	}
	switch strings.ToLower(strings.TrimSpace(source.GatewayStatus)) {
	case "auth":
		return http.StatusUnauthorized, "来源授权已失效，请重新登录后重试", true
	case "blocked":
		return http.StatusNotFound, "来源暂时不存在，请刷新来源列表", true
	case "", "initializing", "degraded", "unavailable":
		return http.StatusServiceUnavailable, "来源正在准备或暂时不可用，请稍后重试", true
	}
	// A non-healthy status from the local source projection (for example after
	// an interrupted first install) is still not a trustworthy empty result.
	if strings.ToLower(strings.TrimSpace(source.Status)) != "ok" || source.LastSyncAt == "" {
		return http.StatusServiceUnavailable, "来源正在准备或暂时不可用，请稍后重试", true
	}
	return 0, "", false
}

func writeEntry(w http.ResponseWriter, r *http.Request) {
	var req entryWriteRequest
	if err := app.DecodeStrictJSON(w, r, maxBodyBytes, &req); err != nil || validateEntryWrite(req) != nil {
		app.UserErr(w, http.StatusBadRequest, "条目数据格式错误")
		return
	}
	if _, err := feedSourceByID(req.SourceID); errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "来源不存在")
		return
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if req.ContentFormat == "" {
		req.ContentFormat = "text"
	}
	if req.ViewType == "" {
		req.ViewType = "article"
	}
	if req.Origin == "" {
		req.Origin = "agent"
	}
	if req.Confidence == "" {
		req.Confidence = "unverified"
	}
	if req.PublishedAt == "" {
		req.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if req.FetchedAt == "" {
		req.FetchedAt = time.Now().UTC().Format(time.RFC3339)
	}
	var renderedArticle *extractedArticle
	if req.Content != "" {
		contentType := "text/plain; charset=utf-8"
		switch req.ContentFormat {
		case "markdown":
			contentType = "text/markdown; charset=utf-8"
		case "html":
			contentType = "text/html; charset=utf-8"
		}
		article := renderEntryContent([]byte(req.Content), contentType, req.URL, req.URL)
		article = finalizeArticleBlocks(article)
		if strings.TrimSpace(article.Text) == "" {
			app.UserErr(w, http.StatusUnprocessableEntity, "文章正文没有可读内容")
			return
		}
		renderedArticle = &article
		digest := sha256.Sum256([]byte(req.Content))
		req.ContentHash = hex.EncodeToString(digest[:])
		req.ContentFormat = extractedArticleFormat(article)
		if validText(article.SafeHTML, 64000, false) {
			req.SafeHTML = article.SafeHTML
		} else {
			req.SafeHTML = ""
		}
		if strings.TrimSpace(req.SummaryText) == "" {
			req.SummaryText = limitText(article.Text, 1200)
		}
	} else if req.ContentHash == "" {
		digest := sha256.Sum256([]byte(req.Title + "\x00" + req.SummaryText + "\x00" + req.SafeHTML))
		req.ContentHash = hex.EncodeToString(digest[:])
	}
	if req.SourceID == "focus-chat-links" {
		if req.Content == "" || (req.SourceType != "note" && req.URL == "") {
			app.UserErr(w, http.StatusBadRequest, "对话资料必须包含已读取的正文")
			return
		}
		// Reader news is shared by the app, so a one-off Chat link must carry
		// the current owner/profile in its identity. The scope mapping below is
		// the second, explicit authorization check used when linking it to a
		// Focus.
		req.ExternalID = stableObjectID("focus-chat-entry", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+req.SourceType+"\x00"+req.URL+"\x00"+req.ContentHash)
		req.ID = stableObjectID("entry", req.SourceID+"|"+req.ExternalID)
	} else if req.ID == "" {
		req.ID = stableObjectID("entry", req.SourceID+"|"+req.ExternalID)
	}
	evidenceRaw, _ := json.Marshal(req.Evidence)
	incomingRaw, _ := json.Marshal(req)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	var existingID string
	var currentRevision int
	var currentRaw string
	var currentHash string
	err = tx.QueryRow(`SELECT id,revision,content_hash,json_object('id',id,'title',title,'summary_text',summary_text,'content_hash',content_hash,'evidence_json',evidence_json) FROM news WHERE source_id=? AND external_id=?`, req.SourceID, req.ExternalID).Scan(&existingID, &currentRevision, &currentHash, &currentRaw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		app.HTTPErr(w, err)
		return
	}
	if err == nil && req.ExpectedRevision > 0 && req.ExpectedRevision != currentRevision {
		_, conflictErr := tx.Exec(`INSERT INTO entry_conflicts(source_id,external_id,current_revision,expected_revision,current_json,incoming_json,created_at) VALUES(?,?,?,?,?,?,?)`, req.SourceID, req.ExternalID, currentRevision, req.ExpectedRevision, currentRaw, string(incomingRaw), now)
		if conflictErr == nil {
			_, conflictErr = tx.Exec(`UPDATE news SET conflict_state='needs_review',updated_at=? WHERE id=?`, now, existingID)
		}
		if conflictErr != nil {
			app.HTTPErr(w, conflictErr)
			return
		}
		if err = tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusConflict, map[string]any{"error": "revision_conflict", "id": existingID, "current_revision": currentRevision, "review_required": true})
		return
	}
	if existingID != "" {
		req.ID = existingID
	}
	if existingID != "" && currentHash == req.ContentHash && req.ExpectedRevision == 0 {
		// A repeated source_id + external_id with the same content is a true
		// idempotent retry: preserve the revision and all user state while
		// refreshing the fetch timestamp and source provenance.
		if _, err = tx.Exec(`UPDATE news SET fetched_at=?,permission_scope=?,source_level=?,failure_reason=?,updated_at=? WHERE id=?`, req.FetchedAt, req.PermissionScope, req.SourceLevel, req.FailureReason, now, req.ID); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if renderedArticle != nil {
			if err = persistEntryContentTx(tx, req.ID, req.URL, req.ContentHash, *renderedArticle, now); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
		if req.SourceID == "focus-chat-links" {
			if err = registerFocusChatLinkTx(tx, req.ID, req.URL, req.ContentHash, now); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
		if err = updateEntrySourceStatsTx(tx, req.SourceID, req.FetchedAt, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if renderedArticle != nil {
			pruneEntryContentCache()
		}
		entry, lookupErr := entryByID(req.ID)
		if lookupErr != nil {
			app.HTTPErr(w, lookupErr)
			return
		}
		app.WriteJSON(w, http.StatusOK, entry)
		return
	}
	if existingID != "" {
		_, err = tx.Exec(`UPDATE news SET title=?,url=?,source=?,origin=?,source_type=?,view_type=?,author=?,published_at=?,summary_text=?,content_format=?,safe_html=?,thumbnail_url=?,media_url=?,media_mime=?,duration_seconds=?,content_hash=?,fetched_at=?,permission_scope=?,source_level=?,failure_reason=?,updated_at=?,revision=revision+1,confidence=?,evidence_json=?,conflict_state='' WHERE id=?`,
			req.Title, req.URL, req.Source, req.Origin, req.SourceType, req.ViewType, req.Author, req.PublishedAt, req.SummaryText, req.ContentFormat, req.SafeHTML, req.ThumbnailURL, req.MediaURL, req.MediaMIME, req.DurationSecs, req.ContentHash, req.FetchedAt, req.PermissionScope, req.SourceLevel, req.FailureReason, now, req.Confidence, string(evidenceRaw), req.ID)
	} else {
		_, err = tx.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at,revision,confidence,conflict_state,evidence_json,deleted_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,?,'',?,'')`,
			req.ID, req.ExternalID, req.Title, req.URL, req.Source, req.SourceID, req.Origin, req.SourceType, req.ViewType, req.Author, req.PublishedAt, req.SummaryText, req.ContentFormat, req.SafeHTML, req.ThumbnailURL, req.MediaURL, req.MediaMIME, req.DurationSecs, req.ContentHash, req.FetchedAt, req.PermissionScope, req.SourceLevel, req.FailureReason, now, req.Confidence, string(evidenceRaw))
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if renderedArticle != nil {
		if err = persistEntryContentTx(tx, req.ID, req.URL, req.ContentHash, *renderedArticle, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if req.SourceID == "focus-chat-links" {
		if err = registerFocusChatLinkTx(tx, req.ID, req.URL, req.ContentHash, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err = updateEntrySourceStatsTx(tx, req.SourceID, req.FetchedAt, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if renderedArticle != nil {
		pruneEntryContentCache()
	}
	entry, err := entryByID(req.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, entry)
}

func registerFocusChatLinkTx(tx *sql.Tx, entryID, sourceURL, contentHash, now string) error {
	if tx == nil || strings.TrimSpace(entryID) == "" || strings.TrimSpace(contentHash) == "" {
		return errors.New("invalid focus chat link ownership")
	}
	_, err := tx.Exec(`INSERT INTO focus_chat_link_entries(owner_id,profile_id,entry_id,source_url,content_hash,created_at,updated_at)
VALUES(?,?,?,?,?,?,?)
ON CONFLICT(owner_id,profile_id,entry_id) DO UPDATE SET source_url=excluded.source_url,content_hash=excluded.content_hash,updated_at=excluded.updated_at`,
		appDataScope.OwnerID, appDataScope.ProfileID, entryID, sourceURL, contentHash, now, now)
	return err
}

func updateEntrySourceStatsTx(tx *sql.Tx, sourceID, fetchedAt, now string) error {
	var stored, degraded int
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN source_level='official_fallback' OR failure_reason<>'' THEN 1 ELSE 0 END), 0) FROM news WHERE source_id=? AND deleted_at=''`, sourceID).Scan(&stored, &degraded); err != nil {
		return err
	}
	status, lastError := "ok", ""
	if degraded > 0 {
		status = "partial"
		if err := tx.QueryRow(`SELECT failure_reason FROM news WHERE source_id=? AND deleted_at='' AND failure_reason<>'' ORDER BY updated_at DESC LIMIT 1`, sourceID).Scan(&lastError); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if strings.TrimSpace(fetchedAt) == "" {
		fetchedAt = now
	}
	_, err := tx.Exec(`UPDATE feed_sources SET status=?, records_seen=MAX(records_seen, ?), records_used=?, last_sync_at=?, last_error=?, updated_at=? WHERE id=?`, status, stored, stored, fetchedAt, lastError, now, sourceID)
	return err
}

func updateEntryState(w http.ResponseWriter, r *http.Request) {
	var req entryStateRequest
	if err := app.DecodeStrictJSON(w, r, 8<<10, &req); err != nil || !validText(req.ID, 128, true) || req.Deleted == nil && req.Read == nil && req.Starred == nil && req.ReadLater == nil && req.ReadingProgress == nil {
		app.UserErr(w, http.StatusBadRequest, "条目状态格式错误")
		return
	}
	if req.ReadingProgress != nil && (math.IsNaN(*req.ReadingProgress) || math.IsInf(*req.ReadingProgress, 0) || *req.ReadingProgress < 0 || *req.ReadingProgress > 1) {
		app.UserErr(w, http.StatusBadRequest, "阅读进度不合法")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	var revision int
	if err = tx.QueryRow(`SELECT revision FROM news n
WHERE n.id=? AND (COALESCE(n.source_id,'') <> 'focus-chat-links' OR EXISTS (
  SELECT 1 FROM focus_chat_link_entries l
  WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=?
    AND l.source_url=n.url AND l.content_hash=n.content_hash
))`, req.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "条目不存在")
		return
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if req.ExpectedRevision > 0 && req.ExpectedRevision != revision {
		app.WriteJSON(w, http.StatusConflict, map[string]any{"error": "revision_conflict", "id": req.ID, "current_revision": revision})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if req.Deleted != nil {
		deletedAt := ""
		if *req.Deleted {
			deletedAt = now
		}
		if _, err = tx.Exec(`UPDATE news SET deleted_at=?,revision=revision+1,updated_at=? WHERE id=? AND (COALESCE(source_id,'') <> 'focus-chat-links' OR EXISTS (
  SELECT 1 FROM focus_chat_link_entries l
  WHERE l.entry_id=news.id AND l.owner_id=? AND l.profile_id=?
    AND l.source_url=news.url AND l.content_hash=news.content_hash
))`, deletedAt, now, req.ID, appDataScope.OwnerID, appDataScope.ProfileID); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if req.Read != nil || req.Starred != nil || req.ReadLater != nil || req.ReadingProgress != nil {
		var read, starred, later int
		var progress float64
		err = tx.QueryRow(`SELECT is_read,starred,read_later,reading_progress FROM item_preferences WHERE item_key=?`, "news:"+req.ID).Scan(&read, &starred, &later, &progress)
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
			later = boolInt(*req.ReadLater)
		}
		if req.ReadingProgress != nil {
			progress = *req.ReadingProgress
		}
		_, err = tx.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(item_key) DO UPDATE SET is_read=excluded.is_read,starred=excluded.starred,read_later=excluded.read_later,reading_progress=excluded.reading_progress,updated_at=excluded.updated_at`, "news:"+req.ID, read, starred, later, progress, now)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	entry, err := entryByID(req.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, entry)
}

func validateEntryWrite(req entryWriteRequest) error {
	if (req.SchemaVersion != 0 && req.SchemaVersion != 1) || !validText(req.ID, 128, false) || !validText(req.SourceID, 128, true) || !validText(req.SourceType, 64, true) || (req.SourceID != "focus-chat-links" && !validText(req.ExternalID, 256, true)) || !validText(req.ExternalID, 256, false) || !validText(req.Title, 500, true) || !validText(req.Source, 120, false) || !validText(req.Origin, 64, false) || !validText(req.ViewType, 32, false) || !validText(req.Author, 200, false) || !validText(req.SummaryText, 20000, false) || !validText(req.SafeHTML, 64000, false) || len(req.Content) > maxEntryWriteContent || !utf8.ValidString(req.Content) || !validText(req.MediaMIME, 128, false) || !validText(req.PermissionScope, 64, false) || !validText(req.SourceLevel, 64, false) || !validText(req.FailureReason, 1000, false) || req.DurationSecs < 0 || req.DurationSecs > 7*24*3600 || req.ExpectedRevision < 0 || !validTextList(req.Evidence, 50, 1000) {
		return errors.New("invalid entry")
	}
	for _, raw := range []string{req.URL, req.ThumbnailURL, req.MediaURL} {
		if raw != "" && !validHTTPURL(raw) {
			return errors.New("invalid URL")
		}
	}
	if req.ContentFormat != "" && req.ContentFormat != "text" && req.ContentFormat != "markdown" && req.ContentFormat != "html" {
		return errors.New("invalid format")
	}
	if req.Confidence != "" && req.Confidence != "unverified" && req.Confidence != "low" && req.Confidence != "medium" && req.Confidence != "high" {
		return errors.New("invalid confidence")
	}
	for _, raw := range []string{req.PublishedAt, req.FetchedAt} {
		if raw != "" {
			if _, err := time.Parse(time.RFC3339, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func entryByID(id string) (StandardEntry, error) {
	var item StandardEntry
	var evidenceRaw, overviewKeyPoints string
	var overview ArticleAIOverview
	var read, starred, later int
	err := db.QueryRow(`SELECT n.id,n.external_id,n.title,n.url,n.source,n.source_id,n.origin,n.source_type,n.view_type,n.author,n.published_at,n.updated_at,n.summary_text,n.content_format,n.safe_html,n.thumbnail_url,n.media_url,n.media_mime,n.duration_seconds,n.content_hash,n.fetched_at,n.permission_scope,n.source_level,n.failure_reason,n.revision,n.confidence,n.conflict_state,n.evidence_json,n.deleted_at,COALESCE(o.entry_id,''),COALESCE(o.content_hash,''),COALESCE(o.prompt_version,''),COALESCE(o.status,''),COALESCE(o.stage,''),COALESCE(o.attempt_count,0),COALESCE(o.summary,''),COALESCE(o.key_points_json,'[]'),COALESCE(o.impact,''),COALESCE(o.generated_at,''),COALESCE(o.error_code,''),COALESCE(p.is_read,0),COALESCE(p.starred,0),COALESCE(p.read_later,0),COALESCE(p.reading_progress,0) FROM news n LEFT JOIN item_preferences p ON p.item_key='news:' || n.id LEFT JOIN article_ai_overviews o ON o.entry_id=n.id AND o.content_hash=n.content_hash AND o.prompt_version=? WHERE n.id=? AND (COALESCE(n.source_id,'') <> 'focus-chat-links' OR EXISTS (SELECT 1 FROM focus_chat_link_entries l WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=? AND l.source_url=n.url AND l.content_hash=n.content_hash))`, articleOverviewPromptVersion, id, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&item.ID, &item.ExternalID, &item.Title, &item.URL, &item.Source, &item.SourceID, &item.Origin, &item.SourceType, &item.ViewType, &item.Author, &item.PublishedAt, &item.UpdatedAt, &item.SummaryText, &item.ContentFormat, &item.SafeHTML, &item.ThumbnailURL, &item.MediaURL, &item.MediaMIME, &item.DurationSecs, &item.ContentHash, &item.FetchedAt, &item.PermissionScope, &item.SourceLevel, &item.FailureReason, &item.Revision, &item.Confidence, &item.ConflictState, &evidenceRaw, &item.DeletedAt, &overview.EntryID, &overview.ContentHash, &overview.PromptVersion, &overview.Status, &overview.Stage, &overview.AttemptCount, &overview.Summary, &overviewKeyPoints, &overview.Impact, &overview.GeneratedAt, &overview.ErrorCode, &read, &starred, &later, &item.ReadingProgress)
	if err != nil {
		return StandardEntry{}, err
	}
	_ = json.Unmarshal([]byte(evidenceRaw), &item.Evidence)
	if overview.EntryID != "" {
		if err := json.Unmarshal([]byte(overviewKeyPoints), &overview.KeyPoints); err != nil {
			return StandardEntry{}, err
		}
		item.AIOverview = &overview
	}
	item.Read, item.Starred, item.ReadLater = read == 1, starred == 1, later == 1
	return item, nil
}

func boundedEntryQueryInt(r *http.Request, key string, fallback, minimum, maximum int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, true
	}
	value, err := strconv.Atoi(raw)
	return value, err == nil && value >= minimum && value <= maximum
}

package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"
)

const (
	maintenanceReadSourceLimit      = 5
	maintenanceReadEntryLimit       = 2
	maintenanceReadTotalLimit       = 10
	maintenanceApplyLimit           = 3
	maintenanceOverviewLimit        = 3
	maintenanceOverviewExcerptRunes = 6000
	maintenanceCommandMaxBytes      = 1 << 20
)

var errInvalidReaderMaintenanceRequest = errors.New("invalid reader maintenance request")

type readerMaintenanceHTTPError struct {
	status int
	body   []byte
}

func (e *readerMaintenanceHTTPError) Error() string {
	return fmt.Sprintf("维护写入失败: status=%d", e.status)
}

// readerMaintenanceSource and readerMaintenanceEntry are deliberately
// smaller than the app_data entry contract. The unattended job receives only
// enough evidence to draft a conclusion; it never receives a full article,
// credentials, or an execution parameter.
type readerMaintenanceSource struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	SourceURL   string `json:"source_url"`
	Status      string `json:"status"`
	LastSyncAt  string `json:"last_sync_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	Subscribed  bool   `json:"subscribed"`
	AutoExtract bool   `json:"auto_extract"`
}

type readerMaintenanceEntry struct {
	ID            string `json:"id"`
	SourceID      string `json:"source_id"`
	ContentHash   string `json:"content_hash"`
	Title         string `json:"title"`
	SourceURL     string `json:"source_url"`
	Source        string `json:"source"`
	PublishedAt   string `json:"published_at,omitempty"`
	SummaryText   string `json:"summary_text,omitempty"`
	SourceLevel   string `json:"source_level,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type readerMaintenanceOverviewCandidate struct {
	EntryID     string `json:"entry_id"`
	ContentHash string `json:"content_hash"`
	Title       string `json:"title"`
	Source      string `json:"source"`
	PublishedAt string `json:"published_at,omitempty"`
	Excerpt     string `json:"excerpt"`
}

type readerMaintenanceReadResult struct {
	SchemaVersion               int                                  `json:"schema_version"`
	GeneratedAt                 string                               `json:"generated_at"`
	Settings                    AppSettings                          `json:"settings"`
	Sources                     []readerMaintenanceSource            `json:"sources"`
	Entries                     []readerMaintenanceEntry             `json:"entries"`
	Receipts                    []ReaderDigestReceipt                `json:"receipts"`
	ArticleAIOverviewCandidates []readerMaintenanceOverviewCandidate `json:"article_ai_overview_candidates,omitempty"`
	ReaderFocusClaim            *ReaderFocusClaimReceipt             `json:"reader_focus_claim,omitempty"`
	ReaderFocus                 *ReaderFocusProjection               `json:"reader_focus,omitempty"`
	ReaderRecommendations       *ReaderRecommendationsProjection     `json:"reader_recommendations,omitempty"`
}

func openReaderMaintenanceDB() error {
	if err := os.MkdirAll("data", 0o755); err != nil {
		return err
	}
	opened, err := openDB()
	if err != nil {
		return err
	}
	db = opened
	if err := migrate(db); err != nil {
		_ = db.Close()
		db = nil
		return err
	}
	return nil
}

func runReaderMaintenanceRead(out io.Writer, topicIDs ...string) error {
	if db == nil {
		return errors.New("数据库未打开")
	}
	settings, err := readAppSettings()
	if err != nil {
		return err
	}
	requestedTopicID := ""
	if len(topicIDs) > 0 {
		requestedTopicID = strings.TrimSpace(topicIDs[0])
		if requestedTopicID != "" && !validText(requestedTopicID, 128, true) {
			return fmt.Errorf("主题标识不合法")
		}
	}
	allSources, err := queryFeedSources(maintenanceReadSourceLimit*4, "")
	if err != nil {
		return err
	}
	selected := make([]FeedSource, 0, maintenanceReadSourceLimit)
	for _, source := range allSources {
		if !source.Subscribed || source.Hidden || !source.AutoExtract {
			continue
		}
		selected = append(selected, source)
		if len(selected) == maintenanceReadSourceLimit {
			break
		}
	}

	result := readerMaintenanceReadResult{
		SchemaVersion:               1,
		GeneratedAt:                 time.Now().UTC().Format(time.RFC3339),
		Settings:                    settings,
		Sources:                     make([]readerMaintenanceSource, 0, len(selected)),
		Entries:                     make([]readerMaintenanceEntry, 0, maintenanceReadTotalLimit),
		Receipts:                    make([]ReaderDigestReceipt, 0, maintenanceReadTotalLimit),
		ArticleAIOverviewCandidates: make([]readerMaintenanceOverviewCandidate, 0, maintenanceOverviewLimit),
	}
	// In focus mode, Entries are only an evidence projection for the exact
	// overview candidates retained by the local budget gate below. Keep the
	// slice separate so a zero-budget round cannot accidentally fall back to
	// the broader unread query.
	var focusOverviewCandidates []readerMaintenanceOverviewCandidate
	for _, source := range selected {
		result.Sources = append(result.Sources, readerMaintenanceSource{
			ID: source.ID, Kind: source.Kind, Name: source.Name, SourceURL: source.URL,
			Status: source.Status, LastSyncAt: source.LastSyncAt, LastError: source.LastError,
			Subscribed: source.Subscribed, AutoExtract: source.AutoExtract,
		})
	}
	if err := loadReaderMaintenanceReceipts(&result.Receipts); err != nil {
		return err
	}
	// A topic-scoped read continues the user's explicit 2/3 choice. Allow that
	// bounded foreground batch even when unattended overview generation is
	// disabled; Cron reads carry no topic ID and still honor the setting.
	if settings.AutoGenerateArticleOverviews || requestedTopicID != "" {
		// Focus maintenance is opt-in. Its candidate set is derived from the
		// same local unread query used by foreground recommendations; an old
		// profile with no active focus keeps the historical three-item path.
		focusState, focusActive, focusErr := readerFocusMaintenanceState()
		if focusErr != nil {
			return focusErr
		}
		if focusActive {
			if requestedTopicID != "" {
				focusState, err = readerFocusStateForTopic(focusState, requestedTopicID)
				if err != nil {
					return err
				}
			}
			limit, err := readerFocusMaintenanceLimit(focusState)
			if err != nil {
				return err
			}
			if limit > readerFocusPerRunLimit {
				limit = readerFocusPerRunLimit
			}
			if limit > 0 {
				candidates, claim, reserveErr := reserveReaderFocusCandidates(focusState, requestedTopicID, limit)
				if reserveErr != nil {
					return reserveErr
				}
				result.ArticleAIOverviewCandidates = candidates
				focusOverviewCandidates = candidates
				result.ReaderFocusClaim = claim
			} else if requestedTopicID != "" {
				// Surface an in-flight foreground reservation even when its
				// estimated budget leaves no new capacity. This prevents callers
				// from mistaking a busy batch for an empty/no-claim response.
				_, claim, reserveErr := reserveReaderFocusCandidates(focusState, requestedTopicID, 1)
				if reserveErr != nil {
					return reserveErr
				}
				result.ReaderFocusClaim = claim
			}
			scope := "daily"
			if focusState.Policy.Mode == readerFocusModeAutoBootstrap {
				scope = "bootstrap"
			}
			focus, recommendations, err := buildReaderFocusProjections(scope, 5)
			if err != nil {
				return err
			}
			result.ReaderFocus = focus
			result.ReaderRecommendations = recommendations
		} else if requestedTopicID == "" {
			candidates, err := readArticleAIOverviewCandidates(maintenanceOverviewLimit)
			if err != nil {
				return err
			}
			result.ArticleAIOverviewCandidates = candidates
		}
	}

	focusState, focusActive, focusErr := readerFocusMaintenanceState()
	if focusErr != nil {
		return focusErr
	}
	if focusActive {
		if requestedTopicID != "" {
			focusState, err = readerFocusStateForTopic(focusState, requestedTopicID)
			if err != nil {
				return err
			}
		}
		if result.ReaderFocus == nil {
			scope := "daily"
			if focusState.Policy.Mode == readerFocusModeAutoBootstrap {
				scope = "bootstrap"
			}
			focus, recommendations, err := buildReaderFocusProjections(scope, 5)
			if err != nil {
				return err
			}
			result.ReaderFocus = focus
			result.ReaderRecommendations = recommendations
		}
		if (settings.AutoGenerateArticleOverviews || requestedTopicID != "") && len(focusOverviewCandidates) > 0 {
			focused, err := readFocusedMaintenanceEntriesForOverviewCandidates(focusState, focusOverviewCandidates)
			if err != nil {
				return err
			}
			result.Entries = append(result.Entries, focused...)
		}
	}
	if focusActive {
		return writeBoundedJSON(out, result, maintenanceCommandMaxBytes)
	}
	for _, source := range selected {
		entries, err := readMaintenanceEntries(source.ID, maintenanceReadEntryLimit)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(result.Entries) == maintenanceReadTotalLimit {
				break
			}
			result.Entries = append(result.Entries, entry)
		}
		if len(result.Entries) == maintenanceReadTotalLimit {
			break
		}
	}
	return writeBoundedJSON(out, result, maintenanceCommandMaxBytes)
}

// readFocusedMaintenanceEntriesForOverviewCandidates deliberately does not
// rescan the full unread set for the maintenance payload. The overview
// candidates have already passed the per-run article and token budget gate;
// only those exact entry/hash pairs may be sent as additional evidence to the
// model. Re-read the bounded candidate projection to discard entries that were
// concurrently marked read, hidden, or analyzed between the two reads.
func readFocusedMaintenanceEntriesForOverviewCandidates(state ReaderFocusState, candidates []readerMaintenanceOverviewCandidate) ([]readerMaintenanceEntry, error) {
	if len(candidates) == 0 {
		return []readerMaintenanceEntry{}, nil
	}
	matched, err := queryReaderFocusCandidates(readerFocusMaintenanceTopics(state))
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]readerFocusCandidate, len(matched))
	for _, candidate := range matched {
		if candidate.ID == "" || candidate.Hash == "" {
			continue
		}
		byKey[candidate.ID+"\x00"+candidate.Hash] = candidate
	}
	limit := len(candidates)
	if limit > maintenanceReadTotalLimit {
		limit = maintenanceReadTotalLimit
	}
	items := make([]readerMaintenanceEntry, 0, limit)
	for _, requested := range candidates {
		if len(items) == limit {
			break
		}
		key := requested.EntryID + "\x00" + requested.ContentHash
		candidate, ok := byKey[key]
		if !ok || candidate.Score <= 0 || !candidate.AutoExtract || candidate.Overview != nil {
			continue
		}
		items = append(items, readerMaintenanceEntry{
			ID: candidate.ID, SourceID: candidate.SourceID, ContentHash: candidate.Hash,
			Title: candidate.Title, SourceURL: candidate.URL, Source: candidate.Source,
			PublishedAt: candidate.PublishedAt, SummaryText: limitText(candidate.Summary, maintenanceOverviewExcerptRunes),
		})
	}
	return items, nil
}

func readArticleAIOverviewCandidates(limit int) ([]readerMaintenanceOverviewCandidate, error) {
	rows, err := db.Query(`SELECT n.id, n.content_hash, n.title, n.source, n.published_at,
COALESCE(NULLIF(f.content_text, ''), n.summary_text)
FROM news n
JOIN feed_sources s ON s.id = n.source_id
JOIN feed_subscriptions sub ON sub.source_id = n.source_id AND sub.enabled = 1
LEFT JOIN entry_full_texts f ON f.entry_id = n.id AND f.content_hash = n.content_hash AND f.status IN ('ok', 'stale')
WHERE n.deleted_at = '' AND n.content_hash <> '' AND s.hidden = 0 AND s.auto_extract = 1
  AND NOT EXISTS (SELECT 1 FROM article_ai_overviews o WHERE o.entry_id = n.id AND o.content_hash = n.content_hash AND o.prompt_version = ?)
  AND TRIM(COALESCE(NULLIF(f.content_text, ''), n.summary_text)) <> ''
ORDER BY n.published_at DESC, n.updated_at DESC LIMIT ?`, articleOverviewPromptVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]readerMaintenanceOverviewCandidate, 0, limit)
	for rows.Next() {
		var item readerMaintenanceOverviewCandidate
		if err := rows.Scan(&item.EntryID, &item.ContentHash, &item.Title, &item.Source, &item.PublishedAt, &item.Excerpt); err != nil {
			return nil, err
		}
		item.Excerpt = limitText(item.Excerpt, maintenanceOverviewExcerptRunes)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadReaderMaintenanceReceipts(dst *[]ReaderDigestReceipt) error {
	rows, err := db.Query(`SELECT content_hash, source_id, entry_id, processed_at, outcome
FROM reader_digest_receipts ORDER BY processed_at DESC LIMIT ?`, maintenanceReadTotalLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item ReaderDigestReceipt
		if err := rows.Scan(&item.ContentHash, &item.SourceID, &item.EntryID, &item.ProcessedAt, &item.Outcome); err != nil {
			return err
		}
		*dst = append(*dst, item)
	}
	return rows.Err()
}

func readMaintenanceEntries(sourceID string, limit int) ([]readerMaintenanceEntry, error) {
	rows, err := db.Query(`
SELECT n.id, n.source_id, n.content_hash, n.title, n.url, n.source,
       n.published_at, n.summary_text, n.source_level, n.failure_reason
FROM news n
WHERE n.source_id = ? AND n.deleted_at = '' AND n.content_hash <> ''
  AND NOT EXISTS (SELECT 1 FROM reader_digest_receipts r WHERE r.content_hash = n.content_hash)
ORDER BY n.published_at DESC, n.updated_at DESC LIMIT ?`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]readerMaintenanceEntry, 0, limit)
	for rows.Next() {
		var item readerMaintenanceEntry
		if err := rows.Scan(&item.ID, &item.SourceID, &item.ContentHash, &item.Title, &item.SourceURL, &item.Source,
			&item.PublishedAt, &item.SummaryText, &item.SourceLevel, &item.FailureReason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func runReaderMaintenanceApply(in io.Reader, out io.Writer) error {
	return runReaderMaintenanceApplyWithKey(in, out, "")
}

func runReaderMaintenanceApplyWithKey(in io.Reader, out io.Writer, idempotencyKey string) error {
	return runReaderMaintenanceApplyWithKeyHeaders(in, out, idempotencyKey, nil)
}

func runReaderMaintenanceApplyWithKeyHeaders(in io.Reader, out io.Writer, idempotencyKey string, headers http.Header) error {
	if db == nil {
		return errors.New("数据库未打开")
	}
	payload, err := io.ReadAll(io.LimitReader(in, maintenanceCommandMaxBytes+1))
	if err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > maintenanceCommandMaxBytes {
		return fmt.Errorf("%w: 维护请求过大", errInvalidReaderMaintenanceRequest)
	}
	var dashboard Dashboard
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&dashboard); err != nil {
		return fmt.Errorf("%w: 维护请求格式错误", errInvalidReaderMaintenanceRequest)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: 维护请求包含多余数据", errInvalidReaderMaintenanceRequest)
	}
	if err := validateReaderMaintenanceDashboard(&dashboard); err != nil {
		return fmt.Errorf("%w: %v", errInvalidReaderMaintenanceRequest, err)
	}
	// Fingerprint the normalized, validated draft before removing protected
	// objects. Two different caller payloads must never replay the same result,
	// even when both happen to leave the same user-owned projection.
	var requestFingerprint string
	if idempotencyKey != "" {
		fingerprintPayload, marshalErr := json.Marshal(dashboard)
		if marshalErr != nil {
			return marshalErr
		}
		requestFingerprint = appDataMutationFingerprint("reader_digest.apply", fingerprintPayload, "")
	}
	if err := filterConfirmedMaintenanceItems(&dashboard); err != nil {
		return err
	}
	request := httptest.NewRequest(http.MethodPost, "/api/dashboard", bytes.NewReader(payload))
	// The filter may remove protected objects, so re-encode the exact payload
	// that putDashboard will persist. This keeps the normal atomic dashboard
	// transaction and sidecar reconciliation as the single write path.
	filtered, err := json.Marshal(dashboard)
	if err != nil {
		return err
	}
	request.Body = io.NopCloser(bytes.NewReader(filtered))
	request.ContentLength = int64(len(filtered))
	recorder := httptest.NewRecorder()
	if idempotencyKey != "" {
		request.Header.Set("X-Zettlab-Idempotency-Key", idempotencyKey)
		putDashboardWithIdempotencyFingerprint(recorder, request, "reader_digest.apply", idempotencyKey, requestFingerprint)
	} else {
		putDashboard(recorder, request)
	}
	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		return &readerMaintenanceHTTPError{status: recorder.Code, body: append([]byte(nil), recorder.Body.Bytes()...)}
	}
	if idempotencyKey != "" {
		if headers != nil {
			for name, values := range recorder.Header() {
				for _, value := range values {
					headers.Add(name, value)
				}
			}
		}
		_, err := out.Write(recorder.Body.Bytes())
		return err
	}
	return writeBoundedJSON(out, map[string]any{
		"ok":           true,
		"generated_at": dashboard.GeneratedAt,
		"signals":      len(dashboard.Signals),
		"actions":      len(dashboard.Actions),
		"decisions":    len(dashboard.Decisions),
		"receipts":     len(dashboard.ReaderDigests),
	}, 32<<10)
}

func readerDigestReadOperationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	topicID := strings.TrimSpace(r.URL.Query().Get("topic_id"))
	if topicID != "" && !validText(topicID, 128, true) {
		app.UserErr(w, http.StatusBadRequest, "主题标识不合法")
		return
	}
	var output bytes.Buffer
	if err := runReaderMaintenanceRead(&output, topicID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func readerDigestApplyOperationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "写操作需要合法幂等键")
		return
	}
	var output bytes.Buffer
	err := runReaderMaintenanceApplyWithKeyHeaders(r.Body, &output, key, w.Header())
	if err != nil {
		var httpFailure *readerMaintenanceHTTPError
		if errors.As(err, &httpFailure) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(httpFailure.status)
			_, _ = w.Write(httpFailure.body)
			return
		}
		if errors.Is(err, errInvalidReaderMaintenanceRequest) {
			app.UserErr(w, http.StatusBadRequest, "维护草案格式或来源不合法")
			return
		}
		app.HTTPErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
}

func validateReaderMaintenanceDashboard(d *Dashboard) error {
	if d.SchemaVersion != 1 || !validText(d.GeneratedAt, 80, true) {
		return errors.New("维护看板版本或时间不合法")
	}
	if d.Source.Kind != "local_sources" && d.Source.Kind != "mixed" && d.Source.Kind != "profile_meetings" && d.Source.Kind != "agent" {
		return errors.New("维护来源类型不合法")
	}
	if d.Source.Status != "ok" && d.Source.Status != "partial" && d.Source.Status != "unavailable" {
		return errors.New("维护来源状态不合法")
	}
	overviewLimit := maintenanceOverviewLimit
	if state, active, stateErr := readerFocusMaintenanceState(); stateErr == nil {
		if active {
			overviewLimit = state.Policy.PerRunLimit
			if overviewLimit < 1 || overviewLimit > readerFocusPerRunLimit {
				overviewLimit = readerFocusPerRunLimit
			}
		} else if key, _ := readerFocusRunKey(state, *d); key != "" {
			// A completed bootstrap topic is no longer an active maintenance
			// scope, but the exact prior payload must remain replayable after a
			// connection drop. Do not broaden new writes; only the durable key
			// check below can accept this larger replay envelope.
			if exists, err := readerFocusRunExists(key); err == nil && exists {
				overviewLimit = readerFocusPerRunLimit
			}
		}
	}
	if len(d.Evidence) > maintenanceApplyLimit*10 || len(d.Actions) > maintenanceApplyLimit || len(d.Signals) > maintenanceApplyLimit || len(d.Decisions) > maintenanceApplyLimit || len(d.Metrics) > maintenanceApplyLimit || len(d.Projects) > maintenanceApplyLimit || len(d.Recordings) > 5 || len(d.ReaderDigests) > maintenanceReadTotalLimit || len(d.ArticleAIOverviews) > overviewLimit {
		return errors.New("维护草案数量超限")
	}
	if len(d.News) > 0 || len(d.Sources) > 0 || len(d.Discoveries) > 0 || len(d.HiddenModules) > 0 || len(d.ModuleLayout) > 0 {
		return errors.New("维护命令不能修改应用设置或来源目录")
	}
	// Morning Brief is an agent-owned local projection. Keep it in this
	// operation so the Cron path preserves the existing daily brief behavior;
	// settings, source catalog, and user-owned layout remain closed above.
	if err := validateMorningBrief(d.MorningBrief); err != nil {
		return err
	}
	for _, action := range d.Actions {
		if action.ConfirmationStatus != "" && action.ConfirmationStatus != actionConfirmationDraft {
			return errors.New("维护命令只能写行动草案")
		}
		if action.ScheduleState != "" && action.ScheduleState != actionScheduleNone {
			return errors.New("维护命令不能调度行动")
		}
	}
	for i := range d.Signals {
		d.Signals[i].Status = "draft"
	}
	for i := range d.Decisions {
		d.Decisions[i].Status = "draft"
	}
	for i := range d.Actions {
		d.Actions[i].ConfirmationStatus = actionConfirmationDraft
		d.Actions[i].ScheduleState = actionScheduleNone
		d.Actions[i].Revision = 0
		d.Actions[i].ScheduleRequestID = ""
		d.Actions[i].ScheduleCandidateID = ""
		d.Actions[i].ClientRequestID = ""
		d.Actions[i].JobID = ""
		d.Actions[i].NextRunAt = ""
		d.Actions[i].ScheduleReplayed = false
		d.Actions[i].ScheduleErrorCode = ""
		d.Actions[i].ScheduleError = ""
		d.Actions[i].WorkflowUpdatedAt = ""
	}
	if err := validateEvidence(d.Evidence); err != nil {
		return err
	}
	if err := validateActions(d.Actions); err != nil {
		return err
	}
	if err := validateSignals(d.Signals); err != nil {
		return err
	}
	if err := validateDecisions(d.Decisions); err != nil {
		return err
	}
	if err := validateMetrics(d.Metrics); err != nil {
		return err
	}
	if err := validateProjects(d.Projects); err != nil {
		return err
	}
	if err := validateRecordings(d.Recordings); err != nil {
		return err
	}
	if err := validateReaderDigestReceipts(d.ReaderDigests); err != nil {
		return err
	}
	if err := validateArticleAIOverviews(d.ArticleAIOverviews, overviewLimit); err != nil {
		return err
	}
	if err := validateReaderFocusProjection(d.ReaderFocus); err != nil {
		return err
	}
	if err := validateReaderRecommendationsProjection(d.ReaderRecommendations); err != nil {
		return err
	}
	if err := validateReaderFocusClaimID(*d); err != nil {
		return err
	}
	if err := validateReaderFocusMaintenanceApply(d); err != nil {
		return err
	}
	return validateMaintenanceSources(d)
}

func validateMaintenanceSources(d *Dashboard) error {
	allowed := make(map[string]string, len(d.ReaderDigests))
	for _, receipt := range d.ReaderDigests {
		var sourceID, hash string
		if err := db.QueryRow(`SELECT source_id, content_hash FROM news WHERE id=? AND deleted_at=''`, receipt.EntryID).Scan(&sourceID, &hash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("维护回执引用了不存在的文章")
			}
			return err
		}
		if sourceID != receipt.SourceID || hash != receipt.ContentHash {
			return errors.New("维护回执与文章内容不匹配")
		}
		allowed[receipt.EntryID] = receipt.ContentHash
	}
	for _, overview := range d.ArticleAIOverviews {
		var hash string
		if err := db.QueryRow(`SELECT content_hash FROM news WHERE id=? AND deleted_at=''`, overview.EntryID).Scan(&hash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("文章概览引用了不存在的文章")
			}
			return err
		}
		if hash != overview.ContentHash {
			return errors.New("文章概览与文章内容不匹配")
		}
	}
	for _, item := range d.Signals {
		for _, sourceID := range item.SourceIDs {
			if _, ok := allowed[sourceID]; !ok {
				if _, evidence := findEvidence(d.Evidence, sourceID); !evidence {
					return errors.New("草案引用了未验证来源")
				}
			}
		}
	}
	for _, item := range d.Decisions {
		for _, sourceID := range item.SourceIDs {
			if _, ok := allowed[sourceID]; !ok {
				if _, evidence := findEvidence(d.Evidence, sourceID); !evidence {
					return errors.New("草案引用了未验证来源")
				}
			}
		}
	}
	for _, action := range d.Actions {
		for _, sourceID := range action.SourceIDs {
			if _, ok := allowed[sourceID]; !ok {
				if _, evidence := findEvidence(d.Evidence, sourceID); !evidence {
					return errors.New("行动引用了未验证来源")
				}
			}
		}
	}
	return nil
}

func findEvidence(items []Evidence, id string) (Evidence, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return Evidence{}, false
}

func filterConfirmedMaintenanceItems(d *Dashboard) error {
	filterEvidence := d.Evidence[:0]
	for _, item := range d.Evidence {
		protected, err := stableContextIsProtected("evidence:" + item.ID)
		if err != nil {
			return err
		}
		if !protected {
			filterEvidence = append(filterEvidence, item)
		}
	}
	d.Evidence = filterEvidence
	filterSignals := d.Signals[:0]
	for _, item := range d.Signals {
		protected, err := stableContextIsProtected("signal:" + item.ID)
		if err != nil {
			return err
		}
		if !protected {
			filterSignals = append(filterSignals, item)
		}
	}
	d.Signals = filterSignals
	filterDecisions := d.Decisions[:0]
	for _, item := range d.Decisions {
		protected, err := stableContextIsProtected("decision:" + item.ID)
		if err != nil {
			return err
		}
		if !protected {
			filterDecisions = append(filterDecisions, item)
		}
	}
	d.Decisions = filterDecisions
	return nil
}

func stableContextIsProtected(itemKey string) (bool, error) {
	if strings.TrimSpace(appDataScope.OwnerID) == "" || strings.TrimSpace(appDataScope.ProfileID) == "" {
		return false, errors.New("维护作用域未设置")
	}
	var state string
	err := db.QueryRow(`SELECT state FROM stable_context_items WHERE owner_id=? AND profile_id=? AND item_key=?`, appDataScope.OwnerID, appDataScope.ProfileID, itemKey).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state == "active" || state == "removed", nil
}

func writeBoundedJSON(out io.Writer, value any, maxBytes int) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(raw) > maxBytes {
		return errors.New("维护输出过大")
	}
	_, err = out.Write(append(raw, '\n'))
	return err
}

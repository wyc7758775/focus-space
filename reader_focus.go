package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	readerFocusSchemaVersion       = 1
	readerFocusTopicLimit          = 8
	readerFocusTermsPerTopicLimit  = 32
	readerFocusTermRunes           = 64
	readerFocusCandidateScanLimit  = 500
	readerFocusRecommendationLimit = 5
	readerFocusPerRunLimit         = 5
	readerFocusDailyArticleLimit   = 10
	readerFocusDefaultTokenBudget  = 8000
	readerFocusMinTokenBudget      = 1000
	readerFocusMaxTokenBudget      = 12000
	readerFocusInputRunes          = 6000
	readerFocusRunRetentionDays    = 90
	readerFocusRunRetentionLimit   = 1024
)

// errReaderFocusPayloadConflict is intentionally kept separate from the
// generic idempotency error. A focus run is keyed by its article revisions,
// while this guard binds the complete draft payload (signals/actions/
// decisions/evidence as well) to that run. A caller that reuses the article
// batch with a different draft must fail closed before any projection write is
// committed.
var errReaderFocusPayloadConflict = errors.New("reader focus payload conflict")

const (
	readerFocusModeRecommendOnly = "recommend_only"
	readerFocusModeAutoNew       = "auto_new"
	readerFocusModeAutoBootstrap = "auto_bootstrap"
)

const (
	readerFocusBootstrapPending = "pending"
	readerFocusBootstrapSkipped = "skipped"
	readerFocusBootstrapActive  = "active"
	readerFocusBootstrapDone    = "done"
)

// ReaderFocusTopic is user preference data, not model-generated content.
type ReaderFocusTopic struct {
	ID                      string   `json:"id"`
	Label                   string   `json:"label"`
	Terms                   []string `json:"terms"`
	Enabled                 bool     `json:"enabled"`
	WatchSince              string   `json:"watch_since"`
	BootstrapStatus         string   `json:"bootstrap_status"`
	BootstrapMode           string   `json:"bootstrap_mode"`
	BootstrapLimit          int      `json:"bootstrap_limit"`
	BootstrapProcessedCount int      `json:"bootstrap_processed_count"`
	Revision                int      `json:"revision"`
	CreatedAt               string   `json:"created_at"`
	UpdatedAt               string   `json:"updated_at"`
}

type ReaderFocusPolicy struct {
	Mode              string `json:"mode"`
	DailyArticleLimit int    `json:"daily_article_limit"`
	DailyTokenBudget  int    `json:"daily_token_budget"`
	PerRunLimit       int    `json:"per_run_limit"`
	BootstrapLimit    int    `json:"bootstrap_limit"`
	FullTextMode      string `json:"full_text_mode"`
}

type ReaderFocusState struct {
	SchemaVersion int                `json:"schema_version"`
	Revision      int                `json:"revision"`
	Topics        []ReaderFocusTopic `json:"topics"`
	Policy        ReaderFocusPolicy  `json:"policy"`
	Bootstrap     string             `json:"bootstrap_status"`
}

// ReaderFocusProjection is embedded in Dashboard as an optional field. It is
// rebuilt from local state on reads, so older dashboard snapshots remain valid.
type ReaderFocusProjection struct {
	SchemaVersion          int                `json:"schema_version"`
	Topics                 []ReaderFocusTopic `json:"topics"`
	Policy                 ReaderFocusPolicy  `json:"policy"`
	CandidateCount         int                `json:"candidate_count"`
	CandidateScanTruncated bool               `json:"candidate_scan_truncated,omitempty"`
	PendingCount           int                `json:"pending_count"`
	HistoricalUnreadCount  int                `json:"historical_unread_count"`
	EstimatedTokensLow     int                `json:"estimated_tokens_low"`
	EstimatedTokensHigh    int                `json:"estimated_tokens_high"`
	DailyUsedTokens        int                `json:"daily_used_tokens"`
	DailyRemainingTokens   int                `json:"daily_remaining_tokens"`
	BootstrapStatus        string             `json:"bootstrap_status"`
	GeneratedAt            string             `json:"generated_at"`
}

type ReaderRecommendationItem struct {
	EntryID       string             `json:"entry_id"`
	Title         string             `json:"title"`
	Source        string             `json:"source"`
	SourceURL     string             `json:"source_url"`
	PublishedAt   string             `json:"published_at,omitempty"`
	MatchedTopics []string           `json:"matched_topics"`
	Score         int                `json:"score"`
	Status        string             `json:"status"`
	Reason        string             `json:"reason"`
	Overview      *ArticleAIOverview `json:"overview,omitempty"`
}

type ReaderRecommendationsProjection struct {
	SchemaVersion      int                        `json:"schema_version"`
	Items              []ReaderRecommendationItem `json:"items"`
	LocalCount         int                        `json:"local_count"`
	WebSupplementCount int                        `json:"web_supplement_count"`
	GeneratedAt        string                     `json:"generated_at"`
}

type readerFocusCandidate struct {
	ID          string
	SourceID    string
	Title       string
	Source      string
	URL         string
	PublishedAt string
	FetchedAt   string
	Summary     string
	Hash        string
	AutoExtract bool
	Overview    *ArticleAIOverview
	Score       int
	Topics      []string
	Reason      string
}

type readerFocusUpdateRequest struct {
	SchemaVersion    int                        `json:"schema_version"`
	ExpectedRevision int                        `json:"expected_revision"`
	Action           string                     `json:"action"`
	Topic            *readerFocusTopicPatch     `json:"topic,omitempty"`
	Policy           *readerFocusPolicyPatch    `json:"policy,omitempty"`
	Bootstrap        *readerFocusBootstrapPatch `json:"bootstrap,omitempty"`
}

type readerFocusTopicPatch struct {
	ID      string   `json:"id,omitempty"`
	Label   string   `json:"label"`
	Terms   []string `json:"terms"`
	Enabled *bool    `json:"enabled,omitempty"`
}

type readerFocusPolicyPatch struct {
	Mode              *string `json:"mode,omitempty"`
	DailyArticleLimit *int    `json:"daily_article_limit,omitempty"`
	DailyTokenBudget  *int    `json:"daily_token_budget,omitempty"`
	PerRunLimit       *int    `json:"per_run_limit,omitempty"`
	BootstrapLimit    *int    `json:"bootstrap_limit,omitempty"`
	FullTextMode      *string `json:"full_text_mode,omitempty"`
}

type readerFocusBootstrapPatch struct {
	TopicID  string `json:"topic_id,omitempty"`
	Decision string `json:"decision"`
	Limit    int    `json:"limit,omitempty"`
}

func defaultReaderFocusPolicy() ReaderFocusPolicy {
	return ReaderFocusPolicy{
		Mode: readerFocusModeRecommendOnly, DailyArticleLimit: readerFocusDailyArticleLimit,
		DailyTokenBudget: readerFocusDefaultTokenBudget, PerRunLimit: readerFocusPerRunLimit,
		BootstrapLimit: 5, FullTextMode: "summary",
	}
}

func validReaderFocusPolicy(policy ReaderFocusPolicy) bool {
	return (policy.Mode == readerFocusModeRecommendOnly || policy.Mode == readerFocusModeAutoNew || policy.Mode == readerFocusModeAutoBootstrap) &&
		policy.DailyArticleLimit >= 1 && policy.DailyArticleLimit <= readerFocusDailyArticleLimit &&
		policy.DailyTokenBudget >= readerFocusMinTokenBudget && policy.DailyTokenBudget <= readerFocusMaxTokenBudget &&
		policy.PerRunLimit >= 1 && policy.PerRunLimit <= readerFocusPerRunLimit &&
		(policy.BootstrapLimit == 3 || policy.BootstrapLimit == 5 || policy.BootstrapLimit == 10) &&
		(policy.FullTextMode == "summary" || policy.FullTextMode == "explicit_full_text")
}

func validReaderFocusBootstrapLimit(limit int) bool {
	return limit == 3 || limit == 5 || limit == 10
}

// readerFocusTopicBootstrapLimit accepts zero for hand-built legacy state in
// callers/tests. Persisted rows from migration 050 always carry an explicit
// value; zero falls back to the compatibility policy limit.
func readerFocusTopicBootstrapLimit(topic ReaderFocusTopic, policy ReaderFocusPolicy) int {
	if validReaderFocusBootstrapLimit(topic.BootstrapLimit) {
		return topic.BootstrapLimit
	}
	if validReaderFocusBootstrapLimit(policy.BootstrapLimit) {
		return policy.BootstrapLimit
	}
	return defaultReaderFocusPolicy().BootstrapLimit
}

func normalizeReaderFocusTerms(label string, terms []string) ([]string, error) {
	if len(terms) == 0 {
		terms = []string{label}
	}
	if len(terms) > readerFocusTermsPerTopicLimit {
		return nil, errors.New("关注主题关键词过多")
	}
	seen := make(map[string]struct{}, len(terms))
	result := make([]string, 0, len(terms))
	for _, raw := range terms {
		term := normalizeReaderFocusText(raw)
		if term == "" || len([]rune(term)) > readerFocusTermRunes {
			return nil, errors.New("关注主题关键词不合法")
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		result = append(result, term)
	}
	if len(result) == 0 {
		return nil, errors.New("关注主题至少需要一个关键词")
	}
	return result, nil
}

func normalizeReaderFocusText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func normalizeReaderFocusTopic(patch *readerFocusTopicPatch, now string) (ReaderFocusTopic, error) {
	if patch == nil || !validText(patch.Label, 120, true) {
		return ReaderFocusTopic{}, errors.New("关注主题名称不合法")
	}
	terms, err := normalizeReaderFocusTerms(patch.Label, patch.Terms)
	if err != nil {
		return ReaderFocusTopic{}, err
	}
	id := strings.TrimSpace(patch.ID)
	if id == "" {
		id = stableObjectID("reader-focus", normalizeReaderFocusText(patch.Label)+"\x00"+strings.Join(terms, "\x00"))
	}
	if !validText(id, 128, true) {
		return ReaderFocusTopic{}, errors.New("关注主题标识不合法")
	}
	enabled := true
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	return ReaderFocusTopic{ID: id, Label: strings.TrimSpace(patch.Label), Terms: terms, Enabled: enabled, WatchSince: now, BootstrapStatus: readerFocusBootstrapPending, BootstrapMode: "none", Revision: 1, CreatedAt: now, UpdatedAt: now}, nil
}

// readerFocusAnalysisText is the only path that selects model input for a
// focus run. Summary mode never opens the stored article body; full text is
// opt-in and remains bounded by the same 6,000-rune limit.
func readerFocusAnalysisText(candidate readerFocusCandidate, policy ReaderFocusPolicy) (string, error) {
	return readerFocusAnalysisTextTx(nil, candidate, policy)
}

func readerFocusAnalysisTextTx(tx *sql.Tx, candidate readerFocusCandidate, policy ReaderFocusPolicy) (string, error) {
	// Keep the model input evidence-shaped and bounded: title/source provide
	// identity and context, while the RSS summary remains the default body.
	// Do not pass the stored article body unless the user explicitly enabled it.
	parts := make([]string, 0, 3)
	if title := strings.TrimSpace(candidate.Title); title != "" {
		parts = append(parts, "标题："+title)
	}
	if source := strings.TrimSpace(candidate.Source); source != "" {
		parts = append(parts, "来源："+source)
	}
	if summary := strings.TrimSpace(candidate.Summary); summary != "" {
		parts = append(parts, "摘要："+summary)
	}
	text := strings.Join(parts, "\n")
	if policy.FullTextMode == "explicit_full_text" && candidate.ID != "" && candidate.Hash != "" {
		var fullText string
		var err error
		query := `SELECT content_text FROM entry_full_texts WHERE entry_id=? AND content_hash=? AND status IN ('ok','stale') AND content_text<>'' LIMIT 1`
		if tx != nil {
			err = tx.QueryRow(query, candidate.ID, candidate.Hash).Scan(&fullText)
		} else {
			err = db.QueryRow(query, candidate.ID, candidate.Hash).Scan(&fullText)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if strings.TrimSpace(fullText) != "" {
			fullParts := append([]string{}, parts...)
			fullParts = append(fullParts, "正文："+fullText)
			text = strings.Join(fullParts, "\n")
		}
	}
	return limitText(text, readerFocusInputRunes), nil
}

func readReaderFocusState() (ReaderFocusState, error) {
	if db == nil {
		return ReaderFocusState{}, errors.New("数据库未打开")
	}
	state, err := readReaderFocusStateTx(nil)
	if err != nil {
		return ReaderFocusState{}, err
	}
	return state, nil
}

func readReaderFocusStateTx(tx *sql.Tx) (ReaderFocusState, error) {
	policy := defaultReaderFocusPolicy()
	var revision int
	var mode, fullText string
	var dailyLimit, dailyBudget, perRun, bootstrapLimit int
	query := func(dest ...any) error {
		if tx != nil {
			return tx.QueryRow(`SELECT mode,daily_article_limit,daily_token_budget,per_run_limit,bootstrap_limit,full_text_mode,revision FROM reader_focus_policy WHERE id='default'`).Scan(dest...)
		}
		return db.QueryRow(`SELECT mode,daily_article_limit,daily_token_budget,per_run_limit,bootstrap_limit,full_text_mode,revision FROM reader_focus_policy WHERE id='default'`).Scan(dest...)
	}
	if err := query(&mode, &dailyLimit, &dailyBudget, &perRun, &bootstrapLimit, &fullText, &revision); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReaderFocusState{}, err
	}
	if mode != "" {
		policy = ReaderFocusPolicy{Mode: mode, DailyArticleLimit: dailyLimit, DailyTokenBudget: dailyBudget, PerRunLimit: perRun, BootstrapLimit: bootstrapLimit, FullTextMode: fullText}
	}
	if !validReaderFocusPolicy(policy) {
		return ReaderFocusState{}, errors.New("关注策略数据损坏")
	}
	rowsQuery := `SELECT id,label,terms_json,enabled,watch_since,bootstrap_status,bootstrap_mode,bootstrap_limit,bootstrap_processed_count,revision,created_at,updated_at FROM reader_focus_topics ORDER BY created_at ASC,id ASC LIMIT ?`
	var rows *sql.Rows
	var err error
	if tx != nil {
		rows, err = tx.Query(rowsQuery, readerFocusTopicLimit)
	} else {
		rows, err = db.Query(rowsQuery, readerFocusTopicLimit)
	}
	legacyTopicBudget := false
	if err != nil && readerFocusSchemaMissing(err) {
		// A process can briefly read an older 049 database while its additive
		// migration is pending. Fall back to the policy quota for that window.
		legacyTopicBudget = true
		legacyQuery := `SELECT id,label,terms_json,enabled,watch_since,bootstrap_status,bootstrap_mode,revision,created_at,updated_at FROM reader_focus_topics ORDER BY created_at ASC,id ASC LIMIT ?`
		if tx != nil {
			rows, err = tx.Query(legacyQuery, readerFocusTopicLimit)
		} else {
			rows, err = db.Query(legacyQuery, readerFocusTopicLimit)
		}
	}
	if err != nil {
		return ReaderFocusState{}, err
	}
	defer rows.Close()
	topics := make([]ReaderFocusTopic, 0, readerFocusTopicLimit)
	bootstrap := readerFocusBootstrapSkipped
	for rows.Next() {
		var topic ReaderFocusTopic
		var termsJSON string
		var enabled int
		var scanErr error
		if legacyTopicBudget {
			scanErr = rows.Scan(&topic.ID, &topic.Label, &termsJSON, &enabled, &topic.WatchSince, &topic.BootstrapStatus, &topic.BootstrapMode, &topic.Revision, &topic.CreatedAt, &topic.UpdatedAt)
			topic.BootstrapLimit = policy.BootstrapLimit
		} else {
			scanErr = rows.Scan(&topic.ID, &topic.Label, &termsJSON, &enabled, &topic.WatchSince, &topic.BootstrapStatus, &topic.BootstrapMode, &topic.BootstrapLimit, &topic.BootstrapProcessedCount, &topic.Revision, &topic.CreatedAt, &topic.UpdatedAt)
		}
		if scanErr != nil {
			return ReaderFocusState{}, scanErr
		}
		if err := json.Unmarshal([]byte(termsJSON), &topic.Terms); err != nil {
			return ReaderFocusState{}, err
		}
		if !validReaderFocusTopic(topic) {
			return ReaderFocusState{}, errors.New("关注主题数据损坏")
		}
		topic.Enabled = enabled == 1
		topics = append(topics, topic)
		if topic.Enabled && topic.BootstrapStatus == readerFocusBootstrapPending {
			bootstrap = readerFocusBootstrapPending
		} else if topic.Enabled && topic.BootstrapStatus == readerFocusBootstrapActive && bootstrap != readerFocusBootstrapPending {
			bootstrap = readerFocusBootstrapActive
		} else if topic.Enabled && topic.BootstrapStatus == readerFocusBootstrapDone && bootstrap == readerFocusBootstrapSkipped {
			bootstrap = readerFocusBootstrapDone
		}
	}
	if err := rows.Err(); err != nil {
		return ReaderFocusState{}, err
	}
	if len(topics) == 0 {
		bootstrap = readerFocusBootstrapSkipped
	}
	return ReaderFocusState{SchemaVersion: readerFocusSchemaVersion, Revision: revision, Topics: topics, Policy: policy, Bootstrap: bootstrap}, nil
}

func validReaderFocusTopic(topic ReaderFocusTopic) bool {
	if !validText(topic.ID, 128, true) || !validText(topic.Label, 120, true) ||
		len(topic.Terms) == 0 || len(topic.Terms) > readerFocusTermsPerTopicLimit ||
		(topic.BootstrapLimit != 0 && !validReaderFocusBootstrapLimit(topic.BootstrapLimit)) || topic.BootstrapProcessedCount < 0 ||
		(topic.BootstrapStatus != readerFocusBootstrapPending && topic.BootstrapStatus != readerFocusBootstrapSkipped && topic.BootstrapStatus != readerFocusBootstrapActive && topic.BootstrapStatus != readerFocusBootstrapDone) ||
		(topic.BootstrapMode != "" && topic.BootstrapMode != "none" && topic.BootstrapMode != "analyze" && topic.BootstrapMode != "future_only") {
		return false
	}
	for _, term := range topic.Terms {
		if term == "" || len([]rune(term)) > readerFocusTermRunes {
			return false
		}
	}
	return true
}

func validateReaderFocusProjection(focus *ReaderFocusProjection) error {
	if focus == nil {
		return nil
	}
	if focus.SchemaVersion != readerFocusSchemaVersion || len(focus.Topics) > readerFocusTopicLimit ||
		focus.CandidateCount < 0 || focus.PendingCount < 0 || focus.HistoricalUnreadCount < 0 || focus.EstimatedTokensLow < 0 || focus.EstimatedTokensHigh < 0 || focus.DailyUsedTokens < 0 || focus.DailyRemainingTokens < 0 {
		return errors.New("关注投影不合法")
	}
	if !validReaderFocusPolicy(focus.Policy) {
		return errors.New("关注策略不合法")
	}
	for _, topic := range focus.Topics {
		if !validReaderFocusTopic(topic) {
			return errors.New("关注主题不合法")
		}
	}
	return nil
}

func validateReaderRecommendationsProjection(recommendations *ReaderRecommendationsProjection) error {
	if recommendations == nil {
		return nil
	}
	if recommendations.SchemaVersion != readerFocusSchemaVersion || len(recommendations.Items) > readerFocusRecommendationLimit || recommendations.LocalCount < 0 || recommendations.WebSupplementCount < 0 {
		return errors.New("关注推荐投影不合法")
	}
	for _, item := range recommendations.Items {
		if !validText(item.EntryID, 128, true) || !validText(item.Title, 240, true) || !validText(item.Source, 160, true) || !validText(item.SourceURL, 2048, false) || !validText(item.Reason, 500, false) || item.Score < 0 || len(item.MatchedTopics) > readerFocusTopicLimit {
			return errors.New("关注推荐条目不合法")
		}
		if item.Status != "pending" && item.Status != "queued" && item.Status != "budget_limited" && item.Status != "ready" && item.Status != "failed" {
			return errors.New("关注推荐状态不合法")
		}
	}
	return nil
}

func readerFocusHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := readReaderFocusState()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, state)
	case http.MethodPatch:
		var req readerFocusUpdateRequest
		if err := app.DecodeStrictJSON(w, r, 16<<10, &req); err != nil || req.SchemaVersion != readerFocusSchemaVersion || req.ExpectedRevision < 1 {
			app.UserErr(w, http.StatusBadRequest, "关注设置格式不合法")
			return
		}
		if err := validateReaderFocusUpdateShape(req); err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := updateReaderFocus(req); err != nil {
			if errors.Is(err, errReaderFocusRevisionConflict) {
				app.UserErr(w, http.StatusConflict, "关注设置已变化，请重新读取")
				return
			}
			if errors.Is(err, errReaderFocusInvalid) {
				app.UserErr(w, http.StatusBadRequest, err.Error())
				return
			}
			app.HTTPErr(w, err)
			return
		}
		state, err := readReaderFocusState()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, state)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

var (
	errReaderFocusRevisionConflict = errors.New("reader focus revision conflict")
	errReaderFocusInvalid          = errors.New("reader focus request invalid")
)

// validateReaderFocusUpdateShape keeps each mutation branch closed. The JSON
// decoder rejects unknown keys; this second check rejects known fields that
// belong to a different action so a caller cannot silently believe a policy or
// bootstrap change was applied together with a topic edit.
func validateReaderFocusUpdateShape(req readerFocusUpdateRequest) error {
	invalid := func(message string) error {
		return fmt.Errorf("关注设置格式不合法: %s", message)
	}
	switch req.Action {
	case "upsert_topic":
		if req.Topic == nil || req.Policy != nil || req.Bootstrap != nil {
			return invalid("upsert_topic 只能包含 topic")
		}
	case "remove_topic":
		if req.Topic == nil || req.Policy != nil || req.Bootstrap != nil {
			return invalid("remove_topic 只能包含 topic")
		}
		if strings.TrimSpace(req.Topic.ID) == "" || req.Topic.Label != "" || req.Topic.Terms != nil || req.Topic.Enabled != nil {
			return invalid("remove_topic 只允许 topic.id")
		}
	case "set_policy":
		if req.Policy == nil || req.Topic != nil || req.Bootstrap != nil {
			return invalid("set_policy 只能包含 policy")
		}
	case "set_bootstrap":
		if req.Bootstrap == nil || req.Topic != nil || req.Policy != nil {
			return invalid("set_bootstrap 只能包含 bootstrap")
		}
	default:
		return invalid("未知 action")
	}
	return nil
}

func updateReaderFocus(req readerFocusUpdateRequest) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRow(`SELECT revision FROM reader_focus_policy WHERE id='default'`).Scan(&current); err != nil {
		return err
	}
	if current != req.ExpectedRevision {
		return errReaderFocusRevisionConflict
	}
	now := time.Now().UTC().Format(time.RFC3339)
	switch req.Action {
	case "upsert_topic":
		topic, err := normalizeReaderFocusTopic(req.Topic, now)
		if err != nil {
			return fmt.Errorf("%w: %v", errReaderFocusInvalid, err)
		}
		var existingRevision, existingEnabled int
		var existingStatus, existingMode, existingSince, createdAt string
		err = tx.QueryRow(`SELECT revision,enabled,bootstrap_status,bootstrap_mode,watch_since,created_at FROM reader_focus_topics WHERE id=?`, topic.ID).Scan(&existingRevision, &existingEnabled, &existingStatus, &existingMode, &existingSince, &createdAt)
		if errors.Is(err, sql.ErrNoRows) {
			var count int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM reader_focus_topics`).Scan(&count); err != nil {
				return err
			}
			if count >= readerFocusTopicLimit {
				return fmt.Errorf("%w: 关注主题已达到上限", errReaderFocusInvalid)
			}
			var defaultBootstrapLimit int
			if err := tx.QueryRow(`SELECT bootstrap_limit FROM reader_focus_policy WHERE id='default'`).Scan(&defaultBootstrapLimit); err != nil {
				return err
			}
			if !validReaderFocusBootstrapLimit(defaultBootstrapLimit) {
				defaultBootstrapLimit = defaultReaderFocusPolicy().BootstrapLimit
			}
			terms, _ := json.Marshal(topic.Terms)
			_, err = tx.Exec(`INSERT INTO reader_focus_topics(id,label,terms_json,enabled,watch_since,bootstrap_status,bootstrap_mode,bootstrap_limit,bootstrap_processed_count,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, topic.ID, topic.Label, terms, boolInt(topic.Enabled), topic.WatchSince, topic.BootstrapStatus, topic.BootstrapMode, defaultBootstrapLimit, 0, 1, now, now)
		} else if err == nil {
			// Omitting enabled on an edit must preserve a paused topic. Re-enabling
			// a paused topic creates a fresh bounded bootstrap opportunity.
			if req.Topic.Enabled == nil {
				topic.Enabled = existingEnabled == 1
			}
			terms, _ := json.Marshal(topic.Terms)
			if existingSince == "" {
				existingSince = topic.WatchSince
			}
			if createdAt == "" {
				createdAt = now
			}
			status := existingStatus
			if status == "" {
				status = readerFocusBootstrapPending
			}
			if existingEnabled == 0 && topic.Enabled {
				status = readerFocusBootstrapPending
				existingMode = "none"
			}
			if existingMode == "" {
				existingMode = "none"
			}
			_, err = tx.Exec(`UPDATE reader_focus_topics SET label=?,terms_json=?,enabled=?,watch_since=?,bootstrap_status=?,bootstrap_mode=?,revision=?,created_at=?,updated_at=? WHERE id=?`, topic.Label, terms, boolInt(topic.Enabled), existingSince, status, existingMode, existingRevision+1, createdAt, now, topic.ID)
		} else {
			return err
		}
		if err != nil {
			return err
		}
	case "remove_topic":
		if req.Topic == nil || !validText(req.Topic.ID, 128, true) {
			return fmt.Errorf("%w: 缺少主题标识", errReaderFocusInvalid)
		}
		if _, err := tx.Exec(`DELETE FROM reader_focus_topics WHERE id=?`, req.Topic.ID); err != nil {
			return err
		}
	case "set_policy":
		if req.Policy == nil {
			return fmt.Errorf("%w: 缺少策略", errReaderFocusInvalid)
		}
		var policy ReaderFocusPolicy
		if _, err := readReaderFocusStateTx(tx); err != nil {
			return err
		}
		var mode, fullText string
		var dailyLimit, dailyBudget, perRun, bootstrapLimit int
		if err := tx.QueryRow(`SELECT mode,daily_article_limit,daily_token_budget,per_run_limit,bootstrap_limit,full_text_mode FROM reader_focus_policy WHERE id='default'`).Scan(&mode, &dailyLimit, &dailyBudget, &perRun, &bootstrapLimit, &fullText); err != nil {
			return err
		}
		policy = ReaderFocusPolicy{Mode: mode, DailyArticleLimit: dailyLimit, DailyTokenBudget: dailyBudget, PerRunLimit: perRun, BootstrapLimit: bootstrapLimit, FullTextMode: fullText}
		if req.Policy.Mode != nil {
			policy.Mode = *req.Policy.Mode
		}
		if req.Policy.DailyArticleLimit != nil {
			policy.DailyArticleLimit = *req.Policy.DailyArticleLimit
		}
		if req.Policy.DailyTokenBudget != nil {
			policy.DailyTokenBudget = *req.Policy.DailyTokenBudget
		}
		if req.Policy.PerRunLimit != nil {
			policy.PerRunLimit = *req.Policy.PerRunLimit
		}
		if req.Policy.BootstrapLimit != nil {
			policy.BootstrapLimit = *req.Policy.BootstrapLimit
		}
		if req.Policy.FullTextMode != nil {
			policy.FullTextMode = *req.Policy.FullTextMode
		}
		if !validReaderFocusPolicy(policy) {
			return fmt.Errorf("%w: 策略参数不合法", errReaderFocusInvalid)
		}
		_, err := tx.Exec(`UPDATE reader_focus_policy SET mode=?,daily_article_limit=?,daily_token_budget=?,per_run_limit=?,bootstrap_limit=?,full_text_mode=?,revision=?,updated_at=? WHERE id='default'`, policy.Mode, policy.DailyArticleLimit, policy.DailyTokenBudget, policy.PerRunLimit, policy.BootstrapLimit, policy.FullTextMode, current+1, now)
		if err != nil {
			return err
		}
	case "set_bootstrap":
		if req.Bootstrap == nil || (req.Bootstrap.Decision != "analyze" && req.Bootstrap.Decision != "future_only" && req.Bootstrap.Decision != "none") {
			return fmt.Errorf("%w: 首次分析选择不合法", errReaderFocusInvalid)
		}
		targetIDs, err := readerFocusBootstrapTargetIDsTx(tx, req.Bootstrap.TopicID)
		if err != nil {
			return err
		}
		limit := req.Bootstrap.Limit
		if limit == 0 {
			limit = 5
		}
		if limit != 3 && limit != 5 && limit != 10 {
			return fmt.Errorf("%w: 首次分析数量不合法", errReaderFocusInvalid)
		}
		mode := readerFocusModeRecommendOnly
		status := readerFocusBootstrapSkipped
		bootstrapMode := "none"
		switch req.Bootstrap.Decision {
		case "analyze":
			mode, status = readerFocusModeAutoBootstrap, readerFocusBootstrapActive
			bootstrapMode = "analyze"
		case "future_only":
			mode, status = readerFocusModeAutoNew, readerFocusBootstrapActive
			bootstrapMode = "future_only"
		}
		if req.Bootstrap.Decision == "analyze" {
			if err := clearFailedReaderFocusClaimsForTopicTx(tx, targetIDs[0], now[:10]); err != nil {
				return err
			}
			topic, topicErr := readerFocusTopicTx(tx, targetIDs[0])
			if topicErr != nil {
				return topicErr
			}
			used, usedErr := readerFocusTopicBootstrapUsedTx(tx, topic.ID)
			if usedErr != nil {
				return usedErr
			}
			hasHistorical, historyErr := readerFocusHasHistoricalCandidateTx(tx, topic, nil)
			if historyErr != nil {
				return historyErr
			}
			if used >= limit || !hasHistorical {
				// Nothing needs a one-off historical batch. Enter the steady
				// future-only mode immediately without spending model budget.
				mode, status = readerFocusModeAutoNew, readerFocusBootstrapDone
				bootstrapMode = "future_only"
			}
		}
		// Keep the local variable for compatibility with older payload semantics;
		// the persisted aggregate is recomputed from every topic below.
		_ = mode
		if _, err := tx.Exec(`UPDATE reader_focus_topics SET bootstrap_status=?,bootstrap_mode=?,bootstrap_limit=?,watch_since=CASE WHEN ?='future_only' THEN ? ELSE watch_since END,updated_at=?,revision=revision+1 WHERE id=? AND enabled=1`, status, bootstrapMode, limit, req.Bootstrap.Decision, now, now, targetIDs[0]); err != nil {
			return err
		}
		if err := recomputeReaderFocusPolicyModeTx(tx, current+1, now); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: 未知操作", errReaderFocusInvalid)
	}
	if req.Action != "set_policy" && req.Action != "set_bootstrap" {
		if _, err := tx.Exec(`UPDATE reader_focus_policy SET revision=?,updated_at=? WHERE id='default'`, current+1, now); err != nil {
			return err
		}
	}
	if req.Action == "upsert_topic" || req.Action == "remove_topic" {
		if err := recomputeReaderFocusPolicyModeTx(tx, current+1, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// recomputeReaderFocusPolicyModeTx keeps the aggregate mode as a compatibility
// projection. Topic bootstrap_mode is the source of truth, so changing one
// topic cannot disable or broaden another topic's maintenance.
func recomputeReaderFocusPolicyModeTx(tx *sql.Tx, revision int, now string) error {
	var hasAnalyze, hasFuture int
	if err := tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN enabled=1 AND bootstrap_mode='analyze' AND bootstrap_status='active' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN enabled=1 AND bootstrap_mode='future_only' AND bootstrap_status IN ('active','done') THEN 1 ELSE 0 END),0) FROM reader_focus_topics`).Scan(&hasAnalyze, &hasFuture); err != nil {
		return err
	}
	mode := readerFocusModeRecommendOnly
	if hasAnalyze > 0 {
		mode = readerFocusModeAutoBootstrap
	} else if hasFuture > 0 {
		mode = readerFocusModeAutoNew
	}
	_, err := tx.Exec(`UPDATE reader_focus_policy SET mode=?,revision=?,updated_at=? WHERE id='default'`, mode, revision, now)
	return err
}

// readerFocusBootstrapTargetIDsTx keeps the legacy single-topic payload
// compatible while making multi-topic decisions explicit. A global policy
// still exists for backwards compatibility, but its per-topic activation is
// always scoped to exactly one topic here.
func readerFocusBootstrapTargetIDsTx(tx *sql.Tx, requestedID string) ([]string, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID != "" {
		if !validText(requestedID, 128, true) {
			return nil, fmt.Errorf("%w: 主题标识不合法", errReaderFocusInvalid)
		}
		var enabled int
		if err := tx.QueryRow(`SELECT enabled FROM reader_focus_topics WHERE id=?`, requestedID).Scan(&enabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: 关注主题不存在", errReaderFocusInvalid)
			}
			return nil, err
		}
		if enabled != 1 {
			return nil, fmt.Errorf("%w: 关注主题未启用", errReaderFocusInvalid)
		}
		return []string{requestedID}, nil
	}
	rows, err := tx.Query(`SELECT id FROM reader_focus_topics WHERE enabled=1 ORDER BY created_at ASC,id ASC LIMIT ?`, readerFocusTopicLimit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: 没有启用的关注主题", errReaderFocusInvalid)
	}
	if len(ids) > 1 {
		return nil, fmt.Errorf("%w: 多个关注主题必须指定 topic_id", errReaderFocusInvalid)
	}
	return ids, nil
}

func readerRecommendationReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "daily"
	}
	if scope != "daily" && scope != "bootstrap" {
		app.UserErr(w, http.StatusBadRequest, "推荐范围不合法")
		return
	}
	limit := 5
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > readerFocusRecommendationLimit {
			app.UserErr(w, http.StatusBadRequest, "推荐数量不合法")
			return
		}
		limit = parsed
	}
	topicID := strings.TrimSpace(r.URL.Query().Get("topic_id"))
	if topicID != "" && !validText(topicID, 128, true) {
		app.UserErr(w, http.StatusBadRequest, "主题标识不合法")
		return
	}
	projection, recommendations, err := buildReaderFocusProjectionsForTopic(scope, limit, topicID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	projectionBytes, _ := json.Marshal(map[string]any{"reader_focus": projection, "reader_recommendations": recommendations})
	if len(projectionBytes) > 512<<10 {
		app.UserErr(w, http.StatusBadGateway, "推荐结果过大")
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"reader_focus": projection, "reader_recommendations": recommendations})
}

func buildReaderFocusProjections(scope string, limit int) (*ReaderFocusProjection, *ReaderRecommendationsProjection, error) {
	return buildReaderFocusProjectionsForTopic(scope, limit, "")
}

func buildReaderFocusProjectionsForTopic(scope string, limit int, topicID string) (*ReaderFocusProjection, *ReaderRecommendationsProjection, error) {
	state, err := readReaderFocusState()
	if err != nil {
		return nil, nil, err
	}
	if topicID != "" {
		state, err = readerFocusStateForTopic(state, topicID)
		if err != nil {
			return nil, nil, err
		}
	}
	now := time.Now().UTC()
	projection := &ReaderFocusProjection{SchemaVersion: readerFocusSchemaVersion, Topics: state.Topics, Policy: state.Policy, BootstrapStatus: state.Bootstrap, GeneratedAt: now.Format(time.RFC3339)}
	recommendations := &ReaderRecommendationsProjection{SchemaVersion: readerFocusSchemaVersion, Items: []ReaderRecommendationItem{}, GeneratedAt: now.Format(time.RFC3339)}
	if len(state.Topics) == 0 {
		return projection, recommendations, nil
	}
	candidates, candidateScanTruncated, err := queryReaderFocusCandidatesWithMeta(state.Topics)
	if err != nil {
		return nil, nil, err
	}
	projection.CandidateScanTruncated = candidateScanTruncated
	filtered := make([]readerFocusCandidate, 0, len(candidates))
	newCandidates := make([]readerFocusCandidate, 0, len(candidates))
	historicalCandidates := make([]readerFocusCandidate, 0, len(candidates))
	newByID := make(map[string]bool, len(candidates))
	// Keep these counts independent from the requested scope. The UI uses the
	// historical count to explain backlog cost even when the daily list only
	// contains post-watch_since items.
	for _, candidate := range candidates {
		if candidate.Score <= 0 {
			continue
		}
		projection.CandidateCount++
		if candidate.Overview == nil {
			projection.PendingCount++
		}
		if !readerFocusCandidateIsNew(candidate, state.Topics) {
			projection.HistoricalUnreadCount++
		}
	}
	for _, candidate := range candidates {
		if candidate.Score <= 0 {
			continue
		}
		if scope != "daily" || readerFocusCandidateIsNew(candidate, state.Topics) {
			newCandidates = append(newCandidates, candidate)
			newByID[candidate.ID] = true
		} else {
			historicalCandidates = append(historicalCandidates, candidate)
		}
	}
	// Ready overviews lead the list so Chat can answer with evidence immediately.
	// Within the same overview state, daily recommendations prefer post-watch_since
	// content and then use historical unread items to fill the requested page.
	filtered = append(filtered, newCandidates...)
	filtered = append(filtered, historicalCandidates...)
	sort.SliceStable(filtered, func(i, j int) bool {
		leftReady := filtered[i].Overview != nil && filtered[i].Overview.Status == "ready"
		rightReady := filtered[j].Overview != nil && filtered[j].Overview.Status == "ready"
		if leftReady != rightReady {
			return leftReady
		}
		if scope == "daily" && newByID[filtered[i].ID] != newByID[filtered[j].ID] {
			return newByID[filtered[i].ID]
		}
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		if filtered[i].PublishedAt != filtered[j].PublishedAt {
			return filtered[i].PublishedAt > filtered[j].PublishedAt
		}
		return filtered[i].ID < filtered[j].ID
	})
	used, err := readerFocusUsedTokens(now.Format("2006-01-02"))
	if err != nil {
		return nil, nil, err
	}
	projection.DailyUsedTokens = used
	projection.DailyRemainingTokens = maxInt(0, state.Policy.DailyTokenBudget-used)
	perRun := state.Policy.PerRunLimit
	if perRun < 1 || perRun > readerFocusPerRunLimit {
		perRun = readerFocusPerRunLimit
	}
	selectedPending := 0
	for _, candidate := range filtered {
		if len(recommendations.Items) >= limit {
			break
		}
		status := "pending"
		if candidate.Overview != nil {
			status = candidate.Overview.Status
		}
		reason := candidate.Reason
		// A feed item without a content hash is still a valid unread
		// recommendation, but it cannot safely enter AI analysis or the
		// idempotent focus ledger. Keep it visible and explain the wait instead
		// of silently dropping it or inventing a dedupe key.
		if strings.TrimSpace(candidate.Hash) == "" {
			status = "queued"
			reason = strings.TrimSpace(strings.TrimSuffix(reason, "；"))
			if reason != "" {
				reason += "；"
			}
			reason += "内容指纹尚未生成，暂不进行 AI 分析"
		} else if status == "pending" && selectedPending < perRun {
			analysisText, textErr := readerFocusAnalysisText(candidate, state.Policy)
			if textErr != nil {
				return nil, nil, textErr
			}
			low, high := estimateReaderFocusTokens(analysisText)
			if projection.DailyRemainingTokens-projection.EstimatedTokensHigh >= high {
				projection.EstimatedTokensLow += low
				projection.EstimatedTokensHigh += high
				selectedPending++
			} else {
				status = "budget_limited"
			}
		} else if status == "pending" && candidate.Overview == nil {
			status = "queued"
		}
		recommendations.Items = append(recommendations.Items, ReaderRecommendationItem{EntryID: candidate.ID, Title: candidate.Title, Source: candidate.Source, SourceURL: candidate.URL, PublishedAt: candidate.PublishedAt, MatchedTopics: candidate.Topics, Score: candidate.Score, Status: status, Reason: reason, Overview: candidate.Overview})
		recommendations.LocalCount++
	}
	return projection, recommendations, nil
}

func readerFocusStateForTopic(state ReaderFocusState, topicID string) (ReaderFocusState, error) {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return state, nil
	}
	selected := make([]ReaderFocusTopic, 0, 1)
	for _, topic := range state.Topics {
		if topic.ID != topicID {
			continue
		}
		if !topic.Enabled {
			return ReaderFocusState{}, fmt.Errorf("%w: 主题未启用", errReaderFocusInvalid)
		}
		selected = append(selected, topic)
		break
	}
	if len(selected) == 0 {
		return ReaderFocusState{}, fmt.Errorf("%w: 主题不存在", errReaderFocusInvalid)
	}
	state.Topics = selected
	return state, nil
}

func queryReaderFocusCandidates(topics []ReaderFocusTopic) ([]readerFocusCandidate, error) {
	candidates, _, err := queryReaderFocusCandidatesWithMeta(topics)
	return candidates, err
}

func queryReaderFocusCandidatesWithMeta(topics []ReaderFocusTopic) ([]readerFocusCandidate, bool, error) {
	rows, err := db.Query(`SELECT n.id,n.source_id,n.title,n.source,n.url,n.published_at,n.fetched_at,n.summary_text,n.content_hash,fs.auto_extract,
	COALESCE(o.entry_id,''),COALESCE(o.content_hash,''),COALESCE(o.prompt_version,''),COALESCE(o.status,''),COALESCE(o.stage,''),COALESCE(o.attempt_count,0),COALESCE(o.summary,''),COALESCE(o.key_points_json,'[]'),COALESCE(o.impact,''),COALESCE(o.generated_at,''),COALESCE(o.error_code,'')
FROM news n
JOIN feed_subscriptions sub ON sub.source_id=n.source_id AND sub.enabled=1
	JOIN feed_sources fs ON fs.id=n.source_id AND fs.hidden=0
LEFT JOIN item_preferences ip ON ip.item_key='news:' || n.id
LEFT JOIN news_preferences np ON np.news_id=n.id AND np.dismissed=1
LEFT JOIN article_ai_overviews o ON n.content_hash<>'' AND o.entry_id=n.id AND o.content_hash=n.content_hash AND o.prompt_version=?
WHERE n.deleted_at='' AND COALESCE(ip.is_read,0)=0 AND np.news_id IS NULL
	ORDER BY n.published_at DESC,n.updated_at DESC LIMIT ?`, articleOverviewPromptVersion, readerFocusCandidateScanLimit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := make([]readerFocusCandidate, 0, readerFocusCandidateScanLimit)
	truncated := false
	for rows.Next() {
		if len(result) == readerFocusCandidateScanLimit {
			truncated = true
			break
		}
		var c readerFocusCandidate
		var overview ArticleAIOverview
		var keyPoints string
		var autoExtract int
		if err := rows.Scan(&c.ID, &c.SourceID, &c.Title, &c.Source, &c.URL, &c.PublishedAt, &c.FetchedAt, &c.Summary, &c.Hash, &autoExtract, &overview.EntryID, &overview.ContentHash, &overview.PromptVersion, &overview.Status, &overview.Stage, &overview.AttemptCount, &overview.Summary, &keyPoints, &overview.Impact, &overview.GeneratedAt, &overview.ErrorCode); err != nil {
			return nil, false, err
		}
		c.AutoExtract = autoExtract == 1
		if overview.EntryID != "" {
			if err := json.Unmarshal([]byte(keyPoints), &overview.KeyPoints); err != nil {
				return nil, false, err
			}
			c.Overview = &overview
		}
		c.Score, c.Topics, c.Reason = scoreReaderFocusCandidate(c, topics)
		result = append(result, c)
	}
	return result, truncated, rows.Err()
}

func scoreReaderFocusCandidate(candidate readerFocusCandidate, topics []ReaderFocusTopic) (int, []string, string) {
	title := normalizeReaderFocusText(candidate.Title)
	source := normalizeReaderFocusText(candidate.Source)
	summary := normalizeReaderFocusText(candidate.Summary)
	matched := make([]string, 0, len(topics))
	reasons := make([]string, 0, len(topics))
	score := 0
	for _, topic := range topics {
		if !topic.Enabled {
			continue
		}
		topicScore := 0
		matchedTerms := make([]string, 0, len(topic.Terms))
		for _, term := range topic.Terms {
			if focusTermInText(title, term) {
				topicScore += 4
				matchedTerms = append(matchedTerms, term)
			} else if focusTermInText(source, term) {
				topicScore += 2
				matchedTerms = append(matchedTerms, term)
			} else if focusTermInText(summary, term) {
				topicScore++
				matchedTerms = append(matchedTerms, term)
			}
		}
		if topicScore > 0 {
			score += topicScore
			matched = append(matched, topic.Label)
			reasons = append(reasons, topic.Label+": "+strings.Join(matchedTerms, ", "))
		}
	}
	return score, matched, strings.Join(reasons, "；")
}

func focusTermInText(text, term string) bool {
	if text == "" || term == "" {
		return false
	}
	textRunes := []rune(text)
	termRunes := []rune(term)
	if len(termRunes) > len(textRunes) {
		return false
	}
	for start := 0; start+len(termRunes) <= len(textRunes); start++ {
		matched := true
		for offset, expected := range termRunes {
			if textRunes[start+offset] != expected {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		// ASCII terms use Latin/digit word boundaries. CJK characters are script
		// boundaries, so a mixed-language phrase such as "ai computer产品"
		// still matches while "éai" remains an embedded Latin word.
		if !isASCIITermRune(termRunes[0]) {
			return true
		}
		end := start + len(termRunes)
		beforeOK := start == 0 || !focusWordRuneBoundary(textRunes[start-1])
		afterOK := end == len(textRunes) || !focusWordRuneBoundary(textRunes[end])
		if beforeOK && afterOK {
			return true
		}
	}
	return false
}

func isASCIITermRune(r rune) bool {
	return r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

func focusWordRuneBoundary(r rune) bool {
	if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
		return false
	}
	if unicode.Is(unicode.Han, r) {
		return false
	}
	return true
}

func readerFocusCandidateIsNew(candidate readerFocusCandidate, topics []ReaderFocusTopic) bool {
	when := candidate.PublishedAt
	if when == "" {
		when = candidate.FetchedAt
	}
	date, err := time.Parse(time.RFC3339, when)
	if err != nil {
		return false
	}
	for _, topic := range topics {
		if !topic.Enabled || !readerFocusContainsString(candidate.Topics, topic.Label) {
			continue
		}
		watch, err := time.Parse(time.RFC3339, topic.WatchSince)
		if err == nil && !date.Before(watch) {
			return true
		}
	}
	return false
}

func estimateReaderFocusTokens(summary string) (int, int) {
	inputLow, inputHigh, outputLow, outputHigh := estimateReaderFocusUsage(summary)
	return inputLow + outputLow, inputHigh + outputHigh
}

// estimateReaderFocusUsage keeps input and output portions separate for the
// durable run ledger while estimateReaderFocusTokens remains the compact range
// used by the UI and older callers.
func estimateReaderFocusUsage(summary string) (inputLow, inputHigh, outputLow, outputHigh int) {
	chars := len([]rune(limitText(summary, readerFocusInputRunes)))
	return (chars + 3) / 4, (chars + 2) / 3, 300, 600
}

func readerFocusUsedTokens(day string) (int, error) {
	var used int
	err := db.QueryRow(`SELECT COALESCE(SUM(CASE
	WHEN status IN ('pending','failed') THEN estimated_input_tokens+estimated_output_tokens
	WHEN actual_input_tokens+actual_output_tokens>0 THEN actual_input_tokens+actual_output_tokens
	ELSE estimated_input_tokens+estimated_output_tokens END),0)
FROM reader_focus_runs WHERE run_day=? AND status IN ('pending','completed','partial','failed')`, day).Scan(&used)
	return used, err
}

// Bootstrap is a one-time, user-approved budget. Unlike the daily budget it
// must survive a day boundary while the selected backlog is processed in
// bounded batches.
func readerFocusUsedArticlesScope(scope string) (int, error) {
	var used int
	err := db.QueryRow(`SELECT COALESCE(SUM(processed_count),0) FROM reader_focus_runs WHERE scope=? AND status IN ('completed','partial')`, scope).Scan(&used)
	return used, err
}

// readerFocusTopicBootstrapUsedTx reads the durable counter introduced by
// migration 050. The run-ledger fallback keeps an older database compatible
// while the migration is being applied; once the counter exists it remains the
// source of truth after retention removes historical run rows.
func readerFocusTopicBootstrapUsedTx(tx *sql.Tx, topicID string) (int, error) {
	var used int
	err := tx.QueryRow(`SELECT bootstrap_processed_count FROM reader_focus_topics WHERE id=?`, topicID).Scan(&used)
	if err != nil && !readerFocusSchemaMissing(err) {
		return 0, err
	}
	var legacyUsed int
	err = tx.QueryRow(`SELECT COALESCE(SUM(r.processed_count),0)
FROM reader_focus_runs r
WHERE r.scope='bootstrap' AND r.status IN ('completed','partial')
  AND json_valid(r.topic_ids_json)
  AND EXISTS (SELECT 1 FROM json_each(r.topic_ids_json) ids WHERE ids.value=?)`, topicID).Scan(&legacyUsed)
	if err != nil {
		return 0, err
	}
	if used < legacyUsed {
		used = legacyUsed
	}
	return used, nil
}

func readerFocusTopicBootstrapUsed(topicID string) (int, error) {
	var used int
	err := db.QueryRow(`SELECT bootstrap_processed_count FROM reader_focus_topics WHERE id=?`, topicID).Scan(&used)
	if err != nil && !readerFocusSchemaMissing(err) {
		return 0, err
	}
	var legacyUsed int
	err = db.QueryRow(`SELECT COALESCE(SUM(r.processed_count),0)
FROM reader_focus_runs r
WHERE r.scope='bootstrap' AND r.status IN ('completed','partial')
  AND json_valid(r.topic_ids_json)
  AND EXISTS (SELECT 1 FROM json_each(r.topic_ids_json) ids WHERE ids.value=?)`, topicID).Scan(&legacyUsed)
	if err != nil {
		return 0, err
	}
	if used < legacyUsed {
		used = legacyUsed
	}
	return used, nil
}

func readerFocusTopicTx(tx *sql.Tx, topicID string) (ReaderFocusTopic, error) {
	var topic ReaderFocusTopic
	var termsJSON string
	var enabled int
	err := tx.QueryRow(`SELECT id,label,terms_json,enabled,watch_since,bootstrap_status,bootstrap_mode,bootstrap_limit,bootstrap_processed_count,revision,created_at,updated_at FROM reader_focus_topics WHERE id=?`, topicID).Scan(
		&topic.ID, &topic.Label, &termsJSON, &enabled, &topic.WatchSince, &topic.BootstrapStatus, &topic.BootstrapMode, &topic.BootstrapLimit, &topic.BootstrapProcessedCount, &topic.Revision, &topic.CreatedAt, &topic.UpdatedAt,
	)
	if err != nil && readerFocusSchemaMissing(err) {
		// Keep the transaction usable during an additive migration window.
		if fallbackErr := tx.QueryRow(`SELECT id,label,terms_json,enabled,watch_since,bootstrap_status,bootstrap_mode,revision,created_at,updated_at FROM reader_focus_topics WHERE id=?`, topicID).Scan(
			&topic.ID, &topic.Label, &termsJSON, &enabled, &topic.WatchSince, &topic.BootstrapStatus, &topic.BootstrapMode, &topic.Revision, &topic.CreatedAt, &topic.UpdatedAt,
		); fallbackErr != nil {
			return ReaderFocusTopic{}, fallbackErr
		}
		if policyErr := tx.QueryRow(`SELECT bootstrap_limit FROM reader_focus_policy WHERE id='default'`).Scan(&topic.BootstrapLimit); policyErr != nil {
			return ReaderFocusTopic{}, policyErr
		}
	}
	if err != nil {
		// The fallback above populated the topic successfully; only return the
		// original error when no fallback was possible.
		if topic.ID == "" {
			return ReaderFocusTopic{}, err
		}
	}
	if err := json.Unmarshal([]byte(termsJSON), &topic.Terms); err != nil {
		return ReaderFocusTopic{}, err
	}
	topic.Enabled = enabled == 1
	return topic, nil
}

// readerFocusHasHistoricalCandidateTx checks only analyzable historical
// backlog. Current-batch results are excluded because their overview rows are
// persisted immediately after the focus ledger in the same transaction.
func readerFocusHasHistoricalCandidateTx(tx *sql.Tx, topic ReaderFocusTopic, currentBatch map[string]struct{}) (bool, error) {
	rows, err := tx.Query(`SELECT n.id,n.title,n.source,n.published_at,n.fetched_at,n.summary_text,n.content_hash
FROM news n
JOIN feed_subscriptions sub ON sub.source_id=n.source_id AND sub.enabled=1
JOIN feed_sources fs ON fs.id=n.source_id AND fs.hidden=0 AND fs.auto_extract=1
LEFT JOIN item_preferences ip ON ip.item_key='news:' || n.id
LEFT JOIN news_preferences np ON np.news_id=n.id AND np.dismissed=1
WHERE n.deleted_at='' AND n.content_hash<>'' AND COALESCE(ip.is_read,0)=0 AND np.news_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM article_ai_overviews o WHERE o.entry_id=n.id AND o.content_hash=n.content_hash AND o.prompt_version=?)
  AND NOT EXISTS (SELECT 1 FROM reader_digest_receipts r WHERE r.content_hash=n.content_hash)
	ORDER BY n.published_at DESC,n.updated_at DESC LIMIT ?`, articleOverviewPromptVersion, readerFocusCandidateScanLimit)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate readerFocusCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Title, &candidate.Source, &candidate.PublishedAt, &candidate.FetchedAt, &candidate.Summary, &candidate.Hash); err != nil {
			return false, err
		}
		if _, exists := currentBatch[candidate.ID+"\x00"+candidate.Hash]; exists {
			continue
		}
		score, _, _ := scoreReaderFocusCandidate(candidate, []ReaderFocusTopic{topic})
		if score <= 0 {
			continue
		}
		candidate.Topics = []string{topic.Label}
		if !readerFocusCandidateIsNew(candidate, []ReaderFocusTopic{topic}) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func readerFocusDashboardFingerprint(d Dashboard) string {
	raw, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func readerFocusPayloadSchemaMissing(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such column") || strings.Contains(message, "no such table")
}

// readerFocusRunByKeyTx returns the durable run fingerprint. Rows from before
// migration 056 intentionally return an empty fingerprint; callers must treat
// those rows as untrusted instead of silently authorizing a replay.
func readerFocusRunByKeyTx(tx *sql.Tx, key string) (bool, string, error) {
	var id, fingerprint string
	err := tx.QueryRow(`SELECT id,payload_fingerprint FROM reader_focus_runs WHERE idempotency_key=?`, key).Scan(&id, &fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if readerFocusPayloadSchemaMissing(err) {
		// Keep an old database readable while its additive migration is being
		// applied. The empty fingerprint is deliberately fail-closed below.
		if fallbackErr := tx.QueryRow(`SELECT id FROM reader_focus_runs WHERE idempotency_key=?`, key).Scan(&id); errors.Is(fallbackErr, sql.ErrNoRows) {
			return false, "", nil
		} else if fallbackErr != nil {
			return false, "", fallbackErr
		}
		return true, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, strings.TrimSpace(fingerprint), nil
}

func validateReaderFocusPayloadFingerprint(existing, incoming string) error {
	if strings.TrimSpace(existing) == "" || strings.TrimSpace(incoming) == "" || existing != incoming {
		return errReaderFocusPayloadConflict
	}
	return nil
}

// bindReaderFocusClaimPayloadFingerprintTx serializes the first payload that
// reaches a still-pending claim. This closes the small race before the formal
// run row is inserted: a second transport key cannot win the same reservation
// with a different set of drafts.
func bindReaderFocusClaimPayloadFingerprintTx(tx *sql.Tx, claimID, fingerprint string) error {
	if strings.TrimSpace(claimID) == "" || strings.TrimSpace(fingerprint) == "" {
		return nil
	}
	var existing, status string
	err := tx.QueryRow(`SELECT payload_fingerprint,status FROM reader_focus_runs WHERE id=?`, claimID).Scan(&existing, &status)
	if readerFocusPayloadSchemaMissing(err) {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return nil
	}
	if strings.TrimSpace(existing) != "" && existing != fingerprint {
		return errReaderFocusPayloadConflict
	}
	if strings.TrimSpace(existing) == "" {
		_, err = tx.Exec(`UPDATE reader_focus_runs SET payload_fingerprint=?,updated_at=? WHERE id=? AND status='pending' AND payload_fingerprint=''`, fingerprint, time.Now().UTC().Format(time.RFC3339), claimID)
	}
	return err
}

func storeReaderFocusRunTx(tx *sql.Tx, d Dashboard) error {
	return storeReaderFocusRunTxWithFingerprint(tx, d, "")
}

func storeReaderFocusRunTxWithFingerprint(tx *sql.Tx, d Dashboard, suppliedFingerprint string) error {
	payloadFingerprint := strings.TrimSpace(suppliedFingerprint)
	if payloadFingerprint == "" {
		payloadFingerprint = readerFocusDashboardFingerprint(d)
	}
	claimID := strings.TrimSpace(d.ReaderFocusClaimID)
	if claimID == "" && len(d.ArticleAIOverviews) > 0 {
		claim, matched, claimErr := readerFocusClaimReservationForDashboardTx(tx, d)
		if claimErr != nil {
			return claimErr
		}
		if matched {
			claimID = claim.ID
		}
	} else if claimID != "" {
		if err := validateReaderFocusClaimIDTx(tx, d); err != nil {
			return err
		}
	}
	state, err := readReaderFocusStateTx(tx)
	if err != nil {
		// A pre-048 database can still receive an old dashboard refresh while
		// the migration is being applied; keep the legacy write path usable.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return err
	}
	// Generic Reader digest receipts are not focus-analysis usage. They follow
	// the legacy digest ledger and must not create a second focus run key.
	if len(d.ArticleAIOverviews) == 0 {
		return nil
	}
	key, _ := readerFocusRunKey(state, d)
	if key == "" {
		return nil
	}
	if err := bindReaderFocusClaimPayloadFingerprintTx(tx, claimID, payloadFingerprint); err != nil {
		return err
	}
	// A completed focus batch may be replayed through the original operation
	// idempotency key; putDashboardInternal returns that cached response before
	// entering this transaction. Any request that reaches here without a live
	// claim is a new write, however. Reject it even when the topic was paused or
	// deleted, otherwise a changed draft could bypass the focus budget and
	// overwrite the stable dashboard projection.
	exists, existingFingerprint, err := readerFocusRunByKeyTx(tx, key)
	if err != nil {
		return err
	}
	if exists {
		if err := validateReaderFocusPayloadFingerprint(existingFingerprint, payloadFingerprint); err != nil {
			return err
		}
		// A matching complete payload is a safe replay. The operation-level
		// idempotency row normally returns before this point, but this branch also
		// protects direct legacy dashboard calls and connection-recovery paths.
		return nil
	}
	if len(readerFocusMaintenanceTopics(state)) == 0 {
		return nil
	}
	// A completed request is replayed by putDashboardInternal before this
	// transaction, using the caller's idempotency key and full payload
	// fingerprint. Reaching this point without a live claim therefore means a
	// new request is trying to reuse an old article batch and must fail closed.
	if claimID == "" {
		return errors.New("关注主题 claim 不存在")
	}
	id := stableObjectID("reader-focus-run", key)
	now := time.Now().UTC().Format(time.RFC3339)
	day := now[:10]
	runTopicIDs := make(map[string]struct{}, len(state.Topics))
	topicProcessedCounts := make(map[string]int, len(state.Topics))
	runScope := ""
	var estimatedIn, estimatedOut int
	processedCount := 0
	// Focus budget accounting is tied to AI overviews only. Reader digest
	// receipts remain governed by their existing bounded receipt contract and
	// cannot be forged to spend the focus analysis budget.
	usageSeen := make(map[string]struct{}, len(d.ArticleAIOverviews))
	for _, overview := range d.ArticleAIOverviews {
		itemKey := overview.EntryID + "\x00" + overview.ContentHash
		if itemKey == "\x00" || hasStringKey(usageSeen, itemKey) {
			continue
		}
		usageSeen[itemKey] = struct{}{}
		var candidate readerFocusCandidate
		if err := tx.QueryRow(`SELECT title,source,published_at,fetched_at,summary_text,content_hash FROM news WHERE id=?`, overview.EntryID).Scan(&candidate.Title, &candidate.Source, &candidate.PublishedAt, &candidate.FetchedAt, &candidate.Summary, &candidate.Hash); err != nil {
			delete(usageSeen, itemKey)
			continue
		}
		candidate.ID = overview.EntryID
		candidateScope, topicIDs := readerFocusCandidateScope(candidate, state)
		if candidateScope == "" {
			delete(usageSeen, itemKey)
			continue
		}
		if runScope == "" {
			runScope = candidateScope
		} else if runScope != candidateScope {
			return errors.New("同一轮概览不能混合首次分析和后续新文章")
		}
		analysisText, textErr := readerFocusAnalysisTextTx(tx, candidate, state.Policy)
		if textErr != nil {
			return textErr
		}
		var cachedStatus string
		_ = tx.QueryRow(`SELECT status FROM article_ai_overviews WHERE entry_id=? AND content_hash=? AND prompt_version=?`, overview.EntryID, overview.ContentHash, articleOverviewPromptVersion).Scan(&cachedStatus)
		if cachedStatus == "ready" {
			delete(usageSeen, itemKey)
			continue
		}
		for _, topicID := range topicIDs {
			runTopicIDs[topicID] = struct{}{}
			if candidateScope == "bootstrap" {
				topicProcessedCounts[topicID]++
			}
		}
		_, inputHigh, _, outputHigh := estimateReaderFocusUsage(analysisText)
		estimatedIn += inputHigh
		estimatedOut += outputHigh
		processedCount++
	}
	if processedCount == 0 {
		if claimID != "" {
			return consumeReaderFocusClaimTx(tx, claimID, d)
		}
		return nil
	}
	if len(runTopicIDs) == 0 {
		return errors.New("关注主题运行缺少有效主题")
	}
	topicIDsList := make([]string, 0, len(runTopicIDs))
	for topicID := range runTopicIDs {
		topicIDsList = append(topicIDsList, topicID)
	}
	sort.Strings(topicIDsList)
	topicIDs, _ := json.Marshal(topicIDsList)
	scope := runScope
	if scope == "" {
		return nil
	}
	var usedTokens, usedArticles int
	if err = tx.QueryRow(`SELECT COALESCE(SUM(CASE WHEN status IN ('pending','failed') AND candidate_count>processed_count THEN candidate_count ELSE processed_count END),0), COALESCE(SUM(CASE WHEN status IN ('pending','failed') THEN estimated_input_tokens+estimated_output_tokens WHEN actual_input_tokens+actual_output_tokens>0 THEN actual_input_tokens+actual_output_tokens ELSE estimated_input_tokens+estimated_output_tokens END),0) FROM reader_focus_runs WHERE run_day=? AND status IN ('pending','completed','partial','failed')`, day).Scan(&usedArticles, &usedTokens); err != nil {
		return err
	}
	if claimID != "" {
		claim, claimOK, claimErr := readerFocusClaimByIDWithQuerier(tx, claimID)
		if claimErr != nil {
			return claimErr
		}
		if claimOK && claim.Status == "pending" {
			usedArticles -= claim.CandidateCount
			usedTokens -= claim.EstimatedInput + claim.EstimatedOutput
			if usedArticles < 0 {
				usedArticles = 0
			}
			if usedTokens < 0 {
				usedTokens = 0
			}
		}
	}
	if scope == "bootstrap" {
		for topicID, topicCount := range topicProcessedCounts {
			topic, topicErr := readerFocusTopicTx(tx, topicID)
			if topicErr != nil {
				return topicErr
			}
			bootstrapUsed, usedErr := readerFocusTopicBootstrapUsedTx(tx, topicID)
			if usedErr != nil {
				return usedErr
			}
			if bootstrapUsed+topicCount > readerFocusTopicBootstrapLimit(topic, state.Policy) {
				return fmt.Errorf("关注主题首次分析额度已达到上限")
			}
		}
	}
	if usedArticles+processedCount > state.Policy.DailyArticleLimit {
		return fmt.Errorf("关注主题每日文章上限已达到")
	}
	if usedTokens+estimatedIn+estimatedOut > state.Policy.DailyTokenBudget {
		return fmt.Errorf("关注主题每日 token 预算已达到")
	}
	insertResult, err := tx.Exec(`INSERT OR IGNORE INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at,payload_fingerprint) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, scope, day, topicIDs, len(usageSeen), processedCount, estimatedIn, estimatedOut, "completed", key, now, now, payloadFingerprint)
	if readerFocusPayloadSchemaMissing(err) {
		// A profile may briefly execute the old binary while the additive
		// migration is pending. Preserve the existing run contract in that
		// window; once 056 is applied all new rows carry the fingerprint.
		insertResult, err = tx.Exec(`INSERT OR IGNORE INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, scope, day, topicIDs, len(usageSeen), processedCount, estimatedIn, estimatedOut, "completed", key, now, now)
	}
	if err != nil {
		return err
	}
	inserted, err := insertResult.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		// INSERT OR IGNORE can lose a race with another writer that committed
		// the same article batch between the lookup above and this statement.
		// Re-read its fingerprint instead of treating any ignored insert as a
		// harmless replay.
		exists, existingFingerprint, lookupErr := readerFocusRunByKeyTx(tx, key)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return errors.New("关注主题分析批次写入结果未知")
		}
		return validateReaderFocusPayloadFingerprint(existingFingerprint, payloadFingerprint)
	}
	if scope == "bootstrap" {
		for topicID, topicCount := range topicProcessedCounts {
			if _, err := tx.Exec(`UPDATE reader_focus_topics SET bootstrap_processed_count=bootstrap_processed_count+? WHERE id=?`, topicCount, topicID); err != nil {
				return err
			}
		}
		transitioned := false
		for _, topicID := range topicIDsList {
			topic, topicErr := readerFocusTopicTx(tx, topicID)
			if topicErr != nil {
				return topicErr
			}
			bootstrapUsed, usedErr := readerFocusTopicBootstrapUsedTx(tx, topicID)
			if usedErr != nil {
				return usedErr
			}
			complete := bootstrapUsed >= readerFocusTopicBootstrapLimit(topic, state.Policy)
			if !complete {
				hasHistorical, historyErr := readerFocusHasHistoricalCandidateTx(tx, topic, usageSeen)
				if historyErr != nil {
					return historyErr
				}
				complete = !hasHistorical
			}
			if !complete {
				continue
			}
			// Historical analysis is one-off. After it completes, retain the
			// original watch boundary and keep this topic in daily maintenance.
			result, updateErr := tx.Exec(`UPDATE reader_focus_topics SET bootstrap_status='done',bootstrap_mode='future_only',watch_since=CASE WHEN TRIM(watch_since)='' THEN ? ELSE watch_since END,revision=revision+1,updated_at=? WHERE id=? AND enabled=1 AND bootstrap_mode='analyze' AND bootstrap_status='active'`, now, now, topicID)
			if updateErr != nil {
				return updateErr
			}
			if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
				return affectedErr
			} else if affected > 0 {
				transitioned = true
			}
		}
		if transitioned {
			if err := recomputeReaderFocusPolicyModeTx(tx, state.Revision+1, now); err != nil {
				return err
			}
		}
	}
	if claimID != "" {
		err = consumeReaderFocusClaimTx(tx, claimID, d)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM reader_focus_runs WHERE julianday(created_at) < julianday('now', ?) OR id NOT IN (SELECT id FROM reader_focus_runs ORDER BY created_at DESC LIMIT ?)`, fmt.Sprintf("-%d days", readerFocusRunRetentionDays), readerFocusRunRetentionLimit)
	return err
}

func readerFocusRunKey(_ ReaderFocusState, d Dashboard) (string, []string) {
	// A content hash alone is not a sufficient batch identity: two entries can
	// legitimately carry the same normalized feed content. Bind the idempotency
	// key to entry_id + revision hash while retaining the returned hash list for
	// callers that use it for diagnostics.
	parts := make([]string, 0, len(d.ArticleAIOverviews))
	hashes := make([]string, 0, len(d.ArticleAIOverviews))
	seen := make(map[string]struct{}, len(d.ArticleAIOverviews))
	for _, overview := range d.ArticleAIOverviews {
		if overview.EntryID == "" || overview.ContentHash == "" {
			continue
		}
		part := overview.EntryID + "\x00" + overview.ContentHash
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		parts = append(parts, part)
		hashes = append(hashes, overview.ContentHash)
	}
	if len(parts) == 0 {
		return "", nil
	}
	sort.Strings(parts)
	return "reader-focus:" + strings.Join(parts, ","), hashes
}

func hasStringKey(values map[string]struct{}, key string) bool {
	_, ok := values[key]
	return ok
}

func topicIDsFromProjection(topics []ReaderFocusTopic) []string {
	ids := make([]string, 0, len(topics))
	for _, topic := range topics {
		if topic.Enabled {
			ids = append(ids, topic.ID)
		}
	}
	return ids
}

func readerFocusContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

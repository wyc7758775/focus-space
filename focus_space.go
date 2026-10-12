package main

// Focus Space is deliberately implemented as an Agent-owned sidecar.  It
// consumes bounded local projections and persists one analysis run that both
// Chat and the dashboard can read.  It never invokes a model, writes stable
// context, schedules work, or forwards arbitrary transport fields.

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
	"sync"
	"time"
)

const (
	focusSpaceSchemaVersion = 1
	focusSpaceLimit         = 8
	focusSpaceSourceLimit   = 16
	focusAnalysisLimit      = 5
	focusEvidenceScanLimit  = 100
	focusEvidenceExcerptMax = 6000
	focusGoalMaxRunes       = 500
	focusTitleMaxRunes      = 120
	focusQuestionMaxRunes   = 500
	focusPromptVersion      = "focus-v1"
	// The host bounds one analysis turn to four minutes. Keep a small margin
	// for the final writeback, but do not leave a client showing “分析中” for
	// fifteen minutes after the model has already disappeared.
	focusAnalysisClaimTTL             = 30 * time.Minute
	focusRunRetentionDays             = 90
	focusRunRetentionLimit            = 1024
	focusInterpretationMaxBytes       = 32 << 10
	focusInterpretationRetentionDays  = 90
	focusInterpretationRetentionLimit = 1024
)

var (
	errFocusSpaceRevision  = errors.New("focus space revision conflict")
	errFocusSpaceInvalid   = errors.New("focus space request invalid")
	errFocusAnalysisStale  = errors.New("focus analysis revision is stale")
	errFocusAnalysisBudget = errors.New("focus analysis daily token budget exceeded")
	focusAnalysisClaimMu   sync.Mutex
)

type FocusSourceLink struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Enabled    bool   `json:"enabled"`
	Revision   int    `json:"revision"`
}

type FocusSpace struct {
	ID                      string            `json:"id"`
	Title                   string            `json:"title"`
	Category                string            `json:"category,omitempty"`
	Goal                    string            `json:"goal"`
	DecisionQuestion        string            `json:"decision_question,omitempty"`
	DecisionMode            string            `json:"decision_mode"`
	WorkflowState           string            `json:"workflow_state"`
	Kind                    string            `json:"kind"`
	IntentKind              string            `json:"intent_kind"`
	BlueprintID             string            `json:"blueprint_id"`
	BlueprintVersion        string            `json:"blueprint_version"`
	ThinkingTemplateID      string            `json:"thinking_template_id"`
	ThinkingTemplateVersion string            `json:"thinking_template_version"`
	BindingRevision         int               `json:"binding_revision"`
	HorizonMode             string            `json:"horizon_mode"`
	HorizonStart            string            `json:"horizon_start,omitempty"`
	HorizonEnd              string            `json:"horizon_end,omitempty"`
	ReviewAt                string            `json:"review_at,omitempty"`
	OwnerHint               string            `json:"owner_hint,omitempty"`
	Status                  string            `json:"status"`
	Revision                int               `json:"revision"`
	CreatedAt               string            `json:"created_at"`
	UpdatedAt               string            `json:"updated_at"`
	Sources                 []FocusSourceLink `json:"sources,omitempty"`
	Materials               []FocusMaterial   `json:"materials,omitempty"`
}

type FocusSpaceGetResponse struct {
	SchemaVersion int                      `json:"schema_version"`
	Focuses       []FocusSpace             `json:"focuses"`
	Focus         *FocusSpace              `json:"focus,omitempty"`
	Policy        focusBudgetProjection    `json:"policy"`
	RecentRuns    []focusAnalysisRunPublic `json:"recent_runs"`
}

type focusBudgetProjection struct {
	DailyArticleLimit      int `json:"daily_article_limit"`
	DailyTokenBudget       int `json:"daily_token_budget"`
	PerRunLimit            int `json:"per_run_limit"`
	DailyUsedArticles      int `json:"daily_used_articles"`
	DailyUsedTokens        int `json:"daily_used_tokens"`
	DailyRemainingArticles int `json:"daily_remaining_articles"`
	DailyRemainingTokens   int `json:"daily_remaining_tokens"`
}

type focusAnalysisRunPublic struct {
	RunID           string `json:"run_id"`
	FocusID         string `json:"focus_id"`
	FocusRevision   int    `json:"focus_revision"`
	EvidenceSetHash string `json:"evidence_set_hash"`
	Scope           string `json:"scope"`
	Status          string `json:"status"`
	CandidateCount  int    `json:"candidate_count"`
	ProcessedCount  int    `json:"processed_count"`
	EstimatedInput  int    `json:"estimated_input_tokens"`
	EstimatedOutput int    `json:"estimated_output_tokens"`
	ActualInput     int    `json:"actual_input_tokens"`
	ActualOutput    int    `json:"actual_output_tokens"`
	ErrorCode       string `json:"error_code,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type focusSourcePatch struct {
	SourceKind string `json:"source_kind"`
	SourceID   string `json:"source_id"`
	Enabled    *bool  `json:"enabled,omitempty"`
}

type focusSpaceMutation struct {
	SchemaVersion      int                 `json:"schema_version"`
	Action             string              `json:"action"`
	FocusID            string              `json:"focus_id,omitempty"`
	ExpectedRevision   int                 `json:"expected_revision"`
	Title              *string             `json:"title,omitempty"`
	Category           *string             `json:"category,omitempty"`
	Goal               *string             `json:"goal,omitempty"`
	DecisionQuestion   *string             `json:"decision_question,omitempty"`
	DecisionMode       *string             `json:"decision_mode,omitempty"`
	WorkflowState      *string             `json:"workflow_state,omitempty"`
	Kind               *string             `json:"kind,omitempty"`
	IntentKind         *string             `json:"intent_kind,omitempty"`
	BlueprintID        *string             `json:"blueprint_id,omitempty"`
	ThinkingTemplateID *string             `json:"thinking_template_id,omitempty"`
	HorizonMode        *string             `json:"horizon_mode,omitempty"`
	HorizonStart       *string             `json:"horizon_start,omitempty"`
	HorizonEnd         *string             `json:"horizon_end,omitempty"`
	ReviewAt           *string             `json:"review_at,omitempty"`
	OwnerHint          *string             `json:"owner_hint,omitempty"`
	Status             *string             `json:"status,omitempty"`
	Sources            *[]focusSourcePatch `json:"sources,omitempty"`
	SourceKind         string              `json:"source_kind,omitempty"`
	SourceID           string              `json:"source_id,omitempty"`
	Enabled            *bool               `json:"enabled,omitempty"`
}

type focusEvidenceEnvelope struct {
	ID             string         `json:"id"`
	SourceKind     string         `json:"source_kind"`
	SourceID       string         `json:"source_id"`
	SourceRevision string         `json:"source_revision"`
	Title          string         `json:"title"`
	Locator        string         `json:"locator,omitempty"`
	EntryID        string         `json:"entry_id,omitempty"`
	ContentHash    string         `json:"content_hash,omitempty"`
	BlockID        string         `json:"block_id,omitempty"`
	Quote          string         `json:"quote,omitempty"`
	TextHash       string         `json:"text_hash,omitempty"`
	EvidenceRole   string         `json:"evidence_role,omitempty"`
	SourceLocator  map[string]any `json:"source_locator,omitempty"`
	ObservedAt     string         `json:"observed_at,omitempty"`
	PublishedAt    string         `json:"published_at,omitempty"`
	Excerpt        string         `json:"excerpt,omitempty"`
	AccessScope    string         `json:"access_scope,omitempty"`
	Rank           int            `json:"rank"`
}

// focusCitationInput is accepted from the model/apply bridge. The custom
// list decoder below also accepts a legacy string source id, so older callers
// can opt into paragraph citations incrementally without changing their
// payload shape.
type focusCitationInput struct {
	EvidenceID     string `json:"evidence_id,omitempty"`
	SourceKind     string `json:"source_kind,omitempty"`
	SourceID       string `json:"source_id,omitempty"`
	SourceRevision string `json:"source_revision,omitempty"`
	EntryID        string `json:"entry_id,omitempty"`
	ContentHash    string `json:"content_hash,omitempty"`
	BlockID        string `json:"block_id,omitempty"`
	Quote          string `json:"quote,omitempty"`
	TextHash       string `json:"text_hash,omitempty"`
	Role           string `json:"role,omitempty"`
}

type focusCitationInputs []focusCitationInput

func (items *focusCitationInputs) UnmarshalJSON(raw []byte) error {
	if strings.TrimSpace(string(raw)) == "null" || strings.TrimSpace(string(raw)) == "" {
		*items = nil
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	result := make(focusCitationInputs, 0, len(values))
	for _, value := range values {
		var sourceID string
		if err := json.Unmarshal(value, &sourceID); err == nil {
			result = append(result, focusCitationInput{SourceID: sourceID})
			continue
		}
		var item focusCitationInput
		if err := json.Unmarshal(value, &item); err != nil {
			return err
		}
		result = append(result, item)
	}
	*items = result
	return nil
}

type focusResultCitation struct {
	EvidenceID     string         `json:"evidence_id,omitempty"`
	SourceKind     string         `json:"source_kind,omitempty"`
	SourceID       string         `json:"source_id,omitempty"`
	SourceRevision string         `json:"source_revision,omitempty"`
	Locator        string         `json:"locator,omitempty"`
	SourceLocator  map[string]any `json:"source_locator,omitempty"`
	EntryID        string         `json:"entry_id,omitempty"`
	AssetID        string         `json:"asset_id,omitempty"`
	MIME           string         `json:"mime,omitempty"`
	ContentHash    string         `json:"content_hash,omitempty"`
	BlockID        string         `json:"block_id,omitempty"`
	Quote          string         `json:"quote,omitempty"`
	TextHash       string         `json:"text_hash,omitempty"`
	Role           string         `json:"role,omitempty"`
}

type focusAnalysisReadResponse struct {
	SchemaVersion           int                     `json:"schema_version"`
	RunID                   string                  `json:"run_id"`
	FocusID                 string                  `json:"focus_id"`
	FocusRevision           int                     `json:"focus_revision"`
	EvidenceSetHash         string                  `json:"evidence_set_hash"`
	AnalysisProfile         string                  `json:"analysis_profile"`
	PromptVersion           string                  `json:"prompt_version"`
	Scope                   string                  `json:"scope"`
	Status                  string                  `json:"status"`
	FocusTitle              string                  `json:"focus_title,omitempty"`
	FocusGoal               string                  `json:"focus_goal,omitempty"`
	DecisionQuestion        string                  `json:"decision_question,omitempty"`
	IntentKind              string                  `json:"intent_kind,omitempty"`
	BlueprintID             string                  `json:"blueprint_id,omitempty"`
	BlueprintVersion        string                  `json:"blueprint_version,omitempty"`
	ThinkingTemplateID      string                  `json:"thinking_template_id,omitempty"`
	ThinkingTemplateVersion string                  `json:"thinking_template_version,omitempty"`
	ExpiresAt               string                  `json:"expires_at,omitempty"`
	Evidence                []focusEvidenceEnvelope `json:"evidence"`
	CandidateCount          int                     `json:"candidate_count"`
	AvailableCount          int                     `json:"available_count"`
	EstimatedInput          int                     `json:"estimated_input_tokens"`
	EstimatedOutput         int                     `json:"estimated_output_tokens"`
	EstimatedTotalLow       int                     `json:"estimated_tokens_low"`
	EstimatedTotalHigh      int                     `json:"estimated_tokens_high"`
	Budget                  focusBudgetProjection   `json:"budget"`
	Sources                 []focusSourceStatus     `json:"sources"`
	GeneratedAt             string                  `json:"generated_at"`
}

type focusSourceStatus struct {
	SourceKind  string `json:"source_kind"`
	Status      string `json:"status"`
	RecordsSeen int    `json:"records_seen"`
	RecordsUsed int    `json:"records_used"`
	Message     string `json:"message,omitempty"`
}

type focusAnalysisUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

type focusSignalInput struct {
	ID                  string              `json:"id"`
	Title               string              `json:"title"`
	Detail              string              `json:"detail"`
	SourceIDs           []string            `json:"source_ids"`
	Citations           focusCitationInputs `json:"citations,omitempty"`
	SupportingCitations focusCitationInputs `json:"supporting_citations,omitempty"`
	CounterCitations    focusCitationInputs `json:"counter_citations,omitempty"`
	SupportingEvidence  []string            `json:"supporting_evidence"`
	Counterevidence     []string            `json:"counterevidence"`
	Confidence          string              `json:"confidence"`
	Freshness           string              `json:"freshness"`
	ValidationItems     []string            `json:"validation_items"`
}

type focusActionInput struct {
	ID                  string              `json:"id"`
	Task                string              `json:"task"`
	Owner               string              `json:"owner"`
	DueAt               string              `json:"due_at,omitempty"`
	SourceIDs           []string            `json:"source_ids"`
	Citations           focusCitationInputs `json:"citations,omitempty"`
	SupportingCitations focusCitationInputs `json:"supporting_citations,omitempty"`
	CounterCitations    focusCitationInputs `json:"counter_citations,omitempty"`
	Priority            string              `json:"priority"`
	SupportingEvidence  []string            `json:"supporting_evidence"`
	Counterevidence     []string            `json:"counterevidence"`
	Confidence          string              `json:"confidence"`
	Freshness           string              `json:"freshness"`
	ValidationItems     []string            `json:"validation_items"`
}

type focusDecisionInput struct {
	ID                  string              `json:"id"`
	Question            string              `json:"question"`
	Options             []string            `json:"options"`
	Recommendation      string              `json:"recommendation"`
	ExpectedOutcome     string              `json:"expected_outcome,omitempty"`
	ReviewAt            string              `json:"review_at,omitempty"`
	SourceIDs           []string            `json:"source_ids"`
	Citations           focusCitationInputs `json:"citations,omitempty"`
	SupportingCitations focusCitationInputs `json:"supporting_citations,omitempty"`
	CounterCitations    focusCitationInputs `json:"counter_citations,omitempty"`
	Assumptions         []string            `json:"assumptions,omitempty"`
	Risks               []string            `json:"risks,omitempty"`
	SupportingEvidence  []string            `json:"supporting_evidence"`
	Counterevidence     []string            `json:"counterevidence"`
	Confidence          string              `json:"confidence"`
	Freshness           string              `json:"freshness"`
	ValidationItems     []string            `json:"validation_items"`
}

type focusAnalysisApplyRequest struct {
	SchemaVersion   int                     `json:"schema_version"`
	RunID           string                  `json:"run_id"`
	FocusID         string                  `json:"focus_id"`
	FocusRevision   int                     `json:"focus_revision"`
	EvidenceSetHash string                  `json:"evidence_set_hash"`
	PromptVersion   string                  `json:"prompt_version"`
	Status          string                  `json:"status"`
	ErrorCode       string                  `json:"error_code,omitempty"`
	Signals         []focusSignalInput      `json:"signals"`
	Actions         []focusActionInput      `json:"actions"`
	Decisions       []focusDecisionInput    `json:"decisions"`
	WorkProducts    []focusWorkProductInput `json:"work_products,omitempty"`
	Usage           focusAnalysisUsage      `json:"usage,omitempty"`
}

type focusResultProjection struct {
	Kind               string                `json:"kind"`
	ID                 string                `json:"id"`
	FocusID            string                `json:"focus_id"`
	AnalysisRunID      string                `json:"analysis_run_id"`
	Rank               int                   `json:"rank"`
	PromptVersion      string                `json:"prompt_version"`
	EvidenceVersion    string                `json:"evidence_version"`
	Status             string                `json:"status"`
	Revision           int                   `json:"revision"`
	FeedbackStatus     string                `json:"feedback_status"`
	SourceIDs          []string              `json:"source_ids"`
	Citations          []focusResultCitation `json:"citations,omitempty"`
	SupportingEvidence []string              `json:"supporting_evidence"`
	Counterevidence    []string              `json:"counterevidence"`
	Confidence         string                `json:"confidence"`
	Freshness          string                `json:"freshness"`
	ValidationItems    []string              `json:"validation_items"`
	Data               any                   `json:"data"`
}

// focusEvidenceInterpretation is a bounded, goal-specific cache entry.  The
// payload is intentionally opaque to the platform; Chat and the dashboard
// can reuse it only when all revision and prompt dimensions still match.
type focusEvidenceInterpretation struct {
	FocusID          string         `json:"focus_id"`
	FocusRevision    int            `json:"focus_revision"`
	EvidenceID       string         `json:"evidence_id"`
	EvidenceRevision string         `json:"evidence_revision"`
	EvidenceSetHash  string         `json:"evidence_set_hash"`
	PromptVersion    string         `json:"prompt_version"`
	AnalysisProfile  string         `json:"analysis_profile"`
	RunID            string         `json:"run_id"`
	Interpretation   map[string]any `json:"interpretation"`
	UpdatedAt        string         `json:"updated_at"`
}

type focusAnalysisGetResponse struct {
	SchemaVersion         int                           `json:"schema_version"`
	FocusID               string                        `json:"focus_id"`
	RunID                 string                        `json:"run_id,omitempty"`
	Runs                  []map[string]any              `json:"runs"`
	Evidence              []focusEvidenceEnvelope       `json:"evidence"`
	Signals               []focusResultProjection       `json:"signals"`
	Actions               []focusResultProjection       `json:"actions"`
	Decisions             []focusResultProjection       `json:"decisions"`
	WorkProducts          []focusResultProjection       `json:"work_products,omitempty"`
	Results               []focusResultProjection       `json:"results"`
	CachedInterpretations []focusEvidenceInterpretation `json:"cached_interpretations,omitempty"`
}

type focusResultPatch struct {
	Task      *string `json:"task,omitempty"`
	Owner     *string `json:"owner,omitempty"`
	DueAt     *string `json:"due_at,omitempty"`
	Priority  *string `json:"priority,omitempty"`
	Completed *bool   `json:"completed,omitempty"`
	Detail    *string `json:"detail,omitempty"`
	Question  *string `json:"question,omitempty"`
}

type focusResultFeedbackRequest struct {
	SchemaVersion    int               `json:"schema_version"`
	FocusID          string            `json:"focus_id"`
	ResultKind       string            `json:"result_kind"`
	ResultID         string            `json:"result_id"`
	ExpectedRevision int               `json:"expected_revision"`
	Decision         string            `json:"decision"`
	Patch            *focusResultPatch `json:"patch,omitempty"`
}

type focusResultFeedbackResponse struct {
	FocusID    string `json:"focus_id"`
	ResultKind string `json:"result_kind"`
	ResultID   string `json:"result_id"`
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
}

func focusSpaceQueryKeys() map[string]struct{} { return map[string]struct{}{"focus_id": {}} }

func focusAnalysisReadQueryKeys() map[string]struct{} {
	return map[string]struct{}{"focus_id": {}, "scope": {}, "limit": {}, "include_full_text": {}, "claim": {}}
}

func focusAnalysisGetQueryKeys() map[string]struct{} {
	return map[string]struct{}{"focus_id": {}, "run_id": {}, "limit": {}}
}

func validFocusID(value string) bool {
	return validText(value, 128, true) && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func validFocusSource(kind, id string) bool {
	_, known := normalizeFocusSourceKind(kind)
	return known && validText(id, 256, true) && !strings.ContainsAny(id, "\x00\r\n")
}

func validFocusSpaceValues(space FocusSpace) bool {
	return validFocusID(space.ID) && validText(space.Title, focusTitleMaxRunes, true) && validText(space.Category, 80, false) && validText(space.Goal, focusGoalMaxRunes, true) && validText(space.DecisionQuestion, focusQuestionMaxRunes, false) && validText(space.OwnerHint, 120, false) && validFocusOptionalTime(space.ReviewAt) &&
		(space.DecisionMode == "sprint" || space.DecisionMode == "slow_burn") &&
		(space.WorkflowState == "accumulating" || space.WorkflowState == "deciding" || space.WorkflowState == "executing" || space.WorkflowState == "done" || space.WorkflowState == "archived") &&
		(space.Kind == "monitor" || space.Kind == "decision") && (space.HorizonMode == "rolling" || space.HorizonMode == "fixed" || space.HorizonMode == "open") &&
		(space.Status == "active" || space.Status == "paused" || space.Status == "archived") && space.Revision >= 1 && validFocusHorizon(space)
}

func validFocusOptionalTime(value string) bool {
	if value == "" {
		return true
	}
	if !validText(value, 80, false) {
		return false
	}
	if _, err := time.Parse(time.RFC3339, value); err == nil {
		return true
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func validFocusHorizon(space FocusSpace) bool {
	if !validText(space.HorizonStart, 80, false) || !validText(space.HorizonEnd, 80, false) {
		return false
	}
	var start, end time.Time
	var err error
	if space.HorizonStart != "" {
		start, err = time.Parse(time.RFC3339, space.HorizonStart)
		if err != nil {
			return false
		}
	}
	if space.HorizonEnd != "" {
		end, err = time.Parse(time.RFC3339, space.HorizonEnd)
		if err != nil {
			return false
		}
	}
	if space.HorizonMode == "fixed" && (space.HorizonStart == "" || space.HorizonEnd == "") {
		return false
	}
	return start.IsZero() || end.IsZero() || !end.Before(start)
}

func focusSpaceHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
		if focusID != "" && !validFocusID(focusID) {
			app.UserErr(w, http.StatusBadRequest, "焦点标识不合法")
			return
		}
		if err := ensureLegacyFocusSpaces(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		focuses, err := listFocusSpaces(focusID)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		policy, err := readFocusBudgetProjection(nil, time.Now().UTC())
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		runs, err := listRecentFocusRuns(focusID, 10)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		response := FocusSpaceGetResponse{SchemaVersion: focusSpaceSchemaVersion, Focuses: focuses, Policy: policy, RecentRuns: runs}
		if len(focuses) == 1 && focusID != "" {
			response.Focus = &focuses[0]
		}
		app.WriteJSON(w, http.StatusOK, response)
	case http.MethodPatch:
		var request focusSpaceMutation
		if err := app.DecodeStrictJSON(w, r, 32<<10, &request); err != nil || request.SchemaVersion != focusSpaceSchemaVersion {
			app.UserErr(w, http.StatusBadRequest, "焦点空间请求格式不合法")
			return
		}
		space, err := mutateFocusSpace(request)
		if err != nil {
			if errors.Is(err, errFocusSpaceRevision) {
				app.UserErr(w, http.StatusConflict, "焦点空间已变化，请重新读取")
			} else if errors.Is(err, errFocusSpaceInvalid) {
				app.UserErr(w, http.StatusBadRequest, err.Error())
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
		app.WriteJSON(w, http.StatusOK, space)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func ensureLegacyFocusSpaces() error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureLegacyFocusSpacesTx(tx); err != nil {
		if readerFocusSchemaMissing(err) {
			return nil
		}
		return err
	}
	return tx.Commit()
}

func ensureLegacyFocusSpacesTx(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,label,enabled,watch_since,created_at,updated_at FROM reader_focus_topics ORDER BY created_at ASC,id ASC LIMIT ?`, readerFocusTopicLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, label, watchSince, createdAt, updatedAt string
		var enabled int
		if err := rows.Scan(&id, &label, &enabled, &watchSince, &createdAt, &updatedAt); err != nil {
			return err
		}
		if !validText(id, 128, true) || !validText(label, focusTitleMaxRunes, true) {
			continue
		}
		legacyID := stableObjectID("legacy-reader-focus", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+id)
		now := time.Now().UTC().Format(time.RFC3339)
		if createdAt == "" {
			createdAt = now
		}
		if updatedAt == "" {
			updatedAt = now
		}
		status := "paused"
		if enabled == 1 {
			status = "active"
		}
		_, err = tx.Exec(`INSERT INTO focus_spaces(id,owner_id,profile_id,title,goal,decision_question,kind,horizon_mode,horizon_start,horizon_end,status,revision,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  title=excluded.title,
  goal=excluded.goal,
  horizon_start=excluded.horizon_start,
  status=excluded.status,
  revision=focus_spaces.revision+1,
  updated_at=excluded.updated_at
WHERE focus_spaces.owner_id=excluded.owner_id
  AND focus_spaces.profile_id=excluded.profile_id
  AND (focus_spaces.title<>excluded.title OR focus_spaces.goal<>excluded.goal OR focus_spaces.horizon_start<>excluded.horizon_start OR focus_spaces.status<>excluded.status)`, legacyID, appDataScope.OwnerID, appDataScope.ProfileID, label, label, "", "monitor", "rolling", watchSince, "", status, 1, createdAt, updatedAt)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',expires_at='',updated_at=?
WHERE owner_id=? AND profile_id=? AND focus_id=? AND status IN ('claimed','applied','partial')
  AND focus_revision<>(SELECT revision FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?)`, updatedAt, appDataScope.OwnerID, appDataScope.ProfileID, legacyID, legacyID, appDataScope.OwnerID, appDataScope.ProfileID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func listFocusSpaces(focusID string) ([]FocusSpace, error) {
	query := `SELECT id,title,category,goal,decision_question,decision_mode,workflow_state,kind,horizon_mode,horizon_start,horizon_end,review_at,owner_hint,status,revision,created_at,updated_at FROM focus_spaces WHERE owner_id=? AND profile_id=?`
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID}
	if focusID != "" {
		query += ` AND id=?`
		args = append(args, focusID)
	}
	query += ` ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END,updated_at DESC,id ASC LIMIT ?`
	args = append(args, focusSpaceLimit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	// Materialize the outer query before loading each space's related rows.
	// Holding this cursor while issuing nested queries can consume every SQLite
	// pool connection when several focus reads arrive concurrently.
	result := make([]FocusSpace, 0, focusSpaceLimit)
	for rows.Next() {
		space, err := scanFocusSpace(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		result = append(result, space)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range result {
		space := &result[index]
		binding, err := readFocusWorkspaceBinding(*space)
		if err != nil {
			return nil, err
		}
		assignFocusWorkspaceBinding(space, binding)
		sources, err := listFocusSources(space.ID)
		if err != nil {
			return nil, err
		}
		space.Sources = sources
		materials, err := listFocusMaterials(space.ID)
		if err != nil {
			return nil, err
		}
		space.Materials = materials
	}
	return result, nil
}

type focusRowScanner interface{ Scan(...any) error }

func scanFocusSpace(scanner focusRowScanner) (FocusSpace, error) {
	var space FocusSpace
	err := scanner.Scan(&space.ID, &space.Title, &space.Category, &space.Goal, &space.DecisionQuestion, &space.DecisionMode, &space.WorkflowState, &space.Kind, &space.HorizonMode, &space.HorizonStart, &space.HorizonEnd, &space.ReviewAt, &space.OwnerHint, &space.Status, &space.Revision, &space.CreatedAt, &space.UpdatedAt)
	return space, err
}

func listFocusSources(focusID string) ([]FocusSourceLink, error) {
	rows, err := db.Query(`SELECT source_kind,source_id,enabled,revision FROM focus_space_sources WHERE owner_id=? AND profile_id=? AND focus_id=? ORDER BY source_kind,source_id LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusSpaceSourceLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]FocusSourceLink, 0, focusSpaceSourceLimit)
	for rows.Next() {
		var item FocusSourceLink
		var enabled int
		if err := rows.Scan(&item.SourceKind, &item.SourceID, &enabled, &item.Revision); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusSpaceByIDTx(tx *sql.Tx, focusID string) (FocusSpace, error) {
	row := tx.QueryRow(`SELECT id,title,category,goal,decision_question,decision_mode,workflow_state,kind,horizon_mode,horizon_start,horizon_end,review_at,owner_hint,status,revision,created_at,updated_at FROM focus_spaces WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
	space, err := scanFocusSpace(row)
	if err != nil {
		return FocusSpace{}, err
	}
	binding, err := readFocusWorkspaceBindingTx(tx, space)
	if err != nil {
		return FocusSpace{}, err
	}
	assignFocusWorkspaceBinding(&space, binding)
	rows, err := tx.Query(`SELECT source_kind,source_id,enabled,revision FROM focus_space_sources WHERE owner_id=? AND profile_id=? AND focus_id=? ORDER BY source_kind,source_id LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusSpaceSourceLimit)
	if err != nil {
		return FocusSpace{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var source FocusSourceLink
		var enabled int
		if err := rows.Scan(&source.SourceKind, &source.SourceID, &enabled, &source.Revision); err != nil {
			return FocusSpace{}, err
		}
		source.Enabled = enabled == 1
		space.Sources = append(space.Sources, source)
	}
	if err := rows.Err(); err != nil {
		return FocusSpace{}, err
	}
	if err := rows.Close(); err != nil {
		return FocusSpace{}, err
	}
	space.Materials, err = listFocusMaterialsTx(tx, focusID)
	return space, err
}

func mutateFocusSpace(request focusSpaceMutation) (FocusSpace, error) {
	if request.Action == "" {
		return FocusSpace{}, fmt.Errorf("%w: 缺少 action", errFocusSpaceInvalid)
	}
	if request.Action == "create" && request.ExpectedRevision != 0 {
		return FocusSpace{}, fmt.Errorf("%w: 创建不接受旧 revision", errFocusSpaceInvalid)
	}
	if request.Action != "create" && (request.ExpectedRevision < 1 || !validFocusID(strings.TrimSpace(request.FocusID))) {
		return FocusSpace{}, fmt.Errorf("%w: 缺少 focus_id 或 expected_revision", errFocusSpaceInvalid)
	}
	tx, err := db.Begin()
	if err != nil {
		return FocusSpace{}, err
	}
	defer tx.Rollback()
	if err := ensureLegacyFocusSpacesTx(tx); err != nil && !readerFocusSchemaMissing(err) {
		return FocusSpace{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var space FocusSpace
	switch request.Action {
	case "create":
		space, err = createFocusSpaceTx(tx, request, now)
	case "update", "pause", "archive", "link_source", "unlink_source":
		space, err = updateFocusSpaceTx(tx, request, now)
	default:
		return FocusSpace{}, fmt.Errorf("%w: 未知 action", errFocusSpaceInvalid)
	}
	if err != nil {
		return FocusSpace{}, err
	}
	if err := tx.Commit(); err != nil {
		return FocusSpace{}, err
	}
	return space, nil
}

func createFocusSpaceTx(tx *sql.Tx, request focusSpaceMutation, now string) (FocusSpace, error) {
	var focusCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_spaces WHERE owner_id=? AND profile_id=?`, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&focusCount); err != nil {
		return FocusSpace{}, err
	}
	if focusCount >= focusSpaceLimit {
		return FocusSpace{}, fmt.Errorf("%w: 焦点空间数量已达到上限", errFocusSpaceInvalid)
	}
	goal := ""
	if request.Goal != nil {
		goal = strings.TrimSpace(*request.Goal)
	}
	if !validText(goal, focusGoalMaxRunes, true) {
		return FocusSpace{}, fmt.Errorf("%w: goal 不合法", errFocusSpaceInvalid)
	}
	title := goal
	if request.Title != nil && strings.TrimSpace(*request.Title) != "" {
		title = strings.TrimSpace(*request.Title)
	}
	kind, horizonMode := "monitor", "open"
	if request.Kind != nil && strings.TrimSpace(*request.Kind) != "" {
		kind = strings.TrimSpace(*request.Kind)
	}
	if request.HorizonMode != nil && strings.TrimSpace(*request.HorizonMode) != "" {
		horizonMode = strings.TrimSpace(*request.HorizonMode)
	}
	question, start, end := "", "", ""
	if request.DecisionQuestion != nil {
		question = strings.TrimSpace(*request.DecisionQuestion)
	}
	if request.HorizonStart != nil {
		start = strings.TrimSpace(*request.HorizonStart)
	}
	if request.HorizonEnd != nil {
		end = strings.TrimSpace(*request.HorizonEnd)
	}
	category, decisionMode, workflowState, reviewAt, ownerHint := "", "slow_burn", "accumulating", "", ""
	if request.Category != nil {
		category = strings.TrimSpace(*request.Category)
	}
	if request.DecisionMode != nil && strings.TrimSpace(*request.DecisionMode) != "" {
		decisionMode = strings.TrimSpace(*request.DecisionMode)
	}
	if request.WorkflowState != nil && strings.TrimSpace(*request.WorkflowState) != "" {
		workflowState = strings.TrimSpace(*request.WorkflowState)
	}
	if request.ReviewAt != nil {
		reviewAt = strings.TrimSpace(*request.ReviewAt)
	}
	if request.OwnerHint != nil {
		ownerHint = strings.TrimSpace(*request.OwnerHint)
	}
	space := FocusSpace{Title: title, Category: category, Goal: goal, DecisionQuestion: question, DecisionMode: decisionMode, WorkflowState: workflowState, Kind: kind, HorizonMode: horizonMode, HorizonStart: start, HorizonEnd: end, ReviewAt: reviewAt, OwnerHint: ownerHint, Status: "active", Revision: 1, CreatedAt: now, UpdatedAt: now}
	space.ID = stableObjectID("focus", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+goal+"\x00"+now)
	if !validFocusSpaceValues(space) {
		return FocusSpace{}, fmt.Errorf("%w: 焦点空间字段不合法", errFocusSpaceInvalid)
	}
	if _, err := tx.Exec(`INSERT INTO focus_spaces(id,owner_id,profile_id,title,category,goal,decision_question,decision_mode,workflow_state,kind,horizon_mode,horizon_start,horizon_end,review_at,owner_hint,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, space.Title, space.Category, space.Goal, space.DecisionQuestion, space.DecisionMode, space.WorkflowState, space.Kind, space.HorizonMode, space.HorizonStart, space.HorizonEnd, space.ReviewAt, space.OwnerHint, space.Status, space.Revision, space.CreatedAt, space.UpdatedAt); err != nil {
		return FocusSpace{}, err
	}
	binding, err := defaultFocusWorkspaceBinding(space)
	if err != nil {
		return FocusSpace{}, err
	}
	binding, err = applyFocusWorkspaceBindingMutation(binding, request)
	if err != nil {
		return FocusSpace{}, fmt.Errorf("%w: %v", errFocusSpaceInvalid, err)
	}
	binding.CreatedAt, binding.UpdatedAt = now, now
	if _, err := tx.Exec(`INSERT INTO focus_workspace_bindings(focus_id,owner_id,profile_id,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?)`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, binding.IntentKind, binding.BlueprintID, binding.BlueprintVersion, binding.ThinkingTemplateID, binding.ThinkingTemplateVersion, now, now); err != nil {
		return FocusSpace{}, err
	}
	assignFocusWorkspaceBinding(&space, binding)
	if request.Sources != nil {
		if err := replaceFocusSourcesTx(tx, space.ID, *request.Sources, now); err != nil {
			return FocusSpace{}, err
		}
	}
	space.Sources, _ = listFocusSourcesTx(tx, space.ID)
	return space, nil
}

func updateFocusSpaceTx(tx *sql.Tx, request focusSpaceMutation, now string) (FocusSpace, error) {
	space, err := focusSpaceByIDTx(tx, strings.TrimSpace(request.FocusID))
	if errors.Is(err, sql.ErrNoRows) {
		return FocusSpace{}, fmt.Errorf("%w: 焦点空间不存在", errFocusSpaceInvalid)
	}
	if err != nil {
		return FocusSpace{}, err
	}
	if space.Revision != request.ExpectedRevision {
		return FocusSpace{}, errFocusSpaceRevision
	}
	currentBinding, err := readFocusWorkspaceBindingTx(tx, space)
	if err != nil {
		return FocusSpace{}, err
	}
	desiredBinding, err := applyFocusWorkspaceBindingMutation(currentBinding, request)
	if err != nil {
		return FocusSpace{}, fmt.Errorf("%w: %v", errFocusSpaceInvalid, err)
	}
	bindingChanged := currentBinding.IntentKind != desiredBinding.IntentKind || currentBinding.BlueprintID != desiredBinding.BlueprintID || currentBinding.BlueprintVersion != desiredBinding.BlueprintVersion || currentBinding.ThinkingTemplateID != desiredBinding.ThinkingTemplateID || currentBinding.ThinkingTemplateVersion != desiredBinding.ThinkingTemplateVersion
	analysisInputsChanged := request.Sources != nil || bindingChanged ||
		(request.Title != nil && strings.TrimSpace(*request.Title) != space.Title) ||
		(request.Category != nil && strings.TrimSpace(*request.Category) != space.Category) ||
		(request.Goal != nil && strings.TrimSpace(*request.Goal) != space.Goal) ||
		(request.DecisionQuestion != nil && strings.TrimSpace(*request.DecisionQuestion) != space.DecisionQuestion) ||
		(request.DecisionMode != nil && strings.TrimSpace(*request.DecisionMode) != space.DecisionMode) ||
		(request.Kind != nil && strings.TrimSpace(*request.Kind) != space.Kind) ||
		(request.HorizonMode != nil && strings.TrimSpace(*request.HorizonMode) != space.HorizonMode) ||
		(request.HorizonStart != nil && strings.TrimSpace(*request.HorizonStart) != space.HorizonStart) ||
		(request.HorizonEnd != nil && strings.TrimSpace(*request.HorizonEnd) != space.HorizonEnd) ||
		(request.OwnerHint != nil && strings.TrimSpace(*request.OwnerHint) != space.OwnerHint)
	if request.Action == "link_source" || request.Action == "unlink_source" {
		rawSourceKind, sourceID := strings.TrimSpace(request.SourceKind), strings.TrimSpace(request.SourceID)
		materialMutation := rawSourceKind == "entry" || rawSourceKind == "image"
		sourceKind := rawSourceKind
		if !materialMutation {
			var known bool
			sourceKind, known = normalizeFocusSourceKind(rawSourceKind)
			if !known {
				return FocusSpace{}, fmt.Errorf("%w: source_kind/source_id 不合法", errFocusSpaceInvalid)
			}
		}
		if (!materialMutation && !validFocusSource(sourceKind, sourceID)) ||
			(materialMutation && !validText(sourceID, 256, true)) ||
			(sourceKind == "image" && request.Action == "link_source") {
			return FocusSpace{}, fmt.Errorf("%w: source_kind/source_id 不合法", errFocusSpaceInvalid)
		}
		enabled := request.Action == "link_source"
		if request.Enabled != nil {
			enabled = *request.Enabled
		}
		if materialMutation {
			if enabled {
				if err := linkFocusEntryMaterialTx(tx, space.ID, sourceID, request.ExpectedRevision+1, now); err != nil {
					return FocusSpace{}, err
				}
			} else if err := unlinkFocusMaterialTx(tx, space.ID, sourceKind, sourceID, request.ExpectedRevision+1, now); err != nil {
				return FocusSpace{}, err
			}
		} else if enabled {
			var sourceCount int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_sources WHERE focus_id=? AND owner_id=? AND profile_id=?`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&sourceCount); err != nil {
				return FocusSpace{}, err
			}
			var existing int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_sources WHERE focus_id=? AND owner_id=? AND profile_id=? AND source_kind=? AND source_id=?`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, sourceKind, sourceID).Scan(&existing); err != nil {
				return FocusSpace{}, err
			}
			if existing == 0 && sourceCount >= focusSpaceSourceLimit {
				return FocusSpace{}, fmt.Errorf("%w: 来源数量已达到上限", errFocusSpaceInvalid)
			}
			_, err = tx.Exec(`INSERT INTO focus_space_sources(focus_id,owner_id,profile_id,source_kind,source_id,enabled,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?) ON CONFLICT(focus_id,source_kind,source_id) DO UPDATE SET enabled=excluded.enabled,revision=focus_space_sources.revision+1,updated_at=excluded.updated_at`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, sourceKind, sourceID, 1, now, now)
		} else {
			_, err = tx.Exec(`UPDATE focus_space_sources SET enabled=0,revision=revision+1,updated_at=? WHERE focus_id=? AND owner_id=? AND profile_id=? AND source_kind=? AND source_id=?`, now, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, sourceKind, sourceID)
		}
		if err != nil {
			return FocusSpace{}, err
		}
	} else {
		if request.Title != nil {
			space.Title = strings.TrimSpace(*request.Title)
		}
		if request.Category != nil {
			space.Category = strings.TrimSpace(*request.Category)
		}
		if request.Goal != nil {
			space.Goal = strings.TrimSpace(*request.Goal)
		}
		if request.DecisionQuestion != nil {
			space.DecisionQuestion = strings.TrimSpace(*request.DecisionQuestion)
		}
		if request.DecisionMode != nil {
			space.DecisionMode = strings.TrimSpace(*request.DecisionMode)
		}
		if request.WorkflowState != nil {
			space.WorkflowState = strings.TrimSpace(*request.WorkflowState)
		}
		if request.Kind != nil {
			space.Kind = strings.TrimSpace(*request.Kind)
		}
		if request.HorizonMode != nil {
			space.HorizonMode = strings.TrimSpace(*request.HorizonMode)
		}
		if request.HorizonStart != nil {
			space.HorizonStart = strings.TrimSpace(*request.HorizonStart)
		}
		if request.HorizonEnd != nil {
			space.HorizonEnd = strings.TrimSpace(*request.HorizonEnd)
		}
		if request.ReviewAt != nil {
			space.ReviewAt = strings.TrimSpace(*request.ReviewAt)
		}
		if request.OwnerHint != nil {
			space.OwnerHint = strings.TrimSpace(*request.OwnerHint)
		}
		switch request.Action {
		case "pause":
			space.Status = "paused"
		case "archive":
			space.Status = "archived"
			space.WorkflowState = "archived"
		case "update":
			if request.Status != nil {
				space.Status = strings.TrimSpace(*request.Status)
			}
		}
		space.UpdatedAt = now
		if request.WorkflowState != nil {
			if err := validateFocusWorkflowTransitionTx(tx, space.ID, space.WorkflowState); err != nil {
				return FocusSpace{}, err
			}
		}
		if !validFocusSpaceValues(space) {
			return FocusSpace{}, fmt.Errorf("%w: 焦点空间字段不合法", errFocusSpaceInvalid)
		}
		result, updateErr := tx.Exec(`UPDATE focus_spaces SET title=?,category=?,goal=?,decision_question=?,decision_mode=?,workflow_state=?,kind=?,horizon_mode=?,horizon_start=?,horizon_end=?,review_at=?,owner_hint=?,status=?,revision=revision+1,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`, space.Title, space.Category, space.Goal, space.DecisionQuestion, space.DecisionMode, space.WorkflowState, space.Kind, space.HorizonMode, space.HorizonStart, space.HorizonEnd, space.ReviewAt, space.OwnerHint, space.Status, now, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, request.ExpectedRevision)
		if updateErr != nil {
			return FocusSpace{}, updateErr
		}
		if changed, changedErr := result.RowsAffected(); changedErr != nil {
			return FocusSpace{}, changedErr
		} else if changed != 1 {
			return FocusSpace{}, errFocusSpaceRevision
		}
		invalidateQuery := `UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',expires_at='',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND status='claimed'`
		if analysisInputsChanged {
			invalidateQuery = `UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',expires_at='',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND status IN ('claimed','applied','partial')`
		}
		if _, err := tx.Exec(invalidateQuery, now, appDataScope.OwnerID, appDataScope.ProfileID, space.ID); err != nil {
			return FocusSpace{}, err
		}
		if analysisInputsChanged {
			if err := supersedeActiveDecisionRunsTx(tx, space.ID, now); err != nil {
				return FocusSpace{}, err
			}
		}
		if request.Sources != nil {
			if err := replaceFocusSourcesTx(tx, space.ID, *request.Sources, now); err != nil {
				return FocusSpace{}, err
			}
		}
	}
	if bindingChanged {
		if _, _, err := persistFocusWorkspaceBindingTx(tx, currentBinding, desiredBinding, now); err != nil {
			return FocusSpace{}, err
		}
	}
	space, err = focusSpaceByIDTx(tx, space.ID)
	if err != nil {
		return FocusSpace{}, err
	}
	// Source-only mutations still advance the focus revision so an in-flight
	// analysis cannot apply against an obsolete source set.
	if request.Action == "link_source" || request.Action == "unlink_source" {
		result, updateErr := tx.Exec(`UPDATE focus_spaces SET revision=revision+1,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`, now, space.ID, appDataScope.OwnerID, appDataScope.ProfileID, request.ExpectedRevision)
		if updateErr != nil {
			return FocusSpace{}, updateErr
		}
		if changed, changedErr := result.RowsAffected(); changedErr != nil {
			return FocusSpace{}, changedErr
		} else if changed != 1 {
			return FocusSpace{}, errFocusSpaceRevision
		}
		if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',expires_at='',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND status IN ('claimed','applied','partial')`, now, appDataScope.OwnerID, appDataScope.ProfileID, space.ID); err != nil {
			return FocusSpace{}, err
		}
		if err := supersedeActiveDecisionRunsTx(tx, space.ID, now); err != nil {
			return FocusSpace{}, err
		}
		space, err = focusSpaceByIDTx(tx, space.ID)
		if err != nil {
			return FocusSpace{}, err
		}
	}
	return space, nil
}

func validateFocusWorkflowTransitionTx(tx *sql.Tx, focusID, workflowState string) error {
	if workflowState != "executing" && workflowState != "done" {
		return nil
	}
	var confirmed int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND status='confirmed'`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&confirmed); err != nil {
		return err
	}
	if confirmed == 0 {
		return fmt.Errorf("%w: 进入执行或完成前必须先确认决策", errFocusSpaceInvalid)
	}
	if workflowState == "done" {
		var openActions int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM actions WHERE focus_id=? AND analysis_run_id<>'' AND status NOT IN ('done','rejected') AND confirmation_status<>'rejected'`, focusID).Scan(&openActions); err != nil {
			return err
		}
		if openActions > 0 {
			return fmt.Errorf("%w: 仍有未完成行动，不能标记完成", errFocusSpaceInvalid)
		}
	}
	return nil
}

func replaceFocusSourcesTx(tx *sql.Tx, focusID string, sources []focusSourcePatch, now string) error {
	if len(sources) > focusSpaceSourceLimit {
		return fmt.Errorf("%w: 来源过多", errFocusSpaceInvalid)
	}
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		kind, id := strings.TrimSpace(source.SourceKind), strings.TrimSpace(source.SourceID)
		canonicalKind, known := normalizeFocusSourceKind(kind)
		if !known || !validFocusSource(canonicalKind, id) {
			return fmt.Errorf("%w: 来源字段不合法", errFocusSpaceInvalid)
		}
		kind = canonicalKind
		key := kind + "\x00" + id
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: 来源重复", errFocusSpaceInvalid)
		}
		seen[key] = struct{}{}
		enabled := 1
		if source.Enabled != nil && !*source.Enabled {
			enabled = 0
		}
		if _, err := tx.Exec(`INSERT INTO focus_space_sources(focus_id,owner_id,profile_id,source_kind,source_id,enabled,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?) ON CONFLICT(focus_id,source_kind,source_id) DO UPDATE SET enabled=excluded.enabled,revision=focus_space_sources.revision+1,updated_at=excluded.updated_at`, focusID, appDataScope.OwnerID, appDataScope.ProfileID, kind, id, enabled, now, now); err != nil {
			return err
		}
	}
	// Reaching this helper means replacement was explicit. An empty JSON array
	// therefore clears all links; an omitted field never calls this helper.
	rows, err := tx.Query(`SELECT source_kind,source_id FROM focus_space_sources WHERE focus_id=? AND owner_id=? AND profile_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID)
	if err != nil {
		return err
	}
	var stale [][2]string
	for rows.Next() {
		var kind, id string
		if err := rows.Scan(&kind, &id); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seen[kind+"\x00"+id]; !ok {
			stale = append(stale, [2]string{kind, id})
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range stale {
		if _, err := tx.Exec(`DELETE FROM focus_space_sources WHERE focus_id=? AND owner_id=? AND profile_id=? AND source_kind=? AND source_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID, item[0], item[1]); err != nil {
			return err
		}
	}
	return nil
}

func listFocusSourcesTx(tx *sql.Tx, focusID string) ([]FocusSourceLink, error) {
	rows, err := tx.Query(`SELECT source_kind,source_id,enabled,revision FROM focus_space_sources WHERE owner_id=? AND profile_id=? AND focus_id=? ORDER BY source_kind,source_id LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusSpaceSourceLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]FocusSourceLink, 0, focusSpaceSourceLimit)
	for rows.Next() {
		var item FocusSourceLink
		var enabled int
		if err := rows.Scan(&item.SourceKind, &item.SourceID, &enabled, &item.Revision); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		result = append(result, item)
	}
	return result, rows.Err()
}

type focusCandidate struct {
	Envelope focusEvidenceEnvelope
	Score    int
	Date     string
}

func focusCandidateEnvelopes(candidates []focusCandidate) []focusEvidenceEnvelope {
	result := make([]focusEvidenceEnvelope, 0, len(candidates))
	for index := range candidates {
		envelope := candidates[index].Envelope
		envelope.Rank = index + 1
		result = append(result, envelope)
	}
	return result
}

func articleBlockForExcerpt(raw, excerpt string, terms []string) (ArticleBlock, bool) {
	blocks, err := DecodeArticleBlocks(raw)
	if err != nil || len(blocks) == 0 {
		return ArticleBlock{}, false
	}
	normalize := func(value string) string {
		return strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	}
	wanted := normalize(excerpt)
	if wanted == "" {
		return ArticleBlock{}, false
	}
	matches := make([]ArticleBlock, 0, 2)
	for _, block := range blocks {
		value := normalize(block.Text)
		if value == "" {
			continue
		}
		if value == wanted || strings.Contains(value, wanted) || strings.Contains(wanted, value) {
			matches = append(matches, block)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	// Summary-mode claims often carry a feed summary rather than the article
	// body. When that happens, use the focus terms to select one uniquely
	// relevant cached block; ties intentionally remain article-level fallback.
	bestScore := 0
	best := make([]ArticleBlock, 0, 2)
	for _, block := range blocks {
		value := strings.ToLower(normalize(block.Text))
		score := 0
		for _, term := range terms {
			term = strings.ToLower(normalize(term))
			if term != "" && strings.Contains(value, term) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = []ArticleBlock{block}
		} else if score > 0 && score == bestScore {
			best = append(best, block)
		}
	}
	if bestScore > 0 && len(best) == 1 {
		return best[0], true
	}
	// Full-text excerpts can contain several adjacent blocks. Use a bounded
	// prefix only when it identifies one block; otherwise keep article-level
	// fallback rather than risking a misleading paragraph anchor.
	prefix := wanted
	if len([]rune(prefix)) > 120 {
		prefix = string([]rune(prefix)[:120])
	}
	matches = matches[:0]
	for _, block := range blocks {
		if strings.Contains(normalize(block.Text), prefix) {
			matches = append(matches, block)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return ArticleBlock{}, false
}

func focusEvidenceSourceLocatorJSON(item focusEvidenceEnvelope) string {
	if len(item.SourceLocator) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(item.SourceLocator)
	if err != nil || len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func focusRunKey(focusID string, focusRevision int, scope, analysisProfile, evidenceSetHash string) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s:%s", focusID, focusRevision, scope, analysisProfile, evidenceSetHash, focusPromptVersion)
}

func focusRunClaimExpired(run focusRunSummary, now time.Time) bool {
	if run.Status != "claimed" || strings.TrimSpace(run.ExpiresAt) == "" {
		return false
	}
	expires, err := time.Parse(time.RFC3339, run.ExpiresAt)
	if err != nil {
		return true
	}
	return !now.Before(expires)
}

func activeFocusRunBySelectorTx(tx *sql.Tx, selector string, now time.Time) (focusRunSummary, bool, error) {
	return focusRunByQueryTx(tx, `WHERE owner_id=? AND profile_id=? AND selector_key=? AND status='claimed' AND expires_at>?`, appDataScope.OwnerID, appDataScope.ProfileID, selector, now.UTC().Format(time.RFC3339))
}

func retryableFocusRunBySelectorTx(tx *sql.Tx, selector, focusID string, focusRevision int, analysisProfile string) (focusRunSummary, bool, error) {
	return focusRunByQueryTx(tx, `WHERE owner_id=? AND profile_id=? AND selector_key=? AND focus_id=? AND focus_revision=? AND analysis_profile=? AND prompt_version=? AND status='failed' AND candidate_count>0 ORDER BY updated_at DESC,id DESC LIMIT 1`,
		appDataScope.OwnerID, appDataScope.ProfileID, selector, focusID, focusRevision, analysisProfile, focusPromptVersion)
}

func reclaimFocusRunTx(tx *sql.Tx, run focusRunSummary, now time.Time) (focusRunSummary, error) {
	if run.Status != "failed" {
		return run, nil
	}
	if active, found, err := activeFocusRunBySelectorTx(tx, run.SelectorKey, now); err != nil {
		return focusRunSummary{}, err
	} else if found && active.ID != run.ID {
		return active, nil
	}
	expiresAt := now.Add(focusAnalysisClaimTTL).Format(time.RFC3339)
	updatedAt := now.Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE focus_analysis_runs SET status='claimed',processed_count=0,actual_input_tokens=0,actual_output_tokens=0,error_code='',expires_at=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND status='failed'`, expiresAt, updatedAt, run.ID, appDataScope.OwnerID, appDataScope.ProfileID)
	if err != nil {
		return focusRunSummary{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return focusRunSummary{}, err
	}
	if changed != 1 {
		return focusRunSummary{}, errors.New("焦点分析 claim 恢复冲突")
	}
	run.Status = "claimed"
	run.ProcessedCount = 0
	run.ActualInput = 0
	run.ActualOutput = 0
	run.ErrorCode = ""
	run.ExpiresAt = expiresAt
	run.UpdatedAt = updatedAt
	return run, nil
}

func focusAnalysisReadResponseForRun(run focusRunSummary, availableCount int, budget focusBudgetProjection, sources []focusSourceStatus) (focusAnalysisReadResponse, error) {
	evidence, err := envelopesForRun(run)
	if err != nil {
		return focusAnalysisReadResponse{}, err
	}
	inputLow, inputHigh := 0, 0
	for _, item := range evidence {
		low, high := estimateFocusEvidenceTokens(item.Excerpt)
		inputLow += low
		inputHigh += high
	}
	focusTitle, focusGoal, decisionQuestion := "", "", ""
	intentKind, blueprintID, blueprintVersion, thinkingTemplateID, thinkingTemplateVersion := "", "", "", "", ""
	if spaces, spaceErr := listFocusSpaces(run.FocusID); spaceErr == nil && len(spaces) > 0 {
		focusTitle, focusGoal, decisionQuestion = spaces[0].Title, spaces[0].Goal, spaces[0].DecisionQuestion
		intentKind, blueprintID, blueprintVersion = spaces[0].IntentKind, spaces[0].BlueprintID, spaces[0].BlueprintVersion
		thinkingTemplateID, thinkingTemplateVersion = spaces[0].ThinkingTemplateID, spaces[0].ThinkingTemplateVersion
	}
	return focusAnalysisReadResponse{
		SchemaVersion: focusSpaceSchemaVersion, RunID: run.ID, FocusID: run.FocusID,
		FocusRevision: run.FocusRevision, EvidenceSetHash: run.EvidenceSetHash,
		AnalysisProfile: run.AnalysisProfile, PromptVersion: run.PromptVersion, Scope: run.Scope,
		Status: run.Status, FocusTitle: focusTitle, FocusGoal: focusGoal, DecisionQuestion: decisionQuestion,
		IntentKind: intentKind, BlueprintID: blueprintID, BlueprintVersion: blueprintVersion, ThinkingTemplateID: thinkingTemplateID, ThinkingTemplateVersion: thinkingTemplateVersion,
		ExpiresAt: run.ExpiresAt, Evidence: evidence,
		CandidateCount: run.CandidateCount, AvailableCount: availableCount,
		EstimatedInput: run.EstimatedInput, EstimatedOutput: run.EstimatedOutput,
		EstimatedTotalLow:  inputLow + len(evidence)*300,
		EstimatedTotalHigh: inputHigh + len(evidence)*600,
		Budget:             budget, Sources: sources, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func focusAnalysisReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	if !validFocusID(focusID) {
		app.UserErr(w, http.StatusBadRequest, "focus_id 不合法")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "question"
	}
	if scope != "bootstrap" && scope != "incremental" && scope != "question" {
		app.UserErr(w, http.StatusBadRequest, "分析范围不合法")
		return
	}
	limit := focusAnalysisLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > focusAnalysisLimit {
			app.UserErr(w, http.StatusBadRequest, "分析数量不合法")
			return
		}
		limit = parsed
	}
	includeFullText := false
	if raw := strings.TrimSpace(r.URL.Query().Get("include_full_text")); raw != "" {
		if raw != "0" && raw != "1" {
			app.UserErr(w, http.StatusBadRequest, "include_full_text 不合法")
			return
		}
		includeFullText = raw == "1"
	}
	claim := true
	if raw := strings.TrimSpace(r.URL.Query().Get("claim")); raw != "" {
		if raw != "0" && raw != "1" {
			app.UserErr(w, http.StatusBadRequest, "claim 不合法")
			return
		}
		claim = raw == "1"
	}
	if err := ensureLegacyFocusSpaces(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	focuses, err := listFocusSpaces(focusID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if len(focuses) != 1 {
		app.UserErr(w, http.StatusNotFound, "焦点空间不存在")
		return
	}
	space := focuses[0]
	if space.Status != "active" {
		app.UserErr(w, http.StatusConflict, "焦点空间未启用")
		return
	}
	candidates, sourceStatuses, err := collectFocusCandidates(space, scope, includeFullText)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	availableCount := len(candidates)
	now := time.Now().UTC()
	analysisProfile := "evidence-v1-summary"
	if includeFullText {
		analysisProfile = "evidence-v1-full-text"
	}
	if !claim {
		budget, budgetErr := readFocusBudgetProjection(nil, now)
		if budgetErr != nil {
			app.HTTPErr(w, budgetErr)
			return
		}
		if limit > budget.PerRunLimit {
			limit = budget.PerRunLimit
		}
		if limit > len(candidates) {
			limit = len(candidates)
		}
		preview := focusCandidateEnvelopes(candidates[:limit])
		previewHash := focusEvidenceSetHash(preview)
		inputLow, inputHigh := 0, 0
		for _, evidence := range preview {
			low, high := estimateFocusEvidenceTokens(evidence.Excerpt)
			inputLow += low
			inputHigh += high
		}
		status := "preview"
		if len(preview) == 0 {
			status = "empty"
		}
		app.WriteJSON(w, http.StatusOK, focusAnalysisReadResponse{
			SchemaVersion: focusSpaceSchemaVersion, FocusID: focusID, FocusRevision: space.Revision,
			FocusTitle: space.Title, FocusGoal: space.Goal, DecisionQuestion: space.DecisionQuestion,
			EvidenceSetHash: previewHash, AnalysisProfile: analysisProfile, PromptVersion: focusPromptVersion,
			Scope: scope, Status: status, Evidence: preview, CandidateCount: len(preview), AvailableCount: availableCount,
			EstimatedInput: inputHigh, EstimatedOutput: len(preview) * 600,
			EstimatedTotalLow: inputLow + len(preview)*300, EstimatedTotalHigh: inputHigh + len(preview)*600,
			Budget: budget, Sources: sourceStatuses, GeneratedAt: now.Format(time.RFC3339),
		})
		return
	}

	// Focus decisions have their own user-visible daily budget. Reader digest
	// maintenance must not consume it before the user opens an active Focus.
	focusAnalysisClaimMu.Lock()
	defer focusAnalysisClaimMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if err := expireFocusClaimsTx(tx, now.Format(time.RFC3339)); err != nil {
		app.HTTPErr(w, err)
		return
	}
	budget, err := readFocusBudgetProjection(tx, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if limit > budget.PerRunLimit {
		limit = budget.PerRunLimit
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}
	requested := focusCandidateEnvelopes(candidates[:limit])
	requestedHash := focusEvidenceSetHash(requested)
	requestedKey := focusRunKey(focusID, space.Revision, scope, analysisProfile, requestedHash)
	if existing, found, lookupErr := focusRunByKeyTx(tx, requestedKey); lookupErr != nil {
		app.HTTPErr(w, lookupErr)
		return
	} else if found {
		run, reclaimErr := reclaimFocusRunTx(tx, existing, now)
		if reclaimErr != nil {
			app.HTTPErr(w, reclaimErr)
			return
		}
		budget, err = readFocusBudgetProjection(tx, now)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		response, responseErr := focusAnalysisReadResponseForRun(run, availableCount, budget, sourceStatuses)
		if responseErr != nil {
			app.HTTPErr(w, responseErr)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}

	selector := "focus:" + focusID + ":" + scope
	if active, found, activeErr := activeFocusRunBySelectorTx(tx, selector, now); activeErr != nil {
		app.HTTPErr(w, activeErr)
		return
	} else if found {
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		response, responseErr := focusAnalysisReadResponseForRun(active, availableCount, budget, sourceStatuses)
		if responseErr != nil {
			app.HTTPErr(w, responseErr)
			return
		}
		response.Status = "busy"
		app.WriteJSON(w, http.StatusOK, response)
		return
	}

	if len(requested) == 0 {
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, focusAnalysisReadResponse{
			SchemaVersion: focusSpaceSchemaVersion, FocusID: focusID, FocusRevision: space.Revision,
			EvidenceSetHash: requestedHash, AnalysisProfile: analysisProfile, PromptVersion: focusPromptVersion,
			Scope: scope, Status: "empty", Evidence: []focusEvidenceEnvelope{}, CandidateCount: 0,
			AvailableCount: availableCount, Budget: budget, Sources: sourceStatuses, GeneratedAt: now.Format(time.RFC3339),
		})
		return
	}

	selected := make([]focusEvidenceEnvelope, 0, len(requested))
	estimatedHigh := 0
	for _, evidence := range requested {
		if len(selected) >= budget.DailyRemainingArticles {
			break
		}
		_, high := estimateFocusEvidenceTokens(evidence.Excerpt)
		if estimatedHigh+high+600 > budget.DailyRemainingTokens {
			break
		}
		selected = append(selected, evidence)
		estimatedHigh += high + 600
	}
	if len(selected) == 0 {
		// A failed run already reserved its evidence and daily budget. When the
		// owner retries on the same Focus revision, reuse that immutable evidence
		// instead of requiring a second reservation. This is especially important
		// after an application-side validation failure: the clarification answer
		// and model work remain recoverable without waiting for the next UTC day.
		if retryable, found, retryErr := retryableFocusRunBySelectorTx(tx, selector, focusID, space.Revision, analysisProfile); retryErr != nil {
			app.HTTPErr(w, retryErr)
			return
		} else if found {
			run, reclaimErr := reclaimFocusRunTx(tx, retryable, now)
			if reclaimErr != nil {
				app.HTTPErr(w, reclaimErr)
				return
			}
			budget, err = readFocusBudgetProjection(tx, now)
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			if err := tx.Commit(); err != nil {
				app.HTTPErr(w, err)
				return
			}
			response, responseErr := focusAnalysisReadResponseForRun(run, availableCount, budget, sourceStatuses)
			if responseErr != nil {
				app.HTTPErr(w, responseErr)
				return
			}
			app.WriteJSON(w, http.StatusOK, response)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusConflict, map[string]string{
			"code": "focus_daily_budget_exhausted", "message": "焦点分析每日额度已用完",
		})
		return
	}
	selectedHash := focusEvidenceSetHash(selected)
	key := focusRunKey(focusID, space.Revision, scope, analysisProfile, selectedHash)
	if existing, found, lookupErr := focusRunByKeyTx(tx, key); lookupErr != nil {
		app.HTTPErr(w, lookupErr)
		return
	} else if found {
		run, reclaimErr := reclaimFocusRunTx(tx, existing, now)
		if reclaimErr != nil {
			app.HTTPErr(w, reclaimErr)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		response, responseErr := focusAnalysisReadResponseForRun(run, availableCount, budget, sourceStatuses)
		if responseErr != nil {
			app.HTTPErr(w, responseErr)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}

	inputHigh := 0
	for _, evidence := range selected {
		_, high := estimateFocusEvidenceTokens(evidence.Excerpt)
		inputHigh += high
	}
	expiresAt := now.Add(focusAnalysisClaimTTL).Format(time.RFC3339)
	// The idempotency key is scoped by the database row owner/profile, but the
	// run primary key is global within SQLite. Include the same scope in the
	// deterministic ID so two profiles cannot collide when they choose equal
	// focus IDs and evidence hashes.
	runID := stableObjectID("focus-run", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+key)
	run := focusRunSummary{ID: runID, FocusID: focusID, FocusRevision: space.Revision, EvidenceSetHash: selectedHash, AnalysisProfile: analysisProfile, PromptVersion: focusPromptVersion, Scope: scope, Status: "claimed", CandidateCount: len(selected), EstimatedInput: inputHigh, EstimatedOutput: len(selected) * 600, IdempotencyKey: key, SelectorKey: selector, ExpiresAt: expiresAt, CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano)}
	if _, err := tx.Exec(`INSERT INTO focus_analysis_runs(id,owner_id,profile_id,focus_id,focus_revision,evidence_set_hash,analysis_profile,prompt_version,scope,status,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,actual_input_tokens,actual_output_tokens,idempotency_key,selector_key,expires_at,error_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.FocusRevision, run.EvidenceSetHash, run.AnalysisProfile, run.PromptVersion, run.Scope, run.Status, run.CandidateCount, 0, run.EstimatedInput, run.EstimatedOutput, 0, 0, run.IdempotencyKey, run.SelectorKey, run.ExpiresAt, "", run.CreatedAt, run.UpdatedAt); err != nil {
		app.HTTPErr(w, err)
		return
	}
	for _, evidence := range selected {
		if _, err := tx.Exec(`INSERT INTO focus_analysis_evidence(run_id,owner_id,profile_id,evidence_id,source_kind,source_id,source_revision,title,locator,observed_at,published_at,excerpt,access_scope,rank,created_at,source_locator_json,entry_id,content_hash,block_id,quote,text_hash,evidence_role) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, evidence.ID, evidence.SourceKind, evidence.SourceID, evidence.SourceRevision, evidence.Title, evidence.Locator, evidence.ObservedAt, evidence.PublishedAt, evidence.Excerpt, evidence.AccessScope, evidence.Rank, run.CreatedAt, focusEvidenceSourceLocatorJSON(evidence), evidence.EntryID, evidence.ContentHash, evidence.BlockID, evidence.Quote, evidence.TextHash, firstNonEmpty(evidence.EvidenceRole, "support")); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	budget, err = readFocusBudgetProjection(tx, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, responseErr := focusAnalysisReadResponseForRun(run, availableCount, budget, sourceStatuses)
	if responseErr != nil {
		app.HTTPErr(w, responseErr)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func collectFocusCandidates(space FocusSpace, scope string, includeFullText bool) ([]focusCandidate, []focusSourceStatus, error) {
	allowed := make(map[string]map[string]struct{})
	hasConfiguredSources := len(space.Sources) > 0
	for _, source := range space.Sources {
		if !source.Enabled {
			continue
		}
		kind, known := normalizeFocusSourceKind(source.SourceKind)
		if !known {
			continue
		}
		if allowed[kind] == nil {
			allowed[kind] = make(map[string]struct{})
		}
		allowed[kind][source.SourceID] = struct{}{}
	}
	filterSource := func(kind, id string) bool {
		if canonical, known := normalizeFocusSourceKind(kind); known {
			kind = canonical
		}
		ids, restricted := allowed[kind]
		if !hasConfiguredSources {
			// A Focus without linked sources may still analyze explicitly added
			// materials and saved clarification answers. It must not silently
			// widen into a scan of every Reader, meeting, and Obsidian record.
			return false
		}
		if !restricted {
			return false
		}
		_, ok := ids[id]
		return ok
	}
	terms := focusTerms(space.Goal + " " + space.DecisionQuestion)
	result := make([]focusCandidate, 0, focusEvidenceScanLimit)
	statuses := make([]focusSourceStatus, 0, 3)
	adapterErrors := make([]error, 0, 3)
	successfulAdapters := 0
	accessScope := "owner/profile-scoped"
	fullTextFlag := 0
	if includeFullText {
		fullTextFlag = 1
	}
	answerRows, answerErr := db.Query(`SELECT r.id,q.question_id,substr(q.question,1,240),a.answer_json,a.revision,substr(a.answered_at,1,80),q.question_type,q.options_json
FROM focus_decision_runs r
JOIN focus_decision_questions q
  ON q.run_id=r.id AND q.owner_id=r.owner_id AND q.profile_id=r.profile_id
JOIN focus_decision_answers a
  ON a.run_id=q.run_id AND a.question_id=q.question_id
 AND a.owner_id=q.owner_id AND a.profile_id=q.profile_id AND a.is_current=1
WHERE r.owner_id=? AND r.profile_id=? AND r.focus_id=?
  AND r.status IN ('queued','collecting_context','running_ai','waiting_clarification','applying','failed_retryable')
ORDER BY r.updated_at DESC,q.sequence_no ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, space.ID, decisionRunQuestionLimit)
	answerStatus := focusSourceStatus{SourceKind: "user_answer", Status: "ok"}
	if answerErr != nil {
		answerStatus.Status, answerStatus.Message = "unavailable", "source_unavailable"
		adapterErrors = append(adapterErrors, answerErr)
	} else {
		for answerRows.Next() {
			var runID, questionID, question, answerJSON, answeredAt, questionType, optionsJSON string
			var answerRevision int
			if err := answerRows.Scan(&runID, &questionID, &question, &answerJSON, &answerRevision, &answeredAt, &questionType, &optionsJSON); err != nil {
				answerRows.Close()
				return nil, statuses, err
			}
			answerStatus.RecordsSeen++
			answer := strings.TrimSpace(decisionAnswerDisplay(answerJSON, questionType, optionsJSON))
			if answer == "" {
				continue
			}
			sourceID := "answer-" + shortDigest(runID+"\x00"+questionID+"\x00"+strconv.Itoa(answerRevision))
			envelope := focusEvidenceEnvelope{
				// Persist through the existing generic entry kind so upgraded packages
				// remain compatible with the source_kind CHECK on installed databases.
				// The stable ID and locator retain the real user_answer provenance.
				ID: "user_answer:" + sourceID, SourceKind: "entry", SourceID: sourceID,
				SourceRevision: strconv.Itoa(answerRevision), Title: question, Locator: "focus-answer:" + sourceID,
				Quote: answer, TextHash: articleTextHash(answer), SourceLocator: map[string]any{
					"kind": "user_answer", "run_id": runID, "question_id": questionID, "answer_revision": answerRevision,
				}, ObservedAt: answeredAt, Excerpt: answer, AccessScope: accessScope, EvidenceRole: "support",
			}
			answerStatus.RecordsUsed++
			result = append(result, focusCandidate{Envelope: envelope, Score: 30000, Date: answeredAt})
		}
		if err := answerRows.Close(); err != nil {
			return nil, statuses, err
		}
		if answerStatus.RecordsSeen > 0 {
			successfulAdapters++
		}
	}
	statuses = append(statuses, answerStatus)

	rows, err := db.Query(`SELECT id,substr(title,1,240),substr(locator,1,2048),substr(observed_at,1,80),
CASE WHEN ?=1 AND content<>'' THEN substr(content,1,?) ELSE substr(quote,1,?) END,
source_type,COALESCE(evidence_version,'')
FROM evidence
WHERE lower(source_type) IN ('obsidian','note','file')
ORDER BY observed_at DESC,id ASC LIMIT ?`, fullTextFlag, focusEvidenceExcerptMax, focusEvidenceExcerptMax, focusEvidenceScanLimit)
	evidenceStatus := focusSourceStatus{SourceKind: "obsidian", Status: "ok"}
	if err != nil {
		evidenceStatus.Status, evidenceStatus.Message = "unavailable", "source_unavailable"
		adapterErrors = append(adapterErrors, err)
	} else {
		successfulAdapters++
		for rows.Next() {
			var id, title, locator, observed, excerpt, sourceType, version string
			if err := rows.Scan(&id, &title, &locator, &observed, &excerpt, &sourceType, &version); err != nil {
				rows.Close()
				return nil, statuses, err
			}
			evidenceStatus.RecordsSeen++
			kind, known := normalizeFocusSourceKind(sourceType)
			if !known || !filterSource(kind, id) || !focusDateWithinHorizon(space, observed, scope) {
				continue
			}
			if version == "" {
				version = shortDigest(title + "\x00" + observed + "\x00" + excerpt)
			}
			envelope := focusEvidenceEnvelope{ID: kind + ":" + id, SourceKind: kind, SourceID: id, SourceRevision: version, Title: title, Locator: locator, EntryID: id, Quote: excerpt, TextHash: articleTextHash(excerpt), SourceLocator: map[string]any{"path": locator}, ObservedAt: observed, Excerpt: excerpt, AccessScope: accessScope, EvidenceRole: "support"}
			score := scoreFocusText(terms, envelope.Title, envelope.Excerpt)
			if score == 0 {
				continue
			}
			evidenceStatus.RecordsUsed++
			result = append(result, focusCandidate{Envelope: envelope, Score: score, Date: observed})
		}
		if err := rows.Close(); err != nil {
			return nil, statuses, err
		}
	}
	statuses = append(statuses, evidenceStatus)

	// Scan linked Reader sources before the global unread stream. A Focus may
	// intentionally bind a source whose newest items are older than the latest
	// 100 rows across all subscriptions; applying the allowlist only after a
	// global LIMIT would then make a healthy Focus look empty.
	rssSourceIDs := make([]string, 0, len(allowed["rss"]))
	for sourceID := range allowed["rss"] {
		rssSourceIDs = append(rssSourceIDs, sourceID)
	}
	sort.Strings(rssSourceIDs)
	newsQuery := `SELECT n.id,n.source_id,substr(n.title,1,240),substr(n.url,1,2048),substr(n.published_at,1,80),n.source_type,
	CASE WHEN ?=1 AND COALESCE(ft.content_text,'')<>'' THEN substr(ft.content_text,1,?) ELSE substr(n.summary_text,1,?) END,
		n.content_hash,substr(n.fetched_at,1,80),COALESCE(ft.blocks_json,''),CASE WHEN fm.id IS NULL THEN 0 ELSE 1 END
	FROM news n
	LEFT JOIN focus_space_materials fm ON fm.owner_id=? AND fm.profile_id=? AND fm.focus_id=?
		AND fm.source_kind='entry' AND fm.entry_id=n.id AND fm.enabled=1
	LEFT JOIN feed_subscriptions sub ON sub.source_id=n.source_id AND sub.enabled=1
	LEFT JOIN feed_sources fs ON fs.id=n.source_id AND fs.hidden=0
	LEFT JOIN item_preferences ip ON ip.item_key='news:' || n.id
	LEFT JOIN news_preferences np ON np.news_id=n.id AND np.dismissed=1
	LEFT JOIN entry_full_texts ft ON ft.entry_id=n.id AND ft.content_hash=n.content_hash AND ft.status IN ('ok','stale')
	WHERE n.deleted_at='' AND (
		fm.id IS NOT NULL OR (sub.source_id IS NOT NULL AND fs.id IS NOT NULL AND COALESCE(ip.is_read,0)=0 AND np.news_id IS NULL)
	)
	`
	newsArgs := []any{fullTextFlag, focusEvidenceExcerptMax, focusEvidenceExcerptMax, appDataScope.OwnerID, appDataScope.ProfileID, space.ID}
	if len(rssSourceIDs) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(rssSourceIDs)), ",")
		newsQuery += " ORDER BY CASE WHEN n.source_id IN (" + placeholders + ") THEN 0 ELSE 1 END, CASE WHEN fm.id IS NULL THEN 1 ELSE 0 END,n.published_at DESC,n.updated_at DESC LIMIT ?"
		for _, sourceID := range rssSourceIDs {
			newsArgs = append(newsArgs, sourceID)
		}
	} else {
		newsQuery += " ORDER BY CASE WHEN fm.id IS NULL THEN 1 ELSE 0 END,n.published_at DESC,n.updated_at DESC LIMIT ?"
	}
	newsArgs = append(newsArgs, focusEvidenceScanLimit)
	rows, err = db.Query(newsQuery, newsArgs...)
	rssStatus := focusSourceStatus{SourceKind: "rss", Status: "ok"}
	entryStatus := focusSourceStatus{SourceKind: "entry", Status: "ok"}
	if err != nil {
		rssStatus.Status, rssStatus.Message = "unavailable", "source_unavailable"
		adapterErrors = append(adapterErrors, err)
	} else {
		successfulAdapters++
		for rows.Next() {
			var id, sourceID, title, locator, published, sourceType, excerpt, hash, fetched, blocksJSON string
			var explicitlyLinked int
			if err := rows.Scan(&id, &sourceID, &title, &locator, &published, &sourceType, &excerpt, &hash, &fetched, &blocksJSON, &explicitlyLinked); err != nil {
				rows.Close()
				return nil, statuses, err
			}
			if explicitlyLinked == 1 {
				entryStatus.RecordsSeen++
			} else {
				rssStatus.RecordsSeen++
			}
			candidateKind, known := normalizeFocusSourceKind(sourceType)
			if !known {
				// Older Reader rows did not persist a provider-specific source type;
				// they are still RSS candidates and must keep the legacy behavior.
				candidateKind = "rss"
			}
			if explicitlyLinked == 0 && (!filterSource(candidateKind, sourceID) || !focusDateWithinHorizon(space, published, scope)) {
				continue
			}
			if hash == "" {
				hash = shortDigest(title + "\x00" + published + "\x00" + excerpt)
			}
			kind := candidateKind
			if explicitlyLinked == 1 {
				kind = "entry"
			}
			envelope := focusEvidenceEnvelope{ID: kind + ":" + id, SourceKind: kind, SourceID: id, SourceRevision: hash, Title: title, Locator: locator, EntryID: id, ContentHash: hash, Quote: excerpt, TextHash: articleTextHash(excerpt), SourceLocator: map[string]any{"url": locator, "entry_id": id, "feed_source_id": sourceID, "explicit_focus_material": explicitlyLinked == 1}, PublishedAt: published, ObservedAt: firstNonEmpty(fetched, published), Excerpt: excerpt, AccessScope: accessScope, EvidenceRole: "support"}
			if block, ok := articleBlockForExcerpt(blocksJSON, excerpt, terms); ok {
				envelope.BlockID = block.BlockID
				envelope.TextHash = block.TextHash
				envelope.Quote = block.Text
			}
			score := scoreFocusText(terms, envelope.Title, envelope.Excerpt)
			if explicitlyLinked == 1 {
				score += 10000
			}
			if score == 0 {
				continue
			}
			if explicitlyLinked == 1 {
				entryStatus.RecordsUsed++
			} else {
				rssStatus.RecordsUsed++
			}
			result = append(result, focusCandidate{Envelope: envelope, Score: score, Date: published})
		}
		if err := rows.Close(); err != nil {
			return nil, statuses, err
		}
	}
	statuses = append(statuses, rssStatus)
	statuses = append(statuses, entryStatus)

	rows, err = db.Query(`SELECT record_id,substr(title,1,240),substr(started_at,1,80),substr(updated_at,1,80),
CASE WHEN summary<>'' THEN substr(summary,1,?) ELSE substr(report,1,?) END
FROM recordings ORDER BY started_at DESC,updated_at DESC LIMIT ?`, focusEvidenceExcerptMax, focusEvidenceExcerptMax, focusEvidenceScanLimit)
	meetingStatus := focusSourceStatus{SourceKind: "meeting", Status: "ok"}
	if err != nil {
		meetingStatus.Status, meetingStatus.Message = "unavailable", "source_unavailable"
		adapterErrors = append(adapterErrors, err)
	} else {
		successfulAdapters++
		for rows.Next() {
			var id, title, started, updated, excerpt string
			if err := rows.Scan(&id, &title, &started, &updated, &excerpt); err != nil {
				rows.Close()
				return nil, statuses, err
			}
			meetingStatus.RecordsSeen++
			if !filterSource("meeting", id) || !focusDateWithinHorizon(space, firstNonEmpty(updated, started), scope) {
				continue
			}
			observed := firstNonEmpty(updated, started)
			version := shortDigest(title + "\x00" + observed + "\x00" + excerpt)
			envelope := focusEvidenceEnvelope{ID: "meeting:" + id, SourceKind: "meeting", SourceID: id, SourceRevision: version, Title: title, Locator: "meeting:" + id, EntryID: id, Quote: excerpt, TextHash: articleTextHash(excerpt), SourceLocator: map[string]any{"recording_id": id}, ObservedAt: observed, Excerpt: excerpt, AccessScope: accessScope, EvidenceRole: "support"}
			score := scoreFocusText(terms, envelope.Title, envelope.Excerpt)
			if score == 0 {
				continue
			}
			meetingStatus.RecordsUsed++
			result = append(result, focusCandidate{Envelope: envelope, Score: score, Date: observed})
		}
		if err := rows.Close(); err != nil {
			return nil, statuses, err
		}
	}
	statuses = append(statuses, meetingStatus)

	// The SQLite projections above are retained for offline/replay behavior.
	// When App Host exposes the capability-gated device bridge, merge its live
	// meeting and Obsidian evidence into the same candidate set. A bridge
	// failure is source-scoped and must not hide usable RSS/local evidence.
	deviceCandidates, deviceStatuses, deviceErr := collectDeviceFocusCandidates(space, scope, includeFullText, terms, filterSource)
	if len(deviceStatuses) > 0 {
		statuses = mergeFocusSourceStatuses(statuses, deviceStatuses)
		for _, source := range deviceStatuses {
			if source.Status != "unavailable" {
				successfulAdapters++
			}
		}
	}
	if deviceErr != nil {
		adapterErrors = append(adapterErrors, deviceErr)
	}
	result = append(result, deviceCandidates...)
	if successfulAdapters == 0 {
		return nil, statuses, errors.Join(adapterErrors...)
	}
	// Source links are an allowlist. Explicit materials and user answers were
	// already admitted above without going through this source filter.
	seen := make(map[string]struct{}, len(result))
	filtered := make([]focusCandidate, 0, len(result))
	for _, candidate := range result {
		if _, ok := seen[candidate.Envelope.ID]; ok {
			continue
		}
		seen[candidate.Envelope.ID] = struct{}{}
		filtered = append(filtered, candidate)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Score != filtered[j].Score {
			return filtered[i].Score > filtered[j].Score
		}
		if filtered[i].Date != filtered[j].Date {
			return filtered[i].Date > filtered[j].Date
		}
		return filtered[i].Envelope.ID < filtered[j].Envelope.ID
	})
	if len(filtered) > focusEvidenceScanLimit {
		filtered = filtered[:focusEvidenceScanLimit]
	}
	return filtered, statuses, nil
}

func normalizeFocusSourceKind(raw string) (string, bool) {
	// Focus persists every subscription-backed provider as rss. Provider
	// labels remain valid at the API boundary so YouTube/Bilibili links from
	// the Reader can reuse the same allowlist without a schema migration.
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "obsidian", "note", "file":
		return "obsidian", true
	case "meeting", "recording", "profile_meetings":
		return "meeting", true
	case "rss", "reader", "feed", "youtube", "youtube-video", "bilibili", "bilibili-video", "b23":
		return "rss", true
	default:
		return "", false
	}
}

func focusTerms(value string) []string {
	// Reuse the bounded search tokenizer so natural Chinese goals do not
	// become one unmatchable phrase. For example, a goal such as
	// "跟踪 AI 芯片、模型和科技产品发布" must recall the linked feeds by
	// terms like "ai" and "芯片", rather than requiring an article to contain
	// the entire goal verbatim. The helper also caps the number of CJK
	// bigrams, keeping candidate scoring bounded for long user goals.
	return contextSearchTerms(normalizeReaderFocusText(value))
}

func scoreFocusText(terms []string, title, excerpt string) int {
	if len(terms) == 0 {
		return 1
	}
	score := 0
	for _, term := range terms {
		if focusTermInText(normalizeReaderFocusText(title), term) {
			score += 3
		}
		if focusTermInText(normalizeReaderFocusText(excerpt), term) {
			score++
		}
	}
	if score == 0 {
		return 0
	}
	return score
}

func focusDateWithinHorizon(space FocusSpace, value, scope string) bool {
	start, end := strings.TrimSpace(space.HorizonStart), strings.TrimSpace(space.HorizonEnd)
	if scope == "incremental" && start == "" {
		start = strings.TrimSpace(space.CreatedAt)
	}
	if start == "" && end == "" {
		return true
	}
	observed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return false
	}
	if start != "" {
		boundary, err := time.Parse(time.RFC3339, start)
		if err != nil || observed.Before(boundary) {
			return false
		}
	}
	if end != "" {
		boundary, err := time.Parse(time.RFC3339, end)
		if err != nil || observed.After(boundary) {
			return false
		}
	}
	return true
}

func estimateFocusEvidenceTokens(value string) (int, int) {
	chars := len([]rune(limitText(value, focusEvidenceExcerptMax)))
	return (chars + 3) / 4, (chars + 2) / 3
}

func focusEvidenceSetHash(items []focusEvidenceEnvelope) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.ID+"\x00"+item.SourceRevision)
	}
	sort.Strings(parts)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(digest[:])
}

type focusRunSummary struct {
	ID              string
	FocusID         string
	FocusRevision   int
	EvidenceSetHash string
	AnalysisProfile string
	PromptVersion   string
	Scope           string
	Status          string
	CandidateCount  int
	ProcessedCount  int
	EstimatedInput  int
	EstimatedOutput int
	ActualInput     int
	ActualOutput    int
	IdempotencyKey  string
	SelectorKey     string
	ExpiresAt       string
	ErrorCode       string
	CreatedAt       string
	UpdatedAt       string
}

type focusQueryer interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func focusQueryerFor(tx *sql.Tx) focusQueryer {
	if tx != nil {
		return tx
	}
	return db
}

func readFocusBudgetProjection(tx *sql.Tx, now time.Time) (focusBudgetProjection, error) {
	queryer := focusQueryerFor(tx)
	policy := defaultReaderFocusPolicy()
	var mode, fullText string
	var revision int
	err := queryer.QueryRow(`SELECT mode,daily_article_limit,daily_token_budget,per_run_limit,bootstrap_limit,full_text_mode,revision FROM reader_focus_policy WHERE id='default'`).Scan(
		&mode, &policy.DailyArticleLimit, &policy.DailyTokenBudget, &policy.PerRunLimit, &policy.BootstrapLimit, &fullText, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return focusBudgetProjection{}, err
	}
	if mode != "" {
		policy.Mode = mode
		policy.FullTextMode = fullText
	}
	if !validReaderFocusPolicy(policy) {
		return focusBudgetProjection{}, errors.New("焦点分析预算配置损坏")
	}
	day := now.UTC().Format("2006-01-02")
	usedArticles, usedTokens := 0, 0
	if err := queryer.QueryRow(`SELECT COALESCE(SUM(CASE
	WHEN status IN ('claimed','failed') AND candidate_count>processed_count THEN candidate_count
	ELSE processed_count END),0), COALESCE(SUM(CASE
	WHEN status IN ('claimed','failed') THEN estimated_input_tokens+estimated_output_tokens
	WHEN actual_input_tokens+actual_output_tokens>estimated_input_tokens+estimated_output_tokens THEN actual_input_tokens+actual_output_tokens
	ELSE estimated_input_tokens+estimated_output_tokens END),0)
FROM focus_analysis_runs
WHERE owner_id=? AND profile_id=? AND substr(created_at,1,10)=?
  AND status IN ('claimed','applied','partial','failed')`, appDataScope.OwnerID, appDataScope.ProfileID, day).Scan(&usedArticles, &usedTokens); err != nil {
		return focusBudgetProjection{}, err
	}
	return focusBudgetProjection{
		DailyArticleLimit:      policy.DailyArticleLimit,
		DailyTokenBudget:       policy.DailyTokenBudget,
		PerRunLimit:            policy.PerRunLimit,
		DailyUsedArticles:      usedArticles,
		DailyUsedTokens:        usedTokens,
		DailyRemainingArticles: maxInt(0, policy.DailyArticleLimit-usedArticles),
		DailyRemainingTokens:   maxInt(0, policy.DailyTokenBudget-usedTokens),
	}, nil
}

// focusAnalysisUsageCharge keeps the daily ledger conservative. A provider
// usage report can be missing or lower than the local estimate; neither case
// should release already reserved budget. Only a report above the estimate
// increases the charge.
func focusAnalysisUsageCharge(run focusRunSummary, usage focusAnalysisUsage) int {
	estimated := run.EstimatedInput + run.EstimatedOutput
	actual := usage.InputTokens + usage.OutputTokens
	if actual > estimated {
		return actual
	}
	return estimated
}

// validateFocusAnalysisBudgetTx runs while the apply transaction is open. The
// current claimed run is included in readFocusBudgetProjection as its
// estimate, so remove that reservation before checking the final charge. A
// run created on a previous UTC day is not part of today's projection and must
// not be subtracted.
func validateFocusAnalysisBudgetTx(tx *sql.Tx, run focusRunSummary, usage focusAnalysisUsage, now time.Time) error {
	if tx == nil {
		return errors.New("焦点分析预算事务不可用")
	}
	budget, err := readFocusBudgetProjection(tx, now)
	if err != nil {
		return err
	}
	usedWithoutRun := budget.DailyUsedTokens
	if strings.HasPrefix(run.CreatedAt, now.UTC().Format("2006-01-02")) {
		reserved := run.EstimatedInput + run.EstimatedOutput
		usedWithoutRun -= reserved
		if usedWithoutRun < 0 {
			usedWithoutRun = 0
		}
	}
	if usedWithoutRun+focusAnalysisUsageCharge(run, usage) > budget.DailyTokenBudget {
		return errFocusAnalysisBudget
	}
	return nil
}

func listRecentFocusRuns(focusID string, limit int) ([]focusAnalysisRunPublic, error) {
	if limit < 1 || limit > 20 {
		limit = 10
	}
	query := `SELECT id,focus_id,focus_revision,evidence_set_hash,scope,status,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,actual_input_tokens,actual_output_tokens,error_code,created_at,updated_at FROM focus_analysis_runs WHERE owner_id=? AND profile_id=?`
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID}
	if focusID != "" {
		query += ` AND focus_id=?`
		args = append(args, focusID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusAnalysisRunPublic, 0, limit)
	for rows.Next() {
		var run focusAnalysisRunPublic
		if err := rows.Scan(&run.RunID, &run.FocusID, &run.FocusRevision, &run.EvidenceSetHash, &run.Scope, &run.Status, &run.CandidateCount, &run.ProcessedCount, &run.EstimatedInput, &run.EstimatedOutput, &run.ActualInput, &run.ActualOutput, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func focusRunByKeyTx(tx *sql.Tx, key string) (focusRunSummary, bool, error) {
	return focusRunByQueryTx(tx, `WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, appDataScope.OwnerID, appDataScope.ProfileID, key)
}

func focusRunByIDTx(tx *sql.Tx, id string) (focusRunSummary, bool, error) {
	return focusRunByQueryTx(tx, `WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, id)
}

func focusRunByQueryTx(tx *sql.Tx, where string, args ...any) (focusRunSummary, bool, error) {
	var run focusRunSummary
	err := tx.QueryRow(`SELECT id,focus_id,focus_revision,evidence_set_hash,analysis_profile,prompt_version,scope,status,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,actual_input_tokens,actual_output_tokens,idempotency_key,selector_key,expires_at,error_code,created_at,updated_at FROM focus_analysis_runs `+where, args...).Scan(
		&run.ID, &run.FocusID, &run.FocusRevision, &run.EvidenceSetHash, &run.AnalysisProfile, &run.PromptVersion, &run.Scope, &run.Status, &run.CandidateCount, &run.ProcessedCount, &run.EstimatedInput, &run.EstimatedOutput, &run.ActualInput, &run.ActualOutput, &run.IdempotencyKey, &run.SelectorKey, &run.ExpiresAt, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return focusRunSummary{}, false, nil
	}
	if err != nil {
		return focusRunSummary{}, false, err
	}
	return run, true, nil
}

func expireFocusClaimsTx(tx *sql.Tx, now string) error {
	_, err := tx.Exec(`UPDATE focus_analysis_runs SET status='failed',error_code='claim_expired',updated_at=? WHERE owner_id=? AND profile_id=? AND status='claimed' AND expires_at<>'' AND expires_at<=?`, now, appDataScope.OwnerID, appDataScope.ProfileID, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? AND status<>'claimed' AND julianday(created_at)<julianday('now',?)`, appDataScope.OwnerID, appDataScope.ProfileID, fmt.Sprintf("-%d days", focusRunRetentionDays))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? AND status<>'claimed' AND id NOT IN (SELECT id FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? ORDER BY created_at DESC,id DESC LIMIT ?)`, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, focusRunRetentionLimit)
	return err
}

func pruneFocusIdempotencyTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM focus_analysis_idempotency WHERE owner_id=? AND profile_id=? AND julianday(created_at)<julianday('now','-7 days')`, appDataScope.OwnerID, appDataScope.ProfileID); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM focus_analysis_idempotency WHERE owner_id=? AND profile_id=? AND rowid IN (
SELECT rowid FROM focus_analysis_idempotency WHERE owner_id=? AND profile_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET ?
)`, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, maxPersistedMutationRecords-1)
	return err
}

func envelopesForRun(run focusRunSummary) ([]focusEvidenceEnvelope, error) {
	rows, err := db.Query(`SELECT e.evidence_id,e.source_kind,e.source_id,e.source_revision,e.title,e.locator,e.observed_at,e.published_at,e.excerpt,e.access_scope,e.rank,e.source_locator_json,e.entry_id,e.content_hash,e.block_id,e.quote,e.text_hash,e.evidence_role
FROM focus_analysis_evidence e
JOIN focus_analysis_runs r ON r.id=e.run_id AND r.owner_id=e.owner_id AND r.profile_id=e.profile_id
WHERE e.run_id=? AND e.owner_id=? AND e.profile_id=? AND r.focus_id=?
ORDER BY e.rank ASC,e.evidence_id ASC LIMIT ?`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, focusAnalysisLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusEvidenceEnvelope, 0, focusAnalysisLimit)
	for rows.Next() {
		var item focusEvidenceEnvelope
		var sourceLocatorJSON string
		if err := rows.Scan(&item.ID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Title, &item.Locator, &item.ObservedAt, &item.PublishedAt, &item.Excerpt, &item.AccessScope, &item.Rank, &sourceLocatorJSON, &item.EntryID, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.EvidenceRole); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(sourceLocatorJSON), &item.SourceLocator) != nil {
			item.SourceLocator = nil
		}
		if item.Quote == "" {
			item.Quote = item.Excerpt
		}
		// Older runs predate the citation columns. RSS evidence IDs are stable
		// `rss:<news id>` values, so deriving only this bounded identity keeps
		// legacy article-level citations useful without trusting model-provided
		// arbitrary paths or block IDs.
		if (item.SourceKind == "rss" || item.SourceKind == "entry") && item.EntryID == "" {
			item.EntryID = strings.TrimPrefix(strings.TrimPrefix(item.ID, "rss:"), "entry:")
			item.ContentHash = firstNonEmpty(item.ContentHash, item.SourceRevision)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusAnalysisApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusAnalysisApplyRequest
	if err := app.DecodeStrictJSON(w, r, 128<<10, &request); err != nil {
		var typeError *json.UnmarshalTypeError
		message := "焦点分析结果格式不合法"
		if errors.As(err, &typeError) {
			message += fmt.Sprintf("：字段 %s 必须为 %s", typeError.Field, typeError.Type)
		} else if strings.HasPrefix(err.Error(), "json: unknown field ") {
			message += "：存在未声明字段"
		}
		app.UserErr(w, http.StatusBadRequest, message)
		return
	}
	if request.SchemaVersion != focusSpaceSchemaVersion {
		app.UserErr(w, http.StatusBadRequest, "焦点分析结果格式不合法：schema_version 必须为 1")
		return
	}
	if request.Status == "" {
		request.Status = "applied"
	}
	// The claimed run is authoritative for every immutable analysis envelope
	// field. Models are good at producing bounded conclusions but can still
	// transpose a character while copying an opaque hash. Once the scoped run
	// and focus match, restore those server-owned values here; the transaction
	// below still rejects stale, expired or non-claimed runs.
	if validFocusID(request.RunID) {
		var boundFocusID, boundEvidenceHash, boundPromptVersion string
		var boundFocusRevision int
		if db.QueryRow(`SELECT focus_id,focus_revision,evidence_set_hash,prompt_version FROM focus_analysis_runs WHERE id=? AND owner_id=? AND profile_id=?`, request.RunID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&boundFocusID, &boundFocusRevision, &boundEvidenceHash, &boundPromptVersion) == nil &&
			request.FocusID == boundFocusID {
			request.FocusRevision = boundFocusRevision
			request.EvidenceSetHash = boundEvidenceHash
			request.PromptVersion = boundPromptVersion
		}
	}
	needsDecisionQuestion := false
	for _, item := range request.Decisions {
		if strings.TrimSpace(item.Question) == "" {
			needsDecisionQuestion = true
			break
		}
	}
	if needsDecisionQuestion && validFocusID(request.FocusID) {
		var storedQuestion string
		if db.QueryRow(`SELECT decision_question FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, request.FocusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&storedQuestion) == nil && strings.TrimSpace(storedQuestion) != "" {
			for index := range request.Decisions {
				if strings.TrimSpace(request.Decisions[index].Question) == "" {
					request.Decisions[index].Question = storedQuestion
				}
			}
		}
	}
	needsActionTask := false
	for _, item := range request.Actions {
		if strings.TrimSpace(item.Task) == "" {
			needsActionTask = true
			break
		}
	}
	if needsActionTask {
		storedGoal := ""
		if validFocusID(request.FocusID) {
			_ = db.QueryRow(`SELECT goal FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, request.FocusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&storedGoal)
		}
		for index := range request.Actions {
			if strings.TrimSpace(request.Actions[index].Task) != "" {
				continue
			}
			if len(request.Actions[index].ValidationItems) > 0 && strings.TrimSpace(request.Actions[index].ValidationItems[0]) != "" {
				request.Actions[index].Task = request.Actions[index].ValidationItems[0]
			} else if strings.TrimSpace(storedGoal) != "" {
				request.Actions[index].Task = storedGoal
			}
		}
	}
	if err := validateFocusAnalysisApply(request); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "焦点分析写入需要合法幂等键")
		return
	}
	fingerprintBytes, _ := json.Marshal(request)
	fingerprintDigest := sha256.Sum256(fingerprintBytes)
	fingerprint := hex.EncodeToString(fingerprintDigest[:])
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := pruneFocusIdempotencyTx(tx); err != nil {
		app.HTTPErr(w, err)
		return
	}
	var storedFingerprint, responseJSON string
	var responseStatus int
	err = tx.QueryRow(`SELECT fingerprint,response_json,response_status FROM focus_analysis_idempotency WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, appDataScope.OwnerID, appDataScope.ProfileID, key).Scan(&storedFingerprint, &responseJSON, &responseStatus)
	if err == nil {
		if storedFingerprint != fingerprint {
			app.UserErr(w, http.StatusConflict, "幂等键已用于另一份焦点分析")
			return
		}
		if responseJSON != "" {
			if err := tx.Commit(); err != nil {
				app.HTTPErr(w, err)
				return
			}
			w.Header().Set("X-Zettlab-Idempotent-Replay", "true")
			writeActionWorkflowResponse(w, responseStatus, []byte(responseJSON))
			return
		}
		app.UserErr(w, http.StatusConflict, "同一焦点分析仍待确认")
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		app.HTTPErr(w, err)
		return
	}
	if _, err := tx.Exec(`INSERT INTO focus_analysis_idempotency(owner_id,profile_id,idempotency_key,fingerprint,response_json,response_status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, key, fingerprint, "", 0, now, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	run, found, err := focusRunByIDTx(tx, request.RunID)
	if err != nil || !found {
		if err == nil {
			err = fmt.Errorf("%w: run 不存在", errFocusSpaceInvalid)
		}
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if run.FocusID != request.FocusID || run.FocusRevision != request.FocusRevision || run.EvidenceSetHash != request.EvidenceSetHash || run.PromptVersion != request.PromptVersion {
		app.UserErr(w, http.StatusConflict, errFocusAnalysisStale.Error())
		return
	}
	if focusRunClaimExpired(run, time.Now().UTC()) {
		if _, updateErr := tx.Exec(`UPDATE focus_analysis_runs SET status='failed',error_code='claim_expired',expires_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='claimed'`, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID); updateErr != nil {
			app.HTTPErr(w, updateErr)
			return
		}
		app.UserErr(w, http.StatusConflict, "焦点分析 claim 已过期")
		return
	}
	if run.Status == "applied" || run.Status == "partial" {
		response, responseErr := focusAnalysisGetResponseForRunTx(tx, run)
		if responseErr != nil {
			app.HTTPErr(w, responseErr)
			return
		}
		raw, _ := json.Marshal(response)
		if _, err := tx.Exec(`UPDATE focus_analysis_idempotency SET response_json=?,response_status=200,updated_at=? WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, string(raw), now, appDataScope.OwnerID, appDataScope.ProfileID, key); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}
	if run.Status != "claimed" {
		app.UserErr(w, http.StatusConflict, "焦点分析 claim 已失效")
		return
	}
	if request.Status != "failed" {
		if budgetErr := validateFocusAnalysisBudgetTx(tx, run, request.Usage, time.Now().UTC()); budgetErr != nil {
			if errors.Is(budgetErr, errFocusAnalysisBudget) {
				app.UserErr(w, http.StatusConflict, "焦点分析每日 token 预算不足，请减少本轮结果或稍后重试")
			} else {
				app.HTTPErr(w, budgetErr)
			}
			return
		}
		clarificationReason, clarificationErr := focusAnalysisClarificationReasonTx(tx, run, request)
		if clarificationErr != nil {
			app.HTTPErr(w, clarificationErr)
			return
		}
		if clarificationReason != "" {
			app.WriteJSON(w, http.StatusConflict, map[string]any{
				"code": "clarification_required", "error": clarificationReason,
			})
			return
		}
	}
	if err := applyFocusAnalysisTx(tx, run, request, now); err != nil {
		if errors.Is(err, errFocusAnalysisStale) {
			app.UserErr(w, http.StatusConflict, err.Error())
		} else {
			app.UserErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	run.Status = request.Status
	if request.Status == "failed" {
		run.ProcessedCount = 0
	} else {
		run.ProcessedCount = run.CandidateCount
	}
	response, responseErr := focusAnalysisGetResponseForRunTx(tx, run)
	if responseErr != nil {
		app.HTTPErr(w, responseErr)
		return
	}
	raw, _ := json.Marshal(response)
	if _, err := tx.Exec(`UPDATE focus_analysis_idempotency SET response_json=?,response_status=200,updated_at=? WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, string(raw), now, appDataScope.OwnerID, appDataScope.ProfileID, key); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func validateFocusAnalysisApply(request focusAnalysisApplyRequest) error {
	if !validFocusID(request.RunID) || !validFocusID(request.FocusID) || request.FocusRevision < 1 || len(request.EvidenceSetHash) != 64 || request.PromptVersion == "" || (request.Status != "applied" && request.Status != "partial" && request.Status != "failed") || !validText(request.ErrorCode, 80, false) {
		return errors.New("焦点分析字段不合法")
	}
	if request.Status == "failed" && request.ErrorCode == "" {
		return errors.New("失败分析缺少 error_code")
	}
	if request.Status == "failed" && (len(request.Signals) > 0 || len(request.Actions) > 0 || len(request.Decisions) > 0 || len(request.WorkProducts) > 0) {
		return errors.New("失败分析不能携带结果")
	}
	for i := range request.Signals {
		if !validFocusID(request.Signals[i].ID) {
			request.Signals[i].ID = fmt.Sprintf("signal-%d", i+1)
		}
		if strings.TrimSpace(request.Signals[i].Title) == "" {
			request.Signals[i].Title = "焦点信号"
		}
		if strings.TrimSpace(request.Signals[i].Confidence) == "" {
			request.Signals[i].Confidence = "medium"
		}
		if strings.TrimSpace(request.Signals[i].Freshness) == "" {
			request.Signals[i].Freshness = "current"
		}
	}
	for i := range request.Actions {
		if !validFocusID(request.Actions[i].ID) {
			request.Actions[i].ID = fmt.Sprintf("action-%d", i+1)
		}
		if strings.TrimSpace(request.Actions[i].Owner) == "" {
			request.Actions[i].Owner = "user"
		}
		if strings.TrimSpace(request.Actions[i].Priority) == "" {
			request.Actions[i].Priority = "medium"
		}
		if strings.TrimSpace(request.Actions[i].Confidence) == "" {
			request.Actions[i].Confidence = "medium"
		}
		if strings.TrimSpace(request.Actions[i].Freshness) == "" {
			request.Actions[i].Freshness = "current"
		}
	}
	for i := range request.Decisions {
		if !validFocusID(request.Decisions[i].ID) {
			request.Decisions[i].ID = fmt.Sprintf("decision-%d", i+1)
		}
		if strings.TrimSpace(request.Decisions[i].Question) == "" {
			request.Decisions[i].Question = "当前焦点的关键决策"
		}
		if strings.TrimSpace(request.Decisions[i].Confidence) == "" {
			request.Decisions[i].Confidence = "medium"
		}
		if strings.TrimSpace(request.Decisions[i].Freshness) == "" {
			request.Decisions[i].Freshness = "current"
		}
	}
	if len(request.Signals) > focusAnalysisLimit || len(request.Actions) > focusAnalysisLimit || len(request.Decisions) > focusAnalysisLimit || len(request.Signals)+len(request.Actions)+len(request.Decisions) > focusAnalysisLimit*2 {
		return errors.New("焦点分析结果过多")
	}
	if err := validateFocusWorkProducts(request.WorkProducts); err != nil {
		return err
	}
	seen := make(map[string]struct{})
	checkIDs := func(id string) error {
		if !validFocusID(id) {
			return errors.New("焦点结果标识不合法")
		}
		if _, ok := seen[id]; ok {
			return errors.New("焦点结果标识重复")
		}
		seen[id] = struct{}{}
		return nil
	}
	for _, item := range request.Signals {
		if err := checkIDs(item.ID); err != nil || !validText(item.Title, 240, true) || !validText(item.Detail, 2000, false) || !validText(item.Confidence, 32, true) || !validText(item.Freshness, 80, true) || !validIDs(item.SourceIDs) || !validTextList(item.SupportingEvidence, 10, 500) || !validTextList(item.Counterevidence, 10, 500) || !validTextList(item.ValidationItems, 10, 500) {
			return errors.New("Signal 字段不合法")
		}
	}
	for _, item := range request.Actions {
		if err := checkIDs(item.ID); err != nil || !validText(item.Task, 1000, true) || !validText(item.Owner, 120, true) || !validText(item.DueAt, 80, false) || !validText(item.Confidence, 32, true) || !validText(item.Freshness, 80, true) || !validIDs(item.SourceIDs) || !validTextList(item.SupportingEvidence, 10, 500) || !validTextList(item.Counterevidence, 10, 500) || !validTextList(item.ValidationItems, 10, 500) || !validActionPriority(item.Priority) {
			return errors.New("Action 字段不合法")
		}
	}
	for _, item := range request.Decisions {
		if err := checkIDs(item.ID); err != nil || !validText(item.Question, 500, true) || !validText(item.Recommendation, 1200, false) || !validText(item.ExpectedOutcome, 2000, false) || !validReviewAt(item.ReviewAt) || !validText(item.Confidence, 32, true) || !validText(item.Freshness, 80, true) || !validIDs(item.SourceIDs) || !validTextList(item.SupportingEvidence, 10, 500) || !validTextList(item.Counterevidence, 10, 500) || !validTextList(item.ValidationItems, 10, 500) || !validTextList(item.Options, 8, 500) || !validTextList(item.Assumptions, 8, 500) || !validTextList(item.Risks, 8, 500) {
			return errors.New("Decision 字段不合法")
		}
	}
	for _, item := range request.WorkProducts {
		if err := checkIDs(item.ID); err != nil {
			return errors.New("产出物标识不合法或重复")
		}
	}
	if request.Usage.InputTokens < 0 || request.Usage.OutputTokens < 0 || request.Usage.InputTokens > 100000 || request.Usage.OutputTokens > 100000 {
		return errors.New("焦点分析 usage 不合法")
	}
	return nil
}

func applyFocusAnalysisTx(tx *sql.Tx, run focusRunSummary, request focusAnalysisApplyRequest, now string) error {
	var currentRevision int
	if err := tx.QueryRow(`SELECT revision FROM focus_spaces WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID).Scan(&currentRevision); err != nil {
		return err
	}
	if currentRevision != request.FocusRevision {
		_, _ = tx.Exec(`UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',updated_at=? WHERE id=? AND status='claimed'`, now, run.ID)
		return errFocusAnalysisStale
	}
	allowed, err := focusRunEvidenceIDsTx(tx, run.ID)
	if err != nil {
		return err
	}
	evidenceIndex, err := focusRunEvidenceIndexTx(tx, run.ID)
	if err != nil {
		return err
	}
	signalCitations := make(map[string][]focusResultCitation, len(request.Signals))
	actionCitations := make(map[string][]focusResultCitation, len(request.Actions))
	decisionCitations := make(map[string][]focusResultCitation, len(request.Decisions))
	workProductCitations := make(map[string][]focusResultCitation, len(request.WorkProducts))
	for _, item := range request.Signals {
		if !focusResultSourcesAllowed(item.SourceIDs, allowed) {
			return errors.New("Signal 缺少有效证据引用")
		}
		citations, citationErr := resolveFocusCitations(tx, evidenceIndex, item.SourceIDs, item.Citations, item.SupportingCitations, item.CounterCitations)
		if citationErr != nil {
			return citationErr
		}
		signalCitations[item.ID] = citations
	}
	for _, item := range request.Actions {
		if !focusResultSourcesAllowed(item.SourceIDs, allowed) {
			return errors.New("Action 缺少有效证据引用")
		}
		citations, citationErr := resolveFocusCitations(tx, evidenceIndex, item.SourceIDs, item.Citations, item.SupportingCitations, item.CounterCitations)
		if citationErr != nil {
			return citationErr
		}
		actionCitations[item.ID] = citations
	}
	for _, item := range request.Decisions {
		if !focusResultSourcesAllowed(item.SourceIDs, allowed) {
			return errors.New("Decision 缺少有效证据引用")
		}
		citations, citationErr := resolveFocusCitations(tx, evidenceIndex, item.SourceIDs, item.Citations, item.SupportingCitations, item.CounterCitations)
		if citationErr != nil {
			return citationErr
		}
		decisionCitations[item.ID] = citations
	}
	for _, item := range request.WorkProducts {
		if len(item.SourceIDs) > 0 && !focusResultSourcesAllowed(item.SourceIDs, allowed) {
			return errors.New("产出物包含无效证据引用")
		}
		citations, citationErr := resolveFocusCitations(tx, evidenceIndex, item.SourceIDs, item.Citations, nil, nil)
		if citationErr != nil {
			return citationErr
		}
		workProductCitations[item.ID] = citations
	}
	if request.Status != "failed" {
		if err := persistFocusEvidenceProjectionTx(tx, run); err != nil {
			return err
		}
	}
	for rank, item := range request.Signals {
		storedID := focusResultStoredID("signal", run.ID, item.ID)
		if err := ensureFocusResultSlotTx(tx, "signal", storedID, run.FocusID, run.ID); err != nil {
			return err
		}
		if feedback, ok, feedbackErr := focusFeedbackStatusTx(tx, run.FocusID, "signal", storedID); feedbackErr != nil {
			return feedbackErr
		} else if focusFeedbackAllowsAnalysisWrite(feedback, ok) {
			ids, _ := json.Marshal(item.SourceIDs)
			supporting, _ := json.Marshal(item.SupportingEvidence)
			counter, _ := json.Marshal(item.Counterevidence)
			validation, _ := json.Marshal(item.ValidationItems)
			if _, err := tx.Exec(`INSERT INTO signals(id,title,detail,source_ids,confidence,counterevidence,status,focus_id,analysis_run_id,result_rank,prompt_version,evidence_version,supporting_evidence,freshness,validation_items) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,detail=excluded.detail,source_ids=excluded.source_ids,confidence=excluded.confidence,counterevidence=excluded.counterevidence,status=excluded.status,focus_id=excluded.focus_id,analysis_run_id=excluded.analysis_run_id,result_rank=excluded.result_rank,prompt_version=excluded.prompt_version,evidence_version=excluded.evidence_version,supporting_evidence=excluded.supporting_evidence,freshness=excluded.freshness,validation_items=excluded.validation_items`, storedID, item.Title, item.Detail, ids, item.Confidence, counter, "draft", run.FocusID, run.ID, rank+1, request.PromptVersion, request.EvidenceSetHash, supporting, item.Freshness, validation); err != nil {
				return err
			}
			if err := persistFocusResultCitationsTx(tx, run, "signal", storedID, signalCitations[item.ID], now); err != nil {
				return err
			}
		}
	}
	for rank, item := range request.Actions {
		storedID := focusResultStoredID("action", run.ID, item.ID)
		if err := ensureFocusResultSlotTx(tx, "action", storedID, run.FocusID, run.ID); err != nil {
			return err
		}
		if feedback, ok, feedbackErr := focusFeedbackStatusTx(tx, run.FocusID, "action", storedID); feedbackErr != nil {
			return feedbackErr
		} else if focusFeedbackAllowsAnalysisWrite(feedback, ok) {
			ids, _ := json.Marshal(item.SourceIDs)
			supporting, _ := json.Marshal(item.SupportingEvidence)
			counter, _ := json.Marshal(item.Counterevidence)
			validation, _ := json.Marshal(item.ValidationItems)
			if _, err := tx.Exec(`INSERT INTO actions(id,task,owner,due_at,source_ids,status,confirmation_status,priority,focus_id,analysis_run_id,result_rank,prompt_version,evidence_version,supporting_evidence,counterevidence,confidence,freshness,validation_items) VALUES(?,?,?,?,?,?,?, ?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET task=excluded.task,owner=excluded.owner,due_at=excluded.due_at,source_ids=excluded.source_ids,status=excluded.status,confirmation_status=excluded.confirmation_status,priority=excluded.priority,focus_id=excluded.focus_id,analysis_run_id=excluded.analysis_run_id,result_rank=excluded.result_rank,prompt_version=excluded.prompt_version,evidence_version=excluded.evidence_version,supporting_evidence=excluded.supporting_evidence,counterevidence=excluded.counterevidence,confidence=excluded.confidence,freshness=excluded.freshness,validation_items=excluded.validation_items`, storedID, item.Task, item.Owner, item.DueAt, ids, "draft", "draft", item.Priority, run.FocusID, run.ID, rank+1, request.PromptVersion, request.EvidenceSetHash, supporting, counter, item.Confidence, item.Freshness, validation); err != nil {
				return err
			}
			if err := persistFocusResultCitationsTx(tx, run, "action", storedID, actionCitations[item.ID], now); err != nil {
				return err
			}
		}
	}
	for rank, item := range request.Decisions {
		storedID := focusResultStoredID("decision", run.ID, item.ID)
		if err := ensureFocusResultSlotTx(tx, "decision", storedID, run.FocusID, run.ID); err != nil {
			return err
		}
		if feedback, ok, feedbackErr := focusFeedbackStatusTx(tx, run.FocusID, "decision", storedID); feedbackErr != nil {
			return feedbackErr
		} else if focusFeedbackAllowsAnalysisWrite(feedback, ok) {
			options, _ := json.Marshal(item.Options)
			ids, _ := json.Marshal(item.SourceIDs)
			assumptions, _ := json.Marshal(item.Assumptions)
			risks, _ := json.Marshal(item.Risks)
			supporting, _ := json.Marshal(item.SupportingEvidence)
			counter, _ := json.Marshal(item.Counterevidence)
			validation, _ := json.Marshal(item.ValidationItems)
			if _, err := tx.Exec(`INSERT INTO decisions(id,question,options,recommendation,expected_outcome,review_at,source_ids,assumptions,risks,status,focus_id,analysis_run_id,result_rank,prompt_version,evidence_version,supporting_evidence,counterevidence,confidence,freshness,validation_items) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET question=excluded.question,options=excluded.options,recommendation=excluded.recommendation,expected_outcome=excluded.expected_outcome,review_at=excluded.review_at,source_ids=excluded.source_ids,assumptions=excluded.assumptions,risks=excluded.risks,status=excluded.status,focus_id=excluded.focus_id,analysis_run_id=excluded.analysis_run_id,result_rank=excluded.result_rank,prompt_version=excluded.prompt_version,evidence_version=excluded.evidence_version,supporting_evidence=excluded.supporting_evidence,counterevidence=excluded.counterevidence,confidence=excluded.confidence,freshness=excluded.freshness,validation_items=excluded.validation_items`, storedID, item.Question, options, item.Recommendation, item.ExpectedOutcome, item.ReviewAt, ids, assumptions, risks, "draft", run.FocusID, run.ID, rank+1, request.PromptVersion, request.EvidenceSetHash, supporting, counter, item.Confidence, item.Freshness, validation); err != nil {
				return err
			}
			if err := persistFocusResultCitationsTx(tx, run, "decision", storedID, decisionCitations[item.ID], now); err != nil {
				return err
			}
		}
	}
	for rank, item := range request.WorkProducts {
		if err := persistFocusWorkProductTx(tx, run, request, item, workProductCitations[item.ID], rank+1, now); err != nil {
			return err
		}
	}
	if err := writeFocusEvidenceInterpretationsTx(tx, run, request, now); err != nil {
		return err
	}
	status := request.Status
	if status == "" {
		status = "applied"
	}
	result, err := tx.Exec(`UPDATE focus_analysis_runs SET status=?,processed_count=?,actual_input_tokens=?,actual_output_tokens=?,error_code=?,expires_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='claimed' AND expires_at>?`, status, func() int {
		if status == "failed" {
			return 0
		}
		return run.CandidateCount
	}(), request.Usage.InputTokens, request.Usage.OutputTokens, request.ErrorCode, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, now)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed != 1 {
		return errFocusAnalysisStale
	}
	return nil
}

// persistFocusEvidenceProjectionTx keeps the existing evidence projection
// aligned with a claimed Focus run without copying unbounded source content or
// activating stable context. The immutable run sidecar remains authoritative;
// this table is only a bounded, provenance-aware compatibility projection for
// older dashboard consumers.
func persistFocusEvidenceProjectionTx(tx *sql.Tx, run focusRunSummary) error {
	rows, err := tx.Query(`SELECT evidence_id,source_kind,title,locator,observed_at,excerpt,source_revision,rank
FROM focus_analysis_evidence
WHERE run_id=? AND owner_id=? AND profile_id=?
ORDER BY rank ASC,evidence_id ASC LIMIT ?`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, focusAnalysisLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var evidenceID, sourceKind, title, locator, observedAt, excerpt, evidenceVersion string
		var rank int
		if err := rows.Scan(&evidenceID, &sourceKind, &title, &locator, &observedAt, &excerpt, &evidenceVersion, &rank); err != nil {
			return err
		}
		// A source ID can already be present in the user's ordinary dashboard.
		// Preserve that record's content and ownership when it belongs to another
		// focus; only an unassigned row or this same focus may be re-projected.
		_, err := tx.Exec(`INSERT INTO evidence(id,title,locator,observed_at,quote,source_type,content,content_format,focus_id,analysis_run_id,rank,prompt_version,evidence_version)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET focus_id=excluded.focus_id,analysis_run_id=excluded.analysis_run_id,rank=excluded.rank,prompt_version=excluded.prompt_version,evidence_version=excluded.evidence_version
WHERE evidence.focus_id='' OR evidence.focus_id=excluded.focus_id`,
			evidenceID, title, locator, observedAt, excerpt, sourceKind, "", "text", run.FocusID, run.ID, rank, run.PromptVersion, evidenceVersion)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func focusResultStoredID(kind, runID, clientID string) string {
	return stableObjectID("focus-"+kind, runID+"\x00"+clientID)
}

func ensureFocusResultSlotTx(tx *sql.Tx, kind, id, focusID, runID string) error {
	var storedFocus, storedRun string
	var err error
	switch kind {
	case "signal":
		err = tx.QueryRow(`SELECT focus_id,analysis_run_id FROM signals WHERE id=?`, id).Scan(&storedFocus, &storedRun)
	case "action":
		err = tx.QueryRow(`SELECT focus_id,analysis_run_id FROM actions WHERE id=?`, id).Scan(&storedFocus, &storedRun)
	case "decision":
		err = tx.QueryRow(`SELECT focus_id,analysis_run_id FROM decisions WHERE id=?`, id).Scan(&storedFocus, &storedRun)
	default:
		return errors.New("未知焦点结果类型")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if storedFocus != focusID || storedRun != runID {
		return errors.New("焦点结果标识与已有数据冲突")
	}
	return nil
}

func focusRunEvidenceIDsTx(tx *sql.Tx, runID string) (map[string]struct{}, error) {
	index, err := focusRunEvidenceIndexTx(tx, runID)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{})
	for evidenceID := range index.byID {
		allowed[evidenceID] = struct{}{}
	}
	for sourceID, evidence := range index.bySource {
		if len(evidence) == 1 {
			allowed[sourceID] = struct{}{}
		}
	}
	return allowed, nil
}

type focusRunEvidenceIndex struct {
	byID     map[string]focusEvidenceEnvelope
	bySource map[string][]focusEvidenceEnvelope
}

func focusRunEvidenceIndexTx(tx *sql.Tx, runID string) (focusRunEvidenceIndex, error) {
	rows, err := tx.Query(`SELECT evidence_id,source_kind,source_id,source_revision,title,locator,observed_at,published_at,excerpt,access_scope,rank,source_locator_json,entry_id,content_hash,block_id,quote,text_hash,evidence_role
FROM focus_analysis_evidence
WHERE run_id=? AND owner_id=? AND profile_id=?
ORDER BY rank ASC,evidence_id ASC LIMIT ?`, runID, appDataScope.OwnerID, appDataScope.ProfileID, focusAnalysisLimit)
	if err != nil {
		return focusRunEvidenceIndex{}, err
	}
	defer rows.Close()
	index := focusRunEvidenceIndex{byID: make(map[string]focusEvidenceEnvelope), bySource: make(map[string][]focusEvidenceEnvelope)}
	for rows.Next() {
		var item focusEvidenceEnvelope
		var sourceLocatorJSON string
		if err := rows.Scan(&item.ID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Title, &item.Locator, &item.ObservedAt, &item.PublishedAt, &item.Excerpt, &item.AccessScope, &item.Rank, &sourceLocatorJSON, &item.EntryID, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.EvidenceRole); err != nil {
			return focusRunEvidenceIndex{}, err
		}
		if json.Unmarshal([]byte(sourceLocatorJSON), &item.SourceLocator) != nil {
			item.SourceLocator = nil
		}
		if item.Quote == "" {
			item.Quote = item.Excerpt
		}
		if item.EvidenceRole == "" {
			item.EvidenceRole = "support"
		}
		if item.SourceKind == "rss" && item.EntryID == "" {
			item.EntryID = strings.TrimPrefix(item.ID, "rss:")
			item.ContentHash = firstNonEmpty(item.ContentHash, item.SourceRevision)
		}
		index.byID[item.ID] = item
		if item.SourceID != "" {
			index.bySource[item.SourceID] = append(index.bySource[item.SourceID], item)
		}
	}
	return index, rows.Err()
}

func focusCitationRole(role, fallback string) string {
	value := strings.ToLower(strings.TrimSpace(role))
	if value == "counter" || value == "counterevidence" || value == "challenge" || value == "challenges" {
		return "challenge"
	}
	if value == "qualifies" || value == "qualification" {
		return "qualify"
	}
	if value == "gap" {
		return "gap"
	}
	if value == "support" || value == "supports" {
		return "support"
	}
	return firstNonEmpty(fallback, "support")
}

func focusCitationInputList(base, supporting, counter focusCitationInputs) []focusCitationInput {
	result := make([]focusCitationInput, 0, len(base)+len(supporting)+len(counter))
	for _, item := range base {
		item.Role = focusCitationRole(item.Role, "support")
		result = append(result, item)
	}
	for _, item := range supporting {
		item.Role = focusCitationRole(item.Role, "support")
		result = append(result, item)
	}
	for _, item := range counter {
		item.Role = focusCitationRole(item.Role, "challenge")
		result = append(result, item)
	}
	return result
}

func focusCitationFromEvidence(evidence focusEvidenceEnvelope, role string) focusResultCitation {
	assetID, mimeType := focusCitationAssetFields(evidence.SourceLocator)
	return focusResultCitation{
		EvidenceID: evidence.ID, SourceKind: evidence.SourceKind, SourceID: evidence.SourceID,
		SourceRevision: evidence.SourceRevision, Locator: evidence.Locator, SourceLocator: evidence.SourceLocator,
		EntryID: evidence.EntryID, AssetID: assetID, MIME: mimeType, ContentHash: evidence.ContentHash,
		BlockID: evidence.BlockID, Quote: firstNonEmpty(evidence.Quote, evidence.Excerpt), TextHash: evidence.TextHash,
		Role: focusCitationRole(role, evidence.EvidenceRole),
	}
}

func focusCitationAssetFields(locator map[string]any) (string, string) {
	if locator == nil {
		return "", ""
	}
	value := func(key string) string {
		raw, ok := locator[key]
		if !ok {
			return ""
		}
		text, ok := raw.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(text)
	}
	return firstNonEmpty(value("asset_id"), value("material_id")), strings.ToLower(value("mime"))
}

func focusCitationSourceMatch(input focusCitationInput, evidence focusEvidenceEnvelope) bool {
	if input.EvidenceID != "" && input.EvidenceID != evidence.ID {
		return false
	}
	if input.SourceID != "" && input.SourceID != evidence.SourceID && input.SourceID != evidence.ID {
		return false
	}
	return true
}

func validateFocusCitationText(item focusCitationInput) error {
	if !validText(item.EvidenceID, 256, false) || !validText(item.SourceKind, 40, false) || !validText(item.SourceID, 256, false) || !validText(item.SourceRevision, 160, false) || !validText(item.EntryID, 256, false) || !validText(item.ContentHash, 160, false) || !validText(item.BlockID, 128, false) || !validText(item.Quote, 2000, false) || !validText(item.TextHash, 160, false) || !validText(item.Role, 32, false) {
		return errors.New("焦点引用字段不合法")
	}
	return nil
}

func focusCitationMatchesBlock(tx *sql.Tx, entryID, contentHash, blockID, quote, textHash string) error {
	if blockID == "" {
		return nil
	}
	var storedHash, blocksJSON string
	err := tx.QueryRow(`SELECT COALESCE(NULLIF(n.content_hash,''),NULLIF(f.content_hash,''),''),COALESCE(f.blocks_json,'')
FROM news n LEFT JOIN entry_full_texts f ON f.entry_id=n.id
WHERE n.id=? AND n.deleted_at=''`, entryID).Scan(&storedHash, &blocksJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("焦点引用文章不存在")
	}
	if err != nil {
		return err
	}
	if contentHash != "" && storedHash != "" && contentHash != storedHash {
		return errors.New("焦点引用文章版本已变化")
	}
	blocks, err := DecodeArticleBlocks(blocksJSON)
	if err != nil {
		return errors.New("焦点引用段落结构不可用")
	}
	for _, block := range blocks {
		if block.BlockID != blockID {
			continue
		}
		if textHash != "" && block.TextHash != textHash {
			return errors.New("焦点引用段落版本已变化")
		}
		if quote != "" && !strings.Contains(strings.Join(strings.Fields(block.Text), " "), strings.Join(strings.Fields(quote), " ")) {
			return errors.New("焦点引用段落内容不匹配")
		}
		return nil
	}
	return errors.New("焦点引用段落不存在")
}

func resolveFocusCitations(tx *sql.Tx, index focusRunEvidenceIndex, sourceIDs []string, base, supporting, counter focusCitationInputs) ([]focusResultCitation, error) {
	inputs := focusCitationInputList(base, supporting, counter)
	if len(inputs) == 0 {
		for _, sourceID := range sourceIDs {
			if evidence, ok := index.byID[sourceID]; ok {
				inputs = append(inputs, focusCitationInput{EvidenceID: evidence.ID, Role: "support"})
				continue
			}
			matches := index.bySource[sourceID]
			if len(matches) != 1 {
				continue
			}
			inputs = append(inputs, focusCitationInput{EvidenceID: matches[0].ID, Role: "support"})
		}
	}
	if len(inputs) > focusAnalysisLimit*2 {
		return nil, errors.New("焦点引用过多")
	}
	result := make([]focusResultCitation, 0, len(inputs))
	seen := make(map[string]struct{})
	for _, input := range inputs {
		if err := validateFocusCitationText(input); err != nil {
			return nil, err
		}
		var evidence focusEvidenceEnvelope
		if input.EvidenceID != "" {
			var ok bool
			evidence, ok = index.byID[input.EvidenceID]
			if !ok {
				return nil, errors.New("焦点引用不属于当前分析")
			}
		} else if input.SourceID != "" {
			// `source_ids` historically carried both the bare source_id and the
			// scoped evidence id (`rss:<entry_id>`). Resolve the latter first so
			// legacy model payloads can opt into the structured citation shape
			// without changing their identifiers.
			if direct, ok := index.byID[input.SourceID]; ok {
				evidence = direct
			} else {
				matches := index.bySource[input.SourceID]
				if len(matches) != 1 {
					return nil, errors.New("焦点引用来源不唯一")
				}
				evidence = matches[0]
			}
		} else {
			return nil, errors.New("焦点引用缺少 evidence_id 或 source_id")
		}
		if !focusCitationSourceMatch(input, evidence) || (input.SourceKind != "" && input.SourceKind != evidence.SourceKind) || (input.SourceRevision != "" && input.SourceRevision != evidence.SourceRevision) {
			return nil, errors.New("焦点引用来源与证据不匹配")
		}
		if (input.EntryID != "" && (evidence.EntryID == "" || input.EntryID != evidence.EntryID)) ||
			(input.ContentHash != "" && (evidence.ContentHash == "" || input.ContentHash != evidence.ContentHash)) ||
			(input.BlockID != "" && (evidence.BlockID == "" || input.BlockID != evidence.BlockID)) ||
			(input.TextHash != "" && (evidence.TextHash == "" || input.TextHash != evidence.TextHash)) {
			return nil, errors.New("焦点引用定位与证据不匹配")
		}
		citation := focusCitationFromEvidence(evidence, input.Role)
		if input.EntryID != "" {
			citation.EntryID = input.EntryID
		}
		if input.ContentHash != "" {
			citation.ContentHash = input.ContentHash
		}
		if input.BlockID != "" {
			citation.BlockID = input.BlockID
		}
		if input.Quote != "" {
			citation.Quote = input.Quote
		}
		if input.TextHash != "" {
			citation.TextHash = input.TextHash
		}
		// Explicit Focus article materials are represented as `entry` evidence,
		// while legacy subscribed articles use `rss`. Both point at the same
		// versioned article/block store and must be checked before a decision can
		// persist a citation. Without the `entry` branch, a stale explicit article
		// citation could survive after its block content changed.
		if (citation.SourceKind == "rss" || citation.SourceKind == "entry") && citation.EntryID != "" {
			if err := focusCitationMatchesBlock(tx, citation.EntryID, citation.ContentHash, citation.BlockID, citation.Quote, citation.TextHash); err != nil {
				return nil, err
			}
		}
		key := strings.Join([]string{citation.EvidenceID, citation.BlockID, citation.Quote, citation.Role}, "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, citation)
	}
	return result, nil
}

func persistFocusResultCitationsTx(tx *sql.Tx, run focusRunSummary, kind, resultID string, citations []focusResultCitation, now string) error {
	if _, err := tx.Exec(`DELETE FROM focus_result_citations WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, kind, resultID); err != nil {
		if readerFocusSchemaMissing(err) {
			return nil
		}
		return err
	}
	for ordinal, citation := range citations {
		sourceLocator, marshalErr := json.Marshal(citation.SourceLocator)
		if marshalErr != nil || len(sourceLocator) == 0 {
			sourceLocator = []byte("{}")
		}
		if _, err := tx.Exec(`INSERT INTO focus_result_citations(owner_id,profile_id,focus_id,result_kind,result_id,run_id,ordinal,evidence_id,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,asset_id,mime,content_hash,block_id,quote,text_hash,role,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, kind, resultID, run.ID, ordinal, citation.EvidenceID, citation.SourceKind, citation.SourceID, citation.SourceRevision, citation.Locator, string(sourceLocator), citation.EntryID, citation.AssetID, citation.MIME, citation.ContentHash, citation.BlockID, citation.Quote, citation.TextHash, focusCitationRole(citation.Role, "support"), now); err != nil {
			if readerFocusSchemaMissing(err) {
				return persistFocusResultCitationsLegacyTx(tx, run, kind, resultID, citations, now)
			}
			return err
		}
	}
	return nil
}

func persistFocusResultCitationsLegacyTx(tx *sql.Tx, run focusRunSummary, kind, resultID string, citations []focusResultCitation, now string) error {
	for ordinal, citation := range citations {
		if _, err := tx.Exec(`INSERT INTO focus_result_citations(owner_id,profile_id,focus_id,result_kind,result_id,run_id,ordinal,evidence_id,source_kind,source_id,source_revision,entry_id,content_hash,block_id,quote,text_hash,role,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, kind, resultID, run.ID, ordinal, citation.EvidenceID, citation.SourceKind, citation.SourceID, citation.SourceRevision, citation.EntryID, citation.ContentHash, citation.BlockID, citation.Quote, citation.TextHash, focusCitationRole(citation.Role, "support"), now); err != nil {
			if readerFocusSchemaMissing(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

func readFocusResultCitationsTx(tx *sql.Tx, focusID, runID, kind, resultID string) ([]focusResultCitation, error) {
	rows, err := tx.Query(`SELECT evidence_id,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,asset_id,mime,content_hash,block_id,quote,text_hash,role
FROM focus_result_citations
WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=? AND run_id=?
ORDER BY ordinal ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, kind, resultID, runID, focusAnalysisLimit*2)
	if err != nil {
		if readerFocusSchemaMissing(err) {
			return readFocusResultCitationsLegacyTx(tx, focusID, runID, kind, resultID)
		}
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultCitation, 0, focusAnalysisLimit)
	for rows.Next() {
		var item focusResultCitation
		var sourceLocator string
		if err := rows.Scan(&item.EvidenceID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Locator, &sourceLocator, &item.EntryID, &item.AssetID, &item.MIME, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.Role); err != nil {
			return nil, err
		}
		if sourceLocator != "" && json.Unmarshal([]byte(sourceLocator), &item.SourceLocator) != nil {
			item.SourceLocator = nil
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readFocusResultCitationsLegacyTx(tx *sql.Tx, focusID, runID, kind, resultID string) ([]focusResultCitation, error) {
	rows, err := tx.Query(`SELECT evidence_id,source_kind,source_id,source_revision,entry_id,content_hash,block_id,quote,text_hash,role
FROM focus_result_citations
WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=? AND run_id=?
ORDER BY ordinal ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, kind, resultID, runID, focusAnalysisLimit*2)
	if err != nil {
		if readerFocusSchemaMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultCitation, 0, focusAnalysisLimit)
	for rows.Next() {
		var item focusResultCitation
		if err := rows.Scan(&item.EvidenceID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.EntryID, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.Role); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusResultSourcesAllowed(sourceIDs []string, allowed map[string]struct{}) bool {
	if len(sourceIDs) == 0 {
		return false
	}
	for _, sourceID := range sourceIDs {
		if _, ok := allowed[sourceID]; !ok {
			return false
		}
	}
	return true
}

func focusFeedbackStatusTx(tx *sql.Tx, focusID, kind, resultID string) (string, bool, error) {
	var status string
	err := tx.QueryRow(`SELECT status FROM focus_result_feedback WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, kind, resultID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return status, err == nil, err
}

// Only an unreviewed draft may be refreshed by a repeated analysis apply.
// Once a user has accepted, rejected, snoozed, or edited a result, a later
// partial retry must preserve that decision instead of silently replacing it.
func focusFeedbackAllowsAnalysisWrite(status string, exists bool) bool {
	return !exists || status == "draft"
}

func focusAnalysisGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	if !validFocusID(focusID) {
		app.UserErr(w, http.StatusBadRequest, "focus_id 不合法")
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	if runID != "" && !validFocusID(runID) {
		app.UserErr(w, http.StatusBadRequest, "run_id 不合法")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			app.UserErr(w, http.StatusBadRequest, "结果数量不合法")
			return
		}
		limit = parsed
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	response, err := focusAnalysisGetTx(tx, focusID, runID, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "焦点空间不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func focusAnalysisGetResponseForRunTx(tx *sql.Tx, run focusRunSummary) (focusAnalysisGetResponse, error) {
	return focusAnalysisGetTx(tx, run.FocusID, run.ID, 50)
}

func focusAnalysisGetTx(tx *sql.Tx, focusID, runID string, limit int) (focusAnalysisGetResponse, error) {
	var found string
	var currentFocusRevision int
	if err := tx.QueryRow(`SELECT id,revision FROM focus_spaces WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&found, &currentFocusRevision); err != nil {
		return focusAnalysisGetResponse{}, err
	}
	response := focusAnalysisGetResponse{SchemaVersion: focusSpaceSchemaVersion, FocusID: focusID, Runs: []map[string]any{}, Evidence: []focusEvidenceEnvelope{}, Signals: []focusResultProjection{}, Actions: []focusResultProjection{}, Decisions: []focusResultProjection{}, WorkProducts: []focusResultProjection{}, Results: []focusResultProjection{}}
	if runID != "" {
		var scopedRun string
		if err := tx.QueryRow(`SELECT id FROM focus_analysis_runs WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`, runID, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&scopedRun); err != nil {
			return response, err
		}
	}
	runs, err := tx.Query(`SELECT id,focus_revision,evidence_set_hash,scope,status,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,actual_input_tokens,actual_output_tokens,created_at,updated_at,error_code FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? AND focus_id=? ORDER BY created_at DESC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, minInt(limit, 20))
	if err != nil {
		return response, err
	}
	defer runs.Close()
	selectedRun := runID
	preferredRun := ""
	fallbackAppliedRun := ""
	for runs.Next() {
		var id, hash, scope, status, created, updated, errorCode string
		var revision, candidate, processed, input, output, actualInput, actualOutput int
		if err := runs.Scan(&id, &revision, &hash, &scope, &status, &candidate, &processed, &input, &output, &actualInput, &actualOutput, &created, &updated, &errorCode); err != nil {
			return response, err
		}
		if preferredRun == "" && revision == currentFocusRevision && (status == "applied" || status == "partial") {
			preferredRun = id
		}
		if fallbackAppliedRun == "" && (status == "applied" || status == "partial") {
			fallbackAppliedRun = id
		}
		response.Runs = append(response.Runs, map[string]any{"run_id": id, "focus_revision": revision, "evidence_set_hash": hash, "scope": scope, "status": status, "candidate_count": candidate, "processed_count": processed, "estimated_input_tokens": input, "estimated_output_tokens": output, "actual_input_tokens": actualInput, "actual_output_tokens": actualOutput, "created_at": created, "updated_at": updated, "error_code": errorCode})
	}
	if err := runs.Err(); err != nil {
		return response, err
	}
	if selectedRun == "" {
		if preferredRun != "" {
			selectedRun = preferredRun
		} else {
			// A newly claimed run is not yet a user-visible result. Keep the latest
			// applied projection visible while that run is collecting evidence or
			// waiting for the model; the decision-run panel separately exposes its
			// live progress. This prevents valid actions and decisions from
			// disappearing during a retry, clarification, or transient failure.
			selectedRun = fallbackAppliedRun
		}
	}
	response.RunID = selectedRun
	var selectedSummary focusRunSummary
	if selectedRun != "" {
		var selectedFound bool
		selectedSummary, selectedFound, err = focusRunByIDTx(tx, selectedRun)
		if err != nil {
			return response, err
		}
		if !selectedFound || selectedSummary.FocusID != focusID {
			return response, sql.ErrNoRows
		}
		rows, err := tx.Query(`SELECT e.evidence_id,e.source_kind,e.source_id,e.source_revision,e.title,e.locator,e.observed_at,e.published_at,e.excerpt,e.access_scope,e.rank,e.source_locator_json,e.entry_id,e.content_hash,e.block_id,e.quote,e.text_hash,e.evidence_role
FROM focus_analysis_evidence e
JOIN focus_analysis_runs r ON r.id=e.run_id AND r.owner_id=e.owner_id AND r.profile_id=e.profile_id
WHERE e.run_id=? AND e.owner_id=? AND e.profile_id=? AND r.focus_id=?
ORDER BY e.rank ASC,e.evidence_id ASC LIMIT ?`, selectedRun, appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusAnalysisLimit)
		if err != nil {
			return response, err
		}
		for rows.Next() {
			var item focusEvidenceEnvelope
			var sourceLocatorJSON string
			if err := rows.Scan(&item.ID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Title, &item.Locator, &item.ObservedAt, &item.PublishedAt, &item.Excerpt, &item.AccessScope, &item.Rank, &sourceLocatorJSON, &item.EntryID, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.EvidenceRole); err != nil {
				rows.Close()
				return response, err
			}
			if json.Unmarshal([]byte(sourceLocatorJSON), &item.SourceLocator) != nil {
				item.SourceLocator = nil
			}
			if item.Quote == "" {
				item.Quote = item.Excerpt
			}
			response.Evidence = append(response.Evidence, item)
		}
		if err := rows.Close(); err != nil {
			return response, err
		}
	}
	if selectedRun == "" {
		return response, nil
	}
	response.Signals, err = queryFocusSignalsTx(tx, focusID, selectedRun, limit)
	if err != nil {
		return response, err
	}
	response.Actions, err = queryFocusActionsTx(tx, focusID, selectedRun, limit)
	if err != nil {
		return response, err
	}
	response.Decisions, err = queryFocusDecisionsTx(tx, focusID, selectedRun, limit)
	if err != nil {
		return response, err
	}
	response.WorkProducts, err = queryFocusWorkProductsTx(tx, focusID, selectedRun, limit)
	if err != nil {
		return response, err
	}
	response.Results = append(response.Results, response.Signals...)
	response.Results = append(response.Results, response.Actions...)
	response.Results = append(response.Results, response.Decisions...)
	response.Results = append(response.Results, response.WorkProducts...)
	sort.SliceStable(response.Results, func(i, j int) bool {
		if response.Results[i].Rank != response.Results[j].Rank {
			return response.Results[i].Rank < response.Results[j].Rank
		}
		return response.Results[i].ID < response.Results[j].ID
	})
	response.CachedInterpretations, err = readFocusEvidenceInterpretationsTx(tx, selectedSummary, limit)
	if err != nil {
		return response, err
	}
	return response, nil
}

func queryFocusSignalsTx(tx *sql.Tx, focusID, runID string, limit int) ([]focusResultProjection, error) {
	rows, err := tx.Query(`SELECT s.id,s.title,s.detail,s.source_ids,s.supporting_evidence,s.counterevidence,s.confidence,s.freshness,s.validation_items,s.status,s.analysis_run_id,s.result_rank,s.prompt_version,s.evidence_version,COALESCE(f.revision,1),COALESCE(f.status,'draft')
FROM signals s
LEFT JOIN focus_result_feedback f ON f.owner_id=? AND f.profile_id=? AND f.focus_id=s.focus_id AND f.result_kind='signal' AND f.result_id=s.id
WHERE s.focus_id=? AND s.analysis_run_id=?
ORDER BY s.result_rank ASC,s.id ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultProjection, 0, limit)
	for rows.Next() {
		var item Signal
		var sourceJSON, supportingJSON, counterJSON, validationJSON string
		var runID, prompt, evidence, feedback string
		var rank, revision int
		var freshness string
		if err := rows.Scan(&item.ID, &item.Title, &item.Detail, &sourceJSON, &supportingJSON, &counterJSON, &item.Confidence, &freshness, &validationJSON, &item.Status, &runID, &rank, &prompt, &evidence, &revision, &feedback); err != nil {
			return nil, err
		}
		sourceIDs, supporting, counter, validation, err := decodeFocusResultLists(sourceJSON, supportingJSON, counterJSON, validationJSON)
		if err != nil {
			return nil, err
		}
		item.SourceIDs, item.Counterevidence = sourceIDs, counter
		citations, citationErr := readFocusResultCitationsTx(tx, focusID, runID, "signal", item.ID)
		if citationErr != nil {
			return nil, citationErr
		}
		data := focusResultData(item, supporting, counter, item.Confidence, freshness, validation)
		if len(citations) > 0 {
			data["citations"] = citations
		}
		result = append(result, focusResultProjection{Kind: "signal", ID: item.ID, FocusID: focusID, AnalysisRunID: runID, Rank: rank, PromptVersion: prompt, EvidenceVersion: evidence, Status: item.Status, Revision: revision, FeedbackStatus: feedback, SourceIDs: sourceIDs, Citations: citations, SupportingEvidence: supporting, Counterevidence: counter, Confidence: item.Confidence, Freshness: freshness, ValidationItems: validation, Data: data})
	}
	return result, rows.Err()
}

func queryFocusActionsTx(tx *sql.Tx, focusID, runID string, limit int) ([]focusResultProjection, error) {
	rows, err := tx.Query(`SELECT a.id,a.task,a.owner,a.due_at,a.source_ids,a.supporting_evidence,a.counterevidence,a.confidence,a.freshness,a.validation_items,a.status,a.confirmation_status,a.priority,a.analysis_run_id,a.result_rank,a.prompt_version,a.evidence_version,COALESCE(f.revision,1),COALESCE(f.status,'draft')
FROM actions a
LEFT JOIN focus_result_feedback f ON f.owner_id=? AND f.profile_id=? AND f.focus_id=a.focus_id AND f.result_kind='action' AND f.result_id=a.id
WHERE a.focus_id=? AND a.analysis_run_id=?
ORDER BY a.result_rank ASC,a.id ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultProjection, 0, limit)
	for rows.Next() {
		var item Action
		var sourceJSON, supportingJSON, counterJSON, validationJSON, confidence, freshness, runID, prompt, evidence, feedback string
		var rank, revision int
		if err := rows.Scan(&item.ID, &item.Task, &item.Owner, &item.DueAt, &sourceJSON, &supportingJSON, &counterJSON, &confidence, &freshness, &validationJSON, &item.Status, &item.ConfirmationStatus, &item.Priority, &runID, &rank, &prompt, &evidence, &revision, &feedback); err != nil {
			return nil, err
		}
		if feedback == "draft" && (item.ConfirmationStatus == actionConfirmationAccepted || item.ConfirmationStatus == actionConfirmationRejected) {
			feedback = item.ConfirmationStatus
		}
		sourceIDs, supporting, counter, validation, err := decodeFocusResultLists(sourceJSON, supportingJSON, counterJSON, validationJSON)
		if err != nil {
			return nil, err
		}
		item.SourceIDs = sourceIDs
		citations, citationErr := readFocusResultCitationsTx(tx, focusID, runID, "action", item.ID)
		if citationErr != nil {
			return nil, citationErr
		}
		data := focusResultData(item, supporting, counter, confidence, freshness, validation)
		if len(citations) > 0 {
			data["citations"] = citations
		}
		result = append(result, focusResultProjection{Kind: "action", ID: item.ID, FocusID: focusID, AnalysisRunID: runID, Rank: rank, PromptVersion: prompt, EvidenceVersion: evidence, Status: item.Status, Revision: revision, FeedbackStatus: feedback, SourceIDs: sourceIDs, Citations: citations, SupportingEvidence: supporting, Counterevidence: counter, Confidence: confidence, Freshness: freshness, ValidationItems: validation, Data: data})
	}
	return result, rows.Err()
}

func queryFocusDecisionsTx(tx *sql.Tx, focusID, runID string, limit int) ([]focusResultProjection, error) {
	rows, err := tx.Query(`WITH current_revisions AS (
SELECT r.* FROM focus_decision_revisions r
JOIN (
  SELECT owner_id,profile_id,focus_id,decision_id,MAX(revision_no) AS revision_no
  FROM focus_decision_revisions
  WHERE owner_id=? AND profile_id=? AND status='confirmed'
  GROUP BY owner_id,profile_id,focus_id,decision_id
) current ON current.owner_id=r.owner_id AND current.profile_id=r.profile_id
  AND current.focus_id=r.focus_id AND current.decision_id=r.decision_id
  AND current.revision_no=r.revision_no
)
SELECT d.id,d.question,d.options,COALESCE(cr.content,d.recommendation),
CASE WHEN cr.id IS NULL THEN d.expected_outcome ELSE cr.expected_outcome END,
CASE WHEN cr.id IS NULL THEN d.review_at ELSE cr.review_at END,
d.source_ids,d.assumptions,d.risks,d.supporting_evidence,d.counterevidence,d.confidence,d.freshness,d.validation_items,d.status,d.analysis_run_id,d.result_rank,d.prompt_version,d.evidence_version,COALESCE(f.revision,1),COALESCE(f.status,'draft'),COALESCE(cr.id,''),COALESCE(cr.revision_no,0)
FROM decisions d
LEFT JOIN focus_result_feedback f ON f.owner_id=? AND f.profile_id=? AND f.focus_id=d.focus_id AND f.result_kind='decision' AND f.result_id=d.id
LEFT JOIN current_revisions cr ON cr.owner_id=? AND cr.profile_id=? AND cr.focus_id=d.focus_id AND cr.decision_id=d.id
WHERE d.focus_id=? AND d.analysis_run_id=?
ORDER BY d.result_rank ASC,d.id ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, focusID, runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultProjection, 0, limit)
	for rows.Next() {
		var item Decision
		var options, expectedOutcome, reviewAt, sourceJSON, assumptions, risks, supportingJSON, counterJSON, confidence, freshness, validationJSON, runID, prompt, evidence, feedback, decisionRevisionID string
		var rank, revision, decisionRevisionNo int
		if err := rows.Scan(&item.ID, &item.Question, &options, &item.Recommendation, &expectedOutcome, &reviewAt, &sourceJSON, &assumptions, &risks, &supportingJSON, &counterJSON, &confidence, &freshness, &validationJSON, &item.Status, &runID, &rank, &prompt, &evidence, &revision, &feedback, &decisionRevisionID, &decisionRevisionNo); err != nil {
			return nil, err
		}
		sourceIDs, supporting, counter, validation, err := decodeFocusResultLists(sourceJSON, supportingJSON, counterJSON, validationJSON)
		if err != nil || json.Unmarshal([]byte(options), &item.Options) != nil || json.Unmarshal([]byte(assumptions), &item.Assumptions) != nil || json.Unmarshal([]byte(risks), &item.Risks) != nil {
			return nil, errors.New("焦点决策投影损坏")
		}
		item.SourceIDs = sourceIDs
		citations, citationErr := readFocusResultCitationsTx(tx, focusID, runID, "decision", item.ID)
		if citationErr != nil {
			return nil, citationErr
		}
		data := focusResultData(item, supporting, counter, confidence, freshness, validation)
		if expectedOutcome != "" {
			data["expected_outcome"] = expectedOutcome
		}
		if reviewAt != "" {
			data["review_at"] = reviewAt
		}
		if decisionRevisionID != "" {
			data["decision_revision_id"] = decisionRevisionID
			data["decision_revision_no"] = decisionRevisionNo
		}
		if len(citations) > 0 {
			data["citations"] = citations
		}
		result = append(result, focusResultProjection{Kind: "decision", ID: item.ID, FocusID: focusID, AnalysisRunID: runID, Rank: rank, PromptVersion: prompt, EvidenceVersion: evidence, Status: item.Status, Revision: revision, FeedbackStatus: feedback, SourceIDs: sourceIDs, Citations: citations, SupportingEvidence: supporting, Counterevidence: counter, Confidence: confidence, Freshness: freshness, ValidationItems: validation, Data: data})
	}
	return result, rows.Err()
}

func decodeFocusResultLists(raw ...string) ([]string, []string, []string, []string, error) {
	if len(raw) != 4 {
		return nil, nil, nil, nil, errors.New("焦点结果列表数量不合法")
	}
	values := make([][]string, 4)
	for index := range raw {
		if err := json.Unmarshal([]byte(raw[index]), &values[index]); err != nil {
			return nil, nil, nil, nil, errors.New("焦点结果投影损坏")
		}
		if values[index] == nil {
			values[index] = []string{}
		}
	}
	return values[0], values[1], values[2], values[3], nil
}

func focusResultData(value any, supporting, counter []string, confidence, freshness string, validation []string) map[string]any {
	raw, _ := json.Marshal(value)
	result := make(map[string]any)
	_ = json.Unmarshal(raw, &result)
	result["supporting_evidence"] = supporting
	result["counterevidence"] = counter
	result["confidence"] = confidence
	result["freshness"] = freshness
	result["validation_items"] = validation
	return result
}

type focusInterpretationEvidenceRow struct {
	ID             string
	SourceID       string
	SourceRevision string
}

func focusInterpretationEvidenceRowsTx(tx *sql.Tx, runID string) ([]focusInterpretationEvidenceRow, error) {
	rows, err := tx.Query(`SELECT evidence_id,source_id,source_revision
FROM focus_analysis_evidence
WHERE run_id=? AND owner_id=? AND profile_id=?
ORDER BY rank ASC,evidence_id ASC LIMIT ?`, runID, appDataScope.OwnerID, appDataScope.ProfileID, focusAnalysisLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusInterpretationEvidenceRow, 0, focusAnalysisLimit)
	for rows.Next() {
		var item focusInterpretationEvidenceRow
		if err := rows.Scan(&item.ID, &item.SourceID, &item.SourceRevision); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusInterpretationSourceMatches(sourceIDs []string, evidence focusInterpretationEvidenceRow) bool {
	for _, sourceID := range sourceIDs {
		if sourceID == evidence.ID || sourceID == evidence.SourceID {
			return true
		}
	}
	return false
}

func focusInterpretationResult(kind, id, title, detail string, sourceIDs, supporting, counter, validation []string, confidence, freshness string) map[string]any {
	return map[string]any{
		"kind":                kind,
		"id":                  id,
		"title":               title,
		"detail":              detail,
		"source_ids":          sourceIDs,
		"supporting_evidence": supporting,
		"counterevidence":     counter,
		"confidence":          confidence,
		"freshness":           freshness,
		"validation_items":    validation,
	}
}

func focusInterpretationPayload(run focusRunSummary, request focusAnalysisApplyRequest, evidence focusInterpretationEvidenceRow) []byte {
	results := make([]any, 0, len(request.Signals)+len(request.Actions)+len(request.Decisions)+len(request.WorkProducts))
	for _, item := range request.Signals {
		if focusInterpretationSourceMatches(item.SourceIDs, evidence) {
			result := focusInterpretationResult("signal", item.ID, item.Title, item.Detail, item.SourceIDs, item.SupportingEvidence, item.Counterevidence, item.ValidationItems, item.Confidence, item.Freshness)
			result["citations"] = focusCitationInputList(item.Citations, item.SupportingCitations, item.CounterCitations)
			results = append(results, result)
		}
	}
	for _, item := range request.Actions {
		if focusInterpretationSourceMatches(item.SourceIDs, evidence) {
			result := focusInterpretationResult("action", item.ID, item.Task, "", item.SourceIDs, item.SupportingEvidence, item.Counterevidence, item.ValidationItems, item.Confidence, item.Freshness)
			result["owner"] = item.Owner
			result["priority"] = item.Priority
			result["due_at"] = item.DueAt
			result["citations"] = focusCitationInputList(item.Citations, item.SupportingCitations, item.CounterCitations)
			results = append(results, result)
		}
	}
	for _, item := range request.Decisions {
		if focusInterpretationSourceMatches(item.SourceIDs, evidence) {
			result := focusInterpretationResult("decision", item.ID, item.Question, item.Recommendation, item.SourceIDs, item.SupportingEvidence, item.Counterevidence, item.ValidationItems, item.Confidence, item.Freshness)
			result["options"] = item.Options
			result["assumptions"] = item.Assumptions
			result["risks"] = item.Risks
			result["citations"] = focusCitationInputList(item.Citations, item.SupportingCitations, item.CounterCitations)
			results = append(results, result)
		}
	}
	for _, item := range request.WorkProducts {
		if focusInterpretationSourceMatches(item.SourceIDs, evidence) {
			results = append(results, map[string]any{
				"kind": "work_product", "id": item.ID, "type": item.Type, "title": item.Title,
				"summary": item.Summary, "source_ids": item.SourceIDs, "validation_items": item.ValidationItems,
			})
		}
	}
	payload := map[string]any{
		"run_id":            run.ID,
		"status":            request.Status,
		"scope":             run.Scope,
		"focus_id":          run.FocusID,
		"focus_revision":    run.FocusRevision,
		"evidence_id":       evidence.ID,
		"evidence_revision": evidence.SourceRevision,
		"evidence_set_hash": run.EvidenceSetHash,
		"prompt_version":    run.PromptVersion,
		"analysis_profile":  run.AnalysisProfile,
		"results":           results,
	}
	raw, _ := json.Marshal(payload)
	if len(raw) <= focusInterpretationMaxBytes {
		return raw
	}
	// Keep the cache bounded even if a model returns unusually large lists.
	ids := make([]string, 0, len(results))
	for _, rawResult := range results {
		if result, ok := rawResult.(map[string]any); ok {
			if id, ok := result["id"].(string); ok {
				ids = append(ids, id)
			}
		}
	}
	compact := map[string]any{
		"run_id": run.ID, "status": request.Status, "scope": run.Scope,
		"focus_id": run.FocusID, "focus_revision": run.FocusRevision,
		"evidence_id": evidence.ID, "evidence_revision": evidence.SourceRevision,
		"evidence_set_hash": run.EvidenceSetHash, "prompt_version": run.PromptVersion,
		"analysis_profile": run.AnalysisProfile, "result_ids": ids,
	}
	raw, _ = json.Marshal(compact)
	if len(raw) <= focusInterpretationMaxBytes {
		return raw
	}
	minimal := map[string]any{"run_id": run.ID, "status": request.Status, "evidence_id": evidence.ID, "evidence_set_hash": run.EvidenceSetHash}
	raw, _ = json.Marshal(minimal)
	return raw
}

func pruneFocusInterpretationsTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM focus_evidence_interpretations
WHERE owner_id=? AND profile_id=? AND julianday(updated_at)<julianday('now',?)`, appDataScope.OwnerID, appDataScope.ProfileID, fmt.Sprintf("-%d days", focusInterpretationRetentionDays)); err != nil {
		if readerFocusSchemaMissing(err) {
			return nil
		}
		return err
	}
	_, err := tx.Exec(`DELETE FROM focus_evidence_interpretations
WHERE owner_id=? AND profile_id=? AND rowid IN (
  SELECT rowid FROM focus_evidence_interpretations
  WHERE owner_id=? AND profile_id=? ORDER BY updated_at DESC,rowid DESC LIMIT -1 OFFSET ?
)`, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, focusInterpretationRetentionLimit-1)
	if readerFocusSchemaMissing(err) {
		return nil
	}
	return err
}

func writeFocusEvidenceInterpretationsTx(tx *sql.Tx, run focusRunSummary, request focusAnalysisApplyRequest, now string) error {
	evidenceRows, err := focusInterpretationEvidenceRowsTx(tx, run.ID)
	if err != nil {
		if readerFocusSchemaMissing(err) {
			return nil
		}
		return err
	}
	for _, evidence := range evidenceRows {
		payload := focusInterpretationPayload(run, request, evidence)
		if len(payload) > focusInterpretationMaxBytes {
			return errors.New("焦点解释缓存过大")
		}
		_, err := tx.Exec(`INSERT INTO focus_evidence_interpretations(owner_id,profile_id,focus_id,focus_revision,evidence_id,evidence_revision,evidence_set_hash,prompt_version,analysis_profile,run_id,interpretation_json,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(owner_id,profile_id,focus_id,focus_revision,evidence_id,evidence_revision,prompt_version,analysis_profile)
DO UPDATE SET evidence_set_hash=excluded.evidence_set_hash,run_id=excluded.run_id,interpretation_json=excluded.interpretation_json,updated_at=excluded.updated_at`,
			appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.FocusRevision, evidence.ID, evidence.SourceRevision, run.EvidenceSetHash, run.PromptVersion, run.AnalysisProfile, run.ID, string(payload), now, now)
		if err != nil {
			if readerFocusSchemaMissing(err) {
				return nil
			}
			return err
		}
	}
	return pruneFocusInterpretationsTx(tx)
}

func readFocusEvidenceInterpretationsTx(tx *sql.Tx, run focusRunSummary, limit int) ([]focusEvidenceInterpretation, error) {
	rows, err := tx.Query(`SELECT focus_id,focus_revision,evidence_id,evidence_revision,evidence_set_hash,prompt_version,analysis_profile,run_id,interpretation_json,updated_at
FROM focus_evidence_interpretations
WHERE owner_id=? AND profile_id=? AND focus_id=? AND focus_revision=? AND evidence_set_hash=? AND prompt_version=? AND analysis_profile=?
ORDER BY updated_at DESC,evidence_id ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.FocusRevision, run.EvidenceSetHash, run.PromptVersion, run.AnalysisProfile, minInt(limit, focusAnalysisLimit))
	if err != nil {
		if readerFocusSchemaMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := make([]focusEvidenceInterpretation, 0, minInt(limit, focusAnalysisLimit))
	for rows.Next() {
		var item focusEvidenceInterpretation
		var raw string
		if err := rows.Scan(&item.FocusID, &item.FocusRevision, &item.EvidenceID, &item.EvidenceRevision, &item.EvidenceSetHash, &item.PromptVersion, &item.AnalysisProfile, &item.RunID, &raw, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if len(raw) > focusInterpretationMaxBytes || json.Unmarshal([]byte(raw), &item.Interpretation) != nil {
			return nil, errors.New("焦点解释缓存损坏")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusResultFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusResultFeedbackRequest
	if err := app.DecodeStrictJSON(w, r, 16<<10, &request); err != nil || request.SchemaVersion != focusSpaceSchemaVersion {
		app.UserErr(w, http.StatusBadRequest, "焦点反馈格式不合法")
		return
	}
	if !validFocusID(request.FocusID) || !validFocusID(request.ResultID) || request.ExpectedRevision < 1 || (request.ResultKind != "signal" && request.ResultKind != "action" && request.ResultKind != "decision") || (request.Decision != "accepted" && request.Decision != "rejected" && request.Decision != "snoozed" && request.Decision != "edited" && request.Decision != "draft") {
		app.UserErr(w, http.StatusBadRequest, "焦点反馈字段不合法")
		return
	}
	if err := validateFocusResultPatch(request); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := focusSpaceByIDTx(tx, request.FocusID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "焦点空间不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if !focusResultExistsTx(tx, request.FocusID, request.ResultKind, request.ResultID) {
		app.UserErr(w, http.StatusNotFound, "焦点结果不存在")
		return
	}
	if request.Decision == "draft" && request.ResultKind != "decision" {
		app.UserErr(w, http.StatusBadRequest, "只有决策可以撤销为待确认")
		return
	}
	if request.ResultKind == "decision" && request.Decision != "draft" {
		var revisionID string
		err := tx.QueryRow(`SELECT id FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=? AND status='confirmed' LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultID).Scan(&revisionID)
		if err == nil {
			app.UserErr(w, http.StatusConflict, "已确认决策不能反向修改，请先撤销接受或通过补充说明、纠错、复盘形成新版本")
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			app.HTTPErr(w, err)
			return
		}
	}
	var decisionSnapshot decisionResultSnapshot
	if request.ResultKind == "decision" {
		decisionSnapshot, err = readDecisionResultTx(tx, request.FocusID, request.ResultID)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	var currentRevision int
	var currentStatus string
	err = tx.QueryRow(`SELECT revision,status FROM focus_result_feedback WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultKind, request.ResultID).Scan(&currentRevision, &currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		if request.ExpectedRevision != 1 {
			app.UserErr(w, http.StatusConflict, "焦点结果已变化，请重新读取")
			return
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO focus_result_feedback(owner_id,profile_id,focus_id,result_kind,result_id,revision,status,patch_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultKind, request.ResultID, 1, "draft", "{}", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := tx.QueryRow(`SELECT revision,status FROM focus_result_feedback WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultKind, request.ResultID).Scan(&currentRevision, &currentStatus); err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if currentRevision != request.ExpectedRevision {
		app.UserErr(w, http.StatusConflict, "焦点结果已变化，请重新读取")
		return
	}
	newRevision := currentRevision + 1
	patchJSON := "{}"
	if request.Patch != nil {
		raw, _ := json.Marshal(request.Patch)
		patchJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE focus_result_feedback SET revision=?,status=?,patch_json=?,updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind=? AND result_id=? AND revision=?`, newRevision, request.Decision, patchJSON, now, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultKind, request.ResultID, request.ExpectedRevision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, err := result.RowsAffected(); err != nil {
		app.HTTPErr(w, err)
		return
	} else if changed != 1 {
		app.UserErr(w, http.StatusConflict, "焦点结果已变化，请重新读取")
		return
	}
	if request.Patch != nil {
		if err := applyFocusResultPatchTx(tx, request); err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if request.ResultKind == "decision" && request.Decision == "accepted" {
		reason, checkErr := decisionRunClarificationReasonTx(tx, "", request.FocusID, decisionSnapshot.AnalysisRunID, decisionSnapshot.ID)
		if checkErr != nil {
			app.HTTPErr(w, checkErr)
			return
		}
		if reason != "" {
			app.UserErr(w, http.StatusConflict, reason)
			return
		}
	}
	if request.Decision == "accepted" || request.Decision == "rejected" || request.Decision == "draft" {
		if err := setFocusResultStatusTx(tx, request.FocusID, request.ResultKind, request.ResultID, request.Decision); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if request.Decision == "edited" && request.ResultKind == "decision" {
		if err := setFocusResultStatusTx(tx, request.FocusID, request.ResultKind, request.ResultID, "draft"); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if request.ResultKind == "decision" {
		if request.Decision == "draft" {
			if _, err := tx.Exec(`UPDATE focus_decision_revisions SET status='superseded',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=? AND status='confirmed'`, now, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.ResultID); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
		actionStatus := request.Decision
		if request.Decision == "edited" {
			actionStatus = "draft"
		}
		if actionStatus == "accepted" || actionStatus == "rejected" || actionStatus == "draft" {
			if err := setDecisionRelatedActionsStatusTx(tx, request.FocusID, decisionSnapshot, actionStatus); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, focusResultFeedbackResponse{FocusID: request.FocusID, ResultKind: request.ResultKind, ResultID: request.ResultID, Revision: newRevision, Status: request.Decision})
}

func focusResultExistsTx(tx *sql.Tx, focusID, kind, id string) bool {
	var found string
	var err error
	switch kind {
	case "signal":
		err = tx.QueryRow(`SELECT id FROM signals WHERE id=? AND focus_id=? AND analysis_run_id<>''`, id, focusID).Scan(&found)
	case "action":
		err = tx.QueryRow(`SELECT id FROM actions WHERE id=? AND focus_id=? AND analysis_run_id<>''`, id, focusID).Scan(&found)
	case "decision":
		err = tx.QueryRow(`SELECT id FROM decisions WHERE id=? AND focus_id=? AND analysis_run_id<>''`, id, focusID).Scan(&found)
	}
	return err == nil && found == id
}

func validateFocusResultPatch(request focusResultFeedbackRequest) error {
	patch := request.Patch
	if request.Decision == "edited" && patch == nil {
		return errors.New("编辑反馈缺少 patch")
	}
	if patch == nil {
		return nil
	}
	if request.Decision != "edited" {
		return errors.New("只有编辑反馈可以携带 patch")
	}
	fields := 0
	switch request.ResultKind {
	case "action":
		if patch.Completed != nil {
			fields++
		}
		if patch.Task != nil {
			fields++
			if !validText(*patch.Task, 1000, true) {
				return errors.New("焦点反馈 patch 不合法")
			}
		}
		if patch.Owner != nil {
			fields++
			if !validText(*patch.Owner, 120, true) {
				return errors.New("焦点反馈 patch 不合法")
			}
		}
		if patch.DueAt != nil {
			fields++
			if !validText(*patch.DueAt, 80, false) {
				return errors.New("焦点反馈 patch 不合法")
			}
		}
		if patch.Priority != nil {
			fields++
			if !validActionPriority(*patch.Priority) {
				return errors.New("焦点反馈 patch 不合法")
			}
		}
		if patch.Detail != nil || patch.Question != nil {
			return errors.New("焦点反馈 patch 与结果类型不匹配")
		}
	case "signal":
		if patch.Detail == nil || !validText(*patch.Detail, 2000, false) {
			return errors.New("Signal 编辑必须提供 detail")
		}
		fields = 1
		if patch.Task != nil || patch.Owner != nil || patch.DueAt != nil || patch.Priority != nil || patch.Completed != nil || patch.Question != nil {
			return errors.New("焦点反馈 patch 与结果类型不匹配")
		}
	case "decision":
		if patch.Question == nil || !validText(*patch.Question, 500, true) {
			return errors.New("Decision 编辑必须提供 question")
		}
		fields = 1
		if patch.Task != nil || patch.Owner != nil || patch.DueAt != nil || patch.Priority != nil || patch.Completed != nil || patch.Detail != nil {
			return errors.New("焦点反馈 patch 与结果类型不匹配")
		}
	default:
		return errors.New("结果类型不合法")
	}
	if fields == 0 {
		return errors.New("编辑反馈缺少可修改字段")
	}
	return nil
}

func applyFocusResultPatchTx(tx *sql.Tx, request focusResultFeedbackRequest) error {
	patch := request.Patch
	if patch == nil {
		return nil
	}
	switch request.ResultKind {
	case "action":
		current := []any{}
		if patch.Task != nil {
			current = append(current, "task", *patch.Task)
		}
		if patch.Owner != nil {
			current = append(current, "owner", *patch.Owner)
		}
		if patch.DueAt != nil {
			current = append(current, "due_at", *patch.DueAt)
		}
		if patch.Priority != nil {
			current = append(current, "priority", *patch.Priority)
		}
		if patch.Completed != nil {
			status := "accepted"
			if *patch.Completed {
				status = "done"
			}
			current = append(current, "status", status, "confirmation_status", "accepted")
		}
		for index := 0; index+1 < len(current); index += 2 {
			if _, err := tx.Exec(`UPDATE actions SET `+current[index].(string)+`=? WHERE id=? AND focus_id=?`, current[index+1], request.ResultID, request.FocusID); err != nil {
				return err
			}
		}
	case "signal":
		if patch.Detail != nil {
			if _, err := tx.Exec(`UPDATE signals SET detail=? WHERE id=? AND focus_id=?`, *patch.Detail, request.ResultID, request.FocusID); err != nil {
				return err
			}
		}
	case "decision":
		if patch.Question != nil {
			if _, err := tx.Exec(`UPDATE decisions SET question=? WHERE id=? AND focus_id=?`, *patch.Question, request.ResultID, request.FocusID); err != nil {
				return err
			}
		}
	}
	return nil
}

func setFocusResultStatusTx(tx *sql.Tx, focusID, kind, id, status string) error {
	var result sql.Result
	var err error
	switch kind {
	case "action":
		result, err = tx.Exec(`UPDATE actions SET status=?,confirmation_status=? WHERE id=? AND focus_id=? AND analysis_run_id<>''`, status, status, id, focusID)
	case "signal":
		result, err = tx.Exec(`UPDATE signals SET status=? WHERE id=? AND focus_id=? AND analysis_run_id<>''`, status, id, focusID)
	case "decision":
		result, err = tx.Exec(`UPDATE decisions SET status=? WHERE id=? AND focus_id=? AND analysis_run_id<>''`, status, id, focusID)
	default:
		return errors.New("结果类型不合法")
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("焦点结果状态更新冲突")
	}
	return nil
}

package main

// Focus Decision Runtime persists the small amount of control-plane state
// needed to continue a decision after a page reload, process restart or model
// interruption. It does not call a model and it does not duplicate Focus,
// Decision, Evidence or Action persistence.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const (
	decisionRunSchemaVersion       = 2
	decisionRunLimitPerFocus       = 16
	decisionRunQuestionLimit       = 12
	decisionRunOptionLimit         = 6
	decisionRunAnswerRevisionLimit = 20
	decisionRunRequestBytes        = 48 << 10
)

var decisionRunStatuses = []string{
	"queued", "collecting_context", "running_ai", "waiting_clarification",
	"applying", "completed", "paused_insufficient_credits", "failed_retryable",
	"failed_terminal", "cancelled", "superseded",
}

type decisionRunRow struct {
	ID                      string
	FocusID                 string
	FocusRevision           int
	AnalysisRunID           string
	DecisionID              string
	DecisionMode            string
	IntentKind              string
	BlueprintID             string
	BlueprintVersion        string
	ThinkingTemplateID      string
	ThinkingTemplateVersion string
	Status                  string
	Revision                int
	Attempt                 int
	QuestionCount           int
	CurrentQuestionID       string
	InputTokens             int
	OutputTokens            int
	ErrorCode               string
	ErrorMessage            string
	StartedAt               string
	CompletedAt             string
	CreatedAt               string
	UpdatedAt               string
}

type decisionRunPublic struct {
	ID                      string `json:"id"`
	FocusID                 string `json:"focus_id"`
	FocusRevision           int    `json:"focus_revision"`
	AnalysisRunID           string `json:"analysis_run_id,omitempty"`
	DecisionID              string `json:"decision_id,omitempty"`
	DecisionMode            string `json:"decision_mode"`
	IntentKind              string `json:"intent_kind"`
	BlueprintID             string `json:"blueprint_id"`
	BlueprintVersion        string `json:"blueprint_version"`
	ThinkingTemplateID      string `json:"thinking_template_id"`
	ThinkingTemplateVersion string `json:"thinking_template_version"`
	Status                  string `json:"status"`
	Revision                int    `json:"revision"`
	Attempt                 int    `json:"attempt"`
	QuestionCount           int    `json:"question_count"`
	CurrentQuestionID       string `json:"current_question_id,omitempty"`
	InputTokens             int    `json:"input_tokens"`
	OutputTokens            int    `json:"output_tokens"`
	ErrorCode               string `json:"error_code,omitempty"`
	ErrorMessage            string `json:"error_message,omitempty"`
	StartedAt               string `json:"started_at,omitempty"`
	CompletedAt             string `json:"completed_at,omitempty"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

type decisionQuestionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type decisionQuestionInput struct {
	ID        string                   `json:"id,omitempty"`
	Type      string                   `json:"type"`
	Question  string                   `json:"question"`
	Options   []decisionQuestionOption `json:"options,omitempty"`
	Required  *bool                    `json:"required,omitempty"`
	MaxSelect int                      `json:"max_select,omitempty"`
	ScaleMin  int                      `json:"scale_min,omitempty"`
	ScaleMax  int                      `json:"scale_max,omitempty"`
}

// UnmarshalJSON accepts the compact string option form emitted by older
// Hermes prompts and normalizes it to the application's canonical option
// objects. This keeps the resume contract backward compatible while the
// persisted representation remains typed and stable.
func (q *decisionQuestionInput) UnmarshalJSON(data []byte) error {
	type alias decisionQuestionInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for key := range fields {
		switch key {
		case "id", "type", "question", "options", "required", "max_select", "scale_min", "scale_max":
		default:
			return fmt.Errorf("json: unknown field %q", key)
		}
	}
	var raw struct {
		ID        string          `json:"id,omitempty"`
		Type      string          `json:"type"`
		Question  string          `json:"question"`
		Options   json.RawMessage `json:"options,omitempty"`
		Required  *bool           `json:"required,omitempty"`
		MaxSelect int             `json:"max_select,omitempty"`
		ScaleMin  int             `json:"scale_min,omitempty"`
		ScaleMax  int             `json:"scale_max,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	q.ID, q.Type, q.Question = raw.ID, raw.Type, raw.Question
	switch q.Type {
	case "single_select":
		q.Type = "choice"
	case "multi_select":
		q.Type = "multi"
	}
	q.Required, q.MaxSelect, q.ScaleMin, q.ScaleMax = raw.Required, raw.MaxSelect, raw.ScaleMin, raw.ScaleMax
	if len(raw.Options) == 0 || string(raw.Options) == "null" {
		q.Options = nil
		return nil
	}
	if err := json.Unmarshal(raw.Options, &q.Options); err == nil {
		return nil
	}
	var labels []string
	if err := json.Unmarshal(raw.Options, &labels); err != nil {
		return fmt.Errorf("options must be option objects or strings: %w", err)
	}
	q.Options = make([]decisionQuestionOption, len(labels))
	for i, label := range labels {
		q.Options[i] = decisionQuestionOption{ID: fmt.Sprintf("option-%d", i+1), Label: label}
	}
	return nil
}

type decisionAnswerPublic struct {
	Type       string `json:"type"`
	Value      any    `json:"value"`
	Revision   int    `json:"revision"`
	AnsweredAt string `json:"answered_at"`
}

type decisionQuestionPublic struct {
	ID             string                   `json:"id"`
	Sequence       int                      `json:"sequence"`
	Type           string                   `json:"type"`
	Question       string                   `json:"question"`
	Options        []decisionQuestionOption `json:"options"`
	Required       bool                     `json:"required"`
	MaxSelect      int                      `json:"max_select,omitempty"`
	ScaleMin       int                      `json:"scale_min,omitempty"`
	ScaleMax       int                      `json:"scale_max,omitempty"`
	Origin         string                   `json:"origin"`
	Status         string                   `json:"status"`
	Revision       int                      `json:"revision"`
	AnswerRevision int                      `json:"answer_revision"`
	Answer         *decisionAnswerPublic    `json:"answer,omitempty"`
}

type decisionRunProgress struct {
	Answered int `json:"answered"`
	Total    int `json:"total"`
	Percent  int `json:"percent"`
}

type decisionRunResponse struct {
	SchemaVersion      int                      `json:"schema_version"`
	Status             string                   `json:"status"`
	Run                *decisionRunPublic       `json:"run"`
	CurrentQuestion    *decisionQuestionPublic  `json:"current_question,omitempty"`
	Questions          []decisionQuestionPublic `json:"questions"`
	Progress           decisionRunProgress      `json:"progress"`
	AllowedTransitions []string                 `json:"allowed_transitions"`
	CanStart           bool                     `json:"can_start"`
	CanResume          bool                     `json:"can_resume"`
	CanCancel          bool                     `json:"can_cancel"`
}

type decisionRunStartRequest struct {
	SchemaVersion         int                     `json:"schema_version"`
	FocusID               string                  `json:"focus_id"`
	ExpectedFocusRevision int                     `json:"expected_focus_revision"`
	DecisionMode          string                  `json:"decision_mode"`
	AnalysisRunID         string                  `json:"analysis_run_id,omitempty"`
	DecisionID            string                  `json:"decision_id,omitempty"`
	SkipClarification     bool                    `json:"skip_clarification,omitempty"`
	ReplaceActive         bool                    `json:"replace_active,omitempty"`
	Questions             []decisionQuestionInput `json:"questions,omitempty"`
}

type decisionRunUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type decisionRunResumeRequest struct {
	SchemaVersion    int                    `json:"schema_version"`
	RunID            string                 `json:"run_id"`
	ExpectedRevision int                    `json:"expected_revision"`
	Status           string                 `json:"status,omitempty"`
	Question         *decisionQuestionInput `json:"question,omitempty"`
	AnalysisRunID    string                 `json:"analysis_run_id,omitempty"`
	DecisionID       string                 `json:"decision_id,omitempty"`
	ErrorCode        string                 `json:"error_code,omitempty"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	Usage            *decisionRunUsage      `json:"usage,omitempty"`
}

type decisionAnswerInput struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

type decisionRunAnswerRequest struct {
	SchemaVersion          int                 `json:"schema_version"`
	RunID                  string              `json:"run_id"`
	QuestionID             string              `json:"question_id"`
	ExpectedRunRevision    int                 `json:"expected_run_revision"`
	ExpectedAnswerRevision int                 `json:"expected_answer_revision"`
	Answer                 decisionAnswerInput `json:"answer"`
}

type decisionRunCancelRequest struct {
	SchemaVersion    int    `json:"schema_version"`
	RunID            string `json:"run_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Reason           string `json:"reason,omitempty"`
}

func decisionRunQueryKeys() map[string]struct{} {
	return map[string]struct{}{"run_id": {}, "focus_id": {}}
}

func validDecisionRunStatus(value string) bool {
	for _, status := range decisionRunStatuses {
		if value == status {
			return true
		}
	}
	return false
}

func validDecisionMode(value string) bool {
	return value == "rational" || value == "bounded_rationality" || value == "first_principles" || value == "guided"
}

func decisionRunTerminal(status string) bool {
	return status == "completed" || status == "failed_terminal" || status == "cancelled" || status == "superseded"
}

func decisionRunResumable(status string) bool {
	return status == "paused_insufficient_credits" || status == "failed_retryable"
}

func decisionRunCanTransition(from, to string) bool {
	if from == to {
		return !decisionRunTerminal(from)
	}
	allowed := map[string]map[string]struct{}{
		"queued":                      {"collecting_context": {}, "waiting_clarification": {}, "cancelled": {}, "failed_retryable": {}, "failed_terminal": {}},
		"collecting_context":          {"running_ai": {}, "waiting_clarification": {}, "paused_insufficient_credits": {}, "failed_retryable": {}, "failed_terminal": {}, "cancelled": {}},
		"running_ai":                  {"waiting_clarification": {}, "applying": {}, "paused_insufficient_credits": {}, "failed_retryable": {}, "failed_terminal": {}, "cancelled": {}},
		"waiting_clarification":       {"failed_retryable": {}, "failed_terminal": {}, "cancelled": {}},
		"applying":                    {"completed": {}, "paused_insufficient_credits": {}, "failed_retryable": {}, "failed_terminal": {}, "cancelled": {}},
		"completed":                   {"superseded": {}},
		"paused_insufficient_credits": {"queued": {}, "collecting_context": {}, "waiting_clarification": {}, "cancelled": {}},
		"failed_retryable":            {"queued": {}, "collecting_context": {}, "waiting_clarification": {}, "cancelled": {}, "failed_terminal": {}},
		"failed_terminal":             {"superseded": {}},
		"cancelled":                   {},
		"superseded":                  {},
	}
	_, ok := allowed[from][to]
	return ok
}

func decisionRunAllowedTransitions(status string) []string {
	result := make([]string, 0, len(decisionRunStatuses))
	for _, target := range decisionRunStatuses {
		if target != status && decisionRunCanTransition(status, target) {
			result = append(result, target)
		}
	}
	return result
}

func decisionRunStartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		decisionRunError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法", 0)
		return
	}
	var request decisionRunStartRequest
	key, ok := decodeDecisionRunMutation(w, r, &request)
	if !ok {
		return
	}
	if request.SchemaVersion != decisionRunSchemaVersion || !validFocusID(request.FocusID) || request.ExpectedFocusRevision < 1 || !validDecisionMode(request.DecisionMode) ||
		!validFocusIDOrEmpty(request.AnalysisRunID) || !validFocusIDOrEmpty(request.DecisionID) || len(request.Questions) > decisionRunQuestionLimit || request.SkipClarification && len(request.Questions) > 0 {
		decisionRunError(w, http.StatusBadRequest, "invalid_request", "决策运行参数不合法", 0)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	space, err := focusSpaceByIDTx(tx, request.FocusID)
	if errors.Is(err, sql.ErrNoRows) {
		decisionRunError(w, http.StatusNotFound, "focus_not_found", "焦点空间不存在", 0)
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if space.Revision != request.ExpectedFocusRevision {
		decisionRunError(w, http.StatusConflict, "focus_revision_conflict", "焦点已变化，请重新读取", space.Revision)
		return
	}
	if space.Status != "active" {
		decisionRunError(w, http.StatusConflict, "focus_not_active", "焦点空间未启用", space.Revision)
		return
	}
	binding, err := readFocusWorkspaceBindingTx(tx, space)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	blueprintResponse, err := resolveFocusBlueprintTx(tx, "focus", space.ID, binding.BlueprintID, nil, false)
	if err != nil {
		decisionRunError(w, http.StatusConflict, "blueprint_invalid", err.Error(), space.Revision)
		return
	}
	if !focusBlueprintContains(blueprintResponse.Effective.AllowedThinkingTemplates, request.DecisionMode) {
		decisionRunError(w, http.StatusBadRequest, "thinking_template_not_allowed", "当前任务模式不允许该思考模板", space.Revision)
		return
	}
	if binding.ThinkingTemplateID != request.DecisionMode || binding.BlueprintVersion != blueprintResponse.BaseVersion {
		desiredBinding := binding
		desiredBinding.BlueprintVersion = blueprintResponse.BaseVersion
		desiredBinding.ThinkingTemplateID = request.DecisionMode
		desiredBinding.ThinkingTemplateVersion = "builtin-v1"
		binding, _, err = persistFocusWorkspaceBindingTx(tx, binding, desiredBinding, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := validateDecisionRunReferencesTx(tx, request.FocusID, request.AnalysisRunID, request.DecisionID); err != nil {
		decisionRunError(w, http.StatusBadRequest, "reference_invalid", err.Error(), space.Revision)
		return
	}
	if active, found, lookupErr := activeDecisionRunTx(tx, request.FocusID); lookupErr != nil {
		app.HTTPErr(w, lookupErr)
		return
	} else if found && !request.ReplaceActive {
		decisionRunError(w, http.StatusConflict, "active_run_exists", "当前焦点已有进行中的决策", active.Revision)
		return
	} else if found {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`UPDATE focus_decision_runs SET status='superseded',revision=revision+1,current_question_id='',completed_at=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`, now, now, active.ID, appDataScope.OwnerID, appDataScope.ProfileID, active.Revision); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := pruneUnreferencedTerminalDecisionRunsTx(tx, request.FocusID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	var runCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_runs WHERE owner_id=? AND profile_id=? AND focus_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID).Scan(&runCount); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if runCount >= decisionRunLimitPerFocus {
		decisionRunError(w, http.StatusConflict, "run_limit_reached", "当前焦点的决策运行已达到上限", space.Revision)
		return
	}
	questions := request.Questions
	origin := "runtime"
	if len(questions) == 0 && !request.SkipClarification {
		questions = builtinDecisionQuestions(request.DecisionMode, space)
		origin = "builtin"
	}
	questions, err = normalizeDecisionQuestions(questions)
	if err != nil {
		decisionRunError(w, http.StatusBadRequest, "invalid_questions", err.Error(), 0)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status := "queued"
	currentQuestionID := ""
	if len(questions) > 0 {
		status = "waiting_clarification"
		currentQuestionID = questions[0].ID
	}
	runID := stableObjectID("decision-run", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.FocusID+"\x00"+key)
	_, err = tx.Exec(`INSERT INTO focus_decision_runs(id,owner_id,profile_id,focus_id,focus_revision,analysis_run_id,decision_id,decision_mode,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,status,revision,attempt,question_count,current_question_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,1,?,?,?,?)`,
		runID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, space.Revision, request.AnalysisRunID, request.DecisionID, request.DecisionMode, binding.IntentKind, binding.BlueprintID, binding.BlueprintVersion, binding.ThinkingTemplateID, binding.ThinkingTemplateVersion, status, len(questions), currentQuestionID, now, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	for index, question := range questions {
		options, _ := json.Marshal(question.Options)
		required := true
		if question.Required != nil {
			required = *question.Required
		}
		if _, err := tx.Exec(`INSERT INTO focus_decision_questions(run_id,owner_id,profile_id,question_id,sequence_no,question_type,question,options_json,required,max_select,scale_min,scale_max,origin,status,revision,answer_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,0,?,?)`,
			runID, appDataScope.OwnerID, appDataScope.ProfileID, question.ID, index+1, question.Type, question.Question, string(options), boolInt(required), question.MaxSelect, question.ScaleMin, question.ScaleMax, origin, now, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	run, _, err := decisionRunByIDTx(tx, runID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionRunProjectionTx(tx, run)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusCreated, response)
}

// Keep the per-Focus run history rolling without deleting any run that is
// already part of an immutable decision revision. Questions and answers are
// removed through their existing ON DELETE CASCADE constraints.
func pruneUnreferencedTerminalDecisionRunsTx(tx *sql.Tx, focusID string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_runs WHERE owner_id=? AND profile_id=? AND focus_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&count); err != nil {
		return err
	}
	deleteCount := count - decisionRunLimitPerFocus + 1
	if deleteCount <= 0 {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM focus_decision_runs
WHERE id IN (
  SELECT r.id
  FROM focus_decision_runs r
  LEFT JOIN focus_decision_revisions v
    ON v.owner_id=r.owner_id AND v.profile_id=r.profile_id
   AND v.focus_id=r.focus_id AND v.run_id=r.id
  WHERE r.owner_id=? AND r.profile_id=? AND r.focus_id=?
    AND r.status IN ('completed','failed_terminal','cancelled','superseded')
    AND v.id IS NULL
  ORDER BY r.created_at ASC,r.id ASC
  LIMIT ?
)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, deleteCount)
	return err
}

func decisionRunGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		decisionRunError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法", 0)
		return
	}
	runID := strings.TrimSpace(r.URL.Query().Get("run_id"))
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	if runID == "" && focusID == "" || !validFocusIDOrEmpty(runID) || !validFocusIDOrEmpty(focusID) {
		decisionRunError(w, http.StatusBadRequest, "invalid_selector", "需要合法的 run_id 或 focus_id", 0)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	var run decisionRunRow
	var found bool
	canStart := false
	if runID != "" {
		run, found, err = decisionRunByIDTx(tx, runID)
	} else {
		var scopedFocusID, scopedFocusStatus string
		if scopeErr := tx.QueryRow(`SELECT id,status FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&scopedFocusID, &scopedFocusStatus); errors.Is(scopeErr, sql.ErrNoRows) {
			decisionRunError(w, http.StatusNotFound, "focus_not_found", "焦点空间不存在", 0)
			return
		} else if scopeErr != nil {
			app.HTTPErr(w, scopeErr)
			return
		}
		canStart = scopedFocusStatus == "active"
		run, found, err = latestDecisionRunTx(tx, focusID)
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !found && runID == "" {
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, decisionRunResponse{
			SchemaVersion:      decisionRunSchemaVersion,
			Status:             "not_started",
			Run:                nil,
			Questions:          []decisionQuestionPublic{},
			Progress:           decisionRunProgress{},
			AllowedTransitions: []string{},
			CanStart:           canStart,
		})
		return
	}
	if !found || focusID != "" && run.FocusID != focusID {
		decisionRunError(w, http.StatusNotFound, "run_not_found", "决策运行不存在", 0)
		return
	}
	run, err = reconcileExpiredDecisionAnalysisTx(tx, run, time.Now().UTC())
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if decisionRunStatusActive(run.Status) {
		var currentFocusRevision int
		if err := tx.QueryRow(`SELECT revision FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, run.FocusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&currentFocusRevision); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if currentFocusRevision != run.FocusRevision {
			now := time.Now().UTC().Format(time.RFC3339Nano)
			if err := supersedeActiveDecisionRunsTx(tx, run.FocusID, now); err != nil {
				app.HTTPErr(w, err)
				return
			}
			run, found, err = decisionRunByIDTx(tx, run.ID)
			if err != nil || !found {
				if err == nil {
					err = errors.New("决策运行失效状态没有保存")
				}
				app.HTTPErr(w, err)
				return
			}
		}
	}
	response, err := decisionRunProjectionTx(tx, run)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func decisionRunAnswerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		decisionRunError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法", 0)
		return
	}
	var request decisionRunAnswerRequest
	_, ok := decodeDecisionRunMutation(w, r, &request)
	if !ok {
		return
	}
	if request.SchemaVersion != decisionRunSchemaVersion || !validFocusID(request.RunID) || !validFocusID(request.QuestionID) || request.ExpectedRunRevision < 1 || request.ExpectedAnswerRevision < 0 {
		decisionRunError(w, http.StatusBadRequest, "invalid_request", "澄清回答参数不合法", 0)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	run, found, err := decisionRunByIDTx(tx, request.RunID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !found {
		decisionRunError(w, http.StatusNotFound, "run_not_found", "决策运行不存在", 0)
		return
	}
	if run.Revision != request.ExpectedRunRevision {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	question, found, err := decisionQuestionByIDTx(tx, run.ID, request.QuestionID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !found || question.Status == "superseded" {
		decisionRunError(w, http.StatusNotFound, "question_not_found", "澄清问题不存在", run.Revision)
		return
	}
	editingCompletedAnswer := run.Status != "waiting_clarification"
	if editingCompletedAnswer {
		if run.Status == "cancelled" || run.Status == "superseded" || question.Status != "answered" {
			decisionRunError(w, http.StatusConflict, "run_not_waiting", "当前决策没有可修改的澄清回答", run.Revision)
			return
		}
		var confirmed int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND run_id=? AND status='confirmed'`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.ID).Scan(&confirmed); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if confirmed > 0 {
			decisionRunError(w, http.StatusConflict, "decision_already_confirmed", "已确认决策请通过重新思考形成新版本", run.Revision)
			return
		}
	}
	if question.Status == "pending" && run.CurrentQuestionID != question.ID {
		decisionRunError(w, http.StatusConflict, "question_not_current", "请先回答当前澄清问题", run.Revision)
		return
	}
	if question.AnswerRevision != request.ExpectedAnswerRevision {
		decisionRunError(w, http.StatusConflict, "answer_revision_conflict", "澄清回答已变化，请重新读取", run.Revision)
		return
	}
	answerJSON, err := validateDecisionAnswer(question, request.Answer)
	if err != nil {
		decisionRunError(w, http.StatusBadRequest, "invalid_answer", err.Error(), run.Revision)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if question.Status == "answered" {
		if _, err := tx.Exec(`UPDATE focus_decision_answers SET is_current=0 WHERE run_id=? AND question_id IN (SELECT question_id FROM focus_decision_questions WHERE run_id=? AND sequence_no>=?) AND is_current=1`, run.ID, run.ID, question.Sequence); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if _, err := tx.Exec(`UPDATE focus_decision_questions SET status='pending',updated_at=? WHERE run_id=? AND sequence_no>?`, now, run.ID, question.Sequence); err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else if _, err := tx.Exec(`UPDATE focus_decision_answers SET is_current=0 WHERE run_id=? AND question_id=? AND is_current=1`, run.ID, question.ID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	newAnswerRevision := question.AnswerRevision + 1
	if _, err := tx.Exec(`INSERT INTO focus_decision_answers(run_id,owner_id,profile_id,question_id,revision,answer_type,answer_json,is_current,answered_at) VALUES(?,?,?,?,?,?,?,1,?)`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, question.ID, newAnswerRevision, request.Answer.Type, answerJSON, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	questionUpdate, err := tx.Exec(`UPDATE focus_decision_questions SET status='answered',answer_revision=?,updated_at=? WHERE run_id=? AND question_id=? AND answer_revision=?`, newAnswerRevision, now, run.ID, question.ID, question.AnswerRevision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := questionUpdate.RowsAffected(); changed != 1 {
		decisionRunError(w, http.StatusConflict, "answer_revision_conflict", "澄清回答已变化，请重新读取", run.Revision)
		return
	}
	if _, err := tx.Exec(`DELETE FROM focus_decision_answers WHERE run_id=? AND question_id=? AND is_current=0 AND revision NOT IN (SELECT revision FROM focus_decision_answers WHERE run_id=? AND question_id=? ORDER BY revision DESC LIMIT ?)`, run.ID, question.ID, run.ID, question.ID, decisionRunAnswerRevisionLimit); err != nil {
		app.HTTPErr(w, err)
		return
	}
	var nextQuestionID string
	err = tx.QueryRow(`SELECT question_id FROM focus_decision_questions WHERE run_id=? AND sequence_no>? AND status='pending' ORDER BY sequence_no LIMIT 1`, run.ID, question.Sequence).Scan(&nextQuestionID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		app.HTTPErr(w, err)
		return
	}
	nextStatus := "waiting_clarification"
	if errors.Is(err, sql.ErrNoRows) {
		nextQuestionID = ""
		nextStatus = "collecting_context"
	}
	var result sql.Result
	if editingCompletedAnswer {
		focusUpdate, updateErr := tx.Exec(`UPDATE focus_spaces SET revision=revision+1,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=? AND status='active'`, now, run.FocusID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusRevision)
		if updateErr != nil {
			app.HTTPErr(w, updateErr)
			return
		}
		if changed, _ := focusUpdate.RowsAffected(); changed != 1 {
			decisionRunError(w, http.StatusConflict, "focus_revision_conflict", "焦点已变化，请重新读取", run.Revision)
			return
		}
		newFocusRevision := run.FocusRevision + 1
		if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='superseded',error_code='clarification_changed',expires_at='',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND focus_revision=? AND status IN ('claimed','applied','partial')`, now, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.FocusRevision); err != nil {
			app.HTTPErr(w, err)
			return
		}
		result, err = tx.Exec(`UPDATE focus_decision_runs SET focus_revision=?,analysis_run_id='',decision_id='',status=?,current_question_id=?,revision=revision+1,attempt=attempt+1,input_tokens=0,output_tokens=0,error_code='',error_message='',started_at='',completed_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=? AND status=?`, newFocusRevision, nextStatus, nextQuestionID, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.Revision, run.Status)
	} else {
		result, err = tx.Exec(`UPDATE focus_decision_runs SET status=?,current_question_id=?,revision=revision+1,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=? AND status='waiting_clarification'`, nextStatus, nextQuestionID, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.Revision)
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	run, _, err = decisionRunByIDTx(tx, run.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionRunProjectionTx(tx, run)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func decisionRunResumeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		decisionRunError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法", 0)
		return
	}
	var request decisionRunResumeRequest
	_, ok := decodeDecisionRunMutation(w, r, &request)
	if !ok {
		return
	}
	if request.SchemaVersion != decisionRunSchemaVersion || !validFocusID(request.RunID) || request.ExpectedRevision < 1 || request.Status != "" && !validDecisionRunStatus(request.Status) ||
		!validFocusIDOrEmpty(request.AnalysisRunID) || !validFocusIDOrEmpty(request.DecisionID) || !validText(request.ErrorCode, 80, false) || !validText(request.ErrorMessage, 1000, false) ||
		request.Usage != nil && (request.Usage.InputTokens < 0 || request.Usage.OutputTokens < 0) {
		decisionRunError(w, http.StatusBadRequest, "invalid_request", "决策恢复参数不合法", 0)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	run, found, err := decisionRunByIDTx(tx, request.RunID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !found {
		decisionRunError(w, http.StatusNotFound, "run_not_found", "决策运行不存在", 0)
		return
	}
	if run.Revision != request.ExpectedRevision {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	analysisRunReplacement := request.AnalysisRunID != "" && run.AnalysisRunID != "" && request.AnalysisRunID != run.AnalysisRunID
	if request.DecisionID != "" && run.DecisionID != "" && request.DecisionID != run.DecisionID {
		decisionRunError(w, http.StatusConflict, "decision_conflict", "决策运行已经关联另一项决策", run.Revision)
		return
	}
	effectiveAnalysisRunID := run.AnalysisRunID
	if request.AnalysisRunID != "" {
		effectiveAnalysisRunID = request.AnalysisRunID
	}
	effectiveDecisionID := run.DecisionID
	if request.DecisionID != "" {
		effectiveDecisionID = request.DecisionID
	}
	if err := validateDecisionRunReferencesTx(tx, run.FocusID, effectiveAnalysisRunID, effectiveDecisionID); err != nil {
		decisionRunError(w, http.StatusBadRequest, "reference_invalid", err.Error(), run.Revision)
		return
	}
	_, hasPendingRequired, err := pendingDecisionQuestionTx(tx, run.ID, true)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	target := request.Status
	if target == "" {
		if !decisionRunResumable(run.Status) {
			decisionRunError(w, http.StatusConflict, "run_not_resumable", "当前决策运行不需要恢复", run.Revision)
			return
		}
		if hasPendingRequired {
			target = "waiting_clarification"
		} else {
			target = "queued"
		}
	}
	if !decisionRunCanTransition(run.Status, target) {
		decisionRunError(w, http.StatusConflict, "invalid_transition", "当前状态不允许该操作", run.Revision)
		return
	}
	if analysisRunReplacement {
		allowed, replacementErr := decisionRunAnalysisRebindAllowedTx(tx, run, request.AnalysisRunID, target)
		if replacementErr != nil {
			app.HTTPErr(w, replacementErr)
			return
		}
		if !allowed {
			decisionRunError(w, http.StatusConflict, "analysis_run_conflict", "决策运行已经关联另一轮分析", run.Revision)
			return
		}
	}
	if request.Question != nil && target != "waiting_clarification" {
		decisionRunError(w, http.StatusBadRequest, "question_without_clarification", "追加问题时必须进入等待澄清状态", run.Revision)
		return
	}
	if hasPendingRequired && target != "waiting_clarification" && target != "cancelled" && target != "failed_retryable" && target != "failed_terminal" {
		decisionRunError(w, http.StatusConflict, "clarification_required", "仍有必答问题需要确认", run.Revision)
		return
	}
	if target == "running_ai" || target == "applying" || target == "completed" {
		if effectiveAnalysisRunID == "" {
			decisionRunError(w, http.StatusConflict, "analysis_run_required", "进入分析阶段前需要关联焦点分析", run.Revision)
			return
		}
	}
	if target == "completed" {
		code, message, contractErr := validateDecisionRunCompletionTx(tx, run, effectiveAnalysisRunID, effectiveDecisionID)
		if contractErr != nil {
			app.HTTPErr(w, contractErr)
			return
		}
		if code != "" {
			decisionRunError(w, http.StatusConflict, code, message, run.Revision)
			return
		}
	}
	if target == "applying" || target == "completed" {
		reason, checkErr := decisionRunClarificationReasonTx(tx, run.ID, run.FocusID, effectiveAnalysisRunID, effectiveDecisionID)
		if checkErr != nil {
			app.HTTPErr(w, checkErr)
			return
		}
		if reason != "" {
			decisionRunError(w, http.StatusConflict, "clarification_required", reason, run.Revision)
			return
		}
	}
	if decisionRunStatusActive(target) {
		active, found, lookupErr := activeDecisionRunTx(tx, run.FocusID)
		if lookupErr != nil {
			app.HTTPErr(w, lookupErr)
			return
		}
		if found && active.ID != run.ID {
			decisionRunError(w, http.StatusConflict, "active_run_exists", "当前焦点已有进行中的决策", active.Revision)
			return
		}
	}
	run.AnalysisRunID = effectiveAnalysisRunID
	run.DecisionID = effectiveDecisionID
	if request.Question != nil {
		var questionCount, maxSequence int
		if err := tx.QueryRow(`SELECT COUNT(*),COALESCE(MAX(sequence_no),0) FROM focus_decision_questions WHERE run_id=? AND owner_id=? AND profile_id=?`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&questionCount, &maxSequence); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if questionCount >= decisionRunQuestionLimit || maxSequence >= decisionRunQuestionLimit {
			decisionRunError(w, http.StatusConflict, "question_limit_reached", "当前决策的澄清问题已达到上限", run.Revision)
			return
		}
		question := *request.Question
		if strings.TrimSpace(question.ID) == "" {
			question.ID = fmt.Sprintf("q_runtime_%02d", maxSequence+1)
		}
		normalized, normalizeErr := normalizeDecisionQuestions([]decisionQuestionInput{question})
		if normalizeErr != nil {
			decisionRunError(w, http.StatusBadRequest, "invalid_question", normalizeErr.Error(), run.Revision)
			return
		}
		question = normalized[0]
		var duplicate string
		duplicateErr := tx.QueryRow(`SELECT question_id FROM focus_decision_questions WHERE run_id=? AND owner_id=? AND profile_id=? AND question_id=?`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, question.ID).Scan(&duplicate)
		if duplicateErr == nil {
			decisionRunError(w, http.StatusConflict, "question_exists", "澄清问题标识已经存在", run.Revision)
			return
		}
		if !errors.Is(duplicateErr, sql.ErrNoRows) {
			app.HTTPErr(w, duplicateErr)
			return
		}
		repeatedStatus, repeated, repeatedErr := repeatedDecisionQuestionTx(tx, run.ID, question.Question)
		if repeatedErr != nil {
			app.HTTPErr(w, repeatedErr)
			return
		}
		if repeated {
			message := "相同的澄清问题已存在，请等待用户回答"
			if repeatedStatus == "answered" {
				message = "该澄清问题已经回答，请基于现有回答继续分析"
			}
			decisionRunError(w, http.StatusConflict, "question_repeated", message, run.Revision)
			return
		}
		options, _ := json.Marshal(question.Options)
		required := true
		if question.Required != nil {
			required = *question.Required
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.Exec(`INSERT INTO focus_decision_questions(run_id,owner_id,profile_id,question_id,sequence_no,question_type,question,options_json,required,max_select,scale_min,scale_max,origin,status,revision,answer_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',1,0,?,?)`,
			run.ID, appDataScope.OwnerID, appDataScope.ProfileID, question.ID, maxSequence+1, question.Type, question.Question, string(options), boolInt(required), question.MaxSelect, question.ScaleMin, question.ScaleMax, "runtime", now, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
		run.QuestionCount = questionCount + 1
	}
	if target == "waiting_clarification" {
		pending, found, pendingErr := pendingDecisionQuestionTx(tx, run.ID, false)
		if pendingErr != nil {
			app.HTTPErr(w, pendingErr)
			return
		}
		if !found {
			decisionRunError(w, http.StatusConflict, "clarification_missing", "没有待回答的澄清问题", run.Revision)
			return
		}
		run.CurrentQuestionID = pending
	} else if target != "waiting_clarification" {
		run.CurrentQuestionID = ""
	}
	if request.Usage != nil {
		if request.Usage.InputTokens < run.InputTokens || request.Usage.OutputTokens < run.OutputTokens {
			decisionRunError(w, http.StatusConflict, "usage_regression", "Token 用量不能回退", run.Revision)
			return
		}
		run.InputTokens = request.Usage.InputTokens
		run.OutputTokens = request.Usage.OutputTokens
	}
	run.ErrorCode, run.ErrorMessage = request.ErrorCode, request.ErrorMessage
	if target == "paused_insufficient_credits" && run.ErrorCode == "" {
		run.ErrorCode = "insufficient_credits"
	}
	if target != "paused_insufficient_credits" && target != "failed_retryable" && target != "failed_terminal" {
		run.ErrorCode, run.ErrorMessage = "", ""
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if run.StartedAt == "" && (target == "collecting_context" || target == "running_ai" || target == "applying") {
		run.StartedAt = now
	}
	if decisionRunTerminal(target) {
		run.CompletedAt = now
	} else {
		run.CompletedAt = ""
	}
	if decisionRunResumable(run.Status) && target != run.Status {
		run.Attempt++
	}
	result, err := tx.Exec(`UPDATE focus_decision_runs SET analysis_run_id=?,decision_id=?,status=?,revision=revision+1,attempt=?,question_count=?,current_question_id=?,input_tokens=?,output_tokens=?,error_code=?,error_message=?,started_at=?,completed_at=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`,
		run.AnalysisRunID, run.DecisionID, target, run.Attempt, run.QuestionCount, run.CurrentQuestionID, run.InputTokens, run.OutputTokens, run.ErrorCode, run.ErrorMessage, run.StartedAt, run.CompletedAt, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.Revision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	// A terminal decision failure cannot resume this run. Release its analysis
	// claim immediately so the next user-triggered attempt can claim a fresh
	// evidence snapshot instead of waiting for the lease TTL to expire.
	if target == "failed_terminal" && run.AnalysisRunID != "" {
		errorCode := firstNonEmpty(run.ErrorCode, "decision_run_failed")
		if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='failed',error_code=?,expires_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='claimed'`,
			errorCode, now, run.AnalysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	run, _, err = decisionRunByIDTx(tx, run.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionRunProjectionTx(tx, run)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

// A model may generate a fresh question ID while repeating the same wording.
// Compare the bounded question history by normalized visible text so retries do
// not create duplicate user prompts. The per-run history is capped at 12 rows.
func repeatedDecisionQuestionTx(tx *sql.Tx, runID, question string) (string, bool, error) {
	comparisonKey := decisionQuestionComparisonKey(question)
	rows, err := tx.Query(`SELECT question,status FROM focus_decision_questions WHERE run_id=? AND owner_id=? AND profile_id=?`, runID, appDataScope.OwnerID, appDataScope.ProfileID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	for rows.Next() {
		var existingQuestion, status string
		if err := rows.Scan(&existingQuestion, &status); err != nil {
			return "", false, err
		}
		existingKey := decisionQuestionComparisonKey(existingQuestion)
		if existingKey == comparisonKey {
			return status, true, nil
		}
	}
	return "", false, rows.Err()
}

func decisionQuestionComparisonKey(value string) string {
	runes := []rune(strings.ToLower(strings.TrimSpace(value)))
	result := make([]rune, 0, len(runes))
	pendingSpace := false
	for _, current := range runes {
		if unicode.IsSpace(current) {
			pendingSpace = true
			continue
		}
		if pendingSpace && len(result) > 0 && !unicode.IsPunct(result[len(result)-1]) && !unicode.IsPunct(current) {
			result = append(result, ' ')
		}
		result = append(result, current)
		pendingSpace = false
	}
	return string(result)
}

func decisionRunCancelHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		decisionRunError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的请求方法", 0)
		return
	}
	var request decisionRunCancelRequest
	_, ok := decodeDecisionRunMutation(w, r, &request)
	if !ok {
		return
	}
	if request.SchemaVersion != decisionRunSchemaVersion || !validFocusID(request.RunID) || request.ExpectedRevision < 1 || !validText(request.Reason, 500, false) {
		decisionRunError(w, http.StatusBadRequest, "invalid_request", "取消参数不合法", 0)
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	run, found, err := decisionRunByIDTx(tx, request.RunID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !found {
		decisionRunError(w, http.StatusNotFound, "run_not_found", "决策运行不存在", 0)
		return
	}
	if run.Revision != request.ExpectedRevision {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	if decisionRunTerminal(run.Status) {
		decisionRunError(w, http.StatusConflict, "run_terminal", "决策运行已经结束", run.Revision)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE focus_decision_runs SET status='cancelled',revision=revision+1,current_question_id='',error_code='user_cancelled',error_message=?,completed_at=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`, request.Reason, now, now, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.Revision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		decisionRunError(w, http.StatusConflict, "run_revision_conflict", "决策运行已变化，请重新读取", run.Revision)
		return
	}
	run, _, err = decisionRunByIDTx(tx, run.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionRunProjectionTx(tx, run)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func decodeDecisionRunMutation(w http.ResponseWriter, r *http.Request, dst any) (string, bool) {
	key, err := actionIdempotencyKey(r)
	if err != nil {
		decisionRunError(w, http.StatusBadRequest, "idempotency_key_invalid", err.Error(), 0)
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, decisionRunRequestBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		decisionRunError(w, http.StatusRequestEntityTooLarge, "request_too_large", "请求数据过大", 0)
		return "", false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		decisionRunError(w, http.StatusBadRequest, "invalid_json", "请求数据格式不合法", 0)
		return "", false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		decisionRunError(w, http.StatusBadRequest, "invalid_json", "请求数据包含多余内容", 0)
		return "", false
	}
	return key, true
}

func validFocusIDOrEmpty(value string) bool {
	return value == "" || validFocusID(value)
}

func decisionRunStatusActive(status string) bool {
	return status == "queued" || status == "collecting_context" || status == "running_ai" || status == "waiting_clarification" || status == "applying" || status == "paused_insufficient_credits" || status == "failed_retryable"
}

func supersedeActiveDecisionRunsTx(tx *sql.Tx, focusID, now string) error {
	_, err := tx.Exec(`UPDATE focus_decision_runs
SET status='superseded',revision=revision+1,current_question_id='',error_code='stale_focus_revision',error_message='焦点资料已变化，请重新分析',completed_at=?,updated_at=?
WHERE owner_id=? AND profile_id=? AND focus_id=?
  AND status IN ('queued','collecting_context','running_ai','waiting_clarification','applying','paused_insufficient_credits','failed_retryable')`,
		now, now, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
	return err
}

func pendingDecisionQuestionTx(tx *sql.Tx, runID string, requiredOnly bool) (string, bool, error) {
	query := `SELECT question_id FROM focus_decision_questions WHERE run_id=? AND owner_id=? AND profile_id=? AND status='pending'`
	if requiredOnly {
		query += ` AND required=1`
	}
	query += ` ORDER BY sequence_no LIMIT 1`
	var questionID string
	err := tx.QueryRow(query, runID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&questionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return questionID, err == nil, err
}

func activeDecisionRunTx(tx *sql.Tx, focusID string) (decisionRunRow, bool, error) {
	return queryDecisionRunTx(tx, `WHERE owner_id=? AND profile_id=? AND focus_id=? AND status IN ('queued','collecting_context','running_ai','waiting_clarification','applying','paused_insufficient_credits','failed_retryable') ORDER BY created_at DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
}

func latestDecisionRunTx(tx *sql.Tx, focusID string) (decisionRunRow, bool, error) {
	return queryDecisionRunTx(tx, `WHERE owner_id=? AND profile_id=? AND focus_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
}

func decisionRunByIDTx(tx *sql.Tx, runID string) (decisionRunRow, bool, error) {
	return queryDecisionRunTx(tx, `WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, runID)
}

// An analysis claim has a bounded lease. If the model process disappears after
// the decision run is linked to that claim, the run must not remain in
// running_ai forever. Reconcile the two records on the next read so the UI and
// the host can safely resume the same run without creating a duplicate result.
func reconcileExpiredDecisionAnalysisTx(tx *sql.Tx, run decisionRunRow, now time.Time) (decisionRunRow, error) {
	// A waiting clarification intentionally keeps its evidence context while
	// the user decides. Do not turn that visible question into a retryable error
	// merely because the bounded claim lease elapsed while waiting for input.
	if !decisionRunStatusActive(run.Status) || run.Status == "waiting_clarification" || strings.TrimSpace(run.AnalysisRunID) == "" {
		return run, nil
	}
	var analysisStatus, expiresAt string
	err := tx.QueryRow(`SELECT status,expires_at FROM focus_analysis_runs WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`,
		run.AnalysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID).Scan(&analysisStatus, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) || analysisStatus != "claimed" || strings.TrimSpace(expiresAt) == "" {
		return run, nil
	}
	expires, parseErr := time.Parse(time.RFC3339, expiresAt)
	if parseErr != nil || now.UTC().Before(expires) {
		return run, nil
	}
	updatedAt := now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='failed',error_code='claim_expired',expires_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='claimed'`,
		updatedAt, run.AnalysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID); err != nil {
		return run, err
	}
	if _, err := tx.Exec(`UPDATE focus_decision_runs SET status='failed_retryable',revision=revision+1,current_question_id='',error_code='focus_analysis_claim_expired',error_message='本轮分析已超时，已有结果未被覆盖，可以重试',completed_at='',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND revision=? AND status IN ('queued','collecting_context','running_ai','waiting_clarification','applying','paused_insufficient_credits','failed_retryable')`,
		updatedAt, run.ID, appDataScope.OwnerID, appDataScope.ProfileID, run.Revision); err != nil {
		return run, err
	}
	reconciled, found, err := decisionRunByIDTx(tx, run.ID)
	if err != nil {
		return run, err
	}
	if !found {
		return run, errors.New("决策运行恢复后不存在")
	}
	return reconciled, nil
}

func queryDecisionRunTx(tx *sql.Tx, where string, args ...any) (decisionRunRow, bool, error) {
	var run decisionRunRow
	err := tx.QueryRow(`SELECT id,focus_id,focus_revision,analysis_run_id,decision_id,decision_mode,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,status,revision,attempt,question_count,current_question_id,input_tokens,output_tokens,error_code,error_message,started_at,completed_at,created_at,updated_at FROM focus_decision_runs `+where, args...).Scan(
		&run.ID, &run.FocusID, &run.FocusRevision, &run.AnalysisRunID, &run.DecisionID, &run.DecisionMode, &run.IntentKind, &run.BlueprintID, &run.BlueprintVersion, &run.ThinkingTemplateID, &run.ThinkingTemplateVersion, &run.Status, &run.Revision, &run.Attempt, &run.QuestionCount, &run.CurrentQuestionID, &run.InputTokens, &run.OutputTokens, &run.ErrorCode, &run.ErrorMessage, &run.StartedAt, &run.CompletedAt, &run.CreatedAt, &run.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return decisionRunRow{}, false, nil
	}
	return run, err == nil, err
}

func decisionRunProjectionTx(tx *sql.Tx, run decisionRunRow) (decisionRunResponse, error) {
	rows, err := tx.Query(`SELECT q.question_id,q.sequence_no,q.question_type,q.question,q.options_json,q.required,q.max_select,q.scale_min,q.scale_max,q.origin,q.status,q.revision,q.answer_revision,COALESCE(a.answer_type,''),COALESCE(a.answer_json,''),COALESCE(a.revision,0),COALESCE(a.answered_at,'') FROM focus_decision_questions q LEFT JOIN focus_decision_answers a ON a.run_id=q.run_id AND a.question_id=q.question_id AND a.is_current=1 WHERE q.run_id=? AND q.owner_id=? AND q.profile_id=? ORDER BY q.sequence_no`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID)
	if err != nil {
		return decisionRunResponse{}, err
	}
	defer rows.Close()
	questions := make([]decisionQuestionPublic, 0, run.QuestionCount)
	answered := 0
	var current *decisionQuestionPublic
	for rows.Next() {
		var item decisionQuestionPublic
		var optionsJSON, answerType, answerJSON, answeredAt string
		var required int
		var answerRevision int
		if err := rows.Scan(&item.ID, &item.Sequence, &item.Type, &item.Question, &optionsJSON, &required, &item.MaxSelect, &item.ScaleMin, &item.ScaleMax, &item.Origin, &item.Status, &item.Revision, &item.AnswerRevision, &answerType, &answerJSON, &answerRevision, &answeredAt); err != nil {
			return decisionRunResponse{}, err
		}
		item.Required = required == 1
		if json.Unmarshal([]byte(optionsJSON), &item.Options) != nil {
			return decisionRunResponse{}, errors.New("决策澄清选项损坏")
		}
		if answerType != "" {
			var value any
			decoder := json.NewDecoder(strings.NewReader(answerJSON))
			decoder.UseNumber()
			if decoder.Decode(&value) != nil {
				return decisionRunResponse{}, errors.New("决策澄清回答损坏")
			}
			item.Answer = &decisionAnswerPublic{Type: answerType, Value: value, Revision: answerRevision, AnsweredAt: answeredAt}
		}
		if item.Status == "answered" {
			answered++
		}
		questions = append(questions, item)
		if item.ID == run.CurrentQuestionID {
			copy := item
			current = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return decisionRunResponse{}, err
	}
	percent := 100
	if len(questions) > 0 {
		percent = answered * 100 / len(questions)
	}
	canStart := false
	if decisionRunTerminal(run.Status) {
		var focusStatus string
		if err := tx.QueryRow(`SELECT status FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, run.FocusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&focusStatus); err != nil {
			return decisionRunResponse{}, err
		}
		canStart = focusStatus == "active"
	}
	canResume := decisionRunResumable(run.Status)
	if canResume && !decisionRunStatusActive(run.Status) {
		active, found, err := activeDecisionRunTx(tx, run.FocusID)
		if err != nil {
			return decisionRunResponse{}, err
		}
		canResume = !found || active.ID == run.ID
	}
	publicRun := decisionRunPublic{ID: run.ID, FocusID: run.FocusID, FocusRevision: run.FocusRevision, AnalysisRunID: run.AnalysisRunID, DecisionID: run.DecisionID, DecisionMode: run.DecisionMode, IntentKind: run.IntentKind, BlueprintID: run.BlueprintID, BlueprintVersion: run.BlueprintVersion, ThinkingTemplateID: run.ThinkingTemplateID, ThinkingTemplateVersion: run.ThinkingTemplateVersion, Status: run.Status, Revision: run.Revision, Attempt: run.Attempt, QuestionCount: run.QuestionCount, CurrentQuestionID: run.CurrentQuestionID, InputTokens: run.InputTokens, OutputTokens: run.OutputTokens, ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessage, StartedAt: run.StartedAt, CompletedAt: run.CompletedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
	return decisionRunResponse{
		SchemaVersion:   decisionRunSchemaVersion,
		Status:          run.Status,
		Run:             &publicRun,
		CurrentQuestion: current, Questions: questions,
		Progress:           decisionRunProgress{Answered: answered, Total: len(questions), Percent: percent},
		AllowedTransitions: decisionRunAllowedTransitions(run.Status),
		CanStart:           canStart, CanResume: canResume, CanCancel: !decisionRunTerminal(run.Status),
	}, nil
}

type decisionQuestionRow struct {
	ID             string
	Sequence       int
	Type           string
	Options        []decisionQuestionOption
	Required       bool
	MaxSelect      int
	ScaleMin       int
	ScaleMax       int
	Status         string
	AnswerRevision int
}

func decisionQuestionByIDTx(tx *sql.Tx, runID, questionID string) (decisionQuestionRow, bool, error) {
	var row decisionQuestionRow
	var optionsJSON string
	var required int
	err := tx.QueryRow(`SELECT question_id,sequence_no,question_type,options_json,required,max_select,scale_min,scale_max,status,answer_revision FROM focus_decision_questions WHERE run_id=? AND question_id=? AND owner_id=? AND profile_id=?`, runID, questionID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&row.ID, &row.Sequence, &row.Type, &optionsJSON, &required, &row.MaxSelect, &row.ScaleMin, &row.ScaleMax, &row.Status, &row.AnswerRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return decisionQuestionRow{}, false, nil
	}
	if err != nil {
		return decisionQuestionRow{}, false, err
	}
	row.Required = required == 1
	if json.Unmarshal([]byte(optionsJSON), &row.Options) != nil {
		return decisionQuestionRow{}, false, errors.New("决策澄清选项损坏")
	}
	return row, true, nil
}

func validateDecisionAnswer(question decisionQuestionRow, answer decisionAnswerInput) (string, error) {
	if answer.Type != question.Type || len(answer.Value) == 0 || len(answer.Value) > 8192 {
		return "", errors.New("回答类型与当前问题不匹配")
	}
	optionExists := func(id string) bool {
		for _, option := range question.Options {
			if option.ID == id {
				return true
			}
		}
		return false
	}
	switch question.Type {
	case "text":
		var value string
		if json.Unmarshal(answer.Value, &value) != nil || !validText(value, 2000, question.Required) {
			return "", errors.New("文本回答不合法")
		}
	case "choice":
		var value string
		if json.Unmarshal(answer.Value, &value) != nil || !optionExists(value) {
			return "", errors.New("请选择当前问题提供的选项")
		}
	case "multi":
		var values []string
		if json.Unmarshal(answer.Value, &values) != nil || len(values) == 0 || len(values) > question.MaxSelect {
			return "", errors.New("多选回答数量不合法")
		}
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			if !optionExists(value) {
				return "", errors.New("多选回答包含未知选项")
			}
			if _, exists := seen[value]; exists {
				return "", errors.New("多选回答包含重复选项")
			}
			seen[value] = struct{}{}
		}
	case "scale":
		var value int
		if json.Unmarshal(answer.Value, &value) != nil || value < question.ScaleMin || value > question.ScaleMax {
			return "", errors.New("评分超出允许范围")
		}
	default:
		return "", errors.New("不支持的回答类型")
	}
	compact := bytes.NewBuffer(nil)
	if err := json.Compact(compact, answer.Value); err != nil {
		return "", errors.New("回答 JSON 不合法")
	}
	return compact.String(), nil
}

func normalizeDecisionQuestions(input []decisionQuestionInput) ([]decisionQuestionInput, error) {
	if len(input) > decisionRunQuestionLimit {
		return nil, errors.New("澄清问题过多")
	}
	result := make([]decisionQuestionInput, len(input))
	seen := make(map[string]struct{}, len(input))
	for index, item := range input {
		item.ID = strings.TrimSpace(item.ID)
		if item.ID == "" {
			item.ID = fmt.Sprintf("q_%02d", index+1)
		}
		item.Question = strings.TrimSpace(item.Question)
		if !validFocusID(item.ID) || !validText(item.Question, 2000, true) || (item.Type != "choice" && item.Type != "multi" && item.Type != "text" && item.Type != "scale") {
			return nil, fmt.Errorf("第 %d 个澄清问题不合法", index+1)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, errors.New("澄清问题标识重复")
		}
		seen[item.ID] = struct{}{}
		if len(item.Options) > decisionRunOptionLimit || (item.Type == "choice" || item.Type == "multi") && len(item.Options) < 2 || (item.Type == "text" || item.Type == "scale") && len(item.Options) != 0 {
			return nil, fmt.Errorf("第 %d 个澄清问题选项不合法", index+1)
		}
		optionSeen := make(map[string]struct{}, len(item.Options))
		for optionIndex := range item.Options {
			item.Options[optionIndex].ID = strings.TrimSpace(item.Options[optionIndex].ID)
			item.Options[optionIndex].Label = strings.TrimSpace(item.Options[optionIndex].Label)
			option := item.Options[optionIndex]
			if !validFocusID(option.ID) || !validText(option.Label, 240, true) {
				return nil, fmt.Errorf("第 %d 个澄清问题选项不合法", index+1)
			}
			if _, exists := optionSeen[option.ID]; exists {
				return nil, errors.New("澄清选项标识重复")
			}
			optionSeen[option.ID] = struct{}{}
		}
		if item.MaxSelect == 0 {
			item.MaxSelect = 1
		}
		if item.Type == "multi" && (item.MaxSelect < 1 || item.MaxSelect > len(item.Options)) || item.Type != "multi" && item.MaxSelect != 1 {
			return nil, fmt.Errorf("第 %d 个澄清问题 max_select 不合法", index+1)
		}
		if item.Type == "scale" {
			if item.ScaleMin == 0 && item.ScaleMax == 0 {
				item.ScaleMin, item.ScaleMax = 1, 5
			}
			if item.ScaleMin < 0 || item.ScaleMax > 10 || item.ScaleMin >= item.ScaleMax {
				return nil, fmt.Errorf("第 %d 个评分范围不合法", index+1)
			}
		} else {
			item.ScaleMin, item.ScaleMax = 1, 5
		}
		result[index] = item
	}
	return result, nil
}

func builtinDecisionQuestions(mode string, focus FocusSpace) []decisionQuestionInput {
	required := true
	textQuestion := func(id, question string) decisionQuestionInput {
		return decisionQuestionInput{ID: id, Type: "text", Question: question, Required: &required, MaxSelect: 1, ScaleMin: 1, ScaleMax: 5}
	}
	decisionQuestion := strings.TrimSpace(focus.DecisionQuestion)
	if decisionQuestion == "" {
		return []decisionQuestionInput{textQuestion("q_goal", "这次需要做出的具体决定是什么？")}
	}
	if mode == "bounded_rationality" && strings.TrimSpace(focus.HorizonEnd) == "" {
		return []decisionQuestionInput{textQuestion("q_deadline", "这件事最晚什么时候必须有结果？")}
	}
	// The bound Agent turn reads current evidence and answers before deciding
	// whether another question is material. Preloading a generic questionnaire
	// would make later questions ignore earlier answers and duplicate fields the
	// user already supplied on the Focus itself.
	return []decisionQuestionInput{}
}

func validateDecisionRunCompletionTx(tx *sql.Tx, run decisionRunRow, analysisRunID, decisionID string) (string, string, error) {
	var analysisStatus string
	if err := tx.QueryRow(`SELECT status FROM focus_analysis_runs WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`, analysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID).Scan(&analysisStatus); err != nil {
		return "", "", err
	}
	if analysisStatus != "applied" && analysisStatus != "partial" {
		return "analysis_not_applied", "本轮分析尚未成功写回，不能标记为完成", nil
	}
	if run.IntentKind == "decide" && decisionID == "" {
		return "decision_required", "完成做决定任务前需要关联决策结果", nil
	}
	blueprintResponse, err := resolveFocusBlueprintTx(tx, "focus", run.FocusID, run.BlueprintID, nil, false)
	if err != nil {
		return "", "", err
	}
	if blueprintResponse.BaseVersion != run.BlueprintVersion || blueprintResponse.Effective.IntentKind != run.IntentKind {
		return "blueprint_changed", "本轮分析使用的应用蓝图已经变化，请重新分析；已有结果已保留", nil
	}
	counts := map[string]int{}
	queries := map[string]string{
		"signals":       `SELECT COUNT(*) FROM signals WHERE focus_id=? AND analysis_run_id=? AND status<>'rejected'`,
		"decisions":     `SELECT COUNT(*) FROM decisions WHERE focus_id=? AND analysis_run_id=? AND status<>'rejected'`,
		"actions":       `SELECT COUNT(*) FROM actions WHERE focus_id=? AND analysis_run_id=? AND status<>'rejected' AND confirmation_status<>'rejected'`,
		"work_products": `SELECT COUNT(*) FROM focus_work_products WHERE owner_id=? AND profile_id=? AND focus_id=? AND analysis_run_id=? AND status<>'rejected'`,
	}
	for kind, query := range queries {
		var count int
		var queryErr error
		if kind == "work_products" {
			queryErr = tx.QueryRow(query, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, analysisRunID).Scan(&count)
		} else {
			queryErr = tx.QueryRow(query, run.FocusID, analysisRunID).Scan(&count)
		}
		if queryErr != nil {
			return "", "", queryErr
		}
		counts[kind] = count
	}
	labels := map[string]string{"signals": "信号", "decisions": "决定", "work_products": "产出物", "actions": "行动"}
	requiredCodes := map[string]string{"signals": "signal_required", "decisions": "decision_required", "work_products": "work_product_required", "actions": "action_required"}
	for _, kind := range []string{"signals", "decisions", "work_products", "actions"} {
		rule := blueprintResponse.Effective.ResultContract[kind]
		if counts[kind] < rule.Min {
			return requiredCodes[kind], fmt.Sprintf("当前%s至少需要 %d 个%s，请重新分析", focusIntentKindLabel(run.IntentKind), rule.Min, labels[kind]), nil
		}
		if counts[kind] > rule.Max {
			return "result_contract_exceeded", fmt.Sprintf("当前%s最多允许 %d 个%s，请减少结果后重试", focusIntentKindLabel(run.IntentKind), rule.Max, labels[kind]), nil
		}
	}
	allowedTypes := stringSet(blueprintResponse.Effective.ResultContract["work_products"].Types...)
	if len(allowedTypes) > 0 {
		rows, queryErr := tx.Query(`SELECT product_type FROM focus_work_products WHERE owner_id=? AND profile_id=? AND focus_id=? AND analysis_run_id=? AND status<>'rejected'`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, analysisRunID)
		if queryErr != nil {
			return "", "", queryErr
		}
		defer rows.Close()
		for rows.Next() {
			var productType string
			if err := rows.Scan(&productType); err != nil {
				return "", "", err
			}
			if _, ok := allowedTypes[productType]; !ok {
				return "work_product_type_not_allowed", "当前任务模式不允许这个产出物类型，请重新分析", nil
			}
		}
		if err := rows.Err(); err != nil {
			return "", "", err
		}
	}
	return "", "", nil
}

func focusIntentKindLabel(intentKind string) string {
	switch intentKind {
	case "plan":
		return "计划"
	case "create":
		return "创作"
	case "monitor":
		return "关注"
	default:
		return "决策"
	}
}

func validateDecisionRunReferencesTx(tx *sql.Tx, focusID, analysisRunID, decisionID string) error {
	if analysisRunID != "" {
		var found string
		if err := tx.QueryRow(`SELECT id FROM focus_analysis_runs WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`, analysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&found); err != nil {
			return errors.New("关联的焦点分析不存在")
		}
	}
	if decisionID != "" {
		var found, decisionAnalysisRunID string
		if err := tx.QueryRow(`SELECT id,analysis_run_id FROM decisions WHERE id=? AND focus_id=?`, decisionID, focusID).Scan(&found, &decisionAnalysisRunID); err != nil {
			return errors.New("关联的决策不存在")
		}
		if analysisRunID != "" && decisionAnalysisRunID != analysisRunID {
			return errors.New("关联的决策不属于当前焦点分析")
		}
	}
	return nil
}

func decisionRunClarificationReasonTx(tx *sql.Tx, runID, focusID, analysisRunID, decisionID string) (string, error) {
	if analysisRunID == "" || decisionID == "" {
		return "", nil
	}
	var answered int
	if runID != "" {
		if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_answers WHERE run_id=? AND owner_id=? AND profile_id=? AND is_current=1`, runID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&answered); err != nil {
			return "", err
		}
	} else if err := tx.QueryRow(`SELECT COUNT(*)
FROM focus_decision_answers a
JOIN focus_decision_runs r ON r.id=a.run_id AND r.owner_id=a.owner_id AND r.profile_id=a.profile_id
WHERE a.owner_id=? AND a.profile_id=? AND a.is_current=1
  AND r.focus_id=? AND r.analysis_run_id=? AND r.decision_id=?
  AND r.status='completed'`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, analysisRunID, decisionID).Scan(&answered); err != nil {
		return "", err
	}
	if answered > 0 {
		return "", nil
	}
	var independentEvidence int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT source_kind || char(0) || source_id)
FROM focus_analysis_evidence
WHERE run_id=? AND owner_id=? AND profile_id=?`, analysisRunID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&independentEvidence); err != nil {
		return "", err
	}
	if independentEvidence < 2 {
		return "当前依据不足以直接形成决策，请先向用户确认一个会改变结论的关键条件", nil
	}
	var counterJSON string
	if err := tx.QueryRow(`SELECT counterevidence FROM decisions WHERE id=? AND focus_id=? AND analysis_run_id=?`, decisionID, focusID, analysisRunID).Scan(&counterJSON); err != nil {
		return "", err
	}
	var counterevidence []string
	if json.Unmarshal([]byte(counterJSON), &counterevidence) != nil {
		return "", errors.New("决策反向依据数据损坏")
	}
	for _, item := range counterevidence {
		if strings.TrimSpace(item) != "" {
			return "当前依据存在冲突，请先向用户说明冲突并确认优先采用哪一侧依据", nil
		}
	}
	return "", nil
}

// focusAnalysisClarificationReasonTx runs before model output is persisted.
// The decision-run completion guard below is intentionally retained as a
// second line of defence, but checking only after apply would briefly store a
// conclusion/action that the user had not yet clarified.
func focusAnalysisClarificationReasonTx(tx *sql.Tx, run focusRunSummary, request focusAnalysisApplyRequest) (string, error) {
	var decisionRunID string
	err := tx.QueryRow(`SELECT id FROM focus_decision_runs
WHERE owner_id=? AND profile_id=? AND focus_id=? AND analysis_run_id=?
  AND status IN ('running_ai','collecting_context','waiting_clarification','applying','failed_retryable')
ORDER BY updated_at DESC,id DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.ID).Scan(&decisionRunID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var answered int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_answers WHERE run_id=? AND owner_id=? AND profile_id=? AND is_current=1`, decisionRunID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&answered); err != nil {
		return "", err
	}
	if answered > 0 {
		return "", nil
	}
	var independentEvidence int
	if err := tx.QueryRow(`SELECT COUNT(DISTINCT source_kind || char(0) || source_id)
FROM focus_analysis_evidence
WHERE run_id=? AND owner_id=? AND profile_id=?`, run.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&independentEvidence); err != nil {
		return "", err
	}
	if independentEvidence < 2 {
		return "当前依据不足以直接形成决策，请先向用户确认一个会改变结论的关键条件", nil
	}
	hasCounterevidence := func(items []string) bool {
		for _, item := range items {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
		return false
	}
	for _, item := range request.Signals {
		if hasCounterevidence(item.Counterevidence) {
			return "当前依据存在冲突，请先向用户说明冲突并确认优先采用哪一侧依据", nil
		}
	}
	for _, item := range request.Actions {
		if hasCounterevidence(item.Counterevidence) {
			return "当前依据存在冲突，请先向用户说明冲突并确认优先采用哪一侧依据", nil
		}
	}
	for _, item := range request.Decisions {
		if hasCounterevidence(item.Counterevidence) {
			return "当前依据存在冲突，请先向用户说明冲突并确认优先采用哪一侧依据", nil
		}
	}
	return "", nil
}

// Rebinding is deliberately narrower than a normal resume. The host may
// replace a stale evidence claim only after it has fenced the previous model
// session and the old claim has been durably marked failed. This keeps an
// active model session from being overwritten while allowing a changed
// evidence set to restart the same decision run.
func decisionRunAnalysisRebindAllowed(runStatus, target, decisionID, previousAnalysisStatus, replacementAnalysisStatus string) bool {
	return runStatus == "collecting_context" && target == "running_ai" && decisionID == "" &&
		previousAnalysisStatus == "failed" && replacementAnalysisStatus == "claimed"
}

func decisionRunAnalysisRebindAllowedTx(tx *sql.Tx, run decisionRunRow, replacementID, target string) (bool, error) {
	var previousStatus, replacementStatus string
	err := tx.QueryRow(`SELECT previous.status,replacement.status
FROM focus_analysis_runs previous
JOIN focus_analysis_runs replacement ON replacement.id=?
WHERE previous.id=? AND previous.owner_id=? AND previous.profile_id=? AND previous.focus_id=?
  AND replacement.owner_id=? AND replacement.profile_id=? AND replacement.focus_id=?`,
		replacementID, run.AnalysisRunID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID,
		appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID).Scan(&previousStatus, &replacementStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return decisionRunAnalysisRebindAllowed(run.Status, target, run.DecisionID, previousStatus, replacementStatus), nil
}

func decisionRunError(w http.ResponseWriter, status int, code, message string, revision int) {
	app.WriteJSON(w, status, map[string]any{"error": message, "code": code, "revision": revision})
}

package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"focus-space/internal/app"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	actionConfirmationDraft    = "draft"
	actionConfirmationAccepted = "accepted"
	actionConfirmationRejected = "rejected"
	actionScheduleNone         = "none"
	actionSchedulePending      = "pending"
	actionScheduleScheduled    = "scheduled"
	actionScheduleFailed       = "failed"
	maxActionWorkflowRows      = 2000
	maxActionMutationRequests  = 1024
	maxReaderDigestReceipts    = 2000
	trustedHostOperationHeader = "X-Zettlab-Trusted-Host-Operation"
	trustedHostOperationValue  = "v1"
)

var actionWorkflowMu sync.Mutex

var (
	errActionIdempotencyConflict = errors.New("idempotency key 已用于另一份请求")
	errActionStaleRevision       = errors.New("行动版本已变化")
	errActionStateConflict       = errors.New("行动状态不允许当前操作")
)

type actionWorkflowRow struct {
	ActionID            string
	Revision            int
	AgentFingerprint    string
	UserEdited          bool
	HasTaskOverride     bool
	TaskOverride        string
	HasOwnerOverride    bool
	OwnerOverride       string
	HasDueAtOverride    bool
	DueAtOverride       string
	HasPriorityOverride bool
	PriorityOverride    string
	ConfirmationStatus  string
	ScheduleState       string
	RequestID           string
	CandidateID         string
	ClientRequestID     string
	JobID               string
	NextRunAt           string
	Replayed            bool
	ErrorCode           string
	ErrorSummary        string
	UpdatedAt           string
}

type actionFeedbackRequest struct {
	ActionID           string  `json:"action_id"`
	ExpectedRevision   int     `json:"expected_revision"`
	ConfirmationStatus string  `json:"confirmation_status"`
	Task               *string `json:"task,omitempty"`
	Owner              *string `json:"owner,omitempty"`
	DueAt              *string `json:"due_at,omitempty"`
	Priority           *string `json:"priority,omitempty"`
}

type actionScheduleBeginRequest struct {
	ActionID         string `json:"action_id"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	CandidateID      string `json:"candidate_id"`
	ClientRequestID  string `json:"client_request_id"`
}

type actionScheduleReceiptRequest struct {
	ActionID         string `json:"action_id"`
	ExpectedRevision int    `json:"expected_revision"`
	RequestID        string `json:"request_id"`
	CandidateID      string `json:"candidate_id"`
	Status           string `json:"status"`
	ClientRequestID  string `json:"client_request_id"`
	JobID            string `json:"job_id,omitempty"`
	Replayed         bool   `json:"replayed,omitempty"`
	NextRunAt        string `json:"next_run_at,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	ErrorSummary     string `json:"error_summary,omitempty"`
}

type actionFeedbackResult struct {
	ActionID           string `json:"action_id"`
	Revision           int    `json:"revision"`
	ConfirmationStatus string `json:"confirmation_status"`
	ScheduleState      string `json:"schedule_state"`
}

type actionScheduleBeginResult struct {
	ActionID        string `json:"action_id"`
	Revision        int    `json:"revision"`
	ScheduleState   string `json:"schedule_state"`
	RequestID       string `json:"request_id"`
	CandidateID     string `json:"candidate_id"`
	ClientRequestID string `json:"client_request_id"`
	Replayed        bool   `json:"replayed,omitempty"`
}

type actionScheduleReceiptResult struct {
	ActionID           string `json:"action_id"`
	Revision           int    `json:"revision"`
	ConfirmationStatus string `json:"confirmation_status"`
	ScheduleState      string `json:"schedule_state"`
	RequestID          string `json:"request_id"`
	CandidateID        string `json:"candidate_id"`
	ClientRequestID    string `json:"client_request_id"`
	JobID              string `json:"job_id,omitempty"`
	Replayed           bool   `json:"replayed,omitempty"`
	NextRunAt          string `json:"next_run_at,omitempty"`
	ErrorCode          string `json:"error_code,omitempty"`
	ErrorSummary       string `json:"error_summary,omitempty"`
}

func validActionConfirmationStatus(value string) bool {
	return value == actionConfirmationDraft || value == actionConfirmationAccepted || value == actionConfirmationRejected
}

func validActionScheduleState(value string) bool {
	return value == "" || value == actionScheduleNone || value == actionSchedulePending || value == actionScheduleScheduled || value == actionScheduleFailed
}

func validActionPriority(value string) bool {
	return value == "low" || value == "medium" || value == "high"
}

func validActionWorkflowID(value string) bool {
	return validText(value, 128, true) && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func validActionIdempotencyKey(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}

func requestIdempotencyKey(r *http.Request) string {
	if r == nil {
		return ""
	}
	if key := strings.TrimSpace(r.Header.Get("X-Zettlab-Idempotency-Key")); key != "" {
		return key
	}
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
}

func actionIdempotencyKey(r *http.Request) (string, error) {
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		return "", errors.New("缺少或不合法的幂等键")
	}
	return key, nil
}

func loadActionWorkflowTx(tx *sql.Tx, actionID string) (actionWorkflowRow, error) {
	var row actionWorkflowRow
	var userEdited, hasTask, hasOwner, hasDue, hasPriority, replayed int
	err := tx.QueryRow(`
SELECT action_id, revision, agent_fingerprint, user_edited,
       has_task_override, task_override, has_owner_override, owner_override,
       has_due_at_override, due_at_override, has_priority_override, priority_override,
       confirmation_status, schedule_state, request_id, candidate_id,
       client_request_id, job_id, next_run_at, replayed, error_code, error_summary, updated_at
FROM action_workflows WHERE action_id = ?`, actionID).Scan(
		&row.ActionID, &row.Revision, &row.AgentFingerprint, &userEdited,
		&hasTask, &row.TaskOverride, &hasOwner, &row.OwnerOverride,
		&hasDue, &row.DueAtOverride, &hasPriority, &row.PriorityOverride,
		&row.ConfirmationStatus, &row.ScheduleState, &row.RequestID, &row.CandidateID,
		&row.ClientRequestID, &row.JobID, &row.NextRunAt, &replayed, &row.ErrorCode,
		&row.ErrorSummary, &row.UpdatedAt,
	)
	row.UserEdited = userEdited == 1
	row.HasTaskOverride = hasTask == 1
	row.HasOwnerOverride = hasOwner == 1
	row.HasDueAtOverride = hasDue == 1
	row.HasPriorityOverride = hasPriority == 1
	row.Replayed = replayed == 1
	return row, err
}

func insertActionWorkflowTx(tx *sql.Tx, row actionWorkflowRow) error {
	if row.Revision < 1 {
		row.Revision = 1
	}
	if row.ConfirmationStatus == "" {
		row.ConfirmationStatus = actionConfirmationDraft
	}
	if row.ScheduleState == "" {
		row.ScheduleState = actionScheduleNone
	}
	_, err := tx.Exec(`
INSERT INTO action_workflows(
 action_id, revision, agent_fingerprint, user_edited,
 has_task_override, task_override, has_owner_override, owner_override,
 has_due_at_override, due_at_override, has_priority_override, priority_override,
 confirmation_status, schedule_state, request_id, candidate_id, client_request_id,
 job_id, next_run_at, replayed, error_code, error_summary, updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ActionID, row.Revision, row.AgentFingerprint, boolInt(row.UserEdited),
		boolInt(row.HasTaskOverride), row.TaskOverride, boolInt(row.HasOwnerOverride), row.OwnerOverride,
		boolInt(row.HasDueAtOverride), row.DueAtOverride, boolInt(row.HasPriorityOverride), row.PriorityOverride,
		row.ConfirmationStatus, row.ScheduleState, row.RequestID, row.CandidateID, row.ClientRequestID,
		row.JobID, row.NextRunAt, boolInt(row.Replayed), row.ErrorCode, row.ErrorSummary, row.UpdatedAt,
	)
	return err
}

func updateActionWorkflowTx(tx *sql.Tx, row actionWorkflowRow) error {
	_, err := tx.Exec(`
UPDATE action_workflows SET revision=?, agent_fingerprint=?, user_edited=?,
 has_task_override=?, task_override=?, has_owner_override=?, owner_override=?,
 has_due_at_override=?, due_at_override=?, has_priority_override=?, priority_override=?,
 confirmation_status=?, schedule_state=?, request_id=?, candidate_id=?, client_request_id=?,
 job_id=?, next_run_at=?, replayed=?, error_code=?, error_summary=?, updated_at=?
WHERE action_id=?`,
		row.Revision, row.AgentFingerprint, boolInt(row.UserEdited),
		boolInt(row.HasTaskOverride), row.TaskOverride, boolInt(row.HasOwnerOverride), row.OwnerOverride,
		boolInt(row.HasDueAtOverride), row.DueAtOverride, boolInt(row.HasPriorityOverride), row.PriorityOverride,
		row.ConfirmationStatus, row.ScheduleState, row.RequestID, row.CandidateID, row.ClientRequestID,
		row.JobID, row.NextRunAt, boolInt(row.Replayed), row.ErrorCode, row.ErrorSummary, row.UpdatedAt,
		row.ActionID,
	)
	return err
}

func actionProjectionFingerprint(action Action) string {
	value := struct {
		Task      string   `json:"task"`
		Owner     string   `json:"owner"`
		DueAt     string   `json:"due_at"`
		SourceIDs []string `json:"source_ids"`
		Status    string   `json:"status"`
		Priority  string   `json:"priority"`
	}{action.Task, action.Owner, action.DueAt, action.SourceIDs, action.Status, action.Priority}
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func actionWorkflowForAgentTx(tx *sql.Tx, action Action) (actionWorkflowRow, error) {
	row, err := loadActionWorkflowTx(tx, action.ID)
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return actionWorkflowRow{}, err
	}
	row = actionWorkflowRow{
		ActionID:           action.ID,
		Revision:           1,
		AgentFingerprint:   actionProjectionFingerprint(action),
		ConfirmationStatus: actionConfirmationDraft,
		ScheduleState:      actionScheduleNone,
		UpdatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := insertActionWorkflowTx(tx, row); err != nil {
		return actionWorkflowRow{}, err
	}
	return row, nil
}

func overlayAction(action Action, row actionWorkflowRow) Action {
	if row.HasTaskOverride {
		action.Task = row.TaskOverride
	}
	if row.HasOwnerOverride {
		action.Owner = row.OwnerOverride
	}
	if row.HasDueAtOverride {
		action.DueAt = row.DueAtOverride
	}
	if row.HasPriorityOverride {
		action.Priority = row.PriorityOverride
	}
	action.Revision = row.Revision
	action.ConfirmationStatus = firstNonEmpty(row.ConfirmationStatus, actionConfirmationDraft)
	action.ScheduleState = firstNonEmpty(row.ScheduleState, actionScheduleNone)
	action.ScheduleRequestID = row.RequestID
	action.ScheduleCandidateID = row.CandidateID
	action.ClientRequestID = row.ClientRequestID
	action.JobID = row.JobID
	action.NextRunAt = row.NextRunAt
	action.ScheduleReplayed = row.Replayed
	action.ScheduleErrorCode = row.ErrorCode
	action.ScheduleError = row.ErrorSummary
	action.WorkflowUpdatedAt = row.UpdatedAt
	return action
}

// actionWorkflowProtectsProjection reports whether the Agent may replace the
// base action row during a refresh. Once a user has edited or confirmed an
// action, or a scheduling handshake has started, the last user-owned
// projection is authoritative until the user changes it again.
func actionWorkflowProtectsProjection(row actionWorkflowRow) bool {
	return row.UserEdited || row.ConfirmationStatus != actionConfirmationDraft || row.ScheduleState != actionScheduleNone
}

func reconcileAgentActionTx(tx *sql.Tx, action Action) (Action, error) {
	row, err := loadActionWorkflowTx(tx, action.ID)
	if errors.Is(err, sql.ErrNoRows) {
		row, err = actionWorkflowForAgentTx(tx, action)
		if err != nil {
			return Action{}, err
		}
		return overlayAction(action, row), nil
	}
	if err != nil {
		return Action{}, err
	}
	if actionWorkflowProtectsProjection(row) {
		// Do not even advance the Agent fingerprint here. A later refresh must
		// remain a no-op until the user explicitly edits/reopens the action.
		current, currentErr := actionBaseByIDTx(tx, action.ID)
		if currentErr == nil {
			return overlayAction(current, row), nil
		}
		if !errors.Is(currentErr, sql.ErrNoRows) {
			return Action{}, currentErr
		}
		// A stale sidecar without a base row is recoverable: recreate the row
		// from the incoming projection while retaining the sidecar status.
		return overlayAction(action, row), nil
	}
	fingerprint := actionProjectionFingerprint(action)
	if row.Revision < 1 {
		row.Revision = 1
	}
	if row.AgentFingerprint != "" && row.AgentFingerprint != fingerprint &&
		row.ConfirmationStatus == actionConfirmationDraft && row.ScheduleState == actionScheduleNone && !row.UserEdited {
		row.Revision++
	}
	row.AgentFingerprint = fingerprint
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := updateActionWorkflowTx(tx, row); err != nil {
		return Action{}, err
	}
	return overlayAction(action, row), nil
}

func applyActionWorkflows(d *Dashboard) error {
	rows, err := db.Query(`
SELECT action_id, revision, agent_fingerprint, user_edited,
       has_task_override, task_override, has_owner_override, owner_override,
       has_due_at_override, due_at_override, has_priority_override, priority_override,
       confirmation_status, schedule_state, request_id, candidate_id,
       client_request_id, job_id, next_run_at, replayed, error_code, error_summary, updated_at
FROM action_workflows ORDER BY updated_at DESC LIMIT ?`, maxActionWorkflowRows)
	if err != nil {
		return err
	}
	defer rows.Close()
	workflows := make(map[string]actionWorkflowRow)
	for rows.Next() {
		var row actionWorkflowRow
		var userEdited, hasTask, hasOwner, hasDue, hasPriority, replayed int
		if err := rows.Scan(&row.ActionID, &row.Revision, &row.AgentFingerprint, &userEdited,
			&hasTask, &row.TaskOverride, &hasOwner, &row.OwnerOverride,
			&hasDue, &row.DueAtOverride, &hasPriority, &row.PriorityOverride,
			&row.ConfirmationStatus, &row.ScheduleState, &row.RequestID, &row.CandidateID,
			&row.ClientRequestID, &row.JobID, &row.NextRunAt, &replayed, &row.ErrorCode,
			&row.ErrorSummary, &row.UpdatedAt); err != nil {
			return err
		}
		row.UserEdited = userEdited == 1
		row.HasTaskOverride = hasTask == 1
		row.HasOwnerOverride = hasOwner == 1
		row.HasDueAtOverride = hasDue == 1
		row.HasPriorityOverride = hasPriority == 1
		row.Replayed = replayed == 1
		workflows[row.ActionID] = row
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range d.Actions {
		if row, ok := workflows[d.Actions[i].ID]; ok {
			d.Actions[i] = overlayAction(d.Actions[i], row)
		} else {
			d.Actions[i].Revision = 1
			d.Actions[i].ConfirmationStatus = actionConfirmationDraft
			d.Actions[i].ScheduleState = actionScheduleNone
		}
	}
	return nil
}

func pruneActionWorkflowsTx(tx *sql.Tx) error {
	_, err := tx.Exec(`DELETE FROM action_workflows WHERE rowid IN (
SELECT rowid FROM action_workflows ORDER BY updated_at DESC LIMIT -1 OFFSET ?)`, maxActionWorkflowRows)
	return err
}

func validateReaderDigestReceipts(items []ReaderDigestReceipt) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !validDigestHash(item.ContentHash) || !validText(item.SourceID, 128, true) || !validActionWorkflowID(item.EntryID) || !validText(item.ProcessedAt, 80, true) {
			return errors.New("RSS 提炼回执字段不合法")
		}
		if _, err := time.Parse(time.RFC3339, item.ProcessedAt); err != nil {
			return errors.New("RSS 提炼回执时间不合法")
		}
		if item.Outcome != "drafted" && item.Outcome != "no_candidate" && item.Outcome != "skipped" {
			return errors.New("RSS 提炼回执状态不合法")
		}
		if _, exists := seen[item.ContentHash]; exists {
			return errors.New("RSS 提炼回执重复")
		}
		seen[item.ContentHash] = struct{}{}
	}
	return nil
}

func validDigestHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func storeReaderDigestReceiptsTx(tx *sql.Tx, items []ReaderDigestReceipt) error {
	for _, item := range items {
		if _, err := tx.Exec(`
INSERT INTO reader_digest_receipts(content_hash,source_id,entry_id,processed_at,outcome)
VALUES(?,?,?,?,?)
ON CONFLICT(content_hash) DO UPDATE SET source_id=excluded.source_id,
entry_id=excluded.entry_id, processed_at=excluded.processed_at, outcome=excluded.outcome`,
			item.ContentHash, item.SourceID, item.EntryID, item.ProcessedAt, item.Outcome); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`DELETE FROM reader_digest_receipts WHERE content_hash IN (
SELECT content_hash FROM reader_digest_receipts ORDER BY processed_at DESC LIMIT -1 OFFSET ?)`, maxReaderDigestReceipts)
	return err
}

func applyReaderDigestReceipts(d *Dashboard) error {
	rows, err := db.Query(`SELECT content_hash, source_id, entry_id, processed_at, outcome FROM reader_digest_receipts ORDER BY processed_at DESC LIMIT ?`, maxReaderDigestReceipts)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]ReaderDigestReceipt, 0, 32)
	for rows.Next() {
		var item ReaderDigestReceipt
		if err := rows.Scan(&item.ContentHash, &item.SourceID, &item.EntryID, &item.ProcessedAt, &item.Outcome); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	d.ReaderDigests = items
	return nil
}

func decodeActionWorkflowJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return app.DecodeStrictJSON(w, r, 8<<10, dst)
}

func loadIdempotentActionResponseTx(tx *sql.Tx, operation, key, fingerprint string) ([]byte, bool, error) {
	var storedFingerprint, responseJSON string
	err := tx.QueryRow(`SELECT fingerprint, response_json FROM action_mutation_requests WHERE operation = ? AND idempotency_key = ?`, operation, key).Scan(&storedFingerprint, &responseJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if storedFingerprint != fingerprint {
		return nil, false, errActionIdempotencyConflict
	}
	return []byte(responseJSON), true, nil
}

func storeIdempotentActionResponseTx(tx *sql.Tx, operation, key, fingerprint string, response any) ([]byte, error) {
	raw, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`INSERT INTO action_mutation_requests(operation,idempotency_key,fingerprint,response_json,created_at) VALUES(?,?,?,?,?)`,
		operation, key, fingerprint, raw, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`DELETE FROM action_mutation_requests WHERE rowid IN (
SELECT rowid FROM action_mutation_requests ORDER BY created_at DESC LIMIT -1 OFFSET ?)`, maxActionMutationRequests)
	return raw, err
}

func withActionReplay(raw []byte) []byte {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	value["replayed"] = true
	result, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return result
}

func writeActionWorkflowResponse(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func actionWorkflowError(w http.ResponseWriter, status int, code, message string, revision int) {
	value := map[string]any{"error": message, "code": code}
	if revision > 0 {
		value["revision"] = revision
	}
	app.WriteJSON(w, status, value)
}

func actionWorkflowFingerprint(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func actionBaseByIDTx(tx *sql.Tx, id string) (Action, error) {
	var action Action
	var sourceIDs string
	err := tx.QueryRow(`SELECT id, task, owner, due_at, source_ids, status, confirmation_status, priority FROM actions WHERE id = ?`, id).Scan(
		&action.ID, &action.Task, &action.Owner, &action.DueAt, &sourceIDs, &action.Status, &action.ConfirmationStatus, &action.Priority)
	if err != nil {
		return Action{}, err
	}
	_ = json.Unmarshal([]byte(sourceIDs), &action.SourceIDs)
	return action, nil
}

func actionRevisionConflict(w http.ResponseWriter, row actionWorkflowRow) {
	actionWorkflowError(w, http.StatusConflict, "stale_revision", errActionStaleRevision.Error(), row.Revision)
}

func actionFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	key, err := actionIdempotencyKey(r)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var req actionFeedbackRequest
	if err := decodeActionWorkflowJSON(w, r, &req); err != nil || !validActionWorkflowID(req.ActionID) || req.ExpectedRevision < 1 || !validActionConfirmationStatus(req.ConfirmationStatus) {
		app.UserErr(w, http.StatusBadRequest, "行动反馈格式不合法")
		return
	}
	if req.Task != nil && !validText(*req.Task, 800, true) || req.Owner != nil && !validText(*req.Owner, 120, true) || req.DueAt != nil && !validText(*req.DueAt, 80, false) || req.Priority != nil && !validActionPriority(*req.Priority) {
		app.UserErr(w, http.StatusBadRequest, "行动反馈字段不合法")
		return
	}
	fingerprint := actionWorkflowFingerprint(req)
	actionWorkflowMu.Lock()
	defer actionWorkflowMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if cached, replayed, lookupErr := loadIdempotentActionResponseTx(tx, "action.feedback", key, fingerprint); lookupErr != nil {
		if errors.Is(lookupErr, errActionIdempotencyConflict) {
			actionWorkflowError(w, http.StatusConflict, "idempotency_conflict", lookupErr.Error(), 0)
		} else {
			app.HTTPErr(w, lookupErr)
		}
		return
	} else if replayed {
		writeActionWorkflowResponse(w, http.StatusOK, cached)
		return
	}
	action, err := actionBaseByIDTx(tx, req.ActionID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "行动不存在")
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	row, err := actionWorkflowForAgentTx(tx, action)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if row.Revision != req.ExpectedRevision {
		actionRevisionConflict(w, row)
		return
	}
	if (row.ScheduleState == actionSchedulePending || row.ScheduleState == actionScheduleScheduled) && req.ConfirmationStatus != actionConfirmationAccepted {
		actionWorkflowError(w, http.StatusConflict, "scheduled_action_locked", errActionStateConflict.Error(), row.Revision)
		return
	}
	if req.Task != nil {
		row.HasTaskOverride, row.TaskOverride = true, *req.Task
	}
	if req.Owner != nil {
		row.HasOwnerOverride, row.OwnerOverride = true, *req.Owner
	}
	if req.DueAt != nil {
		row.HasDueAtOverride, row.DueAtOverride = true, *req.DueAt
	}
	if req.Priority != nil {
		row.HasPriorityOverride, row.PriorityOverride = true, *req.Priority
	}
	row.UserEdited = true
	row.ConfirmationStatus = req.ConfirmationStatus
	if req.ConfirmationStatus == actionConfirmationRejected {
		row.ScheduleState = actionScheduleNone
		row.RequestID, row.CandidateID, row.ClientRequestID, row.JobID, row.NextRunAt = "", "", "", "", ""
		row.Replayed, row.ErrorCode, row.ErrorSummary = false, "", ""
	}
	row.Revision++
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	effective := overlayAction(action, row)
	if _, err = tx.Exec(`UPDATE actions SET task=?, owner=?, due_at=?, status=?, confirmation_status=?, priority=? WHERE id=?`, effective.Task, effective.Owner, effective.DueAt, effective.Status, effective.ConfirmationStatus, effective.Priority, effective.ID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = updateActionWorkflowTx(tx, row); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if req.ConfirmationStatus == actionConfirmationAccepted {
		if _, err = setStableContextStateTx(tx, "action:"+action.ID, contextStateActive, contextAddedByAction); err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else {
		if _, err = setStableContextStateTx(tx, "action:"+action.ID, contextStateRemoved, contextAddedByAction); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	result := actionFeedbackResult{ActionID: action.ID, Revision: row.Revision, ConfirmationStatus: row.ConfirmationStatus, ScheduleState: row.ScheduleState}
	if _, err = storeIdempotentActionResponseTx(tx, "action.feedback", key, fingerprint, result); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	raw, _ := json.Marshal(result)
	writeActionWorkflowResponse(w, http.StatusOK, raw)
}

func actionScheduleBeginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	key, err := actionIdempotencyKey(r)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var req actionScheduleBeginRequest
	if err := decodeActionWorkflowJSON(w, r, &req); err != nil || !validActionWorkflowID(req.ActionID) || req.ExpectedRevision < 1 || !validActionWorkflowID(req.RequestID) || !validActionWorkflowID(req.CandidateID) || !validActionWorkflowID(req.ClientRequestID) {
		app.UserErr(w, http.StatusBadRequest, "调度候选格式不合法")
		return
	}
	fingerprint := actionWorkflowFingerprint(req)
	actionWorkflowMu.Lock()
	defer actionWorkflowMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if cached, replayed, lookupErr := loadIdempotentActionResponseTx(tx, "action.schedule.begin", key, fingerprint); lookupErr != nil {
		if errors.Is(lookupErr, errActionIdempotencyConflict) {
			actionWorkflowError(w, http.StatusConflict, "idempotency_conflict", lookupErr.Error(), 0)
		} else {
			app.HTTPErr(w, lookupErr)
		}
		return
	} else if replayed {
		writeActionWorkflowResponse(w, http.StatusOK, withActionReplay(cached))
		return
	}
	action, err := actionBaseByIDTx(tx, req.ActionID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "行动不存在")
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	row, err := actionWorkflowForAgentTx(tx, action)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if row.ScheduleState == actionSchedulePending && row.RequestID == req.RequestID && row.CandidateID == req.CandidateID && row.ClientRequestID == req.ClientRequestID {
		result := actionScheduleBeginResult{ActionID: action.ID, Revision: row.Revision, ScheduleState: row.ScheduleState, RequestID: row.RequestID, CandidateID: row.CandidateID, ClientRequestID: row.ClientRequestID, Replayed: true}
		raw, _ := json.Marshal(result)
		if _, err = storeIdempotentActionResponseTx(tx, "action.schedule.begin", key, fingerprint, result); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		writeActionWorkflowResponse(w, http.StatusOK, raw)
		return
	}
	if row.Revision != req.ExpectedRevision {
		actionRevisionConflict(w, row)
		return
	}
	if row.ConfirmationStatus != actionConfirmationAccepted || (row.ScheduleState != actionScheduleNone && row.ScheduleState != actionScheduleFailed) {
		actionWorkflowError(w, http.StatusConflict, "action_not_schedulable", errActionStateConflict.Error(), row.Revision)
		return
	}
	row.ScheduleState = actionSchedulePending
	row.RequestID, row.CandidateID, row.ClientRequestID = req.RequestID, req.CandidateID, req.ClientRequestID
	row.JobID, row.NextRunAt, row.ErrorCode, row.ErrorSummary = "", "", "", ""
	row.Replayed = false
	row.UserEdited = true
	row.Revision++
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = updateActionWorkflowTx(tx, row); err != nil {
		app.HTTPErr(w, err)
		return
	}
	result := actionScheduleBeginResult{ActionID: action.ID, Revision: row.Revision, ScheduleState: row.ScheduleState, RequestID: row.RequestID, CandidateID: row.CandidateID, ClientRequestID: row.ClientRequestID}
	if _, err = storeIdempotentActionResponseTx(tx, "action.schedule.begin", key, fingerprint, result); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	raw, _ := json.Marshal(result)
	writeActionWorkflowResponse(w, http.StatusOK, raw)
}

func actionScheduleReceiptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	if r.Header.Get(trustedHostOperationHeader) != trustedHostOperationValue {
		app.UserErr(w, http.StatusForbidden, "调度回执仅允许可信宿主提交")
		return
	}
	key, err := actionIdempotencyKey(r)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var req actionScheduleReceiptRequest
	if err := decodeActionWorkflowJSON(w, r, &req); err != nil || !validActionWorkflowID(req.ActionID) || req.ExpectedRevision < 1 || !validActionWorkflowID(req.RequestID) || !validActionWorkflowID(req.CandidateID) || !validActionWorkflowID(req.ClientRequestID) || (req.Status != actionScheduleScheduled && req.Status != actionScheduleFailed) {
		app.UserErr(w, http.StatusBadRequest, "调度回执格式不合法")
		return
	}
	if req.Status == actionScheduleScheduled && !validActionWorkflowID(req.JobID) || req.Status == actionScheduleFailed && !validText(req.ErrorCode, 64, true) {
		app.UserErr(w, http.StatusBadRequest, "调度回执字段不合法")
		return
	}
	if !validText(req.NextRunAt, 80, false) || req.NextRunAt != "" && func() bool { _, e := time.Parse(time.RFC3339, req.NextRunAt); return e != nil }() || !validText(req.ErrorSummary, 240, false) {
		app.UserErr(w, http.StatusBadRequest, "调度回执字段不合法")
		return
	}
	fingerprint := actionWorkflowFingerprint(req)
	actionWorkflowMu.Lock()
	defer actionWorkflowMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if cached, replayed, lookupErr := loadIdempotentActionResponseTx(tx, "action.schedule.receipt", key, fingerprint); lookupErr != nil {
		if errors.Is(lookupErr, errActionIdempotencyConflict) {
			actionWorkflowError(w, http.StatusConflict, "idempotency_conflict", lookupErr.Error(), 0)
		} else {
			app.HTTPErr(w, lookupErr)
		}
		return
	} else if replayed {
		writeActionWorkflowResponse(w, http.StatusOK, withActionReplay(cached))
		return
	}
	action, err := actionBaseByIDTx(tx, req.ActionID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "行动不存在")
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	row, err := actionWorkflowForAgentTx(tx, action)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if row.ScheduleState != actionSchedulePending && row.RequestID == req.RequestID && row.CandidateID == req.CandidateID && row.ClientRequestID == req.ClientRequestID && ((row.ScheduleState == actionScheduleScheduled && req.Status == actionScheduleScheduled && row.JobID == req.JobID) || (row.ScheduleState == actionScheduleFailed && req.Status == actionScheduleFailed && row.ErrorCode == req.ErrorCode)) {
		result := actionScheduleReceiptResult{ActionID: action.ID, Revision: row.Revision, ConfirmationStatus: row.ConfirmationStatus, ScheduleState: row.ScheduleState, RequestID: row.RequestID, CandidateID: row.CandidateID, ClientRequestID: row.ClientRequestID, JobID: row.JobID, Replayed: true, NextRunAt: row.NextRunAt, ErrorCode: row.ErrorCode, ErrorSummary: row.ErrorSummary}
		raw, _ := json.Marshal(result)
		if _, err = storeIdempotentActionResponseTx(tx, "action.schedule.receipt", key, fingerprint, result); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		writeActionWorkflowResponse(w, http.StatusOK, raw)
		return
	}
	if row.Revision != req.ExpectedRevision {
		actionRevisionConflict(w, row)
		return
	}
	if row.ConfirmationStatus != actionConfirmationAccepted || row.ScheduleState != actionSchedulePending || row.RequestID != req.RequestID || row.CandidateID != req.CandidateID || row.ClientRequestID != req.ClientRequestID {
		actionWorkflowError(w, http.StatusConflict, "schedule_receipt_conflict", errActionStateConflict.Error(), row.Revision)
		return
	}
	row.ScheduleState = req.Status
	row.JobID, row.NextRunAt, row.ErrorCode, row.ErrorSummary = req.JobID, req.NextRunAt, req.ErrorCode, req.ErrorSummary
	row.Replayed = req.Replayed
	row.Revision++
	row.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err = updateActionWorkflowTx(tx, row); err != nil {
		app.HTTPErr(w, err)
		return
	}
	result := actionScheduleReceiptResult{ActionID: action.ID, Revision: row.Revision, ConfirmationStatus: row.ConfirmationStatus, ScheduleState: row.ScheduleState, RequestID: row.RequestID, CandidateID: row.CandidateID, ClientRequestID: row.ClientRequestID, JobID: row.JobID, NextRunAt: row.NextRunAt, ErrorCode: row.ErrorCode, ErrorSummary: row.ErrorSummary}
	if _, err = storeIdempotentActionResponseTx(tx, "action.schedule.receipt", key, fingerprint, result); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	raw, _ := json.Marshal(result)
	writeActionWorkflowResponse(w, http.StatusOK, raw)
}

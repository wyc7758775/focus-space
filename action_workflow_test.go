package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIdempotencyKeyAcceptsBrowserAndInternalHeaders(t *testing.T) {
	for _, header := range []string{"Idempotency-Key", "X-Zettlab-Idempotency-Key"} {
		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		req.Header.Set(header, "focus-link-browser-1")
		if got := requestIdempotencyKey(req); got != "focus-link-browser-1" {
			t.Fatalf("%s key=%q", header, got)
		}
	}
}

func callActionJSON(t *testing.T, handler http.HandlerFunc, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("X-Zettlab-Idempotency-Key", key)
	}
	result := httptest.NewRecorder()
	handler(result, req)
	return result
}

func callTrustedActionJSON(t *testing.T, handler http.HandlerFunc, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("X-Zettlab-Idempotency-Key", key)
	}
	req.Header.Set(trustedHostOperationHeader, trustedHostOperationValue)
	result := httptest.NewRecorder()
	handler(result, req)
	return result
}

func insertWorkflowAction(t *testing.T, conn *sql.DB, id, task string) {
	t.Helper()
	_, err := conn.Exec(`
INSERT INTO actions(id,task,owner,due_at,source_ids,status,confirmation_status,priority)
VALUES(?,?,?,'',?,'draft','draft','medium')`, id, task, "负责人待确认", `[]`)
	if err != nil {
		t.Fatal(err)
	}
}

func workflowDashboardPayload(t *testing.T, action Action, receipts []ReaderDigestReceipt) string {
	t.Helper()
	actions := []Action{}
	if action.ID != "" {
		actions = []Action{action}
	}
	payload := Dashboard{
		SchemaVersion: 1,
		GeneratedAt:   "2026-08-10T12:00:00Z",
		Source:        SourceStatus{Kind: "mixed", Status: "ok", RecordsSeen: 1, RecordsUsed: 1, LastReadAt: "2026-08-10T12:00:00Z"},
		Actions:       actions,
		Evidence:      []Evidence{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{},
		ReaderDigests: receipts,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestActionWorkflowUnitRejectsMissingKeyAndUnknownFields(t *testing.T) {
	db = openTestDB(t)
	insertWorkflowAction(t, db, "A-UNIT", "确认范围")
	missing := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", `{"action_id":"A-UNIT","expected_revision":1,"confirmation_status":"accepted"}`, "")
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status=%d body=%s", missing.Code, missing.Body.String())
	}
	unknown := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", `{"action_id":"A-UNIT","expected_revision":1,"confirmation_status":"accepted","token":"x"}`, "feedback-unknown")
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestActionScheduleReceiptUnitRejectsUntrustedHost(t *testing.T) {
	db = openTestDB(t)
	for _, headerValue := range []string{"", "1", "true", "2"} {
		req := httptest.NewRequest(http.MethodPost, "/api/actions/schedule/receipt", strings.NewReader(`{}`))
		req.Header.Set("X-Zettlab-Idempotency-Key", "receipt-untrusted")
		if headerValue != "" {
			req.Header.Set(trustedHostOperationHeader, headerValue)
		}
		result := httptest.NewRecorder()
		actionScheduleReceiptHandler(result, req)
		if result.Code != http.StatusForbidden {
			t.Fatalf("trusted host header %q status=%d body=%s", headerValue, result.Code, result.Body.String())
		}
	}
}

func TestActionWorkflowFlowAcceptEditScheduleAndReplay(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-workflow", "profile-workflow")
	insertWorkflowAction(t, db, "A-FLOW", "Agent 草案")

	acceptedBody := `{"action_id":"A-FLOW","expected_revision":1,"confirmation_status":"accepted","task":"用户确认后的行动","owner":"产品组","due_at":"2026-08-20T09:00:00Z","priority":"high"}`
	accepted := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", acceptedBody, "feedback-a-flow")
	if accepted.Code != http.StatusOK || !strings.Contains(accepted.Body.String(), `"revision":2`) || !strings.Contains(accepted.Body.String(), `"confirmation_status":"accepted"`) {
		t.Fatalf("accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var contextState, contextSnapshot string
	if err := db.QueryRow(`SELECT state, content_snapshot FROM stable_context_items WHERE item_key='action:A-FLOW'`).Scan(&contextState, &contextSnapshot); err != nil {
		t.Fatal(err)
	}
	if contextState != contextStateActive || !strings.Contains(contextSnapshot, "用户确认后的行动") {
		t.Fatalf("accepted action was not atomically added to context: state=%s snapshot=%s", contextState, contextSnapshot)
	}
	repeat := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", acceptedBody, "feedback-a-flow")
	if repeat.Code != http.StatusOK || repeat.Body.String() != accepted.Body.String() {
		t.Fatalf("same-key replay changed response: first=%s repeat=%s", accepted.Body.String(), repeat.Body.String())
	}
	conflict := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", strings.Replace(acceptedBody, "用户确认后的行动", "冲突修改", 1), "feedback-a-flow")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("same key with different payload status=%d body=%s", conflict.Code, conflict.Body.String())
	}

	beginBody := `{"action_id":"A-FLOW","expected_revision":2,"request_id":"req-flow","candidate_id":"A-FLOW","client_request_id":"client-flow"}`
	begin := callActionJSON(t, actionScheduleBeginHandler, http.MethodPost, "/api/actions/schedule/begin", beginBody, "begin-a-flow")
	if begin.Code != http.StatusOK || !strings.Contains(begin.Body.String(), `"revision":3`) || !strings.Contains(begin.Body.String(), `"schedule_state":"pending"`) {
		t.Fatalf("begin status=%d body=%s", begin.Code, begin.Body.String())
	}
	replayBegin := callActionJSON(t, actionScheduleBeginHandler, http.MethodPost, "/api/actions/schedule/begin", beginBody, "begin-a-flow")
	if replayBegin.Code != http.StatusOK || !strings.Contains(replayBegin.Body.String(), `"replayed":true`) {
		t.Fatalf("begin replay status=%d body=%s", replayBegin.Code, replayBegin.Body.String())
	}
	receiptBody := `{"action_id":"A-FLOW","expected_revision":3,"status":"scheduled","request_id":"req-flow","candidate_id":"A-FLOW","client_request_id":"client-flow","job_id":"job-flow","next_run_at":"2026-08-11T08:00:00Z"}`
	publicReceipt := callActionJSON(t, actionScheduleReceiptHandler, http.MethodPost, "/api/actions/schedule/receipt", receiptBody, "receipt-a-flow")
	if publicReceipt.Code != http.StatusForbidden {
		t.Fatalf("public receipt status=%d body=%s", publicReceipt.Code, publicReceipt.Body.String())
	}
	var pendingState string
	var pendingRevision int
	if err := db.QueryRow(`SELECT schedule_state, revision FROM action_workflows WHERE action_id='A-FLOW'`).Scan(&pendingState, &pendingRevision); err != nil {
		t.Fatal(err)
	}
	if pendingState != actionSchedulePending || pendingRevision != 3 {
		t.Fatalf("public receipt changed workflow: state=%s revision=%d", pendingState, pendingRevision)
	}
	receipt := callTrustedActionJSON(t, actionScheduleReceiptHandler, http.MethodPost, "/api/actions/schedule/receipt", receiptBody, "receipt-a-flow")
	if receipt.Code != http.StatusOK || !strings.Contains(receipt.Body.String(), `"revision":4`) || !strings.Contains(receipt.Body.String(), `"schedule_state":"scheduled"`) {
		t.Fatalf("receipt status=%d body=%s", receipt.Code, receipt.Body.String())
	}
	replayReceipt := callTrustedActionJSON(t, actionScheduleReceiptHandler, http.MethodPost, "/api/actions/schedule/receipt", receiptBody, "receipt-a-flow")
	if replayReceipt.Code != http.StatusOK || !strings.Contains(replayReceipt.Body.String(), `"replayed":true`) {
		t.Fatalf("receipt replay status=%d body=%s", replayReceipt.Code, replayReceipt.Body.String())
	}
	stale := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", `{"action_id":"A-FLOW","expected_revision":1,"confirmation_status":"accepted"}`, "feedback-stale")
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "stale_revision") {
		t.Fatalf("stale feedback status=%d body=%s", stale.Code, stale.Body.String())
	}

	// A later Agent refresh cannot replace the accepted edit or the schedule receipt.
	refresh := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{ID: "A-FLOW", Task: "Agent 覆盖尝试", Owner: "Agent", Status: "draft", ConfirmationStatus: "draft", Priority: "low"}, nil))
	if refresh.Code != http.StatusOK {
		t.Fatalf("agent refresh status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	get := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"task":"用户确认后的行动"`) || !strings.Contains(get.Body.String(), `"confirmation_status":"accepted"`) || !strings.Contains(get.Body.String(), `"schedule_state":"scheduled"`) || !strings.Contains(get.Body.String(), `"job_id":"job-flow"`) {
		t.Fatalf("user state was overwritten by refresh: %s", get.Body.String())
	}
}

func TestActionWorkflowRejectDoesNotCreateStableContextFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-reject", "profile-reject")
	insertWorkflowAction(t, db, "A-REJECT", "Agent 草案")

	rejected := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", `{"action_id":"A-REJECT","expected_revision":1,"confirmation_status":"rejected","task":"用户拒绝的草案","owner":"产品组","due_at":"","priority":"medium"}`, "feedback-a-reject")
	if rejected.Code != http.StatusOK || !strings.Contains(rejected.Body.String(), `"confirmation_status":"rejected"`) {
		t.Fatalf("reject status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	var contextRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM stable_context_items WHERE item_key='action:A-REJECT'`).Scan(&contextRows); err != nil {
		t.Fatal(err)
	}
	if contextRows != 0 {
		t.Fatalf("rejected action entered stable context: rows=%d", contextRows)
	}

	refresh := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{ID: "A-REJECT", Task: "Agent 覆盖尝试", Owner: "Agent", Status: "draft", ConfirmationStatus: "draft", Priority: "low"}, nil))
	if refresh.Code != http.StatusOK {
		t.Fatalf("agent refresh status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	get := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"task":"用户拒绝的草案"`) || !strings.Contains(get.Body.String(), `"confirmation_status":"rejected"`) {
		t.Fatalf("rejected user state was overwritten: %s", get.Body.String())
	}
}

func TestActionWorkflowAcceptedWithoutFieldEditSurvivesAgentRefreshFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-accepted-no-edit", "profile-accepted-no-edit")
	insertWorkflowAction(t, db, "A-ACCEPTED-NO-EDIT", "原始行动")

	accepted := callActionJSON(t, actionFeedbackHandler, http.MethodPost, "/api/actions/feedback", `{"action_id":"A-ACCEPTED-NO-EDIT","expected_revision":1,"confirmation_status":"accepted"}`, "feedback-accepted-no-edit")
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	refresh := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{ID: "A-ACCEPTED-NO-EDIT", Task: "Agent 新草案", Owner: "Agent", Status: "draft", ConfirmationStatus: "draft", Priority: "low"}, nil))
	if refresh.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	get := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"task":"原始行动"`) || !strings.Contains(get.Body.String(), `"confirmation_status":"accepted"`) {
		t.Fatalf("accepted projection was overwritten: %s", get.Body.String())
	}
}

func TestReaderDigestReceiptIsPersistentAndBoundedFlow(t *testing.T) {
	db = openTestDB(t)
	receipt := ReaderDigestReceipt{ContentHash: strings.Repeat("a", 64), SourceID: "feed-a", EntryID: "entry-a", ProcessedAt: "2026-08-10T12:00:00Z", Outcome: "no_candidate"}
	first := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{}, []ReaderDigestReceipt{receipt}))
	if first.Code != http.StatusOK {
		t.Fatalf("digest write status=%d body=%s", first.Code, first.Body.String())
	}
	second := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{}, nil))
	if second.Code != http.StatusOK {
		t.Fatalf("empty refresh status=%d body=%s", second.Code, second.Body.String())
	}
	get := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), receipt.ContentHash) {
		t.Fatalf("digest receipt was lost after refresh: %s", get.Body.String())
	}
	duplicate := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{}, []ReaderDigestReceipt{receipt, receipt}))
	if duplicate.Code != http.StatusBadRequest {
		t.Fatalf("duplicate digest receipt status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestActionWorkflowSidecarIsBoundedFlow(t *testing.T) {
	db = openTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxActionWorkflowRows+2; index++ {
		row := actionWorkflowRow{
			ActionID:           fmt.Sprintf("A-OLD-%04d", index),
			Revision:           1,
			ConfirmationStatus: actionConfirmationDraft,
			ScheduleState:      actionScheduleNone,
			UpdatedAt:          fmt.Sprintf("2026-08-01T00:%02d:%02dZ", (index/60)%60, index%60),
		}
		if err := insertActionWorkflowTx(tx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	refresh := callJSON(t, dashboardHandler, http.MethodPost, "/api/dashboard", workflowDashboardPayload(t, Action{ID: "A-CURRENT", Task: "当前草案", Owner: "负责人待确认", Status: "draft", ConfirmationStatus: "draft", Priority: "medium"}, nil))
	if refresh.Code != http.StatusOK {
		t.Fatalf("dashboard refresh status=%d body=%s", refresh.Code, refresh.Body.String())
	}
	var count, current int
	if err := db.QueryRow(`SELECT COUNT(*) FROM action_workflows`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM action_workflows WHERE action_id='A-CURRENT'`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if count != maxActionWorkflowRows || current != 1 {
		t.Fatalf("sidecar bound/current row mismatch: count=%d current=%d", count, current)
	}
}

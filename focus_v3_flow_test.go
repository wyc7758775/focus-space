package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"focus-space/internal/bridges"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func decodeDecisionRunFlow(t *testing.T, response *httptest.ResponseRecorder) decisionRunResponse {
	t.Helper()
	var decoded decisionRunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode decision run response: %v body=%s", err, response.Body.String())
	}
	return decoded
}

func appDataMutationFlow(t *testing.T, operation string, payload any, key string) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return appDataCall(t, "/api/app-data/v1/operations/"+operation, string(raw), key)
}

func stringPointer(value string) *string {
	return &value
}

func TestFocusV3DecisionRunCASIdempotencyAndScopeFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-run", "profile-focus-v3-run")
	focus := createFocusFlowSpace(t, "选择下一季度产品方向")

	startPayload := decisionRunStartRequest{
		SchemaVersion:         decisionRunSchemaVersion,
		FocusID:               focus.ID,
		ExpectedFocusRevision: focus.Revision,
		DecisionMode:          "guided",
	}
	started := appDataMutationFlow(t, "decision_run.start", startPayload, "focus-v3-run-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	if run.Run == nil || run.Run.Status != "waiting_clarification" || run.CurrentQuestion == nil || run.Progress.Total != 1 {
		t.Fatalf("unexpected started run: %#v", run)
	}

	replay := appDataMutationFlow(t, "decision_run.start", startPayload, "focus-v3-run-start")
	if replay.Code != started.Code || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || replay.Body.String() != started.Body.String() {
		t.Fatalf("start replay status=%d replay=%q body=%s", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String())
	}

	answerPayload := decisionRunAnswerRequest{
		SchemaVersion:          decisionRunSchemaVersion,
		RunID:                  run.Run.ID,
		QuestionID:             run.CurrentQuestion.ID,
		ExpectedRunRevision:    run.Run.Revision,
		ExpectedAnswerRevision: 0,
		Answer: decisionAnswerInput{
			Type:  "text",
			Value: json.RawMessage(`"在本季度内决定一条主线"`),
		},
	}
	answered := appDataMutationFlow(t, "decision_run.answer", answerPayload, "focus-v3-run-answer")
	if answered.Code != http.StatusOK {
		t.Fatalf("answer status=%d body=%s", answered.Code, answered.Body.String())
	}
	answeredRun := decodeDecisionRunFlow(t, answered)
	if answeredRun.Run == nil || answeredRun.Run.Status != "collecting_context" || answeredRun.Run.Revision != run.Run.Revision+1 || answeredRun.Progress.Percent != 100 {
		t.Fatalf("unexpected answered run: %#v", answeredRun)
	}
	staleAnswer := appDataMutationFlow(t, "decision_run.answer", answerPayload, "focus-v3-run-answer-stale")
	if staleAnswer.Code != http.StatusConflict || !strings.Contains(staleAnswer.Body.String(), "run_revision_conflict") {
		t.Fatalf("stale answer was not rejected: status=%d body=%s", staleAnswer.Code, staleAnswer.Body.String())
	}

	updatedGoal := "在本季度内明确产品主线"
	updated := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
		SchemaVersion: focusSpaceSchemaVersion, Action: "update", FocusID: focus.ID,
		ExpectedRevision: focus.Revision, Goal: &updatedGoal,
	}, "focus-v3-run-stale-focus")
	if updated.Code != http.StatusOK {
		t.Fatalf("update focus status=%d body=%s", updated.Code, updated.Body.String())
	}
	staleRun := callFocusFlowJSON(t, decisionRunGetHandler, http.MethodGet, "/api/internal/decision-runs?run_id="+run.Run.ID, nil, "")
	if staleRun.Code != http.StatusOK {
		t.Fatalf("get stale run status=%d body=%s", staleRun.Code, staleRun.Body.String())
	}
	staleRunState := decodeDecisionRunFlow(t, staleRun)
	if staleRunState.Run == nil || staleRunState.Run.Status != "superseded" || staleRunState.Run.ErrorCode != "stale_focus_revision" {
		t.Fatalf("focus mutation did not supersede decision run: %#v", staleRunState)
	}

	previousScope := appDataScope
	appDataScope = dataScope{OwnerID: "owner-focus-v3-other", ProfileID: "profile-focus-v3-other"}
	isolated := callFocusFlowJSON(t, decisionRunGetHandler, http.MethodGet, "/api/internal/decision-runs?run_id="+run.Run.ID, nil, "")
	appDataScope = previousScope
	if isolated.Code != http.StatusNotFound {
		t.Fatalf("cross-owner run read status=%d body=%s", isolated.Code, isolated.Body.String())
	}
}

func TestFocusV3ExpiredAnalysisClaimMakesDecisionRetryable(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-expired", "profile-focus-v3-expired")
	focus := createFocusFlowSpace(t, "恢复 AI Computer 的超时分析")
	insertFocusFlowMeeting(t, "meeting-focus-v3-expired", "需要保留一条可验证的 AI Computer 行动路径。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.RunID == "" || claim.Status != "claimed" {
		t.Fatalf("unexpected analysis claim: %#v", claim)
	}
	if _, err := db.Exec(`UPDATE focus_analysis_runs SET expires_at=? WHERE id=?`, "2020-01-01T00:00:00Z", claim.RunID); err != nil {
		t.Fatal(err)
	}
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
		AnalysisRunID: claim.RunID, SkipClarification: true,
	}, "focus-v3-expired-start")
	run := decodeDecisionRunFlow(t, started)
	if started.Code != http.StatusCreated || run.Run == nil {
		t.Fatalf("start expired flow status=%d body=%s", started.Code, started.Body.String())
	}
	got := callFocusFlowJSON(t, decisionRunGetHandler, http.MethodGet, "/api/internal/decision-runs?run_id="+run.Run.ID, nil, "")
	state := decodeDecisionRunFlow(t, got)
	if got.Code != http.StatusOK || state.Run == nil || state.Run.Status != "failed_retryable" || !state.CanResume || state.Run.ErrorCode != "focus_analysis_claim_expired" {
		t.Fatalf("expired claim was not made retryable: status=%d body=%s state=%#v", got.Code, got.Body.String(), state)
	}
	var analysisStatus, analysisError string
	if err := db.QueryRow(`SELECT status,error_code FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&analysisStatus, &analysisError); err != nil {
		t.Fatal(err)
	}
	if analysisStatus != "failed" || analysisError != "claim_expired" {
		t.Fatalf("expired analysis claim was not fenced: status=%q error=%q", analysisStatus, analysisError)
	}
}

func TestFocusV3TerminalDecisionFailureReleasesAnalysisClaim(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-terminal-release", "profile-focus-v3-terminal-release")
	focus := createFocusFlowSpace(t, "模型不可用时释放焦点分析占用")
	insertFocusFlowMeeting(t, "meeting-focus-v3-terminal-release", "这条依据足以进入 Focus 逻辑链，但设备当前没有可用模型。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.RunID == "" || claim.Status != "claimed" {
		t.Fatalf("unexpected terminal-release claim: %#v", claim)
	}

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
		AnalysisRunID: claim.RunID, SkipClarification: true,
	}, "focus-v3-terminal-release-start")
	run := decodeDecisionRunFlow(t, started)
	if started.Code != http.StatusCreated || run.Run == nil || run.Run.Status != "queued" {
		t.Fatalf("start terminal-release flow status=%d body=%s run=%#v", started.Code, started.Body.String(), run)
	}

	failed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		ExpectedRevision: run.Run.Revision, Status: "failed_terminal",
		AnalysisRunID: claim.RunID, ErrorCode: "model_not_configured",
		ErrorMessage: "智能分析无法继续，请检查相关配置后重新发起",
	}, "focus-v3-terminal-release-failed")
	failedRun := decodeDecisionRunFlow(t, failed)
	if failed.Code != http.StatusOK || failedRun.Run == nil || failedRun.Run.Status != "failed_terminal" || failedRun.Run.ErrorCode != "model_not_configured" {
		t.Fatalf("terminal failure status=%d body=%s run=%#v", failed.Code, failed.Body.String(), failedRun)
	}

	var analysisStatus, analysisError, expiresAt string
	if err := db.QueryRow(`SELECT status,error_code,expires_at FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&analysisStatus, &analysisError, &expiresAt); err != nil {
		t.Fatal(err)
	}
	if analysisStatus != "failed" || analysisError != "model_not_configured" || expiresAt != "" {
		t.Fatalf("terminal failure did not release analysis claim: status=%q error=%q expires_at=%q", analysisStatus, analysisError, expiresAt)
	}
}

func TestFocusV3DecisionCanCompleteWithoutActionFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-action-required", "profile-focus-v3-action-required")
	focus := createFocusFlowSpace(t, "将证据转成可执行的 AI Computer 路线")
	insertFocusFlowMeeting(t, "meeting-focus-v3-action-required-a", "团队确认需要优先保证离线稳定性。", "2026-08-15T09:30:00Z")
	insertFocusFlowMeeting(t, "meeting-focus-v3-action-required-b", "客户访谈确认断网时仍需继续使用核心能力。", "2026-08-16T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.RunID == "" || len(claim.Evidence) != 2 {
		t.Fatalf("unexpected action-required claim: %#v", claim)
	}
	applyRequest := focusFlowApplyRequest(claim, "unused-action-required-signal")
	applyRequest.Signals = nil
	applyRequest.Actions = nil
	applyRequest.Decisions = []focusDecisionInput{{
		ID: "decision-action-required", Question: "是否采用本地优先路线?",
		Options: []string{"本地优先", "云端优先"}, Recommendation: "采用本地优先路线",
		ExpectedOutcome: "离线场景可持续使用", SourceIDs: []string{claim.Evidence[0].ID},
		SupportingEvidence: []string{"两次独立会议均明确要求离线稳定性"},
		Confidence:         "high", Freshness: "current", ValidationItems: []string{"验证离线启动"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-action-required-apply")
	var analysis focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 {
		t.Fatalf("action-required apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "guided", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID, SkipClarification: true,
	}, "focus-v3-action-required-start")
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
		}, "focus-v3-action-required-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	completed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "completed", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
	}, "focus-v3-action-required-complete")
	completedRun := decodeDecisionRunFlow(t, completed)
	if completed.Code != http.StatusOK || completedRun.Run == nil || completedRun.Run.Status != "completed" {
		t.Fatalf("decision-only result did not complete: status=%d body=%s", completed.Code, completed.Body.String())
	}
}

func TestFocusV3AnsweredClarificationCannotBeRepeatedWithNewIDFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-repeat-question", "profile-focus-v3-repeat-question")
	focus := createFocusFlowSpace(t, "在 AI Computer 冲突依据中选择端侧或云端路线")
	insertFocusFlowMeeting(t, "meeting-focus-v3-repeat-question", "端侧离线能力与云端能力存在取舍。", "2026-08-15T09:30:00Z")
	insertFocusFlowMeeting(t, "meeting-focus-v3-repeat-question-cloud", "云端能力更完整，但离线时不可用。", "2026-08-16T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.RunID == "" || len(claim.Evidence) != 2 {
		t.Fatalf("unexpected repeated-question claim: %#v", claim)
	}

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
		AnalysisRunID: claim.RunID, SkipClarification: true,
	}, "focus-v3-repeat-question-start")
	run := decodeDecisionRunFlow(t, started)
	if started.Code != http.StatusCreated || run.Run == nil || run.Run.Status != "queued" {
		t.Fatalf("start repeated-question flow status=%d run=%#v", started.Code, run)
	}
	for index, status := range []string{"collecting_context", "running_ai"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
			ExpectedRevision: run.Run.Revision, Status: status, AnalysisRunID: claim.RunID,
		}, "focus-v3-repeat-question-pre-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	questionText := "现有依据分别支持端侧离线和云端能力，本轮优先采用哪一侧？"
	waiting := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		ExpectedRevision: run.Run.Revision, Status: "waiting_clarification", AnalysisRunID: claim.RunID,
		Question: &decisionQuestionInput{ID: "q_runtime_01", Type: "text", Question: questionText},
	}, "focus-v3-repeat-question-first")
	run = decodeDecisionRunFlow(t, waiting)
	if waiting.Code != http.StatusOK || run.CurrentQuestion == nil {
		t.Fatalf("first question status=%d body=%s", waiting.Code, waiting.Body.String())
	}
	answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		QuestionID: run.CurrentQuestion.ID, ExpectedRunRevision: run.Run.Revision, ExpectedAnswerRevision: 0,
		Answer: decisionAnswerInput{Type: "text", Value: json.RawMessage(`"优先采用端侧离线能力"`)},
	}, "focus-v3-repeat-question-answer")
	run = decodeDecisionRunFlow(t, answered)
	resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		ExpectedRevision: run.Run.Revision, Status: "running_ai", AnalysisRunID: claim.RunID,
	}, "focus-v3-repeat-question-running")
	run = decodeDecisionRunFlow(t, resumed)

	repeated := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		ExpectedRevision: run.Run.Revision, Status: "waiting_clarification", AnalysisRunID: claim.RunID,
		Question: &decisionQuestionInput{ID: "q_runtime_02", Type: "text", Question: "  现有依据分别支持端侧离线和云端能力，\n本轮优先采用哪一侧？  "},
	}, "focus-v3-repeat-question-second")
	if repeated.Code != http.StatusConflict || !strings.Contains(repeated.Body.String(), "question_repeated") || !strings.Contains(repeated.Body.String(), "已经回答") {
		t.Fatalf("semantic duplicate question was accepted: status=%d body=%s", repeated.Code, repeated.Body.String())
	}
	var questionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_decision_questions WHERE run_id=?`, run.Run.ID).Scan(&questionCount); err != nil {
		t.Fatal(err)
	}
	if questionCount != 1 {
		t.Fatalf("duplicate question changed history: count=%d", questionCount)
	}
}

func TestFocusV3EditedClarificationInvalidatesOldAnalysisFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-answer-edit", "profile-focus-v3-answer-edit")
	focus := createFocusFlowSpace(t, "选择端侧 AI 投入方向")
	insertFocusFlowMeeting(t, "meeting-focus-v3-answer-edit", "端侧推理能够降低交互延迟。", "2026-08-15T09:30:00Z")

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
	}, "focus-v3-answer-edit-start")
	run := decodeDecisionRunFlow(t, started)
	if started.Code != http.StatusCreated || run.CurrentQuestion == nil {
		t.Fatalf("start answer edit flow status=%d run=%#v", started.Code, run)
	}
	firstAnswer := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		QuestionID: run.CurrentQuestion.ID, ExpectedRunRevision: run.Run.Revision,
		ExpectedAnswerRevision: 0,
		Answer:                 decisionAnswerInput{Type: "text", Value: json.RawMessage(`"优先验证离线可用性"`)},
	}, "focus-v3-answer-edit-first")
	run = decodeDecisionRunFlow(t, firstAnswer)
	if firstAnswer.Code != http.StatusOK || run.Run.Status != "collecting_context" {
		t.Fatalf("first answer status=%d run=%#v", firstAnswer.Code, run)
	}

	claim := readFocusFlowClaim(t, focus.ID)
	applyRequest := focusFlowApplyRequest(claim, "signal-answer-edit")
	applyRequest.Signals = nil
	applyRequest.Decisions = []focusDecisionInput{{
		ID: "decision-answer-edit", Question: "是否优先投入端侧 AI？",
		Options: []string{"优先投入", "继续观察"}, Recommendation: "优先投入",
		ExpectedOutcome: "离线体验改善", SourceIDs: []string{claim.Evidence[0].ID},
		SupportingEvidence: []string{"会议支持低延迟价值"}, Confidence: "medium", Freshness: "current",
	}}
	applyRequest.Actions = []focusActionInput{{
		ID: "action-answer-edit", Task: "验证修改回答后的投入边界", Owner: "产品负责人",
		SourceIDs: []string{claim.Evidence[0].ID}, Priority: "medium",
		SupportingEvidence: []string{"根据当前回答验证投入边界"}, Confidence: "medium", Freshness: "current",
		ValidationItems: []string{"记录验证结果"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-answer-edit-apply")
	var analysis focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 {
		t.Fatalf("apply answer edit analysis status=%d body=%s", applied.Code, applied.Body.String())
	}
	decisionID := analysis.Decisions[0].ID
	for index, status := range []string{"running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: decisionID,
		}, "focus-v3-answer-edit-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	if run.Run.Status != "completed" || run.Run.AnalysisRunID != claim.RunID {
		t.Fatalf("old result was not completed: %#v", run)
	}

	editedPayload := decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		QuestionID: run.Questions[0].ID, ExpectedRunRevision: run.Run.Revision,
		ExpectedAnswerRevision: run.Questions[0].AnswerRevision,
		Answer:                 decisionAnswerInput{Type: "text", Value: json.RawMessage(`"先控制成本，暂不优先投入"`)},
	}
	edited := appDataMutationFlow(t, "decision_run.answer", editedPayload, "focus-v3-answer-edit-second")
	if edited.Code != http.StatusOK {
		t.Fatalf("edit completed answer status=%d body=%s", edited.Code, edited.Body.String())
	}
	editedRun := decodeDecisionRunFlow(t, edited)
	if editedRun.Run.Status != "collecting_context" || editedRun.Run.FocusRevision != focus.Revision+1 || editedRun.Run.AnalysisRunID != "" || editedRun.Run.DecisionID != "" || editedRun.Run.Attempt != 2 || editedRun.Questions[0].AnswerRevision != 2 {
		t.Fatalf("edited answer did not invalidate old analysis: %#v", editedRun)
	}
	var oldAnalysisStatus, oldAnalysisError string
	if err := db.QueryRow(`SELECT status,error_code FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&oldAnalysisStatus, &oldAnalysisError); err != nil {
		t.Fatal(err)
	}
	if oldAnalysisStatus != "superseded" || oldAnalysisError != "clarification_changed" {
		t.Fatalf("edited answer left old analysis active: status=%q error=%q", oldAnalysisStatus, oldAnalysisError)
	}
	defaultAnalysis := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID, nil, "")
	var defaultProjection focusAnalysisGetResponse
	if defaultAnalysis.Code != http.StatusOK || json.Unmarshal(defaultAnalysis.Body.Bytes(), &defaultProjection) != nil || defaultProjection.RunID != "" || len(defaultProjection.Results) != 0 {
		t.Fatalf("superseded analysis remained the default projection: status=%d body=%s", defaultAnalysis.Code, defaultAnalysis.Body.String())
	}
	newClaim := readFocusFlowClaim(t, focus.ID)
	if newClaim.RunID == claim.RunID || newClaim.FocusRevision != focus.Revision+1 {
		t.Fatalf("answer edit reused stale analysis: old=%#v new=%#v", claim, newClaim)
	}
	newApply := focusFlowApplyRequest(newClaim, "signal-answer-edit-new")
	newApply.Signals = nil
	newApply.Decisions = []focusDecisionInput{{
		ID: "decision-answer-edit-new", Question: "是否优先投入端侧 AI？",
		Options: []string{"优先投入", "继续观察"}, Recommendation: "暂不优先投入",
		ExpectedOutcome: "先控制本季度投入成本", SourceIDs: []string{newClaim.Evidence[0].ID},
		SupportingEvidence: []string{"用户修改后的约束要求优先控制成本"}, Confidence: "medium", Freshness: "current",
	}}
	newApply.Actions = []focusActionInput{{
		ID: "action-answer-edit-new", Task: "验证修改回答后的成本边界", Owner: "产品负责人",
		SourceIDs: []string{newClaim.Evidence[0].ID}, Priority: "medium",
		SupportingEvidence: []string{"用户修改后的约束要求优先控制成本"}, Confidence: "medium", Freshness: "current",
		ValidationItems: []string{"记录验证结果"},
	}}
	newApplied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", newApply, "focus-v3-answer-edit-new-apply")
	var newAnalysis focusAnalysisGetResponse
	if newApplied.Code != http.StatusOK || json.Unmarshal(newApplied.Body.Bytes(), &newAnalysis) != nil || len(newAnalysis.Decisions) != 1 {
		t.Fatalf("apply changed-answer analysis status=%d body=%s", newApplied.Code, newApplied.Body.String())
	}
	newDecisionData, _ := newAnalysis.Decisions[0].Data.(map[string]any)
	if newDecisionData["recommendation"] != "暂不优先投入" || newAnalysis.Decisions[0].ID == decisionID {
		t.Fatalf("changed answer did not produce a distinct conclusion: old=%s new=%#v", decisionID, newAnalysis.Decisions[0])
	}
	replay := appDataMutationFlow(t, "decision_run.answer", editedPayload, "focus-v3-answer-edit-second")
	if replay.Code != edited.Code || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || replay.Body.String() != edited.Body.String() {
		t.Fatalf("answer edit replay was not idempotent: status=%d body=%s", replay.Code, replay.Body.String())
	}
	var answerHistory, currentAnswers int
	if err := db.QueryRow(`SELECT COUNT(*),SUM(CASE WHEN is_current=1 THEN 1 ELSE 0 END) FROM focus_decision_answers WHERE run_id=? AND question_id=?`, run.Run.ID, run.Questions[0].ID).Scan(&answerHistory, &currentAnswers); err != nil {
		t.Fatal(err)
	}
	if answerHistory != 2 || currentAnswers != 1 {
		t.Fatalf("answer history duplicated or lost: history=%d current=%d", answerHistory, currentAnswers)
	}
}

func TestFocusV3ConfirmedDecisionReplacementHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-history", "profile-focus-v3-history")
	focus := createFocusFlowSpace(t, "AI Computer 技术路线")
	insertFocusFlowMeeting(t, "meeting-focus-v3-history", "AI Computer 团队确认需要优先保证离线稳定性。", "2026-08-15T09:30:00Z")
	insertFocusFlowMeeting(t, "meeting-focus-v3-history-2", "客户访谈确认断网时仍需继续使用核心能力。", "2026-08-16T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || len(claim.Evidence) != 2 {
		t.Fatalf("unexpected decision evidence claim: %#v", claim)
	}
	evidenceID := claim.Evidence[0].ID
	applyRequest := focusFlowApplyRequest(claim, "unused-signal")
	applyRequest.Signals = nil
	applyRequest.Actions = []focusActionInput{{
		ID: "action-v3-history", Task: "验证离线启动稳定性", Owner: "产品组", Priority: "high",
		SourceIDs: []string{evidenceID}, SupportingEvidence: []string{"会议明确要求离线稳定性"},
		Confidence: "high", Freshness: "current", ValidationItems: []string{"记录三次冷启动结果"},
	}}
	applyRequest.Decisions = []focusDecisionInput{{
		ID: "decision-v3-history", Question: "是否采用本地优先路线？",
		Options: []string{"本地优先", "云端优先"}, Recommendation: "采用本地优先路线",
		ExpectedOutcome: "离线场景可持续使用", SourceIDs: []string{evidenceID},
		SupportingEvidence: []string{"两次独立会议均明确要求离线稳定性"},
		Assumptions:        []string{"设备资源预算不变"}, Risks: []string{"初始同步时间增加"},
		Confidence: "high", Freshness: "current", ValidationItems: []string{"验证离线启动"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-history-apply")
	if applied.Code != http.StatusOK {
		t.Fatalf("apply decision status=%d body=%s", applied.Code, applied.Body.String())
	}
	var analysis focusAnalysisGetResponse
	if err := json.Unmarshal(applied.Body.Bytes(), &analysis); err != nil || len(analysis.Decisions) != 1 {
		t.Fatalf("decode decision analysis err=%v body=%s", err, applied.Body.String())
	}
	decision := analysis.Decisions[0]

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "guided", AnalysisRunID: claim.RunID, DecisionID: decision.ID, SkipClarification: true,
	}, "focus-v3-history-run")
	if started.Code != http.StatusCreated {
		t.Fatalf("start history run status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: decision.ID,
		}, "focus-v3-history-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	if run.Run.Status != "completed" {
		t.Fatalf("run did not complete: %#v", run)
	}

	confirmed := appDataMutationFlow(t, "decision_revision.apply", decisionRevisionApplyRequest{
		SchemaVersion: decisionHistorySchemaVersion, FocusID: focus.ID, DecisionID: decision.ID, RunID: run.Run.ID,
		ExpectedFocusRevision: focus.Revision, ExpectedResultRevision: decision.Revision, ExpectedRunRevision: run.Run.Revision,
	}, "focus-v3-history-confirm")
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirm decision status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var history decisionHistoryResponse
	if err := json.Unmarshal(confirmed.Body.Bytes(), &history); err != nil || len(history.Revisions) != 1 || history.SelectedID == "" || history.Stats.NodeCount == 0 {
		t.Fatalf("unexpected confirmed history err=%v history=%#v", err, history)
	}
	firstRevisionID := history.SelectedID
	var actionID, actionStatus, confirmationStatus string
	if err := db.QueryRow(`SELECT id,status,confirmation_status FROM actions WHERE focus_id=? AND analysis_run_id=?`, focus.ID, claim.RunID).Scan(&actionID, &actionStatus, &confirmationStatus); err != nil {
		t.Fatal(err)
	}
	if actionStatus != "accepted" || confirmationStatus != "accepted" {
		t.Fatalf("confirmed decision did not accept related action: status=%s confirmation=%s", actionStatus, confirmationStatus)
	}
	var linkedActions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_decision_action_links WHERE focus_id=? AND decision_revision_id=? AND action_id=?`, focus.ID, firstRevisionID, actionID).Scan(&linkedActions); err != nil {
		t.Fatal(err)
	}
	if linkedActions != 1 {
		t.Fatalf("confirmed decision action link count=%d", linkedActions)
	}

	replaced := appDataMutationFlow(t, "decision_feedback.apply", decisionFeedbackApplyRequest{
		SchemaVersion: decisionHistorySchemaVersion, FocusID: focus.ID, RevisionID: firstRevisionID,
		Action: "replacement", Content: "采用本地优先路线，但首期只覆盖核心离线流程",
	}, "focus-v3-history-replacement")
	if replaced.Code != http.StatusCreated {
		t.Fatalf("replace decision status=%d body=%s", replaced.Code, replaced.Body.String())
	}
	if err := json.Unmarshal(replaced.Body.Bytes(), &history); err != nil || len(history.Revisions) != 2 || history.SelectedID == firstRevisionID {
		t.Fatalf("replacement history err=%v history=%#v", err, history)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_decision_action_links WHERE focus_id=? AND decision_revision_id=? AND action_id=?`, focus.ID, history.SelectedID, actionID).Scan(&linkedActions); err != nil {
		t.Fatal(err)
	}
	if linkedActions != 1 {
		t.Fatalf("replacement lost or duplicated action link: count=%d", linkedActions)
	}
	compared := callFocusFlowJSON(t, decisionRevisionCompareHandler, http.MethodGet,
		"/api/internal/decision-revisions/compare?focus_id="+focus.ID+"&base_revision_id="+firstRevisionID+"&compare_revision_id="+history.SelectedID, nil, "")
	if compared.Code != http.StatusOK || !strings.Contains(compared.Body.String(), `"changed_node_ids"`) {
		t.Fatalf("compare history status=%d body=%s", compared.Code, compared.Body.String())
	}
}

func TestFocusV3InsufficientAndConflictingEvidenceRequireClarificationFlow(t *testing.T) {
	cases := []struct {
		name         string
		meetingCount int
		counter      []string
		expectedText string
	}{
		{name: "insufficient", meetingCount: 1, expectedText: "依据不足"},
		{name: "conflicting", meetingCount: 2, counter: []string{"另一份访谈认为云端优先更重要"}, expectedText: "存在冲突"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db = openTestDB(t)
			useStableContextScope(t, "owner-focus-v3-clarify-"+testCase.name, "profile-focus-v3-clarify-"+testCase.name)
			focus := createFocusFlowSpace(t, "端侧 AI 证据门槛 "+testCase.name)
			for index := 0; index < testCase.meetingCount; index++ {
				insertFocusFlowMeeting(t, "meeting-focus-v3-clarify-"+testCase.name+string(rune('a'+index)), "端侧与云端路线需要根据用户约束取舍。", "2026-08-15T09:30:00Z")
			}
			claim := readFocusFlowClaim(t, focus.ID)
			applyRequest := focusFlowApplyRequest(claim, "unused-clarify-signal")
			applyRequest.Signals = nil
			applyRequest.Decisions = []focusDecisionInput{{
				ID: "decision-clarify-" + testCase.name, Question: "是否优先投入端侧 AI？",
				Options: []string{"端侧优先", "云端优先"}, Recommendation: "端侧优先",
				SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"端侧可降低离线延迟"},
				Counterevidence: testCase.counter, Confidence: "medium", Freshness: "current",
			}}
			applyRequest.Actions = []focusActionInput{{
				ID: "action-clarify-" + testCase.name, Task: "验证澄清后的路线约束", Owner: "产品负责人",
				SourceIDs: []string{claim.Evidence[0].ID}, Priority: "medium",
				SupportingEvidence: []string{"根据澄清回答验证路线选择"}, Confidence: "medium", Freshness: "current",
				ValidationItems: []string{"记录验证结果"},
			}}
			applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-clarify-apply-"+testCase.name)
			var analysis focusAnalysisGetResponse
			if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 {
				t.Fatalf("apply clarification fixture status=%d body=%s", applied.Code, applied.Body.String())
			}
			directAccept := appDataMutationFlow(t, "focus_result.feedback", focusResultFeedbackRequest{
				SchemaVersion: focusSpaceSchemaVersion, FocusID: focus.ID, ResultKind: "decision",
				ResultID: analysis.Decisions[0].ID, ExpectedRevision: analysis.Decisions[0].Revision, Decision: "accepted",
			}, "focus-v3-clarify-direct-accept-"+testCase.name)
			if directAccept.Code != http.StatusConflict || !strings.Contains(directAccept.Body.String(), testCase.expectedText) {
				t.Fatalf("direct acceptance bypassed clarification guard: status=%d body=%s", directAccept.Code, directAccept.Body.String())
			}
			started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
				SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
				DecisionMode: "guided", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID, SkipClarification: true,
			}, "focus-v3-clarify-start-"+testCase.name)
			run := decodeDecisionRunFlow(t, started)
			for index, status := range []string{"collecting_context", "running_ai"} {
				resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
					SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
					Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
				}, "focus-v3-clarify-pre-"+testCase.name+string(rune('1'+index)))
				if resumed.Code != http.StatusOK {
					t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
				}
				run = decodeDecisionRunFlow(t, resumed)
				if run.Run.AnalysisRunID != claim.RunID {
					t.Fatalf("resume %s lost analysis link: body=%s", status, resumed.Body.String())
				}
			}
			blocked := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
				Status: "applying", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
			}, "focus-v3-clarify-blocked-"+testCase.name)
			if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "clarification_required") || !strings.Contains(blocked.Body.String(), testCase.expectedText) {
				t.Fatalf("unsafe direct decision was not blocked: status=%d body=%s", blocked.Code, blocked.Body.String())
			}
			questionText := "请确认：缺少哪一项关键约束会改变本轮结论？"
			if testCase.name == "conflicting" {
				questionText = "现有依据一侧支持端侧离线能力，另一侧支持云端优先；本轮优先采用哪一侧？"
			}
			waiting := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
				Status: "waiting_clarification", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
				Question: &decisionQuestionInput{ID: "q_material", Type: "text", Question: questionText},
			}, "focus-v3-clarify-question-"+testCase.name)
			run = decodeDecisionRunFlow(t, waiting)
			if waiting.Code != http.StatusOK || run.CurrentQuestion == nil || run.CurrentQuestion.Question != questionText {
				t.Fatalf("clarification question status=%d run=%#v", waiting.Code, run)
			}
			answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, QuestionID: run.CurrentQuestion.ID,
				ExpectedRunRevision: run.Run.Revision, ExpectedAnswerRevision: 0,
				Answer: decisionAnswerInput{Type: "text", Value: json.RawMessage(`"优先保证离线可用性"`)},
			}, "focus-v3-clarify-answer-"+testCase.name)
			run = decodeDecisionRunFlow(t, answered)
			for index, status := range []string{"running_ai", "applying"} {
				resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
					SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
					Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
				}, "focus-v3-clarify-post-"+testCase.name+string(rune('1'+index)))
				if resumed.Code != http.StatusOK {
					t.Fatalf("answered resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
				}
				run = decodeDecisionRunFlow(t, resumed)
			}
			if run.Run.Status != "applying" || run.Questions[0].AnswerRevision != 1 {
				t.Fatalf("answered clarification did not unblock applying: %#v", run)
			}
			completed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
				Status: "completed", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
			}, "focus-v3-clarify-complete-"+testCase.name)
			run = decodeDecisionRunFlow(t, completed)
			if completed.Code != http.StatusOK || run.Run.Status != "completed" {
				t.Fatalf("clarified decision did not complete: status=%d run=%#v", completed.Code, run)
			}
			accepted := appDataMutationFlow(t, "focus_result.feedback", focusResultFeedbackRequest{
				SchemaVersion: focusSpaceSchemaVersion, FocusID: focus.ID, ResultKind: "decision",
				ResultID: analysis.Decisions[0].ID, ExpectedRevision: analysis.Decisions[0].Revision, Decision: "accepted",
			}, "focus-v3-clarify-accept-after-answer-"+testCase.name)
			if accepted.Code != http.StatusOK {
				t.Fatalf("answered clarification did not allow acceptance: status=%d body=%s", accepted.Code, accepted.Body.String())
			}
		})
	}
}

func TestFocusV3ApplyCannotPersistBeforeRequiredClarificationFlow(t *testing.T) {
	cases := []struct {
		name         string
		meetingCount int
		counterKind  string
	}{
		{name: "insufficient", meetingCount: 1},
		{name: "conflicting-decision", meetingCount: 2, counterKind: "decision"},
		{name: "conflicting-signal", meetingCount: 2, counterKind: "signal"},
		{name: "conflicting-action", meetingCount: 2, counterKind: "action"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db = openTestDB(t)
			useStableContextScope(t, "owner-focus-v3-preapply-"+testCase.name, "profile-focus-v3-preapply-"+testCase.name)
			focus := createFocusFlowSpace(t, "AI Computer 端侧与云端路线写入前澄清 "+testCase.name)
			for index := 0; index < testCase.meetingCount; index++ {
				insertFocusFlowMeeting(t, "meeting-focus-v3-preapply-"+testCase.name+string(rune('a'+index)), "端侧与云端路线需要根据用户约束取舍。", "2026-08-15T09:30:00Z")
			}
			claim := readFocusFlowClaim(t, focus.ID)
			if claim.RunID == "" || claim.Status != "claimed" || len(claim.Evidence) != testCase.meetingCount {
				t.Fatalf("unexpected pre-apply claim: %#v", claim)
			}
			started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
				SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
				DecisionMode: "guided", AnalysisRunID: claim.RunID, SkipClarification: true,
			}, "focus-v3-preapply-start-"+testCase.name)
			run := decodeDecisionRunFlow(t, started)
			if started.Code != http.StatusCreated || run.Run == nil || run.Run.AnalysisRunID != claim.RunID {
				t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
			}
			for index, status := range []string{"collecting_context", "running_ai"} {
				resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
					SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
					Status: status, AnalysisRunID: claim.RunID,
				}, "focus-v3-preapply-resume-"+testCase.name+string(rune('1'+index)))
				if resumed.Code != http.StatusOK {
					t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
				}
				run = decodeDecisionRunFlow(t, resumed)
				if run.Run.AnalysisRunID != claim.RunID {
					t.Fatalf("pre-apply resume %s lost analysis link: body=%s", status, resumed.Body.String())
				}
			}
			request := focusFlowApplyRequest(claim, "unused-preapply-signal")
			request.Signals = nil
			request.Decisions = []focusDecisionInput{{
				ID: "decision-preapply-" + testCase.name, Question: "是否优先投入端侧 AI？",
				Options: []string{"端侧优先", "云端优先"}, Recommendation: "端侧优先",
				SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"端侧可降低离线延迟"},
				Confidence: "medium", Freshness: "current",
			}}
			counter := []string{"另一份资料支持相反路线"}
			switch testCase.counterKind {
			case "decision":
				request.Decisions[0].Counterevidence = counter
			case "signal":
				request.Signals = []focusSignalInput{{
					ID: "signal-preapply-" + testCase.name, Title: "路线证据冲突", Detail: "资料给出相反结论",
					SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"支持端侧"},
					Counterevidence: counter, Confidence: "medium", Freshness: "current",
				}}
			case "action":
				request.Actions = []focusActionInput{{
					ID: "action-preapply-" + testCase.name, Task: "执行端侧路线", Owner: "产品组", Priority: "medium",
					SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"支持端侧"},
					Counterevidence: counter, Confidence: "medium", Freshness: "current",
				}}
			}
			blocked := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "focus-v3-preapply-blocked-"+testCase.name)
			if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "clarification_required") {
				t.Fatalf("pre-apply result was not blocked: status=%d body=%s", blocked.Code, blocked.Body.String())
			}
			var persisted int
			if err := db.QueryRow(`SELECT COUNT(*) FROM decisions WHERE focus_id=? AND analysis_run_id=?`, focus.ID, claim.RunID).Scan(&persisted); err != nil {
				t.Fatal(err)
			}
			if persisted != 0 {
				t.Fatalf("unclarified decision was persisted: %d", persisted)
			}
			waiting := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
				Status: "waiting_clarification", AnalysisRunID: claim.RunID,
				Question: &decisionQuestionInput{ID: "q_basis", Type: "text", Question: "哪项约束应作为本轮优先依据？"},
			}, "focus-v3-preapply-question-"+testCase.name)
			run = decodeDecisionRunFlow(t, waiting)
			answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, QuestionID: run.CurrentQuestion.ID,
				ExpectedRunRevision: run.Run.Revision, ExpectedAnswerRevision: 0,
				Answer: decisionAnswerInput{Type: "text", Value: json.RawMessage(`"优先保证离线可用性"`)},
			}, "focus-v3-preapply-answer-"+testCase.name)
			run = decodeDecisionRunFlow(t, answered)
			resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
				SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
				Status: "running_ai", AnalysisRunID: claim.RunID,
			}, "focus-v3-preapply-running-"+testCase.name)
			run = decodeDecisionRunFlow(t, resumed)
			if answered.Code != http.StatusOK || resumed.Code != http.StatusOK || run.Run.Status != "running_ai" {
				t.Fatalf("clarification did not resume analysis: answer=%d resume=%d run=%#v", answered.Code, resumed.Code, run)
			}
			applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "focus-v3-preapply-applied-"+testCase.name)
			if applied.Code != http.StatusOK {
				t.Fatalf("answered analysis remained blocked: status=%d body=%s", applied.Code, applied.Body.String())
			}
		})
	}
}

func TestFocusV3DecisionFeedbackSynchronizesRelatedActionsFlow(t *testing.T) {
	cases := []struct {
		name           string
		decision       string
		expectedStatus string
		patch          *focusResultPatch
	}{
		{name: "accept", decision: "accepted", expectedStatus: "accepted"},
		{name: "reject", decision: "rejected", expectedStatus: "rejected"},
		{name: "edit", decision: "edited", expectedStatus: "draft", patch: &focusResultPatch{Question: stringPointer("是否先完成成本验证再投入端侧 AI？")}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			db = openTestDB(t)
			useStableContextScope(t, "owner-focus-v3-feedback-"+testCase.name, "profile-focus-v3-feedback-"+testCase.name)
			focus := createFocusFlowSpace(t, "端侧 AI 决策联动 "+testCase.name)
			insertFocusFlowMeeting(t, "meeting-focus-v3-feedback-"+testCase.name, "端侧推理可降低延迟，但需要验证资源成本。", "2026-08-15T09:30:00Z")
			insertFocusFlowMeeting(t, "meeting-focus-v3-feedback-independent-"+testCase.name, "客户访谈确认离线可用性会影响采购决定。", "2026-08-16T09:30:00Z")
			claim := readFocusFlowClaim(t, focus.ID)
			evidenceID := claim.Evidence[0].ID
			applyRequest := focusFlowApplyRequest(claim, "unused-feedback-signal")
			applyRequest.Signals = nil
			applyRequest.Actions = []focusActionInput{{
				ID: "action-feedback-" + testCase.name, Task: "验证端侧推理资源成本", Owner: "产品组", Priority: "high",
				SourceIDs: []string{evidenceID}, SupportingEvidence: []string{"会议提出资源成本约束"},
				Confidence: "medium", Freshness: "current", ValidationItems: []string{"记录峰值内存"},
			}}
			applyRequest.Decisions = []focusDecisionInput{{
				ID: "decision-feedback-" + testCase.name, Question: "是否优先投入端侧 AI？",
				Options: []string{"优先投入", "先验证成本"}, Recommendation: "先验证成本",
				SourceIDs: []string{evidenceID}, SupportingEvidence: []string{"会议提出资源成本约束"},
				Confidence: "medium", Freshness: "current", ValidationItems: []string{"验证资源成本"},
			}}
			applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-feedback-apply-"+testCase.name)
			var analysis focusAnalysisGetResponse
			if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Actions) != 1 || len(analysis.Decisions) != 1 {
				t.Fatalf("apply feedback fixture status=%d body=%s", applied.Code, applied.Body.String())
			}
			request := focusResultFeedbackRequest{
				SchemaVersion: focusSpaceSchemaVersion, FocusID: focus.ID, ResultKind: "decision",
				ResultID: analysis.Decisions[0].ID, ExpectedRevision: analysis.Decisions[0].Revision,
				Decision: testCase.decision, Patch: testCase.patch,
			}
			feedback := appDataMutationFlow(t, "focus_result.feedback", request, "focus-v3-feedback-"+testCase.name)
			if feedback.Code != http.StatusOK {
				t.Fatalf("feedback status=%d body=%s", feedback.Code, feedback.Body.String())
			}
			var status, confirmation string
			if err := db.QueryRow(`SELECT status,confirmation_status FROM actions WHERE id=? AND focus_id=?`, analysis.Actions[0].ID, focus.ID).Scan(&status, &confirmation); err != nil {
				t.Fatal(err)
			}
			if status != testCase.expectedStatus || confirmation != testCase.expectedStatus {
				t.Fatalf("decision %s left related action status=%s confirmation=%s", testCase.decision, status, confirmation)
			}
			projected := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID, nil, "")
			var projectedAnalysis focusAnalysisGetResponse
			if projected.Code != http.StatusOK || json.Unmarshal(projected.Body.Bytes(), &projectedAnalysis) != nil || len(projectedAnalysis.Actions) != 1 {
				t.Fatalf("project related action status=%d body=%s", projected.Code, projected.Body.String())
			}
			if projectedAnalysis.Actions[0].FeedbackStatus != testCase.expectedStatus {
				t.Fatalf("decision %s projected related action as %q, want %q", testCase.decision, projectedAnalysis.Actions[0].FeedbackStatus, testCase.expectedStatus)
			}
			if testCase.decision == "edited" {
				var question, decisionStatus string
				if err := db.QueryRow(`SELECT question,status FROM decisions WHERE id=? AND focus_id=?`, analysis.Decisions[0].ID, focus.ID).Scan(&question, &decisionStatus); err != nil {
					t.Fatal(err)
				}
				if question != *testCase.patch.Question || decisionStatus != "draft" {
					t.Fatalf("edited decision question=%q status=%q", question, decisionStatus)
				}
			}
			replay := appDataMutationFlow(t, "focus_result.feedback", request, "focus-v3-feedback-"+testCase.name)
			if replay.Code != feedback.Code || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || replay.Body.String() != feedback.Body.String() {
				t.Fatalf("feedback replay was not idempotent: status=%d body=%s", replay.Code, replay.Body.String())
			}
		})
	}
}

func TestFocusV3RetryableInterruptionReusesEvidenceAndAnswerFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-retry", "profile-focus-v3-retry")
	focus := createFocusFlowSpace(t, "端侧 AI 中断恢复")
	insertFocusFlowMeeting(t, "meeting-focus-v3-retry", "端侧推理可以降低离线交互延迟。", "2026-08-15T09:30:00Z")
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
	}, "focus-v3-retry-start")
	run := decodeDecisionRunFlow(t, started)
	answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, QuestionID: run.CurrentQuestion.ID,
		ExpectedRunRevision: run.Run.Revision, ExpectedAnswerRevision: 0,
		Answer: decisionAnswerInput{Type: "text", Value: json.RawMessage(`"优先保证离线可用性"`)},
	}, "focus-v3-retry-answer")
	run = decodeDecisionRunFlow(t, answered)
	claim := readFocusFlowClaim(t, focus.ID)
	if len(claim.Evidence) < 2 || !strings.HasPrefix(claim.Evidence[0].ID, "user_answer:") || claim.Evidence[0].SourceLocator["kind"] != "user_answer" || claim.Evidence[0].Excerpt != "优先保证离线可用性" {
		t.Fatalf("saved clarification was not claimed as traceable evidence: %#v", claim.Evidence)
	}
	running := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "running_ai", AnalysisRunID: claim.RunID,
	}, "focus-v3-retry-running")
	run = decodeDecisionRunFlow(t, running)
	failed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "failed_retryable", AnalysisRunID: claim.RunID, ErrorCode: "stream_read", ErrorMessage: "模型流中断",
	}, "focus-v3-retry-failed")
	run = decodeDecisionRunFlow(t, failed)
	if failed.Code != http.StatusOK || run.Run.Status != "failed_retryable" || run.Run.AnalysisRunID != claim.RunID {
		t.Fatalf("retryable failure lost run binding: status=%d run=%#v", failed.Code, run)
	}
	recovered := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
	}, "focus-v3-retry-recover")
	run = decodeDecisionRunFlow(t, recovered)
	if recovered.Code != http.StatusOK || run.Run.Status != "queued" || run.Run.AnalysisRunID != claim.RunID || run.Run.Attempt != 2 || len(run.Questions) != 1 || run.Questions[0].AnswerRevision != 1 {
		t.Fatalf("recovery did not preserve evidence and answer: status=%d run=%#v", recovered.Code, run)
	}
	applyRequest := focusFlowApplyRequest(claim, "unused-retry-signal")
	applyRequest.Signals = nil
	applyRequest.Decisions = []focusDecisionInput{{
		ID: "decision-retry", Question: "是否优先保证端侧离线能力？", Options: []string{"优先保证", "继续观察"},
		Recommendation: "优先保证", SourceIDs: []string{claim.Evidence[0].ID},
		SupportingEvidence: []string{"会议支持离线低延迟"}, Confidence: "high", Freshness: "current",
	}}
	applyRequest.Actions = []focusActionInput{{
		ID: "action-retry", Task: "验证端侧离线能力", Owner: "产品负责人", Priority: "high",
		SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"会议支持离线低延迟"},
		Confidence: "high", Freshness: "current", ValidationItems: []string{"记录离线验证结果"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-v3-retry-apply")
	var analysis focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 {
		t.Fatalf("retry analysis apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	for index, status := range []string{"collecting_context", "running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
		}, "focus-v3-retry-complete-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	var answerCount, decisionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_decision_answers WHERE run_id=?`, run.Run.ID).Scan(&answerCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM decisions WHERE focus_id=? AND analysis_run_id=?`, focus.ID, claim.RunID).Scan(&decisionCount); err != nil {
		t.Fatal(err)
	}
	if run.Run.Status != "completed" || run.Run.ID == "" || answerCount != 1 || decisionCount != 1 {
		t.Fatalf("recovery duplicated durable state: run=%#v answers=%d decisions=%d", run, answerCount, decisionCount)
	}
	confirmed := appDataMutationFlow(t, "decision_revision.apply", decisionRevisionApplyRequest{
		SchemaVersion: decisionHistorySchemaVersion, FocusID: focus.ID, DecisionID: analysis.Decisions[0].ID,
		RunID: run.Run.ID, ExpectedFocusRevision: focus.Revision,
		ExpectedResultRevision: analysis.Decisions[0].Revision, ExpectedRunRevision: run.Run.Revision,
	}, "focus-v3-retry-confirm")
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirm recovered decision status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var answerNodes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_logic_nodes WHERE focus_id=? AND node_type='user_answer' AND author='user'`, focus.ID).Scan(&answerNodes); err != nil {
		t.Fatal(err)
	}
	if answerNodes != 1 {
		t.Fatalf("clarification answer was not preserved in the confirmed evidence chain: nodes=%d", answerNodes)
	}
}

func TestFocusV3ClarificationAloneBecomesClaimableEvidenceFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-answer-only", "profile-focus-v3-answer-only")
	focus := createFocusFlowSpace(t, "没有外部资料时选择验证方向")

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
	}, "focus-v3-answer-only-start")
	run := decodeDecisionRunFlow(t, started)
	if started.Code != http.StatusCreated || run.CurrentQuestion == nil {
		t.Fatalf("start answer-only flow status=%d run=%#v", started.Code, run)
	}
	answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		QuestionID: run.CurrentQuestion.ID, ExpectedRunRevision: run.Run.Revision,
		ExpectedAnswerRevision: 0,
		Answer:                 decisionAnswerInput{Type: "text", Value: json.RawMessage(`"先验证真实用户是否愿意连续使用"`)},
	}, "focus-v3-answer-only-answer")
	run = decodeDecisionRunFlow(t, answered)
	if answered.Code != http.StatusOK || run.Run.Status != "collecting_context" {
		t.Fatalf("answer-only response status=%d run=%#v", answered.Code, run)
	}

	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || len(claim.Evidence) != 1 {
		t.Fatalf("clarification-only evidence was not claimable: %#v", claim)
	}
	evidence := claim.Evidence[0]
	if !strings.HasPrefix(evidence.ID, "user_answer:") || evidence.SourceLocator["kind"] != "user_answer" || evidence.Excerpt != "先验证真实用户是否愿意连续使用" {
		t.Fatalf("clarification-only evidence lost traceability: %#v", evidence)
	}
}

func TestFocusV3DecisionRunRebindsOnlyFencedFailedEvidenceFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-rebind", "profile-focus-v3-rebind")
	focus := createFocusFlowSpace(t, "选择 AI 平台验证方向")
	insertFocusFlowMeeting(t, "meeting-focus-v3-rebind", "第一版资料：先验证端侧 AI 平台。", "2026-08-15T09:30:00Z")
	previous := readFocusFlowClaim(t, focus.ID)
	if previous.Status != "claimed" || previous.RunID == "" {
		t.Fatalf("unexpected previous claim: %#v", previous)
	}
	failed := focusFlowApplyRequest(previous, "")
	failed.Status = "failed"
	failed.ErrorCode = "apply_invalid_request"
	failed.Signals = nil
	failed.Actions = nil
	failed.Decisions = nil
	failed.Usage = focusAnalysisUsage{}
	failedResponse := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", failed, "focus-v3-rebind-fail")
	if failedResponse.Code != http.StatusOK {
		t.Fatalf("failed claim status=%d body=%s", failedResponse.Code, failedResponse.Body.String())
	}
	if _, err := db.Exec(`UPDATE recordings SET summary=?,updated_at=? WHERE record_id=?`, "第二版资料：改为比较端云两种 AI 平台。", "2026-08-15T10:00:00Z", "meeting-focus-v3-rebind"); err != nil {
		t.Fatal(err)
	}
	replacement := readFocusFlowClaim(t, focus.ID)
	if replacement.Status != "claimed" || replacement.RunID == previous.RunID {
		t.Fatalf("unexpected replacement claim: previous=%#v replacement=%#v", previous, replacement)
	}

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "guided", AnalysisRunID: previous.RunID, SkipClarification: true,
	}, "focus-v3-rebind-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("decision start status=%d body=%s", started.Code, started.Body.String())
	}
	startedRun := decodeDecisionRunFlow(t, started)
	toContext := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: startedRun.Run.ID, ExpectedRevision: startedRun.Run.Revision,
		Status: "collecting_context", AnalysisRunID: previous.RunID,
	}, "focus-v3-rebind-context")
	if toContext.Code != http.StatusOK {
		t.Fatalf("decision context status=%d body=%s", toContext.Code, toContext.Body.String())
	}
	contextRun := decodeDecisionRunFlow(t, toContext)
	rebound := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: contextRun.Run.ID, ExpectedRevision: contextRun.Run.Revision,
		Status: "running_ai", AnalysisRunID: replacement.RunID,
	}, "focus-v3-rebind-running")
	if rebound.Code != http.StatusOK {
		t.Fatalf("decision rebind status=%d body=%s", rebound.Code, rebound.Body.String())
	}
	reboundRun := decodeDecisionRunFlow(t, rebound)
	if reboundRun.Run == nil || reboundRun.Run.Status != "running_ai" || reboundRun.Run.AnalysisRunID != replacement.RunID {
		t.Fatalf("decision did not bind replacement claim: %#v", reboundRun)
	}
}

func TestFocusV3TurnBridgeAndImageMaterialIsolationFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-v3-media", "profile-focus-v3-media")
	focus := createFocusFlowSpace(t, "评估产品截图")
	imageBody := []byte("bounded-focus-image")
	digestRaw := sha256.Sum256(imageBody)
	digest := hex.EncodeToString(digestRaw[:])

	upstreamCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "device-data-token" {
			t.Errorf("device-data token was not forwarded")
		}
		switch r.URL.Path {
		case "/focus-decision/v1/turns":
			if r.Header.Get("X-Zettlab-Idempotency-Key") != "focus-turn-1" {
				t.Errorf("turn idempotency key was not forwarded")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"state":"accepted","run_id":"decision-run-1","revision":1}`))
		case "/device-data/v1/chat-turn-media":
			if r.Header.Get(businessExecutionTokenHeader) != "execution-token" || r.Header.Get(hermesTurnIDHeader) != "turn-1" {
				t.Errorf("trusted execution scope was not forwarded")
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"index": 0, "mime": "image/png", "size": len(imageBody), "digest": digest}}})
		case "/device-data/v1/chat-turn-media/0":
			if r.URL.Query().Get("digest") != digest {
				t.Errorf("image digest binding missing")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bridge := newFocusDecisionTurnBridge(server.URL+"/device-data/v1", "device-data-token", server.Client())
	turnRequest := httptest.NewRequest(http.MethodPost, focusDecisionTurnPath, bytes.NewBufferString(`{"focus_id":"`+focus.ID+`","run_id":"decision-run-1","expected_revision":1}`))
	turnRequest.Header.Set("X-Zettlab-Idempotency-Key", "focus-turn-1")
	turnResponse := httptest.NewRecorder()
	bridge.ServeHTTP(turnResponse, turnRequest)
	if turnResponse.Code != http.StatusAccepted || !strings.Contains(turnResponse.Body.String(), `"state":"accepted"`) {
		t.Fatalf("turn bridge status=%d body=%s", turnResponse.Code, turnResponse.Body.String())
	}

	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", server.URL+"/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "device-data-token")
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDir) })

	callImport := func(turnID string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/internal/focus-attachments/import", bytes.NewBufferString(`{"focus_id":"`+focus.ID+`","expected_revision":1}`))
		request.Header.Set("X-Zettlab-Idempotency-Key", "focus-image-import")
		request.Header.Set(trustedHostOperationHeader, trustedHostOperationValue)
		request.Header.Set(trustedExecutionTokenHeader, "execution-token")
		request.Header.Set(trustedTurnIDHeader, turnID)
		request.Header.Set(trustedSessionIDHeader, "session-1")
		request.Header.Set(trustedSessionKeyHeader, "session-key-1")
		response := httptest.NewRecorder()
		focusAttachmentImportHandler(response, request)
		return response
	}
	imported := callImport("turn-1")
	if imported.Code != http.StatusOK {
		t.Fatalf("image import status=%d body=%s", imported.Code, imported.Body.String())
	}
	var importResult focusAttachmentImportResponse
	if err := json.Unmarshal(imported.Body.Bytes(), &importResult); err != nil || len(importResult.Imported) != 1 || importResult.Focus.Revision != focus.Revision+1 {
		t.Fatalf("unexpected import result err=%v result=%#v", err, importResult)
	}
	assetID := importResult.Imported[0].AssetID
	replay := callImport("turn-1")
	var replayResult focusAttachmentImportResponse
	replayDecodeErr := json.Unmarshal(replay.Body.Bytes(), &replayResult)
	if replay.Code != http.StatusOK || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || replayDecodeErr != nil || len(replayResult.Imported) != 1 || replayResult.Imported[0].AssetID != assetID || replayResult.Focus.Revision != importResult.Focus.Revision {
		t.Fatalf("image replay status=%d replay=%q body=%s", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String())
	}
	differentTurn := callImport("turn-2")
	if differentTurn.Code != http.StatusConflict {
		t.Fatalf("different turn reused receipt: status=%d body=%s", differentTurn.Code, differentTurn.Body.String())
	}

	assetResponse := callFocusFlowJSON(t, focusAssetHandler, http.MethodGet, "/api/focus-assets?id="+assetID, nil, "")
	if assetResponse.Code != http.StatusOK || !bytes.Equal(assetResponse.Body.Bytes(), imageBody) {
		t.Fatalf("asset read status=%d body=%q", assetResponse.Code, assetResponse.Body.Bytes())
	}
	previousScope := appDataScope
	appDataScope = dataScope{OwnerID: "owner-focus-v3-other", ProfileID: "profile-focus-v3-other"}
	crossOwner := callFocusFlowJSON(t, focusAssetHandler, http.MethodGet, "/api/focus-assets?id="+assetID, nil, "")
	appDataScope = previousScope
	if crossOwner.Code != http.StatusNotFound {
		t.Fatalf("cross-owner asset read status=%d body=%s", crossOwner.Code, crossOwner.Body.String())
	}
	if upstreamCalls < 3 {
		t.Fatalf("expected turn, media list and media download calls, got %d", upstreamCalls)
	}
}

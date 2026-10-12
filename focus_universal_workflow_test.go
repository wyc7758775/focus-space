package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestAcceptedDecisionCanReturnToPendingWithoutDeletingHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-undo", "profile-focus-undo")
	focus := createFocusFlowSpace(t, "选择开发电脑")
	insertFocusFlowMeeting(t, "meeting-focus-undo-a", "需要在续航和性能之间平衡。", "2026-09-17T09:00:00Z")
	insertFocusFlowMeeting(t, "meeting-focus-undo-b", "预算上限会影响最终选择。", "2026-09-17T10:00:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if _, err := db.Exec(`INSERT INTO focus_analysis_evidence(run_id,owner_id,profile_id,evidence_id,source_kind,source_id,title,created_at) VALUES(?,?,?,?,?,?,?,?)`, claim.RunID, appDataScope.OwnerID, appDataScope.ProfileID, "obsidian:focus-undo-budget", "obsidian", "focus-undo-budget", "预算笔记", "2026-09-17T10:05:00Z"); err != nil {
		t.Fatal(err)
	}
	request := focusFlowApplyRequest(claim, "signal-focus-undo")
	request.Signals = nil
	request.Decisions = []focusDecisionInput{{
		ID: "decision-focus-undo", Question: "选择哪台电脑？", Options: []string{"A", "B"}, Recommendation: "选择 A",
		SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"续航更符合需求"},
		Confidence: "medium", Freshness: "current", ValidationItems: []string{"确认最终价格"},
	}}
	request.Actions = []focusActionInput{{
		ID: "action-focus-undo", Task: "确认 A 的最终价格", Owner: "用户", Priority: "medium",
		SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"价格仍需确认"},
		Confidence: "medium", Freshness: "current", ValidationItems: []string{"记录价格"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "focus-undo-apply")
	var analysis focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 || len(analysis.Actions) != 1 {
		t.Fatalf("apply fixture status=%d body=%s", applied.Code, applied.Body.String())
	}

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "rational", AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID, SkipClarification: true,
	}, "focus-undo-run")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
		}, "focus-undo-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	confirmed := appDataMutationFlow(t, "decision_revision.apply", decisionRevisionApplyRequest{
		SchemaVersion: decisionHistorySchemaVersion, FocusID: focus.ID, DecisionID: analysis.Decisions[0].ID, RunID: run.Run.ID,
		ExpectedFocusRevision: focus.Revision, ExpectedResultRevision: analysis.Decisions[0].Revision, ExpectedRunRevision: run.Run.Revision,
	}, "focus-undo-confirm")
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var acceptedRevision int
	if err := db.QueryRow(`SELECT revision FROM focus_result_feedback WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind='decision' AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID, analysis.Decisions[0].ID).Scan(&acceptedRevision); err != nil {
		t.Fatal(err)
	}

	undone := appDataMutationFlow(t, "focus_result.feedback", focusResultFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, ResultKind: "decision", ResultID: analysis.Decisions[0].ID,
		ExpectedRevision: acceptedRevision, Decision: "draft",
	}, "focus-undo-draft")
	if undone.Code != http.StatusOK {
		t.Fatalf("undo status=%d body=%s", undone.Code, undone.Body.String())
	}
	var decisionStatus, actionStatus, actionConfirmation, historyStatus string
	if err := db.QueryRow(`SELECT status FROM decisions WHERE id=? AND focus_id=?`, analysis.Decisions[0].ID, focus.ID).Scan(&decisionStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status,confirmation_status FROM actions WHERE id=? AND focus_id=?`, analysis.Actions[0].ID, focus.ID).Scan(&actionStatus, &actionConfirmation); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT status FROM focus_decision_revisions WHERE focus_id=? AND decision_id=? ORDER BY revision_no DESC LIMIT 1`, focus.ID, analysis.Decisions[0].ID).Scan(&historyStatus); err != nil {
		t.Fatal(err)
	}
	if decisionStatus != "draft" || actionStatus != "draft" || actionConfirmation != "draft" || historyStatus != "superseded" {
		t.Fatalf("undo did not restore pending state: decision=%q action=%q confirmation=%q history=%q", decisionStatus, actionStatus, actionConfirmation, historyStatus)
	}
}

func createUniversalFocusFlow(t *testing.T, goal, intentKind, thinkingTemplate string) FocusSpace {
	t.Helper()
	title := goal
	kind := "monitor"
	if intentKind == "decide" {
		kind = "decision"
	}
	decisionMode := "slow_burn"
	workflowState := "accumulating"
	horizonMode := "open"
	blueprintID := defaultFocusBlueprintID(intentKind)
	response := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
		SchemaVersion:      focusSpaceSchemaVersion,
		Action:             "create",
		ExpectedRevision:   0,
		Title:              &title,
		Goal:               &goal,
		Kind:               &kind,
		DecisionMode:       &decisionMode,
		WorkflowState:      &workflowState,
		HorizonMode:        &horizonMode,
		IntentKind:         &intentKind,
		BlueprintID:        &blueprintID,
		ThinkingTemplateID: &thinkingTemplate,
	}, "universal-focus-create-"+intentKind)
	if response.Code != http.StatusOK {
		t.Fatalf("create %s focus status=%d body=%s", intentKind, response.Code, response.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(response.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if focus.IntentKind != intentKind || focus.BlueprintID != blueprintID || focus.ThinkingTemplateID != thinkingTemplate {
		t.Fatalf("workspace binding mismatch: %#v", focus)
	}
	return focus
}

func applyUniversalFocusFlow(t *testing.T, focus FocusSpace, signalID string, decisions []focusDecisionInput, products []focusWorkProductInput, actions []focusActionInput) focusAnalysisGetResponse {
	t.Helper()
	insertFocusFlowMeeting(t, "meeting-"+signalID, "本轮资料用于验证 "+focus.Goal, "2026-09-17T11:00:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || claim.RunID == "" || len(claim.Evidence) == 0 {
		t.Fatalf("unexpected %s claim: %#v", focus.IntentKind, claim)
	}
	evidenceID := claim.Evidence[0].ID
	for index := range decisions {
		if len(decisions[index].SourceIDs) == 0 {
			decisions[index].SourceIDs = []string{evidenceID}
		}
	}
	for index := range products {
		if len(products[index].SourceIDs) == 0 {
			products[index].SourceIDs = []string{evidenceID}
		}
	}
	for index := range actions {
		if len(actions[index].SourceIDs) == 0 {
			actions[index].SourceIDs = []string{evidenceID}
		}
	}
	request := focusFlowApplyRequest(claim, signalID)
	request.Decisions = decisions
	request.WorkProducts = products
	request.Actions = actions
	response := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "universal-apply-"+signalID)
	if response.Code != http.StatusOK {
		t.Fatalf("apply %s status=%d body=%s", focus.IntentKind, response.Code, response.Body.String())
	}
	var projection focusAnalysisGetResponse
	if err := json.Unmarshal(response.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	return projection
}

func clarifyAndAcceptUniversalDecisionFlow(t *testing.T, focus FocusSpace, projection focusAnalysisGetResponse, thinkingTemplate string) focusResultFeedbackResponse {
	t.Helper()
	if len(projection.Decisions) != 1 {
		t.Fatalf("expected one decision before clarification: %#v", projection.Decisions)
	}
	decision := projection.Decisions[0]
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: thinkingTemplate, AnalysisRunID: projection.RunID, DecisionID: decision.ID, SkipClarification: true,
	}, "universal-shopping-run-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start shopping decision status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: projection.RunID, DecisionID: decision.ID,
		}, "universal-shopping-run-pre-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume shopping %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	blocked := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "applying", AnalysisRunID: projection.RunID, DecisionID: decision.ID,
	}, "universal-shopping-run-requires-clarification")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "clarification_required") {
		t.Fatalf("shopping decision skipped clarification: status=%d body=%s", blocked.Code, blocked.Body.String())
	}
	waiting := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "waiting_clarification", AnalysisRunID: projection.RunID, DecisionID: decision.ID,
		Question: &decisionQuestionInput{ID: "q_budget", Type: "text", Question: "预算上限是多少？"},
	}, "universal-shopping-run-question")
	if waiting.Code != http.StatusOK {
		t.Fatalf("shopping clarification status=%d body=%s", waiting.Code, waiting.Body.String())
	}
	run = decodeDecisionRunFlow(t, waiting)
	answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, QuestionID: run.CurrentQuestion.ID,
		ExpectedRunRevision: run.Run.Revision, ExpectedAnswerRevision: 0,
		Answer: decisionAnswerInput{Type: "text", Value: json.RawMessage(`"预算上限为 15000 元"`)},
	}, "universal-shopping-run-answer")
	if answered.Code != http.StatusOK {
		t.Fatalf("shopping answer status=%d body=%s", answered.Code, answered.Body.String())
	}
	run = decodeDecisionRunFlow(t, answered)
	for index, status := range []string{"running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: projection.RunID, DecisionID: decision.ID,
		}, "universal-shopping-run-post-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume clarified shopping %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	accepted := appDataMutationFlow(t, "focus_result.feedback", focusResultFeedbackRequest{
		SchemaVersion: focusSpaceSchemaVersion, FocusID: focus.ID, ResultKind: "decision", ResultID: decision.ID,
		ExpectedRevision: decision.Revision, Decision: "accepted",
	}, "universal-shopping-decision-accepted")
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept clarified shopping decision status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var response focusResultFeedbackResponse
	if err := json.Unmarshal(accepted.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func completeUniversalNonDecisionRunFlow(t *testing.T, focus FocusSpace, projection focusAnalysisGetResponse, thinkingTemplate, subject string) decisionRunResponse {
	t.Helper()
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: thinkingTemplate, AnalysisRunID: projection.RunID, SkipClarification: true,
	}, subject+"-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start %s status=%d body=%s", subject, started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: projection.RunID,
		}, subject+"-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s as %s status=%d body=%s", subject, status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	return run
}

func TestUniversalShoppingDecisionFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-shopping", "profile-universal-shopping")
	focus := createUniversalFocusFlow(t, "选择一台适合开发的电脑", "decide", "rational")
	projection := applyUniversalFocusFlow(t, focus, "shopping-signal", []focusDecisionInput{{
		ID: "shopping-decision", Question: "选择哪一台电脑？", Options: []string{"A", "B"}, Recommendation: "选择 A",
		Confidence: "high", Freshness: "current", ValidationItems: []string{"确认价格"},
	}}, nil, []focusActionInput{{
		ID: "shopping-action", Task: "确认 A 的最终价格", Owner: "用户", Priority: "medium",
		Confidence: "medium", Freshness: "current", ValidationItems: []string{"记录报价"},
	}})
	if len(projection.Decisions) != 1 || len(projection.Actions) != 1 || len(projection.WorkProducts) != 0 {
		t.Fatalf("shopping result bundle mismatch: %#v", projection)
	}
	decision := projection.Decisions[0]
	acceptedFeedback := clarifyAndAcceptUniversalDecisionFlow(t, focus, projection, "rational")
	undone := callFocusFlowJSON(t, focusResultFeedbackHandler, http.MethodPost, "/api/focus-result/feedback", focusResultFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, ResultKind: "decision", ResultID: decision.ID,
		ExpectedRevision: acceptedFeedback.Revision, Decision: "draft",
	}, "")
	if undone.Code != http.StatusOK {
		t.Fatalf("undo decision status=%d body=%s", undone.Code, undone.Body.String())
	}
}

func TestUniversalTokyoTravelPlanFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-travel", "profile-universal-travel")
	focus := createUniversalFocusFlow(t, "规划东京五日旅行", "plan", "bounded_rationality")
	projection := applyUniversalFocusFlow(t, focus, "travel-signal", []focusDecisionInput{{
		ID: "travel-area", Question: "住在哪个区域？", Options: []string{"上野", "新宿"}, Recommendation: "选择上野",
		Confidence: "medium", Freshness: "current", ValidationItems: []string{"核对酒店"},
	}}, []focusWorkProductInput{{
		ID: "travel-guide", Type: "travel_guide", Title: "东京五日旅行攻略", Summary: "城市文化路线",
		ContentMarkdown: "# 东京五日旅行攻略\n\n## 第一天\n浅草和上野。", ValidationItems: []string{"确认机票"},
	}}, []focusActionInput{{
		ID: "travel-booking", Task: "预订酒店", Owner: "用户", Priority: "high",
		Confidence: "medium", Freshness: "current", ValidationItems: []string{"保留确认单"},
	}})
	if len(projection.Decisions) != 1 || len(projection.WorkProducts) != 1 || len(projection.Actions) != 1 {
		t.Fatalf("travel result bundle mismatch: %#v", projection)
	}
	completed := completeUniversalNonDecisionRunFlow(t, focus, projection, "bounded_rationality", "universal-travel")
	if completed.Run == nil || completed.Run.Status != "completed" {
		t.Fatalf("travel plan did not complete: %#v", completed)
	}
	product := projection.WorkProducts[0]
	feedback := callFocusFlowJSON(t, focusWorkProductFeedbackHandler, http.MethodPost, "/api/internal/focus-work-products/feedback", focusWorkProductFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: product.ID, ExpectedRevision: product.Revision, Decision: "accepted",
	}, "")
	if feedback.Code != http.StatusOK {
		t.Fatalf("accept travel guide status=%d body=%s", feedback.Code, feedback.Body.String())
	}
}

func TestUniversalACMArticleCreationFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-create", "profile-universal-create")
	focus := createUniversalFocusFlow(t, "写一篇 ACM Computer 文章", "create", "first_principles")
	projection := applyUniversalFocusFlow(t, focus, "create-signal", nil, []focusWorkProductInput{
		{ID: "article-brief", Type: "writing_brief", Title: "文章 Brief", ContentMarkdown: "# Brief\n\n面向系统研究者。", ValidationItems: []string{"确认读者"}},
		{ID: "article-outline", Type: "outline", Title: "文章大纲", ContentMarkdown: "# 大纲\n\n## 问题\n\n## 方法\n\n## 评估", ValidationItems: []string{"补齐引用"}},
	}, []focusActionInput{{
		ID: "article-draft", Task: "在 Obsidian 中完成初稿", Owner: "用户", Priority: "medium",
		Confidence: "high", Freshness: "current", ValidationItems: []string{"复制 Markdown 大纲"},
	}})
	if len(projection.WorkProducts) != 2 || len(projection.Actions) != 1 || len(projection.Decisions) != 0 {
		t.Fatalf("creation result bundle mismatch: %#v", projection)
	}
	completed := completeUniversalNonDecisionRunFlow(t, focus, projection, "first_principles", "universal-create")
	if completed.Run == nil || completed.Run.Status != "completed" {
		t.Fatalf("creation workflow did not complete: %#v", completed)
	}
	for _, product := range projection.WorkProducts {
		data, ok := product.Data.(map[string]any)
		if !ok || data["content_markdown"] == "" {
			t.Fatalf("creation output lost markdown: %#v", product)
		}
	}
}

func TestUniversalContinuousMonitoringFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-monitor", "profile-universal-monitor")
	focus := createUniversalFocusFlow(t, "持续关注 AI 芯片", "monitor", "guided")
	projection := applyUniversalFocusFlow(t, focus, "monitor-signal", nil, nil, nil)
	if len(projection.Signals) != 1 || len(projection.Decisions) != 0 || len(projection.WorkProducts) != 0 || len(projection.Actions) != 0 {
		t.Fatalf("monitoring should emit a signal without forced outputs: %#v", projection)
	}
	completed := completeUniversalNonDecisionRunFlow(t, focus, projection, "guided", "universal-monitor")
	if completed.Run == nil || completed.Run.Status != "completed" {
		t.Fatalf("monitoring workflow did not complete: %#v", completed)
	}
	feedback := callFocusFlowJSON(t, focusResultFeedbackHandler, http.MethodPost, "/api/focus-result/feedback", focusResultFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, ResultKind: "signal", ResultID: projection.Signals[0].ID,
		ExpectedRevision: projection.Signals[0].Revision, Decision: "accepted",
	}, "")
	if feedback.Code != http.StatusOK {
		t.Fatalf("accept monitoring signal status=%d body=%s", feedback.Code, feedback.Body.String())
	}
	refreshed := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID+"&run_id="+projection.RunID, nil, "")
	if refreshed.Code != http.StatusOK || !strings.Contains(refreshed.Body.String(), `"feedback_status":"accepted"`) {
		t.Fatalf("monitor refresh lost feedback status=%d body=%s", refreshed.Code, refreshed.Body.String())
	}
}

func TestUniversalCompletionRejectsChangedBlueprintVersionFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-blueprint-version", "profile-universal-blueprint-version")
	focus := createUniversalFocusFlow(t, "持续关注端侧模型变化", "monitor", "guided")
	projection := applyUniversalFocusFlow(t, focus, "blueprint-version-signal", nil, nil, nil)
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "guided", AnalysisRunID: projection.RunID, SkipClarification: true,
	}, "universal-blueprint-version-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start blueprint version flow status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: projection.RunID,
		}, "universal-blueprint-version-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume blueprint version flow as %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	if _, err := db.Exec(`UPDATE focus_decision_runs SET blueprint_version='0' WHERE id=? AND owner_id=? AND profile_id=?`, run.Run.ID, appDataScope.OwnerID, appDataScope.ProfileID); err != nil {
		t.Fatal(err)
	}
	completed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "completed", AnalysisRunID: projection.RunID,
	}, "universal-blueprint-version-complete")
	if completed.Code != http.StatusConflict || !strings.Contains(completed.Body.String(), "blueprint_changed") {
		t.Fatalf("changed blueprint version was accepted: status=%d body=%s", completed.Code, completed.Body.String())
	}
}

func TestUniversalCreateCompletionRequiresWorkProductFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-create-contract", "profile-universal-create-contract")
	focus := createUniversalFocusFlow(t, "写一篇系统架构文章", "create", "first_principles")
	projection := applyUniversalFocusFlow(t, focus, "create-contract-signal", nil, nil, nil)
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "first_principles", AnalysisRunID: projection.RunID, SkipClarification: true,
	}, "universal-create-contract-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start create contract status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	for index, status := range []string{"collecting_context", "running_ai", "applying"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: projection.RunID,
		}, "universal-create-contract-resume-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume create contract %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	completed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "completed", AnalysisRunID: projection.RunID,
	}, "universal-create-contract-complete")
	if completed.Code != http.StatusConflict || !strings.Contains(completed.Body.String(), "work_product_required") {
		t.Fatalf("create workflow completed without a work product: status=%d body=%s", completed.Code, completed.Body.String())
	}
}

func TestUniversalReanalysisSwitchesThinkingTemplateAndPreservesHistoryFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-universal-reanalysis", "profile-universal-reanalysis")
	focus := createUniversalFocusFlow(t, "撰写端侧 AI 架构文章", "create", "guided")
	first := applyUniversalFocusFlow(t, focus, "reanalysis-first-signal", nil, []focusWorkProductInput{{
		ID: "reanalysis-first-outline", Type: "outline", Title: "第一版大纲",
		ContentMarkdown: "# 第一版大纲\n\n## 背景\n\n## 架构", ValidationItems: []string{"核对第一版资料"},
	}}, nil)
	if len(first.WorkProducts) != 1 || first.RunID == "" {
		t.Fatalf("first analysis result mismatch: %#v", first)
	}
	firstProduct := first.WorkProducts[0]
	accepted := callFocusFlowJSON(t, focusWorkProductFeedbackHandler, http.MethodPost, "/api/internal/focus-work-products/feedback", focusWorkProductFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: firstProduct.ID,
		ExpectedRevision: firstProduct.Revision, Decision: "accepted",
	}, "universal-reanalysis-first-accepted")
	if accepted.Code != http.StatusOK {
		t.Fatalf("accept first work product status=%d body=%s", accepted.Code, accepted.Body.String())
	}

	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "first_principles", SkipClarification: true,
	}, "universal-reanalysis-switch-template")
	if started.Code != http.StatusCreated {
		t.Fatalf("start reanalysis status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	if run.Run == nil || run.Run.ThinkingTemplateID != "first_principles" || run.Run.AnalysisRunID != "" {
		t.Fatalf("fresh reanalysis did not snapshot the selected template: %#v", run.Run)
	}
	collecting := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "collecting_context",
	}, "universal-reanalysis-collect")
	if collecting.Code != http.StatusOK {
		t.Fatalf("collect reanalysis status=%d body=%s", collecting.Code, collecting.Body.String())
	}
	run = decodeDecisionRunFlow(t, collecting)

	insertFocusFlowMeeting(t, "meeting-reanalysis-second", "新增资料要求从基本约束重新推导文章结构。", "2026-09-17T12:00:00Z")
	secondClaim := readFocusFlowClaim(t, focus.ID)
	if secondClaim.Status != "claimed" || secondClaim.RunID == "" || secondClaim.RunID == first.RunID || secondClaim.ThinkingTemplateID != "first_principles" || len(secondClaim.Evidence) < 2 {
		t.Fatalf("fresh analysis claim mismatch: %#v", secondClaim)
	}
	running := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
		Status: "running_ai", AnalysisRunID: secondClaim.RunID,
	}, "universal-reanalysis-running")
	if running.Code != http.StatusOK {
		t.Fatalf("bind fresh analysis status=%d body=%s", running.Code, running.Body.String())
	}
	run = decodeDecisionRunFlow(t, running)

	request := focusFlowApplyRequest(secondClaim, "reanalysis-second-signal")
	request.Signals = nil
	request.Decisions = nil
	request.Actions = nil
	request.WorkProducts = []focusWorkProductInput{{
		ID: "reanalysis-second-outline", Type: "outline", Title: "第一性原理版大纲",
		ContentMarkdown: "# 第一性原理版大纲\n\n## 基本约束\n\n## 推导",
		SourceIDs:       []string{secondClaim.Evidence[0].ID}, ValidationItems: []string{"核对新增资料"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "universal-reanalysis-apply-second")
	var second focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &second) != nil {
		t.Fatalf("apply fresh analysis status=%d body=%s", applied.Code, applied.Body.String())
	}
	if second.RunID != secondClaim.RunID || len(second.WorkProducts) != 1 {
		t.Fatalf("second analysis result mismatch: %#v", second)
	}
	for index, status := range []string{"applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: second.RunID,
		}, "universal-reanalysis-finish-"+string(rune('1'+index)))
		if resumed.Code != http.StatusOK {
			t.Fatalf("finish reanalysis %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}

	latestResponse := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID, nil, "")
	var latest focusAnalysisGetResponse
	if latestResponse.Code != http.StatusOK || json.Unmarshal(latestResponse.Body.Bytes(), &latest) != nil {
		t.Fatalf("latest analysis status=%d body=%s", latestResponse.Code, latestResponse.Body.String())
	}
	if latest.RunID != second.RunID || len(latest.Runs) < 2 || len(latest.WorkProducts) != 1 || latest.WorkProducts[0].ID != second.WorkProducts[0].ID {
		t.Fatalf("latest analysis did not select the new run while retaining history: %#v", latest)
	}

	historyResponse := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID+"&run_id="+first.RunID, nil, "")
	var history focusAnalysisGetResponse
	if historyResponse.Code != http.StatusOK || json.Unmarshal(historyResponse.Body.Bytes(), &history) != nil {
		t.Fatalf("history analysis status=%d body=%s", historyResponse.Code, historyResponse.Body.String())
	}
	if history.RunID != first.RunID || len(history.WorkProducts) != 1 || history.WorkProducts[0].ID != firstProduct.ID || history.WorkProducts[0].FeedbackStatus != "accepted" {
		t.Fatalf("accepted historical result was overwritten: %#v", history)
	}
	var selectedThinking string
	if err := db.QueryRow(`SELECT thinking_template_id FROM focus_workspace_bindings WHERE focus_id=? AND owner_id=? AND profile_id=?`, focus.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&selectedThinking); err != nil {
		t.Fatal(err)
	}
	if selectedThinking != "first_principles" {
		t.Fatalf("selected thinking template was not persisted: %q", selectedThinking)
	}
}

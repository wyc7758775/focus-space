package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFocusFeedbackAllowsAnalysisWritePreservesReviewedStates(t *testing.T) {
	tests := []struct {
		name   string
		status string
		exists bool
		allow  bool
	}{
		{name: "missing feedback", exists: false, allow: true},
		{name: "unreviewed draft", status: "draft", exists: true, allow: true},
		{name: "accepted", status: "accepted", exists: true, allow: false},
		{name: "rejected", status: "rejected", exists: true, allow: false},
		{name: "snoozed", status: "snoozed", exists: true, allow: false},
		{name: "edited", status: "edited", exists: true, allow: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := focusFeedbackAllowsAnalysisWrite(tt.status, tt.exists); got != tt.allow {
				t.Fatalf("focusFeedbackAllowsAnalysisWrite(%q, %t)=%t, want %t", tt.status, tt.exists, got, tt.allow)
			}
		})
	}
}

func callFocusFlowJSON(t *testing.T, handler http.HandlerFunc, method, path string, body any, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if idempotencyKey != "" {
		request.Header.Set("X-Zettlab-Idempotency-Key", idempotencyKey)
	}
	response := httptest.NewRecorder()
	handler(response, request)
	return response
}

func createFocusFlowSpace(t *testing.T, goal string) FocusSpace {
	t.Helper()
	title := goal + " 焦点"
	kind := "monitor"
	horizonMode := "open"
	category := "趋势跟踪"
	decisionMode := "slow_burn"
	workflowState := "accumulating"
	response := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
		SchemaVersion:    focusSpaceSchemaVersion,
		Action:           "create",
		ExpectedRevision: 0,
		Title:            &title,
		Category:         &category,
		Goal:             &goal,
		DecisionMode:     &decisionMode,
		WorkflowState:    &workflowState,
		Kind:             &kind,
		HorizonMode:      &horizonMode,
	}, "focus-flow-create")
	if response.Code != http.StatusOK {
		t.Fatalf("create focus status=%d body=%s", response.Code, response.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(response.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if focus.ID == "" || focus.Revision != 1 || focus.Goal != goal || focus.Category != category || focus.DecisionMode != decisionMode || focus.WorkflowState != workflowState {
		t.Fatalf("unexpected created focus: %#v", focus)
	}
	return focus
}

func insertFocusFlowMeeting(t *testing.T, recordID, summary, updatedAt string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO recordings(record_id,title,status,started_at,created_at,updated_at,summary) VALUES(?,?,?,?,?,?,?)`, recordID, "AI Computer 产品评审", "ready", "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z", updatedAt, summary); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO focus_space_sources(focus_id,owner_id,profile_id,source_kind,source_id,enabled,revision,created_at,updated_at)
SELECT id,owner_id,profile_id,'meeting',?,1,1,?,? FROM focus_spaces
WHERE owner_id=? AND profile_id=? ORDER BY created_at DESC,id DESC LIMIT 1`, recordID, updatedAt, updatedAt, appDataScope.OwnerID, appDataScope.ProfileID); err != nil {
		t.Fatal(err)
	}
}

func TestFocusAnalysisEmptyLinksDoNotScanGlobalAdaptersFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-empty-sources", "profile-focus-empty-sources")
	focus := createFocusFlowSpace(t, "端侧 AI 上线方向")
	if _, err := db.Exec(`INSERT INTO recordings(record_id,title,status,started_at,created_at,updated_at,summary) VALUES(?,?,?,?,?,?,?)`,
		"meeting-unrelated-global", "完全无关的财务会议", "ready", "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z", "2026-08-15T09:30:00Z", "讨论报销制度与办公室装修。"); err != nil {
		t.Fatal(err)
	}

	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "empty" || claim.RunID != "" || len(claim.Evidence) != 0 || claim.AvailableCount != 0 {
		t.Fatalf("empty Focus scanned unrelated global adapters: %#v", claim)
	}
}

func readFocusFlowClaim(t *testing.T, focusID string) focusAnalysisReadResponse {
	t.Helper()
	path := "/api/focus-analysis/read?focus_id=" + url.QueryEscape(focusID) + "&scope=question&limit=5"
	response := callFocusFlowJSON(t, focusAnalysisReadHandler, http.MethodGet, path, nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("read focus analysis status=%d body=%s", response.Code, response.Body.String())
	}
	var claim focusAnalysisReadResponse
	if err := json.Unmarshal(response.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	return claim
}

func focusFlowApplyRequest(claim focusAnalysisReadResponse, clientResultID string) focusAnalysisApplyRequest {
	evidenceID := ""
	if len(claim.Evidence) > 0 {
		evidenceID = claim.Evidence[0].ID
	}
	return focusAnalysisApplyRequest{
		SchemaVersion:   focusSpaceSchemaVersion,
		RunID:           claim.RunID,
		FocusID:         claim.FocusID,
		FocusRevision:   claim.FocusRevision,
		EvidenceSetHash: claim.EvidenceSetHash,
		PromptVersion:   claim.PromptVersion,
		Status:          "applied",
		Signals: []focusSignalInput{{
			ID:                 clientResultID,
			Title:              "本地 AI 产品需求增强",
			Detail:             "同一批会议证据显示用户持续关注低延迟的本地智能工作流。",
			SourceIDs:          []string{evidenceID},
			SupportingEvidence: []string{"产品评审明确提出本地低延迟需求"},
			Counterevidence:    []string{"尚无付费转化数据"},
			Confidence:         "high",
			Freshness:          "current",
			ValidationItems:    []string{"验证三个真实用户场景"},
		}},
		Actions:   []focusActionInput{},
		Decisions: []focusDecisionInput{},
		Usage:     focusAnalysisUsage{InputTokens: 320, OutputTokens: 120},
	}
}

func TestFocusSpaceSingleAnalysisSharedResultAndFeedbackCASFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-flow", "profile-focus-flow")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-flow", "用户需要更低延迟的 AI Computer 本地工作流。", "2026-08-15T09:30:00Z")

	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || claim.RunID == "" || claim.FocusRevision != focus.Revision || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected focus claim: %#v", claim)
	}
	if claim.FocusGoal != focus.Goal || claim.FocusTitle != focus.Title || claim.DecisionQuestion != focus.DecisionQuestion {
		t.Fatalf("focus context lost before model read: %#v", claim)
	}
	applyRequest := focusFlowApplyRequest(claim, "signal-flow")
	apply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-flow-apply-1")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply focus analysis status=%d body=%s", apply.Code, apply.Body.String())
	}
	var applied focusAnalysisGetResponse
	if err := json.Unmarshal(apply.Body.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.RunID != claim.RunID || len(applied.Signals) != 1 || applied.Signals[0].AnalysisRunID != claim.RunID {
		t.Fatalf("apply did not return the claimed run: %#v", applied)
	}
	var evidenceFocus, evidenceRun, evidencePrompt, evidenceVersion string
	var evidenceRank int
	if err := db.QueryRow(`SELECT focus_id,analysis_run_id,rank,prompt_version,evidence_version FROM evidence WHERE id=?`, claim.Evidence[0].ID).Scan(&evidenceFocus, &evidenceRun, &evidenceRank, &evidencePrompt, &evidenceVersion); err != nil {
		t.Fatalf("focus evidence projection was not persisted: %v", err)
	}
	if evidenceFocus != focus.ID || evidenceRun != claim.RunID || evidenceRank != 1 || evidencePrompt != claim.PromptVersion || evidenceVersion == "" {
		t.Fatalf("unexpected focus evidence provenance: focus=%q run=%q rank=%d prompt=%q version=%q", evidenceFocus, evidenceRun, evidenceRank, evidencePrompt, evidenceVersion)
	}

	replay := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", applyRequest, "focus-flow-apply-1")
	if replay.Code != http.StatusOK || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" {
		t.Fatalf("idempotent replay status=%d replay=%q body=%s", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String())
	}
	var storedResults int
	if err := db.QueryRow(`SELECT COUNT(*) FROM signals WHERE focus_id=? AND analysis_run_id=?`, focus.ID, claim.RunID).Scan(&storedResults); err != nil || storedResults != 1 {
		t.Fatalf("idempotent apply stored results=%d err=%v", storedResults, err)
	}

	getPath := "/api/focus-analysis?focus_id=" + url.QueryEscape(focus.ID) + "&run_id=" + url.QueryEscape(claim.RunID)
	get := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, getPath, nil, "")
	if get.Code != http.StatusOK {
		t.Fatalf("get focus analysis status=%d body=%s", get.Code, get.Body.String())
	}
	var shared focusAnalysisGetResponse
	if err := json.Unmarshal(get.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	if shared.RunID != claim.RunID || len(shared.Evidence) != 1 || len(shared.Signals) != 1 || shared.Signals[0].ID != applied.Signals[0].ID {
		t.Fatalf("chat/dashboard read did not share one run: %#v", shared)
	}

	feedbackRequest := focusResultFeedbackRequest{
		SchemaVersion:    focusSpaceSchemaVersion,
		FocusID:          focus.ID,
		ResultKind:       "signal",
		ResultID:         shared.Signals[0].ID,
		ExpectedRevision: shared.Signals[0].Revision,
		Decision:         "accepted",
	}
	feedback := callFocusFlowJSON(t, focusResultFeedbackHandler, http.MethodPost, "/api/focus-result/feedback", feedbackRequest, "")
	if feedback.Code != http.StatusOK {
		t.Fatalf("feedback status=%d body=%s", feedback.Code, feedback.Body.String())
	}
	var accepted focusResultFeedbackResponse
	if err := json.Unmarshal(feedback.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.Revision != feedbackRequest.ExpectedRevision+1 || accepted.Status != "accepted" {
		t.Fatalf("unexpected feedback response: %#v", accepted)
	}
	staleFeedback := callFocusFlowJSON(t, focusResultFeedbackHandler, http.MethodPost, "/api/focus-result/feedback", feedbackRequest, "")
	if staleFeedback.Code != http.StatusConflict {
		t.Fatalf("stale feedback status=%d body=%s", staleFeedback.Code, staleFeedback.Body.String())
	}

	afterFeedback := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, getPath, nil, "")
	if afterFeedback.Code != http.StatusOK {
		t.Fatalf("get after feedback status=%d body=%s", afterFeedback.Code, afterFeedback.Body.String())
	}
	if err := json.Unmarshal(afterFeedback.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared.Signals) != 1 || shared.Signals[0].Revision != accepted.Revision || shared.Signals[0].FeedbackStatus != "accepted" {
		t.Fatalf("accepted feedback was not projected: %#v", shared.Signals)
	}
}

func TestFocusAnalysisApplyTraversesClosedAppDataBridgeFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-bridge", "profile-focus-bridge")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-bridge", "AI Computer 产品需要更低的端侧延迟。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	payload, err := json.Marshal(focusFlowApplyRequest(claim, "signal-bridge"))
	if err != nil {
		t.Fatal(err)
	}
	var boundedPayload map[string]any
	if err := json.Unmarshal(payload, &boundedPayload); err != nil {
		t.Fatal(err)
	}
	delete(boundedPayload, "focus_revision")
	delete(boundedPayload, "status")
	boundedPayload["prompt_version"] = "evidence-v1-summary"
	// Reproduce the real model failure: a valid claimed run with one copied
	// character wrong in the opaque evidence hash must still be recoverable.
	boundedPayload["evidence_set_hash"] = strings.Repeat("f", 64)
	if _, err := db.Exec(`UPDATE focus_spaces SET decision_question=? WHERE id=?`, "是否继续验证本地 AI 方向？", claim.FocusID); err != nil {
		t.Fatal(err)
	}
	evidenceID := claim.Evidence[0].ID
	boundedPayload["decisions"] = []map[string]any{{
		"id":                  "decision-bridge",
		"options":             []string{"继续验证", "暂停验证"},
		"recommendation":      "继续验证",
		"expected_outcome":    "获得可复核结果",
		"review_at":           "2026-09-14",
		"source_ids":          []string{evidenceID},
		"supporting_evidence": []string{"会议记录支持继续验证"},
		"counterevidence":     []string{},
		"confidence":          "medium",
		"freshness":           "current",
		"validation_items":    []string{"复核真实场景"},
	}}
	boundedPayload["actions"] = []map[string]any{{
		"id":                  "action-bridge",
		"owner":               "",
		"due_at":              "",
		"priority":            "medium",
		"source_ids":          []string{evidenceID},
		"supporting_evidence": []string{"会议记录支持继续验证"},
		"counterevidence":     []string{},
		"confidence":          "medium",
		"freshness":           "current",
		"validation_items":    []string{"复核真实场景"},
	}}
	payload, err = json.Marshal(boundedPayload)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/app-data/v1/operations/focus_analysis.apply", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Zettlab-Idempotency-Key", "focus-bridge-apply-1")
	response := httptest.NewRecorder()
	appDataOperationHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("app_data apply status=%d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"prompt_version":"focus-v1"`) || !strings.Contains(response.Body.String(), `"analysis_run_id":"`+claim.RunID+`"`) || !strings.Contains(response.Body.String(), `"question":"是否继续验证本地 AI 方向？"`) || !strings.Contains(response.Body.String(), `"task":"复核真实场景"`) {
		t.Fatalf("app_data apply did not return the persisted run: %s", response.Body.String())
	}
	var storedOwner, storedReviewAt string
	if err := db.QueryRow(`SELECT owner FROM actions WHERE analysis_run_id=?`, claim.RunID).Scan(&storedOwner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT review_at FROM decisions WHERE analysis_run_id=?`, claim.RunID).Scan(&storedReviewAt); err != nil {
		t.Fatal(err)
	}
	if storedOwner != "负责人待确认" || storedReviewAt != "2026-09-14T00:00:00Z" {
		t.Fatalf("normalized persisted fields owner=%q review_at=%q", storedOwner, storedReviewAt)
	}

	forbidden := bytes.Replace(payload, []byte(`"prompt_version":"evidence-v1-summary"`), []byte(`"prompt_version":"evidence-v1-summary","model":"override"`), 1)
	request = httptest.NewRequest(http.MethodPost, "/api/app-data/v1/operations/focus_analysis.apply", bytes.NewReader(forbidden))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Zettlab-Idempotency-Key", "focus-bridge-forbidden-1")
	response = httptest.NewRecorder()
	appDataOperationHandler(response, request)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "model") {
		t.Fatalf("unsafe model field was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFocusCreateToleratesLegacySourceCategoryHintsFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-legacy-sources", "profile-focus-legacy-sources")
	payload := `{"schema_version":1,"action":"create","expected_revision":0,"title":"端侧 AI 判断","goal":"判断是否优先投入端侧 AI","decision_question":"证据是否足够？","sources":["meeting","reader","obsidian"]}`

	created := appDataCall(t, "/api/app-data/v1/operations/focus_space.update", payload, "focus-create-legacy-source-categories")
	if created.Code != http.StatusOK {
		t.Fatalf("legacy source category hints blocked focus creation: status=%d body=%s", created.Code, created.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(created.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if focus.ID == "" || focus.Goal != "判断是否优先投入端侧 AI" || len(focus.Sources) != 0 {
		t.Fatalf("legacy categories must not become unscoped source links: %#v", focus)
	}
}

func TestFocusCreateRejectsUnknownStringSourceHintsFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-invalid-sources", "profile-focus-invalid-sources")
	payload := `{"schema_version":1,"action":"create","expected_revision":0,"title":"端侧 AI 判断","goal":"判断是否优先投入端侧 AI","decision_question":"证据是否足够？","sources":["meeting","unscoped-secret"]}`

	created := appDataCall(t, "/api/app-data/v1/operations/focus_space.update", payload, "focus-create-invalid-string-sources")
	if created.Code != http.StatusBadRequest {
		t.Fatalf("unknown string source hints were silently discarded: status=%d body=%s", created.Code, created.Body.String())
	}
}

func TestFocusOperationsTraverseClosedAppDataBridgeFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-app-data", "profile-focus-app-data")

	title := "AI Computer 焦点"
	goal := "AI Computer"
	kind := "monitor"
	horizonMode := "open"
	createPayload, err := json.Marshal(focusSpaceMutation{
		SchemaVersion:    focusSpaceSchemaVersion,
		Action:           "create",
		ExpectedRevision: 0,
		Title:            &title,
		Goal:             &goal,
		Kind:             &kind,
		HorizonMode:      &horizonMode,
	})
	if err != nil {
		t.Fatal(err)
	}
	created := appDataCall(t, "/api/app-data/v1/operations/focus_space.update", string(createPayload), "focus-app-data-create")
	if created.Code != http.StatusOK {
		t.Fatalf("focus_space.update status=%d body=%s", created.Code, created.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(created.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if focus.ID == "" || focus.Revision != 1 || focus.Goal != goal {
		t.Fatalf("unexpected focus_space.update response: %#v", focus)
	}
	createReplay := appDataCall(t, "/api/app-data/v1/operations/focus_space.update", string(createPayload), "focus-app-data-create")
	if createReplay.Code != http.StatusOK || createReplay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || createReplay.Body.String() != created.Body.String() {
		t.Fatalf("focus_space.update replay status=%d replay=%q equal=%v body=%s", createReplay.Code, createReplay.Header().Get("X-Zettlab-Idempotent-Replay"), createReplay.Body.String() == created.Body.String(), createReplay.Body.String())
	}

	readSpace := appDataCall(t, "/api/app-data/v1/operations/focus_space.get?focus_id="+url.QueryEscape(focus.ID), "", "")
	if readSpace.Code != http.StatusOK {
		t.Fatalf("focus_space.get status=%d body=%s", readSpace.Code, readSpace.Body.String())
	}
	var focusState FocusSpaceGetResponse
	if err := json.Unmarshal(readSpace.Body.Bytes(), &focusState); err != nil {
		t.Fatal(err)
	}
	if focusState.Focus == nil || focusState.Focus.ID != focus.ID || len(focusState.Focuses) != 1 {
		t.Fatalf("focus_space.get did not return created focus: %#v", focusState)
	}
	executing := "executing"
	invalidTransition, err := json.Marshal(focusSpaceMutation{SchemaVersion: focusSpaceSchemaVersion, Action: "update", FocusID: focus.ID, ExpectedRevision: focus.Revision, WorkflowState: &executing})
	if err != nil {
		t.Fatal(err)
	}
	rejectedTransition := appDataCall(t, "/api/app-data/v1/operations/focus_space.update", string(invalidTransition), "focus-app-data-invalid-transition")
	if rejectedTransition.Code != http.StatusBadRequest || !strings.Contains(rejectedTransition.Body.String(), "先确认决策") {
		t.Fatalf("Focus V3 workflow invariant was not enforced: status=%d body=%s", rejectedTransition.Code, rejectedTransition.Body.String())
	}

	insertFocusFlowMeeting(t, "meeting-focus-app-data", "AI Computer 需要更低延迟的设备端推理。", "2026-08-15T09:30:00Z")
	readPath := "/api/app-data/v1/operations/focus_analysis.read?focus_id=" + url.QueryEscape(focus.ID) + "&scope=question&limit=5&include_full_text=0"
	claimResponse := appDataCall(t, readPath, "", "")
	if claimResponse.Code != http.StatusOK {
		t.Fatalf("focus_analysis.read status=%d body=%s", claimResponse.Code, claimResponse.Body.String())
	}
	var claim focusAnalysisReadResponse
	if err := json.Unmarshal(claimResponse.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	if claim.Status != "claimed" || claim.RunID == "" || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected focus_analysis.read response: %#v", claim)
	}

	applyPayload, err := json.Marshal(focusFlowApplyRequest(claim, "signal-app-data"))
	if err != nil {
		t.Fatal(err)
	}
	applied := appDataCall(t, "/api/app-data/v1/operations/focus_analysis.apply", string(applyPayload), "focus-app-data-apply")
	if applied.Code != http.StatusOK {
		t.Fatalf("focus_analysis.apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	var appliedState focusAnalysisGetResponse
	if err := json.Unmarshal(applied.Body.Bytes(), &appliedState); err != nil {
		t.Fatal(err)
	}
	if appliedState.RunID != claim.RunID || len(appliedState.Signals) != 1 {
		t.Fatalf("focus_analysis.apply did not persist claimed run: %#v", appliedState)
	}

	getPath := "/api/app-data/v1/operations/focus_analysis.get?focus_id=" + url.QueryEscape(focus.ID) + "&run_id=" + url.QueryEscape(claim.RunID)
	got := appDataCall(t, getPath, "", "")
	if got.Code != http.StatusOK {
		t.Fatalf("focus_analysis.get status=%d body=%s", got.Code, got.Body.String())
	}
	var shared focusAnalysisGetResponse
	if err := json.Unmarshal(got.Body.Bytes(), &shared); err != nil {
		t.Fatal(err)
	}
	if shared.RunID != appliedState.RunID || len(shared.Signals) != 1 || shared.Signals[0].ID != appliedState.Signals[0].ID {
		t.Fatalf("focus_analysis.get returned a different result: applied=%#v get=%#v", appliedState.Signals, shared.Signals)
	}

	feedbackPayload, err := json.Marshal(focusResultFeedbackRequest{
		SchemaVersion:    focusSpaceSchemaVersion,
		FocusID:          focus.ID,
		ResultKind:       "signal",
		ResultID:         shared.Signals[0].ID,
		ExpectedRevision: shared.Signals[0].Revision,
		Decision:         "accepted",
	})
	if err != nil {
		t.Fatal(err)
	}
	feedback := appDataCall(t, "/api/app-data/v1/operations/focus_result.feedback", string(feedbackPayload), "focus-app-data-feedback")
	if feedback.Code != http.StatusOK {
		t.Fatalf("focus_result.feedback status=%d body=%s", feedback.Code, feedback.Body.String())
	}
	feedbackReplay := appDataCall(t, "/api/app-data/v1/operations/focus_result.feedback", string(feedbackPayload), "focus-app-data-feedback")
	if feedbackReplay.Code != http.StatusOK || feedbackReplay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || feedbackReplay.Body.String() != feedback.Body.String() {
		t.Fatalf("focus_result.feedback replay status=%d replay=%q equal=%v body=%s", feedbackReplay.Code, feedbackReplay.Header().Get("X-Zettlab-Idempotent-Replay"), feedbackReplay.Body.String() == feedback.Body.String(), feedbackReplay.Body.String())
	}
	afterFeedback := appDataCall(t, getPath, "", "")
	if afterFeedback.Code != http.StatusOK || !strings.Contains(afterFeedback.Body.String(), `"feedback_status":"accepted"`) {
		t.Fatalf("focus_analysis.get did not project accepted feedback: status=%d body=%s", afterFeedback.Code, afterFeedback.Body.String())
	}
}

func TestFocusAnalysisRetryReusesFailedReservationWhenDailyLimitIsExhaustedFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-retry-budget", "profile-focus-retry-budget")
	focus := createFocusFlowSpace(t, "P0 验收行动")
	insertFocusFlowMeeting(t, "meeting-focus-retry-budget-1", "第一条 P0 验收行动真实资料。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected initial claim: %#v", claim)
	}
	failed := focusAnalysisApplyRequest{
		SchemaVersion: focusSpaceSchemaVersion, RunID: claim.RunID, FocusID: claim.FocusID,
		FocusRevision: claim.FocusRevision, EvidenceSetHash: claim.EvidenceSetHash,
		PromptVersion: claim.PromptVersion, Status: "failed", ErrorCode: "apply_invalid_request",
	}
	failedResponse := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", failed, "focus-retry-budget-failed")
	if failedResponse.Code != http.StatusOK {
		t.Fatalf("failed run writeback status=%d body=%s", failedResponse.Code, failedResponse.Body.String())
	}
	if _, err := db.Exec(`UPDATE reader_focus_policy SET daily_article_limit=1 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	insertFocusFlowMeeting(t, "meeting-focus-retry-budget-2", "第二条更新后的 P0 验收行动真实资料。", "2026-08-16T09:30:00Z")
	retried := callFocusFlowJSON(t, focusAnalysisReadHandler, http.MethodGet,
		"/api/focus-analysis/read?focus_id="+focus.ID+"&scope=question&limit=5&include_full_text=0", nil, "")
	if retried.Code != http.StatusOK {
		t.Fatalf("retry claim status=%d body=%s", retried.Code, retried.Body.String())
	}
	var retryClaim focusAnalysisReadResponse
	if err := json.Unmarshal(retried.Body.Bytes(), &retryClaim); err != nil {
		t.Fatal(err)
	}
	if retryClaim.RunID != claim.RunID || retryClaim.Status != "claimed" || len(retryClaim.Evidence) != 1 {
		t.Fatalf("retry did not reuse failed reservation: initial=%#v retry=%#v", claim, retryClaim)
	}
}

func TestFocusAnalysisBusyAndStaleClaimsFailClosedFlow(t *testing.T) {
	t.Run("busy claim reuses the active run", func(t *testing.T) {
		db = openTestDB(t)
		useStableContextScope(t, "owner-focus-busy", "profile-focus-busy")
		focus := createFocusFlowSpace(t, "AI Computer")
		insertFocusFlowMeeting(t, "meeting-focus-busy", "第一版 AI Computer 需求。", "2026-08-15T09:30:00Z")
		first := readFocusFlowClaim(t, focus.ID)
		if first.Status != "claimed" || first.RunID == "" {
			t.Fatalf("unexpected first claim: %#v", first)
		}
		if _, err := db.Exec(`UPDATE recordings SET summary=?,updated_at=? WHERE record_id=?`, "第二版 AI Computer 需求，证据版本已变化。", "2026-08-15T10:00:00Z", "meeting-focus-busy"); err != nil {
			t.Fatal(err)
		}
		busy := readFocusFlowClaim(t, focus.ID)
		if busy.Status != "busy" || busy.RunID != first.RunID || busy.EvidenceSetHash != first.EvidenceSetHash {
			t.Fatalf("changed evidence did not return the active busy claim: first=%#v busy=%#v", first, busy)
		}
		var claimedRuns int
		if err := db.QueryRow(`SELECT COUNT(*) FROM focus_analysis_runs WHERE focus_id=? AND status='claimed'`, focus.ID).Scan(&claimedRuns); err != nil || claimedRuns != 1 {
			t.Fatalf("busy selector claimed runs=%d err=%v", claimedRuns, err)
		}
	})

	t.Run("focus revision supersedes the claim", func(t *testing.T) {
		db = openTestDB(t)
		useStableContextScope(t, "owner-focus-stale", "profile-focus-stale")
		focus := createFocusFlowSpace(t, "AI Computer")
		insertFocusFlowMeeting(t, "meeting-focus-stale", "AI Computer 原型评审结论。", "2026-08-15T09:30:00Z")
		claim := readFocusFlowClaim(t, focus.ID)
		updatedGoal := "AI Computer 企业版"
		update := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
			SchemaVersion:    focusSpaceSchemaVersion,
			Action:           "update",
			FocusID:          focus.ID,
			ExpectedRevision: focus.Revision,
			Goal:             &updatedGoal,
		}, "focus-flow-update-stale")
		if update.Code != http.StatusOK {
			t.Fatalf("update focus status=%d body=%s", update.Code, update.Body.String())
		}

		staleApply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", focusFlowApplyRequest(claim, "signal-stale"), "focus-flow-stale-apply")
		if staleApply.Code != http.StatusConflict {
			t.Fatalf("stale apply status=%d body=%s", staleApply.Code, staleApply.Body.String())
		}
		var runStatus, errorCode string
		if err := db.QueryRow(`SELECT status,error_code FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&runStatus, &errorCode); err != nil {
			t.Fatal(err)
		}
		if runStatus != "superseded" || errorCode != "stale_focus_revision" {
			t.Fatalf("stale run status=%q error_code=%q", runStatus, errorCode)
		}
		var resultCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM signals WHERE analysis_run_id=?`, claim.RunID).Scan(&resultCount); err != nil || resultCount != 0 {
			t.Fatalf("stale apply stored results=%d err=%v", resultCount, err)
		}
	})

	t.Run("focus revision invalidates an applied projection", func(t *testing.T) {
		db = openTestDB(t)
		useStableContextScope(t, "owner-focus-applied-stale", "profile-focus-applied-stale")
		focus := createFocusFlowSpace(t, "AI Computer")
		insertFocusFlowMeeting(t, "meeting-focus-applied-stale", "AI Computer 原型评审结论。", "2026-08-15T09:30:00Z")
		claim := readFocusFlowClaim(t, focus.ID)
		applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", focusFlowApplyRequest(claim, "signal-applied-stale"), "focus-flow-applied-stale")
		if applied.Code != http.StatusOK {
			t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
		}
		updatedGoal := "AI Computer 企业版"
		update := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
			SchemaVersion: focusSpaceSchemaVersion, Action: "update", FocusID: focus.ID,
			ExpectedRevision: focus.Revision, Goal: &updatedGoal,
		}, "focus-flow-update-applied-stale")
		if update.Code != http.StatusOK {
			t.Fatalf("update focus status=%d body=%s", update.Code, update.Body.String())
		}
		var status, errorCode string
		if err := db.QueryRow(`SELECT status,error_code FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&status, &errorCode); err != nil {
			t.Fatal(err)
		}
		if status != "superseded" || errorCode != "stale_focus_revision" {
			t.Fatalf("applied analysis remained current: status=%q error=%q", status, errorCode)
		}
		current := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID, nil, "")
		var projection focusAnalysisGetResponse
		if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), &projection) != nil || projection.RunID != "" || len(projection.Results) != 0 {
			t.Fatalf("stale applied analysis remained default: status=%d body=%s", current.Code, current.Body.String())
		}
	})

	t.Run("workflow revision preserves an applied projection", func(t *testing.T) {
		db = openTestDB(t)
		useStableContextScope(t, "owner-focus-workflow-revision", "profile-focus-workflow-revision")
		focus := createFocusFlowSpace(t, "AI Computer")
		insertFocusFlowMeeting(t, "meeting-focus-workflow-revision", "AI Computer 原型评审结论。", "2026-08-15T09:30:00Z")
		claim := readFocusFlowClaim(t, focus.ID)
		applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", focusFlowApplyRequest(claim, "signal-workflow-revision"), "focus-flow-workflow-revision")
		if applied.Code != http.StatusOK {
			t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
		}
		workflowState := "deciding"
		update := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
			SchemaVersion: focusSpaceSchemaVersion, Action: "update", FocusID: focus.ID,
			ExpectedRevision: focus.Revision, WorkflowState: &workflowState,
		}, "focus-flow-update-workflow-revision")
		if update.Code != http.StatusOK {
			t.Fatalf("update focus status=%d body=%s", update.Code, update.Body.String())
		}
		var status string
		if err := db.QueryRow(`SELECT status FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "applied" {
			t.Fatalf("workflow-only update invalidated applied analysis: status=%q", status)
		}
		current := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID, nil, "")
		var projection focusAnalysisGetResponse
		if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), &projection) != nil || projection.RunID != claim.RunID || len(projection.Results) == 0 {
			t.Fatalf("workflow-only update hid applied analysis: status=%d body=%s", current.Code, current.Body.String())
		}
	})

	t.Run("active replacement run preserves the last applied projection", func(t *testing.T) {
		db = openTestDB(t)
		useStableContextScope(t, "owner-focus-active-replacement", "profile-focus-active-replacement")
		focus := createFocusFlowSpace(t, "AI Computer")
		insertFocusFlowMeeting(t, "meeting-focus-active-replacement-1", "第一版 AI Computer 产品评审结论。", "2026-08-15T09:30:00Z")
		firstClaim := readFocusFlowClaim(t, focus.ID)
		firstApplyRequest := focusFlowApplyRequest(firstClaim, "signal-active-replacement")
		firstApplyRequest.Actions = []focusActionInput{{
			ID:                 "action-active-replacement",
			Task:               "复现一次本地 AI 工作流并记录结果",
			Owner:              "产品团队",
			Priority:           "high",
			SourceIDs:          []string{firstClaim.Evidence[0].ID},
			SupportingEvidence: []string{"产品评审要求用真实工作流验证"},
			Counterevidence:    []string{},
			Confidence:         "high",
			Freshness:          "current",
			ValidationItems:    []string{"记录复现结果"},
		}}
		firstApply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", firstApplyRequest, "focus-active-replacement-apply-1")
		if firstApply.Code != http.StatusOK {
			t.Fatalf("first apply status=%d body=%s", firstApply.Code, firstApply.Body.String())
		}

		insertFocusFlowMeeting(t, "meeting-focus-active-replacement-2", "第二版 AI Computer 产品评审补充证据。", "2026-08-15T10:30:00Z")
		secondClaim := readFocusFlowClaim(t, focus.ID)
		if secondClaim.Status != "claimed" || secondClaim.RunID == "" || secondClaim.RunID == firstClaim.RunID {
			t.Fatalf("expected a replacement claim, got %#v", secondClaim)
		}

		current := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+url.QueryEscape(focus.ID), nil, "")
		var projection focusAnalysisGetResponse
		if current.Code != http.StatusOK || json.Unmarshal(current.Body.Bytes(), &projection) != nil {
			t.Fatalf("active replacement read status=%d body=%s", current.Code, current.Body.String())
		}
		if projection.RunID != firstClaim.RunID || len(projection.Signals) != 1 || projection.Signals[0].ID == "" || len(projection.Actions) != 1 || projection.Actions[0].ID == "" {
			t.Fatalf("active replacement hid last applied projection: %#v", projection)
		}
		foundClaimed := false
		for _, run := range projection.Runs {
			if runID, _ := run["run_id"].(string); runID == secondClaim.RunID {
				foundClaimed = run["status"] == "claimed"
				break
			}
		}
		if !foundClaimed {
			t.Fatalf("active replacement run missing from run history: %#v", projection.Runs)
		}
	})
}

func TestFocusAnalysisPreviewDoesNotClaimBeforeConfirmationFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-preview", "profile-focus-preview")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-preview", "AI Computer 预览候选。", "2026-08-15T09:30:00Z")

	path := "/api/focus-analysis/read?focus_id=" + url.QueryEscape(focus.ID) + "&scope=question&limit=5&claim=0"
	response := callFocusFlowJSON(t, focusAnalysisReadHandler, http.MethodGet, path, nil, "")
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}
	var preview focusAnalysisReadResponse
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Status != "preview" || preview.RunID != "" || len(preview.Evidence) != 1 || preview.EstimatedTotalHigh <= 0 {
		t.Fatalf("unexpected preview response: %#v", preview)
	}
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_analysis_runs WHERE focus_id=?`, focus.ID).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("preview created claim rows=%d err=%v", runs, err)
	}
}

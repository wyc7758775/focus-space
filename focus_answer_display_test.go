package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDecisionAnswerDisplay(t *testing.T) {
	options := `[{"id":"cost","label":"成本与吞吐"},{"id":"reliability","label":"产品可靠性"}]`
	for _, tc := range []struct{ name, raw, kind, options, want string }{
		{"choice", `"cost"`, "choice", options, "成本与吞吐"},
		{"multi", `["reliability","cost"]`, "multi", options, "产品可靠性、成本与吞吐"},
		{"text stays literal", `"cost"`, "text", options, "cost"},
		{"scale", `3`, "scale", "[]", "3"},
		{"legacy missing options", `"cost"`, "choice", "", "cost"},
		{"invalid answer", `{`, "choice", options, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := decisionAnswerDisplay(tc.raw, tc.kind, tc.options); got != tc.want {
				t.Fatalf("display=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecisionRunAcceptsEveryReasoningModel(t *testing.T) {
	for _, mode := range []string{"guided", "rational", "bounded_rationality", "first_principles"} {
		t.Run(mode, func(t *testing.T) {
			db = openTestDB(t)
			useStableContextScope(t, "owner-mode-"+mode, "profile-mode-"+mode)
			focus := createFocusFlowSpace(t, "模型选择 "+mode)
			started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
				SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
				ExpectedFocusRevision: focus.Revision, DecisionMode: mode,
				SkipClarification: true,
			}, "mode-start-"+mode)
			if started.Code != http.StatusCreated {
				t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
			}
			run := decodeDecisionRunFlow(t, started)
			if run.Run.DecisionMode != mode {
				t.Fatalf("decision mode=%q, want %q", run.Run.DecisionMode, mode)
			}
		})
	}
}

func TestFocusV3ChoiceAnswerMeaningReachesEvidenceAndGraphFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-choice-meaning", "profile-choice-meaning")
	focus := createFocusFlowSpace(t, "选择验证主线")
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID,
		ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
		Questions: []decisionQuestionInput{{
			ID: "priority", Type: "choice", Question: "你要优先验证哪一方面？",
			Options: []decisionQuestionOption{{ID: "cost", Label: "成本与吞吐"}, {ID: "reliability", Label: "产品可靠性"}},
		}},
	}, "choice-meaning-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	answered := appDataMutationFlow(t, "decision_run.answer", decisionRunAnswerRequest{
		SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID,
		QuestionID: "priority", ExpectedRunRevision: run.Run.Revision,
		Answer: decisionAnswerInput{Type: "choice", Value: json.RawMessage(`"cost"`)},
	}, "choice-meaning-answer")
	if answered.Code != http.StatusOK {
		t.Fatalf("answer status=%d body=%s", answered.Code, answered.Body.String())
	}
	run = decodeDecisionRunFlow(t, answered)
	claim := readFocusFlowClaim(t, focus.ID)
	if len(claim.Evidence) != 1 || claim.Evidence[0].Excerpt != "成本与吞吐" {
		t.Fatalf("analysis received option IDs instead of meaning: %#v", claim.Evidence)
	}
	request := focusFlowApplyRequest(claim, "")
	request.Signals = nil
	request.Decisions = []focusDecisionInput{{
		ID: "choice-decision", Question: "是否先建立成本与吞吐基线？", Options: []string{"先建立基线", "暂缓"},
		Recommendation: "先建立基线", SourceIDs: []string{claim.Evidence[0].ID},
		SupportingEvidence: []string{"用户明确选择成本与吞吐"}, Confidence: "high", Freshness: "current",
	}}
	request.Actions = []focusActionInput{{
		ID: "choice-action", Task: "建立成本与吞吐基线", Owner: "产品负责人", Priority: "high",
		SourceIDs: []string{claim.Evidence[0].ID}, SupportingEvidence: []string{"用户明确选择成本与吞吐"},
		Confidence: "high", Freshness: "current", ValidationItems: []string{"记录基线结果"},
	}}
	applied := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "choice-meaning-analysis")
	var analysis focusAnalysisGetResponse
	if applied.Code != http.StatusOK || json.Unmarshal(applied.Body.Bytes(), &analysis) != nil || len(analysis.Decisions) != 1 {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	for _, status := range []string{"running_ai", "applying", "completed"} {
		resumed := appDataMutationFlow(t, "decision_run.resume", decisionRunResumeRequest{
			SchemaVersion: decisionRunSchemaVersion, RunID: run.Run.ID, ExpectedRevision: run.Run.Revision,
			Status: status, AnalysisRunID: claim.RunID, DecisionID: analysis.Decisions[0].ID,
		}, "choice-meaning-"+status)
		if resumed.Code != http.StatusOK {
			t.Fatalf("resume %s status=%d body=%s", status, resumed.Code, resumed.Body.String())
		}
		run = decodeDecisionRunFlow(t, resumed)
	}
	confirmed := appDataMutationFlow(t, "decision_revision.apply", decisionRevisionApplyRequest{
		SchemaVersion: decisionHistorySchemaVersion, FocusID: focus.ID, DecisionID: analysis.Decisions[0].ID,
		RunID: run.Run.ID, ExpectedFocusRevision: focus.Revision,
		ExpectedResultRevision: analysis.Decisions[0].Revision, ExpectedRunRevision: run.Run.Revision,
	}, "choice-meaning-confirm")
	if confirmed.Code != http.StatusCreated {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var body string
	if err := db.QueryRow(`SELECT body FROM focus_logic_nodes WHERE focus_id=? AND node_type='user_answer'`, focus.ID).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if body != "成本与吞吐" {
		t.Fatalf("confirmed graph answer=%q", body)
	}
}

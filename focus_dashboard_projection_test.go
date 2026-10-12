package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFocusAnalysisProjectionKeepsSelectedRunUnit(t *testing.T) {
	analysis := focusAnalysisGetResponse{
		RunID: "run-current",
		Signals: []focusResultProjection{
			{ID: "signal-current", AnalysisRunID: "run-current"},
			{ID: "signal-old", AnalysisRunID: "run-old"},
		},
		Actions: []focusResultProjection{
			{ID: "action-current", AnalysisRunID: "run-current"},
			{ID: "action-old", AnalysisRunID: "run-old"},
		},
		Decisions: []focusResultProjection{
			{ID: "decision-current", AnalysisRunID: "run-current"},
			{ID: "decision-old", AnalysisRunID: "run-old"},
		},
		Results: []focusResultProjection{
			{ID: "signal-current", AnalysisRunID: "run-current"},
			{ID: "signal-old", AnalysisRunID: "run-old"},
		},
	}

	got := focusAnalysisProjectionForRun(analysis)
	if len(got.Signals) != 1 || got.Signals[0].ID != "signal-current" ||
		len(got.Actions) != 1 || got.Actions[0].ID != "action-current" ||
		len(got.Decisions) != 1 || got.Decisions[0].ID != "decision-current" ||
		len(got.Results) != 1 || got.Results[0].ID != "signal-current" {
		t.Fatalf("projection mixed analysis runs: %#v", got)
	}
}

func TestDashboardFocusProjectionSharesLatestAnalysisRunFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus", "profile-focus")

	create := callJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", `{
		"schema_version":1,
		"action":"create",
		"expected_revision":0,
		"title":"AI Computer 决策",
		"goal":"判断 AI Computer 产品机会",
		"decision_question":"是否应该在本季度投入原型",
		"kind":"decision",
		"horizon_mode":"fixed",
		"horizon_start":"2026-08-01T00:00:00Z",
		"horizon_end":"2026-09-30T23:59:59Z",
		"sources":[{"source_kind":"meeting","source_id":"meeting-1","enabled":true}]
	}`)
	if create.Code != http.StatusOK {
		t.Fatalf("focus create status=%d body=%s", create.Code, create.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(create.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO recordings(record_id,title,status,started_at,created_at,updated_at,summary) VALUES(?,?,?,?,?,?,?)`, "meeting-1", "AI Computer 产品会", "ready", "2026-08-15T09:00:00Z", "2026-08-15T09:00:00Z", "2026-08-15T09:30:00Z", "用户需要更低延迟的本地 AI 工作流"); err != nil {
		t.Fatal(err)
	}

	read := callJSON(t, focusAnalysisReadHandler, http.MethodGet, "/api/focus-analysis/read?focus_id="+focus.ID+"&scope=question&limit=5", "")
	if read.Code != http.StatusOK {
		t.Fatalf("focus read status=%d body=%s", read.Code, read.Body.String())
	}
	var claim focusAnalysisReadResponse
	if err := json.Unmarshal(read.Body.Bytes(), &claim); err != nil {
		t.Fatal(err)
	}
	if claim.RunID == "" || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected focus claim: %#v", claim)
	}
	evidenceID := claim.Evidence[0].ID
	applyPayload, err := json.Marshal(focusAnalysisApplyRequest{
		SchemaVersion:   1,
		RunID:           claim.RunID,
		FocusID:         focus.ID,
		FocusRevision:   claim.FocusRevision,
		EvidenceSetHash: claim.EvidenceSetHash,
		PromptVersion:   claim.PromptVersion,
		Status:          "applied",
		Signals: []focusSignalInput{{
			ID: "signal-current", Title: "本地 AI 低延迟需求增强", Detail: "会议证据显示用户更重视设备侧响应速度。", SourceIDs: []string{evidenceID}, SupportingEvidence: []string{"会议明确提出低延迟需求"}, Confidence: "high", Freshness: "current", Counterevidence: []string{"尚无付费转化数据"}, ValidationItems: []string{"验证三个用户场景"},
		}},
		Actions: []focusActionInput{{
			ID: "action-current", Task: "验证三个 AI Computer 原型场景", Owner: "产品负责人", SourceIDs: []string{evidenceID}, Priority: "high", SupportingEvidence: []string{"会议要求验证原型"}, Counterevidence: []string{"样本量仍有限"}, Confidence: "medium", Freshness: "current", ValidationItems: []string{"记录验证结果"},
		}},
		Decisions: []focusDecisionInput{{
			ID: "decision-current", Question: "是否投入本季度原型", Options: []string{"投入", "暂缓"}, Recommendation: "先投入一个有界原型", SourceIDs: []string{evidenceID}, Assumptions: []string{"现有设备性能可用"}, Risks: []string{"样本量有限"}, SupportingEvidence: []string{"会议讨论了本季度投入"}, Counterevidence: []string{"尚未完成成本验证"}, Confidence: "medium", Freshness: "current", ValidationItems: []string{"补充成本评估"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/internal/focus-analysis/apply", bytes.NewReader(applyPayload))
	applyRequest.Header.Set("X-Zettlab-Idempotency-Key", "focus-dashboard-flow-1")
	apply := httptest.NewRecorder()
	focusAnalysisApplyHandler(apply, applyRequest)
	if apply.Code != http.StatusOK {
		t.Fatalf("focus apply status=%d body=%s", apply.Code, apply.Body.String())
	}

	if _, err := db.Exec(`INSERT INTO signals(id,title,detail,source_ids,confidence,counterevidence,status,focus_id,analysis_run_id,result_rank,prompt_version,evidence_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "signal-old", "旧批次结论", "不应出现在最新投影", `["`+evidenceID+`"]`, "low", `[]`, "draft", focus.ID, "run-old", 1, "focus-v0", "old-evidence"); err != nil {
		t.Fatal(err)
	}

	dashboard := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}
	var projected Dashboard
	if err := json.Unmarshal(dashboard.Body.Bytes(), &projected); err != nil {
		t.Fatal(err)
	}
	if projected.FocusSpace == nil || projected.FocusSpace.ID != focus.ID {
		t.Fatalf("active focus was not projected: %#v", projected.FocusSpace)
	}
	if projected.FocusAnalysis == nil || projected.FocusAnalysis.RunID != claim.RunID {
		t.Fatalf("latest analysis was not projected: %#v", projected.FocusAnalysis)
	}
	analysis := projected.FocusAnalysis
	if len(analysis.Evidence) != 1 || analysis.Evidence[0].ID != evidenceID ||
		len(analysis.Signals) != 1 || analysis.Signals[0].ID == "" || analysis.Signals[0].ID == "signal-old" || analysis.Signals[0].AnalysisRunID != claim.RunID ||
		len(analysis.Actions) != 1 || analysis.Actions[0].ID == "" || analysis.Actions[0].ID == "action-old" || analysis.Actions[0].AnalysisRunID != claim.RunID ||
		len(analysis.Decisions) != 1 || analysis.Decisions[0].ID == "" || analysis.Decisions[0].ID == "decision-old" || analysis.Decisions[0].AnalysisRunID != claim.RunID ||
		len(analysis.Results) != 3 {
		t.Fatalf("dashboard projection did not keep one analysis batch: %#v", analysis)
	}
	resultKinds := make(map[string]int, len(analysis.Results))
	for _, result := range analysis.Results {
		if result.AnalysisRunID != claim.RunID {
			t.Fatalf("dashboard projection mixed analysis runs: %#v", analysis.Results)
		}
		resultKinds[result.Kind]++
	}
	if resultKinds["signal"] != 1 || resultKinds["action"] != 1 || resultKinds["decision"] != 1 {
		t.Fatalf("dashboard projection result kinds=%v", resultKinds)
	}
}

func TestDashboardFocusProjectionDegradesWithoutFocusFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-empty", "profile-empty")

	dashboard := callJSON(t, dashboardHandler, http.MethodGet, "/api/dashboard", "")
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", dashboard.Code, dashboard.Body.String())
	}
	var projected Dashboard
	if err := json.Unmarshal(dashboard.Body.Bytes(), &projected); err != nil {
		t.Fatal(err)
	}
	if projected.FocusSpace != nil || projected.FocusAnalysis != nil {
		t.Fatalf("empty legacy dashboard unexpectedly exposed focus: %#v %#v", projected.FocusSpace, projected.FocusAnalysis)
	}
}

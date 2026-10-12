package main

import (
	"net/http"
	"testing"
	"time"
)

func TestFocusAnalysisUsageChargeIsConservativeUnit(t *testing.T) {
	run := focusRunSummary{EstimatedInput: 900, EstimatedOutput: 600}
	tests := []struct {
		name  string
		usage focusAnalysisUsage
		want  int
	}{
		{name: "missing usage keeps estimate", usage: focusAnalysisUsage{}, want: 1500},
		{name: "lower usage keeps estimate", usage: focusAnalysisUsage{InputTokens: 400, OutputTokens: 200}, want: 1500},
		{name: "higher usage is charged", usage: focusAnalysisUsage{InputTokens: 1200, OutputTokens: 700}, want: 1900},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := focusAnalysisUsageCharge(run, tt.usage); got != tt.want {
				t.Fatalf("focusAnalysisUsageCharge()=%d, want %d", got, tt.want)
			}
		})
	}
}

func TestFocusAnalysisRunIDIncludesOwnerProfileScopeUnit(t *testing.T) {
	key := "same-focus:1:question:evidence:focus-v1"
	first := stableObjectID("focus-run", "owner-a\x00profile-a\x00"+key)
	second := stableObjectID("focus-run", "owner-b\x00profile-b\x00"+key)
	if first == second {
		t.Fatalf("focus run IDs collided across owner/profile scopes: %q", first)
	}
}

func TestReaderDigestBudgetDoesNotStarveActiveFocusFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-budget-isolated", "profile-focus-budget-isolated")
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"reader-exhausted-focus-isolated", "daily", now.Format("2006-01-02"), `[]`, 100, 100, 8000, 8000, "completed", "reader-exhausted-focus-isolated", nowText, nowText); err != nil {
		t.Fatal(err)
	}
	projection, err := readFocusBudgetProjection(nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if projection.DailyUsedArticles != 0 || projection.DailyUsedTokens != 0 {
		t.Fatalf("Reader maintenance consumed Focus budget: %#v", projection)
	}
	focus := createFocusFlowSpace(t, "端侧 AI 上线方向")
	insertFocusFlowMeeting(t, "meeting-focus-budget-isolated", "端侧 AI 可降低离线延迟。", nowText)
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || claim.RunID == "" || len(claim.Evidence) != 1 {
		t.Fatalf("Reader maintenance starved active Focus claim: %#v", claim)
	}
}

func TestFocusAnalysisApplyRejectsUsageOverDailyBudgetFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-budget", "profile-focus-budget")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-budget", "用户需要更低延迟的 AI Computer 本地工作流。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || claim.RunID == "" {
		t.Fatalf("unexpected focus claim: %#v", claim)
	}
	reserved := claim.EstimatedInput + claim.EstimatedOutput
	budget := reserved
	if budget < readerFocusMinTokenBudget {
		budget = readerFocusMinTokenBudget
	}
	if _, err := db.Exec(`UPDATE reader_focus_policy SET daily_token_budget=? WHERE id='default'`, budget); err != nil {
		t.Fatal(err)
	}

	request := focusFlowApplyRequest(claim, "signal-over-budget")
	request.Usage = focusAnalysisUsage{InputTokens: budget + 1}
	rejected := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "focus-budget-over-1")
	if rejected.Code != http.StatusConflict {
		t.Fatalf("over-budget apply status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	var storedResults int
	if err := db.QueryRow(`SELECT COUNT(*) FROM signals WHERE focus_id=?`, focus.ID).Scan(&storedResults); err != nil {
		t.Fatal(err)
	}
	if storedResults != 0 {
		t.Fatalf("over-budget apply wrote %d signal(s)", storedResults)
	}
	var runStatus string
	if err := db.QueryRow(`SELECT status FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(&runStatus); err != nil {
		t.Fatal(err)
	}
	if runStatus != "claimed" {
		t.Fatalf("over-budget apply changed claim status to %q", runStatus)
	}

	// A missing or lower provider usage report remains charged at the local
	// estimate and is accepted when that estimate fits the daily budget.
	request.Usage = focusAnalysisUsage{}
	accepted := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "focus-budget-estimate-1")
	if accepted.Code != http.StatusOK {
		t.Fatalf("estimate-budget apply status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var used int
	day := claim.GeneratedAt[:10]
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE
		WHEN status IN ('claimed','failed') THEN estimated_input_tokens+estimated_output_tokens
		WHEN actual_input_tokens+actual_output_tokens>estimated_input_tokens+estimated_output_tokens THEN actual_input_tokens+actual_output_tokens
		ELSE estimated_input_tokens+estimated_output_tokens END),0)
		FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? AND substr(created_at,1,10)=? AND status IN ('claimed','applied','partial','failed')`, "owner-focus-budget", "profile-focus-budget", day).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != reserved {
		t.Fatalf("budget ledger used=%d, want conservative reservation %d", used, reserved)
	}
}

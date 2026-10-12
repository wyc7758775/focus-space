package main

import (
	"strings"
	"testing"
)

func TestDecisionPanelDoesNotShowStaleEmptyClarificationAfterAppliedAnalysis(t *testing.T) {
	page, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(page)
	guard := `if (analysisRunID && run.status === "waiting_clarification" && !run.analysisRunID)`
	if !strings.Contains(script, guard) {
		t.Fatalf("decision panel is missing the stale empty clarification guard %q", guard)
	}
	guardIndex := strings.Index(script, guard)
	panelIndex := strings.Index(script, "function decisionRunPanelMarkup")
	if panelIndex < 0 || guardIndex < panelIndex {
		t.Fatal("stale clarification guard must be inside decisionRunPanelMarkup")
	}
	for _, expected := range []string{
		"const latestAnalysisRunID = text(analysis?.run_id)",
		"const reusableAnalysisStatus = text(",
	} {
		if !strings.Contains(script[panelIndex:guardIndex], expected) {
			t.Fatalf("stale clarification guard must derive applied analysis state from %q", expected)
		}
	}
	if !strings.Contains(script[guardIndex:], "run.analysisRunID") {
		t.Fatal("stale clarification guard must check whether the run is linked to the applied analysis")
	}
}

func TestDecisionPanelExposesCancellationForActiveRuns(t *testing.T) {
	page, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(page)
	for _, expected := range []string{
		"canCancel: Boolean(root.can_cancel ?? run.can_cancel ?? capabilities.cancel)",
		"data-decision-run-action=\"cancel\"",
		"[\"start\", \"resume\", \"cancel\"]",
		"本轮分析已停止。",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("active decision runs must expose cancellation through %q", expected)
		}
	}
}

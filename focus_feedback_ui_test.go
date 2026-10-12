package main

import (
	"strings"
	"testing"
)

func TestFocusResultFeedbackControlsFlow(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	pageBytes := indexPageCorpus(t)
	script, styles, page := string(scriptBytes), string(styleBytes), string(pageBytes)
	for _, expected := range []string{
		"function focusResultCardMarkup(item)",
		"function focusResultFeedbackKey(focusID, kind, resultID, revision, decision, patch = null)",
		"function submitFocusResultFeedback(item, decision, patch = null, card = null)",
		"const shouldConfirmDecisionRevision =",
		"const confirmedRevision = shouldConfirmDecisionRevision",
		"? await confirmDecisionRevision(item, focusID, revision)",
		"if (!compatibleRun) return false;",
		`run.status === "completed"`,
		"run.analysisRunID === analysisRunID",
		"run.decisionID === payload.decision_id",
		"if (!confirmedRevision)",
		`callFocusOperation("focus_space.get")`,
		`callFocusOperation("focus_analysis.get"`,
		`loadDecisionRunState(focusID, { force: true, silent: true })`,
		"expected_focus_revision: Number(freshFocus?.revision) || 0",
		"expected_result_revision: Number(freshItem?.revision)",
		"./api/app-data/v1/operations/focus_result.feedback",
		"Idempotency-Key",
		"expected_revision: Number(revision) || 1",
		"focus_id: text(focusID)",
		"expected_revision: revision",
		`data-focus-feedback="accepted"`,
		`data-focus-feedback="rejected"`,
		`data-focus-feedback="snoozed"`,
		"data-focus-edit-toggle",
		"data-focus-result-editor",
		"data-focus-edit-field=\"detail\"",
		"data-focus-edit-field=\"question\"",
		"focusResultEditPatch(card, item)",
		`const actionDone = kind === "action" && (text(item.status) === "done" || data.completed === true);`,
		`readerFocusCopy("已完成", "Completed")`,
		"bindFocusSpaceOverviewEvents()",
		"async function reloadFocusProjection(focusID, options = {})",
		`callFocusOperation("focus_space.get", { query: { focus_id: id } })`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("focus feedback UI is missing %q", expected)
		}
	}
	for _, expected := range []string{
		".focus-result-card{",
		".focus-result-actions{",
		".focus-result-editor{",
		".focus-result-status.accepted",
		".focus-result-status.rejected",
		"@media(max-width:640px)",
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("focus feedback styles are missing %q", expected)
		}
	}
	if !strings.Contains(page, "edit:") {
		t.Error("focus feedback edit control is missing its icon")
	}
	if strings.Contains(script, `request("./api/focus-results/feedback"`) {
		t.Error("focus feedback must use the typed app-data operation, not a direct page endpoint")
	}
	feedbackStart := strings.Index(script, "async function submitFocusResultFeedback(")
	if feedbackStart < 0 {
		t.Fatal("focus feedback function boundary is missing")
	}
	feedbackEnd := strings.Index(script[feedbackStart:], "\n  function focusResultEditPatch(")
	if feedbackEnd < 0 {
		t.Fatal("focus feedback function boundary is missing")
	}
	feedbackBody := script[feedbackStart : feedbackStart+feedbackEnd]
	if strings.Contains(feedbackBody, "refresh(true)") {
		t.Error("focus feedback must not refresh the entire Reader surface")
	}
	if !strings.Contains(feedbackBody, "reloadFocusProjection(focusID") {
		t.Error("focus feedback must reload only the changed Focus projection")
	}
}

func TestTerminalDecisionRunRestartUsesPreviousRunInIdempotencySubject(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`data-previous-run-id="${esc(run.runID)}"`,
		`data-previous-run-revision="${Number(run.revision) || 0}"`,
		`const previousRunID = text(button.dataset.previousRunId);`,
		`[focusRevision || 0, previousRunID, previousRunRevision].join(":")`,
		`analysisFocusRevision === Number(focus.revision)`,
		`run.status === "completed" && run.canStart`,
		`freshAnalysis = false`,
		`!freshAnalysis && analysisRunID`,
		`startAction(readerFocusCopy("重新分析", "Analyze again"), "refresh", true)`,
		`重新分析`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("terminal decision restart idempotency is missing %q", expected)
		}
	}
}

func TestDecisionRunRestartCarriesSelectedReasoningModel(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(styleBytes)
	for _, expected := range []string{
		`data-decision-run-configure`,
		`function openDecisionRunStartDialog(button)`,
		`data-decision-mode-select`,
		`payload.decision_mode = text(decisionMode || button.dataset.decisionMode, "guided")`,
		`上一轮结果会保留，新结果将作为独立版本生成。`,
		`first_principles`,
		`bounded_rationality`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("reasoning model dialog is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.decision-run-start-layer{`,
		`.decision-run-start-dialog{`,
		`.decision-run-panel.is-complete{`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("reasoning model dialog styles are missing %q", expected)
		}
	}
}

func TestCompletedDecisionRunUsesCompactResultState(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`!["not_started", "idle", "completed"].includes(run.status)`,
		`readerFocusCopy("分析记录", "Analysis record")`,
		`class="focus-v3-decision-section"`,
		`focus-result-card${confirmedDecision ? " is-confirmed-decision" : ""}`,
		`decisionHistoryTriggerMarkup(item, focusID, evidenceCount)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("compact completed decision state is missing %q", expected)
		}
	}
	panelStart := strings.Index(script, "function decisionRunPanelMarkup(")
	panelEnd := strings.Index(script[panelStart:], "\n  function readerFocusOverviewText")
	if panelStart < 0 || panelEnd < 0 {
		t.Fatal("decision run panel boundary is missing")
	}
	panel := script[panelStart : panelStart+panelEnd]
	if strings.Contains(panel, "tokenUsage") || strings.Contains(panel, "本轮分析已完成") {
		t.Fatal("completed decision state must not repeat token usage or completion notices")
	}
}

func TestFocusDecisionAndTodoPresentation(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(styleBytes)
	for _, expected := range []string{
		`function decisionGraphWrapText(value, maxUnits = 28)`,
		`data-decision-fullscreen`,
		`readerFocusCopy("为什么这样决定", "Why this decision")`,
		`decision-overview-intro`,
		`kind !== "decision" || !["accepted", "edited"].includes(text(item.feedback_status))`,
		`readerFocusCopy("打开 TODO List", "Open To-do list")`,
		`focus-v3-empty-mascot`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("focus decision presentation is missing %q", expected)
		}
	}
	graphStart := strings.Index(script, "function decisionHistoryGraphMarkup(")
	graphEnd := strings.Index(script[graphStart:], "\n  function decisionHistoryTimelineMarkup")
	if graphStart < 0 || graphEnd < 0 {
		t.Fatal("decision graph markup boundary is missing")
	}
	graphMarkup := script[graphStart : graphStart+graphEnd]
	toolbarEnd := strings.Index(graphMarkup, "</div>${comparisonBanner}")
	if toolbarEnd < 0 || strings.Contains(graphMarkup[:toolbarEnd], "data-decision-fit-graph") {
		t.Error("the graph toolbar must not keep a second fit-canvas button")
	}
	for _, expected := range []string{
		`.decision-overview-intro{`,
		`.focus-v3-inline-edit{min-width:38px`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("focus decision styles are missing %q", expected)
		}
	}
}

func TestFocusV3OwnsTodayProjectionAndAnimatedEmptyState(t *testing.T) {
	pageBytes := indexPageCorpus(t)
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	styleBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	page, script, styles := string(pageBytes), string(scriptBytes), string(styleBytes)
	for _, expected := range []string{
		`const focusV3View = ["today", "decision", "action", "signal"].includes(`,
		`if (focusV3View) {`,
		`panel.replaceChildren()`,
		`focus-v3-mascot-svg`,
		`readerFocusCopy("皮卡丘正在等你", "Pikachu is waiting for you")`,
	} {
		if !strings.Contains(page+script, expected) {
			t.Errorf("Focus V3 ownership or mascot state is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`const focusV3View = window.__plaudParityReady`,
		`<h2>今日焦点</h2>`,
	} {
		if strings.Contains(page, forbidden) {
			t.Errorf("retired focus overview path is still present %q", forbidden)
		}
	}
	for _, expected := range []string{
		`@keyframes focus-v3-mascot-bob`,
		`animation:focus-v3-mascot-bob`,
		`focus-v3-mascot-sparkle`,
		`prefers-reduced-motion`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("animated mascot styling is missing %q", expected)
		}
	}
}

func TestDecisionGraphUsesAdaptiveNodeGeometry(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`function decisionGraphWrapLine(value, maxUnits)`,
		`function decisionGraphNodePresentation(node, workspace)`,
		`function decisionGraphColumnPositions(nodes, x, presentations, positions, gap = 38)`,
		`"width": "data(width)"`,
		`"height": "data(height)"`,
		`"text-max-width": "data(textMaxWidth)"`,
		`const displayedTitle = workspace.expandedGroups.has("all")`,
		`const fullscreen = expanded && Boolean(workspace.fullscreen)`,
		`const maxRows = fullscreen ? 9 : 6`,
		`const minimumReadableZoom = expanded ? (workspace.fullscreen ? 0.6 : 0.56) : 0.48`,
		`"font-size": "data(fontSize)"`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("adaptive decision graph is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`"width": 224`,
		`"height": 104`,
		`(row - (rows - 1) / 2) * 116`,
		`const maxHeight = expanded ? 184 : 148`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("fixed decision graph geometry is still present %q", forbidden)
		}
	}
}

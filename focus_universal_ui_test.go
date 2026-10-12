package main

import (
	"strings"
	"testing"
)

func TestFocusUniversalWorkspaceSectionsAndCreationControls(t *testing.T) {
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
		`["overview", readerFocusCopy("概览", "Overview")]`,
		`["evidence", readerFocusCopy("资料", "Sources")]`,
		`["workspace", readerFocusCopy("工作台", "Workspace")]`,
		`["result", readerFocusCopy("当前结果", "Current result")]`,
		`["review", readerFocusCopy("复盘", "Review")]`,
		`const hasCurrentResult = acceptedDecisions.length > 0 || workProducts.length > 0;`,
		`...(hasCurrentResult ? [["result"`,
		`...(hasReview ? [["review"`,
		`name="intent_kind"`,
		`name="thinking_template_id"`,
		`intent_kind: intentKind`,
		`blueprint_id: focusBlueprintID(intentKind)`,
		`thinking_template_id: text(data.get("thinking_template_id")`,
		`function recommendFocusIntent(value)`,
		`kind: intentKind === "decide" ? "decision" : "monitor"`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("universal Focus workspace is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`["decision", readerFocusCopy("决策", "Decision")]`,
		`["artifact", readerFocusCopy("成果", "Outcomes")]`,
		`<h3>${readerFocusCopy("行动与成果", "Actions and outcomes")}</h3>`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("retired Focus section remains: %q", forbidden)
		}
	}
	for _, expected := range []string{
		`.focus-v3-create-grid{`,
		`.focus-workspace-settings{`,
		`.focus-current-result{`,
		`.focus-analysis-history{`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("universal Focus styling is missing %q", expected)
		}
	}
}

func TestFocusWorkProductReadingExportAndFeedbackUI(t *testing.T) {
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
		`function focusWorkProductCardMarkup(item)`,
		`renderMarkdownContent(markdown,`,
		`data-work-product-copy`,
		`data-work-product-download`,
		`data-work-product-feedback="accepted"`,
		`data-work-product-feedback="rejected"`,
		`data-work-product-feedback="snoozed"`,
		`callFocusOperation("focus_work_product.feedback"`,
		`navigator.clipboard?.writeText`,
		`URL.createObjectURL(new Blob([markdown]`,
		`当前仅支持复制或下载 Markdown，不会直接写入 Obsidian。`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("work product UI is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.focus-work-product-card{`,
		`.focus-work-product-body{`,
		`.focus-work-product-actions{`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("work product styling is missing %q", expected)
		}
	}
}

func TestFocusAnalysisHistoryAndDecisionUndoUI(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`focusAnalysisRunSelections: new Map()`,
		`focusAnalysisSnapshots: new Map()`,
		`function focusAnalysisHistoryMarkup(focus, latestAnalysis)`,
		`data-focus-analysis-run`,
		`query: { focus_id: id, run_id: selectedRunID, limit: 20 }`,
		`正在查看历史回合。当前有效结果和已接受决定没有被覆盖。`,
		`data-focus-feedback="draft"`,
		`decision === "draft" && kind !== "decision"`,
		`已撤销接受，可重新确认`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("analysis history or undo UI is missing %q", expected)
		}
	}
}

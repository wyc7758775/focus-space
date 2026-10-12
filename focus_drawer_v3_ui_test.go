package main

import (
	"strings"
	"testing"
)

func TestFocusDrawerMirrorsV3OverviewAndContainsLongResults(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	script, styles := string(scriptBytes), string(stylesBytes)
	for _, expected := range []string{
		`class="focus-v3-drawer-head-copy"`,
		`class="focus-v3-goal"`,
		`class="focus-v3-tabs" role="tablist"`,
		`role="tab" aria-selected=`,
		`role="tabpanel" aria-labelledby=`,
		`class="focus-v3-overview-properties"`,
		`class="focus-v3-overview-workspace"`,
		`class="focus-v3-overview-main"`,
		`class="focus-v3-overview-sidebar"`,
		`function updateFocusV3DrawerPanel(layer, focus, panel)`,
		`layer.querySelectorAll('.focus-v3-tabs [role="tab"][data-focus-panel]')`,
		`event.stopPropagation();`,
		`currentPanel.innerHTML = nextPanel.innerHTML`,
		`if (panel === "workspace")`,
		`void loadDecisionRunState(focus.id, { force: true, silent: true })`,
		`["ArrowLeft", "ArrowRight", "Home", "End"]`,
		`data-open-focus-actions`,
		`switchProductV3View("action")`,
		`function focusDeadlineBadge(focus)`,
		`if (!horizon && focus?.decision_mode !== "sprint") return ""`,
		`${focusModeBadge(focus)}${focusDeadlineBadge(focus)}<span>${readerFocusCopy("版本", "Version")}`,
		`...(hasCurrentResult ? [["result", readerFocusCopy("当前结果", "Current result")]] : [])`,
		`...(hasReview ? [["review", readerFocusCopy("复盘", "Review")]] : [])`,
		`执行后的复盘`,
		`复盘只记录执行效果，不覆盖分析产物、已接受决定或历史回合。`,
		`readerFocusCopy("前往 TODO List", "Open To-do list")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("V3 focus drawer interaction is missing %q", expected)
		}
	}
	if strings.Contains(script, `本轮分析引用`) {
		t.Error("duplicate analysis source entry should not be rendered")
	}
	for _, expected := range []string{
		`.focus-v3-drawer{position:absolute;top:0;right:0;bottom:0;width:60vw;`,
		`.focus-v3-drawer-body{min-width:0;min-height:0;flex:1;overflow-x:hidden;overflow-y:auto;`,
		`.focus-v3-overview-workspace{display:grid;grid-template-columns:minmax(0,1fr) 238px;`,
		`.focus-v3-panel .focus-result-list,.focus-v3-panel .focus-result-card{min-width:0;width:100%;box-sizing:border-box}`,
		`.focus-space-materials>.focus-v3-evidence-list{margin-top:7px}`,
		`.focus-v3-review-intro{`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("V3 focus drawer layout contract is missing %q", expected)
		}
	}
	if strings.Contains(styles, `.focus-v3-drawer{position:absolute;top:0;right:0;bottom:0;width:min(60vw,1050px);min-width:620px;`) {
		t.Fatal("focus drawer must not retain the capped legacy width")
	}
	if strings.Contains(script, `parity.focusPanel = node.dataset.focusPanel; openFocusV3Drawer(focus.id, parity.focusPanel)`) {
		t.Fatal("tab changes must update the mounted panel instead of recreating the drawer")
	}
	if strings.Contains(script, `class="focus-v3-kicker"`) {
		t.Fatal("focus drawer must not repeat the redundant focus-space and workflow breadcrumb above the title")
	}
	if strings.Contains(script, `<h3>分析证据</h3>`) {
		t.Fatal("focus material groups must not expose the internal analysis-evidence label or a mismatched bare heading")
	}
	if strings.Contains(script, `${focusModeBadge(focus)}<span>${focus.horizon_end ?`) {
		t.Fatal("slowburn focus details must not render the redundant no-deadline badge")
	}
}

func TestFocusV3UserFacingLabelsUseLocaleAwareCopy(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		`readerFocusCopy("看板", "Board")`,
		`readerFocusCopy("列表", "List")`,
		`readerFocusCopy(workflow[1], workflow[2])`,
		`readerFocusCopy("进行中", "active")`,
		`readerFocusCopy("待确认", "Pending")`,
		`readerFocusCopy("已推迟", "Snoozed")`,
		`readerFocusCopy("概览", "Overview")`,
		`readerFocusCopy("资料", "Sources")`,
		`readerFocusCopy("工作台", "Workspace")`,
		`readerFocusCopy("当前结果", "Current result")`,
		`readerFocusCopy("复盘", "Review")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("Focus V3 label must have a locale-aware copy pair %q", expected)
		}
	}
}

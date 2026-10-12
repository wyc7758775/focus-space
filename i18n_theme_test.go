package main

import (
	"strings"
	"testing"
)

func TestStaticDashboardThemeAndLocaleContract(t *testing.T) {
	page, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	css, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	i18n, err := appFS.ReadFile("static/i18n.js")
	if err != nil {
		t.Fatal(err)
	}
	parity, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"prefers-color-scheme: dark",
		"--canvas:",
		"--surface:",
		"--surface-soft:",
		"./static/i18n.js?v=",
	} {
		if !strings.Contains(string(page), fragment) {
			t.Fatalf("index.html missing theme/i18n fragment %q", fragment)
		}
	}
	if !strings.Contains(string(css), "@media(prefers-color-scheme:dark)") {
		t.Fatal("reader parity CSS must provide a dark theme")
	}
	for _, fragment := range []string{
		".x-entry-head strong{min-width:64px;max-width:42%;flex:0 1 42%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink)",
		".x-entry-text{display:block;margin-top:3px;color:var(--ink)",
		".x-detail-author strong{overflow:hidden;color:var(--ink)",
		".x-detail-post p{margin:0;color:var(--ink)",
	} {
		if !strings.Contains(string(css), fragment) {
			t.Errorf("explicit host dark theme is missing X token %q", fragment)
		}
	}
	for _, fragment := range []string{
		`html[data-theme="light"] body.product-v3-shell`,
		`html[data-theme="light"] .reader-shell.product-v3-shell>.sidebar`,
	} {
		if !strings.Contains(string(css), fragment) {
			t.Errorf("Focus Space V3 light theme missing %q", fragment)
		}
	}
	for _, fragment := range []string{
		`"zettlab-appearance-host-sync-v1"`,
		`productV3AppearanceMedia.addEventListener("change"`,
		`document.documentElement.style.colorScheme = resolved`,
		`button.setAttribute("aria-pressed", String(active))`,
	} {
		if !strings.Contains(string(parity), fragment) {
			t.Errorf("Focus Space V3 host theme contract missing %q", fragment)
		}
	}
	for _, locale := range []string{"zh-CN", "en"} {
		if !strings.Contains(string(i18n), `"`+locale+`"`) {
			t.Fatalf("i18n dictionary missing locale %s", locale)
		}
	}
	if !strings.Contains(string(i18n), "沉淀到知识库") || !strings.Contains(string(i18n), "Save to knowledge base") {
		t.Fatal("knowledge-base labels are not covered by locale resources")
	}
	// Translation is exact-match only; article/transcript payloads remain data.
	if !strings.Contains(string(i18n), "User titles, article bodies, transcripts") {
		t.Fatal("i18n must document the user-content boundary")
	}
	for _, key := range []string{
		"sourceAvailableCount", "sourceAvailableCountDegraded", "subscriptionWaiting",
		"savedContent", "knowledgeOnly", "pinnedSources", "subscriptions",
		"aiVideo", "builtInSources",
	} {
		if strings.Count(string(i18n), key+":") != 2 {
			t.Errorf("locale key %q must exist exactly once in zh-CN and en", key)
		}
		if !strings.Contains(string(parity), `t("`+key+`"`) {
			t.Errorf("reader parity does not consume locale key %q", key)
		}
	}
	for _, forbidden := range []string{
		"`共 ${stored} 条可浏览内容",
		`textContent = "订阅已保留，等待来源恢复"`,
		`textContent = "已保存的内容"`,
		`textContent = "仅显示已明确加入的内容"`,
		`<span>${esc(group)}</span>`,
	} {
		if strings.Contains(string(parity), forbidden) {
			t.Errorf("reader navigation still renders untranslated copy %q", forbidden)
		}
	}
	policyStart := strings.Index(string(parity), "async function saveReaderFocusPolicy()")
	if policyStart < 0 {
		t.Fatal("reader focus policy save handler is missing")
	}
	policySource := string(parity)[policyStart:]
	if end := strings.Index(policySource, "\n  function renderSettings()"); end >= 0 {
		policySource = policySource[:end]
	}
	if strings.Contains(policySource, "reader-focus-mode") || strings.Contains(policySource, "mode,") {
		t.Fatal("settings UI must not edit aggregate reader focus mode; topic bootstrap state is authoritative")
	}
	if !strings.Contains(string(parity), "selected disabled") || !strings.Contains(string(parity), "每个主题由 Chat 确认") || !strings.Contains(string(parity), "Each topic is confirmed in Chat") {
		t.Fatal("settings UI must present aggregate mode as a read-only topic-scoped summary")
	}
	for _, forbidden := range []string{
		"<strong>AI 概览</strong>",
		"<span class=\"article-ai-overview-state\">正在生成</span>",
		"<span>重试生成</span>",
		"<span>与你相关</span>",
	} {
		if strings.Contains(string(parity), forbidden) {
			t.Errorf("article AI overview still renders untranslated product copy %q", forbidden)
		}
	}
	for _, translated := range []string{
		"AI overview",
		"Retry generation",
		"Relevant to you",
		"Reading the original article is unaffected.",
	} {
		if !strings.Contains(string(parity), translated) {
			t.Errorf("article AI overview missing English locale copy %q", translated)
		}
	}
}

package main

import (
	"strings"
	"testing"
)

func TestFocusLinkedSourcesUseLocaleCopy(t *testing.T) {
	raw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	start := strings.Index(script, "function focusLinkedSourcesMarkup")
	end := strings.Index(script, "function decisionHistoryCacheKey")
	if start < 0 || end <= start {
		t.Fatal("focus linked source renderer is missing")
	}
	section := script[start:end]
	for _, expected := range []string{
		`readerFocusCopy("会议纪要", "Meeting notes")`,
		`readerFocusCopy("Obsidian 笔记", "Obsidian note")`,
		`readerFocusCopy("会议与录音", "Meetings and recordings")`,
		`readerFocusCopy("会议与笔记", "Meetings and notes")`,
	} {
		if !strings.Contains(section, expected) {
			t.Fatalf("linked Focus sources must be localized through %q", expected)
		}
	}
	for _, forbidden := range []string{
		`kind === "meeting" ? "会议纪要" : "Obsidian 笔记"`,
		`<small>${kind === "meeting" ? "会议与录音" : "Obsidian"}</small>`,
		`<span>会议与笔记</span>`,
	} {
		if strings.Contains(section, forbidden) {
			t.Fatalf("linked Focus sources still mix raw Chinese labels: %q", forbidden)
		}
	}
}

func TestObsidianAndFocusFallbacksUseLocaleCopy(t *testing.T) {
	raw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, expected := range []string{
		`readerFocusCopy("笔记路径", "Note path")`,
		`readerFocusCopy("修改于", "Modified")`,
		`readerFocusCopy("PDF 预览", "PDF preview")`,
		`readerFocusCopy("图片预览", "Image preview")`,
		`readerFocusCopy("PDF 暂时无法显示", "PDF is temporarily unavailable")`,
		`readerFocusCopy("图片暂时无法显示", "Image is temporarily unavailable")`,
		`readerFocusCopy("关闭图片", "Close image")`,
		`readerFocusCopy("未命名焦点", "Untitled focus")`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("Obsidian and Focus fallbacks must be locale-aware: missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`aria-label="笔记路径"`,
		`<p>修改于 ${esc(`,
		`return text(parity.focusSpaces.find((item) => text(item.id) === text(focusID))?.title, "未命名焦点")`,
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("user-visible copy still bypasses locale fallback: %q", forbidden)
		}
	}
}

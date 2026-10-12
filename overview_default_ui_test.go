package main

import (
	"regexp"
	"strings"
	"testing"
)

// 内容概览已下线：未选中条目时详情面板渲染缺省页（#4），模块入口收敛到左侧导航；
// Obsidian 文件夹行数字徽标需要第四个 grid 列，否则掉进隐式第二行折成"多余横线"（#2）。
func TestOverviewDefaultPageReplacesLegacyOverview(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/js/focus-app.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`function renderOverview()`,
		`overview-default`,
		`data-overview-view="`,
		`从左侧选择信息、会议或笔记，内容会在这里展开`,
		`window.__plaudSwitchView`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("overview default page is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`function bindOverviewEvents`,
		`function morningBriefOverview`,
		`function overviewDecisions`,
		`id="open-overview"`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("legacy overview markup is still wired up: %q", forbidden)
		}
	}
}

func TestOverviewDefaultPageStylesAndNavigationHook(t *testing.T) {
	htmlBytes, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	for _, expected := range []string{
		`.overview-default`,
		`.overview-default-link`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("overview default page styles are missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`open-overview`,
		`mobile-overview`,
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("legacy overview entry point is still present: %q", forbidden)
		}
	}

	parityBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	parity := string(parityBytes)
	if !strings.Contains(parity, `window.__plaudSwitchView = switchProductV3View;`) {
		t.Error("parity layer must expose switchProductV3View so the default page shares the sidebar view-switch path")
	}
}

// X 推文线程详情：卡片可点开（data-select-entry），选中走局部更新路径，
// 线程（引用/互动数据/回复）渲染在 #detail-panel 常规文档流里随面板滚动。
func TestXThreadDetailOpensAndScrolls(t *testing.T) {
	parityBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	parity := string(parityBytes)
	for _, expected := range []string{
		`<button class="x-entry-main" type="button" data-select-entry="`,
		`return selectNewsEntryLocally(entry);`,
		`function parseXThread(`,
		`function xThreadMarkup(`,
		`async function hydrateXDetailThread(`,
		`data-x-thread`,
		`x-thread-replies`,
	} {
		if !strings.Contains(parity, expected) {
			t.Errorf("x thread detail is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`state.selected = null;
      document.body.classList.remove("detail-open");
      if (!pref(entry).read) void markReaderEntryReadLocally(entry);
      return;`,
	} {
		if strings.Contains(parity, forbidden) {
			t.Errorf("x entries must open the thread detail instead of being dropped: %q", forbidden)
		}
	}
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, expected := range []string{`.x-reply{`, `.x-thread-quote{`, `.x-thread-stats{`} {
		if !strings.Contains(css, expected) {
			t.Errorf("x thread styles are missing %q", expected)
		}
	}
}

// index.html 曾有一个 "</span" 丢了闭合 ">"：浏览器把下一行的 "</div>" 当成
// 该结束标签的垃圾属性吃掉，timeline-title-row 永不闭合，整棵时间线子树嵌套
// 错位一层，timeline-head 长到内容高度被 panel overflow:hidden 裁掉 —— 时间线
// 完全无法滚动。禁止任何行以未闭合的结束标签收尾（下一行必须以 ">" 续上）。
func TestIndexHtmlEndTagsCloseOnTheirOwnLine(t *testing.T) {
	htmlBytes, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(htmlBytes), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !regexp.MustCompile(`</[a-zA-Z][a-zA-Z0-9-]*$`).MatchString(trimmed) {
			continue
		}
		next := ""
		if i+1 < len(lines) {
			next = strings.TrimSpace(lines[i+1])
		}
		if !strings.HasPrefix(next, ">") {
			t.Errorf("index.html:%d ends with an unclosed end tag %q; the following line must start with \">\" or the tag must close on its own line", i+1, trimmed)
		}
	}
}

func TestObsidianFolderRowCountStaysOnItsRow(t *testing.T) {
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	// 文件夹行 4 个子元素（chevron/图标/名称/计数），不给第四列计数会掉进隐式第二行，
	// 在行下方折成一竖条 —— 用户看到的"多余横线"。
	if !strings.Contains(css, `.obsidian-folder-row{grid-template-columns:16px 17px minmax(0,1fr) auto}`) {
		t.Error("obsidian folder rows need a fourth auto grid column for the count badge")
	}
	if !strings.Contains(css, `.obsidian-folder-row .count{`) {
		t.Error("obsidian folder row count badge styling is missing")
	}
}

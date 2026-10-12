package main

import (
	"strings"
	"testing"
)

func TestCitationDrawerAnchorsLegacyMultiParagraphExcerpt(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		"function citationTargetIn(drawer, citation, hashMismatch)",
		"quoteIndex: quote.indexOf(value)",
		"item.value.length >= 12 && item.quoteIndex >= 0",
		"left.quoteIndex - right.quoteIndex",
		"return ranked[0].node",
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("citation locator is missing %q", expected)
		}
	}
}

func TestCitationDrawerUsesTypedReaderDetails(t *testing.T) {
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
		`function renderCitationArticleDetail(drawer, entry, citation, loaded)`,
		`function hydrateCitationEntry(entry)`,
		`renderMeetingDetail(entry, drawer, { citation: true })`,
		`renderObsidianDetail(entry, drawer, { citation: true })`,
		`articleAIOverviewMarkup(entry.item)`,
		`data-close-focus-citation`,
		`highlightCitationTarget(drawer, citation, loaded, entry)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("typed citation drawer is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.focus-source-detail-drawer.detail-panel{padding:0;overflow:hidden}`,
		`.focus-source-scroll{min-height:0;flex:1;`,
		`.focus-source-detail-drawer.meeting-v3-detail-panel{display:flex;`,
		`.focus-source-detail-drawer.obsidian-detail-panel{display:block;`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("typed citation styles are missing %q", expected)
		}
	}
}

func TestFocusLinkedMeetingAndObsidianKeepFocusDrawerOpen(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`function focusLinkedSourceCitation(source, entry = focusSourceEntry(source))`,
		`source_kind: kind`,
		`focusCitationDataAttributes(citation)`,
		`if (citation) void openCitationDrawer(citation);`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("linked source drawer is missing %q", expected)
		}
	}
	if strings.Contains(script, `closeFocusV3Drawer();\n        if (key) selectEntry(key);`) {
		t.Error("linked meeting/Obsidian must not close the focus drawer and route through the legacy Reader detail")
	}
}

func TestFocusResultCitationsResolveReaderTitlesAndDeduplicateLegacyMirrors(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`function enrichFocusCitation(value)`,
		`const entry = citationEntryFor(citation);`,
		`title: citation.title || entry.title`,
		`citation.asset_id || citation.entry_id || citation.source_id || citation.quote`,
		`.map((value) => enrichFocusCitation(value))`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("focus citation enrichment is missing %q", expected)
		}
	}
	if strings.Contains(script, `const key = [citation.source_kind, citation.entry_id, citation.asset_id, citation.block_id, citation.source_id, citation.quote]`) {
		t.Fatal("rich citations and legacy source_ids must not render as duplicate evidence chips")
	}
}

// TestBodyMountedCitationArticleDrawerStaysFixedOverlay 资料/文章详情抽屉是
// presentCitationDrawer 直挂 document.body 的 overlay；renderCitationArticleDetail
// 给它拼上 detail-panel article-v3-detail 复用阅读器排版后，阅读器的
// .detail-panel.article-v3-detail{position:relative}（特异性 0,2,0）压过抽屉
// 基座 .focus-citation-drawer{position:fixed}（0,1,0），抽屉掉回 body 文档流
// 左上角、高度塌陷且 overflow:hidden 滚不动。必须有一条更高特异性的覆盖规则
// 把这个变体钉回 fixed 视口定位，并排在阅读器基础规则之后（双保险）。
func TestBodyMountedCitationArticleDrawerStaysFixedOverlay(t *testing.T) {
	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)

	// 阅读器自己的文章详情面板仍是文档流内的 relative，不许被顺手改掉。
	base := `.detail-panel.article-v3-detail{position:relative;`
	// 抽屉变体覆盖：(0,3,0) > (0,2,0)，恢复视口定位与上/右/下三边锚定。
	override := `.focus-citation-drawer.detail-panel.article-v3-detail{position:fixed;top:0;right:0;bottom:0}`
	for _, expected := range []string{base, override} {
		if !strings.Contains(css, expected) {
			t.Errorf("body-mounted citation drawer overlay rule is missing %q", expected)
		}
	}
	if strings.LastIndex(css, override) < strings.LastIndex(css, base) {
		t.Error("the citation drawer fixed-position override must come after the reader .detail-panel.article-v3-detail base rule")
	}
	if strings.Contains(css, `.focus-citation-drawer.detail-panel.article-v3-detail{position:relative`) {
		t.Error("the article citation drawer variant must never be flow-positioned")
	}
}

// TestCitationArticleVariantClassContractPinsBodyDirectMount CSS 覆盖选择器
// （.focus-citation-drawer.detail-panel.article-v3-detail）是按"直挂 body 的
// aside、类名恰为这一串"写的：JS 侧改挂载点或改类名都必须同步改覆盖规则。
// 这里把两端钉死，防止只改一端让抽屉再次掉回文档流。
func TestCitationArticleVariantClassContractPinsBodyDirectMount(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		`document.body.append(mask, drawer);`,
		`drawer.className = "focus-citation-drawer focus-source-detail-drawer detail-panel article-v3-detail";`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("body-direct citation mount contract is missing %q", expected)
		}
	}
}

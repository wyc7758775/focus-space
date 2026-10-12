package main

import (
	"strings"
	"testing"
)

func TestArticleAIOverviewPresentationFlow(t *testing.T) {
	scriptRaw, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	stylesRaw, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	pageRaw := indexPageCorpus(t)
	script, styles, page := string(scriptRaw), string(stylesRaw), string(pageRaw)
	bootstrapRaw, err := appFS.ReadFile("static/reader-bootstrap.js")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := string(bootstrapRaw)
	for _, expected := range []string{
		`id="focus-bootstrap"`,
		`id="app" hidden`,
		`reader-bootstrap.js?v=20260911-first-paint`,
		`if (window.__plaudParityReady) void refresh()`,
	} {
		if !strings.Contains(page, expected) {
			t.Errorf("first-paint bootstrap is missing %q", expected)
		}
	}
	if !strings.Contains(page, `item.safe_html || item.content_html`) {
		t.Fatal("static article details must render the sanitized content_html projection when safe_html is absent")
	}
	for _, expected := range []string{
		`if (!window.__plaudParityReady) throw new Error`,
		`appRoot.hidden = false`,
		`status.remove()`,
		`焦点空间未能打开，请重新加载。`,
	} {
		if !strings.Contains(bootstrap, expected) {
			t.Errorf("first-paint bootstrap script is missing %q", expected)
		}
	}

	for _, expected := range []string{
		`function articleAIOverviewMarkup(item)`,
		`function installArticleAIOverview(entry)`,
		`const anchor = panel.querySelector(".detail-top");`,
		`role="status" aria-live="polite"`,
		`overview.key_points`,
		`overview.impact`,
		`ai_overview: item.ai_overview || null`,
		`auto_generate_article_overviews`,
		`requestArticleAIOverview`,
		`data-generate-article-overview`,
		`data-retry-article-ai-overview`,
		`articleAIOverviewErrorMessage(errorCode)`,
		`设备 AI 服务暂时不可用，请确认设备已联网并运行 AI 服务后重试`,
		`article-ai-overview-skeleton`,
		`<span>来源摘要</span>`,
		`reader_focus`,
		`reader_recommendations`,
		`已达预算上限`,
		`排队中`,
		`preparing_source`,
		`generating`,
		`validating`,
		`retry_wait`,
		`if (loading && !overview)`,
		`entry.item.ai_overview?.status === "pending"`,
		`...(timeoutMs > 0 ? { timeoutMs } : {})`,
		`function scheduleArticleAIOverviewPoll(entry, delayMs = 1500)`,
		`pendingAfterRequestFailure`,
		`function articleHTML(content)`,
		`content?.safe_html`,
		`format === "html" ? content?.content : ""`,
		`document.body.classList.remove("x-feed", "video-gallery")`,
		`$("timeline")?.classList.remove("video-grid")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("article AI overview UI is missing %q", expected)
		}
	}
	if strings.Contains(script, `<section class="x-detail-summary"><span>AI 概览</span>`) {
		t.Fatal("RSS source summary must not be presented as an AI-generated overview")
	}
	if strings.Contains(script, `panel.querySelector(".x-detail-post")`) {
		t.Fatal("X overview must appear before the post body, not after it")
	}
	for _, expected := range []string{
		`.article-ai-overview{`,
		`.article-ai-overview{width:100%;max-width:760px;min-height:132px;margin:0 auto 24px`,
		`.article-v3-detail .article-ai-overview{max-width:720px;min-height:0;margin:20px 0 28px`,
		`.article-v3-detail .detail-body{max-width:720px!important;margin:36px 0 0!important;padding:24px 0 0!important`,
		`.article-ai-overview-summary{`,
		`.article-ai-overview-points{`,
		`.article-ai-overview-impact{`,
		`.article-ai-overview.is-pending`,
		`.article-ai-overview.is-failed`,
		`.article-ai-overview.is-loading`,
		`.article-ai-overview-action`,
		`prefers-reduced-motion`,
		`.x-detail-panel .article-ai-overview+.x-detail-post`,
		`.article-ai-overview-action{display:inline-flex;align-items:center;justify-content:center;gap:5px;min-height:32px`,
		`@media(pointer:coarse){.article-ai-overview-action{min-height:44px}}`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("article AI overview styles are missing %q", expected)
		}
	}
	if !strings.Contains(page, `sparkles:`) {
		t.Fatal("article AI overview must use the embedded vector icon")
	}
}

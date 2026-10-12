package main

import (
	"strings"
	"testing"
)

// TestReaderNavStabilityPackMarkers pins the three 2026-09-26 reader fixes:
// the v3 sidebar exposes the aggregate reader views again (全部内容/订阅文章/
// 视频/图片), the article detail body stays centered, and the product-v3 nav
// markup no longer bakes runtime icon state (which made every later render
// rebuild the whole sidebar and visibly flash).
func TestReaderNavStabilityPackMarkers(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		// Aggregate reader views wired into the product-v3 nav. Once the v3
		// shell mounts, #view-nav is hidden by CSS and these buttons are the
		// only path back to the reader views.
		`<nav class="focus-v3-nav-categories">${category}</nav>`,
		`["all", "全部内容", "All", "all"]`,
		`["rss", "订阅文章", "Articles", "rss"]`,
		`["video", "视频", "Videos", "video"]`,
		`["picture", "图片", "Pictures", "picture"]`,
		// Nav build must depend only on source rows + static assets.
		// src/is-loaded transitions live on the DOM node (load handler).
		`const immediateIcon = priorityIcon ? remoteIcon : "";`,
		`The built markup must stay byte-identical across icon-state transitions`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("reader nav stability pack is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		// Baked icon cache: reintroducing either makes the nav memo guard
		// fire on every icon settle and rebuild the sidebar.
		`cachedIcon`,
		`sourceIconState`,
		// Folo-hosted assets are outside the licensing boundary and a
		// remote single point of failure for a fallback tier.
		`icons.folo.is`,
	} {
		if strings.Contains(script, forbidden) {
			t.Errorf("reader nav stability pack reintroduced %q", forbidden)
		}
	}

	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, expected := range []string{
		// Wide detail panels keep the article composition centered on one
		// axis: heading block, body, and the in-column siblings all use auto
		// side margins instead of hugging the left edge.
		`.article-v3-heading{width:100%!important;max-width:760px!important;display:block!important;margin:0 auto!important`,
		`margin:36px auto 0!important`,
		`.article-v3-byline{max-width:720px;margin:22px auto 0`,
		`.article-v3-detail .detail-tools-row{max-width:720px;margin:18px auto 0}`,
		// The load handler still owns is-loaded visibility.
		`.source-avatar-image:not(.is-loaded){visibility:hidden}`,
		// The category nav block the markup now renders.
		`.focus-v3-nav-categories{`,
		// Sidebar/timeline widths have exactly one source of truth: the
		// applyReaderWidths inline vars. Every shell grid (reader, focus,
		// video-gallery, x-feed) must read them, so switching views never
		// resizes the sidebar column.
		`grid-template-columns:var(--sidebar-width) 5px minmax(320px,var(--timeline-width)) 5px minmax(0,1fr)!important`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader css is missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		// Hardcoded third widths on top of the preference vars made the
		// sidebar jump 256→268px every time a view auto-opened a detail.
		`268px 5px minmax(420px,420px)`,
		`grid-template-columns:238px`,
	} {
		if strings.Contains(css, forbidden) {
			t.Errorf("reader css reintroduced a competing sidebar width: %q", forbidden)
		}
	}
}

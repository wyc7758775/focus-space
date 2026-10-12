package main

import (
	"strings"
	"testing"
)

// TestReaderInteractionPackMarkers pins the Phase-1 interaction surface: the
// keyboard additions (Shift+A / ? with overlay guards), the undoable bulk
// mark-all, the opt-in scroll-read observer, and the per-source display
// override. Markers are behavior anchors, not coverage of the JS itself.
func TestReaderInteractionPackMarkers(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	for _, expected := range []string{
		// Keyboard: Shift+A mark-all and ? help, plus the overlay guards so
		// navigation keys never leak through the open help panel.
		`event.key === "A"`,
		`event.key === "?"`,
		`isShortcutsOverlayOpen() ||`,
		`function toggleShortcutsOverlay()`,
		`function isShortcutsOverlayOpen()`,
		// Undoable mark-all: snapshot of previously-unread entries, toast
		// action, and reverse mutation in bounded 200-key batches.
		`function undoMarkVisibleRead(`,
		`function showToastAction(`,
		`newsKeys.slice(index, index + 200)`,
		// Opt-in scroll-read: one observer, re-observed after every timeline
		// repaint, never applied to recording cards, off unless enabled.
		`function ensureMarkReadOnScrollObserver(`,
		`function observeTimelineForScrollRead()`,
		`if (!state.reader?.mark_read_on_scroll) return`,
		`key.startsWith("recording:")`,
		`renderParityTimelineScrollRead`,
		`mark_read_on_scroll: Boolean(state.reader.mark_read_on_scroll)`,
		// Per-source display override: resolution + selector + persistence.
		`function viewModeForSourceID(`,
		`function syncSourceDisplaySelector()`,
		`./api/source-display`,
		// Card-local preference flip bridge (no full repaint per m/s/l).
		`window.__updateReaderCardLocally = updateNewsCardLocally`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("reader interaction pack is missing %q", expected)
		}
	}

	inlineBytes, err := appFS.ReadFile("static/js/focus-app.js")
	if err != nil {
		t.Fatal(err)
	}
	inline := string(inlineBytes)
	for _, expected := range []string{
		// Filter-sensitive fields (starred/read-later views) still repaint;
		// everything else flips the visible card locally.
		`window.__updateReaderCardLocally?.(key, field, value)`,
		`state.view === "starred" && field === "starred"`,
		`state.view === "later" && field === "read_later"`,
	} {
		if !strings.Contains(inline, expected) {
			t.Errorf("focus-app local card update is missing %q", expected)
		}
	}
}

// TestReaderInteractionScrollReadObserverIsBounded guards the HR1 shape of the
// scroll-read path: the observer is disconnected before each re-observe sweep
// (replaced cards never accumulate) and recordings are excluded from
// observation at observe time, not only at callback time.
func TestReaderInteractionScrollReadObserverIsBounded(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)

	observeStart := strings.Index(script, "function observeTimelineForScrollRead()")
	observeEnd := strings.Index(script[observeStart:], "function syncScrollReadButton()")
	if observeStart < 0 || observeEnd < 0 {
		t.Fatal("observe/sync function boundaries are missing")
	}
	observeBody := script[observeStart : observeStart+observeEnd]
	for _, expected := range []string{
		"observer.disconnect()",
		`startsWith("recording:")`,
	} {
		if !strings.Contains(observeBody, expected) {
			t.Errorf("scroll-read observe sweep is missing %q", expected)
		}
	}
}

// TestReaderPictureWallMarkers pins the Phase-3 picture wall: the aggregate
// view joins the typed pagination contract (view_type=picture with its own
// counts field), tiles render through the media proxy, the wall is pure CSS
// columns, and the lightbox owns the keyboard while it is open.
func TestReaderPictureWallMarkers(t *testing.T) {
	scriptBytes, err := appFS.ReadFile("static/reader-parity.js")
	if err != nil {
		t.Fatal(err)
	}
	script := string(scriptBytes)
	for _, expected := range []string{
		// Aggregate contract: one predicate mirrored client-side, its own
		// counts field on both total maps, and the typed request parameter.
		`function aggregatePictureNews(`,
		`case "picture":`,
		`picture: "picture_entries"`,
		`["rss", "video", "youtube", "picture"].includes`,
		// Wall: tiles resolve images via the media proxy, per-source "gallery"
		// mode reuses the wall, and the container class is a single toggle.
		`function pictureImageForItem(`,
		`function renderPictureEntry(`,
		`data-open-picture=`,
		`state.view === "picture" || scopedDisplayMode === "gallery"`,
		`classList.toggle("picture-grid", pictureWall)`,
		// Lightbox: overlay + clamped stepping (no wrap), opened from tile
		// actions, Esc/arrow handling inside the one keyboard listener.
		`function openPictureLightbox(`,
		`function closePictureLightbox(`,
		`function isPictureLightboxOpen(`,
		`next < 0 || next >= pictureLightboxState.entries.length`,
		`stepPictureLightbox(-1)`,
		`stepPictureLightbox(1)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("picture wall is missing %q", expected)
		}
	}
	if got := strings.Count(script, `picture: "picture_entries"`); got < 2 {
		t.Errorf("picture_entries total field mapped %d times, want entryTotalForView + aggregateTotalField", got)
	}
	if got := strings.Count(script, `["rss", "video", "youtube", "picture"].includes`); got < 3 {
		t.Errorf("typed picture request wired in %d fetch sites, want >= 3 (aggregate load, page fetch, scope fetch)", got)
	}

	inlineBytes, err := appFS.ReadFile("static/js/focus-app.js")
	if err != nil {
		t.Fatal(err)
	}
	inline := string(inlineBytes)
	for _, expected := range []string{
		// Persisted view allowlist and the dashboard counts field map.
		`"youtube", "picture", "meeting"`,
		`["picture", "picture_entries"]`,
	} {
		if !strings.Contains(inline, expected) {
			t.Errorf("focus-app picture wiring is missing %q", expected)
		}
	}

	indexBytes, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexBytes)
	for _, expected := range []string{
		`data-view="picture"`,
		`data-count="picture"`,
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("index.html picture navigation is missing %q", expected)
		}
	}

	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, expected := range []string{
		// Masonry is CSS columns only — no measurement loop, no library.
		`.timeline-scroll.picture-grid{display:block;column-width:`,
		`break-inside:avoid`,
		`.picture-lightbox[hidden]{display:none}`,
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("picture wall css is missing %q", expected)
		}
	}
}

// TestMotionTokenLayer pins the Phase-2 visual foundation: the motion/radius
// tokens exist once in :root, the interaction surfaces use them, and every
// keyboard-reachable hit target shares one focus-visible contract.
func TestMotionTokenLayer(t *testing.T) {
	indexBytes, err := appFS.ReadFile("static/index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexBytes)
	for _, expected := range []string{
		"--ease: cubic-bezier(0.33, 1, 0.68, 1)",
		"--t-fast: 130ms",
		"--t-base: 180ms",
		"--t-slow: 260ms",
		"--radius-sm: 5px",
		"--radius-lg: 10px",
	} {
		if !strings.Contains(index, expected) {
			t.Errorf("motion token layer is missing %q", expected)
		}
	}

	cssBytes, err := appFS.ReadFile("static/reader-parity.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(cssBytes)
	for _, expected := range []string{
		"transition:transform var(--t-base) var(--ease)",
		".icon-btn:focus-visible,.nav-item:focus-visible,.text-btn:focus-visible,.primary-btn:focus-visible,.source-display-select:focus-visible,.toast-action:focus-visible",
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("reader css is missing %q", expected)
		}
	}
}

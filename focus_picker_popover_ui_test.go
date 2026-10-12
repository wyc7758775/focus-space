package main

import (
	"strings"
	"testing"
)

func TestFocusPickerUsesAnchoredShadcnPopoverFlow(t *testing.T) {
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
		`function positionFocusPicker(trigger, popover)`,
		`function syncFocusPickerTrigger(trigger, descriptor)`,
		`syncFocusPickerTrigger(trigger, descriptor);`,
		`const popoverWidth = popover.offsetWidth;`,
		`const popoverHeight = popover.offsetHeight;`,
		`async function openFocusPicker(entryKey, trigger = null)`,
		`class="focus-v3-picker-popover"`,
		`placeholder="${readerFocusCopy("搜索焦点", "Search focuses")}"`,
		`role="listbox" aria-label="${readerFocusCopy("选择焦点", "Select focus")}"`,
		`data-focus-picker-search=`,
		`document.addEventListener("pointerdown", dismiss, true)`,
		`const schedulePlace = () => {`,
		`document.addEventListener("scroll", schedulePlace, true)`,
		`window.addEventListener("resize", schedulePlace)`,
		`closeFocusPicker(true)`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("anchored focus picker flow is missing %q", expected)
		}
	}
	for _, expected := range []string{
		`.focus-v3-picker-layer{position:fixed;inset:0;z-index:95;pointer-events:none}`,
		`.focus-v3-picker-popover{position:fixed;width:min(320px,calc(100vw - 16px));`,
		`box-shadow:0 16px 34px rgba(0,0,0,.42);pointer-events:auto;`,
		`html[data-theme="light"] .focus-v3-picker-popover`,
		`@media(prefers-reduced-motion:reduce)`,
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("anchored focus picker styles are missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`focus-v3-picker-dialog`,
		`aria-modal="true" aria-label="加入焦点"`,
	} {
		if strings.Contains(script, forbidden) || strings.Contains(styles, forbidden) {
			t.Errorf("focus picker must not use the old centered modal contract %q", forbidden)
		}
	}
}

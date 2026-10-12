package main

import (
	"strings"
	"testing"
)

func TestFocusLinkUIHandlesUnicodeSourcesAndKeepsArticleEntryVisible(t *testing.T) {
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
		"function focusIdempotencyKey(rawValue)",
		`/^[A-Za-z0-9._:-]{1,128}$/.test(value)`,
		`new TextEncoder().encode(value)`,
		`options.headers["Idempotency-Key"] = focusIdempotencyKey(`,
		`if (!focus) {`,
		`focus.dataset.addEntryFocus = entry.key`,
		`article-v3-focus-picker`,
		`<span>焦点${`,
		`icon("chevron")`,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("focus link UI is missing %q", expected)
		}
	}
	for _, expected := range []string{
		".article-v3-focus-picker{",
		".article-v3-focus-picker.is-linked{",
		".article-v3-focus-picker span{",
	} {
		if !strings.Contains(styles, expected) {
			t.Errorf("focus link styles are missing %q", expected)
		}
	}
}

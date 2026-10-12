package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSourceDisplayPrefPutGetDeleteFlow proves the per-source display override
// contract: PUT stores an explicit override, "auto" deletes it (the table only
// holds explicit user choices), GET returns the surviving map, and invalid
// ids/modes fail closed with 400 before touching the store.
func TestSourceDisplayPrefPutGetDeleteFlow(t *testing.T) {
	db = openTestDB(t)

	put := func(body string) *httptest.ResponseRecorder {
		return callJSON(t, sourceDisplayPrefHandler, http.MethodPut, "/api/source-display", body)
	}

	for _, tc := range []struct{ id, mode string }{
		{"youtube-anthropic", "video"},
		{"wechat-ruanyifeng", "article"},
		{"openai-news", "gallery"},
	} {
		if rec := put(`{"source_id":"` + tc.id + `","view_mode":"` + tc.mode + `"}`); rec.Code != 200 {
			t.Fatalf("put %s: %s", tc.id, rec.Body.String())
		}
	}

	// "auto" removes the override instead of storing a row.
	if rec := put(`{"source_id":"youtube-anthropic","view_mode":"auto"}`); rec.Code != 200 {
		t.Fatalf("put auto: %s", rec.Body.String())
	}

	rec := callJSON(t, sourceDisplayPrefHandler, http.MethodGet, "/api/source-display", "")
	if rec.Code != 200 {
		t.Fatalf("get: %s", rec.Body.String())
	}
	body := rec.Body.String()
	want := []string{`"wechat-ruanyifeng":"article"`, `"openai-news":"gallery"`}
	for _, fragment := range want {
		if !staticContains(body, fragment) {
			t.Fatalf("get body missing %s: %s", fragment, body)
		}
	}
	if staticContains(body, "youtube-anthropic") {
		t.Fatalf("auto must delete the override, got: %s", body)
	}

	for _, bad := range []string{
		`{"source_id":"news:123","view_mode":"video"}`,   // item key, not a source id
		`{"source_id":"","view_mode":"video"}`,           // empty
		`{"source_id":"has space","view_mode":"video"}`,  // invalid charset
		`{"source_id":"UPPER","view_mode":"video"}`,      // uppercase rejected
		`{"source_id":"ok-id","view_mode":"magazine"}`,   // unknown mode
		`{"source_id":"ok-id","view_mode":""}`,           // empty mode
	} {
		if rec := put(bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d: %s", bad, rec.Code, rec.Body.String())
		}
	}

	if rec := callJSON(t, sourceDisplayPrefHandler, http.MethodPost, "/api/source-display", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestReaderPreferencesMarkReadOnScrollRoundTrip proves the opt-in scroll-read
// preference survives a PATCH + GET round trip and defaults to off for a fresh
// database.
func TestReaderPreferencesMarkReadOnScrollRoundTrip(t *testing.T) {
	db = openTestDB(t)

	rec := callJSON(t, readerPreferenceHandler, http.MethodGet, "/api/reader", "")
	if rec.Code != 200 {
		t.Fatalf("get: %s", rec.Body.String())
	}
	if !staticContains(rec.Body.String(), `"mark_read_on_scroll":false`) {
		t.Fatalf("default must be off, got: %s", rec.Body.String())
	}

	rec = callJSON(t, readerPreferenceHandler, http.MethodPatch, "/api/reader",
		`{"sidebar_width":256,"timeline_width":420,"view_mode":"compact","mark_read_on_scroll":true}`)
	if rec.Code != 200 {
		t.Fatalf("patch: %s", rec.Body.String())
	}
	rec = callJSON(t, readerPreferenceHandler, http.MethodGet, "/api/reader", "")
	if !staticContains(rec.Body.String(), `"mark_read_on_scroll":true`) {
		t.Fatalf("expected persisted true, got: %s", rec.Body.String())
	}

	// Legacy three-field PATCH (old clients) keeps the stored value.
	rec = callJSON(t, readerPreferenceHandler, http.MethodPatch, "/api/reader",
		`{"sidebar_width":260,"timeline_width":430,"view_mode":"comfortable"}`)
	if rec.Code != 200 {
		t.Fatalf("legacy patch: %s", rec.Body.String())
	}
	rec = callJSON(t, readerPreferenceHandler, http.MethodGet, "/api/reader", "")
	if !staticContains(rec.Body.String(), `"mark_read_on_scroll":true`) {
		t.Fatalf("legacy patch must not reset scroll-read, got: %s", rec.Body.String())
	}
}

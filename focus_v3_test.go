package main

import (
	"net/http"
	"testing"
)

func TestFocusDecisionTurnBridgeResponseContractUnit(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "accepted receipt", status: http.StatusAccepted, body: `{"state":"accepted","run_id":"decision-run-1","revision":2}`, want: true},
		{name: "terminal receipt", status: http.StatusOK, body: `{"state":"terminal","run_id":"decision-run-1","revision":4}`, want: true},
		{name: "structured conflict", status: http.StatusConflict, body: `{"error":{"code":"run_revision_conflict","message":"决策运行已变化","current_revision":3}}`, want: true},
		{name: "legacy wrong field", status: http.StatusOK, body: `{"status":"accepted"}`, want: false},
		{name: "html error", status: http.StatusBadGateway, body: `<html>upstream failure</html>`, want: false},
		{name: "redirect", status: http.StatusFound, body: `{"error":{"code":"redirect","message":"redirected"}}`, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validFocusDecisionTurnBridgeResponse(test.status, []byte(test.body)); got != test.want {
				t.Fatalf("validFocusDecisionTurnBridgeResponse(%d, %q)=%t, want %t", test.status, test.body, got, test.want)
			}
		})
	}
}

func TestDecisionRunTransitionMatrixUnit(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{name: "queued starts context", from: "queued", to: "collecting_context", want: true},
		{name: "context starts model", from: "collecting_context", to: "running_ai", want: true},
		{name: "model applies", from: "running_ai", to: "applying", want: true},
		{name: "apply completes", from: "applying", to: "completed", want: true},
		{name: "retry resumes", from: "failed_retryable", to: "queued", want: true},
		{name: "terminal cannot restart", from: "completed", to: "queued", want: false},
		{name: "cancelled is immutable", from: "cancelled", to: "running_ai", want: false},
		{name: "question cannot skip model", from: "waiting_clarification", to: "running_ai", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decisionRunCanTransition(test.from, test.to); got != test.want {
				t.Fatalf("decisionRunCanTransition(%q, %q)=%t, want %t", test.from, test.to, got, test.want)
			}
		})
	}
}

func TestDecisionRunAnalysisRebindPolicyUnit(t *testing.T) {
	tests := []struct {
		name       string
		runStatus  string
		target     string
		decisionID string
		oldStatus  string
		newStatus  string
		want       bool
	}{
		{name: "fenced stale claim can be replaced", runStatus: "collecting_context", target: "running_ai", oldStatus: "failed", newStatus: "claimed", want: true},
		{name: "active claim cannot be replaced", runStatus: "collecting_context", target: "running_ai", oldStatus: "claimed", newStatus: "claimed", want: false},
		{name: "queued transition is not a rebind", runStatus: "queued", target: "running_ai", oldStatus: "failed", newStatus: "claimed", want: false},
		{name: "completed decision cannot change evidence", runStatus: "collecting_context", target: "running_ai", decisionID: "decision-1", oldStatus: "failed", newStatus: "claimed", want: false},
		{name: "replacement must be claimed", runStatus: "collecting_context", target: "running_ai", oldStatus: "failed", newStatus: "applied", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := decisionRunAnalysisRebindAllowed(test.runStatus, test.target, test.decisionID, test.oldStatus, test.newStatus); got != test.want {
				t.Fatalf("decisionRunAnalysisRebindAllowed(%q, %q, %q, %q, %q)=%t, want %t", test.runStatus, test.target, test.decisionID, test.oldStatus, test.newStatus, got, test.want)
			}
		})
	}
}

func TestFocusCitationKeepsArticleIdentityAndFeedLocatorUnit(t *testing.T) {
	evidence := focusEvidenceEnvelope{
		ID:             "evidence-article-42",
		SourceKind:     "rss",
		SourceID:       "article-42",
		SourceRevision: "content-hash-42",
		EntryID:        "article-42",
		BlockID:        "b_42",
		Quote:          "关键证据",
		TextHash:       "text-hash-42",
		SourceLocator: map[string]any{
			"feed_source_id": "feed-public-7",
			"entry_id":       "article-42",
		},
	}
	citation := focusCitationFromEvidence(evidence, "support")
	if citation.SourceID != "article-42" || citation.EntryID != "article-42" {
		t.Fatalf("article identity drifted: %#v", citation)
	}
	if citation.SourceLocator["feed_source_id"] != "feed-public-7" {
		t.Fatalf("feed source locator missing: %#v", citation.SourceLocator)
	}
	if citation.SourceID == citation.SourceLocator["feed_source_id"] {
		t.Fatal("article SourceID must not collapse to feed_sources.id")
	}
}

func TestArticleBlocksAreStableAndAddressableUnit(t *testing.T) {
	body := []byte(`<article><h2>判断依据</h2><p>第一条关键证据。</p><p>第二条反向证据。</p></article>`)
	first := renderEntryContent(body, "text/html", "https://example.com/article", "https://example.com/article")
	second := renderEntryContent(body, "text/html", "https://example.com/article", "https://example.com/article")
	if len(first.Blocks) != 3 || len(second.Blocks) != len(first.Blocks) || first.BlocksHash == "" || first.BlocksHash != second.BlocksHash {
		t.Fatalf("unstable article blocks: first=%#v second=%#v", first, second)
	}
	for index := range first.Blocks {
		if first.Blocks[index].BlockID == "" || first.Blocks[index].TextHash == "" || first.Blocks[index].BlockID != second.Blocks[index].BlockID {
			t.Fatalf("block %d is not stably addressable: %#v / %#v", index, first.Blocks[index], second.Blocks[index])
		}
		if !containsBlockAttribute(first.SafeHTML, first.Blocks[index].BlockID) {
			t.Fatalf("safe HTML does not expose block %q: %s", first.Blocks[index].BlockID, first.SafeHTML)
		}
	}
}

func containsBlockAttribute(html, blockID string) bool {
	return len(blockID) > 0 && containsAll(html, `data-block-id="`, blockID)
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if fragment == "" || !containsText(value, fragment) {
			return false
		}
	}
	return true
}

func containsText(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func TestFocusChatExecutionScopeRequiresTrustedCompleteHeadersUnit(t *testing.T) {
	headers := make(http.Header)
	headers.Set(trustedHostOperationHeader, trustedHostOperationValue)
	headers.Set(trustedExecutionTokenHeader, "execution-token")
	headers.Set(trustedTurnIDHeader, "turn-1")
	headers.Set(trustedSessionIDHeader, "session-1")
	headers.Set(trustedSessionKeyHeader, "session-key-1")
	if scope, ok := focusChatExecutionScopeFromHeaders(headers); !ok || scope.TurnID != "turn-1" {
		t.Fatalf("trusted scope rejected: %#v ok=%t", scope, ok)
	}
	headers.Del(trustedExecutionTokenHeader)
	if _, ok := focusChatExecutionScopeFromHeaders(headers); ok {
		t.Fatal("scope without execution token was accepted")
	}
	headers.Set(trustedExecutionTokenHeader, "token\nforged")
	if _, ok := focusChatExecutionScopeFromHeaders(headers); ok {
		t.Fatal("scope containing a forged header line was accepted")
	}
}

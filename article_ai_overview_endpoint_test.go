package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArticleOverviewEndpointUsesHostOrigin(t *testing.T) {
	old := articleOverviewProxyEndpoint
	articleOverviewProxyEndpoint = articleOverviewProxyURL
	t.Cleanup(func() { articleOverviewProxyEndpoint = old })
	for _, tc := range []struct{ name, explicit, host, want string }{
		{"gateway device", "", "http://127.0.0.1:19090/api/v1/internal/app-runtime/plaud/device-data/v1", "http://127.0.0.1:19090" + articleOverviewProxyPath},
		{"legacy device", "", "http://127.0.0.1:9090/api/v1/internal/app-runtime/plaud/device-data/v1", articleOverviewProxyURL},
		{"custom port", "", "http://[::1]:20100/api/v1/internal/app-runtime/plaud/device-data/v1", "http://[::1]:20100" + articleOverviewProxyPath},
		{"explicit override", "http://127.0.0.1:20000" + articleOverviewProxyPath, "http://127.0.0.1:19090/host", "http://127.0.0.1:20000" + articleOverviewProxyPath},
		{"standalone legacy", "", "", articleOverviewProxyURL},
		{"malformed host", "", "http://[broken", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AI_PROXY_URL", tc.explicit)
			t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", tc.host)
			got, configured := configuredArticleOverviewProxyEndpoint()
			if got != tc.want || !configured {
				t.Fatalf("endpoint = %q, configured=%v; want %q", got, configured, tc.want)
			}
		})
	}
}

func TestArticleOverviewHostEndpointFailsClosed(t *testing.T) {
	t.Setenv("AI_PROXY_URL", "")
	for _, base := range []string{"https://example.com/host", "http://user:secret@127.0.0.1/host", "http://127.0.0.1/host?token=secret", "http://127.0.0.1/host#fragment", "file:///host"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", base)
			_, err := requestArticleOverviewAI(context.Background(), map[string]any{}, articleOverviewJob{})
			if err == nil || err.Error() != "ai_proxy_unavailable" {
				t.Fatalf("unsafe host accepted: %v", err)
			}
		})
	}
}

func TestArticleOverviewHostEndpointGenerationFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "host-overview-entry")
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != articleOverviewProxyPath || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected path or credential forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"内部地址生成成功\",\"key_points\":[],\"impact\":\"\"}"}}]}`))
	}))
	defer proxy.Close()
	t.Setenv("AI_PROXY_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", proxy.URL+"/api/v1/internal/app-runtime/plaud/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "must-not-be-forwarded")
	queued := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"host-overview-entry"}`)
	if queued.Code != http.StatusOK {
		t.Fatalf("queue failed: %s", queued.Body.String())
	}
	runNextArticleOverviewJobForTest(t)
	ready := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"host-overview-entry"}`)
	if calls != 1 || !strings.Contains(ready.Body.String(), `"status":"ready"`) {
		t.Fatalf("generation did not complete once: calls=%d result=%s", calls, ready.Body.String())
	}
}

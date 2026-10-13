package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func platformPayload() map[string]any {
	return map[string]any{"messages": []map[string]string{{"role": "system", "content": "只输出 JSON"}, {"role": "user", "content": "文章内容"}}}
}

func TestPlatformAIRejectsUnsafeConfigurationUnit(t *testing.T) {
	for _, base := range []string{"", "https://example.com/api/v1/internal/app-runtime/focus/ai/v1", "http://user:pass@127.0.0.1/api/v1/internal/app-runtime/focus/ai/v1", "http://127.0.0.1/api/v1/ai-proxy/v1", "http://127.0.0.1/api/v1/internal/app-runtime/focus/ai/v1?token=x"} {
		t.Run(base, func(t *testing.T) {
			t.Setenv("ZAPP_AI_BASE_URL", base)
			t.Setenv("ZAPP_AI_TOKEN", "private")
			_, err := requestPlatformArticleAI(context.Background(), platformPayload())
			if err == nil || articleOverviewErrorRetryable(err) {
				t.Fatalf("unsafe config: %v", err)
			}
		})
	}
}

func TestPlatformAIFailureDoesNotRetryOrLeakCredentialsUnit(t *testing.T) {
	for _, mode := range []string{"busy", "redirect", "too_large", "unavailable", "body_too_large"} {
		t.Run(mode, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/leaked" {
					t.Error("followed redirect")
				}
				if strings.HasSuffix(r.URL.Path, "/status") {
					available, limit := true, 32768
					if mode == "unavailable" {
						available = false
					}
					if mode == "too_large" {
						limit = 1
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"available": available, "max_input_chars": limit, "max_output_chars": 4000})
					return
				}
				posts++
				if mode == "redirect" {
					http.Redirect(w, r, "/leaked", http.StatusTemporaryRedirect)
					return
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":"ai_busy","error":"private secret must not escape"}`))
			}))
			defer server.Close()
			t.Setenv("ZAPP_AI_BASE_URL", server.URL+"/api/v1/internal/app-runtime/focus/ai/v1")
			t.Setenv("ZAPP_AI_TOKEN", "private")
			payload := platformPayload()
			if mode == "body_too_large" {
				payload["messages"].([]map[string]string)[1]["content"] = strings.Repeat("文", 30000)
			}
			_, err := requestPlatformArticleAI(context.Background(), payload)
			if err == nil || articleOverviewErrorRetryable(err) || strings.Contains(err.Error(), "private") {
				t.Fatalf("failure must be safe and terminal: %v", err)
			}
			if (mode == "too_large" || mode == "unavailable" || mode == "body_too_large") && posts != 0 {
				t.Fatal("unavailable/oversized request consumed AI")
			}
		})
	}
}

func TestPlatformAIOverviewPersistenceFlow(t *testing.T) {
	db = openTestDB(t)
	disableArticleOverviewPublicFetch(t)
	insertArticleOverviewFixture(t, db, "platform-overview")
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Zettlab-App-Data-Token") != "app-only-token" || r.Header.Get("Authorization") != "" {
			t.Error("wrong credentials")
		}
		if strings.HasSuffix(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"available":true,"max_input_chars":32768,"max_output_chars":4000}`))
			return
		}
		if r.URL.Path != "/api/v1/internal/app-runtime/plaud-action-dashboard/ai/v1/complete" {
			t.Errorf("unexpected route: %s", r.URL.Path)
		}
		posts++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 3 || body["instruction"] == nil || body["input"] == nil || body["model"] != nil {
			t.Errorf("not semantic AI request: %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"output": `{"summary":"应用 AI 摘要已保存","key_points":[],"impact":""}`})
	}))
	defer server.Close()
	t.Setenv("ZAPP_AI_BASE_URL", server.URL+"/api/v1/internal/app-runtime/plaud-action-dashboard/ai/v1")
	t.Setenv("ZAPP_AI_TOKEN", "app-only-token")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "different-token")
	queued := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"platform-overview"}`)
	if queued.Code != http.StatusOK {
		t.Fatal(queued.Body.String())
	}
	runNextArticleOverviewJobForTest(t)
	for i := 0; i < 2; i++ {
		result := callJSON(t, articleAIOverviewHandler, http.MethodPost, "/api/entries/ai-overview", `{"entry_id":"platform-overview"}`)
		if !strings.Contains(result.Body.String(), `"status":"ready"`) || !strings.Contains(result.Body.String(), "应用 AI 摘要已保存") {
			t.Fatal(result.Body.String())
		}
	}
	if posts != 1 {
		t.Fatalf("cached result invoked AI %d times", posts)
	}
}

func TestPlatformAIMissingGrantDoesNotUseLegacyProxyFlow(t *testing.T) {
	t.Setenv("ZAPP_LISTEN_ADDR", "127.0.0.1:12345")
	t.Setenv("ZAPP_AI_BASE_URL", "")
	t.Setenv("ZAPP_AI_TOKEN", "")
	_, err := requestArticleOverviewAI(context.Background(), platformPayload(), articleOverviewJob{})
	if err == nil || err.Error() != "ai_unavailable" {
		t.Fatalf("expected missing grant: %v", err)
	}
}

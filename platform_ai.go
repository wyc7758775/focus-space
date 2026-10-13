package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Platform AI failures terminate this attempt. The user can explicitly retry;
// the durable worker must not automatically spend more credits after a timeout.
type platformAIError struct{ code string }

func (e *platformAIError) Error() string { return e.code }

func usesPlatformArticleAI() bool {
	return os.Getenv("ZAPP_AI_BASE_URL") != "" || os.Getenv("ZAPP_AI_TOKEN") != "" || os.Getenv("ZAPP_LISTEN_ADDR") != ""
}

func requestPlatformArticleAI(parent context.Context, payload map[string]any) ([]byte, error) {
	fail := func(code string) ([]byte, error) { return nil, &platformAIError{code: code} }
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(os.Getenv("ZAPP_AI_BASE_URL")), "/"))
	token := strings.TrimSpace(os.Getenv("ZAPP_AI_TOKEN"))
	if err != nil || token == "" || base.Scheme != "http" || !isLoopbackHost(base.Hostname()) || base.User != nil || base.RawQuery != "" || base.Fragment != "" || base.RawPath != "" {
		return fail("ai_unavailable")
	}
	parts := strings.Split(strings.Trim(base.Path, "/"), "/")
	if len(parts) != 7 || strings.Join(parts[:4], "/") != "api/v1/internal/app-runtime" || parts[4] == "" || parts[4] == "." || parts[4] == ".." || strings.Join(parts[5:], "/") != "ai/v1" {
		return fail("ai_unavailable")
	}
	ctx, cancel := boundedArticleOverviewContext(parent, 35*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 35 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path string, body any, out any) error {
		var input io.Reader
		if body != nil {
			raw, err := json.Marshal(body)
			if err != nil {
				return &platformAIError{code: "ai_invalid_request"}
			}
			if len(raw) > 64<<10 {
				return &platformAIError{code: "ai_input_too_large"}
			}
			input = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, base.String()+path, input)
		if err != nil {
			return &platformAIError{code: "ai_unavailable"}
		}
		req.Header.Set("X-Zettlab-App-Data-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return &platformAIError{code: "ai_timeout"}
			}
			return &platformAIError{code: "ai_unavailable"}
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, articleOverviewResponseBytes+1))
		if err != nil || len(raw) > articleOverviewResponseBytes {
			return &platformAIError{code: "ai_invalid_response"}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			var failure struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(raw, &failure)
			switch failure.Code {
			case "ai_timeout", "ai_credits_insufficient", "ai_rate_limited", "ai_content_blocked", "ai_busy", "ai_forbidden", "ai_upstream_error":
				return &platformAIError{code: failure.Code}
			default:
				return &platformAIError{code: "ai_unavailable"}
			}
		}
		if json.Unmarshal(raw, out) != nil {
			return &platformAIError{code: "ai_invalid_response"}
		}
		return nil
	}
	var status struct {
		Available bool `json:"available"`
		MaxInput  int  `json:"max_input_chars"`
		MaxOutput int  `json:"max_output_chars"`
	}
	if err := call(http.MethodGet, "/status", nil, &status); err != nil {
		return nil, err
	}
	if !status.Available || status.MaxInput <= 0 || status.MaxOutput <= 0 {
		return fail("ai_unavailable")
	}
	messages, ok := payload["messages"].([]map[string]string)
	if !ok || len(messages) != 2 {
		return fail("ai_invalid_request")
	}
	if utf8.RuneCountInString(messages[1]["content"]) > status.MaxInput || utf8.RuneCountInString(messages[0]["content"]) > 4096 {
		return fail("ai_input_too_large")
	}
	request := map[string]any{
		"instruction": messages[0]["content"], "input": messages[1]["content"],
		"max_output_chars": min(status.MaxOutput, 2000),
	}
	var result struct {
		Output string `json:"output"`
	}
	if err := call(http.MethodPost, "/complete", request, &result); err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.Output) == "" || utf8.RuneCountInString(result.Output) > min(status.MaxOutput, 2000) {
		return fail("ai_invalid_response")
	}
	// Reuse the existing strict draft validator and persistence path.
	return json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": result.Output}}}})
}

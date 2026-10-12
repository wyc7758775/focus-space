package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"focus-space/internal/bridges"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	focusDecisionTurnPath         = "/api/focus-decision/turn"
	focusDecisionRequestBytes     = 4096
	focusDecisionResponseBytes    = 64 << 10
	focusDecisionRequestTimeout   = 15 * time.Second
	focusDecisionDevicePathSuffix = "/device-data/v1"
)

var focusDecisionIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

type focusDecisionTurnBridge struct {
	target *url.URL
	token  string
	client *http.Client
	err    error
}

type focusDecisionTurnRequest struct {
	FocusID          string `json:"focus_id"`
	RunID            string `json:"run_id"`
	ExpectedRevision int    `json:"expected_revision"`
}

type focusDecisionTurnBridgeResponse struct {
	State    string `json:"state"`
	RunID    string `json:"run_id"`
	Revision int    `json:"revision"`
	Error    *struct {
		Code            string `json:"code"`
		Message         string `json:"message"`
		CurrentRevision int    `json:"current_revision"`
	} `json:"error"`
}

func newFocusDecisionTurnBridgeFromEnv() *focusDecisionTurnBridge {
	return newFocusDecisionTurnBridge(
		os.Getenv("ZAPP_DEVICE_DATA_BASE_URL"),
		os.Getenv("ZAPP_DEVICE_DATA_TOKEN"),
		&http.Client{Timeout: focusDecisionRequestTimeout},
	)
}

func newFocusDecisionTurnBridge(rawBaseURL, token string, client *http.Client) *focusDecisionTurnBridge {
	bridge := &focusDecisionTurnBridge{token: strings.TrimSpace(token)}
	deviceBridge := bridges.NewDeviceDataBridge(rawBaseURL, token, client)
	if deviceBridge.Err != nil || deviceBridge.BaseURL == nil || deviceBridge.Client == nil {
		bridge.err = errors.New("focus decision bridge is not configured")
		return bridge
	}
	path := strings.TrimRight(deviceBridge.BaseURL.Path, "/")
	if !strings.HasSuffix(path, focusDecisionDevicePathSuffix) {
		bridge.err = errors.New("focus decision bridge configuration is invalid")
		return bridge
	}
	target := *deviceBridge.BaseURL
	target.Path = strings.TrimSuffix(path, focusDecisionDevicePathSuffix) + "/focus-decision/v1/turns"
	target.RawPath = ""
	bridge.target = &target
	bridge.client = deviceBridge.Client
	return bridge
}

func focusDecisionTurnRoutes(mux *http.ServeMux) {
	mux.Handle(focusDecisionTurnPath, newFocusDecisionTurnBridgeFromEnv())
}

func (b *focusDecisionTurnBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFocusDecisionBridgeError(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	if b == nil || b.err != nil || b.target == nil || b.client == nil || b.token == "" {
		writeFocusDecisionBridgeError(w, http.StatusServiceUnavailable, "智能分析暂时不可用")
		return
	}
	key := requestIdempotencyKey(r)
	if !focusDecisionIdempotencyKeyPattern.MatchString(key) {
		writeFocusDecisionBridgeError(w, http.StatusBadRequest, "幂等键不合法")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, focusDecisionRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input focusDecisionTurnRequest
	if err := decoder.Decode(&input); err != nil {
		writeFocusDecisionBridgeError(w, http.StatusBadRequest, "请求数据格式不合法")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeFocusDecisionBridgeError(w, http.StatusBadRequest, "请求包含多余数据")
		return
	}
	if !validFocusID(input.FocusID) || !validFocusID(input.RunID) || input.ExpectedRevision < 1 {
		writeFocusDecisionBridgeError(w, http.StatusBadRequest, "焦点分析参数不合法")
		return
	}
	body, err := json.Marshal(input)
	if err != nil {
		writeFocusDecisionBridgeError(w, http.StatusInternalServerError, "智能分析暂时不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), focusDecisionRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.target.String(), bytes.NewReader(body))
	if err != nil {
		writeFocusDecisionBridgeError(w, http.StatusServiceUnavailable, "智能分析暂时不可用")
		return
	}
	request.Header.Set(bridges.DeviceDataTokenHeader, b.token)
	request.Header.Set("X-Zettlab-Idempotency-Key", key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := b.client.Do(request)
	if err != nil {
		writeFocusDecisionBridgeError(w, http.StatusServiceUnavailable, "智能分析暂时不可用")
		return
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, focusDecisionResponseBytes+1))
	if err != nil || len(responseBody) > focusDecisionResponseBytes {
		writeFocusDecisionBridgeError(w, http.StatusBadGateway, "智能分析状态返回异常")
		return
	}
	if !validFocusDecisionTurnBridgeResponse(response.StatusCode, responseBody) {
		writeFocusDecisionBridgeError(w, http.StatusBadGateway, "智能分析状态返回异常")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(responseBody)
}

func validFocusDecisionTurnBridgeResponse(status int, body []byte) bool {
	var response focusDecisionTurnBridgeResponse
	if len(bytes.TrimSpace(body)) == 0 || json.Unmarshal(body, &response) != nil {
		return false
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return response.Error == nil &&
			(response.State == "accepted" || response.State == "running" || response.State == "terminal") &&
			validFocusID(response.RunID) && response.Revision >= 1
	}
	return status >= http.StatusBadRequest && status <= 599 && response.Error != nil &&
		focusDecisionIdempotencyKeyPattern.MatchString(strings.TrimSpace(response.Error.Code)) &&
		validText(response.Error.Message, 1000, true) && response.Error.CurrentRevision >= 0
}

func writeFocusDecisionBridgeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"code": "focus_turn_unavailable", "message": message},
	})
}

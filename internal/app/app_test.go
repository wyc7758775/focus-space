package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONSetsStatusContentTypeAndBody(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, http.StatusCreated, map[string]int{"count": 3})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	var decoded struct{ Count int }
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil || decoded.Count != 3 {
		t.Fatalf("body = %q err = %v", recorder.Body.String(), err)
	}
}

func TestUserErrWritesErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	UserErr(recorder, http.StatusNotFound, "来源不存在")
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got, want := strings.TrimSpace(recorder.Body.String()), `{"error":"来源不存在"}`; got != want {
		t.Fatalf("body = %q want %q", got, want)
	}
}

func TestHTTPErrAlwaysAnswers500WithGenericMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	HTTPErr(recorder, errForTest("内部细节"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "内部细节") {
		t.Fatal("内部错误细节不得泄漏给客户端")
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }

func TestDecodeStrictJSONRejectsUnknownFieldsAndTrailingContent(t *testing.T) {
	request := func(body string) *http.Request {
		return httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte(body)))
	}
	var dst struct {
		Name string `json:"name"`
	}
	if err := DecodeStrictJSON(httptest.NewRecorder(), request(`{"name":"a"}`), 1<<20, &dst); err != nil {
		t.Fatalf("合法请求不应报错: %v", err)
	}
	if err := DecodeStrictJSON(httptest.NewRecorder(), request(`{"name":"a","extra":1}`), 1<<20, &dst); err == nil {
		t.Fatal("未知字段必须拒绝")
	}
	if err := DecodeStrictJSON(httptest.NewRecorder(), request(`{"name":"a"}{"name":"b"}`), 1<<20, &dst); err == nil {
		t.Fatal("尾随 JSON 必须拒绝")
	}
	if err := DecodeStrictJSON(httptest.NewRecorder(), request(`{"name":"`+strings.Repeat("x", 4096)+`"}`), 128, &dst); err == nil {
		t.Fatal("超过大小上限必须拒绝")
	}
}

// Package app 提供应用各处共享的纯 HTTP/JSON 助手。
// 从 main.go 的公共层抽出：零业务依赖，只依赖标准库。
package app

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// WriteJSON 以 JSON 编码写出响应。
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// UserErr 写出用户可见的错误信息。
func UserErr(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// HTTPErr 记录日志并返回统一的服务器内部错误。
func HTTPErr(w http.ResponseWriter, err error) {
	log.Printf("错误: %v", err)
	UserErr(w, http.StatusInternalServerError, "服务器内部错误，请稍后重试")
}

// DecodeStrictJSON 严格解码请求体：大小上限 + 未知字段拒绝 + 尾随内容拒绝。
func DecodeStrictJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

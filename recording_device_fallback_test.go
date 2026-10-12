package main

import (
	"context"
	"focus-space/internal/bridges"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceRecordingFallbackExposesTheSameMeetingToChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "test-device-token" {
			t.Fatalf("missing device data token")
		}
		switch r.URL.Path {
		case "/device-data/v1/meetings":
			_, _ = w.Write([]byte(`{"meetings":[{"id":"meeting-real","title":"焦点空间与卡牌分类","status":"completed","duration_seconds":141,"speaker_count":1,"started_at":1787386620}]}`))
		case "/device-data/v1/meetings/meeting-real":
			_, _ = w.Write([]byte(`{"meeting":{"id":"meeting-real","title":"焦点空间与卡牌分类","status":"completed","duration_seconds":141,"speaker_count":1,"started_at":1787386620,"summary":"真实会议摘要","transcript":"真实逐字稿"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", server.URL+"/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "test-device-token")

	list, err := queryDeviceRecordings(context.Background(), 20)
	if err != nil || len(list) != 1 || list[0].Title != "焦点空间与卡牌分类" {
		t.Fatalf("device recording list = %#v, err = %v", list, err)
	}
	detail, err := queryDeviceRecordingByID(context.Background(), "meeting-real")
	if err != nil || detail.Title != "焦点空间与卡牌分类" || detail.Transcript != "真实逐字稿" {
		t.Fatalf("device recording detail = %#v, err = %v", detail, err)
	}
}

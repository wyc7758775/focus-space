package main

import (
	"focus-space/internal/bridges"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceCountsAndPagingBridgeFlow(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "scope" {
			t.Error("lost app scope")
		}
		w.Write([]byte(`{"total":45,"complete":true}`))
	}))
	defer up.Close()
	bridge := bridges.NewDeviceDataBridge(up.URL+"/device-data/v1", "scope", up.Client())
	for _, path := range []string{"/meetings/count", "/obsidian/counts?path=Notes", "/obsidian/list?path=Notes&limit=200&cursor=next"} {
		w := httptest.NewRecorder()
		bridge.ServeHTTP(w, httptest.NewRequest("GET", bridges.DeviceDataPathPrefix+path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}

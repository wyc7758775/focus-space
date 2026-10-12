package bridges

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceDataBridgeForwardsOnlyFixedReadSurfaceFlow(t *testing.T) {
	var receivedPath, receivedQuery, receivedToken, receivedAuthorization string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		receivedToken = r.Header.Get(DeviceDataTokenHeader)
		receivedAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entries":[{"type":"markdown","path":"PARA/note.md"}]}`))
	}))
	defer upstream.Close()

	bridge := NewDeviceDataBridge(upstream.URL+"/api/v1/internal/app-runtime/plaud-action-dashboard/device-data/v1", "trusted-token", upstream.Client())
	req := httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/obsidian/list?path=PARA", nil)
	req.Header.Set("Authorization", "Bearer browser-token")
	recorder := httptest.NewRecorder()
	bridge.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if receivedPath != "/api/v1/internal/app-runtime/plaud-action-dashboard/device-data/v1/obsidian/list" || receivedQuery != "path=PARA" {
		t.Fatalf("upstream path=%q query=%q", receivedPath, receivedQuery)
	}
	if receivedToken != "trusted-token" || receivedAuthorization != "" {
		t.Fatalf("token=%q authorization=%q", receivedToken, receivedAuthorization)
	}
}

func TestDeviceDataBridgeRoutesUseInjectedCredentialsFlow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/app-runtime/plaud-action-dashboard/device-data/v1/meetings" {
			t.Fatalf("upstream path=%q", r.URL.Path)
		}
		if r.Header.Get(DeviceDataTokenHeader) != "injected-token" {
			t.Fatalf("upstream token=%q", r.Header.Get(DeviceDataTokenHeader))
		}
		_, _ = w.Write([]byte(`{"meetings":[{"id":"meeting-1"}]}`))
	}))
	defer upstream.Close()
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", upstream.URL+"/api/v1/internal/app-runtime/plaud-action-dashboard/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "injected-token")

	mux := http.NewServeMux()
	RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/meetings", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "meeting-1") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDeviceDataBridgeStreamsMeetingAudioRangesFlow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device-data/v1/meetings/meeting-1/audio" {
			t.Fatalf("upstream path=%q", r.URL.Path)
		}
		if r.Header.Get(DeviceDataTokenHeader) != "audio-token" || r.Header.Get("Range") != "bytes=2-5" {
			t.Fatalf("token=%q range=%q", r.Header.Get(DeviceDataTokenHeader), r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "audio/webm")
		w.Header().Set("Content-Range", "bytes 2-5/10")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("2345"))
	}))
	defer upstream.Close()
	bridge := NewDeviceDataBridge(upstream.URL+"/device-data/v1", "audio-token", upstream.Client())
	req := httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/meetings/meeting-1/audio", nil)
	req.Header.Set("Range", "bytes=2-5")
	recorder := httptest.NewRecorder()
	bridge.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "2345" || recorder.Header().Get("Content-Range") != "bytes 2-5/10" {
		t.Fatalf("status=%d range=%q body=%q", recorder.Code, recorder.Header().Get("Content-Range"), recorder.Body.String())
	}
}

func TestDeviceDataBridgeStreamsObsidianAssetRangesFlow(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/device-data/v1/obsidian/asset" || r.URL.Query().Get("path") != "Projects/brief.pdf" {
			t.Fatalf("upstream path=%q query=%q", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get(DeviceDataTokenHeader) != "asset-token" || r.Header.Get("Range") != "bytes=0-3" {
			t.Fatalf("token=%q range=%q", r.Header.Get(DeviceDataTokenHeader), r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Range", "bytes 0-3/12")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("%PDF"))
	}))
	defer upstream.Close()
	bridge := NewDeviceDataBridge(upstream.URL+"/device-data/v1", "asset-token", upstream.Client())
	req := httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/obsidian/asset?path=Projects%2Fbrief.pdf", nil)
	req.Header.Set("Range", "bytes=0-3")
	recorder := httptest.NewRecorder()
	bridge.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "%PDF" || recorder.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("status=%d type=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

func TestDeviceDataBridgeFailsClosedWithoutBreakingAppRoutesUnit(t *testing.T) {
	bridge := NewDeviceDataBridge("", "", nil)
	recorder := httptest.NewRecorder()
	bridge.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/meetings", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing config status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	invalid := NewDeviceDataBridge("https://example.com/device-data/v1", "token", http.DefaultClient)
	recorder = httptest.NewRecorder()
	invalid.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/meetings", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("non-loopback config status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestDeviceDataBridgeRejectsWritesAndOpenProxyInputsUnit(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	bridge := NewDeviceDataBridge(upstream.URL+"/device-data/v1", "token", upstream.Client())

	tests := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, DeviceDataPathPrefix + "/meetings", http.StatusMethodNotAllowed},
		{http.MethodGet, DeviceDataPathPrefix + "/meetings?url=http://example.com", http.StatusNotFound},
		{http.MethodGet, DeviceDataPathPrefix + "/obsidian/read?path=a.md&path=b.md", http.StatusNotFound},
		{http.MethodGet, DeviceDataPathPrefix + "/unknown", http.StatusNotFound},
		{http.MethodGet, DeviceDataPathPrefix + "/meetings/a/b", http.StatusNotFound},
		{http.MethodGet, DeviceDataPathPrefix + "/meetings/a/audio/b", http.StatusNotFound},
	}
	for _, test := range tests {
		recorder := httptest.NewRecorder()
		bridge.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader("ignored")))
		if recorder.Code != test.status {
			t.Fatalf("%s %s status=%d expected=%d", test.method, test.path, recorder.Code, test.status)
		}
	}
	if upstreamCalls != 0 {
		t.Fatalf("rejected requests reached upstream: %d", upstreamCalls)
	}
}

func TestDeviceDataBridgeResponseIsBoundedUnit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxDeviceDataResponseBytes+1)))
	}))
	defer upstream.Close()
	bridge := NewDeviceDataBridge(upstream.URL+"/device-data/v1", "token", upstream.Client())
	recorder := httptest.NewRecorder()
	bridge.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, DeviceDataPathPrefix+"/meetings", nil))
	if recorder.Code != http.StatusBadGateway || !strings.Contains(recorder.Body.String(), "too large") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

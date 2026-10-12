package bridges

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	DeviceDataPathPrefix       = "/_zettlab/device-data/v1"
	DeviceDataTokenHeader      = "X-Zettlab-App-Data-Token"
	deviceDataRequestTimeout   = 7 * time.Second
	maxDeviceDataResponseBytes = 4 << 20
)

type DeviceDataBridge struct {
	BaseURL *url.URL
	Token   string
	Client  *http.Client
	Err     error
}

func NewDeviceDataBridgeFromEnv() *DeviceDataBridge {
	return NewDeviceDataBridge(
		os.Getenv("ZAPP_DEVICE_DATA_BASE_URL"),
		os.Getenv("ZAPP_DEVICE_DATA_TOKEN"),
		&http.Client{Timeout: deviceDataRequestTimeout},
	)
}

func NewDeviceDataBridge(rawBaseURL, token string, client *http.Client) *DeviceDataBridge {
	bridge := &DeviceDataBridge{Token: strings.TrimSpace(token), Client: client}
	if strings.TrimSpace(rawBaseURL) == "" || bridge.Token == "" {
		bridge.Err = errors.New("device data bridge is not configured")
		return bridge
	}
	baseURL, err := url.Parse(strings.TrimRight(rawBaseURL, "/"))
	if err != nil || baseURL.Scheme != "http" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" || !IsDeviceDataLoopbackHost(baseURL.Hostname()) || !strings.HasSuffix(baseURL.Path, "/device-data/v1") {
		bridge.Err = errors.New("device data bridge configuration is invalid")
		return bridge
	}
	if client == nil {
		client = &http.Client{Timeout: deviceDataRequestTimeout}
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	bridge.BaseURL = baseURL
	bridge.Client = &clientCopy
	return bridge
}

func IsDeviceDataLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func RegisterRoutes(mux *http.ServeMux) {
	bridge := NewDeviceDataBridgeFromEnv()
	mux.Handle(DeviceDataPathPrefix+"/", bridge)
}

func (b *DeviceDataBridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeDeviceDataBridgeError(w, http.StatusMethodNotAllowed, "device data bridge is read-only")
		return
	}
	if b == nil || b.Err != nil || b.BaseURL == nil || b.Client == nil {
		writeDeviceDataBridgeError(w, http.StatusServiceUnavailable, "device data is unavailable")
		return
	}
	suffix, allowedQuery, ok := deviceDataRoute(r.URL.Path)
	if !ok || !validDeviceDataQuery(r.URL.Query(), allowedQuery) {
		writeDeviceDataBridgeError(w, http.StatusNotFound, "device data path is unavailable")
		return
	}

	target := *b.BaseURL
	target.Path = strings.TrimRight(target.Path, "/") + suffix
	target.RawQuery = r.URL.RawQuery
	isAudio := deviceDataMeetingAudioID(r.URL.Path) != ""
	isAsset := r.URL.Path == DeviceDataPathPrefix+"/obsidian/asset"
	isStream := isAudio || isAsset
	ctx := r.Context()
	cancel := func() {}
	if !isStream {
		ctx, cancel = context.WithTimeout(ctx, deviceDataRequestTimeout)
	}
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, r.Method, target.String(), nil)
	if err != nil {
		writeDeviceDataBridgeError(w, http.StatusServiceUnavailable, "device data is unavailable")
		return
	}
	request.Header.Set(DeviceDataTokenHeader, b.Token)
	if isAudio {
		request.Header.Set("Accept", "audio/*,video/webm;q=0.9,*/*;q=0.1")
		if value := r.Header.Get("Range"); value != "" {
			request.Header.Set("Range", value)
		}
	} else if isAsset {
		request.Header.Set("Accept", "application/pdf,image/*;q=0.9,*/*;q=0.1")
		if value := r.Header.Get("Range"); value != "" {
			request.Header.Set("Range", value)
		}
	} else {
		request.Header.Set("Accept", "application/json")
	}
	client := b.Client
	if isStream && b.Client.Timeout > 0 {
		copy := *b.Client
		copy.Timeout = 0
		client = &copy
	}
	response, err := client.Do(request)
	if err != nil {
		writeDeviceDataBridgeError(w, http.StatusServiceUnavailable, "device data is temporarily unavailable")
		return
	}
	defer response.Body.Close()
	if isStream {
		for _, header := range []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified"} {
			if value := response.Header.Get(header); value != "" {
				w.Header().Set(header, value)
			}
		}
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(response.StatusCode)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, response.Body)
		}
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDeviceDataResponseBytes+1))
	if err != nil {
		writeDeviceDataBridgeError(w, http.StatusBadGateway, "device data response is unavailable")
		return
	}
	if len(body) > maxDeviceDataResponseBytes {
		writeDeviceDataBridgeError(w, http.StatusBadGateway, "device data response is too large")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func deviceDataRoute(path string) (string, map[string]struct{}, bool) {
	switch {
	case path == DeviceDataPathPrefix+"/meetings/count":
		return "/meetings/count", nil, true
	case path == DeviceDataPathPrefix+"/obsidian/counts":
		return "/obsidian/counts", map[string]struct{}{"path": {}}, true
	case path == DeviceDataPathPrefix+"/meetings":
		return "/meetings", map[string]struct{}{"limit": {}, "offset": {}}, true
	case deviceDataMeetingAudioID(path) != "":
		return "/meetings/" + deviceDataMeetingAudioID(path) + "/audio", nil, true
	case strings.HasPrefix(path, DeviceDataPathPrefix+"/meetings/"):
		id := strings.TrimPrefix(path, DeviceDataPathPrefix+"/meetings/")
		if id == "" || strings.ContainsAny(id, "/\\") || strings.ContainsRune(id, '\x00') {
			return "", nil, false
		}
		return "/meetings/" + id, nil, true
	case path == DeviceDataPathPrefix+"/obsidian/list":
		return "/obsidian/list", map[string]struct{}{"path": {}, "limit": {}, "cursor": {}}, true
	case path == DeviceDataPathPrefix+"/obsidian/read":
		return "/obsidian/read", map[string]struct{}{"path": {}}, true
	case path == DeviceDataPathPrefix+"/obsidian/asset":
		return "/obsidian/asset", map[string]struct{}{"path": {}}, true
	default:
		return "", nil, false
	}
}

func deviceDataMeetingAudioID(path string) string {
	prefix := DeviceDataPathPrefix + "/meetings/"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, "/audio") {
		return ""
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/audio")
	if id == "" || strings.ContainsAny(id, "/\\\x00\r\n") || strings.Contains(id, "..") {
		return ""
	}
	return id
}

func validDeviceDataQuery(query url.Values, allowed map[string]struct{}) bool {
	for key, values := range query {
		if _, ok := allowed[key]; !ok || len(values) != 1 {
			return false
		}
	}
	return true
}

func writeDeviceDataBridgeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

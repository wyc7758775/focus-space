package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mediaRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn mediaRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

type mediaFailingReader struct {
	data []byte
	err  error
}

func (r *mediaFailingReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, r.err
}

type mediaSizedReader struct {
	remaining int64
}

func (r *mediaSizedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = 'x'
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestMediaProxyStreamsOnlyStoredPublicEntryFieldFlow(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-08T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,media_url,media_mime,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"media-entry", "media-external", "Podcast", "https://example.com/post", "Example", "github-blog", "app-fetch", "rss", "audio", "", now, "摘要", "text", "", "https://cdn.example.com/audio.mp3", "audio/mpeg", now); err != nil {
		t.Fatal(err)
	}
	previousFactory := mediaHTTPClientFactory
	defer func() { mediaHTTPClientFactory = previousFactory }()
	calls := 0
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.URL.String() != "https://cdn.example.com/audio.mp3" || req.Header.Get("Range") != "bytes=0-3" {
				t.Fatalf("unexpected upstream request: %s range=%q", req.URL, req.Header.Get("Range"))
			}
			if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Fatal("credential header leaked to media source")
			}
			header := make(http.Header)
			header.Set("Content-Type", "audio/mpeg")
			header.Set("Content-Range", "bytes 0-3/8")
			header.Set("Accept-Ranges", "bytes")
			return &http.Response{StatusCode: http.StatusPartialContent, Header: header, Body: io.NopCloser(strings.NewReader("DATA")), ContentLength: 4}, nil
		})}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/media?id=media-entry&kind=media", nil)
	req.Header.Set("Range", "bytes=0-3")
	req.Header.Set("Authorization", "Bearer "+"never-forward")
	req.Header.Set("Cookie", "session=never-forward")
	result := httptest.NewRecorder()
	mediaProxyHandler(result, req)
	if result.Code != http.StatusPartialContent || result.Body.String() != "DATA" || result.Header().Get("Content-Type") != "audio/mpeg" || result.Header().Get("Content-Range") != "bytes 0-3/8" {
		t.Fatalf("media response = %d headers=%v body=%q", result.Code, result.Header(), result.Body.String())
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d", calls)
	}

	if _, err := db.Exec(`UPDATE news SET media_url='http://127.0.0.1/private' WHERE id='media-entry'`); err != nil {
		t.Fatal(err)
	}
	unsafe := httptest.NewRecorder()
	mediaProxyHandler(unsafe, httptest.NewRequest(http.MethodGet, "/api/media?id=media-entry&kind=media", nil))
	if unsafe.Code != http.StatusUnprocessableEntity || calls != 1 {
		t.Fatalf("unsafe media status=%d calls=%d body=%s", unsafe.Code, calls, unsafe.Body.String())
	}
}

func TestGatewayMediaAttachmentStreamsFixedIndexRangeFlow(t *testing.T) {
	db = openTestDB(t)
	entryID := seedGatewayEntry(t, "hacker-news", "gateway-media-external", "sg_media_1", "https://example.com/post", "gateway-media-hash")
	if _, err := db.Exec(`UPDATE news SET media_url='/agent/v1/api/source-gateway/entries/sg_media_1/media/1',media_mime='video/mp4' WHERE id=?`, entryID); err != nil {
		t.Fatal(err)
	}
	mock := newGatewayMockServer(t)
	mock.mediaMIME = "video/mp4"
	mock.media["sg_media_1/1"] = []byte("DATAREST")
	mock.configure(t)
	previousFactory := mediaHTTPClientFactory
	publicFetches := 0
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
			publicFetches++
			return nil, errors.New("gateway media must not use public URL")
		})}
	}
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })

	request := httptest.NewRequest(http.MethodGet, "/api/media?id="+entryID+"&kind=media", nil)
	request.Header.Set("Range", "bytes=0-3")
	result := httptest.NewRecorder()
	mediaProxyHandler(result, request)
	if result.Code != http.StatusPartialContent || result.Body.String() != "DATA" || result.Header().Get("Content-Type") != "video/mp4" || result.Header().Get("Content-Range") != "bytes 0-3/8" || result.Header().Get("ETag") != `"gateway-media"` {
		t.Fatalf("gateway media status=%d headers=%v body=%s", result.Code, result.Header(), result.Body.String())
	}
	if publicFetches != 0 || mock.entryCallCount("GET /source-gateway/entries/sg_media_1/media/1") != 1 {
		t.Fatalf("public_fetches=%d gateway_requests=%v", publicFetches, mock.requests)
	}

	if _, err := db.Exec(`UPDATE news SET media_url='/agent/v1/api/source-gateway/entries/other/media/1' WHERE id=?`, entryID); err != nil {
		t.Fatal(err)
	}
	unsafe := httptest.NewRecorder()
	mediaProxyHandler(unsafe, httptest.NewRequest(http.MethodGet, "/api/media?id="+entryID+"&kind=media", nil))
	if unsafe.Code != http.StatusUnprocessableEntity || mock.entryCallCount("/media/") != 1 {
		t.Fatalf("mismatched gateway media status=%d requests=%v", unsafe.Code, mock.requests)
	}
}

func TestGatewayBilibiliThumbnailUsesRealProviderFallbackFlow(t *testing.T) {
	db = openTestDB(t)
	entryID := seedGatewayEntry(t, "bilibili-yingshijufeng", "bili:BV1abc123456", "sg_bili_1", "https://www.bilibili.com/video/BV1abc123456", "gateway-bili-hash")
	if _, err := db.Exec(`UPDATE news SET thumbnail_url='/agent/v1/api/source-gateway/entries/sg_bili_1/media/0',view_type='video' WHERE id=?`, entryID); err != nil {
		t.Fatal(err)
	}
	previousLookup := bilibiliThumbnailFetch
	previousMedia := mediaHTTPClientFactory
	t.Cleanup(func() {
		bilibiliThumbnailFetch = previousLookup
		mediaHTTPClientFactory = previousMedia
	})
	bilibiliThumbnailFetch = func(_ context.Context, raw string) (feedFetchResponse, error) {
		if raw != "https://api.bilibili.com/x/web-interface/view?bvid=BV1abc123456" {
			t.Fatalf("metadata URL = %q", raw)
		}
		return feedFetchResponse{Body: []byte(`{"code":0,"data":{"bvid":"BV1abc123456","pic":"http://i0.hdslb.com/bfs/archive/real-cover.jpg"}}`)}, nil
	}
	mediaHTTPClientFactory = func() *http.Client {
		return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://i0.hdslb.com/bfs/archive/real-cover.jpg" {
				t.Fatalf("cover URL = %q", req.URL.String())
			}
			header := make(http.Header)
			header.Set("Content-Type", "image/jpeg")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("JPEG")), ContentLength: 4}, nil
		})}
	}

	result := httptest.NewRecorder()
	mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id="+entryID+"&kind=thumbnail", nil))
	if result.Code != http.StatusOK || result.Body.String() != "JPEG" || result.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumbnail response = %d %v %q", result.Code, result.Header(), result.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT thumbnail_url FROM news WHERE id=?`, entryID).Scan(&stored); err != nil || stored != "https://i0.hdslb.com/bfs/archive/real-cover.jpg" {
		t.Fatalf("stored thumbnail = %q err=%v", stored, err)
	}
}

func TestMediaProxyRejectsMultiRangeAndUnexpectedType(t *testing.T) {
	for _, value := range []string{"bytes=0-10,20-30", "items=0-1", "bytes=bad", "bytes=-"} {
		if validMediaRangeHeader(value) {
			t.Fatalf("unsafe range accepted: %q", value)
		}
	}
	if !validMediaRangeHeader("bytes=0-") || !validMediaRangeHeader("bytes=-500") {
		t.Fatal("valid single range rejected")
	}
	if allowedMediaContentType("text/html", "media") || allowedMediaContentType("video/mp4", "thumbnail") || !allowedMediaContentType("image/jpeg", "thumbnail") {
		t.Fatal("media content-type allowlist is wrong")
	}
}

func TestMissingGatewayListThumbnailBecomesCacheableEmptyProjection(t *testing.T) {
	db = openTestDB(t)
	mock := newGatewayMockServer(t)
	mock.configure(t)
	entryID := seedGatewayEntry(t, "wechat-aiera", "missing-cover", "sg_missing_cover", "https://mp.weixin.qq.com/s/missing", "missing-cover-hash")

	result := httptest.NewRecorder()
	mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id="+entryID+"&kind=thumbnail", nil))
	if result.Code != http.StatusNoContent || result.Body.Len() != 0 {
		t.Fatalf("missing optional thumbnail = %d %q", result.Code, result.Body.String())
	}
	if result.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("missing thumbnail cache policy = %q", result.Header().Get("Cache-Control"))
	}
	var stored string
	if err := db.QueryRow(`SELECT thumbnail_url FROM news WHERE id=?`, entryID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("stale thumbnail projection was not cleared: %q", stored)
	}

	// Article-body image endpoints keep their explicit 404 contract; only the
	// optional list cover is normalized to an empty successful response.
	articleImage := httptest.NewRecorder()
	serveGatewayEntryImage(articleImage, httptest.NewRequest(http.MethodGet, "/image", nil), "sg_missing_cover", 0)
	if articleImage.Code != http.StatusNotFound {
		t.Fatalf("missing article image = %d, want 404", articleImage.Code)
	}
}

func seedMediaProxyNews(t *testing.T, id, mediaURL, mime string) {
	t.Helper()
	db = openTestDB(t)
	now := "2026-08-08T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,media_url,media_mime,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, id+"-external", "Media", "https://example.com/post", "Example", id, "app-fetch", "rss", "audio", "", now, "摘要", "text", "", mediaURL, mime, now); err != nil {
		t.Fatal(err)
	}
}

func TestMediaProxyHEADAndUpstreamStatusFlow(t *testing.T) {
	previousFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })

	t.Run("head does not copy the upstream body", func(t *testing.T) {
		seedMediaProxyNews(t, "head-entry", "https://cdn.example.com/audio.mp3", "audio/mpeg")
		upstreamMethod := ""
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				upstreamMethod = req.Method
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("should-not-be-copied")), ContentLength: 18}, nil
			})}
		}
		result := httptest.NewRecorder()
		mediaProxyHandler(result, httptest.NewRequest(http.MethodHead, "/api/media?id=head-entry&kind=media", nil))
		if result.Code != http.StatusOK || result.Body.Len() != 0 || upstreamMethod != http.MethodHead {
			t.Fatalf("HEAD response=%d body=%q upstream_method=%q", result.Code, result.Body.String(), upstreamMethod)
		}
		if result.Header().Get("Content-Type") != "audio/mpeg" || result.Header().Get("Content-Length") != "18" {
			t.Fatalf("HEAD headers=%v", result.Header())
		}
	})

	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			seedMediaProxyNews(t, "error-entry", "https://cdn.example.com/audio.mp3", "audio/mpeg")
			mediaHTTPClientFactory = func() *http.Client {
				return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("upstream error")), ContentLength: 13}, nil
				})}
			}
			result := httptest.NewRecorder()
			mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id=error-entry&kind=media", nil))
			if result.Code != http.StatusBadGateway {
				t.Fatalf("upstream %d mapped to %d, want 502", status, result.Code)
			}
		})
	}
}

func TestMediaProxySuffixRangeAndKnownOversizeFlow(t *testing.T) {
	previousFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })
	seedMediaProxyNews(t, "range-entry", "https://cdn.example.com/audio.mp3", "audio/mpeg")

	t.Run("suffix range is forwarded", func(t *testing.T) {
		seenRange := ""
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				seenRange = req.Header.Get("Range")
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				header.Set("Content-Range", "bytes 8-11/12")
				return &http.Response{StatusCode: http.StatusPartialContent, Header: header, Body: io.NopCloser(strings.NewReader("TAIL")), ContentLength: 4}, nil
			})}
		}
		req := httptest.NewRequest(http.MethodGet, "/api/media?id=range-entry&kind=media", nil)
		req.Header.Set("Range", "bytes=-4")
		result := httptest.NewRecorder()
		mediaProxyHandler(result, req)
		if result.Code != http.StatusPartialContent || result.Body.String() != "TAIL" || seenRange != "bytes=-4" {
			t.Fatalf("suffix range response=%d body=%q upstream_range=%q", result.Code, result.Body.String(), seenRange)
		}
	})

	t.Run("known oversized response is rejected before copying", func(t *testing.T) {
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("too large")), ContentLength: maxMediaResponseBytes + 1}, nil
			})}
		}
		result := httptest.NewRecorder()
		mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id=range-entry&kind=media", nil))
		if result.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized response status=%d, want 413", result.Code)
		}
	})
}

func TestPublicMediaRedirectRejectsPrivateDestination(t *testing.T) {
	client := publicMediaHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("media client has no redirect policy")
	}
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("private redirect destination was accepted")
	}
}

func TestMediaProxyUnknownLengthAndRangeValidationFlow(t *testing.T) {
	previousFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })
	seedMediaProxyNews(t, "unknown-length", "https://cdn.example.com/unknown.mp3", "audio/mpeg")

	t.Run("unknown length is staged and served completely", func(t *testing.T) {
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("chunked")), ContentLength: -1}, nil
			})}
		}
		result := httptest.NewRecorder()
		mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id=unknown-length", nil))
		if result.Code != http.StatusOK || result.Body.String() != "chunked" || result.Header().Get("Content-Length") != "7" {
			t.Fatalf("unknown-length response=%d headers=%v body=%q", result.Code, result.Header(), result.Body.String())
		}
	})

	t.Run("unknown length over cap is rejected", func(t *testing.T) {
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(&mediaSizedReader{remaining: maxMediaResponseBytes + 1}), ContentLength: -1}, nil
			})}
		}
		result := httptest.NewRecorder()
		mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id=unknown-length", nil))
		if result.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("unknown oversized response=%d, want 413", result.Code)
		}
	})

	t.Run("unknown length read failure is gateway error", func(t *testing.T) {
		mediaHTTPClientFactory = func() *http.Client {
			return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Content-Type", "audio/mpeg")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(&mediaFailingReader{data: []byte("partial"), err: errors.New("read failed")}), ContentLength: -1}, nil
			})}
		}
		result := httptest.NewRecorder()
		mediaProxyHandler(result, httptest.NewRequest(http.MethodGet, "/api/media?id=unknown-length", nil))
		if result.Code != http.StatusBadGateway {
			t.Fatalf("unknown read error response=%d, want 502", result.Code)
		}
	})

	for _, tc := range []struct {
		name          string
		rangeHeader   string
		status        int
		contentRange  string
		contentLength int64
		wantStatus    int
	}{
		{name: "missing content range", rangeHeader: "bytes=0-3", status: http.StatusPartialContent, contentLength: 4, wantStatus: http.StatusBadGateway},
		{name: "range ignored by upstream", rangeHeader: "bytes=0-3", status: http.StatusOK, contentLength: 4, wantStatus: http.StatusBadGateway},
		{name: "mismatched content range length", rangeHeader: "bytes=0-3", status: http.StatusPartialContent, contentRange: "bytes 0-3/8", contentLength: 3, wantStatus: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mediaHTTPClientFactory = func() *http.Client {
				return &http.Client{Transport: mediaRoundTripFunc(func(*http.Request) (*http.Response, error) {
					header := make(http.Header)
					header.Set("Content-Type", "audio/mpeg")
					if tc.contentRange != "" {
						header.Set("Content-Range", tc.contentRange)
					}
					return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader("DATA")), ContentLength: tc.contentLength}, nil
				})}
			}
			req := httptest.NewRequest(http.MethodGet, "/api/media?id=unknown-length", nil)
			req.Header.Set("Range", tc.rangeHeader)
			result := httptest.NewRecorder()
			mediaProxyHandler(result, req)
			if result.Code != tc.wantStatus {
				t.Fatalf("range validation response=%d, want %d", result.Code, tc.wantStatus)
			}
		})
	}
}

func TestMediaProxyResponseBoundaries(t *testing.T) {
	db = openTestDB(t)
	now := "2026-08-08T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,media_url,media_mime,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"media-boundary", "media-boundary", "Boundary", "https://example.com/post", "Example", "boundary", "app-fetch", "rss", "audio", "", now, "摘要", "text", "", "https://cdn.example.com/boundary.mp3", "audio/mpeg", now); err != nil {
		t.Fatal(err)
	}
	previousFactory := mediaHTTPClientFactory
	t.Cleanup(func() { mediaHTTPClientFactory = previousFactory })

	tests := []struct {
		name           string
		method         string
		upstreamStatus int
		contentLength  int64
		body           string
		rangeHeader    string
		wantStatus     int
		wantBody       string
		wantLength     string
	}{
		{
			name:           "head preserves metadata without body",
			method:         http.MethodHead,
			upstreamStatus: http.StatusOK,
			contentLength:  4,
			body:           "DATA",
			wantStatus:     http.StatusOK,
			wantBody:       "",
			wantLength:     "4",
		},
		{
			name:           "upstream not found is gateway failure",
			method:         http.MethodGet,
			upstreamStatus: http.StatusNotFound,
			contentLength:  12,
			body:           "upstream-body",
			wantStatus:     http.StatusBadGateway,
			wantBody:       "{\"error\":\"媒体来源暂时不可用\"}\n",
		},
		{
			name:           "upstream server error is gateway failure",
			method:         http.MethodGet,
			upstreamStatus: http.StatusInternalServerError,
			contentLength:  12,
			body:           "upstream-body",
			wantStatus:     http.StatusBadGateway,
			wantBody:       "{\"error\":\"媒体来源暂时不可用\"}\n",
		},
		{
			name:           "known response over cap is rejected",
			method:         http.MethodGet,
			upstreamStatus: http.StatusOK,
			contentLength:  maxMediaResponseBytes + 1,
			body:           "not streamed",
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantBody:       "{\"error\":\"媒体响应超过单次读取上限\"}\n",
		},
		{
			name:           "suffix range is forwarded",
			method:         http.MethodGet,
			upstreamStatus: http.StatusPartialContent,
			contentLength:  4,
			body:           "DATA",
			rangeHeader:    "bytes=-500",
			wantStatus:     http.StatusPartialContent,
			wantBody:       "DATA",
			wantLength:     "4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaHTTPClientFactory = func() *http.Client {
				return &http.Client{Transport: mediaRoundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
						t.Error("credential header leaked to media source")
					}
					if got := req.Header.Get("Range"); got != tt.rangeHeader {
						t.Errorf("upstream Range = %q, want %q", got, tt.rangeHeader)
					}
					header := make(http.Header)
					header.Set("Content-Type", "audio/mpeg")
					if tt.upstreamStatus == http.StatusPartialContent {
						header.Set("Content-Range", "bytes 0-3/8")
						header.Set("Accept-Ranges", "bytes")
					}
					return &http.Response{
						StatusCode:    tt.upstreamStatus,
						Header:        header,
						Body:          io.NopCloser(strings.NewReader(tt.body)),
						ContentLength: tt.contentLength,
					}, nil
				})}
			}
			req := httptest.NewRequest(tt.method, "/api/media?id=media-boundary", nil)
			if tt.rangeHeader != "" {
				req.Header.Set("Range", tt.rangeHeader)
			}
			req.Header.Set("Cookie", "session=should-not-forward")
			result := httptest.NewRecorder()
			mediaProxyHandler(result, req)
			if result.Code != tt.wantStatus {
				t.Fatalf("media response status = %d, want %d; body=%s", result.Code, tt.wantStatus, result.Body.String())
			}
			if tt.wantBody != "" && result.Body.String() != tt.wantBody {
				t.Fatalf("media response body = %q, want %q", result.Body.String(), tt.wantBody)
			}
			if tt.wantLength != "" && result.Header().Get("Content-Length") != tt.wantLength {
				t.Fatalf("media response Content-Length = %q, want %q", result.Header().Get("Content-Length"), tt.wantLength)
			}
			if tt.wantStatus == http.StatusBadGateway && strings.Contains(result.Body.String(), "upstream-body") {
				t.Fatal("upstream error body leaked")
			}
		})
	}
}

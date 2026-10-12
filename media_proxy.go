package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"focus-space/internal/app"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxMediaResponseBytes = int64(128 << 20)
	mediaRequestTimeout   = 5 * time.Minute
	mediaSlotWait         = 2 * time.Second
)

var (
	mediaRangeRE           = regexp.MustCompile(`^bytes=\d*-\d*$`)
	mediaContentRangeRE    = regexp.MustCompile(`^bytes [0-9]+-[0-9]+/(?:[0-9]+|\*)$`)
	mediaProxySlots        = make(chan struct{}, 2)
	mediaHTTPClientFactory = publicMediaHTTPClient
	bilibiliThumbnailFetch = fetchPublicURL
)

var (
	errMediaBodyTooLarge = errors.New("媒体响应超过单次读取上限")
	errMediaBodyRead     = errors.New("媒体响应读取失败")
	errMediaContentRange = errors.New("媒体来源返回了不合法的范围")
	errMediaBodyLength   = errors.New("媒体来源响应长度不一致")
)

type mediaRangeSpec struct {
	start  *int64
	end    *int64
	suffix *int64
}

type mediaContentRange struct {
	start int64
	end   int64
	total *int64
}

// mediaProxyHandler keeps public feed media usable without granting the page
// arbitrary network access. The caller selects only an existing entry id and
// a fixed field; the stored URL is revalidated and resolved through the same
// SSRF-safe dialer as feed sync.
func mediaProxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "id" && key != "kind") || len(values) != 1 {
			app.UserErr(w, http.StatusBadRequest, "媒体筛选条件不合法")
			return
		}
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind == "" {
		kind = "media"
	}
	if !validText(id, 128, true) || strings.ContainsAny(id, "/\\\x00\r\n") || kind != "media" && kind != "thumbnail" {
		app.UserErr(w, http.StatusBadRequest, "媒体标识不合法")
		return
	}
	rangeHeader := strings.TrimSpace(r.Header.Get("Range"))
	if rangeHeader != "" && !validMediaRangeHeader(rangeHeader) {
		app.UserErr(w, http.StatusRequestedRangeNotSatisfiable, "仅支持单段媒体范围请求")
		return
	}
	rangeSpec, rangeErr := parseMediaRangeHeader(rangeHeader)
	if rangeErr != nil {
		app.UserErr(w, http.StatusRequestedRangeNotSatisfiable, "仅支持单段媒体范围请求")
		return
	}

	var mediaURL, declaredMIME, gatewayEntryID, collectionMode, entryURL, sourceID string
	query := `SELECT n.media_url,n.media_mime,n.gateway_entry_id,COALESCE(f.collection_mode,''),n.url,n.source_id
FROM news n LEFT JOIN feed_sources f ON f.id=n.source_id WHERE n.id=? AND n.deleted_at=''`
	if kind == "thumbnail" {
		query = `SELECT n.thumbnail_url,'',n.gateway_entry_id,COALESCE(f.collection_mode,''),n.url,n.source_id
FROM news n LEFT JOIN feed_sources f ON f.id=n.source_id WHERE n.id=? AND n.deleted_at=''`
	}
	if err := db.QueryRow(query, id).Scan(&mediaURL, &declaredMIME, &gatewayEntryID, &collectionMode, &entryURL, &sourceID); errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "媒体不存在")
		return
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if isGatewayManagedEntry(gatewayEntryID, collectionMode) {
		if gatewayEntryID == "" {
			app.UserErr(w, http.StatusBadGateway, "云端媒体暂时读取失败")
			return
		}
		if kind == "thumbnail" {
			if imageURL, sourceURL, ok := verifiedWeChatCachedImage(id, 0); ok {
				servePersistedEntryImage(w, r, imageURL, sourceURL)
				return
			}
			// Old local-server releases do not expose the app-scoped Source
			// Gateway media route. Bilibili video identity is still fixed by the
			// stored public URL, so resolve its real cover through the anonymous
			// provider metadata API and persist the validated CDN URL locally.
			// This is a real-data fallback, never a generated placeholder.
			if strings.HasPrefix(sourceID, "bilibili-") {
				if direct := bilibiliImageURL(mediaURL); direct != "" {
					mediaURL = direct
				} else if resolved, resolveErr := resolveBilibiliThumbnail(r.Context(), entryURL); resolveErr == nil {
					mediaURL = resolved
					_, _ = db.Exec(`UPDATE news SET thumbnail_url=? WHERE id=? AND deleted_at=''`, resolved, id)
				} else {
					serveGatewayListThumbnail(w, r, id, gatewayEntryID)
					return
				}
			} else {
				// 网关条目的缩略图只走 local-server media 路由：条目的首图固定映射到
				// 来源记录的图片索引 0，不直接加载任何第三方图片 URL。
				serveGatewayListThumbnail(w, r, id, gatewayEntryID)
				return
			}
		}
		if kind == "media" {
			index, ok := gatewayMediaIndex(mediaURL, gatewayEntryID)
			if !ok {
				app.UserErr(w, http.StatusUnprocessableEntity, "该云端条目没有可安全读取的媒体附件")
				return
			}
			serveGatewayEntryMedia(w, r, gatewayEntryID, index, declaredMIME, rangeHeader, rangeSpec)
			return
		}
	}
	if !validPublicFeedURL(mediaURL) {
		app.UserErr(w, http.StatusUnprocessableEntity, "该媒体不是可安全读取的公开来源")
		return
	}
	timer := time.NewTimer(mediaSlotWait)
	defer timer.Stop()
	select {
	case mediaProxySlots <- struct{}{}:
		defer func() { <-mediaProxySlots }()
	case <-r.Context().Done():
		return
	case <-timer.C:
		app.UserErr(w, http.StatusServiceUnavailable, "媒体读取繁忙，请稍后重试")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), mediaRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, mediaURL, nil)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, "媒体地址不合法")
		return
	}
	req.Header.Set("User-Agent", feedUserAgent)
	if kind == "thumbnail" {
		req.Header.Set("Accept", "image/*")
	} else {
		req.Header.Set("Accept", "audio/*, video/*, application/octet-stream;q=0.5")
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	response, err := mediaHTTPClientFactory().Do(req)
	if trustedXVideoMediaURL(mediaURL) && mediaResponseFailed(response, err) {
		if proxyClient := configuredXMediaProxyClient(); proxyClient != nil {
			defer proxyClient.CloseIdleConnections()
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			retryReq := req.Clone(ctx)
			retryReq.Header = req.Header.Clone()
			response, err = proxyClient.Do(retryReq)
		}
	}
	if err != nil {
		app.UserErr(w, http.StatusBadGateway, "媒体暂时读取失败")
		return
	}
	if response.Body == nil {
		app.UserErr(w, http.StatusBadGateway, "媒体来源响应不完整")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		app.UserErr(w, http.StatusBadGateway, "媒体来源暂时不可用")
		return
	}
	if response.ContentLength > maxMediaResponseBytes {
		app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
		return
	}
	contentRange, err := validateMediaContentRange(response, rangeSpec)
	if err != nil {
		if errors.Is(err, errMediaBodyTooLarge) {
			app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
			return
		}
		app.UserErr(w, http.StatusBadGateway, errMediaContentRange.Error())
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if contentType == "" {
		contentType = strings.ToLower(strings.TrimSpace(declaredMIME))
	}
	if !allowedMediaContentType(contentType, kind) {
		app.UserErr(w, http.StatusUnsupportedMediaType, "来源返回的媒体类型不受支持")
		return
	}
	if r.Method == http.MethodHead {
		headLength := response.ContentLength
		if headLength < 0 && contentRange != nil {
			headLength = contentRange.end - contentRange.start + 1
		}
		writeMediaHeaders(w, response, contentType, contentRange, headLength)
		w.WriteHeader(response.StatusCode)
		return
	}

	// A chunked/streaming upstream response has no trustworthy length. Stage it
	// in a 0600 temporary file before sending our headers so an over-cap or read
	// failure becomes an explicit gateway error instead of a silent truncation.
	var staged *os.File
	bodyLength := response.ContentLength
	if response.ContentLength < 0 {
		staged, bodyLength, err = stageMediaBody(response.Body)
		if err != nil {
			if errors.Is(err, errMediaBodyTooLarge) {
				app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
			} else {
				app.UserErr(w, http.StatusBadGateway, "媒体来源响应读取失败")
			}
			return
		}
		defer func() {
			name := staged.Name()
			_ = staged.Close()
			_ = os.Remove(name)
		}()
	}
	if contentRange != nil && bodyLength != contentRange.end-contentRange.start+1 {
		app.UserErr(w, http.StatusBadGateway, errMediaBodyLength.Error())
		return
	}
	if bodyLength < 0 || bodyLength > maxMediaResponseBytes {
		app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
		return
	}
	writeMediaHeaders(w, response, contentType, contentRange, bodyLength)
	w.WriteHeader(response.StatusCode)
	var copyErr error
	if staged != nil {
		_, copyErr = io.Copy(w, staged)
	} else {
		_, copyErr = io.CopyN(w, response.Body, bodyLength)
		if errors.Is(copyErr, io.EOF) {
			copyErr = io.ErrUnexpectedEOF
		}
	}
	if copyErr != nil {
		// Headers are already committed; closing the response stream and logging
		// the write/read error prevents a false success in server diagnostics.
		log.Printf("媒体响应传输失败: %v", copyErr)
	}
}

func gatewayMediaIndex(raw, gatewayEntryID string) (int, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return 0, false
	}
	marker := "/source-gateway/entries/"
	markerIndex := strings.Index(parsed.EscapedPath(), marker)
	if markerIndex < 0 {
		return 0, false
	}
	tail := strings.TrimPrefix(parsed.EscapedPath()[markerIndex:], marker)
	parts := strings.Split(tail, "/")
	if len(parts) != 3 || parts[1] != "media" {
		return 0, false
	}
	entryID, err := url.PathUnescape(parts[0])
	if err != nil || entryID != gatewayEntryID {
		return 0, false
	}
	index, err := strconv.Atoi(parts[2])
	return index, err == nil && index >= 0 && index < maxEntryArticleImages
}

func serveGatewayEntryMedia(w http.ResponseWriter, r *http.Request, gatewayEntryID string, index int, declaredMIME, rangeHeader string, rangeSpec mediaRangeSpec) {
	bridge := newSourceGatewayBridgeFromEnv()
	media, err := bridge.mediaWithRange(r.Context(), gatewayEntryID, index, r.Method, "media", rangeHeader)
	if err != nil {
		if errors.Is(err, errSourceGatewayNotFound) {
			app.UserErr(w, http.StatusNotFound, "媒体不存在")
			return
		}
		app.UserErr(w, http.StatusBadGateway, "云端媒体暂时读取失败")
		return
	}
	defer media.Body.Close()
	if declared := strings.ToLower(strings.TrimSpace(strings.Split(declaredMIME, ";")[0])); declared != "" && declared != media.ContentType {
		app.UserErr(w, http.StatusUnsupportedMediaType, "云端媒体类型与条目记录不一致")
		return
	}
	header := make(http.Header)
	header.Set("Content-Type", media.ContentType)
	if media.ContentRange != "" {
		header.Set("Content-Range", media.ContentRange)
	}
	if media.AcceptRanges != "" {
		header.Set("Accept-Ranges", media.AcceptRanges)
	}
	if media.ETag != "" {
		header.Set("ETag", media.ETag)
	}
	if media.LastModified != "" {
		header.Set("Last-Modified", media.LastModified)
	}
	response := &http.Response{StatusCode: media.StatusCode, Header: header, ContentLength: media.Length}
	contentRange, err := validateMediaContentRange(response, rangeSpec)
	if err != nil {
		if errors.Is(err, errMediaBodyTooLarge) {
			app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
			return
		}
		app.UserErr(w, http.StatusBadGateway, errMediaContentRange.Error())
		return
	}
	if media.Length > maxMediaResponseBytes {
		app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
		return
	}
	if r.Method == http.MethodHead {
		writeMediaHeaders(w, response, media.ContentType, contentRange, media.Length)
		w.WriteHeader(media.StatusCode)
		return
	}
	body := io.Reader(media.Body)
	bodyLength := media.Length
	var staged *os.File
	if bodyLength < 0 {
		staged, bodyLength, err = stageMediaBody(media.Body)
		if err != nil {
			if errors.Is(err, errMediaBodyTooLarge) {
				app.UserErr(w, http.StatusRequestEntityTooLarge, errMediaBodyTooLarge.Error())
			} else {
				app.UserErr(w, http.StatusBadGateway, errMediaBodyRead.Error())
			}
			return
		}
		defer func() {
			name := staged.Name()
			_ = staged.Close()
			_ = os.Remove(name)
		}()
		body = staged
	}
	if contentRange != nil && bodyLength != contentRange.end-contentRange.start+1 {
		app.UserErr(w, http.StatusBadGateway, errMediaBodyLength.Error())
		return
	}
	writeMediaHeaders(w, response, media.ContentType, contentRange, bodyLength)
	w.WriteHeader(media.StatusCode)
	if _, err := io.CopyN(w, body, bodyLength); err != nil && !errors.Is(err, io.EOF) {
		log.Printf("云端媒体响应传输失败: %v", err)
	}
}

// serveGatewayListThumbnail keeps an optional missing cover out of the error
// channel. Source Gateway can legitimately return an article without image
// index 0 even when an older projection still contains a thumbnail path. Clear
// that stale projection and return a cacheable empty image response so the
// reader removes the cover without a broken-image flash or retry storm.
func serveGatewayListThumbnail(w http.ResponseWriter, r *http.Request, entryID, gatewayEntryID string) {
	err := streamGatewayEntryImage(w, r, gatewayEntryID, 0)
	if err == nil {
		return
	}
	if errors.Is(err, errSourceGatewayNotFound) {
		_, _ = db.Exec(`UPDATE news SET thumbnail_url='' WHERE id=? AND deleted_at=''`, entryID)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	app.UserErr(w, http.StatusBadGateway, "云端图片暂时读取失败")
}

func resolveBilibiliThumbnail(ctx context.Context, entryURL string) (string, error) {
	bvid, _, ok := validBilibiliVideoURL(entryURL)
	if !ok {
		return "", errors.New("Bilibili 视频地址无效")
	}
	metadataURL := "https://api.bilibili.com/x/web-interface/view?bvid=" + url.QueryEscape(bvid)
	response, err := bilibiliThumbnailFetch(ctx, metadataURL)
	if err != nil {
		return "", err
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			BVID string `json:"bvid"`
			Pic  string `json:"pic"`
		} `json:"data"`
	}
	if json.Unmarshal(response.Body, &payload) != nil || payload.Code != 0 || payload.Data.BVID != bvid {
		return "", errors.New("Bilibili 视频元数据无效")
	}
	thumbnail := bilibiliImageURL(payload.Data.Pic)
	if thumbnail == "" {
		return "", errors.New("Bilibili 视频封面无效")
	}
	return thumbnail, nil
}

func writeMediaHeaders(w http.ResponseWriter, response *http.Response, contentType string, contentRange *mediaContentRange, bodyLength int64) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Entry media is addressed by a stable local entry id. Cache successful
	// thumbnails/covers in the browser so reopening the reader does not refetch
	// the same public image on every render; failed responses never receive this
	// header because they return before this function.
	w.Header().Set("Cache-Control", "public, max-age=3600, stale-while-revalidate=86400")
	if value := response.Header.Get("Accept-Ranges"); value != "" {
		w.Header().Set("Accept-Ranges", value)
	}
	if contentRange != nil {
		w.Header().Set("Content-Range", strings.TrimSpace(response.Header.Get("Content-Range")))
	}
	if value := strings.TrimSpace(response.Header.Get("ETag")); value != "" {
		w.Header().Set("ETag", value)
	}
	if value := strings.TrimSpace(response.Header.Get("Last-Modified")); value != "" {
		w.Header().Set("Last-Modified", value)
	}
	if bodyLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(bodyLength, 10))
	}
}

func stageMediaBody(body io.Reader) (*os.File, int64, error) {
	file, err := os.CreateTemp("", "plaud-media-proxy-")
	if err != nil {
		return nil, 0, errors.Join(errMediaBodyRead, err)
	}
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}
	n, copyErr := io.Copy(file, io.LimitReader(body, maxMediaResponseBytes+1))
	if n > maxMediaResponseBytes {
		cleanup()
		return nil, n, errMediaBodyTooLarge
	}
	if copyErr != nil {
		cleanup()
		return nil, n, errors.Join(errMediaBodyRead, copyErr)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, n, errors.Join(errMediaBodyRead, err)
	}
	return file, n, nil
}

func publicMediaHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 nil,
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		DialContext:           publicDialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= feedFetchRedirectLimit {
				return errors.New("公开媒体重定向次数过多")
			}
			if !validPublicFeedURL(req.URL.String()) {
				return errors.New("公开媒体重定向到不安全地址")
			}
			return nil
		},
	}
}

func mediaResponseFailed(response *http.Response, err error) bool {
	return err != nil || response == nil || response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent
}

func trustedXVideoMediaURL(raw string) bool {
	_, ok := validFoloMediaURL(raw, "video.twimg.com")
	return ok
}

func configuredXMediaProxyClient() *http.Client {
	proxyURL := configuredHTTPSProxyURL()
	if proxyURL == nil {
		return nil
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyURL(proxyURL),
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= feedFetchRedirectLimit {
				return errors.New("X 媒体重定向次数过多")
			}
			if !trustedXVideoMediaURL(req.URL.String()) {
				return errors.New("X 媒体重定向到不受信任地址")
			}
			return nil
		},
	}
}

func configuredHTTPSProxyURL() *url.URL {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		raw := strings.TrimSpace(os.Getenv(key))
		if raw == "" {
			continue
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" {
			continue
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			return parsed
		}
	}
	return nil
}

func allowedMediaContentType(value, kind string) bool {
	if kind == "thumbnail" {
		return strings.HasPrefix(value, "image/")
	}
	return strings.HasPrefix(value, "audio/") || strings.HasPrefix(value, "video/") || value == "application/ogg" || value == "application/octet-stream"
}

func validMediaRangeHeader(value string) bool {
	if !mediaRangeRE.MatchString(value) {
		return false
	}
	parts := strings.TrimPrefix(value, "bytes=")
	left, right, ok := strings.Cut(parts, "-")
	return ok && (left != "" || right != "")
}

func parseMediaRangeHeader(value string) (mediaRangeSpec, error) {
	var spec mediaRangeSpec
	if value == "" {
		return spec, nil
	}
	if !validMediaRangeHeader(value) {
		return spec, errors.New("invalid media range")
	}
	left, right, _ := strings.Cut(strings.TrimPrefix(value, "bytes="), "-")
	parse := func(raw string) (*int64, error) {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return nil, errors.New("invalid media range number")
		}
		return &value, nil
	}
	if left == "" {
		suffix, err := parse(right)
		if err != nil || suffix == nil || *suffix == 0 {
			return spec, errors.New("invalid media suffix range")
		}
		spec.suffix = suffix
		return spec, nil
	}
	start, err := parse(left)
	if err != nil {
		return spec, err
	}
	spec.start = start
	if right != "" {
		end, err := parse(right)
		if err != nil || end == nil || *end < *start {
			return spec, errors.New("invalid media range bounds")
		}
		spec.end = end
	}
	return spec, nil
}

func validateMediaContentRange(response *http.Response, requested mediaRangeSpec) (*mediaContentRange, error) {
	raw := strings.TrimSpace(response.Header.Get("Content-Range"))
	if response.StatusCode == http.StatusOK {
		if raw != "" || requested.start != nil || requested.suffix != nil {
			return nil, errMediaContentRange
		}
		return nil, nil
	}
	if response.StatusCode != http.StatusPartialContent || !mediaContentRangeRE.MatchString(raw) || requested.start == nil && requested.suffix == nil {
		return nil, errMediaContentRange
	}
	value := strings.TrimPrefix(raw, "bytes ")
	bounds, totalRaw, ok := strings.Cut(value, "/")
	if !ok {
		return nil, errMediaContentRange
	}
	startRaw, endRaw, ok := strings.Cut(bounds, "-")
	if !ok {
		return nil, errMediaContentRange
	}
	start, errStart := strconv.ParseInt(startRaw, 10, 64)
	end, errEnd := strconv.ParseInt(endRaw, 10, 64)
	if errStart != nil || errEnd != nil || start < 0 || end < start {
		return nil, errMediaContentRange
	}
	var total *int64
	if totalRaw != "*" {
		parsed, err := strconv.ParseInt(totalRaw, 10, 64)
		if err != nil || parsed <= end {
			return nil, errMediaContentRange
		}
		total = &parsed
	}
	length := end - start + 1
	if length <= 0 || length > maxMediaResponseBytes {
		return nil, errMediaBodyTooLarge
	}
	if response.ContentLength >= 0 && response.ContentLength != length {
		return nil, errMediaBodyLength
	}
	if requested.start != nil {
		if start != *requested.start || requested.end != nil && end > *requested.end {
			return nil, errMediaContentRange
		}
	} else if requested.suffix != nil && length > *requested.suffix {
		return nil, errMediaContentRange
	}
	return &mediaContentRange{start: start, end: end, total: total}, nil
}

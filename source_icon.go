package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxSourceIconBytes         = int64(256 << 10)
	maxSourceIconHTMLBytes     = int64(256 << 10)
	maxSourceIconRows          = 200
	sourceIconFetchLimit       = 4
	sourceIconFetchWait        = 1500 * time.Millisecond
	sourceIconTimeout          = 25 * time.Second
	sourceIconCandidateTimeout = 8 * time.Second
	sourceIconDirectTimeout    = 3 * time.Second
	sourceIconFreshFor         = 7 * 24 * time.Hour
	sourceIconRetryAfter       = 6 * time.Hour
	sourceIconEmptyRetryAfter  = 5 * time.Minute
	sourceIconProxyHost        = "img.folo.is"
)

var (
	sourceIconSlots             = make(chan struct{}, sourceIconFetchLimit)
	defaultSourceIconHTTPClient = func() *http.Client {
		client := publicFeedHTTPClient()
		client.Timeout = sourceIconTimeout
		return client
	}()
	sourceIconHTTPClientFactory = func() *http.Client { return defaultSourceIconHTTPClient }
	errSourceIconUnavailable    = errors.New("来源图标暂不可用")
)

type sourceIconCacheEntry struct {
	SourceID    string
	IconURL     string
	ContentType string
	Body        []byte
	FetchedAt   string
	RetryAfter  string
	LastError   string
}

type sourceIconDescriptor struct {
	Candidates         []string
	SiteURLs           []string
	FallbackCandidates []string
	GatewayManaged     bool
}

const youtubeProviderIconURL = "https://icons.folo.is/youtube.com"

// The browser always reads through the app-owned icon endpoint. For the fixed
// video catalog, resolve and cache the actual creator portrait using only the
// pinned provider identifier; arbitrary sources cannot use this image proxy.
func creatorAvatarCandidateURL(sourceID string) string {
	spec, ok := operationalSourceSpecs[sourceID]
	if !ok {
		return ""
	}
	switch spec.parser {
	case "bilibili":
		if spec.bilibiliMID > 0 {
			return "https://unavatar.io/bilibili/" + strconv.FormatInt(spec.bilibiliMID, 10)
		}
	case "youtube":
		if youtubeChannelIDRE.MatchString(spec.youtubeChannelID) {
			return "https://unavatar.io/youtube/" + spec.youtubeChannelID
		}
	case "folo":
		if handle := strings.TrimSpace(spec.foloHandle); operationalXHandleRE.MatchString(handle) {
			return "https://unavatar.io/twitter/" + handle
		}
	}
	return ""
}

func preferredSourceIconURL(source FeedSource, feed fetchedFeed) string {
	candidates := []string{
		feed.IconURL,
		source.IconURL,
		sourceFaviconURL(firstNonEmpty(feed.SiteURL, source.SiteURL, source.URL)),
	}
	for _, candidate := range candidates {
		if normalized, err := normalizePublicURL(candidate); err == nil {
			return normalized
		}
	}
	return ""
}

func sourceIconURLAllowed(sourceID, raw string) bool {
	// The retired default-source collectors used to pin per-platform avatar
	// hosts here.  Gateway-managed sources carry no fetchable URL, so the
	// generic public-URL rule now covers every source uniformly.
	_ = sourceID
	return validPublicFeedURL(raw)
}

func sourceFaviconURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = "/favicon.ico"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if !validPublicFeedURL(parsed.String()) {
		return ""
	}
	return parsed.String()
}

func sourceOriginURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = "/"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	if !validPublicFeedURL(parsed.String()) {
		return ""
	}
	return parsed.String()
}

func persistSourceIconCandidate(sourceID, iconURL string) error {
	if !validText(sourceID, 128, true) || iconURL == "" || !sourceIconURLAllowed(sourceID, iconURL) {
		return nil
	}
	_, err := db.Exec(`INSERT INTO feed_catalog_entries(source_id,icon_url)
VALUES(?,?) ON CONFLICT(source_id) DO UPDATE SET
icon_url=CASE WHEN excluded.icon_url <> '' THEN excluded.icon_url ELSE feed_catalog_entries.icon_url END`, sourceID, iconURL)
	return err
}

func sourceIconHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	for key, values := range r.URL.Query() {
		if key == "id" && len(values) == 1 {
			continue
		}
		// Allow only the bounded numeric cache-buster used by the page after an
		// image failure. Arbitrary filters remain rejected.
		if key == "retry" && len(values) == 1 && len(values[0]) <= 13 && allASCIIDigits(values[0]) {
			continue
		}
		// Bump this fixed client revision when the resolver/fallback policy
		// changes. It invalidates an older browser-cached provider logo without
		// allowing arbitrary query parameters through the icon endpoint.
		if key == "v" && len(values) == 1 && (values[0] == "3" || values[0] == "4") {
			continue
		}
		app.UserErr(w, http.StatusBadRequest, "来源图标筛选条件不合法")
		return
	}
	sourceID := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validText(sourceID, 128, true) || strings.ContainsAny(sourceID, "/\\\x00\r\n") {
		app.UserErr(w, http.StatusBadRequest, "来源图标标识不合法")
		return
	}
	// The client makes exactly one bounded retry after an image failure. For a
	// pinned video creator, let that explicit retry re-check the portrait now
	// instead of inheriting an earlier empty-cache cooldown from app startup.
	if r.URL.Query().Get("retry") != "" && creatorAvatarCandidateURL(sourceID) != "" {
		_, _ = db.Exec(`UPDATE source_icon_cache SET retry_after='' WHERE source_id=? AND length(body)=0`, sourceID)
	}

	entry, err := resolveSourceIcon(r.Context(), sourceID)
	if errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "来源不存在")
		return
	}
	if errors.Is(err, errSourceIconUnavailable) || err == nil && len(entry.Body) == 0 {
		app.UserErr(w, http.StatusNotFound, "来源图标暂不可用")
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	writeSourceIcon(w, r, entry)
}

func resolveSourceIcon(ctx context.Context, sourceID string) (sourceIconCacheEntry, error) {
	descriptor, err := sourceIconDescriptorByID(sourceID)
	if err != nil {
		return sourceIconCacheEntry{}, err
	}
	candidates := descriptor.Candidates
	fallbackCandidates := descriptor.FallbackCandidates
	cache, cacheErr := loadSourceIconCache(sourceID)
	if cacheErr != nil && !errors.Is(cacheErr, sql.ErrNoRows) {
		return sourceIconCacheEntry{}, cacheErr
	}
	// Empty icon URLs are failure-cache records. Keep them so their retry
	// cooldown can suppress repeated metadata/network requests.
	if cacheErr == nil && len(cache.Body) > 0 && cache.IconURL != "" && !sourceIconURLAllowed(sourceID, cache.IconURL) {
		_, _ = db.Exec(`DELETE FROM source_icon_cache WHERE source_id=?`, sourceID)
		cache = sourceIconCacheEntry{}
	}
	// Gateway-managed sources may fetch only the exact catalog icon URL. They
	// never perform website discovery or derive a third-party favicon from a
	// source URL, which keeps the egress boundary fixed and auditable.
	if descriptor.GatewayManaged {
		if len(cache.Body) > 0 && (len(candidates) == 0 || cache.IconURL == candidates[0]) {
			return cache, nil
		}
		if len(candidates) == 0 {
			return sourceIconCacheEntry{}, fmt.Errorf("%w: 云端来源没有图标地址", errSourceIconUnavailable)
		}
	}
	now := time.Now().UTC()
	if len(candidates) == 0 && len(descriptor.SiteURLs) == 0 && len(fallbackCandidates) == 0 {
		if len(cache.Body) > 0 {
			return cache, nil
		}
		if sourceIconRetryMatches(cache, candidates) && retryPending(cache.RetryAfter, now) {
			return sourceIconCacheEntry{}, fmt.Errorf("%w: 等待重试", errSourceIconUnavailable)
		}
		return sourceIconCacheEntry{}, fmt.Errorf("%w: 来源没有可用图标地址", errSourceIconUnavailable)
	}
	if len(candidates) > 0 && cache.IconURL == candidates[0] && len(cache.Body) > 0 && timeWithin(cache.FetchedAt, now, sourceIconFreshFor) {
		return cache, nil
	}
	if sourceIconRetryMatches(cache, candidates) && retryPending(cache.RetryAfter, now) {
		if len(cache.Body) > 0 {
			return cache, nil
		}
		return sourceIconCacheEntry{}, fmt.Errorf("%w: 等待重试", errSourceIconUnavailable)
	}

	timer := time.NewTimer(sourceIconFetchWait)
	defer timer.Stop()
	select {
	case sourceIconSlots <- struct{}{}:
		defer func() { <-sourceIconSlots }()
	case <-ctx.Done():
		return sourceIconCacheEntry{}, ctx.Err()
	case <-timer.C:
		if len(cache.Body) > 0 {
			return cache, nil
		}
		return sourceIconCacheEntry{}, fmt.Errorf("%w: 读取繁忙", errSourceIconUnavailable)
	}

	// Another request may have filled the cache while this request waited.
	if latest, latestErr := loadSourceIconCache(sourceID); latestErr == nil {
		cache = latest
		if len(candidates) > 0 && latest.IconURL == candidates[0] && len(latest.Body) > 0 && timeWithin(latest.FetchedAt, now, sourceIconFreshFor) {
			return latest, nil
		}
		if sourceIconRetryMatches(latest, candidates) && retryPending(latest.RetryAfter, now) {
			if len(latest.Body) > 0 {
				return latest, nil
			}
			return sourceIconCacheEntry{}, fmt.Errorf("%w: 等待重试", errSourceIconUnavailable)
		}
	}

	requestCtx, cancelRequest := context.WithTimeout(ctx, sourceIconTimeout)
	defer cancelRequest()
	var lastErr error
	tried := make(map[string]struct{}, len(candidates)+1)
	for index, iconURL := range candidates {
		tried[iconURL] = struct{}{}
		if cache.IconURL == iconURL && len(cache.Body) > 0 && timeWithin(cache.FetchedAt, now, sourceIconFreshFor) {
			if index > 0 {
				_ = persistSourceIconCandidate(sourceID, iconURL)
			}
			return cache, nil
		}
		fetchCtx, cancelFetch := context.WithTimeout(requestCtx, sourceIconCandidateTimeout)
		body, contentType, fetchErr := fetchSourceIconCandidate(fetchCtx, iconURL)
		cancelFetch()
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		entry := sourceIconCacheEntry{
			SourceID: sourceID, IconURL: iconURL, ContentType: contentType, Body: body,
			FetchedAt: now.Format(time.RFC3339),
		}
		if err := storeSourceIcon(entry); err != nil {
			return sourceIconCacheEntry{}, err
		}
		if index > 0 {
			_ = persistSourceIconCandidate(sourceID, iconURL)
		}
		return entry, nil
	}
	// Bilibili exposes the current UP portrait in the pinned account feed. If
	// the deterministic portrait resolver is temporarily unavailable, use that
	// verified feed metadata before giving up or showing a placeholder.
	if discoveredURL, discoveryErr := discoverBilibiliCreatorAvatarURL(requestCtx, sourceID); discoveryErr == nil {
		if _, alreadyTried := tried[discoveredURL]; !alreadyTried {
			tried[discoveredURL] = struct{}{}
			fetchCtx, cancelFetch := context.WithTimeout(requestCtx, sourceIconCandidateTimeout)
			body, contentType, fetchErr := fetchSourceIconCandidate(fetchCtx, discoveredURL)
			cancelFetch()
			if fetchErr == nil {
				entry := sourceIconCacheEntry{
					SourceID: sourceID, IconURL: discoveredURL, ContentType: contentType, Body: body,
					FetchedAt: now.Format(time.RFC3339),
				}
				if err := storeSourceIcon(entry); err != nil {
					return sourceIconCacheEntry{}, err
				}
				_ = persistSourceIconCandidate(sourceID, discoveredURL)
				return entry, nil
			}
			lastErr = fetchErr
		}
	} else if creatorAvatarCandidateURL(sourceID) != "" {
		lastErr = discoveryErr
	}
	for _, siteURL := range descriptor.SiteURLs {
		if requestCtx.Err() != nil {
			break
		}
		discoveryCtx, cancelDiscovery := context.WithTimeout(requestCtx, sourceIconCandidateTimeout)
		discoveredURL, discoveryErr := discoverWebsiteIconURL(discoveryCtx, siteURL)
		cancelDiscovery()
		if discoveryErr != nil {
			lastErr = discoveryErr
			continue
		}
		if _, alreadyTried := tried[discoveredURL]; alreadyTried {
			continue
		}
		tried[discoveredURL] = struct{}{}
		if cache.IconURL == discoveredURL && len(cache.Body) > 0 && timeWithin(cache.FetchedAt, now, sourceIconFreshFor) {
			_ = persistSourceIconCandidate(sourceID, discoveredURL)
			return cache, nil
		}
		fetchCtx, cancelFetch := context.WithTimeout(requestCtx, sourceIconCandidateTimeout)
		body, contentType, fetchErr := fetchSourceIconCandidate(fetchCtx, discoveredURL)
		cancelFetch()
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		entry := sourceIconCacheEntry{
			SourceID: sourceID, IconURL: discoveredURL, ContentType: contentType, Body: body,
			FetchedAt: now.Format(time.RFC3339),
		}
		if err := storeSourceIcon(entry); err != nil {
			return sourceIconCacheEntry{}, err
		}
		_ = persistSourceIconCandidate(sourceID, discoveredURL)
		return entry, nil
	}
	// Provider branding is intentionally attempted only after all first-party
	// metadata candidates fail. This gives a real creator portrait priority.
	for _, iconURL := range fallbackCandidates {
		if requestCtx.Err() != nil {
			break
		}
		if _, alreadyTried := tried[iconURL]; alreadyTried {
			continue
		}
		tried[iconURL] = struct{}{}
		fetchCtx, cancelFetch := context.WithTimeout(requestCtx, sourceIconCandidateTimeout)
		body, contentType, fetchErr := fetchSourceIconCandidate(fetchCtx, iconURL)
		cancelFetch()
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		entry := sourceIconCacheEntry{
			SourceID: sourceID, IconURL: iconURL, ContentType: contentType, Body: body,
			FetchedAt: now.Format(time.RFC3339),
		}
		if err := storeSourceIcon(entry); err != nil {
			return sourceIconCacheEntry{}, err
		}
		return entry, nil
	}
	if lastErr == nil {
		lastErr = errSourceIconUnavailable
	}
	failureURL := ""
	if len(descriptor.SiteURLs) > 0 {
		failureURL = descriptor.SiteURLs[0]
	}
	if len(candidates) > 0 {
		failureURL = candidates[0]
	}
	retryAfter := sourceIconRetryAfter
	if len(cache.Body) == 0 {
		retryAfter = sourceIconEmptyRetryAfter
	}
	_ = recordSourceIconFailure(sourceID, failureURL, lastErr, now, retryAfter)
	if len(cache.Body) > 0 {
		return cache, nil
	}
	return sourceIconCacheEntry{}, fmt.Errorf("%w: %v", errSourceIconUnavailable, lastErr)
}

func discoverBilibiliCreatorAvatarURL(ctx context.Context, sourceID string) (string, error) {
	spec, ok := operationalSourceSpecs[sourceID]
	if !ok || spec.parser != "bilibili" || spec.bilibiliMID <= 0 {
		return "", errors.New("来源不是固定 Bilibili UP 主")
	}
	metadataURL := bilibiliFoloFeedURL(spec.bilibiliMID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "application/json")
	response, err := sourceIconHTTPClientFactory().Do(req)
	if err != nil {
		return "", err
	}
	if response.Body == nil {
		return "", errors.New("Bilibili UP 主元数据响应不完整")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("Bilibili UP 主元数据返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceIconHTMLBytes+1))
	if err != nil || len(body) == 0 || int64(len(body)) > maxSourceIconHTMLBytes {
		return "", errors.New("Bilibili UP 主元数据为空或超过大小上限")
	}
	var metadata foloResponse
	if jsonErr := json.Unmarshal(body, &metadata); jsonErr != nil || metadata.Code != 0 {
		return "", errors.New("Bilibili UP 主元数据无法解析")
	}
	if !validBilibiliSpaceURL(metadata.Data.Feed.SiteURL, spec.bilibiliMID) {
		return "", errors.New("Bilibili UP 主元数据账号不匹配")
	}
	iconURL := bilibiliImageURL(metadata.Data.Feed.Image)
	if iconURL == "" || !sourceIconURLAllowed(sourceID, iconURL) {
		return "", errors.New("Bilibili UP 主元数据没有头像")
	}
	// Original Bilibili portraits can exceed the icon cache's 256 KiB bound.
	// Ask the first-party image host for a small square rendition instead of
	// buffering the multi-megabyte original on the 2 GB board.
	return iconURL + "@180w_180h_1c.jpg", nil
}

func sourceIconRetryMatches(cache sourceIconCacheEntry, candidates []string) bool {
	if len(candidates) == 0 {
		return true
	}
	for _, candidate := range candidates {
		if cache.IconURL == candidate {
			return true
		}
	}
	return false
}

func sourceIconDescriptorByID(sourceID string) (sourceIconDescriptor, error) {
	var stored, siteURL, feedURL, collectionMode string
	err := db.QueryRow(`SELECT COALESCE(c.icon_url,''),COALESCE(c.site_url,''),f.url,COALESCE(f.collection_mode,'')
	FROM feed_sources f LEFT JOIN feed_catalog_entries c ON c.source_id=f.id WHERE f.id=?`, sourceID).Scan(&stored, &siteURL, &feedURL, &collectionMode)
	if err != nil {
		return sourceIconDescriptor{}, err
	}
	if strings.TrimSpace(collectionMode) == "source_gateway" {
		creatorAvatar := creatorAvatarCandidateURL(sourceID)
		candidates := make([]string, 0, 2)
		fallbackCandidates := make([]string, 0, 1)
		if creatorAvatar != "" {
			candidates = append(candidates, creatorAvatar)
		}
		if normalized, normalizeErr := normalizePublicURL(stored); normalizeErr == nil && sourceIconURLAllowed(sourceID, normalized) {
			if normalized == youtubeProviderIconURL {
				fallbackCandidates = append(fallbackCandidates, normalized)
			} else if normalized != creatorAvatar {
				candidates = append(candidates, normalized)
			}
		}
		return sourceIconDescriptor{Candidates: candidates, FallbackCandidates: fallbackCandidates, GatewayManaged: true}, nil
	}
	candidates := make([]string, 0, 3)
	fallbackCandidates := make([]string, 0, 1)
	seen := make(map[string]struct{}, 3)
	storedValid := false
	candidateInputs := []string{stored, sourceFaviconURL(siteURL), sourceFaviconURL(feedURL)}
	for _, candidate := range candidateInputs {
		if !sourceIconURLAllowed(sourceID, candidate) {
			continue
		}
		normalized, normalizeErr := normalizePublicURL(candidate)
		if normalizeErr != nil {
			continue
		}
		if candidate == stored {
			storedValid = true
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	}
	if !storedValid && len(candidates) > 0 {
		_ = persistSourceIconCandidate(sourceID, candidates[0])
	}
	siteURLs := make([]string, 0, 2)
	seenSites := make(map[string]struct{}, 2)
	for _, candidate := range []string{siteURL, sourceOriginURL(feedURL)} {
		normalized, normalizeErr := normalizePublicURL(candidate)
		if normalizeErr != nil {
			continue
		}
		if _, exists := seenSites[normalized]; exists {
			continue
		}
		seenSites[normalized] = struct{}{}
		siteURLs = append(siteURLs, normalized)
	}
	return sourceIconDescriptor{Candidates: candidates, SiteURLs: siteURLs, FallbackCandidates: fallbackCandidates}, nil
}

func discoverWebsiteIconURL(ctx context.Context, siteURL string) (string, error) {
	normalized, err := normalizePublicURL(siteURL)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9")
	response, err := sourceIconHTTPClientFactory().Do(req)
	if err != nil {
		return "", err
	}
	if response.Body == nil {
		return "", errors.New("来源网站响应不完整")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("来源网站返回 HTTP %d", response.StatusCode)
	}
	// Favicons are declared in <head>, so a bounded prefix is sufficient even
	// when the full page is large. Never buffer the complete website response.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceIconHTMLBytes))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", errors.New("来源网站返回空页面")
	}
	baseURL := normalized
	if response.Request != nil && response.Request.URL != nil && validPublicFeedURL(response.Request.URL.String()) {
		baseURL = response.Request.URL.String()
	}
	for _, tag := range autodiscoveryLinkRE.FindAllString(string(body), 200) {
		attrs := map[string]string{}
		for _, match := range htmlAttributeRE.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(match[1])] = strings.TrimSpace(match[2])
		}
		rel := strings.ToLower(attrs["rel"])
		if !strings.Contains(rel, "icon") || strings.Contains(rel, "mask-icon") {
			continue
		}
		if candidate := resolvePublicLink(baseURL, attrs["href"]); candidate != "" {
			return candidate, nil
		}
	}
	return "", errors.New("来源网站没有声明可用图标")
}

func fetchSourceIcon(ctx context.Context, raw string) ([]byte, string, error) {
	normalized, err := normalizePublicURL(raw)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, normalized, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", feedUserAgent)
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/gif,image/x-icon,image/vnd.microsoft.icon;q=0.9")
	response, err := sourceIconHTTPClientFactory().Do(req)
	if err != nil {
		return nil, "", err
	}
	if response.Body == nil {
		return nil, "", errors.New("来源图标响应不完整")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("来源图标返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxSourceIconBytes {
		return nil, "", errors.New("来源图标超过大小上限")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceIconBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 || int64(len(body)) > maxSourceIconBytes {
		return nil, "", errors.New("来源图标为空或超过大小上限")
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(body), ";")[0]))
	if !allowedSourceIconContentType(contentType) {
		return nil, "", errors.New("来源图标格式不受支持")
	}
	return body, contentType, nil
}

func fetchSourceIconCandidate(ctx context.Context, raw string) ([]byte, string, error) {
	directCtx, cancelDirect := context.WithTimeout(ctx, sourceIconDirectTimeout)
	body, contentType, directErr := fetchSourceIcon(directCtx, raw)
	cancelDirect()
	if directErr == nil {
		return body, contentType, nil
	}

	proxyURL, ok := sourceIconProxyURL(raw)
	if !ok {
		return nil, "", directErr
	}
	body, contentType, proxyErr := fetchSourceIcon(ctx, proxyURL)
	if proxyErr != nil {
		return nil, "", errors.Join(directErr, proxyErr)
	}
	return body, contentType, nil
}

func sourceIconProxyURL(raw string) (string, bool) {
	// X image hosts are frequently unreachable on otherwise healthy networks.
	// Keep the fallback fixed and narrow: the original URL must already be a
	// validated public profile image, and only bounded image bytes come back.
	original, ok := validFoloMediaURL(raw, "pbs.twimg.com")
	if !ok {
		return "", false
	}
	query := url.Values{}
	query.Set("url", original)
	query.Set("width", "96")
	query.Set("height", "96")
	return (&url.URL{Scheme: "https", Host: sourceIconProxyHost, RawQuery: query.Encode()}).String(), true
}

func allowedSourceIconContentType(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "image/png", "image/jpeg", "image/webp", "image/gif", "image/x-icon", "image/vnd.microsoft.icon":
		return true
	default:
		return false
	}
}

func loadSourceIconCache(sourceID string) (sourceIconCacheEntry, error) {
	var entry sourceIconCacheEntry
	err := db.QueryRow(`SELECT source_id,icon_url,content_type,body,fetched_at,retry_after,last_error
FROM source_icon_cache WHERE source_id=?`, sourceID).Scan(&entry.SourceID, &entry.IconURL, &entry.ContentType,
		&entry.Body, &entry.FetchedAt, &entry.RetryAfter, &entry.LastError)
	if err == nil && int64(len(entry.Body)) > maxSourceIconBytes {
		return sourceIconCacheEntry{}, errors.New("来源图标缓存超过大小上限")
	}
	return entry, err
}

func storeSourceIcon(entry sourceIconCacheEntry) error {
	if !sourceIconURLAllowed(entry.SourceID, entry.IconURL) || !allowedSourceIconContentType(entry.ContentType) || len(entry.Body) == 0 || int64(len(entry.Body)) > maxSourceIconBytes {
		return errors.New("来源图标缓存内容不合法")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO source_icon_cache(source_id,icon_url,content_type,body,fetched_at,retry_after,last_error)
VALUES(?,?,?,?,?,'','') ON CONFLICT(source_id) DO UPDATE SET icon_url=excluded.icon_url,
content_type=excluded.content_type,body=excluded.body,fetched_at=excluded.fetched_at,retry_after='',last_error=''`,
		entry.SourceID, entry.IconURL, entry.ContentType, entry.Body, entry.FetchedAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM source_icon_cache WHERE source_id NOT IN
(SELECT source_id FROM source_icon_cache
 ORDER BY COALESCE(NULLIF(fetched_at,''),retry_after) DESC,source_id LIMIT ?)`, maxSourceIconRows)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func recordSourceIconFailure(sourceID, iconURL string, fetchErr error, now time.Time, retryAfter time.Duration) error {
	if retryAfter <= 0 {
		retryAfter = sourceIconRetryAfter
	}
	retryAt := now.Add(retryAfter).Format(time.RFC3339)
	message := limitText(fetchErr.Error(), 500)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Bind an empty-body failure to the candidate that actually failed. A
	// metadata refresh can discover a new URL after the previous failure was
	// cached; retaining an empty/stale URL would let the next request bypass
	// the cooldown. Existing image bytes remain authoritative and keep their
	// previously served URL.
	failedCandidate := ""
	if sourceIconURLAllowed(sourceID, iconURL) {
		failedCandidate = iconURL
	}
	result, err := tx.Exec(`UPDATE source_icon_cache SET icon_url=CASE WHEN length(body)=0 AND ? <> '' THEN ? ELSE icon_url END,
retry_after=?,last_error=? WHERE source_id=?`, failedCandidate, failedCandidate, retryAt, message, sourceID)
	if err != nil {
		return err
	}
	if changed, changedErr := result.RowsAffected(); changedErr == nil && changed > 0 {
		return tx.Commit()
	}
	_, err = tx.Exec(`INSERT INTO source_icon_cache(source_id,icon_url,content_type,body,fetched_at,retry_after,last_error)
VALUES(?,?,'',X'','',?,?) ON CONFLICT(source_id) DO UPDATE SET retry_after=excluded.retry_after,last_error=excluded.last_error`,
		sourceID, failedCandidate, retryAt, message)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM source_icon_cache WHERE source_id NOT IN
(SELECT source_id FROM source_icon_cache
 ORDER BY COALESCE(NULLIF(fetched_at,''),retry_after) DESC,source_id LIMIT ?)`, maxSourceIconRows)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func timeWithin(raw string, now time.Time, duration time.Duration) bool {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return err == nil && !parsed.After(now) && now.Sub(parsed) < duration
}

func retryPending(raw string, now time.Time) bool {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return err == nil && parsed.After(now)
}

func writeSourceIcon(w http.ResponseWriter, r *http.Request, entry sourceIconCacheEntry) {
	digest := sha256.Sum256(entry.Body)
	etag := `"` + hex.EncodeToString(digest[:]) + `"`
	w.Header().Set("Content-Type", entry.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(entry.Body)))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Body)
}

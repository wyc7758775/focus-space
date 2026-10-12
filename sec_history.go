package main

import (
	"bufio"
	"compress/gzip"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// Keep the released database URL as the source identity. EFTS is not used
	// for history because broad anonymous searches regularly stall.
	secHistoryEndpoint             = "https://efts.sec.gov/LATEST/search-index"
	secCurrentFeedEndpoint         = "https://www.sec.gov/Archives/edgar/xbrl-inline.rss.xml"
	secQuarterlyFeedBaseURL        = "https://www.sec.gov/Archives/edgar/full-index/"
	secHistoryCursorPrefix         = "sec-xbrl-quarter:v1:"
	secHistoryMaxCompressedBytes   = 2 << 20
	secHistoryMaxDecompressedBytes = 16 << 20
	secHistoryMaxLineBytes         = 256 << 10
	secHistoryMaxAttempts          = 2
	secHistoryRequestTimeout       = 35 * time.Second
	secHistorySourceTimeout        = 75 * time.Second
	secHistoryBackfillInterval     = time.Minute
	secHistoryRetryInterval        = 10 * time.Minute
)

const secHistoryIndexHeader = "CIK|Company Name|Form Type|Date Filed|Filename"

type secHistoryQuarterFetchFunc func(context.Context, string, FeedSource, int) (fetchedFeed, error)

var secHistoryQuarterFetch secHistoryQuarterFetchFunc = fetchSECQuarterHistory
var secHistoryClientFactory = newSECHistoryHTTPClient

func collectSECHistoryOperational(ctx context.Context, source FeedSource, fetcher operationalFetchFunc, fetchedAt string) operationalSourceResult {
	if source.ID != "sec-xbrl-filings" || source.URL != secHistoryEndpoint || fetcher == nil {
		return operationalSourceResult{Status: "unavailable", Message: "SEC 固定历史来源配置无效"}
	}
	reference := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, fetchedAt); err == nil {
		reference = parsed.UTC()
	}
	backfill := secHistoryNeedsBackfill(source)
	quarter := secHistoryQuarterAt(reference)
	if backfill {
		if resumed, ok := validSECHistoryCursor(source.BackfillCursor); ok {
			quarter = resumed
		}
	}
	limit := trustedHistoricalItemLimit(source)
	feed, err := secHistoryQuarterFetch(ctx, secHistoryQuarterURL(quarter), source, limit)
	if err != nil || len(feed.Items) == 0 {
		return collectSECHistoryFallback(ctx, source, fetcher, quarter, backfill, err)
	}

	status, message := "ok", ""
	nextCursor := ""
	if backfill {
		nextQuarter := previousSECQuarter(quarter)
		if secHistoryQuarterInsideWindow(nextQuarter, reference, source.BackfillDays) {
			nextCursor = secHistoryCursor(nextQuarter)
			status = "partial"
			message = "已读取当前 SEC XBRL 历史窗口；后续同步将继续回填更早申报"
		}
	}
	result := operationalFeedResultWithLimit(feed, status, message, feedFetchResponse{}, limit)
	result.Backfill = backfill
	result.BackfillCursor = nextCursor
	result.BackfillCursorChanged = backfill
	if nextCursor != "" {
		result.NextSyncAfter = secHistoryBackfillInterval
	}
	return result
}

func collectSECHistoryFallback(ctx context.Context, source FeedSource, fetcher operationalFetchFunc, quarter string, backfill bool, historyErr error) operationalSourceResult {
	fallback := collectSECCurrentFeed(ctx, source, fetcher)
	if fallback.Status == "unavailable" {
		if historyErr != nil {
			fallback.Message = limitText("SEC 季度索引与最新窗口均读取失败："+errorText(historyErr)+"；"+fallback.Message, 500)
		}
		return fallback
	}
	fallback.Status = "partial"
	fallback.Message = "SEC 季度索引暂不可用，已保留最新 XBRL 申报"
	if historyErr != nil {
		fallback.Message = limitText(fallback.Message+"："+errorText(historyErr), 500)
	}
	if backfill {
		fallback.Message = limitText(fallback.Message+"；后续同步继续回填历史", 500)
		fallback.Backfill = true
		fallback.BackfillCursor = secHistoryCursor(quarter)
		fallback.BackfillCursorChanged = true
		fallback.NextSyncAfter = secHistoryRetryInterval
	}
	return fallback
}

func secHistoryNeedsBackfill(source FeedSource) bool {
	if source.BackfillDays <= 0 {
		return false
	}
	if source.RetentionLimit > 0 && source.EntryCount >= source.RetentionLimit {
		return false
	}
	return source.LastSyncAt == "" || source.RecordsSeen == 0 || strings.TrimSpace(source.BackfillCursor) != ""
}

func secHistoryQuarterAt(reference time.Time) string {
	quarter := (int(reference.UTC().Month())-1)/3 + 1
	return fmt.Sprintf("%04d-Q%d", reference.UTC().Year(), quarter)
}

func parseSECQuarter(value string) (int, int, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[4:6] != "-Q" {
		return 0, 0, false
	}
	year, yearErr := strconv.Atoi(value[:4])
	quarter, quarterErr := strconv.Atoi(value[6:])
	if yearErr != nil || quarterErr != nil || year < 2009 || quarter < 1 || quarter > 4 {
		return 0, 0, false
	}
	return year, quarter, true
}

func secHistoryCursor(quarter string) string {
	if _, _, ok := parseSECQuarter(quarter); !ok {
		return ""
	}
	return secHistoryCursorPrefix + quarter
}

func validSECHistoryCursor(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, secHistoryCursorPrefix) {
		return "", false
	}
	quarter := strings.TrimPrefix(value, secHistoryCursorPrefix)
	year, _, ok := parseSECQuarter(quarter)
	if !ok || year > time.Now().UTC().Year()+1 || secHistoryCursor(quarter) != value {
		return "", false
	}
	return quarter, true
}

func secHistoryQuarterURL(quarter string) string {
	year, number, ok := parseSECQuarter(quarter)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s%04d/QTR%d/xbrl.gz", secQuarterlyFeedBaseURL, year, number)
}

func secHistoryQuarterFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Hostname(), "www.sec.gov") {
		return "", false
	}
	path := strings.TrimPrefix(parsed.EscapedPath(), "/Archives/edgar/full-index/")
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[2] != "xbrl.gz" || len(parts[1]) != 4 || !strings.HasPrefix(parts[1], "QTR") {
		return "", false
	}
	quarter := parts[0] + "-Q" + strings.TrimPrefix(parts[1], "QTR")
	return quarter, secHistoryQuarterURL(quarter) == raw
}

func previousSECQuarter(quarter string) string {
	year, number, ok := parseSECQuarter(quarter)
	if !ok {
		return ""
	}
	if number > 1 {
		number--
	} else {
		year--
		number = 4
	}
	return fmt.Sprintf("%04d-Q%d", year, number)
}

func secHistoryQuarterInsideWindow(quarter string, reference time.Time, backfillDays int) bool {
	year, number, ok := parseSECQuarter(quarter)
	if !ok || backfillDays <= 0 {
		return false
	}
	month := time.Month((number-1)*3 + 1)
	quarterStart := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	cutoff := reference.UTC().Add(-time.Duration(backfillDays) * 24 * time.Hour)
	cutoffQuarter := time.Date(cutoff.Year(), time.Month(((int(cutoff.Month())-1)/3)*3+1), 1, 0, 0, 0, 0, time.UTC)
	return !quarterStart.Before(cutoffQuarter)
}

func collectSECCurrentFeed(ctx context.Context, source FeedSource, fetcher operationalFetchFunc) operationalSourceResult {
	response, err := fetcher(ctx, secCurrentFeedEndpoint, "", "")
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	feed, err := parseXMLFeedWithLimit(response.Body, secCurrentFeedEndpoint, feedFetchMaxItems)
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	feed, err = validateSECXBRLFeed(feed, source)
	if err != nil {
		return operationalSourceResult{Status: "unavailable", Message: limitText(errorText(err), 500)}
	}
	return operationalFeedResultWithLimit(feed, "ok", "", response, feedFetchMaxItems)
}

func fetchSECQuarterHistory(ctx context.Context, raw string, source FeedSource, limit int) (fetchedFeed, error) {
	if _, ok := secHistoryQuarterFromURL(raw); !ok || limit < 1 || limit > feedFetchTrustedMaxItems {
		return fetchedFeed{}, errors.New("SEC 季度索引地址或读取边界无效")
	}
	var lastErr error
	for attempt := 0; attempt < secHistoryMaxAttempts; attempt++ {
		feed, retryable, err := fetchSECQuarterHistoryAttempt(ctx, raw, source, limit)
		if err == nil {
			return feed, nil
		}
		lastErr = err
		if !retryable || attempt+1 >= secHistoryMaxAttempts || ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fetchedFeed{}, ctx.Err()
		case <-timer.C:
		}
	}
	return fetchedFeed{}, lastErr
}

func fetchSECQuarterHistoryAttempt(ctx context.Context, raw string, source FeedSource, limit int) (fetchedFeed, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, secHistoryRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, raw, nil)
	if err != nil {
		return fetchedFeed{}, false, err
	}
	request.Header.Set("User-Agent", feedUserAgent+" contact=https://zettlab.com")
	request.Header.Set("Accept", "application/gzip, application/octet-stream;q=0.9")
	client := secHistoryClientFactory()
	if client == nil {
		return fetchedFeed{}, false, errors.New("SEC 季度索引客户端不可用")
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return fetchedFeed{}, ctx.Err() == nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooEarly || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return fetchedFeed{}, retryable, fmt.Errorf("SEC 季度索引返回 HTTP %d", response.StatusCode)
	}
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.String() != raw {
		return fetchedFeed{}, false, errors.New("SEC 季度索引响应地址不匹配")
	}
	if response.ContentLength > secHistoryMaxCompressedBytes {
		return fetchedFeed{}, false, errors.New("SEC 季度索引超过压缩大小上限")
	}
	feed, err := parseSECQuarterGZIP(response.Body, source, raw, limit, secHistoryMaxCompressedBytes, secHistoryMaxDecompressedBytes)
	return feed, requestCtx.Err() != nil && ctx.Err() == nil, err
}

func newSECHistoryHTTPClient() *http.Client {
	client := publicFeedHTTPClient()
	if transport, ok := client.Transport.(*http.Transport); ok {
		cloned := transport.Clone()
		cloned.ResponseHeaderTimeout = secHistoryRequestTimeout
		client.Transport = cloned
	}
	client.Timeout = secHistoryRequestTimeout
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("SEC 固定季度索引不允许重定向")
	}
	return client
}

func parseSECQuarterGZIP(reader io.Reader, source FeedSource, raw string, limit int, maxCompressed, maxDecompressed int64) (fetchedFeed, error) {
	if reader == nil || maxCompressed < 1 || maxCompressed > secHistoryMaxCompressedBytes || maxDecompressed < 1 || maxDecompressed > secHistoryMaxDecompressedBytes {
		return fetchedFeed{}, errors.New("SEC 季度索引压缩边界无效")
	}
	compressed := &io.LimitedReader{R: reader, N: maxCompressed + 1}
	archive, err := gzip.NewReader(compressed)
	if err != nil {
		return fetchedFeed{}, errors.New("SEC 季度索引不是有效 GZIP")
	}
	feed, parseErr := parseSECQuarterIndex(archive, source, raw, limit, maxDecompressed)
	closeErr := archive.Close()
	if parseErr != nil {
		return fetchedFeed{}, parseErr
	}
	if closeErr != nil {
		return fetchedFeed{}, closeErr
	}
	if compressed.N <= 0 {
		return fetchedFeed{}, errors.New("SEC 季度索引超过压缩大小上限")
	}
	return feed, nil
}

func parseSECQuarterIndex(reader io.Reader, source FeedSource, raw string, limit int, maxBytes int64) (fetchedFeed, error) {
	quarter, ok := secHistoryQuarterFromURL(raw)
	if reader == nil || !ok || source.ID != "sec-xbrl-filings" || source.URL != secHistoryEndpoint || limit < 1 || limit > feedFetchTrustedMaxItems || maxBytes < 1 || maxBytes > secHistoryMaxDecompressedBytes {
		return fetchedFeed{}, errors.New("SEC 季度索引读取边界无效")
	}
	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 64<<10), secHistoryMaxLineBytes)
	headerSeen, separatorSeen := false, false
	items := &secHistoryNewsHeap{}
	selectedIDs := make(map[string]struct{}, limit)
	heap.Init(items)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !headerSeen {
			headerSeen = line == secHistoryIndexHeader
			continue
		}
		if !separatorSeen {
			separatorSeen = len(line) >= 20 && strings.Trim(line, "-") == ""
			continue
		}
		if line == "" {
			continue
		}
		item, valid := secHistoryIndexNewsItem(line, source, quarter)
		if !valid {
			continue
		}
		if _, duplicate := selectedIDs[item.ExternalID]; duplicate {
			continue
		}
		if items.Len() < limit {
			heap.Push(items, item)
			selectedIDs[item.ExternalID] = struct{}{}
		} else if secHistoryNewsLess((*items)[0], item) {
			removed := heap.Pop(items).(NewsItem)
			delete(selectedIDs, removed.ExternalID)
			heap.Push(items, item)
			selectedIDs[item.ExternalID] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		return fetchedFeed{}, err
	}
	if limited.N <= 0 {
		return fetchedFeed{}, errors.New("SEC 季度索引超过解压大小上限")
	}
	if !headerSeen || !separatorSeen {
		return fetchedFeed{}, errors.New("SEC 季度索引身份不匹配")
	}
	if items.Len() == 0 {
		return fetchedFeed{}, errors.New("SEC 季度索引没有可用申报")
	}
	result := make([]NewsItem, items.Len())
	copy(result, *items)
	sort.Slice(result, func(i, j int) bool { return secHistoryNewsLess(result[j], result[i]) })
	return fetchedFeed{
		Title:   source.Name,
		SiteURL: "https://www.sec.gov/data-research/structured-data/structured-disclosure-rss-feeds",
		FeedURL: source.URL,
		Format:  "idx",
		Items:   result,
	}, nil
}

func secHistoryIndexNewsItem(line string, source FeedSource, quarter string) (NewsItem, bool) {
	fields := strings.SplitN(line, "|", 5)
	if len(fields) != 5 {
		return NewsItem{}, false
	}
	cik, company, form, filed, filename := strings.TrimSpace(fields[0]), cleanPlain(fields[1]), cleanPlain(fields[2]), strings.TrimSpace(fields[3]), strings.TrimSpace(fields[4])
	if len(cik) > 10 || !allASCIIDigits(cik) || company == "" || len([]rune(company)) > 300 || form == "" || len(form) > 64 {
		return NewsItem{}, false
	}
	filedAt, filedErr := time.Parse("2006-01-02", filed)
	if filedErr != nil || secHistoryQuarterAt(filedAt) != quarter {
		return NewsItem{}, false
	}
	const prefix = "edgar/data/"
	path := strings.TrimPrefix(filename, prefix)
	parts := strings.Split(path, "/")
	if path == filename || len(parts) != 2 || !strings.HasSuffix(parts[1], ".txt") {
		return NewsItem{}, false
	}
	pathCIK := strings.TrimSpace(parts[0])
	accession := strings.TrimSuffix(parts[1], ".txt")
	cleanCIK, cleanPathCIK := strings.TrimLeft(cik, "0"), strings.TrimLeft(pathCIK, "0")
	if cleanCIK == "" || cleanCIK != cleanPathCIK || !allASCIIDigits(pathCIK) || !secAccessionRE.MatchString(accession) {
		return NewsItem{}, false
	}
	cleanAccession := strings.ReplaceAll(accession, "-", "")
	link := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s-index.html", cleanCIK, cleanAccession, accession)
	title := company + " · " + form
	item := makeOperationalNews(source, accession+":"+form, title, link, filed+"T00:00:00Z", form+" XBRL filing", "", "sec-xbrl", "official", "")
	return item, true
}

type secHistoryNewsHeap []NewsItem

func (items secHistoryNewsHeap) Len() int           { return len(items) }
func (items secHistoryNewsHeap) Less(i, j int) bool { return secHistoryNewsLess(items[i], items[j]) }
func (items secHistoryNewsHeap) Swap(i, j int)      { items[i], items[j] = items[j], items[i] }
func (items *secHistoryNewsHeap) Push(value any)    { *items = append(*items, value.(NewsItem)) }
func (items *secHistoryNewsHeap) Pop() any {
	old := *items
	last := old[len(old)-1]
	*items = old[:len(old)-1]
	return last
}

func secHistoryNewsLess(first, second NewsItem) bool {
	if first.PublishedAt != second.PublishedAt {
		return first.PublishedAt < second.PublishedAt
	}
	return first.ExternalID < second.ExternalID
}

func validateSECXBRLFeed(feed fetchedFeed, source FeedSource) (fetchedFeed, error) {
	if !strings.Contains(strings.ToLower(feed.Title), "xbrl") {
		return fetchedFeed{}, errors.New("SEC XBRL 最新窗口身份不匹配")
	}
	items := make([]NewsItem, 0, len(feed.Items))
	for _, item := range feed.Items {
		accession, ok := secAccessionFromArchiveURL(item.URL)
		if !ok || strings.TrimSpace(item.Title) == "" {
			continue
		}
		form := strings.TrimSpace(strings.TrimPrefix(item.SummaryText, "Description:"))
		if form == "" {
			form = "XBRL"
		}
		item.ExternalID = accession + ":" + form
		item.Source = source.Name
		item.SourceType = "sec-xbrl"
		item.SourceLevel = "official"
		item.PermissionScope = "public_read"
		items = append(items, item)
	}
	if len(items) == 0 {
		return fetchedFeed{}, errors.New("SEC XBRL 最新窗口没有可用申报")
	}
	feed.Title = source.Name
	feed.SiteURL = "https://www.sec.gov/data-research/structured-data/structured-disclosure-rss-feeds"
	feed.FeedURL = source.URL
	feed.Format = "rss"
	feed.Items = items
	return feed, nil
}

func secAccessionFromArchiveURL(raw string) (string, bool) {
	if !validSECArchiveURL(raw, "-index.htm", "-index.html") {
		return "", false
	}
	parsed, _ := url.Parse(raw)
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}
	filename := parts[len(parts)-1]
	accession := strings.TrimSuffix(strings.TrimSuffix(filename, "-index.html"), "-index.htm")
	return accession, secAccessionRE.MatchString(accession)
}

func validSECArchiveURL(raw string, suffixes ...string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(parsed.Hostname(), "www.sec.gov") || !strings.HasPrefix(parsed.EscapedPath(), "/Archives/edgar/data/") {
		return false
	}
	path := strings.ToLower(parsed.EscapedPath())
	for _, suffix := range suffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

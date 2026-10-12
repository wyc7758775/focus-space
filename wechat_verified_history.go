package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	verifiedWeChatHistoryMaxBytes       int64 = 64 << 20
	verifiedWeChatHistoryMaxItems             = 1000
	verifiedWeChatHistoryMaxAttempts          = 2
	verifiedWeChatHistoryRequestTimeout       = 60 * time.Second
	verifiedWeChatHistoryHeaderTimeout        = 30 * time.Second
	verifiedWeChatHistorySourceTimeout        = 75 * time.Second
	verifiedWeChatHistoryMinBudget            = 25 * time.Second
	verifiedWeChatHistoryRetryDelay           = 10 * time.Minute
	verifiedWeChatHistoryStartupRetry         = time.Minute
	verifiedWeChatHistoryCursorPrefix         = "folo-history:v1:"
)

var errVerifiedWeChatHistoryWindowComplete = errors.New("公众号历史窗口已满")

type verifiedWeChatHistoryFeed struct {
	Type    string          `json:"type"`
	ID      json.RawMessage `json:"id"`
	URL     string          `json:"url"`
	SiteURL string          `json:"siteUrl"`
	Title   string          `json:"title"`
}

type verifiedWeChatHistoryEntry struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	GUID        string `json:"guid"`
	PublishedAt string `json:"publishedAt"`
	Author      string `json:"author"`
}

type verifiedWeChatHistoryCollectorFunc func(context.Context, FeedSource, weChatVerifiedFeedPolicy) (fetchedFeed, error)

type verifiedWeChatHistorySlotContextKey struct{}

var verifiedWeChatHistoryCollector verifiedWeChatHistoryCollectorFunc = fetchVerifiedWeChatHistory
var weChatSourceFetch operationalFetchFunc = fetchPublicURLWithRetry
var verifiedWeChatHistorySlot = make(chan struct{}, 1)
var verifiedWeChatHistoryClientFactory = newVerifiedWeChatHistoryHTTPClient

func verifiedWeChatHistoryEndpoint(policy weChatVerifiedFeedPolicy) (string, bool) {
	feedID := strings.TrimSpace(policy.HistoryFeedID)
	if feedID == "" || len(feedID) > 24 || !allASCIIDigits(feedID) {
		return "", false
	}
	return "https://api.folo.is/feeds?id=" + feedID + "&entriesLimit=-1", true
}

func verifiedWeChatHistoryCursor(policy weChatVerifiedFeedPolicy) string {
	if _, ok := verifiedWeChatHistoryEndpoint(policy); !ok {
		return ""
	}
	return verifiedWeChatHistoryCursorPrefix + policy.HistoryFeedID
}

func verifiedWeChatHistoryPending(source FeedSource, policy weChatVerifiedFeedPolicy) bool {
	cursor := verifiedWeChatHistoryCursor(policy)
	return cursor != "" && strings.TrimSpace(source.BackfillCursor) != cursor
}

func isVerifiedWeChatHistorySourceID(sourceID string) bool {
	sourceID = strings.TrimSpace(sourceID)
	for _, policy := range weChatVerifiedDirectFeedPolicies {
		if policy.SourceID == sourceID && policy.HistoryFeedID != "" {
			return true
		}
	}
	return false
}

func verifiedWeChatHistoryCompletedCursor(cursor string) bool {
	cursor = strings.TrimSpace(cursor)
	for _, policy := range weChatVerifiedDirectFeedPolicies {
		if completed := verifiedWeChatHistoryCursor(policy); completed != "" && cursor == completed {
			return true
		}
	}
	return false
}

func feedSyncTimeoutForSource(sourceID string) time.Duration {
	if isVerifiedWeChatHistorySourceID(sourceID) {
		return verifiedWeChatHistorySourceTimeout
	}
	if strings.TrimSpace(sourceID) == "sec-xbrl-filings" {
		return secHistorySourceTimeout
	}
	return feedFetchTimeout
}

func verifiedWeChatPolicyForSource(source FeedSource) (weChatVerifiedFeedPolicy, bool) {
	policy, ok := weChatVerifiedDirectFeedPolicies[strings.TrimSpace(source.URL)]
	if !ok || policy.SourceID != strings.TrimSpace(source.ID) || policy.SourceName != strings.TrimSpace(source.Name) {
		return weChatVerifiedFeedPolicy{}, false
	}
	return policy, true
}

func acquireVerifiedWeChatHistorySlot(ctx context.Context) (func(), error) {
	if held, _ := ctx.Value(verifiedWeChatHistorySlotContextKey{}).(bool); held {
		return func() {}, nil
	}
	select {
	case verifiedWeChatHistorySlot <- struct{}{}:
		return func() { <-verifiedWeChatHistorySlot }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func withVerifiedWeChatHistorySourceSlot(parent context.Context, sourceID string, task func(context.Context) error) error {
	if !isVerifiedWeChatHistorySourceID(sourceID) {
		return task(parent)
	}
	release, err := acquireVerifiedWeChatHistorySlot(parent)
	if err != nil {
		return err
	}
	defer release()
	return task(context.WithValue(parent, verifiedWeChatHistorySlotContextKey{}, true))
}

func newVerifiedWeChatHistoryHTTPClient() *http.Client {
	client := publicFeedHTTPClient()
	if transport, ok := client.Transport.(*http.Transport); ok {
		cloned := transport.Clone()
		cloned.ResponseHeaderTimeout = verifiedWeChatHistoryHeaderTimeout
		client.Transport = cloned
	}
	client.Timeout = verifiedWeChatHistoryRequestTimeout
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("固定公众号历史来源不允许重定向")
	}
	return client
}

func fetchVerifiedWeChatHistory(ctx context.Context, source FeedSource, policy weChatVerifiedFeedPolicy) (fetchedFeed, error) {
	if bound, ok := verifiedWeChatPolicyForSource(source); !ok || bound.HistoryFeedID == "" || bound.HistoryFeedID != policy.HistoryFeedID {
		return fetchedFeed{}, errors.New("固定公众号历史来源身份不匹配")
	}
	endpoint, ok := verifiedWeChatHistoryEndpoint(policy)
	if !ok {
		return fetchedFeed{}, errors.New("固定公众号历史地址不合法")
	}
	release, err := acquireVerifiedWeChatHistorySlot(ctx)
	if err != nil {
		return fetchedFeed{}, err
	}
	defer release()

	var lastErr error
	for attempt := 0; attempt < verifiedWeChatHistoryMaxAttempts; attempt++ {
		feed, retryable, attemptErr := fetchVerifiedWeChatHistoryAttempt(ctx, endpoint, source, policy)
		if attemptErr == nil {
			return feed, nil
		}
		lastErr = attemptErr
		if !retryable || attempt+1 >= verifiedWeChatHistoryMaxAttempts || ctx.Err() != nil {
			break
		}
	}
	return fetchedFeed{}, lastErr
}

func fetchVerifiedWeChatHistoryAttempt(ctx context.Context, endpoint string, source FeedSource, policy weChatVerifiedFeedPolicy) (fetchedFeed, bool, error) {
	requestCtx, cancel := context.WithTimeout(ctx, verifiedWeChatHistoryRequestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fetchedFeed{}, false, err
	}
	request.Header.Set("User-Agent", feedUserAgent)
	request.Header.Set("Accept", "application/json")
	client := verifiedWeChatHistoryClientFactory()
	if client == nil {
		return fetchedFeed{}, false, errors.New("公众号历史客户端不可用")
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return fetchedFeed{}, ctx.Err() == nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		retryable := response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooEarly || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500
		return fetchedFeed{}, retryable, fmt.Errorf("公众号历史来源返回 HTTP %d", response.StatusCode)
	}
	if response.Request == nil || response.Request.URL == nil || response.Request.URL.String() != endpoint {
		return fetchedFeed{}, false, errors.New("公众号历史来源响应地址不匹配")
	}
	feed, err := parseVerifiedWeChatHistoryWithLimits(response.Body, source, policy, verifiedWeChatHistoryMaxBytes, verifiedWeChatHistoryMaxItems)
	return feed, requestCtx.Err() != nil && ctx.Err() == nil, err
}

func parseVerifiedWeChatHistoryWithLimits(reader io.Reader, source FeedSource, policy weChatVerifiedFeedPolicy, maxBytes int64, maxItems int) (fetchedFeed, error) {
	if reader == nil || maxBytes < 1 || maxBytes > verifiedWeChatHistoryMaxBytes || maxItems < 1 || maxItems > verifiedWeChatHistoryMaxItems {
		return fetchedFeed{}, errors.New("公众号历史读取边界不合法")
	}
	if bound, ok := verifiedWeChatPolicyForSource(source); !ok || bound.HistoryFeedID == "" || bound.HistoryFeedID != policy.HistoryFeedID {
		return fetchedFeed{}, errors.New("固定公众号历史来源身份不匹配")
	}

	limited := &io.LimitedReader{R: reader, N: maxBytes + 1}
	decoder := json.NewDecoder(limited)
	decoder.UseNumber()
	codeSeen, dataSeen := false, false
	code := -1
	var identity verifiedWeChatHistoryFeed
	items := make([]NewsItem, 0, minInt(maxItems, 128))
	err := decodeVerifiedWeChatJSONObject(decoder, func(key string) error {
		switch key {
		case "code":
			codeSeen = true
			return decoder.Decode(&code)
		case "data":
			dataSeen = true
			return decodeVerifiedWeChatHistoryData(decoder, source, policy, maxItems, &identity, &items)
		default:
			return skipVerifiedWeChatJSONValue(decoder)
		}
	})
	windowComplete := errors.Is(err, errVerifiedWeChatHistoryWindowComplete)
	if err == nil {
		if token, trailingErr := decoder.Token(); trailingErr != io.EOF || token != nil {
			if trailingErr == nil {
				err = errors.New("公众号历史来源包含额外内容")
			} else {
				err = trailingErr
			}
		}
	}
	if limited.N <= 0 {
		return fetchedFeed{}, errors.New("公众号历史内容超过大小上限")
	}
	if err != nil && !windowComplete {
		return fetchedFeed{}, errors.New("公众号历史来源返回不可解析内容：" + errorText(err))
	}
	if !codeSeen || !dataSeen || code != 0 {
		return fetchedFeed{}, errors.New("公众号历史来源返回失败状态")
	}
	if err := validateVerifiedWeChatHistoryFeed(identity, source, policy); err != nil {
		return fetchedFeed{}, err
	}
	if len(items) == 0 || windowComplete && len(items) != maxItems {
		return fetchedFeed{}, errors.New("公众号历史来源没有返回可浏览文章")
	}
	return fetchedFeed{
		Title:       source.Name,
		Description: source.Name + " 的历史文章",
		SiteURL:     "https://mp.weixin.qq.com/",
		FeedURL:     source.URL,
		Format:      "rss",
		Items:       items,
	}, nil
}

func decodeVerifiedWeChatHistoryData(decoder *json.Decoder, source FeedSource, policy weChatVerifiedFeedPolicy, maxItems int, identity *verifiedWeChatHistoryFeed, items *[]NewsItem) error {
	feedSeen, entriesSeen := false, false
	err := decodeVerifiedWeChatJSONObject(decoder, func(key string) error {
		switch key {
		case "feed":
			if feedSeen {
				return errors.New("公众号历史来源重复返回 feed")
			}
			feedSeen = true
			if err := decoder.Decode(identity); err != nil {
				return err
			}
			return validateVerifiedWeChatHistoryFeed(*identity, source, policy)
		case "entries":
			if entriesSeen {
				return errors.New("公众号历史来源重复返回 entries")
			}
			entriesSeen = true
			return decodeVerifiedWeChatHistoryEntries(decoder, source, policy, maxItems, feedSeen, items)
		default:
			return skipVerifiedWeChatJSONValue(decoder)
		}
	})
	if err != nil {
		return err
	}
	if !feedSeen || !entriesSeen {
		return errors.New("公众号历史来源缺少 feed 或 entries")
	}
	return nil
}

func decodeVerifiedWeChatHistoryEntries(decoder *json.Decoder, source FeedSource, policy weChatVerifiedFeedPolicy, maxItems int, allowEarlyStop bool, items *[]NewsItem) error {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return errors.New("公众号历史 entries 不是数组")
	}
	seenIdentity := make(map[string]int, maxItems)
	seenURL := make(map[string]string, maxItems)
	for decoder.More() {
		if len(*items) >= maxItems {
			if allowEarlyStop {
				return errVerifiedWeChatHistoryWindowComplete
			}
			return errors.New("公众号历史条目超过数量上限")
		}
		var raw verifiedWeChatHistoryEntry
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		link, normalizeErr := normalizeWechatEntryURL(raw.URL)
		identity, valid := verifiedWeChatArticleIdentity(link, policy)
		guid, guidErr := normalizeWechatEntryURL(raw.GUID)
		if normalizeErr != nil || guidErr != nil || !valid || guid != link {
			return errors.New("公众号历史文章身份与固定来源不匹配")
		}
		published, publishedErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw.PublishedAt))
		if publishedErr != nil || strings.TrimSpace(cleanPlain(raw.Title)) == "" {
			return errors.New("公众号历史文章缺少标题或发布时间")
		}
		item := makeFetchedNews(identity, raw.Title, link, source.Name, firstNonEmpty(raw.Author, source.Name), published.UTC().Format(time.RFC3339), "", "", "", "", 0, source.URL)
		item.SourceType = "wechat"
		item.SourceLevel = "public_aggregator"
		item.PermissionScope = "public_read"
		item.historyMetadata = true
		if existingIndex, duplicate := seenIdentity[identity]; duplicate {
			existingPublished, _ := time.Parse(time.RFC3339, (*items)[existingIndex].PublishedAt)
			if published.After(existingPublished) {
				delete(seenURL, (*items)[existingIndex].URL)
				if otherIdentity, occupied := seenURL[link]; occupied && otherIdentity != identity {
					return errors.New("公众号历史来源返回冲突文章地址")
				}
				(*items)[existingIndex] = item
				seenURL[link] = identity
			}
			continue
		}
		if otherIdentity, duplicate := seenURL[link]; duplicate && otherIdentity != identity {
			return errors.New("公众号历史来源返回冲突文章地址")
		}
		seenIdentity[identity] = len(*items)
		seenURL[link] = identity
		*items = append(*items, item)
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') {
		return errors.New("公众号历史 entries 没有正常结束")
	}
	return nil
}

func decodeVerifiedWeChatJSONObject(decoder *json.Decoder, visit func(string) error) error {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("公众号历史对象格式不合法")
	}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("公众号历史对象键不合法")
		}
		if err := visit(key); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return errors.New("公众号历史对象没有正常结束")
	}
	return nil
}

func skipVerifiedWeChatJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, nested := token.(json.Delim)
	if !nested || delim != '{' && delim != '[' {
		return nil
	}
	depth := 1
	for depth > 0 {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
		if child, ok := token.(json.Delim); ok {
			switch child {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
	return nil
}

func validateVerifiedWeChatHistoryFeed(identity verifiedWeChatHistoryFeed, source FeedSource, policy weChatVerifiedFeedPolicy) error {
	feedID, err := verifiedWeChatJSONID(identity.ID)
	if err != nil || feedID != policy.HistoryFeedID || strings.TrimSpace(identity.Type) != "feed" || strings.TrimSpace(cleanPlain(identity.Title)) != policy.FeedTitle {
		return errors.New("公众号历史 feed 身份与固定来源不匹配")
	}
	feedURL, feedErr := normalizePublicURL(identity.URL)
	siteURL, siteErr := normalizePublicURL(identity.SiteURL)
	if feedErr != nil || siteErr != nil || feedURL != source.URL || siteURL != source.URL {
		return errors.New("公众号历史 feed 地址与固定来源不匹配")
	}
	return nil
}

func verifiedWeChatJSONID(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		if value != "" && len(value) <= 24 && allASCIIDigits(value) {
			return value, nil
		}
		return "", errors.New("公众号历史 feed id 不合法")
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value = number.String()
		if len(value) <= 24 && allASCIIDigits(value) {
			return value, nil
		}
	}
	return "", errors.New("公众号历史 feed id 不合法")
}

func verifiedWeChatArticleIdentity(raw string, policy weChatVerifiedFeedPolicy) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !verifiedWeChatArticleURL(parsed.String(), policy) {
		return "", false
	}
	values := parsed.Query()
	for _, key := range []string{"__biz", "mid", "idx", "sn"} {
		if len(values[key]) != 1 || strings.TrimSpace(values[key][0]) == "" {
			return "", false
		}
	}
	mid, idx, sn := values.Get("mid"), values.Get("idx"), values.Get("sn")
	idxNumber, idxErr := strconv.Atoi(idx)
	if len(mid) > 32 || !allASCIIDigits(mid) || idxErr != nil || idxNumber < 1 || idxNumber > 99 || len(sn) > 128 {
		return "", false
	}
	for _, char := range sn {
		if char < '0' || char > '9' && char < 'A' || char > 'Z' && char < 'a' || char > 'z' && char != '_' && char != '-' {
			return "", false
		}
	}
	return "wechat:" + values.Get("__biz") + ":" + mid + ":" + idx, true
}

func mergeVerifiedWeChatHistory(latest fetchedFeed, history fetchedFeed) fetchedFeed {
	if latest.Title == "" {
		latest.Title = history.Title
	}
	latest.Description = firstNonEmpty(latest.Description, history.Description)
	latest.SiteURL = firstNonEmpty(latest.SiteURL, history.SiteURL)
	latest.FeedURL = firstNonEmpty(latest.FeedURL, history.FeedURL)
	latest.Format = firstNonEmpty(latest.Format, history.Format)
	seen := make(map[string]struct{}, len(latest.Items)+len(history.Items))
	for _, item := range latest.Items {
		seen[item.ExternalID] = struct{}{}
	}
	for _, item := range history.Items {
		if _, duplicate := seen[item.ExternalID]; duplicate {
			continue
		}
		seen[item.ExternalID] = struct{}{}
		latest.Items = append(latest.Items, item)
		if len(latest.Items) >= verifiedWeChatHistoryMaxItems {
			break
		}
	}
	return latest
}

func alignVerifiedWeChatItemIdentityTx(tx *sql.Tx, item *NewsItem) error {
	if tx == nil || item == nil || item.SourceID == "" || item.URL == "" {
		return nil
	}
	var policy weChatVerifiedFeedPolicy
	found := false
	for _, candidate := range weChatVerifiedDirectFeedPolicies {
		if candidate.SourceID == item.SourceID && candidate.HistoryFeedID != "" {
			policy, found = candidate, true
			break
		}
	}
	if !found {
		return nil
	}
	identity, valid := verifiedWeChatArticleIdentity(item.URL, policy)
	if !valid {
		return errors.New("固定公众号文章身份不合法")
	}
	item.ExternalID = identity
	var existingExternalID string
	err := tx.QueryRow(`SELECT external_id FROM news WHERE source_id=? AND url=? ORDER BY published_at DESC,id LIMIT 1`, item.SourceID, item.URL).Scan(&existingExternalID)
	if err == nil && existingExternalID != "" {
		item.ExternalID = existingExternalID
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func verifiedWeChatHistoryItemExistsTx(tx *sql.Tx, item NewsItem) (bool, error) {
	var existingID string
	err := tx.QueryRow(`SELECT id FROM news WHERE source_id=? AND (external_id=? OR url=?) ORDER BY published_at DESC,id LIMIT 1`, item.SourceID, item.ExternalID, item.URL).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && existingID != "", err
}

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"focus-space/internal/app"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxEntryFullTextRunes  = 32_000
	maxEntryArticleImages  = 24
	maxEntryImageBytes     = 16 << 20
	maxPersistedFullTexts  = 64
	entryImageFetchTimeout = 30 * time.Second
	entryImageSlotWait     = 5 * time.Second
)

var entryImageProxySlots = make(chan struct{}, 4)

// A verified WeChat feed is the only direct recovery path for a gateway
// article whose public page has turned into a challenge shell. The URL is
// selected from the pinned source map, never from the entry or the caller.
var entryContentFetchVerifiedWeChatFeed = fetchPublicURLWithCache

func boolToSQLiteInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// Kept as a package variable so boundary tests can prove that gateway-managed
// rows never reach the retained single-article public fetch path.
var entryContentFetchPublicURL = fetchPublicURLWithCache

type EntryContent struct {
	EntryID         string         `json:"entry_id"`
	ContentHash     string         `json:"content_hash,omitempty"`
	ContentText     string         `json:"content_text"`
	ContentHTML     string         `json:"content_html,omitempty"`
	ContentFormat   string         `json:"content_format"`
	Blocks          []ArticleBlock `json:"blocks,omitempty"`
	BlocksVersion   string         `json:"blocks_version,omitempty"`
	BlocksHash      string         `json:"blocks_hash,omitempty"`
	BlocksTruncated bool           `json:"blocks_truncated,omitempty"`
	ImageCount      int            `json:"image_count,omitempty"`
	LeadImageIndex  int            `json:"lead_image_index"`
	FetchedAt       string         `json:"fetched_at"`
	Status          string         `json:"status"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	SourceURL       string         `json:"source_url,omitempty"`
	Cached          bool           `json:"cached"`
	UpdatedAt       string         `json:"updated_at,omitempty"`
	imageURLsJSON   string
}

// entryContentHandler implements on-demand article reading. The browser never
// fetches the external page. Non-gateway single articles use the same
// public-only bounded request as feed sync; gateway-managed rows only use the
// local Source Gateway bridge. Successful projections are persisted in the AC
// reader cache so the next open is local; the cache is explicitly bounded to
// avoid unbounded device growth.
func entryContentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "id" && key != "refresh") || len(values) != 1 {
			app.UserErr(w, http.StatusBadRequest, "全文筛选条件不合法")
			return
		}
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validText(id, 128, true) || strings.ContainsAny(id, "/\\\x00\r\n") {
		app.UserErr(w, http.StatusBadRequest, "条目标识不合法")
		return
	}
	refresh := r.URL.Query().Get("refresh")
	if refresh != "" && refresh != "0" && refresh != "1" {
		app.UserErr(w, http.StatusBadRequest, "refresh 不合法")
		return
	}

	var sourceURL, sourceHash, summaryText, gatewayEntryID, collectionMode string
	if err := db.QueryRow(`SELECT n.url,n.content_hash,n.summary_text,n.gateway_entry_id,COALESCE(f.collection_mode,'')
FROM news n LEFT JOIN feed_sources f ON f.id=n.source_id
WHERE n.id=? AND n.deleted_at='' AND (COALESCE(n.source_id,'') <> 'focus-chat-links' OR EXISTS (
  SELECT 1 FROM focus_chat_link_entries l
  WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=?
    AND l.source_url=n.url AND l.content_hash=n.content_hash
))`, id, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&sourceURL, &sourceHash, &summaryText, &gatewayEntryID, &collectionMode); errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "条目不存在")
		return
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}

	cached, cacheErr := readEntryContent(id)
	if cacheErr != nil && !errors.Is(cacheErr, sql.ErrNoRows) {
		app.HTTPErr(w, cacheErr)
		return
	}
	// A cached projection is the stable reader surface. Do not re-fetch it on
	// every open when the public page has started returning a challenge page.
	if refresh != "1" && cacheErr == nil && entryContentCacheUsable(cached, sourceURL, sourceHash) && !entryContentProjectionLooksBlocked(cached) {
		cached.Cached = true
		if cached.Status != "ok" {
			cached.Status = "stale"
		}
		app.WriteJSON(w, http.StatusOK, cached)
		return
	}
	if isGatewayManagedEntry(gatewayEntryID, collectionMode) {
		if gatewayEntryID == "" {
			gatewayContentFailure(w, id, sourceURL, sourceHash, summaryText, cached, cacheErr, gatewayUnavailableNotice)
			return
		}
		// 网关条目：正文按需经设备本地来源网关读取，不直接请求原文站。
		serveGatewayEntryContent(w, r, id, sourceURL, sourceHash, summaryText, gatewayEntryID, cached, cacheErr)
		return
	}
	if !validPublicFeedURL(sourceURL) {
		if cacheErr == nil && cached.ContentText != "" && !entryContentProjectionLooksBlocked(cached) {
			cached.Cached = true
			cached.Status = "stale"
			cached.ErrorMessage = "原文不是公开来源，保留上次缓存"
			app.WriteJSON(w, http.StatusOK, cached)
			return
		}
		app.UserErr(w, http.StatusUnprocessableEntity, "该条目没有可安全读取的公开原文")
		return
	}

	ctx, cancel := requestContext(r)
	defer cancel()
	response, err := entryContentFetchPublicURL(ctx, sourceURL, "", "")
	if err != nil {
		_ = writeEntryContentFailure(id, sourceURL, sourceHash, err.Error())
		if cacheErr == nil && entryContentCacheUsable(cached, sourceURL, sourceHash) && !entryContentProjectionLooksBlocked(cached) {
			cached.Cached = true
			cached.Status = "stale"
			cached.ErrorMessage = limitText(err.Error(), 500)
			app.WriteJSON(w, http.StatusOK, cached)
			return
		}
		if strings.TrimSpace(summaryText) != "" {
			app.WriteJSON(w, http.StatusOK, EntryContent{
				EntryID: id, ContentHash: sourceHash, ContentText: summaryText,
				ContentFormat: "text", FetchedAt: nowUTC(), Status: "summary",
				SourceURL: sourceURL, Cached: false,
			})
			return
		}
		app.UserErr(w, http.StatusBadGateway, "原文暂时读取失败")
		return
	}
	article := renderEntryContent(response.Body, response.ContentType, firstNonEmpty(response.FinalURL, sourceURL), sourceURL)
	// WeChat/RSS bridges occasionally return a challenge shell (for example
	// “参数错误”) with navigation chrome instead of the article. Never expose
	// that shell as readable content. Fall back to the bounded feed summary;
	// the last known durable projection is kept when one is available.
	if articleResponseLooksBlocked(sourceURL, response.Body, article) {
		_ = writeEntryContentFailure(id, sourceURL, sourceHash, "原文来源返回挑战页")
		if cacheErr == nil && entryContentCacheUsable(cached, sourceURL, sourceHash) && !entryContentProjectionLooksBlocked(cached) {
			cached.Cached = true
			cached.Status = "stale"
			cached.ErrorMessage = "原文来源返回挑战页，保留上次缓存"
			app.WriteJSON(w, http.StatusOK, cached)
			return
		}
		if strings.TrimSpace(summaryText) == "" {
			app.UserErr(w, http.StatusBadGateway, "原文来源暂时不可用")
			return
		}
		app.WriteJSON(w, http.StatusOK, EntryContent{
			EntryID: id, ContentHash: sourceHash, ContentText: summaryText,
			ContentFormat: "text", FetchedAt: nowUTC(), Status: "summary",
			SourceURL: sourceURL, Cached: false,
		})
		return
	}
	if article.Text == "" {
		err = errors.New("原文没有可读正文")
		_ = writeEntryContentFailure(id, sourceURL, sourceHash, err.Error())
		if cacheErr == nil && entryContentCacheUsable(cached, sourceURL, sourceHash) && !entryContentProjectionLooksBlocked(cached) {
			cached.Cached = true
			cached.Status = "stale"
			cached.ErrorMessage = err.Error()
			app.WriteJSON(w, http.StatusOK, cached)
			return
		}
		if strings.TrimSpace(summaryText) != "" {
			app.WriteJSON(w, http.StatusOK, EntryContent{
				EntryID: id, ContentHash: sourceHash, ContentText: summaryText,
				ContentFormat: "text", FetchedAt: nowUTC(), Status: "summary",
				SourceURL: sourceURL, Cached: false,
			})
			return
		}
		app.UserErr(w, http.StatusUnprocessableEntity, "原文没有可读正文")
		return
	}

	now := nowUTC()
	if err := persistEntryContent(id, sourceURL, sourceHash, article, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if article.LeadImageURL != "" {
		_, _ = db.Exec(`UPDATE news SET thumbnail_url=CASE WHEN thumbnail_url='' THEN ? ELSE thumbnail_url END, updated_at=? WHERE id=?`, article.LeadImageURL, now, id)
	}
	article = finalizeArticleBlocks(article)
	app.WriteJSON(w, http.StatusOK, EntryContent{EntryID: id, ContentHash: sourceHash, ContentText: article.Text, ContentHTML: article.SafeHTML, ContentFormat: extractedArticleFormat(article), Blocks: article.Blocks, BlocksVersion: article.BlocksVersion, BlocksHash: article.BlocksHash, BlocksTruncated: article.BlocksTruncated, ImageCount: len(article.ImageURLs), LeadImageIndex: article.LeadImageIndex, FetchedAt: now, Status: "ok", SourceURL: sourceURL, Cached: false})
}

func persistEntryContent(id, sourceURL, sourceHash string, article extractedArticle, now string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := persistEntryContentTx(tx, id, sourceURL, sourceHash, article, now); err != nil {
		return err
	}
	if err := repairEntrySummaryTx(tx, id, article.Text, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pruneEntryContentCache()
	return nil
}

func repairEntrySummaryTx(tx *sql.Tx, entryID, recoveredText, now string) error {
	recoveredSummary := limitText(strings.TrimSpace(recoveredText), 1200)
	if recoveredSummary == "" || containsBlockedArticleMarker(recoveredSummary) {
		return nil
	}
	var currentSummary string
	if err := tx.QueryRow(`SELECT summary_text FROM news WHERE id=? AND deleted_at=''`, entryID).Scan(&currentSummary); err != nil {
		return err
	}
	if strings.TrimSpace(currentSummary) != "" && !containsBlockedArticleMarker(currentSummary) {
		return nil
	}
	_, err := tx.Exec(`UPDATE news SET summary_text=?,updated_at=? WHERE id=?`, recoveredSummary, now, entryID)
	return err
}

func persistEntryContentTx(tx *sql.Tx, id, sourceURL, sourceHash string, article extractedArticle, now string) error {
	imageURLsJSON, err := json.Marshal(article.ImageURLs)
	if err != nil {
		return err
	}
	blocksJSON, blocksVersion, blocksHash, blocksTruncated, err := articleBlocksJSON(article)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_html,image_urls_json,content_format,lead_image_index,fetched_at,status,error_message,source_url,updated_at,content_hash,blocks_json,blocks_version,blocks_hash,blocks_truncated)
VALUES(?,?,?,?,?,?,?,'ok','',?,?,?,?,?,?,?)
ON CONFLICT(entry_id) DO UPDATE SET content_text=excluded.content_text,content_html=excluded.content_html,
image_urls_json=excluded.image_urls_json,content_format=excluded.content_format,lead_image_index=excluded.lead_image_index,
fetched_at=excluded.fetched_at,status='ok',error_message='',source_url=excluded.source_url,updated_at=excluded.updated_at,content_hash=excluded.content_hash,
blocks_json=excluded.blocks_json,blocks_version=excluded.blocks_version,blocks_hash=excluded.blocks_hash,blocks_truncated=excluded.blocks_truncated`,
		id, article.Text, article.SafeHTML, string(imageURLsJSON), extractedArticleFormat(article), article.LeadImageIndex, now, sourceURL, now, sourceHash, blocksJSON, blocksVersion, blocksHash, boolToSQLiteInt(blocksTruncated)); err != nil {
		return err
	}
	return nil
}

func pruneEntryContentCache() {
	// Keep the durable reader cache useful without allowing every opened feed
	// item to grow the SQLite file forever. Rows are ordered by last successful
	// refresh, so a re-open naturally keeps the most useful projections. A
	// cleanup failure must not hide the freshly rendered article; the next
	// successful open retries the same bounded cleanup.
	_, _ = db.Exec(`DELETE FROM entry_full_texts
WHERE entry_id IN (
    SELECT entry_id FROM entry_full_texts
    ORDER BY updated_at DESC, entry_id DESC
    LIMIT -1 OFFSET ?
)`, maxPersistedFullTexts)
}

func nowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// serveGatewayEntryContent implements on-demand reading for gateway-managed
// entries (spec §8.4): the body comes from the local-server fulltext route,
// is re-sanitized by the existing bounded renderer, and persists into the
// same entry_full_texts cache used by every other entry.  Failures keep the
// cached projection or the feed summary plus a retry affordance; they never
// clear existing content.
func serveGatewayEntryContent(w http.ResponseWriter, r *http.Request, id, sourceURL, sourceHash, summaryText, gatewayEntryID string, cached EntryContent, cacheErr error) {
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		gatewayContentFailure(w, id, sourceURL, sourceHash, summaryText, cached, cacheErr, gatewayUnavailableNotice)
		return
	}
	fulltext, err := bridge.fulltext(r.Context(), gatewayEntryID)
	var article extractedArticle
	status := "ok"
	message := ""
	if err != nil {
		// A verified WeChat feed is also the safe recovery source when the
		// gateway rejects the whole source before returning a challenge shell.
		// Previously recovery only ran after a successful fulltext response,
		// which left blocked sources with neither readable content nor repaired
		// neighbouring thumbnails.
		recovered, recoverErr := recoverVerifiedWeChatGatewayArticle(r.Context(), id, sourceURL)
		if recoverErr != nil {
			gatewayContentFailure(w, id, sourceURL, sourceHash, summaryText, cached, cacheErr, noticeForGatewayError(err))
			return
		}
		article = recovered
		status = "stale"
		message = "云端正文抓取降级，显示可信订阅内容"
	} else {
		article = gatewayArticleProjection(fulltext, sourceURL)
		if containsBlockedArticleMarker(article.Text+"\n"+article.SafeHTML) || strings.TrimSpace(article.Text) == "" && strings.TrimSpace(article.SafeHTML) == "" {
			recovered, recoverErr := recoverVerifiedWeChatGatewayArticle(r.Context(), id, sourceURL)
			if recoverErr != nil {
				message := "云端正文返回验证页"
				if strings.TrimSpace(article.Text) == "" && strings.TrimSpace(article.SafeHTML) == "" {
					message = "云端没有返回可读正文"
				}
				gatewayContentFailure(w, id, sourceURL, sourceHash, summaryText, cached, cacheErr, message)
				return
			}
			article = recovered
		}
		if fulltext.Degraded {
			status = "stale"
			message = "云端正文抓取降级，显示已有内容"
		}
	}
	now := nowUTC()
	if err := persistEntryContent(id, sourceURL, sourceHash, article, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	article = finalizeArticleBlocks(article)
	app.WriteJSON(w, http.StatusOK, EntryContent{EntryID: id, ContentHash: sourceHash, ContentText: article.Text, ContentHTML: article.SafeHTML, ContentFormat: extractedArticleFormat(article), Blocks: article.Blocks, BlocksVersion: article.BlocksVersion, BlocksHash: article.BlocksHash, BlocksTruncated: article.BlocksTruncated, ImageCount: len(article.ImageURLs), LeadImageIndex: article.LeadImageIndex, FetchedAt: now, Status: status, ErrorMessage: message, SourceURL: sourceURL, Cached: false})
}

// recoverVerifiedWeChatGatewayArticle restores the rich content already
// present in the maintained RSS item when WeChat blocks the gateway's
// single-page fetch. Both the feed and article identities are pinned and
// verified before any HTML or image URL enters the reader cache.
func recoverVerifiedWeChatGatewayArticle(ctx context.Context, entryID, sourceURL string) (extractedArticle, error) {
	var sourceID string
	if err := db.QueryRow(`SELECT source_id FROM news WHERE id=? AND deleted_at=''`, entryID).Scan(&sourceID); err != nil {
		return extractedArticle{}, err
	}
	feedURL, ok := weChatVerifiedDirectFeedURLs[sourceID]
	if !ok {
		return extractedArticle{}, errors.New("没有可信公众号正文源")
	}
	policy, ok := weChatVerifiedDirectFeedPolicies[feedURL]
	if !ok || policy.SourceID != sourceID {
		return extractedArticle{}, errors.New("公众号正文源身份不匹配")
	}
	targetIdentity, ok := verifiedWeChatArticleIdentity(sourceURL, policy)
	if !ok {
		return extractedArticle{}, errors.New("公众号文章身份不合法")
	}
	feedCtx, cancel := context.WithTimeout(ctx, weChatVerifiedFeedBudget)
	defer cancel()
	response, err := entryContentFetchVerifiedWeChatFeed(feedCtx, feedURL, "", "")
	if err != nil {
		return extractedArticle{}, err
	}
	feed, err := parseDirectWechatRSSFeed(response.Body, feedURL, policy.SourceName, "", sourceURL)
	if err != nil {
		return extractedArticle{}, err
	}
	if err := persistRecoveredVerifiedWeChatFeed(sourceID, feed); err != nil {
		return extractedArticle{}, err
	}
	for _, item := range feed.Items {
		identity, valid := verifiedWeChatArticleIdentity(item.URL, policy)
		if !valid || identity != targetIdentity {
			continue
		}
		article := finalizeArticleBlocks(item.feedArticle)
		if strings.TrimSpace(article.Text) == "" || strings.TrimSpace(article.SafeHTML) == "" {
			return extractedArticle{}, errors.New("可信订阅源没有返回富文本正文")
		}
		return article, nil
	}
	return extractedArticle{}, errors.New("可信订阅源中没有找到当前文章")
}

// persistRecoveredVerifiedWeChatFeed updates the bounded reader cache for the
// whole verified feed window. A single successful recovery therefore repairs
// the neighbouring list thumbnails and avoids repeating the same large feed
// download as the user opens the next article.
func persistRecoveredVerifiedWeChatFeed(sourceID string, feed fetchedFeed) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowUTC()
	for _, item := range feed.Items {
		article := finalizeArticleBlocks(item.feedArticle)
		if strings.TrimSpace(article.Text) == "" || strings.TrimSpace(article.SafeHTML) == "" {
			continue
		}
		var entryID, sourceURL, contentHash, currentSummary string
		if err := tx.QueryRow(`SELECT id,url,content_hash,summary_text FROM news
WHERE source_id=? AND (external_id=? OR url=?) AND deleted_at=''
ORDER BY CASE WHEN external_id=? THEN 0 ELSE 1 END,id LIMIT 1`, sourceID, item.ExternalID, item.URL, item.ExternalID).Scan(&entryID, &sourceURL, &contentHash, &currentSummary); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return err
		}
		if err := persistEntryContentTx(tx, entryID, sourceURL, contentHash, article, now); err != nil {
			return err
		}
		thumbnailURL := article.LeadImageURL
		if thumbnailURL == "" && len(article.ImageURLs) > 0 {
			thumbnailURL = article.ImageURLs[0]
		}
		if thumbnailURL != "" {
			if _, err := tx.Exec(`UPDATE news SET thumbnail_url=?,updated_at=? WHERE id=?`, thumbnailURL, now, entryID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(currentSummary) == "" || containsBlockedArticleMarker(currentSummary) {
			recoveredSummary := limitText(firstNonEmpty(item.SummaryText, article.Text), 1200)
			if recoveredSummary != "" && !containsBlockedArticleMarker(recoveredSummary) {
				if _, err := tx.Exec(`UPDATE news SET summary_text=?,updated_at=? WHERE id=?`, recoveredSummary, now, entryID); err != nil {
					return err
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pruneEntryContentCache()
	return nil
}

// gatewayArticleProjection re-sanitizes the gateway fulltext through the same
// bounded renderer used for locally fetched articles.  The gateway's recorded
// image list is authoritative for the media index space when present.
func gatewayArticleProjection(fulltext sourceGatewayFulltext, sourceURL string) extractedArticle {
	var article extractedArticle
	if strings.TrimSpace(fulltext.ContentHTML) != "" {
		article = renderEntryContent([]byte(fulltext.ContentHTML), "text/html; charset=utf-8", sourceURL, sourceURL)
	} else {
		article = extractedArticle{Text: fulltext.ContentText, Format: "text", Blocks: plainArticleBlocks(fulltext.ContentText, sourceURL)}
		article = finalizeArticleBlocks(article)
	}
	if len(fulltext.Images) > 0 {
		article.ImageURLs = fulltext.Images
		if article.LeadImageURL == "" {
			article.LeadImageURL = fulltext.Images[0]
			article.LeadImageIndex = 0
		}
	}
	return article
}

func noticeForGatewayError(err error) string {
	if errors.Is(err, errSourceGatewayAuth) {
		return gatewayAuthNotice
	}
	return gatewayUnavailableNotice
}

func gatewayContentFailure(w http.ResponseWriter, id, sourceURL, sourceHash, summaryText string, cached EntryContent, cacheErr error, message string) {
	_ = writeEntryContentFailure(id, sourceURL, sourceHash, message)
	if cacheErr == nil && entryContentCacheUsable(cached, sourceURL, sourceHash) && !entryContentProjectionLooksBlocked(cached) {
		cached.Cached = true
		cached.Status = "stale"
		cached.ErrorMessage = limitText(message, 500)
		app.WriteJSON(w, http.StatusOK, cached)
		return
	}
	if strings.TrimSpace(summaryText) != "" && !containsBlockedArticleMarker(summaryText) {
		app.WriteJSON(w, http.StatusOK, EntryContent{
			EntryID: id, ContentHash: sourceHash, ContentText: summaryText,
			ContentFormat: "text", FetchedAt: nowUTC(), Status: "summary",
			ErrorMessage: limitText(message, 500), SourceURL: sourceURL, Cached: false,
		})
		return
	}
	app.UserErr(w, http.StatusBadGateway, "云端正文暂时读取失败")
}

func articleResponseLooksBlocked(sourceURL string, body []byte, article extractedArticle) bool {
	parsed, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host != "mp.weixin.qq.com" && host != "wechat2rss.xlab.app" && !strings.HasSuffix(host, ".mmbiz.qpic.cn") {
		return false
	}
	return containsBlockedArticleMarker(string(body) + "\n" + article.Text)
}

func entryContentProjectionLooksBlocked(content EntryContent) bool {
	return containsBlockedArticleMarker(content.ContentText + "\n" + content.ContentHTML)
}

func containsBlockedArticleMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"参数错误", "链接已过期", "请在微信客户端打开", "环境异常", "访问过于频繁", "页面不存在", "完成验证后"} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func extractedArticleFormat(article extractedArticle) string {
	if article.Format == "markdown" {
		return "markdown"
	}
	if strings.TrimSpace(article.SafeHTML) != "" {
		return "html"
	}
	return "text"
}

func entryContentCacheUsable(cached EntryContent, sourceURL, sourceHash string) bool {
	if cached.ContentText == "" || cached.SourceURL != sourceURL {
		return false
	}
	// Rows written before the content-hash migration remain readable when the
	// source URL is unchanged. Current rows must match the entry revision.
	return cached.ContentHash == sourceHash || cached.ContentHash == ""
}

func requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), feedFetchTimeout)
}

func readEntryContent(id string) (EntryContent, error) {
	var value EntryContent
	var blocksJSON string
	var blocksTruncated int
	err := db.QueryRow(`SELECT entry_id,content_text,content_html,image_urls_json,content_format,lead_image_index,fetched_at,status,error_message,source_url,updated_at,content_hash,blocks_json,blocks_version,blocks_hash,blocks_truncated FROM entry_full_texts WHERE entry_id=?`, id).Scan(
		&value.EntryID, &value.ContentText, &value.ContentHTML, &value.imageURLsJSON, &value.ContentFormat, &value.LeadImageIndex, &value.FetchedAt, &value.Status, &value.ErrorMessage, &value.SourceURL, &value.UpdatedAt, &value.ContentHash, &blocksJSON, &value.BlocksVersion, &value.BlocksHash, &blocksTruncated)
	if err == nil {
		var images []string
		if json.Unmarshal([]byte(value.imageURLsJSON), &images) == nil {
			value.ImageCount = len(images)
		}
		if blocks, decodeErr := DecodeArticleBlocks(blocksJSON); decodeErr == nil {
			value.Blocks = blocks
			value.BlocksTruncated = blocksTruncated != 0
		} else {
			// A malformed optional projection must not make the article unreadable.
			value.Blocks = []ArticleBlock{}
			value.BlocksVersion = ""
			value.BlocksHash = ""
			value.BlocksTruncated = true
		}
	}
	return value, err
}

func writeEntryContentFailure(id, sourceURL, contentHash, message string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO entry_full_texts(entry_id,content_text,content_format,fetched_at,status,error_message,source_url,updated_at,content_hash)
VALUES(?, '', 'text', '', 'unavailable', ?, ?, ?, ?)
ON CONFLICT(entry_id) DO UPDATE SET status=CASE WHEN entry_full_texts.content_text<>'' THEN 'stale' ELSE 'unavailable' END,error_message=?,source_url=?,updated_at=?,content_hash=?`, id, limitText(message, 500), sourceURL, now, contentHash, limitText(message, 500), sourceURL, now, contentHash)
	if err == nil {
		pruneEntryContentCache()
	}
	return err
}

// entryContentImageHandler proxies only image URLs previously extracted and
// persisted for a known article. Callers cannot supply an arbitrary URL.
func entryContentImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	for key, values := range r.URL.Query() {
		if (key != "id" && key != "index") || len(values) != 1 {
			app.UserErr(w, http.StatusBadRequest, "图片筛选条件不合法")
			return
		}
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if !validText(id, 128, true) || strings.ContainsAny(id, "/\\\x00\r\n") || err != nil || index < 0 || index >= maxEntryArticleImages {
		app.UserErr(w, http.StatusBadRequest, "图片标识不合法")
		return
	}
	var encoded, sourceURL, gatewayEntryID, collectionMode string
	if err := db.QueryRow(`SELECT f.image_urls_json,f.source_url,n.gateway_entry_id,COALESCE(s.collection_mode,'')
FROM entry_full_texts f
JOIN news n ON n.id=f.entry_id AND (n.content_hash=f.content_hash OR f.content_hash='')
LEFT JOIN feed_sources s ON s.id=n.source_id
WHERE f.entry_id=? AND f.status IN ('ok','stale','unavailable') AND f.content_text<>'' AND n.deleted_at='' AND (COALESCE(n.source_id,'') <> 'focus-chat-links' OR EXISTS (
  SELECT 1 FROM focus_chat_link_entries l
  WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=?
    AND l.source_url=n.url AND l.content_hash=n.content_hash
))`, id, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&encoded, &sourceURL, &gatewayEntryID, &collectionMode); errors.Is(err, sql.ErrNoRows) {
		app.UserErr(w, http.StatusNotFound, "图片不存在")
		return
	} else if err != nil {
		app.HTTPErr(w, err)
		return
	}
	var imageURLs []string
	if json.Unmarshal([]byte(encoded), &imageURLs) != nil || index >= len(imageURLs) {
		app.UserErr(w, http.StatusNotFound, "图片不存在")
		return
	}

	timer := time.NewTimer(entryImageSlotWait)
	defer timer.Stop()
	select {
	case entryImageProxySlots <- struct{}{}:
		defer func() { <-entryImageProxySlots }()
	case <-timer.C:
		app.UserErr(w, http.StatusServiceUnavailable, "图片读取繁忙，请稍后重试")
		return
	case <-r.Context().Done():
		return
	}

	verifiedFeedImage := validVerifiedWeChatFeedImageURL(imageURLs[index])
	if isGatewayManagedEntry(gatewayEntryID, collectionMode) && !verifiedFeedImage {
		if gatewayEntryID == "" {
			app.UserErr(w, http.StatusBadGateway, "云端图片暂时读取失败")
			return
		}
		// 网关条目图片只按来源记录的索引经 local-server media 路由读取，
		// 不直接请求 Miniflux/RSSHub/微信/B站/YouTube 等第三方图床。
		serveGatewayEntryImage(w, r, gatewayEntryID, index)
		return
	}
	servePersistedEntryImage(w, r, imageURLs[index], sourceURL)
}

func servePersistedEntryImage(w http.ResponseWriter, r *http.Request, imageURL, sourceURL string) {
	if !validPublicFeedURL(imageURL) {
		app.UserErr(w, http.StatusNotFound, "图片不存在")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), entryImageFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, imageURL, nil)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", feedUserAgent)
	if referer := publicRefererOrigin(sourceURL); referer != "" {
		req.Header.Set("Referer", referer)
	}
	response, err := mediaHTTPClientFactory().Do(req)
	if err != nil {
		app.UserErr(w, http.StatusBadGateway, "图片暂时读取失败")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		app.UserErr(w, http.StatusBadGateway, "图片来源暂时不可用")
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if !allowedMediaContentType(contentType, "thumbnail") {
		app.UserErr(w, http.StatusUnsupportedMediaType, "图片类型不受支持")
		return
	}
	if response.ContentLength > maxEntryImageBytes {
		app.UserErr(w, http.StatusRequestEntityTooLarge, "图片超过大小上限")
		return
	}
	w.Header().Set("Content-Type", contentType)
	// The URL is bound to an entry id and a persisted, validated image index;
	// allow the browser to reuse successful article images across renders.
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	if response.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, io.LimitReader(response.Body, maxEntryImageBytes))
}

// validVerifiedWeChatFeedImageURL accepts only the fixed image bridge shape
// emitted by the pinned WeChat feeds. The nested target must remain on the
// public WeChat image CDN; arbitrary persisted URLs never bypass the gateway.
func validVerifiedWeChatFeedImageURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" || strings.ToLower(parsed.Hostname()) != "wechat2rss.xlab.app" || strings.TrimRight(parsed.EscapedPath(), "/") != "/img-proxy" {
		return false
	}
	values := parsed.Query()
	for key, entries := range values {
		if key != "k" && key != "u" || len(entries) != 1 {
			return false
		}
	}
	targets := values["u"]
	if len(targets) != 1 {
		return false
	}
	target, err := url.Parse(targets[0])
	if err != nil || target.Scheme != "https" || target.User != nil || target.Port() != "" || target.Fragment != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	return host == "mmbiz.qpic.cn" || strings.HasSuffix(host, ".mmbiz.qpic.cn")
}

func verifiedWeChatCachedImage(entryID string, index int) (string, string, bool) {
	if index < 0 || index >= maxEntryArticleImages {
		return "", "", false
	}
	var encoded, sourceURL, sourceID string
	if err := db.QueryRow(`SELECT f.image_urls_json,f.source_url,n.source_id
FROM entry_full_texts f JOIN news n ON n.id=f.entry_id
WHERE f.entry_id=? AND f.content_text<>'' AND f.status IN ('ok','stale')`, entryID).Scan(&encoded, &sourceURL, &sourceID); err != nil {
		return "", "", false
	}
	if _, ok := weChatVerifiedDirectFeedURLs[sourceID]; !ok {
		return "", "", false
	}
	var images []string
	if json.Unmarshal([]byte(encoded), &images) != nil || index >= len(images) || !validVerifiedWeChatFeedImageURL(images[index]) {
		return "", "", false
	}
	return images[index], sourceURL, true
}

// serveGatewayEntryImage streams one gateway-recorded image through the
// local-server media route.  A failure is scoped to this single image: the
// article text and the remaining images are untouched (spec §8.4).
func serveGatewayEntryImage(w http.ResponseWriter, r *http.Request, gatewayEntryID string, index int) {
	err := streamGatewayEntryImage(w, r, gatewayEntryID, index)
	if err == nil {
		return
	}
	if errors.Is(err, errSourceGatewayNotFound) {
		app.UserErr(w, http.StatusNotFound, "图片不存在")
		return
	}
	app.UserErr(w, http.StatusBadGateway, "云端图片暂时读取失败")
}

// streamGatewayEntryImage writes only successful image responses and returns
// the typed failure to the caller. The list-thumbnail route uses this to turn
// a known-missing optional cover into a cacheable empty response without
// weakening the article-body image contract, which still reports 404.
func streamGatewayEntryImage(w http.ResponseWriter, r *http.Request, gatewayEntryID string, index int) error {
	bridge := newSourceGatewayBridgeFromEnv()
	if bridge.err != nil {
		return bridge.err
	}
	media, err := bridge.media(r.Context(), gatewayEntryID, index, r.Method)
	if err != nil {
		return err
	}
	defer media.Body.Close()
	w.Header().Set("Content-Type", media.ContentType)
	// The index space is bound to the entry's recorded image list, so browsers
	// may reuse a fetched image across renders.
	w.Header().Set("Cache-Control", "public, max-age=86400, stale-while-revalidate=604800")
	if media.Length >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(media.Length, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return nil
	}
	_, _ = io.Copy(w, io.LimitReader(media.Body, sourceGatewayMaxMediaBytes))
	return nil
}

func publicRefererOrigin(raw string) string {
	if !validPublicFeedURL(raw) {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host + "/"
}

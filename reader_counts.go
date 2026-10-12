package main

import (
	"context"
	"errors"
	"focus-space/internal/app"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// readerCountsQueryTimeout bounds the lifetime of the SQLite connection
	// used by the independent counts projection.  The browser gives this
	// request a five-second budget; keeping the database budget a little
	// shorter leaves room for JSON encoding and transport overhead.
	readerCountsQueryTimeout = 4 * time.Second

	// readerCountsMaxSourceRows is a hard upper bound for the per-source
	// projection.  Source ids are user/content controlled, so never let an
	// unexpectedly large catalog grow an in-memory response without bound.
	// The normal catalog is well below this (currently dozens of sources), and
	// the extra row fetched below lets us report truncation explicitly.
	readerCountsMaxSourceRows = 512
)

// readerVisibleEntryPredicateLegacy is used while an older database is being
// upgraded and migration 098 has not created the Chat-link ownership table
// yet. SQLite resolves table names when preparing a statement, so putting an
// optional table reference behind an OR is not sufficient for compatibility.
// Keep the pre-098 visibility contract (deleted/dismissed filtering) until the
// next migration makes the scoped predicate available. Focus-chat rows are
// hidden while unscoped: without an ownership table there is no safe way to
// prove which profile may read one.
const readerVisibleEntryPredicateLegacy = `
n.deleted_at = ''
AND NOT EXISTS (
    SELECT 1 FROM news_preferences np
    WHERE np.news_id = n.id AND np.dismissed = 1
)
AND COALESCE(n.source_id, '') <> 'focus-chat-links'`

const readerVisibleEntryPredicate = `
n.deleted_at = ''
AND NOT EXISTS (
    SELECT 1 FROM news_preferences np
    WHERE np.news_id = n.id AND np.dismissed = 1
)
AND (
    COALESCE(n.source_id, '') <> 'focus-chat-links'
    OR EXISTS (
        SELECT 1 FROM focus_chat_link_entries l
        WHERE l.entry_id = n.id
          AND l.owner_id = ?
          AND l.profile_id = ?
          AND l.source_url = n.url
          AND l.content_hash = n.content_hash
    )
)`

// readerVisibleEntryPredicateForDB returns the visibility SQL and the scope
// arguments required by the current schema. Migration tests (and a device
// briefly paused between migration files during upgrade) can legitimately
// execute a read before migration 098; in that state, fall back to the
// historical predicate instead of failing every source lookup with "no such
// table: focus_chat_link_entries". The schema probe is a single indexed
// sqlite_master lookup and does not retain process-global state, which is
// important because tests and profile reloads can swap the database handle.
func readerVisibleEntryPredicateForDB() (string, []any) {
	if db == nil {
		return readerVisibleEntryPredicateLegacy, nil
	}
	var exists int
	if err := db.QueryRow(`SELECT 1 FROM sqlite_master WHERE type='table' AND name='focus_chat_link_entries' LIMIT 1`).Scan(&exists); err != nil || exists != 1 {
		return readerVisibleEntryPredicateLegacy, nil
	}
	return readerVisibleEntryPredicate, []any{appDataScope.OwnerID, appDataScope.ProfileID}
}

// The dedicated video gallery is provider-scoped.  A generic RSS/article can
// carry an incidental video enclosure (or a stale view_type/media_mime), but
// it must remain in the source's normal timeline unless its YouTube/Bilibili
// identity is unambiguous.  This mirrors videoProviderForItem in the Reader
// UI and keeps gallery totals, pagination and badges on one contract.
const readerVideoEntryPredicate = `(
    lower(n.source_type) IN ('youtube', 'bilibili', 'bilibili-video')
    OR lower(COALESCE(n.source_id, '')) LIKE 'youtube-%'
    OR lower(COALESCE(n.source_id, '')) LIKE 'bilibili-%'
    OR lower(COALESCE(n.source_id, '')) LIKE 'bilibili-video-%'
    OR lower(COALESCE(n.source_id, '')) LIKE 'b23-%'
    OR lower(n.url) LIKE '%youtube.com/watch%'
    OR lower(n.url) LIKE '%youtube.com/shorts/%'
    OR lower(n.url) LIKE '%youtube.com/embed/%'
    OR lower(n.url) LIKE '%youtube.com/live/%'
    OR lower(n.url) LIKE '%youtu.be/%'
    OR lower(n.url) LIKE '%bilibili.com/video/%'
    OR lower(n.url) LIKE '%b23.tv/%'
    OR EXISTS (
        SELECT 1 FROM feed_sources vf
        WHERE vf.id = n.source_id
          AND (
              lower(vf.kind) IN ('youtube', 'bilibili', 'bilibili-video')
              OR EXISTS (
                  SELECT 1 FROM feed_catalog_entries vfc
                  WHERE vfc.source_id = vf.id
                    AND lower(COALESCE(vfc.feed_format, '')) IN ('youtube', 'bilibili', 'bilibili-video')
              )
              OR lower(vf.url) LIKE '%youtube.com/feeds/%'
              OR lower(vf.url) LIKE '%space.bilibili.com/%'
          )
    )
)`

const readerYouTubeEntryPredicate = `(
    lower(n.source_type) = 'youtube'
    OR lower(COALESCE(n.source_id, '')) LIKE 'youtube-%'
    OR lower(n.url) LIKE '%youtube.com/watch%'
    OR lower(n.url) LIKE '%youtube.com/shorts/%'
    OR lower(n.url) LIKE '%youtube.com/embed/%'
    OR lower(n.url) LIKE '%youtube.com/live/%'
    OR lower(n.url) LIKE '%youtu.be/%'
    OR EXISTS (
        SELECT 1 FROM feed_sources yf
        WHERE yf.id = n.source_id
          AND (
              lower(yf.kind) = 'youtube'
              OR EXISTS (
                  SELECT 1 FROM feed_catalog_entries yfc
                  WHERE yfc.source_id = yf.id
                    AND lower(COALESCE(yfc.feed_format, '')) = 'youtube'
              )
              OR lower(yf.url) LIKE '%youtube.com/feeds/%'
          )
    )
)`

// RSS is the complementary aggregate view to the dedicated video gallery.
// Keep this predicate in the server contract so keyset pagination advances
// over RSS rows only; filtering video rows in the browser would consume the
// cursor budget and silently skip older RSS articles when videos are dense.
const readerRSSEntryPredicate = `NOT (` + readerVideoEntryPredicate + `)`

// The picture wall aggregates image entries across sources. An entry qualifies
// when the ingestion pipeline classified it as a picture (view_type) or when
// it carries an image media attachment. A bare thumbnail_url is deliberately
// NOT enough: ordinary article thumbnails would flood the wall. Video sources
// keep their dedicated gallery, so picture entries are always the video-free
// complement here and in the Go-side count classification below.
const readerPictureEntryPredicate = `(
    (
        lower(COALESCE(n.view_type, '')) = 'picture'
        OR COALESCE(n.media_mime, '') LIKE 'image/%'
    )
    AND NOT (` + readerVideoEntryPredicate + `)
)`

type readerAggregateCounts struct {
	TotalEntries   int `json:"total_entries"`
	UnreadEntries  int `json:"unread_entries"`
	VideoEntries   int `json:"video_entries"`
	RSSEntries     int `json:"rss_entries"`
	YouTubeEntries int `json:"youtube_entries"`
	PictureEntries int `json:"picture_entries"`
}

type readerSourceCounts struct {
	SourceID      string `json:"source_id"`
	TotalEntries  int    `json:"total_entries"`
	UnreadEntries int    `json:"unread_entries"`
	VideoEntries  int    `json:"video_entries"`
}

type readerCountsResponse struct {
	readerAggregateCounts
	SourceCounts          []readerSourceCounts `json:"source_counts"`
	SourceCountsTruncated bool                 `json:"source_counts_truncated,omitempty"`
}

type readerCountClassification struct {
	videoSourceIDs      []string
	youtubeSourceIDs    []string
	videoSourceIDsSet   map[string]struct{}
	youtubeSourceIDsSet map[string]struct{}
}

// loadReaderCountClassification resolves provider metadata once per counts
// request.  The old projection ran correlated feed_sources/feed_catalog_entries
// subqueries for every news row and every aggregate, which is especially slow
// on the board's ARM SQLite build.  This bounded lookup keeps the same
// provider contract while making the row scan local to news.
func loadReaderCountClassification(ctx context.Context) (readerCountClassification, error) {
	var classification readerCountClassification
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return classification, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT fs.id, lower(COALESCE(fs.kind, '')), lower(COALESCE(fs.url, '')), lower(COALESCE(fc.feed_format, ''))
FROM feed_sources fs
LEFT JOIN feed_catalog_entries fc ON fc.source_id = fs.id
ORDER BY fs.id`)
	if err != nil {
		// Older databases can briefly exist between the source migrations.  In
		// that state local news fields and URL/source-id prefixes still provide a
		// safe classification, so continue without directory hints.
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return classification, nil
		}
		return classification, err
	}
	defer rows.Close()

	video := make(map[string]struct{})
	youtube := make(map[string]struct{})
	for rows.Next() {
		var sourceID, kind, sourceURL, feedFormat string
		if err := rows.Scan(&sourceID, &kind, &sourceURL, &feedFormat); err != nil {
			return classification, err
		}
		if sourceID == "" {
			continue
		}
		isYouTube := kind == "youtube" || feedFormat == "youtube" || strings.Contains(sourceURL, "youtube.com/feeds/")
		isBilibili := kind == "bilibili" || kind == "bilibili-video" || feedFormat == "bilibili" || feedFormat == "bilibili-video" || strings.Contains(sourceURL, "space.bilibili.com/")
		if isYouTube || isBilibili {
			video[sourceID] = struct{}{}
		}
		if isYouTube {
			youtube[sourceID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return classification, err
	}
	for sourceID := range video {
		classification.videoSourceIDs = append(classification.videoSourceIDs, sourceID)
	}
	for sourceID := range youtube {
		classification.youtubeSourceIDs = append(classification.youtubeSourceIDs, sourceID)
	}
	sort.Strings(classification.videoSourceIDs)
	sort.Strings(classification.youtubeSourceIDs)
	classification.videoSourceIDsSet = make(map[string]struct{}, len(classification.videoSourceIDs))
	classification.youtubeSourceIDsSet = make(map[string]struct{}, len(classification.youtubeSourceIDs))
	for _, sourceID := range classification.videoSourceIDs {
		classification.videoSourceIDsSet[strings.ToLower(sourceID)] = struct{}{}
	}
	for _, sourceID := range classification.youtubeSourceIDs {
		classification.youtubeSourceIDsSet[strings.ToLower(sourceID)] = struct{}{}
	}
	return classification, nil
}

func readerCountEntryClassification(sourceID, sourceType, entryURL string, classification readerCountClassification) (bool, bool) {
	sourceIDLower := strings.ToLower(sourceID)
	sourceTypeLower := strings.ToLower(sourceType)
	urlLower := strings.ToLower(entryURL)
	_, catalogVideo := classification.videoSourceIDsSet[sourceIDLower]
	_, catalogYouTube := classification.youtubeSourceIDsSet[sourceIDLower]
	isYouTube := sourceTypeLower == "youtube" || strings.HasPrefix(sourceIDLower, "youtube-") ||
		strings.Contains(urlLower, "youtube.com/watch") || strings.Contains(urlLower, "youtube.com/shorts/") ||
		strings.Contains(urlLower, "youtube.com/embed/") || strings.Contains(urlLower, "youtube.com/live/") ||
		strings.Contains(urlLower, "youtu.be/") || catalogYouTube
	isVideo := sourceTypeLower == "youtube" || sourceTypeLower == "bilibili" || sourceTypeLower == "bilibili-video" ||
		strings.HasPrefix(sourceIDLower, "youtube-") || strings.HasPrefix(sourceIDLower, "bilibili-") ||
		strings.HasPrefix(sourceIDLower, "bilibili-video-") || strings.HasPrefix(sourceIDLower, "b23-") ||
		strings.Contains(urlLower, "youtube.com/watch") || strings.Contains(urlLower, "youtube.com/shorts/") ||
		strings.Contains(urlLower, "youtube.com/embed/") || strings.Contains(urlLower, "youtube.com/live/") ||
		strings.Contains(urlLower, "youtu.be/") || strings.Contains(urlLower, "bilibili.com/video/") ||
		strings.Contains(urlLower, "b23.tv/") || catalogVideo
	return isVideo, isYouTube
}

func queryReaderRowsForClassification(ctx context.Context, classification readerCountClassification) (readerAggregateCounts, []readerSourceCounts, bool, error) {
	var aggregate readerAggregateCounts
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return aggregate, nil, false, err
	}
	visiblePredicate, visibleArgs := readerVisibleEntryPredicateForDB()
	// Keep SQLite responsible only for the indexed visibility scan and a small
	// bounded row shape. Complex provider predicates, aggregate CASEs and GROUP
	// BYs are intentionally evaluated in Go because they are disproportionately
	// expensive in the board's ARM SQLite runtime.
	rows, err := db.QueryContext(ctx, `
SELECT substr(n.source_id, 1, 256), substr(n.source_type, 1, 64), substr(n.url, 1, 2048),
       substr(COALESCE(n.view_type, ''), 1, 32), substr(COALESCE(n.media_mime, ''), 1, 128),
       CASE WHEN COALESCE(ip.is_read, 0) = 0 THEN 1 ELSE 0 END
FROM news n
LEFT JOIN item_preferences ip ON ip.item_key = 'news:' || n.id
WHERE `+visiblePredicate, visibleArgs...)
	if err != nil {
		return aggregate, nil, false, err
	}
	defer rows.Close()

	sourceCounts := make(map[string]readerSourceCounts, readerCountsMaxSourceRows)
	truncated := false
	for rows.Next() {
		var sourceID, sourceType, entryURL, viewType, mediaMIME string
		var unread int
		if err := rows.Scan(&sourceID, &sourceType, &entryURL, &viewType, &mediaMIME, &unread); err != nil {
			return aggregate, nil, false, err
		}
		aggregate.TotalEntries++
		if unread == 1 {
			aggregate.UnreadEntries++
		}
		isVideo, isYouTube := readerCountEntryClassification(sourceID, sourceType, entryURL, classification)
		if isVideo {
			aggregate.VideoEntries++
		} else {
			aggregate.RSSEntries++
			// Mirror readerPictureEntryPredicate so the picture badge, the
			// paginated wall and this independent count projection can never
			// disagree about what a picture entry is.
			if strings.EqualFold(viewType, "picture") || strings.HasPrefix(strings.ToLower(mediaMIME), "image/") {
				aggregate.PictureEntries++
			}
		}
		if isYouTube {
			aggregate.YouTubeEntries++
		}

		item, ok := sourceCounts[sourceID]
		if !ok {
			if len(sourceCounts) >= readerCountsMaxSourceRows {
				truncated = true
				continue
			}
			item = readerSourceCounts{SourceID: sourceID}
		}
		item.TotalEntries++
		if unread == 1 {
			item.UnreadEntries++
		}
		if isVideo {
			item.VideoEntries++
		}
		sourceCounts[sourceID] = item
	}
	if err := rows.Err(); err != nil {
		return aggregate, nil, false, err
	}

	sources := make([]readerSourceCounts, 0, len(sourceCounts))
	for _, item := range sourceCounts {
		sources = append(sources, item)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].SourceID < sources[j].SourceID })
	return aggregate, sources, truncated, nil
}

func queryReaderAggregateCounts() (readerAggregateCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readerCountsQueryTimeout)
	defer cancel()
	classification, err := loadReaderCountClassification(ctx)
	if err != nil {
		return readerAggregateCounts{}, err
	}
	aggregate, _, _, err := queryReaderRowsForClassification(ctx, classification)
	return aggregate, err
}

// queryReaderAggregateCountsWithContext runs the aggregate projection with a
// caller-owned context. Keeping the context-aware variant separate preserves
// the existing internal call sites while allowing HTTP handlers to cancel the
// query as soon as the client disconnects.
func queryReaderAggregateCountsWithContext(ctx context.Context) (readerAggregateCounts, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	classification, err := loadReaderCountClassification(ctx)
	if err != nil {
		return readerAggregateCounts{}, err
	}
	aggregate, _, _, err := queryReaderRowsForClassification(ctx, classification)
	return aggregate, err
}

func queryReaderSourceCounts() ([]readerSourceCounts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readerCountsQueryTimeout)
	defer cancel()
	classification, err := loadReaderCountClassification(ctx)
	if err != nil {
		return nil, err
	}
	_, sources, _, err := queryReaderRowsForClassification(ctx, classification)
	return sources, err
}

// queryReaderSourceCountsWithContext returns at most
// readerCountsMaxSourceRows rows. It bounds the source map while the row
// stream is consumed, so a large catalog never creates an unbounded response.
func queryReaderSourceCountsWithContext(ctx context.Context) ([]readerSourceCounts, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	classification, err := loadReaderCountClassification(ctx)
	if err != nil {
		return nil, false, err
	}
	_, sources, truncated, err := queryReaderRowsForClassification(ctx, classification)
	return sources, truncated, err
}

func queryReaderCountsWithContext(ctx context.Context) (readerAggregateCounts, []readerSourceCounts, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	classification, err := loadReaderCountClassification(ctx)
	if err != nil {
		return readerAggregateCounts{}, nil, false, err
	}
	return queryReaderRowsForClassification(ctx, classification)
}

func readerCountsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	if len(r.URL.Query()) != 0 {
		app.UserErr(w, http.StatusBadRequest, "计数查询参数不合法")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readerCountsQueryTimeout)
	defer cancel()
	aggregate, sources, truncated, err := queryReaderCountsWithContext(ctx)
	if err != nil {
		writeReaderCountsQueryError(w, r, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, readerCountsResponse{
		readerAggregateCounts: aggregate,
		SourceCounts:          sources,
		SourceCountsTruncated: truncated,
	})
}

// writeReaderCountsQueryError keeps a canceled client from producing a noisy
// server-side 500, and gives a bounded query timeout a retryable status.  The
// response body intentionally stays generic; SQL/context details are only
// logged by the existing httpErr path.
func writeReaderCountsQueryError(w http.ResponseWriter, r *http.Request, err error) {
	if r != nil && r.Context().Err() != nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		app.UserErr(w, http.StatusServiceUnavailable, "计数暂时不可用，请稍后重试")
		return
	}
	app.HTTPErr(w, err)
}

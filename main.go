package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"focus-space/internal/bridges"
	"io"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql static blueprints/*.json
var appFS embed.FS

const (
	embeddedAssetRevision = "20260826-v3-obsidian-assets"
	dbPath                = "data/app.db"
	maxBodyBytes          = 256 << 10
	maxItems              = 100
	minModuleHeight       = 160
	maxModuleHeight       = 960
	minModuleSpan         = 1
	maxModuleSpan         = 3
	minSidebarWidth       = 220
	maxSidebarWidth       = 360
	minTimelineWidth      = 360
	maxTimelineWidth      = 640
	shutdownTimeout       = 2 * time.Second
)

var db *sql.DB

type SourceStatus struct {
	Kind        string        `json:"kind"`
	Status      string        `json:"status"`
	RecordsSeen int           `json:"records_seen"`
	RecordsUsed int           `json:"records_used"`
	LastReadAt  string        `json:"last_read_at"`
	Message     string        `json:"message"`
	Inputs      []InputStatus `json:"inputs,omitempty"`
}

// InputStatus keeps the health of each evidence stream separate. New clients
// may render it; old clients can safely ignore the optional field.
type InputStatus struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Label       string `json:"label"`
	Status      string `json:"status"`
	RecordsSeen int    `json:"records_seen"`
	RecordsUsed int    `json:"records_used"`
	LastReadAt  string `json:"last_read_at"`
	Message     string `json:"message"`
}

type Evidence struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Locator         string `json:"locator"`
	ObservedAt      string `json:"observed_at"`
	Quote           string `json:"quote"`
	SourceType      string `json:"source_type,omitempty"`
	Content         string `json:"content,omitempty"`
	ContentFormat   string `json:"content_format,omitempty"`
	FocusID         string `json:"focus_id,omitempty"`
	AnalysisRunID   string `json:"analysis_run_id,omitempty"`
	Rank            int    `json:"rank,omitempty"`
	PromptVersion   string `json:"prompt_version,omitempty"`
	EvidenceVersion string `json:"evidence_version,omitempty"`
}
type Action struct {
	ID                  string   `json:"id"`
	Task                string   `json:"task"`
	Owner               string   `json:"owner"`
	DueAt               string   `json:"due_at"`
	SourceIDs           []string `json:"source_ids"`
	Status              string   `json:"status"`
	ConfirmationStatus  string   `json:"confirmation_status"`
	Priority            string   `json:"priority"`
	Revision            int      `json:"revision,omitempty"`
	ScheduleState       string   `json:"schedule_state,omitempty"`
	ScheduleRequestID   string   `json:"schedule_request_id,omitempty"`
	ScheduleCandidateID string   `json:"schedule_candidate_id,omitempty"`
	ClientRequestID     string   `json:"client_request_id,omitempty"`
	JobID               string   `json:"job_id,omitempty"`
	NextRunAt           string   `json:"next_run_at,omitempty"`
	ScheduleReplayed    bool     `json:"schedule_replayed,omitempty"`
	ScheduleErrorCode   string   `json:"schedule_error_code,omitempty"`
	ScheduleError       string   `json:"schedule_error,omitempty"`
	WorkflowUpdatedAt   string   `json:"workflow_updated_at,omitempty"`
}

type ReaderDigestReceipt struct {
	ContentHash string `json:"content_hash"`
	SourceID    string `json:"source_id"`
	EntryID     string `json:"entry_id"`
	ProcessedAt string `json:"processed_at"`
	Outcome     string `json:"outcome"`
}
type Metric struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Value      string   `json:"value"`
	Period     string   `json:"period"`
	Comparison string   `json:"comparison"`
	SourceIDs  []string `json:"source_ids"`
	Status     string   `json:"status"`
}
type Project struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Summary   string   `json:"summary"`
	Blockers  []string `json:"blockers"`
	SourceIDs []string `json:"source_ids"`
}
type Signal struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Detail          string   `json:"detail"`
	SourceIDs       []string `json:"source_ids"`
	Confidence      string   `json:"confidence"`
	Counterevidence []string `json:"counterevidence,omitempty"`
	Status          string   `json:"status"`
}

// MorningBrief is the bounded, device-local CEO briefing produced by the
// profile maintenance workflow. It lives in the same versioned snapshot as
// the rest of the projection so the standalone UI and Chat share one
// last-known-good result without a second persistence system.
type MorningBrief struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	GeneratedAt string   `json:"generated_at"`
	Summary     string   `json:"summary"`
	Priorities  []string `json:"priorities,omitempty"`
	Risks       []string `json:"risks,omitempty"`
	SourceIDs   []string `json:"source_ids,omitempty"`
	Status      string   `json:"status"`
}
type NewsItem struct {
	ID          string `json:"id"`
	ExternalID  string `json:"external_id,omitempty"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Source      string `json:"source"`
	SourceID    string `json:"source_id,omitempty"`
	Origin      string `json:"origin,omitempty"`
	SourceType  string `json:"source_type"`
	ViewType    string `json:"view_type,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at"`
	// UpdatedAt is optional response metadata used to keep equal-published-at
	// entries in the same order as the typed keyset cursor. Older clients may
	// ignore this additive field.
	UpdatedAt       string             `json:"updated_at,omitempty"`
	SummaryText     string             `json:"summary_text"`
	ContentFormat   string             `json:"content_format"`
	SafeHTML        string             `json:"safe_html"`
	ThumbnailURL    string             `json:"thumbnail_url,omitempty"`
	MediaURL        string             `json:"media_url,omitempty"`
	MediaMIME       string             `json:"media_mime,omitempty"`
	DurationSecs    int                `json:"duration_seconds,omitempty"`
	ContentHash     string             `json:"content_hash,omitempty"`
	FetchedAt       string             `json:"fetched_at,omitempty"`
	PermissionScope string             `json:"permission_scope,omitempty"`
	SourceLevel     string             `json:"source_level,omitempty"`
	FailureReason   string             `json:"failure_reason,omitempty"`
	AIOverview      *ArticleAIOverview `json:"ai_overview,omitempty"`
	feedArticle     extractedArticle
	historyMetadata bool
}

type ArticleAIOverview struct {
	EntryID       string   `json:"entry_id"`
	ContentHash   string   `json:"content_hash"`
	PromptVersion string   `json:"prompt_version,omitempty"`
	Status        string   `json:"status"`
	Stage         string   `json:"stage,omitempty"`
	AttemptCount  int      `json:"attempt_count,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	KeyPoints     []string `json:"key_points,omitempty"`
	Impact        string   `json:"impact,omitempty"`
	GeneratedAt   string   `json:"generated_at"`
	ErrorCode     string   `json:"error_code,omitempty"`
}
type Decision struct {
	ID             string   `json:"id"`
	Question       string   `json:"question"`
	Options        []string `json:"options"`
	Recommendation string   `json:"recommendation"`
	SourceIDs      []string `json:"source_ids"`
	Assumptions    []string `json:"assumptions"`
	Risks          []string `json:"risks"`
	Status         string   `json:"status"`
}

type ModuleLayoutState struct {
	HeightPx int `json:"height_px,omitempty"`
	Span     int `json:"span,omitempty"`
}

type ItemPreference struct {
	Key             string  `json:"key"`
	Read            bool    `json:"read"`
	Starred         bool    `json:"starred"`
	ReadLater       bool    `json:"read_later"`
	ReadingProgress float64 `json:"reading_progress"`
}

type ReaderPreferences struct {
	SidebarWidth     int    `json:"sidebar_width"`
	TimelineWidth    int    `json:"timeline_width"`
	ViewMode         string `json:"view_mode"`
	MarkReadOnScroll *bool  `json:"mark_read_on_scroll"`
}

type FeedSource struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
	IsDefault   bool   `json:"is_default"`
	Subscribed  bool   `json:"subscribed"`
	// FormerlySubscribed marks feeds the user subscribed to and then
	// unsubscribed (a feed_subscriptions row exists with enabled=0).  It is
	// the honest input for an "已退订" list: never-subscribed catalog rows
	// must not show up there.
	FormerlySubscribed bool `json:"formerly_subscribed,omitempty"`
	Status      string `json:"status"`
	RecordsSeen int    `json:"records_seen"`
	RecordsUsed int    `json:"records_used"`
	EntryCount  int    `json:"entry_count"`
	UnreadCount int    `json:"unread_count"`
	LastSyncAt  string `json:"last_sync_at"`
	// GatewayLastSuccessAt is the server-owned freshness watermark. It is
	// separate from LastSyncAt, which records the last local projection write.
	GatewayLastSuccessAt string   `json:"last_success_at,omitempty"`
	GatewayCachedCount   int      `json:"cached_entry_count,omitempty"`
	LastError            string   `json:"last_error,omitempty"`
	SiteURL              string   `json:"site_url,omitempty"`
	Category             string   `json:"category,omitempty"`
	Language             string   `json:"language,omitempty"`
	IconText             string   `json:"icon_text,omitempty"`
	IconColor            string   `json:"icon_color,omitempty"`
	IconURL              string   `json:"-"`
	PortraitURL          string   `json:"portrait_url,omitempty"`
	HasIcon              bool     `json:"has_icon,omitempty"`
	CatalogTags          []string `json:"catalog_tags,omitempty"`
	ContentTypes         []string `json:"content_types,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	FeedFormat           string   `json:"feed_format,omitempty"`
	Recommended          bool     `json:"recommended"`
	Popularity           int      `json:"popularity"`
	ValidationStatus     string   `json:"validation_status,omitempty"`
	ValidatedAt          string   `json:"validated_at,omitempty"`
	CatalogOrigin        string   `json:"catalog_origin,omitempty"`
	FolderID             string   `json:"folder_id,omitempty"`
	FolderName           string   `json:"folder_name,omitempty"`
	TitleOverride        string   `json:"title_override,omitempty"`
	Pinned               bool     `json:"pinned"`
	Muted                bool     `json:"muted"`
	SortOrder            int      `json:"sort_order"`
	CollectionMode       string   `json:"collection_mode"`
	SyncIntervalSecs     int      `json:"sync_interval_seconds"`
	NextSyncAt           string   `json:"next_sync_at,omitempty"`
	BackfillDays         int      `json:"backfill_days"`
	RetentionDays        int      `json:"retention_days"`
	RetentionLimit       int      `json:"retention_limit"`
	AutoExtract          bool     `json:"auto_extract"`
	Hidden               bool     `json:"hidden"`
	ETag                 string   `json:"-"`
	LastModified         string   `json:"-"`
	BackfillCursor       string   `json:"-"`
	// Gateway projection (Source Gateway V1): cloud catalog state, the opaque
	// history cursor and whether the cloud reports more pages than the bounded
	// local window currently holds.  Empty/zero for app/agent-collected rows.
	GatewayStatus  string `json:"gateway_status,omitempty"`
	GatewayCursor  string `json:"-"`
	GatewayHasMore int    `json:"gateway_has_more,omitempty"`
}

type TranscriptSegment struct {
	Speaker string  `json:"speaker"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
}

type Recording struct {
	ID                  string              `json:"id"`
	Title               string              `json:"title"`
	Status              string              `json:"status"`
	DurationSecs        int                 `json:"duration_seconds"`
	StartedAt           string              `json:"started_at"`
	CreatedAt           string              `json:"created_at"`
	UpdatedAt           string              `json:"updated_at"`
	SpeakerCount        int                 `json:"speaker_count"`
	AudioRef            string              `json:"audio_ref,omitempty"`
	Transcript          string              `json:"transcript,omitempty"`
	TranscriptTruncated bool                `json:"transcript_truncated,omitempty"`
	Segments            []TranscriptSegment `json:"segments,omitempty"`
	SegmentsTruncated   bool                `json:"segments_truncated,omitempty"`
	SpeakerNames        map[string]string   `json:"speaker_names,omitempty"`
	Summary             string              `json:"summary,omitempty"`
	PerSpeakerSummary   string              `json:"per_speaker_summary,omitempty"`
	Report              string              `json:"report,omitempty"`
	ActionIDs           []string            `json:"action_ids,omitempty"`
	Read                bool                `json:"read,omitempty"`
	Starred             bool                `json:"starred,omitempty"`
	PlaybackSeconds     float64             `json:"playback_seconds,omitempty"`
}

type RecordingPreference struct {
	RecordingID     string  `json:"recording_id"`
	Read            bool    `json:"read"`
	Starred         bool    `json:"starred"`
	PlaybackSeconds float64 `json:"playback_seconds"`
}

type sourceMutationRequest struct {
	ID               string  `json:"id,omitempty"`
	Kind             string  `json:"kind,omitempty"`
	Name             string  `json:"name,omitempty"`
	URL              string  `json:"url,omitempty"`
	Description      string  `json:"description,omitempty"`
	CollectionMode   *string `json:"collection_mode,omitempty"`
	SyncIntervalSecs *int    `json:"sync_interval_seconds,omitempty"`
	BackfillDays     *int    `json:"backfill_days,omitempty"`
	RetentionDays    *int    `json:"retention_days,omitempty"`
	RetentionLimit   *int    `json:"retention_limit,omitempty"`
	AutoExtract      *bool   `json:"auto_extract,omitempty"`
	Hidden           *bool   `json:"hidden,omitempty"`
	allowCatalogEdit bool
}

const (
	catalogSourceRequiresStableIDMessage = "该地址属于维护目录来源，请使用目录中的稳定来源标识"
	legacyLocalCollectionDisabledMessage = "该来源已停止本地采集，已保留缓存内容"
)

type subscriptionMutationRequest struct {
	SourceID      string  `json:"source_id"`
	Enabled       *bool   `json:"enabled,omitempty"`
	FolderID      *string `json:"folder_id,omitempty"`
	TitleOverride *string `json:"title_override,omitempty"`
	Pinned        *bool   `json:"pinned,omitempty"`
	Muted         *bool   `json:"muted,omitempty"`
	SortOrder     *int    `json:"sort_order,omitempty"`
}

type syncRequest struct {
	SourceID string `json:"source_id,omitempty"`
}

type recordingPreferenceRequest struct {
	RecordingID     string   `json:"recording_id"`
	Read            *bool    `json:"read,omitempty"`
	Starred         *bool    `json:"starred,omitempty"`
	PlaybackSeconds *float64 `json:"playback_seconds,omitempty"`
}

type recordingActionRequest struct {
	RecordingID string `json:"recording_id"`
	ActionID    string `json:"action_id"`
}

type Dashboard struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Source        SourceStatus  `json:"source"`
	Evidence      []Evidence    `json:"evidence"`
	Actions       []Action      `json:"actions"`
	Metrics       []Metric      `json:"metrics"`
	Projects      []Project     `json:"projects"`
	Signals       []Signal      `json:"signals"`
	MorningBrief  *MorningBrief `json:"morning_brief,omitempty"`
	News          []NewsItem    `json:"news"`
	// These fields are always emitted, including explicit zeroes on the
	// lightweight (include_counts=0) response.  That prevents a consumer that
	// merges dashboard snapshots from retaining a stale count from a previous
	// read; /api/reader/counts remains the authoritative independent read model.
	TotalEntries int `json:"total_entries"`
	// VideoEntries is an optional aggregate for the Folo-style Videos view.
	// Older clients ignore it while newer clients can render the full count
	// without treating the dashboard's first 100 news rows as the total.
	VideoEntries       int                   `json:"video_entries"`
	// PictureEntries is the same optional aggregate for the picture wall.
	PictureEntries     int                   `json:"picture_entries"`
	UnreadEntries      int                   `json:"unread_entries"`
	Decisions          []Decision            `json:"decisions"`
	Sources            []FeedSource          `json:"sources,omitempty"`
	Discoveries        []FeedDiscovery       `json:"discoveries,omitempty"`
	Recordings         []Recording           `json:"recordings,omitempty"`
	ReaderDigests      []ReaderDigestReceipt `json:"reader_digest_receipts,omitempty"`
	ArticleAIOverviews []ArticleAIOverview   `json:"article_ai_overviews,omitempty"`
	// ReaderFocusClaimID is an optional, device-local reservation echoed by
	// reader_digest.apply. It is never persisted into the dashboard projection.
	ReaderFocusClaimID    string                           `json:"reader_focus_claim_id,omitempty"`
	ReaderFocus           *ReaderFocusProjection           `json:"reader_focus,omitempty"`
	ReaderRecommendations *ReaderRecommendationsProjection `json:"reader_recommendations,omitempty"`
	FocusSpace            *FocusSpace                      `json:"focus_space,omitempty"`
	FocusAnalysis         *focusAnalysisGetResponse        `json:"focus_analysis,omitempty"`
	HiddenModules         []string                         `json:"hidden_modules,omitempty"`
	ModuleLayout          map[string]ModuleLayoutState     `json:"module_layout,omitempty"`
}

var dashboardModuleIDs = map[string]struct{}{
	"actions":       {},
	"metrics":       {},
	"projects":      {},
	"signals":       {},
	"morning_brief": {},
	"news":          {},
	"decisions":     {},
	"sources":       {},
	"recordings":    {},
	"reader_focus":  {},
	"focus_space":   {},
}

var defaultFeedSources = []FeedSource{
	{ID: "openai-news", Kind: "rss", Name: "OpenAI News", URL: "https://openai.com/news/rss.xml", Description: "OpenAI 官方新闻更新", IsDefault: true},
	{ID: "hacker-news", Kind: "api", Name: "Hacker News AI", URL: hackerNewsAISearchURL, Description: "Hacker News 的 AI、模型与智能体文章", IsDefault: true},
	{ID: "sspai", Kind: "rss", Name: "少数派", URL: "https://sspai.com/feed", Description: "中文科技与效率内容", IsDefault: true},
	{ID: "bilibili-geekerwan", Kind: "api", Name: "极客湾Geekerwan", URL: "https://space.bilibili.com/25876945/video", Description: "科技数码、手机、芯片与 AI 硬件实测", IsDefault: true},
	{ID: "bilibili-tech-aesthetics", Kind: "api", Name: "科技美学", URL: "https://space.bilibili.com/3766866/video", Description: "数码产品、系统与 AI 体验", IsDefault: true},
	{ID: "bilibili-yingshijufeng", Kind: "api", Name: "影视飓风", URL: "https://space.bilibili.com/946974/video", Description: "影像技术、创作工具与 AI 视频实践", IsDefault: true},
	{ID: "bilibili-machine-heart", Kind: "api", Name: "机器之心官方", URL: "https://space.bilibili.com/73414544/video", Description: "人工智能研究、模型与产业动态", IsDefault: true},
	{ID: "bilibili-qbit", Kind: "api", Name: "量子位", URL: "https://space.bilibili.com/673779175/video", Description: "AI、机器人、芯片与前沿科技报道", IsDefault: true},
	{ID: "bilibili-geekpark", Kind: "api", Name: "极客公园", URL: "https://space.bilibili.com/13260662/video", Description: "科技产品、创新公司与 AI 趋势", IsDefault: true},
	{ID: "bilibili-hardware-tea", Kind: "api", Name: "硬件茶谈", URL: "https://space.bilibili.com/14871346/video", Description: "硬件评测、装机与计算平台科普", IsDefault: true},
	{ID: "bilibili-he-tongxue", Kind: "api", Name: "老师好我叫何同学", URL: "https://space.bilibili.com/163637592/video", Description: "科技产品、创意硬件与技术实验", IsDefault: true},
	{ID: "bilibili-li-mu-ai", Kind: "api", Name: "跟李沐学AI", URL: "https://space.bilibili.com/1567748478/video", Description: "开源项目、机器学习与 AI 实践", IsDefault: true},
	{ID: "bilibili-ai-tech-review", Kind: "api", Name: "AI科技评论", URL: "https://space.bilibili.com/216720985/video", Description: "人工智能研究、产品与产业评论", IsDefault: true},
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--reader-maintenance-read":
			if err := openReaderMaintenanceDB(); err != nil {
				log.Printf("维护数据库打开失败: %v", err)
				os.Exit(1)
			}
			if err := runReaderMaintenanceRead(os.Stdout); err != nil {
				log.Printf("维护读取失败: %v", err)
				_ = db.Close()
				os.Exit(1)
			}
			_ = db.Close()
			return
		case "--reader-maintenance-apply":
			if err := openReaderMaintenanceDB(); err != nil {
				log.Printf("维护数据库打开失败: %v", err)
				os.Exit(1)
			}
			if err := runReaderMaintenanceApply(os.Stdin, os.Stdout); err != nil {
				log.Printf("维护写入失败: %v", err)
				_ = db.Close()
				os.Exit(1)
			}
			_ = db.Close()
			return
		}
	}
	if appDataScopeErr != nil {
		log.Fatal(appDataScopeErr)
	}
	addr, err := listenAddr()
	if err != nil {
		log.Fatal(err)
	}
	if err := validateStaticDirOverride(); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll("data", 0o755); err != nil {
		log.Fatal(err)
	}
	db, err = openDB()
	if err != nil {
		log.Fatal(err)
	}
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}
	if err := recoverArticleOverviewJobs(); err != nil {
		log.Fatal(err)
	}
	feedSyncRuntimeEnabled = true
	feedSyncQueue = make(chan string, 32)
	articleOverviewJobWake = make(chan struct{}, 1)
	syncCtx, cancelSync := context.WithCancel(context.Background())
	var syncWG sync.WaitGroup
	syncWG.Add(4)
	go func() {
		defer syncWG.Done()
		runFeedSyncWorker(syncCtx)
	}()
	go func() {
		defer syncWG.Done()
		// Keep the first source projection repair off the HTTP startup path. This
		// is metadata-only and bounded; article hydration remains on-demand when
		// the user opens a source, including when lazy sync is enabled.
		runSourceGatewayStartupReconcile(syncCtx)
	}()
	go func() {
		defer syncWG.Done()
		runFeedSyncScheduler(syncCtx)
	}()
	go func() {
		defer syncWG.Done()
		runArticleOverviewWorker(syncCtx)
	}()

	mux := http.NewServeMux()
	appDataOperationRoutes(mux)
	bridges.RegisterRoutes(mux)
	focusDecisionTurnRoutes(mux)
	mux.HandleFunc("/api/dashboard", dashboardHandler)
	mux.HandleFunc("/api/capabilities", capabilityHandler)
	mux.HandleFunc("/api/settings", settingsHandler)
	mux.HandleFunc("/api/entries", entryHandler)
	mux.HandleFunc("/api/entries/ai-overview", articleAIOverviewHandler)
	mux.HandleFunc("/api/entries/content", entryContentHandler)
	mux.HandleFunc("/api/entries/content/image", entryContentImageHandler)
	mux.HandleFunc("/api/focus-assets", focusAssetHandler)
	mux.HandleFunc("/api/media", mediaProxyHandler)
	mux.HandleFunc("/api/actions", actionPreferenceHandler)
	mux.HandleFunc("/api/actions/feedback", actionFeedbackHandler)
	mux.HandleFunc("/api/actions/schedule/begin", actionScheduleBeginHandler)
	mux.HandleFunc("/api/actions/schedule/receipt", actionScheduleReceiptHandler)
	mux.HandleFunc("/api/signals", signalPreferenceHandler)
	mux.HandleFunc("/api/news", newsPreferenceHandler)
	mux.HandleFunc("/api/modules", modulePreferenceHandler)
	mux.HandleFunc("/api/layout", moduleLayoutHandler)
	mux.HandleFunc("/api/items", itemPreferenceHandler)
	mux.HandleFunc("/api/context", stableContextHandler)
	mux.HandleFunc("/api/reader", readerPreferenceHandler)
	mux.HandleFunc("/api/reader/counts", readerCountsHandler)
	mux.HandleFunc("/api/reader-focus", readerFocusHandler)
	mux.HandleFunc("/api/reader-recommendations", readerRecommendationReadHandler)
	mux.HandleFunc("/api/sources", sourceHandler)
	mux.HandleFunc("/api/sources/icon", sourceIconHandler)
	mux.HandleFunc("/api/sources/opml", sourceOPMLHandler)
	mux.HandleFunc("/api/sources/preview", sourcePreviewHandler)
	mux.HandleFunc("/api/subscriptions", subscriptionHandler)
	mux.HandleFunc("/api/subscriptions/bulk", bulkSubscriptionHandler)
	mux.HandleFunc("/api/folders", folderHandler)
	mux.HandleFunc("/api/tags", tagHandler)
	mux.HandleFunc("/api/packs", sourcePackHandler)
	mux.HandleFunc("/api/discoveries", discoveryHandler)
	mux.HandleFunc("/api/sync", syncHandler)
	mux.HandleFunc("/api/source-gateway/status", sourceGatewayStatusHandler)
	mux.HandleFunc("/api/items/bulk", bulkItemPreferenceHandler)
	mux.HandleFunc("/api/source-display", sourceDisplayPrefHandler)
	mux.HandleFunc("/api/recordings", recordingsHandler)
	mux.HandleFunc("/api/recordings/detail", recordingDetailHandler)
	mux.HandleFunc("/api/recordings/preferences", recordingPreferenceHandler)
	mux.HandleFunc("/api/recordings/actions", recordingActionHandler)
	staticFS, _ := staticAssetsFS()
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The dashboard is shipped as a single embedded binary. Reusing an older
		// JS/CSS response after an application upgrade can mix two incompatible UI
		// revisions, so the browser must revalidate the embedded assets.
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		staticHandler.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/", indexHandler)
	srv := &http.Server{Addr: addr, Handler: withLog(mux), ReadHeaderTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("服务异常: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop
	signal.Stop(stop)
	cancelSync()
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	_ = srv.Shutdown(ctx)
	cancel()
	// The feed and overview workers own long-lived network requests. Wait for
	// their cancellation before closing SQLite so a late response cannot
	// race a database close and turn a clean AppHost stop into a corrupting
	// write. The bound keeps supervisor shutdown finite even if an unexpected
	// third-party transport ignores cancellation.
	workersDone := make(chan struct{})
	go func() {
		syncWG.Wait()
		close(workersDone)
	}()
	select {
	case <-workersDone:
	case <-time.After(shutdownTimeout):
		log.Printf("来源同步 worker 未在关闭窗口内退出")
	}
	feedSyncRuntimeEnabled = false
	_ = db.Close()
}

func listenAddr() (string, error) {
	addr := strings.TrimSpace(os.Getenv("ZAPP_LISTEN_ADDR"))
	if addr == "" {
		return "", errors.New("ZAPP_LISTEN_ADDR 未设置")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", errors.New("ZAPP_LISTEN_ADDR 不是合法地址")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("应用只允许监听 loopback 地址")
	}
	return addr, nil
}

func openDB() (*sql.DB, error) {
	conn, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(4)
	conn.SetMaxIdleConns(4)
	conn.SetConnMaxIdleTime(5 * time.Minute)
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func migrate(conn *sql.DB) error {
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := conn.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}
	// Some development builds recorded 074 before every expected column was
	// present. Repair that shape before a later migration references it.
	if current >= 74 {
		if err := reconcileSourceGatewaySchema(conn); err != nil {
			return err
		}
	}
	entries, err := fs.ReadDir(appFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil || version <= current {
			continue
		}
		body, err := appFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := conn.Begin()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(body)); err == nil {
			_, err = tx.Exec(`INSERT INTO schema_version(version, applied_at) VALUES (?, ?)`, version, time.Now().UTC().Format(time.RFC3339))
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return reconcileSourceGatewaySchema(conn)
}

const sourceGatewayMaintainedSourceIDsJSON = `[
  "wechat-aiera","wechat-jiqizhixin","wechat-geekpark","wechat-qbitai",
  "bilibili-tech-aesthetics","bilibili-yingshijufeng","bilibili-geekerwan","bilibili-hardware-tea",
  "youtube-anthropic","youtube-google-deepmind","youtube-nvidia","youtube-fireship",
  "elon-musk-x","sam-altman-x","andrej-karpathy-x","demis-hassabis-x","tibo-sottiaux-x"
]`

const localStandardFeedSourceIDsJSON = `[
  "openai-news","sspai","arxiv-ai","hkex-regulatory-announcements"
]`

const sourceGatewayDecommissionedSourceIDsJSON = `[
  "wechat-ali-tech","wechat-chaping","wechat-paperweekly",
  "bilibili-machine-heart","bilibili-qbit","bilibili-geekpark","bilibili-he-tongxue","bilibili-li-mu-ai","bilibili-ai-tech-review",
  "youtube-two-minute-papers","youtube-ai-explained","youtube-computerphile","youtube-lex-fridman",
  "dario-amodei-x","donald-trump-x","jensen-huang-x","satya-nadella-x",
  "eurlex-legislation","china-policy-library","cninfo-disclosures","crossref-research","epo-publication-server",
  "govinfo-federal-register","hacker-news","sec-xbrl-filings","sse-periodic-reports"
]`

const sourceGatewayRetiredSourceIDsJSON = `[
  "cnipa-patent-data","wipo-patentscope","wechat-ai-paper","wechat-fnos",
  "wechat-huxiu","wechat-founder-park","wechat-liuxiaopai"
]`

// reconcileSourceGatewaySchema 修复中间态 074：测试期装过中间包会把
// schema_version 标到 74 但只落了部分列（如 collection_mode），完整版 074
// 因版本号已存在而被跳过，导致 gateway_* 列/表缺失、阅读器全面 500。
// 这里按 PRAGMA 实际结果幂等补齐（可重复执行，对完整应用 074 的库零影响）。
func reconcileSourceGatewaySchema(conn *sql.DB) error {
	hasColumn := func(table, col string) (bool, error) {
		rows, err := conn.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notnull, pk int
			var dflt sql.NullString
			if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
				return false, err
			}
			if name == col {
				return true, nil
			}
		}
		return false, rows.Err()
	}
	adds := []struct{ table, col, def string }{
		{"news", "gateway_entry_id", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "etag", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "last_modified", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "gateway_status", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "gateway_cursor", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "gateway_has_more", `INTEGER NOT NULL DEFAULT 0 CHECK (gateway_has_more IN (0, 1))`},
		{"feed_sources", "gateway_enabled", `INTEGER NOT NULL DEFAULT 0 CHECK (gateway_enabled IN (0, 1))`},
		{"feed_sources", "gateway_last_success_at", `TEXT NOT NULL DEFAULT ''`},
		{"feed_sources", "gateway_cached_entry_count", `INTEGER NOT NULL DEFAULT 0`},
	}
	for _, a := range adds {
		ok, err := hasColumn(a.table, a.col)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := conn.Exec(`ALTER TABLE ` + a.table + ` ADD COLUMN ` + a.col + ` ` + a.def); err != nil {
				return err
			}
		}
	}
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS source_gateway_subscription_outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		operation_id TEXT NOT NULL UNIQUE,
		source_id TEXT NOT NULL REFERENCES feed_sources(id) ON DELETE CASCADE,
		enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
		attempt_count INTEGER NOT NULL DEFAULT 0,
		next_retry_at TEXT NOT NULL DEFAULT '',
		last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if _, err := conn.Exec(`CREATE INDEX IF NOT EXISTS idx_source_gateway_subscription_outbox_due
		ON source_gateway_subscription_outbox(next_retry_at, id)`); err != nil {
		return err
	}
	if _, err := conn.Exec(`CREATE INDEX IF NOT EXISTS idx_news_gateway_entry
		ON news(gateway_entry_id) WHERE gateway_entry_id <> ''`); err != nil {
		return err
	}
	// 074/092/099/102 的数据订正（幂等：重复执行结果不变）。17 个兼容源
	// 只经 Source Gateway，4 个标准 Feed 在应用内有界采集，其余旧目录
	// 来源保留缓存和用户状态但停止采集。
	if _, err := conn.Exec(`UPDATE feed_sources
		SET collection_mode = 'source_gateway',
		    url = 'source-gateway://' || id,
		    etag = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE etag END,
		    last_modified = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE last_modified END,
		    sync_interval_seconds = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN 1800 ELSE sync_interval_seconds END,
		    gateway_enabled = 1,
		    next_sync_at = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE next_sync_at END,
		    last_error = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN '' ELSE last_error END,
		    gateway_status = CASE
		        WHEN (collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id) AND gateway_status = '' THEN 'initializing'
		        ELSE gateway_status
		    END,
		    gateway_has_more = CASE WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id THEN 1 ELSE gateway_has_more END,
		    updated_at = CASE
		        WHEN collection_mode <> 'source_gateway' OR url <> 'source-gateway://' || id OR gateway_enabled <> 1 THEN CURRENT_TIMESTAMP
		        ELSE updated_at
		    END
		WHERE id IN (SELECT value FROM json_each(?))`, sourceGatewayMaintainedSourceIDsJSON); err != nil {
		return err
	}
	if _, err := conn.Exec(`UPDATE feed_sources
		SET is_default = 1,
		    kind = CASE id WHEN 'arxiv-ai' THEN 'atom' ELSE 'rss' END,
		    collection_mode = 'app',
		    url = CASE id
		        WHEN 'openai-news' THEN 'https://openai.com/news/rss.xml'
		        WHEN 'sspai' THEN 'https://sspai.com/feed'
		        WHEN 'arxiv-ai' THEN 'https://export.arxiv.org/api/query?search_query=cat:cs.AI&start=0&max_results=50&sortBy=submittedDate&sortOrder=descending'
		        WHEN 'hkex-regulatory-announcements' THEN 'https://www.hkex.com.hk/Services/RSS-Feeds/regulatory-announcements?sc_lang=zh-HK'
		        ELSE url
		    END,
		    sync_interval_seconds = CASE id
		        WHEN 'openai-news' THEN 7200
		        WHEN 'sspai' THEN 7200
		        ELSE 21600
		    END,
		    etag = CASE WHEN collection_mode <> 'app' OR url LIKE 'source-gateway://%' THEN '' ELSE etag END,
		    last_modified = CASE WHEN collection_mode <> 'app' OR url LIKE 'source-gateway://%' THEN '' ELSE last_modified END,
		    next_sync_at = CASE WHEN collection_mode <> 'app' OR url LIKE 'source-gateway://%' THEN '' ELSE next_sync_at END,
		    last_error = CASE WHEN collection_mode <> 'app' OR url LIKE 'source-gateway://%' THEN '' ELSE last_error END,
		    gateway_enabled = 0,
		    gateway_status = '',
		    gateway_has_more = 0,
		    updated_at = CASE WHEN collection_mode <> 'app' OR url LIKE 'source-gateway://%' OR gateway_enabled <> 0 THEN CURRENT_TIMESTAMP ELSE updated_at END
		WHERE id IN (SELECT value FROM json_each(?))`, localStandardFeedSourceIDsJSON); err != nil {
		return err
	}
	if _, err := conn.Exec(`UPDATE feed_sources
		SET is_default = 0,
		    collection_mode = 'agent',
		    url = 'source-retired://' || id,
		    hidden = 1,
		    auto_extract = 0,
		    etag = '',
		    last_modified = '',
		    next_sync_at = '',
		    last_error = '该维护来源已停止采集，历史内容仍可离线查看',
		    gateway_enabled = 0,
		    gateway_status = '',
		    gateway_has_more = 0,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id IN (
		    SELECT value FROM json_each(?)
		    UNION ALL
		    SELECT value FROM json_each(?)
		)`, sourceGatewayDecommissionedSourceIDsJSON, sourceGatewayRetiredSourceIDsJSON); err != nil {
		return err
	}
	if _, err := conn.Exec(`DELETE FROM source_gateway_subscription_outbox
		WHERE source_id NOT IN (SELECT value FROM json_each(?))`, sourceGatewayMaintainedSourceIDsJSON); err != nil {
		return err
	}
	if _, err := conn.Exec(`UPDATE sync_requests
		SET status = 'superseded', completed_at = CURRENT_TIMESTAMP, message = '该维护来源已停止采集，保留历史缓存'
		WHERE status IN ('pending', 'retry_wait')
		  AND source_id IN (
		      SELECT value FROM json_each(?)
		      UNION ALL
		      SELECT value FROM json_each(?)
		  )`, sourceGatewayDecommissionedSourceIDsJSON, sourceGatewayRetiredSourceIDsJSON); err != nil {
		return err
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	idx := strings.IndexByte(name, '_')
	if idx <= 0 {
		return 0, errors.New("迁移文件名格式错误")
	}
	version, err := strconv.Atoi(name[:idx])
	if err != nil || version <= 0 {
		return 0, errors.New("迁移版本号不合法")
	}
	return version, nil
}

// staticAssetsFS 返回 /static/ 与 index.html 的文件源。生产路径是打进二进制的
// embed FS；本地开发时设置 ZAPP_STATIC_DIR 指向本仓库根目录，静态资源直接从
// 磁盘读取，改 JS/CSS 后刷新浏览器即可生效，无需重新编译。no-cache 头在两种
// 模式下都保留，避免磁盘模式与 embed 版本混用。
func staticAssetsFS() (fs.FS, error) {
	if dir := strings.TrimSpace(os.Getenv("ZAPP_STATIC_DIR")); dir != "" {
		return fs.Sub(os.DirFS(dir), "static")
	}
	return fs.Sub(appFS, "static")
}

func validateStaticDirOverride() error {
	dir := strings.TrimSpace(os.Getenv("ZAPP_STATIC_DIR"))
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "static", "index.html")); err != nil {
		return fmt.Errorf("ZAPP_STATIC_DIR 无效（应指向含 static/ 的仓库根目录）: %w", err)
	}
	log.Printf("开发模式: 静态资源从磁盘读取 %s", dir)
	return nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	staticFS, err := staticAssetsFS()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	body, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	_, _ = w.Write(body)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		options, err := parseDashboardReadOptions(r)
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		getDashboard(w, options)
	case http.MethodPost:
		putDashboard(w, r)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

type dashboardReadOptions struct {
	IncludeNews    bool
	IncludeSources bool
	IncludeCounts  bool
}

// dashboardReadResponse carries explicit projection flags alongside the
// legacy dashboard object.  The flags let clients distinguish an intentional
// lightweight response (for example news_included=false during background
// refresh) from an authoritative empty projection, so they do not discard an
// open detail panel or merge stale counts.
type dashboardReadResponse struct {
	Dashboard
	NewsIncluded    bool `json:"news_included"`
	SourcesIncluded bool `json:"sources_included"`
	CountsIncluded  bool `json:"counts_included"`
}

func writeDashboardReadResponse(w http.ResponseWriter, dashboard Dashboard, options dashboardReadOptions) {
	app.WriteJSON(w, http.StatusOK, dashboardReadResponse{
		Dashboard:       dashboard,
		NewsIncluded:    options.IncludeNews,
		SourcesIncluded: options.IncludeSources,
		CountsIncluded:  options.IncludeCounts,
	})
}

func parseDashboardReadOptions(r *http.Request) (dashboardReadOptions, error) {
	options := dashboardReadOptions{IncludeNews: true, IncludeSources: true, IncludeCounts: true}
	allowed := map[string]*bool{
		"include_news":    &options.IncludeNews,
		"include_sources": &options.IncludeSources,
		"include_counts":  &options.IncludeCounts,
	}
	for key, values := range r.URL.Query() {
		target, ok := allowed[key]
		// The direct HTTP dashboard historically ignored unknown query keys.
		// Preserve that behavior while the typed app-data bridge keeps its
		// stricter operation allowlist.
		if !ok {
			continue
		}
		if len(values) != 1 || (values[0] != "0" && values[0] != "1") {
			return dashboardReadOptions{}, errors.New("看板查询参数不合法")
		}
		*target = values[0] == "1"
	}
	return options, nil
}

func getDashboard(w http.ResponseWriter, options dashboardReadOptions) {
	var raw string
	err := db.QueryRow(`SELECT payload_json FROM dashboard_runs ORDER BY id DESC LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		dashboard := Dashboard{SchemaVersion: 1, Source: SourceStatus{Kind: "mixed", Status: "unavailable", Message: "还没有生成看板数据"}, Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{}}
		// A fresh install can have a fully hydrated local database before the
		// first agent-produced dashboard snapshot exists.  Do not turn that
		// valid state into an empty home screen: recover the durable action,
		// signal and decision projections first, then apply user preferences.
		if err := hydrateDashboardFromLocalTables(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := applyPreferencesWithNews(&dashboard, options.IncludeNews, options.IncludeCounts); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if options.IncludeSources {
			if err := applySources(&dashboard); err != nil {
				app.HTTPErr(w, err)
				return
			}
			finalizeFallbackSourceStatus(&dashboard)
		} else {
			dashboard.Sources = nil
		}
		if err := applyRecordings(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := applyModulePreferences(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := applyModuleLayouts(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
		applyReaderFocusProjection(&dashboard)
		applyFocusSpaceProjection(&dashboard)
		writeDashboardReadResponse(w, dashboard, options)
		return
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	var dashboard Dashboard
	if err := json.Unmarshal([]byte(raw), &dashboard); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := applyPreferencesWithNews(&dashboard, options.IncludeNews, options.IncludeCounts); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if options.IncludeSources {
		if err := applySources(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else {
		dashboard.Sources = nil
	}
	if err := applyRecordings(&dashboard); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := applyModulePreferences(&dashboard); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := applyModuleLayouts(&dashboard); err != nil {
		app.HTTPErr(w, err)
		return
	}
	applyReaderFocusProjection(&dashboard)
	applyFocusSpaceProjection(&dashboard)
	writeDashboardReadResponse(w, dashboard, options)
}

// applyReaderFocusProjection is deliberately best-effort on the ordinary
// dashboard read path. A malformed or temporarily unavailable focus sidecar
// must not make the existing Reader/meeting projection unavailable; the
// typed reader_recommendation operation still reports the underlying error to
// its caller so the Agent can explain the degraded state.
func applyReaderFocusProjection(d *Dashboard) {
	focus, recommendations, err := buildReaderFocusProjections("daily", 5)
	if err != nil {
		return
	}
	if focus != nil {
		d.ReaderFocus = focus
	}
	if recommendations != nil {
		d.ReaderRecommendations = recommendations
	}
}

// applyFocusSpaceProjection keeps dashboard GET aligned with the typed
// focus_analysis.get read model. It is best-effort by design: an older
// database, a missing focus, or a failed sidecar read must leave the ordinary
// dashboard response usable.
func applyFocusSpaceProjection(d *Dashboard) {
	d.FocusSpace = nil
	d.FocusAnalysis = nil
	if err := ensureLegacyFocusSpaces(); err != nil {
		return
	}
	spaces, err := listFocusSpaces("")
	if err != nil {
		return
	}
	for index := range spaces {
		if spaces[index].Status != "active" {
			continue
		}
		space := spaces[index]
		d.FocusSpace = &space
		break
	}
	if d.FocusSpace == nil {
		return
	}
	tx, err := db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()
	analysis, err := focusAnalysisGetTx(tx, d.FocusSpace.ID, "", focusAnalysisLimit)
	if err != nil || analysis.RunID == "" {
		return
	}
	analysis = focusAnalysisProjectionForRun(analysis)
	if err := tx.Commit(); err != nil {
		return
	}
	d.FocusAnalysis = &analysis
}

func focusAnalysisProjectionForRun(analysis focusAnalysisGetResponse) focusAnalysisGetResponse {
	runID := strings.TrimSpace(analysis.RunID)
	filter := func(items []focusResultProjection) []focusResultProjection {
		filtered := make([]focusResultProjection, 0, len(items))
		for _, item := range items {
			if item.AnalysisRunID == runID {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}
	analysis.Signals = filter(analysis.Signals)
	analysis.Actions = filter(analysis.Actions)
	analysis.Decisions = filter(analysis.Decisions)
	analysis.Results = filter(analysis.Results)
	return analysis
}

func applyPreferences(d *Dashboard) error {
	return applyPreferencesWithNews(d, true, true)
}

// hydrateDashboardFromLocalTables reconstructs the durable projection used by
// the Reader when the agent has not written dashboard_runs yet.  This is the
// normal state immediately after installing an app or after clearing a stale
// dashboard snapshot; the underlying Focus/action tables are still the source
// of truth and must not be hidden behind an empty fallback object.
func hydrateDashboardFromLocalTables(d *Dashboard) error {
	rows, err := db.Query(`
SELECT id, task, owner, due_at, source_ids, status, confirmation_status, priority
FROM actions
WHERE COALESCE(focus_id, '') = ''
  AND lower(COALESCE(status, '')) NOT IN ('rejected', 'deleted')
ORDER BY CASE WHEN due_at = '' THEN 1 ELSE 0 END, due_at ASC, id ASC
LIMIT ?`, maxItems)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item Action
		var sourceIDs string
		if err := rows.Scan(&item.ID, &item.Task, &item.Owner, &item.DueAt, &sourceIDs, &item.Status, &item.ConfirmationStatus, &item.Priority); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(sourceIDs), &item.SourceIDs) != nil || item.SourceIDs == nil {
			item.SourceIDs = []string{}
		}
		d.Actions = append(d.Actions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	rows, err = db.Query(`
SELECT id, title, detail, source_ids, confidence, counterevidence, status
FROM signals
WHERE COALESCE(focus_id, '') = ''
  AND lower(COALESCE(status, '')) NOT IN ('dismissed', 'deleted')
ORDER BY id ASC LIMIT ?`, maxItems)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item Signal
		var sourceIDs, counterevidence string
		if err := rows.Scan(&item.ID, &item.Title, &item.Detail, &sourceIDs, &item.Confidence, &counterevidence, &item.Status); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(sourceIDs), &item.SourceIDs) != nil || item.SourceIDs == nil {
			item.SourceIDs = []string{}
		}
		if json.Unmarshal([]byte(counterevidence), &item.Counterevidence) != nil || item.Counterevidence == nil {
			item.Counterevidence = []string{}
		}
		d.Signals = append(d.Signals, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	rows, err = db.Query(`
SELECT id, question, options, recommendation, source_ids, assumptions, risks, status
FROM decisions
WHERE COALESCE(focus_id, '') = ''
  AND lower(COALESCE(status, '')) NOT IN ('rejected', 'deleted')
ORDER BY id ASC LIMIT ?`, maxItems)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item Decision
		var options, sourceIDs, assumptions, risks string
		if err := rows.Scan(&item.ID, &item.Question, &options, &item.Recommendation, &sourceIDs, &assumptions, &risks, &item.Status); err != nil {
			rows.Close()
			return err
		}
		if json.Unmarshal([]byte(options), &item.Options) != nil || item.Options == nil {
			item.Options = []string{}
		}
		if json.Unmarshal([]byte(sourceIDs), &item.SourceIDs) != nil || item.SourceIDs == nil {
			item.SourceIDs = []string{}
		}
		if json.Unmarshal([]byte(assumptions), &item.Assumptions) != nil || item.Assumptions == nil {
			item.Assumptions = []string{}
		}
		if json.Unmarshal([]byte(risks), &item.Risks) != nil || item.Risks == nil {
			item.Risks = []string{}
		}
		d.Decisions = append(d.Decisions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

// finalizeFallbackSourceStatus distinguishes “no snapshot yet” from “no
// sources exist”.  Individual source health remains in Source.Inputs; the
// aggregate only describes whether the directory itself is usable.
func finalizeFallbackSourceStatus(d *Dashboard) {
	if len(d.Sources) == 0 {
		return
	}
	available := 0
	seen, used := 0, 0
	for _, source := range d.Sources {
		if source.Status == "ok" || source.Status == "partial" || source.EntryCount > 0 {
			available++
		}
		seen += source.RecordsSeen
		used += source.RecordsUsed
	}
	d.Source.RecordsSeen = seen
	d.Source.RecordsUsed = used
	d.Source.Status = "partial"
	if available == len(d.Sources) {
		d.Source.Status = "ok"
	}
	d.Source.Message = fmt.Sprintf("已载入 %d 个来源，内容按来源状态更新", len(d.Sources))
}

func applyPreferencesWithNews(d *Dashboard, includeNews bool, includeCounts bool) error {
	actionRows, err := db.Query(`SELECT action_id, completed, deleted FROM action_preferences`)
	if err != nil {
		return err
	}
	actionState := make(map[string][2]bool)
	for actionRows.Next() {
		var id string
		var completed, deleted int
		if err := actionRows.Scan(&id, &completed, &deleted); err != nil {
			actionRows.Close()
			return err
		}
		actionState[id] = [2]bool{completed == 1, deleted == 1}
	}
	if err := actionRows.Err(); err != nil {
		actionRows.Close()
		return err
	}
	if err := actionRows.Close(); err != nil {
		return err
	}
	actions := d.Actions[:0]
	for _, action := range d.Actions {
		state := actionState[action.ID]
		if state[1] {
			continue
		}
		if state[0] {
			action.Status = "completed"
		}
		actions = append(actions, action)
	}
	d.Actions = actions

	signalRows, err := db.Query(`SELECT signal_id FROM signal_preferences WHERE dismissed = 1`)
	if err != nil {
		return err
	}
	dismissed := make(map[string]struct{})
	for signalRows.Next() {
		var id string
		if err := signalRows.Scan(&id); err != nil {
			signalRows.Close()
			return err
		}
		dismissed[id] = struct{}{}
	}
	if err := signalRows.Err(); err != nil {
		signalRows.Close()
		return err
	}
	if err := signalRows.Close(); err != nil {
		return err
	}
	signals := d.Signals[:0]
	for _, signal := range d.Signals {
		if _, hidden := dismissed[signal.ID]; !hidden {
			signals = append(signals, signal)
		}
	}
	d.Signals = signals

	if includeCounts {
		counts, err := queryReaderAggregateCounts()
		if err != nil {
			return err
		}
		d.TotalEntries = counts.TotalEntries
		d.UnreadEntries = counts.UnreadEntries
		d.VideoEntries = counts.VideoEntries
		d.PictureEntries = counts.PictureEntries
	} else {
		// The lightweight dashboard read deliberately omits the expensive count
		// query.  Clear values carried by an older persisted snapshot instead of
		// leaking those stale numbers into the response (or letting a client merge
		// them into its current navigation state).  Counts are authoritative only
		// through /api/reader/counts on this path.
		d.TotalEntries = 0
		d.UnreadEntries = 0
		d.VideoEntries = 0
		d.PictureEntries = 0
	}
	d.News = d.News[:0]
	if !includeNews {
		if err := applyActionWorkflows(d); err != nil {
			return err
		}
		return applyReaderDigestReceipts(d)
	}

	// Keep the dashboard news projection on the same visibility contract as the
	// dedicated counts endpoint and typed /api/entries feed. In particular,
	// Chat-imported focus links are scoped to the active owner/profile; without
	// these arguments a lightweight refresh could leak another profile's link
	// into the Reader while its count and detail endpoints correctly hid it.
	visiblePredicate, visibleArgs := readerVisibleEntryPredicateForDB()
	newsQueryArgs := append([]any{articleOverviewPromptVersion}, visibleArgs...)
	newsQueryArgs = append(newsQueryArgs, maxItems)
	newsRows, err := db.Query(`SELECT n.id, n.external_id, n.title, n.url, n.source, n.source_id, n.origin, n.source_type, n.view_type, n.author, n.published_at, n.updated_at, n.summary_text, n.content_format, n.safe_html, n.thumbnail_url, n.media_url, n.media_mime, n.duration_seconds, n.content_hash, n.fetched_at, n.permission_scope, n.source_level, n.failure_reason, COALESCE(o.entry_id, ''), COALESCE(o.content_hash, ''), COALESCE(o.prompt_version, ''), COALESCE(o.status, ''), COALESCE(o.stage, ''), COALESCE(o.attempt_count, 0), COALESCE(o.summary, ''), COALESCE(o.key_points_json, '[]'), COALESCE(o.impact, ''), COALESCE(o.generated_at, ''), COALESCE(o.error_code, '') FROM news n LEFT JOIN article_ai_overviews o ON o.entry_id = n.id AND o.content_hash = n.content_hash AND o.prompt_version = ? WHERE `+visiblePredicate+` ORDER BY n.published_at DESC, n.updated_at DESC LIMIT ?`, newsQueryArgs...)
	if err != nil {
		return err
	}
	for newsRows.Next() {
		var item NewsItem
		var overview ArticleAIOverview
		var keyPoints string
		if err := newsRows.Scan(&item.ID, &item.ExternalID, &item.Title, &item.URL, &item.Source, &item.SourceID, &item.Origin, &item.SourceType, &item.ViewType, &item.Author, &item.PublishedAt, &item.UpdatedAt, &item.SummaryText, &item.ContentFormat, &item.SafeHTML, &item.ThumbnailURL, &item.MediaURL, &item.MediaMIME, &item.DurationSecs, &item.ContentHash, &item.FetchedAt, &item.PermissionScope, &item.SourceLevel, &item.FailureReason, &overview.EntryID, &overview.ContentHash, &overview.PromptVersion, &overview.Status, &overview.Stage, &overview.AttemptCount, &overview.Summary, &keyPoints, &overview.Impact, &overview.GeneratedAt, &overview.ErrorCode); err != nil {
			newsRows.Close()
			return err
		}
		if overview.EntryID != "" {
			if err := json.Unmarshal([]byte(keyPoints), &overview.KeyPoints); err != nil {
				newsRows.Close()
				return err
			}
			item.AIOverview = &overview
		}
		d.News = append(d.News, item)
	}
	if err := newsRows.Err(); err != nil {
		newsRows.Close()
		return err
	}
	if err := newsRows.Close(); err != nil {
		return err
	}
	if err := applyActionWorkflows(d); err != nil {
		return err
	}
	if err := applyReaderDigestReceipts(d); err != nil {
		return err
	}
	return nil
}

func applySources(d *Dashboard) error {
	sources, err := queryFeedSources(100, "")
	if err != nil {
		return err
	}
	d.Sources = sources
	inputByID := make(map[string]int, len(d.Source.Inputs))
	for i := range d.Source.Inputs {
		inputByID[d.Source.Inputs[i].ID] = i
	}
	for _, source := range sources {
		input := InputStatus{ID: source.ID, Kind: source.Kind, Label: source.Name, Status: source.Status, RecordsSeen: source.RecordsSeen, RecordsUsed: source.RecordsUsed, LastReadAt: source.LastSyncAt, Message: source.LastError}
		if idx, ok := inputByID[source.ID]; ok {
			d.Source.Inputs[idx] = input
		} else {
			d.Source.Inputs = append(d.Source.Inputs, input)
		}
	}
	return nil
}

func applyRecordings(d *Dashboard) error {
	recordings, err := queryRecordings(20, false)
	if err != nil {
		return err
	}
	for i := range recordings {
		recordings[i].Transcript = ""
		recordings[i].Segments = nil
		recordings[i].SpeakerNames = nil
		recordings[i].PerSpeakerSummary = ""
		recordings[i].Report = ""
	}
	d.Recordings = recordings
	return nil
}

func queryRecordings(limit int, includeDetails bool) ([]Recording, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := db.Query(`
SELECT r.record_id, r.title, r.status, r.duration_seconds, r.started_at,
       r.created_at, r.updated_at, r.speaker_count, r.audio_ref,
       r.transcript, r.transcript_truncated, r.segments_json,
       r.segments_truncated, r.speaker_names_json, r.summary,
       r.per_speaker_summary, r.report, r.action_ids_json,
       COALESCE(p.is_read, 0), COALESCE(p.starred, 0), COALESCE(p.playback_seconds, 0)
FROM recordings r
LEFT JOIN recording_preferences p ON p.recording_id = r.record_id
ORDER BY CASE WHEN r.status = 'done' THEN 0 ELSE 1 END,
         r.started_at DESC, r.updated_at DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Recording, 0, limit)
	for rows.Next() {
		var recording Recording
		var transcriptTruncated, segmentsTruncated, read, starred int
		var segmentsJSON, speakerNamesJSON, actionIDsJSON string
		if err := rows.Scan(&recording.ID, &recording.Title, &recording.Status, &recording.DurationSecs, &recording.StartedAt, &recording.CreatedAt, &recording.UpdatedAt, &recording.SpeakerCount, &recording.AudioRef, &recording.Transcript, &transcriptTruncated, &segmentsJSON, &segmentsTruncated, &speakerNamesJSON, &recording.Summary, &recording.PerSpeakerSummary, &recording.Report, &actionIDsJSON, &read, &starred, &recording.PlaybackSeconds); err != nil {
			return nil, err
		}
		recording.TranscriptTruncated = transcriptTruncated == 1
		recording.SegmentsTruncated = segmentsTruncated == 1
		recording.Read = read == 1
		recording.Starred = starred == 1
		if includeDetails {
			_ = json.Unmarshal([]byte(segmentsJSON), &recording.Segments)
			_ = json.Unmarshal([]byte(speakerNamesJSON), &recording.SpeakerNames)
			_ = json.Unmarshal([]byte(actionIDsJSON), &recording.ActionIDs)
		} else {
			recording.Transcript = ""
			recording.PerSpeakerSummary = ""
			recording.Report = ""
			recording.Segments = nil
			recording.SpeakerNames = nil
			_ = json.Unmarshal([]byte(actionIDsJSON), &recording.ActionIDs)
		}
		result = append(result, recording)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func sourceHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		options, err := parseSourceQueryOptions(r)
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		// A first-install source click can race the asynchronous startup
		// projection repair.  Reconcile only the requested source's metadata;
		// the unscoped directory remains a fast local read and article hydration
		// is still deferred until the source is opened.  Existing disabled rows
		// are left untouched by the helper.
		if options.SourceID != "" {
			if projectionErr := ensureSourceGatewaySubscriptionProjection(r.Context(), options.SourceID); projectionErr != nil {
				// Never serialize a transient/missing projection as a successful
				// empty source. The client can retry this bounded request while the
				// background metadata worker catches up.
				log.Printf("来源点击触发目录协调失败 source=%s: %v", options.SourceID, projectionErr)
				status, message := sourceGatewayEntryReadHTTPError(projectionErr)
				app.UserErr(w, status, message)
				return
			}
		}
		// The initial directory projection deliberately omits entry counts.  It
		// should also avoid scanning the full news table just to decorate every
		// source with observed content types.  A real content_type filter still
		// opts into the authoritative observed-type query; otherwise catalog
		// metadata supplies bounded type hints for badges and facets.
		includeContentTypes := options.IncludeCounts || (options.ContentType != "" && options.ContentType != "all")
		sources, err := queryFeedSourcesForCatalog(maxFeedSourceQueryLimit, options.SourceID, options.IncludeCounts, includeContentTypes)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		categories := make(map[string]int)
		languages := make(map[string]int)
		contentTypes := make(map[string]int)
		for _, source := range sources {
			categories[source.Category]++
			languages[source.Language]++
			for _, contentType := range source.ContentTypes {
				contentTypes[contentType]++
			}
		}
		filtered := filterAndSortFeedSources(sources, options)
		app.WriteJSON(w, http.StatusOK, map[string]any{
			"sources":              filtered,
			"categories":           categories,
			"languages":            languages,
			"content_types":        contentTypes,
			"catalog_origin":       "zettlab-maintained-public-rss-v1",
			"catalog_policy":       "公开 RSS/Atom 地址由 Zettlab 维护；不依赖第三方私有目录接口",
			"popularity_available": false,
			"trend_basis":          "local_recent_activity",
			"trend_policy":         "没有可靠公开订阅数时，不展示或推测热度；趋势仅按本地 Focus Space 最近同步活动排序",
		})
	case http.MethodPost:
		var req sourceMutationRequest
		if err := app.DecodeStrictJSON(w, r, 8<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "来源数据格式错误")
			return
		}
		source, err := upsertFeedSource(req)
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		app.WriteJSON(w, http.StatusOK, source)
	case http.MethodPatch:
		var req sourceMutationRequest
		if err := app.DecodeStrictJSON(w, r, 8<<10, &req); err != nil || !validText(req.ID, 128, true) {
			app.UserErr(w, http.StatusBadRequest, "来源设置格式错误")
			return
		}
		current, err := feedSourceByID(req.ID)
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "来源不存在")
			return
		}
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if current.CollectionMode == "source_gateway" {
			source, updateErr := updateGatewaySourceSettings(current, req)
			if updateErr != nil {
				app.UserErr(w, http.StatusBadRequest, updateErr.Error())
				return
			}
			app.WriteJSON(w, http.StatusOK, source)
			return
		}
		if strings.TrimSpace(req.URL) == "" {
			req.URL = current.URL
		}
		if strings.TrimSpace(req.Name) == "" {
			req.Name = current.Name
		}
		if strings.TrimSpace(req.Description) == "" {
			req.Description = current.Description
		}
		if req.CollectionMode == nil {
			req.CollectionMode = &current.CollectionMode
		}
		if req.SyncIntervalSecs == nil {
			req.SyncIntervalSecs = &current.SyncIntervalSecs
		}
		if req.BackfillDays == nil {
			req.BackfillDays = &current.BackfillDays
		}
		if req.RetentionDays == nil {
			req.RetentionDays = &current.RetentionDays
		}
		if req.RetentionLimit == nil {
			req.RetentionLimit = &current.RetentionLimit
		}
		if req.AutoExtract == nil {
			req.AutoExtract = &current.AutoExtract
		}
		if req.Hidden == nil {
			req.Hidden = &current.Hidden
		}
		req.Kind = current.Kind
		req.ID = current.ID
		req.allowCatalogEdit = true
		source, err := upsertFeedSource(req)
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.Hidden != nil {
			_, _ = db.Exec(`UPDATE feed_sources SET hidden = ?, updated_at = ? WHERE id = ?`, boolInt(*req.Hidden), time.Now().UTC().Format(time.RFC3339), req.ID)
			source.Hidden = *req.Hidden
		}
		app.WriteJSON(w, http.StatusOK, source)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func updateGatewaySourceSettings(current FeedSource, req sourceMutationRequest) (FeedSource, error) {
	if raw := strings.TrimSpace(req.URL); raw != "" && raw != current.URL {
		return FeedSource{}, errors.New("云端维护来源地址不允许修改")
	}
	if kind := strings.ToLower(strings.TrimSpace(req.Kind)); kind != "" && kind != current.Kind {
		return FeedSource{}, errors.New("云端维护来源类型不允许修改")
	}
	if name := strings.TrimSpace(req.Name); name != "" && name != current.Name {
		return FeedSource{}, errors.New("云端维护来源名称不允许修改")
	}
	if description := strings.TrimSpace(req.Description); description != "" && description != current.Description {
		return FeedSource{}, errors.New("云端维护来源说明不允许修改")
	}
	if req.CollectionMode != nil && strings.ToLower(strings.TrimSpace(*req.CollectionMode)) != current.CollectionMode {
		return FeedSource{}, errors.New("云端维护来源采集模式不允许修改")
	}

	interval := current.SyncIntervalSecs
	backfill := current.BackfillDays
	retentionDays := current.RetentionDays
	retentionLimit := current.RetentionLimit
	autoExtract := current.AutoExtract
	hidden := current.Hidden
	if req.SyncIntervalSecs != nil {
		interval = *req.SyncIntervalSecs
	}
	if req.BackfillDays != nil {
		backfill = *req.BackfillDays
	}
	if req.RetentionDays != nil {
		retentionDays = *req.RetentionDays
	}
	if req.RetentionLimit != nil {
		retentionLimit = *req.RetentionLimit
	}
	if req.AutoExtract != nil {
		autoExtract = *req.AutoExtract
	}
	if req.Hidden != nil {
		hidden = *req.Hidden
	}
	if !validCollectionSettings(interval, backfill, retentionDays, retentionLimit) {
		return FeedSource{}, errors.New("来源采集设置超出范围")
	}

	result, err := db.Exec(`UPDATE feed_sources
SET sync_interval_seconds=?,backfill_days=?,retention_days=?,retention_limit=?,auto_extract=?,hidden=?,updated_at=?
WHERE id=? AND collection_mode='source_gateway'`, interval, backfill, retentionDays, retentionLimit,
		boolInt(autoExtract), boolInt(hidden), time.Now().UTC().Format(time.RFC3339), current.ID)
	if err != nil {
		return FeedSource{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return FeedSource{}, err
	}
	if updated != 1 {
		return FeedSource{}, errors.New("云端维护来源状态已变化，请刷新后重试")
	}
	return feedSourceByID(current.ID)
}

func subscriptionHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var dashboard Dashboard
		if err := applySources(&dashboard); err != nil {
			app.HTTPErr(w, err)
			return
		}
		active := make([]FeedSource, 0, len(dashboard.Sources))
		for _, source := range dashboard.Sources {
			if source.Subscribed {
				active = append(active, source)
			}
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"subscriptions": active})
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
		var req subscriptionMutationRequest
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil || !validText(req.SourceID, 128, true) {
			app.UserErr(w, http.StatusBadRequest, "订阅数据格式错误")
			return
		}
		if _, err := feedSourceByID(req.SourceID); errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "来源不存在")
			return
		} else if err != nil {
			app.HTTPErr(w, err)
			return
		}
		current, err := subscriptionState(req.SourceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			app.HTTPErr(w, err)
			return
		}
		enabled := r.Method != http.MethodDelete
		folderID, titleOverride := current.FolderID, current.TitleOverride
		pinned, muted, sortOrder := current.Pinned, current.Muted, current.SortOrder
		if r.Method != http.MethodDelete && req.Enabled != nil {
			enabled = *req.Enabled
		}
		if req.FolderID != nil {
			folderID = strings.TrimSpace(*req.FolderID)
			if folderID != "" && !folderExists(folderID) {
				app.UserErr(w, http.StatusBadRequest, "订阅分组不存在")
				return
			}
		}
		if req.TitleOverride != nil {
			titleOverride = strings.TrimSpace(*req.TitleOverride)
			if !validText(titleOverride, 120, false) {
				app.UserErr(w, http.StatusBadRequest, "订阅标题过长")
				return
			}
		}
		if req.Pinned != nil {
			pinned = *req.Pinned
		}
		if req.Muted != nil {
			muted = *req.Muted
		}
		if req.SortOrder != nil {
			if *req.SortOrder < -100000 || *req.SortOrder > 100000 {
				app.UserErr(w, http.StatusBadRequest, "订阅顺序超出范围")
				return
			}
			sortOrder = *req.SortOrder
		}
		now := time.Now().UTC().Format(time.RFC3339)
		_, err = db.Exec(`
INSERT INTO feed_subscriptions(source_id, enabled, subscribed_at, updated_at, folder_id, title_override, pinned, muted, sort_order)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(source_id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at,
folder_id=excluded.folder_id, title_override=excluded.title_override, pinned=excluded.pinned,
muted=excluded.muted, sort_order=excluded.sort_order`, req.SourceID, boolInt(enabled), now, now, folderID, titleOverride, boolInt(pinned), boolInt(muted), sortOrder)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		source, err := feedSourceByID(req.SourceID)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		source.Subscribed = enabled
		if enabled {
			if _, syncErr := enqueueSyncRequest(req.SourceID); syncErr != nil {
				log.Printf("创建首次同步请求失败 source=%s: %v", req.SourceID, syncErr)
			}
		} else if cancelErr := cancelPendingSyncRequests(req.SourceID); cancelErr != nil {
			log.Printf("取消退订来源的待处理同步失败 source=%s: %v", req.SourceID, cancelErr)
		}
		// 云端订阅转发只在运行时开启后异步进行；测试与维护模式不触发。
		if feedSyncRuntimeEnabled {
			go forwardGatewaySubscription(req.SourceID, enabled)
		}
		app.WriteJSON(w, http.StatusOK, source)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func syncHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT id, source_id, status, requested_at, completed_at, message, attempt_count, next_retry_at FROM sync_requests ORDER BY id DESC LIMIT 20`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		requests := make([]map[string]any, 0, 20)
		for rows.Next() {
			var id int64
			var sourceID, status, requestedAt, completedAt, message, nextRetryAt string
			var attemptCount int
			if err := rows.Scan(&id, &sourceID, &status, &requestedAt, &completedAt, &message, &attemptCount, &nextRetryAt); err != nil {
				app.HTTPErr(w, err)
				return
			}
			requests = append(requests, map[string]any{"id": id, "source_id": sourceID, "status": status, "requested_at": requestedAt, "completed_at": completedAt, "message": message, "attempt_count": attemptCount, "next_retry_at": nextRetryAt})
		}
		if err := rows.Err(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"requests": requests})
	case http.MethodPost:
		var req syncRequest
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "同步请求格式错误")
			return
		}
		if req.SourceID != "" {
			if _, err := feedSourceByID(req.SourceID); errors.Is(err, sql.ErrNoRows) {
				app.UserErr(w, http.StatusNotFound, "来源不存在")
				return
			} else if err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
		id, err := enqueueSyncRequest(req.SourceID)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "request_id": id, "source_id": req.SourceID, "status": "pending"})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func enqueueSyncRequest(sourceID string) (int64, error) {
	feedSyncWriteMu.Lock()
	defer feedSyncWriteMu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRow(`SELECT id FROM sync_requests
WHERE source_id=? AND status IN ('pending','retry_wait')
ORDER BY id DESC LIMIT 1`, sourceID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
		result, insertErr := tx.Exec(`INSERT INTO sync_requests(source_id, status, requested_at) VALUES(?, 'pending', ?)`, sourceID, now)
		if insertErr != nil {
			return 0, insertErr
		}
		id, err = result.LastInsertId()
	} else if err == nil {
		_, err = tx.Exec(`UPDATE sync_requests SET status='pending', requested_at=?, completed_at='', message='', next_retry_at='' WHERE id=?`, now, id)
	}
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	if strings.TrimSpace(sourceID) != "" {
		_, _ = db.Exec(`UPDATE feed_sources SET next_sync_at=?,updated_at=? WHERE id=?`, now, now, sourceID)
	}
	_, _ = db.Exec(`DELETE FROM sync_requests WHERE id NOT IN (SELECT id FROM sync_requests ORDER BY id DESC LIMIT 100)`)
	triggerSourceSync(sourceID)
	return id, nil
}

func upsertFeedSource(req sourceMutationRequest) (FeedSource, error) {
	rawURL, err := normalizePublicURL(req.URL)
	if err != nil || !validPublicFeedURL(rawURL) {
		return FeedSource{}, errors.New("请输入不含凭据的公开 http 或 https 来源地址")
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "rss"
	}
	if kind != "rss" && kind != "atom" && kind != "json" {
		return FeedSource{}, errors.New("来源类型不受支持")
	}
	id := sourceIDForURL(rawURL)
	// Curated (and previously user-added) sources may use a stable catalog ID
	// instead of the URL-derived ID. Reuse the existing row by URL so adding an
	// already-known address is idempotent and never trips the unique URL index.
	var existingID string
	var existingSource *FeedSource
	err = db.QueryRow(`SELECT id FROM feed_sources WHERE url = ?`, rawURL).Scan(&existingID)
	if err == nil {
		if existingID != sourceIDForURL(rawURL) && !req.allowCatalogEdit {
			return FeedSource{}, errors.New(catalogSourceRequiresStableIDMessage)
		}
		if req.ID != "" && req.ID != existingID {
			return FeedSource{}, errors.New("来源地址已绑定到其他来源")
		}
		id = existingID
		current, currentErr := feedSourceByID(existingID)
		if currentErr != nil {
			return FeedSource{}, currentErr
		}
		existingSource = &current
		if strings.TrimSpace(req.Kind) == "" {
			kind = current.Kind
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return FeedSource{}, err
	}
	if req.ID != "" && req.ID != id {
		return FeedSource{}, errors.New("来源标识与地址不匹配")
	}
	if existingSource == nil {
		if collision, collisionErr := feedSourceByID(id); collisionErr == nil && collision.URL != rawURL {
			return FeedSource{}, errors.New("来源标识已被其他来源使用")
		} else if collisionErr != nil && !errors.Is(collisionErr, sql.ErrNoRows) {
			return FeedSource{}, collisionErr
		}
	} else if existingSource.CollectionMode == "source_gateway" {
		return FeedSource{}, errors.New("云端维护来源身份不允许修改")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" && existingSource != nil {
		name = existingSource.Name
	} else if name == "" {
		parsed, _ := url.Parse(rawURL)
		name = parsed.Hostname()
	}
	description := strings.TrimSpace(req.Description)
	if description == "" && existingSource != nil {
		description = existingSource.Description
	}
	if !validText(name, 120, true) || !validText(description, 500, false) {
		return FeedSource{}, errors.New("来源名称或说明过长")
	}
	if existing, err := feedSourceByID(id); err == nil && existing.IsDefault && existing.URL != rawURL {
		return FeedSource{}, errors.New("默认来源地址不允许修改")
	}
	mode := "app"
	interval, backfill, retentionDays, retentionLimit := 7200, 30, 90, 1000
	autoExtract := true
	hidden := false
	if existingSource != nil {
		mode = existingSource.CollectionMode
		interval = existingSource.SyncIntervalSecs
		backfill = existingSource.BackfillDays
		retentionDays = existingSource.RetentionDays
		retentionLimit = existingSource.RetentionLimit
		autoExtract = existingSource.AutoExtract
		hidden = existingSource.Hidden
	}
	if req.CollectionMode != nil {
		mode = strings.ToLower(strings.TrimSpace(*req.CollectionMode))
	}
	if !validCollectionMode(mode) {
		return FeedSource{}, errors.New("采集模式不受支持")
	}
	if existingSource == nil && mode != "app" {
		return FeedSource{}, errors.New("自定义来源只允许使用本地采集模式")
	}
	if mode == "source_gateway" {
		return FeedSource{}, errors.New("自定义来源不能使用云端维护来源模式")
	}
	if req.SyncIntervalSecs != nil {
		interval = *req.SyncIntervalSecs
	}
	if req.BackfillDays != nil {
		backfill = *req.BackfillDays
	}
	if req.RetentionDays != nil {
		retentionDays = *req.RetentionDays
	}
	if req.RetentionLimit != nil {
		retentionLimit = *req.RetentionLimit
	}
	if req.AutoExtract != nil {
		autoExtract = *req.AutoExtract
	}
	if req.Hidden != nil {
		hidden = *req.Hidden
	}
	if !validCollectionSettings(interval, backfill, retentionDays, retentionLimit) {
		return FeedSource{}, errors.New("来源采集设置超出范围")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return FeedSource{}, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
INSERT INTO feed_sources(id, kind, name, url, description, is_default, status, created_at, updated_at,
collection_mode, sync_interval_seconds, next_sync_at, backfill_days, retention_days, retention_limit, auto_extract, hidden)
VALUES(?,?,?,?,?,0,'unavailable',?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, name=excluded.name, url=excluded.url,
description=excluded.description, collection_mode=excluded.collection_mode,
sync_interval_seconds=excluded.sync_interval_seconds, backfill_days=excluded.backfill_days,
retention_days=excluded.retention_days, retention_limit=excluded.retention_limit,
auto_extract=excluded.auto_extract, hidden=excluded.hidden, updated_at=excluded.updated_at`,
		id, kind, name, rawURL, description, now, now, mode, interval, now, backfill,
		retentionDays, retentionLimit, boolInt(autoExtract), boolInt(hidden))
	if err != nil {
		return FeedSource{}, err
	}
	if _, err = tx.Exec(`INSERT INTO feed_catalog_entries
(source_id,site_url,category,language,icon_text,icon_color,tags_json,feed_format,recommended,popularity,validation_status,validated_at,catalog_origin)
VALUES(?,?,'other','und',?,?,'[]',?,0,0,'unverified','','user-added-public-source')
ON CONFLICT(source_id) DO UPDATE SET
site_url=CASE WHEN feed_catalog_entries.site_url='' THEN excluded.site_url ELSE feed_catalog_entries.site_url END,
icon_text=CASE WHEN feed_catalog_entries.icon_text='' THEN excluded.icon_text ELSE feed_catalog_entries.icon_text END,
feed_format=excluded.feed_format`, id, sourceOriginURL(rawURL), firstRune(name), sourceIconColor(id), kind); err != nil {
		return FeedSource{}, err
	}
	if err = tx.Commit(); err != nil {
		return FeedSource{}, err
	}
	return feedSourceByID(id)
}

func feedSourceByID(id string) (FeedSource, error) {
	items, err := queryFeedSources(1, id)
	if err != nil {
		return FeedSource{}, err
	}
	if len(items) == 0 {
		return FeedSource{}, sql.ErrNoRows
	}
	return items[0], nil
}

func sourceIDForURL(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return "feed-" + hex.EncodeToString(digest[:8])
}

func validPublicFeedURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 1000 {
		return false
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	if feedURLHasSensitiveCredentials(raw) {
		return false
	}
	host := strings.Trim(strings.ToLower(parsed.Hostname()), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "localhost.localdomain" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return isPublicIP(ip)
	}
	return true
}

func feedURLHasSensitiveCredentials(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil {
		return true
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return true
	}
	for key := range query {
		if sensitiveFeedQueryKey(key) {
			return true
		}
	}
	return false
}

func sensitiveFeedQueryKey(key string) bool {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(key)), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	for _, part := range parts {
		switch part {
		case "token", "secret", "password", "authorization", "auth", "signature", "sig", "signed",
			"credential", "credentials", "cookie", "session", "jwt":
			return true
		}
	}
	compact := strings.Join(parts, "")
	switch compact {
	case "apikey", "accesstoken", "authtoken", "bearertoken", "refreshtoken", "idtoken",
		"accesskey", "accesskeyid", "accessid", "secretaccesskey", "clientsecret",
		"privatekey", "signingkey", "sessionid", "sessiontoken", "securitytoken":
		return true
	default:
		return false
	}
}

func recordingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 50 {
			app.UserErr(w, http.StatusBadRequest, "录音数量不合法")
			return
		}
		limit = parsed
	}
	items, err := queryRecordings(limit, false)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if len(items) == 0 {
		if liveItems, liveErr := queryDeviceRecordings(r.Context(), limit); liveErr == nil {
			items = liveItems
		}
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"recordings": items, "limit": limit})
}

func recordingDetailHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validRecordingID(id) {
		app.UserErr(w, http.StatusBadRequest, "录音标识不合法")
		return
	}
	recording, err := queryRecordingByID(id)
	if errors.Is(err, sql.ErrNoRows) {
		recording, err = queryDeviceRecordingByID(r.Context(), id)
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "录音不存在")
			return
		}
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"recording": recording})
}

func recordingPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT recording_id, is_read, starred, playback_seconds FROM recording_preferences ORDER BY updated_at DESC LIMIT 2000`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		items := make([]RecordingPreference, 0, 32)
		for rows.Next() {
			var item RecordingPreference
			var read, starred int
			if err := rows.Scan(&item.RecordingID, &read, &starred, &item.PlaybackSeconds); err != nil {
				app.HTTPErr(w, err)
				return
			}
			item.Read = read == 1
			item.Starred = starred == 1
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPatch:
		var req recordingPreferenceRequest
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil || !validRecordingID(req.RecordingID) || req.Read == nil && req.Starred == nil && req.PlaybackSeconds == nil {
			app.UserErr(w, http.StatusBadRequest, "录音状态格式错误")
			return
		}
		var read, starred int
		var playback float64
		err := db.QueryRow(`SELECT COALESCE(is_read, 0), COALESCE(starred, 0), COALESCE(playback_seconds, 0) FROM recording_preferences WHERE recording_id = ?`, req.RecordingID).Scan(&read, &starred, &playback)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			app.HTTPErr(w, err)
			return
		}
		if _, err := recordingByIDExists(req.RecordingID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				app.UserErr(w, http.StatusNotFound, "录音不存在")
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
		if req.Read != nil {
			read = boolInt(*req.Read)
		}
		if req.Starred != nil {
			starred = boolInt(*req.Starred)
		}
		if req.PlaybackSeconds != nil {
			if math.IsNaN(*req.PlaybackSeconds) || math.IsInf(*req.PlaybackSeconds, 0) || *req.PlaybackSeconds < 0 || *req.PlaybackSeconds > 86400 {
				app.UserErr(w, http.StatusBadRequest, "播放进度不合法")
				return
			}
			playback = *req.PlaybackSeconds
		}
		now := time.Now().UTC().Format(time.RFC3339)
		tx, err := db.Begin()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer tx.Rollback()
		_, err = tx.Exec(`
INSERT INTO recording_preferences(recording_id, is_read, starred, playback_seconds, updated_at) VALUES(?,?,?,?,?)
ON CONFLICT(recording_id) DO UPDATE SET is_read=excluded.is_read, starred=excluded.starred,
playback_seconds=excluded.playback_seconds, updated_at=excluded.updated_at`, req.RecordingID, read, starred, playback, now)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if req.Starred != nil {
			// Starred is a Reader preference only. Stable context requires an
			// explicit context.confirm action so it does not become a cleanup
			// burden for ordinary favorites.
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		prunePreferences()
		app.WriteJSON(w, http.StatusOK, RecordingPreference{RecordingID: req.RecordingID, Read: read == 1, Starred: starred == 1, PlaybackSeconds: playback})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func recordingActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req recordingActionRequest
	if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil || !validRecordingID(req.RecordingID) || !validText(req.ActionID, 64, true) {
		app.UserErr(w, http.StatusBadRequest, "录音行动关联格式错误")
		return
	}
	if _, err := recordingByIDExists(req.RecordingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "录音不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if r.Method == http.MethodPost {
		var actionID string
		if err := db.QueryRow(`SELECT id FROM actions WHERE id = ?`, req.ActionID).Scan(&actionID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				app.UserErr(w, http.StatusNotFound, "行动不存在")
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
		_, err := db.Exec(`INSERT OR IGNORE INTO recording_actions(recording_id, action_id, created_at) VALUES(?,?,?)`, req.RecordingID, req.ActionID, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else {
		if _, err := db.Exec(`DELETE FROM recording_actions WHERE recording_id = ? AND action_id = ?`, req.RecordingID, req.ActionID); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := refreshRecordingActionIDs(req.RecordingID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "recording_id": req.RecordingID, "action_id": req.ActionID, "linked": r.Method == http.MethodPost})
}

func queryRecordingByID(id string) (Recording, error) {
	items, err := queryRecordingsByIDs(id)
	if err != nil {
		return Recording{}, err
	}
	if len(items) == 0 {
		return Recording{}, sql.ErrNoRows
	}
	return items[0], nil
}

func queryRecordingsByIDs(id string) ([]Recording, error) {
	rows, err := db.Query(`
SELECT r.record_id, r.title, r.status, r.duration_seconds, r.started_at,
       r.created_at, r.updated_at, r.speaker_count, r.audio_ref,
       r.transcript, r.transcript_truncated, r.segments_json,
       r.segments_truncated, r.speaker_names_json, r.summary,
       r.per_speaker_summary, r.report, r.action_ids_json,
       COALESCE(p.is_read, 0), COALESCE(p.starred, 0), COALESCE(p.playback_seconds, 0)
FROM recordings r
LEFT JOIN recording_preferences p ON p.recording_id = r.record_id
WHERE r.record_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Recording, 0, 1)
	for rows.Next() {
		var recording Recording
		var transcriptTruncated, segmentsTruncated, read, starred int
		var segmentsJSON, speakerNamesJSON, actionIDsJSON string
		if err := rows.Scan(&recording.ID, &recording.Title, &recording.Status, &recording.DurationSecs, &recording.StartedAt, &recording.CreatedAt, &recording.UpdatedAt, &recording.SpeakerCount, &recording.AudioRef, &recording.Transcript, &transcriptTruncated, &segmentsJSON, &segmentsTruncated, &speakerNamesJSON, &recording.Summary, &recording.PerSpeakerSummary, &recording.Report, &actionIDsJSON, &read, &starred, &recording.PlaybackSeconds); err != nil {
			return nil, err
		}
		recording.TranscriptTruncated = transcriptTruncated == 1
		recording.SegmentsTruncated = segmentsTruncated == 1
		recording.Read = read == 1
		recording.Starred = starred == 1
		_ = json.Unmarshal([]byte(segmentsJSON), &recording.Segments)
		_ = json.Unmarshal([]byte(speakerNamesJSON), &recording.SpeakerNames)
		_ = json.Unmarshal([]byte(actionIDsJSON), &recording.ActionIDs)
		result = append(result, recording)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func recordingByIDExists(id string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT record_id FROM recordings WHERE record_id = ?`, id).Scan(&value)
	return value, err
}

func refreshRecordingActionIDs(recordingID string) error {
	rows, err := db.Query(`SELECT action_id FROM recording_actions WHERE recording_id = ? ORDER BY action_id`, recordingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	ids := make([]string, 0, 10)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	raw, _ := json.Marshal(ids)
	_, err = db.Exec(`UPDATE recordings SET action_ids_json = ? WHERE record_id = ?`, raw, recordingID)
	return err
}

func validRecordingID(id string) bool {
	return validText(id, 128, true) && !strings.ContainsAny(id, "/\\\x00\r\n")
}

func validAudioRef(ref string) bool {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "/")
	if ref == "" {
		return ref == ""
	}
	if !strings.HasSuffix(ref, "/audio") || strings.ContainsAny(ref, "?&#\\\x00\r\n") {
		return false
	}
	prefix := "_zettlab/recordings/"
	deviceMeetingPrefix := strings.TrimPrefix(bridges.DeviceDataPathPrefix, "/") + "/meetings/"
	if strings.HasPrefix(ref, deviceMeetingPrefix) {
		prefix = deviceMeetingPrefix
	} else if !strings.HasPrefix(ref, prefix) {
		return false
	}
	middle := strings.TrimSuffix(strings.TrimPrefix(ref, prefix), "/audio")
	return validRecordingID(middle) && !strings.Contains(middle, "..")
}

type actionPreferenceRequest struct {
	ID        string `json:"id"`
	Completed bool   `json:"completed"`
}

func actionPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	var req actionPreferenceRequest
	if r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	if err := decodeMutation(w, r, &req); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var result sql.Result
	var err error
	if r.Method == http.MethodPatch {
		result, err = db.Exec(`
INSERT INTO action_preferences(action_id, completed, deleted, updated_at)
SELECT id, ?, 0, ? FROM actions WHERE id = ?
ON CONFLICT(action_id) DO UPDATE SET completed=excluded.completed, deleted=0, updated_at=excluded.updated_at`, boolInt(req.Completed), now, req.ID)
	} else {
		result, err = db.Exec(`
INSERT INTO action_preferences(action_id, completed, deleted, updated_at)
SELECT id, 0, 1, ? FROM actions WHERE id = ?
ON CONFLICT(action_id) DO UPDATE SET deleted=1, updated_at=excluded.updated_at`, now, req.ID)
	}
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !mutationMatched(result) {
		app.UserErr(w, http.StatusNotFound, "待办不存在")
		return
	}
	prunePreferences()
	app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type signalPreferenceRequest struct {
	ID string `json:"id"`
}

func signalPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req signalPreferenceRequest
	if err := decodeMutation(w, r, &req); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := db.Exec(`
INSERT INTO signal_preferences(signal_id, dismissed, updated_at)
SELECT id, 1, ? FROM signals WHERE id = ?
ON CONFLICT(signal_id) DO UPDATE SET dismissed=1, updated_at=excluded.updated_at`, time.Now().UTC().Format(time.RFC3339), req.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !mutationMatched(result) {
		app.UserErr(w, http.StatusNotFound, "内容不存在")
		return
	}
	prunePreferences()
	app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func newsPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req signalPreferenceRequest
	if err := decodeMutation(w, r, &req); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := db.Exec(`
INSERT INTO news_preferences(news_id, dismissed, updated_at)
SELECT id, 1, ? FROM news WHERE id = ?
ON CONFLICT(news_id) DO UPDATE SET dismissed=1, updated_at=excluded.updated_at`, time.Now().UTC().Format(time.RFC3339), req.ID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if !mutationMatched(result) {
		app.UserErr(w, http.StatusNotFound, "新闻不存在")
		return
	}
	prunePreferences()
	app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type modulePreferenceRequest struct {
	ID     string `json:"id"`
	Hidden bool   `json:"hidden"`
}

func modulePreferenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req modulePreferenceRequest
	if err := decodeMutation(w, r, &req); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := dashboardModuleIDs[req.ID]; !ok {
		app.UserErr(w, http.StatusBadRequest, "模块标识不合法")
		return
	}
	_, err := db.Exec(`
INSERT INTO module_preferences(module_id, hidden, updated_at) VALUES(?,?,?)
ON CONFLICT(module_id) DO UPDATE SET hidden=excluded.hidden, updated_at=excluded.updated_at`, req.ID, boolInt(req.Hidden), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	prunePreferences()
	app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type moduleLayoutRequest struct {
	ID       string `json:"id"`
	HeightPx *int   `json:"height_px,omitempty"`
	Span     *int   `json:"span,omitempty"`
	Reset    bool   `json:"reset,omitempty"`
}

func moduleLayoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var req moduleLayoutRequest
	if err := decodeMutation(w, r, &req); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, ok := dashboardModuleIDs[req.ID]; !ok {
		app.UserErr(w, http.StatusBadRequest, "模块标识不合法")
		return
	}
	if req.Reset {
		if req.HeightPx != nil || req.Span != nil {
			app.UserErr(w, http.StatusBadRequest, "布局数据格式错误")
			return
		}
		if _, err := db.Exec(`DELETE FROM module_layouts WHERE module_id = ?`, req.ID); err != nil {
			app.HTTPErr(w, err)
			return
		}
		prunePreferences()
		app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if req.HeightPx == nil || req.Span == nil {
		app.UserErr(w, http.StatusBadRequest, "布局数据不完整")
		return
	}
	if *req.HeightPx < minModuleHeight || *req.HeightPx > maxModuleHeight {
		app.UserErr(w, http.StatusBadRequest, "模块高度超出范围")
		return
	}
	if *req.Span < minModuleSpan || *req.Span > maxModuleSpan {
		app.UserErr(w, http.StatusBadRequest, "模块宽度超出范围")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
INSERT INTO module_layouts(module_id, height_px, span, updated_at) VALUES(?,?,?,?)
ON CONFLICT(module_id) DO UPDATE SET height_px=excluded.height_px, span=excluded.span, updated_at=excluded.updated_at`, req.ID, *req.HeightPx, *req.Span, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	prunePreferences()
	app.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

type itemPreferenceRequest struct {
	Key             string   `json:"key"`
	Read            *bool    `json:"read,omitempty"`
	Starred         *bool    `json:"starred,omitempty"`
	ReadLater       *bool    `json:"read_later,omitempty"`
	ReadingProgress *float64 `json:"reading_progress,omitempty"`
}

func itemPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rows, err := db.Query(`SELECT item_key, is_read, starred, read_later, reading_progress FROM item_preferences ORDER BY item_key`)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer rows.Close()
		items := make([]ItemPreference, 0)
		for rows.Next() {
			var item ItemPreference
			var read, starred, readLater int
			if err := rows.Scan(&item.Key, &read, &starred, &readLater, &item.ReadingProgress); err != nil {
				app.HTTPErr(w, err)
				return
			}
			item.Read = read == 1
			item.Starred = starred == 1
			item.ReadLater = readLater == 1
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	case http.MethodPatch:
		var req itemPreferenceRequest
		if err := app.DecodeStrictJSON(w, r, 4<<10, &req); err != nil {
			app.UserErr(w, http.StatusBadRequest, "条目状态格式错误")
			return
		}
		if !validItemKey(req.Key) || req.Read == nil && req.Starred == nil && req.ReadLater == nil && req.ReadingProgress == nil {
			app.UserErr(w, http.StatusBadRequest, "条目状态不合法")
			return
		}
		var read, starred, readLater int
		var readingProgress float64
		err := db.QueryRow(`SELECT is_read, starred, read_later, reading_progress FROM item_preferences WHERE item_key = ?`, req.Key).Scan(&read, &starred, &readLater, &readingProgress)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			app.HTTPErr(w, err)
			return
		}
		if req.Read != nil {
			read = boolInt(*req.Read)
		}
		if req.Starred != nil {
			starred = boolInt(*req.Starred)
		}
		if req.ReadLater != nil {
			readLater = boolInt(*req.ReadLater)
		}
		if req.ReadingProgress != nil {
			if math.IsNaN(*req.ReadingProgress) || math.IsInf(*req.ReadingProgress, 0) || *req.ReadingProgress < 0 || *req.ReadingProgress > 1 {
				app.UserErr(w, http.StatusBadRequest, "阅读进度不合法")
				return
			}
			readingProgress = *req.ReadingProgress
		}
		tx, err := db.Begin()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		defer tx.Rollback()
		_, err = tx.Exec(`
INSERT INTO item_preferences(item_key, is_read, starred, read_later, reading_progress, updated_at) VALUES(?,?,?,?,?,?)
ON CONFLICT(item_key) DO UPDATE SET is_read=excluded.is_read, starred=excluded.starred,
read_later=excluded.read_later, reading_progress=excluded.reading_progress,
updated_at=excluded.updated_at`, req.Key, read, starred, readLater, readingProgress, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if req.Starred != nil {
			// Starred is intentionally independent from stable context. The
			// explicit "沉淀到知识库" action owns context.confirm.
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		prunePreferences()
		app.WriteJSON(w, http.StatusOK, ItemPreference{Key: req.Key, Read: read == 1, Starred: starred == 1, ReadLater: readLater == 1, ReadingProgress: readingProgress})
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func validItemKey(key string) bool {
	kind, id, ok := strings.Cut(key, ":")
	if !ok || !validText(id, 64, true) {
		return false
	}
	switch kind {
	case "news", "action", "metric", "project", "signal", "decision", "evidence":
		return true
	default:
		return false
	}
}

// validSourceDisplayID accepts source-gateway catalog ids and local feed_sources
// ids (e.g. "youtube-anthropic", "openai-news"): lowercase slug charset, no
// colons — item keys and source ids are different namespaces.
func validSourceDisplayID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func readerPreferenceHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefs, err := readReaderPreferences()
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, prefs)
	case http.MethodPatch:
		var prefs ReaderPreferences
		if err := app.DecodeStrictJSON(w, r, 4<<10, &prefs); err != nil {
			app.UserErr(w, http.StatusBadRequest, "栏宽设置不合法")
			return
		}
		if prefs.ViewMode == "" {
			prefs.ViewMode = "comfortable"
		}
		if !validReaderPreferences(prefs) {
			app.UserErr(w, http.StatusBadRequest, "栏宽设置不合法")
			return
		}
		// Legacy clients PATCH without mark_read_on_scroll; a missing field
		// keeps the stored value instead of silently resetting the opt-in.
		if prefs.MarkReadOnScroll == nil {
			previous, err := readReaderPreferences()
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			prefs.MarkReadOnScroll = previous.MarkReadOnScroll
		}
		_, err := db.Exec(`
INSERT INTO reader_preferences(id, sidebar_width, timeline_width, view_mode, mark_read_on_scroll, updated_at) VALUES('default',?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET sidebar_width=excluded.sidebar_width, timeline_width=excluded.timeline_width,
view_mode=excluded.view_mode, mark_read_on_scroll=excluded.mark_read_on_scroll, updated_at=excluded.updated_at`, prefs.SidebarWidth, prefs.TimelineWidth, prefs.ViewMode, boolInt(*prefs.MarkReadOnScroll), time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, prefs)
	default:
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
	}
}

func readReaderPreferences() (ReaderPreferences, error) {
	prefs := ReaderPreferences{SidebarWidth: 256, TimelineWidth: 380, ViewMode: "comfortable"}
	markReadOnScroll := false
	prefs.MarkReadOnScroll = &markReadOnScroll
	var stored int
	err := db.QueryRow(`SELECT sidebar_width, timeline_width, view_mode, mark_read_on_scroll FROM reader_preferences WHERE id = 'default'`).Scan(&prefs.SidebarWidth, &prefs.TimelineWidth, &prefs.ViewMode, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return prefs, nil
	}
	if err == nil {
		markReadOnScroll = stored == 1
	}
	return prefs, err
}

func validReaderPreferences(prefs ReaderPreferences) bool {
	return prefs.SidebarWidth >= minSidebarWidth && prefs.SidebarWidth <= maxSidebarWidth && prefs.TimelineWidth >= minTimelineWidth && prefs.TimelineWidth <= maxTimelineWidth && (prefs.ViewMode == "comfortable" || prefs.ViewMode == "compact")
}

func applyModulePreferences(d *Dashboard) error {
	rows, err := db.Query(`SELECT module_id FROM module_preferences WHERE hidden = 1`)
	if err != nil {
		return err
	}
	hidden := make([]string, 0, len(dashboardModuleIDs))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if _, ok := dashboardModuleIDs[id]; ok {
			hidden = append(hidden, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	sort.Strings(hidden)
	d.HiddenModules = hidden
	return nil
}

func applyModuleLayouts(d *Dashboard) error {
	rows, err := db.Query(`SELECT module_id, height_px, span FROM module_layouts`)
	if err != nil {
		return err
	}
	layout := make(map[string]ModuleLayoutState, len(dashboardModuleIDs))
	for rows.Next() {
		var id string
		var state ModuleLayoutState
		if err := rows.Scan(&id, &state.HeightPx, &state.Span); err != nil {
			rows.Close()
			return err
		}
		if _, ok := dashboardModuleIDs[id]; !ok {
			continue
		}
		if state.HeightPx < minModuleHeight || state.HeightPx > maxModuleHeight || state.Span < minModuleSpan || state.Span > maxModuleSpan {
			continue
		}
		layout[id] = state
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(layout) == 0 {
		d.ModuleLayout = nil
		return nil
	}
	d.ModuleLayout = layout
	return nil
}

func decodeMutation(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return errors.New("操作数据格式错误")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("操作数据格式错误")
	}
	var id string
	switch value := dst.(type) {
	case *actionPreferenceRequest:
		id = value.ID
	case *signalPreferenceRequest:
		id = value.ID
	case *modulePreferenceRequest:
		id = value.ID
	case *moduleLayoutRequest:
		id = value.ID
	}
	if !validText(id, 64, true) {
		return errors.New("条目标识不合法")
	}
	return nil
}

func mutationMatched(result sql.Result) bool {
	rows, err := result.RowsAffected()
	return err == nil && rows > 0
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func prunePreferences() {
	_, _ = db.Exec(`DELETE FROM action_preferences WHERE action_id NOT IN (SELECT action_id FROM action_preferences ORDER BY updated_at DESC LIMIT 1000)`)
	_, _ = db.Exec(`DELETE FROM signal_preferences WHERE signal_id NOT IN (SELECT signal_id FROM signal_preferences ORDER BY updated_at DESC LIMIT 1000)`)
	_, _ = db.Exec(`DELETE FROM news_preferences WHERE news_id NOT IN (SELECT news_id FROM news_preferences ORDER BY updated_at DESC LIMIT 1000)
AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id = ? AND c.profile_id = ? AND c.state = 'active' AND c.item_key = 'news:' || news_preferences.news_id)`, appDataScope.OwnerID, appDataScope.ProfileID)
	_, _ = db.Exec(`DELETE FROM module_preferences WHERE module_id NOT IN (SELECT module_id FROM module_preferences ORDER BY updated_at DESC LIMIT 100)`)
	_, _ = db.Exec(`DELETE FROM module_layouts WHERE module_id NOT IN (SELECT module_id FROM module_layouts ORDER BY updated_at DESC LIMIT 100)`)
	_, _ = db.Exec(`DELETE FROM item_preferences WHERE item_key NOT IN (SELECT item_key FROM item_preferences ORDER BY updated_at DESC LIMIT 2000)
	AND starred = 0 AND read_later = 0
	AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id = ? AND c.profile_id = ? AND c.state = 'active' AND c.item_key = item_preferences.item_key)`, appDataScope.OwnerID, appDataScope.ProfileID)
	_, _ = db.Exec(`DELETE FROM recording_preferences WHERE recording_id NOT IN (SELECT record_id FROM recordings ORDER BY updated_at DESC LIMIT 2000)
	AND starred = 0
	AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id = ? AND c.profile_id = ? AND c.state = 'active' AND c.item_key = 'recording:' || recording_preferences.recording_id)`, appDataScope.OwnerID, appDataScope.ProfileID)
	_, _ = db.Exec(`DELETE FROM recording_actions WHERE recording_id NOT IN (SELECT record_id FROM recordings ORDER BY updated_at DESC LIMIT 2000)
AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id = ? AND c.profile_id = ? AND c.state = 'active' AND c.item_key = 'recording:' || recording_actions.recording_id)`, appDataScope.OwnerID, appDataScope.ProfileID)
	_, _ = db.Exec(`DELETE FROM recordings WHERE record_id NOT IN (SELECT record_id FROM recordings ORDER BY updated_at DESC LIMIT 200)
	AND NOT EXISTS (SELECT 1 FROM stable_context_items c WHERE c.owner_id = ? AND c.profile_id = ? AND c.state = 'active' AND c.item_key = 'recording:' || recordings.record_id)`, appDataScope.OwnerID, appDataScope.ProfileID)
	// Feed history is bounded by each source's retention_days/retention_limit in
	// pruneSourceNews. A global row cap here would silently erase other sources'
	// history whenever the user changes an unrelated preference.
	pruneStableContextTombstones()
}

func putDashboard(w http.ResponseWriter, r *http.Request) {
	putDashboardInternal(w, r, "", "", "")
}

// putDashboardWithIdempotency is used by the profile-local maintenance bridge.
// The idempotency row and the dashboard projection are committed together so
// a lost response can be replayed without applying the draft twice.
func putDashboardWithIdempotency(w http.ResponseWriter, r *http.Request, operation, key string) {
	putDashboardInternal(w, r, operation, key, "")
}

func putDashboardWithIdempotencyFingerprint(w http.ResponseWriter, r *http.Request, operation, key, fingerprint string) {
	putDashboardInternal(w, r, operation, key, fingerprint)
}

func putDashboardInternal(w http.ResponseWriter, r *http.Request, operation, key, suppliedFingerprint string) {
	var d Dashboard
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, "看板数据格式错误")
		return
	}
	dec := json.NewDecoder(bytes.NewReader(rawBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		app.UserErr(w, http.StatusBadRequest, "看板数据格式错误")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		app.UserErr(w, http.StatusBadRequest, "看板数据格式错误")
		return
	}
	// These fields are read-only projections derived from Focus Space tables.
	// Accept a GET-shaped payload for compatibility, but never persist or trust
	// a caller-supplied copy of the projection.
	d.FocusSpace = nil
	d.FocusAnalysis = nil
	if err := validateDashboard(d); err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// A refresh is an additive projection update. The Agent may send only the
	// bounded records it saw this turn; it must not make an omitted record
	// disappear from the user's local history. User-owned dismissal and
	// completion preferences are applied later, independently of this merge.
	if err := mergePreviousDashboard(&d); err != nil {
		app.HTTPErr(w, err)
		return
	}
	// Action feedback and dashboard refresh share the same SQLite state. Keep
	// their critical section together so a refresh cannot race a user accept or
	// scheduling receipt.
	actionWorkflowMu.Lock()
	defer actionWorkflowMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	var fingerprint string
	if operation != "" {
		fingerprint = suppliedFingerprint
		if fingerprint == "" {
			fingerprint = appDataMutationFingerprint(operation, rawBody, r.URL.RawQuery)
		}
		if cached, replayed, lookupErr := loadIdempotentActionResponseTx(tx, operation, key, fingerprint); lookupErr != nil {
			if errors.Is(lookupErr, errActionIdempotencyConflict) {
				actionWorkflowError(w, http.StatusConflict, "idempotency_conflict", lookupErr.Error(), 0)
			} else {
				app.HTTPErr(w, lookupErr)
			}
			return
		} else if replayed {
			w.Header().Set("X-Zettlab-Idempotent-Replay", "true")
			writeActionWorkflowResponse(w, http.StatusOK, cached)
			return
		}
	}
	for _, e := range d.Evidence {
		_, err = tx.Exec(`INSERT INTO evidence(id,title,locator,observed_at,quote,source_type,content,content_format) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, locator=excluded.locator,
observed_at=excluded.observed_at, quote=excluded.quote, source_type=excluded.source_type,
content=excluded.content, content_format=excluded.content_format`, e.ID, e.Title, e.Locator, e.ObservedAt, e.Quote, e.SourceType, e.Content, e.ContentFormat)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromEvidence(e)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for i, a := range d.Actions {
		effective, reconcileErr := reconcileAgentActionTx(tx, a)
		if reconcileErr != nil {
			app.HTTPErr(w, reconcileErr)
			return
		}
		d.Actions[i] = effective
		ids, _ := json.Marshal(effective.SourceIDs)
		_, err = tx.Exec(`INSERT INTO actions(id,task,owner,due_at,source_ids,status,confirmation_status,priority) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET task=excluded.task, owner=excluded.owner, due_at=excluded.due_at,
source_ids=excluded.source_ids, status=excluded.status, confirmation_status=excluded.confirmation_status,
priority=excluded.priority`, effective.ID, effective.Task, effective.Owner, effective.DueAt, ids, effective.Status, effective.ConfirmationStatus, effective.Priority)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromAction(effective)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for _, m := range d.Metrics {
		ids, _ := json.Marshal(m.SourceIDs)
		_, err = tx.Exec(`INSERT INTO metrics(id,name,value,period,comparison,source_ids,status) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, value=excluded.value, period=excluded.period,
comparison=excluded.comparison, source_ids=excluded.source_ids, status=excluded.status`, m.ID, m.Name, m.Value, m.Period, m.Comparison, ids, m.Status)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromMetric(m)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for _, p := range d.Projects {
		blockers, _ := json.Marshal(p.Blockers)
		ids, _ := json.Marshal(p.SourceIDs)
		_, err = tx.Exec(`INSERT INTO projects(id,name,status,summary,blockers,source_ids) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, status=excluded.status, summary=excluded.summary,
blockers=excluded.blockers, source_ids=excluded.source_ids`, p.ID, p.Name, p.Status, p.Summary, blockers, ids)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromProject(p)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for _, s := range d.Signals {
		ids, _ := json.Marshal(s.SourceIDs)
		counterevidence, _ := json.Marshal(s.Counterevidence)
		_, err = tx.Exec(`INSERT INTO signals(id,title,detail,source_ids,confidence,counterevidence,status) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, detail=excluded.detail,
source_ids=excluded.source_ids, confidence=excluded.confidence,
counterevidence=excluded.counterevidence, status=excluded.status`, s.ID, s.Title, s.Detail, ids, s.Confidence, counterevidence, s.Status)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromSignal(s)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if err = upsertDiscoveriesTx(tx, d.Discoveries, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	for _, input := range d.Source.Inputs {
		if !validText(input.ID, 128, true) {
			continue
		}
		if _, err = tx.Exec(`UPDATE feed_sources SET status = ?, records_seen = ?, records_used = ?, last_sync_at = ?, last_error = ?, updated_at = ? WHERE id = ?`, input.Status, input.RecordsSeen, input.RecordsUsed, input.LastReadAt, input.Message, now, input.ID); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = completeSyncRequestsTx(tx, input, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if d.News != nil {
		for _, item := range d.News {
			origin := item.Origin
			if origin == "" {
				origin = "agent"
			}
			_, err = tx.Exec(`
INSERT INTO news(id,external_id,title,url,source,source_id,origin,source_type,view_type,author,published_at,summary_text,content_format,safe_html,thumbnail_url,media_url,media_mime,duration_seconds,content_hash,fetched_at,permission_scope,source_level,failure_reason,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET title=excluded.title, url=excluded.url, source=excluded.source,
source_id=excluded.source_id, origin=excluded.origin, source_type=excluded.source_type,
view_type=excluded.view_type, author=excluded.author, published_at=excluded.published_at,
summary_text=excluded.summary_text, content_format=excluded.content_format, safe_html=excluded.safe_html,
thumbnail_url=excluded.thumbnail_url, media_url=excluded.media_url, media_mime=excluded.media_mime,
duration_seconds=excluded.duration_seconds,
external_id=CASE WHEN excluded.external_id <> '' THEN excluded.external_id ELSE news.external_id END,
content_hash=excluded.content_hash, fetched_at=excluded.fetched_at,
permission_scope=excluded.permission_scope, source_level=excluded.source_level,
failure_reason=excluded.failure_reason, updated_at=excluded.updated_at`,
				item.ID, firstNonEmpty(item.ExternalID, item.ID), item.Title, item.URL, item.Source, item.SourceID, origin, item.SourceType, item.ViewType, item.Author, item.PublishedAt, item.SummaryText, item.ContentFormat, item.SafeHTML, item.ThumbnailURL, item.MediaURL, item.MediaMIME, item.DurationSecs, item.ContentHash, firstNonEmpty(item.FetchedAt, now), item.PermissionScope, item.SourceLevel, item.FailureReason, now)
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			if err = refreshActiveStableContextTx(tx, stableSnapshotFromNews(item)); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
	}
	if d.Recordings != nil {
		for _, item := range d.Recordings {
			segments, _ := json.Marshal(item.Segments)
			speakerNames, _ := json.Marshal(item.SpeakerNames)
			actionIDs, _ := json.Marshal(item.ActionIDs)
			_, err = tx.Exec(`
INSERT INTO recordings(record_id,title,status,duration_seconds,started_at,created_at,updated_at,speaker_count,audio_ref,transcript,transcript_truncated,segments_json,segments_truncated,speaker_names_json,summary,per_speaker_summary,report,action_ids_json)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(record_id) DO UPDATE SET title=excluded.title, status=excluded.status,
duration_seconds=excluded.duration_seconds, started_at=excluded.started_at, created_at=excluded.created_at,
updated_at=excluded.updated_at, speaker_count=excluded.speaker_count, audio_ref=excluded.audio_ref,
transcript=excluded.transcript, transcript_truncated=excluded.transcript_truncated, segments_json=excluded.segments_json,
segments_truncated=excluded.segments_truncated, speaker_names_json=excluded.speaker_names_json,
summary=excluded.summary, per_speaker_summary=excluded.per_speaker_summary, report=excluded.report,
action_ids_json=excluded.action_ids_json`, item.ID, item.Title, item.Status, item.DurationSecs, item.StartedAt, item.CreatedAt, item.UpdatedAt, item.SpeakerCount, strings.TrimPrefix(item.AudioRef, "/"), item.Transcript, boolInt(item.TranscriptTruncated), segments, boolInt(item.SegmentsTruncated), speakerNames, item.Summary, item.PerSpeakerSummary, item.Report, actionIDs)
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			if err = refreshActiveStableContextTx(tx, stableSnapshotFromRecording(item)); err != nil {
				app.HTTPErr(w, err)
				return
			}
			for _, actionID := range item.ActionIDs {
				if _, err = tx.Exec(`INSERT OR IGNORE INTO recording_actions(recording_id, action_id, created_at) VALUES(?,?,?)`, item.ID, actionID, now); err != nil {
					app.HTTPErr(w, err)
					return
				}
			}
		}
	}
	for _, x := range d.Decisions {
		options, _ := json.Marshal(x.Options)
		ids, _ := json.Marshal(x.SourceIDs)
		assumptions, _ := json.Marshal(x.Assumptions)
		risks, _ := json.Marshal(x.Risks)
		_, err = tx.Exec(`INSERT INTO decisions(id,question,options,recommendation,source_ids,assumptions,risks,status) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET question=excluded.question, options=excluded.options,
recommendation=excluded.recommendation, source_ids=excluded.source_ids, assumptions=excluded.assumptions,
risks=excluded.risks, status=excluded.status`, x.ID, x.Question, options, x.Recommendation, ids, assumptions, risks, x.Status)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err = refreshActiveStableContextTx(tx, stableSnapshotFromDecision(x)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err = storeReaderFocusRunTxWithFingerprint(tx, d, fingerprint); err != nil {
		if errors.Is(err, errReaderFocusPayloadConflict) {
			app.UserErr(w, http.StatusConflict, "关注主题分析批次与已有结果不一致，请使用原幂等键重试")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	// The claim is a transport-only reservation. Never expose it in the
	// persisted dashboard snapshot after the atomic apply succeeds.
	d.ReaderFocusClaimID = ""
	if err = storeReaderDigestReceiptsTx(tx, d.ReaderDigests); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err = storeArticleAIOverviewsTx(tx, d.ArticleAIOverviews); err != nil {
		app.HTTPErr(w, err)
		return
	}
	d.ArticleAIOverviews = nil
	if err = pruneActionWorkflowsTx(tx); err != nil {
		app.HTTPErr(w, err)
		return
	}
	payload, err := json.Marshal(d)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	_, err = tx.Exec(`INSERT INTO dashboard_runs(generated_at,source_status,source_message,records_seen,records_used,payload_json) VALUES(?,?,?,?,?,?)`, d.GeneratedAt, d.Source.Status, d.Source.Message, d.Source.RecordsSeen, d.Source.RecordsUsed, payload)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if _, err = tx.Exec(`DELETE FROM dashboard_runs WHERE id NOT IN (SELECT id FROM dashboard_runs ORDER BY id DESC LIMIT 30)`); err != nil {
		app.HTTPErr(w, err)
		return
	}
	result := map[string]any{"ok": true, "generated_at": d.GeneratedAt}
	var resultRaw []byte
	if operation != "" {
		resultRaw, err = storeIdempotentActionResponseTx(tx, operation, key, fingerprint, result)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	} else {
		resultRaw, err = json.Marshal(result)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if operation != "" {
		writeActionWorkflowResponse(w, http.StatusOK, resultRaw)
		return
	}
	app.WriteJSON(w, http.StatusOK, result)
}

// mergePreviousDashboard keeps the last known local projection when a source
// refresh is partial or returns only a recent window. It is deliberately
// bounded by the same display caps as validation, so a malformed or repeated
// refresh cannot grow the in-memory payload without limit. The source tables
// are also upserted below; this merge keeps the latest dashboard_runs payload
// self-contained for older readers that only understand that contract.
func mergePreviousDashboard(d *Dashboard) error {
	var raw string
	err := db.QueryRow(`SELECT payload_json FROM dashboard_runs ORDER BY id DESC LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var previous Dashboard
	if err := json.Unmarshal([]byte(raw), &previous); err != nil {
		return fmt.Errorf("读取上一份看板快照: %w", err)
	}
	d.Evidence = mergeDashboardItems(d.Evidence, previous.Evidence, func(item Evidence) string { return item.ID }, maxItems)
	d.Actions = mergeDashboardItems(d.Actions, previous.Actions, func(item Action) string { return item.ID }, maxItems)
	d.Metrics = mergeDashboardItems(d.Metrics, previous.Metrics, func(item Metric) string { return item.ID }, maxItems)
	d.Projects = mergeDashboardItems(d.Projects, previous.Projects, func(item Project) string { return item.ID }, maxItems)
	d.Signals = mergeDashboardItems(d.Signals, previous.Signals, func(item Signal) string { return item.ID }, maxItems)
	if d.MorningBrief == nil {
		d.MorningBrief = previous.MorningBrief
	}
	d.News = mergeDashboardItems(d.News, previous.News, func(item NewsItem) string { return item.ID }, maxItems)
	d.Decisions = mergeDashboardItems(d.Decisions, previous.Decisions, func(item Decision) string { return item.ID }, maxItems)
	d.Recordings = mergeDashboardItems(d.Recordings, previous.Recordings, func(item Recording) string { return item.ID }, 20)
	return nil
}

func mergeDashboardItems[T any](current, previous []T, id func(T) string, limit int) []T {
	if limit <= 0 {
		return current
	}
	result := make([]T, 0, minInt(limit, len(current)+len(previous)))
	seen := make(map[string]struct{}, len(current)+len(previous))
	for _, item := range current {
		key := id(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
		if len(result) == limit {
			return result
		}
	}
	for _, item := range previous {
		key := id(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
		if len(result) == limit {
			break
		}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func validateDashboard(d Dashboard) error {
	if d.SchemaVersion != 1 {
		return errors.New("看板版本不受支持")
	}
	if !validDashboardSourceKind(d.Source.Kind) {
		return errors.New("看板来源不受支持")
	}
	if !validText(d.GeneratedAt, 80, true) || !validText(d.Source.Kind, 64, true) || !validText(d.Source.LastReadAt, 80, false) || !validText(d.Source.Message, 500, false) {
		return errors.New("看板来源信息过长")
	}
	if d.Source.Status != "ok" && d.Source.Status != "partial" && d.Source.Status != "unavailable" {
		return errors.New("看板来源状态不合法")
	}
	overviewLimit := maintenanceOverviewLimit
	if state, active, stateErr := readerFocusMaintenanceState(); stateErr == nil {
		if active {
			overviewLimit = state.Policy.PerRunLimit
			if overviewLimit < 1 || overviewLimit > readerFocusPerRunLimit {
				overviewLimit = readerFocusPerRunLimit
			}
		} else if key, _ := readerFocusRunKey(state, d); key != "" {
			// Preserve the bounded envelope for an exact idempotent replay after
			// a bootstrap topic has transitioned out of active maintenance.
			if exists, err := readerFocusRunExists(key); err == nil && exists {
				overviewLimit = readerFocusPerRunLimit
			}
		}
	}
	if len(d.Evidence) > maxItems || len(d.Actions) > maxItems || len(d.Metrics) > maxItems || len(d.Projects) > maxItems || len(d.Signals) > maxItems || len(d.News) > maxItems || len(d.Decisions) > maxItems || len(d.Recordings) > 20 || len(d.Sources) > 100 || len(d.Discoveries) > 20 || len(d.ReaderDigests) > maxReaderDigestReceipts || len(d.ArticleAIOverviews) > overviewLimit {
		return errors.New("看板条目数量超限")
	}
	if d.Source.RecordsSeen < 0 || d.Source.RecordsUsed < 0 || d.Source.RecordsUsed > d.Source.RecordsSeen {
		return errors.New("看板来源数量不合法")
	}
	if err := validateInputStatuses(d.Source.Inputs); err != nil {
		return err
	}
	if err := validateEvidence(d.Evidence); err != nil {
		return err
	}
	if err := validateActions(d.Actions); err != nil {
		return err
	}
	if err := validateMetrics(d.Metrics); err != nil {
		return err
	}
	if err := validateProjects(d.Projects); err != nil {
		return err
	}
	if err := validateSignals(d.Signals); err != nil {
		return err
	}
	if err := validateMorningBrief(d.MorningBrief); err != nil {
		return err
	}
	if err := validateNews(d.News); err != nil {
		return err
	}
	if err := validateRecordings(d.Recordings); err != nil {
		return err
	}
	if err := validateSources(d.Sources); err != nil {
		return err
	}
	if err := validateDiscoveries(d.Discoveries); err != nil {
		return err
	}
	if err := validateDecisions(d.Decisions); err != nil {
		return err
	}
	if err := validateReaderDigestReceipts(d.ReaderDigests); err != nil {
		return err
	}
	if err := validateArticleAIOverviews(d.ArticleAIOverviews, overviewLimit); err != nil {
		return err
	}
	if err := validateReaderFocusProjection(d.ReaderFocus); err != nil {
		return err
	}
	if err := validateReaderRecommendationsProjection(d.ReaderRecommendations); err != nil {
		return err
	}
	if err := validateReaderFocusClaimID(d); err != nil {
		return err
	}
	if err := validateHiddenModules(d.HiddenModules); err != nil {
		return err
	}
	return nil
}

func validateMorningBrief(brief *MorningBrief) error {
	if brief == nil {
		return nil
	}
	if !validText(brief.ID, 128, true) || !validText(brief.Title, 160, true) ||
		!validText(brief.GeneratedAt, 80, true) || !validText(brief.Summary, 12000, true) {
		return errors.New("晨报字段不合法")
	}
	if brief.Status != "ready" && brief.Status != "partial" && brief.Status != "unavailable" {
		return errors.New("晨报状态不合法")
	}
	if len(brief.Priorities) > 10 || len(brief.Risks) > 10 || len(brief.SourceIDs) > 20 {
		return errors.New("晨报条目数量超限")
	}
	items := make([]string, 0, len(brief.Priorities)+len(brief.Risks)+len(brief.SourceIDs))
	items = append(items, brief.Priorities...)
	items = append(items, brief.Risks...)
	items = append(items, brief.SourceIDs...)
	for _, item := range items {
		if !validText(item, 1000, true) {
			return errors.New("晨报条目字段不合法")
		}
	}
	return nil
}

func validDashboardSourceKind(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "profile_meetings", "plaud", "local_sources", "mixed", "agent", "hybrid":
		return true
	default:
		return false
	}
}

func validateInputStatuses(items []InputStatus) error {
	if len(items) > 20 {
		return errors.New("输入源数量超限")
	}
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Kind, 32, true) || !validText(item.Label, 120, true) || !validText(item.LastReadAt, 80, false) || !validText(item.Message, 500, false) {
			return errors.New("输入源字段不合法")
		}
		if item.Status != "ok" && item.Status != "partial" && item.Status != "unavailable" {
			return errors.New("输入源状态不合法")
		}
		if item.RecordsSeen < 0 || item.RecordsUsed < 0 || item.RecordsUsed > item.RecordsSeen {
			return errors.New("输入源数量不合法")
		}
	}
	return nil
}

func validateHiddenModules(ids []string) error {
	seen := map[string]struct{}{}
	for _, id := range ids {
		if !validText(id, 32, true) {
			return errors.New("模块标识不合法")
		}
		if _, ok := dashboardModuleIDs[id]; !ok {
			return errors.New("模块标识不合法")
		}
		if _, exists := seen[id]; exists {
			return errors.New("模块标识重复")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateNews(items []NewsItem) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.ExternalID, 512, false) || !validText(item.Title, 240, true) || !validText(item.URL, 1000, true) || !validHTTPURL(item.URL) || !validText(item.Source, 120, true) || !validText(item.SourceID, 128, false) || !validText(item.Origin, 32, false) || !validText(item.SourceType, 32, false) || !validText(item.Author, 120, false) || !validText(item.PublishedAt, 80, false) || !validText(item.SummaryText, 4000, false) || !validText(item.SafeHTML, 12000, false) || !validText(item.ContentHash, 128, false) || !validText(item.FetchedAt, 80, false) || !validText(item.PermissionScope, 64, false) || !validText(item.SourceLevel, 64, false) || !validText(item.FailureReason, 1000, false) || !validText(item.ThumbnailURL, 1000, false) || !validOptionalHTTPURL(item.ThumbnailURL) || !validText(item.MediaURL, 1000, false) || !validOptionalHTTPURL(item.MediaURL) || !validText(item.MediaMIME, 120, false) || item.DurationSecs < 0 || item.DurationSecs > 86400 {
			return errors.New("新闻字段不合法")
		}
		if item.ViewType != "" && item.ViewType != "article" && item.ViewType != "social" && item.ViewType != "picture" && item.ViewType != "video" && item.ViewType != "audio" {
			return errors.New("新闻视图类型不合法")
		}
		if item.ContentFormat != "html" && item.ContentFormat != "markdown" && item.ContentFormat != "text" {
			return errors.New("新闻格式不合法")
		}
	}
	return nil
}

func validateArticleAIOverviews(items []ArticleAIOverview, limit int) error {
	if len(items) > limit {
		return errors.New("文章概览数量超限")
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !validActionWorkflowID(item.EntryID) || !validDigestHash(item.ContentHash) ||
			!validText(item.GeneratedAt, 80, true) || !validText(item.Summary, 4000, false) ||
			!validText(item.Impact, 2000, false) || !validText(item.ErrorCode, 64, false) ||
			len(item.KeyPoints) > 3 || item.Status != "ready" && item.Status != "failed" {
			return errors.New("文章概览字段不合法")
		}
		if _, err := time.Parse(time.RFC3339, item.GeneratedAt); err != nil {
			return errors.New("文章概览生成时间不合法")
		}
		if item.Status == "ready" && (strings.TrimSpace(item.Summary) == "" || strings.TrimSpace(item.ErrorCode) != "") ||
			item.Status == "failed" && (strings.TrimSpace(item.Summary) != "" || len(item.KeyPoints) != 0 || strings.TrimSpace(item.Impact) != "" || strings.TrimSpace(item.ErrorCode) == "") {
			return errors.New("文章概览状态字段不合法")
		}
		if !validTextList(item.KeyPoints, 3, 600) {
			return errors.New("文章概览要点不合法")
		}
		key := item.EntryID + "\x00" + item.ContentHash
		if _, exists := seen[key]; exists {
			return errors.New("文章概览重复")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func storeArticleAIOverviewsTx(tx *sql.Tx, items []ArticleAIOverview) error {
	for _, item := range items {
		var currentHash string
		if err := tx.QueryRow(`SELECT content_hash FROM news WHERE id=? AND deleted_at=''`, item.EntryID).Scan(&currentHash); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("文章概览引用了不存在的文章")
			}
			return err
		}
		if currentHash != item.ContentHash {
			return errors.New("文章概览与文章内容不匹配")
		}
		keyPoints, err := json.Marshal(item.KeyPoints)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code)
VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(entry_id,content_hash) DO UPDATE SET prompt_version=excluded.prompt_version,status=excluded.status,summary=excluded.summary,
key_points_json=excluded.key_points_json,impact=excluded.impact,generated_at=excluded.generated_at,error_code=excluded.error_code`,
			item.EntryID, item.ContentHash, articleOverviewPromptVersion, item.Status, item.Summary, string(keyPoints), item.Impact, item.GeneratedAt, item.ErrorCode); err != nil {
			return err
		}
	}
	_, err := tx.Exec(`DELETE FROM article_ai_overviews WHERE rowid IN (
SELECT rowid FROM article_ai_overviews ORDER BY generated_at DESC LIMIT -1 OFFSET 1000)`)
	return err
}

func validateRecordings(items []Recording) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !validRecordingID(item.ID) || !uniqueID(item.ID, seen) || !validText(item.Title, 240, true) || !validText(item.Status, 40, true) || !validText(item.StartedAt, 80, false) || !validText(item.CreatedAt, 80, false) || !validText(item.UpdatedAt, 80, false) || item.DurationSecs < 0 || item.DurationSecs > 86400 || item.SpeakerCount < 0 || item.SpeakerCount > 100 || !validAudioRef(item.AudioRef) || !validText(item.Transcript, 60000, false) || !validText(item.Summary, 12000, false) || !validText(item.PerSpeakerSummary, 12000, false) || !validText(item.Report, 32000, false) || !validIDs(item.ActionIDs) || item.PlaybackSeconds < 0 || item.PlaybackSeconds > 86400 || math.IsNaN(item.PlaybackSeconds) || math.IsInf(item.PlaybackSeconds, 0) {
			return errors.New("录音字段不合法")
		}
		if len(item.Segments) > 500 {
			return errors.New("录音分段数量超限")
		}
		for _, segment := range item.Segments {
			if !validText(segment.Speaker, 120, false) || !validText(segment.Text, 2000, true) || segment.Start < 0 || segment.End < segment.Start || segment.End > 86400 || math.IsNaN(segment.Start) || math.IsNaN(segment.End) || math.IsInf(segment.Start, 0) || math.IsInf(segment.End, 0) {
				return errors.New("录音分段字段不合法")
			}
		}
		if len(item.SpeakerNames) > 100 {
			return errors.New("说话人数量超限")
		}
		for speaker, name := range item.SpeakerNames {
			if !validText(speaker, 120, true) || !validText(name, 120, true) {
				return errors.New("说话人字段不合法")
			}
		}
	}
	return nil
}

func validateSources(items []FeedSource) error {
	seen := map[string]struct{}{}
	for _, source := range items {
		if !validText(source.ID, 128, true) || !uniqueID(source.ID, seen) || !validText(source.Kind, 16, true) || !validText(source.Name, 120, true) || !validText(source.Description, 500, false) || !validPublicFeedURL(source.URL) || source.RecordsSeen < 0 || source.RecordsUsed < 0 || source.RecordsUsed > source.RecordsSeen || !validText(source.Status, 32, true) || !validText(source.LastSyncAt, 80, false) || !validText(source.LastError, 500, false) || !validCollectionMode(source.CollectionMode) || !validCollectionSettings(source.SyncIntervalSecs, source.BackfillDays, source.RetentionDays, source.RetentionLimit) {
			return errors.New("来源目录字段不合法")
		}
	}
	return nil
}

func validHTTPURL(raw string) bool {
	parsed, err := url.ParseRequestURI(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func validOptionalHTTPURL(raw string) bool {
	return raw == "" || validHTTPURL(raw)
}

func validText(value string, max int, required bool) bool {
	if required && strings.TrimSpace(value) == "" {
		return false
	}
	return len([]rune(value)) <= max
}

func validIDs(ids []string) bool {
	if len(ids) > 20 {
		return false
	}
	for _, id := range ids {
		if !validText(id, 64, true) {
			return false
		}
	}
	return true
}

func validTextList(values []string, count, max int) bool {
	if len(values) > count {
		return false
	}
	for _, value := range values {
		if !validText(value, max, true) {
			return false
		}
	}
	return true
}

func uniqueID(id string, seen map[string]struct{}) bool {
	if !validText(id, 64, true) {
		return false
	}
	if _, exists := seen[id]; exists {
		return false
	}
	seen[id] = struct{}{}
	return true
}

func validateEvidence(items []Evidence) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Title, 240, true) || !validText(item.Locator, 240, false) || !validText(item.ObservedAt, 80, false) || !validText(item.Quote, 1200, false) || !validText(item.SourceType, 32, false) || !validText(item.Content, 32000, false) || !validText(item.ContentFormat, 16, false) {
			return errors.New("证据字段不合法")
		}
		if item.Content != "" && item.ContentFormat != "markdown" && item.ContentFormat != "text" {
			return errors.New("证据正文格式不合法")
		}
		if item.SourceType == "obsidian" && item.Content != "" && !validObsidianLocator(item.Locator) {
			return errors.New("Obsidian 笔记路径不合法")
		}
	}
	return nil
}

func validObsidianLocator(value string) bool {
	value = filepath.ToSlash(strings.TrimSpace(value))
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\x00") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func validateActions(items []Action) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Task, 800, true) || !validText(item.Owner, 120, true) || !validText(item.DueAt, 80, false) || !validText(item.Status, 40, true) || !validActionConfirmationStatus(item.ConfirmationStatus) || !validActionPriority(item.Priority) || !validActionScheduleState(item.ScheduleState) || !validIDs(item.SourceIDs) {
			return errors.New("行动字段不合法")
		}
	}
	return nil
}

func validateMetrics(items []Metric) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Name, 240, true) || !validText(item.Value, 160, false) || !validText(item.Period, 80, false) || !validText(item.Comparison, 240, false) || !validText(item.Status, 40, true) || !validIDs(item.SourceIDs) {
			return errors.New("指标字段不合法")
		}
	}
	return nil
}

func validateProjects(items []Project) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Name, 240, true) || !validText(item.Status, 40, true) || !validText(item.Summary, 800, false) || !validTextList(item.Blockers, 10, 240) || !validIDs(item.SourceIDs) {
			return errors.New("项目字段不合法")
		}
	}
	return nil
}

func validateSignals(items []Signal) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Title, 240, true) || !validText(item.Detail, 800, false) || !validText(item.Confidence, 40, true) || !validTextList(item.Counterevidence, 10, 400) || !validText(item.Status, 40, true) || !validIDs(item.SourceIDs) {
			return errors.New("信号字段不合法")
		}
	}
	return nil
}

func validateDecisions(items []Decision) error {
	seen := map[string]struct{}{}
	for _, item := range items {
		if !uniqueID(item.ID, seen) || !validText(item.Question, 500, true) || !validText(item.Recommendation, 800, false) || !validText(item.Status, 40, true) || !validTextList(item.Options, 10, 400) || !validIDs(item.SourceIDs) || !validTextList(item.Assumptions, 10, 400) || !validTextList(item.Risks, 10, 400) {
			return errors.New("决策字段不合法")
		}
	}
	return nil
}

func withLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReaderFocusTextNormalizationAndMatchingUnit(t *testing.T) {
	if got := normalizeReaderFocusText("  AI—Computer\t平台  "); got != "ai computer 平台" {
		t.Fatalf("normalizeReaderFocusText() = %q", got)
	}
	terms, err := normalizeReaderFocusTerms("AI Computer", []string{"AI-Computer", " ai computer ", "人工智能"})
	if err != nil {
		t.Fatalf("normalizeReaderFocusTerms() error = %v", err)
	}
	if got, want := strings.Join(terms, "|"), "ai computer|人工智能"; got != want {
		t.Fatalf("normalized terms = %q, want %q", got, want)
	}

	cases := []struct {
		name string
		text string
		term string
		want bool
	}{
		{name: "ascii word boundary", text: "chairman discusses ai systems", term: "ai", want: true},
		{name: "ascii substring is rejected", text: "email systems", term: "ai", want: false},
		{name: "ascii term beside unicode letter is rejected", text: "éai平台", term: "ai", want: false},
		{name: "mixed latin and han phrase", text: "ai computer产品更新", term: "ai computer", want: true},
		{name: "mixed latin and han prefix", text: "关注产品ai computer", term: "ai computer", want: true},
		{name: "chinese phrase", text: "人工智能电脑值得关注", term: "电脑", want: true},
		{name: "empty term", text: "anything", term: "", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := focusTermInText(tc.text, tc.term); got != tc.want {
				t.Fatalf("focusTermInText(%q, %q) = %v, want %v", tc.text, tc.term, got, tc.want)
			}
		})
	}
}

func TestReaderFocusCandidateScoringWeightsTitleAndEvidenceUnit(t *testing.T) {
	candidate := readerFocusCandidate{
		Title:   "AI Computer 产品更新",
		Source:  "AI Weekly",
		Summary: "这是一篇关于 AI Computer 的摘要",
	}
	topics := []ReaderFocusTopic{
		{ID: "ai", Label: "AI Computer", Terms: []string{"ai computer"}, Enabled: true},
		{ID: "weekly", Label: "Weekly", Terms: []string{"weekly"}, Enabled: true},
		{ID: "off", Label: "Disabled", Terms: []string{"ai"}, Enabled: false},
	}
	score, matched, reason := scoreReaderFocusCandidate(candidate, topics)
	if score != 6 {
		t.Fatalf("score = %d, want title 4 + source 2", score)
	}
	if !readerFocusContainsString(matched, "AI Computer") || !readerFocusContainsString(matched, "Weekly") {
		t.Fatalf("matched topics = %#v", matched)
	}
	if readerFocusContainsString(matched, "Disabled") || reason == "" {
		t.Fatalf("disabled topic or empty reason: matched=%#v reason=%q", matched, reason)
	}
}

func TestReaderFocusEstimateAndPolicyBoundsUnit(t *testing.T) {
	if low, high := estimateReaderFocusTokens(""); low != 300 || high != 600 {
		t.Fatalf("empty estimate = %d..%d, want 300..600", low, high)
	}
	long := strings.Repeat("x", readerFocusInputRunes+100)
	if low, high := estimateReaderFocusTokens(long); low != 1800 || high != 2600 {
		t.Fatalf("bounded estimate = %d..%d, want 1800..2600", low, high)
	}
	valid := defaultReaderFocusPolicy()
	if !validReaderFocusPolicy(valid) {
		t.Fatal("default focus policy should be valid")
	}
	for _, mutate := range []func(*ReaderFocusPolicy){
		func(p *ReaderFocusPolicy) { p.DailyArticleLimit = 0 },
		func(p *ReaderFocusPolicy) { p.DailyTokenBudget = readerFocusMaxTokenBudget + 1 },
		func(p *ReaderFocusPolicy) { p.PerRunLimit = readerFocusPerRunLimit + 1 },
		func(p *ReaderFocusPolicy) { p.BootstrapLimit = 4 },
		func(p *ReaderFocusPolicy) { p.Mode = "unknown" },
	} {
		candidate := valid
		mutate(&candidate)
		if validReaderFocusPolicy(candidate) {
			t.Fatalf("invalid policy accepted: %#v", candidate)
		}
	}
}

func TestReaderFocusTopicBootstrapLimitUsesTopicThenLegacyPolicyUnit(t *testing.T) {
	policy := defaultReaderFocusPolicy()
	policy.BootstrapLimit = 10
	if got := readerFocusTopicBootstrapLimit(ReaderFocusTopic{BootstrapLimit: 3}, policy); got != 3 {
		t.Fatalf("explicit topic bootstrap limit = %d, want 3", got)
	}
	if got := readerFocusTopicBootstrapLimit(ReaderFocusTopic{}, policy); got != 10 {
		t.Fatalf("legacy topic bootstrap limit = %d, want policy default 10", got)
	}
	policy.BootstrapLimit = 4
	if got := readerFocusTopicBootstrapLimit(ReaderFocusTopic{}, policy); got != 5 {
		t.Fatalf("invalid legacy policy fallback = %d, want 5", got)
	}
}

func TestReaderFocusAnalysisInputIncludesBoundedEvidenceLabelsUnit(t *testing.T) {
	candidate := readerFocusCandidate{
		Title:   "AI Computer 产品更新",
		Source:  "技术来源",
		Summary: "RSS 摘要内容",
	}
	got, err := readerFocusAnalysisText(candidate, defaultReaderFocusPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"标题：AI Computer 产品更新", "来源：技术来源", "摘要：RSS 摘要内容"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("analysis input %q missing %q", got, expected)
		}
	}
	if strings.Contains(got, "正文：") {
		t.Fatalf("summary mode unexpectedly selected full text: %q", got)
	}
}

func TestReaderFocusWatchSinceSeparatesHistoricalAndNewUnit(t *testing.T) {
	topics := []ReaderFocusTopic{{Label: "AI", Enabled: true, WatchSince: "2026-08-14T12:00:00Z"}}
	base := readerFocusCandidate{Topics: []string{"AI"}}
	for _, tc := range []struct {
		name string
		when string
		want bool
	}{
		{name: "before watch since", when: "2026-08-14T11:59:59Z", want: false},
		{name: "at watch since", when: "2026-08-14T12:00:00Z", want: true},
		{name: "after watch since", when: "2026-08-14T12:00:01Z", want: true},
		{name: "invalid timestamp is conservative", when: "not-a-time", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			candidate.PublishedAt = tc.when
			if got := readerFocusCandidateIsNew(candidate, topics); got != tc.want {
				t.Fatalf("readerFocusCandidateIsNew() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReaderFocusUpdateUsesRevisionCASAndPreservesWatchSinceFlow(t *testing.T) {
	db = openTestDB(t)
	req := readerFocusUpdateRequest{
		SchemaVersion:    readerFocusSchemaVersion,
		ExpectedRevision: 1,
		Action:           "upsert_topic",
		Topic:            &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer", "人工智能电脑"}},
	}
	if err := updateReaderFocus(req); err != nil {
		t.Fatalf("initial topic update: %v", err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Revision != 2 || len(state.Topics) != 1 {
		t.Fatalf("state after insert = %#v", state)
	}
	watchSince := state.Topics[0].WatchSince
	if _, err := time.Parse(time.RFC3339, watchSince); err != nil {
		t.Fatalf("new topic watch_since = %q: %v", watchSince, err)
	}
	if err := updateReaderFocus(req); !errors.Is(err, errReaderFocusRevisionConflict) {
		t.Fatalf("stale update error = %v, want revision conflict", err)
	}
	enabled := false
	update := readerFocusUpdateRequest{
		SchemaVersion:    readerFocusSchemaVersion,
		ExpectedRevision: state.Revision,
		Action:           "upsert_topic",
		Topic:            &readerFocusTopicPatch{ID: state.Topics[0].ID, Label: "AI Computer 改名", Terms: []string{"ai computer"}, Enabled: &enabled},
	}
	if err := updateReaderFocus(update); err != nil {
		t.Fatalf("topic edit: %v", err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Topics) != 1 || state.Topics[0].Enabled || state.Topics[0].WatchSince != watchSince || state.Topics[0].Revision != 2 {
		t.Fatalf("edited topic lost state: %#v", state.Topics[0])
	}
}

func TestReaderFocusCandidatesFilterUnreadHiddenAndDismissedFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "focus-source", "rss", "Focus Source", "https://example.com/feed", "", 0, "ok", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "focus-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	insert := func(id, title, summary string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, title, "https://example.com/"+id, "Focus Source", now, summary, "text", summary, now, "focus-source", "hash-"+id, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("unread", "AI Computer 发布", "关注 AI Computer")
	insert("read", "AI Computer 已读", "关注 AI Computer")
	insert("dismissed", "AI Computer 忽略", "关注 AI Computer")
	if _, err := db.Exec(`INSERT INTO item_preferences(item_key,is_read,starred,read_later,reading_progress,updated_at) VALUES(?,?,?,?,?,?)`, "news:read", 1, 0, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news_preferences(news_id,dismissed,updated_at) VALUES(?,?,?)`, "dismissed", 1, now); err != nil {
		t.Fatal(err)
	}
	rows, err := queryReaderFocusCandidates([]ReaderFocusTopic{{Label: "AI Computer", Terms: []string{"ai computer"}, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "unread" || rows[0].Score <= 0 {
		t.Fatalf("filtered candidates = %#v", rows)
	}
}

func TestReaderFocusRecommendationsIncludeManualOnlySourceButMaintenanceSkipsFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,auto_extract,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "disabled-focus-source", "rss", "Disabled Focus Source", "https://example.com/disabled-feed", "", 0, "ok", 0, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "disabled-focus-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "disabled-focus-entry", "AI Computer should not be analyzed", "https://example.com/disabled", "Disabled Focus Source", now, "AI Computer 摘要", "text", "", now, "disabled-focus-source", "disabled-hash", now); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"ai computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := queryReaderFocusCandidates([]ReaderFocusTopic{{Label: "AI Computer", Terms: []string{"ai computer"}, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "disabled-focus-entry" || rows[0].AutoExtract {
		t.Fatalf("manual-only recommendation candidate = %#v", rows)
	}
	_, recommendations, err := buildReaderFocusProjections("daily", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations.Items) != 1 || recommendations.Items[0].EntryID != "disabled-focus-entry" {
		t.Fatalf("manual-only recommendation was hidden: %#v", recommendations.Items)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: state.SchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{TopicID: state.Topics[0].ID, Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	overviews, err := readFocusedOverviewCandidates(state, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(overviews) != 0 {
		t.Fatalf("manual-only source entered overview maintenance: %#v", overviews)
	}
	entries, err := readFocusedMaintenanceEntries(state, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("manual-only source entered digest maintenance: %#v", entries)
	}
	apply := Dashboard{ArticleAIOverviews: []ArticleAIOverview{{
		EntryID: "disabled-focus-entry", ContentHash: "disabled-hash", Status: "ready", Summary: "不应写入",
	}}}
	if err := validateReaderFocusMaintenanceApply(&apply); err == nil || !strings.Contains(err.Error(), "未开启自动分析") {
		t.Fatalf("manual-only maintenance apply error = %v", err)
	}
}

func TestReaderFocusRecommendationsExposeHashlessUnreadFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "hashless-source", "rss", "Hashless Source", "https://example.com/feed", "", 0, "ok", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "hashless-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "hashless-entry", "AI Computer without fingerprint", "https://example.com/hashless", "Hashless Source", now, "仍然值得关注的摘要", "text", "", now, "hashless-source", "", now); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := queryReaderFocusCandidates([]ReaderFocusTopic{{Label: "AI Computer", Terms: []string{"ai computer"}, Enabled: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "hashless-entry" || rows[0].Hash != "" || rows[0].Score <= 0 {
		t.Fatalf("hashless candidate = %#v", rows)
	}
	_, recommendations, err := buildReaderFocusProjections("daily", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recommendations.Items) != 1 || recommendations.Items[0].Status != "queued" || !strings.Contains(recommendations.Items[0].Reason, "内容指纹") {
		t.Fatalf("hashless recommendation = %#v", recommendations.Items)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: state.SchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{TopicID: state.Topics[0].ID, Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := readFocusedOverviewCandidates(state, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(maintenance) != 0 {
		t.Fatalf("hashless item entered maintenance candidates: %#v", maintenance)
	}
}

func TestReaderFocusProjectionReportsBoundedPendingCostFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "projection-source", "rss", "Projection Source", "https://example.com/feed", "", 0, "ok", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "projection-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "projection-entry", "AI Computer 新消息", "https://example.com/projection-entry", "Projection Source", now, strings.Repeat("摘要 ", 80), "text", "", now, "projection-source", "projection-hash", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "projection-ready", "AI Computer 已有概览", "https://example.com/projection-ready", "Projection Source", now, strings.Repeat("摘要 ", 80), "text", "", now, "projection-source", "projection-ready-hash", now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code) VALUES(?,?,?,?,?,?,?,?,?)`, "projection-ready", "projection-ready-hash", articleOverviewPromptVersion, "ready", "已确认结论", `["要点"]`, "影响", now, ""); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}}}); err != nil {
		t.Fatal(err)
	}
	focus, recommendations, err := buildReaderFocusProjections("bootstrap", 5)
	if err != nil {
		t.Fatal(err)
	}
	if focus.PendingCount != 1 || focus.EstimatedTokensLow <= 0 || focus.EstimatedTokensHigh < focus.EstimatedTokensLow {
		t.Fatalf("focus projection = %#v", focus)
	}
	if len(recommendations.Items) != 2 || recommendations.Items[0].EntryID != "projection-ready" || recommendations.Items[0].Status != "ready" || recommendations.Items[1].Status != "pending" {
		t.Fatalf("recommendations = %#v", recommendations)
	}
}

func TestReaderFocusRecommendationTopicFilterFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	insertSource := func(id, name string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, "rss", name, "https://example.com/"+id, "", 0, "ok", now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, id, 1, now, now); err != nil {
			t.Fatal(err)
		}
	}
	insertEntry := func(id, sourceID, title string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, title, "https://example.com/"+id, sourceID, now, title+" 摘要", "text", "", now, sourceID, "hash-"+id, now); err != nil {
			t.Fatal(err)
		}
	}
	insertSource("focus-a-source", "Focus A Source")
	insertSource("focus-b-source", "Focus B Source")
	insertEntry("focus-a-entry", "focus-a-source", "AI Computer A")
	insertEntry("focus-b-entry", "focus-b-source", "Robotics B")
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}}}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "Robotics", Terms: []string{"Robotics"}}}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Topics) != 2 {
		t.Fatalf("topics=%#v", state.Topics)
	}
	var first, second ReaderFocusTopic
	for _, topic := range state.Topics {
		if topic.Label == "AI Computer" {
			first = topic
		} else if topic.Label == "Robotics" {
			second = topic
		}
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("topics missing expected labels=%#v", state.Topics)
	}
	projection, recommendations, err := buildReaderFocusProjectionsForTopic("bootstrap", 5, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Topics) != 1 || projection.Topics[0].ID != first.ID || len(recommendations.Items) != 1 || recommendations.Items[0].EntryID != "focus-a-entry" {
		t.Fatalf("topic-filtered recommendations=%#v projection=%#v", recommendations.Items, projection)
	}
	if _, _, err := buildReaderFocusProjectionsForTopic("bootstrap", 5, "missing-topic"); !errors.Is(err, errReaderFocusInvalid) {
		t.Fatalf("missing topic error=%v", err)
	}
	if second.ID == first.ID {
		t.Fatal("topic IDs unexpectedly collided")
	}
}

func TestReaderFocusProjectionReportsCandidateScanTruncationFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "scan-source", "rss", "Scan Source", "https://example.com/scan", "", 0, "ok", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "scan-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= readerFocusCandidateScanLimit; index++ {
		id := fmt.Sprintf("scan-entry-%03d", index)
		if _, err := tx.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, "AI Computer "+id, "https://example.com/"+id, "Scan Source", now, "AI Computer 摘要", "text", "", now, "scan-source", "hash-"+id, now); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}}}); err != nil {
		t.Fatal(err)
	}
	focus, _, err := buildReaderFocusProjections("bootstrap", 5)
	if err != nil {
		t.Fatal(err)
	}
	if focus.CandidateCount != readerFocusCandidateScanLimit || !focus.CandidateScanTruncated {
		t.Fatalf("candidate scan projection = %#v", focus)
	}
}

func TestReaderFocusAppDataMutationReplayAndStrictSchemaFlow(t *testing.T) {
	db = openTestDB(t)
	payload := `{"schema_version":1,"expected_revision":1,"action":"upsert_topic","topic":{"label":"AI Computer","terms":["AI Computer"]}}`
	first := appDataCall(t, "/api/app-data/v1/operations/reader_focus.update", payload, "reader-focus-cas-1")
	if first.Code != 200 {
		t.Fatalf("first focus update status=%d body=%s", first.Code, first.Body.String())
	}
	replay := appDataCall(t, "/api/app-data/v1/operations/reader_focus.update", payload, "reader-focus-cas-1")
	if replay.Code != 200 || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" || replay.Body.String() != first.Body.String() {
		t.Fatalf("focus replay status=%d replay=%q body_equal=%v", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String() == first.Body.String())
	}
	stale := appDataCall(t, "/api/app-data/v1/operations/reader_focus.update", payload, "reader-focus-cas-stale")
	if stale.Code != 409 {
		t.Fatalf("stale focus update status=%d body=%s", stale.Code, stale.Body.String())
	}
	unknown := appDataCall(t, "/api/app-data/v1/operations/reader_focus.update", `{"schema_version":1,"expected_revision":2,"action":"set_policy","policy":{"mode":"recommend_only"},"extra":true}`, "reader-focus-extra")
	if unknown.Code != 400 {
		t.Fatalf("unknown focus field status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestReaderFocusUpdateRejectsKnownFieldsFromOtherActionBranchesUnit(t *testing.T) {
	cases := []struct {
		name string
		req  readerFocusUpdateRequest
	}{
		{
			name: "topic plus policy",
			req: readerFocusUpdateRequest{
				Action: "upsert_topic",
				Topic:  &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"ai computer"}},
				Policy: &readerFocusPolicyPatch{},
			},
		},
		{
			name: "policy plus bootstrap",
			req: readerFocusUpdateRequest{
				Action:    "set_policy",
				Policy:    &readerFocusPolicyPatch{},
				Bootstrap: &readerFocusBootstrapPatch{Decision: "none"},
			},
		},
		{
			name: "remove topic with extra fields",
			req: readerFocusUpdateRequest{
				Action: "remove_topic",
				Topic:  &readerFocusTopicPatch{ID: "topic-1", Label: "ignored"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateReaderFocusUpdateShape(tc.req); err == nil {
				t.Fatal("cross-branch fields were accepted")
			}
		})
	}
	valid := readerFocusUpdateRequest{
		Action: "remove_topic",
		Topic:  &readerFocusTopicPatch{ID: "topic-1"},
	}
	if err := validateReaderFocusUpdateShape(valid); err != nil {
		t.Fatalf("valid remove_topic rejected: %v", err)
	}
}

func TestReaderFocusRunLedgerUsesStableIdempotencyAndRetentionShapeUnit(t *testing.T) {
	if readerFocusRunRetentionDays != 90 || readerFocusRunRetentionLimit != 1024 {
		t.Fatalf("unexpected ledger retention: days=%d rows=%d", readerFocusRunRetentionDays, readerFocusRunRetentionLimit)
	}
	if got := topicIDsFromProjection([]ReaderFocusTopic{{ID: "a", Enabled: true}, {ID: "b", Enabled: false}}); len(got) != 1 || got[0] != "a" {
		t.Fatalf("topicIDsFromProjection() = %#v", got)
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestReaderFocusBootstrapScopesOneTopicWhenMultipleAreEnabledFlow(t *testing.T) {
	db = openTestDB(t)
	for _, label := range []string{"AI Computer", "Robotics"} {
		state, err := readReaderFocusState()
		if err != nil {
			t.Fatal(err)
		}
		if err := updateReaderFocus(readerFocusUpdateRequest{
			SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision,
			Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: label, Terms: []string{label}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	withoutID := readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision,
		Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{Decision: "analyze", Limit: 5},
	}
	if err := updateReaderFocus(withoutID); !errors.Is(err, errReaderFocusInvalid) {
		t.Fatalf("multi-topic bootstrap without topic_id error=%v, want invalid", err)
	}
	unchanged, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Policy.Mode != readerFocusModeRecommendOnly || unchanged.Topics[0].BootstrapStatus != readerFocusBootstrapPending || unchanged.Topics[1].BootstrapStatus != readerFocusBootstrapPending {
		t.Fatalf("ambiguous bootstrap changed state: %#v", unchanged)
	}
	selected := unchanged.Topics[0].ID
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: unchanged.Revision,
		Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: selected, Decision: "analyze", Limit: 10},
	}); err != nil {
		t.Fatal(err)
	}
	finalState, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	var selectedTopic, pendingTopic ReaderFocusTopic
	for _, topic := range finalState.Topics {
		if topic.ID == selected {
			selectedTopic = topic
		} else {
			pendingTopic = topic
		}
	}
	if finalState.Policy.Mode != readerFocusModeAutoNew || finalState.Policy.BootstrapLimit != 5 || selectedTopic.BootstrapLimit != 10 || pendingTopic.BootstrapLimit != 5 || selectedTopic.BootstrapStatus != readerFocusBootstrapDone || selectedTopic.BootstrapMode != "future_only" || pendingTopic.BootstrapStatus != readerFocusBootstrapPending {
		t.Fatalf("topic-scoped bootstrap state = %#v", finalState)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: finalState.Revision,
		Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: pendingTopic.ID, Decision: "future_only", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	independentState, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range independentState.Topics {
		switch topic.ID {
		case selectedTopic.ID:
			if topic.BootstrapLimit != 10 {
				t.Fatalf("second topic decision changed first topic limit: %#v", topic)
			}
		case pendingTopic.ID:
			if topic.BootstrapLimit != 5 {
				t.Fatalf("second topic limit = %d, want 5", topic.BootstrapLimit)
			}
		}
	}
}

func TestReaderFocusCompletedHistoricalTopicLeavesMaintenanceUnit(t *testing.T) {
	state := ReaderFocusState{
		Policy: ReaderFocusPolicy{Mode: readerFocusModeRecommendOnly},
		Topics: []ReaderFocusTopic{
			{ID: "historical", Label: "Historical", Terms: []string{"historical"}, Enabled: true, BootstrapMode: "analyze", BootstrapStatus: readerFocusBootstrapDone},
			{ID: "future", Label: "Future", Terms: []string{"future"}, Enabled: true, BootstrapMode: "future_only", BootstrapStatus: readerFocusBootstrapDone},
		},
	}
	got := readerFocusMaintenanceTopics(state)
	if len(got) != 1 || got[0].ID != "future" {
		t.Fatalf("maintenance topics = %#v, want only completed future-only topic", got)
	}
}

func TestReaderFocusExplicitFutureTopicDoesNotUseAggregateBootstrapQuotaUnit(t *testing.T) {
	now := time.Now().UTC()
	state := ReaderFocusState{
		Policy: ReaderFocusPolicy{Mode: readerFocusModeAutoBootstrap},
		Topics: []ReaderFocusTopic{
			{ID: "history", Label: "历史主题", Terms: []string{"历史主题"}, Enabled: true, BootstrapMode: "analyze", BootstrapStatus: readerFocusBootstrapActive, WatchSince: now.Add(time.Hour).Format(time.RFC3339)},
			{ID: "future", Label: "AI Computer", Terms: []string{"ai computer"}, Enabled: true, BootstrapMode: "future_only", BootstrapStatus: readerFocusBootstrapActive, WatchSince: now.Add(-time.Hour).Format(time.RFC3339)},
		},
	}
	candidate := readerFocusCandidate{ID: "future-entry", Title: "AI Computer 发布新产品", PublishedAt: now.Format(time.RFC3339)}
	scope, topicIDs := readerFocusCandidateScope(candidate, state)
	if scope != "daily" || len(topicIDs) != 1 || topicIDs[0] != "future" {
		t.Fatalf("future-only candidate scope=%q topicIDs=%#v, want daily/future", scope, topicIDs)
	}
}

func TestReaderFocusFutureTopicStillRunsAfterOtherBootstrapQuotaExhaustedFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,auto_extract,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "mixed-focus-source", "rss", "Mixed Focus Source", "https://example.com/mixed-focus", "", 0, "ok", 1, nowText, nowText); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "mixed-focus-source", 1, nowText, nowText); err != nil {
		t.Fatal(err)
	}
	insert := func(id, title, published string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, title, "https://example.com/"+id, "Mixed Focus Source", published, title+" 摘要", "text", "", published, "mixed-focus-source", "hash-"+id, published); err != nil {
			t.Fatal(err)
		}
	}
	insert("history-entry", "历史主题积压", now.Add(-2*time.Hour).Format(time.RFC3339))
	insert("future-entry", "AI Computer 新文章", now.Format(time.RFC3339))
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"历史主题", "AI Computer"} {
		terms := []string{label}
		if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: label, Terms: terms}}); err != nil {
			t.Fatal(err)
		}
		state, err = readReaderFocusState()
		if err != nil {
			t.Fatal(err)
		}
	}
	historyID, futureID := state.Topics[0].ID, state.Topics[1].ID
	if _, err := db.Exec(`UPDATE reader_focus_topics SET watch_since=? WHERE id=?`, now.Add(-time.Hour).Format(time.RFC3339), futureID); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: historyID, Decision: "analyze", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: futureID, Decision: "future_only", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "bootstrap-used", "bootstrap", nowText[:10], `["`+historyID+`"]`, 5, 5, 0, 0, "completed", "bootstrap-used-key", nowText, nowText); err != nil {
		t.Fatal(err)
	}
	items, err := readFocusedOverviewCandidates(state, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EntryID != "future-entry" {
		t.Fatalf("future topic candidates=%#v, want future-entry after bootstrap quota exhaustion", items)
	}
}

func TestReaderFocusBootstrapCounterSurvivesRunRetentionFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,auto_extract,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "retained-focus-source", "rss", "Retained Focus Source", "https://example.com/retained-focus", "", 0, "ok", 1, nowText, nowText); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "retained-focus-source", 1, nowText, nowText); err != nil {
		t.Fatal(err)
	}
	published := now.Add(-time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "retained-focus-entry", "AI Computer 历史文章", "https://example.com/retained-focus-entry", "Retained Focus Source", published, "AI Computer 历史摘要", "text", "", published, "retained-focus-source", "retained-focus-hash", published); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	topicID := state.Topics[0].ID
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{TopicID: topicID, Decision: "analyze", Limit: 3},
	}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	candidates, claim, err := reserveReaderFocusCandidates(state, topicID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.Status != "claimed" || len(candidates) != 1 {
		t.Fatalf("retention claim candidates=%#v claim=%#v", candidates, claim)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	dashboard := readerFocusClaimDashboard(candidates, claim.ID)
	if err := storeReaderFocusRunTx(tx, dashboard); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var persisted int
	if err := db.QueryRow(`SELECT bootstrap_processed_count FROM reader_focus_topics WHERE id=?`, topicID).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != 1 {
		t.Fatalf("persisted bootstrap count = %d, want 1", persisted)
	}
	if _, err := db.Exec(`DELETE FROM reader_focus_runs`); err != nil {
		t.Fatal(err)
	}
	used, err := readerFocusTopicBootstrapUsed(topicID)
	if err != nil {
		t.Fatal(err)
	}
	if used != 1 {
		t.Fatalf("bootstrap usage after run retention = %d, want 1", used)
	}
}

func TestReaderFocusShortBootstrapTransitionsToDailyFutureAnalysisFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:short-bootstrap", "short-bootstrap")
	insertMaintenanceSource(t, db, "short-bootstrap-source", 0)
	for index := 0; index < 2; index++ {
		entryID := fmt.Sprintf("short-history-%d", index)
		insertMaintenanceEntry(t, db, "short-bootstrap-source", entryID, index)
		if _, err := db.Exec(`UPDATE news SET title=?,summary_text=? WHERE id=?`, "AI Computer 历史 "+entryID, "AI Computer 历史摘要", entryID); err != nil {
			t.Fatal(err)
		}
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}}}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: state.Topics[0].ID, Decision: "analyze", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	originalBoundary := state.Topics[0].WatchSince
	var readOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&readOutput); err != nil {
		t.Fatal(err)
	}
	var readResult readerMaintenanceReadResult
	if err := json.Unmarshal(readOutput.Bytes(), &readResult); err != nil {
		t.Fatal(err)
	}
	if len(readResult.ArticleAIOverviewCandidates) != 2 {
		t.Fatalf("historical candidates=%d, want 2", len(readResult.ArticleAIOverviewCandidates))
	}
	overviews := make([]ArticleAIOverview, 0, len(readResult.ArticleAIOverviewCandidates))
	for _, candidate := range readResult.ArticleAIOverviewCandidates {
		overviews = append(overviews, ArticleAIOverview{EntryID: candidate.EntryID, ContentHash: candidate.ContentHash, Status: "ready", Summary: "结论", KeyPoints: []string{"要点"}, Impact: "影响", GeneratedAt: time.Now().UTC().Format(time.RFC3339)})
	}
	payload, err := json.Marshal(Dashboard{SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339), Source: SourceStatus{Kind: "local_sources", Status: "ok", RecordsSeen: 2, RecordsUsed: 2}, Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{}, Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{}, ArticleAIOverviews: overviews})
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApply(bytes.NewReader(payload), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Policy.Mode != readerFocusModeAutoNew || state.Topics[0].BootstrapStatus != readerFocusBootstrapDone || state.Topics[0].BootstrapMode != "future_only" || state.Topics[0].WatchSince != originalBoundary {
		t.Fatalf("short bootstrap did not enter daily mode: %#v", state)
	}

	futureID := "short-future"
	insertMaintenanceEntry(t, db, "short-bootstrap-source", futureID, 3)
	boundary, err := time.Parse(time.RFC3339, originalBoundary)
	if err != nil {
		t.Fatal(err)
	}
	futureTime := boundary.Add(time.Second).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE news SET title=?,summary_text=?,published_at=?,fetched_at=?,updated_at=? WHERE id=?`, "AI Computer 新文章", "AI Computer 新摘要", futureTime, futureTime, futureTime, futureID); err != nil {
		t.Fatal(err)
	}
	readOutput.Reset()
	if err := runReaderMaintenanceRead(&readOutput); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readOutput.Bytes(), &readResult); err != nil {
		t.Fatal(err)
	}
	if len(readResult.ArticleAIOverviewCandidates) != 1 || readResult.ArticleAIOverviewCandidates[0].EntryID != futureID {
		t.Fatalf("daily candidates after short bootstrap=%#v, want %s", readResult.ArticleAIOverviewCandidates, futureID)
	}
}

func TestReaderFocusFailedOverviewIsNotAutomaticallyRetriedFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,auto_extract,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, "focus-failed-source", "rss", "Focus Failed Source", "https://example.com/focus-failed", "", 0, "ok", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "focus-failed-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "failed-entry", "AI Computer 失败文章", "https://example.com/failed-entry", "Focus Failed Source", now, "AI Computer 摘要", "text", "", now, "focus-failed-source", "failed-hash", now); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}}}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: state.Topics[0].ID, Decision: "future_only", Limit: 5}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code) VALUES(?,?,?,?,?,?,?,?,?)`, "failed-entry", "failed-hash", articleOverviewPromptVersion, "failed", "", `[]`, "", now, "ai_unavailable"); err != nil {
		t.Fatal(err)
	}
	state, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	items, err := readFocusedOverviewCandidates(state, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed overview was selected for automatic retry: %#v", items)
	}
}

func TestReaderFocusDailyRecommendationsPreferNewAndFillHistoricalFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "mixed-source", "rss", "Mixed Source", "https://example.com/feed", "", 0, "ok", nowText, nowText); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "mixed-source", 1, nowText, nowText); err != nil {
		t.Fatal(err)
	}
	insert := func(id, published string) {
		t.Helper()
		_, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, "AI Computer "+id, "https://example.com/"+id, "Mixed Source", published, "AI Computer 摘要", "text", "", published, "mixed-source", "hash-"+id, published)
		if err != nil {
			t.Fatal(err)
		}
	}
	watchSince := now.Add(-time.Hour).Format(time.RFC3339)
	insert("historical", now.Add(-2*time.Hour).Format(time.RFC3339))
	insert("newer", now.Add(-30*time.Minute).Format(time.RFC3339))
	insert("newest", now.Add(-10*time.Minute).Format(time.RFC3339))
	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code) VALUES(?,?,?,?,?,?,?,?,?)`, "historical", "hash-historical", articleOverviewPromptVersion, "ready", "已有概览", `["要点"]`, "影响", nowText, ""); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1,
		Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE reader_focus_topics SET watch_since=?`, watchSince); err != nil {
		t.Fatal(err)
	}
	projection, recommendations, err := buildReaderFocusProjections("daily", 5)
	if err != nil {
		t.Fatal(err)
	}
	if projection.HistoricalUnreadCount != 1 {
		t.Fatalf("historical unread count=%d, want 1", projection.HistoricalUnreadCount)
	}
	if len(recommendations.Items) != 3 || recommendations.Items[0].EntryID != "historical" || recommendations.Items[1].EntryID != "newest" || recommendations.Items[2].EntryID != "newer" {
		t.Fatalf("daily recommendation order = %#v", recommendations.Items)
	}
}

func TestReaderFocusReadyOverviewReplayDoesNotSpendAnotherRunFlow(t *testing.T) {
	db = openTestDB(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, "replay-source", "rss", "Replay Source", "https://example.com/feed", "", 0, "ok", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, "replay-source", 1, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at,source_id,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "replay-entry", "AI Computer replay", "https://example.com/replay", "Replay Source", now, "AI Computer 摘要", "text", "", now, "replay-source", "replay-hash", now); err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: 1,
		Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: "AI Computer", Terms: []string{"AI Computer"}},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision,
		Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,status,summary,key_points_json,impact,generated_at,error_code) VALUES(?,?,?,?,?,?,?,?)`, "replay-entry", "replay-hash", "ready", "已有结论", `["要点"]`, "影响", now, ""); err != nil {
		t.Fatal(err)
	}
	overview := ArticleAIOverview{EntryID: "replay-entry", ContentHash: "replay-hash", Status: "ready", Summary: "已有结论", KeyPoints: []string{"要点"}, Impact: "影响", GeneratedAt: now}
	dashboard := Dashboard{ArticleAIOverviews: []ArticleAIOverview{overview}}
	if err := validateReaderFocusMaintenanceApply(&dashboard); err != nil {
		t.Fatalf("ready overview replay rejected: %v", err)
	}
	key, hashes := readerFocusRunKey(state, dashboard)
	if key == "" || len(hashes) != 1 || hashes[0] != "replay-hash" {
		t.Fatalf("focus replay key=%q hashes=%#v", key, hashes)
	}
}

func TestReaderFocusRunKeyIgnoresGenericDigestReceiptsUnit(t *testing.T) {
	state := ReaderFocusState{Policy: ReaderFocusPolicy{Mode: readerFocusModeAutoNew}}
	digestOnly := Dashboard{ReaderDigests: []ReaderDigestReceipt{{ContentHash: "digest-hash"}}}
	if key, hashes := readerFocusRunKey(state, digestOnly); key != "" || len(hashes) != 0 {
		t.Fatalf("digest-only focus key=%q hashes=%#v, want empty", key, hashes)
	}
	combined := Dashboard{ReaderDigests: digestOnly.ReaderDigests, ArticleAIOverviews: []ArticleAIOverview{{EntryID: "entry-1", ContentHash: "overview-hash"}}}
	key, hashes := readerFocusRunKey(state, combined)
	if key == "" || !strings.Contains(key, "entry-1") || len(hashes) != 1 || hashes[0] != "overview-hash" {
		t.Fatalf("combined focus key=%q hashes=%#v", key, hashes)
	}
}

func TestReaderFocusRunKeyDistinguishesEntriesWithSameHashUnit(t *testing.T) {
	first := Dashboard{ArticleAIOverviews: []ArticleAIOverview{{EntryID: "entry-a", ContentHash: "same-hash"}}}
	second := Dashboard{ArticleAIOverviews: []ArticleAIOverview{{EntryID: "entry-b", ContentHash: "same-hash"}}}
	firstKey, _ := readerFocusRunKey(ReaderFocusState{}, first)
	secondKey, _ := readerFocusRunKey(ReaderFocusState{}, second)
	if firstKey == "" || secondKey == "" || firstKey == secondKey {
		t.Fatalf("same hash entries collided: first=%q second=%q", firstKey, secondKey)
	}
}

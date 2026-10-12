package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type readerFocusClaimTestResult struct {
	candidates []readerMaintenanceOverviewCandidate
	claim      *ReaderFocusClaimReceipt
	err        error
}

func readerFocusClaimDashboard(candidates []readerMaintenanceOverviewCandidate, claimID string) Dashboard {
	overviews := make([]ArticleAIOverview, 0, len(candidates))
	for _, candidate := range candidates {
		overviews = append(overviews, ArticleAIOverview{
			EntryID: candidate.EntryID, ContentHash: candidate.ContentHash,
			Status: "ready", Summary: "结论", KeyPoints: []string{"要点"},
			Impact: "影响", GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		})
	}
	return Dashboard{
		SchemaVersion: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:   SourceStatus{Kind: "local_sources", Status: "ok"},
		Evidence: []Evidence{}, Actions: []Action{}, Metrics: []Metric{}, Projects: []Project{},
		Signals: []Signal{}, News: []NewsItem{}, Decisions: []Decision{}, Recordings: []Recording{},
		ReaderFocusClaimID: claimID, ArticleAIOverviews: overviews,
	}
}

func TestReaderFocusClaimSerializesConcurrentReadsAndReservesBudgetFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 3)
	topicID := state.Topics[0].ID

	start := make(chan struct{})
	results := make(chan readerFocusClaimTestResult, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			candidates, claim, err := reserveReaderFocusCandidates(state, topicID, 3)
			results <- readerFocusClaimTestResult{candidates: candidates, claim: claim, err: err}
		}()
	}
	close(start)

	var claimed, busy *readerFocusClaimTestResult
	for index := 0; index < 2; index++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.claim == nil {
			t.Fatalf("concurrent result has no claim: %#v", result)
		}
		switch result.claim.Status {
		case "claimed":
			copy := result
			claimed = &copy
		case "busy":
			copy := result
			busy = &copy
		default:
			t.Fatalf("unexpected claim status: %#v", result.claim)
		}
	}
	if claimed == nil || busy == nil || len(claimed.candidates) != 3 || len(busy.candidates) != 0 || busy.claim.ID != claimed.claim.ID {
		t.Fatalf("concurrent claim results claimed=%#v busy=%#v", claimed, busy)
	}
	usedArticles, err := readerFocusUsedArticles(readerFocusToday())
	if err != nil {
		t.Fatal(err)
	}
	usedTokens, err := readerFocusUsedTokens(readerFocusToday())
	if err != nil {
		t.Fatal(err)
	}
	if usedArticles != 3 || usedTokens <= 0 {
		t.Fatalf("pending claim budget articles=%d tokens=%d", usedArticles, usedTokens)
	}
}

func TestReaderFocusClaimApplyAtDailyLimitIsReplayableAndLeavesOneRunFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 5)
	if _, err := db.Exec(`UPDATE reader_focus_policy SET daily_article_limit=5 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || claim.Status != "claimed" || len(candidates) != 5 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	dashboard := readerFocusClaimDashboard(candidates, claim.ID)
	payload, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "claim-replay-key"); err != nil {
			t.Fatalf("apply attempt %d: %v", attempt+1, err)
		}
	}
	mutated := dashboard
	mutated.ArticleAIOverviews = append([]ArticleAIOverview(nil), dashboard.ArticleAIOverviews...)
	mutated.ArticleAIOverviews[0].Summary = "不同结果"
	mutatedPayload, err := json.Marshal(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(mutatedPayload), &bytes.Buffer{}, "claim-reuse-different-key"); err == nil {
		t.Fatal("completed claim must not authorize a different payload under a new idempotency key")
	}
	// The overview itself can be an unchanged ready cache hit while a caller
	// attempts to smuggle a different Signal/Action/Decision under a new key.
	// That batch identity must still fail closed, including after bootstrap
	// transitions or topic changes.
	tamperedDraft := dashboard
	tamperedDraft.Signals = []Signal{{
		ID: "tampered-signal", Title: "篡改草案", Detail: "不应覆盖既有结果",
		Confidence: "high", Status: "draft",
	}}
	tamperedPayload, err := json.Marshal(tamperedDraft)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(tamperedPayload), &bytes.Buffer{}, "claim-reuse-tampered-draft"); err == nil {
		t.Fatal("completed focus batch accepted a changed draft under a new idempotency key")
	}
	var tamperedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM signals WHERE id=?`, "tampered-signal").Scan(&tamperedCount); err != nil {
		t.Fatal(err)
	}
	if tamperedCount != 0 {
		t.Fatalf("tampered signal was persisted despite rejected batch: %d", tamperedCount)
	}
	legacyReplay := dashboard
	legacyReplay.ReaderFocusClaimID = ""
	legacyPayload, err := json.Marshal(legacyReplay)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(legacyPayload), &bytes.Buffer{}, "claim-reuse-legacy-key"); err == nil {
		t.Fatal("legacy payload must not reuse a consumed claim under a new idempotency key")
	}
	var total, completed, claimItems int
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0) FROM reader_focus_runs`).Scan(&total, &completed); err != nil {
		t.Fatal(err)
	}
	var payloadFingerprint string
	if err := db.QueryRow(`SELECT payload_fingerprint FROM reader_focus_runs WHERE status='completed' LIMIT 1`).Scan(&payloadFingerprint); err != nil {
		t.Fatal(err)
	}
	if len(payloadFingerprint) != 64 {
		t.Fatalf("focus payload fingerprint length=%d, want 64", len(payloadFingerprint))
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_focus_claim_items`).Scan(&claimItems); err != nil {
		t.Fatal(err)
	}
	if total != 1 || completed != 1 || claimItems != 0 {
		t.Fatalf("formal ledger total=%d completed=%d claim_items=%d", total, completed, claimItems)
	}
}

func TestReaderFocusClaimLegacyApplyAutoMatchesInsideTransactionFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 2)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 2 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	// Force the compatibility matcher to use the active transaction. A global
	// DB query here would wait forever for the only connection.
	db.SetMaxOpenConns(1)
	dashboard := readerFocusClaimDashboard(candidates, "")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := storeReaderFocusRunTx(tx, dashboard); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var pending, completed int
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0) FROM reader_focus_runs`).Scan(&pending, &completed); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || completed != 1 {
		t.Fatalf("legacy apply ledger pending=%d completed=%d", pending, completed)
	}
}

func TestReaderFocusClaimPartialLegacyApplyConsumesSubsetAndReleasesRemainderFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "agent:focus-legacy-subset", "focus-legacy-subset")
	state := setupReaderFocusMaintenanceReadFixture(t, 5)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 5 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	partial := readerFocusClaimDashboard(candidates[:3], "")
	payload, err := json.Marshal(partial)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "legacy-subset-key"); err != nil {
		t.Fatalf("legacy partial payload should consume a safe subset: %v", err)
	}
	var pending, completed, processed int
	if err := db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN status='pending' THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN status='completed' THEN 1 ELSE 0 END),0), COALESCE(SUM(processed_count),0) FROM reader_focus_runs`).Scan(&pending, &completed, &processed); err != nil {
		t.Fatal(err)
	}
	if pending != 0 || completed != 1 || processed != 3 {
		t.Fatalf("legacy subset ledger pending=%d completed=%d processed=%d", pending, completed, processed)
	}
	var claimItems int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_focus_claim_items WHERE run_id=?`, claim.ID).Scan(&claimItems); err != nil {
		t.Fatal(err)
	}
	if claimItems != 0 {
		t.Fatalf("consumed claim retained %d items", claimItems)
	}
	// The two entries omitted by the old caller remain eligible for the next
	// bounded read instead of being lost or permanently charged.
	remaining, nextClaim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 2 || nextClaim == nil || nextClaim.Status != "claimed" {
		t.Fatalf("released remainder candidates=%#v claim=%#v", remaining, nextClaim)
	}
}

func TestReaderFocusClaimLegacyApplyAcceptsFourOfFiveSubsetFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 5)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 5 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	legacy := readerFocusClaimDashboard(candidates[:4], "")
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "legacy-four-of-five"); err != nil {
		t.Fatalf("legacy four-item payload should consume a safe subset: %v", err)
	}
	var processed int
	if err := db.QueryRow(`SELECT COALESCE(SUM(processed_count),0) FROM reader_focus_runs WHERE status='completed'`).Scan(&processed); err != nil {
		t.Fatal(err)
	}
	if processed != 4 {
		t.Fatalf("legacy four-item processed=%d, want 4", processed)
	}
	remaining, nextClaim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 || nextClaim == nil || nextClaim.Status != "claimed" {
		t.Fatalf("four-item remainder candidates=%#v claim=%#v", remaining, nextClaim)
	}
}

func TestReaderFocusClaimRejectsDifferentPayloadWhileReservationPendingFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 2)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 2 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	// Simulate a first transport that bound the reservation but disconnected
	// before completing its formal run. A different payload must not take over
	// that pending claim under another idempotency key.
	if _, err := db.Exec(`UPDATE reader_focus_runs SET payload_fingerprint=? WHERE id=?`, strings.Repeat("a", 64), claim.ID); err != nil {
		t.Fatal(err)
	}
	dashboard := readerFocusClaimDashboard(candidates, claim.ID)
	dashboard.Signals = []Signal{{ID: "pending-tampered-signal", Title: "不应写入", Detail: "", Confidence: "high", Status: "draft"}}
	payload, err := json.Marshal(dashboard)
	if err != nil {
		t.Fatal(err)
	}
	if err := runReaderMaintenanceApplyWithKey(bytes.NewReader(payload), &bytes.Buffer{}, "pending-different-payload"); err == nil {
		t.Fatal("different payload unexpectedly consumed pending claim")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM reader_focus_runs WHERE id=?`, claim.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("pending claim status=%q after rejected payload", status)
	}
	var formalCount, signalCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_focus_runs WHERE status='completed'`).Scan(&formalCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM signals WHERE id=?`, "pending-tampered-signal").Scan(&signalCount); err != nil {
		t.Fatal(err)
	}
	if formalCount != 0 || signalCount != 0 {
		t.Fatalf("rejected payload mutated formalCount=%d signalCount=%d", formalCount, signalCount)
	}
}

func TestReaderFocusClaimRejectsApplyAfterTopicPauseFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 1)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "upsert_topic",
		Topic: &readerFocusTopicPatch{ID: state.Topics[0].ID, Label: state.Topics[0].Label, Terms: state.Topics[0].Terms, Enabled: &disabled},
	}); err != nil {
		t.Fatal(err)
	}
	dashboard := readerFocusClaimDashboard(candidates, claim.ID)
	if err := validateReaderFocusMaintenanceApply(&dashboard); err == nil {
		t.Fatal("paused topic claim unexpectedly downgraded to generic apply")
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM reader_focus_runs WHERE id=?`, claim.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("paused apply changed claim status=%q err=%v", status, err)
	}
}

func TestReaderFocusClaimExpiryNeedsExplicitRetryAndStaysBoundedFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 1)
	candidates, claim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 1 {
		t.Fatalf("reservation candidates=%#v claim=%#v", candidates, claim)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.Exec(`UPDATE reader_focus_runs SET expires_at=? WHERE id=?`, past, claim.ID); err != nil {
		t.Fatal(err)
	}
	if err := expireReaderFocusClaims(time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	sameDay, sameDayClaim, err := reserveReaderFocusCandidates(state, state.Topics[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(sameDay) != 0 || sameDayClaim != nil {
		t.Fatalf("expired item retried automatically candidates=%#v claim=%#v", sameDay, sameDayClaim)
	}
	current, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	if err := updateReaderFocus(readerFocusUpdateRequest{
		SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: current.Revision, Action: "set_bootstrap",
		Bootstrap: &readerFocusBootstrapPatch{TopicID: current.Topics[0].ID, Decision: "analyze", Limit: 5},
	}); err != nil {
		t.Fatal(err)
	}
	var oldClaims int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_focus_runs WHERE id=?`, claim.ID).Scan(&oldClaims); err != nil {
		t.Fatal(err)
	}
	if oldClaims != 0 {
		t.Fatalf("explicit retry retained failed claim %q", claim.ID)
	}
	current, err = readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	retried, retryClaim, err := reserveReaderFocusCandidates(current, current.Topics[0].ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || retryClaim == nil || retryClaim.ID == claim.ID {
		t.Fatalf("explicit retry candidates=%#v claim=%#v", retried, retryClaim)
	}

	// The expiry path owns a bounded cleanup even when no successful apply ever
	// reaches the normal run-ledger retention code.
	if _, err := db.Exec(`UPDATE reader_focus_runs SET status='failed',failure_code='claim_expired' WHERE id=?`, retryClaim.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for index := 0; index < readerFocusRunRetentionLimit+2; index++ {
		id := fmt.Sprintf("old-failed-claim-%04d", index)
		if _, err := tx.Exec(`INSERT INTO reader_focus_runs(id,scope,run_day,topic_ids_json,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,status,idempotency_key,created_at,updated_at,selector_key,expires_at,failure_code) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, "daily", readerFocusToday(), `[]`, 0, 0, 0, 0, "failed", "reader-focus-claim:"+id, now, now, "", past, "claim_expired"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := expireReaderFocusClaims(now); err != nil {
		t.Fatal(err)
	}
	var failedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reader_focus_runs WHERE status='failed' AND idempotency_key LIKE 'reader-focus-claim:%'`).Scan(&failedCount); err != nil {
		t.Fatal(err)
	}
	if failedCount > readerFocusRunRetentionLimit {
		t.Fatalf("failed claim retention=%d, limit=%d", failedCount, readerFocusRunRetentionLimit)
	}
}

func TestReaderFocusPendingTopicCountsUsePerItemTopicsFlow(t *testing.T) {
	db = openTestDB(t)
	insertMaintenanceSource(t, db, "claim-topic-source", 0)
	for index, label := range []string{"AI Computer", "Robotics"} {
		id := fmt.Sprintf("claim-topic-entry-%d", index)
		insertMaintenanceEntry(t, db, "claim-topic-source", id, index)
		if _, err := db.Exec(`UPDATE news SET title=?,summary_text=? WHERE id=?`, label, label, id); err != nil {
			t.Fatal(err)
		}
	}
	state, err := readReaderFocusState()
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"AI Computer", "Robotics"} {
		if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "upsert_topic", Topic: &readerFocusTopicPatch{Label: label, Terms: []string{label}}}); err != nil {
			t.Fatal(err)
		}
		state, err = readReaderFocusState()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, topic := range state.Topics {
		if err := updateReaderFocus(readerFocusUpdateRequest{SchemaVersion: readerFocusSchemaVersion, ExpectedRevision: state.Revision, Action: "set_bootstrap", Bootstrap: &readerFocusBootstrapPatch{TopicID: topic.ID, Decision: "analyze", Limit: 5}}); err != nil {
			t.Fatal(err)
		}
		state, err = readReaderFocusState()
		if err != nil {
			t.Fatal(err)
		}
	}
	candidates, claim, err := reserveReaderFocusCandidates(state, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if claim == nil || len(candidates) != 2 {
		t.Fatalf("multi-topic reservation candidates=%#v claim=%#v", candidates, claim)
	}
	counts, err := readerFocusPendingTopicCounts(readerFocusToday())
	if err != nil {
		t.Fatal(err)
	}
	for _, topic := range state.Topics {
		if counts[topic.ID] != 1 {
			t.Fatalf("topic %s pending count=%d, want 1", topic.ID, counts[topic.ID])
		}
	}
}

func TestReaderFocusExplicitTopicReadOverridesOnlyForegroundOverviewSettingFlow(t *testing.T) {
	db = openTestDB(t)
	state := setupReaderFocusMaintenanceReadFixture(t, 2)
	if _, err := db.Exec(`UPDATE app_settings SET auto_generate_article_overviews=0 WHERE id='default'`); err != nil {
		t.Fatal(err)
	}
	var cronOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&cronOutput); err != nil {
		t.Fatal(err)
	}
	var cronResult readerMaintenanceReadResult
	if err := json.Unmarshal(cronOutput.Bytes(), &cronResult); err != nil {
		t.Fatal(err)
	}
	if len(cronResult.ArticleAIOverviewCandidates) != 0 || cronResult.ReaderFocusClaim != nil {
		t.Fatalf("unattended read ignored disabled setting: %#v", cronResult)
	}
	var foregroundOutput bytes.Buffer
	if err := runReaderMaintenanceRead(&foregroundOutput, state.Topics[0].ID); err != nil {
		t.Fatal(err)
	}
	var foregroundResult readerMaintenanceReadResult
	if err := json.Unmarshal(foregroundOutput.Bytes(), &foregroundResult); err != nil {
		t.Fatal(err)
	}
	if len(foregroundResult.ArticleAIOverviewCandidates) != 2 || len(foregroundResult.Entries) != 2 || foregroundResult.ReaderFocusClaim == nil || foregroundResult.ReaderFocusClaim.Status != "claimed" {
		t.Fatalf("explicit topic read did not continue confirmed batch: %s", foregroundOutput.String())
	}
}

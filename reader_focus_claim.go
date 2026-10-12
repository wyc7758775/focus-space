package main

import (
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const readerFocusClaimTTL = 15 * time.Minute

// A claim is deliberately serialized only around the bounded SQLite read and
// insert. Model execution happens after the lock is released, so the sidecar
// cannot block the dashboard's ordinary request path for the duration of an AI
// call.
var readerFocusClaimMu sync.Mutex

type ReaderFocusClaimReceipt struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	CandidateCount int    `json:"candidate_count,omitempty"`
}

type readerFocusClaimReservation struct {
	ID              string
	Status          string
	ExpiresAt       string
	CandidateCount  int
	EstimatedInput  int
	EstimatedOutput int
}

func newReaderFocusClaimID() (string, error) {
	value := make([]byte, 16)
	if _, err := cryptorand.Read(value); err != nil {
		return "", err
	}
	return "rfc-" + hex.EncodeToString(value), nil
}

func readerFocusClaimSelector(topicID string) string {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return "all"
	}
	return "topic:" + topicID
}

func readerFocusClaimSchemaMissing(err error) bool {
	return readerFocusSchemaMissing(err)
}

func expireReaderFocusClaims(now string) error {
	if db == nil {
		return errors.New("数据库未打开")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE reader_focus_claim_items
SET state='expired', updated_at=?
WHERE state='claimed' AND run_id IN (
    SELECT id FROM reader_focus_runs
    WHERE status='pending' AND expires_at<>'' AND expires_at<=?
)`, now, now); err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return nil
		}
		return err
	}
	if _, err = tx.Exec(`UPDATE reader_focus_runs
SET status='failed', failure_code='claim_expired', updated_at=?
WHERE status='pending' AND expires_at<>'' AND expires_at<=?`, now, now); err != nil {
		return err
	}
	parsedNow, parseErr := time.Parse(time.RFC3339, now)
	if parseErr != nil {
		parsedNow = time.Now().UTC()
	}
	cutoff := parsedNow.AddDate(0, 0, -readerFocusRunRetentionDays).Format(time.RFC3339)
	// Failed claims must remain bounded even when no later apply succeeds and
	// therefore the normal run-ledger retention path never executes. Formal
	// runs and active reservations are deliberately outside this deletion.
	if _, err = tx.Exec(`DELETE FROM reader_focus_runs
WHERE status='failed' AND idempotency_key LIKE 'reader-focus-claim:%'
  AND (created_at<? OR id NOT IN (
      SELECT id FROM reader_focus_runs
      WHERE status='failed' AND idempotency_key LIKE 'reader-focus-claim:%'
      ORDER BY created_at DESC,id DESC LIMIT ?
  ))`, cutoff, readerFocusRunRetentionLimit); err != nil {
		return err
	}
	return tx.Commit()
}

func activeReaderFocusClaim(selector, now string) (readerFocusClaimReservation, bool, error) {
	var claim readerFocusClaimReservation
	err := db.QueryRow(`SELECT id,status,expires_at,candidate_count,
       estimated_input_tokens,estimated_output_tokens
FROM reader_focus_runs
WHERE selector_key=? AND status='pending' AND expires_at>? LIMIT 1`, selector, now).Scan(
		&claim.ID, &claim.Status, &claim.ExpiresAt, &claim.CandidateCount,
		&claim.EstimatedInput, &claim.EstimatedOutput)
	if errors.Is(err, sql.ErrNoRows) {
		return readerFocusClaimReservation{}, false, nil
	}
	if err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return readerFocusClaimReservation{}, false, nil
		}
		return readerFocusClaimReservation{}, false, err
	}
	return claim, true, nil
}

// readerFocusClaimExcludedKeys keeps failed/expired attempts out of the same
// day's automatic retry loop. The daily budget remains the recovery guard, but
// this also prevents a connection error from immediately re-selecting the same
// article in the next maintenance tick.
func readerFocusClaimExcludedKeys(day string) (map[string]struct{}, error) {
	rows, err := db.Query(`SELECT i.entry_id,i.content_hash
FROM reader_focus_claim_items i
JOIN reader_focus_runs r ON r.id=i.run_id
WHERE r.run_day=? AND r.status IN ('pending','failed')
  AND i.state IN ('claimed','failed','expired')`, day)
	if err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return map[string]struct{}{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var entryID, contentHash string
		if err := rows.Scan(&entryID, &contentHash); err != nil {
			return nil, err
		}
		result[entryID+"\x00"+contentHash] = struct{}{}
	}
	return result, rows.Err()
}

func readerFocusPendingTopicCounts(day string) (map[string]int, error) {
	rows, err := db.Query(`SELECT i.topic_ids_json
FROM reader_focus_claim_items i
JOIN reader_focus_runs r ON r.id=i.run_id
WHERE r.run_day=? AND r.status IN ('pending','failed')
  AND i.state IN ('claimed','failed','expired')`, day)
	if err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return map[string]int{}, nil
		}
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var topicIDs []string
		if json.Unmarshal([]byte(raw), &topicIDs) != nil {
			continue
		}
		for _, topicID := range topicIDs {
			if topicID != "" {
				result[topicID]++
			}
		}
	}
	return result, rows.Err()
}

// clearFailedReaderFocusClaimsForTopicTx is invoked only by a new explicit
// set_bootstrap(analyze) confirmation. It releases same-day failed/expired
// transport reservations for that topic without touching completed formal
// runs or an in-flight pending batch.
func clearFailedReaderFocusClaimsForTopicTx(tx *sql.Tx, topicID, day string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	_, err := tx.Exec(`DELETE FROM reader_focus_runs
WHERE status='failed' AND run_day=?
  AND idempotency_key LIKE 'reader-focus-claim:%'
  AND id IN (
      SELECT DISTINCT r.id
      FROM reader_focus_runs r
      JOIN reader_focus_claim_items i ON i.run_id=r.id
      JOIN json_each(CASE WHEN json_valid(i.topic_ids_json) THEN i.topic_ids_json ELSE '[]' END) topic
        ON topic.value=?
      WHERE r.status='failed' AND r.run_day=?
        AND i.state IN ('failed','expired')
  )`, day, topicID, day)
	if readerFocusClaimSchemaMissing(err) {
		return nil
	}
	return err
}

func reserveReaderFocusCandidates(state ReaderFocusState, requestedTopicID string, limit int) ([]readerMaintenanceOverviewCandidate, *ReaderFocusClaimReceipt, error) {
	readerFocusClaimMu.Lock()
	defer readerFocusClaimMu.Unlock()

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339)
	if err := expireReaderFocusClaims(nowText); err != nil {
		return nil, nil, err
	}
	selector := readerFocusClaimSelector(requestedTopicID)
	if claim, busy, err := activeReaderFocusClaim(selector, nowText); err != nil {
		return nil, nil, err
	} else if busy {
		return []readerMaintenanceOverviewCandidate{}, &ReaderFocusClaimReceipt{
			ID: claim.ID, Status: "busy", ExpiresAt: claim.ExpiresAt,
		}, nil
	}
	candidates, err := readFocusedOverviewCandidates(state, limit)
	if err != nil || len(candidates) == 0 {
		return candidates, nil, err
	}
	matched, err := queryReaderFocusCandidates(readerFocusMaintenanceTopics(state))
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[string]readerFocusCandidate, len(matched))
	for _, candidate := range matched {
		byKey[candidate.ID+"\x00"+candidate.Hash] = candidate
	}
	claimID, err := newReaderFocusClaimID()
	if err != nil {
		return nil, nil, err
	}
	expiresAt := now.Add(readerFocusClaimTTL).Format(time.RFC3339)
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO reader_focus_runs
(id,scope,run_day,topic_ids_json,candidate_count,processed_count,
 estimated_input_tokens,estimated_output_tokens,status,idempotency_key,
 created_at,updated_at,selector_key,expires_at,failure_code)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, claimID, "daily", nowText[:10], `[]`, 0, 0, 0, 0,
		"pending", "reader-focus-claim:"+claimID, nowText, nowText, selector, expiresAt, ""); err != nil {
		// Another process may have won the selector race. Return a bounded busy
		// response when that is the only failure; never hand out the same batch.
		_ = tx.Rollback()
		if claim, busy, busyErr := activeReaderFocusClaim(selector, nowText); busyErr == nil && busy {
			return []readerMaintenanceOverviewCandidate{}, &ReaderFocusClaimReceipt{ID: claim.ID, Status: "busy", ExpiresAt: claim.ExpiresAt}, nil
		}
		return nil, nil, err
	}
	selected := make([]readerMaintenanceOverviewCandidate, 0, len(candidates))
	runTopicIDs := make(map[string]struct{})
	scope := ""
	estimatedIn, estimatedOut := 0, 0
	for _, candidate := range candidates {
		matchedCandidate, ok := byKey[candidate.EntryID+"\x00"+candidate.ContentHash]
		if !ok {
			continue
		}
		candidateScope, topicIDs := readerFocusCandidateScope(matchedCandidate, state)
		if candidateScope == "" || (scope != "" && scope != candidateScope) {
			continue
		}
		analysisText, textErr := readerFocusAnalysisText(matchedCandidate, state.Policy)
		if textErr != nil || strings.TrimSpace(analysisText) == "" {
			continue
		}
		inputHigh, outputHigh := 0, 0
		_, inputHigh, _, outputHigh = estimateReaderFocusUsage(analysisText)
		topicJSON, _ := json.Marshal(topicIDs)
		result, insertErr := tx.Exec(`INSERT OR IGNORE INTO reader_focus_claim_items
(run_id,entry_id,content_hash,topic_ids_json,estimated_input_tokens,
 estimated_output_tokens,state,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`, claimID, candidate.EntryID, candidate.ContentHash, topicJSON,
			inputHigh, outputHigh, "claimed", nowText, nowText)
		if insertErr != nil {
			return nil, nil, insertErr
		}
		affected, affectedErr := result.RowsAffected()
		if affectedErr != nil {
			return nil, nil, affectedErr
		}
		if affected == 0 {
			continue
		}
		selected = append(selected, candidate)
		if scope == "" {
			scope = candidateScope
		}
		for _, topicID := range topicIDs {
			runTopicIDs[topicID] = struct{}{}
		}
		estimatedIn += inputHigh
		estimatedOut += outputHigh
	}
	if len(selected) == 0 {
		return []readerMaintenanceOverviewCandidate{}, nil, nil
	}
	topicIDs := make([]string, 0, len(runTopicIDs))
	for topicID := range runTopicIDs {
		topicIDs = append(topicIDs, topicID)
	}
	sort.Strings(topicIDs)
	topicJSON, _ := json.Marshal(topicIDs)
	if _, err = tx.Exec(`UPDATE reader_focus_runs
SET scope=?,topic_ids_json=?,candidate_count=?,estimated_input_tokens=?,
estimated_output_tokens=?,updated_at=? WHERE id=?`, scope, topicJSON, len(selected),
		estimatedIn, estimatedOut, nowText, claimID); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return selected, &ReaderFocusClaimReceipt{ID: claimID, Status: "claimed", ExpiresAt: expiresAt, CandidateCount: len(selected)}, nil
}

type readerFocusClaimQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

func readerFocusClaimReservationForDashboard(d Dashboard) (readerFocusClaimReservation, bool, error) {
	return readerFocusClaimReservationForDashboardWithQuerier(db, d)
}

func readerFocusClaimReservationForDashboardTx(tx *sql.Tx, d Dashboard) (readerFocusClaimReservation, bool, error) {
	return readerFocusClaimReservationForDashboardWithQuerier(tx, d)
}

func readerFocusClaimReservationForDashboardWithQuerier(q readerFocusClaimQuerier, d Dashboard) (readerFocusClaimReservation, bool, error) {
	if strings.TrimSpace(d.ReaderFocusClaimID) != "" {
		return readerFocusClaimByIDWithQuerier(q, d.ReaderFocusClaimID)
	}
	keys := make(map[string]struct{}, len(d.ArticleAIOverviews))
	for _, overview := range d.ArticleAIOverviews {
		if overview.EntryID != "" && overview.ContentHash != "" {
			keys[overview.EntryID+"\x00"+overview.ContentHash] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return readerFocusClaimReservation{}, false, nil
	}
	rows, err := q.Query(`SELECT DISTINCT r.id FROM reader_focus_runs r
JOIN reader_focus_claim_items i ON i.run_id=r.id
WHERE r.status='pending' AND r.expires_at>?`, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return readerFocusClaimReservation{}, false, nil
		}
		return readerFocusClaimReservation{}, false, err
	}
	var candidateIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return readerFocusClaimReservation{}, false, err
		}
		candidateIDs = append(candidateIDs, id)
	}
	if err := rows.Err(); err != nil {
		return readerFocusClaimReservation{}, false, err
	}
	if err := rows.Close(); err != nil {
		return readerFocusClaimReservation{}, false, err
	}
	var match readerFocusClaimReservation
	count := 0
	overlapsPendingClaim := false
	for _, id := range candidateIDs {
		claim, ok, err := readerFocusClaimByIDWithQuerier(q, id)
		if err != nil || !ok {
			if err != nil {
				return readerFocusClaimReservation{}, false, err
			}
			continue
		}
		items, err := readerFocusClaimItemKeysWithQuerier(q, id)
		if err != nil {
			return readerFocusClaimReservation{}, false, err
		}
		// Legacy maintenance callers may process only the first part of a
		// claimed batch (the pre-Focus prompt was capped at three items). A
		// payload is safe to bind when every submitted item belongs to one
		// unique pending claim; the unsent remainder is released when that
		// claim is consumed and can be reserved by the next read.
		all := true
		for key := range keys {
			if _, exists := items[key]; exists {
				overlapsPendingClaim = true
			} else {
				all = false
			}
		}
		if all && len(keys) > 0 {
			match = claim
			count++
		}
	}
	if count > 1 {
		return readerFocusClaimReservation{}, false, errors.New("关注主题 claim 不明确")
	}
	if count == 0 && overlapsPendingClaim {
		return readerFocusClaimReservation{}, false, errors.New("关注主题 claim 与概览批次不匹配")
	}
	return match, count == 1, nil
}

func readerFocusClaimByID(id string) (readerFocusClaimReservation, bool, error) {
	return readerFocusClaimByIDWithQuerier(db, id)
}

func readerFocusClaimByIDWithQuerier(q readerFocusClaimQuerier, id string) (readerFocusClaimReservation, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return readerFocusClaimReservation{}, false, nil
	}
	var claim readerFocusClaimReservation
	err := q.QueryRow(`SELECT id,status,expires_at,candidate_count,
estimated_input_tokens,estimated_output_tokens FROM reader_focus_runs WHERE id=?`, id).Scan(
		&claim.ID, &claim.Status, &claim.ExpiresAt, &claim.CandidateCount,
		&claim.EstimatedInput, &claim.EstimatedOutput)
	if errors.Is(err, sql.ErrNoRows) {
		return readerFocusClaimReservation{}, false, nil
	}
	if err != nil {
		if readerFocusClaimSchemaMissing(err) {
			return readerFocusClaimReservation{}, false, nil
		}
		return readerFocusClaimReservation{}, false, err
	}
	return claim, true, nil
}

func readerFocusClaimItemKeys(runID string) (map[string]struct{}, error) {
	return readerFocusClaimItemKeysWithQuerier(db, runID)
}

func readerFocusClaimItemKeysWithQuerier(q readerFocusClaimQuerier, runID string) (map[string]struct{}, error) {
	rows, err := q.Query(`SELECT entry_id,content_hash FROM reader_focus_claim_items WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var entryID, hash string
		if err := rows.Scan(&entryID, &hash); err != nil {
			return nil, err
		}
		result[entryID+"\x00"+hash] = struct{}{}
	}
	return result, rows.Err()
}

func consumeReaderFocusClaimTx(tx *sql.Tx, claimID string, d Dashboard) error {
	claimID = strings.TrimSpace(claimID)
	if claimID == "" {
		return nil
	}
	var claim readerFocusClaimReservation
	err := tx.QueryRow(`SELECT id,status,expires_at,candidate_count,
estimated_input_tokens,estimated_output_tokens FROM reader_focus_runs WHERE id=?`, claimID).Scan(
		&claim.ID, &claim.Status, &claim.ExpiresAt, &claim.CandidateCount,
		&claim.EstimatedInput, &claim.EstimatedOutput)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("关注主题 claim 不存在")
	}
	if err != nil {
		return err
	}
	if claim.Status != "pending" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE reader_focus_claim_items SET state='completed', updated_at=? WHERE run_id=?`, now, claim.ID); err != nil {
		return err
	}
	for _, overview := range d.ArticleAIOverviews {
		if overview.Status != "failed" {
			continue
		}
		if _, err := tx.Exec(`UPDATE reader_focus_claim_items SET state='failed',updated_at=? WHERE run_id=? AND entry_id=? AND content_hash=?`, now, claim.ID, overview.EntryID, overview.ContentHash); err != nil {
			return err
		}
	}
	// The normal run ledger written by storeReaderFocusRunTx now owns the
	// completed budget. Remove the temporary reservation atomically so legacy
	// run-count and retention queries continue to see exactly one completed run.
	_, err = tx.Exec(`DELETE FROM reader_focus_runs WHERE id=? AND status='pending'`, claim.ID)
	return err
}

func readerFocusClaimReservationBudget(d Dashboard) (int, int, bool, error) {
	claim, matched, err := readerFocusClaimReservationForDashboard(d)
	if err != nil || !matched || claim.Status != "pending" {
		return 0, 0, false, err
	}
	return claim.CandidateCount, claim.EstimatedInput + claim.EstimatedOutput, true, nil
}

func validateReaderFocusClaimID(d Dashboard) error {
	id := strings.TrimSpace(d.ReaderFocusClaimID)
	if id == "" {
		return nil
	}
	if len(id) > 80 || !validText(id, 80, true) {
		return fmt.Errorf("关注主题 claim 标识不合法")
	}
	return nil
}

func validateReaderFocusClaimIDTx(tx *sql.Tx, d Dashboard) error {
	return validateReaderFocusClaimIDWithQuerier(tx, d)
}

func validateReaderFocusClaimIDWithQuerier(q readerFocusClaimQuerier, d Dashboard) error {
	if err := validateReaderFocusClaimID(d); err != nil {
		return err
	}
	if strings.TrimSpace(d.ReaderFocusClaimID) == "" {
		return nil
	}
	claim, ok, err := readerFocusClaimByIDWithQuerier(q, d.ReaderFocusClaimID)
	if err != nil {
		return err
	}
	if !ok || claim.Status != "pending" || (claim.ExpiresAt != "" && claim.ExpiresAt <= time.Now().UTC().Format(time.RFC3339)) {
		return errors.New("关注主题 claim 已失效")
	}
	items, err := readerFocusClaimItemKeysWithQuerier(q, claim.ID)
	if err != nil {
		return err
	}
	payload := make(map[string]struct{}, len(d.ArticleAIOverviews))
	for _, overview := range d.ArticleAIOverviews {
		payload[overview.EntryID+"\x00"+overview.ContentHash] = struct{}{}
	}
	if len(payload) == 0 || len(payload) > len(items) {
		return errors.New("关注主题 claim 与概览批次不匹配")
	}
	for key := range payload {
		if _, exists := items[key]; !exists {
			return errors.New("关注主题 claim 与概览批次不匹配")
		}
	}
	return nil
}

package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
)

func TestFocusEvidenceInterpretationCacheSharesBoundedResult(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-cache", "profile-focus-cache")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-cache", "用户需要更低延迟的 AI Computer 本地工作流。", "2026-08-15T09:30:00Z")

	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected focus claim: %#v", claim)
	}
	apply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", focusFlowApplyRequest(claim, "signal-cache"), "focus-cache-apply-1")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply focus analysis status=%d body=%s", apply.Code, apply.Body.String())
	}
	var projected focusAnalysisGetResponse
	if err := json.Unmarshal(apply.Body.Bytes(), &projected); err != nil {
		t.Fatal(err)
	}
	if len(projected.CachedInterpretations) != 1 {
		t.Fatalf("apply did not return one interpretation cache entry: %#v", projected.CachedInterpretations)
	}
	cache := projected.CachedInterpretations[0]
	if cache.FocusID != focus.ID || cache.FocusRevision != claim.FocusRevision || cache.EvidenceID != claim.Evidence[0].ID || cache.EvidenceSetHash != claim.EvidenceSetHash || cache.RunID != claim.RunID {
		t.Fatalf("cache provenance mismatch: %#v", cache)
	}
	if len(cache.Interpretation) == 0 || cache.Interpretation["run_id"] != claim.RunID {
		t.Fatalf("cache payload missing run metadata: %#v", cache.Interpretation)
	}

	var raw string
	if err := db.QueryRow(`SELECT interpretation_json FROM focus_evidence_interpretations WHERE owner_id=? AND profile_id=? AND focus_id=?`, "owner-focus-cache", "profile-focus-cache", focus.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) > focusInterpretationMaxBytes {
		t.Fatalf("cache payload exceeded bound: %d", len(raw))
	}

	get := callFocusFlowJSON(t, focusAnalysisGetHandler, http.MethodGet, "/api/focus-analysis?focus_id="+focus.ID+"&run_id="+claim.RunID, nil, "")
	if get.Code != http.StatusOK {
		t.Fatalf("get focus analysis status=%d body=%s", get.Code, get.Body.String())
	}
	var reread focusAnalysisGetResponse
	if err := json.Unmarshal(get.Body.Bytes(), &reread); err != nil {
		t.Fatal(err)
	}
	if len(reread.CachedInterpretations) != 1 || reread.CachedInterpretations[0].RunID != claim.RunID {
		t.Fatalf("get did not reuse current interpretation cache: %#v", reread.CachedInterpretations)
	}
}

func TestFocusEvidenceInterpretationCacheDoesNotCrossEvidenceSet(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-cache-isolation", "profile-focus-cache-isolation")
	focus := createFocusFlowSpace(t, "AI Computer")
	insertFocusFlowMeeting(t, "meeting-focus-cache-isolation", "用户需要更低延迟的 AI Computer 本地工作流。", "2026-08-15T09:30:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	apply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", focusFlowApplyRequest(claim, "signal-cache-isolation"), "focus-cache-isolation-1")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply focus analysis status=%d body=%s", apply.Code, apply.Body.String())
	}

	// A different hash must not expose the prior interpretation, even when all
	// other focus dimensions happen to match.
	var run focusRunSummary
	if err := db.QueryRow(`SELECT id,focus_id,focus_revision,evidence_set_hash,analysis_profile,prompt_version,scope,status,candidate_count,processed_count,estimated_input_tokens,estimated_output_tokens,actual_input_tokens,actual_output_tokens,idempotency_key,selector_key,expires_at,error_code,created_at,updated_at FROM focus_analysis_runs WHERE id=?`, claim.RunID).Scan(
		&run.ID, &run.FocusID, &run.FocusRevision, &run.EvidenceSetHash, &run.AnalysisProfile, &run.PromptVersion, &run.Scope, &run.Status, &run.CandidateCount, &run.ProcessedCount, &run.EstimatedInput, &run.EstimatedOutput, &run.ActualInput, &run.ActualOutput, &run.IdempotencyKey, &run.SelectorKey, &run.ExpiresAt, &run.ErrorCode, &run.CreatedAt, &run.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	run.EvidenceSetHash = "0000000000000000000000000000000000000000000000000000000000000000"
	cached, err := readFocusEvidenceInterpretationsTx(beginReadTx(t), run, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 0 {
		t.Fatalf("different evidence hash reused cache: %#v", cached)
	}
}

func beginReadTx(t *testing.T) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

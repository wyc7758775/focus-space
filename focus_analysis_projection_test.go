package main

import "testing"

func TestFocusAnalysisGetTxSelectsUsableProjection(t *testing.T) {
	tests := []struct {
		name           string
		previousStatus string
		latestStatus   string
		explicitRun    string
		wantRun        string
	}{
		{"active preserves applied result", "applied", "claimed", "", "previous"},
		{"active preserves partial result", "partial", "claimed", "", "previous"},
		{"failure preserves applied result", "applied", "failed", "", "previous"},
		{"invalidated result is not restored", "superseded", "claimed", "", ""},
		{"completed replacement wins", "applied", "applied", "", "latest"},
		{"explicit active read is unchanged", "applied", "claimed", "latest", "latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db = openTestDB(t)
			useStableContextScope(t, "owner-projection-selection", "profile-projection-selection")
			focus := createFocusFlowSpace(t, "AI Computer")
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec(`UPDATE focus_spaces SET revision=2 WHERE id=?`, focus.ID); err != nil {
				t.Fatal(err)
			}
			for _, run := range []struct {
				id       string
				status   string
				revision int
				created  string
			}{
				{"previous", tt.previousStatus, 1, "2026-09-12T01:00:00Z"},
				{"latest", tt.latestStatus, 2, "2026-09-12T02:00:00Z"},
			} {
				if _, err := tx.Exec(`INSERT INTO focus_analysis_runs
(id,owner_id,profile_id,focus_id,focus_revision,evidence_set_hash,scope,status,idempotency_key,created_at,updated_at)
VALUES(?,?,?,?,?,?,'question',?,?,?,?)`, run.id, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID, run.revision, "evidence-"+run.id, run.status, "key-"+run.id, run.created, run.created); err != nil {
					t.Fatal(err)
				}
			}
			projection, err := focusAnalysisGetTx(tx, focus.ID, tt.explicitRun, 20)
			if err != nil {
				t.Fatal(err)
			}
			if projection.RunID != tt.wantRun {
				t.Fatalf("selected run=%q, want %q", projection.RunID, tt.wantRun)
			}
			if len(projection.Runs) != 2 {
				t.Fatalf("live run history was lost: %#v", projection.Runs)
			}
		})
	}
}

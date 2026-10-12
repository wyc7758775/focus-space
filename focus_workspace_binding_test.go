package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestFocusWorkspaceBindingCreateReadAndDecisionSnapshotFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-binding", "profile-binding")
	title, goal := "东京旅行", "规划东京五日文化旅行"
	kind, horizon, intent, blueprint, thinking := "monitor", "open", "plan", "plan.v1", "first_principles"
	created := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
		SchemaVersion: 1, Action: "create", ExpectedRevision: 0, Title: &title, Goal: &goal, Kind: &kind, HorizonMode: &horizon,
		IntentKind: &intent, BlueprintID: &blueprint, ThinkingTemplateID: &thinking,
	}, "")
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var focus FocusSpace
	if err := json.Unmarshal(created.Body.Bytes(), &focus); err != nil {
		t.Fatal(err)
	}
	if focus.IntentKind != "plan" || focus.BlueprintID != "plan.v1" || focus.BlueprintVersion != "1" || focus.ThinkingTemplateID != "first_principles" || focus.BindingRevision != 1 {
		t.Fatalf("unexpected binding: %#v", focus)
	}
	insertFocusFlowMeeting(t, "meeting-binding", "东京旅行需要确定路线和预订动作。", "2026-09-17T09:00:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.IntentKind != "plan" || claim.BlueprintID != "plan.v1" || claim.ThinkingTemplateID != "first_principles" {
		t.Fatalf("analysis read lost binding: %#v", claim)
	}
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision, DecisionMode: "rational",
	}, "binding-run-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	if run.Run == nil || run.Run.IntentKind != "plan" || run.Run.BlueprintID != "plan.v1" || run.Run.ThinkingTemplateID != "rational" || run.Run.ThinkingTemplateVersion != "builtin-v1" {
		t.Fatalf("decision run did not freeze binding: %#v", run.Run)
	}
	var bindingRevision int
	var selectedThinking string
	if err := db.QueryRow(`SELECT revision,thinking_template_id FROM focus_workspace_bindings WHERE focus_id=? AND owner_id=? AND profile_id=?`, focus.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&bindingRevision, &selectedThinking); err != nil {
		t.Fatal(err)
	}
	if bindingRevision != 2 || selectedThinking != "rational" {
		t.Fatalf("thinking selection was not persisted: revision=%d thinking=%q", bindingRevision, selectedThinking)
	}
}

func TestFocusWorkspaceBindingModeChangeInvalidatesActiveRunFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-binding-change", "profile-binding-change")
	focus := createFocusFlowSpace(t, "撰写 ACM 文章")
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision, DecisionMode: "guided",
	}, "binding-change-start")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}
	run := decodeDecisionRunFlow(t, started)
	intent, blueprint, thinking := "create", "create.v1", "first_principles"
	updated := callFocusFlowJSON(t, focusSpaceHandler, http.MethodPatch, "/api/focus-space", focusSpaceMutation{
		SchemaVersion: 1, Action: "update", FocusID: focus.ID, ExpectedRevision: focus.Revision,
		IntentKind: &intent, BlueprintID: &blueprint, ThinkingTemplateID: &thinking,
	}, "")
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	var changed FocusSpace
	if err := json.Unmarshal(updated.Body.Bytes(), &changed); err != nil {
		t.Fatal(err)
	}
	if changed.IntentKind != "create" || changed.BlueprintID != "create.v1" || changed.ThinkingTemplateID != "first_principles" || changed.Revision != focus.Revision+1 {
		t.Fatalf("unexpected changed focus: %#v", changed)
	}
	got := callFocusFlowJSON(t, decisionRunGetHandler, http.MethodGet, "/api/internal/decision-runs?run_id="+run.Run.ID, nil, "")
	state := decodeDecisionRunFlow(t, got)
	if got.Code != http.StatusOK || state.Run == nil || state.Run.Status != "superseded" {
		t.Fatalf("mode change did not invalidate active run: status=%d body=%s", got.Code, got.Body.String())
	}
}

func TestListFocusSpacesReleasesOuterRowsBeforeNestedReads(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-focus-pool", "profile-focus-pool")
	focus := createFocusFlowSpace(t, "连接池回归")
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	done := make(chan error, 1)
	go func() {
		spaces, err := listFocusSpaces(focus.ID)
		if err == nil && len(spaces) != 1 {
			done <- fmt.Errorf("listFocusSpaces returned %d spaces, want 1", len(spaces))
			return
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listFocusSpaces held its outer rows while issuing nested reads")
	}
}

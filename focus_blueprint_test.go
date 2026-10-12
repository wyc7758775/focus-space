package main

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"testing"
)

func TestFocusBaseBlueprintsValidateUnit(t *testing.T) {
	for intent, blueprintID := range focusBlueprintIDs {
		t.Run(intent, func(t *testing.T) {
			blueprint, raw, err := loadFocusBaseBlueprint(blueprintID)
			if err != nil {
				t.Fatal(err)
			}
			if blueprint.IntentKind != intent || blueprint.BlueprintID != blueprintID || len(raw) == 0 {
				t.Fatalf("unexpected blueprint: %#v", blueprint)
			}
		})
	}
}

func TestFocusBlueprintDecoderRejectsUnknownAndTrailingJSONUnit(t *testing.T) {
	valid, _, err := loadFocusBaseBlueprint("decide.v1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(valid)
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["script"] = "alert(1)"
	unknown, _ := json.Marshal(object)
	if _, _, err := decodeAndValidateFocusBlueprint(unknown); err == nil {
		t.Fatal("unknown blueprint field was accepted")
	}
	if _, _, err := decodeAndValidateFocusBlueprint(append(raw, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	if _, err := decodeJSONObject([]byte(`{} []`), focusBlueprintPatchMaxBytes); err == nil {
		t.Fatal("trailing patch JSON was accepted")
	}
}

func setupFocusBlueprintFlow(t *testing.T) FocusSpace {
	t.Helper()
	db = openTestDB(t)
	useStableContextScope(t, "owner-blueprint", "profile-blueprint")
	return createFocusFlowSpace(t, "规划一次东京旅行")
}

func callFocusBlueprintMutation(t *testing.T, action string, request focusBlueprintMutationRequest) *focusBlueprintResponse {
	t.Helper()
	response := callFocusFlowJSON(t, focusBlueprintMutationHandler, http.MethodPost, "/api/internal/focus-blueprints/"+action, request, "")
	if response.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", action, response.Code, response.Body.String())
	}
	var value focusBlueprintResponse
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return &value
}

func TestFocusBlueprintApplyRollbackAndOwnerIsolationFlow(t *testing.T) {
	focus := setupFocusBlueprintFlow(t)
	first := callFocusBlueprintMutation(t, "apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 0,
		Patch: json.RawMessage(`{"copy":{"result_title":"旅行决定"},"theme_tokens":{"accent":"orange","density":"comfortable"}}`),
	})
	if first.Revision != 1 || first.Effective.Copy["result_title"] != "旅行决定" {
		t.Fatalf("unexpected first apply: %#v", first)
	}
	second := callFocusBlueprintMutation(t, "apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 1,
		Patch: json.RawMessage(`{"copy":{"result_title":"旅行方案"}}`),
	})
	if second.Revision != 2 || second.Effective.Copy["result_title"] != "旅行方案" {
		t.Fatalf("unexpected second apply: %#v", second)
	}
	rolled := callFocusBlueprintMutation(t, "rollback", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 2, TargetRevision: 1,
	})
	if rolled.Revision != 3 || rolled.Effective.Copy["result_title"] != "旅行决定" {
		t.Fatalf("rollback did not restore target revision: %#v", rolled)
	}
	var patch map[string]any
	if err := json.Unmarshal(rolled.Patch, &patch); err != nil {
		t.Fatal(err)
	}
	copyPatch := patch["copy"].(map[string]any)
	if copyPatch["result_title"] != "旅行决定" {
		t.Fatalf("rollback patch was merged with current state: %s", rolled.Patch)
	}

	stale := callFocusFlowJSON(t, focusBlueprintMutationHandler, http.MethodPost, "/api/internal/focus-blueprints/apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 2,
		Patch: json.RawMessage(`{"copy":{"result_title":"过期写入"}}`),
	}, "")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale apply status=%d body=%s", stale.Code, stale.Body.String())
	}

	previousScope := appDataScope
	appDataScope = dataScope{OwnerID: "other-owner", ProfileID: "other-profile"}
	defer func() { appDataScope = previousScope }()
	other := callFocusFlowJSON(t, focusBlueprintGetHandler, http.MethodGet, "/api/internal/focus-blueprints?scope=focus&focus_id="+focus.ID+"&blueprint_id=decide.v1", nil, "")
	if other.Code != http.StatusBadRequest {
		t.Fatalf("cross-owner blueprint read status=%d body=%s", other.Code, other.Body.String())
	}
}

func TestFocusBlueprintWorkflowMutationWaitsForActiveAnalysisFlow(t *testing.T) {
	focus := setupFocusBlueprintFlow(t)
	started := appDataMutationFlow(t, "decision_run.start", decisionRunStartRequest{
		SchemaVersion: decisionRunSchemaVersion, FocusID: focus.ID, ExpectedFocusRevision: focus.Revision,
		DecisionMode: "rational", SkipClarification: true,
	}, "blueprint-active-run")
	if started.Code != http.StatusCreated {
		t.Fatalf("start status=%d body=%s", started.Code, started.Body.String())
	}

	for _, request := range []focusBlueprintMutationRequest{
		{
			SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: focus.BlueprintID, ExpectedRevision: 0, Confirmed: true,
			Patch: json.RawMessage(`{"result_contract":{"actions":{"min":1}}}`),
		},
		{
			SchemaVersion: 1, Scope: "application", BlueprintID: focus.BlueprintID, ExpectedRevision: 0, Confirmed: true,
			Patch: json.RawMessage(`{"workflow_steps":["collect","analyze","confirm","act"]}`),
		},
	} {
		response := callFocusFlowJSON(t, focusBlueprintMutationHandler, http.MethodPost, "/api/internal/focus-blueprints/apply", request, "")
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "active_analysis_run") {
			t.Fatalf("workflow mutation status=%d body=%s", response.Code, response.Body.String())
		}
	}

	presentation := callFocusFlowJSON(t, focusBlueprintMutationHandler, http.MethodPost, "/api/internal/focus-blueprints/apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: focus.BlueprintID, ExpectedRevision: 0,
		Patch: json.RawMessage(`{"copy":{"result_title":"进行中的旅行决定"}}`),
	}, "")
	if presentation.Code != http.StatusOK {
		t.Fatalf("presentation mutation status=%d body=%s", presentation.Code, presentation.Body.String())
	}
}

func TestFocusBlueprintStateInvariantRandomSequence(t *testing.T) {
	focus := setupFocusBlueprintFlow(t)
	rng := rand.New(rand.NewSource(20260917))
	revision := 0
	for step := 0; step < 100; step++ {
		accent := []string{"blue", "orange", "violet", "green", "neutral"}[rng.Intn(5)]
		response := callFocusBlueprintMutation(t, "apply", focusBlueprintMutationRequest{
			SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: revision,
			Patch: json.RawMessage(`{"theme_tokens":{"accent":"` + accent + `","density":"comfortable"}}`),
		})
		revision++
		if response.Revision != revision || response.Effective.ThemeTokens["accent"] != accent || response.UpgradeState != "current" {
			t.Fatalf("step %d broke blueprint invariants: %#v", step, response)
		}
		var current, history int
		if err := db.QueryRow(`SELECT revision FROM focus_blueprint_overrides WHERE owner_id=? AND profile_id=? AND scope_kind='focus' AND scope_id=? AND blueprint_id='decide.v1'`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID).Scan(&current); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT COUNT(*) FROM focus_blueprint_revisions WHERE owner_id=? AND profile_id=? AND scope_kind='focus' AND scope_id=? AND blueprint_id='decide.v1'`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID).Scan(&history); err != nil {
			t.Fatal(err)
		}
		if current != revision || history != revision {
			t.Fatalf("step %d current=%d history=%d want=%d", step, current, history, revision)
		}
	}
}

func TestFocusBlueprintUpgradeReadyAndConflictKeepLastKnownGoodFlow(t *testing.T) {
	focus := setupFocusBlueprintFlow(t)
	applied := callFocusBlueprintMutation(t, "apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 0,
		Patch: json.RawMessage(`{"copy":{"result_title":"我的决定"}}`),
	})
	if _, err := db.Exec(`UPDATE focus_blueprint_overrides SET base_version='0' WHERE owner_id=? AND profile_id=? AND scope_kind='focus' AND scope_id=? AND blueprint_id='decide.v1'`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ready, err := resolveFocusBlueprintTx(tx, "focus", focus.ID, "decide.v1", nil, false)
	_ = tx.Rollback()
	if err != nil || ready.UpgradeState != "ready" || ready.BaseVersion != "1" || ready.Effective.Copy["result_title"] != "我的决定" {
		t.Fatalf("upgrade replay was not ready: response=%#v err=%v", ready, err)
	}
	if _, err := db.Exec(`UPDATE focus_blueprint_overrides SET patch_json=? WHERE owner_id=? AND profile_id=? AND scope_kind='focus' AND scope_id=? AND blueprint_id='decide.v1'`, `{"result_contract":null}`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	conflict, err := resolveFocusBlueprintTx(tx, "focus", focus.ID, "decide.v1", nil, false)
	_ = tx.Rollback()
	if err != nil || conflict.UpgradeState != "conflict" || conflict.BaseVersion != "0" || conflict.EffectiveHash != applied.EffectiveHash || conflict.Effective.Copy["result_title"] != "我的决定" {
		t.Fatalf("upgrade conflict did not preserve last known good: response=%#v err=%v", conflict, err)
	}
}

func TestFocusResultBlueprintMigrationFlow(t *testing.T) {
	conn := openUpgradeTestDB(t, "107_")
	applyMigrationFile(t, conn, "107_focus_result_blueprints.sql")
	for _, table := range []string{"focus_work_products", "focus_work_product_revisions", "focus_work_product_citations", "focus_result_links", "focus_blueprint_overrides", "focus_blueprint_revisions", "focus_workspace_bindings"} {
		var found string
		if err := conn.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&found); err != nil || found != table {
			t.Fatalf("migration table %s missing: found=%q err=%v", table, found, err)
		}
	}
	columns := map[string]bool{}
	rows, err := conn.Query(`PRAGMA table_info(focus_decision_runs)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	for _, name := range []string{"intent_kind", "blueprint_id", "blueprint_version", "thinking_template_id", "thinking_template_version"} {
		if !columns[name] {
			t.Fatalf("migration column %s missing", name)
		}
	}
}

func TestFocusBlueprintClosedAppDataDispatcherFlow(t *testing.T) {
	focus := setupFocusBlueprintFlow(t)
	preview := appDataMutationFlow(t, "focus_blueprint.preview", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 0,
		Patch: json.RawMessage(`{"copy":{"result_title":"我的焦点结果"}}`),
	}, "blueprint-preview-dispatch")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"revision":0`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	applied := appDataMutationFlow(t, "focus_blueprint.apply", focusBlueprintMutationRequest{
		SchemaVersion: 1, Scope: "focus", FocusID: focus.ID, BlueprintID: "decide.v1", ExpectedRevision: 0,
		Patch: json.RawMessage(`{"copy":{"result_title":"我的焦点结果"}}`),
	}, "blueprint-apply-dispatch")
	if applied.Code != http.StatusOK || !strings.Contains(applied.Body.String(), `"revision":1`) {
		t.Fatalf("apply status=%d body=%s", applied.Code, applied.Body.String())
	}
	got := appDataCall(t, "/api/app-data/v1/operations/focus_blueprint.get?scope=focus&focus_id="+focus.ID+"&blueprint_id=decide.v1", "", "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "我的焦点结果") {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
}

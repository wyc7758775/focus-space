package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

type focusWorkspaceBinding struct {
	FocusID                 string `json:"focus_id"`
	IntentKind              string `json:"intent_kind"`
	BlueprintID             string `json:"blueprint_id"`
	BlueprintVersion        string `json:"blueprint_version"`
	ThinkingTemplateID      string `json:"thinking_template_id"`
	ThinkingTemplateVersion string `json:"thinking_template_version"`
	Revision                int    `json:"revision"`
	CreatedAt               string `json:"created_at"`
	UpdatedAt               string `json:"updated_at"`
}

func defaultFocusWorkspaceBinding(space FocusSpace) (focusWorkspaceBinding, error) {
	intentKind := defaultFocusIntentKind(space.Kind)
	blueprintID := defaultFocusBlueprintID(intentKind)
	blueprint, _, err := loadFocusBaseBlueprint(blueprintID)
	if err != nil {
		return focusWorkspaceBinding{}, err
	}
	return focusWorkspaceBinding{
		FocusID: space.ID, IntentKind: intentKind, BlueprintID: blueprintID, BlueprintVersion: blueprint.BaseVersion,
		ThinkingTemplateID: blueprint.DefaultThinkingTemplate, ThinkingTemplateVersion: "builtin-v1", Revision: 1,
	}, nil
}

func readFocusWorkspaceBindingTx(tx *sql.Tx, space FocusSpace) (focusWorkspaceBinding, error) {
	var binding focusWorkspaceBinding
	err := tx.QueryRow(`SELECT focus_id,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,revision,created_at,updated_at
FROM focus_workspace_bindings WHERE focus_id=? AND owner_id=? AND profile_id=?`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(
		&binding.FocusID, &binding.IntentKind, &binding.BlueprintID, &binding.BlueprintVersion, &binding.ThinkingTemplateID, &binding.ThinkingTemplateVersion, &binding.Revision, &binding.CreatedAt, &binding.UpdatedAt)
	if !errors.Is(err, sql.ErrNoRows) {
		return binding, err
	}
	binding, err = defaultFocusWorkspaceBinding(space)
	if err != nil {
		return focusWorkspaceBinding{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	binding.CreatedAt, binding.UpdatedAt = now, now
	_, err = tx.Exec(`INSERT INTO focus_workspace_bindings(focus_id,owner_id,profile_id,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,revision,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?,1,?,?) ON CONFLICT(focus_id,owner_id,profile_id) DO NOTHING`, binding.FocusID, appDataScope.OwnerID, appDataScope.ProfileID, binding.IntentKind, binding.BlueprintID, binding.BlueprintVersion, binding.ThinkingTemplateID, binding.ThinkingTemplateVersion, now, now)
	if err != nil {
		return focusWorkspaceBinding{}, err
	}
	return binding, nil
}

func applyFocusWorkspaceBindingMutation(binding focusWorkspaceBinding, request focusSpaceMutation) (focusWorkspaceBinding, error) {
	if request.IntentKind != nil && strings.TrimSpace(*request.IntentKind) != "" {
		binding.IntentKind = strings.TrimSpace(*request.IntentKind)
	}
	if !validFocusIntentKind(binding.IntentKind) {
		return focusWorkspaceBinding{}, errors.New("任务模式不合法")
	}
	if request.BlueprintID != nil && strings.TrimSpace(*request.BlueprintID) != "" {
		binding.BlueprintID = strings.TrimSpace(*request.BlueprintID)
	} else if request.IntentKind != nil {
		binding.BlueprintID = defaultFocusBlueprintID(binding.IntentKind)
	}
	blueprint, _, err := loadFocusBaseBlueprint(binding.BlueprintID)
	if err != nil || blueprint.IntentKind != binding.IntentKind {
		return focusWorkspaceBinding{}, errors.New("任务模式与蓝图不匹配")
	}
	binding.BlueprintVersion = blueprint.BaseVersion
	if request.ThinkingTemplateID != nil && strings.TrimSpace(*request.ThinkingTemplateID) != "" {
		binding.ThinkingTemplateID = strings.TrimSpace(*request.ThinkingTemplateID)
	} else if request.IntentKind != nil || request.BlueprintID != nil || binding.ThinkingTemplateID == "" {
		binding.ThinkingTemplateID = blueprint.DefaultThinkingTemplate
	}
	if !focusBlueprintContains(blueprint.AllowedThinkingTemplates, binding.ThinkingTemplateID) {
		return focusWorkspaceBinding{}, errors.New("思考模板未被当前蓝图允许")
	}
	binding.ThinkingTemplateVersion = "builtin-v1"
	return binding, nil
}

func desiredFocusWorkspaceBindingTx(tx *sql.Tx, space FocusSpace, request focusSpaceMutation) (focusWorkspaceBinding, error) {
	binding, err := readFocusWorkspaceBindingTx(tx, space)
	if err != nil {
		return focusWorkspaceBinding{}, err
	}
	return applyFocusWorkspaceBindingMutation(binding, request)
}

func readFocusWorkspaceBinding(space FocusSpace) (focusWorkspaceBinding, error) {
	var binding focusWorkspaceBinding
	err := db.QueryRow(`SELECT focus_id,intent_kind,blueprint_id,blueprint_version,thinking_template_id,thinking_template_version,revision,created_at,updated_at
FROM focus_workspace_bindings WHERE focus_id=? AND owner_id=? AND profile_id=?`, space.ID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(
		&binding.FocusID, &binding.IntentKind, &binding.BlueprintID, &binding.BlueprintVersion, &binding.ThinkingTemplateID, &binding.ThinkingTemplateVersion, &binding.Revision, &binding.CreatedAt, &binding.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultFocusWorkspaceBinding(space)
	}
	return binding, err
}

func persistFocusWorkspaceBindingTx(tx *sql.Tx, current, desired focusWorkspaceBinding, now string) (focusWorkspaceBinding, bool, error) {
	if current.IntentKind == desired.IntentKind && current.BlueprintID == desired.BlueprintID && current.BlueprintVersion == desired.BlueprintVersion && current.ThinkingTemplateID == desired.ThinkingTemplateID && current.ThinkingTemplateVersion == desired.ThinkingTemplateVersion {
		return current, false, nil
	}
	result, err := tx.Exec(`UPDATE focus_workspace_bindings SET intent_kind=?,blueprint_id=?,blueprint_version=?,thinking_template_id=?,thinking_template_version=?,revision=revision+1,updated_at=? WHERE focus_id=? AND owner_id=? AND profile_id=? AND revision=?`, desired.IntentKind, desired.BlueprintID, desired.BlueprintVersion, desired.ThinkingTemplateID, desired.ThinkingTemplateVersion, now, current.FocusID, appDataScope.OwnerID, appDataScope.ProfileID, current.Revision)
	if err != nil {
		return focusWorkspaceBinding{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return focusWorkspaceBinding{}, false, err
	}
	if changed != 1 {
		return focusWorkspaceBinding{}, false, errFocusSpaceRevision
	}
	desired.Revision = current.Revision + 1
	desired.CreatedAt = current.CreatedAt
	desired.UpdatedAt = now
	return desired, true, nil
}

func assignFocusWorkspaceBinding(space *FocusSpace, binding focusWorkspaceBinding) {
	space.IntentKind = binding.IntentKind
	space.BlueprintID = binding.BlueprintID
	space.BlueprintVersion = binding.BlueprintVersion
	space.ThinkingTemplateID = binding.ThinkingTemplateID
	space.ThinkingTemplateVersion = binding.ThinkingTemplateVersion
	space.BindingRevision = binding.Revision
}

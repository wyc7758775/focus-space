package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	focusBlueprintSchemaVersion = 1
	focusBlueprintMaxBytes      = 64 << 10
	focusBlueprintPatchMaxBytes = 16 << 10
)

var (
	focusBlueprintIDs = map[string]string{
		"decide": "decide.v1", "plan": "plan.v1", "create": "create.v1", "monitor": "monitor.v1",
	}
	focusBlueprintIntentByID = map[string]string{
		"decide.v1": "decide", "plan.v1": "plan", "create.v1": "create", "monitor.v1": "monitor",
	}
	focusBlueprintAllowedSections = stringSet("overview", "materials", "workspace", "current_result", "review")
	focusBlueprintAllowedSlots    = stringSet("decision_comparison", "reasoning_graph", "todo_list", "milestone_map", "writing_brief", "outline_map", "evidence_map", "signal_timeline")
	focusBlueprintAllowedHandoffs = stringSet("copy_markdown", "download_markdown", "link_obsidian")
	focusBlueprintAllowedSteps    = stringSet("collect", "clarify", "analyze", "confirm", "act", "review")
	focusBlueprintAllowedThinking = stringSet("guided", "rational", "bounded_rationality", "first_principles")
	focusBlueprintAllowedAccent   = stringSet("blue", "orange", "violet", "green", "neutral")
	focusBlueprintAllowedDensity  = stringSet("compact", "comfortable")
	errFocusBlueprintRevision     = errors.New("focus blueprint revision conflict")
	focusWorkProductTypes         = stringSet("decision_memo", "plan", "travel_guide", "writing_brief", "outline", "comparison", "custom_document")
)

type focusBlueprintResultRule struct {
	Min   int      `json:"min"`
	Max   int      `json:"max"`
	Types []string `json:"types,omitempty"`
}

type focusBlueprint struct {
	SchemaVersion            int                                 `json:"schema_version"`
	BlueprintID              string                              `json:"blueprint_id"`
	BaseVersion              string                              `json:"base_version"`
	IntentKind               string                              `json:"intent_kind"`
	ResultContract           map[string]focusBlueprintResultRule `json:"result_contract"`
	WorkflowSteps            []string                            `json:"workflow_steps"`
	VisibleSections          []string                            `json:"visible_sections"`
	ComponentSlots           []string                            `json:"component_slots"`
	DefaultThinkingTemplate  string                              `json:"default_thinking_template"`
	AllowedThinkingTemplates []string                            `json:"allowed_thinking_templates"`
	Handoffs                 []string                            `json:"handoffs"`
	Copy                     map[string]string                   `json:"copy"`
	ThemeTokens              map[string]string                   `json:"theme_tokens"`
}

type focusBlueprintMutationRequest struct {
	SchemaVersion    int             `json:"schema_version"`
	Scope            string          `json:"scope"`
	FocusID          string          `json:"focus_id,omitempty"`
	BlueprintID      string          `json:"blueprint_id"`
	ExpectedRevision int             `json:"expected_revision"`
	TargetRevision   int             `json:"target_revision,omitempty"`
	Confirmed        bool            `json:"confirmed,omitempty"`
	Patch            json.RawMessage `json:"patch,omitempty"`
}

type focusBlueprintResponse struct {
	SchemaVersion        int             `json:"schema_version"`
	Scope                string          `json:"scope"`
	FocusID              string          `json:"focus_id,omitempty"`
	BlueprintID          string          `json:"blueprint_id"`
	BaseVersion          string          `json:"base_version"`
	Revision             int             `json:"revision"`
	Patch                json.RawMessage `json:"patch"`
	Effective            focusBlueprint  `json:"effective"`
	EffectiveHash        string          `json:"effective_hash"`
	RiskClass            string          `json:"risk_class,omitempty"`
	RequiresConfirmation bool            `json:"requires_confirmation,omitempty"`
	ChangeSummary        []string        `json:"change_summary,omitempty"`
	UpgradeState         string          `json:"upgrade_state"`
}

type focusBlueprintStored struct {
	BaseVersion   string
	Revision      int
	Patch         []byte
	Effective     []byte
	EffectiveHash string
	UpgradeState  string
}

func stringSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func validFocusIntentKind(value string) bool {
	_, ok := focusBlueprintIDs[value]
	return ok
}

func defaultFocusIntentKind(kind string) string {
	if kind == "monitor" {
		return "monitor"
	}
	return "decide"
}

func defaultFocusBlueprintID(intentKind string) string {
	if value, ok := focusBlueprintIDs[intentKind]; ok {
		return value
	}
	return focusBlueprintIDs["decide"]
}

func focusBlueprintQueryKeys() map[string]struct{} {
	return map[string]struct{}{"scope": {}, "focus_id": {}, "blueprint_id": {}}
}

func loadFocusBaseBlueprint(blueprintID string) (focusBlueprint, []byte, error) {
	if _, ok := focusBlueprintIntentByID[blueprintID]; !ok {
		return focusBlueprint{}, nil, errors.New("蓝图不存在")
	}
	raw, err := appFS.ReadFile("blueprints/" + blueprintID + ".json")
	if err != nil {
		return focusBlueprint{}, nil, err
	}
	blueprint, normalized, err := decodeAndValidateFocusBlueprint(raw)
	return blueprint, normalized, err
}

func decodeAndValidateFocusBlueprint(raw []byte) (focusBlueprint, []byte, error) {
	if len(raw) == 0 || len(raw) > focusBlueprintMaxBytes {
		return focusBlueprint{}, nil, errors.New("蓝图大小不合法")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var blueprint focusBlueprint
	if err := decoder.Decode(&blueprint); err != nil {
		return focusBlueprint{}, nil, errors.New("蓝图格式不合法")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return focusBlueprint{}, nil, errors.New("蓝图包含多余内容")
	}
	if err := validateFocusBlueprint(blueprint); err != nil {
		return focusBlueprint{}, nil, err
	}
	normalized, err := json.Marshal(blueprint)
	return blueprint, normalized, err
}

func validateFocusBlueprint(blueprint focusBlueprint) error {
	if blueprint.SchemaVersion != focusBlueprintSchemaVersion || focusBlueprintIntentByID[blueprint.BlueprintID] != blueprint.IntentKind || blueprint.BaseVersion == "" || !validFocusIntentKind(blueprint.IntentKind) {
		return errors.New("蓝图身份不合法")
	}
	if len(blueprint.ResultContract) != 4 {
		return errors.New("蓝图结果契约不完整")
	}
	for _, kind := range []string{"signals", "decisions", "work_products", "actions"} {
		rule, ok := blueprint.ResultContract[kind]
		if !ok || rule.Min < 0 || rule.Max < rule.Min || rule.Max > 5 {
			return errors.New("蓝图结果数量不合法")
		}
		if kind != "work_products" && len(rule.Types) > 0 {
			return errors.New("只有产出物可以声明类型")
		}
		if kind == "work_products" {
			if len(rule.Types) > 7 || !allKnownStrings(rule.Types, focusWorkProductTypes) {
				return errors.New("蓝图产出物类型不合法")
			}
		}
	}
	if len(blueprint.WorkflowSteps) < 2 || len(blueprint.WorkflowSteps) > 8 || !uniqueKnownStrings(blueprint.WorkflowSteps, focusBlueprintAllowedSteps) {
		return errors.New("蓝图工作流不合法")
	}
	if len(blueprint.VisibleSections) < 2 || len(blueprint.VisibleSections) > 5 || !uniqueKnownStrings(blueprint.VisibleSections, focusBlueprintAllowedSections) {
		return errors.New("蓝图可见区域不合法")
	}
	if len(blueprint.ComponentSlots) > 8 || !uniqueKnownStrings(blueprint.ComponentSlots, focusBlueprintAllowedSlots) {
		return errors.New("蓝图组件不合法")
	}
	if _, ok := focusBlueprintAllowedThinking[blueprint.DefaultThinkingTemplate]; !ok || len(blueprint.AllowedThinkingTemplates) == 0 || len(blueprint.AllowedThinkingTemplates) > 8 || !uniqueKnownStrings(blueprint.AllowedThinkingTemplates, focusBlueprintAllowedThinking) {
		return errors.New("蓝图思考模板不合法")
	}
	if !focusBlueprintContains(blueprint.AllowedThinkingTemplates, blueprint.DefaultThinkingTemplate) {
		return errors.New("默认思考模板未启用")
	}
	if len(blueprint.Handoffs) > 4 || !uniqueKnownStrings(blueprint.Handoffs, focusBlueprintAllowedHandoffs) {
		return errors.New("蓝图交接能力不合法")
	}
	if len(blueprint.Copy) > 16 {
		return errors.New("蓝图文案过多")
	}
	for key, value := range blueprint.Copy {
		if !validText(key, 64, true) || !validText(value, 240, false) || unsafeBlueprintString(value) {
			return errors.New("蓝图文案不合法")
		}
	}
	if len(blueprint.ThemeTokens) > 8 {
		return errors.New("蓝图主题字段过多")
	}
	for key, value := range blueprint.ThemeTokens {
		switch key {
		case "accent":
			if _, ok := focusBlueprintAllowedAccent[value]; !ok {
				return errors.New("蓝图强调色不合法")
			}
		case "density":
			if _, ok := focusBlueprintAllowedDensity[value]; !ok {
				return errors.New("蓝图密度不合法")
			}
		default:
			return errors.New("蓝图主题字段不合法")
		}
	}
	return nil
}

func unsafeBlueprintString(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "://") || strings.Contains(lower, "<script") || strings.Contains(lower, "javascript:") || strings.Contains(lower, "file:") || strings.Contains(lower, "\x00") || strings.HasPrefix(lower, "/") || strings.HasPrefix(lower, "../")
}

func allKnownStrings(values []string, allowed map[string]struct{}) bool {
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return false
		}
	}
	return true
}

func uniqueKnownStrings(values []string, allowed map[string]struct{}) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return false
		}
		if _, ok := seen[value]; ok {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func focusBlueprintContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateFocusBlueprintScopeTx(tx *sql.Tx, scope, focusID string) (string, error) {
	scope = strings.TrimSpace(scope)
	focusID = strings.TrimSpace(focusID)
	if scope == "application" {
		if focusID != "" {
			return "", errors.New("应用蓝图不能指定 focus_id")
		}
		return "", nil
	}
	if scope != "focus" || !validFocusID(focusID) {
		return "", errors.New("蓝图作用域不合法")
	}
	var found string
	if err := tx.QueryRow(`SELECT id FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errors.New("焦点空间不存在")
		}
		return "", err
	}
	return focusID, nil
}

func readFocusBlueprintOverrideTx(tx *sql.Tx, scope, scopeID, blueprintID string) (focusBlueprintStored, bool, error) {
	var stored focusBlueprintStored
	err := tx.QueryRow(`SELECT base_version,revision,patch_json,effective_json,effective_hash,upgrade_state FROM focus_blueprint_overrides WHERE owner_id=? AND profile_id=? AND scope_kind=? AND scope_id=? AND blueprint_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, scope, scopeID, blueprintID).Scan(&stored.BaseVersion, &stored.Revision, &stored.Patch, &stored.Effective, &stored.EffectiveHash, &stored.UpgradeState)
	if errors.Is(err, sql.ErrNoRows) {
		return focusBlueprintStored{}, false, nil
	}
	return stored, err == nil, err
}

func decodeJSONObject(raw []byte, max int) (map[string]any, error) {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	if len(raw) > max {
		return nil, errors.New("JSON 数据过大")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("JSON 必须是对象")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON 包含多余内容")
	}
	return value, nil
}

func mergeBlueprintPatch(base map[string]any, patch map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(patch))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		if value == nil {
			delete(result, key)
			continue
		}
		if patchObject, ok := value.(map[string]any); ok {
			if baseObject, ok := result[key].(map[string]any); ok {
				result[key] = mergeBlueprintPatch(baseObject, patchObject)
				continue
			}
		}
		result[key] = value
	}
	return result
}

func validateBlueprintPatchKeys(patch map[string]any) error {
	allowed := stringSet("result_contract", "workflow_steps", "visible_sections", "component_slots", "default_thinking_template", "allowed_thinking_templates", "handoffs", "copy", "theme_tokens")
	for key := range patch {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("蓝图 patch 字段 %s 不允许修改", key)
		}
	}
	return nil
}

func focusBlueprintRiskAndSummary(patch map[string]any) (string, []string) {
	presentation := stringSet("visible_sections", "component_slots", "copy", "theme_tokens")
	risk := "presentation"
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
		if _, ok := presentation[key]; !ok {
			risk = "workflow"
		}
	}
	sort.Strings(keys)
	summary := make([]string, 0, len(keys))
	labels := map[string]string{
		"result_contract": "结果结构", "workflow_steps": "工作流程", "visible_sections": "页面区域", "component_slots": "组件布局", "default_thinking_template": "默认思考模板", "allowed_thinking_templates": "可选思考模板", "handoffs": "交接能力", "copy": "界面文案", "theme_tokens": "主题样式",
	}
	for _, key := range keys {
		summary = append(summary, "修改"+labels[key])
	}
	return risk, summary
}

func resolveFocusBlueprintTx(tx *sql.Tx, scope, focusID, blueprintID string, incoming json.RawMessage, replacePatch bool) (focusBlueprintResponse, error) {
	scopeID, err := validateFocusBlueprintScopeTx(tx, scope, focusID)
	if err != nil {
		return focusBlueprintResponse{}, err
	}
	base, baseRaw, err := loadFocusBaseBlueprint(blueprintID)
	if err != nil {
		return focusBlueprintResponse{}, err
	}
	baseMap, err := decodeJSONObject(baseRaw, focusBlueprintMaxBytes)
	if err != nil {
		return focusBlueprintResponse{}, err
	}
	applicationPatch := map[string]any{}
	var fallbackStored focusBlueprintStored
	fallbackExists := false
	versionDrift := false
	applicationStored, applicationExists, err := readFocusBlueprintOverrideTx(tx, "application", "", blueprintID)
	if err != nil {
		return focusBlueprintResponse{}, err
	}
	if applicationExists {
		if applicationStored.BaseVersion != base.BaseVersion {
			versionDrift = true
		}
		applicationPatch, err = decodeJSONObject(applicationStored.Patch, focusBlueprintPatchMaxBytes)
		if err != nil {
			return focusBlueprintResponse{}, err
		}
		baseMap = mergeBlueprintPatch(baseMap, applicationPatch)
	}
	currentPatch := map[string]any{}
	currentRevision := 0
	upgradeState := "current"
	if scope == "application" {
		currentPatch, currentRevision = applicationPatch, applicationStored.Revision
		fallbackStored, fallbackExists = applicationStored, applicationExists
	} else {
		focusStored, exists, readErr := readFocusBlueprintOverrideTx(tx, "focus", scopeID, blueprintID)
		if readErr != nil {
			return focusBlueprintResponse{}, readErr
		}
		if exists {
			currentPatch, currentRevision, upgradeState = mustJSONObject(focusStored.Patch), focusStored.Revision, focusStored.UpgradeState
			baseMap = mergeBlueprintPatch(baseMap, currentPatch)
			fallbackStored, fallbackExists = focusStored, true
			if focusStored.BaseVersion != base.BaseVersion {
				versionDrift = true
			}
		} else if applicationExists {
			fallbackStored, fallbackExists = applicationStored, true
		}
	}
	if versionDrift {
		upgradeState = "ready"
	}
	combinedPatch := currentPatch
	if len(incoming) > 0 {
		incomingPatch, decodeErr := decodeJSONObject(incoming, focusBlueprintPatchMaxBytes)
		if decodeErr != nil {
			return focusBlueprintResponse{}, decodeErr
		}
		if err := validateBlueprintPatchKeys(incomingPatch); err != nil {
			return focusBlueprintResponse{}, err
		}
		if replacePatch {
			combinedPatch = incomingPatch
		} else {
			combinedPatch = mergeBlueprintPatch(currentPatch, incomingPatch)
		}
		if scope == "application" {
			baseMap, _ = decodeJSONObject(baseRaw, focusBlueprintMaxBytes)
			baseMap = mergeBlueprintPatch(baseMap, combinedPatch)
		} else {
			baseMap, _ = decodeJSONObject(baseRaw, focusBlueprintMaxBytes)
			baseMap = mergeBlueprintPatch(baseMap, applicationPatch)
			baseMap = mergeBlueprintPatch(baseMap, combinedPatch)
		}
	}
	effectiveRaw, err := json.Marshal(baseMap)
	if err != nil {
		return focusBlueprintResponse{}, err
	}
	effective, normalized, err := decodeAndValidateFocusBlueprint(effectiveRaw)
	if err != nil {
		if len(incoming) == 0 && versionDrift && fallbackExists {
			lastKnownGood, storedNormalized, storedErr := decodeAndValidateFocusBlueprint(fallbackStored.Effective)
			if storedErr == nil {
				patchRaw, _ := json.Marshal(currentPatch)
				storedDigest := fallbackStored.EffectiveHash
				if storedDigest == "" {
					digest := sha256.Sum256(storedNormalized)
					storedDigest = hex.EncodeToString(digest[:])
				}
				return focusBlueprintResponse{SchemaVersion: focusBlueprintSchemaVersion, Scope: scope, FocusID: scopeID, BlueprintID: blueprintID, BaseVersion: fallbackStored.BaseVersion, Revision: currentRevision, Patch: patchRaw, Effective: lastKnownGood, EffectiveHash: storedDigest, UpgradeState: "conflict"}, nil
			}
		}
		return focusBlueprintResponse{}, err
	}
	patchRaw, _ := json.Marshal(combinedPatch)
	if len(patchRaw) > focusBlueprintPatchMaxBytes {
		return focusBlueprintResponse{}, errors.New("蓝图 patch 合并后超过 16384 字节")
	}
	digest := sha256.Sum256(normalized)
	return focusBlueprintResponse{SchemaVersion: focusBlueprintSchemaVersion, Scope: scope, FocusID: scopeID, BlueprintID: blueprintID, BaseVersion: base.BaseVersion, Revision: currentRevision, Patch: patchRaw, Effective: effective, EffectiveHash: hex.EncodeToString(digest[:]), UpgradeState: upgradeState}, nil
}

func mustJSONObject(raw []byte) map[string]any {
	value, err := decodeJSONObject(raw, focusBlueprintPatchMaxBytes)
	if err != nil {
		return map[string]any{}
	}
	return value
}

func focusBlueprintGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	blueprintID := strings.TrimSpace(r.URL.Query().Get("blueprint_id"))
	if scope == "" {
		if focusID != "" {
			scope = "focus"
		} else {
			scope = "application"
		}
	}
	if blueprintID == "" && focusID != "" {
		var err error
		blueprintID, err = focusBlueprintIDForFocus(focusID)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if blueprintID == "" {
		blueprintID = "decide.v1"
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	response, err := resolveFocusBlueprintTx(tx, scope, focusID, blueprintID, nil, false)
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func focusBlueprintMutationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusBlueprintMutationRequest
	if err := app.DecodeStrictJSON(w, r, focusBlueprintMaxBytes, &request); err != nil || request.SchemaVersion != focusBlueprintSchemaVersion || request.ExpectedRevision < 0 {
		app.UserErr(w, http.StatusBadRequest, "蓝图请求格式不合法")
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/internal/focus-blueprints/")
	if action == "rollback" && request.TargetRevision < 0 {
		app.UserErr(w, http.StatusBadRequest, "回滚版本不合法")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if action == "rollback" {
		request.Patch, err = focusBlueprintPatchAtRevisionTx(tx, request)
		if err != nil {
			app.UserErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	response, err := resolveFocusBlueprintTx(tx, request.Scope, request.FocusID, request.BlueprintID, request.Patch, action == "rollback")
	if err != nil {
		app.UserErr(w, http.StatusBadRequest, err.Error())
		return
	}
	patch, _ := decodeJSONObject(request.Patch, focusBlueprintPatchMaxBytes)
	if action == "rollback" {
		patch = mustJSONObject(response.Patch)
	}
	response.RiskClass, response.ChangeSummary = focusBlueprintRiskAndSummary(patch)
	response.RequiresConfirmation = response.RiskClass == "workflow"
	if response.Revision != request.ExpectedRevision {
		app.UserErr(w, http.StatusConflict, "蓝图已变化，请重新读取")
		return
	}
	if action == "preview" {
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}
	if action != "apply" && action != "rollback" {
		app.UserErr(w, http.StatusNotFound, "蓝图操作不存在")
		return
	}
	if response.RequiresConfirmation && !request.Confirmed {
		app.WriteJSON(w, http.StatusConflict, map[string]any{"code": "confirmation_required", "preview": response})
		return
	}
	if response.RiskClass == "workflow" {
		active, activeErr := activeDecisionRunForBlueprintMutationTx(tx, request.Scope, request.FocusID, request.BlueprintID)
		if activeErr != nil {
			app.HTTPErr(w, activeErr)
			return
		}
		if active {
			app.WriteJSON(w, http.StatusConflict, map[string]string{
				"code":    "active_analysis_run",
				"message": "当前仍有可恢复的分析回合，请先完成、取消或重新开始分析，再修改工作方式",
			})
			return
		}
	}
	response.Revision = request.ExpectedRevision + 1
	if err := persistFocusBlueprintTx(tx, response); err != nil {
		if errors.Is(err, errFocusBlueprintRevision) {
			app.UserErr(w, http.StatusConflict, "蓝图已变化，请重新读取")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func activeDecisionRunForBlueprintMutationTx(tx *sql.Tx, scope, focusID, blueprintID string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM focus_decision_runs WHERE owner_id=? AND profile_id=? AND blueprint_id=? AND status IN ('queued','collecting_context','running_ai','waiting_clarification','applying','paused_insufficient_credits','failed_retryable')`
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID, blueprintID}
	if scope == "focus" {
		query += ` AND focus_id=?`
		args = append(args, focusID)
	}
	query += `)`
	var active int
	if err := tx.QueryRow(query, args...).Scan(&active); err != nil {
		return false, err
	}
	return active == 1, nil
}

func focusBlueprintPatchAtRevisionTx(tx *sql.Tx, request focusBlueprintMutationRequest) (json.RawMessage, error) {
	if request.TargetRevision == 0 {
		return json.RawMessage("{}"), nil
	}
	scopeID, err := validateFocusBlueprintScopeTx(tx, request.Scope, request.FocusID)
	if err != nil {
		return nil, err
	}
	var raw string
	if err := tx.QueryRow(`SELECT patch_json FROM focus_blueprint_revisions WHERE owner_id=? AND profile_id=? AND scope_kind=? AND scope_id=? AND blueprint_id=? AND revision=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.Scope, scopeID, request.BlueprintID, request.TargetRevision).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("蓝图历史版本不存在")
		}
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func persistFocusBlueprintTx(tx *sql.Tx, response focusBlueprintResponse) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	effectiveRaw, _ := json.Marshal(response.Effective)
	summaryRaw, _ := json.Marshal(response.ChangeSummary)
	result, err := tx.Exec(`INSERT INTO focus_blueprint_overrides(owner_id,profile_id,scope_kind,scope_id,blueprint_id,base_version,revision,patch_json,effective_json,effective_hash,upgrade_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?, 'current',?,?) ON CONFLICT(owner_id,profile_id,scope_kind,scope_id,blueprint_id) DO UPDATE SET base_version=excluded.base_version,revision=excluded.revision,patch_json=excluded.patch_json,effective_json=excluded.effective_json,effective_hash=excluded.effective_hash,upgrade_state='current',updated_at=excluded.updated_at WHERE focus_blueprint_overrides.revision=?`, appDataScope.OwnerID, appDataScope.ProfileID, response.Scope, response.FocusID, response.BlueprintID, response.BaseVersion, response.Revision, string(response.Patch), string(effectiveRaw), response.EffectiveHash, now, now, response.Revision-1)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errFocusBlueprintRevision
	}
	revisionID := stableObjectID("focus-blueprint-revision", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+response.Scope+"\x00"+response.FocusID+"\x00"+response.BlueprintID+"\x00"+fmt.Sprint(response.Revision))
	_, err = tx.Exec(`INSERT INTO focus_blueprint_revisions(id,owner_id,profile_id,scope_kind,scope_id,blueprint_id,base_version,revision,patch_json,effective_json,effective_hash,risk_class,change_summary_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, revisionID, appDataScope.OwnerID, appDataScope.ProfileID, response.Scope, response.FocusID, response.BlueprintID, response.BaseVersion, response.Revision, string(response.Patch), string(effectiveRaw), response.EffectiveHash, response.RiskClass, string(summaryRaw), now)
	return err
}

func focusBlueprintIDForFocus(focusID string) (string, error) {
	var blueprintID string
	err := db.QueryRow(`SELECT blueprint_id FROM focus_workspace_bindings WHERE focus_id=? AND owner_id=? AND profile_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&blueprintID)
	if errors.Is(err, sql.ErrNoRows) {
		var kind string
		if lookupErr := db.QueryRow(`SELECT kind FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&kind); lookupErr != nil {
			return "", lookupErr
		}
		return defaultFocusBlueprintID(defaultFocusIntentKind(kind)), nil
	}
	return blueprintID, err
}

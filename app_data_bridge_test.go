package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func appDataCall(t *testing.T, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if key != "" {
		req.Header.Set("X-Zettlab-Idempotency-Key", key)
	}
	result := httptest.NewRecorder()
	appDataOperationHandler(result, req)
	return result
}

func TestAdaptDecisionRunResumeDropsAbsentOptionalValuesUnit(t *testing.T) {
	raw := []byte(`{"schema_version":2,"run_id":"decision-run-008c8e3e48fe9507","expected_revision":2,"status":"running_ai","question":"","analysis_run_id":"focus-run-0bc6004f77a9afef","decision_id":"","error_code":"","error_message":"","usage":{}}`)
	normalized, err := adaptAppDataRequest("decision_run.resume", raw)
	if err != nil {
		t.Fatal(err)
	}
	var request decisionRunResumeRequest
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("normalized board payload still violates DTO: %v body=%s", err, normalized)
	}
	if request.Question != nil || request.Usage != nil {
		t.Fatalf("absent optional values survived: question=%#v usage=%#v", request.Question, request.Usage)
	}
	if request.AnalysisRunID != "focus-run-0bc6004f77a9afef" || request.Status != "running_ai" {
		t.Fatalf("required state was changed: %#v", request)
	}

	invalidQuestion := []byte(`{"schema_version":2,"run_id":"decision-run-1","expected_revision":2,"status":"waiting_clarification","question":7}`)
	normalized, err = adaptAppDataRequest("decision_run.resume", invalidQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(normalized, &request); err == nil {
		t.Fatal("non-empty malformed question was silently discarded")
	}

	invalidOrigin := []byte(`{"schema_version":2,"run_id":"decision-run-1","expected_revision":2,"status":"waiting_clarification","question":{"type":"single_select","question":"选择依据","origin":"runtime"}}`)
	normalized, err = adaptAppDataRequest("decision_run.resume", invalidOrigin)
	if err != nil {
		t.Fatal(err)
	}
	decoder = json.NewDecoder(bytes.NewReader(normalized))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("unsupported question.origin must be rejected by the typed decoder: %v", err)
	}
}

func TestDecisionRunResumeAcceptsStringOptionsUnit(t *testing.T) {
	var request decisionRunResumeRequest
	raw := []byte(`{"schema_version":2,"run_id":"decision-run-1","expected_revision":3,"status":"waiting_clarification","question":{"type":"single_select","question":"选择一个方向","options":["方案 A","方案 B"],"required":true}}`)
	if err := json.Unmarshal(raw, &request); err != nil {
		t.Fatal(err)
	}
	if request.Question == nil || request.Question.Type != "choice" || len(request.Question.Options) != 2 {
		t.Fatalf("options were not normalized: %#v", request.Question)
	}
	if request.Question.Options[0].ID != "option-1" || request.Question.Options[0].Label != "方案 A" {
		t.Fatalf("unexpected first option: %#v", request.Question.Options[0])
	}
}

func TestAppDataCapabilityMetadataMatchesClosedDispatcherUnit(t *testing.T) {
	raw, err := os.ReadFile("metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Capabilities struct {
			AppData struct {
				Version    int `json:"version"`
				Operations []struct {
					Name string `json:"name"`
					Mode string `json:"mode"`
				} `json:"operations"`
			} `json:"app_data"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if document.Capabilities.AppData.Version != appCapabilityVersion {
		t.Fatalf("metadata app_data version=%d", document.Capabilities.AppData.Version)
	}
	specs := appDataOperationSpecs()
	seen := make(map[string]struct{}, len(document.Capabilities.AppData.Operations))
	for _, operation := range document.Capabilities.AppData.Operations {
		if _, duplicate := seen[operation.Name]; duplicate {
			t.Fatalf("duplicate metadata operation %q", operation.Name)
		}
		seen[operation.Name] = struct{}{}
		spec, ok := specs[operation.Name]
		if !ok {
			t.Fatalf("metadata operation %q has no closed dispatcher", operation.Name)
		}
		expectedMode := "read"
		if spec.mutation {
			expectedMode = "mutation"
		}
		if operation.Mode != expectedMode {
			t.Fatalf("operation %q mode=%q expected=%q", operation.Name, operation.Mode, expectedMode)
		}
	}
	for _, operation := range []string{"discovery.create", "discovery.confirm", "subscription.list", "subscription.set"} {
		if _, ok := seen[operation]; !ok {
			t.Fatalf("metadata is missing foreground source operation %q", operation)
		}
	}
}

func TestFocusOperationContractsMatchMetadataCapabilityAndDispatcherUnit(t *testing.T) {
	expected := map[string]struct {
		mode   string
		method string
		path   string
	}{
		"focus_space.get":             {"read", http.MethodGet, "/api/focus-space"},
		"focus_space.update":          {"mutation", http.MethodPatch, "/api/focus-space"},
		"focus_analysis.read":         {"read", http.MethodGet, "/api/focus-analysis/read"},
		"focus_analysis.apply":        {"mutation", http.MethodPost, "/api/internal/focus-analysis/apply"},
		"focus_analysis.get":          {"read", http.MethodGet, "/api/focus-analysis"},
		"focus_result.feedback":       {"mutation", http.MethodPost, "/api/focus-results/feedback"},
		"focus_blueprint.get":         {"read", http.MethodGet, "/api/internal/focus-blueprints"},
		"focus_blueprint.preview":     {"mutation", http.MethodPost, "/api/internal/focus-blueprints/preview"},
		"focus_blueprint.apply":       {"mutation", http.MethodPost, "/api/internal/focus-blueprints/apply"},
		"focus_blueprint.rollback":    {"mutation", http.MethodPost, "/api/internal/focus-blueprints/rollback"},
		"focus_work_product.get":      {"read", http.MethodGet, "/api/internal/focus-work-products"},
		"focus_work_product.feedback": {"mutation", http.MethodPost, "/api/internal/focus-work-products/feedback"},
		"focus_work_product.link":     {"mutation", http.MethodPost, "/api/internal/focus-work-products/link"},
	}
	raw, err := os.ReadFile("metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Capabilities struct {
			AppData struct {
				Operations []struct {
					Name string `json:"name"`
					Mode string `json:"mode"`
				} `json:"operations"`
			} `json:"app_data"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	metadataModes := make(map[string]string)
	for _, operation := range metadata.Capabilities.AppData.Operations {
		metadataModes[operation.Name] = operation.Mode
	}
	capability := callJSON(t, capabilityHandler, http.MethodGet, "/api/capabilities", "")
	if capability.Code != http.StatusOK {
		t.Fatalf("capability status=%d body=%s", capability.Code, capability.Body.String())
	}
	var advertised struct {
		Operations []string `json:"operations"`
	}
	if err := json.Unmarshal(capability.Body.Bytes(), &advertised); err != nil {
		t.Fatal(err)
	}
	capabilityNames := make(map[string]struct{}, len(advertised.Operations))
	for _, name := range advertised.Operations {
		capabilityNames[name] = struct{}{}
	}
	specs := appDataOperationSpecs()
	for name, contract := range expected {
		if metadataModes[name] != contract.mode {
			t.Errorf("metadata %s mode=%q want=%q", name, metadataModes[name], contract.mode)
		}
		if _, ok := capabilityNames[name]; !ok {
			t.Errorf("capability is missing %s", name)
		}
		spec, ok := specs[name]
		if !ok {
			t.Errorf("dispatcher is missing %s", name)
			continue
		}
		if spec.method != contract.method || spec.path != contract.path || spec.mutation != (contract.mode == "mutation") {
			t.Errorf("dispatcher %s method=%q path=%q mutation=%v", name, spec.method, spec.path, spec.mutation)
		}
	}
}

func TestAppDataReaderCountsReadOperationFlow(t *testing.T) {
	db = openTestDB(t)
	result := appDataCall(t, "/api/app-data/v1/operations/reader_counts.get", "", "")
	if result.Code != http.StatusOK {
		t.Fatalf("reader counts operation status=%d body=%s", result.Code, result.Body.String())
	}
	var payload struct {
		TotalEntries  int `json:"total_entries"`
		UnreadEntries int `json:"unread_entries"`
		SourceCounts  []struct {
			SourceID string `json:"source_id"`
		} `json:"source_counts"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TotalEntries != 0 || payload.UnreadEntries != 0 || payload.SourceCounts == nil {
		t.Fatalf("reader counts operation payload=%s", result.Body.String())
	}
}

func TestAppDataDashboardInvokeReplayAndConflictFlow(t *testing.T) {
	db = openTestDB(t)
	payload := workflowDashboardPayload(t, Action{}, nil)
	first := appDataCall(t, "/api/app-data/v1/operations/dashboard.write", payload, "dashboard-run-1")
	if first.Code != http.StatusOK {
		t.Fatalf("first write status=%d body=%s", first.Code, first.Body.String())
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("first write inserted %d dashboard rows", rows)
	}
	replay := appDataCall(t, "/api/app-data/v1/operations/dashboard.write", payload, "dashboard-run-1")
	if replay.Code != http.StatusOK || replay.Body.String() != first.Body.String() || replay.Header().Get("X-Zettlab-Idempotent-Replay") != "true" {
		t.Fatalf("replay status=%d header=%q body_equal=%v", replay.Code, replay.Header().Get("X-Zettlab-Idempotent-Replay"), replay.Body.String() == first.Body.String())
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM dashboard_runs`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("replay inserted %d dashboard rows", rows)
	}
	conflictPayload := strings.Replace(payload, `"generated_at":"2026-08-10T12:00:00Z"`, `"generated_at":"2026-08-11T12:00:00Z"`, 1)
	conflict := appDataCall(t, "/api/app-data/v1/operations/dashboard.write", conflictPayload, "dashboard-run-1")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "幂等") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	read := appDataCall(t, "/api/app-data/v1/operations/dashboard.get", "", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"schema_version":1`) {
		t.Fatalf("read status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestAppDataDashboardTodoAliasFlow(t *testing.T) {
	db = openTestDB(t)
	payload := `{"schema_version":1,"generated_at":"2026-08-13T12:00:00Z","source":{"kind":"mixed","status":"ok","records_seen":0,"records_used":0},"title":"整理客户反馈","description":"本周完成第一版","assignee":"产品组","due_date":"2026-08-20","state":"draft","news":[],"evidence":[],"metrics":[],"projects":[],"signals":[],"decisions":[]}`
	result := appDataCall(t, "/api/app-data/v1/operations/dashboard.write", payload, "todo-alias-1")
	if result.Code != http.StatusOK {
		t.Fatalf("todo alias write status=%d body=%s", result.Code, result.Body.String())
	}
	var task, owner, dueAt, status, confirmation, priority string
	if err := db.QueryRow(`SELECT task,owner,due_at,status,confirmation_status,priority FROM actions LIMIT 1`).Scan(&task, &owner, &dueAt, &status, &confirmation, &priority); err != nil {
		t.Fatal(err)
	}
	if task != "整理客户反馈\n本周完成第一版" || owner != "产品组" || dueAt != "2026-08-20" || status != "draft" || confirmation != "draft" || priority != "medium" {
		t.Fatalf("normalized action = task=%q owner=%q due_at=%q status=%q confirmation=%q priority=%q", task, owner, dueAt, status, confirmation, priority)
	}
}

func TestFocusAnalysisApplyNormalizesBoundedModelVariantsUnit(t *testing.T) {
	raw := []byte(`{"schema_version":1,"actions":[{"id":"action-p0","task":"核验真实资料","owner":"  ","priority":"P0","source_ids":"rss:entry-1","supporting_evidence":"资料支持","counterevidence":"仍需复核","validation_items":"打开原文"}],"signals":[],"decisions":[{"id":"decision-review","question":"两周后是否继续？","review_at":"2026-09-14"}]}`)
	adapted, err := adaptAppDataRequest("focus_analysis.apply", raw)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(adapted, &payload); err != nil {
		t.Fatal(err)
	}
	actions, ok := payload["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("normalized actions=%#v", payload["actions"])
	}
	action, ok := actions[0].(map[string]any)
	if !ok || action["priority"] != "high" || action["owner"] != "负责人待确认" {
		t.Fatalf("normalized action=%#v", actions[0])
	}
	for _, field := range []string{"source_ids", "supporting_evidence", "counterevidence", "validation_items"} {
		values, ok := action[field].([]any)
		if !ok || len(values) != 1 {
			t.Fatalf("normalized %s=%#v", field, action[field])
		}
	}
	decisions, ok := payload["decisions"].([]any)
	if !ok || len(decisions) != 1 {
		t.Fatalf("normalized decisions=%#v", payload["decisions"])
	}
	decision, ok := decisions[0].(map[string]any)
	if !ok || decision["review_at"] != "2026-09-14T00:00:00Z" {
		t.Fatalf("normalized decision=%#v", decisions[0])
	}
}

func TestFocusAnalysisApplyNormalizesGenericResultsUnit(t *testing.T) {
	raw := []byte(`{"schema_version":1,"run_id":"focus-run-1","focus_id":"focus-1","focus_revision":1,"evidence_set_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","prompt_version":"focus-v1","reader_focus_claim_id":"legacy-claim","results":[{"id":"signal-1","type":"signal","title":"系统可靠性成为关键","summary":"两条资料支持这一变化","source_ids":["entry-1","entry-2"],"confidence":"medium","counter_evidence":"仍需厂商数据","verification_needed":"补充真实采用率"},{"id":"action-1","type":"action","title":"核验厂商数据","source_ids":"entry-1","confidence":"medium"},{"id":"decision-1","type":"decision","title":"是否进入验证阶段？","summary":"先做小范围验证","source_ids":["entry-1","entry-2"],"confidence":"medium"}]}`)
	adapted, err := adaptAppDataRequest("focus_analysis.apply", raw)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(adapted, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["results"]; ok {
		t.Fatalf("generic results were not removed: %s", adapted)
	}
	if _, ok := payload["reader_focus_claim_id"]; ok {
		t.Fatalf("legacy claim field was not removed: %s", adapted)
	}
	if payload["status"] != "applied" {
		t.Fatalf("status=%#v", payload["status"])
	}
	signals := payload["signals"].([]any)
	signal := signals[0].(map[string]any)
	if signal["detail"] != "两条资料支持这一变化" {
		t.Fatalf("signal=%#v", signal)
	}
	actions := payload["actions"].([]any)
	action := actions[0].(map[string]any)
	if action["task"] != "核验厂商数据" || action["owner"] != "负责人待确认" || action["priority"] != "medium" {
		t.Fatalf("action=%#v", action)
	}
	decisions := payload["decisions"].([]any)
	decision := decisions[0].(map[string]any)
	if decision["question"] != "是否进入验证阶段？" || decision["recommendation"] != "先做小范围验证" {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestAppDataDashboardTodoDefaultsAndInvalidEnum(t *testing.T) {
	db = openTestDB(t)
	defaultAction, err := adaptAppDataRequest("dashboard.write", []byte(`{"schema_version":1,"generated_at":"2026-08-13T12:00:00Z","source":{"kind":"mixed","status":"ok"},"task":"只补默认字段","news":[],"evidence":[],"metrics":[],"projects":[],"signals":[],"decisions":[]}`))
	if err != nil || !strings.Contains(string(defaultAction), `"owner":"负责人待确认"`) || !strings.Contains(string(defaultAction), `"priority":"medium"`) {
		t.Fatalf("default action adapter err=%v body=%s", err, defaultAction)
	}
	invalid, err := adaptAppDataRequest("dashboard.write", []byte(`{"schema_version":1,"generated_at":"2026-08-13T12:00:00Z","source":{"kind":"mixed","status":"ok"},"actions":[{"title":"错误优先级","priority":"urgent"}],"news":[],"evidence":[],"metrics":[],"projects":[],"signals":[],"decisions":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(invalid), `"priority":"urgent"`) {
		t.Fatalf("invalid enum was unexpectedly rewritten: %s", invalid)
	}
	result := appDataCall(t, "/api/app-data/v1/operations/dashboard.write", string(invalid), "todo-invalid-enum")
	if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "行动字段不合法") {
		t.Fatalf("invalid priority status=%d body=%s", result.Code, result.Body.String())
	}
}

func TestAppDataBridgeRejectsUnsafeEnvelopeAndQueryUnit(t *testing.T) {
	db = openTestDB(t)
	missingKey := appDataCall(t, "/api/app-data/v1/operations/module.update", `{}`, "")
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}
	unknown := appDataCall(t, "/api/app-data/v1/operations/not.allowed", `{}`, "key-1")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown operation status=%d body=%s", unknown.Code, unknown.Body.String())
	}
	badQuery := appDataCall(t, "/api/app-data/v1/operations/source.list?token=secret", "", "")
	if badQuery.Code != http.StatusBadRequest {
		t.Fatalf("unsafe query status=%d body=%s", badQuery.Code, badQuery.Body.String())
	}
	duplicateQuery := appDataCall(t, "/api/app-data/v1/operations/source.list?q=a&q=b", "", "")
	if duplicateQuery.Code != http.StatusBadRequest {
		t.Fatalf("duplicate query status=%d body=%s", duplicateQuery.Code, duplicateQuery.Body.String())
	}
	scopedQuery := appDataCall(t, "/api/app-data/v1/operations/source.list?source_id=github-blog&include_counts=0", "", "")
	if scopedQuery.Code != http.StatusOK || !strings.Contains(scopedQuery.Body.String(), `"id":"github-blog"`) {
		t.Fatalf("scoped source query status=%d body=%s", scopedQuery.Code, scopedQuery.Body.String())
	}
}

func TestReaderFocusAppDataReadAllowlistFlow(t *testing.T) {
	db = openTestDB(t)
	focus := appDataCall(t, "/api/app-data/v1/operations/reader_focus.get", "", "")
	if focus.Code != http.StatusOK || !strings.Contains(focus.Body.String(), `"schema_version":1`) || !strings.Contains(focus.Body.String(), `"policy"`) {
		t.Fatalf("reader_focus.get status=%d body=%s", focus.Code, focus.Body.String())
	}
	recommendations := appDataCall(t, "/api/app-data/v1/operations/reader_recommendation.read?scope=bootstrap&limit=5", "", "")
	if recommendations.Code != http.StatusOK || !strings.Contains(recommendations.Body.String(), `"reader_recommendations"`) {
		t.Fatalf("reader_recommendation.read status=%d body=%s", recommendations.Code, recommendations.Body.String())
	}
	tooMany := appDataCall(t, "/api/app-data/v1/operations/reader_recommendation.read?limit=6", "", "")
	if tooMany.Code != http.StatusBadRequest {
		t.Fatalf("reader_recommendation.read limit status=%d body=%s", tooMany.Code, tooMany.Body.String())
	}
	unknown := appDataCall(t, "/api/app-data/v1/operations/reader_recommendation.read?scope=daily&refresh=1", "", "")
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("reader_recommendation.read unknown query status=%d body=%s", unknown.Code, unknown.Body.String())
	}
}

func TestAppDataSourceURLAdapterUnit(t *testing.T) {
	created, err := adaptAppDataRequest("source.create", []byte(`{"name":"Example","source_url":"https://example.com/feed.xml"}`))
	if err != nil || strings.Contains(string(created), "source_url") || !strings.Contains(string(created), `"url":"https://example.com/feed.xml"`) {
		t.Fatalf("source.create adapter err=%v body=%s", err, created)
	}
	adapted, err := adaptAppDataRequest("source.update", []byte(`{"id":"github-blog","source_url":"https://example.com/feed.xml"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(adapted), "source_url") || !strings.Contains(string(adapted), `"url":"https://example.com/feed.xml"`) {
		t.Fatalf("internal source adapter = %s", adapted)
	}
	if _, err := adaptAppDataRequest("source.update", []byte(`{"url":"https://example.com/feed.xml"}`)); err == nil {
		t.Fatal("legacy url was accepted on the app-data transport")
	}
	if _, err := adaptAppDataRequest("source.update", []byte(`{"sourceUrl":"https://example.com/feed.xml"}`)); err == nil {
		t.Fatal("near-miss sourceUrl was accepted on the app-data transport")
	}
	dashboard, err := adaptAppDataRequest("dashboard.write", []byte(`{"schema_version":1,"news":[{"id":"n1","source_url":"https://example.com/n1"}]}`))
	if err != nil || !strings.Contains(string(dashboard), `"url":"https://example.com/n1"`) {
		t.Fatalf("dashboard source adapter err=%v body=%s", err, dashboard)
	}
	projected, err := projectAppDataResponse("entry.write", []byte(`{"id":"n1","url":"https://example.com/n1"}`))
	if err != nil || strings.Contains(string(projected), `"url"`) || !strings.Contains(string(projected), `"source_url"`) {
		t.Fatalf("public source projection err=%v body=%s", err, projected)
	}
	redacted, err := projectAppDataResponse("source.list", []byte(`[{"id":"legacy","url":"https://example.com/feed?X-Amz-Signature=top-secret"}]`))
	if err != nil || strings.Contains(string(redacted), "top-secret") || strings.Contains(string(redacted), "source_url") || strings.Contains(string(redacted), `"url"`) {
		t.Fatalf("sensitive source projection err=%v body=%s", err, redacted)
	}
	discovery, err := projectAppDataResponse("discovery.list", []byte(`[{"id":"legacy","input_url":"https://example.com/feed?sig=top-secret","feed_url":"https://example.com/feed?signature=top-secret","site_url":"https://example.com/"}]`))
	if err != nil || strings.Contains(string(discovery), "top-secret") || strings.Contains(string(discovery), "input_url") || strings.Contains(string(discovery), "feed_url") || !strings.Contains(string(discovery), "site_url") {
		t.Fatalf("sensitive discovery projection err=%v body=%s", err, discovery)
	}
}

func TestAppDataSourceCreateAndEntryProjectionFlow(t *testing.T) {
	db = openTestDB(t)
	previous := feedSyncRuntimeEnabled
	feedSyncRuntimeEnabled = false
	t.Cleanup(func() { feedSyncRuntimeEnabled = previous })
	created := appDataCall(t, "/api/app-data/v1/operations/source.create", `{"name":"Example","source_url":"https://example.com/feed.xml"}`, "source-create-source-url")
	if created.Code != http.StatusOK || strings.Contains(created.Body.String(), `"url"`) || !strings.Contains(created.Body.String(), `"source_url":"https://example.com/feed.xml"`) || !strings.Contains(created.Body.String(), `"collection_mode":"app"`) {
		t.Fatalf("source.create status=%d body=%s", created.Code, created.Body.String())
	}
	var sourceRows, mutationRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE url='https://example.com/feed.xml'`).Scan(&sourceRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_data_mutation_requests WHERE operation='source.create'`).Scan(&mutationRows); err != nil {
		t.Fatal(err)
	}
	if sourceRows != 1 || mutationRows != 1 {
		t.Fatalf("source create state: sources=%d reservations=%d", sourceRows, mutationRows)
	}
	source, err := feedSourceByID("github-blog")
	if err != nil {
		t.Fatal(err)
	}
	entryBody := `{"source_id":"` + source.ID + `","source_type":"rss","external_id":"entry-1","title":"一条内容","source_url":"https://example.com/entry-1","summary_text":"摘要"}`
	entry := appDataCall(t, "/api/app-data/v1/operations/entry.write", entryBody, "entry-write-source-url")
	if entry.Code != http.StatusOK || strings.Contains(entry.Body.String(), `"url"`) || !strings.Contains(entry.Body.String(), `"source_url"`) {
		t.Fatalf("entry.write status=%d body=%s", entry.Code, entry.Body.String())
	}
	focusLink := appDataCall(t, "/api/app-data/v1/operations/entry.write", `{"schema_version":1,"source_id":"focus-chat-links","source_type":"article","title":"HTTP 语义","source_url":"https://www.rfc-editor.org/rfc/rfc9110","content":"HTTP Semantics defines the architecture and shared terminology.","content_format":"text","summary_text":"HTTP 语义规范"}`, "entry-write-focus-link-v1")
	if focusLink.Code != http.StatusOK || !strings.Contains(focusLink.Body.String(), `"source_url":"https://www.rfc-editor.org/rfc/rfc9110"`) {
		t.Fatalf("focus link entry.write status=%d body=%s", focusLink.Code, focusLink.Body.String())
	}
	focusNote := appDataCall(t, "/api/app-data/v1/operations/entry.write", `{"schema_version":1,"source_id":"focus-chat-links","source_type":"note","title":"访谈记录","content":"用户需要端侧 AI，但仍需补充稳定性和成本证据。","content_format":"text","summary_text":"端侧 AI 访谈摘录"}`, "entry-write-focus-note-v1")
	if focusNote.Code != http.StatusOK || !strings.Contains(focusNote.Body.String(), `"source_url":""`) || !strings.Contains(focusNote.Body.String(), `"source_type":"note"`) {
		t.Fatalf("focus note entry.write status=%d body=%s", focusNote.Code, focusNote.Body.String())
	}
	signed := appDataCall(t, "/api/app-data/v1/operations/source.create", `{"source_url":"https://example.com/private.xml?signature=top-secret"}`, "source-create-signed-url")
	if signed.Code != http.StatusBadRequest || strings.Contains(signed.Body.String(), "top-secret") {
		t.Fatalf("signed source status=%d body=%s", signed.Code, signed.Body.String())
	}
}

func TestAppDataDiscoveryUsesInputAndConfirmsByIDFlow(t *testing.T) {
	db = openTestDB(t)
	if _, err := adaptAppDataRequest("discovery.create", []byte(`{"input":"https://example.com/feed.xml"}`)); err != nil {
		t.Fatalf("discovery input field rejected: %v", err)
	}
	if _, err := adaptAppDataRequest("discovery.create", []byte(`{"input_url":"https://example.com/feed.xml"}`)); err == nil {
		t.Fatal("internal input_url was accepted on the app-data transport")
	}

	now := "2026-08-15T12:00:00Z"
	if _, err := db.Exec(`INSERT INTO feed_discovery_candidates
(id,input_url,normalized_url,feed_url,title,description,site_url,icon_url,feed_format,status,error_message,preview_json,discovered_at,confirmed_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"discovery-app-data", "https://example.com/feed.xml", "https://example.com/feed.xml", "https://example.com/feed.xml",
		"Example Feed", "Public updates", "https://example.com/", "", "rss", "verified", "", "[]", now, ""); err != nil {
		t.Fatal(err)
	}

	extraFields := appDataCall(t, "/api/app-data/v1/operations/discovery.confirm", `{"id":"discovery-app-data","input_url":"https://example.com/feed.xml"}`, "discovery-confirm-extra")
	if extraFields.Code != http.StatusBadRequest {
		t.Fatalf("discovery.confirm extra fields status=%d body=%s", extraFields.Code, extraFields.Body.String())
	}
	confirmed := appDataCall(t, "/api/app-data/v1/operations/discovery.confirm", `{"id":"discovery-app-data"}`, "discovery-confirm-id")
	if confirmed.Code != http.StatusOK || !strings.Contains(confirmed.Body.String(), `"collection_mode":"app"`) || !strings.Contains(confirmed.Body.String(), `"subscribed":true`) {
		t.Fatalf("discovery.confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	var sources, subscriptions, requests int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources WHERE id=?`, sourceIDForURL("https://example.com/feed.xml")).Scan(&sources); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_subscriptions WHERE source_id=?`, sourceIDForURL("https://example.com/feed.xml")).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_requests WHERE source_id=?`, sourceIDForURL("https://example.com/feed.xml")).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if sources != 1 || subscriptions != 1 || requests != 1 {
		t.Fatalf("confirmed source state: sources=%d subscriptions=%d requests=%d", sources, subscriptions, requests)
	}
}

func TestAppDataMutationReservationIsBoundedUnit(t *testing.T) {
	db = openTestDB(t)
	for index := 0; index < maxPersistedMutationRecords+8; index++ {
		key := "bounded-" + string(rune('a'+index%26)) + "-" + string(rune('0'+index%10))
		fingerprint := appDataMutationFingerprint("settings.update", []byte(`{"brief_enabled":true}`), key)
		reserved, _, _, _, err := reserveAppDataMutation("settings.update", key, fingerprint)
		if err != nil || !reserved {
			t.Fatalf("reserve %d reserved=%v err=%v", index, reserved, err)
		}
		if err := completeAppDataMutation("settings.update", key, fingerprint, http.StatusOK, []byte(`{"ok":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM app_data_mutation_requests`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count > maxPersistedMutationRecords {
		t.Fatalf("mutation tombstones exceeded bound: %d", count)
	}
}

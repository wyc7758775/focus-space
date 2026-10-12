package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// appDataOperationSpec is the only mapping from an Agent operation name to a
// dashboard handler. Local Server calls this fixed dispatcher after checking
// the operation against metadata.json; the app still keeps the final closed
// method/path allowlist so a stale or hand-written capability cannot turn the
// bridge into a generic HTTP proxy.
type appDataOperationSpec struct {
	method      string
	path        string
	queryKeys   map[string]struct{}
	mutation    bool
	trustedHost bool
	handler     http.HandlerFunc
}

const (
	appDataOperationPrefix            = "/api/app-data/v1/operations/"
	maxAppDataRequestBytes            = 128 << 10
	maxAppDataResponseBytes           = 512 << 10
	maxPersistedMutationResponseBytes = 192 << 10
	maxPersistedMutationRecords       = 128
)

func appDataOperationSpecs() map[string]appDataOperationSpec {
	specs := map[string]appDataOperationSpec{
		"dashboard.get":              {http.MethodGet, "/api/dashboard", dashboardQueryKeys(), false, false, dashboardHandler},
		"dashboard.write":            {http.MethodPost, "/api/dashboard", nil, true, false, dashboardHandler},
		"settings.get":               {http.MethodGet, "/api/settings", nil, false, false, settingsHandler},
		"settings.update":            {http.MethodPatch, "/api/settings", nil, true, false, settingsHandler},
		"action.feedback":            {http.MethodPost, "/api/actions/feedback", nil, true, false, actionFeedbackHandler},
		"action.schedule.begin":      {http.MethodPost, "/api/actions/schedule/begin", nil, true, false, actionScheduleBeginHandler},
		"action.schedule.receipt":    {http.MethodPost, "/api/actions/schedule/receipt", nil, true, true, actionScheduleReceiptHandler},
		"source.list":                {http.MethodGet, "/api/sources", sourceQueryKeys(), false, false, sourceHandler},
		"source.create":              {http.MethodPost, "/api/sources", nil, true, false, sourceHandler},
		"source.update":              {http.MethodPatch, "/api/sources", nil, true, false, sourceHandler},
		"source.preview":             {http.MethodGet, "/api/sources/preview", map[string]struct{}{"source_id": {}}, false, false, sourcePreviewHandler},
		"entry.list":                 {http.MethodGet, "/api/entries", entryQueryKeys(), false, false, entryHandler},
		"entry.write":                {http.MethodPost, "/api/entries", nil, true, false, entryHandler},
		"entry.state":                {http.MethodPatch, "/api/entries", nil, true, false, entryHandler},
		"entry.content":              {http.MethodGet, "/api/entries/content", map[string]struct{}{"id": {}, "refresh": {}}, false, false, entryContentHandler},
		"context.query":              {http.MethodGet, "/api/context", contextQueryKeys(), false, false, stableContextHandler},
		"context.confirm":            {http.MethodPost, "/api/context", nil, true, false, stableContextHandler},
		"context.remove":             {http.MethodDelete, "/api/context", nil, true, false, stableContextHandler},
		"reader_settings.get":        {http.MethodGet, "/api/reader", nil, false, false, readerPreferenceHandler},
		"reader_settings.update":     {http.MethodPatch, "/api/reader", nil, true, false, readerPreferenceHandler},
		"reader_counts.get":          {http.MethodGet, "/api/reader/counts", nil, false, false, readerCountsHandler},
		"reader_focus.get":           {http.MethodGet, "/api/reader-focus", nil, false, false, readerFocusHandler},
		"reader_focus.update":        {http.MethodPatch, "/api/reader-focus", nil, true, false, readerFocusHandler},
		"reader_recommendation.read": {http.MethodGet, "/api/reader-recommendations", readerRecommendationQueryKeys(), false, false, readerRecommendationReadHandler},
		"reader_digest.read":         {http.MethodGet, "/api/internal/reader-digest/read", map[string]struct{}{"topic_id": {}}, false, false, readerDigestReadOperationHandler},
		"reader_digest.apply":        {http.MethodPost, "/api/internal/reader-digest/apply", nil, true, false, readerDigestApplyOperationHandler},
		"focus_space.get":            {http.MethodGet, "/api/focus-space", focusSpaceQueryKeys(), false, false, focusSpaceHandler},
		"focus_space.update":         {http.MethodPatch, "/api/focus-space", nil, true, false, focusSpaceHandler},
		"focus_attachment.import":    {http.MethodPost, "/api/internal/focus-attachments/import", nil, true, true, focusAttachmentImportHandler},
		"focus_analysis.read":        {http.MethodGet, "/api/focus-analysis/read", focusAnalysisReadQueryKeys(), false, false, focusAnalysisReadHandler},
		"focus_analysis.apply":       {http.MethodPost, "/api/internal/focus-analysis/apply", nil, true, false, focusAnalysisApplyHandler},
		"focus_analysis.get":         {http.MethodGet, "/api/focus-analysis", focusAnalysisGetQueryKeys(), false, false, focusAnalysisGetHandler},
		"focus_result.feedback":      {http.MethodPost, "/api/focus-results/feedback", nil, true, false, focusResultFeedbackHandler},
		"decision_run.start":         {http.MethodPost, "/api/internal/decision-runs/start", nil, true, false, decisionRunStartHandler},
		"decision_run.get":           {http.MethodGet, "/api/internal/decision-runs", decisionRunQueryKeys(), false, false, decisionRunGetHandler},
		"decision_run.answer":        {http.MethodPost, "/api/internal/decision-runs/answer", nil, true, false, decisionRunAnswerHandler},
		"decision_run.resume":        {http.MethodPost, "/api/internal/decision-runs/resume", nil, true, false, decisionRunResumeHandler},
		"decision_run.cancel":        {http.MethodPost, "/api/internal/decision-runs/cancel", nil, true, false, decisionRunCancelHandler},
		"decision_revision.apply":    {http.MethodPost, "/api/internal/decision-revisions/apply", nil, true, false, decisionRevisionApplyHandler},
		"decision_revision.get":      {http.MethodGet, "/api/internal/decision-revisions", decisionRevisionQueryKeys(), false, false, decisionRevisionGetHandler},
		"decision_revision.compare":  {http.MethodGet, "/api/internal/decision-revisions/compare", decisionRevisionCompareQueryKeys(), false, false, decisionRevisionCompareHandler},
		"decision_feedback.apply":    {http.MethodPost, "/api/internal/decision-feedback/apply", nil, true, false, decisionFeedbackApplyHandler},
		"decision_review.apply":      {http.MethodPost, "/api/internal/decision-reviews/apply", nil, true, false, decisionReviewApplyHandler},
		"module.update":              {http.MethodPatch, "/api/modules", nil, true, false, modulePreferenceHandler},
		"layout.update":              {http.MethodPatch, "/api/layout", nil, true, false, moduleLayoutHandler},
		"recording.list":             {http.MethodGet, "/api/recordings", map[string]struct{}{"limit": {}}, false, false, recordingsHandler},
		"recording.get":              {http.MethodGet, "/api/recordings/detail", map[string]struct{}{"id": {}}, false, false, recordingDetailHandler},
		"recording.state":            {http.MethodPatch, "/api/recordings/preferences", nil, true, false, recordingPreferenceHandler},
		"recording.action_link":      {http.MethodPost, "/api/recordings/actions", nil, true, false, recordingActionHandler},
		"recording.action_unlink":    {http.MethodDelete, "/api/recordings/actions", nil, true, false, recordingActionHandler},
		"folder.list":                {http.MethodGet, "/api/folders", nil, false, false, folderHandler},
		"folder.create":              {http.MethodPost, "/api/folders", nil, true, false, folderHandler},
		"folder.update":              {http.MethodPatch, "/api/folders", nil, true, false, folderHandler},
		"folder.delete":              {http.MethodDelete, "/api/folders", nil, true, false, folderHandler},
		"tag.list":                   {http.MethodGet, "/api/tags", nil, false, false, tagHandler},
		"tag.create":                 {http.MethodPost, "/api/tags", nil, true, false, tagHandler},
		"tag.update":                 {http.MethodPatch, "/api/tags", nil, true, false, tagHandler},
		"tag.delete":                 {http.MethodDelete, "/api/tags", nil, true, false, tagHandler},
		"pack.list":                  {http.MethodGet, "/api/packs", nil, false, false, sourcePackHandler},
		"pack.subscribe":             {http.MethodPost, "/api/packs", nil, true, false, sourcePackHandler},
		"discovery.list":             {http.MethodGet, "/api/discoveries", nil, false, false, discoveryHandler},
		"discovery.create":           {http.MethodPost, "/api/discoveries", nil, true, false, discoveryHandler},
		"discovery.confirm":          {http.MethodPatch, "/api/discoveries", nil, true, false, discoveryHandler},
		"subscription.list":          {http.MethodGet, "/api/subscriptions", nil, false, false, subscriptionHandler},
		"subscription.set":           {http.MethodPatch, "/api/subscriptions", nil, true, false, subscriptionHandler},
		"subscription.bulk":          {http.MethodPost, "/api/subscriptions/bulk", nil, true, false, bulkSubscriptionHandler},
		"sync.list":                  {http.MethodGet, "/api/sync", nil, false, false, syncHandler},
		"sync.request":               {http.MethodPost, "/api/sync", nil, true, false, syncHandler},
		"entry.bulk_state":           {http.MethodPatch, "/api/items/bulk", nil, true, false, bulkItemPreferenceHandler},
	}
	specs["focus_blueprint.get"] = appDataOperationSpec{http.MethodGet, "/api/internal/focus-blueprints", focusBlueprintQueryKeys(), false, false, focusBlueprintGetHandler}
	specs["focus_blueprint.preview"] = appDataOperationSpec{http.MethodPost, "/api/internal/focus-blueprints/preview", nil, true, false, focusBlueprintMutationHandler}
	specs["focus_blueprint.apply"] = appDataOperationSpec{http.MethodPost, "/api/internal/focus-blueprints/apply", nil, true, false, focusBlueprintMutationHandler}
	specs["focus_blueprint.rollback"] = appDataOperationSpec{http.MethodPost, "/api/internal/focus-blueprints/rollback", nil, true, false, focusBlueprintMutationHandler}
	specs["focus_work_product.get"] = appDataOperationSpec{http.MethodGet, "/api/internal/focus-work-products", focusWorkProductQueryKeys(), false, false, focusWorkProductGetHandler}
	specs["focus_work_product.feedback"] = appDataOperationSpec{http.MethodPost, "/api/internal/focus-work-products/feedback", nil, true, false, focusWorkProductFeedbackHandler}
	specs["focus_work_product.link"] = appDataOperationSpec{http.MethodPost, "/api/internal/focus-work-products/link", nil, true, false, focusWorkProductLinkHandler}
	return specs
}

func readerRecommendationQueryKeys() map[string]struct{} {
	return map[string]struct{}{"scope": {}, "limit": {}, "topic_id": {}}
}

func dashboardQueryKeys() map[string]struct{} {
	return map[string]struct{}{"include_news": {}, "include_sources": {}, "include_counts": {}}
}

// reader_digest.apply owns its idempotency row inside the same transaction as
// the dashboard projection. It must not use the generic reservation table
// below, otherwise the generic pending row would be committed before the
// dashboard transaction starts and every first apply would fail closed.
func appDataOperationOwnsMutationTransaction(operation string) bool {
	return operation == "reader_digest.apply" || operation == "focus_analysis.apply" || operation == "focus_attachment.import"
}

func sourceQueryKeys() map[string]struct{} {
	return map[string]struct{}{"source_id": {}, "q": {}, "category": {}, "language": {}, "content_type": {}, "only_subscribed": {}, "sort": {}, "include_counts": {}}
}

func entryQueryKeys() map[string]struct{} {
	return map[string]struct{}{"source_id": {}, "external_id": {}, "q": {}, "limit": {}, "cursor": {}, "include_deleted": {}, "full": {}, "view_type": {}}
}

func contextQueryKeys() map[string]struct{} {
	return map[string]struct{}{"q": {}, "top_k": {}, "max_tokens": {}, "source_type": {}, "since": {}, "membership": {}}
}

func appDataMutationFingerprint(operation string, body []byte, rawQuery string) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(operation))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(body)
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(rawQuery))
	return hex.EncodeToString(digest.Sum(nil))
}

// adaptAppDataRequest translates the public transport contract to the
// dashboard's private handler DTOs. The generic platform boundary only
// permits the exact source_url field; the application may continue to keep
// its historical SQLite/page field named url internally.
func adaptAppDataRequest(operation string, raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, errors.New("操作数据必须是 JSON 对象")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("操作数据包含多余内容")
	}
	if err := rejectAppDataTransportFields(operation, value, ""); err != nil {
		return nil, err
	}
	switch operation {
	case "source.create", "source.update", "entry.write":
		if err := renameSourceURL(value); err != nil {
			return nil, err
		}
	case "focus_space.update":
		// Early Agent prompts used source category names ("meeting", "reader",
		// "obsidian") as creation hints. They are not source bindings because
		// they contain no scoped source_id. Ignore only that legacy all-string
		// create shape so the focus itself is not lost; real source objects and
		// every update/link mutation remain strictly validated by the handler.
		if value["action"] == "create" {
			if sources, ok := value["sources"].([]any); ok && len(sources) > 0 {
				legacyCategories := map[string]struct{}{
					"meeting":  {},
					"reader":   {},
					"obsidian": {},
				}
				allCategories := true
				for _, source := range sources {
					category, isString := source.(string)
					if !isString {
						allCategories = false
						break
					}
					if _, known := legacyCategories[strings.ToLower(strings.TrimSpace(category))]; !known {
						allCategories = false
						break
					}
				}
				if allCategories {
					delete(value, "sources")
				}
			}
		}
	case "focus_analysis.apply":
		if err := normalizeFocusAnalysisApply(value); err != nil {
			return nil, err
		}
	case "decision_run.resume":
		normalizeDecisionRunResume(value)
	case "dashboard.write":
		if err := normalizeDashboardActions(value); err != nil {
			return nil, err
		}
		for _, collection := range []string{"news", "sources"} {
			if err := renameSourceURLCollection(value, collection); err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(value)
}

// normalizeDecisionRunResume removes only semantically absent optional values
// that models commonly emit while copying the typed contract. The private DTO
// intentionally keeps question as a structured object; an empty string must
// therefore mean "no question", not make the whole state transition invalid.
func normalizeDecisionRunResume(value map[string]any) {
	if question, exists := value["question"]; exists && (question == nil || strings.TrimSpace(fmt.Sprint(question)) == "") {
		delete(value, "question")
	}
	if usage, exists := value["usage"]; exists {
		if usage == nil {
			delete(value, "usage")
		} else if object, ok := usage.(map[string]any); ok && len(object) == 0 {
			delete(value, "usage")
		}
	}
}

// normalizeFocusAnalysisApply keeps the bounded AI writeback tolerant of the
// common JSON variants produced by otherwise valid model output: a generic
// results/insights array, a single evidence string instead of a one-item array,
// and P0/P1/P2/P3 priority labels.
// It does not add fields outside the closed focus-analysis contract; the
// domain handler still performs the authoritative run, citation and enum
// validation after this app-specific adaptation.
func normalizeFocusAnalysisApply(value map[string]any) error {
	if err := normalizeLegacyFocusAnalysisResults(value); err != nil {
		return err
	}
	delete(value, "reader_focus_claim_id")
	if _, ok := value["status"]; !ok {
		value["status"] = "applied"
	}
	collections := map[string][]string{
		"signals":   {"source_ids", "supporting_evidence", "counterevidence", "validation_items"},
		"actions":   {"source_ids", "supporting_evidence", "counterevidence", "validation_items"},
		"decisions": {"source_ids", "supporting_evidence", "counterevidence", "validation_items", "options", "assumptions", "risks"},
	}
	for collection, listFields := range collections {
		rawItems, present := value[collection]
		if !present {
			continue
		}
		items, ok := rawItems.([]any)
		if !ok {
			return fmt.Errorf("%s 必须是数组", collection)
		}
		for _, rawItem := range items {
			item, ok := rawItem.(map[string]any)
			if !ok {
				return fmt.Errorf("%s 条目格式不合法", collection)
			}
			for _, field := range listFields {
				if single, ok := item[field].(string); ok {
					if strings.TrimSpace(single) == "" {
						item[field] = []any{}
					} else {
						item[field] = []any{single}
					}
				}
			}
			if collection == "decisions" {
				if reviewAt, ok := item["review_at"].(string); ok {
					reviewAt = strings.TrimSpace(reviewAt)
					if parsed, err := time.Parse("2006-01-02", reviewAt); err == nil {
						item["review_at"] = parsed.UTC().Format(time.RFC3339)
					}
				}
				continue
			}
			if collection != "actions" {
				continue
			}
			owner, _ := item["owner"].(string)
			if strings.TrimSpace(owner) == "" {
				item["owner"] = "负责人待确认"
			}
			if _, ok := item["due_at"]; !ok {
				item["due_at"] = ""
			}
			priority, _ := item["priority"].(string)
			switch strings.ToLower(strings.TrimSpace(priority)) {
			case "", "normal", "p2":
				item["priority"] = "medium"
			case "p0", "p1", "urgent", "critical":
				item["priority"] = "high"
			case "p3", "p4":
				item["priority"] = "low"
			}
		}
	}
	return nil
}

func normalizeLegacyFocusAnalysisResults(value map[string]any) error {
	legacyKey := ""
	for _, key := range []string{"results", "insights"} {
		if _, ok := value[key]; !ok {
			continue
		}
		if legacyKey != "" {
			return errors.New("焦点分析不能同时包含 results 和 insights")
		}
		legacyKey = key
	}
	if legacyKey == "" {
		return nil
	}
	for _, key := range []string{"signals", "actions", "decisions"} {
		if _, ok := value[key]; ok {
			return errors.New("焦点分析不能混用通用结果和分类结果")
		}
	}
	items, ok := value[legacyKey].([]any)
	if !ok {
		return fmt.Errorf("%s 必须是数组", legacyKey)
	}
	grouped := map[string][]any{"signals": {}, "actions": {}, "decisions": {}}
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 条目格式不合法", legacyKey)
		}
		kind, _ := item["type"].(string)
		kind = strings.ToLower(strings.TrimSpace(kind))
		delete(item, "type")
		if _, ok := item["source_ids"]; !ok {
			if evidence, exists := item["supporting_evidence"]; exists {
				item["source_ids"] = evidence
			}
		}
		if counter, ok := item["counter_evidence"]; ok {
			if _, exists := item["counterevidence"]; !exists {
				item["counterevidence"] = counter
			}
			delete(item, "counter_evidence")
		}
		if verification, ok := item["verification_needed"]; ok {
			if _, exists := item["validation_items"]; !exists {
				item["validation_items"] = verification
			}
			delete(item, "verification_needed")
		}
		if _, ok := item["freshness"]; !ok {
			item["freshness"] = "本轮证据"
		}
		summary, _ := item["summary"].(string)
		title, _ := item["title"].(string)
		switch kind {
		case "signal", "insight":
			if _, ok := item["detail"]; !ok {
				item["detail"] = summary
			}
			delete(item, "summary")
			grouped["signals"] = append(grouped["signals"], item)
		case "action":
			if _, ok := item["task"]; !ok {
				item["task"] = firstNonEmptyString(title, summary)
			}
			delete(item, "title")
			delete(item, "summary")
			grouped["actions"] = append(grouped["actions"], item)
		case "decision":
			if _, ok := item["question"]; !ok {
				item["question"] = title
			}
			if _, ok := item["recommendation"]; !ok {
				item["recommendation"] = summary
			}
			delete(item, "title")
			delete(item, "summary")
			grouped["decisions"] = append(grouped["decisions"], item)
		default:
			return fmt.Errorf("%s 条目 type 不合法", legacyKey)
		}
	}
	delete(value, legacyKey)
	for key, collection := range grouped {
		value[key] = collection
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// normalizeDashboardActions keeps the public dashboard contract tolerant of
// the short todo shapes commonly produced by Chat while keeping the persisted
// Action schema closed and validated by putDashboardInternal. This is an app
// adapter, not a platform-wide alias list.
func normalizeDashboardActions(value map[string]any) error {
	rawActions, hasActions := value["actions"]
	if !hasActions {
		if !containsActionInput(value) {
			return nil
		}
		action := make(map[string]any)
		for key := range actionInputKeys {
			if raw, ok := value[key]; ok {
				action[key] = raw
				delete(value, key)
			}
		}
		value["actions"] = []any{action}
		rawActions = value["actions"]
	}

	actions, ok := rawActions.([]any)
	if !ok {
		return errors.New("actions 必须是数组")
	}
	for index, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok {
			return errors.New("行动条目格式不合法")
		}
		if err := normalizeDashboardAction(action, index); err != nil {
			return err
		}
	}
	return nil
}

var actionInputKeys = map[string]struct{}{
	"id": {}, "action_id": {}, "task": {}, "title": {}, "description": {},
	"owner": {}, "assignee": {}, "responsible": {}, "assignee_name": {},
	"due_at": {}, "due_date": {}, "deadline": {}, "due": {},
	"source_ids": {}, "source_id": {}, "status": {}, "state": {}, "action_status": {},
	"confirmation_status": {}, "priority": {},
}

func containsActionInput(value map[string]any) bool {
	for key := range actionInputKeys {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func normalizeDashboardAction(action map[string]any, index int) error {
	if err := moveActionAlias(action, "id", "action_id"); err != nil {
		return err
	}
	if err := moveActionAlias(action, "owner", "assignee", "responsible", "assignee_name"); err != nil {
		return err
	}
	if err := moveActionAlias(action, "due_at", "due_date", "deadline", "due"); err != nil {
		return err
	}
	if err := moveActionAlias(action, "status", "state", "action_status"); err != nil {
		return err
	}
	if err := moveActionAlias(action, "source_ids", "source_id"); err != nil {
		return err
	}
	if sourceID, ok := action["source_ids"].(string); ok {
		action["source_ids"] = []any{sourceID}
	}

	// title/description are the usual Chat todo shape. Action has one visible
	// task field, so retain both pieces of user text in that field.
	task, hasTask := action["task"].(string)
	title, hasTitle := action["title"].(string)
	description, hasDescription := action["description"].(string)
	if !hasTask {
		switch {
		case hasTitle && hasDescription && strings.TrimSpace(description) != "":
			task = strings.TrimSpace(title) + "\n" + strings.TrimSpace(description)
		case hasTitle:
			task = title
		case hasDescription:
			task = description
		}
		if task != "" {
			action["task"] = task
		}
	} else if hasDescription && strings.TrimSpace(description) != "" {
		action["task"] = strings.TrimRight(task, " \t\r\n") + "\n" + strings.TrimSpace(description)
	}
	delete(action, "title")
	delete(action, "description")

	if _, ok := action["owner"]; !ok {
		action["owner"] = "负责人待确认"
	}
	if _, ok := action["due_at"]; !ok {
		action["due_at"] = ""
	}
	if _, ok := action["status"]; !ok {
		action["status"] = "draft"
	}
	if _, ok := action["confirmation_status"]; !ok {
		action["confirmation_status"] = "draft"
	}
	if _, ok := action["priority"]; !ok {
		action["priority"] = "medium"
	}
	if _, ok := action["source_ids"]; !ok {
		action["source_ids"] = []any{}
	}
	if _, ok := action["id"]; !ok {
		text, _ := action["task"].(string)
		owner, _ := action["owner"].(string)
		dueAt, _ := action["due_at"].(string)
		action["id"] = stableObjectID("action", fmt.Sprintf("%d|%s|%s|%s", index, text, owner, dueAt))
	}
	return nil
}

func moveActionAlias(action map[string]any, canonical string, aliases ...string) error {
	if _, canonicalPresent := action[canonical]; canonicalPresent {
		for _, alias := range aliases {
			if _, aliasPresent := action[alias]; aliasPresent {
				return fmt.Errorf("%s 与 %s 不能同时出现", canonical, alias)
			}
		}
		return nil
	}
	for _, alias := range aliases {
		if raw, ok := action[alias]; ok {
			action[canonical] = raw
			delete(action, alias)
			break
		}
	}
	return nil
}

func rejectAppDataTransportFields(operation string, value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			if key != "source_url" && !allowedAppDataTransportField(operation, childPath) && forbiddenAppDataTransportField(key) {
				return fmt.Errorf("字段 %q 必须使用 source_url 或被禁止", key)
			}
			if err := rejectAppDataTransportFields(operation, child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := rejectAppDataTransportFields(operation, child, path+"[]"); err != nil {
				return err
			}
		}
	}
	return nil
}

func allowedAppDataTransportField(operation, path string) bool {
	// Focus apply binds the model output to the server-issued prompt contract.
	// Only these exact contract fields are transport data; arbitrary prompts,
	// tokens, model controls, and similarly named nested fields stay forbidden.
	if operation == "focus_analysis.apply" {
		return path == "prompt_version" || path == "usage.input_tokens" || path == "usage.output_tokens"
	}
	if operation == "decision_run.resume" {
		return path == "usage.input_tokens" || path == "usage.output_tokens"
	}
	return false
}

func forbiddenAppDataTransportField(key string) bool {
	if key == "source_url" {
		return false
	}
	canonical := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return -1
	}, key))
	// Near-miss spellings such as sourceUrl are intentionally rejected; the
	// transport contract permits only the exact snake-case field above.
	if canonical == "agentid" || canonical == "auth" || canonical == "authorization" || canonical == "cookie" || canonical == "jwt" || canonical == "deliverytarget" || canonical == "deliverto" || canonical == "destination" {
		return true
	}
	for _, marker := range []string{"token", "header", "method", "prompt", "provider", "model", "skill"} {
		if strings.Contains(canonical, marker) {
			return true
		}
	}
	return strings.HasSuffix(canonical, "url") || strings.HasSuffix(canonical, "uri") || strings.HasSuffix(canonical, "path")
}

func renameSourceURL(value map[string]any) error {
	if raw, ok := value["source_url"]; ok {
		if _, exists := value["url"]; exists {
			return errors.New("source_url 与 url 不能同时出现")
		}
		value["url"] = raw
		delete(value, "source_url")
	}
	return nil
}

func renameSourceURLCollection(value map[string]any, collection string) error {
	items, ok := value[collection]
	if !ok {
		return nil
	}
	list, ok := items.([]any)
	if !ok {
		return fmt.Errorf("%s 必须是数组", collection)
	}
	for _, item := range list {
		object, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s 条目格式不合法", collection)
		}
		if err := renameSourceURL(object); err != nil {
			return err
		}
	}
	return nil
}

// projectAppDataResponse converts private page/SQLite URL names back to the
// canonical public field without changing ordinary browser responses.
func projectAppDataResponse(operation string, raw []byte) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}
	if operation != "source.list" && operation != "source.create" && operation != "source.update" && operation != "source.preview" && operation != "discovery.list" && operation != "discovery.create" && operation != "discovery.confirm" && operation != "entry.list" && operation != "entry.write" && operation != "entry.content" && operation != "dashboard.get" && operation != "dashboard.write" {
		return raw, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	projectSourceURLFields(value)
	return json.Marshal(value)
}

func projectSourceURLFields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		sourceFieldsSensitive := sourceURLFieldHasSensitiveCredentials(typed["url"]) || sourceURLFieldHasSensitiveCredentials(typed["source_url"])
		for key, raw := range typed {
			if appDataURLField(key) && sourceURLFieldHasSensitiveCredentials(raw) {
				delete(typed, key)
			}
		}
		if sourceFieldsSensitive {
			delete(typed, "url")
			delete(typed, "source_url")
		}
		if raw, ok := typed["url"]; ok {
			if _, exists := typed["source_url"]; !exists {
				typed["source_url"] = raw
			}
			delete(typed, "url")
		}
		for _, child := range typed {
			projectSourceURLFields(child)
		}
	case []any:
		for _, child := range typed {
			projectSourceURLFields(child)
		}
	}
}

func appDataURLField(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "url" || strings.HasSuffix(key, "_url")
}

func sourceURLFieldHasSensitiveCredentials(value any) bool {
	raw, ok := value.(string)
	return ok && strings.TrimSpace(raw) != "" && feedURLHasSensitiveCredentials(raw)
}

// reserveAppDataMutation persists an at-most-once reservation before the
// business handler runs. Successful and rejected responses are then stored
// for exact replay. If the process dies after the business write but before
// storing the response, the durable pending row fails closed instead of
// executing the mutation twice.
func reserveAppDataMutation(operation, key, fingerprint string) (bool, string, int, []byte, error) {
	tx, err := db.Begin()
	if err != nil {
		return false, "", 0, nil, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`DELETE FROM app_data_mutation_requests WHERE julianday(created_at) < julianday('now','-7 days')`); err != nil {
		return false, "", 0, nil, err
	}
	if _, err := tx.Exec(`DELETE FROM app_data_mutation_requests WHERE rowid IN (
SELECT rowid FROM app_data_mutation_requests ORDER BY created_at DESC, rowid DESC LIMIT -1 OFFSET ?
)`, maxPersistedMutationRecords-1); err != nil {
		return false, "", 0, nil, err
	}
	result, err := tx.Exec(`INSERT OR IGNORE INTO app_data_mutation_requests(operation,idempotency_key,fingerprint,state,response_status,response_json,created_at,updated_at)
VALUES(?,?,?,'pending',0,'',?,?)`, operation, key, fingerprint, now, now)
	if err != nil {
		return false, "", 0, nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, "", 0, nil, err
	}
	if inserted == 1 {
		if err := tx.Commit(); err != nil {
			return false, "", 0, nil, err
		}
		return true, "pending", 0, nil, nil
	}
	var storedFingerprint, state, response string
	var status int
	if err := tx.QueryRow(`SELECT fingerprint,state,response_status,response_json FROM app_data_mutation_requests WHERE operation=? AND idempotency_key=?`, operation, key).
		Scan(&storedFingerprint, &state, &status, &response); err != nil {
		return false, "", 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, "", 0, nil, err
	}
	if storedFingerprint != fingerprint {
		return false, "conflict", 0, nil, nil
	}
	return false, state, status, []byte(response), nil
}

func completeAppDataMutation(operation, key, fingerprint string, status int, response []byte) error {
	result, err := db.Exec(`UPDATE app_data_mutation_requests
SET state='complete',response_status=?,response_json=?,updated_at=?
WHERE operation=? AND idempotency_key=? AND fingerprint=? AND state='pending'`,
		status, string(response), time.Now().UTC().Format(time.RFC3339Nano), operation, key, fingerprint)
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated != 1 {
		return errActionIdempotencyConflict
	}
	return nil
}

func boundedOperationError(status int, message string) (int, http.Header, []byte) {
	recorder := httptest.NewRecorder()
	app.UserErr(recorder, status, message)
	return recorder.Code, recorder.Header(), recorder.Body.Bytes()
}

func appDataOperationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, appDataOperationPrefix) {
		app.UserErr(w, http.StatusNotFound, "操作不存在")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, appDataOperationPrefix)
	if name == "" || strings.ContainsAny(name, "/\\\x00") {
		app.UserErr(w, http.StatusBadRequest, "操作名称不合法")
		return
	}
	spec, ok := appDataOperationSpecs()[name]
	if !ok || spec.handler == nil {
		app.UserErr(w, http.StatusNotFound, "操作不存在")
		return
	}
	key := requestIdempotencyKey(r)
	if spec.mutation && !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "写操作需要合法幂等键")
		return
	}
	if spec.trustedHost && r.Header.Get(trustedHostOperationHeader) != trustedHostOperationValue {
		app.UserErr(w, http.StatusForbidden, "操作只允许可信宿主")
		return
	}
	for key, values := range r.URL.Query() {
		if len(values) != 1 || spec.queryKeys == nil {
			app.UserErr(w, http.StatusBadRequest, "查询参数不合法")
			return
		}
		if _, allowed := spec.queryKeys[key]; !allowed || len(values[0]) > 1024 || strings.ContainsAny(values[0], "\x00\r\n") {
			app.UserErr(w, http.StatusBadRequest, "查询参数不合法")
			return
		}
	}

	var requestBody []byte
	inlineMutation := appDataOperationOwnsMutationTransaction(name)
	if spec.mutation {
		limited := http.MaxBytesReader(w, r.Body, maxAppDataRequestBytes)
		var err error
		requestBody, err = io.ReadAll(limited)
		if err != nil {
			app.UserErr(w, http.StatusRequestEntityTooLarge, "操作请求数据过大")
			return
		}
		adapted, adaptErr := adaptAppDataRequest(name, requestBody)
		if adaptErr != nil {
			app.UserErr(w, http.StatusBadRequest, adaptErr.Error())
			return
		}
		requestBody = adapted
		if !inlineMutation {
			fingerprint := appDataMutationFingerprint(name, requestBody, r.URL.RawQuery)
			reserved, state, status, response, err := reserveAppDataMutation(name, key, fingerprint)
			if err != nil {
				app.HTTPErr(w, err)
				return
			}
			if !reserved {
				switch state {
				case "complete":
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Zettlab-Idempotent-Replay", "true")
					w.WriteHeader(status)
					_, _ = w.Write(response)
				case "conflict":
					app.UserErr(w, http.StatusConflict, "幂等键已用于另一份请求")
				default:
					app.UserErr(w, http.StatusConflict, "同一操作结果仍待确认")
				}
				return
			}
		}
	}

	request := r.Clone(r.Context())
	request.Method = spec.method
	request.URL.Path = spec.path
	request.URL.RawPath = ""
	if spec.method == http.MethodGet {
		request.Body = http.NoBody
		request.ContentLength = 0
	} else {
		request.Body = io.NopCloser(bytes.NewReader(requestBody))
		request.ContentLength = int64(len(requestBody))
	}
	recorder := httptest.NewRecorder()
	spec.handler(recorder, request)
	raw := recorder.Body.Bytes()
	status := recorder.Code
	headers := recorder.Header()
	if status == 0 {
		status = http.StatusOK
	}
	if projected, projectErr := projectAppDataResponse(name, raw); projectErr == nil {
		raw = projected
	} else {
		status, headers, raw = boundedOperationError(http.StatusBadGateway, "操作返回数据格式错误")
	}
	if spec.mutation {
		if len(raw) > maxAppDataResponseBytes {
			status, headers, raw = boundedOperationError(http.StatusBadGateway, "操作返回数据过大")
		} else if len(raw) > maxPersistedMutationResponseBytes {
			status, headers, raw = boundedOperationError(http.StatusBadGateway, "写操作返回数据过大")
		}
		if !inlineMutation {
			fingerprint := appDataMutationFingerprint(name, requestBody, r.URL.RawQuery)
			if err := completeAppDataMutation(name, key, fingerprint, status, raw); err != nil {
				app.HTTPErr(w, err)
				return
			}
		}
	} else if len(raw) > maxAppDataResponseBytes {
		app.UserErr(w, http.StatusBadGateway, "操作返回数据过大")
		return
	}
	for header, values := range headers {
		for _, value := range values {
			w.Header().Add(header, value)
		}
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// appDataOperationRoutes is intentionally a single route registration point;
// ordinary browser endpoints keep their existing handlers and behavior.
func appDataOperationRoutes(mux *http.ServeMux) {
	mux.HandleFunc(appDataOperationPrefix, appDataOperationHandler)
}

package main

// Decision history keeps confirmed conclusions immutable and derives a
// bounded, read-only relationship graph from already-authorized Focus data.
// User notes, corrections, and reviews append records; they never rewrite the
// historical reasoning text produced by an earlier run.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	decisionHistorySchemaVersion = 2
	decisionRevisionLimit        = 12
	decisionGraphNodeLimit       = 64
	decisionGraphEdgeLimit       = 128
	decisionEventLimit           = 128
	decisionHistoryTextLimit     = 2000
	decisionHistoryResponseLimit = 176 << 10
)

var decisionHistoryMutationMu sync.Mutex

type decisionRevisionPublic struct {
	ID              string   `json:"id"`
	FocusID         string   `json:"focus_id"`
	RunID           string   `json:"run_id,omitempty"`
	DecisionID      string   `json:"decision_id"`
	RevisionNo      int      `json:"revision_no"`
	Content         string   `json:"content"`
	Status          string   `json:"status"`
	ExpectedOutcome string   `json:"expected_outcome,omitempty"`
	ReviewAt        string   `json:"review_at,omitempty"`
	Assumptions     []string `json:"assumptions"`
	Risks           []string `json:"risks"`
	SupersedesID    string   `json:"supersedes_id,omitempty"`
	ConfirmedAt     string   `json:"confirmed_at,omitempty"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type decisionGraphNodePublic struct {
	ID              string         `json:"id"`
	Type            string         `json:"type"`
	Author          string         `json:"author"`
	Title           string         `json:"title"`
	Body            string         `json:"body,omitempty"`
	SourceKind      string         `json:"source_kind,omitempty"`
	SourceID        string         `json:"source_id,omitempty"`
	SourceRevision  string         `json:"source_revision,omitempty"`
	Locator         string         `json:"locator,omitempty"`
	SourceLocator   map[string]any `json:"source_locator,omitempty"`
	EntryID         string         `json:"entry_id,omitempty"`
	BlockID         string         `json:"block_id,omitempty"`
	ContentHash     string         `json:"content_hash,omitempty"`
	TextHash        string         `json:"text_hash,omitempty"`
	EvidenceRole    string         `json:"evidence_role,omitempty"`
	Status          string         `json:"status"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedEventSeq int            `json:"created_event_seq"`
	CreatedAt       string         `json:"created_at"`
}

type decisionGraphEdgePublic struct {
	ID              string `json:"id"`
	Source          string `json:"source"`
	Target          string `json:"target"`
	Relation        string `json:"relation"`
	Label           string `json:"label,omitempty"`
	CreatedEventSeq int    `json:"created_event_seq"`
	CreatedAt       string `json:"created_at"`
}

type decisionEventPublic struct {
	ID                 string         `json:"id"`
	EventSeq           int            `json:"event_seq"`
	RunID              string         `json:"run_id,omitempty"`
	DecisionRevisionID string         `json:"decision_revision_id,omitempty"`
	Actor              string         `json:"actor"`
	Type               string         `json:"type"`
	ObjectID           string         `json:"object_id,omitempty"`
	Label              string         `json:"label"`
	Payload            map[string]any `json:"payload,omitempty"`
	CreatedAt          string         `json:"created_at"`
}

type decisionReviewPublic struct {
	ID                    string `json:"id"`
	DecisionRevisionID    string `json:"decision_revision_id"`
	Outcome               string `json:"outcome"`
	Result                string `json:"result"`
	ReplacementRevisionID string `json:"replacement_revision_id,omitempty"`
	CreatedAt             string `json:"created_at"`
}

type decisionGraphStats struct {
	EvidenceCount    int  `json:"evidence_count"`
	ConflictCount    int  `json:"conflict_count"`
	GapCount         int  `json:"gap_count"`
	ActionCount      int  `json:"action_count"`
	AlternativeCount int  `json:"alternative_count"`
	NodeCount        int  `json:"node_count"`
	EdgeCount        int  `json:"edge_count"`
	RevisionCount    int  `json:"revision_count"`
	IsComplex        bool `json:"is_complex"`
}

type decisionHistoryResponse struct {
	SchemaVersion int                       `json:"schema_version"`
	FocusID       string                    `json:"focus_id"`
	SelectedID    string                    `json:"selected_revision_id,omitempty"`
	Revisions     []decisionRevisionPublic  `json:"revisions"`
	Nodes         []decisionGraphNodePublic `json:"nodes"`
	Edges         []decisionGraphEdgePublic `json:"edges"`
	Events        []decisionEventPublic     `json:"events"`
	Reviews       []decisionReviewPublic    `json:"reviews"`
	Stats         decisionGraphStats        `json:"stats"`
}

type decisionRevisionCompareChanges struct {
	AddedNodeIDs   []string `json:"added_node_ids"`
	RemovedNodeIDs []string `json:"removed_node_ids"`
	ChangedNodeIDs []string `json:"changed_node_ids"`
	AddedEdgeIDs   []string `json:"added_edge_ids"`
	RemovedEdgeIDs []string `json:"removed_edge_ids"`
	ChangedEdgeIDs []string `json:"changed_edge_ids"`
}

type decisionRevisionCompareResponse struct {
	SchemaVersion     int                            `json:"schema_version"`
	FocusID           string                         `json:"focus_id"`
	BaseRevisionID    string                         `json:"base_revision_id"`
	CompareRevisionID string                         `json:"compare_revision_id"`
	Base              decisionHistoryResponse        `json:"base"`
	Compare           decisionHistoryResponse        `json:"compare"`
	Changes           decisionRevisionCompareChanges `json:"changes"`
}

type decisionRevisionApplyRequest struct {
	SchemaVersion          int    `json:"schema_version"`
	FocusID                string `json:"focus_id"`
	DecisionID             string `json:"decision_id"`
	RunID                  string `json:"run_id,omitempty"`
	ExpectedFocusRevision  int    `json:"expected_focus_revision"`
	ExpectedResultRevision int    `json:"expected_result_revision"`
	ExpectedRunRevision    int    `json:"expected_run_revision,omitempty"`
	ExpectedOutcome        string `json:"expected_outcome,omitempty"`
	ReviewAt               string `json:"review_at,omitempty"`
}

type decisionFeedbackApplyRequest struct {
	SchemaVersion int    `json:"schema_version"`
	FocusID       string `json:"focus_id"`
	RevisionID    string `json:"decision_revision_id"`
	Action        string `json:"action"`
	TargetNodeID  string `json:"target_node_id,omitempty"`
	MaterialID    string `json:"material_id,omitempty"`
	Content       string `json:"content"`
}

type decisionReviewApplyRequest struct {
	SchemaVersion      int    `json:"schema_version"`
	FocusID            string `json:"focus_id"`
	RevisionID         string `json:"decision_revision_id"`
	Outcome            string `json:"outcome"`
	Result             string `json:"result"`
	ReplacementContent string `json:"replacement_content,omitempty"`
}

type decisionGraphNodeDraft struct {
	decisionGraphNodePublic
	eventType  string
	eventLabel string
}

type decisionGraphEdgeDraft struct {
	decisionGraphEdgePublic
}

func decisionRevisionQueryKeys() map[string]struct{} {
	return map[string]struct{}{"focus_id": {}, "decision_id": {}, "revision_id": {}, "limit": {}}
}

func decisionRevisionCompareQueryKeys() map[string]struct{} {
	return map[string]struct{}{"focus_id": {}, "base_revision_id": {}, "compare_revision_id": {}}
}

func decisionRevisionGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	decisionID := strings.TrimSpace(r.URL.Query().Get("decision_id"))
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	if !validFocusID(focusID) || !validFocusIDOrEmpty(decisionID) || !validFocusIDOrEmpty(revisionID) {
		app.UserErr(w, http.StatusBadRequest, "决策版本查询参数不合法")
		return
	}
	limit := decisionRevisionLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > decisionRevisionLimit {
			app.UserErr(w, http.StatusBadRequest, "决策版本数量不合法")
			return
		}
		limit = value
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	response, err := decisionHistoryProjectionTx(tx, focusID, decisionID, revisionID, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "焦点或决策版本不存在")
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

func decisionRevisionCompareHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	baseRevisionID := strings.TrimSpace(r.URL.Query().Get("base_revision_id"))
	compareRevisionID := strings.TrimSpace(r.URL.Query().Get("compare_revision_id"))
	if !validFocusID(focusID) || !validFocusID(baseRevisionID) || !validFocusID(compareRevisionID) || baseRevisionID == compareRevisionID {
		app.UserErr(w, http.StatusBadRequest, "决策版本比较参数不合法")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	baseRevision, err := decisionRevisionByIDTx(tx, focusID, baseRevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "基础决策版本不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	compareRevision, err := decisionRevisionByIDTx(tx, focusID, compareRevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "对比决策版本不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if baseRevision.DecisionID != compareRevision.DecisionID {
		app.UserErr(w, http.StatusBadRequest, "只能比较同一决策的不同版本")
		return
	}
	base, err := decisionHistoryProjectionTx(tx, focusID, baseRevision.DecisionID, baseRevisionID, decisionRevisionLimit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	compare, err := decisionHistoryProjectionTx(tx, focusID, compareRevision.DecisionID, compareRevisionID, decisionRevisionLimit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	response := decisionRevisionCompareResponse{
		SchemaVersion:     decisionHistorySchemaVersion,
		FocusID:           focusID,
		BaseRevisionID:    baseRevisionID,
		CompareRevisionID: compareRevisionID,
		Base:              base,
		Compare:           compare,
		Changes:           compareDecisionGraphs(base, compare),
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, response)
}

func decisionRevisionApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request decisionRevisionApplyRequest
	if err := app.DecodeStrictJSON(w, r, 24<<10, &request); err != nil ||
		request.SchemaVersion != decisionHistorySchemaVersion || !validFocusID(request.FocusID) ||
		!validFocusID(request.DecisionID) || !validFocusID(request.RunID) ||
		request.ExpectedFocusRevision < 1 || request.ExpectedResultRevision < 1 || request.ExpectedRunRevision < 1 ||
		!validText(request.ExpectedOutcome, 2000, false) || !validReviewAt(request.ReviewAt) {
		app.UserErr(w, http.StatusBadRequest, "确认决策参数不合法")
		return
	}
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "确认决策需要合法幂等键")
		return
	}
	decisionHistoryMutationMu.Lock()
	defer decisionHistoryMutationMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	space, err := focusSpaceByIDTx(tx, request.FocusID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "焦点空间不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if space.Revision != request.ExpectedFocusRevision {
		app.UserErr(w, http.StatusConflict, "焦点已变化，请重新读取")
		return
	}
	result, err := readDecisionResultTx(tx, request.FocusID, request.DecisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "决策草案不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionID := stableObjectID("decision-revision", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.FocusID+"\x00"+request.DecisionID+"\x00"+key)
	if existing, lookupErr := decisionRevisionByIDTx(tx, request.FocusID, revisionID); lookupErr == nil {
		if existing.DecisionID != request.DecisionID || existing.RunID != request.RunID {
			app.UserErr(w, http.StatusConflict, "确认决策幂等键与已有版本不匹配")
			return
		}
		response, projectionErr := decisionHistoryProjectionTx(tx, request.FocusID, existing.DecisionID, revisionID, decisionRevisionLimit)
		if projectionErr != nil {
			app.HTTPErr(w, projectionErr)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	} else if !errors.Is(lookupErr, sql.ErrNoRows) {
		app.HTTPErr(w, lookupErr)
		return
	}
	var existingRevisionID string
	existingErr := tx.QueryRow(`SELECT id FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=? AND status='confirmed' ORDER BY revision_no DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.DecisionID).Scan(&existingRevisionID)
	if existingErr == nil {
		app.UserErr(w, http.StatusConflict, "该决策已经确认，请从已有版本继续")
		return
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		app.HTTPErr(w, existingErr)
		return
	}
	if err := bindConfirmedDecisionRunTx(tx, request.FocusID, result.AnalysisRunID, request.RunID, request.DecisionID, request.ExpectedRunRevision); err != nil {
		if errors.Is(err, errFocusSpaceRevision) {
			app.UserErr(w, http.StatusConflict, "决策运行尚未完成或已经变化，请重新读取")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if err := acceptDecisionResultTx(tx, request.FocusID, request.DecisionID, request.ExpectedResultRevision); err != nil {
		if errors.Is(err, errFocusSpaceRevision) {
			app.UserErr(w, http.StatusConflict, "决策草案已变化，请重新读取")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	resolvedExpectedOutcome := firstNonEmpty(strings.TrimSpace(request.ExpectedOutcome), result.ExpectedOutcome)
	resolvedReviewAt := firstNonEmpty(strings.TrimSpace(request.ReviewAt), result.ReviewAt)
	var revisionNo int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(revision_no),0)+1 FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.DecisionID).Scan(&revisionNo); err != nil {
		app.HTTPErr(w, err)
		return
	}
	assumptions, _ := json.Marshal(result.Assumptions)
	risks, _ := json.Marshal(result.Risks)
	if _, err := tx.Exec(`INSERT INTO focus_decision_revisions(id,owner_id,profile_id,focus_id,run_id,decision_id,revision_no,content,status,expected_outcome,review_at,assumptions_json,risks_json,confirmed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'confirmed',?,?,?,?,?,?,?)`,
		revisionID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RunID, request.DecisionID, revisionNo, result.Recommendation, resolvedExpectedOutcome, resolvedReviewAt, string(assumptions), string(risks), now, now, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	nodes, edges, actionIDs, err := buildConfirmedDecisionGraphTx(tx, request.FocusID, revisionID, request.RunID, result, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	startSeq, err := allocateDecisionEventSeqTx(tx, request.FocusID, len(nodes))
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	nodeSeq := make(map[string]int, len(nodes))
	for index := range nodes {
		nodes[index].CreatedEventSeq = startSeq + index
		nodeSeq[nodes[index].ID] = nodes[index].CreatedEventSeq
		if nodes[index].Type == "decision" {
			nodes[index].Metadata["revision_no"] = revisionNo
		}
		if err := insertDecisionEventTx(tx, request.FocusID, nodes[index].CreatedEventSeq, request.RunID, revisionID, nodes[index].Author, nodes[index].eventType, nodes[index].ID, nodes[index].eventLabel, nodes[index].Metadata, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := insertDecisionGraphNodeTx(tx, request.FocusID, revisionID, nodes[index], now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for index := range edges {
		edges[index].CreatedEventSeq = maxInt(nodeSeq[edges[index].Source], nodeSeq[edges[index].Target])
		if err := insertDecisionGraphEdgeTx(tx, request.FocusID, revisionID, edges[index], now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	for _, actionID := range actionIDs {
		if _, err := tx.Exec(`INSERT INTO focus_decision_action_links(owner_id,profile_id,focus_id,decision_revision_id,action_id,created_at) VALUES(?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, revisionID, actionID, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := setDecisionActionIDsStatusTx(tx, request.FocusID, actionIDs, "accepted"); err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionHistoryProjectionTx(tx, request.FocusID, request.DecisionID, revisionID, decisionRevisionLimit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusCreated, response)
}

func decisionFeedbackApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request decisionFeedbackApplyRequest
	if err := app.DecodeStrictJSON(w, r, 16<<10, &request); err != nil {
		return
	}
	request.FocusID = strings.TrimSpace(request.FocusID)
	request.RevisionID = strings.TrimSpace(request.RevisionID)
	request.Action = strings.TrimSpace(request.Action)
	request.TargetNodeID = strings.TrimSpace(request.TargetNodeID)
	request.MaterialID = strings.TrimSpace(request.MaterialID)
	request.Content = strings.TrimSpace(request.Content)
	if request.SchemaVersion != decisionHistorySchemaVersion ||
		!validFocusID(request.FocusID) || !validFocusID(request.RevisionID) ||
		(request.Action != "user_note" && request.Action != "correction" && request.Action != "replacement") ||
		!validFocusIDOrEmpty(request.TargetNodeID) || !validFocusIDOrEmpty(request.MaterialID) ||
		!validText(request.Content, decisionHistoryTextLimit, true) ||
		(request.Action == "correction" && request.TargetNodeID == "") ||
		(request.Action == "replacement" && request.TargetNodeID != "") {
		app.UserErr(w, http.StatusBadRequest, "决策补充参数不合法")
		return
	}
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "决策补充需要合法幂等键")
		return
	}
	decisionHistoryMutationMu.Lock()
	defer decisionHistoryMutationMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	revision, err := decisionRevisionByIDTx(tx, request.FocusID, request.RevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "决策版本不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if request.Action == "user_note" || request.Action == "correction" {
		// Notes and corrections amend the live reasoning chain only. A
		// superseded revision remains readable for history, but accepting a
		// new node against it would make the current graph diverge from the
		// decision the user is actually viewing.
		if revision.Status != "confirmed" {
			app.UserErr(w, http.StatusConflict, "当前决策版本已经变化，请重新读取")
			return
		}
		var currentRevisionID string
		currentErr := tx.QueryRow(`SELECT id
FROM focus_decision_revisions
WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=? AND status='confirmed'
ORDER BY revision_no DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, revision.DecisionID).Scan(&currentRevisionID)
		if currentErr != nil {
			if errors.Is(currentErr, sql.ErrNoRows) {
				app.UserErr(w, http.StatusConflict, "当前决策版本已经变化，请重新读取")
			} else {
				app.HTTPErr(w, currentErr)
			}
			return
		}
		if currentRevisionID != request.RevisionID {
			app.UserErr(w, http.StatusConflict, "当前决策版本已经变化，请重新读取")
			return
		}
	}
	var material *FocusMaterial
	if request.MaterialID != "" {
		item, materialErr := focusMaterialByIDTx(tx, request.FocusID, request.MaterialID)
		if materialErr != nil {
			if errors.Is(materialErr, sql.ErrNoRows) {
				app.UserErr(w, http.StatusNotFound, "引用的焦点资料不存在")
			} else {
				app.HTTPErr(w, materialErr)
			}
			return
		}
		material = &item
	}
	if request.Action == "replacement" {
		replacementID := stableObjectID("decision-revision", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.FocusID+"\x00"+request.RevisionID+"\x00"+key)
		var existingRevisionID string
		existingErr := tx.QueryRow(`SELECT id FROM focus_decision_revisions WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`, replacementID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID).Scan(&existingRevisionID)
		if existingErr == nil {
			response, projectionErr := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, replacementID, decisionRevisionLimit)
			if projectionErr != nil {
				app.HTTPErr(w, projectionErr)
				return
			}
			if err := tx.Commit(); err != nil {
				app.HTTPErr(w, err)
				return
			}
			app.WriteJSON(w, http.StatusOK, response)
			return
		}
		if !errors.Is(existingErr, sql.ErrNoRows) {
			app.HTTPErr(w, existingErr)
			return
		}
		if revision.Status != "confirmed" {
			app.UserErr(w, http.StatusConflict, "当前决策版本已经变化，请重新读取")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if err := createDecisionFeedbackReplacementTx(tx, request.FocusID, revision, replacementID, strings.TrimSpace(request.Content), material, now); err != nil {
			if errors.Is(err, errFocusSpaceRevision) {
				app.UserErr(w, http.StatusConflict, "当前决策版本已经变化，请重新读取")
			} else if strings.Contains(err.Error(), "有界上限") {
				app.UserErr(w, http.StatusConflict, err.Error())
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
		response, err := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, replacementID, decisionRevisionLimit)
		if err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusCreated, response)
		return
	}
	nodeID := stableObjectID(request.Action, appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.RevisionID+"\x00"+key)
	var existingNodeID string
	existingErr := tx.QueryRow(`SELECT id FROM focus_logic_nodes WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID, nodeID).Scan(&existingNodeID)
	if existingErr == nil {
		response, projectionErr := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, request.RevisionID, decisionRevisionLimit)
		if projectionErr != nil {
			app.HTTPErr(w, projectionErr)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		app.HTTPErr(w, existingErr)
		return
	}
	targetNodeID := strings.TrimSpace(request.TargetNodeID)
	if request.Action == "user_note" && targetNodeID == "" {
		// Chat is allowed to add context without knowing an internal node ID.
		// Anchor it to the live decision instead of creating an orphan node.
		var err error
		targetNodeID, err = decisionPrimaryNodeIDTx(tx, request.FocusID, request.RevisionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				app.UserErr(w, http.StatusConflict, "当前决策关系缺少主节点，请重新读取")
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
	}
	if targetNodeID != "" {
		var found string
		if err := tx.QueryRow(`SELECT id FROM focus_logic_nodes WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID, targetNodeID).Scan(&found); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				app.UserErr(w, http.StatusNotFound, "需要补充的关系节点不存在")
			} else {
				app.HTTPErr(w, err)
			}
			return
		}
	}
	materialNodeID := ""
	if material != nil {
		materialNodeID = stableObjectID("logic-feedback-evidence", request.RevisionID+"\x00"+nodeID+"\x00"+material.ID)
	}
	nodeDelta := 1
	edgeDelta := 0
	if targetNodeID != "" {
		edgeDelta++
	}
	if materialNodeID != "" {
		nodeDelta++
		edgeDelta++
	}
	if err := ensureDecisionGraphCapacityTx(tx, request.FocusID, request.RevisionID, nodeDelta, edgeDelta); err != nil {
		app.UserErr(w, http.StatusConflict, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seq, err := allocateDecisionEventSeqTx(tx, request.FocusID, 1)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	title := "你补充的说明"
	if request.Action == "correction" {
		title = "你标记了一处错误"
		result, updateErr := tx.Exec(`UPDATE focus_logic_nodes SET status='invalid' WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? AND id=? AND status='active'`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID, targetNodeID)
		if updateErr != nil {
			app.HTTPErr(w, updateErr)
			return
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			app.UserErr(w, http.StatusConflict, "该节点已经失效，请刷新关系图")
			return
		}
	}
	metadata := map[string]any{"target_node_id": targetNodeID}
	if material != nil {
		metadata["material_id"] = material.ID
		metadata["evidence_node_id"] = materialNodeID
	}
	node := decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: request.Action, Author: "user", Title: title, Body: request.Content, Status: "active", Metadata: metadata, CreatedEventSeq: seq}, eventType: request.Action + "_added", eventLabel: title}
	if material != nil {
		applyFocusMaterialToDecisionNode(*material, &node.decisionGraphNodePublic)
	}
	if material != nil {
		materialNode := decisionFeedbackMaterialNode(*material, request.RevisionID, materialNodeID, seq)
		if err := insertDecisionGraphNodeTx(tx, request.FocusID, request.RevisionID, materialNode, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := insertDecisionGraphNodeTx(tx, request.FocusID, request.RevisionID, node, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if materialNodeID != "" {
		edge := decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", request.RevisionID+"\x00"+materialNodeID+"\x00"+nodeID+"\x00supports"), Source: materialNodeID, Target: nodeID, Relation: "supports", Label: decisionRelationLabel("supports"), CreatedEventSeq: seq}}
		if err := insertDecisionGraphEdgeTx(tx, request.FocusID, request.RevisionID, edge, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if targetNodeID != "" {
		relation := "qualifies"
		if request.Action == "correction" {
			relation = "corrects"
		}
		edge := decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", request.RevisionID+"\x00"+nodeID+"\x00"+targetNodeID+"\x00"+relation), Source: nodeID, Target: targetNodeID, Relation: relation, Label: decisionRelationLabel(relation), CreatedEventSeq: seq}}
		if err := insertDecisionGraphEdgeTx(tx, request.FocusID, request.RevisionID, edge, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	if err := insertDecisionEventTx(tx, request.FocusID, seq, revision.RunID, request.RevisionID, "user", node.eventType, nodeID, node.eventLabel, node.Metadata, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	response, err := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, request.RevisionID, decisionRevisionLimit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusCreated, response)
}

func decisionReviewApplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request decisionReviewApplyRequest
	if err := app.DecodeStrictJSON(w, r, 24<<10, &request); err != nil || request.SchemaVersion != decisionHistorySchemaVersion ||
		!validFocusID(request.FocusID) || !validFocusID(request.RevisionID) ||
		!validText(request.Outcome, 3000, true) ||
		(request.Result != "maintain" && request.Result != "adjust" && request.Result != "overturn") ||
		!validText(request.ReplacementContent, decisionHistoryTextLimit, false) ||
		(request.Result != "maintain" && strings.TrimSpace(request.ReplacementContent) == "") {
		app.UserErr(w, http.StatusBadRequest, "决策复盘参数不合法")
		return
	}
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "决策复盘需要合法幂等键")
		return
	}
	decisionHistoryMutationMu.Lock()
	defer decisionHistoryMutationMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	revision, err := decisionRevisionByIDTx(tx, request.FocusID, request.RevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "决策版本不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	reviewID := stableObjectID("decision-review", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.RevisionID+"\x00"+key)
	var existingReplacementID string
	existingErr := tx.QueryRow(`SELECT replacement_revision_id FROM focus_decision_reviews WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=?`, reviewID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID).Scan(&existingReplacementID)
	if existingErr == nil {
		selected := request.RevisionID
		if existingReplacementID != "" {
			selected = existingReplacementID
		}
		response, projectionErr := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, selected, decisionRevisionLimit)
		if projectionErr != nil {
			app.HTTPErr(w, projectionErr)
			return
		}
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		app.WriteJSON(w, http.StatusOK, response)
		return
	}
	if !errors.Is(existingErr, sql.ErrNoRows) {
		app.HTTPErr(w, existingErr)
		return
	}
	var priorReviewID string
	priorErr := tx.QueryRow(`SELECT id FROM focus_decision_reviews WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID).Scan(&priorReviewID)
	if priorErr == nil {
		app.UserErr(w, http.StatusConflict, "该决策版本已经完成复盘")
		return
	}
	if !errors.Is(priorErr, sql.ErrNoRows) {
		app.HTTPErr(w, priorErr)
		return
	}
	if revision.Status != "confirmed" || !decisionReviewReady(revision.ReviewAt, time.Now()) {
		app.UserErr(w, http.StatusConflict, "当前决策还未到复盘时间")
		return
	}
	if err := ensureDecisionGraphCapacityTx(tx, request.FocusID, request.RevisionID, 1, 1); err != nil {
		app.UserErr(w, http.StatusConflict, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	replacementID := ""
	if request.Result != "maintain" {
		var next int
		if err := tx.QueryRow(`SELECT COALESCE(MAX(revision_no),0)+1 FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, revision.DecisionID).Scan(&next); err != nil {
			app.HTTPErr(w, err)
			return
		}
		replacementID = stableObjectID("decision-revision", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+request.FocusID+"\x00"+revision.DecisionID+"\x00"+reviewID)
		assumptions, _ := json.Marshal(revision.Assumptions)
		risks, _ := json.Marshal(revision.Risks)
		if _, err := tx.Exec(`INSERT INTO focus_decision_revisions(id,owner_id,profile_id,focus_id,run_id,decision_id,revision_no,content,status,expected_outcome,review_at,assumptions_json,risks_json,supersedes_id,confirmed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'confirmed',?,?,?,?,?,?,?,?)`, replacementID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, revision.RunID, revision.DecisionID, next, strings.TrimSpace(request.ReplacementContent), revision.ExpectedOutcome, "", string(assumptions), string(risks), revision.ID, now, now, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
		result, updateErr := tx.Exec(`UPDATE focus_decision_revisions SET status='superseded',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='confirmed'`, now, revision.ID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID)
		if updateErr != nil {
			app.HTTPErr(w, updateErr)
			return
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			app.UserErr(w, http.StatusConflict, "决策版本已经变化，请刷新后重试")
			return
		}
	}
	if _, err := tx.Exec(`INSERT INTO focus_decision_reviews(id,owner_id,profile_id,focus_id,decision_revision_id,outcome,result,replacement_revision_id,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, reviewID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, request.RevisionID, request.Outcome, request.Result, replacementID, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	eventCount := 1
	if replacementID != "" {
		eventCount = 2
	}
	seq, err := allocateDecisionEventSeqTx(tx, request.FocusID, eventCount)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	reviewNodeID := stableObjectID("review-node", reviewID)
	reviewNode := decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: reviewNodeID, Type: "review", Author: "user", Title: decisionReviewResultLabel(request.Result), Body: request.Outcome, Status: "active", Metadata: map[string]any{"result": request.Result, "replacement_revision_id": replacementID}, CreatedEventSeq: seq}, eventType: "decision_reviewed", eventLabel: decisionReviewResultLabel(request.Result)}
	if err := insertDecisionGraphNodeTx(tx, request.FocusID, request.RevisionID, reviewNode, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	decisionNodeID := stableObjectID("logic-decision", request.RevisionID)
	edge := decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", request.RevisionID+"\x00"+decisionNodeID+"\x00"+reviewNodeID), Source: decisionNodeID, Target: reviewNodeID, Relation: "produces", Label: "形成结果", CreatedEventSeq: seq}}
	if err := insertDecisionGraphEdgeTx(tx, request.FocusID, request.RevisionID, edge, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := insertDecisionEventTx(tx, request.FocusID, seq, revision.RunID, request.RevisionID, "user", "decision_reviewed", reviewID, reviewNode.eventLabel, reviewNode.Metadata, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if replacementID != "" {
		if err := cloneDecisionGraphForReplacementTx(tx, request.FocusID, revision, replacementID, strings.TrimSpace(request.ReplacementContent), request.Outcome, request.Result, seq+1, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if err := insertDecisionEventTx(tx, request.FocusID, seq+1, revision.RunID, replacementID, "user", "decision_replaced", replacementID, "形成替代决策版本", map[string]any{"supersedes_id": revision.ID, "review_id": reviewID}, now); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	selected := request.RevisionID
	if replacementID != "" {
		selected = replacementID
	}
	response, err := decisionHistoryProjectionTx(tx, request.FocusID, revision.DecisionID, selected, decisionRevisionLimit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusCreated, response)
}

type decisionResultSnapshot struct {
	ID                 string
	Question           string
	Recommendation     string
	ExpectedOutcome    string
	ReviewAt           string
	AnalysisRunID      string
	SourceIDs          []string
	SupportingEvidence []string
	Options            []string
	Assumptions        []string
	Risks              []string
	Counterevidence    []string
	ValidationItems    []string
}

func readDecisionResultTx(tx *sql.Tx, focusID, decisionID string) (decisionResultSnapshot, error) {
	var item decisionResultSnapshot
	var sourceIDs, supporting, options, assumptions, risks, counter, validation string
	err := tx.QueryRow(`SELECT d.id,d.question,d.recommendation,d.expected_outcome,d.review_at,d.analysis_run_id,d.source_ids,d.supporting_evidence,d.options,d.assumptions,d.risks,d.counterevidence,d.validation_items
FROM decisions d
JOIN focus_analysis_runs r ON r.id=d.analysis_run_id AND r.focus_id=d.focus_id
WHERE d.id=? AND d.focus_id=? AND d.analysis_run_id<>'' AND r.owner_id=? AND r.profile_id=?`, decisionID, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&item.ID, &item.Question, &item.Recommendation, &item.ExpectedOutcome, &item.ReviewAt, &item.AnalysisRunID, &sourceIDs, &supporting, &options, &assumptions, &risks, &counter, &validation)
	if err != nil {
		return item, err
	}
	fields := []struct {
		raw    string
		target *[]string
	}{
		{sourceIDs, &item.SourceIDs},
		{supporting, &item.SupportingEvidence},
		{options, &item.Options},
		{assumptions, &item.Assumptions},
		{risks, &item.Risks},
		{counter, &item.Counterevidence},
		{validation, &item.ValidationItems},
	}
	for _, field := range fields {
		if json.Unmarshal([]byte(field.raw), field.target) != nil {
			return item, errors.New("决策草案数据损坏")
		}
		if *field.target == nil {
			*field.target = []string{}
		}
	}
	item.Recommendation = strings.TrimSpace(item.Recommendation)
	if item.Recommendation == "" {
		item.Recommendation = strings.TrimSpace(item.Question)
	}
	return item, nil
}

func acceptDecisionResultTx(tx *sql.Tx, focusID, decisionID string, expected int) error {
	var revision int
	var status string
	err := tx.QueryRow(`SELECT revision,status FROM focus_result_feedback WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind='decision' AND result_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, decisionID).Scan(&revision, &status)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 1 {
			return errFocusSpaceRevision
		}
		if _, err := tx.Exec(`INSERT INTO focus_result_feedback(owner_id,profile_id,focus_id,result_kind,result_id,revision,status,patch_json,updated_at) VALUES(?,?,?,'decision',?,1,'draft','{}',?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, decisionID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		revision, status = 1, "draft"
	} else if err != nil {
		return err
	}
	if revision != expected {
		return errFocusSpaceRevision
	}
	if status == "accepted" {
		return nil
	}
	result, err := tx.Exec(`UPDATE focus_result_feedback SET revision=revision+1,status='accepted',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND result_kind='decision' AND result_id=? AND revision=?`, time.Now().UTC().Format(time.RFC3339Nano), appDataScope.OwnerID, appDataScope.ProfileID, focusID, decisionID, expected)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errFocusSpaceRevision
	}
	_, err = tx.Exec(`UPDATE decisions SET status='accepted' WHERE id=? AND focus_id=? AND analysis_run_id<>''`, decisionID, focusID)
	return err
}

func bindConfirmedDecisionRunTx(tx *sql.Tx, focusID, analysisRunID, runID, decisionID string, expected int) error {
	var storedFocusID, storedAnalysisRunID, storedDecisionID, status string
	var revision int
	if err := tx.QueryRow(`SELECT focus_id,analysis_run_id,decision_id,status,revision FROM focus_decision_runs WHERE id=? AND owner_id=? AND profile_id=?`, runID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&storedFocusID, &storedAnalysisRunID, &storedDecisionID, &status, &revision); errors.Is(err, sql.ErrNoRows) {
		return errFocusSpaceRevision
	} else if err != nil {
		return err
	}
	if expected < 1 || revision != expected || storedFocusID != focusID || storedAnalysisRunID != analysisRunID || storedDecisionID != decisionID {
		return errFocusSpaceRevision
	}
	if status != "completed" {
		return errFocusSpaceRevision
	}
	return nil
}

func buildConfirmedDecisionGraphTx(tx *sql.Tx, focusID, revisionID, runID string, result decisionResultSnapshot, now string) ([]decisionGraphNodeDraft, []decisionGraphEdgeDraft, []string, error) {
	nodes := make([]decisionGraphNodeDraft, 0, 32)
	edges := make([]decisionGraphEdgeDraft, 0, 48)
	actionIDs := make([]string, 0, 8)
	judgmentID := stableObjectID("logic-judgment", revisionID)
	decisionID := stableObjectID("logic-decision", revisionID)
	var verifiedRunID string
	if err := tx.QueryRow(`SELECT id FROM focus_analysis_runs WHERE id=? AND focus_id=? AND owner_id=? AND profile_id=?`, result.AnalysisRunID, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&verifiedRunID); err != nil {
		return nil, nil, nil, err
	}
	evidenceIndex, err := focusRunEvidenceIndexTx(tx, result.AnalysisRunID)
	if err != nil {
		return nil, nil, nil, err
	}
	citations, err := readFocusResultCitationsTx(tx, focusID, result.AnalysisRunID, "decision", result.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(citations) == 0 {
		citations = legacyDecisionCitations(result.SourceIDs, evidenceIndex)
	}
	for index, citation := range citations {
		evidence, _ := decisionEvidenceForCitation(citation, evidenceIndex)
		evidenceID := firstNonEmpty(citation.EvidenceID, evidence.ID, citation.SourceID)
		nodeID := stableObjectID("logic-evidence", revisionID+"\x00"+evidenceID+"\x00"+strconv.Itoa(index))
		title := firstNonEmpty(evidence.Title, evidenceID, "资料")
		role := focusCitationRole(citation.Role, evidence.EvidenceRole)
		locator := firstNonEmpty(citation.Locator, evidence.Locator)
		sourceLocator := evidence.SourceLocator
		if len(citation.SourceLocator) > 0 {
			sourceLocator = citation.SourceLocator
		}
		nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{
			ID: nodeID, Type: "evidence", Author: "source", Title: title,
			Body:           firstNonEmpty(citation.Quote, evidence.Quote, evidence.Excerpt),
			SourceKind:     firstNonEmpty(citation.SourceKind, evidence.SourceKind),
			SourceID:       firstNonEmpty(citation.SourceID, evidence.SourceID),
			SourceRevision: firstNonEmpty(citation.SourceRevision, evidence.SourceRevision),
			Locator:        locator,
			SourceLocator:  sourceLocator,
			EntryID:        firstNonEmpty(citation.EntryID, evidence.EntryID),
			BlockID:        firstNonEmpty(citation.BlockID, evidence.BlockID),
			ContentHash:    firstNonEmpty(citation.ContentHash, evidence.ContentHash),
			TextHash:       firstNonEmpty(citation.TextHash, evidence.TextHash),
			EvidenceRole:   role,
			Status:         "active",
			Metadata:       map[string]any{"evidence_id": evidenceID, "role": role, "source_revision": firstNonEmpty(citation.SourceRevision, evidence.SourceRevision)},
		}, eventType: "evidence_referenced", eventLabel: "引用资料：" + title})
		relation := "supports"
		if role == "challenge" {
			relation = "challenges"
		} else if role == "qualify" || role == "gap" {
			relation = "qualifies"
		}
		edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+nodeID+"\x00"+judgmentID+"\x00"+relation), Source: nodeID, Target: judgmentID, Relation: relation, Label: decisionRelationLabel(relation)}})
	}
	if runID != "" {
		rows, queryErr := tx.Query(`SELECT q.question_id,q.question,a.answer_json,q.question_type,q.options_json
FROM focus_decision_questions q
JOIN focus_decision_runs r
  ON r.id=q.run_id AND r.owner_id=q.owner_id AND r.profile_id=q.profile_id
JOIN focus_decision_answers a
  ON a.run_id=q.run_id AND a.question_id=q.question_id
 AND a.owner_id=q.owner_id AND a.profile_id=q.profile_id AND a.is_current=1
WHERE q.run_id=? AND q.owner_id=? AND q.profile_id=?
  AND r.focus_id=? AND r.analysis_run_id=?
ORDER BY q.sequence_no LIMIT ?`, runID, appDataScope.OwnerID, appDataScope.ProfileID, focusID, result.AnalysisRunID, decisionRunQuestionLimit)
		if queryErr != nil {
			return nil, nil, nil, queryErr
		}
		for rows.Next() {
			var questionID, question, answer, questionType, optionsJSON string
			if err := rows.Scan(&questionID, &question, &answer, &questionType, &optionsJSON); err != nil {
				rows.Close()
				return nil, nil, nil, err
			}
			nodeID := stableObjectID("logic-answer", revisionID+"\x00"+questionID)
			nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: "user_answer", Author: "user", Title: question, Body: decisionAnswerDisplay(answer, questionType, optionsJSON), Status: "active", Metadata: map[string]any{"question_id": questionID}}, eventType: "clarification_answered", eventLabel: "你回答了：" + question})
			edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+nodeID+"\x00"+judgmentID), Source: nodeID, Target: judgmentID, Relation: "answers", Label: "回答"}})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, nil, nil, err
		}
	}
	for index, value := range result.Counterevidence {
		nodeID := stableObjectID("logic-conflict", revisionID+"\x00"+strconv.Itoa(index))
		nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: "conflict", Author: "ai", Title: "冲突依据", Body: value, Status: "active", Metadata: map[string]any{}}, eventType: "conflict_recorded", eventLabel: "记录冲突依据"})
		edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+nodeID+"\x00"+judgmentID), Source: nodeID, Target: judgmentID, Relation: "challenges", Label: "挑战"}})
	}
	for index, value := range result.ValidationItems {
		nodeID := stableObjectID("logic-gap", revisionID+"\x00"+strconv.Itoa(index))
		nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: "gap", Author: "ai", Title: "待验证项", Body: value, Status: "active", Metadata: map[string]any{}}, eventType: "gap_recorded", eventLabel: "记录待验证项"})
		edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+nodeID+"\x00"+decisionID), Source: nodeID, Target: decisionID, Relation: "qualifies", Label: "限定"}})
	}
	for index, value := range rejectedDecisionOptions(result.Options, result.Recommendation) {
		nodeID := stableObjectID("logic-alternative", revisionID+"\x00"+strconv.Itoa(index)+"\x00"+value)
		nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: "alternative", Author: "ai", Title: "未采用方案", Body: value, Status: "active", Metadata: map[string]any{"selected": false}}, eventType: "alternative_recorded", eventLabel: "记录未采用方案"})
		edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+nodeID+"\x00"+decisionID), Source: nodeID, Target: decisionID, Relation: "challenges", Label: "未采用"}})
	}
	nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: judgmentID, Type: "ai_judgment", Author: "ai", Title: "AI 判断", Body: result.Recommendation, Status: "active", Metadata: map[string]any{"assumptions": result.Assumptions, "risks": result.Risks}}, eventType: "ai_judgment_recorded", eventLabel: "AI 形成判断"})
	nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: decisionID, Type: "decision", Author: "user", Title: "已确认决策", Body: result.Recommendation, Status: "active", Metadata: map[string]any{"decision_id": result.ID}}, eventType: "decision_confirmed", eventLabel: "确认决策：" + result.Recommendation})
	edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+judgmentID+"\x00"+decisionID), Source: judgmentID, Target: decisionID, Relation: "produces", Label: "产生"}})
	rows, err := tx.Query(`SELECT id,task,status,due_at,source_ids,supporting_evidence FROM actions WHERE focus_id=? AND analysis_run_id=? ORDER BY result_rank,id LIMIT ?`, focusID, result.AnalysisRunID, focusAnalysisLimit)
	if err != nil {
		return nil, nil, nil, err
	}
	for rows.Next() {
		var actionID, task, status, dueAt, sourceIDsJSON, supportingJSON string
		if err := rows.Scan(&actionID, &task, &status, &dueAt, &sourceIDsJSON, &supportingJSON); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		var actionSourceIDs, actionSupporting []string
		if json.Unmarshal([]byte(sourceIDsJSON), &actionSourceIDs) != nil || json.Unmarshal([]byte(supportingJSON), &actionSupporting) != nil {
			rows.Close()
			return nil, nil, nil, errors.New("行动项来源数据损坏")
		}
		if !decisionActionRelated(result, actionSourceIDs, actionSupporting) {
			continue
		}
		nodeID := stableObjectID("logic-action", revisionID+"\x00"+actionID)
		actionIDs = append(actionIDs, actionID)
		nodes = append(nodes, decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: nodeID, Type: "action", Author: "system", Title: task, Status: "active", Metadata: map[string]any{"action_id": actionID, "status": status, "due_at": dueAt}}, eventType: "action_linked", eventLabel: "生成行动：" + task})
		edges = append(edges, decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", revisionID+"\x00"+decisionID+"\x00"+nodeID), Source: decisionID, Target: nodeID, Relation: "produces", Label: "产生"}})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, nil, nil, err
	}
	if len(nodes) > decisionGraphNodeLimit || len(edges) > decisionGraphEdgeLimit {
		return nil, nil, nil, errors.New("决策关系超过当前设备的有界上限")
	}
	return nodes, edges, actionIDs, nil
}

func legacyDecisionCitations(sourceIDs []string, index focusRunEvidenceIndex) []focusResultCitation {
	result := make([]focusResultCitation, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		var evidence focusEvidenceEnvelope
		var found bool
		if evidence, found = index.byID[sourceID]; !found {
			matches := index.bySource[sourceID]
			if len(matches) == 1 {
				evidence, found = matches[0], true
			}
		}
		if !found || evidence.ID == "" {
			continue
		}
		if _, exists := seen[evidence.ID]; exists {
			continue
		}
		seen[evidence.ID] = struct{}{}
		result = append(result, focusCitationFromEvidence(evidence, evidence.EvidenceRole))
	}
	return result
}

func decisionEvidenceForCitation(citation focusResultCitation, index focusRunEvidenceIndex) (focusEvidenceEnvelope, bool) {
	if citation.EvidenceID != "" {
		if evidence, found := index.byID[citation.EvidenceID]; found {
			return evidence, true
		}
	}
	if citation.SourceID != "" {
		if evidence, found := index.byID[citation.SourceID]; found {
			return evidence, true
		}
		if matches := index.bySource[citation.SourceID]; len(matches) == 1 {
			return matches[0], true
		}
	}
	return focusEvidenceEnvelope{}, false
}

func rejectedDecisionOptions(options []string, recommendation string) []string {
	normalize := func(value string) string {
		return strings.ToLower(strings.Join(strings.Fields(value), " "))
	}
	recommendation = normalize(recommendation)
	result := make([]string, 0, len(options))
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		normalized := normalize(option)
		if normalized == "" || normalized == recommendation || strings.Contains(recommendation, normalized) {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, option)
	}
	return result
}

func decisionActionRelated(result decisionResultSnapshot, sourceIDs, supporting []string) bool {
	if len(result.SourceIDs) == 0 && len(result.SupportingEvidence) == 0 {
		return true
	}
	return stringListsIntersect(result.SourceIDs, sourceIDs) || stringListsIntersect(result.SupportingEvidence, supporting)
}

func decisionRelatedActionIDsTx(tx *sql.Tx, focusID string, result decisionResultSnapshot) ([]string, error) {
	rows, err := tx.Query(`SELECT id,source_ids,supporting_evidence FROM actions WHERE focus_id=? AND analysis_run_id=? ORDER BY result_rank,id LIMIT ?`, focusID, result.AnalysisRunID, focusAnalysisLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actionIDs := make([]string, 0, 8)
	for rows.Next() {
		var actionID, sourceIDsJSON, supportingJSON string
		if err := rows.Scan(&actionID, &sourceIDsJSON, &supportingJSON); err != nil {
			return nil, err
		}
		var sourceIDs, supporting []string
		if json.Unmarshal([]byte(sourceIDsJSON), &sourceIDs) != nil || json.Unmarshal([]byte(supportingJSON), &supporting) != nil {
			return nil, errors.New("行动项来源数据损坏")
		}
		if decisionActionRelated(result, sourceIDs, supporting) {
			actionIDs = append(actionIDs, actionID)
		}
	}
	return actionIDs, rows.Err()
}

func setDecisionActionIDsStatusTx(tx *sql.Tx, focusID string, actionIDs []string, status string) error {
	if status != "draft" && status != "accepted" && status != "rejected" {
		return errors.New("关联行动项状态不合法")
	}
	for _, actionID := range actionIDs {
		result, err := tx.Exec(`UPDATE actions SET status=?,confirmation_status=? WHERE id=? AND focus_id=? AND analysis_run_id<>'' AND status<>'done'`, status, status, actionID, focusID)
		if err != nil {
			return err
		}
		if changed, err := result.RowsAffected(); err != nil {
			return err
		} else if changed > 1 {
			return errors.New("关联行动项状态更新冲突")
		}
	}
	return nil
}

func setDecisionRelatedActionsStatusTx(tx *sql.Tx, focusID string, result decisionResultSnapshot, status string) error {
	actionIDs, err := decisionRelatedActionIDsTx(tx, focusID, result)
	if err != nil {
		return err
	}
	return setDecisionActionIDsStatusTx(tx, focusID, actionIDs, status)
}

func stringListsIntersect(left, right []string) bool {
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = struct{}{}
		}
	}
	for _, value := range right {
		if _, ok := seen[strings.TrimSpace(value)]; ok {
			return true
		}
	}
	return false
}

func compareDecisionGraphs(base, compare decisionHistoryResponse) decisionRevisionCompareChanges {
	changes := decisionRevisionCompareChanges{
		AddedNodeIDs: []string{}, RemovedNodeIDs: []string{}, ChangedNodeIDs: []string{},
		AddedEdgeIDs: []string{}, RemovedEdgeIDs: []string{}, ChangedEdgeIDs: []string{},
	}
	baseNodes := make(map[string]decisionGraphNodePublic, len(base.Nodes))
	compareNodes := make(map[string]decisionGraphNodePublic, len(compare.Nodes))
	for _, node := range base.Nodes {
		baseNodes[node.ID] = node
	}
	for _, node := range compare.Nodes {
		compareNodes[node.ID] = node
		previous, found := baseNodes[node.ID]
		if !found {
			changes.AddedNodeIDs = append(changes.AddedNodeIDs, node.ID)
		} else if decisionGraphNodeFingerprint(previous) != decisionGraphNodeFingerprint(node) {
			changes.ChangedNodeIDs = append(changes.ChangedNodeIDs, node.ID)
		}
	}
	for _, node := range base.Nodes {
		if _, found := compareNodes[node.ID]; !found {
			changes.RemovedNodeIDs = append(changes.RemovedNodeIDs, node.ID)
		}
	}
	baseEdges := make(map[string]decisionGraphEdgePublic, len(base.Edges))
	compareEdges := make(map[string]decisionGraphEdgePublic, len(compare.Edges))
	for _, edge := range base.Edges {
		baseEdges[edge.ID] = edge
	}
	for _, edge := range compare.Edges {
		compareEdges[edge.ID] = edge
		previous, found := baseEdges[edge.ID]
		if !found {
			changes.AddedEdgeIDs = append(changes.AddedEdgeIDs, edge.ID)
		} else if decisionGraphEdgeFingerprint(previous) != decisionGraphEdgeFingerprint(edge) {
			changes.ChangedEdgeIDs = append(changes.ChangedEdgeIDs, edge.ID)
		}
	}
	for _, edge := range base.Edges {
		if _, found := compareEdges[edge.ID]; !found {
			changes.RemovedEdgeIDs = append(changes.RemovedEdgeIDs, edge.ID)
		}
	}
	return changes
}

func decisionGraphNodeFingerprint(node decisionGraphNodePublic) string {
	value := struct {
		Type           string         `json:"type"`
		Author         string         `json:"author"`
		Title          string         `json:"title"`
		Body           string         `json:"body"`
		SourceKind     string         `json:"source_kind"`
		SourceID       string         `json:"source_id"`
		SourceRevision string         `json:"source_revision"`
		Locator        string         `json:"locator"`
		SourceLocator  map[string]any `json:"source_locator"`
		EntryID        string         `json:"entry_id"`
		BlockID        string         `json:"block_id"`
		ContentHash    string         `json:"content_hash"`
		TextHash       string         `json:"text_hash"`
		EvidenceRole   string         `json:"evidence_role"`
		Status         string         `json:"status"`
		Metadata       map[string]any `json:"metadata"`
	}{node.Type, node.Author, node.Title, node.Body, node.SourceKind, node.SourceID, node.SourceRevision, node.Locator, node.SourceLocator, node.EntryID, node.BlockID, node.ContentHash, node.TextHash, node.EvidenceRole, node.Status, node.Metadata}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func decisionGraphEdgeFingerprint(edge decisionGraphEdgePublic) string {
	return edge.Source + "\x00" + edge.Target + "\x00" + edge.Relation + "\x00" + edge.Label
}

func decisionPrimaryNodeIDTx(tx *sql.Tx, focusID, revisionID string) (string, error) {
	var nodeID string
	err := tx.QueryRow(`SELECT id FROM focus_logic_nodes
WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=?
  AND node_type='decision' AND status='active'
ORDER BY created_event_seq DESC,id DESC LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID).Scan(&nodeID)
	return nodeID, err
}

func decisionFeedbackMaterialNode(material FocusMaterial, revisionID, nodeID string, eventSeq int) decisionGraphNodeDraft {
	title := firstNonEmpty(material.Title, material.Source, material.SourceID, "焦点资料")
	body := firstNonEmpty(material.SummaryText, material.SourceURL)
	if material.SourceKind == "image" {
		title = firstNonEmpty(material.Title, "图片资料")
		body = firstNonEmpty(material.MIME, "图片资料")
	}
	node := decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{
		ID: nodeID, Type: "evidence", Author: "source", Title: title, Body: body,
		EvidenceRole: "support", Status: "active", Metadata: map[string]any{
			"material_id": material.ID,
			"role":        "support",
		}, CreatedEventSeq: eventSeq,
	}, eventType: "material_referenced", eventLabel: "关联资料：" + title}
	applyFocusMaterialToDecisionNode(material, &node.decisionGraphNodePublic)
	return node
}

func ensureDecisionGraphCapacityTx(tx *sql.Tx, focusID, revisionID string, nodeDelta, edgeDelta int) error {
	var nodeCount, edgeCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_logic_nodes WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID).Scan(&nodeCount); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_logic_edges WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID).Scan(&edgeCount); err != nil {
		return err
	}
	if nodeCount+nodeDelta > decisionGraphNodeLimit || edgeCount+edgeDelta > decisionGraphEdgeLimit {
		return errors.New("决策关系已达到当前设备的有界上限")
	}
	return nil
}

func createDecisionFeedbackReplacementTx(tx *sql.Tx, focusID string, previous decisionRevisionPublic, replacementID, replacementContent string, material *FocusMaterial, now string) error {
	var nextRevision int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(revision_no),0)+1 FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, previous.DecisionID).Scan(&nextRevision); err != nil {
		return err
	}
	assumptions, _ := json.Marshal(previous.Assumptions)
	risks, _ := json.Marshal(previous.Risks)
	if _, err := tx.Exec(`INSERT INTO focus_decision_revisions(id,owner_id,profile_id,focus_id,run_id,decision_id,revision_no,content,status,expected_outcome,review_at,assumptions_json,risks_json,supersedes_id,confirmed_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,'confirmed',?,?,?,?,?,?,?,?)`, replacementID, appDataScope.OwnerID, appDataScope.ProfileID, focusID, previous.RunID, previous.DecisionID, nextRevision, replacementContent, previous.ExpectedOutcome, previous.ReviewAt, string(assumptions), string(risks), previous.ID, now, now, now); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE focus_decision_revisions SET status='superseded',updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND status='confirmed'`, now, previous.ID, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errFocusSpaceRevision
	}
	seq, err := allocateDecisionEventSeqTx(tx, focusID, 1)
	if err != nil {
		return err
	}
	metadata := map[string]any{"supersedes_id": previous.ID}
	if material != nil {
		metadata["material_id"] = material.ID
	}
	reasonNode := decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{
		ID: stableObjectID("logic-replacement", replacementID), Type: "replacement", Author: "user",
		Title: "你通过对话替换了决策", Body: replacementContent, Status: "active",
		Metadata: metadata, CreatedEventSeq: seq,
	}}
	if material != nil {
		applyFocusMaterialToDecisionNode(*material, &reasonNode.decisionGraphNodePublic)
	}
	if err := cloneDecisionGraphWithContextTx(tx, focusID, previous, replacementID, replacementContent, reasonNode, true, seq, now); err != nil {
		return err
	}
	eventPayload := map[string]any{"content": replacementContent, "supersedes_id": previous.ID}
	if material != nil {
		eventPayload["material_id"] = material.ID
		materialNodeID := stableObjectID("logic-feedback-evidence", replacementID+"\x00"+reasonNode.ID+"\x00"+material.ID)
		if err := ensureDecisionGraphCapacityTx(tx, focusID, replacementID, 1, 1); err != nil {
			return err
		}
		materialNode := decisionFeedbackMaterialNode(*material, replacementID, materialNodeID, seq)
		if err := insertDecisionGraphNodeTx(tx, focusID, replacementID, materialNode, now); err != nil {
			return err
		}
		edge := decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{
			ID:     stableObjectID("logic-edge", replacementID+"\x00"+materialNodeID+"\x00"+reasonNode.ID+"\x00supports"),
			Source: materialNodeID, Target: reasonNode.ID, Relation: "supports", Label: decisionRelationLabel("supports"), CreatedEventSeq: seq,
		}}
		if err := insertDecisionGraphEdgeTx(tx, focusID, replacementID, edge, now); err != nil {
			return err
		}
		eventPayload["evidence_node_id"] = materialNodeID
	}
	return insertDecisionEventTx(tx, focusID, seq, previous.RunID, replacementID, "user", "decision_replaced", replacementID, "通过对话形成替代决策", eventPayload, now)
}

func cloneDecisionGraphForReplacementTx(tx *sql.Tx, focusID string, previous decisionRevisionPublic, replacementID, replacementContent, reviewOutcome, reviewResult string, eventSeq int, now string) error {
	reviewNode := decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{
		ID: stableObjectID("logic-review", replacementID), Type: "review", Author: "user",
		Title: decisionReviewResultLabel(reviewResult), Body: reviewOutcome, Status: "active",
		Metadata: map[string]any{"result": reviewResult, "previous_revision_id": previous.ID}, CreatedEventSeq: eventSeq,
	}}
	return cloneDecisionGraphWithContextTx(tx, focusID, previous, replacementID, replacementContent, reviewNode, false, eventSeq, now)
}

func cloneDecisionGraphWithContextTx(tx *sql.Tx, focusID string, previous decisionRevisionPublic, replacementID, replacementContent string, contextNode decisionGraphNodeDraft, contextToDecision bool, eventSeq int, now string) error {
	oldNodes, err := readDecisionGraphNodesTx(tx, focusID, previous.ID)
	if err != nil {
		return err
	}
	oldEdges, err := readDecisionGraphEdgesTx(tx, focusID, previous.ID)
	if err != nil {
		return err
	}
	activeDecisionID := ""
	for _, node := range oldNodes {
		if node.Type == "decision" && node.Status == "active" {
			activeDecisionID = node.ID
		}
	}
	if activeDecisionID == "" {
		return errors.New("已确认决策关系缺少主节点")
	}
	mapped := make(map[string]string, len(oldNodes))
	clonedNodes := make([]decisionGraphNodeDraft, 0, len(oldNodes)+3)
	for _, node := range oldNodes {
		if node.ID == activeDecisionID || node.Type == "review" {
			continue
		}
		// Node IDs are revision-scoped by the composite primary key. Reusing the
		// stable ID for unchanged nodes lets version comparison identify the
		// actual additions and removals instead of treating the whole graph as new.
		mapped[node.ID] = node.ID
		node.CreatedEventSeq = eventSeq
		node.CreatedAt = now
		clonedNodes = append(clonedNodes, decisionGraphNodeDraft{decisionGraphNodePublic: node})
	}
	priorID := stableObjectID("logic-prior-decision", replacementID)
	decisionID := stableObjectID("logic-decision", replacementID)
	mapped[activeDecisionID] = decisionID
	contextNode.CreatedEventSeq = eventSeq
	clonedNodes = append(clonedNodes,
		decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: priorID, Type: "correction", Author: "system", Title: "上一版决策", Body: previous.Content, Status: "superseded", Metadata: map[string]any{"decision_revision_id": previous.ID}, CreatedEventSeq: eventSeq}},
		contextNode,
		decisionGraphNodeDraft{decisionGraphNodePublic: decisionGraphNodePublic{ID: decisionID, Type: "decision", Author: "user", Title: "已调整决策", Body: replacementContent, Status: "active", Metadata: map[string]any{"decision_id": previous.DecisionID, "supersedes_id": previous.ID}, CreatedEventSeq: eventSeq}},
	)
	clonedEdges := make([]decisionGraphEdgeDraft, 0, len(oldEdges)+2)
	for _, edge := range oldEdges {
		source, sourceOK := mapped[edge.Source]
		target, targetOK := mapped[edge.Target]
		if !sourceOK || !targetOK {
			continue
		}
		if source != edge.Source || target != edge.Target {
			edge.ID = stableObjectID("logic-edge-clone", replacementID+"\x00"+edge.ID)
		}
		edge.Source = source
		edge.Target = target
		edge.CreatedEventSeq = eventSeq
		edge.CreatedAt = now
		clonedEdges = append(clonedEdges, decisionGraphEdgeDraft{decisionGraphEdgePublic: edge})
	}
	contextSource, contextTarget, contextRelation, contextLabel := priorID, contextNode.ID, "produces", "触发复盘"
	if contextToDecision {
		contextSource, contextTarget, contextRelation, contextLabel = contextNode.ID, decisionID, "qualifies", "补充依据"
	}
	clonedEdges = append(clonedEdges,
		decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", replacementID+"\x00"+contextSource+"\x00"+contextTarget), Source: contextSource, Target: contextTarget, Relation: contextRelation, Label: contextLabel, CreatedEventSeq: eventSeq}},
		decisionGraphEdgeDraft{decisionGraphEdgePublic: decisionGraphEdgePublic{ID: stableObjectID("logic-edge", replacementID+"\x00"+priorID+"\x00"+decisionID), Source: priorID, Target: decisionID, Relation: "replaces", Label: "替代", CreatedEventSeq: eventSeq}},
	)
	if len(clonedNodes) > decisionGraphNodeLimit || len(clonedEdges) > decisionGraphEdgeLimit {
		return errors.New("替代版本关系超过当前设备的有界上限")
	}
	for _, node := range clonedNodes {
		if err := insertDecisionGraphNodeTx(tx, focusID, replacementID, node, now); err != nil {
			return err
		}
	}
	for _, edge := range clonedEdges {
		if err := insertDecisionGraphEdgeTx(tx, focusID, replacementID, edge, now); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT action_id FROM focus_decision_action_links WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, previous.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var actionID string
		if err := rows.Scan(&actionID); err != nil {
			rows.Close()
			return err
		}
		if _, err := tx.Exec(`INSERT INTO focus_decision_action_links(owner_id,profile_id,focus_id,decision_revision_id,action_id,created_at) VALUES(?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, replacementID, actionID, now); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

func decisionHistoryProjectionTx(tx *sql.Tx, focusID, decisionID, revisionID string, limit int) (decisionHistoryResponse, error) {
	var found string
	if err := tx.QueryRow(`SELECT id FROM focus_spaces WHERE owner_id=? AND profile_id=? AND id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&found); err != nil {
		return decisionHistoryResponse{}, err
	}
	response := decisionHistoryResponse{SchemaVersion: decisionHistorySchemaVersion, FocusID: focusID, Revisions: []decisionRevisionPublic{}, Nodes: []decisionGraphNodePublic{}, Edges: []decisionGraphEdgePublic{}, Events: []decisionEventPublic{}, Reviews: []decisionReviewPublic{}}
	query := `SELECT id,focus_id,run_id,decision_id,revision_no,content,status,expected_outcome,review_at,assumptions_json,risks_json,supersedes_id,confirmed_at,created_at,updated_at FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=?`
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID, focusID}
	if decisionID != "" {
		query += ` AND decision_id=?`
		args = append(args, decisionID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, minInt(limit, decisionRevisionLimit))
	rows, err := tx.Query(query, args...)
	if err != nil {
		return response, err
	}
	for rows.Next() {
		var item decisionRevisionPublic
		var assumptions, risks string
		if err := rows.Scan(&item.ID, &item.FocusID, &item.RunID, &item.DecisionID, &item.RevisionNo, &item.Content, &item.Status, &item.ExpectedOutcome, &item.ReviewAt, &assumptions, &risks, &item.SupersedesID, &item.ConfirmedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			rows.Close()
			return response, err
		}
		if json.Unmarshal([]byte(assumptions), &item.Assumptions) != nil || json.Unmarshal([]byte(risks), &item.Risks) != nil {
			rows.Close()
			return response, errors.New("决策版本数据损坏")
		}
		response.Revisions = append(response.Revisions, item)
	}
	if err := rows.Close(); err != nil {
		return response, err
	}
	selected := revisionID
	if selected == "" && len(response.Revisions) > 0 {
		selected = response.Revisions[0].ID
	}
	if selected == "" {
		return response, nil
	}
	selectedRevision, err := decisionRevisionByIDTx(tx, focusID, selected)
	if err != nil {
		return response, err
	}
	if decisionID != "" && selectedRevision.DecisionID != decisionID {
		return response, sql.ErrNoRows
	}
	selectedListed := false
	for _, item := range response.Revisions {
		if item.ID == selectedRevision.ID {
			selectedListed = true
			break
		}
	}
	if !selectedListed {
		response.Revisions = append([]decisionRevisionPublic{selectedRevision}, response.Revisions...)
		if len(response.Revisions) > minInt(limit, decisionRevisionLimit) {
			response.Revisions = response.Revisions[:minInt(limit, decisionRevisionLimit)]
		}
	}
	response.SelectedID = selected
	response.Nodes, err = readDecisionGraphNodesTx(tx, focusID, selected)
	if err != nil {
		return response, err
	}
	response.Edges, err = readDecisionGraphEdgesTx(tx, focusID, selected)
	if err != nil {
		return response, err
	}
	response.Events, err = readDecisionEventsTx(tx, focusID, selectedRevision.DecisionID)
	if err != nil {
		return response, err
	}
	response.Reviews, err = readDecisionReviewsTx(tx, focusID, selectedRevision.DecisionID)
	if err != nil {
		return response, err
	}
	response.Stats = decisionGraphStats{NodeCount: len(response.Nodes), EdgeCount: len(response.Edges)}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_decision_revisions WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, selectedRevision.DecisionID).Scan(&response.Stats.RevisionCount); err != nil {
		return response, err
	}
	var optionsJSON string
	if err := tx.QueryRow(`SELECT options FROM decisions WHERE id=? AND focus_id=?`, selectedRevision.DecisionID, focusID).Scan(&optionsJSON); err == nil {
		var options []any
		if json.Unmarshal([]byte(optionsJSON), &options) == nil {
			response.Stats.AlternativeCount = len(options)
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return response, err
	}
	conflicts := make(map[string]struct{})
	nodeTypes := make(map[string]string, len(response.Nodes))
	for _, node := range response.Nodes {
		nodeTypes[node.ID] = node.Type
		switch node.Type {
		case "evidence":
			response.Stats.EvidenceCount++
		case "conflict":
			conflicts[node.ID] = struct{}{}
		case "gap":
			response.Stats.GapCount++
		case "action":
			response.Stats.ActionCount++
		}
	}
	for _, edge := range response.Edges {
		if edge.Relation == "challenges" && nodeTypes[edge.Source] != "alternative" {
			conflicts[edge.Source] = struct{}{}
		}
	}
	response.Stats.ConflictCount = len(conflicts)
	response.Stats.IsComplex = response.Stats.NodeCount >= 8 || response.Stats.ConflictCount > 0 || response.Stats.AlternativeCount >= 2 || response.Stats.RevisionCount >= 2 || selectedRevision.SupersedesID != ""
	if err := boundDecisionHistoryProjection(&response); err != nil {
		return response, err
	}
	return response, nil
}

func boundDecisionHistoryProjection(response *decisionHistoryResponse) error {
	encodedSize := func() int {
		raw, _ := json.Marshal(response)
		return len(raw)
	}
	for encodedSize() > decisionHistoryResponseLimit && len(response.Events) > 0 {
		response.Events = response.Events[1:]
	}
	for encodedSize() > decisionHistoryResponseLimit && len(response.Revisions) > 1 {
		removeAt := -1
		for index := len(response.Revisions) - 1; index >= 0; index-- {
			if response.Revisions[index].ID != response.SelectedID {
				removeAt = index
				break
			}
		}
		if removeAt < 0 {
			break
		}
		response.Revisions = append(response.Revisions[:removeAt], response.Revisions[removeAt+1:]...)
	}
	if encodedSize() > decisionHistoryResponseLimit {
		return errors.New("决策关系详情超过当前设备的有界响应上限")
	}
	return nil
}

func decisionRevisionByIDTx(tx *sql.Tx, focusID, revisionID string) (decisionRevisionPublic, error) {
	var item decisionRevisionPublic
	var assumptions, risks string
	err := tx.QueryRow(`SELECT id,focus_id,run_id,decision_id,revision_no,content,status,expected_outcome,review_at,assumptions_json,risks_json,supersedes_id,confirmed_at,created_at,updated_at FROM focus_decision_revisions WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=?`, revisionID, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&item.ID, &item.FocusID, &item.RunID, &item.DecisionID, &item.RevisionNo, &item.Content, &item.Status, &item.ExpectedOutcome, &item.ReviewAt, &assumptions, &risks, &item.SupersedesID, &item.ConfirmedAt, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if json.Unmarshal([]byte(assumptions), &item.Assumptions) != nil || json.Unmarshal([]byte(risks), &item.Risks) != nil {
		return item, errors.New("决策版本数据损坏")
	}
	return item, nil
}

func allocateDecisionEventSeqTx(tx *sql.Tx, focusID string, count int) (int, error) {
	if count < 1 || count > decisionEventLimit {
		return 0, errors.New("决策事件数量不合法")
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO focus_decision_event_counters(owner_id,profile_id,focus_id,last_seq) VALUES(?,?,?,0)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE focus_decision_event_counters SET last_seq=last_seq+? WHERE owner_id=? AND profile_id=? AND focus_id=?`, count, appDataScope.OwnerID, appDataScope.ProfileID, focusID); err != nil {
		return 0, err
	}
	var end int
	if err := tx.QueryRow(`SELECT last_seq FROM focus_decision_event_counters WHERE owner_id=? AND profile_id=? AND focus_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&end); err != nil {
		return 0, err
	}
	return end - count + 1, nil
}

func insertDecisionEventTx(tx *sql.Tx, focusID string, seq int, runID, revisionID, actor, eventType, objectID, label string, payload map[string]any, now string) error {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, _ := json.Marshal(payload)
	id := stableObjectID("decision-event", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+focusID+"\x00"+strconv.Itoa(seq))
	_, err := tx.Exec(`INSERT INTO focus_decision_events(owner_id,profile_id,focus_id,event_seq,id,run_id,decision_revision_id,actor,event_type,object_id,label,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, seq, id, runID, revisionID, actor, eventType, objectID, truncateRunes(label, 500), string(raw), now)
	return err
}

func insertDecisionGraphNodeTx(tx *sql.Tx, focusID, revisionID string, node decisionGraphNodeDraft, now string) error {
	metadata := node.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, _ := json.Marshal(metadata)
	sourceLocator, _ := json.Marshal(node.SourceLocator)
	if node.SourceLocator == nil {
		sourceLocator = []byte("{}")
	}
	_, err := tx.Exec(`INSERT INTO focus_logic_nodes(owner_id,profile_id,focus_id,decision_revision_id,id,node_type,author,title,body,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,block_id,content_hash,text_hash,evidence_role,status,metadata_json,created_event_seq,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID, node.ID, node.Type, node.Author, truncateRunes(node.Title, 500), truncateRunes(node.Body, decisionHistoryTextLimit), node.SourceKind, node.SourceID, node.SourceRevision, node.Locator, string(sourceLocator), node.EntryID, node.BlockID, node.ContentHash, node.TextHash, node.EvidenceRole, firstNonEmpty(node.Status, "active"), string(raw), node.CreatedEventSeq, now)
	return err
}

func insertDecisionGraphEdgeTx(tx *sql.Tx, focusID, revisionID string, edge decisionGraphEdgeDraft, now string) error {
	_, err := tx.Exec(`INSERT INTO focus_logic_edges(owner_id,profile_id,focus_id,decision_revision_id,id,source_node_id,target_node_id,relation,label,created_event_seq,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID, edge.ID, edge.Source, edge.Target, edge.Relation, edge.Label, edge.CreatedEventSeq, now)
	return err
}

func readDecisionGraphNodesTx(tx *sql.Tx, focusID, revisionID string) ([]decisionGraphNodePublic, error) {
	rows, err := tx.Query(`SELECT id,node_type,author,title,body,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,block_id,content_hash,text_hash,evidence_role,status,metadata_json,created_event_seq,created_at FROM focus_logic_nodes WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? ORDER BY created_event_seq,id LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID, decisionGraphNodeLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]decisionGraphNodePublic, 0)
	for rows.Next() {
		var item decisionGraphNodePublic
		var metadata, sourceLocator string
		if err := rows.Scan(&item.ID, &item.Type, &item.Author, &item.Title, &item.Body, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Locator, &sourceLocator, &item.EntryID, &item.BlockID, &item.ContentHash, &item.TextHash, &item.EvidenceRole, &item.Status, &metadata, &item.CreatedEventSeq, &item.CreatedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(metadata), &item.Metadata) != nil {
			return nil, errors.New("决策关系节点数据损坏")
		}
		if json.Unmarshal([]byte(sourceLocator), &item.SourceLocator) != nil {
			return nil, errors.New("决策关系来源定位损坏")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readDecisionGraphEdgesTx(tx *sql.Tx, focusID, revisionID string) ([]decisionGraphEdgePublic, error) {
	rows, err := tx.Query(`SELECT id,source_node_id,target_node_id,relation,label,created_event_seq,created_at FROM focus_logic_edges WHERE owner_id=? AND profile_id=? AND focus_id=? AND decision_revision_id=? ORDER BY created_event_seq,id LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, revisionID, decisionGraphEdgeLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]decisionGraphEdgePublic, 0)
	for rows.Next() {
		var item decisionGraphEdgePublic
		if err := rows.Scan(&item.ID, &item.Source, &item.Target, &item.Relation, &item.Label, &item.CreatedEventSeq, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readDecisionEventsTx(tx *sql.Tx, focusID, decisionID string) ([]decisionEventPublic, error) {
	rows, err := tx.Query(`SELECT id,event_seq,run_id,decision_revision_id,actor,event_type,object_id,label,payload_json,created_at FROM (
SELECT e.id,e.event_seq,e.run_id,e.decision_revision_id,e.actor,e.event_type,e.object_id,e.label,e.payload_json,e.created_at
FROM focus_decision_events e
JOIN focus_decision_revisions r ON r.id=e.decision_revision_id AND r.owner_id=e.owner_id AND r.profile_id=e.profile_id AND r.focus_id=e.focus_id
WHERE e.owner_id=? AND e.profile_id=? AND e.focus_id=? AND r.decision_id=?
ORDER BY e.event_seq DESC LIMIT ?
) ORDER BY event_seq`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, decisionID, decisionEventLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]decisionEventPublic, 0)
	for rows.Next() {
		var item decisionEventPublic
		var payload string
		if err := rows.Scan(&item.ID, &item.EventSeq, &item.RunID, &item.DecisionRevisionID, &item.Actor, &item.Type, &item.ObjectID, &item.Label, &payload, &item.CreatedAt); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(payload), &item.Payload) != nil {
			return nil, errors.New("决策事件数据损坏")
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func readDecisionReviewsTx(tx *sql.Tx, focusID, decisionID string) ([]decisionReviewPublic, error) {
	rows, err := tx.Query(`SELECT v.id,v.decision_revision_id,v.outcome,v.result,v.replacement_revision_id,v.created_at
FROM focus_decision_reviews v
JOIN focus_decision_revisions r ON r.id=v.decision_revision_id AND r.owner_id=v.owner_id AND r.profile_id=v.profile_id AND r.focus_id=v.focus_id
WHERE v.owner_id=? AND v.profile_id=? AND v.focus_id=? AND r.decision_id=? ORDER BY v.created_at DESC LIMIT 20`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, decisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]decisionReviewPublic, 0)
	for rows.Next() {
		var item decisionReviewPublic
		if err := rows.Scan(&item.ID, &item.DecisionRevisionID, &item.Outcome, &item.Result, &item.ReplacementRevisionID, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func decisionAnswerDisplay(raw, questionType, optionsJSON string) string {
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return ""
	}
	// Answers persist stable option IDs. Both the next analysis and the
	// confirmed graph need their meaning, not an opaque ID such as "cost".
	// Text/scale answers must remain literal even if they happen to match an ID.
	var options []decisionQuestionOption
	if (questionType == "choice" || questionType == "multi") && len(optionsJSON) <= 16<<10 {
		if json.Unmarshal([]byte(optionsJSON), &options) != nil || len(options) > decisionRunOptionLimit {
			options = nil
		}
	}
	display := func(id string) string {
		for _, option := range options {
			if option.ID == id && strings.TrimSpace(option.Label) != "" {
				return option.Label
			}
		}
		return id
	}
	switch typed := value.(type) {
	case string:
		return display(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, display(fmt.Sprint(item)))
		}
		return strings.Join(parts, "、")
	default:
		return fmt.Sprint(typed)
	}
}

func decisionRelationLabel(relation string) string {
	switch relation {
	case "supports":
		return "支持"
	case "challenges":
		return "挑战"
	case "qualifies":
		return "限定"
	case "answers":
		return "回答"
	case "produces":
		return "产生"
	case "corrects":
		return "纠正"
	case "replaces":
		return "替代"
	default:
		return relation
	}
}

func validReviewAt(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func decisionReviewReady(reviewAt string, now time.Time) bool {
	if strings.TrimSpace(reviewAt) == "" {
		return false
	}
	value, err := time.Parse(time.RFC3339, reviewAt)
	return err == nil && !now.Before(value)
}

func decisionReviewResultLabel(result string) string {
	switch result {
	case "maintain":
		return "复盘结论：维持决策"
	case "adjust":
		return "复盘结论：调整决策"
	case "overturn":
		return "复盘结论：推翻决策"
	default:
		return "决策复盘"
	}
}

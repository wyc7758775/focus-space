package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	focusWorkProductLimit         = 4
	focusWorkProductContentMax    = 16 << 10
	focusWorkProductTotalMax      = 64 << 10
	focusWorkProductQueryMaxLimit = 20
)

type focusWorkProductInput struct {
	ID              string              `json:"id"`
	Type            string              `json:"type"`
	Title           string              `json:"title"`
	Summary         string              `json:"summary,omitempty"`
	ContentMarkdown string              `json:"content_markdown"`
	SourceIDs       []string            `json:"source_ids"`
	Citations       focusCitationInputs `json:"citations,omitempty"`
	ValidationItems []string            `json:"validation_items"`
}

type focusWorkProduct struct {
	ID                string                `json:"id"`
	FocusID           string                `json:"focus_id"`
	AnalysisRunID     string                `json:"analysis_run_id"`
	Type              string                `json:"type"`
	Title             string                `json:"title"`
	Summary           string                `json:"summary"`
	ContentMarkdown   string                `json:"content_markdown"`
	SourceIDs         []string              `json:"source_ids"`
	Citations         []focusResultCitation `json:"citations,omitempty"`
	ValidationItems   []string              `json:"validation_items"`
	Status            string                `json:"status"`
	Revision          int                   `json:"revision"`
	CurrentRevisionID string                `json:"current_revision_id"`
	Rank              int                   `json:"rank"`
	PromptVersion     string                `json:"prompt_version"`
	EvidenceVersion   string                `json:"evidence_version"`
	CreatedAt         string                `json:"created_at"`
	UpdatedAt         string                `json:"updated_at"`
}

type focusWorkProductFeedbackPatch struct {
	Title           *string `json:"title,omitempty"`
	Summary         *string `json:"summary,omitempty"`
	ContentMarkdown *string `json:"content_markdown,omitempty"`
}

type focusWorkProductFeedbackRequest struct {
	SchemaVersion    int                            `json:"schema_version"`
	FocusID          string                         `json:"focus_id"`
	WorkProductID    string                         `json:"work_product_id"`
	ExpectedRevision int                            `json:"expected_revision"`
	Decision         string                         `json:"decision"`
	Patch            *focusWorkProductFeedbackPatch `json:"patch,omitempty"`
}

type focusWorkProductLinkRequest struct {
	SchemaVersion    int    `json:"schema_version"`
	FocusID          string `json:"focus_id"`
	WorkProductID    string `json:"work_product_id"`
	ExpectedRevision int    `json:"expected_revision"`
	Relation         string `json:"relation"`
	TargetKind       string `json:"target_kind"`
	TargetID         string `json:"target_id"`
}

func focusWorkProductQueryKeys() map[string]struct{} {
	return map[string]struct{}{"focus_id": {}, "work_product_id": {}, "limit": {}}
}

func validateFocusWorkProducts(items []focusWorkProductInput) error {
	if len(items) > focusWorkProductLimit {
		return errors.New("产出物数量超过 4 个")
	}
	total := 0
	for _, item := range items {
		if !validFocusID(item.ID) || !validText(item.Title, 240, true) || !validText(item.Summary, 2000, false) || !validIDs(item.SourceIDs) || !validTextList(item.ValidationItems, 10, 500) {
			return errors.New("产出物字段不合法")
		}
		if _, ok := focusWorkProductTypes[item.Type]; !ok {
			return errors.New("产出物类型不合法")
		}
		contentBytes := len([]byte(item.ContentMarkdown))
		if contentBytes == 0 || contentBytes > focusWorkProductContentMax {
			return errors.New("单个产出物正文必须在 1 到 16384 字节之间")
		}
		total += contentBytes
	}
	if total > focusWorkProductTotalMax {
		return errors.New("产出物正文总量超过 65536 字节")
	}
	return nil
}

func ensureFocusWorkProductSlotTx(tx *sql.Tx, id, focusID, runID string) error {
	var storedFocus, storedRun string
	err := tx.QueryRow(`SELECT focus_id,analysis_run_id FROM focus_work_products WHERE id=? AND owner_id=? AND profile_id=?`, id, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&storedFocus, &storedRun)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if storedFocus != focusID || storedRun != runID {
		return errors.New("产出物标识与已有数据冲突")
	}
	return nil
}

func persistFocusWorkProductTx(tx *sql.Tx, run focusRunSummary, request focusAnalysisApplyRequest, item focusWorkProductInput, citations []focusResultCitation, rank int, now string) error {
	storedID := focusResultStoredID("work-product", run.ID, item.ID)
	if err := ensureFocusWorkProductSlotTx(tx, storedID, run.FocusID, run.ID); err != nil {
		return err
	}
	revisionID := stableObjectID("focus-work-product-revision", storedID+"\x001")
	sourceIDs, _ := json.Marshal(item.SourceIDs)
	validationItems, _ := json.Marshal(item.ValidationItems)
	result, err := tx.Exec(`INSERT INTO focus_work_products(id,owner_id,profile_id,focus_id,analysis_run_id,product_type,title,summary,status,revision,current_revision_id,result_rank,prompt_version,evidence_version,created_at,updated_at)
VALUES(?,?,?,?,?,?,?,?, 'draft',1,?,?,?,?,?,?)
ON CONFLICT(id) DO NOTHING`, storedID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, run.ID, item.Type, item.Title, item.Summary, revisionID, rank, request.PromptVersion, request.EvidenceSetHash, now, now)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("产出物已写入，不能覆盖不可变版本")
	}
	if _, err := tx.Exec(`INSERT INTO focus_work_product_revisions(id,owner_id,profile_id,focus_id,work_product_id,revision_no,title,summary,content_markdown,source_ids_json,validation_items_json,status,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, revisionID, appDataScope.OwnerID, appDataScope.ProfileID, run.FocusID, storedID, 1, item.Title, item.Summary, item.ContentMarkdown, string(sourceIDs), string(validationItems), "draft", now); err != nil {
		return err
	}
	return persistFocusWorkProductCitationsTx(tx, run.FocusID, storedID, revisionID, citations, now)
}

func persistFocusWorkProductCitationsTx(tx *sql.Tx, focusID, workProductID, revisionID string, citations []focusResultCitation, now string) error {
	for index, citation := range citations {
		if index >= 32 {
			return errors.New("产出物引用过多")
		}
		locatorJSON, _ := json.Marshal(citation.SourceLocator)
		role := "support"
		if citation.Role == "challenge" {
			role = "counter"
		} else if citation.Role != "support" {
			role = "context"
		}
		if _, err := tx.Exec(`INSERT INTO focus_work_product_citations(owner_id,profile_id,focus_id,work_product_id,revision_id,ordinal,evidence_id,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,asset_id,mime,content_hash,block_id,quote,text_hash,role,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, workProductID, revisionID, index+1, citation.EvidenceID, citation.SourceKind, citation.SourceID, citation.SourceRevision, citation.Locator, string(locatorJSON), citation.EntryID, citation.AssetID, citation.MIME, citation.ContentHash, citation.BlockID, citation.Quote, citation.TextHash, role, now); err != nil {
			return err
		}
	}
	return nil
}

func readFocusWorkProductCitationsTx(tx *sql.Tx, workProductID, revisionID string) ([]focusResultCitation, error) {
	rows, err := tx.Query(`SELECT evidence_id,source_kind,source_id,source_revision,locator,source_locator_json,entry_id,asset_id,mime,content_hash,block_id,quote,text_hash,role
FROM focus_work_product_citations
WHERE owner_id=? AND profile_id=? AND work_product_id=? AND revision_id=?
ORDER BY ordinal ASC`, appDataScope.OwnerID, appDataScope.ProfileID, workProductID, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultCitation, 0, 8)
	for rows.Next() {
		var item focusResultCitation
		var locatorJSON string
		if err := rows.Scan(&item.EvidenceID, &item.SourceKind, &item.SourceID, &item.SourceRevision, &item.Locator, &locatorJSON, &item.EntryID, &item.AssetID, &item.MIME, &item.ContentHash, &item.BlockID, &item.Quote, &item.TextHash, &item.Role); err != nil {
			return nil, err
		}
		if item.Role == "counter" {
			item.Role = "challenge"
		}
		_ = json.Unmarshal([]byte(locatorJSON), &item.SourceLocator)
		result = append(result, item)
	}
	return result, rows.Err()
}

func queryFocusWorkProductsTx(tx *sql.Tx, focusID, runID string, limit int) ([]focusResultProjection, error) {
	rows, err := tx.Query(`SELECT p.id,p.product_type,p.title,p.summary,r.content_markdown,r.source_ids_json,r.validation_items_json,p.status,p.revision,p.current_revision_id,p.analysis_run_id,p.result_rank,p.prompt_version,p.evidence_version,p.created_at,p.updated_at
FROM focus_work_products p
JOIN focus_work_product_revisions r ON r.id=p.current_revision_id AND r.owner_id=p.owner_id AND r.profile_id=p.profile_id
WHERE p.owner_id=? AND p.profile_id=? AND p.focus_id=? AND p.analysis_run_id=?
ORDER BY p.result_rank ASC,p.id ASC LIMIT ?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID, runID, minInt(limit, focusWorkProductQueryMaxLimit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]focusResultProjection, 0, minInt(limit, focusWorkProductLimit))
	for rows.Next() {
		var item focusWorkProduct
		var sourceJSON, validationJSON string
		if err := rows.Scan(&item.ID, &item.Type, &item.Title, &item.Summary, &item.ContentMarkdown, &sourceJSON, &validationJSON, &item.Status, &item.Revision, &item.CurrentRevisionID, &item.AnalysisRunID, &item.Rank, &item.PromptVersion, &item.EvidenceVersion, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.FocusID = focusID
		if json.Unmarshal([]byte(sourceJSON), &item.SourceIDs) != nil || json.Unmarshal([]byte(validationJSON), &item.ValidationItems) != nil {
			return nil, errors.New("产出物投影损坏")
		}
		citations, err := readFocusWorkProductCitationsTx(tx, item.ID, item.CurrentRevisionID)
		if err != nil {
			return nil, err
		}
		item.Citations = citations
		result = append(result, focusResultProjection{
			Kind: "work_product", ID: item.ID, FocusID: focusID, AnalysisRunID: item.AnalysisRunID,
			Rank: item.Rank, PromptVersion: item.PromptVersion, EvidenceVersion: item.EvidenceVersion,
			Status: item.Status, Revision: item.Revision, FeedbackStatus: item.Status,
			SourceIDs: item.SourceIDs, Citations: citations, ValidationItems: item.ValidationItems, Data: item,
		})
	}
	return result, rows.Err()
}

func focusWorkProductGetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	focusID := strings.TrimSpace(r.URL.Query().Get("focus_id"))
	workProductID := strings.TrimSpace(r.URL.Query().Get("work_product_id"))
	limit := 10
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > focusWorkProductQueryMaxLimit {
			app.UserErr(w, http.StatusBadRequest, "产出物 limit 不合法")
			return
		}
		limit = parsed
	}
	if !validFocusID(focusID) || (workProductID != "" && !validFocusID(workProductID)) {
		app.UserErr(w, http.StatusBadRequest, "产出物查询字段不合法")
		return
	}
	query := `SELECT p.analysis_run_id FROM focus_work_products p WHERE p.owner_id=? AND p.profile_id=? AND p.focus_id=?`
	args := []any{appDataScope.OwnerID, appDataScope.ProfileID, focusID}
	if workProductID != "" {
		query += ` AND p.id=?`
		args = append(args, workProductID)
	}
	query += ` ORDER BY p.updated_at DESC,p.id ASC LIMIT 1`
	var runID string
	if err := db.QueryRow(query, args...).Scan(&runID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.WriteJSON(w, http.StatusOK, map[string]any{"focus_id": focusID, "work_products": []focusResultProjection{}})
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	items, err := queryFocusWorkProductsTx(tx, focusID, runID, limit)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if workProductID != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.ID == workProductID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"focus_id": focusID, "work_products": items})
}

func focusWorkProductFeedbackHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusWorkProductFeedbackRequest
	if err := app.DecodeStrictJSON(w, r, focusWorkProductContentMax+8192, &request); err != nil || request.SchemaVersion != focusSpaceSchemaVersion || !validFocusID(request.FocusID) || !validFocusID(request.WorkProductID) || request.ExpectedRevision < 1 {
		app.UserErr(w, http.StatusBadRequest, "产出物反馈格式不合法")
		return
	}
	if request.Decision != "accepted" && request.Decision != "rejected" && request.Decision != "snoozed" && request.Decision != "edited" {
		app.UserErr(w, http.StatusBadRequest, "产出物反馈决定不合法")
		return
	}
	if request.Decision == "edited" && request.Patch == nil {
		app.UserErr(w, http.StatusBadRequest, "编辑产出物需要 patch")
		return
	}
	if request.Decision != "edited" && request.Patch != nil {
		app.UserErr(w, http.StatusBadRequest, "只有编辑产出物可以携带 patch")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	item, err := readFocusWorkProductTx(tx, request.FocusID, request.WorkProductID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "产出物不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if item.Revision != request.ExpectedRevision {
		app.UserErr(w, http.StatusConflict, "产出物已变化，请重新读取")
		return
	}
	if request.Patch != nil {
		if request.Patch.Title != nil {
			item.Title = strings.TrimSpace(*request.Patch.Title)
		}
		if request.Patch.Summary != nil {
			item.Summary = strings.TrimSpace(*request.Patch.Summary)
		}
		if request.Patch.ContentMarkdown != nil {
			item.ContentMarkdown = *request.Patch.ContentMarkdown
		}
	}
	if !validText(item.Title, 240, true) || !validText(item.Summary, 2000, false) || len([]byte(item.ContentMarkdown)) == 0 || len([]byte(item.ContentMarkdown)) > focusWorkProductContentMax {
		app.UserErr(w, http.StatusBadRequest, "产出物编辑内容不合法")
		return
	}
	newRevision := item.Revision + 1
	status := request.Decision
	now := time.Now().UTC().Format(time.RFC3339Nano)
	revisionID, err := appendFocusWorkProductRevisionTx(tx, item, newRevision, status, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	result, err := tx.Exec(`UPDATE focus_work_products SET title=?,summary=?,status=?,revision=?,current_revision_id=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND revision=?`, item.Title, item.Summary, status, newRevision, revisionID, now, item.ID, appDataScope.OwnerID, appDataScope.ProfileID, item.FocusID, request.ExpectedRevision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		app.UserErr(w, http.StatusConflict, "产出物已变化，请重新读取")
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"focus_id": item.FocusID, "work_product_id": item.ID, "revision": newRevision, "status": status})
}

func focusWorkProductLinkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusWorkProductLinkRequest
	if err := app.DecodeStrictJSON(w, r, 8<<10, &request); err != nil || request.SchemaVersion != focusSpaceSchemaVersion || !validFocusID(request.FocusID) || !validFocusID(request.WorkProductID) || !validFocusID(request.TargetID) || request.ExpectedRevision < 1 {
		app.UserErr(w, http.StatusBadRequest, "产出物关系格式不合法")
		return
	}
	if _, ok := stringSet("supports", "contradicts", "produces", "leads_to", "depends_on", "references", "supersedes")[request.Relation]; !ok {
		app.UserErr(w, http.StatusBadRequest, "产出物关系类型不合法")
		return
	}
	if _, ok := stringSet("evidence", "signal", "decision", "work_product", "action")[request.TargetKind]; !ok {
		app.UserErr(w, http.StatusBadRequest, "产出物关系目标不合法")
		return
	}
	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	item, err := readFocusWorkProductTx(tx, request.FocusID, request.WorkProductID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "产出物不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if item.Revision != request.ExpectedRevision || !focusLinkTargetExistsTx(tx, request.FocusID, request.TargetKind, request.TargetID) {
		app.UserErr(w, http.StatusConflict, "产出物或关系目标已变化")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT OR IGNORE INTO focus_result_links(owner_id,profile_id,focus_id,analysis_run_id,source_kind,source_id,relation,target_kind,target_id,created_at) VALUES(?,?,?,?, 'work_product',?,?,?,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, item.FocusID, item.AnalysisRunID, item.ID, request.Relation, request.TargetKind, request.TargetID, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	newRevision := item.Revision + 1
	revisionID, err := appendFocusWorkProductRevisionTx(tx, item, newRevision, item.Status, now)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	result, err := tx.Exec(`UPDATE focus_work_products SET revision=?,current_revision_id=?,updated_at=? WHERE id=? AND owner_id=? AND profile_id=? AND focus_id=? AND revision=?`, newRevision, revisionID, now, item.ID, appDataScope.OwnerID, appDataScope.ProfileID, item.FocusID, request.ExpectedRevision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		app.UserErr(w, http.StatusConflict, "产出物已变化，请重新读取")
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	app.WriteJSON(w, http.StatusOK, map[string]any{"focus_id": item.FocusID, "work_product_id": item.ID, "revision": newRevision, "relation": request.Relation, "target_kind": request.TargetKind, "target_id": request.TargetID})
}

func readFocusWorkProductTx(tx *sql.Tx, focusID, workProductID string) (focusWorkProduct, error) {
	var item focusWorkProduct
	var sourceJSON, validationJSON string
	err := tx.QueryRow(`SELECT p.id,p.focus_id,p.analysis_run_id,p.product_type,p.title,p.summary,r.content_markdown,r.source_ids_json,r.validation_items_json,p.status,p.revision,p.current_revision_id,p.result_rank,p.prompt_version,p.evidence_version,p.created_at,p.updated_at
FROM focus_work_products p JOIN focus_work_product_revisions r ON r.id=p.current_revision_id AND r.owner_id=p.owner_id AND r.profile_id=p.profile_id
WHERE p.id=? AND p.focus_id=? AND p.owner_id=? AND p.profile_id=?`, workProductID, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&item.ID, &item.FocusID, &item.AnalysisRunID, &item.Type, &item.Title, &item.Summary, &item.ContentMarkdown, &sourceJSON, &validationJSON, &item.Status, &item.Revision, &item.CurrentRevisionID, &item.Rank, &item.PromptVersion, &item.EvidenceVersion, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return focusWorkProduct{}, err
	}
	if json.Unmarshal([]byte(sourceJSON), &item.SourceIDs) != nil || json.Unmarshal([]byte(validationJSON), &item.ValidationItems) != nil {
		return focusWorkProduct{}, errors.New("产出物版本损坏")
	}
	item.Citations, err = readFocusWorkProductCitationsTx(tx, item.ID, item.CurrentRevisionID)
	return item, err
}

func appendFocusWorkProductRevisionTx(tx *sql.Tx, item focusWorkProduct, revision int, status, now string) (string, error) {
	revisionID := stableObjectID("focus-work-product-revision", item.ID+"\x00"+fmt.Sprint(revision))
	sourceIDs, _ := json.Marshal(item.SourceIDs)
	validationItems, _ := json.Marshal(item.ValidationItems)
	if _, err := tx.Exec(`INSERT INTO focus_work_product_revisions(id,owner_id,profile_id,focus_id,work_product_id,revision_no,title,summary,content_markdown,source_ids_json,validation_items_json,status,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, revisionID, appDataScope.OwnerID, appDataScope.ProfileID, item.FocusID, item.ID, revision, item.Title, item.Summary, item.ContentMarkdown, string(sourceIDs), string(validationItems), status, now); err != nil {
		return "", err
	}
	if err := persistFocusWorkProductCitationsTx(tx, item.FocusID, item.ID, revisionID, item.Citations, now); err != nil {
		return "", err
	}
	return revisionID, nil
}

func focusLinkTargetExistsTx(tx *sql.Tx, focusID, kind, id string) bool {
	var found string
	var err error
	switch kind {
	case "evidence":
		err = tx.QueryRow(`SELECT evidence_id FROM focus_analysis_evidence WHERE owner_id=? AND profile_id=? AND evidence_id=? AND run_id IN (SELECT id FROM focus_analysis_runs WHERE owner_id=? AND profile_id=? AND focus_id=?) LIMIT 1`, appDataScope.OwnerID, appDataScope.ProfileID, id, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&found)
	case "signal":
		err = tx.QueryRow(`SELECT id FROM signals WHERE id=? AND focus_id=?`, id, focusID).Scan(&found)
	case "decision":
		err = tx.QueryRow(`SELECT id FROM decisions WHERE id=? AND focus_id=?`, id, focusID).Scan(&found)
	case "work_product":
		err = tx.QueryRow(`SELECT id FROM focus_work_products WHERE id=? AND focus_id=? AND owner_id=? AND profile_id=?`, id, focusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&found)
	case "action":
		err = tx.QueryRow(`SELECT id FROM actions WHERE id=? AND focus_id=?`, id, focusID).Scan(&found)
	}
	return err == nil && found == id
}

package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"focus-space/internal/app"
	"focus-space/internal/bridges"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	focusMaterialLimit             = 64
	focusChatImageLimit            = 4
	focusChatImageMaxBytes         = 10 << 20
	focusChatMediaListMaxBytes     = 16 << 10
	focusMaterialReceiptLimit      = 128
	focusMaterialReceiptDays       = 7
	focusAssetDirectory            = "data/focus-assets"
	focusAttachmentRequestMaxBytes = 8 << 10
	trustedExecutionTokenHeader    = "X-Zettlab-Trusted-Execution-Token"
	trustedTurnIDHeader            = "X-Zettlab-Trusted-Turn-Id"
	trustedSessionIDHeader         = "X-Zettlab-Trusted-Session-Id"
	trustedSessionKeyHeader        = "X-Zettlab-Trusted-Session-Key"
	businessExecutionTokenHeader   = "X-Zettlab-Business-Execution-Token"
	hermesTurnIDHeader             = "X-Hermes-Turn-Id"
	hermesSessionIDHeader          = "X-Hermes-Session-Id"
	hermesSessionKeyHeader         = "X-Hermes-Session-Key"
)

var focusMaterialImportMu sync.Mutex

var errFocusMaterialIdempotencyConflict = errors.New("幂等键已用于另一组对话图片")

type FocusMaterial struct {
	ID            string `json:"id"`
	FocusID       string `json:"focus_id"`
	SourceKind    string `json:"source_kind"`
	SourceID      string `json:"source_id"`
	EntryID       string `json:"entry_id,omitempty"`
	AssetID       string `json:"asset_id,omitempty"`
	Title         string `json:"title,omitempty"`
	Source        string `json:"source,omitempty"`
	SourceURL     string `json:"source_url,omitempty"`
	SummaryText   string `json:"summary_text,omitempty"`
	ContentHash   string `json:"content_hash,omitempty"`
	MIME          string `json:"mime,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Enabled       bool   `json:"enabled"`
	Revision      int    `json:"revision"`
	FocusRevision int    `json:"focus_revision"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

type focusAttachmentImportRequest struct {
	FocusID          string `json:"focus_id"`
	ExpectedRevision int    `json:"expected_revision"`
}

type focusChatExecutionScope struct {
	ExecutionToken string
	TurnID         string
	SessionID      string
	SessionKey     string
}

type focusAttachmentImportResponse struct {
	Focus    FocusSpace      `json:"focus"`
	Imported []FocusMaterial `json:"imported"`
}

type focusChatMediaItem struct {
	Index  int    `json:"index"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type downloadedFocusImage struct {
	Item         focusChatMediaItem
	MaterialID   string
	RelativePath string
	Temporary    string
	Final        string
	FinalWasNew  bool
}

func listFocusMaterials(focusID string) ([]FocusMaterial, error) {
	rows, err := db.Query(focusMaterialsQuery(), appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusMaterialLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFocusMaterials(rows)
}

func listFocusMaterialsTx(tx *sql.Tx, focusID string) ([]FocusMaterial, error) {
	rows, err := tx.Query(focusMaterialsQuery(), appDataScope.OwnerID, appDataScope.ProfileID, focusID, focusMaterialLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFocusMaterials(rows)
}

func focusMaterialsQuery() string {
	return `SELECT m.id,m.focus_id,m.source_kind,m.source_id,m.entry_id,m.digest,m.mime,m.size_bytes,
m.enabled,m.revision,m.focus_revision,m.created_at,m.updated_at,
COALESCE(n.title,''),COALESCE(n.source,''),COALESCE(n.url,''),COALESCE(n.summary_text,''),COALESCE(n.content_hash,'')
FROM focus_space_materials m
LEFT JOIN news n ON n.id=m.entry_id AND m.source_kind='entry'
WHERE m.owner_id=? AND m.profile_id=? AND m.focus_id=?
ORDER BY m.enabled DESC,m.created_at DESC,m.id DESC LIMIT ?`
}

func scanFocusMaterials(rows *sql.Rows) ([]FocusMaterial, error) {
	result := make([]FocusMaterial, 0, focusMaterialLimit)
	for rows.Next() {
		var item FocusMaterial
		var digest string
		var enabled int
		if err := rows.Scan(
			&item.ID, &item.FocusID, &item.SourceKind, &item.SourceID, &item.EntryID,
			&digest, &item.MIME, &item.SizeBytes, &enabled, &item.Revision,
			&item.FocusRevision, &item.CreatedAt, &item.UpdatedAt, &item.Title,
			&item.Source, &item.SourceURL, &item.SummaryText, &item.ContentHash,
		); err != nil {
			return nil, err
		}
		item.Enabled = enabled == 1
		if item.SourceKind == "image" {
			item.AssetID = item.ID
			item.ContentHash = digest
			item.SourceURL = "/api/focus-assets?id=" + url.QueryEscape(item.ID)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func focusMaterialByIDTx(tx *sql.Tx, focusID, materialID string) (FocusMaterial, error) {
	rows, err := tx.Query(`SELECT m.id,m.focus_id,m.source_kind,m.source_id,m.entry_id,m.digest,m.mime,m.size_bytes,
m.enabled,m.revision,m.focus_revision,m.created_at,m.updated_at,
COALESCE(n.title,''),COALESCE(n.source,''),COALESCE(n.url,''),COALESCE(n.summary_text,''),COALESCE(n.content_hash,'')
FROM focus_space_materials m
LEFT JOIN news n ON n.id=m.entry_id AND m.source_kind='entry'
WHERE m.id=? AND m.owner_id=? AND m.profile_id=? AND m.focus_id=? AND m.enabled=1 LIMIT 1`,
		materialID, appDataScope.OwnerID, appDataScope.ProfileID, focusID)
	if err != nil {
		return FocusMaterial{}, err
	}
	defer rows.Close()
	materials, err := scanFocusMaterials(rows)
	if err != nil {
		return FocusMaterial{}, err
	}
	if len(materials) != 1 {
		return FocusMaterial{}, sql.ErrNoRows
	}
	return materials[0], nil
}

func applyFocusMaterialToDecisionNode(material FocusMaterial, node *decisionGraphNodePublic) {
	if node == nil {
		return
	}
	node.SourceKind = material.SourceKind
	node.SourceID = material.SourceID
	node.SourceRevision = material.ContentHash
	node.Locator = material.SourceURL
	node.EntryID = material.EntryID
	node.ContentHash = material.ContentHash
	node.SourceLocator = map[string]any{"material_id": material.ID}
	if material.SourceKind == "entry" {
		node.SourceLocator["entry_id"] = material.EntryID
		node.SourceLocator["url"] = material.SourceURL
	} else if material.SourceKind == "image" {
		node.SourceLocator["asset_id"] = material.AssetID
		node.SourceLocator["mime"] = material.MIME
		node.SourceLocator["size_bytes"] = material.SizeBytes
	}
}

func linkFocusEntryMaterialTx(tx *sql.Tx, focusID, entryID string, focusRevision int, now string) error {
	var found string
	if err := tx.QueryRow(`SELECT n.id
FROM news n
WHERE n.id=? AND n.deleted_at='' AND (
  n.source_id<>'focus-chat-links' OR EXISTS (
    SELECT 1 FROM focus_chat_link_entries l
    WHERE l.entry_id=n.id AND l.owner_id=? AND l.profile_id=?
      AND l.source_url=n.url AND l.content_hash=n.content_hash
  )
)`, entryID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: 文章不存在", errFocusSpaceInvalid)
		}
		return err
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_materials WHERE owner_id=? AND profile_id=? AND focus_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focusID).Scan(&count); err != nil {
		return err
	}
	materialID := stableObjectID("focus-entry", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+focusID+"\x00"+entryID)
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_materials WHERE id=? AND owner_id=? AND profile_id=?`, materialID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 && count >= focusMaterialLimit {
		return fmt.Errorf("%w: 焦点资料数量已达到上限", errFocusSpaceInvalid)
	}
	_, err := tx.Exec(`INSERT INTO focus_space_materials(
id,owner_id,profile_id,focus_id,source_kind,source_id,entry_id,digest,mime,size_bytes,relative_path,
source_session_id,source_turn_id,source_index,enabled,revision,focus_revision,created_at,updated_at
) VALUES(?,?,?,?, 'entry',?,?, '', '',0,'','','',0,1,1,?,?,?)
ON CONFLICT(owner_id,profile_id,focus_id,source_kind,source_id) DO UPDATE SET
enabled=1,revision=focus_space_materials.revision+1,focus_revision=excluded.focus_revision,updated_at=excluded.updated_at`,
		materialID, appDataScope.OwnerID, appDataScope.ProfileID, focusID, entryID, entryID, focusRevision, now, now)
	return err
}

func unlinkFocusMaterialTx(tx *sql.Tx, focusID, sourceKind, sourceID string, focusRevision int, now string) error {
	if sourceKind != "entry" && sourceKind != "image" {
		return fmt.Errorf("%w: 资料类型不合法", errFocusSpaceInvalid)
	}
	result, err := tx.Exec(`UPDATE focus_space_materials
SET enabled=0,revision=revision+1,focus_revision=?,updated_at=?
WHERE owner_id=? AND profile_id=? AND focus_id=? AND source_kind=? AND (source_id=? OR id=?)`,
		focusRevision, now, appDataScope.OwnerID, appDataScope.ProfileID, focusID, sourceKind, sourceID, sourceID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return fmt.Errorf("%w: 焦点资料不存在", errFocusSpaceInvalid)
	}
	return nil
}

func focusAttachmentImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	var request focusAttachmentImportRequest
	if err := app.DecodeStrictJSON(w, r, focusAttachmentRequestMaxBytes, &request); err != nil ||
		!validFocusID(strings.TrimSpace(request.FocusID)) || request.ExpectedRevision < 1 {
		app.UserErr(w, http.StatusBadRequest, "对话图片导入参数不合法")
		return
	}
	executionScope, valid := focusChatExecutionScopeFromHeaders(r.Header)
	if !valid {
		app.UserErr(w, http.StatusForbidden, "对话图片导入缺少可信回合授权")
		return
	}
	request.FocusID = strings.TrimSpace(request.FocusID)
	key := requestIdempotencyKey(r)
	if !validActionIdempotencyKey(key) {
		app.UserErr(w, http.StatusBadRequest, "对话图片导入需要合法幂等键")
		return
	}
	// The model cannot see the images attached to a turn, so its client key
	// cannot distinguish two otherwise identical imports made in different
	// turns. Bind the receipt to the host-validated turn/session before any
	// lookup or write; retries from the same turn still replay exactly.
	key = focusAttachmentReceiptKey(key, request.FocusID, executionScope)
	fingerprintRaw, _ := json.Marshal(request)
	fingerprintDigest := sha256.Sum256(fingerprintRaw)
	fingerprint := hex.EncodeToString(fingerprintDigest[:])
	if replayFocusMaterialImport(w, key, fingerprint) {
		return
	}
	focusMaterialImportMu.Lock()
	defer focusMaterialImportMu.Unlock()
	if replayFocusMaterialImport(w, key, fingerprint) {
		return
	}
	var currentRevision int
	if err := db.QueryRow(`SELECT revision FROM focus_spaces WHERE id=? AND owner_id=? AND profile_id=?`, request.FocusID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&currentRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "焦点空间不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	if currentRevision != request.ExpectedRevision {
		app.UserErr(w, http.StatusConflict, "焦点已变化，请重新读取")
		return
	}
	images, err := downloadFocusTurnImages(r.Context(), request.FocusID, executionScope)
	if err != nil {
		app.UserErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	committed := false
	defer func() { cleanupDownloadedFocusImages(images, committed) }()

	tx, err := db.Begin()
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	defer tx.Rollback()
	if replayed, replayErr := replayFocusMaterialImportTx(tx, key, fingerprint); replayErr != nil {
		if errors.Is(replayErr, errFocusMaterialIdempotencyConflict) {
			app.UserErr(w, http.StatusConflict, replayErr.Error())
		} else {
			app.HTTPErr(w, replayErr)
		}
		return
	} else if replayed != nil {
		if err := tx.Commit(); err != nil {
			app.HTTPErr(w, err)
			return
		}
		w.Header().Set("X-Zettlab-Idempotent-Replay", "true")
		writeActionWorkflowResponse(w, replayed.status, replayed.body)
		return
	}
	space, err := focusSpaceByIDTx(tx, request.FocusID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if space.Revision != request.ExpectedRevision {
		app.UserErr(w, http.StatusConflict, "焦点已变化，请重新读取")
		return
	}
	if err := pruneFocusMaterialReceiptsTx(tx); err != nil {
		app.HTTPErr(w, err)
		return
	}
	var materialCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_materials WHERE owner_id=? AND profile_id=? AND focus_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID).Scan(&materialCount); err != nil {
		app.HTTPErr(w, err)
		return
	}
	newCount := 0
	for _, image := range images {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM focus_space_materials WHERE id=? AND owner_id=? AND profile_id=?`, image.MaterialID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&exists); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if exists == 0 {
			newCount++
		}
	}
	if materialCount+newCount > focusMaterialLimit {
		app.UserErr(w, http.StatusConflict, "焦点资料数量已达到上限")
		return
	}
	nextRevision := space.Revision + 1
	for index := range images {
		image := &images[index]
		if err := persistDownloadedFocusImage(image); err != nil {
			app.HTTPErr(w, err)
			return
		}
		if _, err := tx.Exec(`INSERT INTO focus_space_materials(
id,owner_id,profile_id,focus_id,source_kind,source_id,entry_id,digest,mime,size_bytes,relative_path,
source_session_id,source_turn_id,source_index,enabled,revision,focus_revision,created_at,updated_at
) VALUES(?,?,?,?, 'image',?,'',?,?,?,?,'','',?,1,1,?,?,?)
ON CONFLICT(owner_id,profile_id,focus_id,source_kind,source_id) DO UPDATE SET
mime=excluded.mime,size_bytes=excluded.size_bytes,relative_path=excluded.relative_path,
source_session_id='',source_turn_id='',source_index=excluded.source_index,
enabled=1,revision=focus_space_materials.revision+1,focus_revision=excluded.focus_revision,updated_at=excluded.updated_at`,
			image.MaterialID, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID, image.Item.Digest,
			image.Item.Digest, image.Item.MIME, image.Item.Size, image.RelativePath, image.Item.Index,
			nextRevision, time.Now().UTC().Format(time.RFC3339Nano),
			time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			app.HTTPErr(w, err)
			return
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.Exec(`UPDATE focus_spaces SET revision=revision+1,updated_at=?
WHERE id=? AND owner_id=? AND profile_id=? AND revision=?`, now, request.FocusID, appDataScope.OwnerID, appDataScope.ProfileID, request.ExpectedRevision)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		app.UserErr(w, http.StatusConflict, "焦点已变化，请重新读取")
		return
	}
	if _, err := tx.Exec(`UPDATE focus_analysis_runs SET status='superseded',error_code='stale_focus_revision',expires_at='',updated_at=? WHERE owner_id=? AND profile_id=? AND focus_id=? AND status='claimed'`, now, appDataScope.OwnerID, appDataScope.ProfileID, request.FocusID); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := supersedeActiveDecisionRunsTx(tx, request.FocusID, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	space, err = focusSpaceByIDTx(tx, request.FocusID)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	imported := make([]FocusMaterial, 0, len(images))
	for _, material := range space.Materials {
		for _, image := range images {
			if material.ID == image.MaterialID {
				imported = append(imported, material)
				break
			}
		}
	}
	response := focusAttachmentImportResponse{Focus: space, Imported: imported}
	responseRaw, err := json.Marshal(response)
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	if _, err := tx.Exec(`INSERT INTO focus_material_import_requests(owner_id,profile_id,idempotency_key,fingerprint,response_json,response_status,created_at,updated_at) VALUES(?,?,?,?,?,200,?,?)`, appDataScope.OwnerID, appDataScope.ProfileID, key, fingerprint, string(responseRaw), now, now); err != nil {
		app.HTTPErr(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		app.HTTPErr(w, err)
		return
	}
	committed = true
	app.WriteJSON(w, http.StatusOK, response)
}

func focusAttachmentReceiptKey(clientKey, focusID string, scope focusChatExecutionScope) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(clientKey) + "\x00" + strings.TrimSpace(focusID) + "\x00" + strings.TrimSpace(scope.TurnID) + "\x00" + strings.TrimSpace(scope.SessionID) + "\x00" + strings.TrimSpace(scope.SessionKey)))
	return "focus-attachment:" + hex.EncodeToString(digest[:])
}

type focusMaterialReplay struct {
	status int
	body   []byte
}

func replayFocusMaterialImport(w http.ResponseWriter, key, fingerprint string) bool {
	var storedFingerprint, response string
	var status int
	err := db.QueryRow(`SELECT fingerprint,response_json,response_status FROM focus_material_import_requests WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, appDataScope.OwnerID, appDataScope.ProfileID, key).Scan(&storedFingerprint, &response, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		app.HTTPErr(w, err)
		return true
	}
	if storedFingerprint != fingerprint {
		app.UserErr(w, http.StatusConflict, errFocusMaterialIdempotencyConflict.Error())
		return true
	}
	w.Header().Set("X-Zettlab-Idempotent-Replay", "true")
	writeActionWorkflowResponse(w, status, []byte(response))
	return true
}

func replayFocusMaterialImportTx(tx *sql.Tx, key, fingerprint string) (*focusMaterialReplay, error) {
	var storedFingerprint, response string
	var status int
	err := tx.QueryRow(`SELECT fingerprint,response_json,response_status FROM focus_material_import_requests WHERE owner_id=? AND profile_id=? AND idempotency_key=?`, appDataScope.OwnerID, appDataScope.ProfileID, key).Scan(&storedFingerprint, &response, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedFingerprint != fingerprint {
		return nil, errFocusMaterialIdempotencyConflict
	}
	return &focusMaterialReplay{status: status, body: []byte(response)}, nil
}

func pruneFocusMaterialReceiptsTx(tx *sql.Tx) error {
	if _, err := tx.Exec(`DELETE FROM focus_material_import_requests WHERE owner_id=? AND profile_id=? AND julianday(created_at)<julianday('now',?)`, appDataScope.OwnerID, appDataScope.ProfileID, fmt.Sprintf("-%d days", focusMaterialReceiptDays)); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM focus_material_import_requests WHERE owner_id=? AND profile_id=? AND rowid IN (
SELECT rowid FROM focus_material_import_requests WHERE owner_id=? AND profile_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET ?
)`, appDataScope.OwnerID, appDataScope.ProfileID, appDataScope.OwnerID, appDataScope.ProfileID, focusMaterialReceiptLimit)
	return err
}

func validTrustedChatScope(value string, limit int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= limit && !strings.ContainsRune(value, '\x00') && !strings.ContainsAny(value, "\r\n")
}

func focusChatExecutionScopeFromHeaders(headers http.Header) (focusChatExecutionScope, bool) {
	scope := focusChatExecutionScope{
		ExecutionToken: strings.TrimSpace(headers.Get(trustedExecutionTokenHeader)),
		TurnID:         strings.TrimSpace(headers.Get(trustedTurnIDHeader)),
		SessionID:      strings.TrimSpace(headers.Get(trustedSessionIDHeader)),
		SessionKey:     strings.TrimSpace(headers.Get(trustedSessionKeyHeader)),
	}
	valid := headers.Get(trustedHostOperationHeader) == trustedHostOperationValue &&
		validTrustedChatScope(scope.ExecutionToken, 4096) &&
		validTrustedChatScope(scope.TurnID, 256) &&
		validTrustedChatScope(scope.SessionID, 1024) &&
		validTrustedChatScope(scope.SessionKey, 1024)
	return scope, valid
}

func copyFocusExecutionHeaders(target http.Header, scope focusChatExecutionScope) {
	target.Set(businessExecutionTokenHeader, scope.ExecutionToken)
	target.Set(hermesTurnIDHeader, scope.TurnID)
	target.Set(hermesSessionIDHeader, scope.SessionID)
	target.Set(hermesSessionKeyHeader, scope.SessionKey)
}

func downloadFocusTurnImages(ctx context.Context, focusID string, executionScope focusChatExecutionScope) ([]downloadedFocusImage, error) {
	bridge := bridges.NewDeviceDataBridgeFromEnv()
	if bridge == nil || bridge.Err != nil || bridge.BaseURL == nil || bridge.Client == nil {
		return nil, errors.New("当前对话图片暂时不可读取")
	}
	listURL := *bridge.BaseURL
	listURL.Path = strings.TrimRight(listURL.Path, "/") + "/chat-turn-media"
	listRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL.String(), nil)
	if err != nil {
		return nil, errors.New("当前对话图片暂时不可读取")
	}
	listRequest.Header.Set(bridges.DeviceDataTokenHeader, bridge.Token)
	copyFocusExecutionHeaders(listRequest.Header, executionScope)
	listRequest.Header.Set("Accept", "application/json")
	listResponse, err := bridge.Client.Do(listRequest)
	if err != nil {
		return nil, errors.New("当前对话图片暂时不可读取")
	}
	defer listResponse.Body.Close()
	if listResponse.StatusCode != http.StatusOK {
		return nil, errors.New("当前对话没有可导入的图片")
	}
	listBody, err := io.ReadAll(io.LimitReader(listResponse.Body, focusChatMediaListMaxBytes+1))
	if err != nil || len(listBody) > focusChatMediaListMaxBytes {
		return nil, errors.New("当前对话图片列表无效")
	}
	var payload struct {
		Items []focusChatMediaItem `json:"items"`
	}
	if json.Unmarshal(listBody, &payload) != nil || len(payload.Items) == 0 || len(payload.Items) > focusChatImageLimit {
		return nil, errors.New("当前对话没有可导入的图片")
	}
	if err := os.MkdirAll(focusAssetDirectory, 0o700); err != nil {
		return nil, errors.New("图片存储暂时不可用")
	}
	seen := make(map[string]struct{}, len(payload.Items))
	result := make([]downloadedFocusImage, 0, len(payload.Items))
	for _, item := range payload.Items {
		item.MIME = normalizeFocusImageMIME(item.MIME)
		item.Digest = strings.ToLower(strings.TrimSpace(item.Digest))
		if item.Index < 0 || item.Index >= focusChatImageLimit || item.MIME == "" || item.Size <= 0 || item.Size > focusChatImageMaxBytes || !validSHA256Digest(item.Digest) {
			cleanupDownloadedFocusImages(result, false)
			return nil, errors.New("当前对话包含无效图片")
		}
		if _, exists := seen[item.Digest]; exists {
			continue
		}
		seen[item.Digest] = struct{}{}
		image, err := downloadFocusTurnImage(ctx, bridge, focusID, item, executionScope)
		if err != nil {
			cleanupDownloadedFocusImages(result, false)
			return nil, err
		}
		result = append(result, image)
	}
	if len(result) == 0 {
		return nil, errors.New("当前对话没有可导入的图片")
	}
	return result, nil
}

func downloadFocusTurnImage(ctx context.Context, bridge *bridges.DeviceDataBridge, focusID string, item focusChatMediaItem, executionScope focusChatExecutionScope) (downloadedFocusImage, error) {
	target := *bridge.BaseURL
	target.Path = strings.TrimRight(target.Path, "/") + "/chat-turn-media/" + strconv.Itoa(item.Index)
	query := target.Query()
	query.Set("digest", item.Digest)
	target.RawQuery = query.Encode()
	downloadRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return downloadedFocusImage{}, errors.New("当前对话图片暂时不可读取")
	}
	downloadRequest.Header.Set(bridges.DeviceDataTokenHeader, bridge.Token)
	copyFocusExecutionHeaders(downloadRequest.Header, executionScope)
	downloadRequest.Header.Set("Accept", item.MIME)
	response, err := bridge.Client.Do(downloadRequest)
	if err != nil {
		return downloadedFocusImage{}, errors.New("当前对话图片暂时不可读取")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || normalizeFocusImageMIME(response.Header.Get("Content-Type")) != item.MIME {
		return downloadedFocusImage{}, errors.New("当前对话图片已经变化，请重试")
	}
	if response.ContentLength > focusChatImageMaxBytes || response.ContentLength > 0 && response.ContentLength != item.Size {
		return downloadedFocusImage{}, errors.New("当前对话图片大小不一致")
	}
	temporary, err := os.CreateTemp(focusAssetDirectory, ".focus-image-*")
	if err != nil {
		return downloadedFocusImage{}, errors.New("图片存储暂时不可用")
	}
	temporaryPath := temporary.Name()
	fail := func(message string) (downloadedFocusImage, error) {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return downloadedFocusImage{}, errors.New(message)
	}
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(response.Body, focusChatImageMaxBytes+1))
	if err != nil || written != item.Size || written > focusChatImageMaxBytes {
		return fail("当前对话图片大小不一致")
	}
	if hex.EncodeToString(hash.Sum(nil)) != item.Digest {
		return fail("当前对话图片摘要不一致")
	}
	if err := temporary.Sync(); err != nil {
		return fail("图片存储暂时不可用")
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return downloadedFocusImage{}, errors.New("图片存储暂时不可用")
	}
	materialID := stableObjectID("focus-image", appDataScope.OwnerID+"\x00"+appDataScope.ProfileID+"\x00"+focusID+"\x00"+item.Digest)
	filename := materialID + focusImageExtension(item.MIME)
	finalPath := filepath.Join(focusAssetDirectory, filename)
	_, statErr := os.Stat(finalPath)
	return downloadedFocusImage{
		Item: item, MaterialID: materialID, RelativePath: filepath.ToSlash(filepath.Join("focus-assets", filename)),
		Temporary: temporaryPath, Final: finalPath, FinalWasNew: errors.Is(statErr, os.ErrNotExist),
	}, nil
}

func persistDownloadedFocusImage(image *downloadedFocusImage) error {
	if image == nil || image.Temporary == "" || image.Final == "" {
		return errors.New("图片存储暂时不可用")
	}
	if !image.FinalWasNew {
		if focusAssetFileMatches(image.Final, image.Item.Size, image.Item.Digest) {
			_ = os.Remove(image.Temporary)
			image.Temporary = ""
			return nil
		}
		return errors.New("图片存储校验失败")
	}
	if err := os.Rename(image.Temporary, image.Final); err != nil {
		return err
	}
	image.Temporary = ""
	directory, err := os.Open(focusAssetDirectory)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func focusAssetFileMatches(path string, size int64, digest string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		return false
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, focusChatImageMaxBytes+1))
	return err == nil && written == size && hex.EncodeToString(hash.Sum(nil)) == digest
}

func cleanupDownloadedFocusImages(images []downloadedFocusImage, committed bool) {
	for _, image := range images {
		if image.Temporary != "" {
			_ = os.Remove(image.Temporary)
		}
		if !committed && image.FinalWasNew && image.Final != "" {
			_ = os.Remove(image.Final)
		}
	}
}

func normalizeFocusImageMIME(value string) string {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	parsed = strings.ToLower(parsed)
	switch parsed {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return parsed
	default:
		return ""
	}
}

func focusImageExtension(value string) string {
	switch value {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".bin"
	}
}

func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func focusAssetHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		app.UserErr(w, http.StatusMethodNotAllowed, "不支持的请求方法")
		return
	}
	assetID := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validFocusID(assetID) || len(r.URL.Query()) != 1 {
		app.UserErr(w, http.StatusBadRequest, "图片标识不合法")
		return
	}
	var relativePath, mimeType, digest string
	var size int64
	err := db.QueryRow(`SELECT relative_path,mime,size_bytes,digest FROM focus_space_materials
WHERE id=? AND owner_id=? AND profile_id=? AND source_kind='image' AND enabled=1`, assetID, appDataScope.OwnerID, appDataScope.ProfileID).Scan(&relativePath, &mimeType, &size, &digest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			app.UserErr(w, http.StatusNotFound, "图片不存在")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	root, err := filepath.Abs("data")
	if err != nil {
		app.HTTPErr(w, err)
		return
	}
	path, err := filepath.Abs(filepath.Join("data", filepath.FromSlash(relativePath)))
	if err != nil || path == root || !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		app.UserErr(w, http.StatusForbidden, "图片路径不合法")
		return
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			app.UserErr(w, http.StatusNotFound, "图片原件暂时不可用")
		} else {
			app.HTTPErr(w, err)
		}
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != size {
		app.UserErr(w, http.StatusGone, "图片原件校验失败")
		return
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.CopyN(w, file, size)
}

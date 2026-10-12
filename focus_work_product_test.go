package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestFocusWorkProductValidationBoundsUnit(t *testing.T) {
	valid := focusWorkProductInput{ID: "guide-1", Type: "travel_guide", Title: "东京五日攻略", ContentMarkdown: "# 行程", SourceIDs: []string{}, ValidationItems: []string{}}
	if err := validateFocusWorkProducts([]focusWorkProductInput{valid}); err != nil {
		t.Fatal(err)
	}
	tooLarge := valid
	tooLarge.ContentMarkdown = strings.Repeat("a", focusWorkProductContentMax+1)
	if err := validateFocusWorkProducts([]focusWorkProductInput{tooLarge}); err == nil {
		t.Fatal("oversized work product was accepted")
	}
	badType := valid
	badType.Type = "executable"
	if err := validateFocusWorkProducts([]focusWorkProductInput{badType}); err == nil {
		t.Fatal("unknown work product type was accepted")
	}
	tooMany := make([]focusWorkProductInput, focusWorkProductLimit+1)
	for i := range tooMany {
		tooMany[i] = valid
		tooMany[i].ID += string(rune('a' + i))
	}
	if err := validateFocusWorkProducts(tooMany); err == nil {
		t.Fatal("too many work products were accepted")
	}
}

func TestFocusWorkProductAnalysisFeedbackAndLinkFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-work-product", "profile-work-product")
	focus := createFocusFlowSpace(t, "规划东京旅行")
	insertFocusFlowMeeting(t, "meeting-work-product", "计划十月前往东京五天，偏好城市文化路线。", "2026-09-17T08:00:00Z")
	claim := readFocusFlowClaim(t, focus.ID)
	if claim.Status != "claimed" || len(claim.Evidence) != 1 {
		t.Fatalf("unexpected claim: %#v", claim)
	}
	request := focusFlowApplyRequest(claim, "signal-trip")
	request.WorkProducts = []focusWorkProductInput{{
		ID: "tokyo-guide", Type: "travel_guide", Title: "东京五日旅行方案", Summary: "首次访问的城市文化路线",
		ContentMarkdown: "# 东京五日旅行方案\n\n## 第一天\n浅草与上野。", SourceIDs: []string{claim.Evidence[0].ID}, ValidationItems: []string{"确认机票价格"},
	}}
	apply := callFocusFlowJSON(t, focusAnalysisApplyHandler, http.MethodPost, "/api/internal/focus-analysis/apply", request, "work-product-apply")
	if apply.Code != http.StatusOK {
		t.Fatalf("apply status=%d body=%s", apply.Code, apply.Body.String())
	}
	var projection focusAnalysisGetResponse
	if err := json.Unmarshal(apply.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection.WorkProducts) != 1 || projection.WorkProducts[0].Kind != "work_product" || projection.WorkProducts[0].Revision != 1 {
		t.Fatalf("work product missing from result bundle: %#v", projection.WorkProducts)
	}
	productID := projection.WorkProducts[0].ID
	var revisionCount, citationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_work_product_revisions WHERE owner_id=? AND profile_id=? AND work_product_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, productID).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_work_product_citations WHERE owner_id=? AND profile_id=? AND work_product_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, productID).Scan(&citationCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 || citationCount != 1 {
		t.Fatalf("revision=%d citations=%d", revisionCount, citationCount)
	}

	feedback := callFocusFlowJSON(t, focusWorkProductFeedbackHandler, http.MethodPost, "/api/internal/focus-work-products/feedback", focusWorkProductFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: productID, ExpectedRevision: 1, Decision: "accepted",
	}, "")
	if feedback.Code != http.StatusOK || !strings.Contains(feedback.Body.String(), `"revision":2`) {
		t.Fatalf("feedback status=%d body=%s", feedback.Code, feedback.Body.String())
	}
	editedBody := "# 东京五日旅行方案\n\n## 第一天\n浅草、上野与谷中。"
	feedback = callFocusFlowJSON(t, focusWorkProductFeedbackHandler, http.MethodPost, "/api/internal/focus-work-products/feedback", focusWorkProductFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: productID, ExpectedRevision: 2, Decision: "edited",
		Patch: &focusWorkProductFeedbackPatch{ContentMarkdown: &editedBody},
	}, "")
	if feedback.Code != http.StatusOK || !strings.Contains(feedback.Body.String(), `"revision":3`) {
		t.Fatalf("edit status=%d body=%s", feedback.Code, feedback.Body.String())
	}

	link := callFocusFlowJSON(t, focusWorkProductLinkHandler, http.MethodPost, "/api/internal/focus-work-products/link", focusWorkProductLinkRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: productID, ExpectedRevision: 3,
		Relation: "references", TargetKind: "signal", TargetID: projection.Signals[0].ID,
	}, "")
	if link.Code != http.StatusOK || !strings.Contains(link.Body.String(), `"revision":4`) {
		t.Fatalf("link status=%d body=%s", link.Code, link.Body.String())
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_work_product_revisions WHERE owner_id=? AND profile_id=? AND work_product_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, productID).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	var linkCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM focus_result_links WHERE owner_id=? AND profile_id=? AND focus_id=? AND source_kind='work_product' AND source_id=?`, appDataScope.OwnerID, appDataScope.ProfileID, focus.ID, productID).Scan(&linkCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 4 || linkCount != 1 {
		t.Fatalf("revision=%d links=%d", revisionCount, linkCount)
	}

	get := callFocusFlowJSON(t, focusWorkProductGetHandler, http.MethodGet, "/api/internal/focus-work-products?focus_id="+focus.ID+"&work_product_id="+productID, nil, "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "浅草、上野与谷中") || !strings.Contains(get.Body.String(), `"revision":4`) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	stale := callFocusFlowJSON(t, focusWorkProductFeedbackHandler, http.MethodPost, "/api/internal/focus-work-products/feedback", focusWorkProductFeedbackRequest{
		SchemaVersion: 1, FocusID: focus.ID, WorkProductID: productID, ExpectedRevision: 3, Decision: "rejected",
	}, "")
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale feedback status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestFocusAnalysisFailedRejectsWorkProductsUnit(t *testing.T) {
	request := focusAnalysisApplyRequest{
		SchemaVersion: 1, RunID: "run-1", FocusID: "focus-1", FocusRevision: 1,
		EvidenceSetHash: strings.Repeat("a", 64), PromptVersion: "focus-v1", Status: "failed", ErrorCode: "model_unavailable",
		WorkProducts: []focusWorkProductInput{{ID: "outline-1", Type: "outline", Title: "文章大纲", ContentMarkdown: "# 大纲"}},
	}
	if err := validateFocusAnalysisApply(request); err == nil {
		t.Fatal("failed analysis accepted a work product")
	}
}

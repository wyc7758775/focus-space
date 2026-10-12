package main

import (
	"encoding/json"
	"focus-space/internal/bridges"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCollectDeviceFocusCandidatesUsesBoundedMeetingAndObsidianEvidenceFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(bridges.DeviceDataTokenHeader) != "focus-token" {
			t.Fatalf("missing device data token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/device-data/v1/meetings":
			_ = json.NewEncoder(w).Encode(map[string]any{"meetings": []any{
				map[string]any{"id": "meeting-1", "title": "AI Computer 周会", "started_at": 1786752000, "status": "done"},
			}})
		case r.URL.Path == "/device-data/v1/meetings/meeting-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"meeting": map[string]any{
				"id": "meeting-1", "title": "AI Computer 周会", "started_at": 1786752000,
				"summary": "确认 AI Computer 的产品路线和下周行动。", "status": "done",
			}})
		case r.URL.Path == "/device-data/v1/obsidian/list":
			if r.URL.Query().Get("path") == "Projects" {
				_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{map[string]any{"type": "markdown", "name": "AI Computer.md", "path": "Projects/AI Computer.md", "modified_at": "2026-08-14T12:00:00Z"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{map[string]any{"type": "directory", "name": "Projects", "path": "Projects"}}})
		case r.URL.Path == "/device-data/v1/obsidian/read":
			_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Query().Get("path"), "content": "# AI Computer\n记录产品路线、用户反馈和下一步验证。"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", server.URL+"/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "focus-token")

	space := FocusSpace{ID: "focus-1", Goal: "AI Computer", HorizonMode: "open", Status: "active", CreatedAt: "2026-08-01T00:00:00Z"}
	candidates, statuses, err := collectDeviceFocusCandidates(space, "question", false, focusTerms(space.Goal), func(kind, id string) bool { return kind == "meeting" || kind == "obsidian" })
	if err != nil {
		t.Fatalf("collect device candidates: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count=%d, want 2", len(candidates))
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		seen[candidate.Envelope.SourceKind] = true
		if candidate.Envelope.AccessScope != "owner/profile-scoped" || len([]rune(candidate.Envelope.Excerpt)) > 2400 {
			t.Fatalf("unbounded or unscoped envelope: %+v", candidate.Envelope)
		}
	}
	if !seen["meeting"] || !seen["obsidian"] || len(statuses) != 2 {
		t.Fatalf("sources=%+v candidates=%v", statuses, seen)
	}
}

func TestCollectDeviceFocusCandidatesDegradesPerSourceUnit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/meetings") {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"down"}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/obsidian/list") {
			_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{map[string]any{"type": "markdown", "name": "note.md", "path": "note.md", "modified_at": "2026-08-14T12:00:00Z"}}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/obsidian/read") {
			_ = json.NewEncoder(w).Encode(map[string]any{"content": "AI Computer notes"})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", server.URL+"/device-data/v1")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "focus-token")

	space := FocusSpace{ID: "focus-1", Goal: "AI Computer", HorizonMode: "open", Status: "active"}
	candidates, statuses, err := collectDeviceFocusCandidates(space, "question", false, focusTerms(space.Goal), func(kind, id string) bool { return true })
	if err != nil {
		t.Fatalf("one source failure should degrade, got %v", err)
	}
	if len(candidates) != 1 || statuses[0].Status != "unavailable" || statuses[1].Status == "unavailable" {
		t.Fatalf("candidates=%v statuses=%+v", candidates, statuses)
	}
}

func TestCollectDeviceFocusCandidatesWithoutBridgeIsOptionalUnit(t *testing.T) {
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "")
	candidates, statuses, err := collectDeviceFocusCandidates(FocusSpace{Goal: "AI Computer"}, "question", false, []string{"ai"}, func(string, string) bool { return true })
	if err != nil || candidates != nil || statuses != nil {
		t.Fatalf("unconfigured bridge should be optional: candidates=%v statuses=%v err=%v", candidates, statuses, err)
	}
}

func TestFocusTermsTokenizeNaturalChineseGoalUnit(t *testing.T) {
	terms := focusTerms("跟踪 AI 芯片、模型和科技产品发布，形成可复核的优先行动")
	if len(terms) < 2 {
		t.Fatalf("focus terms=%v, want multiple searchable terms", terms)
	}
	joined := strings.Join(terms, "|")
	if !strings.Contains(joined, "ai") || !strings.Contains(joined, "芯片") {
		t.Fatalf("focus terms=%v, want ASCII and CJK terms", terms)
	}
	if got := scoreFocusText(terms, "新一代 AI 推理芯片发布", "模型吞吐与成本数据"); got <= 0 {
		t.Fatalf("scoreFocusText()=%d for a matching article, terms=%v", got, terms)
	}
}

func TestCollectFocusCandidatesPrioritizesLinkedReaderSourcesFlow(t *testing.T) {
	db = openTestDB(t)
	oldScope := appDataScope
	appDataScope = dataScope{OwnerID: "app-local", ProfileID: "app-local"}
	t.Cleanup(func() { appDataScope = oldScope })
	t.Setenv("ZAPP_DEVICE_DATA_BASE_URL", "")
	t.Setenv("ZAPP_DEVICE_DATA_TOKEN", "")
	now := time.Now().UTC()
	for _, sourceID := range []string{"noise-source", "linked-source"} {
		if _, err := db.Exec(`INSERT INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, sourceID, "rss", sourceID, "https://example.com/"+sourceID, "", 0, "ok", now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO feed_subscriptions(source_id,enabled,subscribed_at,updated_at) VALUES(?,?,?,?)`, sourceID, 1, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < focusEvidenceScanLimit; index++ {
		published := now.Add(time.Duration(index+1) * time.Minute).Format(time.RFC3339)
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,updated_at,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			"noise-entry-"+strconv.Itoa(index), "AI 噪声文章", "https://example.com/noise/"+strconv.Itoa(index), "noise-source", "noise-source", published, "AI 噪声摘要", "text", "", published, "noise-hash-"+strconv.Itoa(index), published); err != nil {
			t.Fatal(err)
		}
	}
	linkedPublished := now.Add(-time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO news(id,title,url,source,source_id,published_at,summary_text,content_format,safe_html,updated_at,content_hash,fetched_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		"linked-entry", "AI 芯片发布", "https://example.com/linked", "linked-source", "linked-source", linkedPublished, "新一代 AI 芯片摘要", "text", "", linkedPublished, "linked-hash", linkedPublished); err != nil {
		t.Fatal(err)
	}
	space := FocusSpace{
		ID: "focus-linked-scan", Goal: "跟踪 AI 芯片", DecisionQuestion: "哪些变化值得优先验证？",
		HorizonMode: "open", Status: "active", Sources: []FocusSourceLink{{SourceKind: "rss", SourceID: "linked-source", Enabled: true}},
	}
	candidates, _, err := collectFocusCandidates(space, "question", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Envelope.SourceLocator["feed_source_id"] != "linked-source" {
		t.Fatalf("candidates=%#v, want the linked source despite newer unrelated rows", candidates)
	}
}

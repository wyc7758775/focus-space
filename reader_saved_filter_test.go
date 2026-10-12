package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestSavedFilterOldRowsPaginationFlow(t *testing.T) {
	db = openTestDB(t)
	get := func(path string) map[string]any {
		r := callJSON(t, entryHandler, http.MethodGet, path, "")
		if r.Code != 200 {
			t.Fatalf("%s: %s", path, r.Body.String())
		}
		var v map[string]any
		if e := json.Unmarshal(r.Body.Bytes(), &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	for i := 0; i < 125; i++ {
		seedReaderCountEntry(t, fmt.Sprintf("saved-test-%03d", i), "test", "rss", "https://example.com/a")
	}
	for _, id := range []string{"saved-test-001", "saved-test-002"} {
		if _, e := db.Exec("INSERT INTO item_preferences(item_key,starred,read_later,updated_at) VALUES(?,1,0,123)", "news:"+id); e != nil {
			t.Fatal(e)
		}
	}
	r := get("/api/entries?saved=starred&limit=1")
	if r["total_entries"] != float64(2) {
		t.Fatalf("wrong count: %v", r)
	}
	rows := r["entries"].([]any)
	if len(rows) != 1 {
		t.Fatal(r)
	}
	cursor, _ := r["next_cursor_token"].(string)
	if cursor == "" {
		cursor = fmt.Sprint(r["next_cursor"])
	}
	next := get("/api/entries?saved=starred&limit=1&cursor=" + cursor)
	if len(next["entries"].([]any)) != 1 {
		t.Fatal(next)
	}
	empty := get("/api/entries?saved=read_later")
	if empty["total_entries"] != float64(0) {
		t.Fatal(empty)
	}
}

package main

import "testing"

func TestArticleOverviewCacheIsBoundToPromptVersionFlow(t *testing.T) {
	db = openTestDB(t)
	useStableContextScope(t, "owner-overview-cache", "profile-overview-cache")
	for _, id := range []string{"entry-old-prompt", "entry-current-prompt"} {
		if _, err := db.Exec(`INSERT INTO news(id,title,url,source,published_at,summary_text,content_format,safe_html,updated_at)
VALUES(?,?,?,?,?,?,?,?,?)`, id, id, "https://example.com/"+id, "cache-test", "2026-08-15T00:00:00Z", "摘要", "text", "", "2026-08-15T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code)
VALUES(?,?,?,?,?,?,?,?,?)`, "entry-old-prompt", "hash-1", "article-overview-v0", "ready", "旧结论", `[]`, "", "2026-08-15T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
	old, err := storedArticleOverview("entry-old-prompt", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if old != nil {
		t.Fatalf("old prompt version was reused: %#v", old)
	}

	if _, err := db.Exec(`INSERT INTO article_ai_overviews(entry_id,content_hash,prompt_version,status,summary,key_points_json,impact,generated_at,error_code)
VALUES(?,?,?,?,?,?,?,?,?)`, "entry-current-prompt", "hash-2", articleOverviewPromptVersion, "ready", "当前结论", `[]`, "", "2026-08-15T00:00:00Z", ""); err != nil {
		t.Fatal(err)
	}
	current, err := storedArticleOverview("entry-current-prompt", "hash-2")
	if err != nil {
		t.Fatal(err)
	}
	if current == nil || current.PromptVersion != articleOverviewPromptVersion || current.Summary != "当前结论" {
		t.Fatalf("current prompt version was not read: %#v", current)
	}
}

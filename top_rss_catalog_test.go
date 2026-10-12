package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestTopRSSCatalogMigrationKeepsAllSourcesAndOnlyVerifiedDefaults(t *testing.T) {
	db = openTestDB(t)

	var total, verified, unavailable, defaults int
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE catalog_origin='weekend-project-space/top-rss-list'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE catalog_origin='weekend-project-space/top-rss-list' AND validation_status='verified'`).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_catalog_entries WHERE catalog_origin='weekend-project-space/top-rss-list' AND validation_status='unavailable'`).Scan(&unavailable); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM feed_sources f JOIN feed_catalog_entries c ON c.source_id=f.id JOIN feed_subscriptions s ON s.source_id=f.id WHERE c.catalog_origin='weekend-project-space/top-rss-list' AND f.is_default=1 AND s.enabled=1`).Scan(&defaults); err != nil {
		t.Fatal(err)
	}
	// The upstream README has 357 rows. Six rows resolve to an already-used
	// canonical feed URL, so the SQLite catalog intentionally stores 351
	// distinct subscriptions while the generated audit JSON keeps all rows.
	if total != 351 || verified != 351 || unavailable != 0 || defaults != 20 {
		t.Fatalf("total=%d verified=%d unavailable=%d defaults=%d", total, verified, unavailable, defaults)
	}

	rows, err := db.Query(`SELECT f.name FROM feed_sources f JOIN feed_catalog_entries c ON c.source_id=f.id WHERE c.catalog_origin='weekend-project-space/top-rss-list' AND f.is_default=1 ORDER BY c.popularity DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make([]string, 0, 20)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	want := []string{
		"知乎每日精选", "阮一峰的网络日志", "少数派", "美团技术团队", "V2EX",
		"酷 壳 – CoolShell", "爱范儿", "知乎热榜", "《联合早报》-中港台-即时", "南方周末-新闻",
		"机核", "编程随想的博客", "奇客Solidot–传递最新科技情报", "热榜 - 煎蛋", "《联合早报》-国际-即时",
		"「ONE · 一个」", "云风的 BLOG", "知乎日报", "小众软件", "抽屉新热榜-168小时最热榜",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("defaults=\n%s", strings.Join(got, "\n"))
	}
}

func TestTopRSSCatalogAPIListsBeyondLegacyTwoHundredLimitFlow(t *testing.T) {
	db = openTestDB(t)
	response := callJSON(t, sourceHandler, http.MethodGet, "/api/sources?sort=recommended", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Sources []FeedSource `json:"sources"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources) < 357 {
		t.Fatalf("catalog response silently truncated: %d", len(payload.Sources))
	}
	foundRecovered := false
	for _, source := range payload.Sources {
		if source.ID == "top-rss-024-bde1fe03cb9b" {
			foundRecovered = source.ValidationStatus == "verified" && !source.Subscribed
			break
		}
	}
	if !foundRecovered {
		t.Fatal("retired feed source is not visible through its verified live fallback")
	}
}

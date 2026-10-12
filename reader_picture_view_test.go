package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestReaderPictureViewFilterAndCountsFlow pins the picture wall contract on
// both read paths: the keyset entry list must return exactly the rows the
// picture predicate admits, and the independent counts projection must agree
// with that predicate (same inclusion rules, video identity always wins).
func TestReaderPictureViewFilterAndCountsFlow(t *testing.T) {
	db = openTestDB(t)
	oldScope := appDataScope
	appDataScope = dataScope{OwnerID: "owner-test", ProfileID: "profile-test"}
	t.Cleanup(func() { appDataScope = oldScope })

	for _, sourceID := range []string{"pic-source", "youtube-anthropic"} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO feed_sources(id,kind,name,url,description,is_default,status,created_at,updated_at) VALUES(?,?,?,?,'',0,'ok',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, sourceID, "rss", sourceID, "https://example.com/"+sourceID); err != nil {
			t.Fatalf("seed source %s: %v", sourceID, err)
		}
	}

	fixtures := []string{
		// Ingestion-classified picture entry.
		`{"source_id":"pic-source","source_type":"rss","view_type":"picture","external_id":"pic-1","title":"Classified picture","url":"https://example.com/pic-1","media_url":"https://cdn.example.com/a.png","media_mime":"image/png","published_at":"2026-09-20T10:00:00Z","summary_text":"picture"}`,
		// Plain article carrying an image attachment: media_mime qualifies it.
		`{"source_id":"pic-source","source_type":"rss","view_type":"article","external_id":"pic-2","title":"Image attachment","url":"https://example.com/pic-2","media_url":"https://cdn.example.com/b.jpg","media_mime":"image/jpeg","published_at":"2026-09-20T09:00:00Z","summary_text":"picture"}`,
		// Article with a bare thumbnail must NOT flood the wall.
		`{"source_id":"pic-source","source_type":"rss","view_type":"article","external_id":"pic-3","title":"Article with thumbnail only","url":"https://example.com/pic-3","thumbnail_url":"https://cdn.example.com/t.webp","published_at":"2026-09-20T08:00:00Z","summary_text":"article"}`,
		// A video row claiming picture view_type with an image mime: the video
		// identity has to win on both the list predicate and the counts.
		`{"source_id":"youtube-anthropic","source_type":"youtube","view_type":"picture","external_id":"pic-4","title":"Video row claiming picture","url":"https://www.youtube.com/watch?v=pic4pic4pi","media_mime":"image/png","published_at":"2026-09-20T07:00:00Z","summary_text":"video"}`,
	}
	for index, payload := range fixtures {
		result := callJSON(t, entryHandler, http.MethodPost, "/api/entries", payload)
		if result.Code != http.StatusOK {
			t.Fatalf("fixture %d write status = %d, body = %s", index, result.Code, result.Body.String())
		}
	}

	result := callJSON(t, entryHandler, http.MethodGet, "/api/entries?view_type=picture&limit=50", "")
	if result.Code != http.StatusOK {
		t.Fatalf("picture filter status = %d, body = %s", result.Code, result.Body.String())
	}
	var page struct {
		Entries []StandardEntry `json:"entries"`
		Total   int             `json:"total_entries"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Total != 2 {
		t.Fatalf("picture filter returned %d entries / total %d, want 2 / 2", len(page.Entries), page.Total)
	}
	for _, entry := range page.Entries {
		if entry.ExternalID != "pic-1" && entry.ExternalID != "pic-2" {
			t.Fatalf("unexpected entry %q leaked into the picture wall", entry.ExternalID)
		}
	}

	if bad := callJSON(t, entryHandler, http.MethodGet, "/api/entries?view_type=pictures&limit=10", ""); bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid view_type status = %d, want 400", bad.Code)
	}

	countsResult := callJSON(t, readerCountsHandler, http.MethodGet, "/api/reader/counts", "")
	if countsResult.Code != http.StatusOK {
		t.Fatalf("counts status = %d, body = %s", countsResult.Code, countsResult.Body.String())
	}
	var counts readerCountsResponse
	if err := json.Unmarshal(countsResult.Body.Bytes(), &counts); err != nil {
		t.Fatal(err)
	}
	if counts.PictureEntries != 2 {
		t.Fatalf("picture_entries = %d, want 2 (classified picture + image attachment, never bare thumbnails or videos)", counts.PictureEntries)
	}
	if counts.TotalEntries != 4 || counts.VideoEntries != 1 || counts.RSSEntries != 3 {
		t.Fatalf("aggregate counts = %+v, want total=4 video=1 rss=3", counts.readerAggregateCounts)
	}

	var rawCounts struct {
		PictureEntries int `json:"picture_entries"`
	}
	if err := json.Unmarshal(countsResult.Body.Bytes(), &rawCounts); err != nil {
		t.Fatal(err)
	}
	if rawCounts.PictureEntries != 2 {
		t.Fatalf("picture_entries JSON field = %d, want 2", rawCounts.PictureEntries)
	}
}

// TestFeedMediaSelectionPromotesImageLast pins the ingestion half of the
// picture wall: an image enclosure/attachment becomes the media_url (and the
// entry becomes view_type=picture) only when no playable media exists, so
// video/audio feeds keep their existing classification.
func TestFeedMediaSelectionPromotesImageLast(t *testing.T) {
	imageEnclosure := &rssEnclosure{URL: "https://cdn.example.com/a.png", Type: "image/png"}

	mediaURL, mediaMIME, duration := rssMediaValues(imageEnclosure, nil, "https://example.com/feed")
	if mediaURL != "https://cdn.example.com/a.png" || mediaMIME != "image/png" || duration != 0 {
		t.Fatalf("image-only enclosure: media=%q mime=%q duration=%d, want the image promoted", mediaURL, mediaMIME, duration)
	}

	// A video media:content outranks the image enclosure even though the
	// enclosure comes first in document order.
	mediaURL, mediaMIME, _ = rssMediaValues(imageEnclosure, []rssMedia{{URL: "https://cdn.example.com/v.mp4", Type: "video/mp4"}}, "https://example.com/feed")
	if mediaMIME != "video/mp4" || mediaURL != "https://cdn.example.com/v.mp4" {
		t.Fatalf("video must outrank the image enclosure, got media=%q mime=%q", mediaURL, mediaMIME)
	}

	// JSON feed attachments follow the same ordering.
	mediaURL, mediaMIME, _ = jsonMediaValues([]jsonAttachment{
		{URL: "https://cdn.example.com/a.jpg", MimeType: "image/jpeg"},
		{URL: "https://cdn.example.com/ep.mp3", MimeType: "audio/mpeg"},
	}, "https://example.com/feed")
	if mediaMIME != "audio/mpeg" || mediaURL != "https://cdn.example.com/ep.mp3" {
		t.Fatalf("audio must outrank the image attachment, got media=%q mime=%q", mediaURL, mediaMIME)
	}

	// The Atom link-enclosure path fills media with the image only as a last
	// resort and still records the first image as the thumbnail fallback.
	atomItem := atomEntry{
		Title: "Atom photo",
		Links: []atomLink{
			{Rel: "enclosure", Href: "https://cdn.example.com/b.webp", Type: "image/webp"},
		},
	}
	item := newsItemFromAtom(atomItem, "Photo Feed", "https://example.com/feed")
	if item.MediaMIME != "image/webp" || item.ViewType != "picture" {
		t.Fatalf("atom image enclosure: media_mime=%q view_type=%q, want image/webp + picture", item.MediaMIME, item.ViewType)
	}

	fetched := makeFetchedNews("ext-1", "Photo", "https://example.com/1", "Source", "", "2026-09-20T10:00:00Z", "", "", "https://cdn.example.com/a.jpg", "image/jpeg", 0, "https://example.com")
	if fetched.ViewType != "picture" {
		t.Fatalf("image attachment view_type = %q, want picture", fetched.ViewType)
	}
}

package main

import "testing"

// TestFeedMediaCompatibilityMatrixFlow locks the feed-to-card contract at the
// parser boundary.  These fixtures are local bytes: no network is needed to
// prove URL resolution, media precedence, thumbnail separation, or type
// inference.
func TestFeedMediaCompatibilityMatrixFlow(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		content   string
		finalURL  string
		media     string
		thumbnail string
		mime      string
		duration  int
		view      string
	}{
		{
			name:    "rss media content and thumbnail with itunes duration",
			body:    `<rss xmlns:media="http://search.yahoo.com/mrss/" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>RSS</title><item><guid>rss-1</guid><title>Video</title><link>/video</link><media:content url="../video.mp4" duration=""/><media:thumbnail url="../cover.webp"/><itunes:image href="/fallback.jpg"/><itunes:duration>01:02:03</itunes:duration></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/video.mp4", thumbnail: "https://example.com/cover.webp", mime: "video/mp4", duration: 3723, view: "video",
		},
		{
			name:    "rss enclosure wins over media content",
			body:    `<rss xmlns:media="http://search.yahoo.com/mrss/"><channel><title>RSS</title><item><guid>rss-2</guid><title>Video</title><enclosure url="/enclosure.mp4" type="video/mp4"/><media:content url="/other.mp4" type="video/mp4"/><media:thumbnail url="/cover.jpg"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/enclosure.mp4", thumbnail: "https://example.com/cover.jpg", mime: "video/mp4", view: "video",
		},
		{
			name:    "rss itunes image and duration fallback",
			body:    `<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><title>Podcast</title><item><guid>rss-3</guid><title>Episode</title><enclosure url="/episode.m4a" type=""/><itunes:image href="/episode.jpg"/><itunes:duration>90</itunes:duration></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/episode.m4a", thumbnail: "https://example.com/episode.jpg", mime: "audio/mp4", duration: 90, view: "audio",
		},
		{
			// The picture wall contract: a photo feed's enclosure is the entry
			// payload, so it is promoted to media (with view_type=picture) and
			// also serves as the thumbnail fallback.
			name:    "rss image enclosure becomes picture media",
			body:    `<rss><channel><title>Images</title><item><guid>rss-image</guid><title>Photo</title><enclosure url="/photo.jpg" type="image/jpeg"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/photo.jpg", thumbnail: "https://example.com/photo.jpg", mime: "image/jpeg", view: "picture",
		},
		{
			name:    "rss wrong mime still infers video",
			body:    `<rss><channel><title>Video</title><item><guid>rss-wrong-mime</guid><title>Clip</title><enclosure url="/clip.mp4" type="application/octet-stream"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/clip.mp4", mime: "video/mp4", view: "video",
		},
		{
			name:    "rss conflicting image mime follows mp4 extension",
			body:    `<rss><channel><title>Video</title><item><guid>rss-conflict-video</guid><title>Clip</title><enclosure url="/clip.mp4" type="image/jpeg"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/clip.mp4", mime: "video/mp4", view: "video",
		},
		{
			name:    "rss conflicting video mime follows image extension",
			body:    `<rss><channel><title>Images</title><item><guid>rss-conflict-image</guid><title>Photo</title><enclosure url="/photo.jpg" type="video/mp4"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/photo.jpg", thumbnail: "https://example.com/photo.jpg", mime: "image/jpeg", view: "picture",
		},
		{
			name:    "atom enclosure and thumbnail",
			body:    `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/"><title>Atom</title><entry><id>atom-1</id><title>Atom video</title><link rel="alternate" href="/story"/><link rel="enclosure" href="/atom.mp4" type="video/mp4"/><media:thumbnail url="/atom.jpg"/><summary>Summary</summary></entry></feed>`,
			content: "application/atom+xml", media: "https://example.com/atom.mp4", thumbnail: "https://example.com/atom.jpg", mime: "video/mp4", view: "video",
		},
		{
			name:    "atom image enclosure becomes picture media",
			body:    `<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>atom-image</id><title>Photo</title><link rel="enclosure" href="/photo.jpg" type="image/jpeg"/></entry></feed>`,
			content: "application/atom+xml", media: "https://example.com/photo.jpg", thumbnail: "https://example.com/photo.jpg", mime: "image/jpeg", view: "picture",
		},
		{
			name:    "json chooses video attachment and image",
			body:    `{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"json-1","url":"/story","title":"Clip","image":"/cover.jpg","attachments":[{"url":"/audio.mp3","mime_type":"audio/mpeg"},{"url":"/clip.mp4","mime_type":""}]}]}`,
			content: "application/feed+json", media: "https://example.com/clip.mp4", thumbnail: "https://example.com/cover.jpg", mime: "video/mp4", view: "video",
		},
		{
			name:    "json banner and audio mime inference",
			body:    `{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"json-2","title":"Episode","banner_image":"../banner.png","attachments":[{"url":"../episode.ogg","mime_type":"","duration_in_seconds":12}]}]}`,
			content: "application/feed+json", media: "https://example.com/episode.ogg", thumbnail: "https://example.com/banner.png", mime: "audio/ogg", duration: 12, view: "audio",
		},
		{
			name:    "json image attachment does not hide audio",
			body:    `{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"json-3","title":"Episode","image":"/cover.jpg","attachments":[{"url":"/cover.jpg","mime_type":"image/jpeg"},{"url":"/episode.m4a","mime_type":"audio/mp4","duration_in_seconds":33}]}]}`,
			content: "application/feed+json", media: "https://example.com/episode.m4a", thumbnail: "https://example.com/cover.jpg", mime: "audio/mp4", duration: 33, view: "audio",
		},
		{
			name:    "json invalid image falls back to banner",
			body:    `{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"json-4","title":"Article","image":"http://127.0.0.1/private.jpg","banner_image":"/banner.jpg"}]}`,
			content: "application/feed+json", thumbnail: "https://example.com/banner.jpg", view: "article",
		},
		{
			name:    "json audio mp4 keeps declared audio category",
			body:    `{"version":"https://jsonfeed.org/version/1.1","title":"JSON","items":[{"id":"json-5","title":"Audio","attachments":[{"url":"/audio.mp4","mime_type":"audio/mp4","duration_in_seconds":7}]}]}`,
			content: "application/feed+json", media: "https://example.com/audio.mp4", mime: "audio/mp4", duration: 7, view: "audio",
		},
		{
			name:    "rss image media falls back to thumbnail before video",
			body:    `<rss xmlns:media="http://search.yahoo.com/mrss/"><channel><title>RSS</title><item><guid>rss-4</guid><title>Mixed</title><media:content url="/cover.jpg" type="image/jpeg"/><media:content url="/clip.mp4" type="video/mp4"/></item></channel></rss>`,
			content: "application/rss+xml", media: "https://example.com/clip.mp4", thumbnail: "https://example.com/cover.jpg", mime: "video/mp4", view: "video",
		},
		{
			name:     "final response url resolves relative fields",
			body:     `<rss><channel><title>RSS</title><item><guid>rss-final</guid><title>Final</title><link>/post</link><enclosure url="media.mp4" type="video/mp4"/></item></channel></rss>`,
			content:  "application/rss+xml",
			finalURL: "https://cdn.example.net/path/feed.xml",
			media:    "https://cdn.example.net/path/media.mp4",
			mime:     "video/mp4",
			view:     "video",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feed, err := parseFetchedFeed(feedFetchResponse{Body: []byte(tt.body), ContentType: tt.content, FinalURL: tt.finalURL}, "https://example.com/feed.xml")
			if err != nil {
				t.Fatal(err)
			}
			if len(feed.Items) != 1 {
				t.Fatalf("items = %d", len(feed.Items))
			}
			item := feed.Items[0]
			if item.MediaURL != tt.media || item.ThumbnailURL != tt.thumbnail || item.MediaMIME != tt.mime || item.DurationSecs != tt.duration || item.ViewType != tt.view {
				t.Fatalf("item = %#v", item)
			}
		})
	}
}

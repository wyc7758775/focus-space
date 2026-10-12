-- Reader interaction pack: opt-in mark-as-read-on-scroll + per-source display mode.
-- mark_read_on_scroll stays opt-in (default 0): marking entries read without an
-- explicit click is a destructive-ish state change, so the legacy behavior is
-- unchanged until the user turns it on in settings.
ALTER TABLE reader_preferences
    ADD COLUMN mark_read_on_scroll INTEGER NOT NULL DEFAULT 0 CHECK (mark_read_on_scroll IN (0, 1));

-- No FK into feed_sources: the sidebar also lists source-gateway catalog sources
-- that never exist as local feed_sources rows, and their display preference must
-- survive regardless of local subscription state.
CREATE TABLE IF NOT EXISTS source_display_prefs (
    source_id TEXT PRIMARY KEY,
    view_mode TEXT NOT NULL DEFAULT 'auto' CHECK (view_mode IN ('auto', 'article', 'video', 'gallery')),
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS stable_context_items (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    item_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'removed')),
    added_by TEXT NOT NULL CHECK (added_by IN ('favorite', 'manual_confirm', 'context_action')),
    added_at TEXT NOT NULL,
    removed_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    source_type TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL DEFAULT '',
    source_name TEXT NOT NULL DEFAULT '',
    original_url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    content_format TEXT NOT NULL DEFAULT 'text' CHECK (content_format IN ('text', 'markdown', 'html')),
    content_snapshot TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (owner_id, profile_id, item_key)
);

CREATE INDEX IF NOT EXISTS idx_stable_context_active
    ON stable_context_items(owner_id, profile_id, state, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_stable_context_hash
    ON stable_context_items(owner_id, profile_id, state, content_hash);

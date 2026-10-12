-- Scope one-off Chat links to the owner/profile that imported them.
--
-- ``news`` is shared by the Reader feed, so the hidden focus-chat source
-- cannot be used as an authorization boundary by itself. This mapping is
-- the durable ownership proof used when a Chat link is attached to a Focus.
CREATE TABLE IF NOT EXISTS focus_chat_link_entries (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    entry_id TEXT NOT NULL,
    source_url TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, entry_id),
    UNIQUE (owner_id, profile_id, source_url, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_focus_chat_link_entries_entry
    ON focus_chat_link_entries(entry_id, owner_id, profile_id);

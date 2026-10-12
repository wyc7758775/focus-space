-- Focus materials are explicit, one-item links. They are intentionally kept
-- outside focus_space_sources so linking one article never subscribes its feed
-- and importing one Chat image never creates a synthetic public source.
CREATE TABLE IF NOT EXISTS focus_space_materials (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('entry', 'image')),
    source_id TEXT NOT NULL,
    entry_id TEXT NOT NULL DEFAULT '',
    digest TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0
        CHECK (size_bytes >= 0 AND size_bytes <= 10485760),
    relative_path TEXT NOT NULL DEFAULT '',
    source_session_id TEXT NOT NULL DEFAULT '',
    source_turn_id TEXT NOT NULL DEFAULT '',
    source_index INTEGER NOT NULL DEFAULT 0 CHECK (source_index BETWEEN 0 AND 15),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    focus_revision INTEGER NOT NULL CHECK (focus_revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (id, owner_id, profile_id),
    UNIQUE (owner_id, profile_id, focus_id, source_kind, source_id),
    CHECK (
        (source_kind = 'entry' AND entry_id <> '' AND source_id = entry_id AND
            digest = '' AND mime = '' AND size_bytes = 0 AND relative_path = '' AND
            source_session_id = '' AND source_turn_id = '')
        OR
        (source_kind = 'image' AND entry_id = '' AND digest <> '' AND
            mime IN ('image/jpeg', 'image/png', 'image/gif', 'image/webp') AND
            size_bytes > 0 AND relative_path <> '' AND
            source_session_id = '' AND source_turn_id = '')
    ),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_space_materials_focus
    ON focus_space_materials(owner_id, profile_id, focus_id, enabled, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_focus_space_materials_entry
    ON focus_space_materials(owner_id, profile_id, entry_id)
    WHERE source_kind = 'entry' AND entry_id <> '';

-- File persistence and the Focus revision update are committed by one
-- operation-owned mutation. The bounded receipt makes an unknown Chat retry
-- return the original result instead of importing the same turn twice.
CREATE TABLE IF NOT EXISTS focus_material_import_requests (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    response_json TEXT NOT NULL DEFAULT '',
    response_status INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_focus_material_import_requests_updated
    ON focus_material_import_requests(owner_id, profile_id, updated_at DESC);

-- Chat-read public links use the normal Reader entry and ArticleBlock path,
-- but remain hidden from subscription navigation and background collection.
INSERT OR IGNORE INTO feed_sources (
    id, kind, name, url, description, is_default, status,
    records_seen, records_used, last_sync_at, last_error,
    created_at, updated_at, collection_mode, next_sync_at,
    auto_extract, hidden
) VALUES (
    'focus-chat-links', 'single_article', '对话链接',
    'https://local.zettlab.invalid/focus-chat-links',
    '用户明确加入焦点的一次性公开文章', 0, 'ok',
    0, 0, '', '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP,
    'app', '', 0, 1
);

-- Migration 052 constrained evidence to its original three adapters. Rebuild
-- only this bounded snapshot table so new entry/attachment evidence can be
-- persisted while every legacy row and every 093 citation column is retained.
CREATE TABLE focus_analysis_evidence_v2 (
    run_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    source_kind TEXT NOT NULL
        CHECK (source_kind IN ('meeting', 'rss', 'obsidian', 'entry', 'image')),
    source_id TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    locator TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    excerpt TEXT NOT NULL DEFAULT '',
    access_scope TEXT NOT NULL DEFAULT '',
    rank INTEGER NOT NULL DEFAULT 0 CHECK (rank >= 0),
    created_at TEXT NOT NULL,
    source_locator_json TEXT NOT NULL DEFAULT '{}',
    entry_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    block_id TEXT NOT NULL DEFAULT '',
    quote TEXT NOT NULL DEFAULT '',
    text_hash TEXT NOT NULL DEFAULT '',
    evidence_role TEXT NOT NULL DEFAULT 'support',
    PRIMARY KEY (run_id, evidence_id),
    FOREIGN KEY (run_id, owner_id, profile_id)
        REFERENCES focus_analysis_runs(id, owner_id, profile_id) ON DELETE CASCADE
);

INSERT INTO focus_analysis_evidence_v2 (
    run_id, owner_id, profile_id, evidence_id, source_kind, source_id,
    source_revision, title, locator, observed_at, published_at, excerpt,
    access_scope, rank, created_at, source_locator_json, entry_id, content_hash,
    block_id, quote, text_hash, evidence_role
)
SELECT
    run_id, owner_id, profile_id, evidence_id, source_kind, source_id,
    source_revision, title, locator, observed_at, published_at, excerpt,
    access_scope, rank, created_at, source_locator_json, entry_id, content_hash,
    block_id, quote, text_hash, evidence_role
FROM focus_analysis_evidence;

DROP TABLE focus_analysis_evidence;
ALTER TABLE focus_analysis_evidence_v2 RENAME TO focus_analysis_evidence;

CREATE INDEX idx_focus_analysis_evidence_source
    ON focus_analysis_evidence(source_kind, source_id, observed_at DESC);

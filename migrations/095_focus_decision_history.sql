-- Immutable decision revisions, append-only events, and a bounded read-only
-- graph. These tables extend the existing Focus aggregate; they do not create
-- a second Focus root or change any v1 column semantics.
ALTER TABLE decisions ADD COLUMN expected_outcome TEXT NOT NULL DEFAULT '';
ALTER TABLE decisions ADD COLUMN review_at TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS focus_decision_revisions (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    decision_id TEXT NOT NULL,
    revision_no INTEGER NOT NULL CHECK (revision_no > 0),
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'confirmed'
        CHECK (status IN ('proposed', 'confirmed', 'rejected', 'superseded')),
    expected_outcome TEXT NOT NULL DEFAULT '',
    review_at TEXT NOT NULL DEFAULT '',
    assumptions_json TEXT NOT NULL DEFAULT '[]',
    risks_json TEXT NOT NULL DEFAULT '[]',
    supersedes_id TEXT NOT NULL DEFAULT '',
    confirmed_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (owner_id, profile_id, focus_id, decision_id, revision_no),
    UNIQUE (id, owner_id, profile_id),
    UNIQUE (id, owner_id, profile_id, focus_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_revisions_focus
    ON focus_decision_revisions(owner_id, profile_id, focus_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_focus_decision_revisions_decision
    ON focus_decision_revisions(owner_id, profile_id, focus_id, decision_id, revision_no DESC);

CREATE TABLE IF NOT EXISTS focus_decision_event_counters (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    last_seq INTEGER NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    PRIMARY KEY (owner_id, profile_id, focus_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS focus_decision_events (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    event_seq INTEGER NOT NULL CHECK (event_seq > 0),
    id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    decision_revision_id TEXT NOT NULL DEFAULT '',
    actor TEXT NOT NULL CHECK (actor IN ('source', 'user', 'ai', 'system')),
    event_type TEXT NOT NULL,
    object_id TEXT NOT NULL DEFAULT '',
    label TEXT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, focus_id, event_seq),
    UNIQUE (id, owner_id, profile_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_events_revision
    ON focus_decision_events(owner_id, profile_id, decision_revision_id, event_seq);

CREATE TABLE IF NOT EXISTS focus_logic_nodes (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    decision_revision_id TEXT NOT NULL,
    id TEXT NOT NULL,
    node_type TEXT NOT NULL CHECK (node_type IN (
        'evidence', 'user_answer', 'ai_judgment', 'gap', 'conflict',
        'alternative', 'decision', 'action', 'user_note', 'correction',
        'replacement', 'review'
    )),
    author TEXT NOT NULL CHECK (author IN ('source', 'user', 'ai', 'system')),
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    locator TEXT NOT NULL DEFAULT '',
    source_locator_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(source_locator_json) AND length(source_locator_json) <= 8192),
    entry_id TEXT NOT NULL DEFAULT '',
    block_id TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    text_hash TEXT NOT NULL DEFAULT '',
    evidence_role TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'invalid', 'superseded')),
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_event_seq INTEGER NOT NULL CHECK (created_event_seq > 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, decision_revision_id, id),
    FOREIGN KEY (decision_revision_id, owner_id, profile_id, focus_id)
        REFERENCES focus_decision_revisions(id, owner_id, profile_id, focus_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_logic_nodes_focus
    ON focus_logic_nodes(owner_id, profile_id, focus_id, decision_revision_id, created_event_seq);

CREATE TABLE IF NOT EXISTS focus_logic_edges (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    decision_revision_id TEXT NOT NULL,
    id TEXT NOT NULL,
    source_node_id TEXT NOT NULL,
    target_node_id TEXT NOT NULL,
    relation TEXT NOT NULL CHECK (relation IN (
        'supports', 'challenges', 'qualifies', 'answers', 'produces',
        'corrects', 'replaces'
    )),
    label TEXT NOT NULL DEFAULT '',
    created_event_seq INTEGER NOT NULL CHECK (created_event_seq > 0),
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, decision_revision_id, id),
    FOREIGN KEY (decision_revision_id, owner_id, profile_id, focus_id)
        REFERENCES focus_decision_revisions(id, owner_id, profile_id, focus_id) ON DELETE CASCADE,
    FOREIGN KEY (owner_id, profile_id, decision_revision_id, source_node_id)
        REFERENCES focus_logic_nodes(owner_id, profile_id, decision_revision_id, id) ON DELETE CASCADE,
    FOREIGN KEY (owner_id, profile_id, decision_revision_id, target_node_id)
        REFERENCES focus_logic_nodes(owner_id, profile_id, decision_revision_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_logic_edges_focus
    ON focus_logic_edges(owner_id, profile_id, focus_id, decision_revision_id, created_event_seq);

CREATE TABLE IF NOT EXISTS focus_decision_action_links (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    decision_revision_id TEXT NOT NULL,
    action_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, decision_revision_id, action_id),
    FOREIGN KEY (decision_revision_id, owner_id, profile_id, focus_id)
        REFERENCES focus_decision_revisions(id, owner_id, profile_id, focus_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_action_links_focus
    ON focus_decision_action_links(owner_id, profile_id, focus_id, action_id);

-- Graph version cloning reads links for one revision at a time. Keep the
-- revision in the index prefix so that a large Focus does not scan every
-- historical action link for the same focus.
CREATE INDEX IF NOT EXISTS idx_focus_decision_action_links_revision
    ON focus_decision_action_links(owner_id, profile_id, focus_id, decision_revision_id, action_id);

CREATE TABLE IF NOT EXISTS focus_decision_reviews (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    decision_revision_id TEXT NOT NULL,
    outcome TEXT NOT NULL,
    result TEXT NOT NULL CHECK (result IN ('maintain', 'adjust', 'overturn')),
    replacement_revision_id TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE (id, owner_id, profile_id),
    UNIQUE (owner_id, profile_id, decision_revision_id),
    FOREIGN KEY (decision_revision_id, owner_id, profile_id, focus_id)
        REFERENCES focus_decision_revisions(id, owner_id, profile_id, focus_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_reviews_revision
    ON focus_decision_reviews(owner_id, profile_id, decision_revision_id, created_at DESC);

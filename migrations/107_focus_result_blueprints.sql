-- Plaud Focus V4 adds first-class work products and declarative user
-- customisation without changing the existing signal/action/decision enums.
CREATE TABLE IF NOT EXISTS focus_work_products (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    analysis_run_id TEXT NOT NULL,
    product_type TEXT NOT NULL CHECK (product_type IN (
        'decision_memo','plan','travel_guide','writing_brief',
        'outline','comparison','custom_document'
    )),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 240),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 2000),
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','accepted','rejected','snoozed','edited','superseded')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    current_revision_id TEXT NOT NULL,
    result_rank INTEGER NOT NULL DEFAULT 1 CHECK (result_rank >= 1),
    prompt_version TEXT NOT NULL DEFAULT '',
    evidence_version TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (id, owner_id, profile_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_work_products_run
    ON focus_work_products(owner_id, profile_id, focus_id, analysis_run_id, result_rank, id);

CREATE TABLE IF NOT EXISTS focus_work_product_revisions (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    work_product_id TEXT NOT NULL,
    revision_no INTEGER NOT NULL CHECK (revision_no >= 1),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 240),
    summary TEXT NOT NULL DEFAULT '' CHECK (length(summary) <= 2000),
    content_markdown TEXT NOT NULL CHECK (length(CAST(content_markdown AS BLOB)) <= 16384),
    source_ids_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(source_ids_json) AND length(source_ids_json) <= 8192),
    validation_items_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(validation_items_json) AND length(validation_items_json) <= 8192),
    status TEXT NOT NULL
        CHECK (status IN ('draft','accepted','rejected','snoozed','edited','superseded')),
    created_at TEXT NOT NULL,
    UNIQUE (owner_id, profile_id, work_product_id, revision_no),
    UNIQUE (id, owner_id, profile_id),
    FOREIGN KEY (work_product_id, owner_id, profile_id)
        REFERENCES focus_work_products(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_work_product_revisions_product
    ON focus_work_product_revisions(owner_id, profile_id, work_product_id, revision_no DESC);

CREATE TABLE IF NOT EXISTS focus_work_product_citations (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    work_product_id TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 1 AND 32),
    evidence_id TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL DEFAULT '',
    source_id TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    locator TEXT NOT NULL DEFAULT '',
    source_locator_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(source_locator_json) AND length(source_locator_json) <= 8192),
    entry_id TEXT NOT NULL DEFAULT '',
    asset_id TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    block_id TEXT NOT NULL DEFAULT '',
    quote TEXT NOT NULL DEFAULT '' CHECK (length(quote) <= 2000),
    text_hash TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'support' CHECK (role IN ('support','counter','context')),
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, work_product_id, revision_id, ordinal),
    FOREIGN KEY (revision_id, owner_id, profile_id)
        REFERENCES focus_work_product_revisions(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS focus_result_links (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    analysis_run_id TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL CHECK (source_kind IN ('evidence','signal','decision','work_product','action')),
    source_id TEXT NOT NULL,
    relation TEXT NOT NULL CHECK (relation IN ('supports','contradicts','produces','leads_to','depends_on','references','supersedes')),
    target_kind TEXT NOT NULL CHECK (target_kind IN ('evidence','signal','decision','work_product','action')),
    target_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, focus_id, source_kind, source_id, relation, target_kind, target_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_result_links_focus
    ON focus_result_links(owner_id, profile_id, focus_id, analysis_run_id);

CREATE TABLE IF NOT EXISTS focus_blueprint_overrides (
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('application','focus')),
    scope_id TEXT NOT NULL DEFAULT '',
    blueprint_id TEXT NOT NULL,
    base_version TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    patch_json TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(patch_json) AND length(CAST(patch_json AS BLOB)) <= 16384),
    effective_json TEXT NOT NULL
        CHECK (json_valid(effective_json) AND length(CAST(effective_json AS BLOB)) <= 65536),
    effective_hash TEXT NOT NULL CHECK (length(effective_hash) = 64),
    upgrade_state TEXT NOT NULL DEFAULT 'current'
        CHECK (upgrade_state IN ('current','ready','conflict')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (owner_id, profile_id, scope_kind, scope_id, blueprint_id)
);

CREATE TABLE IF NOT EXISTS focus_blueprint_revisions (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('application','focus')),
    scope_id TEXT NOT NULL DEFAULT '',
    blueprint_id TEXT NOT NULL,
    base_version TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    patch_json TEXT NOT NULL CHECK (json_valid(patch_json)),
    effective_json TEXT NOT NULL CHECK (json_valid(effective_json)),
    effective_hash TEXT NOT NULL CHECK (length(effective_hash) = 64),
    risk_class TEXT NOT NULL CHECK (risk_class IN ('presentation','workflow')),
    change_summary_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(change_summary_json)),
    created_at TEXT NOT NULL,
    UNIQUE (owner_id, profile_id, scope_kind, scope_id, blueprint_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_focus_blueprint_revisions_scope
    ON focus_blueprint_revisions(owner_id, profile_id, scope_kind, scope_id, blueprint_id, revision DESC);

CREATE TABLE IF NOT EXISTS focus_workspace_bindings (
    focus_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    intent_kind TEXT NOT NULL CHECK (intent_kind IN ('decide','plan','create','monitor')),
    blueprint_id TEXT NOT NULL,
    blueprint_version TEXT NOT NULL,
    thinking_template_id TEXT NOT NULL DEFAULT 'guided',
    thinking_template_version TEXT NOT NULL DEFAULT 'builtin-v1',
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (focus_id, owner_id, profile_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

ALTER TABLE focus_decision_runs ADD COLUMN intent_kind TEXT NOT NULL DEFAULT 'decide'
    CHECK (intent_kind IN ('decide','plan','create','monitor'));
ALTER TABLE focus_decision_runs ADD COLUMN blueprint_id TEXT NOT NULL DEFAULT 'decide.v1';
ALTER TABLE focus_decision_runs ADD COLUMN blueprint_version TEXT NOT NULL DEFAULT '1';
ALTER TABLE focus_decision_runs ADD COLUMN thinking_template_id TEXT NOT NULL DEFAULT '';
ALTER TABLE focus_decision_runs ADD COLUMN thinking_template_version TEXT NOT NULL DEFAULT 'builtin-v1';

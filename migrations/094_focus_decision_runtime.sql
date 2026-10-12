-- Focus Decision Runtime is additive to the existing Focus Space model. It
-- owns only resumable run and clarification state; decisions, evidence and
-- actions remain in their existing tables.
CREATE TABLE IF NOT EXISTS focus_decision_runs (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    focus_id TEXT NOT NULL,
    focus_revision INTEGER NOT NULL CHECK (focus_revision >= 1),
    analysis_run_id TEXT NOT NULL DEFAULT '',
    decision_id TEXT NOT NULL DEFAULT '',
    decision_mode TEXT NOT NULL
        CHECK (decision_mode IN ('rational', 'bounded_rationality', 'first_principles', 'guided')),
    status TEXT NOT NULL
        CHECK (status IN (
            'queued', 'collecting_context', 'running_ai',
            'waiting_clarification', 'applying', 'completed',
            'paused_insufficient_credits', 'failed_retryable',
            'failed_terminal', 'cancelled', 'superseded'
        )),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt >= 1),
    question_count INTEGER NOT NULL DEFAULT 0 CHECK (question_count BETWEEN 0 AND 12),
    current_question_id TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL DEFAULT '',
    completed_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (id, owner_id, profile_id),
    FOREIGN KEY (focus_id, owner_id, profile_id)
        REFERENCES focus_spaces(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_runs_focus
    ON focus_decision_runs(owner_id, profile_id, focus_id, created_at DESC);

-- A Focus has at most one resumable run. Completed or terminal runs remain
-- available for history while a later run can start.
CREATE UNIQUE INDEX IF NOT EXISTS idx_focus_decision_runs_active
    ON focus_decision_runs(owner_id, profile_id, focus_id)
    WHERE status IN (
        'queued', 'collecting_context', 'running_ai',
        'waiting_clarification', 'applying',
        'paused_insufficient_credits', 'failed_retryable'
    );

CREATE TABLE IF NOT EXISTS focus_decision_questions (
    run_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    question_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL CHECK (sequence_no BETWEEN 1 AND 12),
    question_type TEXT NOT NULL
        CHECK (question_type IN ('choice', 'multi', 'text', 'scale')),
    question TEXT NOT NULL CHECK (length(question) BETWEEN 1 AND 2000),
    options_json TEXT NOT NULL DEFAULT '[]'
        CHECK (json_valid(options_json) AND length(options_json) <= 8192),
    required INTEGER NOT NULL DEFAULT 1 CHECK (required IN (0, 1)),
    max_select INTEGER NOT NULL DEFAULT 1 CHECK (max_select BETWEEN 1 AND 6),
    scale_min INTEGER NOT NULL DEFAULT 1,
    scale_max INTEGER NOT NULL DEFAULT 5,
    origin TEXT NOT NULL DEFAULT 'builtin'
        CHECK (origin IN ('builtin', 'runtime')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'answered', 'superseded')),
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision >= 1),
    answer_revision INTEGER NOT NULL DEFAULT 0 CHECK (answer_revision >= 0),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, question_id),
    UNIQUE (run_id, sequence_no),
    UNIQUE (run_id, question_id, owner_id, profile_id),
    FOREIGN KEY (run_id, owner_id, profile_id)
        REFERENCES focus_decision_runs(id, owner_id, profile_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_focus_decision_questions_current
    ON focus_decision_questions(owner_id, profile_id, run_id, status, sequence_no);

-- Answers are append-only revisions. Only one revision per question is the
-- current answer, so an edited upstream answer can invalidate downstream
-- answers without erasing their audit history.
CREATE TABLE IF NOT EXISTS focus_decision_answers (
    run_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    profile_id TEXT NOT NULL,
    question_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    answer_type TEXT NOT NULL
        CHECK (answer_type IN ('choice', 'multi', 'text', 'scale')),
    answer_json TEXT NOT NULL
        CHECK (json_valid(answer_json) AND length(answer_json) <= 8192),
    is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
    answered_at TEXT NOT NULL,
    PRIMARY KEY (run_id, question_id, revision),
    FOREIGN KEY (run_id, question_id, owner_id, profile_id)
        REFERENCES focus_decision_questions(run_id, question_id, owner_id, profile_id)
        ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_focus_decision_answers_current
    ON focus_decision_answers(run_id, question_id)
    WHERE is_current = 1;

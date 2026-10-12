-- App Data mutations reserve their idempotency key before invoking a business
-- handler. The bounded tombstone/result table prevents a reconnect from
-- applying the same accepted mutation twice without keeping an unbounded map.
CREATE TABLE IF NOT EXISTS app_data_mutation_requests (
    operation TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'complete')),
    response_status INTEGER NOT NULL DEFAULT 0,
    response_json TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (operation, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_app_data_mutation_requests_created
    ON app_data_mutation_requests(created_at);

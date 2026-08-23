-- +goose Up
-- +goose StatementBegin
-- Durable, ordered domain events. See ADR 0002.
-- `sequence` is a per-database monotonic ordering key assigned inside the append
-- transaction; read-model correctness derives from replaying this order rather
-- than from receiving every in-process pub/sub notification.

CREATE TABLE IF NOT EXISTS domain_events (
    event_id TEXT PRIMARY KEY,
    sequence INTEGER NOT NULL UNIQUE,
    event_type TEXT NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    project_id TEXT,
    session_id TEXT,
    task_id TEXT,
    turn_id TEXT,
    occurred_at INTEGER NOT NULL,        -- Unix timestamp in milliseconds
    payload_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_domain_events_task ON domain_events (task_id, sequence);
CREATE INDEX IF NOT EXISTS idx_domain_events_session ON domain_events (session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_domain_events_project_type ON domain_events (project_id, event_type, occurred_at);
CREATE INDEX IF NOT EXISTS idx_domain_events_type ON domain_events (event_type, sequence);

-- Projection checkpoints let dashboard read models resume from the last applied
-- sequence after a missed notification or restart.
CREATE TABLE IF NOT EXISTS projection_checkpoints (
    projection_name TEXT PRIMARY KEY,
    last_sequence INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS projection_checkpoints;
DROP TABLE IF EXISTS domain_events;
-- +goose StatementEnd

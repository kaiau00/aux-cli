-- +goose Up
-- +goose StatementBegin
-- Runtime observability: per-call model ledger and tool-execution records.
-- project_id/task_id/turn_id are
-- nullable here because project identity (PR 5) and first-class tasks (PR 7)
-- do not exist yet; they are backfilled onto new records once those land.

CREATE TABLE IF NOT EXISTS model_calls (
    model_call_id TEXT PRIMARY KEY,
    project_id TEXT,
    task_id TEXT,
    turn_id TEXT,
    session_id TEXT,
    message_id TEXT,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'started',
    retry_group TEXT,
    price_catalog_version TEXT NOT NULL DEFAULT '',
    cost_state TEXT NOT NULL DEFAULT 'known',
    started_at INTEGER NOT NULL,          -- Unix timestamp in milliseconds
    finished_at INTEGER,                  -- Unix timestamp in milliseconds
    first_token_at INTEGER,               -- Unix timestamp in milliseconds
    latency_ms INTEGER NOT NULL DEFAULT 0,
    ttft_ms INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost REAL NOT NULL DEFAULT 0.0,
    error_code TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_model_calls_session ON model_calls (session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_model_calls_task ON model_calls (task_id, started_at);
CREATE INDEX IF NOT EXISTS idx_model_calls_turn ON model_calls (turn_id);
CREATE INDEX IF NOT EXISTS idx_model_calls_retry_group ON model_calls (retry_group);

CREATE TABLE IF NOT EXISTS tool_executions (
    tool_execution_id TEXT PRIMARY KEY,
    project_id TEXT,
    task_id TEXT,
    turn_id TEXT,
    session_id TEXT,
    model_call_id TEXT,
    tool_call_id TEXT,
    tool_name TEXT NOT NULL DEFAULT '',
    input_hash TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'started',
    started_at INTEGER NOT NULL,          -- Unix timestamp in milliseconds
    finished_at INTEGER,                  -- Unix timestamp in milliseconds
    latency_ms INTEGER NOT NULL DEFAULT 0,
    response_bytes INTEGER NOT NULL DEFAULT 0,
    is_error INTEGER NOT NULL DEFAULT 0,
    artifact_id TEXT,
    metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tool_executions_task ON tool_executions (task_id, started_at);
CREATE INDEX IF NOT EXISTS idx_tool_executions_session ON tool_executions (session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_tool_executions_name ON tool_executions (tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_executions_input_hash ON tool_executions (input_hash);
CREATE INDEX IF NOT EXISTS idx_tool_executions_call ON tool_executions (model_call_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS tool_executions;
DROP TABLE IF EXISTS model_calls;
-- +goose StatementEnd

-- +goose Up
-- +goose StatementBegin
-- Per-task context exclusions: the TUI's x/u/c
-- cross-off controls record a real override here, consulted by the prompt
-- compiler on the task's next compile, rather than only repainting a local
-- checkbox.
CREATE TABLE IF NOT EXISTS context_exclusions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    tool_call_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (task_id, tool_call_id)
);

CREATE INDEX IF NOT EXISTS idx_context_exclusions_task ON context_exclusions (task_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS context_exclusions;
-- +goose StatementEnd

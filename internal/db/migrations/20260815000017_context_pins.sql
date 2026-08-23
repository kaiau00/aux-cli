-- +goose Up
-- +goose StatementBegin
-- Per-task context pins (StatePinned): the TUI's
-- pin control records a real override here, consulted by the prompt compiler
-- on the task's next compile so a pinned page's full content is guaranteed —
-- exempt from both content-dedup stubbing and exclusion stubbing — rather
-- than only repainting a local checkbox. Mirrors context_exclusions.
CREATE TABLE IF NOT EXISTS context_pins (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    tool_call_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    UNIQUE (task_id, tool_call_id)
);

CREATE INDEX IF NOT EXISTS idx_context_pins_task ON context_pins (task_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS context_pins;
-- +goose StatementEnd

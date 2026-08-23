-- +goose Up
-- +goose StatementBegin
-- Parent/child task linkage: multi-repo compilation creates one child task
-- per target repository under a parent task, and efficient subagents give
-- each subagent its own real task row linked
-- to the task that spawned it. Nullable and additive: existing single-task
-- rows are unaffected.
ALTER TABLE tasks ADD COLUMN parent_task_id TEXT;

CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks (parent_task_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_tasks_parent;
ALTER TABLE tasks DROP COLUMN parent_task_id;
-- +goose StatementEnd

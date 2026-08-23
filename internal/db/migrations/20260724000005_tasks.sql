-- +goose Up
-- +goose StatementBegin
-- First-class tasks: a session can contain many tasks; a task belongs to one
-- project revision at execution time.

CREATE TABLE IF NOT EXISTS tasks (
    task_id TEXT PRIMARY KEY,
    project_id TEXT,
    session_id TEXT NOT NULL,
    project_revision_id TEXT NOT NULL DEFAULT '',
    profile_version_set TEXT NOT NULL DEFAULT '',
    objective TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    outcome TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    started_at INTEGER,
    finished_at INTEGER
);

CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks (session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks (status);

CREATE TABLE IF NOT EXISTS task_specs (
    task_id TEXT NOT NULL,
    spec_version INTEGER NOT NULL DEFAULT 1,
    content_json TEXT NOT NULL DEFAULT '{}',
    source_message_id TEXT NOT NULL DEFAULT '',
    compiler_version TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    PRIMARY KEY (task_id, spec_version),
    FOREIGN KEY (task_id) REFERENCES tasks (task_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_steps (
    step_id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    evidence_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks (task_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_steps_task ON task_steps (task_id, ordinal);

CREATE TABLE IF NOT EXISTS task_budgets (
    task_id TEXT PRIMARY KEY,
    mode TEXT NOT NULL DEFAULT '',
    max_cost REAL NOT NULL DEFAULT 0,
    max_input_tokens INTEGER NOT NULL DEFAULT 0,
    max_output_tokens INTEGER NOT NULL DEFAULT 0,
    max_wall_ms INTEGER NOT NULL DEFAULT 0,
    max_tool_calls INTEGER NOT NULL DEFAULT 0,
    policy_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (task_id) REFERENCES tasks (task_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS task_corrections (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    turn_id TEXT NOT NULL DEFAULT '',
    correction_type TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks (task_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_task_corrections_task ON task_corrections (task_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS task_corrections;
DROP TABLE IF EXISTS task_budgets;
DROP TABLE IF EXISTS task_steps;
DROP TABLE IF EXISTS task_specs;
DROP TABLE IF EXISTS tasks;
-- +goose StatementEnd

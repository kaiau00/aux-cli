-- +goose Up
-- +goose StatementBegin
-- Task checkpoints and their entries. A
-- checkpoint references content-addressed before/after blobs (artifacts), so
-- branches share immutable content and store only deltas. parent_checkpoint_id
-- forms an acyclic DAG.

CREATE TABLE IF NOT EXISTS checkpoints (
    checkpoint_id TEXT PRIMARY KEY,
    task_id TEXT,
    parent_checkpoint_id TEXT,
    label TEXT NOT NULL DEFAULT '',
    vcs_revision TEXT NOT NULL DEFAULT '',
    tree_hash TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (parent_checkpoint_id) REFERENCES checkpoints (checkpoint_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_checkpoints_task ON checkpoints (task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_checkpoints_parent ON checkpoints (parent_checkpoint_id);

CREATE TABLE IF NOT EXISTS checkpoint_entries (
    id TEXT PRIMARY KEY,
    checkpoint_id TEXT NOT NULL,
    path TEXT NOT NULL,
    before_artifact_id TEXT,
    after_artifact_id TEXT,
    operation TEXT NOT NULL DEFAULT '',  -- add|modify|delete|rename
    mode TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (checkpoint_id) REFERENCES checkpoints (checkpoint_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_checkpoint_entries_checkpoint ON checkpoint_entries (checkpoint_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS checkpoint_entries;
DROP TABLE IF EXISTS checkpoints;
-- +goose StatementEnd

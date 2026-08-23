-- +goose Up
-- +goose StatementBegin
-- Project identity: stable project, its roots/worktrees, and revisions.
-- Identity is keyed by normalized VCS
-- remote hash where available, else by canonical root path (local-only project).

CREATE TABLE IF NOT EXISTS projects (
    project_id TEXT PRIMARY KEY,
    canonical_name TEXT NOT NULL,
    vcs_type TEXT NOT NULL DEFAULT 'none',
    canonical_remote_hash TEXT,
    created_at INTEGER NOT NULL,
    last_opened_at INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_remote_hash
    ON projects (canonical_remote_hash) WHERE canonical_remote_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS project_roots (
    path_hash TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    canonical_path TEXT NOT NULL,
    workspace_kind TEXT NOT NULL DEFAULT 'primary',
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    FOREIGN KEY (project_id) REFERENCES projects (project_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_project_roots_project ON project_roots (project_id);

CREATE TABLE IF NOT EXISTS project_revisions (
    project_revision_id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    vcs_revision TEXT NOT NULL DEFAULT '',
    branch_name TEXT NOT NULL DEFAULT '',
    dirty_tree_hash TEXT NOT NULL DEFAULT '',
    profile_input_hash TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (project_id) REFERENCES projects (project_id) ON DELETE CASCADE,
    UNIQUE (project_id, vcs_revision, dirty_tree_hash)
);

CREATE INDEX IF NOT EXISTS idx_project_revisions_project ON project_revisions (project_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_revisions;
DROP TABLE IF EXISTS project_roots;
DROP TABLE IF EXISTS projects;
-- +goose StatementEnd

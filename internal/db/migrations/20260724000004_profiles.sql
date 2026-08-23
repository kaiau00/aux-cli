-- +goose Up
-- +goose StatementBegin
-- Project profiles: layered, versioned, compiled project knowledge.

CREATE TABLE IF NOT EXISTS profiles (
    profile_id TEXT PRIMARY KEY,
    owner_type TEXT NOT NULL,      -- builtin | user | org | project | workspace | branch | task_mode
    owner_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    precedence INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (owner_type, owner_id, name)
);

CREATE TABLE IF NOT EXISTS profile_versions (
    profile_version_id TEXT PRIMARY KEY,
    profile_id TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL,
    compiler_version TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (profile_id) REFERENCES profiles (profile_id) ON DELETE CASCADE,
    UNIQUE (profile_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_profile_versions_profile ON profile_versions (profile_id, created_at);

CREATE TABLE IF NOT EXISTS profile_entries (
    entry_id TEXT PRIMARY KEY,
    profile_version_id TEXT NOT NULL,
    entry_type TEXT NOT NULL,
    entry_key TEXT NOT NULL,
    value_json TEXT NOT NULL DEFAULT '{}',
    source_type TEXT NOT NULL DEFAULT '',
    source_ref TEXT NOT NULL DEFAULT '',
    confidence REAL NOT NULL DEFAULT 0.0,
    token_estimate INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (profile_version_id) REFERENCES profile_versions (profile_version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_profile_entries_version ON profile_entries (profile_version_id);
CREATE INDEX IF NOT EXISTS idx_profile_entries_type ON profile_entries (profile_version_id, entry_type);

-- The effective (layered/merged) profile compiled for a project revision + task
-- mode. Populated by the profile compiler (PR 6).
CREATE TABLE IF NOT EXISTS effective_profiles (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    project_revision_id TEXT NOT NULL DEFAULT '',
    task_mode TEXT NOT NULL DEFAULT '',
    version_set_hash TEXT NOT NULL,
    compiled_artifact_id TEXT,
    manifest_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    UNIQUE (project_id, project_revision_id, task_mode, version_set_hash)
);

CREATE INDEX IF NOT EXISTS idx_effective_profiles_project ON effective_profiles (project_id, created_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS effective_profiles;
DROP TABLE IF EXISTS profile_entries;
DROP TABLE IF EXISTS profile_versions;
DROP TABLE IF EXISTS profiles;
-- +goose StatementEnd

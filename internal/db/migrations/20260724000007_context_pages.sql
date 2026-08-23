-- +goose Up
-- +goose StatementBegin
-- Typed context pages: the addressable units the prompt compiler assembles.
-- A page has a stable identity; each
-- version is content-addressed; bindings record what a specific model call held.

CREATE TABLE IF NOT EXISTS context_pages (
    page_id TEXT PRIMARY KEY,
    project_id TEXT,
    page_type TEXT NOT NULL,
    stable_key TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    UNIQUE (project_id, page_type, stable_key)
);

CREATE TABLE IF NOT EXISTS context_page_versions (
    page_version_id TEXT PRIMARY KEY,
    page_id TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    artifact_id TEXT,
    source_revision TEXT NOT NULL DEFAULT '',
    token_count INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (page_id) REFERENCES context_pages (page_id) ON DELETE CASCADE,
    UNIQUE (page_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_page_versions_page ON context_page_versions (page_id, created_at);

CREATE TABLE IF NOT EXISTS context_bindings (
    id TEXT PRIMARY KEY,
    task_id TEXT,
    model_call_id TEXT NOT NULL,
    page_version_id TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'resident',
    rank INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    token_count INTEGER NOT NULL DEFAULT 0,
    bound_at INTEGER NOT NULL,
    evicted_at INTEGER,
    FOREIGN KEY (page_version_id) REFERENCES context_page_versions (page_version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_context_bindings_call ON context_bindings (model_call_id);
CREATE INDEX IF NOT EXISTS idx_context_bindings_task ON context_bindings (task_id);

CREATE TABLE IF NOT EXISTS page_accesses (
    id TEXT PRIMARY KEY,
    task_id TEXT,
    model_call_id TEXT NOT NULL,
    page_version_id TEXT NOT NULL,
    access_type TEXT NOT NULL DEFAULT '',
    useful_signal TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_page_accesses_call ON page_accesses (model_call_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS page_accesses;
DROP TABLE IF EXISTS context_bindings;
DROP TABLE IF EXISTS context_page_versions;
DROP TABLE IF EXISTS context_pages;
-- +goose StatementEnd

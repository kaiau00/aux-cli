-- +goose Up
-- +goose StatementBegin
-- Persistent, scoped memory with provenance and revision-aware invalidation.
-- Memory is compiled, bounded knowledge —
-- never a raw transcript (that is the event store's job).

CREATE TABLE IF NOT EXISTS memories (
    memory_id TEXT PRIMARY KEY,
    project_id TEXT,
    memory_type TEXT NOT NULL,      -- factual | procedural | episodic
    scope TEXT NOT NULL DEFAULT '',
    stable_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'candidate', -- candidate|active|superseded|stale|rejected|archived
    confidence REAL NOT NULL DEFAULT 0.0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (project_id, memory_type, stable_key)
);

CREATE INDEX IF NOT EXISTS idx_memories_project_state ON memories (project_id, state);
CREATE INDEX IF NOT EXISTS idx_memories_type ON memories (memory_type);

CREATE TABLE IF NOT EXISTS memory_versions (
    memory_version_id TEXT PRIMARY KEY,
    memory_id TEXT NOT NULL,
    content_json TEXT NOT NULL DEFAULT '{}',
    content_hash TEXT NOT NULL,
    supporting_revision TEXT NOT NULL DEFAULT '',
    supersedes_version_id TEXT,
    created_at INTEGER NOT NULL,
    FOREIGN KEY (memory_id) REFERENCES memories (memory_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_versions_memory ON memory_versions (memory_id, created_at);

CREATE TABLE IF NOT EXISTS memory_sources (
    id TEXT PRIMARY KEY,
    memory_version_id TEXT NOT NULL,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    source_hash TEXT NOT NULL DEFAULT '',
    relation TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (memory_version_id) REFERENCES memory_versions (memory_version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_sources_version ON memory_sources (memory_version_id);

CREATE TABLE IF NOT EXISTS memory_feedback (
    id TEXT PRIMARY KEY,
    memory_version_id TEXT NOT NULL,
    task_id TEXT,
    outcome TEXT NOT NULL DEFAULT '',
    signal TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (memory_version_id) REFERENCES memory_versions (memory_version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_feedback_version ON memory_feedback (memory_version_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS memory_feedback;
DROP TABLE IF EXISTS memory_sources;
DROP TABLE IF EXISTS memory_versions;
DROP TABLE IF EXISTS memories;
-- +goose StatementEnd

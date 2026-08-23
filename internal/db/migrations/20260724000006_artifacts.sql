-- +goose Up
-- +goose StatementBegin
-- Content-addressed artifact store: SQLite holds identity/metadata/refs; the
-- bytes live outside SQLite in a content-addressed filesystem backend by
-- default.

CREATE TABLE IF NOT EXISTS artifacts (
    artifact_id TEXT PRIMARY KEY,
    content_hash TEXT NOT NULL,
    storage_backend TEXT NOT NULL DEFAULT 'fs',
    storage_key TEXT NOT NULL,
    media_type TEXT NOT NULL DEFAULT 'text/plain',
    byte_size INTEGER NOT NULL DEFAULT 0,
    compression TEXT NOT NULL DEFAULT 'none',
    sensitivity TEXT NOT NULL DEFAULT 'normal',
    created_at INTEGER NOT NULL,
    last_accessed_at INTEGER NOT NULL,
    UNIQUE (content_hash)
);

CREATE TABLE IF NOT EXISTS artifact_refs (
    id TEXT PRIMARY KEY,
    artifact_id TEXT NOT NULL,
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL DEFAULT '',
    relation TEXT NOT NULL DEFAULT 'produced',
    created_at INTEGER NOT NULL,
    FOREIGN KEY (artifact_id) REFERENCES artifacts (artifact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_artifact_refs_artifact ON artifact_refs (artifact_id);
CREATE INDEX IF NOT EXISTS idx_artifact_refs_owner ON artifact_refs (owner_type, owner_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS artifact_refs;
DROP TABLE IF EXISTS artifacts;
-- +goose StatementEnd

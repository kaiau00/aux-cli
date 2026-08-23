-- +goose Up
-- +goose StatementBegin
-- Related-project graph. Edges between distinct projects
-- with a typed relationship (service/client, library/consumer, schema/generator,
-- application/infrastructure, code/documentation). Every edge keeps both project
-- identities so cross-project retrieval never merges symbols from different
-- repositories without identity.
CREATE TABLE IF NOT EXISTS project_relations (
    id TEXT PRIMARY KEY,
    from_project_id TEXT NOT NULL,
    to_project_id TEXT NOT NULL,
    relation_type TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT '', -- how the edge was derived (deps|schema|declared|...)
    created_at INTEGER NOT NULL,
    UNIQUE (from_project_id, to_project_id, relation_type)
);

CREATE INDEX IF NOT EXISTS idx_project_relations_from ON project_relations (from_project_id);
CREATE INDEX IF NOT EXISTS idx_project_relations_to ON project_relations (to_project_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS project_relations;
-- +goose StatementEnd

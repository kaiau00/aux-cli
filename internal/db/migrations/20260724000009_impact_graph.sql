-- +goose Up
-- +goose StatementBegin
-- Hybrid change-impact graph. Nodes are files/packages/symbols/tests/commands;
-- edges are typed relationships with a source and confidence weight. Refresh
-- updates affected partitions by changed paths; a full rebuild is a repair.

CREATE TABLE IF NOT EXISTS graph_nodes (
    node_id TEXT PRIMARY KEY,
    project_id TEXT,
    node_type TEXT NOT NULL,          -- file|directory|package|module|symbol|test|command|generated
    stable_key TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    source_revision TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE (project_id, node_type, stable_key)
);

CREATE INDEX IF NOT EXISTS idx_graph_nodes_project_type ON graph_nodes (project_id, node_type);

CREATE TABLE IF NOT EXISTS graph_edges (
    edge_id TEXT PRIMARY KEY,
    project_id TEXT,
    from_node_id TEXT NOT NULL,
    to_node_id TEXT NOT NULL,
    edge_type TEXT NOT NULL,          -- imports|calls|references|implements|contains|builds|tests|co_changes|owns|generates
    weight REAL NOT NULL DEFAULT 1.0,
    source TEXT NOT NULL DEFAULT '',  -- ast|lsp|manifest|git|mcp
    source_revision TEXT NOT NULL DEFAULT '',
    UNIQUE (from_node_id, to_node_id, edge_type)
);

CREATE INDEX IF NOT EXISTS idx_graph_edges_from ON graph_edges (from_node_id, edge_type);
CREATE INDEX IF NOT EXISTS idx_graph_edges_to ON graph_edges (to_node_id, edge_type);
CREATE INDEX IF NOT EXISTS idx_graph_edges_project ON graph_edges (project_id);

CREATE TABLE IF NOT EXISTS graph_index_state (
    project_id TEXT PRIMARY KEY,
    source_revision TEXT NOT NULL DEFAULT '',
    indexer_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'unindexed', -- unindexed|indexing|indexed|error
    last_indexed_at INTEGER NOT NULL DEFAULT 0
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS graph_index_state;
DROP TABLE IF EXISTS graph_edges;
DROP TABLE IF EXISTS graph_nodes;
-- +goose StatementEnd

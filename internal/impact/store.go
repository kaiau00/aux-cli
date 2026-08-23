package impact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists graph nodes, edges, and index state.
type Store struct {
	db db.DBTX
}

// NewStore returns a graph store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// UpsertNode returns the node for (project, type, stableKey), creating or
// refreshing it. Returns the node id.
func (s *Store) UpsertNode(ctx context.Context, n Node) (string, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT node_id FROM graph_nodes WHERE project_id IS ? AND node_type = ? AND stable_key = ?`,
		nullable(n.ProjectID), n.Type, n.StableKey)
	var id string
	err := row.Scan(&id)
	if err == nil {
		_, uerr := s.db.ExecContext(ctx,
			`UPDATE graph_nodes SET display_name = ?, source_revision = ?, metadata_json = ? WHERE node_id = ?`,
			n.DisplayName, n.SourceRevision, orDefault(n.MetadataJSON), id)
		return id, uerr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("failed to look up node: %w", err)
	}
	id = ids.New()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO graph_nodes (node_id, project_id, node_type, stable_key, display_name, source_revision, metadata_json)
         VALUES (?,?,?,?,?,?,?)`,
		id, nullable(n.ProjectID), n.Type, n.StableKey, n.DisplayName, n.SourceRevision, orDefault(n.MetadataJSON)); err != nil {
		return "", fmt.Errorf("failed to insert node: %w", err)
	}
	return id, nil
}

// UpsertEdge inserts or updates a typed edge between two nodes.
func (s *Store) UpsertEdge(ctx context.Context, e Edge) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO graph_edges (edge_id, project_id, from_node_id, to_node_id, edge_type, weight, source, source_revision)
         VALUES (?,?,?,?,?,?,?,?)
         ON CONFLICT(from_node_id, to_node_id, edge_type) DO UPDATE SET
             weight = excluded.weight, source = excluded.source, source_revision = excluded.source_revision`,
		ids.New(), nullable(e.ProjectID), e.FromNodeID, e.ToNodeID, e.Type, e.Weight, e.Source, e.SourceRevision)
	if err != nil {
		return fmt.Errorf("failed to upsert edge: %w", err)
	}
	return nil
}

// NodeByKey returns a node id for (project, type, stableKey), if present.
func (s *Store) NodeByKey(ctx context.Context, projectID, nodeType, stableKey string) (string, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT node_id FROM graph_nodes WHERE project_id IS ? AND node_type = ? AND stable_key = ?`,
		nullable(projectID), nodeType, stableKey)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// DeleteNodeEdges removes all edges incident to a node (for incremental reindex
// of that node's partition).
func (s *Store) DeleteNodeEdges(ctx context.Context, nodeID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM graph_edges WHERE from_node_id = ? OR to_node_id = ?`, nodeID, nodeID)
	return err
}

// EdgesFrom returns outgoing edges of a node, optionally filtered by type.
func (s *Store) EdgesFrom(ctx context.Context, nodeID string, edgeTypes ...string) ([]Edge, error) {
	return s.edges(ctx, "from_node_id = ?", nodeID, edgeTypes)
}

// EdgesTo returns incoming edges of a node, optionally filtered by type.
func (s *Store) EdgesTo(ctx context.Context, nodeID string, edgeTypes ...string) ([]Edge, error) {
	return s.edges(ctx, "to_node_id = ?", nodeID, edgeTypes)
}

func (s *Store) edges(ctx context.Context, where, nodeID string, edgeTypes []string) ([]Edge, error) {
	q := `SELECT edge_id, project_id, from_node_id, to_node_id, edge_type, weight, source, source_revision FROM graph_edges WHERE ` + where
	args := []any{nodeID}
	if len(edgeTypes) > 0 {
		q += " AND edge_type IN ("
		for i, t := range edgeTypes {
			if i > 0 {
				q += ","
			}
			q += "?"
			args = append(args, t)
		}
		q += ")"
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		var projectID sql.NullString
		if err := rows.Scan(&e.ID, &projectID, &e.FromNodeID, &e.ToNodeID, &e.Type, &e.Weight, &e.Source, &e.SourceRevision); err != nil {
			return nil, err
		}
		e.ProjectID = projectID.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// NodeName returns a node's stable key by id.
func (s *Store) NodeName(ctx context.Context, nodeID string) (string, string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT node_type, stable_key FROM graph_nodes WHERE node_id = ?`, nodeID)
	var nodeType, key string
	if err := row.Scan(&nodeType, &key); err != nil {
		return "", "", err
	}
	return nodeType, key, nil
}

// ListNodes returns up to limit nodes for a project, most recently touched
// first (rowid order), for graph-browsing surfaces such as the dashboard's
// impact view (roadmapplan.md §13.14). limit <= 0 falls back to 200.
func (s *Store) ListNodes(ctx context.Context, projectID string, limit int) ([]Node, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT node_id, project_id, node_type, stable_key, display_name, source_revision, metadata_json
         FROM graph_nodes WHERE project_id IS ? ORDER BY rowid DESC LIMIT ?`, nullable(projectID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Node
	for rows.Next() {
		var n Node
		var projID sql.NullString
		if err := rows.Scan(&n.ID, &projID, &n.Type, &n.StableKey, &n.DisplayName, &n.SourceRevision, &n.MetadataJSON); err != nil {
			return nil, err
		}
		n.ProjectID = projID.String
		out = append(out, n)
	}
	return out, rows.Err()
}

// ListEdges returns up to limit edges for a project, most recently touched
// first. limit <= 0 falls back to 400.
func (s *Store) ListEdges(ctx context.Context, projectID string, limit int) ([]Edge, error) {
	if limit <= 0 {
		limit = 400
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT edge_id, project_id, from_node_id, to_node_id, edge_type, weight, source, source_revision
         FROM graph_edges WHERE project_id IS ? ORDER BY rowid DESC LIMIT ?`, nullable(projectID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Edge
	for rows.Next() {
		var e Edge
		var projID sql.NullString
		if err := rows.Scan(&e.ID, &projID, &e.FromNodeID, &e.ToNodeID, &e.Type, &e.Weight, &e.Source, &e.SourceRevision); err != nil {
			return nil, err
		}
		e.ProjectID = projID.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountNodes returns how many nodes a project has (0 => graph not built).
func (s *Store) CountNodes(ctx context.Context, projectID string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM graph_nodes WHERE project_id IS ?`, nullable(projectID))
	var n int
	err := row.Scan(&n)
	return n, err
}

// SetIndexState upserts a project's index state.
func (s *Store) SetIndexState(ctx context.Context, st IndexState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO graph_index_state (project_id, source_revision, indexer_version, status, last_indexed_at)
         VALUES (?,?,?,?,?)
         ON CONFLICT(project_id) DO UPDATE SET
             source_revision = excluded.source_revision, indexer_version = excluded.indexer_version,
             status = excluded.status, last_indexed_at = excluded.last_indexed_at`,
		nullable(st.ProjectID), st.SourceRevision, st.IndexerVersion, st.Status, st.LastIndexedAt)
	return err
}

// GetIndexState returns a project's index state (zero value if none).
func (s *Store) GetIndexState(ctx context.Context, projectID string) (IndexState, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT project_id, source_revision, indexer_version, status, last_indexed_at FROM graph_index_state WHERE project_id IS ?`,
		nullable(projectID))
	var st IndexState
	var pid sql.NullString
	err := row.Scan(&pid, &st.SourceRevision, &st.IndexerVersion, &st.Status, &st.LastIndexedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return IndexState{}, false, nil
	}
	if err != nil {
		return IndexState{}, false, err
	}
	st.ProjectID = pid.String
	return st, true, nil
}

func orDefault(m string) string {
	if m == "" {
		return "{}"
	}
	return m
}

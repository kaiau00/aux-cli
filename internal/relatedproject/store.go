package relatedproject

import (
	"context"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists the related-project graph.
type Store struct {
	db db.DBTX
}

// NewStore returns a related-project store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

// Add inserts a relation, ignoring duplicates (same from/to/type). A self-edge is
// rejected — a project is never related to itself.
func (s *Store) Add(ctx context.Context, r Relation) error {
	if r.FromProject == "" || r.ToProject == "" {
		return fmt.Errorf("relation requires both project ids")
	}
	if r.FromProject == r.ToProject {
		return fmt.Errorf("a project cannot relate to itself")
	}
	if r.ID == "" {
		r.ID = ids.New()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO project_relations (id, from_project_id, to_project_id, relation_type, source, created_at)
         VALUES (?,?,?,?,?,?)`,
		r.ID, r.FromProject, r.ToProject, string(r.RelationType), string(r.Source), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("failed to add relation: %w", err)
	}
	return nil
}

// From returns edges originating at a project (its dependencies/consumers).
func (s *Store) From(ctx context.Context, projectID string) ([]Relation, error) {
	return s.query(ctx, `WHERE from_project_id = ?`, projectID)
}

// To returns edges pointing at a project (its dependents).
func (s *Store) To(ctx context.Context, projectID string) ([]Relation, error) {
	return s.query(ctx, `WHERE to_project_id = ?`, projectID)
}

func (s *Store) query(ctx context.Context, where string, arg string) ([]Relation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, from_project_id, to_project_id, relation_type, source, created_at
         FROM project_relations `+where+` ORDER BY created_at`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Relation
	for rows.Next() {
		var r Relation
		var relType, src string
		if err := rows.Scan(&r.ID, &r.FromProject, &r.ToProject, &relType, &src, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.RelationType = RelationType(relType)
		r.Source = Source(src)
		out = append(out, r)
	}
	return out, rows.Err()
}

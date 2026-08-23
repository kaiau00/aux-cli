package checkpoint

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
)

// Store persists checkpoints and their entries.
type Store struct {
	db db.DBTX
}

// NewStore returns a checkpoint store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Insert stores a checkpoint and its entries. The parent (if any) must exist,
// which — combined with monotonic creation — keeps the DAG acyclic.
func (s *Store) Insert(ctx context.Context, c Checkpoint, entries []Entry) error {
	if c.ParentID != "" {
		if _, err := s.Get(ctx, c.ParentID); err != nil {
			return fmt.Errorf("parent checkpoint %s not found: %w", c.ParentID, err)
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (checkpoint_id, task_id, parent_checkpoint_id, label, vcs_revision, tree_hash, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		c.ID, nullable(c.TaskID), nullable(c.ParentID), c.Label, c.Revision, c.TreeHash, c.CreatedAt); err != nil {
		return fmt.Errorf("failed to insert checkpoint: %w", err)
	}
	for _, e := range entries {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO checkpoint_entries (id, checkpoint_id, path, before_artifact_id, after_artifact_id, operation, mode)
             VALUES (?,?,?,?,?,?,?)`,
			e.ID, c.ID, e.Path, nullable(e.BeforeArtifactID), nullable(e.AfterArtifactID), string(e.Operation), e.Mode); err != nil {
			return fmt.Errorf("failed to insert checkpoint entry: %w", err)
		}
	}
	return nil
}

// Get returns a checkpoint by id.
func (s *Store) Get(ctx context.Context, id string) (Checkpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT checkpoint_id, task_id, parent_checkpoint_id, label, vcs_revision, tree_hash, created_at
         FROM checkpoints WHERE checkpoint_id = ?`, id)
	var c Checkpoint
	var taskID, parentID sql.NullString
	if err := row.Scan(&c.ID, &taskID, &parentID, &c.Label, &c.Revision, &c.TreeHash, &c.CreatedAt); err != nil {
		return Checkpoint{}, err
	}
	c.TaskID = taskID.String
	c.ParentID = parentID.String
	return c, nil
}

// Entries returns a checkpoint's entries.
func (s *Store) Entries(ctx context.Context, checkpointID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, checkpoint_id, path, before_artifact_id, after_artifact_id, operation, mode
         FROM checkpoint_entries WHERE checkpoint_id = ? ORDER BY path`, checkpointID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		var before, after sql.NullString
		var op string
		if err := rows.Scan(&e.ID, &e.CheckpointID, &e.Path, &before, &after, &op, &e.Mode); err != nil {
			return nil, err
		}
		e.BeforeArtifactID = before.String
		e.AfterArtifactID = after.String
		e.Operation = Operation(op)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListByTask returns a task's checkpoints oldest-first.
func (s *Store) ListByTask(ctx context.Context, taskID string) ([]Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT checkpoint_id, task_id, parent_checkpoint_id, label, vcs_revision, tree_hash, created_at
         FROM checkpoints WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		var tID, pID sql.NullString
		if err := rows.Scan(&c.ID, &tID, &pID, &c.Label, &c.Revision, &c.TreeHash, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.TaskID = tID.String
		c.ParentID = pID.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// Ancestors walks parent links to the root, detecting cycles defensively.
func (s *Store) Ancestors(ctx context.Context, id string) ([]Checkpoint, error) {
	var out []Checkpoint
	seen := map[string]struct{}{}
	cur := id
	for cur != "" {
		if _, dup := seen[cur]; dup {
			return nil, errors.New("checkpoint DAG contains a cycle")
		}
		seen[cur] = struct{}{}
		c, err := s.Get(ctx, cur)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
		cur = c.ParentID
	}
	return out, nil
}

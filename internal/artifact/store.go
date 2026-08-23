package artifact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
)

// Artifact is the metadata record for a stored blob.
type Artifact struct {
	ID             string
	ContentHash    string
	StorageBackend string
	StorageKey     string
	MediaType      string
	ByteSize       int64
	Compression    string
	Sensitivity    string
	CreatedAt      int64
	LastAccessedAt int64
}

// Ref links an artifact to an owner (task, tool execution, page version, …).
type Ref struct {
	ID         string
	ArtifactID string
	OwnerType  string
	OwnerID    string
	Relation   string
	CreatedAt  int64
}

// Store persists artifact metadata and references.
type Store struct {
	db db.DBTX
}

// NewStore returns an artifact metadata store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

// GetByHash returns the artifact with a given content hash, if present.
func (s *Store) GetByHash(ctx context.Context, hash string) (Artifact, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM artifacts WHERE content_hash = ?`, hash)
	a, err := scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, err
	}
	return a, true, nil
}

// GetByID returns the artifact with a given id.
func (s *Store) GetByID(ctx context.Context, id string) (Artifact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+cols+` FROM artifacts WHERE artifact_id = ?`, id)
	return scan(row)
}

// Insert stores artifact metadata.
func (s *Store) Insert(ctx context.Context, a Artifact) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO artifacts (artifact_id, content_hash, storage_backend, storage_key, media_type, byte_size, compression, sensitivity, created_at, last_accessed_at)
         VALUES (?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ContentHash, a.StorageBackend, a.StorageKey, a.MediaType, a.ByteSize, a.Compression, a.Sensitivity, a.CreatedAt, a.LastAccessedAt)
	if err != nil {
		return fmt.Errorf("failed to insert artifact: %w", err)
	}
	return nil
}

// AddRef records an ownership reference.
func (s *Store) AddRef(ctx context.Context, r Ref) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO artifact_refs (id, artifact_id, owner_type, owner_id, relation, created_at) VALUES (?,?,?,?,?,?)`,
		r.ID, r.ArtifactID, r.OwnerType, r.OwnerID, r.Relation, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to add artifact ref: %w", err)
	}
	return nil
}

// Touch updates last_accessed_at.
func (s *Store) Touch(ctx context.Context, id string, at int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE artifacts SET last_accessed_at = ? WHERE artifact_id = ?`, at, id)
	return err
}

// Unreferenced returns artifacts with no refs (GC candidates). This is the
// mark-and-sweep read side; deletion is a separate, explicit step.
func (s *Store) Unreferenced(ctx context.Context) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+cols+` FROM artifacts a
         WHERE NOT EXISTS (SELECT 1 FROM artifact_refs r WHERE r.artifact_id = a.artifact_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const cols = `artifact_id, content_hash, storage_backend, storage_key, media_type, byte_size, compression, sensitivity, created_at, last_accessed_at`

type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (Artifact, error) {
	var a Artifact
	if err := row.Scan(&a.ID, &a.ContentHash, &a.StorageBackend, &a.StorageKey, &a.MediaType,
		&a.ByteSize, &a.Compression, &a.Sensitivity, &a.CreatedAt, &a.LastAccessedAt); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

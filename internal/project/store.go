package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
)

// Store persists project identity, roots, and revisions.
type Store struct {
	db db.DBTX
}

// NewStore returns a project store backed by the given database handle.
func NewStore(dbtx db.DBTX) *Store {
	return &Store{db: dbtx}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// GetProjectByRemoteHash returns the project matching a normalized-remote hash.
func (s *Store) GetProjectByRemoteHash(ctx context.Context, hash string) (Project, bool, error) {
	if hash == "" {
		return Project{}, false, nil
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+projectCols+` FROM projects WHERE canonical_remote_hash = ?`, hash)
	return scanProjectOpt(row)
}

// GetProjectByRootPathHash returns the project owning the root with the given path hash.
func (s *Store) GetProjectByRootPathHash(ctx context.Context, pathHash string) (Project, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+projectColsPrefixed+` FROM projects p
         JOIN project_roots r ON r.project_id = p.project_id
         WHERE r.path_hash = ?`, pathHash)
	return scanProjectOpt(row)
}

// CreateProject inserts a new project.
func (s *Store) CreateProject(ctx context.Context, p Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects (project_id, canonical_name, vcs_type, canonical_remote_hash, created_at, last_opened_at)
         VALUES (?,?,?,?,?,?)`,
		p.ID, p.CanonicalName, p.VCSType, nullable(p.CanonicalRemoteHash), p.CreatedAt, p.LastOpenedAt)
	if err != nil {
		return fmt.Errorf("failed to create project: %w", err)
	}
	return nil
}

// TouchProject updates last_opened_at.
func (s *Store) TouchProject(ctx context.Context, id string, at int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE projects SET last_opened_at = ? WHERE project_id = ?`, at, id)
	return err
}

// GetProject returns a project by id.
func (s *Store) GetProject(ctx context.Context, id string) (Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+projectCols+` FROM projects WHERE project_id = ?`, id)
	p, ok, err := scanProjectOpt(row)
	if err != nil {
		return Project{}, err
	}
	if !ok {
		return Project{}, fmt.Errorf("project %s not found", id)
	}
	return p, nil
}

// ListProjects returns all projects, most recently opened first.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectCols+` FROM projects ORDER BY last_opened_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertRoot inserts or refreshes a root row.
func (s *Store) UpsertRoot(ctx context.Context, r Root) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO project_roots (path_hash, project_id, canonical_path, workspace_kind, created_at, last_seen_at)
         VALUES (?,?,?,?,?,?)
         ON CONFLICT(path_hash) DO UPDATE SET
             project_id = excluded.project_id,
             last_seen_at = excluded.last_seen_at`,
		r.PathHash, r.ProjectID, r.CanonicalPath, r.WorkspaceKind, r.CreatedAt, r.LastSeenAt)
	if err != nil {
		return fmt.Errorf("failed to upsert root: %w", err)
	}
	return nil
}

// ListRoots returns a project's roots.
func (s *Store) ListRoots(ctx context.Context, projectID string) ([]Root, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT path_hash, project_id, canonical_path, workspace_kind, created_at, last_seen_at
         FROM project_roots WHERE project_id = ? ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Root
	for rows.Next() {
		var r Root
		if err := rows.Scan(&r.PathHash, &r.ProjectID, &r.CanonicalPath, &r.WorkspaceKind, &r.CreatedAt, &r.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetOrCreateRevision returns the existing revision matching (project, vcs_revision,
// dirty_tree_hash) or creates it. Reopening the same tree reuses the revision.
func (s *Store) GetOrCreateRevision(ctx context.Context, r Revision) (Revision, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+revisionCols+` FROM project_revisions
         WHERE project_id = ? AND vcs_revision = ? AND dirty_tree_hash = ?`,
		r.ProjectID, r.VCSRevision, r.DirtyTreeHash)
	existing, err := scanRevision(row)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Revision{}, fmt.Errorf("failed to look up revision: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO project_revisions (project_revision_id, project_id, vcs_revision, branch_name, dirty_tree_hash, profile_input_hash, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.VCSRevision, r.BranchName, r.DirtyTreeHash, r.ProfileInputHash, r.CreatedAt)
	if err != nil {
		return Revision{}, fmt.Errorf("failed to create revision: %w", err)
	}
	return r, nil
}

// SetRevisionProfileInputHash records the profile-input fingerprint on a revision.
func (s *Store) SetRevisionProfileInputHash(ctx context.Context, revisionID, hash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE project_revisions SET profile_input_hash = ? WHERE project_revision_id = ?`, hash, revisionID)
	return err
}

const projectCols = `project_id, canonical_name, vcs_type, canonical_remote_hash, created_at, last_opened_at`
const projectColsPrefixed = `p.project_id, p.canonical_name, p.vcs_type, p.canonical_remote_hash, p.created_at, p.last_opened_at`
const revisionCols = `project_revision_id, project_id, vcs_revision, branch_name, dirty_tree_hash, profile_input_hash, created_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (Project, error) {
	var p Project
	var remoteHash sql.NullString
	if err := row.Scan(&p.ID, &p.CanonicalName, &p.VCSType, &remoteHash, &p.CreatedAt, &p.LastOpenedAt); err != nil {
		return Project{}, err
	}
	p.CanonicalRemoteHash = remoteHash.String
	return p, nil
}

func scanProjectOpt(row scanner) (Project, bool, error) {
	p, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	return p, true, nil
}

func scanRevision(row scanner) (Revision, error) {
	var r Revision
	if err := row.Scan(&r.ID, &r.ProjectID, &r.VCSRevision, &r.BranchName, &r.DirtyTreeHash, &r.ProfileInputHash, &r.CreatedAt); err != nil {
		return Revision{}, err
	}
	return r, nil
}

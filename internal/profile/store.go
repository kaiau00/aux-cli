package profile

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// Store persists profiles, versions, and entries.
type Store struct {
	db db.DBTX
}

// NewStore returns a profile store backed by the given database handle.
func NewStore(dbtx db.DBTX) *Store {
	return &Store{db: dbtx}
}

// GetOrCreateProfile returns the profile for (ownerType, ownerID, name), creating it if absent.
func (s *Store) GetOrCreateProfile(ctx context.Context, p Profile) (Profile, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT profile_id, owner_type, owner_id, name, precedence, enabled, created_at, updated_at
         FROM profiles WHERE owner_type = ? AND owner_id = ? AND name = ?`,
		p.OwnerType, p.OwnerID, p.Name)
	existing, err := scanProfile(row)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Profile{}, fmt.Errorf("failed to look up profile: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO profiles (profile_id, owner_type, owner_id, name, precedence, enabled, created_at, updated_at)
         VALUES (?,?,?,?,?,?,?,?)`,
		p.ID, p.OwnerType, p.OwnerID, p.Name, p.Precedence, boolToInt(p.Enabled), p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return Profile{}, fmt.Errorf("failed to create profile: %w", err)
	}
	return p, nil
}

// GetVersionByContentHash returns a version with the given content hash, if any.
func (s *Store) GetVersionByContentHash(ctx context.Context, profileID, contentHash string) (Version, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT profile_version_id, profile_id, source_revision, content_hash, compiler_version, created_at
         FROM profile_versions WHERE profile_id = ? AND content_hash = ?`, profileID, contentHash)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, false, nil
	}
	if err != nil {
		return Version{}, false, err
	}
	return v, true, nil
}

// InsertVersion inserts a new profile version and its entries in one transaction-like batch.
func (s *Store) InsertVersion(ctx context.Context, v Version, entries []Entry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO profile_versions (profile_version_id, profile_id, source_revision, content_hash, compiler_version, created_at)
         VALUES (?,?,?,?,?,?)`,
		v.ID, v.ProfileID, v.SourceRevision, v.ContentHash, v.CompilerVersion, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert profile version: %w", err)
	}
	for _, e := range entries {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO profile_entries (entry_id, profile_version_id, entry_type, entry_key, value_json, source_type, source_ref, confidence, token_estimate)
             VALUES (?,?,?,?,?,?,?,?,?)`,
			e.ID, v.ID, e.Type, e.Key, e.ValueJSON, e.SourceType, e.SourceRef, e.Confidence, e.TokenEstimate)
		if err != nil {
			return fmt.Errorf("failed to insert profile entry: %w", err)
		}
	}
	return nil
}

// ListEntries returns the entries of a profile version.
func (s *Store) ListEntries(ctx context.Context, versionID string) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_id, profile_version_id, entry_type, entry_key, value_json, source_type, source_ref, confidence, token_estimate
         FROM profile_entries WHERE profile_version_id = ? ORDER BY entry_type, entry_key`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.ProfileVersionID, &e.Type, &e.Key, &e.ValueJSON, &e.SourceType, &e.SourceRef, &e.Confidence, &e.TokenEstimate); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// LatestVersion returns the most recent version for a profile.
func (s *Store) LatestVersion(ctx context.Context, profileID string) (Version, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT profile_version_id, profile_id, source_revision, content_hash, compiler_version, created_at
         FROM profile_versions WHERE profile_id = ? ORDER BY created_at DESC LIMIT 1`, profileID)
	v, err := scanVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, false, nil
	}
	if err != nil {
		return Version{}, false, err
	}
	return v, true, nil
}

// SaveEffective persists an effective profile, deduplicating by its unique key.
// It returns the stored id (existing or newly created).
func (s *Store) SaveEffective(ctx context.Context, id string, e Effective) (string, error) {
	manifestJSON, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("failed to marshal effective profile: %w", err)
	}
	// Reuse an existing identical effective profile if present.
	var existingID string
	row := s.db.QueryRowContext(ctx,
		`SELECT id FROM effective_profiles
         WHERE project_id = ? AND project_revision_id = ? AND task_mode = ? AND version_set_hash = ?`,
		e.ProjectID, e.RevisionID, e.TaskMode, e.VersionSetHash)
	if err := row.Scan(&existingID); err == nil {
		return existingID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("failed to look up effective profile: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO effective_profiles (id, project_id, project_revision_id, task_mode, version_set_hash, manifest_json, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		id, e.ProjectID, e.RevisionID, e.TaskMode, e.VersionSetHash, string(manifestJSON), nowMillis())
	if err != nil {
		return "", fmt.Errorf("failed to insert effective profile: %w", err)
	}
	return id, nil
}

// GetLatestEffective returns the most recent effective profile for a project + task mode.
func (s *Store) GetLatestEffective(ctx context.Context, projectID, taskMode string) (Effective, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT manifest_json FROM effective_profiles
         WHERE project_id = ? AND task_mode = ? ORDER BY created_at DESC LIMIT 1`, projectID, taskMode)
	var manifestJSON string
	if err := row.Scan(&manifestJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Effective{}, false, nil
		}
		return Effective{}, false, err
	}
	var e Effective
	if err := json.Unmarshal([]byte(manifestJSON), &e); err != nil {
		return Effective{}, false, fmt.Errorf("failed to decode effective profile: %w", err)
	}
	return e, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProfile(row scanner) (Profile, error) {
	var p Profile
	var enabled int
	if err := row.Scan(&p.ID, &p.OwnerType, &p.OwnerID, &p.Name, &p.Precedence, &enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Profile{}, err
	}
	p.Enabled = enabled != 0
	return p, nil
}

func scanVersion(row scanner) (Version, error) {
	var v Version
	if err := row.Scan(&v.ID, &v.ProfileID, &v.SourceRevision, &v.ContentHash, &v.CompilerVersion, &v.CreatedAt); err != nil {
		return Version{}, err
	}
	return v, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

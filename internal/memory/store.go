package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists memories, versions, sources, and feedback.
type Store struct {
	db db.DBTX
}

// NewStore returns a memory store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SaveCandidate creates (or updates) a memory in the candidate state with a new
// content version and provenance. Deduplication is by (project, type, stableKey):
// an unchanged candidate reuses its existing version; changed content adds a new
// version that supersedes the previous one, keeping a reversible chain.
func (s *Store) SaveCandidate(ctx context.Context, c Candidate) (Memory, Version, error) {
	now := time.Now().UnixMilli()
	contentJSON := "{}"
	if c.Content != nil {
		if b, err := json.Marshal(c.Content); err == nil {
			contentJSON = string(b)
		}
	}
	contentHash := hashString(contentJSON)

	mem, found, err := s.getMemory(ctx, c.ProjectID, string(c.Type), c.StableKey)
	if err != nil {
		return Memory{}, Version{}, err
	}
	if !found {
		mem = Memory{
			ID: ids.New(), ProjectID: c.ProjectID, Type: c.Type, Scope: c.Scope,
			StableKey: c.StableKey, State: StateCandidate, Confidence: c.Confidence,
			CreatedAt: now, UpdatedAt: now,
		}
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO memories (memory_id, project_id, memory_type, scope, stable_key, state, confidence, created_at, updated_at)
             VALUES (?,?,?,?,?,?,?,?,?)`,
			mem.ID, nullable(mem.ProjectID), string(mem.Type), mem.Scope, mem.StableKey, string(mem.State), mem.Confidence, now, now); err != nil {
			return Memory{}, Version{}, fmt.Errorf("failed to create memory: %w", err)
		}
	}

	// Reuse an identical latest version.
	latest, hasLatest, err := s.latestVersion(ctx, mem.ID)
	if err != nil {
		return Memory{}, Version{}, err
	}
	if hasLatest && latest.ContentHash == contentHash {
		return mem, latest, nil
	}

	ver := Version{
		ID: ids.New(), MemoryID: mem.ID, ContentJSON: contentJSON, ContentHash: contentHash,
		SupportingRevision: c.SupportingRevision, CreatedAt: now,
	}
	if hasLatest {
		ver.SupersedesVersionID = latest.ID
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_versions (memory_version_id, memory_id, content_json, content_hash, supporting_revision, supersedes_version_id, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		ver.ID, ver.MemoryID, ver.ContentJSON, ver.ContentHash, ver.SupportingRevision, nullable(ver.SupersedesVersionID), now); err != nil {
		return Memory{}, Version{}, fmt.Errorf("failed to create memory version: %w", err)
	}
	for _, src := range c.Sources {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO memory_sources (id, memory_version_id, source_type, source_id, source_hash, relation, created_at)
             VALUES (?,?,?,?,?,?,?)`,
			ids.New(), ver.ID, src.Type, src.ID, src.Hash, src.Relation, now); err != nil {
			return Memory{}, Version{}, fmt.Errorf("failed to record memory source: %w", err)
		}
	}
	return mem, ver, nil
}

// SetState transitions a memory to a new state.
func (s *Store) SetState(ctx context.Context, memoryID string, state State) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memories SET state = ?, updated_at = ? WHERE memory_id = ?`, string(state), time.Now().UnixMilli(), memoryID)
	if err != nil {
		return fmt.Errorf("failed to set memory state: %w", err)
	}
	return nil
}

// Promote moves a candidate to active, optionally raising confidence.
func (s *Store) Promote(ctx context.Context, memoryID string, confidence float64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memories SET state = 'active', confidence = ?, updated_at = ? WHERE memory_id = ?`,
		confidence, time.Now().UnixMilli(), memoryID)
	return err
}

// MarkStaleForChangedRevision marks active memories whose latest supporting
// revision differs from currentRevision as stale (revision-aware invalidation).
// It never deletes: revalidation can reactivate or supersede later.
func (s *Store) MarkStaleForChangedRevision(ctx context.Context, projectID, currentRevision string) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.memory_id FROM memories m
JOIN memory_versions v ON v.memory_version_id = (
    SELECT memory_version_id FROM memory_versions WHERE memory_id = m.memory_id ORDER BY created_at DESC LIMIT 1
)
WHERE m.project_id IS ? AND m.state = 'active'
  AND v.supporting_revision != '' AND v.supporting_revision != ?`,
		nullable(projectID), currentRevision)
	if err != nil {
		return 0, fmt.Errorf("failed to scan stale memories: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range ids {
		if err := s.SetState(ctx, id, StateStale); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// RecordFeedback records a usefulness/outcome signal for a memory version.
func (s *Store) RecordFeedback(ctx context.Context, versionID, taskID, outcome, signal string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_feedback (id, memory_version_id, task_id, outcome, signal, created_at) VALUES (?,?,?,?,?,?)`,
		ids.New(), versionID, nullable(taskID), outcome, signal, time.Now().UnixMilli())
	return err
}

// Retrieve returns active memories for a project filtered by type and scope,
// ordered by confidence, capped at limit. Hard scope is applied before ranking
// (roadmapplan.md §8.4).
func (s *Store) Retrieve(ctx context.Context, projectID string, types []Type, limit int) ([]Memory, error) {
	q := `SELECT memory_id, project_id, memory_type, scope, stable_key, state, confidence, created_at, updated_at
          FROM memories WHERE project_id IS ? AND state = 'active'`
	args := []any{nullable(projectID)}
	if len(types) > 0 {
		q += " AND memory_type IN ("
		for i, t := range types {
			if i > 0 {
				q += ","
			}
			q += "?"
			args = append(args, string(t))
		}
		q += ")"
	}
	q += " ORDER BY confidence DESC, updated_at DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListByState returns memories in a given state for a project.
func (s *Store) ListByState(ctx context.Context, projectID string, state State) ([]Memory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT memory_id, project_id, memory_type, scope, stable_key, state, confidence, created_at, updated_at
         FROM memories WHERE project_id IS ? AND state = ? ORDER BY updated_at DESC`, nullable(projectID), string(state))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) getMemory(ctx context.Context, projectID, memType, stableKey string) (Memory, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT memory_id, project_id, memory_type, scope, stable_key, state, confidence, created_at, updated_at
         FROM memories WHERE project_id IS ? AND memory_type = ? AND stable_key = ?`,
		nullable(projectID), memType, stableKey)
	m, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, err
	}
	return m, true, nil
}

func (s *Store) latestVersion(ctx context.Context, memoryID string) (Version, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT memory_version_id, memory_id, content_json, content_hash, supporting_revision, created_at
         FROM memory_versions WHERE memory_id = ? ORDER BY created_at DESC LIMIT 1`, memoryID)
	var v Version
	err := row.Scan(&v.ID, &v.MemoryID, &v.ContentJSON, &v.ContentHash, &v.SupportingRevision, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Version{}, false, nil
	}
	if err != nil {
		return Version{}, false, err
	}
	return v, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMemory(row scanner) (Memory, error) {
	var m Memory
	var projectID sql.NullString
	var mtype, state string
	if err := row.Scan(&m.ID, &projectID, &mtype, &m.Scope, &m.StableKey, &state, &m.Confidence, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return Memory{}, err
	}
	m.ProjectID = projectID.String
	m.Type = Type(mtype)
	m.State = State(state)
	return m, nil
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

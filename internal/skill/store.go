package skill

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aux-ai/aux-cli/internal/db"
	"github.com/aux-ai/aux-cli/internal/ids"
)

// Store persists skills, versions, and evaluations.
type Store struct {
	db db.DBTX
}

// NewStore returns a skill store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

// CreateSkill inserts a skill (candidate) if absent and returns it.
func (s *Store) CreateSkill(ctx context.Context, sk Skill) (Skill, error) {
	existing, found, err := s.getSkill(ctx, sk.OwnerType, sk.OwnerID, sk.Name)
	if err != nil {
		return Skill{}, err
	}
	if found {
		return existing, nil
	}
	if sk.ID == "" {
		sk.ID = ids.New()
	}
	now := time.Now().UnixMilli()
	sk.CreatedAt, sk.UpdatedAt = now, now
	if sk.State == "" {
		sk.State = StateCandidate
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO skills (skill_id, owner_type, owner_id, name, scope, state, created_at, updated_at)
         VALUES (?,?,?,?,?,?,?,?)`,
		sk.ID, sk.OwnerType, sk.OwnerID, sk.Name, sk.Scope, string(sk.State), now, now); err != nil {
		return Skill{}, fmt.Errorf("failed to create skill: %w", err)
	}
	return sk, nil
}

// AddVersion inserts a new skill version.
func (s *Store) AddVersion(ctx context.Context, v Version) (Version, error) {
	if v.ID == "" {
		v.ID = ids.New()
	}
	v.CreatedAt = time.Now().UnixMilli()
	content, err := json.Marshal(v.Content)
	if err != nil {
		return Version{}, err
	}
	srcIDs, _ := json.Marshal(v.SourceIDs)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_versions (skill_version_id, skill_id, content_json, source_type, source_ids_json, created_at)
         VALUES (?,?,?,?,?,?)`,
		v.ID, v.SkillID, string(content), v.SourceType, string(srcIDs), v.CreatedAt); err != nil {
		return Version{}, fmt.Errorf("failed to add skill version: %w", err)
	}
	return v, nil
}

// LatestVersion returns the newest version for a skill.
func (s *Store) LatestVersion(ctx context.Context, skillID string) (Version, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT skill_version_id, skill_id, content_json, source_type, source_ids_json, created_at
         FROM skill_versions WHERE skill_id = ? ORDER BY created_at DESC LIMIT 1`, skillID)
	return scanVersionOpt(row)
}

// RecordEvaluation stores a skill evaluation.
func (s *Store) RecordEvaluation(ctx context.Context, e Evaluation) error {
	if e.ID == "" {
		e.ID = ids.New()
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_evaluations (id, skill_version_id, eval_run_id, baseline_version_id, result, metrics_json, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.SkillVersionID, e.EvalRunID, e.BaselineVersion, string(e.Result), orDefault(e.MetricsJSON), time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("failed to record evaluation: %w", err)
	}
	return nil
}

// HasPassingEvaluation reports whether a skill version has a passing evaluation.
func (s *Store) HasPassingEvaluation(ctx context.Context, versionID string) (bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM skill_evaluations WHERE skill_version_id = ? AND result = 'pass'`, versionID)
	var n int
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetState transitions a skill's state.
func (s *Store) SetState(ctx context.Context, skillID string, state State) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE skills SET state = ?, updated_at = ? WHERE skill_id = ?`, string(state), time.Now().UnixMilli(), skillID)
	return err
}

// GetSkill returns a skill by id.
func (s *Store) GetSkill(ctx context.Context, id string) (Skill, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT skill_id, owner_type, owner_id, name, scope, state, created_at, updated_at FROM skills WHERE skill_id = ?`, id)
	sk, ok, err := scanSkillOpt(row)
	if err != nil {
		return Skill{}, err
	}
	if !ok {
		return Skill{}, fmt.Errorf("skill %s not found", id)
	}
	return sk, nil
}

// ListByState returns skills in a state.
func (s *Store) ListByState(ctx context.Context, state State) ([]Skill, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT skill_id, owner_type, owner_id, name, scope, state, created_at, updated_at FROM skills WHERE state = ? ORDER BY updated_at DESC`, string(state))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Skill
	for rows.Next() {
		sk, _, err := scanSkillRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

func (s *Store) getSkill(ctx context.Context, ownerType, ownerID, name string) (Skill, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT skill_id, owner_type, owner_id, name, scope, state, created_at, updated_at
         FROM skills WHERE owner_type = ? AND owner_id = ? AND name = ?`, ownerType, ownerID, name)
	return scanSkillOpt(row)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSkillOpt(row scanner) (Skill, bool, error) {
	sk, ok, err := scanSkillRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Skill{}, false, nil
	}
	return sk, ok, err
}

func scanSkillRow(row scanner) (Skill, bool, error) {
	var sk Skill
	var state string
	if err := row.Scan(&sk.ID, &sk.OwnerType, &sk.OwnerID, &sk.Name, &sk.Scope, &state, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
		return Skill{}, false, err
	}
	sk.State = State(state)
	return sk, true, nil
}

func scanVersionOpt(row scanner) (Version, bool, error) {
	var v Version
	var content, srcIDs string
	if err := row.Scan(&v.ID, &v.SkillID, &content, &v.SourceType, &srcIDs, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Version{}, false, nil
		}
		return Version{}, false, err
	}
	// A version whose stored JSON will not parse is reported as missing rather
	// than returned empty-but-valid. Silently yielding a skill with no content
	// is the worst of the three outcomes: the caller injects nothing into the
	// prompt and has no way to tell that from a skill that legitimately says
	// nothing, so a corrupt skill looks exactly like a working one.
	if err := json.Unmarshal([]byte(content), &v.Content); err != nil {
		return Version{}, false, fmt.Errorf("skill version %s has unreadable content: %w", v.ID, err)
	}
	if err := json.Unmarshal([]byte(srcIDs), &v.SourceIDs); err != nil {
		return Version{}, false, fmt.Errorf("skill version %s has unreadable source ids: %w", v.ID, err)
	}
	return v, true, nil
}

func orDefault(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

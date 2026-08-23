package govpolicy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists governor policies and their evaluations.
type Store struct {
	db db.DBTX
}

// NewStore returns a policy store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

// Create inserts a candidate policy and returns it.
func (s *Store) Create(ctx context.Context, p Policy) (Policy, error) {
	if p.ID == "" {
		p.ID = ids.New()
	}
	if p.State == "" {
		p.State = StateCandidate
	}
	if p.PolicyJSON == "" {
		p.PolicyJSON = "{}"
	}
	now := time.Now().UnixMilli()
	p.CreatedAt, p.UpdatedAt = now, now
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO governor_policies (policy_id, owner_type, owner_id, task_class, state, policy_json, created_at, updated_at)
         VALUES (?,?,?,?,?,?,?,?)`,
		p.ID, p.OwnerType, p.OwnerID, p.TaskClass, string(p.State), p.PolicyJSON, now, now); err != nil {
		return Policy{}, fmt.Errorf("failed to create policy: %w", err)
	}
	return p, nil
}

// RecordEvaluation stores a policy evaluation result.
func (s *Store) RecordEvaluation(ctx context.Context, e Evaluation) error {
	if e.ID == "" {
		e.ID = ids.New()
	}
	metrics := e.MetricsJSON
	if metrics == "" {
		metrics = "{}"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO governor_policy_evaluations (id, policy_id, baseline_policy_id, eval_run_id, result, metrics_json, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		e.ID, e.PolicyID, e.BaselinePolicyID, e.EvalRunID, string(e.Result), metrics, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("failed to record policy evaluation: %w", err)
	}
	return nil
}

// HasPassingEvaluation reports whether a policy has a passing evaluation.
func (s *Store) HasPassingEvaluation(ctx context.Context, policyID string) (bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM governor_policy_evaluations WHERE policy_id = ? AND result = 'pass'`, policyID)
	var n int
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// SetState transitions a policy's state.
func (s *Store) SetState(ctx context.Context, policyID string, state State) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE governor_policies SET state = ?, updated_at = ? WHERE policy_id = ?`,
		string(state), time.Now().UnixMilli(), policyID)
	return err
}

// Get returns a policy by id.
func (s *Store) Get(ctx context.Context, id string) (Policy, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT policy_id, owner_type, owner_id, task_class, state, policy_json, created_at, updated_at
         FROM governor_policies WHERE policy_id = ?`, id)
	p, ok, err := scanOpt(row)
	if err != nil {
		return Policy{}, err
	}
	if !ok {
		return Policy{}, fmt.Errorf("policy %s not found", id)
	}
	return p, nil
}

// ListByState returns policies in a state, newest first.
func (s *Store) ListByState(ctx context.Context, state State) ([]Policy, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT policy_id, owner_type, owner_id, task_class, state, policy_json, created_at, updated_at
         FROM governor_policies WHERE state = ? ORDER BY updated_at DESC`, string(state))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Policy
	for rows.Next() {
		p, _, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanOpt(row scanner) (Policy, bool, error) {
	p, ok, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{}, false, nil
	}
	return p, ok, err
}

func scanRow(row scanner) (Policy, bool, error) {
	var p Policy
	var state string
	if err := row.Scan(&p.ID, &p.OwnerType, &p.OwnerID, &p.TaskClass, &state, &p.PolicyJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Policy{}, false, err
	}
	p.State = State(state)
	return p, true, nil
}

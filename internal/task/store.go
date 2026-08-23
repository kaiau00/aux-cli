package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
)

// Store persists tasks, specs, and budgets.
type Store struct {
	db db.DBTX
}

// NewStore returns a task store backed by the given database handle.
func NewStore(dbtx db.DBTX) *Store {
	return &Store{db: dbtx}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateTask inserts a new task.
func (s *Store) CreateTask(ctx context.Context, t Task) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, project_id, session_id, project_revision_id, profile_version_set, objective, mode, status, outcome, created_at, parent_task_id)
         VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, nullable(t.ProjectID), t.SessionID, t.ProjectRevisionID, t.ProfileVersionSet,
		t.Objective, string(t.Mode), string(t.Status), t.Outcome, t.CreatedAt, nullable(t.ParentTaskID))
	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}
	return nil
}

// SaveSpec inserts a task spec version.
func (s *Store) SaveSpec(ctx context.Context, spec Spec, sourceMessageID string, createdAt int64) error {
	content, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO task_specs (task_id, spec_version, content_json, source_message_id, compiler_version, created_at)
         VALUES (?,?,?,?,?,?)`,
		spec.TaskID, spec.SpecVersion, string(content), sourceMessageID, CompilerVersion, createdAt)
	if err != nil {
		return fmt.Errorf("failed to save task spec: %w", err)
	}
	return nil
}

// SaveBudget inserts the task budget.
func (s *Store) SaveBudget(ctx context.Context, taskID string, b Budget) error {
	policy, _ := json.Marshal(b)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_budgets (task_id, mode, max_cost, max_input_tokens, max_output_tokens, max_wall_ms, max_tool_calls, policy_json)
         VALUES (?,?,?,?,?,?,?,?)
         ON CONFLICT(task_id) DO UPDATE SET mode=excluded.mode, max_cost=excluded.max_cost,
             max_input_tokens=excluded.max_input_tokens, max_output_tokens=excluded.max_output_tokens,
             max_wall_ms=excluded.max_wall_ms, max_tool_calls=excluded.max_tool_calls, policy_json=excluded.policy_json`,
		taskID, b.Mode, b.MaxCost, b.MaxInputTokens, b.MaxOutputToken, b.MaxWallMS, b.MaxToolCalls, string(policy))
	if err != nil {
		return fmt.Errorf("failed to save task budget: %w", err)
	}
	return nil
}

// SetStatus updates a task's status and, for terminal states, its outcome and finish time.
func (s *Store) SetStatus(ctx context.Context, taskID string, status Status, outcome string, startedAt, finishedAt int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, outcome = ?,
            started_at = COALESCE(NULLIF(?,0), started_at),
            finished_at = COALESCE(NULLIF(?,0), finished_at)
         WHERE task_id = ?`,
		string(status), outcome, startedAt, finishedAt, taskID)
	if err != nil {
		return fmt.Errorf("failed to set task status: %w", err)
	}
	return nil
}

// GetTask returns a task by id.
func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE task_id = ?`, id)
	return scanTask(row)
}

// LatestSpec returns the most recent spec for a task.
func (s *Store) LatestSpec(ctx context.Context, taskID string) (Spec, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT content_json FROM task_specs WHERE task_id = ? ORDER BY spec_version DESC LIMIT 1`, taskID)
	var content string
	if err := row.Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Spec{}, false, nil
		}
		return Spec{}, false, err
	}
	var spec Spec
	if err := json.Unmarshal([]byte(content), &spec); err != nil {
		return Spec{}, false, fmt.Errorf("failed to decode spec: %w", err)
	}
	return spec, true, nil
}

// ListBySession returns tasks for a session, most recent first.
func (s *Store) ListBySession(ctx context.Context, sessionID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskCols+` FROM tasks WHERE session_id = ? ORDER BY created_at DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

// ListRecent returns the most recently created tasks across all sessions,
// newest first, capped at limit (<=0 falls back to 20).
func (s *Store) ListRecent(ctx context.Context, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

// ListByParent returns a task's child tasks (multi-repo children, or
// subagent tasks), oldest first.
func (s *Store) ListByParent(ctx context.Context, parentTaskID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskCols+` FROM tasks WHERE parent_task_id = ? ORDER BY created_at ASC`, parentTaskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

const taskCols = `task_id, project_id, session_id, project_revision_id, profile_version_set,
    objective, mode, status, outcome, created_at, started_at, finished_at, parent_task_id`

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (Task, error) {
	var t Task
	var projectID, parentTaskID sql.NullString
	var startedAt, finishedAt sql.NullInt64
	var mode, status string
	if err := row.Scan(&t.ID, &projectID, &t.SessionID, &t.ProjectRevisionID, &t.ProfileVersionSet,
		&t.Objective, &mode, &status, &t.Outcome, &t.CreatedAt, &startedAt, &finishedAt, &parentTaskID); err != nil {
		return Task{}, err
	}
	t.ProjectID = projectID.String
	t.Mode = Mode(mode)
	t.Status = Status(status)
	t.StartedAt = startedAt.Int64
	t.FinishedAt = finishedAt.Int64
	t.ParentTaskID = parentTaskID.String
	return t, nil
}

package validation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Store persists validation runs and evidence.
type Store struct {
	db db.DBTX
}

// NewStore returns a validation store.
func NewStore(dbtx db.DBTX) *Store { return &Store{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertRun records a validation run.
func (s *Store) InsertRun(ctx context.Context, r Run) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO validation_runs (validation_run_id, task_id, intent_id, validator_type, command, command_hash, input_fingerprint, status, started_at, finished_at, exit_code, duration_ms, output_artifact_id, created_at)
         VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, nullable(r.TaskID), r.IntentID, r.ValidatorType, r.Command, r.CommandHash, r.InputFingerprint,
		string(r.Status), r.StartedAt, nullOrInt(r.FinishedAt), r.ExitCode, r.DurationMS, nullable(r.OutputArtifactID), r.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert validation run: %w", err)
	}
	return nil
}

// CachedPass returns a prior passing run for the same command AND input
// fingerprint (roadmapplan.md §14.3: never reuse across changed inputs).
func (s *Store) CachedPass(ctx context.Context, commandHash, inputFingerprint string) (Run, bool, error) {
	if inputFingerprint == "" {
		return Run{}, false, nil // no fingerprint => cannot safely reuse
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+runCols+` FROM validation_runs
         WHERE command_hash = ? AND input_fingerprint = ? AND status = 'passed'
         ORDER BY created_at DESC LIMIT 1`, commandHash, inputFingerprint)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	return r, true, nil
}

// InsertEvidence attaches evidence to an acceptance criterion.
func (s *Store) InsertEvidence(ctx context.Context, e Evidence) error {
	if e.ID == "" {
		e.ID = ids.New()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO validation_evidence (id, task_id, criterion_id, validation_run_id, evidence_type, summary, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		e.ID, nullable(e.TaskID), e.CriterionID, e.ValidationRunID, e.EvidenceType, e.Summary, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert evidence: %w", err)
	}
	return nil
}

// RunsForTask returns validation runs for a task, newest first.
func (s *Store) RunsForTask(ctx context.Context, taskID string) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM validation_runs WHERE task_id = ? ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EvidenceForTask returns evidence rows for a task.
func (s *Store) EvidenceForTask(ctx context.Context, taskID string) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, criterion_id, validation_run_id, evidence_type, summary, created_at
         FROM validation_evidence WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Evidence
	for rows.Next() {
		var e Evidence
		var taskID sql.NullString
		if err := rows.Scan(&e.ID, &taskID, &e.CriterionID, &e.ValidationRunID, &e.EvidenceType, &e.Summary, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.TaskID = taskID.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// HashCommand returns a stable hash of a command string.
func HashCommand(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:])
}

const runCols = `validation_run_id, task_id, intent_id, validator_type, command, command_hash, input_fingerprint, status, started_at, finished_at, exit_code, duration_ms, output_artifact_id, created_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanRun(row scanner) (Run, error) {
	var r Run
	var taskID, artifactID sql.NullString
	var finishedAt sql.NullInt64
	var status string
	if err := row.Scan(&r.ID, &taskID, &r.IntentID, &r.ValidatorType, &r.Command, &r.CommandHash, &r.InputFingerprint,
		&status, &r.StartedAt, &finishedAt, &r.ExitCode, &r.DurationMS, &artifactID, &r.CreatedAt); err != nil {
		return Run{}, err
	}
	r.TaskID = taskID.String
	r.OutputArtifactID = artifactID.String
	r.FinishedAt = finishedAt.Int64
	r.Status = Status(status)
	return r, nil
}

func nullOrInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

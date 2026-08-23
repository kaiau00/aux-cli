package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Experiment defines a control-vs-variant comparison (roadmapplan.md §12.1).
type Experiment struct {
	ID         string
	ProjectID  string
	Name       string
	Hypothesis string
	Status     string
	ConfigJSON string
	CreatedAt  int64
}

// Run is a single recorded evaluation run within an experiment.
type Run struct {
	ID           string
	ExperimentID string
	EvalCaseID   string
	Variant      string
	Status       string
	MetricsJSON  string
	CreatedAt    int64
}

// ExperimentStore persists experiments and their eval runs.
type ExperimentStore struct {
	db db.DBTX
}

// NewExperimentStore returns an experiment store.
func NewExperimentStore(dbtx db.DBTX) *ExperimentStore { return &ExperimentStore{db: dbtx} }

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateExperiment inserts an experiment.
func (s *ExperimentStore) CreateExperiment(ctx context.Context, e Experiment) (Experiment, error) {
	if e.ID == "" {
		e.ID = ids.New()
	}
	e.CreatedAt = time.Now().UnixMilli()
	if e.Status == "" {
		e.Status = "completed"
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO experiments (experiment_id, project_id, name, hypothesis, status, config_json, created_at)
         VALUES (?,?,?,?,?,?,?)`,
		e.ID, nullable(e.ProjectID), e.Name, e.Hypothesis, e.Status, orDefault(e.ConfigJSON), e.CreatedAt); err != nil {
		return Experiment{}, fmt.Errorf("failed to create experiment: %w", err)
	}
	return e, nil
}

// RecordRun inserts an eval run.
func (s *ExperimentStore) RecordRun(ctx context.Context, r Run) error {
	if r.ID == "" {
		r.ID = ids.New()
	}
	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO eval_runs (eval_run_id, experiment_id, eval_case_id, variant, status, started_at, finished_at, metrics_json, created_at)
         VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ExperimentID, r.EvalCaseID, r.Variant, orStatus(r.Status), now, now, orDefault(r.MetricsJSON), now); err != nil {
		return fmt.Errorf("failed to record eval run: %w", err)
	}
	return nil
}

// ListRuns returns an experiment's eval runs.
func (s *ExperimentStore) ListRuns(ctx context.Context, experimentID string) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT eval_run_id, experiment_id, eval_case_id, variant, status, metrics_json, created_at
         FROM eval_runs WHERE experiment_id = ? ORDER BY created_at ASC`, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.ExperimentID, &r.EvalCaseID, &r.Variant, &r.Status, &r.MetricsJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListExperiments returns a project's experiments, most recent first, for
// history/optimization surfaces such as the dashboard (roadmapplan.md §13.14).
// Ties in created_at (same millisecond) break on experiment_id, which is
// itself time-ordered (UUIDv7, see internal/ids), so ordering stays
// deterministic and creation-order-consistent even for rapid successive
// experiments.
func (s *ExperimentStore) ListExperiments(ctx context.Context, projectID string) ([]Experiment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT experiment_id, project_id, name, hypothesis, status, config_json, created_at
         FROM experiments WHERE project_id IS ? ORDER BY created_at DESC, experiment_id DESC`, nullable(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Experiment
	for rows.Next() {
		var e Experiment
		var pid sql.NullString
		if err := rows.Scan(&e.ID, &pid, &e.Name, &e.Hypothesis, &e.Status, &e.ConfigJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ProjectID = pid.String
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetExperiment returns an experiment by id.
func (s *ExperimentStore) GetExperiment(ctx context.Context, id string) (Experiment, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT experiment_id, project_id, name, hypothesis, status, config_json, created_at FROM experiments WHERE experiment_id = ?`, id)
	var e Experiment
	var projectID sql.NullString
	if err := row.Scan(&e.ID, &projectID, &e.Name, &e.Hypothesis, &e.Status, &e.ConfigJSON, &e.CreatedAt); err != nil {
		return Experiment{}, err
	}
	e.ProjectID = projectID.String
	return e, nil
}

// RunCompilerExperiment runs the compatibility-vs-paging comparison over the
// baseline fixtures and persists the experiment and one eval run per fixture. It
// is deterministic and makes no provider calls (roadmapplan.md §12.2
// counterfactual replay).
func RunCompilerExperiment(ctx context.Context, store *ExperimentStore, projectID string) (Experiment, []CompilerResult, error) {
	results := RunBaseline()
	exp, err := store.CreateExperiment(ctx, Experiment{
		ProjectID:  projectID,
		Name:       "prompt-compiler: compatibility vs paging",
		Hypothesis: "demand paging reduces uncached input on repeated reads without losing content",
		Status:     "completed",
	})
	if err != nil {
		return Experiment{}, nil, err
	}
	for _, r := range results {
		metrics, _ := json.Marshal(map[string]any{
			"controlTokens": r.ControlTokens,
			"variantTokens": r.VariantTokens,
			"savedTokens":   r.SavedTokens,
			"savedPercent":  r.SavedPercent,
			"lossless":      r.OutcomePreserved,
		})
		status := "pass"
		if !r.OutcomePreserved {
			status = "fail" // content loss is a hard failure
		}
		if err := store.RecordRun(ctx, Run{
			ExperimentID: exp.ID, EvalCaseID: r.Fixture, Variant: "paging",
			Status: status, MetricsJSON: string(metrics),
		}); err != nil {
			return Experiment{}, nil, err
		}
	}
	return exp, results, nil
}

func orDefault(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

func orStatus(s string) string {
	if s == "" {
		return "completed"
	}
	return s
}

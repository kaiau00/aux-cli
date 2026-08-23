package eval

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

func TestCompareImprovement(t *testing.T) {
	baseline := RunMetrics{TaskID: "b", Cost: 1.0, ValidatedCriteria: 2, ChangesPerDollar: 2.0}
	variant := RunMetrics{TaskID: "v", Cost: 1.0, ValidatedCriteria: 3, ChangesPerDollar: 3.0}
	c := Compare(baseline, variant)
	if !c.Improved {
		t.Fatal("variant with higher changes-per-dollar should be an improvement")
	}
	if c.Delta != 1.0 {
		t.Fatalf("delta = %v, want 1.0", c.Delta)
	}
}

func TestCompareNoImprovement(t *testing.T) {
	baseline := RunMetrics{ChangesPerDollar: 3.0}
	variant := RunMetrics{ChangesPerDollar: 2.0}
	if Compare(baseline, variant).Improved {
		t.Fatal("a worse variant must not count as improvement")
	}
}

func TestCompareUnknownCostNeverImproves(t *testing.T) {
	// An unmeasurable ratio must never look favorable (§9.6).
	baseline := RunMetrics{ChangesPerDollar: 1.0}
	variant := RunMetrics{ChangesPerDollar: 5.0, CostUnknown: true}
	if Compare(baseline, variant).Improved {
		t.Fatal("unknown-cost variant must not count as improvement")
	}
}

func TestChangesPerDollarGuards(t *testing.T) {
	if got := changesPerDollar(3, 0, false); got != 0 {
		t.Fatalf("zero cost should yield 0, got %v", got)
	}
	if got := changesPerDollar(3, 1.5, true); got != 0 {
		t.Fatalf("unknown cost should yield 0, got %v", got)
	}
	if got := changesPerDollar(3, 1.5, false); got != 2.0 {
		t.Fatalf("3 changes / $1.5 = 2.0, got %v", got)
	}
}

func TestMetricsReadsRecordedRun(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	tasks := task.NewStore(conn)
	if err := tasks.CreateTask(ctx, task.Task{
		ID: "t1", SessionID: "s", Objective: "x", Mode: task.ModeImplementation,
		Status: task.StatusCompleted, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := tasks.SaveSpec(ctx, task.Spec{
		TaskID:             "t1",
		AcceptanceCriteria: []task.Criterion{{ID: "c1", Description: "tests pass"}},
	}, "", 1); err != nil {
		t.Fatalf("SaveSpec: %v", err)
	}

	cpStore := checkpoint.NewStore(conn)
	artifacts := artifact.NewService(artifact.NewFSBackend(t.TempDir()), artifact.NewStore(conn))
	cps := checkpoint.NewService(cpStore, artifacts, nil)
	if _, err := cps.Create(ctx, "t1", "", "cp", "", []checkpoint.FileChange{
		{Path: "a.go", After: []byte("x"), Operation: checkpoint.OpAdd},
		{Path: "b.go", Before: []byte("y"), After: []byte("z"), Operation: checkpoint.OpModify},
	}); err != nil {
		t.Fatalf("checkpoint Create: %v", err)
	}

	stores := ABStores{
		Tasks:       tasks,
		Validations: validation.NewService(validation.NewStore(conn), nil),
		Ledger:      cost.NewService(conn),
		Checkpoints: cpStore,
	}
	m, err := stores.Metrics(ctx, "t1")
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.ChangedFiles != 2 {
		t.Fatalf("changed files = %d, want 2", m.ChangedFiles)
	}
	// No validation run recorded and no cost -> unmeasurable ratio stays 0.
	if m.ValidatedCriteria != 0 || m.ChangesPerDollar != 0 {
		t.Fatalf("without validation/cost, metric should be 0: %+v", m)
	}
}

func TestCompareAndRecordPersistsAComparableRun(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	tasks := task.NewStore(conn)
	for _, id := range []string{"baseline-1", "variant-1"} {
		if err := tasks.CreateTask(ctx, task.Task{
			ID: id, SessionID: "s", Objective: "x", Mode: task.ModeImplementation,
			Status: task.StatusCompleted, CreatedAt: 1,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}

	stores := ABStores{
		Tasks:       tasks,
		Validations: validation.NewService(validation.NewStore(conn), nil),
		Ledger:      cost.NewService(conn),
		Checkpoints: checkpoint.NewStore(conn),
	}
	experiments := NewExperimentStore(conn)

	c, err := stores.CompareAndRecord(ctx, experiments, "", "governed vs baseline", "baseline-1", "variant-1")
	if err != nil {
		t.Fatalf("CompareAndRecord: %v", err)
	}
	if c.Baseline.TaskID != "baseline-1" || c.Variant.TaskID != "variant-1" {
		t.Fatalf("unexpected comparison: %+v", c)
	}

	// The comparison must now be durably visible to a dashboard reading
	// experiments/runs, not just returned to the caller.
	exps, err := experiments.ListExperiments(ctx, "")
	if err != nil {
		t.Fatalf("ListExperiments: %v", err)
	}
	if len(exps) != 1 || exps[0].Name != "governed vs baseline" {
		t.Fatalf("expected one persisted experiment named %q, got %+v", "governed vs baseline", exps)
	}

	runs, err := experiments.ListRuns(ctx, exps[0].ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected one run recording the comparison, got %d", len(runs))
	}
	got, ok := ParseComparison(runs[0].MetricsJSON)
	if !ok {
		t.Fatalf("run metrics did not parse as a Comparison: %q", runs[0].MetricsJSON)
	}
	if got.Baseline.TaskID != "baseline-1" || got.Variant.TaskID != "variant-1" {
		t.Fatalf("persisted comparison mismatch: %+v", got)
	}
}

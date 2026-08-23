package viewmodel_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eval"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestOptimizationViewListsExperimentsAndPolicies(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	expStore := eval.NewExperimentStore(conn)
	events := eventstore.NewService(conn)
	policySvc := govpolicy.NewService(govpolicy.NewStore(conn), events)

	exp, _, err := eval.RunCompilerExperiment(ctx, expStore, "proj-1")
	if err != nil {
		t.Fatalf("RunCompilerExperiment: %v", err)
	}
	_ = exp

	if _, err := policySvc.Candidate(ctx, "project", "proj-1", "bug_diagnosis", `{"mode":"efficient"}`); err != nil {
		t.Fatalf("policy Candidate: %v", err)
	}

	stores := viewmodel.OptimizationStores{Experiments: expStore, Policies: policySvc}
	view, err := stores.OptimizationView(ctx, "proj-1")
	if err != nil {
		t.Fatalf("OptimizationView: %v", err)
	}
	if len(view.Experiments) != 1 || view.Experiments[0].Runs != 3 {
		t.Fatalf("expected 1 experiment with 3 runs, got %+v", view.Experiments)
	}
	if len(view.CandidatePolicies) != 1 {
		t.Fatalf("expected 1 candidate policy, got %+v", view.CandidatePolicies)
	}
}

func TestOptimizationViewSurfacesABComparison(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	expStore := eval.NewExperimentStore(conn)

	tasks := task.NewStore(conn)
	for _, id := range []string{"baseline-1", "variant-1"} {
		if err := tasks.CreateTask(ctx, task.Task{
			ID: id, SessionID: "s", Objective: "x", Mode: task.ModeImplementation,
			Status: task.StatusCompleted, CreatedAt: 1,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}
	stores := eval.ABStores{
		Tasks:       tasks,
		Validations: validation.NewService(validation.NewStore(conn), nil),
		Ledger:      cost.NewService(conn),
		Checkpoints: checkpoint.NewStore(conn),
	}
	if _, err := stores.CompareAndRecord(ctx, expStore, "proj-1", "governed vs baseline", "baseline-1", "variant-1"); err != nil {
		t.Fatalf("CompareAndRecord: %v", err)
	}

	view, err := viewmodel.OptimizationStores{Experiments: expStore}.OptimizationView(ctx, "proj-1")
	if err != nil {
		t.Fatalf("OptimizationView: %v", err)
	}
	if len(view.Experiments) != 1 {
		t.Fatalf("expected 1 experiment, got %+v", view.Experiments)
	}
	got := view.Experiments[0].Comparison
	if got == nil {
		t.Fatal("expected the A/B comparison to be surfaced on the experiment")
	}
	if got.Baseline.TaskID != "baseline-1" || got.Variant.TaskID != "variant-1" {
		t.Fatalf("unexpected comparison: %+v", got)
	}
}

func TestOptimizationViewCompilerExperimentHasNoComparison(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	expStore := eval.NewExperimentStore(conn)

	if _, _, err := eval.RunCompilerExperiment(ctx, expStore, "proj-1"); err != nil {
		t.Fatalf("RunCompilerExperiment: %v", err)
	}

	view, err := viewmodel.OptimizationStores{Experiments: expStore}.OptimizationView(ctx, "proj-1")
	if err != nil {
		t.Fatalf("OptimizationView: %v", err)
	}
	if len(view.Experiments) != 1 {
		t.Fatalf("expected 1 experiment, got %+v", view.Experiments)
	}
	if view.Experiments[0].Comparison != nil {
		t.Fatalf("a fixture-run experiment must not report a comparison, got %+v", view.Experiments[0].Comparison)
	}
}

func TestOptimizationViewWithNilReadersIsEmpty(t *testing.T) {
	view, err := viewmodel.OptimizationStores{}.OptimizationView(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("OptimizationView: %v", err)
	}
	if len(view.Experiments) != 0 || len(view.ActivePolicies) != 0 {
		t.Fatalf("expected empty view with nil readers, got %+v", view)
	}
}

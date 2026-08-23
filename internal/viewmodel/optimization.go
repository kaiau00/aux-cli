package viewmodel

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/eval"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
)

// ExperimentVM is one experiment for the dashboard's Optimization view.
type ExperimentVM struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Hypothesis string `json:"hypothesis,omitempty"`
	Status     string `json:"status"`
	Runs       int    `json:"runs"`
	CreatedAt  int64  `json:"createdAt"`
	// Comparison is the governed-vs-baseline / skill-vs-baseline result when
	// this experiment came from `aux eval ab` (eval.ABStores.CompareAndRecord),
	// nil for a RunCompilerExperiment fixture run — never a zero-value
	// Comparison, which could be mistaken for a real (and unimproved) result.
	Comparison *eval.Comparison `json:"comparison,omitempty"`
}

// PolicyVM is one governed-cost policy for the dashboard's Optimization view.
type PolicyVM struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType,omitempty"`
	TaskClass string `json:"taskClass,omitempty"`
	State     string `json:"state"`
}

// OptimizationVM is the project's optimization history — evaluated
// experiments and governed-cost policies (roadmapplan.md §13.14 item 7, §12.1,
// §9.7). This is where "governed vs baseline" and "skill vs baseline" results
// computed by `aux eval ab` become visible once a user has run them.
type OptimizationVM struct {
	Experiments       []ExperimentVM `json:"experiments"`
	ActivePolicies    []PolicyVM     `json:"activePolicies"`
	CandidatePolicies []PolicyVM     `json:"candidatePolicies"`
}

// BuildExperimentVM projects an experiment for display. If any of runs holds
// an A/B comparison (eval.ABStores.CompareAndRecord's ComparisonVariant), it
// is surfaced on the result so the dashboard can show the governed/skill vs
// baseline numbers, not just an experiment name and run count.
func BuildExperimentVM(e eval.Experiment, runs []eval.Run) ExperimentVM {
	vm := ExperimentVM{ID: e.ID, Name: e.Name, Hypothesis: e.Hypothesis, Status: e.Status, Runs: len(runs), CreatedAt: e.CreatedAt}
	for _, r := range runs {
		if r.Variant != eval.ComparisonVariant {
			continue
		}
		if c, ok := eval.ParseComparison(r.MetricsJSON); ok {
			vm.Comparison = &c
			break
		}
	}
	return vm
}

// BuildPolicyVM projects a governed-cost policy for display.
func BuildPolicyVM(p govpolicy.Policy) PolicyVM {
	return PolicyVM{ID: p.ID, OwnerType: p.OwnerType, TaskClass: p.TaskClass, State: string(p.State)}
}

// ExperimentReader lists a project's experiments and their runs.
type ExperimentReader interface {
	ListExperiments(ctx context.Context, projectID string) ([]eval.Experiment, error)
	ListRuns(ctx context.Context, experimentID string) ([]eval.Run, error)
}

// PolicyReader lists governed-cost policies.
type PolicyReader interface {
	Active(ctx context.Context) ([]govpolicy.Policy, error)
	Candidates(ctx context.Context) ([]govpolicy.Policy, error)
}

// OptimizationStores bundles the read dependencies needed to assemble an
// OptimizationVM.
type OptimizationStores struct {
	Experiments ExperimentReader
	Policies    PolicyReader
}

// OptimizationView assembles a project's optimization history for display.
func (s OptimizationStores) OptimizationView(ctx context.Context, projectID string) (OptimizationVM, error) {
	var view OptimizationVM
	if s.Experiments != nil {
		if experiments, err := s.Experiments.ListExperiments(ctx, projectID); err == nil {
			for _, e := range experiments {
				var runs []eval.Run
				runs, _ = s.Experiments.ListRuns(ctx, e.ID)
				view.Experiments = append(view.Experiments, BuildExperimentVM(e, runs))
			}
		}
	}
	if s.Policies != nil {
		if active, err := s.Policies.Active(ctx); err == nil {
			for _, p := range active {
				view.ActivePolicies = append(view.ActivePolicies, BuildPolicyVM(p))
			}
		}
		if candidates, err := s.Policies.Candidates(ctx); err == nil {
			for _, p := range candidates {
				view.CandidatePolicies = append(view.CandidatePolicies, BuildPolicyVM(p))
			}
		}
	}
	return view, nil
}

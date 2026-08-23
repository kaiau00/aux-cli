package eval

import (
	"context"
	"encoding/json"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// ComparisonVariant marks an eval Run as holding a full A/B Comparison in its
// MetricsJSON (from CompareAndRecord), distinguishing it from a
// RunCompilerExperiment fixture run so a reader can tell the two apart.
const ComparisonVariant = "ab-comparison"

// The A/B runner measures a capability (governor policy, skill) against a
// baseline on the same preferred model (roadmapplan.md §9.6, §10.3, §16.7). The
// headline metric is accepted validated changes per dollar: validated acceptance
// criteria divided by reconciled cost. Everything here is computed from durable
// records — ledger, proof-of-done, and checkpoints — so a comparison is
// reproducible offline; only recording the two runs needs a provider.

// RunMetrics is the measured outcome of one recorded task run.
type RunMetrics struct {
	TaskID            string  `json:"taskId"`
	Cost              float64 `json:"cost"`
	CostUnknown       bool    `json:"costUnknown"`
	ValidatedCriteria int     `json:"validatedCriteria"`
	ChangedFiles      int     `json:"changedFiles"`
	ChangesPerDollar  float64 `json:"changesPerDollar"`
}

// Comparison is a baseline-vs-variant result on the same model.
type Comparison struct {
	Baseline RunMetrics `json:"baseline"`
	Variant  RunMetrics `json:"variant"`
	Delta    float64    `json:"delta"`
	Improved bool       `json:"improved"`
}

// changesPerDollar is validated changes divided by cost, or 0 when cost is
// unknown or non-positive (an unmeasurable ratio must never look favorable).
func changesPerDollar(validated int, costAmount float64, costUnknown bool) float64 {
	if costUnknown || costAmount <= 0 {
		return 0
	}
	return float64(validated) / costAmount
}

// Compare decides whether the variant improved changes-per-dollar over the
// baseline. It is conservative: an unknown-cost run on either side never counts
// as an improvement, since the ratio cannot be trusted.
func Compare(baseline, variant RunMetrics) Comparison {
	delta := variant.ChangesPerDollar - baseline.ChangesPerDollar
	improved := !baseline.CostUnknown && !variant.CostUnknown &&
		variant.ChangesPerDollar > baseline.ChangesPerDollar
	return Comparison{Baseline: baseline, Variant: variant, Delta: delta, Improved: improved}
}

// ABStores are the durable read dependencies for computing run metrics.
type ABStores struct {
	Tasks       *task.Store
	Validations *validation.Service
	Ledger      cost.Service
	Checkpoints *checkpoint.Store
}

// Metrics computes the measured outcome for a single recorded task run.
func (s ABStores) Metrics(ctx context.Context, taskID string) (RunMetrics, error) {
	totals, err := s.Ledger.TaskTotals(ctx, taskID)
	if err != nil {
		return RunMetrics{}, err
	}
	m := RunMetrics{TaskID: taskID, Cost: totals.Cost, CostUnknown: totals.CostUnknown}

	if spec, ok, serr := s.Tasks.LatestSpec(ctx, taskID); serr == nil && ok && len(spec.AcceptanceCriteria) > 0 && s.Validations != nil {
		ids := make([]string, 0, len(spec.AcceptanceCriteria))
		for _, c := range spec.AcceptanceCriteria {
			ids = append(ids, c.ID)
		}
		if states, verr := s.Validations.ProofOfDone(ctx, taskID, ids); verr == nil {
			for _, st := range states {
				if st == validation.Validated {
					m.ValidatedCriteria++
				}
			}
		}
	}

	if s.Checkpoints != nil {
		if cps, cerr := s.Checkpoints.ListByTask(ctx, taskID); cerr == nil && len(cps) > 0 {
			if entries, eerr := s.Checkpoints.Entries(ctx, cps[len(cps)-1].ID); eerr == nil {
				m.ChangedFiles = len(entries)
			}
		}
	}

	m.ChangesPerDollar = changesPerDollar(m.ValidatedCriteria, m.Cost, m.CostUnknown)
	return m, nil
}

// CompareRuns computes metrics for a baseline and variant task and compares them.
func (s ABStores) CompareRuns(ctx context.Context, baselineTaskID, variantTaskID string) (Comparison, error) {
	baseline, err := s.Metrics(ctx, baselineTaskID)
	if err != nil {
		return Comparison{}, err
	}
	variant, err := s.Metrics(ctx, variantTaskID)
	if err != nil {
		return Comparison{}, err
	}
	return Compare(baseline, variant), nil
}

// CompareAndRecord computes a baseline-vs-variant comparison via CompareRuns
// and persists it as an experiment with one run, so it durably shows up
// anywhere experiments are read (roadmapplan.md §13.14 item 7, §9.6, §10.3) —
// e.g. the dashboard's Optimization view — instead of only being printed to
// stdout by `aux eval ab`. The whole Comparison is stored as one run's
// MetricsJSON (self-contained; no baseline/variant pairing needed at read
// time), tagged ComparisonVariant so a reader can distinguish it from a
// RunCompilerExperiment fixture run.
func (s ABStores) CompareAndRecord(ctx context.Context, store *ExperimentStore, projectID, name, baselineTaskID, variantTaskID string) (Comparison, error) {
	c, err := s.CompareRuns(ctx, baselineTaskID, variantTaskID)
	if err != nil {
		return Comparison{}, err
	}
	exp, err := store.CreateExperiment(ctx, Experiment{
		ProjectID:  projectID,
		Name:       name,
		Hypothesis: "the variant improves accepted validated changes per dollar over the baseline",
		Status:     "completed",
	})
	if err != nil {
		return Comparison{}, err
	}
	metrics, err := json.Marshal(c)
	if err != nil {
		return Comparison{}, err
	}
	status := "fail"
	if c.Improved {
		status = "pass"
	}
	if err := store.RecordRun(ctx, Run{
		ExperimentID: exp.ID, EvalCaseID: variantTaskID, Variant: ComparisonVariant,
		Status: status, MetricsJSON: string(metrics),
	}); err != nil {
		return Comparison{}, err
	}
	return c, nil
}

// ParseComparison decodes a run's MetricsJSON as a Comparison. It returns
// ok=false for any run that isn't a CompareAndRecord result (e.g. a
// RunCompilerExperiment fixture run, whose MetricsJSON has a different
// shape), rather than returning a zero-value Comparison that could be
// mistaken for a real (and coincidentally unimproved) result.
func ParseComparison(metricsJSON string) (Comparison, bool) {
	var c Comparison
	if err := json.Unmarshal([]byte(metricsJSON), &c); err != nil {
		return Comparison{}, false
	}
	if c.Baseline.TaskID == "" || c.Variant.TaskID == "" {
		return Comparison{}, false
	}
	return c, true
}

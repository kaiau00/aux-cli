package cost_test

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

func TestBudgetPressureAndExhaustion(t *testing.T) {
	b := cost.DefaultBudget(cost.ModeBalanced)
	// Half the input budget consumed -> ~0.5 input pressure, not exhausted.
	p := b.Pressure(cost.Usage{InputTokens: b.MaxInputTokens / 2})
	if p.Input < 0.49 || p.Input > 0.51 {
		t.Fatalf("input pressure = %.2f, want ~0.5", p.Input)
	}
	if b.Exhausted(cost.Usage{InputTokens: b.MaxInputTokens / 2}) {
		t.Fatalf("half budget should not be exhausted")
	}
	if !b.Exhausted(cost.Usage{ToolCalls: b.MaxToolCalls}) {
		t.Fatalf("hitting the tool-call ceiling should exhaust the budget")
	}
}

func TestDefaultBudgetAllocationsSumToInput(t *testing.T) {
	b := cost.DefaultBudget(cost.ModeBalanced)
	var sum int64
	for _, v := range b.Allocations {
		sum += v
	}
	// Fractions sum to 1.0; allow rounding slack.
	if sum > b.MaxInputTokens || sum < b.MaxInputTokens-int64(len(b.Allocations)) {
		t.Fatalf("allocations (%d) should sum to ~MaxInputTokens (%d)", sum, b.MaxInputTokens)
	}
}

func TestGovernorOffIsDisabled(t *testing.T) {
	g := cost.NewGovernor(cost.GovOff)
	a := g.Assess(cost.DefaultBudget(cost.ModeBalanced), cost.Usage{ToolCalls: 999}, nil)
	if a.Enabled || len(a.Decisions) != 0 {
		t.Fatalf("off governor must be disabled and silent")
	}
}

func TestGovernorWarnsAndDegrades(t *testing.T) {
	g := cost.NewGovernor(cost.GovObserve)
	b := cost.DefaultBudget(cost.ModeBalanced)

	// 85% pressure -> warn, not exhausted.
	warn := g.Assess(b, cost.Usage{ToolCalls: int64(float64(b.MaxToolCalls) * 0.85)}, nil)
	if !warn.Enabled || warn.Exhausted {
		t.Fatalf("expected enabled, not-exhausted assessment")
	}
	if !hasAction(warn.Decisions, "warn_budget") {
		t.Fatalf("expected a warn_budget decision, got %+v", warn.Decisions)
	}

	// Over the ceiling -> exhausted with a degradation plan.
	ex := g.Assess(b, cost.Usage{ToolCalls: b.MaxToolCalls + 1}, nil)
	if !ex.Exhausted || len(ex.DegradationPlan) == 0 {
		t.Fatalf("exhausted budget should yield a degradation plan")
	}
	if !hasAction(ex.Decisions, "degrade") {
		t.Fatalf("expected a degrade decision")
	}
}

func TestDetectWaste(t *testing.T) {
	in := cost.WasteInput{
		ToolExecutions: []tools.ExecutionRecord{
			{ID: "e1", ToolName: "view", InputHash: "h1"},
			{ID: "e2", ToolName: "view", InputHash: "h1"}, // repeat
			{ID: "e3", ToolName: "grep", InputHash: "h2"}, // distinct
		},
		Artifacts: []cost.ArtifactAccess{
			{ArtifactID: "a1", ByteSize: 20000, Accessed: false}, // large & unused
			{ArtifactID: "a2", ByteSize: 20000, Accessed: true},  // used
			{ArtifactID: "a3", ByteSize: 10, Accessed: false},    // tiny
		},
		RepeatedValidation: map[string]int{"cmdA|fp": 2},
	}
	warnings := cost.DetectWaste(in)

	got := map[string]bool{}
	for _, w := range warnings {
		got[w.Detector] = true
	}
	if !got["repeated_tool_call"] {
		t.Fatalf("should detect repeated identical tool call")
	}
	if !got["unused_large_artifact"] {
		t.Fatalf("should detect large unused artifact")
	}
	if !got["repeated_validation"] {
		t.Fatalf("should detect repeated validation")
	}
	// No false positives for distinct calls / used or tiny artifacts.
	for _, w := range warnings {
		for _, ref := range w.Refs {
			if ref == "e3" || ref == "a2" || ref == "a3" {
				t.Fatalf("false positive on %s", ref)
			}
		}
	}
}

func TestCompileTrajectory(t *testing.T) {
	events := []eventstore.Event{
		{Sequence: 1, Type: eventstore.TaskCreated},
		{Sequence: 2, Type: eventstore.ModelCallStarted},
		{Sequence: 3, Type: eventstore.ToolStarted},
		{Sequence: 4, Type: eventstore.ToolFailed},
		{Sequence: 5, Type: eventstore.ModelCallCompleted},
		{Sequence: 6, Type: eventstore.TaskCompleted},
	}
	tr := cost.CompileTrajectory("t1", events, cost.Totals{Cost: 1.5})
	if tr.ModelCalls != 1 || tr.ToolCalls != 1 || tr.Failures != 1 {
		t.Fatalf("trajectory counts wrong: %+v", tr)
	}
	if tr.TotalCost != 1.5 || len(tr.Steps) != 6 {
		t.Fatalf("trajectory summary wrong: %+v", tr)
	}
	if tr.Steps[0].Kind != cost.StepState || tr.Steps[5].Kind != cost.StepOutcome {
		t.Fatalf("step classification wrong")
	}
}

func hasAction(decisions []cost.Decision, action string) bool {
	for _, d := range decisions {
		if d.Action == action {
			return true
		}
	}
	return false
}

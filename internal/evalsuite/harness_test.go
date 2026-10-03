package evalsuite

import (
	"context"
	"strings"
	"testing"
)

func TestOpenCodeMetricsSumStepFinishEvents(t *testing.T) {
	out := strings.Join([]string{
		`{"type":"step_start","part":{}}`,
		`{"type":"text","part":{"text":"working"}}`,
		`{"type":"step_finish","part":{"tokens":{"input":1000,"output":50,"reasoning":10,"cache":{"read":200,"write":100}},"cost":0.01}}`,
		`{"type":"step_finish","part":{"tokens":{"input":2000,"output":80,"reasoning":0,"cache":{"read":0,"write":0}},"cost":0.02}}`,
	}, "\n")

	u, err := OpenCodeHarness{}.Metrics(context.Background(), out)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	// Cache reads and writes count as input: they are input the model processed,
	// and excluding them would flatter whichever harness caches better.
	if u.InputTokens != 3300 {
		t.Fatalf("input tokens %d, want 3300 (1000+200+100+2000)", u.InputTokens)
	}
	if u.OutputTokens != 140 {
		t.Fatalf("output tokens %d, want 140 (50+10+80)", u.OutputTokens)
	}
	if u.Turns != 2 {
		t.Fatalf("turns %d, want 2", u.Turns)
	}
}

// No measurable output must be an error, not a zero-token run that would look
// like the cheapest in the suite.
func TestOpenCodeMetricsWithoutStepFinishIsAnError(t *testing.T) {
	u, err := OpenCodeHarness{}.Metrics(context.Background(), `{"type":"text","part":{}}`)
	if err == nil {
		t.Fatal("expected an error when nothing was measured")
	}
	if !u.CostUnknown {
		t.Fatal("an unmeasured run must be flagged")
	}
}

// A harness comparison must not silently become a model comparison.
func TestOpenCodeCommandPinsTheModel(t *testing.T) {
	cmd := OpenCodeHarness{Binary: "opencode", Model: "provider/M3"}.Command("do it")
	for _, want := range []string{"run", "--format json", "--model 'provider/M3'", "'do it'"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q: %s", want, cmd)
		}
	}
}

// Without --yes, `aux -p` denies every edit and command, and the suite would
// measure that instead of the harness.
func TestAuxHarnessApprovesActions(t *testing.T) {
	cmd := AuxHarness{Binary: "aux"}.Command("do it")
	if !strings.Contains(cmd, " --yes") {
		t.Fatalf("aux harness command must pass --yes: %s", cmd)
	}
}

func TestHarnessCommandsQuotePrompts(t *testing.T) {
	prompt := `fix Bob's "thing"`
	for _, h := range []Harness{
		AuxHarness{Binary: "aux"},
		OpenCodeHarness{Binary: "opencode"},
	} {
		if !strings.Contains(h.Command(prompt), `'\''`) {
			t.Errorf("%s did not escape the apostrophe: %s", h.Name(), h.Command(prompt))
		}
	}
}

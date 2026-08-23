package runtime_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/runtime"
	"github.com/kaiau00/aux-cli/internal/runtime/runtimetest"
)

func TestScriptedRunnerSatisfiesContract(t *testing.T) {
	runtimetest.RunnerContract(t, "scripted", func() runtime.Runner {
		return runtime.NewScriptedRunner(
			runtime.TurnResult{Assistant: message.Message{Role: message.Assistant}},
		)
	})
}

func TestScriptedRunnerReplaysInOrder(t *testing.T) {
	first := runtime.TurnResult{Assistant: message.Message{ID: "m1", Role: message.Assistant}}
	second := runtime.TurnResult{Assistant: message.Message{ID: "m2", Role: message.Assistant}}
	r := runtime.NewScriptedRunner(first, second)

	ctx := context.Background()
	t1, err := r.RunTurn(ctx, "s", nil)
	if err != nil || t1.Assistant.ID != "m1" {
		t.Fatalf("first turn = %q (err %v), want m1", t1.Assistant.ID, err)
	}
	t2, _ := r.RunTurn(ctx, "s", nil)
	if t2.Assistant.ID != "m2" {
		t.Fatalf("second turn = %q, want m2", t2.Assistant.ID)
	}
	// Exhausted script yields an empty assistant turn, not an error.
	t3, err := r.RunTurn(ctx, "s", nil)
	if err != nil || t3.Assistant.Role != message.Assistant {
		t.Fatalf("exhausted script should yield an empty assistant turn, got %+v (err %v)", t3, err)
	}
}

func TestScriptedRunnerIsAnAdapter(t *testing.T) {
	var a runtime.Adapter = runtime.NewScriptedRunner()
	if a.Name() != "scripted" {
		t.Fatalf("adapter name = %q, want scripted", a.Name())
	}
}

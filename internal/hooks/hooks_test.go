package hooks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaiau00/aux-cli/internal/hooks"
)

func TestDispatchRunsHandlersInOrder(t *testing.T) {
	r := hooks.NewRegistry()
	var order []string
	r.Register(hooks.TaskEnd, func(_ context.Context, e hooks.Event) error {
		order = append(order, "a:"+e.TaskID)
		return nil
	})
	r.Register(hooks.TaskEnd, func(_ context.Context, _ hooks.Event) error {
		order = append(order, "b")
		return nil
	})

	if err := r.Dispatch(context.Background(), hooks.Event{Point: hooks.TaskEnd, TaskID: "t1"}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(order) != 2 || order[0] != "a:t1" || order[1] != "b" {
		t.Fatalf("handlers ran out of order: %v", order)
	}
}

func TestPreHookVetoStopsChain(t *testing.T) {
	r := hooks.NewRegistry()
	veto := errors.New("blocked")
	ran := false
	r.Register(hooks.ToolPre, func(_ context.Context, _ hooks.Event) error { return veto })
	r.Register(hooks.ToolPre, func(_ context.Context, _ hooks.Event) error { ran = true; return nil })

	if err := r.Dispatch(context.Background(), hooks.Event{Point: hooks.ToolPre, Tool: "edit"}); !errors.Is(err, veto) {
		t.Fatalf("expected veto error, got %v", err)
	}
	if ran {
		t.Fatal("a veto must stop the handler chain")
	}
}

func TestDispatchOnlyMatchingPoint(t *testing.T) {
	r := hooks.NewRegistry()
	fired := 0
	r.Register(hooks.TaskBegin, func(_ context.Context, _ hooks.Event) error { fired++; return nil })

	_ = r.Dispatch(context.Background(), hooks.Event{Point: hooks.TaskEnd})
	if fired != 0 {
		t.Fatal("a handler must not fire for a different point")
	}
	_ = r.Dispatch(context.Background(), hooks.Event{Point: hooks.TaskBegin})
	if fired != 1 {
		t.Fatalf("handler should fire once for its point, got %d", fired)
	}
}

func TestNilRegistryIsNoOp(t *testing.T) {
	var r *hooks.Registry
	if err := r.Dispatch(context.Background(), hooks.Event{Point: hooks.TaskEnd}); err != nil {
		t.Fatalf("nil registry dispatch should be a no-op, got %v", err)
	}
	r.Register(hooks.TaskEnd, func(_ context.Context, _ hooks.Event) error { return nil })
	if r.Count(hooks.TaskEnd) != 0 {
		t.Fatal("nil registry register should be a no-op")
	}
}

package task_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/task"
)

func TestFinishDispatchesTaskEndHook(t *testing.T) {
	conn := dbtest.New(t)
	store := task.NewStore(conn)
	reg := hooks.NewRegistry()

	var got hooks.Event
	reg.Register(hooks.TaskEnd, func(_ context.Context, e hooks.Event) error {
		got = e
		return nil
	})

	coord := task.NewCoordinator(nil, nil, store, eventstore.NewService(conn), "").WithHooks(reg)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	if err := store.CreateTask(ctx, task.Task{
		ID: "t1", SessionID: "sess-1", Objective: "x", Mode: task.ModeImplementation,
		Status: task.StatusRunning, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	coord.Finish(ctx, "t1", "done")

	if got.Point != hooks.TaskEnd || got.TaskID != "t1" || got.Outcome != "done" {
		t.Fatalf("TaskEnd hook not dispatched correctly: %+v", got)
	}
}

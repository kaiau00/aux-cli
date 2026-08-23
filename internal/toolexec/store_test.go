package toolexec_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/toolexec"
)

func TestStoreRoundTrip(t *testing.T) {
	store := toolexec.NewStore(dbtest.New(t))
	ctx := context.Background()

	id := ids.New()
	rec := tools.ExecutionRecord{
		ID: id,
		Correlation: tools.Correlation{
			SessionID: "s", TaskID: "task-1", TurnID: "turn-1", ModelCallID: "call-1",
		},
		ToolCallID: "tc-1",
		ToolName:   "grep",
		InputHash:  "abc123",
		Status:     tools.ExecStarted,
		StartedAt:  1000,
	}
	if err := store.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rec.Status = tools.ExecCompleted
	rec.FinishedAt = 1200
	rec.LatencyMS = 200
	rec.ResponseBytes = 4096
	if err := store.Finish(ctx, rec); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != tools.ExecCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if got.ToolName != "grep" || got.InputHash != "abc123" {
		t.Fatalf("identity not persisted: %+v", got)
	}
	if got.Correlation.TaskID != "task-1" || got.Correlation.ModelCallID != "call-1" {
		t.Fatalf("correlation not persisted: %+v", got.Correlation)
	}
	if got.LatencyMS != 200 || got.ResponseBytes != 4096 {
		t.Fatalf("metrics not persisted: %+v", got)
	}

	byTask, err := store.ListByTask(ctx, "task-1")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(byTask) != 1 || byTask[0].ID != id {
		t.Fatalf("ListByTask returned %d rows", len(byTask))
	}
}

func TestStoreRecordsErrorFlag(t *testing.T) {
	store := toolexec.NewStore(dbtest.New(t))
	ctx := context.Background()
	id := ids.New()
	rec := tools.ExecutionRecord{ID: id, ToolName: "bash", Status: tools.ExecStarted, StartedAt: 1}
	if err := store.Insert(ctx, rec); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rec.Status = tools.ExecError
	rec.IsError = true
	if err := store.Finish(ctx, rec); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.IsError {
		t.Fatalf("is_error not persisted")
	}
}

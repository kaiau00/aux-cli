package task_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/task"
)

// buildCheckpointCoordinator wires a coordinator with history + checkpoint
// capture against a fresh database, returning a real session id (the files
// table has a foreign key to sessions).
func buildCheckpointCoordinator(t *testing.T) (*task.Coordinator, *task.Store, history.Service, *checkpoint.Store, string) {
	t.Helper()
	conn := dbtest.New(t)
	q := db.New(conn)
	store := task.NewStore(conn)
	events := eventstore.NewService(conn)
	files := history.NewService(q, conn)
	cpStore := checkpoint.NewStore(conn)
	artifacts := artifact.NewService(artifact.NewFSBackend(t.TempDir()), artifact.NewStore(conn))
	cps := checkpoint.NewService(cpStore, artifacts, events)

	sess, err := session.NewService(q).Create(context.Background(), "test session")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	coord := task.NewCoordinator(nil, nil, store, events, "").WithCheckpoints(files, cps)
	return coord, store, files, cpStore, sess.ID
}

func TestFinishCapturesCheckpointFromHistory(t *testing.T) {
	coord, store, files, cpStore, sessID := buildCheckpointCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, sessID)

	if err := store.CreateTask(ctx, task.Task{
		ID: "t1", SessionID: sessID, Objective: "edit foo", Mode: task.ModeImplementation,
		Status: task.StatusRunning, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// The edit tool records the original then the edited content.
	if _, err := files.Create(ctx, sessID, "foo.go", "old body"); err != nil {
		t.Fatalf("history Create: %v", err)
	}
	if _, err := files.CreateVersion(ctx, sessID, "foo.go", "new body"); err != nil {
		t.Fatalf("history CreateVersion: %v", err)
	}

	coord.Finish(ctx, "t1", "done")

	cps, err := cpStore.ListByTask(ctx, "t1")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(cps) != 1 {
		t.Fatalf("expected 1 checkpoint after completion, got %d", len(cps))
	}
	entries, err := cpStore.Entries(ctx, cps[0].ID)
	if err != nil {
		t.Fatalf("Entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 changed file, got %d", len(entries))
	}
	if entries[0].Path != "foo.go" || entries[0].Operation != checkpoint.OpModify {
		t.Fatalf("unexpected entry: %+v", entries[0])
	}
	// A modify must carry both before and after content artifacts.
	if entries[0].BeforeArtifactID == "" || entries[0].AfterArtifactID == "" {
		t.Fatalf("modify entry should have before and after artifacts: %+v", entries[0])
	}
}

func TestFinishNoCheckpointWhenNothingChanged(t *testing.T) {
	coord, store, files, cpStore, sessID := buildCheckpointCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, sessID)

	if err := store.CreateTask(ctx, task.Task{
		ID: "t2", SessionID: sessID, Objective: "no-op", Mode: task.ModeImplementation,
		Status: task.StatusRunning, CreatedAt: 1,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Only a single recorded version (a read with no edit) → net-unchanged.
	if _, err := files.Create(ctx, sessID, "foo.go", "same"); err != nil {
		t.Fatalf("history Create: %v", err)
	}

	coord.Finish(ctx, "t2", "done")

	cps, err := cpStore.ListByTask(ctx, "t2")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(cps) != 0 {
		t.Fatalf("expected no checkpoint when nothing changed, got %d", len(cps))
	}
}

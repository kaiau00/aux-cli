package mutationcp_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/mutationcp"
	"github.com/kaiau00/aux-cli/internal/session"
)

func setup(t *testing.T) (*mutationcp.Checkpointer, history.Service, *checkpoint.Store, string) {
	t.Helper()
	conn := dbtest.New(t)
	q := db.New(conn)
	files := history.NewService(q, conn)
	cpStore := checkpoint.NewStore(conn)
	artifacts := artifact.NewService(artifact.NewFSBackend(t.TempDir()), artifact.NewStore(conn))
	cpSvc := checkpoint.NewService(cpStore, artifacts, nil)

	sess, err := session.NewService(q).Create(context.Background(), "s")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cp := mutationcp.New(files, mutationcp.StoreAdapter{Store: cpStore, Service: cpSvc})
	return cp, files, cpStore, sess.ID
}

func TestFirstMutationCreatesOneCheckpoint(t *testing.T) {
	cp, files, cpStore, sessID := setup(t)
	ctx := context.Background()

	// The edit tool recorded the original then the edited content.
	if _, err := files.Create(ctx, sessID, "foo.go", "old"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := files.CreateVersion(ctx, sessID, "foo.go", "new"); err != nil {
		t.Fatalf("CreateVersion: %v", err)
	}

	// First mutating tool -> one checkpoint.
	if err := cp.OnToolCompleted(ctx, "t1", sessID, "edit"); err != nil {
		t.Fatalf("OnToolCompleted: %v", err)
	}
	// A subsequent mutation must NOT create a second first-mutation checkpoint.
	if err := cp.OnToolCompleted(ctx, "t1", sessID, "write"); err != nil {
		t.Fatalf("OnToolCompleted 2: %v", err)
	}

	cps, err := cpStore.ListByTask(ctx, "t1")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(cps) != 1 || cps[0].Label != "first-mutation" {
		t.Fatalf("expected exactly one first-mutation checkpoint, got %+v", cps)
	}
}

func TestNonMutatingToolCreatesNothing(t *testing.T) {
	cp, files, cpStore, sessID := setup(t)
	ctx := context.Background()
	_, _ = files.Create(ctx, sessID, "foo.go", "old")
	_, _ = files.CreateVersion(ctx, sessID, "foo.go", "new")

	if err := cp.OnToolCompleted(ctx, "t1", sessID, "view"); err != nil {
		t.Fatalf("OnToolCompleted: %v", err)
	}
	if cps, _ := cpStore.ListByTask(ctx, "t1"); len(cps) != 0 {
		t.Fatalf("a read-only tool must not checkpoint, got %d", len(cps))
	}
}

func TestIsMutatingTool(t *testing.T) {
	for _, name := range []string{"edit", "write", "patch", "Edit"} {
		if !mutationcp.IsMutatingTool(name) {
			t.Fatalf("%q should be mutating", name)
		}
	}
	for _, name := range []string{"view", "grep", "ls", ""} {
		if mutationcp.IsMutatingTool(name) {
			t.Fatalf("%q should not be mutating", name)
		}
	}
}

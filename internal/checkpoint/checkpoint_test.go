package checkpoint_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
)

func newService(t *testing.T) (*checkpoint.Service, *checkpoint.Store) {
	t.Helper()
	conn := dbtest.New(t)
	arts := artifact.NewService(artifact.NewFSBackend(t.TempDir()), artifact.NewStore(conn))
	store := checkpoint.NewStore(conn)
	return checkpoint.NewService(store, arts, eventstore.NewService(conn)), store
}

func TestCreateAndBranchShareContent(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	base, err := svc.Create(ctx, "task1", "", "base", "rev1", []checkpoint.FileChange{
		{Path: "a.go", After: []byte("package a\nvar X = 1\n"), Operation: checkpoint.OpAdd},
		{Path: "b.go", After: []byte("package b\n"), Operation: checkpoint.OpAdd},
	})
	if err != nil {
		t.Fatalf("Create base: %v", err)
	}

	// A branch changes a.go but leaves b.go identical.
	branch, err := svc.Create(ctx, "task1", base.ID, "branch", "rev1", []checkpoint.FileChange{
		{Path: "a.go", After: []byte("package a\nvar X = 2\n"), Operation: checkpoint.OpModify},
		{Path: "b.go", After: []byte("package b\n"), Operation: checkpoint.OpModify},
	})
	if err != nil {
		t.Fatalf("Create branch: %v", err)
	}

	baseEntries, _ := store.Entries(ctx, base.ID)
	branchEntries, _ := store.Entries(ctx, branch.ID)
	baseB := afterID(baseEntries, "b.go")
	branchB := afterID(branchEntries, "b.go")
	if baseB == "" || baseB != branchB {
		t.Fatalf("unchanged b.go should share the same content artifact across branches: %q vs %q", baseB, branchB)
	}
	baseA := afterID(baseEntries, "a.go")
	branchA := afterID(branchEntries, "a.go")
	if baseA == branchA {
		t.Fatalf("changed a.go should have a distinct content artifact")
	}
}

func TestParentMustExistAndDAGIsAcyclic(t *testing.T) {
	svc, store := newService(t)
	ctx := context.Background()

	// Unknown parent is rejected.
	if _, err := svc.Create(ctx, "t", "does-not-exist", "x", "r", nil); err == nil {
		t.Fatalf("expected error for unknown parent checkpoint")
	}

	base, _ := svc.Create(ctx, "t", "", "base", "r", nil)
	child, _ := svc.Create(ctx, "t", base.ID, "child", "r", nil)
	anc, err := store.Ancestors(ctx, child.ID)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	if len(anc) != 2 || anc[0].ID != child.ID || anc[1].ID != base.ID {
		t.Fatalf("ancestor chain wrong: %+v", anc)
	}
}

func TestCompareAndConflictDetection(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	a, _ := svc.Create(ctx, "t", "", "a", "r", []checkpoint.FileChange{
		{Path: "x.go", After: []byte("v1"), Operation: checkpoint.OpAdd},
		{Path: "y.go", After: []byte("y"), Operation: checkpoint.OpAdd},
	})
	b, _ := svc.Create(ctx, "t", a.ID, "b", "r", []checkpoint.FileChange{
		{Path: "x.go", After: []byte("v2"), Operation: checkpoint.OpModify},
		{Path: "y.go", After: []byte("y"), Operation: checkpoint.OpModify},
	})
	changed, err := svc.Compare(ctx, a.ID, b.ID)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if len(changed) != 1 || changed[0] != "x.go" {
		t.Fatalf("only x.go changed content, got %v", changed)
	}

	// Write-conflict detection over two change sets.
	conflicts := checkpoint.DetectWriteConflicts([]string{"x.go", "y.go"}, []string{"y.go", "z.go"})
	if len(conflicts) != 1 || conflicts[0] != "y.go" {
		t.Fatalf("expected y.go conflict, got %v", conflicts)
	}
	if len(checkpoint.DetectWriteConflicts([]string{"a"}, []string{"b"})) != 0 {
		t.Fatalf("disjoint change sets should not conflict")
	}
}

func afterID(entries []checkpoint.Entry, path string) string {
	for _, e := range entries {
		if e.Path == path {
			return e.AfterArtifactID
		}
	}
	return ""
}

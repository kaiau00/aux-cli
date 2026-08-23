package project_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/project"
)

type fakeVCS struct {
	info project.VCSInfo
}

func (f fakeVCS) Inspect(_ context.Context, dir string) (project.VCSInfo, error) {
	info := f.info
	if info.Root == "" {
		info.Root = dir
	}
	return info, nil
}

func newService(t *testing.T, info project.VCSInfo) *project.Service {
	t.Helper()
	return project.NewService(project.NewStore(dbtest.New(t)), fakeVCS{info: info})
}

func TestResolveReusesProjectByRemote(t *testing.T) {
	svc := newService(t, project.VCSInfo{
		Type: "git", Remote: "https://github.com/acme/widget.git", Revision: "abc123", Branch: "main",
	})
	ctx := context.Background()
	dir := t.TempDir()

	first, err := svc.Resolve(ctx, dir)
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	second, err := svc.Resolve(ctx, dir)
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if first.Project.ID != second.Project.ID {
		t.Fatalf("reopening should reuse project: %s != %s", first.Project.ID, second.Project.ID)
	}
	if first.Project.CanonicalName != "widget" {
		t.Fatalf("name = %q, want widget", first.Project.CanonicalName)
	}
	if first.Project.VCSType != "git" {
		t.Fatalf("vcs type = %q", first.Project.VCSType)
	}
}

func TestResolveLocalOnlyReusedByPath(t *testing.T) {
	svc := newService(t, project.VCSInfo{Type: "none"})
	ctx := context.Background()
	dir := t.TempDir()

	first, _ := svc.Resolve(ctx, dir)
	second, _ := svc.Resolve(ctx, dir)
	if first.Project.ID != second.Project.ID {
		t.Fatalf("local-only project should be reused by path")
	}
	if first.Project.CanonicalRemoteHash != "" {
		t.Fatalf("local-only project should have no remote hash")
	}
}

func TestDifferentRemotesAreDifferentProjects(t *testing.T) {
	store := project.NewStore(dbtest.New(t))
	ctx := context.Background()
	dir := t.TempDir()

	svcA := project.NewService(store, fakeVCS{info: project.VCSInfo{Type: "git", Remote: "https://github.com/acme/a.git"}})
	svcB := project.NewService(store, fakeVCS{info: project.VCSInfo{Type: "git", Remote: "https://github.com/acme/b.git"}})

	a, _ := svcA.Resolve(ctx, dir)
	b, _ := svcB.Resolve(ctx, dir)
	if a.Project.ID == b.Project.ID {
		t.Fatalf("different remotes must be different projects")
	}
}

func TestRevisionReuseAndChange(t *testing.T) {
	svc := newService(t, project.VCSInfo{Type: "git", Remote: "https://github.com/acme/w.git", Revision: "rev1"})
	ctx := context.Background()
	dir := t.TempDir()

	r1, _ := svc.Resolve(ctx, dir)
	r2, _ := svc.Resolve(ctx, dir)
	if r1.Revision.ID != r2.Revision.ID {
		t.Fatalf("identical revision should be reused")
	}

	// A dirtied tree is a new revision.
	dirty := project.NewService(svc.Store(), fakeVCS{info: project.VCSInfo{
		Type: "git", Remote: "https://github.com/acme/w.git", Revision: "rev1", DirtyHash: "deadbeef",
	}})
	r3, _ := dirty.Resolve(ctx, dir)
	if r3.Revision.ID == r1.Revision.ID {
		t.Fatalf("dirty tree should create a new revision")
	}
	if r3.Project.ID != r1.Project.ID {
		t.Fatalf("dirty tree should stay the same project")
	}
}

func TestNormalizeRemoteStripsCredentialsAndScheme(t *testing.T) {
	want := "github.com/acme/widget"
	cases := []string{
		"https://github.com/acme/widget.git",
		"https://user:token@github.com/acme/widget.git",
		"git@github.com:acme/widget.git",
		"ssh://git@github.com/acme/widget",
	}
	for _, c := range cases {
		if got := project.NormalizeRemote(c); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", c, got, want)
		}
	}
	// All variants must hash identically.
	base := project.RemoteHash(cases[0])
	for _, c := range cases[1:] {
		if project.RemoteHash(c) != base {
			t.Errorf("remote hash mismatch for %q", c)
		}
	}
}

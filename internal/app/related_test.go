package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
)

func writeGoMod(t *testing.T, dir, modulePath string, requires ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "module " + modulePath + "\n\ngo 1.24\n"
	if len(requires) > 0 {
		content += "\nrequire (\n"
		for _, r := range requires {
			content += "\t" + r + " v1.0.0\n"
		}
		content += ")\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
}

func TestDeriveRelatedProjectsRecordsEdgeForKnownDependency(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	appDir := t.TempDir()
	libDir := t.TempDir()
	writeGoMod(t, appDir, "github.com/acme/app", "github.com/acme/lib")
	writeGoMod(t, libDir, "github.com/acme/lib")

	pstore := project.NewStore(conn)
	appProject := project.Project{ID: "proj-app", CanonicalName: "app", VCSType: "none", CreatedAt: 1, LastOpenedAt: 1}
	libProject := project.Project{ID: "proj-lib", CanonicalName: "lib", VCSType: "none", CreatedAt: 1, LastOpenedAt: 1}
	if err := pstore.CreateProject(ctx, appProject); err != nil {
		t.Fatalf("CreateProject app: %v", err)
	}
	if err := pstore.CreateProject(ctx, libProject); err != nil {
		t.Fatalf("CreateProject lib: %v", err)
	}
	if err := pstore.UpsertRoot(ctx, project.Root{PathHash: "h-app", ProjectID: "proj-app", CanonicalPath: appDir, CreatedAt: 1, LastSeenAt: 1}); err != nil {
		t.Fatalf("UpsertRoot app: %v", err)
	}
	if err := pstore.UpsertRoot(ctx, project.Root{PathHash: "h-lib", ProjectID: "proj-lib", CanonicalPath: libDir, CreatedAt: 1, LastSeenAt: 1}); err != nil {
		t.Fatalf("UpsertRoot lib: %v", err)
	}

	a := &App{
		Projects:        project.NewService(pstore, project.GitVCS{}),
		RelatedProjects: relatedproject.NewStore(conn),
	}

	res := project.Resolution{
		Project: appProject,
		Root:    project.Root{CanonicalPath: appDir},
	}
	a.deriveRelatedProjects(ctx, res)

	from, err := a.RelatedProjects.From(ctx, "proj-app")
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(from) != 1 || from[0].ToProject != "proj-lib" || from[0].RelationType != relatedproject.LibraryConsumer {
		t.Fatalf("expected one library_consumer edge to proj-lib, got %+v", from)
	}
}

func TestDeriveRelatedProjectsSkipsUnknownDependency(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	appDir := t.TempDir()
	writeGoMod(t, appDir, "github.com/acme/app", "github.com/third/unknown")

	pstore := project.NewStore(conn)
	appProject := project.Project{ID: "proj-app", CanonicalName: "app", VCSType: "none", CreatedAt: 1, LastOpenedAt: 1}
	if err := pstore.CreateProject(ctx, appProject); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := pstore.UpsertRoot(ctx, project.Root{PathHash: "h-app", ProjectID: "proj-app", CanonicalPath: appDir, CreatedAt: 1, LastSeenAt: 1}); err != nil {
		t.Fatalf("UpsertRoot: %v", err)
	}

	a := &App{
		Projects:        project.NewService(pstore, project.GitVCS{}),
		RelatedProjects: relatedproject.NewStore(conn),
	}
	res := project.Resolution{Project: appProject, Root: project.Root{CanonicalPath: appDir}}
	a.deriveRelatedProjects(ctx, res)

	from, err := a.RelatedProjects.From(ctx, "proj-app")
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(from) != 0 {
		t.Fatalf("expected no edges for an unknown dependency, got %+v", from)
	}
}

func TestDeriveRelatedProjectsNoGoMod(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	appDir := t.TempDir() // no go.mod

	pstore := project.NewStore(conn)
	appProject := project.Project{ID: "proj-app", CanonicalName: "app", VCSType: "none", CreatedAt: 1, LastOpenedAt: 1}
	if err := pstore.CreateProject(ctx, appProject); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	a := &App{
		Projects:        project.NewService(pstore, project.GitVCS{}),
		RelatedProjects: relatedproject.NewStore(conn),
	}
	res := project.Resolution{Project: appProject, Root: project.Root{CanonicalPath: appDir}}
	// Must not panic when go.mod is absent (non-Go project).
	a.deriveRelatedProjects(ctx, res)

	from, err := a.RelatedProjects.From(ctx, "proj-app")
	if err != nil {
		t.Fatalf("From: %v", err)
	}
	if len(from) != 0 {
		t.Fatalf("expected no edges without a go.mod, got %+v", from)
	}
}

package profile_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/profile"
)

func newService(t *testing.T) *profile.Service {
	t.Helper()
	store := profile.NewStore(dbtest.New(t))
	return profile.NewService(store, profile.NewBuilder(store, profile.DefaultScanners()))
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func entryByType(entries []profile.Entry, typ, key string) (profile.Entry, bool) {
	for _, e := range entries {
		if e.Type == typ && e.Key == key {
			return e, true
		}
	}
	return profile.Entry{}, false
}

func TestCompileGoProject(t *testing.T) {
	svc := newService(t)
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module github.com/acme/widget\n\ngo 1.24\n")

	_, entries, err := svc.CompileProject(context.Background(), "proj-1", dir, "rev1")
	if err != nil {
		t.Fatalf("CompileProject: %v", err)
	}
	if _, ok := entryByType(entries, profile.EntryLanguage, "go"); !ok {
		t.Fatalf("expected go language entry, got %+v", entries)
	}
	if e, ok := entryByType(entries, profile.EntryValidationCommand, "go.test"); !ok {
		t.Fatalf("expected go.test validation command")
	} else if e.SourceType == "" || e.Confidence == 0 {
		t.Fatalf("entry missing provenance/confidence: %+v", e)
	}
}

func TestUnchangedInputsReuseVersion(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module m\n\ngo 1.24\n")

	first, _, err := svc.CompileProject(ctx, "proj-1", dir, "rev1")
	if err != nil {
		t.Fatalf("first compile: %v", err)
	}
	if first.Reused {
		t.Fatalf("first build should not be reused")
	}
	second, _, err := svc.CompileProject(ctx, "proj-1", dir, "rev1")
	if err != nil {
		t.Fatalf("second compile: %v", err)
	}
	if !second.Reused {
		t.Fatalf("unchanged inputs must reuse the profile version")
	}
	if first.ID != second.ID {
		t.Fatalf("reused version id mismatch: %s != %s", first.ID, second.ID)
	}
}

func TestChangedInputsCreateNewVersion(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module m\n\ngo 1.24\n")

	first, _, _ := svc.CompileProject(ctx, "proj-1", dir, "rev1")

	// Add a package.json — a different input set → new version.
	writeFile(t, dir, "package.json", `{"name":"w","scripts":{"test":"jest","build":"tsc"}}`)
	second, entries, err := svc.CompileProject(ctx, "proj-1", dir, "rev2")
	if err != nil {
		t.Fatalf("second compile: %v", err)
	}
	if second.Reused || second.ID == first.ID {
		t.Fatalf("changed inputs must create a new version")
	}
	if _, ok := entryByType(entries, profile.EntryValidationCommand, "node.test"); !ok {
		t.Fatalf("expected node.test validation command from package.json scripts")
	}
	if _, ok := entryByType(entries, profile.EntryBuildCommand, "node.build"); !ok {
		t.Fatalf("expected node.build build command")
	}
}

func TestInstructionScannerImportsExcerpt(t *testing.T) {
	svc := newService(t)
	dir := t.TempDir()
	writeFile(t, dir, "AGENTS.md", "# Conventions\nAlways run tests before committing.\n")

	_, entries, err := svc.CompileProject(context.Background(), "proj-1", dir, "rev1")
	if err != nil {
		t.Fatalf("CompileProject: %v", err)
	}
	if e, ok := entryByType(entries, profile.EntryInstruction, "AGENTS.md"); !ok {
		t.Fatalf("expected AGENTS.md instruction entry")
	} else if e.TokenEstimate == 0 {
		t.Fatalf("instruction entry should estimate tokens")
	}
}

package profile_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/profile"
)

func effEntry(entries []profile.EffectiveEntry, typ, key string) (profile.EffectiveEntry, bool) {
	for _, e := range entries {
		if e.Type == typ && e.Key == key {
			return e, true
		}
	}
	return profile.EffectiveEntry{}, false
}

func TestCompileHigherPrecedenceWinsWithProvenance(t *testing.T) {
	builtin := profile.LayerInput{
		OwnerType: profile.OwnerBuiltin, Precedence: profile.Precedence[profile.OwnerBuiltin], VersionID: "b",
		Entries: []profile.Entry{{
			Type: profile.EntryValidationCommand, Key: "test",
			ValueJSON:  `{"command":"go test -short ./..."}`,
			SourceType: "builtin", Confidence: 0.5,
		}},
	}
	project := profile.LayerInput{
		OwnerType: profile.OwnerProject, Precedence: profile.Precedence[profile.OwnerProject], VersionID: "p",
		Entries: []profile.Entry{{
			Type: profile.EntryValidationCommand, Key: "test",
			ValueJSON: `{"command":"go test ./..."}`, SourceType: "go.mod", Confidence: 0.9,
		}},
	}

	eff := profile.Compile("proj", "rev", "", []profile.LayerInput{builtin, project})

	e, ok := effEntry(eff.Entries, profile.EntryValidationCommand, "test")
	if !ok {
		t.Fatalf("expected merged test entry")
	}
	if e.OwnerType != profile.OwnerProject {
		t.Fatalf("higher precedence (project) should win, got owner %q", e.OwnerType)
	}
	if !strings.Contains(e.ValueJSON, "go test ./...") {
		t.Fatalf("winning value wrong: %s", e.ValueJSON)
	}
	if len(e.Overrides) != 1 || e.Overrides[0].OwnerType != profile.OwnerBuiltin {
		t.Fatalf("expected builtin recorded as provenance, got %+v", e.Overrides)
	}
	if !e.Conflict {
		t.Fatalf("differing values should be flagged as a conflict")
	}
	if len(eff.Conflicts()) != 1 {
		t.Fatalf("Conflicts() should return the one conflicting entry")
	}
}

func TestCompileIsDeterministic(t *testing.T) {
	layers := []profile.LayerInput{
		{OwnerType: profile.OwnerProject, Precedence: 3, VersionID: "p", Entries: []profile.Entry{
			{Type: profile.EntryLanguage, Key: "go", ValueJSON: `{}`},
			{Type: profile.EntryValidationCommand, Key: "test", ValueJSON: `{"command":"go test ./..."}`},
		}},
	}
	a := profile.Compile("proj", "rev", "", layers)
	b := profile.Compile("proj", "rev", "", layers)
	if a.VersionSetHash != b.VersionSetHash {
		t.Fatalf("version set hash not deterministic")
	}
	if a.Manifest != b.Manifest {
		t.Fatalf("manifest not deterministic")
	}
}

func TestManifestMuchSmallerThanRawSources(t *testing.T) {
	svc := newService(t)
	dir := t.TempDir()
	// A large instruction file: the manifest must not paste the whole thing.
	bigInstructions := "# Guide\n" + strings.Repeat("Follow the convention carefully in every module.\n", 500)
	writeFile(t, dir, "AGENTS.md", bigInstructions)
	writeFile(t, dir, "go.mod", "module m\n\ngo 1.24\n")

	eff, err := svc.CompileEffective(context.Background(), "proj-1", "rev-1", dir, "rev1", "")
	if err != nil {
		t.Fatalf("CompileEffective: %v", err)
	}
	if len(eff.Manifest) >= len(bigInstructions) {
		t.Fatalf("manifest (%d) should be much smaller than raw source (%d)", len(eff.Manifest), len(bigInstructions))
	}
	// It should still be well under a small fraction of the source size.
	if len(eff.Manifest) > len(bigInstructions)/10 {
		t.Fatalf("manifest not compact enough: %d vs source %d", len(eff.Manifest), len(bigInstructions))
	}
}

func TestCompileEffectivePersistsAndDedupes(t *testing.T) {
	store := profile.NewStore(dbtest.New(t))
	svc := profile.NewService(store, profile.NewBuilder(store, profile.DefaultScanners()))
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module m\n\ngo 1.24\n")

	first, err := svc.CompileEffective(ctx, "proj-1", "rev-1", dir, "rev1", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Builtin + project layers both contribute.
	if _, ok := effEntry(first.Entries, profile.EntryConvention, "validation.strategy"); !ok {
		t.Fatalf("expected builtin convention entry")
	}
	if _, ok := effEntry(first.Entries, profile.EntryLanguage, "go"); !ok {
		t.Fatalf("expected project go entry")
	}

	stored, ok, err := store.GetLatestEffective(ctx, "proj-1", "")
	if err != nil || !ok {
		t.Fatalf("expected persisted effective profile, ok=%v err=%v", ok, err)
	}
	if stored.VersionSetHash != first.VersionSetHash {
		t.Fatalf("persisted hash mismatch")
	}
}

func TestDiffEffective(t *testing.T) {
	a := profile.Compile("p", "r", "", []profile.LayerInput{{OwnerType: profile.OwnerProject, Precedence: 3, VersionID: "1", Entries: []profile.Entry{
		{Type: profile.EntryLanguage, Key: "go", ValueJSON: `{}`},
		{Type: profile.EntryValidationCommand, Key: "test", ValueJSON: `{"command":"a"}`},
	}}})
	b := profile.Compile("p", "r", "", []profile.LayerInput{{OwnerType: profile.OwnerProject, Precedence: 3, VersionID: "2", Entries: []profile.Entry{
		{Type: profile.EntryValidationCommand, Key: "test", ValueJSON: `{"command":"b"}`},
		{Type: profile.EntryBuildCommand, Key: "build", ValueJSON: `{"command":"c"}`},
	}}})
	added, removed, changed := profile.DiffEffective(a, b)
	if len(added) != 1 || added[0] != "build_command/build" {
		t.Fatalf("added = %v", added)
	}
	if len(removed) != 1 || removed[0] != "language/go" {
		t.Fatalf("removed = %v", removed)
	}
	if len(changed) != 1 || changed[0] != "validation_command/test" {
		t.Fatalf("changed = %v", changed)
	}
}

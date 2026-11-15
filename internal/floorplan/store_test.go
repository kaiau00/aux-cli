package floorplan

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func sampleArchitecture() Architecture {
	return Architecture{
		Schema:   ArchitectureSchema,
		Audience: "developers",
		Project:  Project{Name: "M", Summary: "A fixture.", Entrypoints: []string{"main.go"}},
		Nodes: []Area{
			{ID: "cli", Label: "Command line", Kind: KindUI, Summary: "Entry point.", Paths: []string{"main.go"}},
			{ID: "ext.db", Label: "SQLite", Kind: KindExternal, Packages: []string{"go:modernc.org/sqlite"}},
		},
		Edges:       []Edge{{From: "cli", To: "ext.db", Kind: LinkData}},
		Annotations: []Annotation{{Path: "main.go", Note: "Read this first.", Tags: []string{"hotspot"}}},
		Flows:       []Flow{{ID: "boot", Title: "Boot", Steps: []FlowStep{{Node: "cli", Note: "starts"}}}},
		Tour:        []TourStop{{Node: "cli", Title: "Start here", Note: "The front door."}},
	}
}

// The authored layer is the expensive half of the map, so it lives in the
// repository where it can be reviewed in a pull request and shared, rather
// than in a database on one machine. That means it has to survive a round trip
// exactly.
func TestArchitectureRoundTrips(t *testing.T) {
	root := t.TempDir()
	want := sampleArchitecture()
	if err := SaveArchitecture(root, want); err != nil {
		t.Fatalf("SaveArchitecture: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, MapDir, "architecture.json")); err != nil {
		t.Fatalf("architecture.json was not written: %v", err)
	}

	got, err := LoadArchitecture(root)
	if err != nil {
		t.Fatalf("LoadArchitecture: %v", err)
	}
	if got.Project.Name != want.Project.Name || got.Audience != want.Audience {
		t.Errorf("project/audience lost: %+v", got)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].ID != "cli" || got.Nodes[0].Summary != "Entry point." {
		t.Errorf("nodes lost: %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].To != "ext.db" {
		t.Errorf("edges lost: %+v", got.Edges)
	}
	if len(got.Annotations) != 1 || got.Annotations[0].Note != "Read this first." {
		t.Errorf("annotations lost: %+v", got.Annotations)
	}
	if len(got.Flows) != 1 || len(got.Flows[0].Steps) != 1 || got.Flows[0].Steps[0].Node != "cli" {
		t.Errorf("flows lost: %+v", got.Flows)
	}
	if len(got.Tour) != 1 || got.Tour[0].Node != "cli" {
		t.Errorf("tour lost: %+v", got.Tour)
	}
}

// A repository with no map is the ordinary first case, not a failure, and the
// caller has to be able to tell it apart from a damaged file.
func TestLoadArchitectureReportsAMissingMapDistinctly(t *testing.T) {
	_, err := LoadArchitecture(t.TempDir())
	if !errors.Is(err, ErrNoMap) {
		t.Fatalf("err = %v; want ErrNoMap", err)
	}
}

// A truncated or hand-edited file must not be mistaken for an absent one: that
// would silently replace somebody's authored map with an empty one.
func TestLoadArchitectureReportsADamagedFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MapDir), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, MapDir, "architecture.json"), []byte(`{"nodes":[`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := LoadArchitecture(root)
	if err == nil {
		t.Fatal("a damaged architecture.json was accepted")
	}
	if errors.Is(err, ErrNoMap) {
		t.Fatal("a damaged file was reported as a missing map")
	}
}

// Descriptions are stored one file per area, which is what keeps a describe
// run over one area from rewriting every other area's prose.
func TestDescriptionsRoundTripPerArea(t *testing.T) {
	root := t.TempDir()
	cli := map[string]FileDescription{
		"main.go": {Name: "Entry point", D: "Starts the CLI.", Sig: "abc123"},
	}
	store := map[string]FileDescription{
		"internal/store/store.go": {Name: "Store", D: "Holds state.", Sig: "def456"},
	}
	if err := SaveAreaDescriptions(root, "cli", cli); err != nil {
		t.Fatalf("SaveAreaDescriptions(cli): %v", err)
	}
	if err := SaveAreaDescriptions(root, "store", store); err != nil {
		t.Fatalf("SaveAreaDescriptions(store): %v", err)
	}

	got, err := LoadDescriptions(root)
	if err != nil {
		t.Fatalf("LoadDescriptions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d descriptions; want 2 (%v)", len(got), got)
	}
	main, ok := got["main.go"]
	if !ok {
		t.Fatalf("main.go missing from %v", got)
	}
	if main.Name != "Entry point" || main.D != "Starts the CLI." || main.Sig != "abc123" {
		t.Errorf("main.go description = %+v; want name, prose and signature preserved", main)
	}

	// Rewriting one area leaves the other alone.
	if err := SaveAreaDescriptions(root, "cli", map[string]FileDescription{"main.go": {Name: "Changed", D: "New.", Sig: "zzz"}}); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, _ = LoadDescriptions(root)
	if got["internal/store/store.go"].D != "Holds state." {
		t.Error("rewriting one area's descriptions disturbed another's")
	}
	if got["main.go"].Name != "Changed" {
		t.Error("the rewritten description was not saved")
	}
}

// The on-disk key is "description"; the built document's is "d". They are
// different schemas and the loader has to read the authored one.
func TestLoadDescriptionsReadsTheAuthoredKeyNames(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, MapDir, "descriptions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw := `{"files":{"main.go":{"name":"Entry point","description":"Starts the CLI.","sig":"abc123"}}}`
	if err := os.WriteFile(filepath.Join(dir, "cli.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := LoadDescriptions(root)
	if err != nil {
		t.Fatalf("LoadDescriptions: %v", err)
	}
	if got["main.go"].D != "Starts the CLI." {
		t.Errorf("description not read from the authored key: %+v", got["main.go"])
	}
}

func TestLoadDescriptionsOfAnUnmappedRepoIsEmpty(t *testing.T) {
	got, err := LoadDescriptions(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDescriptions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d descriptions; want none", len(got))
	}
}

// An area id becomes a filename, so it must not be able to address anything
// outside the map directory.
func TestSaveAreaDescriptionsRejectsAnUnsafeAreaID(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"../escape", "a/b", "", "."} {
		if err := SaveAreaDescriptions(root, id, map[string]FileDescription{"x.go": {D: "x"}}); err == nil {
			t.Errorf("SaveAreaDescriptions accepted the area id %q", id)
		}
	}
}

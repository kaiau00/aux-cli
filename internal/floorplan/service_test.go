package floorplan

import (
	"context"
	"testing"
	"time"
)

// touch rewrites a file with new content and a distinct modification time, so
// a stat-based check sees it as changed regardless of filesystem timestamp
// granularity.
func touch(t *testing.T, root, rel, content string) {
	t.Helper()
	mustWrite(t, root, rel, content)
	bumpModTime(t, root, rel)
}

func TestServiceBuildsTheMapOnFirstUse(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"lib/thing.go": "package lib\n\n// Thing does.\nfunc Thing() {}\n",
	})
	svc := NewService(root)

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if g.Schema != GraphSchema {
		t.Errorf("Schema = %q, want %q", g.Schema, GraphSchema)
	}
	if g.Meta.Counts.Files != 2 {
		t.Errorf("Counts.Files = %d, want 2", g.Meta.Counts.Files)
	}
}

func TestServiceServesTheSameMapWhenNothingChanged(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	first, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	second, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map again: %v", err)
	}
	// generatedAt is stamped per build, so an identical stamp is proof the
	// second call did not rebuild.
	if first.Meta.GeneratedAt != second.Meta.GeneratedAt {
		t.Error("the map was rebuilt although nothing changed")
	}
	if n := svc.Builds(); n != 1 {
		t.Errorf("Builds() = %d, want 1", n)
	}
}

func TestServiceRebuildsWhenAFileChanges(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map: %v", err)
	}
	touch(t, root, "main.go", "package main\n\nfunc main() {}\n")

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map after change: %v", err)
	}
	if n := svc.Builds(); n != 2 {
		t.Errorf("Builds() = %d, want 2 after a file changed", n)
	}
	if g.Meta.Counts.LOC != 2 {
		t.Errorf("LOC = %d, want 2 after the edit", g.Meta.Counts.LOC)
	}
}

func TestServiceRebuildsWhenAFileAppears(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map: %v", err)
	}
	mustWrite(t, root, "added.go", "package main\n")

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map after an addition: %v", err)
	}
	if g.Meta.Counts.Files != 2 {
		t.Errorf("Counts.Files = %d, want 2 after a file appeared", g.Meta.Counts.Files)
	}
}

func TestServiceRebuildsWhenAFileDisappears(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go": "package main\n",
		"gone.go": "package main\n",
	})
	svc := NewService(root)

	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map: %v", err)
	}
	mustRemove(t, root, "gone.go")

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map after a removal: %v", err)
	}
	if g.Meta.Counts.Files != 1 {
		t.Errorf("Counts.Files = %d, want 1 after a file was removed", g.Meta.Counts.Files)
	}
}

func TestServiceReusesTheAnalysisOfUnchangedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package main\n",
		"b.go": "package main\n",
		"c.go": "package main\n",
	})
	svc := NewService(root)

	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map: %v", err)
	}
	if n := svc.Analyzed(); n != 3 {
		t.Fatalf("Analyzed() = %d, want 3 on the first build", n)
	}
	touch(t, root, "b.go", "package main\n\nfunc B() {}\n")
	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map after change: %v", err)
	}
	// Only the changed file should have been read and parsed again.
	if n := svc.Analyzed(); n != 4 {
		t.Errorf("Analyzed() = %d, want 4: three files once, plus b.go a second time", n)
	}
}

func TestServiceRebuildForcesAFreshBuild(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	if _, err := svc.Map(context.Background()); err != nil {
		t.Fatalf("Map: %v", err)
	}
	if _, err := svc.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if n := svc.Builds(); n != 2 {
		t.Errorf("Builds() = %d, want 2 after an explicit rebuild", n)
	}
	if n := svc.Analyzed(); n != 2 {
		t.Errorf("Analyzed() = %d, want 2: a rebuild re-reads everything", n)
	}
}

func TestServiceCarriesGitStateIntoTheDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	commitTree(t, root)
	svc := NewService(root)

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	if len(g.Meta.Repos) != 1 {
		t.Fatalf("Meta.Repos = %v, want one record", g.Meta.Repos)
	}
	if g.Meta.Repos[0].Head == "" {
		t.Error("Meta.Repos[0].Head is empty, want the checked-out commit")
	}
}

func TestServiceWorksOutsideARepository(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map outside a repository: %v", err)
	}
	if g.Meta.Counts.Files != 1 {
		t.Errorf("Counts.Files = %d, want 1", g.Meta.Counts.Files)
	}
	if len(g.Meta.Repos) != 0 {
		t.Errorf("Meta.Repos = %v, want none outside a repository", g.Meta.Repos)
	}
}

func TestServiceReadsTheAuthoredLayerWhenPresent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"lib/thing.go": "package lib\n",
	})
	arch := Architecture{
		Schema: ArchitectureSchema,
		Nodes: []Area{
			{ID: "lib", Label: "The library", Kind: KindLibrary, Paths: []string{"lib/**"}},
		},
	}
	if err := SaveArchitecture(root, arch); err != nil {
		t.Fatalf("SaveArchitecture: %v", err)
	}
	svc := NewService(root)

	g, err := svc.Map(context.Background())
	if err != nil {
		t.Fatalf("Map: %v", err)
	}
	var found bool
	for _, n := range g.Nodes {
		if n.ID == "lib" && n.Label == "The library" {
			found = true
		}
	}
	if !found {
		t.Errorf("the authored area is missing from the built map: %+v", g.Nodes)
	}
	if g.Meta.Counts.Unassigned != 0 {
		t.Errorf("Unassigned = %d, want 0: the glob claims the file", g.Meta.Counts.Unassigned)
	}
}

func TestServiceIsSafeUnderConcurrentUse(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	svc := NewService(root)

	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, err := svc.Map(context.Background())
			done <- err
		}()
	}
	deadline := time.After(30 * time.Second)
	for i := 0; i < 8; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("concurrent Map: %v", err)
			}
		case <-deadline:
			t.Fatal("concurrent Map calls did not finish")
		}
	}
}

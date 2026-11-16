package floorplan

import (
	"context"
	"testing"
	"time"
)

// buildFixture is a small repository with a two-level architecture, one
// unclaimed file, and one external service, so every part of the built
// document has something to show.
func buildFixture(t *testing.T) (string, Architecture) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"go.mod":                       "module example.com/m\n",
		"main.go":                      "package main\n\nimport \"example.com/m/internal/store\"\n\nfunc main() { _ = store.X }\n",
		"internal/store/store.go":      "package store\n\nimport \"github.com/pressly/goose/v3\"\n\nvar X = goose.Up\n",
		"internal/store/cache.go":      "package store\n\nvar Y = 2\n",
		"internal/store/store_test.go": "package store\n\nvar Z = 3\n",
		"internal/orphan/orphan.go":    "package orphan\n", // claimed by no area
		"README.md":                    "# m\n",            // not code: never in files[]
	})
	arch := Architecture{
		Schema:   ArchitectureSchema,
		Audience: "developers",
		Project:  Project{Name: "M", Summary: "A fixture.", Entrypoints: []string{"main.go"}},
		Nodes: []Area{
			{ID: "cli", Label: "Command line", Kind: KindUI, Summary: "Entry point.", Paths: []string{"main.go"}, Exclude: []string{"**/*_test.go"}},
			{ID: "store", Label: "Store", Kind: KindData, Summary: "Persistence.", Paths: []string{"internal/store/**"}, Exclude: []string{"**/*_test.go"}},
			{ID: "store.cache", Parent: "store", Label: "Cache", Kind: KindLibrary, Summary: "In memory.", Paths: []string{"internal/store/cache.go"}},
			{ID: "tests", Label: "Tests", Kind: KindTest, Summary: "The suite.", Paths: []string{"**/*_test.go"}},
			{ID: "ext.sqlite", Label: "SQLite", Kind: KindExternal, Summary: "Embedded database.", Packages: []string{"go:github.com/pressly/goose"}},
		},
		Edges: []Edge{{From: "store", To: "ext.sqlite", Kind: LinkData, Label: "migrations"}},
		Flows: []Flow{{ID: "boot", Title: "Boot", Steps: []FlowStep{{Node: "cli"}, {Node: "internal/store/store.go"}}}},
		Tour:  []TourStop{{Node: "cli", Title: "Start here"}},
	}
	return root, arch
}

func buildFixtureGraph(t *testing.T) Graph {
	t.Helper()
	root, arch := buildFixture(t)
	files, facts, err := ScanAndAnalyze(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanAndAnalyze: %v", err)
	}
	g, err := Build(BuildInput{
		Root:  root,
		Arch:  arch,
		Files: files,
		Facts: facts,
		Links: append(BuildLinks(root, files, facts), ExternalLinks(files, facts, arch)...),
		Repos: []Repo{{Path: ".", Name: "m", Branch: "main", Head: "abc123"}},
		Agent: "aux",
		Now:   time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return g
}

func nodesByID(g Graph) map[string]Node {
	out := make(map[string]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		out[n.ID] = n
	}
	return out
}

// A parent area's size is the size of everything inside it. An area that holds
// only child areas would otherwise read as empty, which is how the reference
// reports it too: its "agent" area owns no files directly and 86 through its
// children.
func TestBuildRollsStatsUpToParents(t *testing.T) {
	n := nodesByID(buildFixtureGraph(t))

	// store directly owns store.go; store.cache owns cache.go.
	if got := n["store.cache"].Stats.Files; got != 1 {
		t.Errorf("store.cache files = %d; want 1", got)
	}
	if got := n["store"].Stats.Files; got != 2 {
		t.Errorf("store files = %d; want 2 (its own plus the child's)", got)
	}
	if got := n["store"].Stats.LOC; got <= n["store.cache"].Stats.LOC {
		t.Errorf("store loc = %d; must include the child's %d", got, n["store.cache"].Stats.LOC)
	}
	if got := n["store"].Stats.Langs["go"]; got == 0 {
		t.Error("store langs does not aggregate go")
	}
}

// Only code is on the map. Counting a README as part of the codebase would
// overstate it, and the reference excludes non-code from files[] as well.
func TestBuildMapsOnlyCodeFiles(t *testing.T) {
	g := buildFixtureGraph(t)
	for _, f := range g.Files {
		if f.P == "README.md" || f.P == "go.mod" {
			t.Errorf("%s is in files[]; only code belongs there", f.P)
		}
	}
	if g.Meta.Counts.Files != len(g.Files) {
		t.Errorf("counts.files = %d; want %d", g.Meta.Counts.Files, len(g.Files))
	}
}

// The unassigned count is how an incomplete map says so. Silently absorbing an
// unclaimed file into some nearby area would hide exactly the thing that needs
// fixing.
func TestBuildCountsUnassignedFiles(t *testing.T) {
	g := buildFixtureGraph(t)
	if g.Meta.Counts.Unassigned != 1 {
		t.Errorf("counts.unassigned = %d; want 1 (internal/orphan/orphan.go)", g.Meta.Counts.Unassigned)
	}
	for _, f := range g.Files {
		if f.P == "internal/orphan/orphan.go" && f.N != "" {
			t.Errorf("the unclaimed file was assigned to %q", f.N)
		}
	}
}

// A root area has no parent, and that has to serialise as null rather than as
// the empty string, or the viewer cannot tell a root from a child of "".
func TestBuildRootAreasHaveANullParent(t *testing.T) {
	n := nodesByID(buildFixtureGraph(t))
	if n["cli"].Parent != nil {
		t.Errorf("cli parent = %v; want nil", *n["cli"].Parent)
	}
	if p := n["store.cache"].Parent; p == nil || *p != "store" {
		t.Errorf("store.cache parent = %v; want store", p)
	}
}

// An authored edge between two areas is a claim no parser could make, so it
// has to survive into the built document.
func TestBuildKeepsAuthoredEdges(t *testing.T) {
	g := buildFixtureGraph(t)
	for _, l := range g.Links {
		if l.A == "n:store" && l.B == "n:ext.sqlite" && l.K == LinkData {
			return
		}
	}
	t.Fatalf("the authored store -> ext.sqlite edge is missing from %d links", len(g.Links))
}

// An external service has no files of its own; reporting a count would invent
// code that does not exist in this repository.
func TestBuildExternalAreasHaveNoFiles(t *testing.T) {
	ext := nodesByID(buildFixtureGraph(t))["ext.sqlite"]
	if ext.Stats.Files != 0 || ext.Stats.LOC != 0 {
		t.Errorf("external stats = %+v; want zero", ext.Stats)
	}
}

// A flow or tour step pointing at something that no longer exists is a broken
// map. It must be reported, because silently dropping the step leaves a flow
// that reads as complete while skipping a stage.
func TestBuildReportsDanglingFlowAndTourRefs(t *testing.T) {
	root, arch := buildFixture(t)
	arch.Flows = []Flow{{ID: "broken", Title: "Broken", Steps: []FlowStep{{Node: "does-not-exist"}}}}
	arch.Tour = []TourStop{{Node: "nope.go", Title: "Missing"}}

	files, _ := Scan(context.Background(), root)
	_, err := Build(BuildInput{Root: root, Arch: arch, Files: files, Now: time.Now()})
	if err == nil {
		t.Fatal("Build accepted a flow and a tour step pointing at nothing")
	}
}

// An architecture the assigner rejects cannot produce a trustworthy map.
func TestBuildRejectsABrokenArchitecture(t *testing.T) {
	root, arch := buildFixture(t)
	arch.Nodes = append(arch.Nodes, Area{ID: "child", Parent: "missing", Label: "C", Kind: KindMisc})
	files, _ := Scan(context.Background(), root)
	if _, err := Build(BuildInput{Root: root, Arch: arch, Files: files, Now: time.Now()}); err == nil {
		t.Fatal("Build accepted an architecture with a missing parent")
	}
}

// A step may name an area or a file, and the built document has to say which
// it resolved to, because the viewer navigates to a different thing in each
// case.
func TestBuildResolvesStepsToAreasAndFiles(t *testing.T) {
	g := buildFixtureGraph(t)
	if len(g.Flows) != 1 || len(g.Flows[0].Steps) != 2 {
		t.Fatalf("flows = %+v; want one flow with two steps", g.Flows)
	}
	if got := g.Flows[0].Steps[0].Ref; got != "n:cli" {
		t.Errorf("step 1 ref = %q; want n:cli (an area id)", got)
	}
	if got := g.Flows[0].Steps[1].Ref; got != "f:internal/store/store.go" {
		t.Errorf("step 2 ref = %q; want f:internal/store/store.go (a file path)", got)
	}
	if got := g.Tour[0].Ref; got != "n:cli" {
		t.Errorf("tour stop ref = %q; want n:cli", got)
	}
}

func TestBuildFillsMetaAndProject(t *testing.T) {
	g := buildFixtureGraph(t)
	if g.Schema != GraphSchema {
		t.Errorf("schema = %q; want %q", g.Schema, GraphSchema)
	}
	if g.Project.Name != "M" {
		t.Errorf("project name = %q; want M", g.Project.Name)
	}
	if g.Meta.GeneratedAt != "2026-11-15T12:00:00Z" {
		t.Errorf("generatedAt = %q; want the injected time in RFC3339", g.Meta.GeneratedAt)
	}
	if g.Meta.Audience != "developers" {
		t.Errorf("audience = %q; want developers", g.Meta.Audience)
	}
	if len(g.Meta.Repos) != 1 || g.Meta.Repos[0].Head != "abc123" {
		t.Errorf("repos = %+v; want the passed repo", g.Meta.Repos)
	}
	if g.Meta.Counts.LOC == 0 {
		t.Error("counts.loc is zero")
	}
}

// The document is served to a browser as JSON, where a nil slice becomes null
// and a caller written against an array breaks on it.
func TestBuildEmitsEmptyArraysNotNull(t *testing.T) {
	g := buildFixtureGraph(t)
	for _, n := range g.Nodes {
		if n.Tags == nil || n.Paths == nil || n.Packages == nil || n.Hosts == nil || n.Stats.Langs == nil || n.Stats.Repos == nil {
			t.Errorf("node %q has a nil slice or map: %+v", n.ID, n)
		}
	}
	if g.Files == nil || g.Links == nil || g.Flows == nil || g.Tour == nil || g.Routes == nil {
		t.Error("a top-level collection is nil; it must serialise as []")
	}
	if g.Recent.Tasks == nil || g.Recent.Items == nil || g.Health.Issues == nil {
		t.Error("recent or health collections are nil")
	}
}

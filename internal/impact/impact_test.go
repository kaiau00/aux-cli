package impact_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/impact"
)

// writeModule lays down a small Go module: package foo, and package bar that
// imports foo and has a test.
func writeModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/proj\n\ngo 1.24\n")
	write("foo/foo.go", "package foo\n\nfunc Hello() string { return \"hi\" }\n")
	write("bar/bar.go", "package bar\n\nimport \"example.com/proj/foo\"\n\nfunc Greet() string { return foo.Hello() }\n")
	write("bar/bar_test.go", "package bar\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) { _ = Greet() }\n")
	return root
}

func newService(t *testing.T) *impact.Service {
	t.Helper()
	return impact.NewService(impact.NewStore(dbtest.New(t)))
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestIndexAndAnalyzeDependents(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	root := writeModule(t)

	n, err := svc.Index(ctx, "proj", root, "rev1")
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 go files indexed, got %d", n)
	}

	// Changing foo should surface bar as a dependent and recommend bar's test.
	res, err := svc.Analyze(ctx, "proj", "rev1", []string{"foo/foo.go"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !contains(res.AffectedPackages, "example.com/proj/foo") {
		t.Fatalf("expected foo package affected: %+v", res.AffectedPackages)
	}
	if !contains(res.DirectDependents, "bar/bar.go") {
		t.Fatalf("expected bar/bar.go as a dependent: %+v", res.DirectDependents)
	}
	foundTest := false
	for _, tr := range res.RelatedTests {
		if tr.Path == "bar/bar_test.go" {
			foundTest = true
		}
	}
	if !foundTest {
		t.Fatalf("expected bar/bar_test.go recommended: %+v", res.RelatedTests)
	}
	if res.BroadenValidation {
		t.Fatalf("well-covered change should not force broad validation: %+v", res)
	}
	if len(res.Recommended) == 0 {
		t.Fatalf("expected targeted validation commands")
	}
}

func TestListNodesAndEdgesScopedToProject(t *testing.T) {
	store := impact.NewStore(dbtest.New(t))
	svc := impact.NewService(store)
	root := writeModule(t)
	ctx := context.Background()

	if _, err := svc.Index(ctx, "proj-a", root, "rev1"); err != nil {
		t.Fatalf("Index proj-a: %v", err)
	}
	if _, err := svc.Index(ctx, "proj-b", root, "rev1"); err != nil {
		t.Fatalf("Index proj-b: %v", err)
	}

	nodes, err := store.ListNodes(ctx, "proj-a", 0)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected nodes for proj-a")
	}
	for _, n := range nodes {
		if n.ProjectID != "proj-a" {
			t.Fatalf("ListNodes leaked a node from another project: %+v", n)
		}
	}

	edges, err := store.ListEdges(ctx, "proj-a", 0)
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	for _, e := range edges {
		if e.ProjectID != "proj-a" {
			t.Fatalf("ListEdges leaked an edge from another project: %+v", e)
		}
	}
}

func TestAnalyzeBroadensWhenUncovered(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	root := writeModule(t)
	if _, err := svc.Index(ctx, "proj", root, "rev1"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// A non-Go change is not covered by the AST indexer -> broaden.
	res, err := svc.Analyze(ctx, "proj", "rev1", []string{"README.md"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !res.BroadenValidation {
		t.Fatalf("uncovered change must broaden validation")
	}
	if !contains(res.Recommended, "go test ./...") {
		t.Fatalf("broad validation should recommend repo-wide test: %+v", res.Recommended)
	}
}

func TestAnalyzeBroadensWhenGraphEmpty(t *testing.T) {
	svc := newService(t)
	res, err := svc.Analyze(context.Background(), "proj", "rev1", []string{"foo/foo.go"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !res.BroadenValidation || res.Uncertainty < 0.5 {
		t.Fatalf("empty graph must broaden with high uncertainty: %+v", res)
	}
}

func TestAnalyzeBroadensWhenStale(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	root := writeModule(t)
	if _, err := svc.Index(ctx, "proj", root, "rev1"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	// Analyzing against a different revision than the graph was built at is stale.
	res, err := svc.Analyze(ctx, "proj", "rev2", []string{"foo/foo.go"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !res.BroadenValidation {
		t.Fatalf("stale graph must broaden validation: %+v", res)
	}
}

func TestReindexUpdatesEdges(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	root := writeModule(t)
	if _, err := svc.Index(ctx, "proj", root, "rev1"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// bar no longer imports foo.
	if err := os.WriteFile(filepath.Join(root, "bar/bar.go"),
		[]byte("package bar\n\nfunc Greet() string { return \"hi\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.Reindex(ctx, "proj", root, "rev2", []string{"bar/bar.go"}); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// foo now has no dependents.
	res, err := svc.Analyze(ctx, "proj", "rev2", []string{"foo/foo.go"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if contains(res.DirectDependents, "bar/bar.go") {
		t.Fatalf("reindex should have removed the stale import edge: %+v", res.DirectDependents)
	}
}

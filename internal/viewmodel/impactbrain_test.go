package viewmodel_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func writeSmallModule(t *testing.T) string {
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
	return root
}

func TestImpactGraphViewReportsBuiltGraph(t *testing.T) {
	store := impact.NewStore(dbtest.New(t))
	svc := impact.NewService(store)
	ctx := context.Background()
	root := writeSmallModule(t)

	if _, err := svc.Index(ctx, "proj-1", root, "rev1"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	stores := viewmodel.ImpactStores{Impact: store}
	view, err := stores.ImpactGraphView(ctx, "proj-1")
	if err != nil {
		t.Fatalf("ImpactGraphView: %v", err)
	}
	if !view.Built || view.NodeCount == 0 {
		t.Fatalf("expected a built graph, got %+v", view)
	}
	if len(view.Nodes) == 0 {
		t.Fatalf("expected nodes listed, got %+v", view)
	}
}

func TestImpactGraphViewReportsUnbuiltGraph(t *testing.T) {
	store := impact.NewStore(dbtest.New(t))
	stores := viewmodel.ImpactStores{Impact: store}
	view, err := stores.ImpactGraphView(context.Background(), "proj-never-indexed")
	if err != nil {
		t.Fatalf("ImpactGraphView: %v", err)
	}
	if view.Built {
		t.Fatalf("expected an unbuilt graph, got %+v", view)
	}
}

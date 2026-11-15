package floorplan

import (
	"context"
	"reflect"
	"testing"
)

// goTree is a small module whose import graph is known, so the derived links
// can be checked exactly. It contains both representative-file cases: a
// package with a file named after it, and one without.
func goTree(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n\ngo 1.25\n",
		"main.go": `package main

import (
	"fmt"
	"example.com/m/internal/store"
	"example.com/m/internal/cost"
	"github.com/third/party"
)

func main() { fmt.Println(store.X, cost.Y, party.Z) }
`,
		// store has a file named after its directory, so that file represents
		// it even though backend.go sorts first.
		"internal/store/backend.go":  "package store\n\nvar X = 1\n",
		"internal/store/store.go":    "package store\n",
		"internal/store/aaa_test.go": "package store\n",
		// cost has no cost.go, so the choice falls back to alphabetical order.
		"internal/cost/budget.go": "package cost\n\nvar Y = 1\n",
		"internal/cost/ledger.go": "package cost\n",
	})
}

func importLinks(t *testing.T, root string) []Link {
	t.Helper()
	files, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return BuildLinks(root, files, FileImports(root, files))
}

// An import names a package, but the map is drawn between files, so each
// import has to resolve to one representative file of the package it names.
func TestBuildLinksResolvesImportsToTheRepresentativeFile(t *testing.T) {
	got := importLinks(t, goTree(t))

	// store/store.go is named after its package, so it wins over backend.go;
	// cost has no such file, so its alphabetically first one stands in.
	want := map[string]string{
		"f:main.go": "f:internal/store/store.go",
	}
	seen := map[string][]string{}
	for _, l := range got {
		if l.K != LinkImport {
			t.Errorf("link kind = %q; want %q", l.K, LinkImport)
		}
		if l.W != 1 {
			t.Errorf("link weight = %v; want 1", l.W)
		}
		seen[l.A] = append(seen[l.A], l.B)
	}
	for a, b := range want {
		if !contains(seen[a], b) {
			t.Errorf("links from %s = %v; want one to %q", a, seen[a], b)
		}
	}
	if !contains(seen["f:main.go"], "f:internal/cost/budget.go") {
		t.Errorf("links from main.go = %v; want one to f:internal/cost/budget.go (alphabetical fallback)", seen["f:main.go"])
	}
	if contains(seen["f:main.go"], "f:internal/store/backend.go") {
		t.Error("main.go links to backend.go; store.go is named after its package and must represent it")
	}
}

// A test file in the same directory as the code it exercises is a real
// dependency of it, and one worth seeing on the map: it is how you find the
// test covering a file you are about to change.
func TestBuildLinksLinksATestFileToItsOwnPackage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":                             "module example.com/m\n",
		"internal/worktree/worktree.go":      "package worktree\n\nvar X = 1\n",
		"internal/worktree/worktree_test.go": "package worktree_test\n\nimport \"example.com/m/internal/worktree\"\n",
	})
	got := importLinks(t, root)
	for _, l := range got {
		if l.A == "f:internal/worktree/worktree_test.go" && l.B == "f:internal/worktree/worktree.go" {
			return
		}
	}
	t.Fatalf("no link from the test file to the package it tests; got %+v", got)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// A dependency outside the module is somebody else's code. Drawing it as a
// room in this repository's map would be a claim that it lives here.
func TestBuildLinksIgnoresImportsOutsideTheModule(t *testing.T) {
	for _, l := range importLinks(t, goTree(t)) {
		if l.B == "f:github.com/third/party" || l.B == "f:fmt" {
			t.Errorf("link to a dependency outside the module: %s -> %s", l.A, l.B)
		}
	}
}

// Two files in one package import each other's package constantly; a link from
// a package to itself is noise that would bury the real structure.
func TestBuildLinksSkipsSelfImports(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":            "module example.com/m\n",
		"internal/a/one.go": "package a\n\nimport \"example.com/m/internal/a\"\n",
		"internal/a/two.go": "package a\n",
	})
	if got := importLinks(t, root); len(got) != 0 {
		t.Fatalf("got %d links; want none (%+v)", len(got), got)
	}
}

// Without a module path there is no way to tell this repository's packages
// from anyone else's, so no import link can be claimed.
func TestBuildLinksWithoutAGoModClaimsNothing(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go": "package main\n\nimport \"example.com/m/internal/store\"\n",
	})
	if got := importLinks(t, root); len(got) != 0 {
		t.Fatalf("got %d links without a go.mod; want none", len(got))
	}
}

// Links are the one part of the built map that is derived rather than
// authored, so two runs over unchanged source must produce the same document.
func TestBuildLinksIsDeterministic(t *testing.T) {
	root := goTree(t)
	first, second := importLinks(t, root), importLinks(t, root)
	if len(first) != len(second) {
		t.Fatalf("link counts differ: %d then %d", len(first), len(second))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two runs over unchanged source produced different links:\n%+v\n%+v", first, second)
	}
}

func TestModulePath(t *testing.T) {
	root := writeTree(t, map[string]string{"go.mod": "// a comment\n\nmodule example.com/m/v2\n\ngo 1.25\n"})
	if got := modulePath(root); got != "example.com/m/v2" {
		t.Fatalf("modulePath = %q; want example.com/m/v2", got)
	}
	if got := modulePath(t.TempDir()); got != "" {
		t.Fatalf("modulePath with no go.mod = %q; want \"\"", got)
	}
}

// archWithExternals declares two services, one of which is reached through two
// separate third-party packages.
func archWithExternals() Architecture {
	return Architecture{
		Nodes: []Area{
			{ID: "db", Label: "Database", Kind: KindData, Paths: []string{"internal/db/**"}},
			{ID: "ext.sqlite", Label: "SQLite", Kind: KindExternal, Packages: []string{
				"go:github.com/ncruces/go-sqlite3", "go:github.com/pressly/goose",
			}},
			{ID: "ext.anthropic", Label: "Anthropic", Kind: KindExternal, Packages: []string{
				"go:github.com/anthropics/anthropic-sdk-go",
			}},
			// Declared only by hostname, so no import can match it.
			{ID: "ext.copilot", Label: "Copilot", Kind: KindExternal, Hosts: []string{"api.github.com"}},
		},
	}
}

func TestExternalLinksWeighEachMatchedPackage(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n",
		"internal/db/connect.go": `package db

import (
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/pressly/goose/v3"
)
`,
		"internal/llm/client.go": "package llm\n\nimport \"github.com/anthropics/anthropic-sdk-go\"\n",
	})
	files, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := ExternalLinks(files, FileImports(root, files), archWithExternals())

	byPair := map[string]Link{}
	for _, l := range got {
		byPair[l.A+"|"+l.B] = l
	}

	// Two of SQLite's declared packages are imported, so the link carries a
	// weight of two and names both.
	sqlite, ok := byPair["f:internal/db/connect.go|n:ext.sqlite"]
	if !ok {
		t.Fatalf("no link from connect.go to ext.sqlite; got %+v", got)
	}
	if sqlite.W != 2 {
		t.Errorf("weight = %v; want 2 (two declared packages imported)", sqlite.W)
	}
	if len(sqlite.Names) != 2 {
		t.Errorf("names = %v; want both matched package paths", sqlite.Names)
	}
	if sqlite.K != LinkImport {
		t.Errorf("kind = %q; want %q", sqlite.K, LinkImport)
	}

	if l, ok := byPair["f:internal/llm/client.go|n:ext.anthropic"]; !ok || l.W != 1 {
		t.Errorf("anthropic link = %+v, present=%v; want weight 1", l, ok)
	}
}

// A service known only by hostname cannot be found in an import list. Guessing
// one would put a dependency on the map that nothing supports.
func TestExternalLinksClaimNothingFromAHostnameAlone(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/m\n",
		"main.go": "package main\n\nimport \"net/http\"\n",
	})
	files, _ := Scan(context.Background(), root)
	for _, l := range ExternalLinks(files, FileImports(root, files), archWithExternals()) {
		if l.B == "n:ext.copilot" {
			t.Errorf("claimed a link to a host-only external: %+v", l)
		}
	}
}

// A package that merely starts with the same letters is a different package.
func TestExternalLinksMatchOnPathBoundaries(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/m\n",
		"main.go": "package main\n\nimport \"github.com/pressly/goosebumps\"\n",
	})
	files, _ := Scan(context.Background(), root)
	if got := ExternalLinks(files, FileImports(root, files), archWithExternals()); len(got) != 0 {
		t.Fatalf("goosebumps matched goose: %+v", got)
	}
}

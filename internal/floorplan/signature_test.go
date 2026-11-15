package floorplan

import (
	"os"
	"path/filepath"
	"testing"
)

func sigOf(t *testing.T, root, rel string) string {
	t.Helper()
	return Signature(root, rel, "go")
}

// A description describes what a file offers, not how it does it. Rewriting a
// function body, renaming a local, or reformatting changes nothing a
// description could have said -- so it must not invalidate one. With 505
// descriptions in this repository, a signature that tracked exact content
// would leave nearly all of them flagged stale nearly all of the time.
func TestSignatureIgnoresChangesBehindTheAPI(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": `package a

// Add returns the sum.
func Add(x, y int) int {
	return x + y
}
`})
	before := sigOf(t, root, "a.go")

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte(`package a

// Add returns the sum.
func Add(x, y int) int {
	total := x
	total += y
	return total
}
`), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if after := sigOf(t, root, "a.go"); after != before {
		t.Errorf("signature changed after an internal rewrite: %q then %q", before, after)
	}
}

// Anything a description could reasonably have mentioned must change it.
func TestSignatureTracksTheAPI(t *testing.T) {
	base := `package a

// Add returns the sum.
func Add(x, y int) int { return x + y }
`
	cases := map[string]string{
		"an added exported function": base + "\n// Sub subtracts.\nfunc Sub(x, y int) int { return x - y }\n",
		"a renamed exported function": `package a

// Plus returns the sum.
func Plus(x, y int) int { return x + y }
`,
		"an added exported type":  base + "\n// Calc does arithmetic.\ntype Calc struct{}\n",
		"an added exported const": base + "\n// Mode is the default.\nconst Mode = \"fast\"\n",
		"a changed doc comment": `// Package a does arithmetic, carefully.
package a

// Add returns the sum.
func Add(x, y int) int { return x + y }
`,
	}

	root := writeTree(t, map[string]string{"a.go": base})
	before := sigOf(t, root, "a.go")
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeTree(t, map[string]string{"a.go": content})
			if got := sigOf(t, dir, "a.go"); got == before {
				t.Errorf("signature unchanged by %s", name)
			}
		})
	}
}

// An unexported helper is not part of what a file offers.
func TestSignatureIgnoresUnexportedDeclarations(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n\nfunc Add(x, y int) int { return x + y }\n"})
	before := sigOf(t, root, "a.go")

	dir := writeTree(t, map[string]string{"a.go": "package a\n\nfunc Add(x, y int) int { return add(x, y) }\n\nfunc add(x, y int) int { return x + y }\n"})
	if got := sigOf(t, dir, "a.go"); got != before {
		t.Errorf("an unexported helper changed the signature: %q then %q", before, got)
	}
}

// Declaration order is not part of a file's API.
func TestSignatureIgnoresDeclarationOrder(t *testing.T) {
	a := writeTree(t, map[string]string{"a.go": "package a\n\nfunc Alpha() {}\n\nfunc Beta() {}\n"})
	b := writeTree(t, map[string]string{"a.go": "package a\n\nfunc Beta() {}\n\nfunc Alpha() {}\n"})
	if sigOf(t, a, "a.go") != sigOf(t, b, "a.go") {
		t.Error("reordering two exported functions changed the signature")
	}
}

// A served route is part of what a file offers, and a description of a server
// file is usually about exactly that.
func TestSignatureTracksRoutes(t *testing.T) {
	before := sigOf(t, writeTree(t, map[string]string{"s.go": `package s

import "net/http"

func routes(mux *http.ServeMux) { mux.HandleFunc("GET /a", nil) }
`}), "s.go")
	after := sigOf(t, writeTree(t, map[string]string{"s.go": `package s

import "net/http"

func routes(mux *http.ServeMux) { mux.HandleFunc("GET /b", nil) }
`}), "s.go")
	if before == after {
		t.Error("changing a served route did not change the signature")
	}
}

// Without a parser there is nothing to extract, so the signature falls back to
// exact content: a description is then flagged more eagerly than it needs to
// be, which is the safe direction to be wrong in.
func TestSignatureFallsBackToContentForOtherLanguages(t *testing.T) {
	root := writeTree(t, map[string]string{"a.ts": "export const a = 1;\n"})
	first := Signature(root, "a.ts", "typescript")
	if first == "" {
		t.Fatal("no signature for a TypeScript file")
	}
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export const a = 2;\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if Signature(root, "a.ts", "typescript") == first {
		t.Error("the fallback signature did not change with the content")
	}
}

func TestSignatureOfAnUnreadableFileIsEmpty(t *testing.T) {
	if got := Signature(t.TempDir(), "missing.go", "go"); got != "" {
		t.Errorf("Signature = %q; want \"\"", got)
	}
}

// Stale descriptions are the ones whose recorded signature no longer matches.
func TestStaleDescriptions(t *testing.T) {
	files := []FileRec{
		{P: "fresh.go", S: "aaa", D: "described", Sig: "aaa"},
		{P: "moved-on.go", S: "bbb", D: "described", Sig: "old"},
		{P: "undescribed.go", S: "ccc"},
		{P: "no-signature.go", S: "", D: "described", Sig: "ddd"},
	}
	got := StaleDescriptions(files)
	if len(got) != 1 || got[0] != "moved-on.go" {
		t.Fatalf("StaleDescriptions = %v; want [moved-on.go]", got)
	}
}

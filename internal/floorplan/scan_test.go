package floorplan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeTree creates the given files (content keyed by repo-relative path) under
// a fresh temp dir and returns the root.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func scanned(t *testing.T, root string) map[string]ScannedFile {
	t.Helper()
	files, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	out := make(map[string]ScannedFile, len(files))
	for _, f := range files {
		out[f.Path] = f
	}
	return out
}

func TestScanReportsPathCategoryLanguageAndLineCount(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"internal/db/x.sql": "SELECT 1;\n",
		"web/app.ts":        "export const a = 1;\n",
		"README.md":         "# Title\n\ntext\n",
		"go.sum":            "mod v1 h1:abc\n",
	})
	got := scanned(t, root)

	cases := map[string]struct {
		cat  string
		lang string
		loc  int
	}{
		"main.go":           {CategoryCode, "go", 2}, // the blank line does not count
		"internal/db/x.sql": {CategoryCode, "sql", 1},
		"web/app.ts":        {CategoryCode, "typescript", 1},
		"README.md":         {CategoryDocs, "", 2},
		"go.sum":            {CategoryLock, "", 1},
	}
	for path, want := range cases {
		f, ok := got[path]
		if !ok {
			t.Errorf("%s was not scanned", path)
			continue
		}
		if f.Category != want.cat {
			t.Errorf("%s category = %q; want %q", path, f.Category, want.cat)
		}
		if f.Lang != want.lang {
			t.Errorf("%s lang = %q; want %q", path, f.Lang, want.lang)
		}
		if f.LOC != want.loc {
			t.Errorf("%s loc = %d; want %d", path, f.LOC, want.loc)
		}
	}
}

// A map of the repository must not be padded out with dependencies, build
// output or version-control internals: they are not the project's own code,
// and node_modules alone would dwarf everything the map is for.
func TestScanSkipsDependenciesAndBuildOutput(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":                   "package main\n",
		"vendor/dep/dep.go":         "package dep\n",
		"node_modules/pkg/index.js": "module.exports = 1\n",
		".git/config":               "[core]\n",
		"dist/bundle.js":            "var a=1\n",
		".aux/state.json":           "{}\n",
		".pytest_cache/v/x.json":    "{}\n",
	})
	got := scanned(t, root)
	if len(got) != 1 {
		t.Fatalf("scanned %v; want only main.go", keysOf(got))
	}
	if _, ok := got["main.go"]; !ok {
		t.Fatalf("main.go missing; got %v", keysOf(got))
	}
}

// A dot-directory is not automatically excluded. .github holds the workflows
// that decide whether the project ships and .vscode its editor settings; both
// are the project's own configuration, and a blanket rule about leading dots
// drops them from the map without saying so.
func TestScanKeepsProjectConfigInDotDirectories(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":                    "package main\n",
		".github/workflows/test.yml": "name: test\n",
		".vscode/settings.json":      "{}\n",
	})
	got := scanned(t, root)

	f, ok := got[".github/workflows/test.yml"]
	if !ok {
		t.Fatalf(".github/workflows/test.yml is missing; got %v", keysOf(got))
	}
	if f.Category != CategoryConfig {
		t.Errorf("workflow category = %q; want %q", f.Category, CategoryConfig)
	}
	if _, ok := got[".vscode/settings.json"]; !ok {
		t.Error(".vscode/settings.json is missing from the map")
	}
}

// Paths are the join key between the scan, the architecture's globs and the
// agent's descriptions, so they must be repo-relative and slash-separated on
// every platform.
func TestScanPathsAreRelativeAndSlashed(t *testing.T) {
	root := writeTree(t, map[string]string{"a/b/c.go": "package c\n"})
	got := scanned(t, root)
	if _, ok := got["a/b/c.go"]; !ok {
		t.Fatalf("want key a/b/c.go; got %v", keysOf(got))
	}
}

// The hash is what lets a description be flagged as written against content
// the file no longer has.
func TestScanHashTracksContent(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	first := scanned(t, root)["a.go"]
	if first.Hash == "" {
		t.Fatal("Hash is empty")
	}

	same := scanned(t, root)["a.go"]
	if same.Hash != first.Hash {
		t.Fatalf("Hash changed without an edit: %q then %q", first.Hash, same.Hash)
	}

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nvar X = 1\n"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	changed := scanned(t, root)["a.go"]
	if changed.Hash == first.Hash {
		t.Fatalf("Hash %q unchanged after an edit", changed.Hash)
	}
}

// A file with no trailing newline still has a last line.
func TestScanCountsAFinalLineWithoutNewline(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nvar X = 1"})
	if loc := scanned(t, root)["a.go"].LOC; loc != 2 {
		t.Fatalf("loc = %d; want 2", loc)
	}
}

// A line count is a measure of content, so whitespace must not pad it.
func TestScanCountsOnlyLinesWithContent(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\n\n\n\t\n   \nvar X = 1\n",
	})
	if loc := scanned(t, root)["a.go"].LOC; loc != 2 {
		t.Fatalf("loc = %d; want 2 (blank and whitespace-only lines must not count)", loc)
	}
}

func TestScanCountsAnEmptyFileAsZero(t *testing.T) {
	root := writeTree(t, map[string]string{"empty.go": ""})
	if loc := scanned(t, root)["empty.go"].LOC; loc != 0 {
		t.Fatalf("loc = %d; want 0", loc)
	}
}

// Images, fonts and archives are recorded as assets rather than dropped, so
// the scan still accounts for every file in the repository -- but they are
// never opened, because their byte count is not a line count and hashing a
// large binary buys nothing.
func TestScanCategorisesAssetsWithoutReadingThem(t *testing.T) {
	root := writeTree(t, map[string]string{
		"main.go":    "package main\n",
		"logo.png":   "\x89PNG\r\n\x1a\n",
		"font.woff2": "wOF2",
	})
	got := scanned(t, root)

	for _, name := range []string{"logo.png", "font.woff2"} {
		f, ok := got[name]
		if !ok {
			t.Errorf("%s is missing from the scan entirely", name)
			continue
		}
		if f.Category != CategoryAsset {
			t.Errorf("%s category = %q; want %q", name, f.Category, CategoryAsset)
		}
		if f.LOC != 0 || f.Hash != "" {
			t.Errorf("%s was read (loc=%d hash=%q); an asset must not be", name, f.LOC, f.Hash)
		}
	}
	if got["main.go"].Category != CategoryCode {
		t.Errorf("main.go category = %q; want code", got["main.go"].Category)
	}
}

// An unreadable entry must not abort the walk: a map of the other 500 files is
// worth far more than no map at all.
func TestScanSurvivesAnUnreadableFile(t *testing.T) {
	root := writeTree(t, map[string]string{"ok.go": "package ok\n", "bad.go": "package bad\n"})
	if err := os.Chmod(filepath.Join(root, "bad.go"), 0o000); err != nil {
		t.Skipf("cannot chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "bad.go"), 0o644) })

	files, err := Scan(context.Background(), root)
	if err != nil {
		t.Fatalf("Scan returned an error for one unreadable file: %v", err)
	}
	for _, f := range files {
		if f.Path == "ok.go" {
			return
		}
	}
	t.Fatalf("ok.go missing from %d scanned files", len(files))
}

func keysOf(m map[string]ScannedFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A repository's own ignore rules already say which files are not part of the
// project. Honouring them is what keeps build output, caches and local scratch
// files out of the map without the map having to guess at names.
func TestScanHonoursGitignore(t *testing.T) {
	root := writeTree(t, map[string]string{
		".gitignore":       "ignored.go\nscratch/\n",
		"main.go":          "package main\n",
		"ignored.go":       "package ignored\n",
		"scratch/notes.go": "package scratch\n",
	})
	gitInit(t, root)

	got := scanned(t, root)
	if _, ok := got["ignored.go"]; ok {
		t.Error("ignored.go is in the map although .gitignore excludes it")
	}
	if _, ok := got["scratch/notes.go"]; ok {
		t.Error("scratch/notes.go is in the map although .gitignore excludes its directory")
	}
	if _, ok := got["main.go"]; !ok {
		t.Errorf("main.go missing; got %v", keysOf(got))
	}
}

// A file written a moment ago and not yet committed is exactly the file someone
// is working on, so it has to be on the map before it is staged.
func TestScanIncludesUncommittedFiles(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	gitInit(t, root)
	if err := os.WriteFile(filepath.Join(root, "brand-new.go"), []byte("package brandnew\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, ok := scanned(t, root)["brand-new.go"]; !ok {
		t.Fatal("a new, uncommitted file is missing from the map")
	}
}

// gitInit makes root a repository. It commits nothing: `git ls-files -co`
// reports the working tree, so no identity or commit is needed.
func gitInit(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v: %s", err, out)
	}
}

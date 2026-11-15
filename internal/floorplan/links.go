package floorplan

import (
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// BuildLinks derives the import links between the repository's own files.
//
// An import names a package, but the map is drawn between files, so each
// import resolves to one representative file of the package it names: the file
// named after its directory when there is one, and otherwise that package's
// alphabetically first non-test source file. Picking a single
// representative keeps the map readable -- fanning one import out to every
// file of the imported package would multiply the same fact by the size of
// that package and bury the structure it is meant to show.
//
// Only this module's own packages become links. An import of a third-party
// dependency is somebody else's code, and drawing it as a room here would
// claim it lives in this repository.
//
// Everything here is derived from source. Links that cross a boundary no
// parser can see -- a child process, an HTTP call, a queue -- cannot be found
// this way and are only ever authored.
func BuildLinks(root string, files []ScannedFile, imports map[string][]string) []Link {
	module := modulePath(root)
	if module == "" {
		return nil // nothing distinguishes this module's packages from any other
	}
	rep := representativeFiles(files)

	var links []Link
	seen := make(map[[2]string]bool)
	for _, f := range files {
		if f.Lang != "go" {
			continue
		}
		for _, imp := range imports[f.Path] {
			dir, ok := moduleDir(module, imp)
			if !ok {
				continue // outside the module: somebody else's code
			}
			target, ok := rep[dir]
			if !ok || target == f.Path {
				continue // nothing to point at, or the file itself
			}
			key := [2]string{f.Path, target}
			if seen[key] {
				continue
			}
			seen[key] = true
			links = append(links, Link{A: "f:" + f.Path, B: "f:" + target, K: LinkImport, W: 1})
		}
	}
	// Derived output has to be byte-identical between runs over unchanged
	// source, or every rebuild looks like a change.
	sort.Slice(links, func(i, j int) bool {
		if links[i].A != links[j].A {
			return links[i].A < links[j].A
		}
		return links[i].B < links[j].B
	})
	return links
}

// representativeFiles maps each package directory to the one file that stands
// for it.
func representativeFiles(files []ScannedFile) map[string]string {
	rep := make(map[string]string)
	for _, f := range files {
		if f.Lang != "go" || strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		dir := path.Dir(f.Path)
		cur, ok := rep[dir]
		if !ok || betterRepresentative(dir, f.Path, cur) {
			rep[dir] = f.Path
		}
	}
	return rep
}

// betterRepresentative reports whether candidate should stand for dir in place
// of current.
//
// A package's own named file -- hooks/hooks.go, store/store.go -- is the one a
// reader opens first, so it wins outright. Only when no such file exists does
// the choice fall back to alphabetical order, which is arbitrary but stable,
// and stability is what matters: an unstable choice would redraw half the
// map's links on an unrelated rename.
func betterRepresentative(dir, candidate, current string) bool {
	named := path.Base(dir) + ".go"
	switch {
	case path.Base(candidate) == named:
		return true
	case path.Base(current) == named:
		return false
	default:
		return candidate < current
	}
}

// moduleDir turns an import path into the repo-relative directory holding that
// package, reporting false when the import is not part of this module.
func moduleDir(module, importPath string) (string, bool) {
	if importPath == module {
		return ".", true
	}
	rest, ok := strings.CutPrefix(importPath, module+"/")
	if !ok {
		return "", false
	}
	return rest, true
}

// FileImports reads every Go file's import paths, once.
//
// Both kinds of derived link need this: the file-to-file links below and the
// links to external services. Parsing the tree twice to answer the same
// question would double the cost of building the map on startup, which is
// already the part of this feature most likely to be felt.
func FileImports(root string, files []ScannedFile) map[string][]string {
	out := make(map[string][]string, len(files))
	for _, f := range files {
		if f.Lang != "go" {
			continue
		}
		if imps := goImports(filepath.Join(root, filepath.FromSlash(f.Path))); len(imps) > 0 {
			out[f.Path] = imps
		}
	}
	return out
}

// goPackagePrefix marks an external's declared package as a Go import path.
// The prefix exists so one external can claim packages in several languages.
const goPackagePrefix = "go:"

// ExternalLinks draws a link from a file to an external service it reaches
// through a third-party package.
//
// Which packages belong to which service cannot be derived: nothing in
// "github.com/ncruces/go-sqlite3" says it is the same dependency as
// "github.com/pressly/goose". That judgement is authored, declared on the
// external area, and this only applies it. The weight is how many of the
// external's packages a file imports, and the matched paths are carried on the
// link so the claim can be checked rather than taken on trust.
func ExternalLinks(files []ScannedFile, imports map[string][]string, arch Architecture) []Link {
	type ext struct {
		id       string
		prefixes []string
	}
	var exts []ext
	for _, a := range arch.Nodes {
		if a.Kind != KindExternal {
			continue
		}
		var prefixes []string
		for _, p := range a.Packages {
			if after, ok := strings.CutPrefix(p, goPackagePrefix); ok {
				prefixes = append(prefixes, after)
			}
		}
		if len(prefixes) > 0 {
			exts = append(exts, ext{id: a.ID, prefixes: prefixes})
		}
	}
	if len(exts) == 0 {
		return nil
	}

	var links []Link
	for _, f := range files {
		imps := imports[f.Path]
		if len(imps) == 0 {
			continue
		}
		for _, e := range exts {
			matched := matchedPackages(imps, e.prefixes)
			if len(matched) == 0 {
				continue
			}
			links = append(links, Link{
				A: "f:" + f.Path, B: "n:" + e.id, K: LinkImport,
				W: float64(len(matched)), Names: matched,
			})
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].A != links[j].A {
			return links[i].A < links[j].A
		}
		return links[i].B < links[j].B
	})
	return links
}

// matchedPackages returns the external packages a file imports, each named
// once, in a stable order.
func matchedPackages(imports, prefixes []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, imp := range imports {
		for _, pre := range prefixes {
			if imp == pre || strings.HasPrefix(imp, pre+"/") {
				if !seen[pre] {
					seen[pre] = true
					out = append(out, pre)
				}
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// goImports reads one Go file's import paths. Only the imports are parsed, so
// a file whose body does not compile still contributes its dependencies.
func goImports(abs string) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, abs, nil, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(f.Imports))
	for _, spec := range f.Imports {
		if spec.Path == nil {
			continue
		}
		out = append(out, strings.Trim(spec.Path.Value, `"`))
	}
	return out
}

// modulePath reads the module path from go.mod, or returns "" when there is
// none to read.
func modulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

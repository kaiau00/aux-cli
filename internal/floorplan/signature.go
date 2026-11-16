package floorplan

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Signature hashes the parts of a file that a description depends on: what it
// exports, what it serves, and what its doc comment says.
//
// This is deliberately not a hash of the file. A description says what a file
// offers, so rewriting a body, renaming a local or reformatting changes
// nothing a description could have claimed, and must not invalidate one. With
// several hundred descriptions in a repository, a staleness check keyed on
// exact content would leave nearly all of them flagged nearly all of the time,
// which is the same as having no staleness check at all.
//
// For a language this package cannot parse the signature falls back to the
// file's content, so descriptions are flagged more eagerly than they need to
// be. That is the safe direction: an unnecessary re-read costs a little, and a
// summary that quietly describes a file that has since changed costs trust.
func Signature(root, rel, lang string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return analyzeFile(rel, lang, data).Signature
}

// exportedNames lists what a file offers to other packages, sorted, since
// declaration order is not part of an API.
func exportedNames(file *ast.File) []string {
	var out []string
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if !d.Name.IsExported() {
				continue
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				name = receiverName(d.Recv.List[0].Type) + "." + name
			}
			out = append(out, name)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				out = append(out, exportedSpecNames(spec)...)
			}
		}
	}
	sort.Strings(out)
	return out
}

// exportedSpecNames lists the exported names a type, const or var spec
// declares.
func exportedSpecNames(spec ast.Spec) []string {
	var out []string
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if s.Name.IsExported() {
			out = append(out, s.Name.Name)
		}
	case *ast.ValueSpec:
		for _, name := range s.Names {
			if name.IsExported() {
				out = append(out, name.Name)
			}
		}
	}
	return out
}

// receiverName is the type name a method is declared on, without its pointer
// or type parameters.
func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	default:
		return ""
	}
}

// docText is the file's own doc comment, which is usually what a description
// of it was written from.
func docText(file *ast.File) string {
	if file.Doc == nil {
		return ""
	}
	return strings.TrimSpace(file.Doc.Text())
}

// digest is the short hash written into the map.
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:hashLen]
}

// StaleDescriptions lists the described files whose signature has moved on
// since the description was written.
//
// A stale description is reported, not discarded: a summary that says it may
// be out of date is still the best account of the file anybody has written,
// and deleting it would throw away work that cost real money to produce.
func StaleDescriptions(files []FileRec) []string {
	var out []string
	for _, f := range files {
		if f.D == "" && f.Name == "" {
			continue // nothing has been written about this file
		}
		if f.Sig == "" || f.S == "" {
			continue // no signature recorded on one side: nothing to compare
		}
		if f.Sig != f.S {
			out = append(out, f.P)
		}
	}
	sort.Strings(out)
	return out
}

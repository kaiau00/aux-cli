package floorplan

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// FileFacts is everything this package reads out of one file's contents.
//
// It exists so the repository is read once and parsed once. Imports, served
// routes, addressed URL paths and the signature a description depends on were
// each gathered by their own pass to begin with, which meant four reads and
// three parses of every Go file -- on a map that is rebuilt whenever Aux
// starts, in a repository of five hundred files.
type FileFacts struct {
	// Imports are the file's import paths, in source order.
	Imports []string
	// Routes are the HTTP routes the file registers.
	Routes []Route
	// Paths are the URL paths the file appears to address.
	Paths []string
	// Literals are the places a hostname may legitimately appear: string
	// literals for Go, and the whole file for a language with no parser here.
	Literals [][]byte
	// Signature hashes the parts of the file a description depends on.
	Signature string
}

// Facts holds one entry per scanned file, keyed by repo-relative path.
type Facts map[string]FileFacts

// analyzeFile derives everything readable from one file's contents.
//
// The bytes are passed in because the scan has already read them: going back
// to disk here would double the I/O for no new information.
func analyzeFile(rel, lang string, data []byte) FileFacts {
	if lang != "go" {
		return FileFacts{
			Paths:     quotedPaths(data),
			Literals:  [][]byte{data},
			Signature: digest(data),
		}
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, data, parser.ParseComments)
	if err != nil {
		// A file that does not parse still belongs on the map; only the facts
		// that need a parse tree are unavailable.
		return FileFacts{Literals: [][]byte{data}}
	}

	facts := FileFacts{Routes: []Route{}}
	imports := make(map[*ast.BasicLit]bool, len(file.Imports))
	for _, spec := range file.Imports {
		imports[spec.Path] = true
		if spec.Path != nil {
			facts.Imports = append(facts.Imports, strings.Trim(spec.Path.Value, `"`))
		}
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING || imports[node] {
				return true
			}
			v, err := strconv.Unquote(node.Value)
			if err != nil {
				return true
			}
			facts.Literals = append(facts.Literals, []byte(v))
			if p, ok := urlPath(v); ok {
				facts.Paths = append(facts.Paths, p)
			}
		case *ast.CallExpr:
			if r, ok := routeFromCall(node, rel, fset); ok {
				facts.Routes = append(facts.Routes, r)
			}
		}
		return true
	})

	facts.Signature = goSignature(file, facts.Routes)
	return facts
}

// routeFromCall reads a route out of a ServeMux registration.
func routeFromCall(call *ast.CallExpr, rel string, fset *token.FileSet) (Route, bool) {
	if len(call.Args) == 0 {
		return Route{}, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !routeRegistrars[sel.Sel.Name] {
		return Route{}, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return Route{}, false // built at runtime: not readable here
	}
	pattern, err := strconv.Unquote(lit.Value)
	if err != nil || pattern == "" {
		return Route{}, false
	}
	method, path := splitPattern(pattern)
	return Route{F: rel, M: method, P: path, L: fset.Position(lit.Pos()).Line}, true
}

// goSignature hashes what a Go file offers: its exported names, the routes it
// serves, and its own doc comment.
func goSignature(file *ast.File, routes []Route) string {
	served := make([]string, 0, len(routes))
	for _, r := range routes {
		served = append(served, r.M+" "+r.P)
	}
	sort.Strings(served)

	parts := struct {
		Exports []string `json:"exports"`
		Routes  []string `json:"routes"`
		Doc     string   `json:"doc"`
	}{
		Exports: exportedNames(file),
		Routes:  served,
		Doc:     docText(file),
	}
	encoded, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return digest(encoded)
}

// GoRoutes collects the routes registered across the repository.
func GoRoutes(files []ScannedFile, facts Facts) []Route {
	var out []Route
	for _, f := range files {
		out = append(out, facts[f.Path].Routes...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].F != out[j].F {
			return out[i].F < out[j].F
		}
		return out[i].L < out[j].L
	})
	return out
}

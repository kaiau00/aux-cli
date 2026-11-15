package floorplan

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// methodAny labels a route registered without a method, which therefore serves
// every method.
const methodAny = "ANY"

// routeRegistrars are the ServeMux methods that register a pattern.
var routeRegistrars = map[string]bool{"HandleFunc": true, "Handle": true}

// httpMethods are the methods Go's ServeMux accepts at the front of a pattern.
var httpMethods = map[string]bool{
	"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "CONNECT": true, "OPTIONS": true, "TRACE": true,
}

// GoRoutes finds the HTTP routes a repository serves, by reading the patterns
// passed to ServeMux registrations.
//
// Since Go 1.22 a pattern may carry its method -- "GET /tasks" -- and the
// method is the most important thing about a route: it is what separates a
// page you can read from an endpoint that changes something. A route table
// that reported every route as ANY would not describe the server.
//
// A pattern assembled at runtime cannot be read from source, so it is skipped
// rather than guessed at: an endpoint on the map that does not exist is worse
// than one that is missing.
func GoRoutes(root string, files []ScannedFile) []Route {
	var out []Route
	for _, f := range files {
		if f.Lang != "go" {
			continue
		}
		out = append(out, goRoutesIn(root, f.Path)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].F != out[j].F {
			return out[i].F < out[j].F
		}
		return out[i].L < out[j].L
	})
	return out
}

// goRoutesIn reads one file's route registrations.
func goRoutesIn(root, rel string) []Route {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, 0)
	if err != nil {
		return nil
	}
	var out []Route
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !routeRegistrars[sel.Sel.Name] {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true // built at runtime: not readable here
		}
		pattern, err := strconv.Unquote(lit.Value)
		if err != nil || pattern == "" {
			return true
		}
		method, path := splitPattern(pattern)
		out = append(out, Route{F: rel, M: method, P: path, L: fset.Position(lit.Pos()).Line})
		return true
	})
	return out
}

// splitPattern separates an optional leading method from the path.
func splitPattern(pattern string) (method, path string) {
	if head, rest, found := strings.Cut(pattern, " "); found {
		if httpMethods[strings.ToUpper(head)] {
			return strings.ToUpper(head), strings.TrimSpace(rest)
		}
	}
	return methodAny, pattern
}

// HostLinks draws a link from a file to an external service whose hostname the
// file names.
//
// A dependency reached over HTTP leaves no trace in an import list -- the only
// evidence is a hostname in the source. In Go only string literals are
// searched, because a hostname in a comment is a mention and not a call: one
// file in this repository names api.githubcopilot.com in a note about model
// ids and never contacts it, and a link drawn from that would be a false
// claim about what the file does.
//
// Even in a literal this is good evidence rather than proof -- the string may
// sit behind a branch that never runs -- so every link is marked inferred, and
// the map distinguishes what it read from what it concluded.
func HostLinks(root string, files []ScannedFile, arch Architecture) []Link {
	type ext struct {
		id    string
		hosts []string
	}
	var exts []ext
	for _, a := range arch.Nodes {
		if a.Kind == KindExternal && len(a.Hosts) > 0 {
			exts = append(exts, ext{id: a.ID, hosts: a.Hosts})
		}
	}
	if len(exts) == 0 {
		return nil
	}

	var links []Link
	for _, f := range files {
		if !CountsTowardLOC(f.Category) {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(f.Path))
		haystacks, ok := stringLiterals(abs, f.Lang)
		if !ok {
			continue
		}
		for _, e := range exts {
			found := matchedHosts(haystacks, e.hosts)
			if len(found) == 0 {
				continue
			}
			links = append(links, Link{
				A: "f:" + f.Path, B: "n:" + e.id, K: LinkHTTP,
				W: float64(len(found)), Labels: found, Inferred: true,
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

// stringLiterals returns the places in a file a hostname may legitimately be
// found.
//
// Go is parsed, so comments are excluded and so are import paths -- an import
// of ".../internal/impact" contains "/impact" and would otherwise look like a
// call to the route of that name, which is how nearly fifty false links were
// drawn before this excluded them.
//
// For any other language the whole file is searched. There is no parser here,
// and writing one is not worth it: a hostname is distinctive enough that a
// boundary check carries the weight, and these links are marked inferred in
// any case.
func stringLiterals(abs, lang string) ([][]byte, bool) {
	if lang == "go" {
		lits, err := goStringLiterals(abs)
		return lits, err == nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return [][]byte{data}, true
}

// goStringLiterals returns the value of every string literal in a Go file,
// except the paths of its imports.
func goStringLiterals(abs string) ([][]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, abs, nil, 0)
	if err != nil {
		return nil, err
	}
	imports := make(map[*ast.BasicLit]bool, len(file.Imports))
	for _, spec := range file.Imports {
		imports[spec.Path] = true
	}
	var out [][]byte
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || imports[lit] {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil {
			out = append(out, []byte(v))
		}
		return true
	})
	return out, nil
}

// pathCandidates returns the URL paths a file appears to address.
//
// Go is parsed and its literals read. For every other language the file is
// scanned for a path that opens a quoted string -- `"/api/snapshot"` -- rather
// than tokenised into strings first. Tokenising looked simpler and was wrong:
// sessions.js contains the regular expression /[&<>"']/g, whose quote
// desynchronises a naive scanner so that every string after it is
// mis-read, and the three endpoints that file actually calls went unseen.
// Matching the quote and the path together needs no lexer and cannot drift.
func pathCandidates(abs, lang string) ([]string, bool) {
	if lang == "go" {
		lits, err := goStringLiterals(abs)
		if err != nil {
			return nil, false
		}
		var out []string
		for _, l := range lits {
			if p, ok := urlPath(string(l)); ok {
				out = append(out, p)
			}
		}
		return out, true
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, false
	}
	return quotedPaths(data), true
}

// pathEnd characters cannot continue a URL path written in source.
func isPathEnd(c byte) bool {
	switch c {
	case '"', '\'', '`', ' ', '\t', '\n', '\r', '+', ')', '?', '#', ',', ';':
		return true
	default:
		return false
	}
}

// quotedPaths finds every root-relative path that begins a quoted string.
func quotedPaths(data []byte) []string {
	var out []string
	for i := 0; i+1 < len(data); i++ {
		q := data[i]
		if q != '"' && q != '\'' && q != '`' {
			continue
		}
		if data[i+1] != '/' {
			continue
		}
		end := i + 1
		for end < len(data) && !isPathEnd(data[end]) {
			end++
		}
		out = append(out, string(data[i+1:end]))
	}
	return out
}

// matchedHosts returns the hosts named anywhere in the haystacks, each once,
// in a stable order. A hostname must end at a character that cannot continue
// it, so "api.openai.com" does not match "notapi.openai.com.evil.test".
func matchedHosts(haystacks [][]byte, hosts []string) []string {
	var out []string
	for _, h := range hosts {
		for _, data := range haystacks {
			if containsHost(data, h) {
				out = append(out, h)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// containsHost reports whether data names host as a whole hostname.
func containsHost(data []byte, host string) bool {
	needle := []byte(host)
	for off := 0; ; {
		i := bytes.Index(data[off:], needle)
		if i < 0 {
			return false
		}
		start := off + i
		end := start + len(needle)
		if !hostBoundary(data, start-1) || !hostBoundary(data, end) {
			off = start + 1
			continue
		}
		return true
	}
}

// hostBoundary reports whether the byte at i cannot be part of a hostname, so
// a match that ends there is a whole host rather than a fragment of a longer
// one.
func hostBoundary(data []byte, i int) bool {
	if i < 0 || i >= len(data) {
		return true
	}
	c := data[i]
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '.' || c == '-':
		return false
	default:
		return true
	}
}

// minRoutePathLen is the shortest route path worth matching. "/" appears in
// almost every file that contains a path at all, so matching it would link
// most of the repository to whichever file serves the index.
const minRoutePathLen = 2

// RouteLinks draws a link from a file that names a served route's path to the
// file that serves it.
//
// This is how a caller is connected to its endpoint: the dashboard's
// JavaScript asks for /api/snapshot, and the Go file registering that pattern
// is what answers. Neither side imports the other, so no import graph contains
// this edge.
//
// Like host matching this reads strings, so it is inference and is marked as
// such. Wildcard segments are trimmed to their literal prefix, since a caller
// writes "/api/v1/tasks/abc123" where the pattern says "{id}".
func RouteLinks(root string, files []ScannedFile, routes []Route) []Link {
	type target struct {
		prefix string
		label  string
		file   string
	}
	var targets []target
	for _, r := range routes {
		prefix := literalRoutePrefix(r.P)
		if len(prefix) < minRoutePathLen {
			continue
		}
		targets = append(targets, target{prefix: prefix, label: r.M + " " + r.P, file: r.F})
	}
	if len(targets) == 0 {
		return nil
	}

	var links []Link
	for _, f := range files {
		if !CountsTowardLOC(f.Category) {
			continue
		}
		candidates, ok := pathCandidates(filepath.Join(root, filepath.FromSlash(f.Path)), f.Lang)
		if !ok {
			continue
		}
		// One file may call several routes served by the same file; that is
		// one link carrying every matched pattern.
		labels := map[string][]string{}
		for _, t := range targets {
			if t.file == f.Path {
				continue // the file serving the route is not calling it
			}
			if namesPath(candidates, t.prefix) && !containsLabel(labels[t.file], t.label) {
				labels[t.file] = append(labels[t.file], t.label)
			}
		}
		for file, ls := range labels {
			sort.Strings(ls)
			links = append(links, Link{
				A: "f:" + f.Path, B: "f:" + file, K: LinkHTTP,
				W: float64(len(ls)), Labels: ls, Inferred: true,
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

// literalRoutePrefix trims a pattern at its first wildcard, leaving the part a
// caller's own URL must begin with.
//
// The trailing slash is kept, because it is what distinguishes a subtree from
// an exact path: dropping it would make "/api/v1/tasks/{id}" and
// "/api/v1/tasks" the same prefix, and a caller of the list endpoint would be
// credited with calling the detail one.
func literalRoutePrefix(pattern string) string {
	if i := strings.IndexAny(pattern, "{*"); i >= 0 {
		return pattern[:i]
	}
	return pattern
}

// namesPath reports whether any candidate path addresses this route.
//
// A prefix ending in "/" is a subtree, and the slash is itself the boundary:
// "/api/v1/tasks/" is addressed by "/api/v1/tasks/abc123". Any other prefix
// has to end at a boundary, so "/api/sessions" does not match
// "/api/sessionsextra".
func namesPath(candidates []string, prefix string) bool {
	subtree := strings.HasSuffix(prefix, "/")
	for _, p := range candidates {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		if subtree || pathBoundary([]byte(p), len(prefix)) {
			return true
		}
	}
	return false
}

// urlPath returns the path part of a string that addresses something: the
// whole of a root-relative path, or what follows the host of an absolute URL.
func urlPath(s string) (string, bool) {
	if strings.HasPrefix(s, "/") {
		return s, true
	}
	if i := strings.Index(s, "://"); i >= 0 {
		if j := strings.IndexByte(s[i+3:], '/'); j >= 0 {
			return s[i+3+j:], true
		}
		return "/", true
	}
	return "", false
}

// pathBoundary reports whether a path match ending at i ends cleanly, so
// "/api/sessions" does not match "/api/sessionsextra".
func pathBoundary(data []byte, i int) bool {
	if i >= len(data) {
		return true
	}
	c := data[i]
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return false
	case c == '-' || c == '_':
		return false
	default:
		return true
	}
}

func containsLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

package floorplan

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func routeFixture(t *testing.T) (string, []ScannedFile, Facts) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n",
		"server.go": `package main

import "net/http"

func routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", index)
	mux.HandleFunc("GET /tasks", tasks)
	mux.HandleFunc("POST /api/v1/tasks", create)
	mux.HandleFunc("GET /api/v1/tasks/{id}", view)
	mux.HandleFunc("/api/sessions/", sessions)
	mux.Handle("/static/", http.NotFoundHandler())
	mux.HandleFunc(dynamicPattern, other) // not a literal: cannot be read
}
`,
		"notes.md": "# not Go\n",
	})
	files, facts, err := ScanAndAnalyze(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanAndAnalyze: %v", err)
	}
	return root, files, facts
}

// Go 1.22's ServeMux takes the method in the pattern. A route table that
// reports every one of them as ANY is not a description of the server: it
// cannot distinguish a page from the endpoint that changes it.
func TestGoRoutesReadsTheMethodFromThePattern(t *testing.T) {
	_, files, facts := routeFixture(t)
	got := GoRoutes(files, facts)

	byPath := map[string]Route{}
	for _, r := range got {
		byPath[r.M+" "+r.P] = r
	}
	for _, want := range []string{
		"ANY /", "GET /tasks", "POST /api/v1/tasks",
		"GET /api/v1/tasks/{id}", "ANY /api/sessions/", "ANY /static/",
	} {
		if _, ok := byPath[want]; !ok {
			t.Errorf("route %q was not found in %+v", want, byPath)
		}
	}
	if len(got) != 6 {
		t.Errorf("found %d routes; want 6", len(got))
	}
}

// The line number is what makes a route on the map clickable, so it has to be
// the line the route is registered on.
func TestGoRoutesRecordTheirFileAndLine(t *testing.T) {
	_, files, facts := routeFixture(t)
	for _, r := range GoRoutes(files, facts) {
		if r.F != "server.go" {
			t.Errorf("route %s %s file = %q; want server.go", r.M, r.P, r.F)
		}
		if r.L <= 0 {
			t.Errorf("route %s %s has line %d", r.M, r.P, r.L)
		}
	}
}

// A pattern built at runtime cannot be read from the source. Guessing one
// would put an endpoint on the map that may not exist.
func TestGoRoutesSkipNonLiteralPatterns(t *testing.T) {
	_, files, facts := routeFixture(t)
	for _, r := range GoRoutes(files, facts) {
		if r.P == "" || r.P == "dynamicPattern" {
			t.Errorf("a non-literal pattern was recorded: %+v", r)
		}
	}
}

func TestGoRoutesAreDeterministic(t *testing.T) {
	_, files, facts := routeFixture(t)
	first, second := GoRoutes(files, facts), GoRoutes(files, facts)
	if len(first) != len(second) {
		t.Fatalf("counts differ: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("route %d differs: %+v then %+v", i, first[i], second[i])
		}
	}
}

// A file that names an external service's host is talking to that service.
// This is the only way such a dependency can be found: it is a string in the
// source, not an import.
func TestHostLinksFindExternalServicesByHostname(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n",
		"internal/llm/copilot.go": `package llm

const (
	tokenURL = "https://api.github.com/copilot_internal/v2/token"
	chatURL  = "https://api.githubcopilot.com/chat/completions"
)
`,
		"internal/llm/other.go": "package llm\n\nconst u = \"https://example.invalid/x\"\n",
	})
	files, facts, err := ScanAndAnalyze(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanAndAnalyze: %v", err)
	}
	arch := Architecture{Nodes: []Area{
		{ID: "ext.copilot", Label: "Copilot", Kind: KindExternal, Hosts: []string{"api.github.com", "api.githubcopilot.com"}},
		{ID: "ext.openai", Label: "OpenAI", Kind: KindExternal, Hosts: []string{"api.openai.com"}},
	}}

	got := HostLinks(files, facts, arch)
	if len(got) != 1 {
		t.Fatalf("got %d links; want 1 (%+v)", len(got), got)
	}
	l := got[0]
	if l.A != "f:internal/llm/copilot.go" || l.B != "n:ext.copilot" {
		t.Errorf("link = %s -> %s; want copilot.go -> ext.copilot", l.A, l.B)
	}
	if l.K != LinkHTTP {
		t.Errorf("kind = %q; want %q", l.K, LinkHTTP)
	}
	if l.W != 2 || len(l.Labels) != 2 {
		t.Errorf("weight = %v labels = %v; want 2 and both hosts named", l.W, l.Labels)
	}
	// A host found in a string is good evidence, not proof that the call is
	// made, so the link says it was inferred.
	if !l.Inferred {
		t.Error("the link does not record that it was inferred")
	}
}

// A hostname that merely appears inside a longer one is a different host.
func TestHostLinksMatchWholeHostnames(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":  "module example.com/m\n",
		"main.go": "package main\n\nconst u = \"https://notapi.openai.com.evil.test/x\"\n",
	})
	files, facts, _ := ScanAndAnalyze(context.Background(), root)
	arch := Architecture{Nodes: []Area{
		{ID: "ext.openai", Label: "OpenAI", Kind: KindExternal, Hosts: []string{"api.openai.com"}},
	}}
	if got := HostLinks(files, facts, arch); len(got) != 0 {
		t.Fatalf("matched a lookalike host: %+v", got)
	}
}

// A hostname in a comment is a mention, not a call. One file in this
// repository names api.githubcopilot.com in a note about model ids and never
// contacts it; a link drawn from that would claim the file does something it
// does not.
func TestHostLinksIgnoreHostnamesInComments(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n",
		"mentions.go": `package m

// The https://api.githubcopilot.com/models endpoint lists them, but this file
// never calls it.
const unrelated = "hello"
`,
		"calls.go": "package m\n\nconst u = \"https://api.githubcopilot.com/chat\"\n",
	})
	files, facts, err := ScanAndAnalyze(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanAndAnalyze: %v", err)
	}
	arch := Architecture{Nodes: []Area{
		{ID: "ext.copilot", Label: "Copilot", Kind: KindExternal, Hosts: []string{"api.githubcopilot.com"}},
	}}

	got := HostLinks(files, facts, arch)
	for _, l := range got {
		if l.A == "f:mentions.go" {
			t.Errorf("drew a link from a hostname that only appears in a comment: %+v", l)
		}
	}
	if len(got) != 1 || got[0].A != "f:calls.go" {
		t.Fatalf("got %+v; want exactly the link from calls.go", got)
	}
}

// routeCallFixture has a server, a Go caller, a JavaScript caller whose file
// also contains a regular expression with a quote in it, and a file whose only
// path-like strings are import paths.
func routeCallFixture(t *testing.T) ([]ScannedFile, Facts, []Route) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"go.mod": "module example.com/m\n",
		"internal/dashboard/server.go": `package dashboard

import "net/http"

func routes(mux *http.ServeMux) {
	mux.HandleFunc("/", index)
	mux.HandleFunc("GET /api/v1/memory", memory)
	mux.HandleFunc("GET /api/v1/tasks/{id}", task)
	mux.HandleFunc("/events", events)
}
`,
		// The regular expression here desynchronises a naive string
		// tokeniser; the fetch below it must still be found.
		"internal/dashboard/assets/js/memory.js": `function esc(v) {
  return String(v).replace(/[&<>"']/g, function (c) { return c; });
}
fetch(api("/api/v1/memory")).then(render);
var es = new EventSource("/events");
`,
		"internal/dashboard/client.go": "package dashboard\n\nconst detail = \"/api/v1/tasks/abc123\"\n",
		// Its only slash-paths are import paths, which address no route.
		"cmd/memory.go": `package cmd

import (
	"encoding/json"

	"example.com/m/internal/dashboard"
)

var _ = json.Marshal
var _ = dashboard.Serve
`,
	})
	files, facts, err := ScanAndAnalyze(context.Background(), root)
	if err != nil {
		t.Fatalf("ScanAndAnalyze: %v", err)
	}
	return files, facts, GoRoutes(files, facts)
}

func routeLinksByPair(t *testing.T) map[string]Link {
	t.Helper()
	files, facts, routes := routeCallFixture(t)
	out := map[string]Link{}
	for _, l := range RouteLinks(files, facts, routes) {
		out[l.A+"|"+l.B] = l
	}
	return out
}

// Neither side imports the other, so this edge exists in no import graph: it
// is the only way the map can show which code calls which endpoint.
func TestRouteLinksConnectCallersToTheServingFile(t *testing.T) {
	got := routeLinksByPair(t)

	js, ok := got["f:internal/dashboard/assets/js/memory.js|f:internal/dashboard/server.go"]
	if !ok {
		t.Fatalf("memory.js is not linked to the server; got %v", keysOfLinks(got))
	}
	// Both the fetch and the EventSource are found, despite the regular
	// expression above them containing a quote.
	if js.W != 2 || len(js.Labels) != 2 {
		t.Errorf("weight = %v labels = %v; want both /api/v1/memory and /events", js.W, js.Labels)
	}
	if !js.Inferred {
		t.Error("a link read from a string must be marked inferred")
	}
	if js.K != LinkHTTP {
		t.Errorf("kind = %q; want %q", js.K, LinkHTTP)
	}

	// A wildcard pattern is matched by its literal prefix, because a caller
	// writes a real id where the pattern says {id}.
	goCaller, ok := got["f:internal/dashboard/client.go|f:internal/dashboard/server.go"]
	if !ok {
		t.Fatalf("client.go is not linked to the server; got %v", keysOfLinks(got))
	}
	if len(goCaller.Labels) != 1 || goCaller.Labels[0] != "GET /api/v1/tasks/{id}" {
		t.Errorf("labels = %v; want the task route named with its method", goCaller.Labels)
	}
}

// An import of ".../internal/dashboard" contains "/dashboard", and an import
// of "encoding/json" contains "/json". Searching inside import paths drew 45
// false links on this repository before they were excluded.
func TestRouteLinksIgnoreImportPaths(t *testing.T) {
	for pair := range routeLinksByPair(t) {
		if strings.HasPrefix(pair, "f:cmd/memory.go|") {
			t.Errorf("drew a link from an import path: %s", pair)
		}
	}
}

// The file registering a route is not a caller of it.
func TestRouteLinksSkipTheServingFile(t *testing.T) {
	for pair := range routeLinksByPair(t) {
		if strings.HasPrefix(pair, "f:internal/dashboard/server.go|") {
			t.Errorf("the serving file was linked to itself: %s", pair)
		}
	}
}

// "/" appears in almost every file that contains a path at all, so matching it
// would link most of the repository to whichever file serves the index.
func TestRouteLinksIgnoreTheRootPath(t *testing.T) {
	for pair, l := range routeLinksByPair(t) {
		for _, label := range l.Labels {
			if label == "ANY /" {
				t.Errorf("%s matched the root route", pair)
			}
		}
	}
}

func TestRouteLinksAreDeterministic(t *testing.T) {
	files, facts, routes := routeCallFixture(t)
	first, second := RouteLinks(files, facts, routes), RouteLinks(files, facts, routes)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two runs differ:\n%+v\n%+v", first, second)
	}
}

func TestLiteralRoutePrefix(t *testing.T) {
	cases := map[string]string{
		"/":                  "/",
		"/api/v1/tasks":      "/api/v1/tasks",
		"/api/v1/tasks/{id}": "/api/v1/tasks/", // the slash marks a subtree
		"/api/sessions/":     "/api/sessions/",
		"/css/":              "/css/",
	}
	for pattern, want := range cases {
		if got := literalRoutePrefix(pattern); got != want {
			t.Errorf("literalRoutePrefix(%q) = %q; want %q", pattern, got, want)
		}
	}
}

func keysOfLinks(m map[string]Link) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eval"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

type fakeProjectResolver struct{ res project.Resolution }

func (f fakeProjectResolver) Resolve(context.Context, string) (project.Resolution, error) {
	return f.res, nil
}

type fakeProfileCompiler struct{ eff profile.Effective }

func (f fakeProfileCompiler) CompileEffective(context.Context, string, string, string, string, string) (profile.Effective, error) {
	return f.eff, nil
}

func testProjectReader(projectID string) viewmodel.ProjectStores {
	return viewmodel.ProjectStores{
		Projects: fakeProjectResolver{res: project.Resolution{Project: project.Project{ID: projectID, CanonicalName: "widget"}}},
		Profiles: fakeProfileCompiler{eff: profile.Effective{Manifest: "# profile"}},
	}
}

func TestProjectViewEndpoint(t *testing.T) {
	server := &Server{token: "secret", services: Services{Project: testProjectReader("proj-1"), Workdir: "/tmp/widget"}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/project?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleProjectView(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.ProjectBrainVM
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if view.ProjectID != "proj-1" || view.Name != "widget" {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestProjectViewRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/project", nil)
	rec := httptest.NewRecorder()
	server.handleProjectView(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestProjectViewUnavailableWhenUnwired(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/project?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleProjectView(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when unwired, got %d", rec.Code)
	}
}

func TestMemoryViewEndpoint(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	events := eventstore.NewService(conn)
	memStore := memory.NewStore(conn)
	memSvc := memory.NewService(memStore, events)
	skillSvc := skill.NewService(skill.NewStore(conn), events)

	if err := memSvc.Learn(ctx, []memory.Candidate{{ProjectID: "proj-1", Type: memory.Factual, StableKey: "lang:go"}}); err != nil {
		t.Fatalf("Learn: %v", err)
	}

	server := &Server{token: "secret", services: Services{
		Project: testProjectReader("proj-1"),
		Memory:  viewmodel.MemoryStores{Memories: memStore, Skills: skillSvc},
		Workdir: "/tmp/widget",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleMemoryView(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.MemoryBrainVM
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if len(view.Candidates) != 1 || view.Candidates[0].StableKey != "lang:go" {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestMemoryViewRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory", nil)
	rec := httptest.NewRecorder()
	server.handleMemoryView(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestImpactViewEndpoint(t *testing.T) {
	conn := dbtest.New(t)
	store := impact.NewStore(conn)
	ctx := context.Background()
	if _, err := store.UpsertNode(ctx, impact.Node{ProjectID: "proj-1", Type: impact.NodeFile, StableKey: "foo.go"}); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	server := &Server{token: "secret", services: Services{
		Project: testProjectReader("proj-1"),
		Impact:  viewmodel.ImpactStores{Impact: store},
		Workdir: "/tmp/widget",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/impact?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleImpactView(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.ImpactGraphVM
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if !view.Built || len(view.Nodes) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestImpactViewRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/impact", nil)
	rec := httptest.NewRecorder()
	server.handleImpactView(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestOptimizationViewEndpoint(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	expStore := eval.NewExperimentStore(conn)
	events := eventstore.NewService(conn)
	policySvc := govpolicy.NewService(govpolicy.NewStore(conn), events)

	if _, _, err := eval.RunCompilerExperiment(ctx, expStore, "proj-1"); err != nil {
		t.Fatalf("RunCompilerExperiment: %v", err)
	}

	server := &Server{token: "secret", services: Services{
		Project:      testProjectReader("proj-1"),
		Optimization: viewmodel.OptimizationStores{Experiments: expStore, Policies: policySvc},
		Workdir:      "/tmp/widget",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/optimization?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleOptimizationView(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.OptimizationVM
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if len(view.Experiments) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestOptimizationViewEndpointSurfacesABComparison(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	expStore := eval.NewExperimentStore(conn)

	tasks := task.NewStore(conn)
	for _, id := range []string{"baseline-1", "variant-1"} {
		if err := tasks.CreateTask(ctx, task.Task{
			ID: id, SessionID: "s", Objective: "x", Mode: task.ModeImplementation,
			Status: task.StatusCompleted, CreatedAt: 1,
		}); err != nil {
			t.Fatalf("CreateTask(%s): %v", id, err)
		}
	}
	stores := eval.ABStores{
		Tasks:       tasks,
		Validations: validation.NewService(validation.NewStore(conn), nil),
		Ledger:      cost.NewService(conn),
		Checkpoints: checkpoint.NewStore(conn),
	}
	if _, err := stores.CompareAndRecord(ctx, expStore, "proj-1", "governed vs baseline", "baseline-1", "variant-1"); err != nil {
		t.Fatalf("CompareAndRecord: %v", err)
	}

	server := &Server{token: "secret", services: Services{
		Project:      testProjectReader("proj-1"),
		Optimization: viewmodel.OptimizationStores{Experiments: expStore},
		Workdir:      "/tmp/widget",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/optimization?token=secret", nil)
	rec := httptest.NewRecorder()
	server.handleOptimizationView(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view viewmodel.OptimizationVM
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode view: %v", err)
	}
	if len(view.Experiments) != 1 || view.Experiments[0].Comparison == nil {
		t.Fatalf("expected the A/B comparison in the served view, got %+v", view.Experiments)
	}
	if view.Experiments[0].Comparison.Variant.TaskID != "variant-1" {
		t.Fatalf("unexpected comparison in served view: %+v", view.Experiments[0].Comparison)
	}
}

func TestOptimizationViewRequiresToken(t *testing.T) {
	server := &Server{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/optimization", nil)
	rec := httptest.NewRecorder()
	server.handleOptimizationView(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}

func TestBrainPagesServedWithToken(t *testing.T) {
	server := &Server{token: "secret"}
	cases := []struct {
		path    string
		handler func(http.ResponseWriter, *http.Request)
		script  string
	}{
		{"/project?token=secret", server.handleProjectPage, "js/project.js"},
		{"/memory?token=secret", server.handleMemoryPage, "js/memory.js"},
		{"/impact?token=secret", server.handleImpactPage, "js/impact.js"},
		{"/optimization?token=secret", server.handleOptimizationPage, "js/optimization.js"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		rec := httptest.NewRecorder()
		c.handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", c.path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, c.script) {
			t.Fatalf("%s: page must load %s", c.path, c.script)
		}
		if !strings.Contains(body, "js/nav.js") {
			t.Fatalf("%s: page must load the shared nav", c.path)
		}
	}
}

// TestNavAssetDoesNotBuildMarkupFromQueryString guards a fixed XSS: nav.js used
// to concatenate location.search into an href inside an innerHTML string, so a
// crafted query string could break out of the attribute and inject script on
// every page that mounts the nav. The nav must keep building its DOM with
// createElement/textContent instead.
func TestNavAssetDoesNotBuildMarkupFromQueryString(t *testing.T) {
	data, err := assets.ReadFile("assets/js/nav.js")
	if err != nil {
		t.Fatalf("read nav.js: %v", err)
	}
	// Comments legitimately mention innerHTML to explain why it isn't used, so
	// only executable lines are checked.
	var code strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
			continue
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	src := code.String()
	if strings.Contains(src, "innerHTML") {
		t.Error("nav.js must not assign innerHTML; build nodes with createElement/textContent")
	}
	if !strings.Contains(src, "textContent") {
		t.Error("nav.js should set link/label text via textContent")
	}
}

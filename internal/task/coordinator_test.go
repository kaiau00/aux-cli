package task_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/multirepo"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

type passingRunner struct{}

func (passingRunner) Run(context.Context, string) (validation.CommandResult, error) {
	return validation.CommandResult{ExitCode: 0}, nil
}

func TestFinishLearnsProceduralMemoryFromValidatedCommands(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	store := task.NewStore(conn)
	events := eventstore.NewService(conn)
	memStore := memory.NewStore(conn)
	memSvc := memory.NewService(memStore, events)
	valSvc := validation.NewService(validation.NewStore(conn), events)

	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/tmp/p"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	eff := profile.Effective{VersionSetHash: "vset-1"}
	coord := task.NewCoordinator(fakeResolver{res}, fakeProfiles{eff}, store, events, "/tmp/p").
		WithMemory(memSvc).WithValidation(valSvc)

	newCtx, taskID, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// A command validates successfully during the task.
	if _, err := valSvc.RunIntent(newCtx, taskID,
		validation.Intent{ID: "i1", Command: "go test ./...", CriterionIDs: []string{"c1"}},
		"fp-1", passingRunner{}); err != nil {
		t.Fatalf("RunIntent: %v", err)
	}

	coord.Finish(newCtx, taskID, "completed")

	// The validated command becomes an active procedural memory (§8.2/§8.3).
	active, err := memSvc.Retrieve(ctx, "proj-1", []memory.Type{memory.Procedural}, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	found := false
	for _, m := range active {
		if m.StableKey == "validate:go test ./..." {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected active procedural memory for the validated command, got %+v", active)
	}
}

type fakeResolver struct{ res project.Resolution }

func (f fakeResolver) Resolve(context.Context, string) (project.Resolution, error) { return f.res, nil }

// multiResolver resolves different working directories to different project
// resolutions, keyed by directory, for multi-repo tests.
type multiResolver struct{ byDir map[string]project.Resolution }

func (m multiResolver) Resolve(_ context.Context, dir string) (project.Resolution, error) {
	res, ok := m.byDir[dir]
	if !ok {
		return project.Resolution{}, fmt.Errorf("no resolution configured for %q", dir)
	}
	return res, nil
}

type fakeProfiles struct{ eff profile.Effective }

func (f fakeProfiles) CompileEffective(context.Context, string, string, string, string, string) (profile.Effective, error) {
	return f.eff, nil
}

func newCoordinator(t *testing.T) (*task.Coordinator, *task.Store, eventstore.Service) {
	t.Helper()
	conn := dbtest.New(t)
	store := task.NewStore(conn)
	events := eventstore.NewService(conn)
	res := project.Resolution{
		Project:  project.Project{ID: "proj-1", CanonicalName: "widget"},
		Root:     project.Root{CanonicalPath: "/tmp/widget"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	eff := profile.Effective{VersionSetHash: "vset-1", Entries: []profile.EffectiveEntry{
		{Type: profile.EntryValidationCommand, Key: "go.test", ValueJSON: `{"command":"go test ./..."}`},
	}}
	coord := task.NewCoordinator(fakeResolver{res}, fakeProfiles{eff}, store, events, "/tmp/widget")
	return coord, store, events
}

func TestBeginCreatesTaskAndSpec(t *testing.T) {
	coord, store, events := newCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	newCtx, taskID, err := coord.Begin(ctx, "sess-1", "Fix the failing test")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if taskID == "" {
		t.Fatalf("expected a task id")
	}

	// Context carries task and project ids for correlation.
	corr := tools.CorrelationFromContext(newCtx)
	if corr.TaskID != taskID || corr.ProjectID != "proj-1" {
		t.Fatalf("context correlation wrong: %+v", corr)
	}

	tk, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if tk.Mode != task.ModeBugDiagnosis {
		t.Fatalf("mode = %q, want bug_diagnosis", tk.Mode)
	}
	if tk.Status != task.StatusRunning {
		t.Fatalf("status = %q, want running", tk.Status)
	}
	if tk.ProfileVersionSet != "vset-1" || tk.ProjectRevisionID != "rev-1" {
		t.Fatalf("profile/revision binding wrong: %+v", tk)
	}

	spec, ok, err := store.LatestSpec(ctx, taskID)
	if err != nil || !ok {
		t.Fatalf("expected spec, ok=%v err=%v", ok, err)
	}
	if spec.ProfileVersionID != "vset-1" || len(spec.ValidationIntents) != 1 {
		t.Fatalf("spec not compiled from profile: %+v", spec)
	}

	// Lifecycle events emitted.
	evs, _ := events.List(ctx, eventstore.Filter{TaskID: taskID})
	types := map[eventstore.Type]bool{}
	for _, e := range evs {
		types[e.Type] = true
	}
	for _, want := range []eventstore.Type{eventstore.TaskCreated, eventstore.TaskCompiled, eventstore.TaskStarted} {
		if !types[want] {
			t.Fatalf("missing event %q; got %v", want, types)
		}
	}
}

func TestFinishAndFail(t *testing.T) {
	coord, store, events := newCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	_, taskID, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	coord.Finish(ctx, taskID, "done")
	tk, _ := store.GetTask(ctx, taskID)
	if tk.Status != task.StatusCompleted {
		t.Fatalf("status = %q, want completed", tk.Status)
	}

	// A second task that gets cancelled.
	_, taskID2, _ := coord.Begin(ctx, "sess-1", "another feature")
	coord.Fail(ctx, taskID2, context.Canceled)
	tk2, _ := store.GetTask(ctx, taskID2)
	if tk2.Status != task.StatusCancelled {
		t.Fatalf("status = %q, want cancelled", tk2.Status)
	}

	// A failed (non-cancel) task.
	_, taskID3, _ := coord.Begin(ctx, "sess-1", "risky feature")
	coord.Fail(ctx, taskID3, errors.New("boom"))
	tk3, _ := store.GetTask(ctx, taskID3)
	if tk3.Status != task.StatusFailed {
		t.Fatalf("status = %q, want failed", tk3.Status)
	}

	completed, _ := events.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.TaskCompleted}})
	if len(completed) != 1 {
		t.Fatalf("expected 1 task.completed event, got %d", len(completed))
	}
}

type fakeRelatedProjects struct {
	rels map[string][]relatedproject.Relation
}

func (f fakeRelatedProjects) From(_ context.Context, projectID string) ([]relatedproject.Relation, error) {
	return f.rels[projectID], nil
}

func TestBeginIncludesRelatedProjectsInManifest(t *testing.T) {
	coord, _, _ := newCoordinator(t)
	related := fakeRelatedProjects{rels: map[string][]relatedproject.Relation{
		"proj-1": {{FromProject: "proj-1", ToProject: "proj-lib", RelationType: relatedproject.LibraryConsumer, Source: relatedproject.SourceDeps}},
	}}
	coord = coord.WithRelatedProjects(related)

	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	newCtx, _, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	manifest, _ := promptcompiler.ProjectContextFromContext(newCtx)
	if !strings.Contains(manifest, "proj-lib") {
		t.Fatalf("expected manifest to include related project, got: %s", manifest)
	}
}

func TestBeginOmitsRelatedProjectsSectionWhenNoneWired(t *testing.T) {
	coord, _, _ := newCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	newCtx, _, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	manifest, _ := promptcompiler.ProjectContextFromContext(newCtx)
	if strings.Contains(manifest, "Related projects:") {
		t.Fatalf("expected no related-projects section when unwired, got: %s", manifest)
	}
}

func TestBeginMultiRepoCreatesParentAndLinkedChildren(t *testing.T) {
	conn := dbtest.New(t)
	store := task.NewStore(conn)
	events := eventstore.NewService(conn)

	appRes := project.Resolution{
		Project:  project.Project{ID: "proj-app", CanonicalName: "app"},
		Root:     project.Root{CanonicalPath: "/tmp/app"},
		Revision: project.Revision{ID: "rev-app", VCSRevision: "app-abc"},
	}
	apiRes := project.Resolution{
		Project:  project.Project{ID: "proj-api", CanonicalName: "api"},
		Root:     project.Root{CanonicalPath: "/tmp/api"},
		Revision: project.Revision{ID: "rev-api", VCSRevision: "api-abc"},
	}
	eff := profile.Effective{VersionSetHash: "vset-1"}
	resolver := multiResolver{byDir: map[string]project.Resolution{
		"/tmp/app": appRes,
		"/tmp/api": apiRes,
	}}
	coord := task.NewCoordinator(resolver, fakeProfiles{eff}, store, events, "/tmp/app")

	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	plan, children, err := coord.BeginMultiRepo(ctx, "sess-1", "add cross-service feature", []multirepo.RepoTarget{
		{ProjectID: "proj-app", Name: "app", Root: "/tmp/app"},
		{ProjectID: "proj-api", Name: "api", Root: "/tmp/api"},
	})
	if err != nil {
		t.Fatalf("BeginMultiRepo: %v", err)
	}
	if len(plan.Children) != 2 {
		t.Fatalf("expected 2 planned children, got %d", len(plan.Children))
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 created child tasks, got %d: %+v", len(children), children)
	}

	// Every child must have a non-empty parent linking it to a real, distinct task.
	byProject := map[string]task.Task{}
	for _, c := range children {
		if c.ParentTaskID == "" {
			t.Fatalf("child task %s has no parent_task_id", c.ID)
		}
		byProject[c.ProjectID] = c
	}
	if byProject["proj-app"].ID == byProject["proj-api"].ID {
		t.Fatalf("child tasks must be distinct per project")
	}
	if byProject["proj-app"].ParentTaskID != byProject["proj-api"].ParentTaskID {
		t.Fatalf("children must share the same parent task id")
	}

	// The parent itself is a real, persisted task distinct from its children.
	parent, err := store.GetTask(ctx, byProject["proj-app"].ParentTaskID)
	if err != nil {
		t.Fatalf("GetTask(parent): %v", err)
	}
	if parent.ParentTaskID != "" {
		t.Fatalf("the parent task must not itself have a parent")
	}

	// ListByParent finds both children.
	byParent, err := store.ListByParent(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListByParent: %v", err)
	}
	if len(byParent) != 2 {
		t.Fatalf("expected 2 children via ListByParent, got %d", len(byParent))
	}
}

func TestBeginMultiRepoReportsUnresolvableTargetWithoutLosingOthers(t *testing.T) {
	conn := dbtest.New(t)
	store := task.NewStore(conn)
	events := eventstore.NewService(conn)

	appRes := project.Resolution{
		Project:  project.Project{ID: "proj-app", CanonicalName: "app"},
		Root:     project.Root{CanonicalPath: "/tmp/app"},
		Revision: project.Revision{ID: "rev-app", VCSRevision: "app-abc"},
	}
	eff := profile.Effective{VersionSetHash: "vset-1"}
	resolver := multiResolver{byDir: map[string]project.Resolution{"/tmp/app": appRes}}
	coord := task.NewCoordinator(resolver, fakeProfiles{eff}, store, events, "/tmp/app")

	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	_, children, err := coord.BeginMultiRepo(ctx, "sess-1", "add feature", []multirepo.RepoTarget{
		{ProjectID: "proj-app", Name: "app", Root: "/tmp/app"},
		{ProjectID: "proj-missing", Name: "missing", Root: "/tmp/does-not-exist"},
	})
	if err == nil {
		t.Fatalf("expected an error reporting the unresolvable target")
	}
	if len(children) != 1 {
		t.Fatalf("expected the one resolvable child to still be created, got %d", len(children))
	}
}

func TestBeginLinksToParentTaskFromContext(t *testing.T) {
	coord, store, _ := newCoordinator(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	// The parent task begins normally, with no parent of its own.
	_, parentTaskID, err := coord.Begin(ctx, "sess-1", "top-level objective")
	if err != nil {
		t.Fatalf("Begin(parent): %v", err)
	}
	parent, _ := store.GetTask(ctx, parentTaskID)
	if parent.ParentTaskID != "" {
		t.Fatalf("top-level task must not have a parent, got %q", parent.ParentTaskID)
	}

	// A subagent's own Begin call carries tools.ParentTaskIDContextKey, set by
	// the agent tool before spawning (roadmapplan.md §11.3).
	subCtx := context.WithValue(ctx, tools.ParentTaskIDContextKey, parentTaskID)
	_, subTaskID, err := coord.Begin(subCtx, "sub-sess-1", "subagent objective")
	if err != nil {
		t.Fatalf("Begin(subagent): %v", err)
	}
	if subTaskID == parentTaskID {
		t.Fatal("subagent task must be a distinct task from its parent")
	}
	sub, _ := store.GetTask(ctx, subTaskID)
	if sub.ParentTaskID != parentTaskID {
		t.Fatalf("subagent task ParentTaskID = %q, want %q", sub.ParentTaskID, parentTaskID)
	}
}

func TestStoreStatusAndListBySession(t *testing.T) {
	store := task.NewStore(dbtest.New(t))
	ctx := context.Background()
	tk := task.Task{ID: "t1", SessionID: "s1", Objective: "obj", Mode: task.ModeImplementation, Status: task.StatusCreated, CreatedAt: 1}
	if err := store.CreateTask(ctx, tk); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.SetStatus(ctx, "t1", task.StatusCompleted, "ok", 5, 10); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, _ := store.GetTask(ctx, "t1")
	if got.Status != task.StatusCompleted || got.Outcome != "ok" || got.FinishedAt != 10 {
		t.Fatalf("status not updated: %+v", got)
	}
	list, _ := store.ListBySession(ctx, "s1")
	if len(list) != 1 {
		t.Fatalf("expected 1 task for session, got %d", len(list))
	}
}

package task_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// The candidate -> evaluate -> promote pipeline was built and correct, but no
// code path ever called Candidate, so no task had ever produced a skill. This
// test exists to keep that call site wired.
func TestFinishProposesSkillCandidateFromValidatedCommands(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	events := eventstore.NewService(conn)
	skillSvc := skill.NewService(skill.NewStore(conn), events)
	valSvc := validation.NewService(validation.NewStore(conn), events)

	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/tmp/p"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	coord := task.NewCoordinator(
		fakeResolver{res}, fakeProfiles{profile.Effective{VersionSetHash: "vset-1"}},
		task.NewStore(conn), events, "/tmp/p",
	).WithSkills(skillSvc).WithValidation(valSvc)

	newCtx, taskID, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := valSvc.RunIntent(newCtx, taskID,
		validation.Intent{ID: "i1", Command: "go test ./...", CriterionIDs: []string{"c1"}},
		"fp-1", passingRunner{}); err != nil {
		t.Fatalf("RunIntent: %v", err)
	}

	coord.Finish(newCtx, taskID, "completed")

	candidates, err := skillSvc.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("a validated task should propose one skill candidate, got %d", len(candidates))
	}

	// Critically: it must be a candidate, not active. An automatically created
	// skill that fires on later tasks without evidence is worse than none.
	if candidates[0].State != skill.StateCandidate {
		t.Fatalf("auto-extracted skills must stay candidates, got %q", candidates[0].State)
	}
	active, err := skillSvc.Active(context.Background())
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("nothing should be active without a passing evaluation, got %d", len(active))
	}
}

// A task that validated nothing has no evidence, so it must propose nothing.
func TestFinishProposesNoSkillWithoutValidatedCommands(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	events := eventstore.NewService(conn)
	skillSvc := skill.NewService(skill.NewStore(conn), events)

	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/tmp/p"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	coord := task.NewCoordinator(
		fakeResolver{res}, fakeProfiles{profile.Effective{VersionSetHash: "vset-1"}},
		task.NewStore(conn), events, "/tmp/p",
	).WithSkills(skillSvc)

	newCtx, taskID, err := coord.Begin(ctx, "sess-1", "poke around")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	coord.Finish(newCtx, taskID, "completed")

	candidates, err := skillSvc.Candidates(context.Background())
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("no evidence should mean no candidate, got %d", len(candidates))
	}
}

// Finishing a task must not depend on a skill service being wired.
func TestFinishWithoutSkillServiceStillCompletes(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")

	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/tmp/p"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	store := task.NewStore(conn)
	coord := task.NewCoordinator(
		fakeResolver{res}, fakeProfiles{profile.Effective{VersionSetHash: "vset-1"}},
		store, eventstore.NewService(conn), "/tmp/p",
	)

	newCtx, taskID, err := coord.Begin(ctx, "sess-1", "do a thing")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	coord.Finish(newCtx, taskID, "completed")

	got, err := store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != task.StatusCompleted {
		t.Fatalf("task should still complete without a skill service, got %q", got.Status)
	}
}

package skill_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/skill"
)

func newService(t *testing.T) (*skill.Service, eventstore.Service) {
	t.Helper()
	conn := dbtest.New(t)
	events := eventstore.NewService(conn)
	return skill.NewService(skill.NewStore(conn), events), events
}

func sampleContent() skill.Content {
	return skill.Content{
		Name:                   "add-endpoint",
		Purpose:                "add a REST endpoint following project conventions",
		Triggers:               []string{"add endpoint", "new route"},
		Procedure:              []skill.Step{{Title: "add handler"}, {Title: "register route"}, {Title: "add test"}},
		ValidationRequirements: []string{"go test ./..."},
	}
}

func TestCandidateIsNotActive(t *testing.T) {
	svc, events := newService(t)
	ctx := context.Background()
	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", []string{"task-1"})
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}
	if sk.State != skill.StateCandidate {
		t.Fatalf("new skill should be a candidate, got %q", sk.State)
	}
	if ver.ID == "" {
		t.Fatalf("expected a version")
	}
	created, _ := events.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.SkillCandidateCreated}})
	if len(created) != 1 {
		t.Fatalf("expected skill.candidate_created event, got %d", len(created))
	}
}

func TestPromoteRequiresPassingEvaluation(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()
	sk, ver, _ := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)

	// No evaluation yet: promotion must be refused.
	if err := svc.Promote(ctx, sk.ID, ver.ID); !errors.Is(err, skill.ErrNoEvaluationEvidence) {
		t.Fatalf("promotion without evaluation must be refused, got %v", err)
	}

	// A failing evaluation still refuses promotion.
	if err := svc.Evaluate(ctx, ver.ID, "", "run-1", skill.EvalFail, "{}"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := svc.Promote(ctx, sk.ID, ver.ID); !errors.Is(err, skill.ErrNoEvaluationEvidence) {
		t.Fatalf("failing evaluation must not allow promotion, got %v", err)
	}

	// A passing evaluation unlocks promotion.
	if err := svc.Evaluate(ctx, ver.ID, "", "run-2", skill.EvalPass, `{"successDelta":0.1}`); err != nil {
		t.Fatalf("Evaluate pass: %v", err)
	}
	if err := svc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("Promote after passing eval: %v", err)
	}
	active, _ := svc.Active(ctx)
	if len(active) != 1 || active[0].ID != sk.ID {
		t.Fatalf("skill should be active after evaluated promotion")
	}
}

func TestRollbackDemotes(t *testing.T) {
	svc, events := newService(t)
	ctx := context.Background()
	sk, ver, _ := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)
	_ = svc.Evaluate(ctx, ver.ID, "", "run", skill.EvalPass, "{}")
	_ = svc.Promote(ctx, sk.ID, ver.ID)

	if err := svc.Rollback(ctx, sk.ID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	active, _ := svc.Active(ctx)
	if len(active) != 0 {
		t.Fatalf("rolled-back skill should not be active")
	}
	rolled, _ := events.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.SkillRolledBack}})
	if len(rolled) != 1 {
		t.Fatalf("expected skill.rolled_back event")
	}
}

func TestAgentSkillsRoundTrip(t *testing.T) {
	c := sampleContent()
	m := skill.ToAgentSkill(c)
	if m.Name != c.Name || m.Description != c.Purpose {
		t.Fatalf("export lost identity: %+v", m)
	}
	if len(m.Steps) != len(c.Procedure) {
		t.Fatalf("export lost steps")
	}
	back := skill.FromAgentSkill(m)
	if back.Name != c.Name || len(back.Procedure) != len(c.Procedure) {
		t.Fatalf("round trip changed content: %+v", back)
	}
}

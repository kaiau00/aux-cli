package govpolicy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/govpolicy"
)

func newService(t *testing.T) *govpolicy.Service {
	t.Helper()
	conn := dbtest.New(t)
	return govpolicy.NewService(govpolicy.NewStore(conn), nil)
}

func TestPromoteRequiresPassingEvaluation(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	p, err := svc.Candidate(ctx, "project", "proj-1", "implementation", `{"mode":"observe"}`)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}

	// No evidence yet -> promotion refused.
	if err := svc.Promote(ctx, p.ID); !errors.Is(err, govpolicy.ErrNoEvaluationEvidence) {
		t.Fatalf("expected ErrNoEvaluationEvidence, got %v", err)
	}
	// Still a candidate, never active without evidence.
	cands, _ := svc.Candidates(ctx)
	if len(cands) != 1 {
		t.Fatalf("policy should remain a candidate, got %d candidates", len(cands))
	}
	active, _ := svc.Active(ctx)
	if len(active) != 0 {
		t.Fatalf("no policy should be active without evidence, got %d", len(active))
	}
}

func TestPromoteAfterPassingEvaluation(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	p, _ := svc.Candidate(ctx, "project", "proj-1", "implementation", `{"mode":"on"}`)

	// A failing evaluation must NOT unlock promotion.
	if err := svc.Evaluate(ctx, p.ID, "", "run-1", govpolicy.Fail, `{"cpd":0.9}`); err != nil {
		t.Fatalf("Evaluate fail: %v", err)
	}
	if err := svc.Promote(ctx, p.ID); !errors.Is(err, govpolicy.ErrNoEvaluationEvidence) {
		t.Fatalf("failing evaluation must not unlock promotion, got %v", err)
	}

	// A passing evaluation unlocks it.
	if err := svc.Evaluate(ctx, p.ID, "", "run-2", govpolicy.Pass, `{"cpd":1.4}`); err != nil {
		t.Fatalf("Evaluate pass: %v", err)
	}
	if err := svc.Promote(ctx, p.ID); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	active, _ := svc.Active(ctx)
	if len(active) != 1 || active[0].ID != p.ID {
		t.Fatalf("policy should be active after passing evaluation, got %d", len(active))
	}
}

func TestRollbackPreservesEvidence(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	p, _ := svc.Candidate(ctx, "project", "proj-1", "implementation", `{}`)
	_ = svc.Evaluate(ctx, p.ID, "", "r", govpolicy.Pass, "{}")
	_ = svc.Promote(ctx, p.ID)

	if err := svc.Rollback(ctx, p.ID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if active, _ := svc.Active(ctx); len(active) != 0 {
		t.Fatalf("rolled-back policy should not be active")
	}
	// Evidence persists: re-promotion succeeds without a new evaluation.
	if err := svc.Promote(ctx, p.ID); err != nil {
		t.Fatalf("re-promotion should still find prior evidence: %v", err)
	}
}

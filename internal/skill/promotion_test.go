package skill_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/skill"
)

// The promotion gate compares the stored result against "pass" exactly, so a
// misspelled or differently-cased result would be recorded without complaint and
// then never unlock promotion -- the evaluation appears on record while promote
// keeps reporting missing evidence. Rejecting it at the boundary is what turns
// that dead end into an error.
func TestParseEvalResultAcceptsOnlyKnownResults(t *testing.T) {
	for _, in := range []string{"pass", "fail", "inconclusive"} {
		if got, err := skill.ParseEvalResult(in); err != nil || string(got) != in {
			t.Errorf("ParseEvalResult(%q) = %q, %v; want %q, nil", in, got, err, in)
		}
	}

	for _, in := range []string{"Pass", "PASS", "passs", "passed", "ok", "true", "", " pass"} {
		if _, err := skill.ParseEvalResult(in); err == nil {
			t.Errorf("ParseEvalResult(%q) must be refused: it would be stored and never unlock promotion", in)
		}
	}
}

// LatestVersion is how a caller acts on "the current candidate" without tracking
// version ids by hand, which is what the CLI needs to be usable at all.
func TestLatestVersionTracksTheNewestVersion(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	if _, ok, err := svc.LatestVersion(ctx, "no-such-skill"); err != nil || ok {
		t.Fatalf("an unknown skill must report no version, got ok=%v err=%v", ok, err)
	}

	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}

	got, ok, err := svc.LatestVersion(ctx, sk.ID)
	if err != nil || !ok {
		t.Fatalf("expected a version, got ok=%v err=%v", ok, err)
	}
	if got.ID != ver.ID {
		t.Fatalf("got version %q, want %q", got.ID, ver.ID)
	}
}

// Callers need to show what is promotable rather than discover it by being
// refused, so this must track the evidence as it is recorded.
func TestHasPassingEvaluationReflectsRecordedEvidence(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}

	passing, err := svc.HasPassingEvaluation(ctx, ver.ID)
	if err != nil {
		t.Fatalf("HasPassingEvaluation: %v", err)
	}
	if passing {
		t.Fatal("a fresh candidate must not report passing evidence")
	}

	// A failing evaluation is still evidence, but not the kind that unlocks.
	if err := svc.Evaluate(ctx, ver.ID, "", "run-1", skill.EvalFail, "{}"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if passing, _ := svc.HasPassingEvaluation(ctx, ver.ID); passing {
		t.Fatal("a failing evaluation must not count as passing evidence")
	}

	if err := svc.Evaluate(ctx, ver.ID, "", "run-2", skill.EvalPass, "{}"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if passing, _ := svc.HasPassingEvaluation(ctx, ver.ID); !passing {
		t.Fatal("a passing evaluation must be visible as passing evidence")
	}

	// And the gate it reports on must agree.
	if err := svc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("promotion should succeed once evidence exists: %v", err)
	}
}

// The whole point of items 4-7 of this work: the lifecycle must be completable.
// Before the CLI could record an evaluation, candidates accumulated and nothing
// could ever supply the evidence promote required.
func TestTheLifecycleCanActuallyComplete(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}

	result, err := skill.ParseEvalResult("pass")
	if err != nil {
		t.Fatalf("ParseEvalResult: %v", err)
	}
	if err := svc.Evaluate(ctx, ver.ID, "", "suite-run-1", result, `{"passRate":1.0}`); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := svc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	active, err := svc.Active(ctx)
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if len(active) != 1 || active[0].ID != sk.ID {
		t.Fatalf("the promoted skill must be active, got %+v", active)
	}

	candidates, _ := svc.Candidates(ctx)
	for _, c := range candidates {
		if c.ID == sk.ID {
			t.Fatal("a promoted skill must no longer be listed as a candidate")
		}
	}
}

// Rollback preserves version history so a version can be re-promoted with fresh
// evidence. That is only true if a rolled-back skill can still be found: before
// RolledBack existed it appeared in neither the active nor the candidate list, so
// it was invisible and its own recovery path was unreachable.
func TestRolledBackSkillsRemainVisibleAndRePromotable(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	sk, ver, err := svc.Candidate(ctx, "project", "proj-1", sampleContent(), "demonstration", nil)
	if err != nil {
		t.Fatalf("Candidate: %v", err)
	}
	if err := svc.Evaluate(ctx, ver.ID, "", "run-1", skill.EvalPass, "{}"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if err := svc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if err := svc.Rollback(ctx, sk.ID); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	active, _ := svc.Active(ctx)
	candidates, _ := svc.Candidates(ctx)
	rolledBack, err := svc.RolledBack(ctx)
	if err != nil {
		t.Fatalf("RolledBack: %v", err)
	}

	if len(active) != 0 || len(candidates) != 0 {
		t.Fatalf("a rolled-back skill is neither active nor a candidate, got active=%d candidates=%d", len(active), len(candidates))
	}
	if len(rolledBack) != 1 || rolledBack[0].ID != sk.ID {
		t.Fatalf("a rolled-back skill must stay findable, got %+v", rolledBack)
	}

	// Its evidence survives the rollback, so it can be put back.
	if err := svc.Promote(ctx, sk.ID, ver.ID); err != nil {
		t.Fatalf("a rolled-back skill must be re-promotable on its existing evidence: %v", err)
	}
}

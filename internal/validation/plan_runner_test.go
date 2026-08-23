package validation

import (
	"context"
	"errors"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/permission"
)

type stubApprover struct {
	grant bool
	seen  []permission.CreatePermissionRequest
}

func (s *stubApprover) Request(opts permission.CreatePermissionRequest) bool {
	s.seen = append(s.seen, opts)
	return s.grant
}

func TestPlanIntentsCoversEveryCriterion(t *testing.T) {
	intents := PlanIntents(
		[]CommandSpec{
			{Key: "go.test", Command: "go test ./...", ValidatorType: "test"},
			{Key: "go.build", Command: "go build ./...", ValidatorType: "build"},
		},
		[]string{"c1", "c2"},
	)
	if len(intents) != 2 {
		t.Fatalf("expected one intent per command, got %d", len(intents))
	}
	for _, in := range intents {
		if len(in.CriterionIDs) != 2 {
			t.Fatalf("each intent must provide evidence for every criterion, got %+v", in.CriterionIDs)
		}
		if in.ID == "" || in.ValidatorType == "" {
			t.Fatalf("intent missing identity/type: %+v", in)
		}
	}
}

func TestPlanIntentsEmptyWithoutCriteriaOrCommands(t *testing.T) {
	if got := PlanIntents([]CommandSpec{{Command: "go test ./..."}}, nil); got != nil {
		t.Fatalf("no criteria means nothing to be evidence of, got %+v", got)
	}
	if got := PlanIntents(nil, []string{"c1"}); got != nil {
		t.Fatalf("no commands means no intents, got %+v", got)
	}
}

func TestPlanIntentsSkipsBlankCommands(t *testing.T) {
	got := PlanIntents([]CommandSpec{{Key: "empty"}, {Key: "real", Command: "true"}}, []string{"c1"})
	if len(got) != 1 || got[0].Command != "true" {
		t.Fatalf("blank commands must be skipped, got %+v", got)
	}
}

func TestShellRunnerRequiresApproval(t *testing.T) {
	// A repo-derived command must never run without the user agreeing to it.
	denier := &stubApprover{grant: false}
	r := ShellRunner{WorkDir: t.TempDir(), SessionID: "s1", Approver: denier}

	_, err := r.Run(context.Background(), "touch should-not-exist.txt")
	if !errors.Is(err, permission.ErrorPermissionDenied) {
		t.Fatalf("expected permission denial, got %v", err)
	}
	if len(denier.seen) != 1 || denier.seen[0].Fingerprint != "touch should-not-exist.txt" {
		t.Fatalf("expected the exact command to be the approval fingerprint, got %+v", denier.seen)
	}
}

func TestShellRunnerFailsClosedWithoutApprover(t *testing.T) {
	r := ShellRunner{WorkDir: t.TempDir(), SessionID: "s1"}
	if _, err := r.Run(context.Background(), "true"); !errors.Is(err, permission.ErrorPermissionDenied) {
		t.Fatalf("no approver must deny rather than run, got %v", err)
	}
}

func TestShellRunnerReportsExitCodes(t *testing.T) {
	r := ShellRunner{WorkDir: t.TempDir(), SessionID: "s1", Approver: &stubApprover{grant: true}}

	res, err := r.Run(context.Background(), "true")
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("passing command: exit=%d err=%v", res.ExitCode, err)
	}

	// A failing command is evidence, not an error: the caller records it as a
	// failed run rather than treating validation as unavailable.
	res, err = r.Run(context.Background(), "exit 3")
	if err != nil {
		t.Fatalf("a non-zero exit must not surface as a runner error: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("exit code = %d, want 3", res.ExitCode)
	}
}

func TestPlannedCommandsProduceRealProofOfDone(t *testing.T) {
	// End to end over the seam that used to be missing entirely: profile
	// commands -> intents -> a really-executed run -> recorded evidence ->
	// proof-of-done. Before this wiring, RunIntent had no production caller,
	// so the evidence table was always empty and every criterion stayed
	// unverified no matter what the agent did.
	conn := dbtest.New(t)
	ctx := context.Background()
	svc := NewService(NewStore(conn), nil)
	runner := ShellRunner{WorkDir: t.TempDir(), SessionID: "s1", Approver: &stubApprover{grant: true}}

	intents := PlanIntents([]CommandSpec{{Key: "go.test", Command: "true", ValidatorType: "test"}}, []string{"c1"})
	if len(intents) != 1 {
		t.Fatalf("expected one intent, got %d", len(intents))
	}
	if _, err := svc.RunIntent(ctx, "task-1", intents[0], "rev-1", runner); err != nil {
		t.Fatalf("RunIntent: %v", err)
	}

	states, err := svc.ProofOfDone(ctx, "task-1", []string{"c1"})
	if err != nil {
		t.Fatalf("ProofOfDone: %v", err)
	}
	if states["c1"] != Validated {
		t.Fatalf("a passing planned command should validate the criterion, got %q", states["c1"])
	}
}

func TestFailingCommandNeverValidates(t *testing.T) {
	// The safety invariant: real execution must not be able to produce a
	// validated criterion when the command actually failed.
	conn := dbtest.New(t)
	ctx := context.Background()
	svc := NewService(NewStore(conn), nil)
	runner := ShellRunner{WorkDir: t.TempDir(), SessionID: "s1", Approver: &stubApprover{grant: true}}

	intents := PlanIntents([]CommandSpec{{Key: "go.test", Command: "exit 1", ValidatorType: "test"}}, []string{"c1"})
	if _, err := svc.RunIntent(ctx, "task-1", intents[0], "rev-1", runner); err != nil {
		t.Fatalf("RunIntent: %v", err)
	}

	states, err := svc.ProofOfDone(ctx, "task-1", []string{"c1"})
	if err != nil {
		t.Fatalf("ProofOfDone: %v", err)
	}
	if states["c1"] == Validated {
		t.Fatal("a failing command must never yield a validated criterion")
	}
}

package agent

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/permission"
)

// approverStub answers budget-continuation prompts with a fixed verdict.
type approverStub struct {
	permission.Service
	approve bool
	asked   int
}

func (a *approverStub) Request(permission.CreatePermissionRequest) bool {
	a.asked++
	return a.approve
}

// exhaustedAgent builds an agent whose ledger reports spend well past any
// default budget, so the governor's exhausted branch is the one under test.
func exhaustedAgent(t *testing.T, mode cost.GovernorMode, perms permission.Service) (*agent, string) {
	t.Helper()
	conn := dbtest.New(t)
	ledger := cost.NewService(conn)
	ctx := context.Background()

	const sessionID = "s-budget"
	call, err := ledger.StartCall(ctx, cost.ModelCall{
		ID: "call-1", SessionID: sessionID, Provider: "test", Model: "test-model",
		Status: cost.CallStarted, StartedAt: 1,
	})
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	call.Status = cost.CallCompleted
	call.FinishedAt = 2
	call.InputTokens = 100_000_000
	call.OutputTokens = 100_000_000
	call.EstimatedCost = 10_000
	if err := ledger.FinishCall(ctx, call); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}

	return &agent{
		ledger:      ledger,
		governor:    cost.NewGovernor(mode),
		permissions: perms,
	}, sessionID
}

func TestBudgetStopObserveModeNeverStops(t *testing.T) {
	// The bug this guards: "on" and "observe" used to behave identically,
	// which made the governor advisory in every mode.
	perms := &approverStub{approve: false}
	a, sessionID := exhaustedAgent(t, cost.GovObserve, perms)

	stop, reason := a.budgetStop(context.Background(), sessionID)
	if stop {
		t.Fatalf("observe mode must never stop a task, got stop=true reason=%q", reason)
	}
	if perms.asked != 0 {
		t.Fatalf("observe mode must not prompt, prompted %d times", perms.asked)
	}
}

func TestBudgetStopOnModeAsksAndHonorsApproval(t *testing.T) {
	perms := &approverStub{approve: true}
	a, sessionID := exhaustedAgent(t, cost.GovOn, perms)

	stop, _ := a.budgetStop(context.Background(), sessionID)
	if stop {
		t.Fatal("an approved overrun should continue the task")
	}
	if perms.asked != 1 {
		t.Fatalf("expected exactly one approval prompt, got %d", perms.asked)
	}
}

func TestBudgetStopOnModeStopsWhenDeclined(t *testing.T) {
	perms := &approverStub{approve: false}
	a, sessionID := exhaustedAgent(t, cost.GovOn, perms)

	stop, reason := a.budgetStop(context.Background(), sessionID)
	if !stop {
		t.Fatal("declining an exhausted budget must stop the task")
	}
	if reason == "" {
		t.Fatal("a stop must explain itself to the user")
	}
}

func TestBudgetStopFailsClosedWithoutApprover(t *testing.T) {
	// "on" mode exists so an unattended run cannot spend without bound; with
	// nobody to ask, stopping is the safe answer.
	a, sessionID := exhaustedAgent(t, cost.GovOn, nil)

	if stop, _ := a.budgetStop(context.Background(), sessionID); !stop {
		t.Fatal("expected a stop when there is no way to ask for approval")
	}
}

func TestBudgetStopOffModeIsInert(t *testing.T) {
	a, sessionID := exhaustedAgent(t, cost.GovOff, &approverStub{approve: false})
	if stop, _ := a.budgetStop(context.Background(), sessionID); stop {
		t.Fatal("a disabled governor must never stop a task")
	}
}

func TestBudgetStopWithinBudgetDoesNotStop(t *testing.T) {
	conn := dbtest.New(t)
	a := &agent{
		ledger:      cost.NewService(conn),
		governor:    cost.NewGovernor(cost.GovOn),
		permissions: &approverStub{approve: false},
	}
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "s-fresh")
	if stop, _ := a.budgetStop(ctx, "s-fresh"); stop {
		t.Fatal("a task that has spent nothing must not be stopped")
	}
}

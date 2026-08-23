package cost_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/task"
)

func TestStartAndFinishCallRoundTrip(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	callID := ids.New()
	if _, err := svc.StartCall(ctx, cost.ModelCall{
		ID:        callID,
		SessionID: "sess-1",
		TurnID:    "turn-1",
		Provider:  "anthropic",
		Model:     "claude",
		Status:    cost.CallStarted,
		StartedAt: 1000,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	got, err := svc.GetCall(ctx, callID)
	if err != nil {
		t.Fatalf("GetCall: %v", err)
	}
	if got.Status != cost.CallStarted {
		t.Fatalf("status = %q, want started", got.Status)
	}
	if got.SessionID != "sess-1" || got.TurnID != "turn-1" {
		t.Fatalf("correlation not persisted: %+v", got)
	}

	if err := svc.FinishCall(ctx, cost.ModelCall{
		ID:            callID,
		Status:        cost.CallCompleted,
		CostState:     cost.CostKnown,
		FinishedAt:    1500,
		FirstTokenAt:  1100,
		LatencyMS:     500,
		TTFTMS:        100,
		InputTokens:   10,
		OutputTokens:  20,
		EstimatedCost: 0.5,
	}); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}

	got, err = svc.GetCall(ctx, callID)
	if err != nil {
		t.Fatalf("GetCall after finish: %v", err)
	}
	if got.Status != cost.CallCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if got.TTFTMS != 100 || got.LatencyMS != 500 {
		t.Fatalf("timing not persisted: ttft=%d latency=%d", got.TTFTMS, got.LatencyMS)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 {
		t.Fatalf("usage not persisted: %+v", got)
	}
}

func TestSessionTotalsAreCumulative(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	// Two completed calls in the same session. The bug this guards against
	// (roadmapplan.md §5.2) is overwriting totals with the latest call.
	finish(t, svc, "sess", cost.ModelCall{
		InputTokens: 100, OutputTokens: 40,
		CacheCreationTokens: 10, CacheReadTokens: 5, EstimatedCost: 1.0,
	})
	finish(t, svc, "sess", cost.ModelCall{
		InputTokens: 200, OutputTokens: 60,
		CacheCreationTokens: 20, CacheReadTokens: 15, EstimatedCost: 2.0,
	})

	totals, err := svc.SessionTotals(ctx, "sess")
	if err != nil {
		t.Fatalf("SessionTotals: %v", err)
	}
	if totals.Calls != 2 {
		t.Fatalf("calls = %d, want 2", totals.Calls)
	}
	if totals.InputTokens != 300 || totals.OutputTokens != 100 {
		t.Fatalf("input/output not summed: %+v", totals)
	}
	if totals.CacheCreationTokens != 30 || totals.CacheReadTokens != 20 {
		t.Fatalf("cache tokens not summed separately: %+v", totals)
	}
	// prompt = input + cache_creation; completion = output + cache_read
	if totals.PromptTokens != 330 || totals.CompletionTokens != 120 {
		t.Fatalf("derived prompt/completion wrong: %+v", totals)
	}
	if !almostEqual(totals.Cost, 3.0) {
		t.Fatalf("cost = %v, want 3.0", totals.Cost)
	}
	if totals.CostUnknown {
		t.Fatalf("CostUnknown should be false for fully-known calls")
	}
}

func TestStartedCallsExcludedFailedIncluded(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	// A still-running call must not contribute to totals.
	if _, err := svc.StartCall(ctx, cost.ModelCall{
		ID: ids.New(), SessionID: "s", Status: cost.CallStarted,
		StartedAt: 1, InputTokens: 999,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	// A failed call that still reported usage must be retained (§5.2).
	failedID := ids.New()
	mustStart(t, svc, failedID, "s")
	if err := svc.FinishCall(ctx, cost.ModelCall{
		ID: failedID, Status: cost.CallFailed, CostState: cost.CostKnown,
		FinishedAt: 5, InputTokens: 50, OutputTokens: 10, EstimatedCost: 0.25,
	}); err != nil {
		t.Fatalf("FinishCall failed call: %v", err)
	}

	totals, err := svc.SessionTotals(ctx, "s")
	if err != nil {
		t.Fatalf("SessionTotals: %v", err)
	}
	if totals.Calls != 1 {
		t.Fatalf("calls = %d, want 1 (started excluded)", totals.Calls)
	}
	if totals.InputTokens != 50 {
		t.Fatalf("input = %d, want 50 (started call's 999 excluded)", totals.InputTokens)
	}
}

func TestCostUnknownPropagates(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	finish(t, svc, "s", cost.ModelCall{EstimatedCost: 1.0, CostState: cost.CostKnown})
	id := ids.New()
	mustStart(t, svc, id, "s")
	if err := svc.FinishCall(ctx, cost.ModelCall{
		ID: id, Status: cost.CallCompleted, CostState: cost.CostUnknown, FinishedAt: 9,
	}); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}

	totals, err := svc.SessionTotals(ctx, "s")
	if err != nil {
		t.Fatalf("SessionTotals: %v", err)
	}
	if !totals.CostUnknown {
		t.Fatalf("CostUnknown should be true when any call has unknown pricing")
	}
}

func TestTaskTotals(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	id := ids.New()
	if _, err := svc.StartCall(ctx, cost.ModelCall{
		ID: id, SessionID: "s", TaskID: "task-42", Status: cost.CallStarted, StartedAt: 1,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	if err := svc.FinishCall(ctx, cost.ModelCall{
		ID: id, Status: cost.CallCompleted, CostState: cost.CostKnown,
		FinishedAt: 2, InputTokens: 7, EstimatedCost: 0.7,
	}); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}

	totals, err := svc.TaskTotals(ctx, "task-42")
	if err != nil {
		t.Fatalf("TaskTotals: %v", err)
	}
	if totals.InputTokens != 7 || !almostEqual(totals.Cost, 0.7) {
		t.Fatalf("task totals wrong: %+v", totals)
	}
}

func TestTaskTotalsRollsUpSubagentTasks(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	taskStore := task.NewStore(conn)
	ctx := context.Background()

	parentID, childID, grandchildID := "parent-task", "child-task", "grandchild-task"
	if err := taskStore.CreateTask(ctx, task.Task{ID: parentID, SessionID: "s", Objective: "o", Status: task.StatusRunning, CreatedAt: 1}); err != nil {
		t.Fatalf("CreateTask parent: %v", err)
	}
	if err := taskStore.CreateTask(ctx, task.Task{ID: childID, SessionID: "s", Objective: "o", Status: task.StatusRunning, CreatedAt: 1, ParentTaskID: parentID}); err != nil {
		t.Fatalf("CreateTask child: %v", err)
	}
	if err := taskStore.CreateTask(ctx, task.Task{ID: grandchildID, SessionID: "s", Objective: "o", Status: task.StatusRunning, CreatedAt: 1, ParentTaskID: childID}); err != nil {
		t.Fatalf("CreateTask grandchild: %v", err)
	}
	// An unrelated top-level task must never be counted.
	if err := taskStore.CreateTask(ctx, task.Task{ID: "unrelated-task", SessionID: "s", Objective: "o", Status: task.StatusRunning, CreatedAt: 1}); err != nil {
		t.Fatalf("CreateTask unrelated: %v", err)
	}

	finishCall := func(id, taskID string, inputTokens int64, estCost float64) {
		t.Helper()
		if _, err := svc.StartCall(ctx, cost.ModelCall{ID: id, SessionID: "s", TaskID: taskID, Status: cost.CallStarted, StartedAt: 1}); err != nil {
			t.Fatalf("StartCall %s: %v", id, err)
		}
		if err := svc.FinishCall(ctx, cost.ModelCall{
			ID: id, Status: cost.CallCompleted, CostState: cost.CostKnown,
			FinishedAt: 2, InputTokens: inputTokens, EstimatedCost: estCost,
		}); err != nil {
			t.Fatalf("FinishCall %s: %v", id, err)
		}
	}
	finishCall(ids.New(), parentID, 10, 1.0)
	finishCall(ids.New(), childID, 20, 2.0)
	finishCall(ids.New(), grandchildID, 30, 3.0)
	finishCall(ids.New(), "unrelated-task", 999, 999.0)

	totals, err := svc.TaskTotals(ctx, parentID)
	if err != nil {
		t.Fatalf("TaskTotals: %v", err)
	}
	if totals.Calls != 3 || !almostEqual(totals.Cost, 6.0) || totals.InputTokens != 60 {
		t.Fatalf("parent totals should include child+grandchild but not the unrelated task: %+v", totals)
	}

	childTotals, err := svc.TaskTotals(ctx, childID)
	if err != nil {
		t.Fatalf("TaskTotals(child): %v", err)
	}
	if childTotals.Calls != 2 || !almostEqual(childTotals.Cost, 5.0) {
		t.Fatalf("child totals should include itself + grandchild only: %+v", childTotals)
	}
}

func TestSessionTotalsRollsUpChildSessions(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	// Parent session with one $1.00 call.
	insertSession(t, conn, "parent", "")
	finish(t, svc, "parent", cost.ModelCall{EstimatedCost: 1.0, CostState: cost.CostKnown})
	// Reconcile parent's stored cost so the child rollup query has data.
	// Child (subagent) session whose stored cost is $0.50.
	insertSession(t, conn, "child", "parent")
	setSessionCost(t, conn, "child", 0.5)

	totals, err := svc.SessionTotals(ctx, "parent")
	if err != nil {
		t.Fatalf("SessionTotals: %v", err)
	}
	if !almostEqual(totals.Cost, 1.5) {
		t.Fatalf("parent cost = %v, want 1.5 (own 1.0 + child 0.5)", totals.Cost)
	}
}

// helpers

func mustStart(t *testing.T, svc cost.Service, id, session string) {
	t.Helper()
	if _, err := svc.StartCall(context.Background(), cost.ModelCall{
		ID: id, SessionID: session, Status: cost.CallStarted, StartedAt: 1,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}
}

func finish(t *testing.T, svc cost.Service, session string, mc cost.ModelCall) {
	t.Helper()
	id := ids.New()
	mustStart(t, svc, id, session)
	mc.ID = id
	if mc.Status == "" {
		mc.Status = cost.CallCompleted
	}
	if mc.CostState == "" {
		mc.CostState = cost.CostKnown
	}
	if mc.FinishedAt == 0 {
		mc.FinishedAt = 2
	}
	if err := svc.FinishCall(context.Background(), mc); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}
}

func insertSession(t *testing.T, conn *sql.DB, id, parent string) {
	t.Helper()
	var parentVal any
	if parent != "" {
		parentVal = parent
	}
	_, err := conn.Exec(`INSERT INTO sessions (id, parent_session_id, title, updated_at, created_at)
		VALUES (?, ?, ?, strftime('%s','now'), strftime('%s','now'))`, id, parentVal, "t")
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func setSessionCost(t *testing.T, conn *sql.DB, id string, cost float64) {
	t.Helper()
	if _, err := conn.Exec(`UPDATE sessions SET cost = ? WHERE id = ?`, cost, id); err != nil {
		t.Fatalf("set session cost: %v", err)
	}
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}

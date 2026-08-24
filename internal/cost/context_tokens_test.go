package cost_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// finish records one completed call with the given usage.
func finishOccupancyCall(t *testing.T, svc cost.Service, sessionID string, startedAt, in, out, cacheNew, cacheRead int64) {
	t.Helper()
	ctx := context.Background()
	id := ids.New()
	if _, err := svc.StartCall(ctx, cost.ModelCall{
		ID: id, SessionID: sessionID, TurnID: "t", Provider: "local",
		Model: "m", Status: cost.CallStarted, StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	if err := svc.FinishCall(ctx, cost.ModelCall{
		ID: id, Status: cost.CallCompleted, CostState: cost.CostKnown,
		FinishedAt: startedAt + 100, InputTokens: in, OutputTokens: out,
		CacheCreationTokens: cacheNew, CacheReadTokens: cacheRead,
	}); err != nil {
		t.Fatalf("FinishCall: %v", err)
	}
}

// A conversation that is resent each turn occupies the window once, not once
// per turn. This is the case that made the header read seven times high: seven
// calls over the same ~21K conversation summed to 148K.
func TestSessionContextTokensIsOccupancyNotCumulativeSpend(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	// Seven turns, each resending a conversation that grows slowly.
	inputs := []int64{20996, 21019, 21101, 21131, 21167, 21209, 21252}
	for i, in := range inputs {
		finishOccupancyCall(t, svc, "sess-growing", int64(1000+i), in, 20, 0, 0)
	}

	got, err := svc.SessionContextTokens(ctx, "sess-growing")
	if err != nil {
		t.Fatalf("SessionContextTokens: %v", err)
	}
	// The last call's whole input plus its output -- what the window holds now.
	if want := int64(21252 + 20); got != want {
		t.Errorf("context tokens = %d, want %d (the latest turn's occupancy)", got, want)
	}

	totals, err := svc.SessionTotals(ctx, "sess-growing")
	if err != nil {
		t.Fatalf("SessionTotals: %v", err)
	}
	cumulative := totals.PromptTokens + totals.CompletionTokens
	if got >= cumulative {
		t.Fatalf("occupancy %d should be far below cumulative spend %d; the two are being confused again",
			got, cumulative)
	}
}

// Cached input occupies the window exactly as fresh input does, so it counts
// towards occupancy. The session totals file cache reads under "completion",
// which is why a cached session could report more completion than prompt.
func TestSessionContextTokensCountsCachedInput(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	finishOccupancyCall(t, svc, "sess-cached", 1000, 21165, 35, 0, 128)
	finishOccupancyCall(t, svc, "sess-cached", 1001, 81, 41, 0, 21248)
	finishOccupancyCall(t, svc, "sess-cached", 1002, 154, 270, 0, 21248)

	got, err := svc.SessionContextTokens(ctx, "sess-cached")
	if err != nil {
		t.Fatalf("SessionContextTokens: %v", err)
	}
	if want := int64(154 + 21248 + 270); got != want {
		t.Errorf("context tokens = %d, want %d; cached input must count towards occupancy", got, want)
	}
}

// A session that has never completed a call has nothing in the window. It must
// read zero rather than erroring, because the header renders before the first
// turn.
func TestSessionContextTokensIsZeroBeforeAnyCall(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)

	got, err := svc.SessionContextTokens(context.Background(), "sess-empty")
	if err != nil {
		t.Fatalf("SessionContextTokens on an untouched session: %v", err)
	}
	if got != 0 {
		t.Errorf("context tokens = %d, want 0", got)
	}
}

// An in-flight call has no usage yet; occupancy must keep reporting the last
// settled turn rather than dropping to zero mid-request.
func TestSessionContextTokensIgnoresInFlightCalls(t *testing.T) {
	conn := dbtest.New(t)
	svc := cost.NewService(conn)
	ctx := context.Background()

	finishOccupancyCall(t, svc, "sess-inflight", 1000, 5000, 100, 0, 0)
	if _, err := svc.StartCall(ctx, cost.ModelCall{
		ID: ids.New(), SessionID: "sess-inflight", TurnID: "t2", Provider: "local",
		Model: "m", Status: cost.CallStarted, StartedAt: 2000,
	}); err != nil {
		t.Fatalf("StartCall: %v", err)
	}

	got, err := svc.SessionContextTokens(ctx, "sess-inflight")
	if err != nil {
		t.Fatalf("SessionContextTokens: %v", err)
	}
	if want := int64(5100); got != want {
		t.Errorf("context tokens = %d, want %d; an unfinished call must not zero the meter", got, want)
	}
}

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
)

// probeTool records concurrency and can be made to block, deny, or stall.
type probeTool struct {
	name string
	hold time.Duration
	deny bool

	mu        sync.Mutex
	running   int
	maxAtOnce int
	calls     []string
}

func (p *probeTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: p.name} }

func (p *probeTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	p.mu.Lock()
	p.running++
	if p.running > p.maxAtOnce {
		p.maxAtOnce = p.running
	}
	p.calls = append(p.calls, call.ID)
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.running--
		p.mu.Unlock()
	}()

	if p.deny {
		return tools.ToolResponse{}, permission.ErrorPermissionDenied
	}
	if p.hold > 0 {
		select {
		case <-time.After(p.hold):
		case <-ctx.Done():
			return tools.ToolResponse{}, ctx.Err()
		}
	}
	return tools.NewTextResponse("ran " + call.ID), nil
}

func (p *probeTool) peakConcurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxAtOnce
}

func (p *probeTool) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func newTestAgent(ts ...tools.BaseTool) *agent {
	return &agent{tools: ts, executor: tools.NewExecutor(nil, nil)}
}

func call(id, name string) message.ToolCall {
	return message.ToolCall{ID: id, Name: name, Input: "{}"}
}

// The whole point of the change: independent reads in one turn should overlap.
func TestReadOnlyCallsRunConcurrently(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName, hold: 60 * time.Millisecond}
	a := newTestAgent(grep)

	calls := []message.ToolCall{
		call("c1", tools.GrepToolName),
		call("c2", tools.GrepToolName),
		call("c3", tools.GrepToolName),
		call("c4", tools.GrepToolName),
	}

	start := time.Now()
	results, cancelled := a.executeToolCalls(t.Context(), calls)
	elapsed := time.Since(start)

	if cancelled {
		t.Fatal("nothing cancelled these calls")
	}
	if peak := grep.peakConcurrency(); peak < 2 {
		t.Fatalf("read-only calls ran with peak concurrency %d; they should overlap", peak)
	}
	// Serially this is 240ms; overlapped it is nearer 60ms. A generous bound
	// keeps the test from flaking on a loaded machine while still failing if
	// execution is actually serial.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("4 x 60ms of read-only work took %v; that looks serial", elapsed)
	}
	for i, r := range results {
		if want := calls[i].ID; r.ToolCallID != want {
			t.Fatalf("result %d is for %q, want %q: order must match the request", i, r.ToolCallID, want)
		}
	}
}

// Parallelism must never reorder what the model sees.
func TestResultOrderMatchesCallOrder(t *testing.T) {
	// Staggered durations so completion order differs from request order.
	view := &probeTool{name: tools.ViewToolName, hold: 40 * time.Millisecond}
	grep := &probeTool{name: tools.GrepToolName}
	a := newTestAgent(view, grep)

	calls := []message.ToolCall{
		call("slow-1", tools.ViewToolName),
		call("fast-1", tools.GrepToolName),
		call("slow-2", tools.ViewToolName),
		call("fast-2", tools.GrepToolName),
	}

	results, _ := a.executeToolCalls(t.Context(), calls)
	for i, r := range results {
		if r.ToolCallID != calls[i].ID {
			t.Fatalf("position %d holds %q, want %q", i, r.ToolCallID, calls[i].ID)
		}
		if !strings.Contains(r.Content, calls[i].ID) {
			t.Fatalf("position %d has the wrong body: %q", i, r.Content)
		}
	}
}

// Anything that writes, executes, or reaches the network must stay serial.
func TestEffectfulCallsRunSequentially(t *testing.T) {
	bash := &probeTool{name: tools.BashToolName, hold: 20 * time.Millisecond}
	a := newTestAgent(bash)

	calls := []message.ToolCall{
		call("b1", tools.BashToolName),
		call("b2", tools.BashToolName),
		call("b3", tools.BashToolName),
	}
	if _, cancelled := a.executeToolCalls(t.Context(), calls); cancelled {
		t.Fatal("unexpected cancellation")
	}
	if peak := bash.peakConcurrency(); peak != 1 {
		t.Fatalf("bash ran %d at once; effectful tools must stay sequential", peak)
	}
}

// fetch reads rather than writes, but it is network egress behind an approval
// prompt, so it must be treated as effectful on both counts.
func TestFetchIsNotTreatedAsReadOnly(t *testing.T) {
	if isReadOnlyTool(tools.FetchToolName) {
		t.Fatal("fetch must not be in the read-only set")
	}
	fetch := &probeTool{name: tools.FetchToolName, hold: 20 * time.Millisecond}
	a := newTestAgent(fetch)

	calls := []message.ToolCall{
		call("f1", tools.FetchToolName),
		call("f2", tools.FetchToolName),
	}
	a.executeToolCalls(t.Context(), calls)
	if peak := fetch.peakConcurrency(); peak != 1 {
		t.Fatalf("fetch ran %d at once, want 1", peak)
	}
}

// An unrecognized tool name must fail closed as effectful rather than being
// silently parallelized.
func TestUnknownToolsAreTreatedAsEffectful(t *testing.T) {
	if isReadOnlyTool("some_new_mcp_tool") {
		t.Fatal("unclassified tools must not be assumed read-only")
	}
}

// The denial policy the user chose: the refusal stops state changes, not the
// reads the model already asked for.
func TestDenialContinuesReadsAndCancelsEffectfulCalls(t *testing.T) {
	deniedBash := &probeTool{name: tools.BashToolName, deny: true}
	grep := &probeTool{name: tools.GrepToolName}
	write := &probeTool{name: tools.WriteToolName}
	a := newTestAgent(deniedBash, grep, write)

	calls := []message.ToolCall{
		call("bash-1", tools.BashToolName),   // denied
		call("grep-1", tools.GrepToolName),   // must still run
		call("write-1", tools.WriteToolName), // must be cancelled
		call("grep-2", tools.GrepToolName),   // must still run
	}

	results, cancelled := a.executeToolCalls(t.Context(), calls)
	if cancelled {
		t.Fatal("a denial is not a turn cancellation")
	}

	if results[0].Content != "Permission denied" {
		t.Fatalf("denied call should report the refusal, got %q", results[0].Content)
	}
	if grep.callCount() != 2 {
		t.Fatalf("both reads should have run after the denial, got %d", grep.callCount())
	}
	if !strings.Contains(results[1].Content, "grep-1") || results[1].IsError {
		t.Fatalf("read after a denial should have succeeded, got %+v", results[1])
	}
	if !strings.Contains(results[3].Content, "grep-2") || results[3].IsError {
		t.Fatalf("later read should have succeeded, got %+v", results[3])
	}
	if write.callCount() != 0 {
		t.Fatal("a write after a denial must not run")
	}
	if !results[2].IsError || !strings.Contains(results[2].Content, "denied") {
		t.Fatalf("cancelled write should say why, got %+v", results[2])
	}
}

// A denial inside a parallel batch must still stop later effectful calls.
func TestDenialInsideReadOnlyBatchStopsLaterEffectfulCalls(t *testing.T) {
	view := &probeTool{name: tools.ViewToolName, deny: true}
	grep := &probeTool{name: tools.GrepToolName}
	bash := &probeTool{name: tools.BashToolName}
	a := newTestAgent(view, grep, bash)

	calls := []message.ToolCall{
		call("view-1", tools.ViewToolName), // denied, inside the batch
		call("grep-1", tools.GrepToolName),
		call("bash-1", tools.BashToolName), // must be cancelled
	}

	results, _ := a.executeToolCalls(t.Context(), calls)
	if bash.callCount() != 0 {
		t.Fatal("bash must not run after a denial earlier in the turn")
	}
	if !results[2].IsError {
		t.Fatalf("cancelled bash should be an error result, got %+v", results[2])
	}
}

func TestCancelledContextCancelsRemainingCalls(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName}
	a := newTestAgent(grep)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	calls := []message.ToolCall{call("c1", tools.GrepToolName), call("c2", tools.GrepToolName)}
	results, cancelled := a.executeToolCalls(ctx, calls)

	if !cancelled {
		t.Fatal("an already-cancelled context must report cancellation")
	}
	if grep.callCount() != 0 {
		t.Fatal("no tool should run under a cancelled context")
	}
	for i, r := range results {
		if !r.IsError || r.ToolCallID != calls[i].ID {
			t.Fatalf("result %d should be a cancellation placeholder, got %+v", i, r)
		}
	}
}

func TestMissingToolReportsNotFoundWithoutStoppingTheTurn(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName}
	a := newTestAgent(grep)

	calls := []message.ToolCall{
		call("x1", "no_such_tool"),
		call("g1", tools.GrepToolName),
	}
	results, cancelled := a.executeToolCalls(t.Context(), calls)
	if cancelled {
		t.Fatal("a missing tool is not a cancellation")
	}
	if !strings.Contains(results[0].Content, "Tool not found") {
		t.Fatalf("expected a not-found result, got %q", results[0].Content)
	}
	if grep.callCount() != 1 {
		t.Fatal("a missing tool must not prevent the rest of the turn")
	}
}

// The concurrency cap must actually bound fan-out.
func TestParallelismIsCapped(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName, hold: 30 * time.Millisecond}
	a := newTestAgent(grep)

	calls := make([]message.ToolCall, 16)
	for i := range calls {
		calls[i] = call(fmt.Sprintf("c%d", i), tools.GrepToolName)
	}
	a.executeToolCalls(t.Context(), calls)

	if peak := grep.peakConcurrency(); peak > maxParallelTools {
		t.Fatalf("peak concurrency %d exceeds the cap of %d", peak, maxParallelTools)
	}
	if peak := grep.peakConcurrency(); peak < 2 {
		t.Fatalf("expected real parallelism, peak was %d", peak)
	}
}

// A single read-only call must not pay for goroutine setup, and must behave
// identically to the batched path.
func TestSingleReadOnlyCallStillRuns(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName}
	a := newTestAgent(grep)

	results, _ := a.executeToolCalls(t.Context(), []message.ToolCall{call("only", tools.GrepToolName)})
	if len(results) != 1 || !strings.Contains(results[0].Content, "only") {
		t.Fatalf("unexpected result: %+v", results)
	}
}

// Interleaved read-only and effectful calls must batch only the read-only runs
// and keep every result in place.
func TestMixedSequenceBatchesOnlyReadOnlyRuns(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName, hold: 30 * time.Millisecond}
	bash := &probeTool{name: tools.BashToolName}
	a := newTestAgent(grep, bash)

	calls := []message.ToolCall{
		call("g1", tools.GrepToolName),
		call("g2", tools.GrepToolName),
		call("b1", tools.BashToolName),
		call("g3", tools.GrepToolName),
		call("g4", tools.GrepToolName),
	}
	results, _ := a.executeToolCalls(t.Context(), calls)

	for i, r := range results {
		if r.ToolCallID != calls[i].ID {
			t.Fatalf("position %d holds %q, want %q", i, r.ToolCallID, calls[i].ID)
		}
	}
	if peak := grep.peakConcurrency(); peak < 2 {
		t.Fatalf("adjacent reads should still batch around a bash call, peak was %d", peak)
	}
	if peak := bash.peakConcurrency(); peak != 1 {
		t.Fatalf("bash peak concurrency %d, want 1", peak)
	}
}

// Guards against a results slice shared unsafely between goroutines.
func TestParallelBatchIsRaceFree(t *testing.T) {
	grep := &probeTool{name: tools.GrepToolName}
	a := newTestAgent(grep)

	var wg sync.WaitGroup
	var total int32
	for r := 0; r < 8; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			calls := make([]message.ToolCall, 8)
			for i := range calls {
				calls[i] = call(fmt.Sprintf("c%d", i), tools.GrepToolName)
			}
			results, _ := a.executeToolCalls(t.Context(), calls)
			atomic.AddInt32(&total, int32(len(results)))
		}()
	}
	wg.Wait()
	if total != 64 {
		t.Fatalf("expected 64 results overall, got %d", total)
	}
}

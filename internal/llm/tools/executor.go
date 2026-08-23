package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// Execution status values recorded by the executor.
const (
	ExecStarted   = "started"
	ExecCompleted = "completed"
	ExecError     = "error"  // tool returned an error ToolResponse (not a Go error)
	ExecFailed    = "failed" // tool.Run returned a Go error (e.g. permission denied)
)

// ExecutionRecord is the observability record for a single tool run. It is
// passed to a Recorder at start and finish. It intentionally carries only
// identifiers and sizes, never the full tool output (that becomes an artifact
// via a Virtualizer).
type ExecutionRecord struct {
	ID            string
	Correlation   Correlation
	ToolCallID    string
	ToolName      string
	InputHash     string
	Status        string
	StartedAt     int64 // unix millis
	FinishedAt    int64 // unix millis
	LatencyMS     int64
	ResponseBytes int64 // original tool-output size
	BytesSaved    int64 // bytes not sent to the model due to virtualization
	IsError       bool
	ArtifactID    string
	Metadata      string
}

// Virtualizer may replace a large tool response with a compact digest plus an
// artifact handle, storing the full output once (roadmapplan.md §7.5). It sets
// rec.ArtifactID and rec.BytesSaved and returns the (possibly rewritten)
// response. In observe mode it stores the artifact but returns the response
// unchanged.
type Virtualizer interface {
	Virtualize(ctx context.Context, rec *ExecutionRecord, resp ToolResponse) (ToolResponse, error)
}

// Recorder persists tool-execution lifecycle and/or emits events. It is
// dependency-inverted so the tools package stays free of db/eventstore imports.
// A nil Recorder disables recording without changing tool behaviour.
type Recorder interface {
	Start(ctx context.Context, rec ExecutionRecord)
	Finish(ctx context.Context, rec ExecutionRecord)
}

// Executor wraps every BaseTool.Run with canonical input hashing, correlation,
// timing, size measurement, and lifecycle recording. It returns exactly the
// tool's response and error so existing behaviour and permission handling are
// unchanged (roadmapplan.md §5.4).
type Executor struct {
	recorder    Recorder
	virtualizer Virtualizer
	hooks       *hooks.Registry
}

// NewExecutor returns an executor. recorder and virtualizer may both be nil.
func NewExecutor(recorder Recorder, virtualizer Virtualizer) *Executor {
	return &Executor{recorder: recorder, virtualizer: virtualizer}
}

// WithHooks wires a lifecycle-hook registry so ToolPre/ToolPost fire around
// every tool execution (roadmapplan.md §12.3). Returning an error from a
// ToolPre handler vetoes the call: the tool never runs and nothing is
// recorded, matching Registry.Dispatch's documented veto semantics. A nil
// registry (the default) is a no-op, identical to today's behaviour.
func (e *Executor) WithHooks(r *hooks.Registry) *Executor {
	e.hooks = r
	return e
}

// Execute runs the tool, recording its lifecycle. The returned response and
// error are identical to calling tool.Run directly, except a ToolPre veto
// short-circuits before the tool ever runs.
func (e *Executor) Execute(ctx context.Context, tool BaseTool, call ToolCall) (ToolResponse, error) {
	corr := CorrelationFromContext(ctx)
	if err := e.hooks.Dispatch(ctx, hooks.Event{
		Point: hooks.ToolPre, TaskID: corr.TaskID, SessionID: corr.SessionID, Tool: call.Name,
	}); err != nil {
		return ToolResponse{}, err
	}

	rec := ExecutionRecord{
		ID:          ids.New(),
		Correlation: corr,
		ToolCallID:  call.ID,
		ToolName:    call.Name,
		InputHash:   HashToolInput(call.Input),
		Status:      ExecStarted,
		StartedAt:   time.Now().UnixMilli(),
	}
	// Expose the execution id so tools can later attach artifacts to it.
	ctx = context.WithValue(ctx, ToolExecutionIDContextKey, rec.ID)

	if e.recorder != nil {
		e.recorder.Start(ctx, rec)
	}

	start := time.Now()
	resp, err := tool.Run(ctx, call)
	rec.LatencyMS = time.Since(start).Milliseconds()
	rec.FinishedAt = time.Now().UnixMilli()
	rec.ResponseBytes = int64(len(resp.Content))
	rec.Metadata = resp.Metadata

	// Virtualize large output before recording the final status. Hard tool
	// failures (Go errors) are left untouched so diagnostics are never hidden.
	if e.virtualizer != nil && err == nil {
		if newResp, verr := e.virtualizer.Virtualize(ctx, &rec, resp); verr == nil {
			resp = newResp
		}
	}

	rec.IsError = resp.IsError || err != nil
	switch {
	case err != nil:
		rec.Status = ExecFailed
	case resp.IsError:
		rec.Status = ExecError
	default:
		rec.Status = ExecCompleted
	}

	if e.recorder != nil {
		e.recorder.Finish(ctx, rec)
	}

	// ToolPost is advisory (roadmapplan.md §12.3 Dispatch semantics): its error,
	// if any, is not surfaced here so a post-hook can never turn a successful
	// tool call into a failure the caller has to handle.
	_ = e.hooks.Dispatch(ctx, hooks.Event{
		Point: hooks.ToolPost, TaskID: corr.TaskID, SessionID: corr.SessionID, Tool: call.Name, Outcome: rec.Status,
	})

	return resp, err
}

// HashToolInput returns a stable SHA-256 hex digest of a tool's input. JSON
// object inputs are canonicalized (Go marshals map keys sorted) so semantically
// equal inputs with different key ordering hash identically; non-JSON inputs are
// hashed as raw bytes.
func HashToolInput(input string) string {
	canonical := []byte(input)
	var v any
	if json.Unmarshal([]byte(input), &v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canonical = b
		}
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

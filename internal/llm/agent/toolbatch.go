package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
)

// maxParallelTools caps how many read-only tool calls run at once.
//
// The work is IO-bound (file reads, ripgrep spawns, LSP queries) rather than
// CPU-bound, and models rarely emit more than a handful of calls per turn, so a
// small fixed cap captures nearly all of the available speedup without turning a
// wide fan-out into a thundering herd against the filesystem.
const maxParallelTools = 4

// readOnlyTools observe the workspace without changing it, running the network,
// or executing anything.
//
// The set is deliberately explicit rather than derived. It decides two separate
// things — which calls may run concurrently, and which calls survive a denial
// elsewhere in the turn — and both answers must fail closed for a tool nobody
// remembered to classify. A tool absent from this map is treated as effectful.
//
// fetch is excluded on purpose: it reads rather than writes, but it is network
// egress gated on user approval, so it belongs with the effectful tools on both
// counts.
var readOnlyTools = map[string]bool{
	tools.ViewToolName:        true,
	tools.LSToolName:          true,
	tools.GlobToolName:        true,
	tools.GrepToolName:        true,
	tools.SourcegraphToolName: true,
	tools.DiagnosticsToolName: true,
}

// isReadOnlyTool reports whether a tool only observes the workspace.
func isReadOnlyTool(name string) bool { return readOnlyTools[name] }

// findTool resolves a tool call to a registered tool, or nil.
func (a *agent) findTool(name string) tools.BaseTool {
	for _, t := range a.tools {
		if t.Info().Name == name {
			return t
		}
	}
	return nil
}

// runToolCall executes one tool call and converts its outcome into a tool
// result. denied reports whether the call failed because the user refused it,
// which the caller uses to decide what happens to the rest of the turn.
func (a *agent) runToolCall(ctx context.Context, call message.ToolCall) (result message.ToolResult, denied bool) {
	tool := a.findTool(call.Name)
	if tool == nil {
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    fmt.Sprintf("Tool not found: %s", call.Name),
			IsError:    true,
		}, false
	}

	resp, err := a.executor.Execute(ctx, tool, tools.ToolCall{
		ID:    call.ID,
		Name:  call.Name,
		Input: call.Input,
	})
	if err != nil && errors.Is(err, permission.ErrorPermissionDenied) {
		return message.ToolResult{
			ToolCallID: call.ID,
			Content:    "Permission denied",
			IsError:    true,
		}, true
	}

	return message.ToolResult{
		ToolCallID: call.ID,
		Content:    resp.Content,
		Metadata:   resp.Metadata,
		IsError:    resp.IsError,
	}, false
}

// runReadOnlyBatch executes a run of read-only calls concurrently, writing each
// result to its own index so the order the model sees is the order it asked for.
//
// Concurrency is safe here because these tools share no mutable state: the
// executor is stateless per call, the recorder writes through SQLite (which
// serializes), the hook registry guards its handler map, and permission prompts
// are serialized by the permission service so at most one dialog is ever open.
func (a *agent) runReadOnlyBatch(ctx context.Context, calls []message.ToolCall, results []message.ToolResult) (denied bool) {
	if len(calls) == 1 {
		results[0], denied = a.runToolCall(ctx, calls[0])
		return denied
	}

	var (
		wg        sync.WaitGroup
		sem       = make(chan struct{}, maxParallelTools)
		mu        sync.Mutex
		anyDenied bool
	)
	for i := range calls {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, d := a.runToolCall(ctx, calls[i])
			results[i] = res
			if d {
				mu.Lock()
				anyDenied = true
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	return anyDenied
}

// cancelledResult is the placeholder recorded for a call that never ran.
func cancelledResult(call message.ToolCall, reason string) message.ToolResult {
	return message.ToolResult{
		ToolCallID: call.ID,
		Content:    reason,
		IsError:    true,
	}
}

// executeToolCalls runs a turn's tool calls and returns their results in order.
//
// Consecutive read-only calls run concurrently; everything else runs one at a
// time, in order, exactly as before. Models routinely emit several independent
// reads per turn, and running those serially was pure latency.
//
// Denial policy: refusing one call does not discard the rest of the turn. The
// denied call reports the refusal, later read-only calls still run — the model
// keeps the context it already asked for and can reformulate — and later
// effectful calls are cancelled, so "no" remains a real stop for anything that
// would change state.
func (a *agent) executeToolCalls(ctx context.Context, calls []message.ToolCall) (results []message.ToolResult, cancelled bool) {
	results = make([]message.ToolResult, len(calls))

	effectfulStopped := false
	for i := 0; i < len(calls); {
		if ctx.Err() != nil {
			for j := i; j < len(calls); j++ {
				results[j] = cancelledResult(calls[j], "Tool execution canceled by user")
			}
			return results, true
		}

		if effectfulStopped && !isReadOnlyTool(calls[i].Name) {
			results[i] = cancelledResult(calls[i], "Tool execution canceled: an earlier call in this turn was denied")
			i++
			continue
		}

		if !isReadOnlyTool(calls[i].Name) {
			result, denied := a.runToolCall(ctx, calls[i])
			results[i] = result
			if denied {
				effectfulStopped = true
			}
			i++
			continue
		}

		// Take the whole run of consecutive read-only calls.
		end := i
		for end < len(calls) && isReadOnlyTool(calls[end].Name) {
			end++
		}
		if a.runReadOnlyBatch(ctx, calls[i:end], results[i:end]) {
			effectfulStopped = true
		}
		i = end
	}

	return results, false
}

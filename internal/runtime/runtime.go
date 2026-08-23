// Package runtime defines the provider-neutral seam for executing one agent turn.
//
// A turn is one model call plus the tool results it produces. In Phase 1.0 the
// implementation (internal/llm/agent) compiles the raw stored message history as
// the model prompt exactly as before — this is the compatibility shell, a seam
// rather than an optimization (roadmapplan.md §5.5). Later phases (§7.2 Prompt
// Compiler) replace history compilation behind this same Runner interface without
// changing its callers.
package runtime

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/message"
)

// TurnResult is the outcome of running one turn: the assistant message and the
// tool-result message it produced (nil when the turn ended without tool use).
type TurnResult struct {
	Assistant   message.Message
	ToolResults *message.Message
}

// Runner executes a single agent turn against a session and its compiled history.
type Runner interface {
	RunTurn(ctx context.Context, sessionID string, history []message.Message) (TurnResult, error)
}

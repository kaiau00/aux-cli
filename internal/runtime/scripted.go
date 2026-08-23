package runtime

import (
	"context"
	"sync"

	"github.com/kaiau00/aux-cli/internal/message"
)

// Adapter is a named runtime that executes agent turns behind the provider-
// neutral Runner seam (roadmapplan.md §12.4). Adapters are interchangeable and
// contract-tested via runtimetest.RunnerContract, so a local, remote, or replay
// runtime can be swapped without changing callers.
type Adapter interface {
	Runner
	Name() string
}

// ScriptedRunner is a deterministic reference Adapter that replays a fixed script
// of turns. It is used for tests, replay, and as the canonical example of the
// adapter contract. It honors context cancellation and never blocks.
type ScriptedRunner struct {
	mu    sync.Mutex
	turns []TurnResult
	idx   int
}

// NewScriptedRunner returns an adapter that yields the given turns in order, then
// yields empty assistant turns.
func NewScriptedRunner(turns ...TurnResult) *ScriptedRunner {
	return &ScriptedRunner{turns: turns}
}

// Name identifies the adapter.
func (s *ScriptedRunner) Name() string { return "scripted" }

// RunTurn returns the next scripted turn, or an empty assistant turn once the
// script is exhausted. A cancelled context is honored before any work.
func (s *ScriptedRunner) RunTurn(ctx context.Context, _ string, _ []message.Message) (TurnResult, error) {
	if err := ctx.Err(); err != nil {
		return TurnResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx >= len(s.turns) {
		return TurnResult{Assistant: message.Message{Role: message.Assistant}}, nil
	}
	t := s.turns[s.idx]
	s.idx++
	return t, nil
}

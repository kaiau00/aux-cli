package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
)

// addendumSpyTool records whether the system addendum leaked into the context
// tools run with; a subagent started from a tool must compile its own.
type addendumSpyTool struct{ seen []string }

func (s *addendumSpyTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: "spy"} }
func (s *addendumSpyTool) Run(ctx context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	s.seen = append(s.seen, provider.SystemAddendumFromContext(ctx))
	return tools.NewTextResponse("ok"), nil
}

func TestRunTurnSendsProjectContextAsSystemAddendum(t *testing.T) {
	p := provider.NewMockProvider(mockModel(),
		provider.ToolCallTurn(provider.TokenUsage{InputTokens: 5, OutputTokens: 2},
			message.ToolCall{ID: "c1", Name: "spy", Input: "{}", Finished: true}))
	spy := &addendumSpyTool{}
	a, _, _, sessID := newTurnAgent(t, p, spy)

	ctx := context.WithValue(context.Background(), tools.TaskIDContextKey, "task-1")
	ctx = promptcompiler.WithProjectContext(ctx, "Languages: go\n", "Task (implementation): add Sub")
	history := []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "add Sub"}}}}
	if _, err := a.RunTurn(ctx, sessID, history); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	got := p.SystemAddenda()
	if len(got) != 1 {
		t.Fatalf("provider calls = %d; want 1", len(got))
	}
	want := promptcompiler.RenderSystemAddendum("Languages: go\n", "Task (implementation): add Sub")
	if got[0] != want || !strings.Contains(got[0], "Languages: go") {
		t.Fatalf("provider received addendum %q; want %q", got[0], want)
	}
	if len(spy.seen) != 1 || spy.seen[0] != "" {
		t.Fatalf("tool context carried addendum %q; it must be scoped to the provider call", spy.seen)
	}
}

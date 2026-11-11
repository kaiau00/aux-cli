package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/page"
)

type compactTestAgent struct {
	idleAgent
	called *string
	err    error
}

func (a compactTestAgent) Model() models.Model                          { return models.Model{ContextWindow: 100} }
func (a compactTestAgent) Summarize(_ context.Context, id string) error { *a.called = id; return a.err }

func TestCompactAppWiringAndImmediateFailure(t *testing.T) {
	a := *newCommandTestApp(t).(*appModel)
	called := ""
	a.app.CoderAgent = compactTestAgent{called: &called, err: errors.New("session busy")}
	a.selectedSession = session.Session{ID: "session", ContextTokens: 99}
	config.Get().AutoCompact = true
	_, cmd := a.Update(pubsub.Event[agent.AgentEvent]{Payload: agent.AgentEvent{Type: agent.AgentEventTypeResponse, Done: true}})
	pressure, ok := cmd().(page.ContextPressureMsg)
	if !ok || pressure.SessionID != "session" || pressure.Used != 99 || pressure.Limit != 100 || !pressure.Auto {
		t.Fatalf("pressure wiring: %#v", pressure)
	}
	updated, cmd := a.Update(page.CompactSessionMsg{})
	a = updated.(appModel)
	if !a.isCompacting {
		t.Fatal("missing progress overlay")
	}
	result := cmd()
	if called != "session" {
		t.Fatalf("summarized %q", called)
	}
	updated, cmd = a.Update(result)
	if updated.(appModel).isCompacting || cmd == nil {
		t.Fatal("immediate failure left overlay stuck or hid error")
	}
}

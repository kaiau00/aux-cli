package agent

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/toolexec"
)

// spyCompiler records the Input it was last called with, so a test can assert
// what the agent actually handed to the compiler without a live provider.
type spyCompiler struct {
	inner promptcompiler.Compiler
	last  promptcompiler.Input
}

func (s *spyCompiler) Compile(in promptcompiler.Input) promptcompiler.CompiledPrompt {
	s.last = in
	return s.inner.Compile(in)
}

func TestStreamAndHandleEventsPassesTaskExclusionsToCompiler(t *testing.T) {
	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ledger := cost.NewService(conn)
	events := eventstore.NewService(conn)
	recorder := toolexec.NewRecorder(toolexec.NewStore(conn), events)
	pages := contextstore.NewStore(conn)

	spy := &spyCompiler{inner: promptcompiler.NewCompatibilityCompiler()}
	p := provider.NewMockProvider(mockModel(),
		provider.TextTurn("ack", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}))

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		sessions: sessions,
		messages: messages,
		ledger:   ledger,
		events:   events,
		compiler: spy,
		pages:    pages,
		executor: tools.NewExecutor(recorder, nil),
		provider: p,
	}

	sess, err := sessions.Create(context.Background(), "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Seed an exclusion for the task this turn will be correlated to.
	if err := pages.Exclude(context.Background(), "task-1", "c1"); err != nil {
		t.Fatalf("Exclude: %v", err)
	}

	ctx := context.WithValue(context.Background(), tools.TaskIDContextKey, "task-1")
	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "read the file"}}},
	}
	if _, err := a.RunTurn(ctx, sess.ID, history); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if !spy.last.ExcludedToolCallIDs["c1"] {
		t.Fatalf("expected the compiler to receive the task's exclusion for c1, got %+v", spy.last.ExcludedToolCallIDs)
	}
}

func TestStreamAndHandleEventsPassesTaskPinsToCompiler(t *testing.T) {
	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ledger := cost.NewService(conn)
	events := eventstore.NewService(conn)
	recorder := toolexec.NewRecorder(toolexec.NewStore(conn), events)
	pages := contextstore.NewStore(conn)

	spy := &spyCompiler{inner: promptcompiler.NewCompatibilityCompiler()}
	p := provider.NewMockProvider(mockModel(),
		provider.TextTurn("ack", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}))

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		sessions: sessions,
		messages: messages,
		ledger:   ledger,
		events:   events,
		compiler: spy,
		pages:    pages,
		executor: tools.NewExecutor(recorder, nil),
		provider: p,
	}

	sess, err := sessions.Create(context.Background(), "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if err := pages.Pin(context.Background(), "task-1", "c1"); err != nil {
		t.Fatalf("Pin: %v", err)
	}

	ctx := context.WithValue(context.Background(), tools.TaskIDContextKey, "task-1")
	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "read the file"}}},
	}
	if _, err := a.RunTurn(ctx, sess.ID, history); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	if !spy.last.PinnedToolCallIDs["c1"] {
		t.Fatalf("expected the compiler to receive the task's pin for c1, got %+v", spy.last.PinnedToolCallIDs)
	}
}

func TestStreamAndHandleEventsWithNoTaskPassesNoExclusions(t *testing.T) {
	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ledger := cost.NewService(conn)
	events := eventstore.NewService(conn)
	recorder := toolexec.NewRecorder(toolexec.NewStore(conn), events)
	pages := contextstore.NewStore(conn)

	spy := &spyCompiler{inner: promptcompiler.NewCompatibilityCompiler()}
	p := provider.NewMockProvider(mockModel(),
		provider.TextTurn("ack", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}))

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		sessions: sessions,
		messages: messages,
		ledger:   ledger,
		events:   events,
		compiler: spy,
		pages:    pages,
		executor: tools.NewExecutor(recorder, nil),
		provider: p,
	}
	sess, err := sessions.Create(context.Background(), "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
	}
	// No TaskIDContextKey set: a turn outside any task must not fail looking
	// up exclusions, and must compile with none.
	if _, err := a.RunTurn(context.Background(), sess.ID, history); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(spy.last.ExcludedToolCallIDs) != 0 {
		t.Fatalf("expected no exclusions without a task id, got %+v", spy.last.ExcludedToolCallIDs)
	}
}

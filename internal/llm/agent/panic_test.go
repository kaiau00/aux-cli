package agent

import (
	"context"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
)

// modelOnlyProvider satisfies provider.Provider for turns that never reach a
// model call. Run consults Model() before it starts its goroutine, so even a
// turn that panics immediately needs one.
type modelOnlyProvider struct{ provider.Provider }

func (modelOnlyProvider) Model() models.Model { return mockModel() }

// panickingMessages stands in for any dependency that fails catastrophically
// mid-turn. List is the first call processGeneration makes that reaches an
// injected dependency, so the panic lands inside the recovered goroutine
// rather than before it starts.
type panickingMessages struct{ message.Service }

func (panickingMessages) List(context.Context, string) ([]message.Message, error) {
	panic("dependency exploded mid-turn")
}

// A panic during a turn must leave the session usable. Recovering the panic is
// not enough on its own: the goroutine also owns the session's busy marker, the
// generation context, and the event channel, and every consumer-visible symptom
// of a wedged session comes from one of those three outliving the panic.
func TestRunReleasesTheSessionWhenTheTurnPanics(t *testing.T) {
	// RecoverPanic writes its log to the working directory; keep the repo clean.
	t.Chdir(t.TempDir())

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		provider: modelOnlyProvider{},
		messages: panickingMessages{},
	}

	const sessionID = "s-panic"
	events, err := a.Run(context.Background(), sessionID, "do the thing")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case ev := <-events:
		if ev.Type != AgentEventTypeError {
			t.Fatalf("expected an error event after a panic, got %v", ev.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a panicking turn delivered no event; the caller waits forever")
	}

	select {
	case _, open := <-events:
		if open {
			t.Fatal("expected the event channel to be closed after the error event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the event channel was never closed; a ranging consumer blocks forever")
	}

	if a.IsSessionBusy(sessionID) {
		t.Fatal("session still marked busy after a panic: it can never accept another message")
	}
}

// The recovery path must not depend on anyone still listening. If the consumer
// has already walked away -- the TUI moved to another session, the caller
// returned -- an unbuffered hand-off would block the goroutine forever, and the
// busy marker would never be released. That is the wedge above, with no symptom
// at all on the consumer side.
func TestRunReleasesTheSessionWhenNobodyIsListening(t *testing.T) {
	t.Chdir(t.TempDir())

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		provider: modelOnlyProvider{},
		messages: panickingMessages{},
	}

	const sessionID = "s-abandoned"
	if _, err := a.Run(context.Background(), sessionID, "do the thing"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for a.IsSessionBusy(sessionID) {
		if time.Now().After(deadline) {
			t.Fatal("session never released with no consumer reading; the goroutine is stuck on an unbuffered send")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

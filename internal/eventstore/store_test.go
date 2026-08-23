package eventstore_test

import (
	"context"
	"sync"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/pubsub"
)

func TestAppendAssignsMonotonicSequence(t *testing.T) {
	svc := eventstore.NewService(dbtest.New(t))
	ctx := context.Background()

	var prev int64
	for i := range 5 {
		ev, err := svc.Append(ctx, eventstore.Append{
			Type:      eventstore.TurnStarted,
			SessionID: "s",
			TurnID:    "turn",
			Payload:   eventstore.TurnPayload{TurnID: "turn"},
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if ev.Sequence != prev+1 {
			t.Fatalf("sequence = %d, want %d", ev.Sequence, prev+1)
		}
		if ev.ID == "" {
			t.Fatalf("event id not assigned")
		}
		if ev.SchemaVersion != eventstore.SchemaVersion {
			t.Fatalf("schema version = %d, want %d", ev.SchemaVersion, eventstore.SchemaVersion)
		}
		prev = ev.Sequence
	}
}

func TestListFiltersAndReconstructsTimeline(t *testing.T) {
	svc := eventstore.NewService(dbtest.New(t))
	ctx := context.Background()

	// Two turns for task A, one model call for task B.
	mustAppend(t, svc, eventstore.Append{Type: eventstore.TurnStarted, TaskID: "A", TurnID: "a1"})
	mustAppend(t, svc, eventstore.Append{Type: eventstore.ModelCallStarted, TaskID: "A", TurnID: "a1",
		Payload: eventstore.ModelCallPayload{ModelCallID: "c1", Model: "m"}})
	mustAppend(t, svc, eventstore.Append{Type: eventstore.TurnCompleted, TaskID: "A", TurnID: "a1"})
	mustAppend(t, svc, eventstore.Append{Type: eventstore.ModelCallStarted, TaskID: "B", TurnID: "b1"})

	aEvents, err := svc.List(ctx, eventstore.Filter{TaskID: "A"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(aEvents) != 3 {
		t.Fatalf("task A events = %d, want 3", len(aEvents))
	}
	// Ascending sequence order == chronological timeline.
	for i := 1; i < len(aEvents); i++ {
		if aEvents[i].Sequence <= aEvents[i-1].Sequence {
			t.Fatalf("events not in ascending sequence order")
		}
	}

	// Payload round-trips.
	var mc eventstore.ModelCallPayload
	if err := aEvents[1].DecodePayload(&mc); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if mc.ModelCallID != "c1" || mc.Model != "m" {
		t.Fatalf("payload wrong: %+v", mc)
	}

	// Type filter.
	starts, err := svc.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.ModelCallStarted}})
	if err != nil {
		t.Fatalf("list by type: %v", err)
	}
	if len(starts) != 2 {
		t.Fatalf("model_call.started events = %d, want 2", len(starts))
	}
}

// TestTimelineReconstructableAfterRestart simulates a process restart by opening
// a fresh Service over the same database file and reading the timeline back.
func TestTimelineReconstructableAfterRestart(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()

	first := eventstore.NewService(conn)
	mustAppend(t, first, eventstore.Append{Type: eventstore.TurnStarted, TaskID: "T", TurnID: "1"})
	mustAppend(t, first, eventstore.Append{Type: eventstore.ModelCallCompleted, TaskID: "T", TurnID: "1"})
	mustAppend(t, first, eventstore.Append{Type: eventstore.TurnCompleted, TaskID: "T", TurnID: "1"})

	// A brand-new service instance (as after restart) sees the full ordered log.
	restarted := eventstore.NewService(conn)
	events, err := restarted.List(ctx, eventstore.Filter{TaskID: "T"})
	if err != nil {
		t.Fatalf("list after restart: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("reconstructed timeline = %d events, want 3", len(events))
	}
	if events[0].Type != eventstore.TurnStarted || events[2].Type != eventstore.TurnCompleted {
		t.Fatalf("timeline order wrong: %v .. %v", events[0].Type, events[2].Type)
	}
}

func TestAfterSequenceForProjectionResume(t *testing.T) {
	svc := eventstore.NewService(dbtest.New(t))
	ctx := context.Background()

	for range 3 {
		mustAppend(t, svc, eventstore.Append{Type: eventstore.TurnStarted})
	}
	if err := svc.SaveCheckpoint(ctx, "dash", 2); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	cp, err := svc.LoadCheckpoint(ctx, "dash")
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if cp != 2 {
		t.Fatalf("checkpoint = %d, want 2", cp)
	}

	tail, err := svc.List(ctx, eventstore.Filter{AfterSequence: cp})
	if err != nil {
		t.Fatalf("list tail: %v", err)
	}
	if len(tail) != 1 || tail[0].Sequence != 3 {
		t.Fatalf("expected only sequence 3 after checkpoint, got %d events", len(tail))
	}
}

func TestPublishNotifiesSubscribers(t *testing.T) {
	svc := eventstore.NewService(dbtest.New(t))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sub := svc.Subscribe(ctx)

	var wg sync.WaitGroup
	wg.Add(1)
	var got eventstore.Event
	go func() {
		defer wg.Done()
		e := <-sub
		got = e.Payload
	}()

	if _, err := svc.Append(ctx, eventstore.Append{Type: eventstore.TurnStarted, TurnID: "x"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	wg.Wait()
	if got.Type != eventstore.TurnStarted {
		t.Fatalf("notification type = %q, want turn.started", got.Type)
	}
	// Notification must carry the assigned sequence (i.e. it happened post-commit).
	if got.Sequence == 0 {
		t.Fatalf("notification missing sequence, publish happened before commit")
	}
	_ = pubsub.CreatedEvent
}

func mustAppend(t *testing.T, svc eventstore.Service, a eventstore.Append) {
	t.Helper()
	if _, err := svc.Append(context.Background(), a); err != nil {
		t.Fatalf("append: %v", err)
	}
}

package message

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/db"
)

// countingQuerier records every durable UpdateMessage so tests can assert on
// write amplification and on the exact content that landed last.
type countingQuerier struct {
	db.Querier

	mu     sync.Mutex
	writes []db.UpdateMessageParams
	block  chan struct{} // when non-nil, each write waits on it
}

func (q *countingQuerier) UpdateMessage(_ context.Context, arg db.UpdateMessageParams) error {
	q.mu.Lock()
	block := q.block
	q.mu.Unlock()
	if block != nil {
		<-block
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	q.writes = append(q.writes, arg)
	return nil
}

func (q *countingQuerier) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.writes)
}

func (q *countingQuerier) last() (db.UpdateMessageParams, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.writes) == 0 {
		return db.UpdateMessageParams{}, false
	}
	return q.writes[len(q.writes)-1], true
}

func streamingMessage(id, text string) Message {
	return Message{
		ID:    id,
		Role:  Assistant,
		Parts: []ContentPart{TextContent{Text: text}},
	}
}

// The point of the whole change: a turn's worth of token deltas must not become
// a turn's worth of synchronous SQLite writes.
func TestStreamedUpdatesCollapseIntoOneWritePerWindow(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, 30*time.Millisecond)

	for i := 0; i < 200; i++ {
		if err := s.UpdateStreamed(t.Context(), streamingMessage("m1", strings.Repeat("x", i+1))); err != nil {
			t.Fatalf("UpdateStreamed: %v", err)
		}
	}
	if got := q.count(); got != 0 {
		t.Fatalf("no write should land before the flush window elapses, got %d", got)
	}

	if err := s.FlushStreamed(t.Context(), "m1"); err != nil {
		t.Fatalf("FlushStreamed: %v", err)
	}
	if got := q.count(); got != 1 {
		t.Fatalf("200 deltas in one window should produce exactly 1 write, got %d", got)
	}

	last, _ := q.last()
	if !strings.Contains(last.Parts, strings.Repeat("x", 200)) {
		t.Fatal("the coalesced write must carry the latest content, not an earlier delta")
	}
}

// Subscribers must still see every delta: coalescing is a storage optimization,
// and the user's transcript should render token-by-token exactly as before.
func TestStreamedUpdatesPublishEveryDelta(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, time.Hour) // never flush on its own during this test

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := s.Subscribe(ctx)

	const deltas = 20
	for i := 0; i < deltas; i++ {
		if err := s.UpdateStreamed(ctx, streamingMessage("m1", strings.Repeat("y", i+1))); err != nil {
			t.Fatalf("UpdateStreamed: %v", err)
		}
	}

	received := 0
	deadline := time.After(2 * time.Second)
	for received < deltas {
		select {
		case <-events:
			received++
		case <-deadline:
			t.Fatalf("only %d of %d deltas were published", received, deltas)
		}
	}
	if got := q.count(); got != 0 {
		t.Fatalf("publishing must not have triggered a durable write, got %d", got)
	}
}

// The ordering hazard this design exists to prevent. A delta buffered earlier in
// the window must never land after a later direct Update, or a finished message
// would silently revert to unfinished.
func TestDirectUpdateSupersedesPendingStreamedWrite(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, time.Hour)

	if err := s.UpdateStreamed(t.Context(), streamingMessage("m1", "partial")); err != nil {
		t.Fatalf("UpdateStreamed: %v", err)
	}

	final := streamingMessage("m1", "final")
	final.AddFinish(FinishReasonEndTurn)
	if err := s.Update(t.Context(), final); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// The pending write must have been dropped, not merely reordered.
	if err := s.FlushStreamed(t.Context(), "m1"); err != nil {
		t.Fatalf("FlushStreamed: %v", err)
	}

	if got := q.count(); got != 1 {
		t.Fatalf("expected exactly the direct write to land, got %d writes", got)
	}
	last, _ := q.last()
	if !strings.Contains(last.Parts, "final") {
		t.Fatalf("the durable state must be the finished message, got %q", last.Parts)
	}
	if !last.FinishedAt.Valid {
		t.Fatal("the finish marker must survive; a stale streamed write would have erased it")
	}
}

// Same hazard, but with the timer genuinely in flight and racing a direct
// Update. Run under -race, this is the test that would catch a lock that does
// not span the write itself.
func TestConcurrentFlushAndUpdateNeverLoseTheFinishMarker(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		q := &countingQuerier{}
		s := newService(q, time.Millisecond)

		if err := s.UpdateStreamed(t.Context(), streamingMessage("m1", "partial")); err != nil {
			t.Fatalf("UpdateStreamed: %v", err)
		}

		final := streamingMessage("m1", "final")
		final.AddFinish(FinishReasonEndTurn)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond)
			_ = s.Update(t.Context(), final)
		}()
		wg.Wait()

		// Give any in-flight timer the chance to land after the direct write.
		time.Sleep(10 * time.Millisecond)

		last, ok := q.last()
		if !ok {
			t.Fatalf("attempt %d: expected at least one write", attempt)
		}
		if !last.FinishedAt.Valid || !strings.Contains(last.Parts, "final") {
			t.Fatalf("attempt %d: a stale streamed write clobbered the finished message: %+v", attempt, last)
		}
	}
}

// A continuous stream must keep flushing on a cadence rather than deferring
// forever, so an interrupted session still has most of its transcript on disk.
func TestArmedTimerKeepsItsDeadlineAcrossDeltas(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, 20*time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 120; i++ {
			_ = s.UpdateStreamed(t.Context(), streamingMessage("m1", strings.Repeat("z", i+1)))
			time.Sleep(time.Millisecond)
		}
	}()
	<-done

	// ~120ms of continuous deltas at a 20ms window: several writes must have
	// landed already. If the deadline were pushed back by each delta, there
	// would be none.
	if got := q.count(); got < 2 {
		t.Fatalf("a continuous stream should flush periodically, got %d writes", got)
	}
	if got := q.count(); got > 20 {
		t.Fatalf("coalescing is not working: %d writes for 120 deltas", got)
	}
}

func TestFlushStreamedOnUnknownMessageIsANoop(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, time.Hour)

	if err := s.FlushStreamed(t.Context(), "never-seen"); err != nil {
		t.Fatalf("flushing an unknown message should be a no-op, got %v", err)
	}
	if got := q.count(); got != 0 {
		t.Fatalf("expected no writes, got %d", got)
	}
}

// Independent messages must not block each other or collapse into one another.
func TestStreamedWritesAreKeyedPerMessage(t *testing.T) {
	q := &countingQuerier{}
	s := newService(q, time.Hour)

	if err := s.UpdateStreamed(t.Context(), streamingMessage("a", "alpha")); err != nil {
		t.Fatalf("UpdateStreamed a: %v", err)
	}
	if err := s.UpdateStreamed(t.Context(), streamingMessage("b", "beta")); err != nil {
		t.Fatalf("UpdateStreamed b: %v", err)
	}

	if err := s.stream.flushAll(); err != nil {
		t.Fatalf("flushAll: %v", err)
	}
	if got := q.count(); got != 2 {
		t.Fatalf("expected one write per message, got %d", got)
	}
}

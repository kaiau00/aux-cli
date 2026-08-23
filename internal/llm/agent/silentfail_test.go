package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/aux-ai/aux-cli/internal/message"
)

// failingUpdates rejects every durable write, standing in for a database that
// has started refusing them.
type failingUpdates struct {
	message.Service
	calls int
}

func (f *failingUpdates) Update(context.Context, message.Message) error {
	f.calls++
	return errors.New("disk full")
}

// captureLogs redirects the global slog default for the duration of a test.
// logging wraps those globals, so this is the seam for asserting that a
// best-effort failure was actually reported somewhere.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// A finish marker that never lands leaves the message rendering as though it
// were still streaming -- permanently, and across restarts, because the stored
// row is what is wrong. The write cannot be retried into correctness from here,
// so the whole of the requirement is that it not vanish silently.
func TestFinishMessageReportsAFailedWrite(t *testing.T) {
	logs := captureLogs(t)
	msgs := &failingUpdates{}
	a := &agent{messages: msgs}

	m := message.Message{ID: "m-1", SessionID: "s-1", Role: message.Assistant}
	a.finishMessage(context.Background(), &m, message.FinishReasonCanceled)

	if msgs.calls != 1 {
		t.Fatalf("expected exactly one Update attempt, got %d", msgs.calls)
	}
	out := logs.String()
	if !strings.Contains(out, "finish marker") {
		t.Fatalf("the failure must say what could not be written; log was:\n%s", out)
	}
	if !strings.Contains(out, "m-1") {
		t.Fatalf("the failure must name the message so it can be found; log was:\n%s", out)
	}
}

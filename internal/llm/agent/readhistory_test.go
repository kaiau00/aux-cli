package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
)

// stubMessages is the slice of message.Service readHistory actually uses.
type stubMessages struct {
	message.Service
	msgs []message.Message
	err  error
}

func (s *stubMessages) List(context.Context, string) ([]message.Message, error) {
	return s.msgs, s.err
}

func viewResult(toolCallID, path string, isError bool) message.Message {
	meta, _ := json.Marshal(tools.ViewResponseMetadata{FilePath: path, Content: "x\ny\n"})
	return message.Message{
		Role: message.Tool,
		Parts: []message.ContentPart{message.ToolResult{
			ToolCallID: toolCallID,
			Metadata:   string(meta),
			IsError:    isError,
		}},
	}
}

func TestToolCallIDsForPathsMatchesRelativeAndAbsolute(t *testing.T) {
	h := newReadHistory(&stubMessages{msgs: []message.Message{
		viewResult("call-a", "/repo/internal/a.go", false),
		viewResult("call-b", "/repo/b.go", false),
	}}, "/repo")

	got, err := h.ToolCallIDsForPaths(t.Context(), "sess-1", []string{"internal/a.go", "/repo/b.go"})
	if err != nil {
		t.Fatalf("ToolCallIDsForPaths: %v", err)
	}
	if len(got["internal/a.go"]) != 1 || got["internal/a.go"][0] != "call-a" {
		t.Fatalf("relative path should resolve to its read, got %+v", got)
	}
	if len(got["/repo/b.go"]) != 1 || got["/repo/b.go"][0] != "call-b" {
		t.Fatalf("absolute path should resolve to its read, got %+v", got)
	}
}

// Re-reading a file produces several tool calls, and excluding the path has to
// drop all of them or the old copy stays in context.
func TestToolCallIDsForPathsCollectsEveryReadOfAFile(t *testing.T) {
	h := newReadHistory(&stubMessages{msgs: []message.Message{
		viewResult("call-1", "/repo/a.go", false),
		viewResult("call-2", "/repo/a.go", false),
		viewResult("call-1", "/repo/a.go", false), // duplicate id, must not repeat
	}}, "/repo")

	got, err := h.ToolCallIDsForPaths(t.Context(), "sess-1", []string{"a.go"})
	if err != nil {
		t.Fatalf("ToolCallIDsForPaths: %v", err)
	}
	if len(got["a.go"]) != 2 {
		t.Fatalf("expected both distinct reads and no duplicates, got %+v", got["a.go"])
	}
}

// A failed read put nothing in context, so there is nothing to exclude.
func TestToolCallIDsForPathsIgnoresFailedReads(t *testing.T) {
	h := newReadHistory(&stubMessages{msgs: []message.Message{
		viewResult("call-bad", "/repo/a.go", true),
	}}, "/repo")

	got, err := h.ToolCallIDsForPaths(t.Context(), "sess-1", []string{"a.go"})
	if err != nil {
		t.Fatalf("ToolCallIDsForPaths: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("failed reads must not be excludable, got %+v", got)
	}
}

// An unread path must be absent rather than present-and-empty, so the caller can
// tell the model it was never in context.
func TestToolCallIDsForPathsOmitsUnreadPaths(t *testing.T) {
	h := newReadHistory(&stubMessages{msgs: []message.Message{
		viewResult("call-a", "/repo/a.go", false),
	}}, "/repo")

	got, _ := h.ToolCallIDsForPaths(t.Context(), "sess-1", []string{"a.go", "never.go"})
	if _, present := got["never.go"]; present {
		t.Fatalf("an unread path should be absent from the map, got %+v", got)
	}
}

func TestToolCallIDsForPathsHandlesMissingInputs(t *testing.T) {
	h := newReadHistory(&stubMessages{}, "/repo")

	if got, err := h.ToolCallIDsForPaths(t.Context(), "", []string{"a.go"}); got != nil || err != nil {
		t.Fatal("no session means nothing to resolve")
	}
	if got, err := h.ToolCallIDsForPaths(t.Context(), "sess-1", nil); got != nil || err != nil {
		t.Fatal("no paths means nothing to resolve")
	}

	var nilHistory *readHistory
	if got, err := nilHistory.ToolCallIDsForPaths(t.Context(), "sess-1", []string{"a.go"}); got != nil || err != nil {
		t.Fatal("a nil readHistory must be a safe no-op")
	}
}

package dashboard

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/message"
)

func TestRedactedMessageHidesSensitiveToolContent(t *testing.T) {
	r := newRedactor(Options{Redaction: RedactionRedacted})
	dto := r.message(message.Message{
		ID:        "msg",
		SessionID: "sess",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "short visible response"},
			message.ReasoningContent{Thinking: "private chain"},
			message.ToolCall{ID: "tool", Name: "bash", Input: "cat ~/.ssh/id_rsa"},
			message.ToolResult{ToolCallID: "tool", Content: "secret output", Metadata: "secret metadata"},
		},
	})

	if dto.Text != "short visible response" {
		t.Fatalf("expected safe text snippet, got %q", dto.Text)
	}
	if dto.Reasoning != "[reasoning redacted]" {
		t.Fatalf("expected reasoning redaction, got %q", dto.Reasoning)
	}
	if dto.Tools[0].Input != "[tool input redacted]" {
		t.Fatalf("expected tool input redaction, got %q", dto.Tools[0].Input)
	}
	if dto.Results[0].Content != "[tool output redacted]" {
		t.Fatalf("expected tool output redaction, got %q", dto.Results[0].Content)
	}
}

func TestFullContentShowsSensitiveToolContent(t *testing.T) {
	r := newRedactor(Options{FullContent: true})
	dto := r.message(message.Message{
		ID:        "msg",
		SessionID: "sess",
		Role:      message.Assistant,
		Parts: []message.ContentPart{
			message.ToolCall{ID: "tool", Name: "bash", Input: "echo full"},
			message.ToolResult{ToolCallID: "tool", Content: "full output", Metadata: "full metadata"},
		},
	})

	if dto.Tools[0].Input != "echo full" {
		t.Fatalf("expected full tool input, got %q", dto.Tools[0].Input)
	}
	if dto.Results[0].Content != "full output" {
		t.Fatalf("expected full tool output, got %q", dto.Results[0].Content)
	}
}

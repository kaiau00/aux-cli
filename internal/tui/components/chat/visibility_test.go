package chat

import (
	"regexp"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/message"
)

var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// renderedText joins what renderAssistantMessage produced, stripped of styling,
// so assertions are about what reaches the screen rather than how it is painted.
func renderedText(t *testing.T, msg message.Message, all []message.Message, thinkingExpanded bool) string {
	t.Helper()
	// Rendering a tool call resolves its paths against the working directory,
	// which panics if no config has been loaded.
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var reasoning []message.Message
	if hasReasoningDetails(msg) {
		reasoning = []message.Message{msg}
	}
	parts := renderAssistantMessage(
		msg, 0, all, reasoning, nil, "", false, thinkingExpanded, "*", 100, 0,
	)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(ansiEscape.ReplaceAllString(p.content, ""))
		b.WriteString("\n")
	}
	return b.String()
}

// The agent's account of what it is doing has to reach the screen even when the
// same message calls a tool.
//
// Measured against this repository's own database before the fix: of 158
// assistant messages carrying prose longer than 40 characters, 94 -- 59% --
// also carried a tool call, and every one of those had its prose replaced by
// "reasoning hidden · ↓ Tab to expand".
func TestProseIsShownEvenWhenTheMessageCallsATool(t *testing.T) {
	const prose = "Checking the cost store first, since the meter reads from the ledger."
	msg := message.Message{
		ID:   "a1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: prose},
			message.ToolCall{ID: "t1", Name: "view", Input: `{"file_path":"store.go"}`, Finished: true},
			message.Finish{Reason: message.FinishReasonToolUse, Time: 10},
		},
	}
	got := renderedText(t, msg, []message.Message{msg}, false)

	if !strings.Contains(got, "Checking the cost store first") {
		t.Errorf("the prose did not reach the screen.\nGot:\n%s", got)
	}
	if strings.Contains(got, "reasoning hidden") {
		t.Errorf("prose was replaced by the reasoning placeholder.\nGot:\n%s", got)
	}
}

// A tool call is part of the transcript, not something to go looking for. Every
// tool call in a turn used to be reachable only by expanding the collapsed
// block, so a turn that ran eight tools showed the user none of them.
func TestToolCallsRenderInline(t *testing.T) {
	msg := message.Message{
		ID:   "a1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Reading two files."},
			message.ToolCall{ID: "t1", Name: "view", Input: `{"file_path":"alpha.go"}`, Finished: true},
			message.ToolCall{ID: "t2", Name: "view", Input: `{"file_path":"beta.go"}`, Finished: true},
		},
	}
	got := renderedText(t, msg, []message.Message{msg}, false)

	for _, want := range []string{"alpha.go", "beta.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("tool call for %s did not render inline.\nGot:\n%s", want, got)
		}
	}
}

// Model thinking stays behind Tab. That is the one thing the collapsed block is
// still for, and it is why the fix is not simply "show everything".
func TestThinkingStaysCollapsedUntilExpanded(t *testing.T) {
	const thought = "The ledger has a status column I should filter on."
	msg := message.Message{
		ID:   "a1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: thought},
			message.TextContent{Text: "Filtering by status."},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 10},
		},
	}

	collapsed := renderedText(t, msg, []message.Message{msg}, false)
	if strings.Contains(collapsed, thought) {
		t.Errorf("thinking should be hidden until Tab.\nGot:\n%s", collapsed)
	}
	if !strings.Contains(collapsed, "Filtering by status") {
		t.Errorf("the prose should still show alongside hidden thinking.\nGot:\n%s", collapsed)
	}

	expanded := renderedText(t, msg, []message.Message{msg}, true)
	if !strings.Contains(expanded, "ledger has a status column") {
		t.Errorf("Tab should reveal the thinking.\nGot:\n%s", expanded)
	}
}

// A message with neither thinking nor tool calls has nothing to collapse, so no
// Tab affordance should be advertised.
func TestPlainReplyOffersNoReasoningToggle(t *testing.T) {
	msg := message.Message{
		ID:   "a1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "Yes, that is the one."},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 10},
		},
	}
	if hasReasoningDetails(msg) {
		t.Error("a plain reply has no reasoning to collapse")
	}
	got := renderedText(t, msg, []message.Message{msg}, false)
	if strings.Contains(got, "Tab to") {
		t.Errorf("a plain reply should advertise no Tab affordance.\nGot:\n%s", got)
	}
}

// The transcript is canvas, not surface: it must not paint over the terminal's
// own background. Glamour emits its own background codes, so this is a live
// regression risk every time a render path is added.
func TestRenderedMessageEmitsNoBackgroundCodes(t *testing.T) {
	msg := message.Message{
		ID:   "a1",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: "A reply with `inline code`, a list:\n\n- one\n- two\n\nand a block:\n\n```go\nfunc main() {}\n```\n"},
			message.ToolCall{ID: "t1", Name: "view", Input: `{"file_path":"alpha.go"}`, Finished: true},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 10},
		},
	}
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	var reasoning []message.Message
	parts := renderAssistantMessage(msg, 0, []message.Message{msg}, reasoning, nil, "", false, false, "*", 100, 0)
	for i, p := range parts {
		if backgroundCode.MatchString(p.content) {
			t.Errorf("part %d painted a background:\n%s", i,
				strings.ReplaceAll(p.content, "\x1b", "ESC"))
		}
	}
}

// Any SGR run that sets a background, in every form glamour and lipgloss emit.
var backgroundCode = regexp.MustCompile(`\x1b\[[0-9;]*?(?:48;|\b(?:4[0-7]|49|10[0-7])\b)[0-9;]*m`)

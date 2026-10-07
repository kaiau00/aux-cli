package chat

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/message"
)

func assistant(parts ...message.ContentPart) message.Message {
	return message.Message{ID: "a1", Role: message.Assistant, Parts: parts}
}

// Every state the working footer reports has to be one it observed. Eleven
// verbs ("Working", "Searching", "Reading", ...) used to be cycled on a spinner
// tick, so the UI said "Searching" when nothing was being searched.
func TestWorkingLabelReportsObservedState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages []message.Message
		want     string
	}{
		{
			name:     "nothing recorded yet",
			messages: []message.Message{assistant()},
			want:     "Thinking...",
		},
		{
			name:     "text arriving, turn not finished",
			messages: []message.Message{assistant(message.TextContent{Text: "Looking at the ledger"})},
			want:     "Responding...",
		},
		{
			name: "a tool call still being built",
			messages: []message.Message{assistant(
				message.ToolCall{ID: "t1", Name: "view", Finished: false},
			)},
			want: "Calling View...",
		},
		{
			name: "a finished tool call with no result yet",
			messages: []message.Message{assistant(
				message.ToolCall{ID: "t1", Name: "view", Finished: true},
			)},
			want: "Waiting for tool response...",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &messagesCmp{messages: tc.messages}
			if got := m.workingStatusLabel(); got != tc.want {
				t.Errorf("workingStatusLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The regression that matters: the label must be a function of the messages and
// nothing else. The old implementation advanced an index on every spinner tick,
// so repeated calls with identical state returned different words.
func TestWorkingLabelDoesNotChangeOnItsOwn(t *testing.T) {
	m := &messagesCmp{messages: []message.Message{
		assistant(message.TextContent{Text: "Reading the cost store"}),
	}}
	first := m.workingStatusLabel()
	for i := 0; i < 50; i++ {
		if got := m.workingStatusLabel(); got != first {
			t.Fatalf("call %d returned %q, first call returned %q; the label is still time-driven",
				i, got, first)
		}
	}
}

// A finished assistant message is not being written, whatever text it holds.
func TestFinishedMessageIsNotReportedAsResponding(t *testing.T) {
	m := &messagesCmp{messages: []message.Message{assistant(
		message.TextContent{Text: "Done."},
		message.Finish{Reason: message.FinishReasonEndTurn, Time: 10},
	)}}
	if m.isWritingResponse() {
		t.Error("a finished message should not count as still writing")
	}
	if got := m.workingStatusLabel(); got != "Thinking..." {
		t.Errorf("workingStatusLabel() = %q, want %q", got, "Thinking...")
	}
}

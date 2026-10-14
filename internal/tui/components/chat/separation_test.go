package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/message"
)

// blankRowsBetween counts the blank rows that separate the first row holding
// want[i] from the one holding want[i+1]. It is what the user sees: rows on a
// screen, after the colour has been stripped.
func blankRowsBetween(t *testing.T, rendered, first, second string) int {
	t.Helper()
	rows := strings.Split(ansi.Strip(rendered), "\n")
	at := func(needle string) int {
		for i, row := range rows {
			if strings.Contains(row, needle) {
				return i
			}
		}
		return -1
	}
	a, b := at(first), at(second)
	if a < 0 || b < 0 {
		t.Fatalf("looked for %q and %q in\n%s", first, second, strings.Join(rows, "\n"))
	}
	if b <= a {
		t.Fatalf("%q is not after %q in\n%s", second, first, strings.Join(rows, "\n"))
	}
	blank := 0
	for _, row := range rows[a+1 : b] {
		if strings.TrimSpace(row) == "" {
			blank++
		}
	}
	return blank
}

func conversation(sessionID string) []message.Message {
	return []message.Message{
		{
			ID: "m0", Role: message.User, SessionID: sessionID,
			Parts: []message.ContentPart{message.TextContent{Text: "the first question"}},
		},
		{
			ID: "m1", Role: message.Assistant, SessionID: sessionID,
			Parts: []message.ContentPart{
				message.TextContent{Text: "the first answer"},
				message.Finish{Reason: message.FinishReasonEndTurn, Time: 1},
			},
		},
		{
			ID: "m2", Role: message.User, SessionID: sessionID,
			Parts: []message.ContentPart{message.TextContent{Text: "the second question"}},
		},
	}
}

// The gap exists at all.
//
// The tests below are written against messageGapRows, so on their own they
// would follow it down to zero -- which is the defect, not a configuration.
// Zero means one message runs straight into the next, which is what was
// reported.
func TestTheGapBetweenMessagesIsAtLeastOneRow(t *testing.T) {
	if messageGapRows < 1 {
		t.Fatalf("messageGapRows is %d, so messages are adjacent rows", messageGapRows)
	}
}

// One message must not run straight into the next.
//
// Measured at 100x30 before this: a question, its reply, the reply's footer
// and the next question were ten consecutive rows with a gutter on every one
// and nothing to show where either turn began. Reported as the separation
// between each chat being too compact.
func TestPrintedMessagesAreSeparatedFromEachOther(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(100, 10)
	m.session.ID = "s1"
	m.messages = conversation("s1")

	printed := m.takeScrollback()
	if printed == "" {
		t.Fatal("nothing was handed over")
	}
	if got := blankRowsBetween(t, printed, "first question", "first answer"); got != messageGapRows {
		t.Errorf("%d blank row(s) between a question and its reply, want %d:\n%s",
			got, messageGapRows, ansi.Strip(printed))
	}
	if got := blankRowsBetween(t, printed, "first answer", "second question"); got != messageGapRows {
		t.Errorf("%d blank row(s) between a reply and the next question, want %d:\n%s",
			got, messageGapRows, ansi.Strip(printed))
	}
}

// The separation has to survive the messages being printed in separate
// batches, which is the normal case: a question settles the moment it is sent
// and its reply only when the model has finished.
//
// This is where it was lost. The gap was written after each message and then
// stripped from the end of every print -- it has to be, or the live region
// starts a row further down than it has to -- so two messages printed
// separately had nothing at all between them.
func TestSeparatelyPrintedMessagesAreStillSeparated(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(100, 10)
	m.session.ID = "s1"

	all := conversation("s1")
	m.messages = all[:1] // the question, on its own
	question := m.takeScrollback()
	if question == "" {
		t.Fatal("the question was not handed over")
	}

	m.messages = all // the reply arrives
	answer := m.takeScrollback()
	if answer == "" {
		t.Fatal("the reply was not handed over")
	}

	// The terminal sees one print after the other, so that is what to check.
	together := question + "\n" + answer
	if got := blankRowsBetween(t, together, "first question", "first answer"); got != messageGapRows {
		t.Errorf("%d blank row(s) between a question and a reply printed separately, want %d:\n%s",
			got, messageGapRows, ansi.Strip(together))
	}
}

// A print may not end with blank rows. They would sit between the conversation
// and the composer, on top of the row the messages pane already reserves, and
// unlike the gap between messages they are never filled by anything.
func TestAPrintDoesNotEndWithBlankRows(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(100, 10)
	m.session.ID = "s1"
	m.messages = conversation("s1")

	rows := strings.Split(ansi.Strip(m.takeScrollback()), "\n")
	if len(rows) == 0 {
		t.Fatal("nothing was handed over")
	}
	if last := rows[len(rows)-1]; strings.TrimSpace(last) == "" {
		t.Errorf("the print ends with a blank row: %q", rows[len(rows)-3:])
	}
}

// What a message looks like must not change when it settles. It is rendered
// into the live region while it is in flight and printed when it is done, and
// the two paths lay its parts out separately -- so a message whose parts are
// spaced one way live and another way printed jumps as the turn ends.
func TestAMessageIsLaidOutTheSameWayPrintedAsLive(t *testing.T) {
	loadConfig(t)
	withTool := []message.Message{
		{
			ID: "m0", Role: message.User, SessionID: "s1",
			Parts: []message.ContentPart{message.TextContent{Text: "run the thing"}},
		},
		{
			ID: "m1", Role: message.Assistant, SessionID: "s1",
			Parts: []message.ContentPart{
				message.TextContent{Text: "reading the file now"},
				message.ToolCall{ID: "t1", Name: "view", Input: `{"file_path":"main.go"}`, Finished: true},
				message.ToolResult{ToolCallID: "t1", Name: "view", Content: "180 lines"},
				message.Finish{Reason: message.FinishReasonEndTurn, Time: 1},
			},
		},
	}

	live := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	live.SetSize(100, 20)
	live.session.ID = "s1"
	live.messages = withTool
	live.renderView()
	liveGap := blankRowsBetween(t, live.viewport.View(), "reading the file now", "main.go")

	printer := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	printer.SetSize(100, 20)
	printer.session.ID = "s1"
	printer.messages = withTool
	printedGap := blankRowsBetween(t, printer.takeScrollback(), "reading the file now", "main.go")

	if liveGap != printedGap {
		t.Errorf("a message's prose and its tool call are %d blank row(s) apart in the live region and %d apart printed, so the message moves when it settles",
			liveGap, printedGap)
	}
}

package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/message"
)

// rowsOf splits a rendered block into the rows a terminal would show, with the
// colour stripped: what the user actually sees.
func rowsOf(rendered string) []string {
	return strings.Split(ansi.Strip(rendered), "\n")
}

// indentOf is how many columns a row is inset from the left.
func indentOf(row string) int {
	return len(row) - len(strings.TrimLeft(row, " "))
}

// Who said it is readable from where it sits: the user's own messages against
// the right edge, everything Aux produces against the left. Asked for in those
// terms -- "your text is on the right side and the ai response and output is
// on the left".
func TestTheUsersMessagesSitOnTheRightAndAuxsOnTheLeft(t *testing.T) {
	loadConfig(t)
	for _, width := range []int{60, 80, 100, 160} {
		user := renderUserMessage(message.Message{
			ID: "u1", Role: message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "run the tests"}},
		}, false, width, 0).content

		rows := rowsOf(user)
		for i, row := range rows {
			if strings.TrimSpace(row) == "" {
				continue
			}
			if indentOf(row) == 0 {
				t.Errorf("width %d: the user's row %d starts at the left edge: %q",
					width, i+1, row)
			}
		}

		// Right edge: every row of the bubble ends in the same column, and
		// that column is at the right of the pane rather than in the middle.
		var ends []int
		for _, row := range rows {
			if trimmed := strings.TrimRight(row, " "); strings.TrimSpace(trimmed) != "" {
				ends = append(ends, len(trimmed))
			}
		}
		if len(ends) == 0 {
			t.Fatalf("width %d: the user's message rendered nothing: %q", width, user)
		}
		for i, end := range ends {
			if end != ends[0] {
				t.Errorf("width %d: the user's rows end in different columns (%d then %d), so the block is not flush right",
					width, ends[0], end)
				break
			}
			_ = i
		}
		if ends[0] < width-2 {
			t.Errorf("width %d: the user's message ends at column %d, which is not the right edge",
				width, ends[0])
		}
	}
}

// The assistant's output stays where it was: flush left, so the two never line
// up and the transcript reads as two columns.
func TestAuxsOwnOutputStaysFlushLeft(t *testing.T) {
	loadConfig(t)
	const width = 100
	rendered := renderMessage("here is what I found", false, false, width)
	for i, row := range rowsOf(rendered) {
		if strings.TrimSpace(row) == "" {
			continue
		}
		if got := indentOf(row); got != 0 {
			t.Errorf("row %d of Aux's own output is inset %d columns: %q", i+1, got, row)
		}
	}
}

// A short message hugs the right edge instead of sitting in a box the width of
// the pane: a full-width block aligned right looks exactly like one aligned
// left, which would make the change pointless.
func TestAShortMessageFromTheUserIsNarrow(t *testing.T) {
	loadConfig(t)
	const width = 100
	user := renderUserMessage(message.Message{
		ID: "u1", Role: message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hi"}},
	}, false, width, 0).content

	indent := width
	for _, row := range rowsOf(user) {
		if strings.TrimSpace(row) == "" {
			continue
		}
		indent = min(indent, indentOf(row))
	}
	if indent < width/2 {
		t.Errorf("a two-character message starts at column %d of %d; it should hug the right edge",
			indent, width)
	}
}

// Nothing may be wider than the pane it is rendered into -- and in particular
// nothing may be exactly the terminal's width, which is the one width Bubble
// Tea's renderer never clears. That is what left a stale copy of the composer
// on screen in #57, and right-aligning is done by padding on the left, which
// is exactly how a row grows to the full width by accident.
func TestNeitherSideOverflowsThePane(t *testing.T) {
	loadConfig(t)
	for _, width := range []int{40, 60, 80, 100, 160} {
		long := strings.Repeat("a long question that has to wrap somewhere ", 6)
		for _, rendered := range []string{
			renderUserMessage(message.Message{
				ID: "u1", Role: message.User,
				Parts: []message.ContentPart{message.TextContent{Text: long}},
			}, false, width, 0).content,
			renderMessage(long, false, false, width),
		} {
			for i, row := range rowsOf(rendered) {
				if w := lipgloss.Width(row); w > width {
					t.Errorf("width %d: row %d is %d cells, %d past the edge: %q",
						width, i+1, w, w-width, row)
					break
				}
			}
		}
	}
}

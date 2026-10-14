package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
)

// The composer stays on the terminal's last row, printed conversation or not.
//
// Reported from a real terminal: "the bar you type in... goes to the top once
// i send a message". The cause is in Bubble Tea's inline renderer. A frame
// shorter than the one before it is followed by an erase-below, so when the
// live region collapsed from owning the screen to sitting tight against its
// own content, every row under it was blanked and the composer was left
// wherever the shorter frame happened to end -- near the top of an otherwise
// empty screen.
//
// What keeps it on the bottom row is accounting: the rows Aux has handed to
// the terminal belong to the terminal, and the region takes exactly what is
// left. Printing four rows shrinks the region by four, so the frame ends on
// the same row it ended on before -- which is the last one.
func TestTheComposerStaysOnTheBottomRowAsTheConversationPrints(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {100, 30}, {160, 44}} {
		width, height := size[0], size[1]
		t.Run(strings.TrimSpace(itoa(width)+"x"+itoa(height)), func(t *testing.T) {
			m, _ := startupModel(t)
			m, cmd := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			printed := len(printedLines(cmd))
			if printed == 0 {
				t.Fatal("test setup: nothing was printed at startup")
			}

			// How tall the region is with nothing left of the screen to
			// pad to: its own content, and the floor every case below is
			// measured against.
			natural := collapsedRegionRows(t, width, height)

			// Each turn hands the terminal a few more rows. The counts are
			// arbitrary; what matters is that the sum stays the terminal's
			// height until the conversation has taken all of it.
			for _, rows := range []int{1, 4, 7, 12} {
				content := strings.TrimRight(strings.Repeat("a settled row\n", rows), "\n")
				var printCmd tea.Cmd
				m, printCmd = m.Update(chat.PrintScrollbackMsg{Content: content})

				got := printedLines(printCmd)
				if len(got) != rows {
					t.Fatalf("printing %d rows handed the terminal %d: %q", rows, len(got), got)
				}
				printed += len(got)

				live := strings.Split(m.View(), "\n")
				// What is printed and what is repainted account for the
				// screen exactly -- until the two together are taller than
				// it, at which point the region is as tall as its content and
				// the terminal scrolls the rest off the top, which leaves the
				// region on the bottom by itself.
				want := max(height, printed+natural)
				if total := printed + len(live); total != want {
					t.Errorf("%d printed + %d live = %d rows, want %d in a %d-row terminal: the composer is %d row(s) off the bottom",
						printed, len(live), total, want, height, want-total)
				}
				assertComposerIsLast(t, live)
			}
		})
	}
}

// Once the printed conversation has filled the screen the region is only as
// tall as its own content, and it is the terminal's scrolling that keeps it on
// the bottom row. Nothing may be padded under the composer at that point: a
// blank row below it is a row the composer is not on.
func TestNothingIsPaddedBelowTheComposerOnceTheScreenIsFull(t *testing.T) {
	const width, height = 100, 30
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m, _ = m.Update(chat.PrintScrollbackMsg{
		Content: strings.TrimRight(strings.Repeat("a settled row\n", height+5), "\n"),
	})

	live := strings.Split(m.View(), "\n")
	if len(live) >= height {
		t.Fatalf("the region is still %d rows after the screen filled; it should be as tall as its content", len(live))
	}
	assertComposerIsLast(t, live)
}

// collapsedRegionRows is how many rows the region renders when the printed
// conversation has taken the whole screen: its own content, with nothing to
// pad to.
func collapsedRegionRows(t *testing.T, width, height int) int {
	t.Helper()
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m, _ = m.Update(chat.PrintScrollbackMsg{
		Content: strings.TrimRight(strings.Repeat("a settled row\n", height*2), "\n"),
	})
	return lipgloss.Height(m.View())
}

// assertComposerIsLast checks the composer is in the last rows of the region.
// Below it belong only its own lower rule, the run information and the status
// line; anything more is a gap between the composer and the bottom row.
func assertComposerIsLast(t *testing.T, live []string) {
	t.Helper()
	composer := -1
	for i, line := range live {
		if strings.Contains(ansi.Strip(line), "Describe a task") {
			composer = i
		}
	}
	if composer < 0 {
		t.Fatalf("no composer in the region at all: %q", live)
	}
	if trailing := len(live) - 1 - composer; trailing > 4 {
		t.Errorf("composer on row %d of %d, %d rows above the bottom of the region",
			composer+1, len(live), trailing)
	}
}

// A printed row is the terminal's, and Aux cannot take it back -- so the count
// it keeps has to be the rows the terminal will actually use. lipgloss.Height
// is that count for a rendered block, and a trailing newline is a row.
func TestThePrintedRowCountIsWhatTheTerminalWillUse(t *testing.T) {
	const width, height = 100, 30
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	for _, content := range []string{"one", "one\ntwo", "one\ntwo\n", "\n"} {
		var cmd tea.Cmd
		m, cmd = m.Update(chat.PrintScrollbackMsg{Content: content})
		if got, want := len(printedLines(cmd)), lipgloss.Height(content); got != want {
			t.Errorf("%q: the terminal gets %d rows, the region was sized for %d", content, got, want)
		}
	}
}

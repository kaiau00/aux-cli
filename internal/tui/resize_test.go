package tui

import (
	"fmt"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// hasClearScreen reports whether a command tree asks the terminal to be
// cleared.
func hasClearScreen(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if hasClearScreen(c) {
				return true
			}
		}
		return false
	}
	return reflect.TypeOf(msg) != nil &&
		fmt.Sprintf("%T", msg) == "tea.clearScreenMsg"
}

// Narrowing the window re-wraps the rows the previous frame left on screen,
// and the inline renderer's cursor-up is computed from the line count it last
// rendered rather than the rows those lines now occupy. It lands short and
// leaves a stale copy of the composer behind.
//
// The renderer gives no way to correct that arithmetic, so Aux clears instead
// -- and only when the width actually shrinks. Widening cannot re-wrap
// anything, and clearing then would cost the user their conversation's place
// on screen for nothing.
func TestOnlyANarrowerTerminalClearsTheScreen(t *testing.T) {
	m, _ := startupModel(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	for _, tc := range []struct {
		name      string
		size      tea.WindowSizeMsg
		wantClear bool
	}{
		{"narrower", tea.WindowSizeMsg{Width: 70, Height: 30}, true},
		{"narrower again", tea.WindowSizeMsg{Width: 60, Height: 30}, true},
		{"wider", tea.WindowSizeMsg{Width: 120, Height: 30}, false},
		{"same width, shorter", tea.WindowSizeMsg{Width: 120, Height: 20}, false},
		{"same width, taller", tea.WindowSizeMsg{Width: 120, Height: 40}, false},
		{"identical", tea.WindowSizeMsg{Width: 120, Height: 40}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cmd tea.Cmd
			m, cmd = m.Update(tc.size)
			if got := hasClearScreen(cmd); got != tc.wantClear {
				t.Errorf("resizing to %dx%d cleared=%v, want %v",
					tc.size.Width, tc.size.Height, got, tc.wantClear)
			}
		})
	}
}

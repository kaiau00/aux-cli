package dialog

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The help overlay must fit the terminal it is shown in. Its width was
// hardcoded to 90 and ignored the window, so with its border and padding it
// came to 93 cells and overflowed every terminal under 93 columns -- 13 over at
// the classic 80, where the right-hand column of keys fell off the screen.
func TestHelpFitsEveryTerminalWidth(t *testing.T) {
	lipgloss.SetColorProfile(0)
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit the application")),
		key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "open the context drawer")),
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "show the model's reasoning")),
	}

	for _, w := range []int{40, 60, 70, 80, 90, 100, 120, 200} {
		h := NewHelpCmp()
		h.SetBindings(bindings)
		m, _ := h.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		view := m.(HelpCmp).View()
		for i, line := range strings.Split(ansi.Strip(view), "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("at terminal width %d, row %d is %d cells (%d over)", w, i, got, got-w)
				break
			}
		}
	}
}

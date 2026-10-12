package dialog

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
)

// The file picker has to fit the terminal it is opened in.
//
// It used to ignore the WindowSizeMsg entirely and hardcode f.width = 60,
// f.viewport.Width = 80, so it rendered about 101 cells wide and 29 rows tall
// whatever the terminal was. Found by opening it in a pty at 80x24: the
// preview pane's right border was off the screen and the path header had
// scrolled off the top.
func TestFilepickerFitsTheTerminal(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	for _, size := range [][2]int{{80, 22}, {100, 28}, {120, 34}, {160, 42}, {200, 48}} {
		width, height := size[0], size[1]
		f := NewFilepickerCmp(&app.App{})
		if f == nil {
			t.Skip("no home directory in this environment")
		}
		m, _ := f.Update(tea.WindowSizeMsg{Width: width, Height: height})
		picker, ok := m.(FilepickerCmp)
		if !ok {
			t.Fatalf("expected a FilepickerCmp, got %T", m)
		}

		plain := ansi.Strip(picker.View())
		lines := strings.Split(plain, "\n")
		for i, line := range lines {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("%dx%d: row %d is %d cells wide, %d over",
					width, height, i+1, w, w-width)
				break
			}
		}
		if len(lines) > height {
			t.Errorf("%dx%d: the picker is %d rows, %d more than the terminal reported",
				width, height, len(lines), len(lines)-height)
		}
	}
}

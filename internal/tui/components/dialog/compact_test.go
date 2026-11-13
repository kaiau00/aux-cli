package dialog

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestCompactControlsAndChoices(t *testing.T) {
	var calls int
	d := NewCompactDialog([]CompactPage{{Name: "large.go", ToolCallIDs: []string{"tool"}}}, func(p CompactPage, pin bool) error {
		calls++
		if p.ToolCallIDs[0] != "tool" {
			t.Fatal("lost tool identity")
		}
		return nil
	})
	for _, key := range []rune{'x', 'p', 'p'} {
		_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		d.Update(cmd())
	}
	if calls != 3 || !d.Pages[0].Excluded || d.Pages[0].Pinned {
		t.Fatalf("controls: %#v, calls %d", d.Pages, calls)
	}
	d.Apply = func(CompactPage, bool) error { return errors.New("write failed") }
	_, cmd := d.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	_, errCmd := d.Update(cmd())
	if errCmd == nil || d.Pages[0].Pinned {
		t.Fatal("failed write presented as success")
	}
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !cmd().(CompactChoiceMsg).Compact {
		t.Fatal("compact action")
	}
	d.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd = d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd().(CompactChoiceMsg).Compact {
		t.Fatal("continue action")
	}
}

func TestCompactDialogFitsAndActionsRemainReachable(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {40, 14}, {30, 10}} {
		d := NewCompactDialog([]CompactPage{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}}, nil)
		d.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for i := 0; i < 7; i++ {
			view := d.View()
			if i == 5 && !strings.Contains(view, "Compact now") {
				t.Fatal("compact action hidden")
			}
			if i == 6 && !strings.Contains(view, "Continue anyway") {
				t.Fatal("continue action hidden")
			}
			if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
				t.Fatalf("%v: %dx%d", size, lipgloss.Width(view), lipgloss.Height(view))
			}
			d.Update(tea.KeyMsg{Type: tea.KeyDown})
		}
	}
}

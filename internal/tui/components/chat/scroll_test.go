package chat

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/message"
)

// idleAgent is the smallest agent.Service the render path will accept: the
// messages pane asks only whether it is busy.
type idleAgent struct{ agent.Service }

func (idleAgent) IsBusy() bool              { return false }
func (idleAgent) IsSessionBusy(string) bool { return false }

// loadConfig gives the render path the global configuration it reads for the
// working directory and the configured model.
func loadConfig(t *testing.T) {
	t.Helper()
	if config.Get() != nil {
		return
	}
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("load config: %v", err)
	}
}

// Scrolling the wheel must move the conversation, not the terminal's own
// scrollback.
//
// Reported by a user: scrolling up in a conversation showed the shell commands
// from before Aux started, and the messages just above the visible area could
// not be reached at all. The program runs in the alternate screen but never
// asked the terminal for mouse reporting, so the wheel was never delivered here
// and the terminal kept it. The viewport has handled wheel events all along;
// nothing was forwarding them.
func TestWheelScrollsTheConversation(t *testing.T) {
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)

	// Content taller than the viewport, so there is somewhere to scroll to.
	m.viewport.SetContent(strings.TrimSpace(strings.Repeat("line\n", 200)))
	m.viewport.GotoBottom()

	atBottom := m.viewport.YOffset
	if atBottom == 0 {
		t.Fatal("test setup: the viewport is not scrollable")
	}

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	got := updated.(*messagesCmp).viewport.YOffset

	if got == atBottom {
		t.Fatalf("the wheel did not scroll the conversation (offset stayed at %d); "+
			"the terminal is still handling it", atBottom)
	}
	if got > atBottom {
		t.Fatalf("wheel up scrolled the wrong way: %d -> %d", atBottom, got)
	}
}

// And back down again, so the fix is not one-directional.
func TestWheelScrollsBackDown(t *testing.T) {
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.viewport.SetContent(strings.TrimSpace(strings.Repeat("line\n", 200)))
	m.viewport.GotoTop()

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	if got := updated.(*messagesCmp).viewport.YOffset; got == 0 {
		t.Fatal("wheel down did not scroll the conversation")
	}
}

// The messages pane must render exactly the height it was given, in both of its
// states. A pane that renders taller than its allocation is clipped by the
// terminal, and what goes missing is at the top -- the oldest content still on
// screen.
func TestMessagesPaneRendersItsExactHeight(t *testing.T) {
	loadConfig(t)
	withMessages := []message.Message{{
		ID:    "m1",
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello"}},
	}}

	for _, tc := range []struct {
		name     string
		messages []message.Message
	}{
		{"conversation", withMessages},
		{"empty session", nil},
	} {
		for _, height := range []int{10, 24, 40, 60} {
			m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
			m.SetSize(80, height)
			m.messages = tc.messages
			m.viewport.SetContent(strings.TrimSpace(strings.Repeat("line\n", 500)))

			got := lipgloss.Height(m.View())
			if got != height {
				t.Errorf("%s at height %d: pane rendered %d lines (%+d)", tc.name, height, got, got-height)
			}
		}
	}
}

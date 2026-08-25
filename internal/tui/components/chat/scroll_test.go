package chat

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
)

// longConversation seeds enough real messages that renderView produces more
// lines than the viewport can show at once -- without that, scroll offset
// stays clamped to zero regardless of what the code under test does, and a
// scroll-preservation test would pass for the wrong reason.
func longConversation(sessionID string, n int) []message.Message {
	body := strings.Repeat("This is a line of conversation text that wraps at a typical width.\n", 4) +
		"```go\nfunc example() {\n\tfmt.Println(\"hello\")\n}\n```\n"
	msgs := make([]message.Message, n)
	for i := range n {
		role := message.Assistant
		if i%2 == 0 {
			role = message.User
		}
		msgs[i] = message.Message{
			ID:        fmt.Sprintf("m%d", i),
			Role:      role,
			SessionID: sessionID,
			Parts:     []message.ContentPart{message.TextContent{Text: fmt.Sprintf("msg %d\n%s", i, body)}},
		}
	}
	return msgs
}

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

// A user scrolled up to read earlier history while a response is actively
// streaming must not be yanked back to the bottom by the next content delta.
// Streaming fires a pubsub update for the last message many times a second;
// this used to call GotoBottom unconditionally on every one of them.
func TestStreamingUpdateDoesNotStealScrollPosition(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 30)
	m.rerender()
	if m.viewport.AtBottom() {
		t.Fatal("test setup: conversation is not tall enough to scroll")
	}
	m.viewport.GotoTop()

	last := m.messages[len(m.messages)-1]
	last.Parts = []message.ContentPart{message.TextContent{Text: "growing streamed content"}}
	updated, cmd := m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	m = updated.(*messagesCmp)
	if cmd == nil {
		t.Fatal("expected a debounced re-render to be scheduled")
	}
	if got := m.viewport.YOffset; got != 0 {
		t.Fatalf("the streaming event rendered synchronously (offset moved to %d); it must be debounced", got)
	}

	// The debounce tick firing.
	fired := cmd()
	updated, _ = m.Update(fired)
	m = updated.(*messagesCmp)

	if m.viewport.AtBottom() {
		t.Fatal("a streaming update while scrolled up jumped back to the bottom")
	}
}

// The converse: a user who has not scrolled away should keep following a
// response as it streams in, the same "stick to bottom" behaviour chat UIs
// use everywhere else.
func TestStreamingUpdateFollowsWhenAlreadyAtBottom(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 30)
	m.rerender()
	m.viewport.GotoBottom()

	last := m.messages[len(m.messages)-1]
	last.Parts = []message.ContentPart{message.TextContent{Text: "growing streamed content"}}
	updated, cmd := m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	m = updated.(*messagesCmp)

	updated, _ = m.Update(cmd())
	m = updated.(*messagesCmp)

	if !m.viewport.AtBottom() {
		t.Fatal("a streaming update while already at the bottom should keep following it")
	}
}

// A real streaming response delivers a content delta many times a second.
// Each one used to force a full rejoin of the whole conversation -- measured
// at 200-700ms on a 400-message session -- which blocked the entire UI loop,
// including the user's own scroll input, on every single delta. Rapid
// updates must coalesce into one scheduled re-render, not stack one timer
// per delta.
func TestStreamingUpdatesCoalesceIntoOneRender(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 5)

	last := m.messages[len(m.messages)-1]

	last.Parts = []message.ContentPart{message.TextContent{Text: "a"}}
	updated, cmd1 := m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	m = updated.(*messagesCmp)
	if cmd1 == nil {
		t.Fatal("the first delta should schedule a re-render")
	}

	last.Parts = []message.ContentPart{message.TextContent{Text: "ab"}}
	updated, cmd2 := m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	m = updated.(*messagesCmp)
	if cmd2 != nil {
		t.Fatal("a second delta while one is already pending must not schedule another timer")
	}

	last.Parts = []message.ContentPart{message.TextContent{Text: "abc"}}
	updated, _ = m.Update(pubsub.Event[message.Message]{Type: pubsub.UpdatedEvent, Payload: last})
	m = updated.(*messagesCmp)

	// The single debounced render, once it fires, must reflect the latest
	// content -- coalescing must not drop the tail of what streamed in. The
	// changed message is last in the conversation, so it may be scrolled out
	// of the default view; look at the bottom, where it actually is.
	updated, _ = m.Update(cmd1())
	m = updated.(*messagesCmp)
	m.viewport.GotoBottom()
	if !strings.Contains(m.viewport.View(), "abc") {
		t.Fatal("the coalesced render did not reflect the latest streamed content")
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

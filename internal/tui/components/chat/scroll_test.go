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

// runCmd executes cmd and feeds every message it produces back into the model,
// unwrapping batches the way the Bubble Tea runtime does. Update now returns a
// batch -- the debounce timer alongside the lines being handed to the
// terminal's scrollback -- so a test that feeds the batch back as one message
// delivers nothing.
func runCmd(m *messagesCmp, cmd tea.Cmd) *messagesCmp {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = runCmd(m, c)
		}
		return m
	default:
		updated, next := m.Update(msg)
		return runCmd(updated.(*messagesCmp), next)
	}
}

// streamingTail appends the thing a real stream looks like: an assistant
// message with no finish part, which messageIsSettled refuses to hand to the
// terminal because more of it is still coming.
func streamingTail(msgs []message.Message, sessionID, text string) []message.Message {
	return append(msgs, message.Message{
		ID:        "streaming",
		Role:      message.Assistant,
		SessionID: sessionID,
		Parts:     []message.ContentPart{message.TextContent{Text: text}},
	})
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

// A message still streaming must not reach the terminal's scrollback. Once a
// line is printed it belongs to the terminal and cannot be taken back, so
// printing a half-finished reply would leave the partial text above the live
// region for good and print the finished version again underneath it.
//
// This replaces TestStreamingUpdateDoesNotStealScrollPosition. That test
// guarded a real bug -- streaming called GotoBottom on every delta and yanked
// a scrolled-up reader back down -- which inline rendering removes by
// construction: history is the terminal's scrollback, and Aux no longer has a
// scroll position to steal. The risk that replaces it is this one.
func TestStreamingMessageIsNotPrintedUntilItSettles(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = streamingTail(longConversation("s1", 4), "s1", "partial repl")

	// The settled history is handed over; the streaming tail is not.
	if lines := m.takeScrollback(); len(lines) == 0 {
		t.Fatal("the settled history should have been handed to the terminal")
	}
	if m.printed["streaming"] {
		t.Fatal("a message with no finish part was printed; a partial reply cannot be unprinted")
	}

	// It grows, and still is not printed.
	m.messages[len(m.messages)-1].Parts = []message.ContentPart{
		message.TextContent{Text: "partial reply, now longer"},
	}
	if lines := m.takeScrollback(); len(lines) != 0 {
		t.Fatalf("a growing message was handed over: %q", lines)
	}

	// Finishing it is what releases it.
	m.messages[len(m.messages)-1].Parts = append(
		m.messages[len(m.messages)-1].Parts,
		message.Finish{Reason: message.FinishReasonEndTurn, Time: 10},
	)
	lines := m.takeScrollback()
	if len(lines) == 0 {
		t.Fatal("a finished message was not handed over")
	}
	if !m.printed["streaming"] {
		t.Fatal("the finished message was not marked as printed, so it would print twice")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "now longer") {
		t.Fatalf("the printed text is not the final content: %q", lines)
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
	// The tail has to be an unfinished assistant message. A user message is
	// settled the moment it exists, so it would be handed to the terminal and
	// leave the live region -- which is not what streaming looks like.
	m.messages = streamingTail(longConversation("s1", 4), "s1", "")

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
	m = runCmd(m, cmd1)
	m.viewport.GotoBottom()
	if !strings.Contains(m.View(), "abc") {
		t.Fatal("the coalesced render did not reflect the latest streamed content")
	}
}

// The messages pane never renders taller than its allocation, and collapses to
// nothing when there is nothing in flight.
//
// The invariant used to be "exactly the height it was given". That was right
// for the alternate screen, where rendering short left stale rows behind. Now
// that settled messages go to the terminal's scrollback, filling the
// allocation means padding the gap between the conversation and the composer
// with blank rows -- nine of them on a 30-row terminal, which is what the
// first cut of the scrollback change actually shipped.
func TestMessagesPaneIsAsTallAsItsContentAndNoTaller(t *testing.T) {
	loadConfig(t)

	for _, height := range []int{10, 24, 40, 60} {
		// Nothing in flight: everything settled and went to scrollback.
		m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
		m.SetSize(80, height)
		m.messages = []message.Message{{
			ID:    "m1",
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: "hello"}},
		}}
		m.takeScrollback()
		m.renderView()
		if got := lipgloss.Height(m.View()); got > 1 {
			t.Errorf("idle at height %d: the pane rendered %d rows; it should collapse", height, got)
		}

		// Something in flight: the pane shows it, capped by the allocation.
		m2 := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
		m2.SetSize(80, height)
		m2.session.ID = "s1"
		m2.messages = streamingTail(longConversation("s1", 4), "s1",
			strings.TrimSpace(strings.Repeat("a line of streamed output\n", 200)))
		m2.takeScrollback()
		m2.renderView()
		got := lipgloss.Height(m2.View())
		if got > height {
			t.Errorf("streaming at height %d: the pane rendered %d rows, over its allocation", height, got)
		}
		if got == 0 {
			t.Errorf("streaming at height %d: the pane rendered nothing", height)
		}
	}
}

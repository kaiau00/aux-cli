package chat

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/message"
)

// Nothing may be printed twice. A printed line belongs to the terminal and
// cannot be withdrawn, so handing the same message over again would leave two
// copies in the user's scrollback with no way to tell which was current.
func TestEachMessageIsHandedOverExactlyOnce(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 6)

	first := m.takeScrollback()
	if len(first) == 0 {
		t.Fatal("the settled conversation was not handed over")
	}
	if again := m.takeScrollback(); len(again) != 0 {
		t.Fatalf("a second call handed the same messages over again: %q", again)
	}

	// A new message settles the one before it, and only that one is new.
	m.messages = append(m.messages, message.Message{
		ID: "m6", Role: message.User, SessionID: "s1",
		Parts: []message.ContentPart{message.TextContent{Text: "a brand new question"}},
	})
	third := m.takeScrollback()
	if len(third) == 0 {
		t.Fatal("the new message was not handed over")
	}
	joined := strings.Join(third, "\n")
	if !strings.Contains(joined, "brand new question") {
		t.Errorf("the new message is missing from what was handed over: %q", joined)
	}
	if strings.Count(joined, "brand new question") != 1 {
		t.Errorf("the new message was handed over more than once: %q", joined)
	}
}

// Opening a session prints its history, which is what --continue, --resume and
// the session picker hand back. Nothing from the previous session may leak into
// it, and nothing from it may be withheld because the previous session had
// already printed a message with the same position.
func TestOpeningASessionHandsOverItsOwnHistory(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)

	m.session.ID = "s1"
	m.messages = longConversation("s1", 4)
	if len(m.takeScrollback()) == 0 {
		t.Fatal("the first session was not handed over")
	}

	// A different session: same message ids, different content.
	m.printed = make(map[string]bool)
	m.session.ID = "s2"
	m.messages = []message.Message{{
		ID: "m0", Role: message.User, SessionID: "s2",
		Parts: []message.ContentPart{message.TextContent{Text: "the second session's question"}},
	}}
	lines := m.takeScrollback()
	if len(lines) == 0 {
		t.Fatal("the second session was not handed over; its ids collided with the first")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "second session") {
		t.Errorf("the wrong content was handed over: %q", lines)
	}
}

// What has been handed to the terminal must leave the region Aux repaints, or
// the user sees every message twice until something else triggers a render.
func TestHandedOverMessagesLeaveTheLiveRegion(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.SetSize(80, 10)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 4)

	m.rerender()
	if !strings.Contains(m.viewport.View(), "msg 0") {
		t.Fatal("test setup: the conversation is not in the live region to begin with")
	}

	m.takeScrollback()
	m.renderView()
	if strings.Contains(m.viewport.View(), "msg 0") {
		t.Error("a message handed to the terminal is still being repainted by Aux")
	}
}

// Width zero means the terminal has not reported itself yet. Rendering at that
// point would hand over lines wrapped to nothing.
func TestNothingIsHandedOverBeforeTheWidthIsKnown(t *testing.T) {
	loadConfig(t)
	m := NewMessagesCmp(&app.App{CoderAgent: idleAgent{}}).(*messagesCmp)
	m.session.ID = "s1"
	m.messages = longConversation("s1", 3)

	if lines := m.takeScrollback(); len(lines) != 0 {
		t.Errorf("handed over %d lines before the width was known", len(lines))
	}
	if m.printed["m0"] {
		t.Error("a message was marked printed before it could be rendered, so it will never print")
	}
}

package page

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
)

// The chat page must render exactly the number of rows it was given. One row
// too many and the alternate screen scrolls: the top line -- the task header,
// which carries the model and context budget -- is pushed out of view and
// cannot be scrolled back to. Reported as "you can't even see the stuff right
// above the top of the screen".
//
// The failure is width-dependent, because the hint lines are rendered with a
// fixed width and wrap instead of truncating, so this sweeps a grid rather
// than checking one size.
func TestChatPageFillsOnlyTheLiveRegion(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	lipgloss.SetColorProfile(0)

	conn := dbtest.New(t)
	q := db.New(conn)
	a := &app.App{
		Sessions:   session.NewService(q),
		Messages:   message.NewService(q),
		CoderAgent: &fakeAgent{},
	}

	widths := []int{30, 40, 50, 60, 70, 80, 90, 100, 120, 160, 200}
	heights := []int{12, 16, 20, 24, 30, 40, 50}

	for _, w := range widths {
		for _, h := range heights {
			m := NewChatPage(a, dialog.NewCommandRegistry())
			m.Init()
			m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			got := strings.Count(m.View(), "\n") + 1
			if got > h {
				t.Errorf("at %dx%d the chat page rendered %d rows, more than the terminal has",
					w, h, got)
			}
			if want := liveRegionHeight(h); got != want {
				t.Errorf("at %dx%d the chat page rendered %d rows, want the live region's %d",
					w, h, got, want)
			}
		}
	}
}

// The empty greeting is the easy case: nothing scrolls and no spinner is
// running. The same invariant has to hold once a conversation exists and the
// agent is mid-turn, which is when the working line and a long tool label are
// competing for the row the composer needs.
func TestChatPageFillsOnlyTheLiveRegionWithAConversation(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	lipgloss.SetColorProfile(0)

	conn := dbtest.New(t)
	q := db.New(conn)
	sessions, messages := session.NewService(q), message.NewService(q)
	busy := &fakeAgent{busy: true}
	a := &app.App{Sessions: sessions, Messages: messages, CoderAgent: busy}

	ctx := context.Background()
	sess, err := sessions.Create(ctx, "a long objective that will not fit in a narrow terminal at all")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 12; i++ {
		if _, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("wrap me ", 20)}},
		}); err != nil {
			t.Fatalf("create message: %v", err)
		}
	}

	for _, w := range []int{30, 40, 60, 80, 100, 120, 200} {
		for _, h := range []int{12, 16, 20, 24, 30, 40} {
			m := NewChatPage(a, dialog.NewCommandRegistry())
			m.Init()
			m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
			m, cmd := m.Update(chat.SessionSelectedMsg(sess))
			m, _ = drain(m, cmd)
			view := m.View()

			// Guard against measuring a screen that never finished rendering:
			// the assertion below is an absence, and an unfinished render would
			// satisfy it for the wrong reason.
			if strings.Contains(view, "Loading") {
				t.Fatalf("at %dx%d the transcript never finished rendering; the test is measuring the wrong screen", w, h)
			}
			// Settled history belongs to the terminal's scrollback now, so it
			// must not also be in the region Aux repaints -- otherwise every
			// message appears twice, once printed and once drawn.
			if strings.Contains(view, "wrap me") {
				t.Errorf("at %dx%d the live region still holds settled history", w, h)
			}
			got := strings.Count(view, "\n") + 1
			if got > h {
				t.Errorf("at %dx%d the chat page rendered %d rows, more than the terminal has", w, h, got)
			}
			if want := liveRegionHeight(h); got != want {
				t.Errorf("at %dx%d the chat page rendered %d rows, want the live region's %d", w, h, got, want)
			}
		}
	}
}

// Fitting the page into the terminal must not be paid for by clipping the
// composer's hint: on a 20-row terminal a purely proportional vertical split
// leaves the editor two rows, one short of border + input + hint, and the hint
// is what tells a first-time user how to send anything at all.
func TestComposerHintSurvivesOnShortTerminals(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	lipgloss.SetColorProfile(0)

	conn := dbtest.New(t)
	q := db.New(conn)
	a := &app.App{
		Sessions:   session.NewService(q),
		Messages:   message.NewService(q),
		CoderAgent: &fakeAgent{},
	}

	for _, h := range []int{12, 16, 20, 24, 40} {
		m := NewChatPage(a, dialog.NewCommandRegistry())
		m.Init()
		m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: h})
		if view := m.View(); !strings.Contains(view, "enter send") {
			t.Errorf("at 100x%d the composer hint is not on screen", h)
		}
	}
}

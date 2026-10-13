package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/muesli/termenv"
)

// No line Aux emits may be padded out to the terminal's width -- neither the
// rows it repaints nor the lines it prints into the scrollback.
//
// A full-width line is the one width that breaks the inline renderer. It only
// appends an erase-to-end-of-line when a line is *shorter* than the terminal,
// so a full-width one never clears what was beneath it; and it sits exactly on
// the wrap boundary, so one column narrower turns every such row into two
// while the renderer still counts one, its cursor-up lands a row short, and it
// paints over the wrong rows. What the user sees is a stale copy of the
// composer with the hint line gone -- reported from a real terminal, and
// dependent on the window's size, which is what pointed here.
//
// Swept across every width rather than a handful, because the defect only
// shows at the widths where a line happens to come out exactly full.
func TestNoLineIsPaddedToTheTerminalWidth(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := config.MarkProjectInitialized(); err != nil {
		t.Fatalf("MarkProjectInitialized: %v", err)
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ctx := context.Background()
	sess, err := sessions.Create(ctx, "a conversation")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, m := range []message.CreateMessageParams{
		{Role: message.User, Parts: []message.ContentPart{
			message.TextContent{Text: "hello how are you today"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "I am a reply long enough to wrap at any sensible width, which is the point of this fixture."},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 2}}},
	} {
		if _, err := messages.Create(ctx, sess.ID, m); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var pump func(tea.Model, tea.Cmd, int) tea.Model
	pump = func(m tea.Model, cmd tea.Cmd, depth int) tea.Model {
		if cmd == nil || depth > 8 {
			return m
		}
		done := make(chan tea.Msg, 1)
		go func() {
			defer func() { _ = recover() }()
			done <- cmd()
		}()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(300 * time.Millisecond):
			return m
		}
		switch msg := msg.(type) {
		case nil:
			return m
		case tea.BatchMsg:
			for _, c := range msg {
				m = pump(m, c, depth+1)
			}
			return m
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			return pump(m, next, depth+1)
		}
	}

	// One model, resized through every width, rather than a fresh one each
	// time: it is far quicker, and resizing is the path the defect was
	// reported on -- the user saw it depend on how big the window was.
	a := &app.App{
		Sessions: sessions, Messages: messages,
		CoderAgent: idleAgent{}, LSPClients: map[string]*lsp.Client{},
	}
	var m tea.Model = New(a)
	m = pump(m, m.Init(), 0)
	var cmd tea.Cmd
	m, cmd = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = pump(m, cmd, 0)
	m, cmd = m.Update(chat.SessionSelectedMsg(sess))
	for _, payload := range printedLines(cmd) {
		for _, line := range strings.Split(payload, "\n") {
			check(t, "printed", 100, line)
		}
	}
	m = pump(m, cmd, 0)

	for width := 40; width <= 200; width++ {
		m, cmd = m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		for _, payload := range printedLines(cmd) {
			for _, line := range strings.Split(payload, "\n") {
				check(t, "printed", width, line)
			}
		}
		m = pump(m, cmd, 0)
		for _, line := range strings.Split(m.View(), "\n") {
			check(t, "rendered", width, line)
		}
	}
}

func check(t *testing.T, what string, width int, line string) {
	t.Helper()
	plain := ansi.Strip(line)
	if got := ansi.StringWidth(line); got > width {
		t.Errorf("width %d: a %s line is %d cells, %d past the edge: %q",
			width, what, got, got-width, plain)
		return
	}
	if trimmed := strings.TrimRight(plain, " "); trimmed != plain {
		t.Errorf("width %d: a %s line is padded to %d cells with %d trailing space(s): %q",
			width, what, ansi.StringWidth(line), len(plain)-len(trimmed), plain)
	}
}

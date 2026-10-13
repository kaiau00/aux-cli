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
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
)

type workingAgent struct{ agent.Service }

func (workingAgent) IsBusy() bool              { return true }
func (workingAgent) IsSessionBusy(string) bool { return true }

// The live region must be the same height whether or not a reply is in flight.
//
// Bubble Tea's inline renderer repaints in place and cannot scroll the terminal
// back down, so a region that comes back one row shorter than the frame before
// it leaves that row's old content on screen. The working line was present
// mid-turn and gone either side of it, so every finished turn left a stale copy
// of the composer above the real one, with the hint line it had overwritten
// missing. Reported from a real terminal, where it is visible; a replay through
// a terminal emulator does not show it, which is why the invariant is asserted
// here rather than in the pty harness.
func TestTheLiveRegionDoesNotChangeHeightAcrossATurn(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := config.MarkProjectInitialized(); err != nil {
		t.Fatalf("MarkProjectInitialized: %v", err)
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ctx := context.Background()
	sess, err := sessions.Create(ctx, "a turn")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hello how are you"}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The conversation loads through a command, so the model has to be pumped
	// or m.messages stays empty -- and the working line is only drawn when
	// there is a conversation to be working on.
	var pump func(tea.Model, tea.Cmd, int) tea.Model
	pump = func(m tea.Model, cmd tea.Cmd, depth int) tea.Model {
		if cmd == nil || depth > 8 {
			return m
		}
		done := make(chan tea.Msg, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- nil
				}
			}()
			done <- cmd()
		}()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(400 * time.Millisecond):
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

	region := func(coder agent.Service, width, height int) (int, []string) {
		t.Helper()
		a := &app.App{
			Sessions: sessions, Messages: messages,
			CoderAgent: coder, LSPClients: map[string]*lsp.Client{},
		}
		var m tea.Model = New(a)
		m = pump(m, m.Init(), 0)
		var cmd tea.Cmd
		m, cmd = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
		m = pump(m, cmd, 0)
		m, cmd = m.Update(chat.SessionSelectedMsg(sess))
		m = pump(m, cmd, 0)
		view := m.View()
		return lipgloss.Height(view), strings.Split(ansi.Strip(view), "\n")
	}

	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 36}, {160, 44}} {
		width, height := size[0], size[1]
		idleRows, idleView := region(idleAgent{}, width, height)
		busyRows, busyView := region(workingAgent{}, width, height)
		if idleRows != busyRows {
			t.Errorf("%dx%d: the region is %d rows idle and %d rows working, so the terminal keeps %d stale row(s) after every turn\nidle: %q\nbusy: %q",
				width, height, idleRows, busyRows, busyRows-idleRows, idleView, busyView)
		}
	}
}

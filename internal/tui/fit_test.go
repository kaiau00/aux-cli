package tui

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
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
)

type idleAgent struct{}

func (idleAgent) Subscribe(context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	ch := make(chan pubsub.Event[agent.AgentEvent])
	close(ch)
	return ch
}
func (idleAgent) Model() models.Model { return models.Model{} }
func (idleAgent) Run(context.Context, string, string, ...message.Attachment) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (idleAgent) Cancel(string)             {}
func (idleAgent) IsSessionBusy(string) bool { return false }
func (idleAgent) IsBusy() bool              { return false }
func (idleAgent) Update(config.AgentName, models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (idleAgent) Summarize(context.Context, string) error { return nil }

// The whole app -- task header, page, status bar -- has to occupy exactly the
// rows the terminal reported. The alternate screen cannot scroll, so one row
// too many silently pushes the task header off the top, taking the model name
// and the context budget with it.
//
// The chat page has its own version of this test; this one guards the two rows
// the app reserves for the header and status bar around it, which is where a
// long session title or a long model name would otherwise wrap.
func TestAppRendersExactlyTheRowsTheTerminalReported(t *testing.T) {
	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	lipgloss.SetColorProfile(0)

	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	a := &app.App{
		Sessions:   sessions,
		Messages:   message.NewService(q),
		CoderAgent: idleAgent{},
		LSPClients: map[string]*lsp.Client{},
	}

	// A title long enough that the header must shorten rather than wrap.
	sess, err := sessions.Create(context.Background(),
		"refactor the entire persistence layer and migrate every store to the new additive schema")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	for _, withSession := range []bool{false, true} {
		for _, w := range []int{30, 40, 60, 80, 100, 120, 160, 200} {
			for _, h := range []int{14, 18, 22, 24, 30, 40, 50} {
				m := New(a)
				m.Init()
				m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				if withSession {
					m, _ = m.Update(chat.SessionSelectedMsg(sess))
				}
				got := strings.Count(m.View(), "\n") + 1
				if got != h {
					t.Errorf("session=%v at %dx%d the app rendered %d rows, want %d",
						withSession, w, h, got, h)
				}
			}
		}
	}
}

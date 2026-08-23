package page

import (
	"context"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/layout"
)

// fakeAgent is a minimal agent.Service that records Run calls instead of
// talking to a real provider, so the composer -> send pipeline can be
// exercised without network access.
type fakeAgent struct {
	mu       sync.Mutex
	runCount int
	lastText string
	lastSess string
	busy     bool
}

func (f *fakeAgent) Subscribe(context.Context) <-chan pubsub.Event[agent.AgentEvent] {
	ch := make(chan pubsub.Event[agent.AgentEvent])
	close(ch)
	return ch
}
func (f *fakeAgent) Model() models.Model { return models.Model{} }
func (f *fakeAgent) Run(_ context.Context, sessionID string, content string, _ ...message.Attachment) (<-chan agent.AgentEvent, error) {
	f.mu.Lock()
	f.runCount++
	f.lastText = content
	f.lastSess = sessionID
	f.mu.Unlock()
	ch := make(chan agent.AgentEvent)
	close(ch)
	return ch, nil
}
func (f *fakeAgent) Cancel(string)             {}
func (f *fakeAgent) IsSessionBusy(string) bool { return f.busy }
func (f *fakeAgent) IsBusy() bool              { return f.busy }
func (f *fakeAgent) Update(_ config.AgentName, _ models.ModelID) (models.Model, error) {
	return models.Model{}, nil
}
func (f *fakeAgent) Summarize(context.Context, string) error { return nil }

func (f *fakeAgent) calls() (int, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runCount, f.lastText, f.lastSess
}

// newTestChatPage builds a minimal chatPage without the heavier services
// NewChatPage's messages/editor components need, just enough to exercise the
// context-drawer overlay logic in View()/Update().
func newTestChatPage(t *testing.T) *chatPage {
	t.Helper()
	left := layout.NewContainer(chat.NewContextPaneCmp(&app.App{}), layout.WithPadding(0, 0, 0, 0))
	contextPane := chat.NewContextPaneCmp(&app.App{})
	contextPaneContainer := layout.NewContainer(contextPane, layout.WithPadding(1, 1, 1, 1))
	p := &chatPage{
		contextPane:          contextPane,
		contextPaneContainer: contextPaneContainer,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(left),
			layout.WithRightPanel(contextPaneContainer),
			layout.WithCollapseRightBelow(narrowWidthThreshold),
		),
	}
	p.SetSize(60, 24)
	p.width, p.height = 60, 24
	return p
}

// TestEnterSendsTypedComposerText reproduces a user report: typed text and
// the AI's response never appeared. It drives the exact same construction
// path production uses (NewChatPage) through a realistic key sequence -
// window size, typed runes, then Enter - and asserts the agent actually gets
// invoked with the composed text. This is a regression test for the send
// pipeline itself, independent of any specific rendering glitch.
func TestEnterSendsTypedComposerText(t *testing.T) {
	conn := dbtest.New(t)
	q := db.New(conn)
	fake := &fakeAgent{}
	a := &app.App{Sessions: session.NewService(q), Messages: message.NewService(q), CoderAgent: fake}

	m := NewChatPage(a)
	m.Init()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	for _, r := range "hello" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected Enter to produce a command (send the message), got nil")
	}
	// Drive whatever message(s) the command produces back through Update, the
	// same way the bubbletea runtime would, until nothing is left to process.
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			var next tea.Cmd
			for _, c := range batch {
				if c == nil {
					continue
				}
				sub := c()
				m, next = m.Update(sub)
				cmd = next
			}
			continue
		}
		m, cmd = m.Update(msg)
	}

	count, text, _ := fake.calls()
	if count != 1 {
		t.Fatalf("expected the agent to run exactly once after Enter, got %d calls", count)
	}
	if text != "hello" {
		t.Fatalf("expected the composed text %q to reach the agent, got %q", "hello", text)
	}
}

func TestContextDrawerTogglesViaKeybinding(t *testing.T) {
	p := newTestChatPage(t)
	before := p.View()

	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if !p.showContextDrawer {
		t.Fatal("expected ctrl+g to open the context drawer")
	}
	after := p.View()
	if after == before {
		t.Fatal("expected the drawer overlay to change the rendered view")
	}

	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if p.showContextDrawer {
		t.Fatal("expected a second ctrl+g to close the drawer")
	}
}

func TestEscClosesOpenContextDrawerBeforeAnythingElse(t *testing.T) {
	p := newTestChatPage(t)
	p.showContextDrawer = true

	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.showContextDrawer {
		t.Fatal("expected esc to close an open context drawer")
	}
}

func TestContextDrawerReachableWhenNarrow(t *testing.T) {
	p := newTestChatPage(t)
	p.SetSize(60, 24) // below narrowWidthThreshold: split layout drops the right panel
	p.width, p.height = 60, 24

	collapsed := p.View()
	if strings.TrimSpace(collapsed) == "" {
		t.Skip("collapsed layout rendered nothing to compare against")
	}

	p.showContextDrawer = true
	drawer := p.View()
	if drawer == collapsed {
		t.Fatal("expected the drawer to make the collapsed context panel visible again")
	}
}

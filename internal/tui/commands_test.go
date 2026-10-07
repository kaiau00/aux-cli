package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

func newCommandTestApp(t *testing.T) tea.Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	q := db.New(dbtest.New(t))
	return New(&app.App{
		Sessions:   session.NewService(q),
		Messages:   message.NewService(q),
		CoderAgent: idleAgent{},
		LSPClients: map[string]*lsp.Client{},
	})
}

// run delivers a RunCommandMsg and then the message its command produces.
func run(t *testing.T, m tea.Model, msg chat.RunCommandMsg) (tea.Model, tea.Msg) {
	t.Helper()
	m, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatalf("%+v produced no command", msg)
	}
	out := cmd()
	m, _ = m.Update(out)
	return m, out
}

func TestRunCommandExcludePassesThePathWithoutTheDialog(t *testing.T) {
	m := newCommandTestApp(t)
	m, out := run(t, m, chat.RunCommandMsg{ID: excludeCommandID, Args: "main.go"})
	if out != (chat.ExcludePathMsg{Path: "main.go"}) {
		t.Fatalf("got %#v, want ExcludePathMsg for main.go", out)
	}
	if m.(appModel).showMultiArgumentsDialog {
		t.Fatal("the arguments dialog opened even though the path was typed")
	}
}

func TestRunCommandWithoutArgsOpensTheArgumentsDialog(t *testing.T) {
	m := newCommandTestApp(t)
	m, _ = run(t, m, chat.RunCommandMsg{ID: excludeCommandID})
	if !m.(appModel).showMultiArgumentsDialog {
		t.Fatal("/exclude with no path did not ask for one")
	}
}

func TestRunCommandRejectsArgsForCommandsThatTakeNone(t *testing.T) {
	m := newCommandTestApp(t)
	_, out := run(t, m, chat.RunCommandMsg{ID: "init", Args: "now"})
	if info, ok := out.(util.InfoMsg); !ok || info.Type != util.InfoTypeWarn || info.Msg != "/init takes no arguments" {
		t.Fatalf("got %#v", out)
	}
}

func TestHelpAndModelCommandsOpenTheirOverlays(t *testing.T) {
	m := newCommandTestApp(t)
	m, _ = run(t, m, chat.RunCommandMsg{ID: "help"})
	if !m.(appModel).showHelp {
		t.Fatal("/help did not open the help overlay")
	}
	m, _ = run(t, m, chat.RunCommandMsg{ID: "model"})
	if !m.(appModel).showModelDialog {
		t.Fatal("/model did not open the model dialog")
	}
}

func TestSessionsCommandReportsWhenThereAreNone(t *testing.T) {
	m := newCommandTestApp(t)
	m, cmd := m.Update(chat.RunCommandMsg{ID: "sessions"})
	m, cmd = m.Update(cmd())
	if cmd == nil {
		t.Fatal("/sessions with no sessions said nothing")
	}
	if info, ok := cmd().(util.InfoMsg); !ok || info.Msg != "No sessions available" {
		t.Fatalf("got %#v", info)
	}
	if m.(appModel).showSessionDialog {
		t.Fatal("an empty session dialog opened")
	}
}

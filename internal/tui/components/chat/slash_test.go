package chat

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

func newSlashTestEditor(t *testing.T) *editorCmp {
	t.Helper()
	commands := dialog.NewCommandRegistry()
	for _, id := range []string{"init", "compact", "exclude", "user:review"} {
		commands.Register(dialog.Command{ID: id})
	}
	// No app: a command must be dispatched before the editor asks whether the
	// agent is busy, so a nil app panics if the input leaks to the send path.
	return NewEditorCmp(nil, commands).(*editorCmp)
}

func sendText(t *testing.T, e *editorCmp, text string) tea.Msg {
	t.Helper()
	e.textarea.SetValue(text)
	cmd := e.send()
	if cmd == nil {
		t.Fatalf("send(%q) returned no command", text)
	}
	return cmd()
}

func TestSlashInitRunsTheCommandAndIsNotSent(t *testing.T) {
	e := newSlashTestEditor(t)
	msg := sendText(t, e, "/init")
	if _, sent := msg.(SendMsg); sent {
		t.Fatal("/init was sent to the model")
	}
	run, ok := msg.(RunCommandMsg)
	if !ok {
		t.Fatalf("got %T, want RunCommandMsg", msg)
	}
	if run != (RunCommandMsg{ID: "init"}) {
		t.Fatalf("got %+v", run)
	}
	if e.textarea.Value() != "" {
		t.Fatalf("composer kept %q after running the command", e.textarea.Value())
	}
}

func TestSlashExcludeCarriesItsArgument(t *testing.T) {
	e := newSlashTestEditor(t)
	msg := sendText(t, e, "/exclude main.go")
	if run, ok := msg.(RunCommandMsg); !ok || run != (RunCommandMsg{ID: "exclude", Args: "main.go"}) {
		t.Fatalf("got %#v, want exclude with main.go", msg)
	}
}

func TestSlashCustomCommandRuns(t *testing.T) {
	e := newSlashTestEditor(t)
	msg := sendText(t, e, "/user:review")
	if run, ok := msg.(RunCommandMsg); !ok || run.ID != "user:review" {
		t.Fatalf("got %#v, want user:review", msg)
	}
}

func TestUnknownSlashCommandWarnsAndKeepsTheText(t *testing.T) {
	e := newSlashTestEditor(t)
	msg := sendText(t, e, "/unknown")
	info, ok := msg.(util.InfoMsg)
	if !ok {
		t.Fatalf("got %T, want a status warning", msg)
	}
	if info.Type != util.InfoTypeWarn || info.Msg != "unknown command /unknown; Ctrl+K lists commands" {
		t.Fatalf("got %+v", info)
	}
	if e.textarea.Value() != "/unknown" {
		t.Fatalf("composer text was %q, want it kept for editing", e.textarea.Value())
	}
}

func TestSlashInsideTextIsNotACommand(t *testing.T) {
	e := newSlashTestEditor(t)
	for _, text := range []string{"see /etc/hosts", "/etc/hosts is broken", "fix /init docs"} {
		if _, isCommand, unknown := e.slashCommand(text); isCommand || unknown {
			t.Errorf("%q: isCommand=%v unknown=%v, want ordinary text", text, isCommand, unknown)
		}
	}
}

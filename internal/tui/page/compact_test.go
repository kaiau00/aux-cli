package page

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

func TestCompactionPressureSequence(t *testing.T) {
	p := newTestChatPage(t)
	p.editor = p.contextPaneContainer
	p.session = session.Session{ID: "session"}
	drive := func(used int64) tea.Cmd {
		t.Helper()
		_, cmd := p.Update(ContextPressureMsg{SessionID: "session", Used: used, Limit: 100, Auto: true})
		return cmd
	}
	if drive(79) != nil {
		t.Fatal("warned before 80%")
	}
	cmd := drive(80)
	if cmd == nil {
		t.Fatal("missing 80% lookup")
	}
	snapshot := cmd().(compactionPagesMsg)
	snapshot.Pages = []dialog.CompactPage{{Name: "largest.go", Tokens: 700}}
	_, cmd = p.Update(snapshot)
	warning, ok := cmd().(util.InfoMsg)
	if !ok || warning.Type != util.InfoTypeWarn || !strings.Contains(warning.Msg, "largest.go") {
		t.Fatalf("warning: %#v", warning)
	}
	if drive(85) != nil {
		t.Fatal("repeated warning")
	}
	cmd = drive(90)
	if cmd == nil {
		t.Fatal("missing 90% lookup")
	}
	p.Update(cmd())
	if p.compactDialog == nil || !p.FillsScreen() {
		t.Fatal("dialog was not opened")
	}
	if drive(95) != nil {
		t.Fatal("compacted while decision still open")
	}
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_, cmd = p.Update(cmd())
	if _, ok := cmd().(CompactSessionMsg); !ok {
		t.Fatal("95% safety net did not use existing compact flow")
	}
	// Selection away and back preserves the once-per-session decision.
	p.Update(chat.SessionSelectedMsg(session.Session{ID: "other"}))
	p.Update(chat.SessionSelectedMsg(session.Session{ID: "session"}))
	cmd = drive(95)
	if _, ok := cmd().(CompactSessionMsg); !ok {
		t.Fatal("forgot the session decision")
	}
}

func TestCompactionJumpMustShowDialogFirst(t *testing.T) {
	for _, auto := range []bool{false, true} {
		p := newTestChatPage(t)
		p.editor = p.contextPaneContainer
		p.session = session.Session{ID: "session"}
		msg := ContextPressureMsg{SessionID: "session", Used: 99, Limit: 100, Auto: auto}
		_, cmd := p.Update(msg)
		if _, ok := cmd().(compactionPagesMsg); !ok {
			t.Fatal("jump bypassed dialog")
		}
		p.Update(cmd())
		if p.compactDialog == nil {
			t.Fatal("jump did not show decision")
		}
		_, cmd = p.Update(dialog.CompactChoiceMsg{})
		if auto && cmd == nil {
			t.Fatal("missing safety net after dialog")
		}
		if !auto && cmd != nil {
			t.Fatal("auto-compacted with AutoCompact off")
		}
	}
}

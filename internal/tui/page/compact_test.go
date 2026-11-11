package page

import (
	"errors"
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

func TestCompactNowThroughPageWithEmptyPages(t *testing.T) {
	p := newTestChatPage(t)
	p.editor = p.contextPaneContainer
	p.session = session.Session{ID: "session"}
	if p.contextPressure(ContextPressureMsg{SessionID: "session", Used: 99, Limit: 0, Auto: true}) != nil {
		t.Fatal("zero limit triggered pressure")
	}
	cmd := p.contextPressure(ContextPressureMsg{SessionID: "session", Used: 90, Limit: 100})
	if p.contextPressure(ContextPressureMsg{SessionID: "session", Used: 91, Limit: 100}) != nil {
		t.Fatal("duplicate lookup")
	}
	result := cmd().(compactionPagesMsg)
	result.Err = errors.New("lookup failed")
	p.Update(result)
	if p.compactDialog == nil {
		t.Fatal("failed lookup disabled dialog")
	}
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd = p.Update(cmd())
	if _, ok := cmd().(CompactSessionMsg); !ok {
		t.Fatal("compact action did not reach page")
	}
}

func TestSafetyNetBeforeRepeatedWarning(t *testing.T) {
	p := newTestChatPage(t)
	p.session = session.Session{ID: "session"}
	p.pressure = map[string]*compactionPressure{"session": {shown: true, warned: true}}
	p.contextPressure(ContextPressureMsg{SessionID: "session", Used: 50, Limit: 100, Auto: true})
	cmd := p.contextPressure(ContextPressureMsg{SessionID: "session", Used: 99, Limit: 100, Auto: true})
	if _, ok := cmd().(CompactSessionMsg); !ok {
		t.Fatal("warning bypassed safety net")
	}
}

func TestCompactDialogResizeKeepsActionsVisible(t *testing.T) {
	p := newTestChatPage(t)
	p.editor = p.contextPaneContainer
	p.session = session.Session{ID: "session"}
	p.pressure = map[string]*compactionPressure{"session": {used: 99, limit: 100, auto: true}}
	p.Update(compactionPagesMsg{SessionID: "session", Open: true, Pages: []dialog.CompactPage{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}}})
	p.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	view := p.compactDialog.View()
	if !strings.Contains(view, "Compact now (transcript)") || !strings.Contains(view, "Continue (auto-compact)") {
		t.Fatalf("actions hidden or misleading after resize: %s", view)
	}
}

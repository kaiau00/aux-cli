package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
)

// No dialog may render past the terminal it was opened in -- not one column,
// not one row. A dialog wider than the terminal loses its right border and
// whatever content sat beyond the edge; one taller than the terminal loses its
// title off the top and its buttons off the bottom, and a prompt whose buttons
// are off the screen cannot be answered at all.
//
// This covers every overlay the model can show, at sizes down to 40x14. The
// old audit grid started at 70x20, and that is where a whole family of these
// was living: the file picker ignored its WindowSizeMsg and rendered 134 cells
// wide; the model and theme pickers were never told the terminal's size at
// all; the init dialog clamped its width only after laying its text out at the
// unclamped value; the session picker's clamp sat inside a max() that put the
// floor back over the edge.
func TestNoDialogRendersPastTheTerminal(t *testing.T) {
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
	sess, err := sessions.Create(context.Background(), "dialog fit")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	// needsRoom is the narrowest terminal in which a dialog is expected to
	// render for real rather than fall back to the notice. Most are compact
	// enough for 44 columns; the file picker is a two-pane browser and the
	// init dialog is four paragraphs of explanation, and in a 44-column
	// window the notice is the honest answer for both.
	states := []struct {
		name      string
		needsRoom int
		set       func(*appModel)
	}{
		{"help", 44, func(a *appModel) { a.showHelp = true }},
		{"quit", 44, func(a *appModel) { a.showQuit = true }},
		{"model picker", 44, func(a *appModel) { a.showModelDialog = true }},
		{"theme picker", 44, func(a *appModel) { a.showThemeDialog = true }},
		{"session picker", 44, func(a *appModel) { a.showSessionDialog = true }},
		{"command palette", 44, func(a *appModel) { a.showCommandDialog = true }},
		{"file picker", 60, func(a *appModel) { a.showFilepicker = true }},
		{"init dialog", 60, func(a *appModel) { a.showInitDialog = true }},
		{"multi-argument prompt", 44, func(a *appModel) { a.showMultiArgumentsDialog = true }},
		{"compacting", 44, func(a *appModel) { a.isCompacting = true }},
		{"permission prompt", 44, func(a *appModel) {
			a.permissions.SetPermissions(permission.PermissionRequest{
				ID: "p1", SessionID: sess.ID, ToolName: "view", Action: "read",
				Description: "Read internal/cost/store.go",
				Params:      map[string]any{"file_path": "internal/cost/store.go"},
			})
			a.showPermissions = true
		}},
	}

	// 44x22 is deliberate: the model and theme pickers had a fixed 47-cell
	// width, so that is the band where being unsized overflows while the
	// terminal still has ample height.
	for _, size := range [][2]int{{40, 14}, {44, 22}, {60, 18}, {80, 24}, {100, 30}, {160, 44}} {
		width, height := size[0], size[1]
		for _, st := range states {
			a := &app.App{
				Sessions: sessions, Messages: messages,
				CoderAgent: idleAgent{}, LSPClients: map[string]*lsp.Client{},
			}
			var m tea.Model = New(a)
			m.Init()
			m, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: height})
			m, _ = m.Update(chat.SessionSelectedMsg(sess))
			am, ok := m.(appModel)
			if !ok {
				t.Fatalf("expected an appModel, got %T", m)
			}
			st.set(&am)

			plain := ansi.Strip(am.View())
			// At 44x22 and above every dialog has to actually render. The
			// too-small notice is there for terminals that cannot hold one,
			// not a licence to stop fitting: without the fixes below this
			// line, the model picker, the theme picker and the file picker
			// fell back to the notice in terminals with ample room, because
			// they were never told the terminal's size. Measured: without
			// that, the model picker falls back to the notice at every width
			// from 44 to 47, because its unsized default is 47 cells.
			if width >= st.needsRoom && height >= 22 && strings.Contains(plain, "Terminal too small") {
				t.Errorf("%s at %dx%d: fell back to the too-small notice in a terminal with room for it",
					st.name, width, height)
			}

			lines := strings.Split(plain, "\n")
			for i, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("%s at %dx%d: row %d is %d cells wide, %d past the edge",
						st.name, width, height, i+1, w, w-width)
					break
				}
			}
			// The region plus the printed banner is the whole terminal, so
			// more rows than that means the dialog pushed past the bottom.
			if len(lines)+am.bannerRows > height {
				t.Errorf("%s at %dx%d: %d rows with %d printed, %d past the bottom",
					st.name, width, height, len(lines), am.bannerRows,
					len(lines)+am.bannerRows-height)
			}
		}
	}
}

// When the terminal genuinely cannot hold a dialog, Aux says so rather than
// drawing a clipped one. A clipped dialog reads as a rendering fault, and a
// clipped prompt cannot be answered.
func TestATerminalTooSmallForADialogSaysSo(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	// A region far too small for the dialog it is asked to hold.
	region := lipgloss.NewStyle().Width(40).Height(10).Render("")
	oversized := lipgloss.NewStyle().Width(70).Height(30).Render("much too big")

	got := ansi.Strip(centreOverlay(region, oversized))
	if !strings.Contains(got, "Terminal too small") {
		t.Errorf("no notice in place of the oversized dialog: %q", got)
	}
	if strings.Contains(got, "much too big") {
		t.Error("the oversized dialog was placed anyway")
	}
	for i, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("the notice itself overflows: row %d is %d cells", i+1, w)
		}
	}
	if rows := len(strings.Split(got, "\n")); rows > 10 {
		t.Errorf("the notice itself is %d rows of a 10-row region", rows)
	}
}

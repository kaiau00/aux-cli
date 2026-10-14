package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
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
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/muesli/termenv"
)

type auditBusyAgent struct{ idleAgent }

func (auditBusyAgent) IsSessionBusy(string) bool { return true }
func (auditBusyAgent) IsBusy() bool              { return true }

// TestFullSurfaceAudit renders every state at every size in both light and
// dark and checks the invariants that matter, collecting findings rather than
// failing on the first one, so a single run enumerates everything wrong at
// once.
//
// Opt-in: it is ~130 renders and takes around three and a half minutes, which
// is too slow for every push. Run it after any change to layout, sizing or
// overlays:
//
//	AUX_AUDIT=1 AUX_UNICODE_ICONS=1 go test -run TestFullSurfaceAudit -v ./internal/tui/
//
// It is what found the help overlay overflowing every terminal under 93
// columns, and the live region padding itself out to a fixed allocation.
func TestFullSurfaceAudit(t *testing.T) {
	if os.Getenv("AUX_AUDIT") == "" {
		t.Skip("set AUX_AUDIT=1 to run the full-surface audit (slow)")
	}

	dir := t.TempDir()
	if _, err := config.Load(dir, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if err := config.MarkProjectInitialized(); err != nil {
		t.Fatalf("MarkProjectInitialized: %v", err)
	}
	lipgloss.SetColorProfile(termenv.TrueColor)

	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ctx := context.Background()

	sess, err := sessions.Create(ctx, "audit the whole surface")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	seed := []struct {
		role  message.MessageRole
		parts []message.ContentPart
	}{
		{message.User, []message.ContentPart{message.TextContent{Text: "why is the meter high?"}}},
		{message.Assistant, []message.ContentPart{
			message.TextContent{Text: "It summed **lifetime spend**.\n\n```go\nfunc x() {}\n```\n\n- one\n- two\n"},
			message.ToolCall{ID: "t1", Name: "view", Input: `{"file_path":"internal/cost/store.go"}`, Finished: true},
			message.ToolResult{ToolCallID: "t1", Name: "view", Content: "180 lines"},
			message.Finish{Reason: message.FinishReasonToolUse, Time: 1},
		}},
		{message.Assistant, []message.ContentPart{
			message.TextContent{Text: "Fixed; it reads the latest call's occupancy now."},
			message.Finish{Reason: message.FinishReasonEndTurn, Time: 2},
		}},
	}
	for _, m := range seed {
		if _, err := messages.Create(ctx, sess.ID, message.CreateMessageParams{Role: m.role, Parts: m.parts}); err != nil {
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

	type state struct {
		name    string
		busy    bool
		open    bool
		overlay func(*appModel)
	}
	states := []state{
		{name: "splash"},
		{name: "idle-conversation", open: true},
		{name: "working", open: true, busy: true},
		{name: "drawer", open: true},
		{name: "permission", open: true, overlay: func(a *appModel) {
			// With a real request in it, the way a user ever sees it. Setting
			// only the flag left the dialog with no permission and so no size.
			a.permissions.SetPermissions(permission.PermissionRequest{
				ID: "p1", SessionID: "audit", ToolName: "view", Action: "read",
				Description: "Read internal/cost/store.go",
				Params:      map[string]any{"file_path": "internal/cost/store.go"},
			})
			a.showPermissions = true
		}},
		{name: "help", open: true, overlay: func(a *appModel) { a.showHelp = true }},
		{name: "model-picker", open: true, overlay: func(a *appModel) { a.showModelDialog = true }},
		{name: "session-picker", open: true, overlay: func(a *appModel) { a.showSessionDialog = true }},
		{name: "commands", open: true, overlay: func(a *appModel) { a.showCommandDialog = true }},
		{name: "compacting", open: true, overlay: func(a *appModel) { a.isCompacting = true }},
		{name: "quit", open: true, overlay: func(a *appModel) { a.showQuit = true }},
		// Added 2026-10-10. These four are overlays hasOverlay knows about
		// that the audit never rendered, and the gap is where a real defect
		// was living: the file picker ignored its WindowSizeMsg entirely and
		// rendered 134 cells wide in every terminal. The audit now covers
		// every overlay the model can show.
		{name: "filepicker", open: true, overlay: func(a *appModel) { a.showFilepicker = true }},
		{name: "theme-picker", open: true, overlay: func(a *appModel) { a.showThemeDialog = true }},
		{name: "init-dialog", open: true, overlay: func(a *appModel) { a.showInitDialog = true }},
		{name: "multi-arguments", open: true, overlay: func(a *appModel) { a.showMultiArgumentsDialog = true }},
	}
	// Down to 40x14. The grid used to start at 70x20, and that is where a
	// family of defects was hiding: the help dialog, the model picker and the
	// file picker all overflow a short terminal, losing their titles and their
	// bottom borders. 80x24 is the classic default; a split pane or a small
	// laptop window is routinely narrower and shorter than that.
	sizes := [][2]int{{40, 14}, {60, 18}, {70, 20}, {80, 24}, {100, 30}, {120, 36}, {160, 44}, {200, 50}}

	var findings []string
	note := func(f string, args ...any) { findings = append(findings, fmt.Sprintf(f, args...)) }

	for _, dark := range []bool{true, false} {
		lipgloss.SetHasDarkBackground(dark)
		mode := "dark"
		if !dark {
			mode = "light"
		}
		for _, st := range states {
			for _, sz := range sizes {
				w, h := sz[0], sz[1]
				agent := &app.App{
					Sessions: sessions, Messages: messages,
					CoderAgent: idleAgent{}, LSPClients: map[string]*lsp.Client{},
				}
				if st.busy {
					agent.CoderAgent = auditBusyAgent{}
				}
				var m tea.Model = New(agent)
				m = pump(m, m.Init(), 0)
				var cmd tea.Cmd
				m, cmd = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
				m = pump(m, cmd, 0)
				if st.open {
					m, cmd = m.Update(chat.SessionSelectedMsg(sess))
					m = pump(m, cmd, 0)
				}
				if st.name == "drawer" {
					m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
					m = pump(m, cmd, 0)
				}
				if st.overlay != nil {
					am := m.(appModel)
					st.overlay(&am)
					m = am
				}

				view := m.View()
				plain := ansi.Strip(view)
				lines := strings.Split(plain, "\n")
				id := fmt.Sprintf("%s/%s/%dx%d", mode, st.name, w, h)

				// 1. never taller than the terminal
				if len(lines) > h {
					note("%s: %d rows, %d more than the terminal has", id, len(lines), len(lines)-h)
				}
				// 2. never wider than the terminal
				for i, l := range lines {
					if wd := ansi.StringWidth(l); wd > w {
						note("%s: row %d is %d cells wide, %d over", id, i, wd, wd-w)
						break
					}
				}
				// 3. nothing at all rendered
				if strings.TrimSpace(plain) == "" {
					note("%s: rendered nothing", id)
					continue
				}
				// 4. an overlay must take the screen, not a sliver.
				//
				// Amended 2026-10-10: the region owns the screen *below* the
				// printed startup banner, so the rows it renders plus the
				// banner's are what account for the terminal. The bare
				// `len(lines) < h` this replaces was a proxy for "not
				// clipped" and fired for every overlay once the banner
				// existed.
				if st.overlay != nil || st.name == "drawer" {
					var bannerRows int
					if am, ok := m.(appModel); ok {
						bannerRows = am.bannerRows
					}
					if len(lines)+bannerRows != h {
						note("%s: overlay region is %d rows with %d printed, which is not the terminal's %d; it is clipped",
							id, len(lines), bannerRows, h)
					}
				}
				// 5. no row of the screen may be left below the composer.
				//
				// Replaces "idle must collapse, not pad". That check was the
				// old rule: the region went tight against the printed
				// conversation and whatever was under it was the terminal's.
				// It is not, because Bubble Tea's inline renderer erases
				// below a frame shorter than the one before it, so those rows
				// went blank and the composer sat wherever the short frame
				// ended -- reported from a real terminal as the typing bar
				// going to the top. Under D18 as amended the region is the
				// screen less what Aux printed, so the two account for the
				// terminal exactly -- or for more than it, once the
				// conversation is taller than the screen and the terminal
				// scrolls the rest off the top. Never for less.
				if st.overlay == nil && st.name != "drawer" {
					var printedRows int
					if am, ok := m.(appModel); ok {
						printedRows = am.printedRows
					}
					if total := printedRows + len(lines); total < h {
						note("%s: %d printed + %d live = %d rows of a %d-row terminal, so the composer is %d row(s) off the bottom",
							id, printedRows, len(lines), total, h, h-total)
					}
				}
				// 6. a run of blank rows *between* the region's own rows is
				// wasted space -- the gap between the history and the
				// composer that #53 was raised to fix. Blank rows above
				// everything are the pad that holds the composer on the last
				// row, which is the point rather than waste, so the count
				// starts at the region's first row with anything on it.
				blank := 0
				worst := 0
				started := false
				for _, l := range lines {
					if strings.TrimSpace(l) == "" {
						if !started {
							continue
						}
						blank++
						if blank > worst {
							worst = blank
						}
					} else {
						started = true
						blank = 0
					}
				}
				if st.overlay == nil && st.name != "drawer" && worst >= 3 {
					note("%s: %d consecutive blank rows inside the region", id, worst)
				}
				// 7. the composer has to be reachable in every non-overlay state
				if st.overlay == nil && st.name != "drawer" && !strings.Contains(plain, ">") {
					note("%s: no composer prompt on screen", id)
				}
			}
		}
	}
	lipgloss.SetHasDarkBackground(true)

	sort.Strings(findings)
	if len(findings) == 0 {
		t.Log("AUDIT: no findings across every state, size and mode")
		return
	}
	t.Logf("AUDIT: %d findings", len(findings))
	for _, f := range findings {
		t.Logf("  %s", f)
	}
}

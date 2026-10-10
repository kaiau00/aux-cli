package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
)

// Every overlay is placed onto the assembled view and bounded by it. Inline
// rendering collapses that view to a handful of rows when nothing is in
// flight, so an overlay has to grow it back to the screen first.
//
// This is a correctness guard, not a cosmetic one: the permission dialog gates
// every tool call, and a clipped permission dialog cannot be answered. The
// first cut of the live-region collapse clipped all eleven overlays, including
// that one.
func TestAnOverlayGrowsTheManagedRegion(t *testing.T) {
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
	if _, err := sessions.Create(context.Background(), "overlay"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	const termHeight = 40
	var m tea.Model = New(a)
	m.Init()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: termHeight})

	idle := lipgloss.Height(m.View())
	if idle >= termHeight {
		t.Fatalf("test setup: the idle region is %d rows of %d, so this cannot show an overlay growing it",
			idle, termHeight)
	}

	am, ok := m.(appModel)
	if !ok {
		t.Fatalf("expected an appModel, got %T", m)
	}
	if am.hasOverlay() {
		t.Fatal("no overlay should be showing yet")
	}

	for _, tc := range []struct {
		name string
		set  func(*appModel)
	}{
		{"permission prompt", func(a *appModel) { a.showPermissions = true }},
		{"help", func(a *appModel) { a.showHelp = true }},
		{"quit confirmation", func(a *appModel) { a.showQuit = true }},
		{"model picker", func(a *appModel) { a.showModelDialog = true }},
		{"session picker", func(a *appModel) { a.showSessionDialog = true }},
		{"command palette", func(a *appModel) { a.showCommandDialog = true }},
		{"theme picker", func(a *appModel) { a.showThemeDialog = true }},
		{"file picker", func(a *appModel) { a.showFilepicker = true }},
		{"compacting", func(a *appModel) { a.isCompacting = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			with := am
			tc.set(&with)
			if !with.hasOverlay() {
				t.Fatal("hasOverlay does not know about this overlay, so the region will not grow for it")
			}
			got := lipgloss.Height(with.View())
			if got != termHeight {
				t.Errorf("with the %s up the view is %d rows, want the terminal's %d; the overlay is clipped",
					tc.name, got, termHeight)
			}
		})
	}
}

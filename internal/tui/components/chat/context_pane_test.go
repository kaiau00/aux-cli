package chat

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestToggleCrossPersistsRealExclusion(t *testing.T) {
	conn := dbtest.New(t)
	pages := contextstore.NewStore(conn)
	m := NewContextPaneCmp(&app.App{Pages: pages})
	m.taskID = "task-1"
	m.entries = []ContextEntry{{Path: "a.go", ToolCallID: "call-a"}}
	m.selected = 0

	m.toggleCross(true)

	excl, err := pages.Exclusions(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Exclusions: %v", err)
	}
	if !excl["call-a"] {
		t.Fatalf("expected call-a excluded after cross-off, got %+v", excl)
	}
	if !m.entries[0].CrossedOff {
		t.Fatal("expected the local entry to also show crossed off")
	}

	m.toggleCross(false)
	excl, err = pages.Exclusions(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Exclusions after un-cross: %v", err)
	}
	if excl["call-a"] {
		t.Fatalf("expected call-a no longer excluded after un-cross, got %+v", excl)
	}
}

func TestTogglePinPersistsRealPin(t *testing.T) {
	conn := dbtest.New(t)
	pages := contextstore.NewStore(conn)
	m := NewContextPaneCmp(&app.App{Pages: pages})
	m.taskID = "task-1"
	m.entries = []ContextEntry{{Path: "a.go", ToolCallID: "call-a"}}
	m.selected = 0

	m.togglePin()

	pins, err := pages.Pins(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	if !pins["call-a"] {
		t.Fatalf("expected call-a pinned after toggle, got %+v", pins)
	}
	if !m.entries[0].Pinned {
		t.Fatal("expected the local entry to also show pinned")
	}

	m.togglePin()
	pins, err = pages.Pins(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Pins after un-pin: %v", err)
	}
	if pins["call-a"] {
		t.Fatalf("expected call-a no longer pinned after second toggle, got %+v", pins)
	}
	if m.entries[0].Pinned {
		t.Fatal("expected the local entry to also show un-pinned")
	}
}

func TestTogglePinWithoutTaskDoesNotPanic(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.entries = []ContextEntry{{Path: "a.go", ToolCallID: "call-a"}}
	m.selected = 0
	m.togglePin() // no Pages, no taskID: must be a safe no-op beyond local state
	if !m.entries[0].Pinned {
		t.Fatal("local state should still update even without a wired store")
	}
}

func TestClearCrossedClearsAllExclusions(t *testing.T) {
	conn := dbtest.New(t)
	pages := contextstore.NewStore(conn)
	m := NewContextPaneCmp(&app.App{Pages: pages})
	m.taskID = "task-1"
	m.entries = []ContextEntry{
		{Path: "a.go", ToolCallID: "call-a", CrossedOff: true},
		{Path: "b.go", ToolCallID: "call-b", CrossedOff: false},
	}
	if err := pages.Exclude(context.Background(), "task-1", "call-a"); err != nil {
		t.Fatalf("seed Exclude: %v", err)
	}

	m.clearCrossed()

	if len(m.entries) != 1 || m.entries[0].ToolCallID != "call-b" {
		t.Fatalf("expected only the non-crossed entry to remain, got %+v", m.entries)
	}
	excl, err := pages.Exclusions(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Exclusions: %v", err)
	}
	if len(excl) != 0 {
		t.Fatalf("expected all exclusions cleared, got %+v", excl)
	}
}

func TestExpandKeyTogglesExpandedView(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.width = 60
	m.budget.TotalTokens = 100
	m.budget.Categories = []viewmodel.ContextCategoryVM{{Label: "Active code", Tokens: 100}}
	m.pageList.Resident = []viewmodel.ContextPageEntryVM{{StableKey: "file:/a.go", Tokens: 100}}

	compact := m.budgetView()
	if compact == "" {
		t.Fatal("expected the compact budget view to render")
	}

	m.expandedContext = true
	expanded := m.budgetView()
	if expanded == "" {
		t.Fatal("expected the expanded view to render once toggled")
	}
	if expanded == compact {
		t.Fatal("expanded view should differ from the compact view")
	}
}

func TestExpandedViewFallsBackToCompactWhenNoPageBindings(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.width = 60
	m.budget.TotalTokens = 100
	m.budget.Categories = []viewmodel.ContextCategoryVM{{Label: "Active code", Tokens: 100}}
	m.expandedContext = true // no pageList populated

	if got := m.budgetView(); got == "" {
		t.Fatal("expected a fallback to the compact view when there is nothing to expand")
	}
}

func TestDashboardURLKeyTogglesVisibility(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.width = 60

	if m.showDashboardURL {
		t.Fatal("dashboard URL should start collapsed")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m.showDashboardURL {
		t.Fatal("expected 'd' to reveal the dashboard URL")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.showDashboardURL {
		t.Fatal("expected a second 'd' to collapse it again")
	}
}

func TestDashboardURLHiddenWhenNotLive(t *testing.T) {
	// No app.Dashboard wired (as in most tests): dashboardView must not
	// dereference a nil Dashboard, and the reveal hint only makes sense once
	// the dashboard is actually live.
	m := NewContextPaneCmp(&app.App{})
	m.width = 60
	if out := m.dashboardView(); out == "" {
		t.Fatal("dashboardView should still render an 'off' line")
	} else if strings.Contains(out, "for url") {
		t.Fatalf("should not hint at revealing a URL that doesn't exist: %q", out)
	}
}

func TestViewCollapsesEmptyContextToOneLine(t *testing.T) {
	conn := dbtest.New(t)
	pages := contextstore.NewStore(conn)
	m := NewContextPaneCmp(&app.App{Pages: pages})
	m.width, m.height = 60, 24

	out := m.View()
	if strings.Contains(out, "no files loaded yet") == false {
		t.Fatalf("expected the collapsed empty-context line, got:\n%s", out)
	}
	if strings.Contains(out, "move · x off · u on · c clear") {
		t.Fatal("hotkey footer should not show when there's nothing to act on")
	}
}

func TestToggleCrossWithoutTaskDoesNotPanic(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.entries = []ContextEntry{{Path: "a.go", ToolCallID: "call-a"}}
	m.selected = 0
	m.toggleCross(true) // no Pages, no taskID: must be a safe no-op beyond local state
	if !m.entries[0].CrossedOff {
		t.Fatal("local state should still update even without a wired store")
	}
}

func TestRenderRowMarksPinnedEntries(t *testing.T) {
	// The pin marker is the only on-screen difference between a pinned page
	// and an ordinary one, so it needs its own guard.
	m := NewContextPaneCmp(&app.App{})
	m.width = 60

	plain := m.renderRow(ContextEntry{Path: "a.go", Lines: 10}, false, 60)
	pinned := m.renderRow(ContextEntry{Path: "a.go", Lines: 10, Pinned: true}, false, 60)

	if plain == pinned {
		t.Fatal("a pinned entry must render differently from an unpinned one")
	}
	if !strings.Contains(pinned, styles.PinIcon) {
		t.Fatalf("pinned row should carry the pin icon %q, got %q", styles.PinIcon, pinned)
	}
	if strings.Contains(plain, styles.PinIcon) {
		t.Fatalf("unpinned row must not show a pin icon, got %q", plain)
	}
}

func TestExcludePathCrossesOffAndPersists(t *testing.T) {
	conn := dbtest.New(t)
	pages := contextstore.NewStore(conn)
	m := NewContextPaneCmp(&app.App{Pages: pages})
	m.taskID = "task-1"
	m.entries = []ContextEntry{
		{Path: "a.go", AbsPath: "/repo/a.go", ToolCallID: "call-a"},
		{Path: "b.go", AbsPath: "/repo/b.go", ToolCallID: "call-b"},
	}

	if matched := m.ExcludePath("a.go"); matched != 1 {
		t.Fatalf("expected one match, got %d", matched)
	}
	if !m.entries[0].CrossedOff {
		t.Fatal("the matched entry should be crossed off")
	}
	if m.entries[1].CrossedOff {
		t.Fatal("an unrelated entry must not be touched")
	}

	excl, err := pages.Exclusions(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("Exclusions: %v", err)
	}
	if !excl["call-a"] || excl["call-b"] {
		t.Fatalf("expected only call-a excluded, got %+v", excl)
	}
}

// A mistyped path must be distinguishable from a successful drop, or the user
// gets silent no-ops.
func TestExcludePathReportsNoMatch(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.entries = []ContextEntry{{Path: "a.go", AbsPath: "/repo/a.go", ToolCallID: "call-a"}}

	if matched := m.ExcludePath("typo.go"); matched != 0 {
		t.Fatalf("expected no matches, got %d", matched)
	}
	if matched := m.ExcludePath(""); matched != 0 {
		t.Fatalf("an empty path must match nothing, got %d", matched)
	}
	if m.entries[0].CrossedOff {
		t.Fatal("nothing should have been crossed off")
	}
}

// Pages the agent dropped itself must be visibly distinct from ones the user
// crossed off, and must be undoable the same way.
func TestAgentDroppedEntriesAreMarkedAndUndoable(t *testing.T) {
	m := NewContextPaneCmp(&app.App{})
	m.width = 60
	m.entries = []ContextEntry{{Path: "a.go", AbsPath: "/repo/a.go", ToolCallID: "call-a", Lines: 10}}

	m.mu.Lock()
	m.markAgentDroppedLocked([]string{"a.go"})
	m.mu.Unlock()

	if !m.entries[0].CrossedOff || !m.entries[0].DroppedByAgent {
		t.Fatalf("agent-dropped entry should be crossed off and marked, got %+v", m.entries[0])
	}

	row := m.renderRow(m.entries[0], false, 60)
	if !strings.Contains(row, styles.AgentDropIcon) {
		t.Fatalf("agent-dropped rows need their own marker, got %q", row)
	}

	// The user restores it with the same key that un-crosses their own edits.
	m.selected = 0
	m.toggleCross(false)
	if m.entries[0].CrossedOff || m.entries[0].DroppedByAgent {
		t.Fatalf("restoring should clear both flags, got %+v", m.entries[0])
	}
	if strings.Contains(m.renderRow(m.entries[0], false, 60), styles.AgentDropIcon) {
		t.Fatal("a restored entry must not still show the dropped marker")
	}
}

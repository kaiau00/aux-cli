package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/activity"
	"github.com/kaiau00/aux-cli/internal/tui/components/contextbudget"
	"github.com/kaiau00/aux-cli/internal/tui/components/workbench"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// ContextEntry represents a single file currently loaded into the agent's
// context window. The path is the display path (relative to the working
// directory); the line count is the number of lines the agent actually
// received, not the file's total length.
type ContextEntry struct {
	Path       string
	AbsPath    string
	Lines      int
	Score      float64
	Reason     string
	CrossedOff bool
	Pinned     bool
	// DroppedByAgent marks an entry the agent excluded itself via the
	// context_exclude tool, as opposed to one the user crossed off. Both are
	// crossed off; only this one needs explaining, and the user can undo it
	// with the same 'u' key.
	DroppedByAgent bool
	ReadAt         time.Time
	MessageID      string
	ToolCallID     string
}

type ContextPaneCmp struct {
	app    *app.App
	width  int
	height int

	mu        sync.Mutex
	sessionID string
	entries   []ContextEntry
	selected  int
	offset    int

	// activity holds the grouped runtime activity for the current session,
	// projected from durable tool events. Refreshed on
	// message updates and session changes; empty when there is nothing to show.
	activity []viewmodel.ActivityGroupVM

	// validation and changes are the task workbench
	// for the session's task. hasTask is false when the session has no task, so
	// the sections are hidden rather than faked. Changes render "no changes yet"
	// as an informative state once a task exists.
	validation viewmodel.ValidationSummaryVM
	changes    viewmodel.ChangeSummaryVM
	hasTask    bool
	// taskID is the current session's latest task id, used so the x/u/c
	// cross-off controls persist a real exclusion for the task's next prompt
	// compile instead of only repainting a checkbox.
	taskID string

	// budget is the context signature composition,
	// projected from the latest model call's page bindings — the same manifest
	// the model received. Empty (and hidden) until demand paging records
	// bindings, leaving the file list below as the compatibility adapter.
	budget viewmodel.ContextBudgetVM

	// pageList is the same call's bindings grouped by state, backing the
	// expanded context view toggled by the Expand key.
	pageList        viewmodel.ContextPageListVM
	expandedContext bool

	// showDashboardURL reveals the full tokened dashboard URL. It stays
	// collapsed to a one-line status by default — the full copy-paste URL
	// isn't something you need visible on every redraw of every session.
	showDashboardURL bool

	// editorFocused suppresses pane hotkeys while the editor textarea is
	// active so the user can type x/u/c/j/k without triggering context
	// pane actions.
	editorFocused bool

	keys ContextPaneKeys
}

type ContextPaneKeys struct {
	Up           key.Binding
	Down         key.Binding
	CrossOff     key.Binding
	Uncross      key.Binding
	Clear        key.Binding
	Pin          key.Binding
	Expand       key.Binding
	DashboardURL key.Binding
}

var defaultContextPaneKeys = ContextPaneKeys{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "context up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "context down"),
	),
	CrossOff: key.NewBinding(
		key.WithKeys("x"),
		key.WithHelp("x", "cross off"),
	),
	Uncross: key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "un-cross"),
	),
	Clear: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear crossed"),
	),
	Pin: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "toggle pin"),
	),
	Expand: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "expand context"),
	),
	DashboardURL: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "show dashboard url"),
	),
}

func (m *ContextPaneCmp) Init() tea.Cmd {
	if m.app == nil || m.app.Messages == nil {
		return nil
	}
	ctx := context.Background()
	ch := m.app.Messages.Subscribe(ctx)
	return func() tea.Msg {
		return <-ch
	}
}

func (m *ContextPaneCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case SessionSelectedMsg:
		if msg.ID != m.currentSessionID() {
			m.setSessionID(msg.ID)
			m.mu.Lock()
			m.entries = nil
			m.selected = 0
			m.offset = 0
			m.activity = nil
			m.hasTask = false
			m.mu.Unlock()
			m.refreshActivity()
			m.refreshWorkbench()
		}
	case SessionClearedMsg:
		m.setSessionID("")
		m.mu.Lock()
		m.entries = nil
		m.selected = 0
		m.offset = 0
		m.activity = nil
		m.hasTask = false
		m.mu.Unlock()
	case pubsub.Event[message.Message]:
		if sid := m.currentSessionID(); sid == "" || msg.Payload.SessionID != sid {
			return m, nil
		}
		if msg.Type == pubsub.CreatedEvent || msg.Type == pubsub.UpdatedEvent {
			m.absorbMessage(msg.Payload)
			m.refreshActivity()
			m.refreshWorkbench()
		}
		return m, func() tea.Msg {
			ctx := context.Background()
			ch := m.app.Messages.Subscribe(ctx)
			return <-ch
		}
	case pubsub.Event[session.Session]:
		if m.currentSessionID() == "" && msg.Type == pubsub.CreatedEvent {
			m.setSessionID(msg.Payload.ID)
		}
	case tea.KeyMsg:
		// Skip pane hotkeys while the editor textarea has focus so x/u/c/j/k
		// type into the prompt instead of acting on the pane.
		if m.editorFocused {
			return m, nil
		}
		switch {
		case key.Matches(msg, m.keys.Up):
			m.moveSelection(-1)
		case key.Matches(msg, m.keys.Down):
			m.moveSelection(1)
		case key.Matches(msg, m.keys.CrossOff):
			m.toggleCross(true)
		case key.Matches(msg, m.keys.Uncross):
			m.toggleCross(false)
		case key.Matches(msg, m.keys.Clear):
			m.clearCrossed()
		case key.Matches(msg, m.keys.Pin):
			m.togglePin()
		case key.Matches(msg, m.keys.Expand):
			m.mu.Lock()
			m.expandedContext = !m.expandedContext
			m.mu.Unlock()
		case key.Matches(msg, m.keys.DashboardURL):
			m.mu.Lock()
			m.showDashboardURL = !m.showDashboardURL
			m.mu.Unlock()
		}
	}
	return m, nil
}

func (m *ContextPaneCmp) BindingKeys() []key.Binding {
	return []key.Binding{m.keys.Up, m.keys.Down, m.keys.CrossOff, m.keys.Uncross, m.keys.Clear, m.keys.Pin, m.keys.Expand, m.keys.DashboardURL}
}

// SetEditorFocused toggles whether the editor textarea currently has focus.
// While true, the pane ignores its letter hotkeys so the user can type
// freely in the editor.
func (m *ContextPaneCmp) SetEditorFocused(focused bool) {
	m.editorFocused = focused
}

func (m *ContextPaneCmp) SetSize(width, height int) tea.Cmd {
	m.width = width
	m.height = height
	return nil
}

func (m *ContextPaneCmp) GetSize() (int, int) {
	return m.width, m.height
}

// currentSessionID and setSessionID keep sessionID under the same mutex that
// guards the rest of the pane's state: it is written from the Bubble Tea
// update goroutine and read by the refresh helpers.
func (m *ContextPaneCmp) currentSessionID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionID
}

func (m *ContextPaneCmp) setSessionID(id string) {
	m.mu.Lock()
	m.sessionID = id
	m.mu.Unlock()
}

func (m *ContextPaneCmp) moveSelection(delta int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) == 0 {
		m.selected = 0
		return
	}
	m.selected += delta
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(m.entries) {
		m.selected = len(m.entries) - 1
	}
}

// toggleCross marks the selected entry crossed off (or not) and persists a
// real exclusion override for the task's next prompt compile, so x/u change what is actually sent to the model rather than only
// repainting a checkbox.
func (m *ContextPaneCmp) toggleCross(crossed bool) {
	m.mu.Lock()
	if m.selected < 0 || m.selected >= len(m.entries) {
		m.mu.Unlock()
		return
	}
	m.entries[m.selected].CrossedOff = crossed
	if !crossed {
		// Restoring a page the agent dropped makes it an ordinary entry again;
		// keeping the marker would imply it is still excluded.
		m.entries[m.selected].DroppedByAgent = false
	}
	toolCallID := m.entries[m.selected].ToolCallID
	taskID := m.taskID
	m.mu.Unlock()

	if m.app == nil || m.app.Pages == nil || taskID == "" || toolCallID == "" {
		return
	}
	ctx := context.Background()
	if crossed {
		_ = m.app.Pages.Exclude(ctx, taskID, toolCallID)
	} else {
		_ = m.app.Pages.Include(ctx, taskID, toolCallID)
	}
}

// clearCrossed drops every crossed-off entry from the display list and clears
// every exclusion override recorded for the task, so the next compile again
// includes everything.
func (m *ContextPaneCmp) clearCrossed() {
	m.mu.Lock()
	kept := m.entries[:0]
	for _, e := range m.entries {
		if !e.CrossedOff {
			kept = append(kept, e)
		}
	}
	m.entries = kept
	if m.selected >= len(m.entries) {
		m.selected = max(0, len(m.entries)-1)
	}
	taskID := m.taskID
	m.mu.Unlock()

	if m.app == nil || m.app.Pages == nil || taskID == "" {
		return
	}
	_ = m.app.Pages.ClearExclusions(context.Background(), taskID)
}

// togglePin marks the selected entry pinned (or not) and persists a real pin
// override for the task's next prompt compile: a
// pinned page's full content is guaranteed in the compiled prompt, exempt
// from both exclusion and dedup stubbing, so p is a stronger guarantee than
// just not crossing something off.
func (m *ContextPaneCmp) togglePin() {
	m.mu.Lock()
	if m.selected < 0 || m.selected >= len(m.entries) {
		m.mu.Unlock()
		return
	}
	pinned := !m.entries[m.selected].Pinned
	m.entries[m.selected].Pinned = pinned
	toolCallID := m.entries[m.selected].ToolCallID
	taskID := m.taskID
	m.mu.Unlock()

	if m.app == nil || m.app.Pages == nil || taskID == "" || toolCallID == "" {
		return
	}
	ctx := context.Background()
	if pinned {
		_ = m.app.Pages.Pin(ctx, taskID, toolCallID)
	} else {
		_ = m.app.Pages.Unpin(ctx, taskID, toolCallID)
	}
}

// absorbMessage scans a message for view tool results and appends one
// ContextEntry per successful read. Re-reads of the same path replace the
// prior entry so the pane reflects what is currently in the agent's context,
// not a historical list.
func (m *ContextPaneCmp) absorbMessage(msg message.Message) {
	results := msg.ToolResults()
	if len(results) == 0 {
		return
	}

	var newEntries []ContextEntry
	var agentDropped []string
	for _, result := range results {
		if result.IsError {
			continue
		}
		// The agent can drop its own pages; reflect that in the pane so the
		// user can see what it discarded and put anything back.
		var dropped tools.ContextExcludeResponseMetadata
		if json.Unmarshal([]byte(result.Metadata), &dropped) == nil && len(dropped.Excluded) > 0 {
			agentDropped = append(agentDropped, dropped.Excluded...)
			continue
		}
		var meta tools.ViewResponseMetadata
		if err := json.Unmarshal([]byte(result.Metadata), &meta); err != nil {
			continue
		}
		if meta.FilePath == "" {
			continue
		}
		entry := ContextEntry{
			AbsPath:    meta.FilePath,
			Path:       removeWorkingDirPrefix(meta.FilePath),
			Lines:      strings.Count(meta.Content, "\n") + 1,
			Score:      meta.DecisionScore,
			Reason:     meta.DecisionReason,
			ReadAt:     time.Now(),
			MessageID:  msg.ID,
			ToolCallID: result.ToolCallID,
		}
		if !meta.DecisionAllowed {
			// Surface rejected reads too, so the user can see what the
			// gate blocked. They render with a distinct style.
			entry.Reason = "rejected: " + entry.Reason
		}
		newEntries = append(newEntries, entry)
	}

	if len(newEntries) == 0 && len(agentDropped) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.markAgentDroppedLocked(agentDropped)

	// Replace prior entries for the same path; keep crossed-off state if the
	// user already marked them.
	for _, ne := range newEntries {
		replaced := false
		for i, existing := range m.entries {
			if existing.AbsPath == ne.AbsPath {
				if existing.CrossedOff {
					ne.CrossedOff = true
				}
				m.entries[i] = ne
				replaced = true
				break
			}
		}
		if !replaced {
			m.entries = append(m.entries, ne)
		}
	}
	sort.SliceStable(m.entries, func(i, j int) bool {
		return m.entries[i].ReadAt.Before(m.entries[j].ReadAt)
	})
}

// ExcludePathMsg drops a file from the current task's context by path, which is
// what the /exclude command sends. The pane's x key does the same thing for the
// selected row; this exists for the case where the file is not on screen.
type ExcludePathMsg struct{ Path string }

// ExcludePath crosses off and persistently excludes every entry matching a path.
// It reports whether anything matched so the caller can tell the user, since a
// typo would otherwise look identical to success.
func (m *ContextPaneCmp) ExcludePath(path string) (matched int) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0
	}

	m.mu.Lock()
	taskID := m.taskID
	var toolCallIDs []string
	for i := range m.entries {
		e := &m.entries[i]
		if e.AbsPath == path || e.Path == path || strings.HasSuffix(e.AbsPath, "/"+strings.TrimPrefix(path, "./")) {
			e.CrossedOff = true
			e.DroppedByAgent = false // the user asked for this one
			if e.ToolCallID != "" {
				toolCallIDs = append(toolCallIDs, e.ToolCallID)
			}
			matched++
		}
	}
	m.mu.Unlock()

	if m.app == nil || m.app.Pages == nil || taskID == "" {
		return matched
	}
	ctx := context.Background()
	for _, id := range toolCallIDs {
		_ = m.app.Pages.Exclude(ctx, taskID, id)
	}
	return matched
}

// markAgentDroppedLocked crosses off entries the agent excluded itself. The
// paths come from the tool's own report, so they are matched leniently: the
// agent may have named a file relative while the entry holds it absolute.
// Callers must hold m.mu.
func (m *ContextPaneCmp) markAgentDroppedLocked(paths []string) {
	for _, p := range paths {
		for i := range m.entries {
			e := &m.entries[i]
			if e.AbsPath == p || e.Path == p || strings.HasSuffix(e.AbsPath, "/"+strings.TrimPrefix(p, "./")) {
				e.CrossedOff = true
				e.DroppedByAgent = true
			}
		}
	}
}

// divider renders a thin, muted rule used between the pane's top-level
// sections (dashboard/activity/changes+validation/context budget/file list)
// so they read as distinct groups instead of a single run-on column of
// same-weight headers.
func (m *ContextPaneCmp) divider() string {
	t := theme.CurrentTheme()
	width := m.width - 2
	if width < 1 {
		width = 1
	}
	return styles.BaseStyle().
		Width(m.width).
		Foreground(t.BorderDim()).
		Render(" " + strings.Repeat("─", width))
}

func (m *ContextPaneCmp) View() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	m.mu.Lock()
	entries := append([]ContextEntry(nil), m.entries...)
	selected := m.selected
	m.mu.Unlock()

	// PaddingLeft rather than a literal leading space: the space only indents
	// the first line, so anything that wraps loses the indent and the pane
	// develops a ragged left edge.
	header := baseStyle.
		Width(m.width).
		PaddingLeft(1).
		Foreground(t.Primary()).
		Bold(true).
		Render(fmt.Sprintf("Context (%d)", len(entries)))
	dashboard := m.dashboardView()
	activitySection := m.activityView()
	workbenchSection := m.workbenchView()
	budgetSection := m.budgetView()

	footer := baseStyle.
		Width(m.width).
		PaddingLeft(1).
		Foreground(t.TextMuted()).
		Render("↑/↓ move · x off · u on · c clear")

	if len(entries) == 0 {
		// A bold header, an empty-state line, and the hotkey footer for zero
		// actionable rows was three lines of boilerplate on every fresh
		// session; the hotkeys aren't relevant until there's something to
		// act on, so this collapses to one muted line instead.
		compact := baseStyle.
			Width(m.width).
			PaddingLeft(1).
			Foreground(t.TextMuted()).
			Italic(true).
			Render("Context — no files loaded yet")
		sections := []string{dashboard, m.divider()}
		if activitySection != "" {
			sections = append(sections, activitySection, m.divider())
		}
		if workbenchSection != "" {
			sections = append(sections, workbenchSection, m.divider())
		}
		if budgetSection != "" {
			sections = append(sections, budgetSection, m.divider())
		}
		sections = append(sections, compact)
		return baseStyle.
			Width(m.width).
			Height(m.height).
			Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
	}

	extraHeight := 0
	if activitySection != "" {
		extraHeight += lipgloss.Height(activitySection) + 1 // section + spacer
	}
	if workbenchSection != "" {
		extraHeight += lipgloss.Height(workbenchSection) + 1
	}
	if budgetSection != "" {
		extraHeight += lipgloss.Height(budgetSection) + 1
	}
	bodyHeight := m.height - lipgloss.Height(dashboard) - extraHeight - 5
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	start := selected - bodyHeight/2
	if start < 0 {
		start = 0
	}
	if start > len(entries)-bodyHeight {
		start = len(entries) - bodyHeight
	}
	if start < 0 {
		start = 0
	}
	end := start + bodyHeight
	if end > len(entries) {
		end = len(entries)
	}

	var rows []string
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(entries[i], i == selected, m.width))
	}

	sections := []string{dashboard, m.divider()}
	if activitySection != "" {
		sections = append(sections, activitySection, m.divider())
	}
	if workbenchSection != "" {
		sections = append(sections, workbenchSection, m.divider())
	}
	if budgetSection != "" {
		sections = append(sections, budgetSection, m.divider())
	}
	sections = append(sections,
		header,
		lipgloss.JoinVertical(lipgloss.Left, rows...),
		" ",
		footer,
	)
	return baseStyle.
		Width(m.width).
		Height(m.height).
		Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
}

// maxActivityRows bounds how many activity groups the pane shows so the section
// never crowds out the context list. There are at most a handful of kinds.
const maxActivityRows = 6

// refreshActivity rebuilds the grouped activity for the current session from
// durable tool events. Failures are ignored so
// observability never disturbs the pane; empty results simply hide the section.
func (m *ContextPaneCmp) refreshActivity() {
	sessionID := m.currentSessionID()
	if m.app == nil || m.app.Events == nil || sessionID == "" {
		m.mu.Lock()
		m.activity = nil
		m.mu.Unlock()
		return
	}
	events, err := m.app.Events.List(context.Background(), eventstore.Filter{SessionID: sessionID})
	if err != nil {
		return
	}
	groups := viewmodel.BuildActivityGroups(events)
	m.mu.Lock()
	m.activity = groups
	m.mu.Unlock()
}

// activityView renders the bounded activity section, or "" when there is nothing
// to show.
func (m *ContextPaneCmp) activityView() string {
	m.mu.Lock()
	groups := append([]viewmodel.ActivityGroupVM(nil), m.activity...)
	m.mu.Unlock()
	if len(groups) == 0 {
		return ""
	}
	if len(groups) > maxActivityRows {
		groups = groups[len(groups)-maxActivityRows:]
	}
	return activity.Render(groups, m.width)
}

// refreshWorkbench rebuilds the task workbench — changes and
// acceptance/proof-of-done — for the session's task. Criteria without
// evidence stay unverified: the surface never implies success because the agent
// stopped. Changes come from the task's latest checkpoint, so they read "no
// changes yet" until a checkpoint exists.
func (m *ContextPaneCmp) refreshWorkbench() {
	sessionID := m.currentSessionID()
	if m.app == nil || m.app.Tasks == nil || sessionID == "" {
		m.mu.Lock()
		m.hasTask = false
		m.mu.Unlock()
		return
	}
	ctx := context.Background()
	tasks, err := m.app.Tasks.ListBySession(ctx, sessionID)
	if err != nil || len(tasks) == 0 {
		return
	}
	latest := tasks[len(tasks)-1]

	validationVM := m.buildValidation(ctx, latest.ID)
	changesVM := m.buildChanges(ctx, latest.ID)
	budgetVM := m.buildBudget(ctx, latest.ID)
	pageListVM := m.buildPageList(ctx, latest.ID)

	m.mu.Lock()
	m.validation = validationVM
	m.changes = changesVM
	m.budget = budgetVM
	m.pageList = pageListVM
	m.hasTask = true
	m.taskID = latest.ID
	m.mu.Unlock()
}

// buildBudget projects the latest model call's page bindings into a context
// budget. It reconciles with the prompt manifest because
// it reads the very bindings recorded for that call. Empty until demand paging
// records bindings.
func (m *ContextPaneCmp) buildBudget(ctx context.Context, taskID string) viewmodel.ContextBudgetVM {
	if m.app.Pages == nil || m.app.Cost == nil {
		return viewmodel.ContextBudgetVM{}
	}
	calls, err := m.app.Cost.ListCallsByTask(ctx, taskID)
	if err != nil || len(calls) == 0 {
		return viewmodel.ContextBudgetVM{}
	}
	last := calls[len(calls)-1]
	bindings, err := m.app.Pages.BindingsForCall(ctx, last.ID)
	if err != nil || len(bindings) == 0 {
		return viewmodel.ContextBudgetVM{}
	}
	// The same call's real, provider-reported total -- not the model's
	// context window. See ContextBudgetVM.CallTotalTokens.
	callTotal := last.InputTokens + last.OutputTokens + last.CacheCreationTokens + last.CacheReadTokens
	return viewmodel.BuildContextBudget(bindings, callTotal, m.savedTokens(ctx))
}

// buildPageList projects the same call's bindings buildBudget reads into the
// expanded, per-page view grouped by binding state.
func (m *ContextPaneCmp) buildPageList(ctx context.Context, taskID string) viewmodel.ContextPageListVM {
	if m.app.Pages == nil || m.app.Cost == nil {
		return viewmodel.ContextPageListVM{}
	}
	calls, err := m.app.Cost.ListCallsByTask(ctx, taskID)
	if err != nil || len(calls) == 0 {
		return viewmodel.ContextPageListVM{}
	}
	last := calls[len(calls)-1]
	bindings, err := m.app.Pages.BindingsForCall(ctx, last.ID)
	if err != nil || len(bindings) == 0 {
		return viewmodel.ContextPageListVM{}
	}
	return viewmodel.BuildContextPageList(bindings)
}

// savedTokens reads the latest context.compiled event's saved-token count for
// the session, so the budget can show real delta/artifact reuse.
func (m *ContextPaneCmp) savedTokens(ctx context.Context) int64 {
	if m.app.Events == nil {
		return 0
	}
	events, err := m.app.Events.List(ctx, eventstore.Filter{SessionID: m.currentSessionID()})
	if err != nil {
		return 0
	}
	var saved int64
	for _, e := range events {
		if e.Type == eventstore.ContextCompiled {
			var cp eventstore.ContextPayload
			if e.DecodePayload(&cp) == nil {
				saved = cp.SavedTokens
			}
		}
	}
	return saved
}

// budgetView renders the context budget, or "" when there are no page bindings
// yet (paging off), so the file list below remains the compatibility adapter.
// The Expand key toggles between the compact summary and the per-page,
// grouped-by-state expanded view.
func (m *ContextPaneCmp) budgetView() string {
	m.mu.Lock()
	vm := m.budget
	pages := m.pageList
	expanded := m.expandedContext
	m.mu.Unlock()
	if expanded {
		if out := contextbudget.RenderExpanded(pages, m.width); out != "" {
			return out
		}
	}
	return contextbudget.Render(vm, m.width)
}

// buildValidation joins the task's acceptance criteria with proof-of-done.
func (m *ContextPaneCmp) buildValidation(ctx context.Context, taskID string) viewmodel.ValidationSummaryVM {
	spec, ok, err := m.app.Tasks.LatestSpec(ctx, taskID)
	if err != nil || !ok || len(spec.AcceptanceCriteria) == 0 {
		return viewmodel.BuildValidationSummary(nil)
	}
	ids := make([]string, 0, len(spec.AcceptanceCriteria))
	for _, c := range spec.AcceptanceCriteria {
		ids = append(ids, c.ID)
	}
	var states map[string]validation.CriterionState
	if m.app.Validations != nil {
		states, _ = m.app.Validations.ProofOfDone(ctx, taskID, ids)
	}
	inputs := make([]viewmodel.CriterionInput, 0, len(spec.AcceptanceCriteria))
	for _, c := range spec.AcceptanceCriteria {
		inputs = append(inputs, viewmodel.CriterionInput{Description: c.Description, State: states[c.ID]})
	}
	return viewmodel.BuildValidationSummary(inputs)
}

// buildChanges projects the task's latest checkpoint into a change summary. An
// empty result renders as the informative "no changes yet" state.
func (m *ContextPaneCmp) buildChanges(ctx context.Context, taskID string) viewmodel.ChangeSummaryVM {
	if m.app.CheckpointStore == nil {
		return viewmodel.ChangeSummaryVM{}
	}
	cps, err := m.app.CheckpointStore.ListByTask(ctx, taskID)
	if err != nil || len(cps) == 0 {
		return viewmodel.ChangeSummaryVM{}
	}
	entries, err := m.app.CheckpointStore.Entries(ctx, cps[len(cps)-1].ID)
	if err != nil {
		return viewmodel.ChangeSummaryVM{}
	}
	return viewmodel.BuildChangeSummary(entries)
}

// workbenchView renders the changes + validation sections, or "" when the
// session has no task yet or neither section has anything to report (both
// render "" individually before there's a change or a criterion to show —
// see workbench.RenderChanges/RenderValidation).
func (m *ContextPaneCmp) workbenchView() string {
	m.mu.Lock()
	has := m.hasTask
	changesVM := m.changes
	validationVM := m.validation
	m.mu.Unlock()
	if !has {
		return ""
	}
	var parts []string
	if s := workbench.RenderChanges(changesVM, m.width); s != "" {
		parts = append(parts, s)
	}
	if s := workbench.RenderValidation(validationVM, m.width); s != "" {
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// dashboardView renders the dashboard status. Collapsed to one line by
// default: the full tokened URL is a copy-paste value,
// not something worth a standing 2-3 line block on every redraw of every
// session. The "d" key reveals/hides it on demand.
func (m *ContextPaneCmp) dashboardView() string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	url := ""
	if m.app != nil && m.app.Dashboard != nil {
		url = m.app.Dashboard.URL()
	}
	live := url != ""

	state := "off"
	stateColor := t.TextMuted()
	if live {
		state = "live · " + dashboardHostPort(url)
		stateColor = t.Success()
	}

	m.mu.Lock()
	showURL := m.showDashboardURL
	m.mu.Unlock()

	hint := ""
	if live {
		if showURL {
			hint = "  ·  d to hide url"
		} else {
			hint = "  ·  d for url"
		}
	}
	line := baseStyle.
		Width(m.width).
		PaddingLeft(1).
		Render(
			lipgloss.NewStyle().Foreground(t.Primary()).Bold(true).Render("Dashboard") +
				"  " +
				lipgloss.NewStyle().Foreground(stateColor).Render(state) +
				lipgloss.NewStyle().Foreground(t.TextMuted()).Render(hint),
		)

	if !live || !showURL {
		return line
	}

	parts := []string{line}
	for _, u := range wrapUnspaced(url, max(4, m.width-1)) {
		parts = append(parts, baseStyle.
			Width(m.width).
			Foreground(t.TextMuted()).
			Render(" "+u))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// dashboardHostPort extracts the compact host:port from a dashboard URL,
// omitting the scheme, path, and token so the panel shows a short live-state.
func dashboardHostPort(url string) string {
	rest := url
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func wrapUnspaced(value string, width int) []string {
	if value == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{value}
	}
	runes := []rune(value)
	lines := make([]string, 0, len(runes)/width+1)
	for len(runes) > width {
		lines = append(lines, string(runes[:width]))
		runes = runes[width:]
	}
	lines = append(lines, string(runes))
	return lines
}

func (m *ContextPaneCmp) renderRow(e ContextEntry, selected bool, width int) string {
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	marker := "  "
	if selected {
		marker = "▸ "
	}

	displayPath := e.Path
	if displayPath == "" {
		displayPath = filepath.Base(e.AbsPath)
	}
	if e.Pinned {
		displayPath = styles.PinIcon + " " + displayPath
	}
	if e.DroppedByAgent {
		displayPath = styles.AgentDropIcon + " " + displayPath
	}

	lineInfo := fmt.Sprintf("%d ln", e.Lines)
	scoreInfo := ""
	if e.Score > 0 {
		scoreInfo = fmt.Sprintf(" · %.3f", e.Score)
	}
	tail := " " + lineInfo + scoreInfo

	pathWidth := width - lipgloss.Width(marker) - lipgloss.Width(tail) - 2
	if pathWidth < 4 {
		pathWidth = 4
	}
	if lipgloss.Width(displayPath) > pathWidth {
		displayPath = ansiTruncate(displayPath, pathWidth-1, "…")
	}

	style := baseStyle.Width(width)
	switch {
	case e.CrossedOff:
		style = style.Foreground(t.TextMuted()).Strikethrough(true)
	case e.Pinned:
		style = style.Foreground(t.Warning()).Bold(true)
	case strings.HasPrefix(e.Reason, "rejected:"):
		style = style.Foreground(t.Error())
	case e.Reason != "":
		style = style.Foreground(t.Text())
	default:
		style = style.Foreground(t.Text())
	}
	if selected {
		style = style.Foreground(t.Primary()).Bold(true)
	}

	return style.Render(marker + displayPath + tail)
}

// ansiTruncate trims a string to roughly maxWidth display cells.
func ansiTruncate(s string, maxWidth int, ellipsis string) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Simple rune-based truncation; ignores ANSI since we operate on raw paths.
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	if len(ellipsis) > maxWidth {
		ellipsis = ellipsis[:maxWidth]
	}
	return string(runes[:maxWidth-len(ellipsis)]) + ellipsis
}

func NewContextPaneCmp(app *app.App) *ContextPaneCmp {
	return &ContextPaneCmp{
		app:  app,
		keys: defaultContextPaneKeys,
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

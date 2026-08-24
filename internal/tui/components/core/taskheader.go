package core

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// The task header shows, in priority order: project and
// branch, compact task title, current stage/status, model, context usage, and
// cost/budget. On constrained widths it progressively collapses in reverse
// priority order — the active stage always stays visible and the line never
// produces negative or zero-width content.

// headerSep separates header fields.
const headerSep = "  "

// segment is one header field with progressively shorter render forms. The
// first variant is the fullest; later variants are collapsed forms. droppable
// segments may be removed entirely at the narrowest widths.
type segment struct {
	priority  int // 1 = most important (order); higher collapses first
	variants  []string
	droppable bool
}

// TaskHeaderMsg pushes a fresh header projection into the component.
type TaskHeaderMsg struct {
	VM viewmodel.TaskHeaderVM
}

// TaskHeaderCmp is the task header component.
type TaskHeaderCmp interface {
	tea.Model
	SetVM(vm viewmodel.TaskHeaderVM)
}

type taskHeaderCmp struct {
	vm    viewmodel.TaskHeaderVM
	width int
}

// NewTaskHeaderCmp constructs an empty task header.
func NewTaskHeaderCmp() TaskHeaderCmp { return &taskHeaderCmp{} }

func (m *taskHeaderCmp) Init() tea.Cmd { return nil }

func (m *taskHeaderCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case TaskHeaderMsg:
		m.vm = msg.VM
	}
	return m, nil
}

func (m *taskHeaderCmp) SetVM(vm viewmodel.TaskHeaderVM) { m.vm = vm }

func (m *taskHeaderCmp) View() string {
	return RenderTaskHeader(m.vm, m.width)
}

// RenderTaskHeader renders the themed, single-line task header at the given
// width. The layout decisions are made by planTaskHeader (pure/testable); this
// only applies theme styling.
func RenderTaskHeader(vm viewmodel.TaskHeaderVM, width int) string {
	if width <= 0 {
		return ""
	}
	line := planTaskHeader(vm, width)
	t := theme.CurrentTheme()
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width).
		Background(t.BackgroundSecondary()).
		Foreground(t.Text()).
		Render(line)
}

// planTaskHeader computes the plain-text header line that fits within width,
// applying responsive collapse. Pure and deterministic.
func planTaskHeader(vm viewmodel.TaskHeaderVM, width int) string {
	if width <= 0 {
		return ""
	}
	segs := headerSegments(vm)
	return truncate(fit(segs, width), width)
}

// headerSegments builds the ordered header segments from a projection, in
// left-to-right display order. The priority on each segment (lower = retained
// longer) drives reverse-priority collapse. The stage word carries the
// mandatory-visibility priority (0) and is the only non-droppable field, so it
// is always the last survivor; the redundant-with-icon status word is a
// separate low-priority chip that drops early.
func headerSegments(vm viewmodel.TaskHeaderVM) []segment {
	segs := []segment{projectSegment(vm)}
	if s, ok := titleSegment(vm); ok {
		segs = append(segs, s)
	}
	segs = append(segs, stageSegment(vm))
	if s, ok := statusSegment(vm); ok {
		segs = append(segs, s)
	}
	if m := strings.TrimSpace(vm.Model); m != "" {
		segs = append(segs, segment{priority: 4, droppable: true, variants: []string{m}})
	}
	if forms := budgetVariants(vm); len(forms) > 0 {
		segs = append(segs, segment{priority: 5, droppable: true, variants: forms})
	}
	return segs
}

// fit greedily collapses/drops the least-important adjustable segment until the
// joined line fits within width. Non-droppable segments (project, stage) only
// shrink to their shortest variant and are never removed, so the line is always
// non-empty and the active stage always remains visible.
func fit(segs []segment, width int) string {
	idx := make([]int, len(segs))
	dropped := make([]bool, len(segs))

	render := func() string {
		parts := make([]string, 0, len(segs))
		for i, s := range segs {
			if dropped[i] {
				continue
			}
			if v := s.variants[idx[i]]; v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, headerSep)
	}

	for lipgloss.Width(render()) > width {
		ci := -1
		for i, s := range segs {
			if dropped[i] {
				continue
			}
			canCollapse := idx[i] < len(s.variants)-1
			if !canCollapse && !s.droppable {
				continue
			}
			if ci == -1 || s.priority > segs[ci].priority {
				ci = i
			}
		}
		if ci == -1 {
			break // only non-droppable segments at their shortest remain
		}
		if idx[ci] < len(segs[ci].variants)-1 {
			idx[ci]++
		} else {
			dropped[ci] = true
		}
	}
	return render()
}

func projectSegment(vm viewmodel.TaskHeaderVM) segment {
	name := strings.TrimSpace(vm.Project)
	if name == "" {
		name = "project"
	}
	base := filepath.Base(name)
	branch := strings.TrimSpace(vm.Branch)
	full, mid, small := name, base, base
	if branch != "" {
		full = name + " @" + branch
		mid = base + " @" + branch
		small = base
	}
	// Project is priority 1 (retained longest of the droppable fields), but the
	// active stage is the one element mandates must always remain visible.
	// So project is droppable as a last resort — only at pathologically narrow
	// widths where project and stage cannot both fit, and only after everything
	// else is gone, does project yield so the stage survives.
	return segment{priority: 1, droppable: true, variants: dedup(full, mid, small)}
}

func titleSegment(vm viewmodel.TaskHeaderVM) (segment, bool) {
	obj := strings.TrimSpace(vm.Objective)
	if obj == "" {
		return segment{}, false
	}
	return segment{priority: 2, droppable: true, variants: dedup(
		truncate(obj, 50), truncate(obj, 28), truncate(obj, 14),
	)}, true
}

// stageSegment is the mandatory, always-visible active stage: icon + stage word.
// It is the only non-droppable field and is retained longest.
func stageSegment(vm viewmodel.TaskHeaderVM) segment {
	stage := strings.TrimSpace(vm.Stage)
	if stage == "" {
		stage = "idle"
	}
	icon := stateIcon(vm.State)
	full := fmt.Sprintf("%s %s", icon, stage)
	small := fmt.Sprintf("%s %s", icon, truncate(stage, 6))
	return segment{priority: 0, droppable: false, variants: dedup(full, small)}
}

// statusSegment is the textual task status. It is largely redundant with the
// stage icon, so it is a low-priority chip that drops early on narrow widths.
func statusSegment(vm viewmodel.TaskHeaderVM) (segment, bool) {
	state := strings.TrimSpace(string(vm.State))
	if state == "" {
		return segment{}, false
	}
	return segment{priority: 3, droppable: true, variants: []string{state}}, true
}

// budgetVariants builds the combined context+cost indicator ("Cost and
// context become one compact budget indicator"), fullest first.
func budgetVariants(vm viewmodel.TaskHeaderVM) []string {
	ctx := ""
	pct := ""
	switch {
	case vm.ContextLimit > 0:
		p := int(float64(vm.ContextUsed) / float64(vm.ContextLimit) * 100)
		pct = fmt.Sprintf("%d%%", p)
		ctx = fmt.Sprintf("%s/%s", formatTokenCount(vm.ContextUsed), formatTokenCount(vm.ContextLimit))
	case vm.ContextUsed > 0:
		ctx = formatTokenCount(vm.ContextUsed)
	}
	cost := ""
	switch {
	case vm.CostUnknown:
		cost = "$?"
	case vm.Cost > 0:
		cost = fmt.Sprintf("$%.2f", vm.Cost)
	}
	if ctx == "" && cost == "" {
		return nil
	}
	ctxFull := ctx
	if pct != "" {
		ctxFull = strings.TrimSpace(ctx + " (" + pct + ")")
	}
	full := joinDot("Context "+ctxFull, cost)
	mid := joinDot(ctxFull, cost)
	small := joinDot(firstNonEmpty(pct, ctx), cost)
	tiny := firstNonEmpty(pct, cost, ctx)
	return dedup(full, mid, small, tiny)
}

// stateIcon returns a compact glyph for a component state.
func stateIcon(state viewmodel.ComponentState) string {
	switch state {
	case viewmodel.StateActive:
		return "●"
	case viewmodel.StateWaiting, viewmodel.StateQueued:
		return "○"
	case viewmodel.StateCompleted, viewmodel.StateValidated:
		return styles.CheckIcon
	case viewmodel.StateFailed:
		return "✗"
	case viewmodel.StateBlocked:
		return "■"
	case viewmodel.StateCancelled:
		return "×"
	case viewmodel.StateStale, viewmodel.StateUnverified:
		return "≈"
	default:
		return "•"
	}
}

// --- small pure helpers ---

func joinDot(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// dedup returns the non-empty variants in order, dropping any variant equal to
// the previous kept one so collapse steps always make progress.
func dedup(variants ...string) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		if v == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == v {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// truncate shortens s to at most max display columns, appending an ellipsis when
// content is cut. It never returns content wider than max and never negative.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	out := make([]rune, 0, max)
	w := 0
	for _, c := range s {
		cw := lipgloss.Width(string(c))
		if w+cw > max-1 {
			break
		}
		out = append(out, c)
		w += cw
	}
	return string(out) + "…"
}

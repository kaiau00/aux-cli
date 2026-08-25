// Package contextbudget renders the context as a signature budget composition
// rather than a flat file list. It shows the total
// against the limit, pressure, the largest categories, pinned/resident counts,
// and tokens saved by delta/artifact reuse — all projected from the same page
// bindings that make up the prompt manifest, so the display reconciles with
// what the model actually received.
package contextbudget

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// highPressure is the fraction of the limit above which context pressure is
// surfaced with a warning treatment.
const highPressure = 0.8

// FormatTokens renders a token count compactly (e.g. 18200 -> "18.2k").
func FormatTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return trimZero(fmt.Sprintf("%.1fM", float64(n)/1_000_000))
	case n >= 1_000:
		return trimZero(fmt.Sprintf("%.1fk", float64(n)/1_000))
	default:
		return fmt.Sprintf("%d", n)
	}
}

func trimZero(s string) string {
	s = strings.Replace(s, ".0k", "k", 1)
	s = strings.Replace(s, ".0M", "M", 1)
	return s
}

// Pressure returns pages as a fraction of what the call actually sent, in the
// range [0,1]; 0 when that total is unknown.
//
// Deliberately not a fraction of the model's context window: TotalTokens is
// an estimate over conversation-message pages only (decomposePages), which
// never includes the system prompt or tool schemas. Against a 1M window that
// ratio is structurally near zero regardless of reality, so it can never
// reach a percentage worth a warning colour. Against what this call actually
// sent, it answers what the pane exists to answer: how much of the prompt is
// tracked content you could exclude or pin.
func Pressure(vm viewmodel.ContextBudgetVM) float64 {
	if vm.CallTotalTokens <= 0 {
		return 0
	}
	return float64(vm.TotalTokens) / float64(vm.CallTotalTokens)
}

// HeaderText returns the plain-text budget header line.
//
// Labelled "Pages", not "Context": the task header and status bar already
// say "Context" for the call's real, provider-reported occupancy. This line
// is a much rougher estimate over a narrower slice (see Pressure) -- a real
// session showed 1,227 tokens of pages against 20,099 tokens of real
// occupancy for the identical call, which is the normal case (pages are one
// ingredient, not the whole prompt), not a discrepancy, but the shared label
// made it look like one.
func HeaderText(vm viewmodel.ContextBudgetVM) string {
	if vm.CallTotalTokens <= 0 {
		return fmt.Sprintf("Pages  %s", FormatTokens(vm.TotalTokens))
	}
	pct := int(Pressure(vm) * 100)
	return fmt.Sprintf("Pages  %s / %s  (%d%%)", FormatTokens(vm.TotalTokens), FormatTokens(vm.CallTotalTokens), pct)
}

// SignalsText returns the compact resident/pinned/saved signal line, or "".
func SignalsText(vm viewmodel.ContextBudgetVM) string {
	parts := []string{}
	if vm.ResidentPages > 0 {
		parts = append(parts, fmt.Sprintf("%d resident", vm.ResidentPages))
	}
	if vm.PinnedPages > 0 {
		parts = append(parts, fmt.Sprintf("%d pinned", vm.PinnedPages))
	}
	if vm.SavedTokens > 0 {
		parts = append(parts, "saved "+FormatTokens(vm.SavedTokens))
	}
	return strings.Join(parts, " · ")
}

// Render draws the budget composition, or "" when there is nothing resident
// (paging off / no bindings yet), so the caller can fall back to its file list.
func Render(vm viewmodel.ContextBudgetVM, width int) string {
	if width <= 0 || (vm.TotalTokens == 0 && len(vm.Categories) == 0) {
		return ""
	}
	t := theme.CurrentTheme()

	headColor := t.Primary()
	if Pressure(vm) >= highPressure {
		headColor = t.Warning()
	}
	rows := []string{
		lipgloss.NewStyle().Width(width).Foreground(headColor).Bold(true).Render(" " + HeaderText(vm)),
	}

	// Largest categories first (compact view).
	cats := append([]viewmodel.ContextCategoryVM(nil), vm.Categories...)
	sort.SliceStable(cats, func(i, j int) bool { return cats[i].Tokens > cats[j].Tokens })
	for _, c := range cats {
		rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(t.Text()).
			Render(" "+categoryRow(c, vm.TotalTokens, width-1)))
	}

	if sig := SignalsText(vm); sig != "" {
		rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(t.TextMuted()).Render(" "+sig))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// categoryRow renders "Label  ····  12.3k" padded to width, with the token count
// right-aligned. Deterministic for testing.
func categoryRow(c viewmodel.ContextCategoryVM, total int64, width int) string {
	value := FormatTokens(c.Tokens)
	label := c.Label
	// Reserve room for the value plus a gap; truncate the label if needed.
	gap := 2
	avail := width - lipgloss.Width(value) - gap
	if avail < 1 {
		return truncate(label+" "+value, width)
	}
	if lipgloss.Width(label) > avail {
		label = truncate(label, avail)
	}
	pad := width - lipgloss.Width(label) - lipgloss.Width(value)
	if pad < 1 {
		pad = 1
	}
	return label + strings.Repeat(" ", pad) + value
}

// expandedGroups pairs a state's label with its entries, in display order.
func expandedGroups(vm viewmodel.ContextPageListVM) []struct {
	label   string
	entries []viewmodel.ContextPageEntryVM
} {
	return []struct {
		label   string
		entries []viewmodel.ContextPageEntryVM
	}{
		{"Pinned", vm.Pinned},
		{"Resident", vm.Resident},
		{"Available", vm.Available},
		{"Evicted", vm.Evicted},
		{"Faulted", vm.Faulted},
	}
}

// RenderExpanded draws the per-page context view grouped by binding state —
// the detail behind the compact Render summary.
// Returns "" when there is nothing to show, so the caller can fall back to
// the compact view.
func RenderExpanded(vm viewmodel.ContextPageListVM, width int) string {
	if width <= 0 {
		return ""
	}
	groups := expandedGroups(vm)
	empty := true
	for _, g := range groups {
		if len(g.entries) > 0 {
			empty = false
			break
		}
	}
	if empty {
		return ""
	}

	t := theme.CurrentTheme()
	var rows []string
	for _, g := range groups {
		if len(g.entries) == 0 {
			continue
		}
		rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(t.Primary()).Bold(true).
			Render(fmt.Sprintf(" %s (%d)", g.label, len(g.entries))))
		for _, e := range g.entries {
			rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(t.Text()).
				Render("  "+categoryRow(viewmodel.ContextCategoryVM{Label: pageEntryLabel(e), Tokens: e.Tokens}, 0, width-2)))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// pageEntryLabel shows the reason alongside the stable key when present
// (e.g. why a page was evicted), so the expanded view explains itself.
func pageEntryLabel(e viewmodel.ContextPageEntryVM) string {
	if e.Reason == "" {
		return e.StableKey
	}
	return e.StableKey + " — " + e.Reason
}

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
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 {
			break
		}
		out = append(out, r)
		w += rw
	}
	return string(out) + "…"
}

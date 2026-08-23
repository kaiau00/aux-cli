// Package activity renders grouped runtime activity from
// truthful, event-backed view models. It turns mechanical tool/runtime events
// into user-understandable collapsed rows — label, status, summary, item count,
// duration, and error state — so tool-heavy work is legible without raw
// transcript noise. Errors are never collapsed away.
package activity

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// KindLabel returns the human-readable stage label for an activity kind.
func KindLabel(k viewmodel.ActivityKind) string {
	switch k {
	case viewmodel.ActivitySearching:
		return "Searching"
	case viewmodel.ActivityReading:
		return "Reading"
	case viewmodel.ActivityEditing:
		return "Editing"
	case viewmodel.ActivityTesting:
		return "Testing"
	case viewmodel.ActivityPlanning:
		return "Planning"
	case viewmodel.ActivityWaiting:
		return "Waiting"
	default:
		return "Working"
	}
}

// RowText returns the plain-text collapsed row for a group: status
// glyph, label, optional summary, item count, duration, and — always, when
// present — the error state. Pure and deterministic for testing.
func RowText(g viewmodel.ActivityGroupVM) string {
	parts := []string{stateGlyph(g.State) + " " + KindLabel(g.Kind)}
	// The error state comes first after the label so it survives width
	// truncation — an error is never collapsed away.
	if g.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%s %d failed", styles.WarningIcon, g.Errors))
	}
	if s := strings.TrimSpace(g.Summary); s != "" {
		parts = append(parts, s)
	}
	if g.Count > 1 {
		parts = append(parts, fmt.Sprintf("%d×", g.Count))
	}
	if d := formatDuration(g.DurationMS); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, "  ")
}

// Render returns the collapsed activity section: a title plus one row per group,
// each fit to width. Completed mechanical rows are muted so failures and the
// final response stay visually louder. Returns "" when there is nothing
// to show, so callers render no empty panel.
func Render(groups []viewmodel.ActivityGroupVM, width int) string {
	if len(groups) == 0 || width <= 0 {
		return ""
	}
	t := theme.CurrentTheme()
	title := lipgloss.NewStyle().
		Width(width).
		Foreground(t.Primary()).
		Bold(true).
		Render(" Activity")

	rows := make([]string, 0, len(groups)+1)
	rows = append(rows, title)
	for _, g := range groups {
		style := lipgloss.NewStyle().Width(width)
		switch {
		case g.Errors > 0 || g.State == viewmodel.StateFailed:
			style = style.Foreground(t.Error())
		case g.State == viewmodel.StateActive:
			style = style.Foreground(t.Text())
		default:
			// Completed mechanical work is quieter than live/errored rows.
			style = style.Foreground(t.TextMuted())
		}
		rows = append(rows, style.Render(" "+truncate(RowText(g), width-1)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// stateGlyph returns a compact status glyph for a component state.
func stateGlyph(state viewmodel.ComponentState) string {
	switch state {
	case viewmodel.StateActive:
		return "▶"
	case viewmodel.StateCompleted, viewmodel.StateValidated:
		return styles.CheckIcon
	case viewmodel.StateFailed:
		return "✗"
	case viewmodel.StateBlocked:
		return "⊘"
	case viewmodel.StateWaiting:
		return "◔"
	default:
		return "•"
	}
}

// formatDuration renders a millisecond duration compactly, or "" for zero.
func formatDuration(ms int64) string {
	switch {
	case ms <= 0:
		return ""
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	default:
		return fmt.Sprintf("%dm%ds", ms/60_000, (ms%60_000)/1000)
	}
}

// truncate shortens s to at most max display columns, appending an ellipsis when
// content is cut. It never returns content wider than max.
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

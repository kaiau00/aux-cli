// Package workbench renders the task-level changes (roadmapplan.md §13.8) and
// validation (§13.9) surfaces from truthful view models. It answers "what
// changed" and "does it work" without transcript search. Diff semantics keep
// conventional green/red; the validation surface never implies success without
// validated evidence — a claim is never shown as passed because the agent
// merely stopped.
package workbench

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// --- Changes (§13.8) ---

// ChangesHeaderText returns the plain-text summary line for a change set, or the
// informative "no changes yet" state when nothing has changed (never an empty
// panel). Pure and deterministic for testing.
func ChangesHeaderText(vm viewmodel.ChangeSummaryVM) string {
	if len(vm.Files) == 0 {
		return "Changes  no changes yet"
	}
	counts := []string{}
	if vm.Added > 0 {
		counts = append(counts, fmt.Sprintf("+%d", vm.Added))
	}
	if vm.Modified > 0 {
		counts = append(counts, fmt.Sprintf("~%d", vm.Modified))
	}
	if vm.Deleted > 0 {
		counts = append(counts, fmt.Sprintf("-%d", vm.Deleted))
	}
	return "Changes  " + strings.Join(counts, " ")
}

// RenderChanges renders the change summary. Additions are green and deletions
// red (§13.8: conventional diff semantics, not both remapped to amber). Before
// anything has changed, this renders nothing — every fresh task otherwise
// opened with an identical "no changes yet" block, which is boilerplate the
// panel doesn't need until there's something to report.
func RenderChanges(vm viewmodel.ChangeSummaryVM, width int) string {
	if width <= 0 || len(vm.Files) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	head := lipgloss.NewStyle().Width(width).Foreground(t.Primary()).Bold(true).
		Render(" " + ChangesHeaderText(vm))
	rows := []string{head}
	for _, f := range vm.Files {
		glyph, color := changeGlyph(f.Operation, t)
		rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(color).
			Render(" "+truncate(glyph+" "+f.Path, width-1)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func changeGlyph(op string, t theme.Theme) (string, lipgloss.AdaptiveColor) {
	switch op {
	case "add":
		return "+", t.Success()
	case "delete":
		return "-", t.Error()
	case "rename":
		return "»", t.Text()
	default: // modify
		return "~", t.Text()
	}
}

// --- Validation (§13.9) ---

// ValidationStateLabel returns the display label for a component state used on
// the validation surface.
func ValidationStateLabel(state viewmodel.ComponentState) string {
	switch state {
	case viewmodel.StateValidated:
		return "validated"
	case viewmodel.StateBlocked:
		return "blocked"
	case viewmodel.StateFailed:
		return "failed"
	case viewmodel.StateCompleted:
		return "waived"
	default:
		return "unverified"
	}
}

// CriterionRowText returns the plain-text row for one acceptance criterion.
func CriterionRowText(c viewmodel.CriterionVM) string {
	return fmt.Sprintf("%s %s  %s", validationGlyph(c.State), ValidationStateLabel(c.State), c.Description)
}

// RenderValidation renders acceptance criteria and their proof-of-done state
// (§13.9). It never renders a success treatment without a validated
// criterion. Before there are any acceptance criteria to report on, this
// renders nothing — the same boilerplate-avoidance as RenderChanges. Once a
// criterion exists, an unverified one is always shown as unverified,
// never silently hidden or implied passing.
func RenderValidation(vm viewmodel.ValidationSummaryVM, width int) string {
	if width <= 0 || len(vm.Criteria) == 0 {
		return ""
	}
	t := theme.CurrentTheme()
	head := lipgloss.NewStyle().Width(width).Foreground(t.Primary()).Bold(true).
		Render(" Validation · " + ValidationStateLabel(vm.Overall))

	rows := []string{head}
	for _, c := range vm.Criteria {
		rows = append(rows, lipgloss.NewStyle().Width(width).Foreground(validationColor(c.State, t)).
			Render(" "+truncate(CriterionRowText(c), width-1)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func validationGlyph(state viewmodel.ComponentState) string {
	switch state {
	case viewmodel.StateValidated:
		return styles.CheckIcon
	case viewmodel.StateBlocked:
		return "⊘"
	case viewmodel.StateFailed:
		return "✗"
	case viewmodel.StateCompleted:
		return "≈"
	default:
		return "○"
	}
}

// validationColor keeps a success (green) treatment reserved for validated
// evidence only; unverified is muted, never green (§13.9).
func validationColor(state viewmodel.ComponentState, t theme.Theme) lipgloss.AdaptiveColor {
	switch state {
	case viewmodel.StateValidated:
		return t.Success()
	case viewmodel.StateFailed, viewmodel.StateBlocked:
		return t.Error()
	default:
		return t.TextMuted()
	}
}

// truncate shortens s to at most max display columns with an ellipsis when cut.
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

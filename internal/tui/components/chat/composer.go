package chat

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Composer affordances: a placeholder appropriate to a
// new task versus a follow-up, and a compact shortcut hint line that changes
// with focus and send state. These are pure so they can be tested without a
// terminal; editor.go renders them.

// composerPlaceholder returns the textarea placeholder for the composer state.
func composerPlaceholder(followUp, slash bool) string {
	switch {
	case slash:
		return "Run a command…"
	case followUp:
		return "Reply or refine the task…"
	default:
		return "Describe a task for Aux…"
	}
}

// composerHint returns the compact shortcut hint line, fitted to width. It
// reflects send state (a clear disabled reason while the agent is working),
// focus, and whether the multiline and attachment affordances are available --
// so the multiline action is discoverable rather than a hidden trailing
// backslash convention.
//
// Fitting matters for layout, not just looks: the caller reserves exactly one
// row for this line, so a hint that wrapped pushed the whole screen down a row
// and scrolled the task header out of the alternate screen. Hints are dropped
// from the least important end until the line fits, and "enter send" -- the one
// affordance a user cannot guess -- is the last to go.
func composerHint(width int, focused, busy, hasAttachments bool) string {
	if busy {
		return fitHints(width, []string{"working…", "esc cancel"})
	}
	if !focused {
		return fitHints(width, []string{"type to focus the composer"})
	}
	hints := []string{"enter send", "\\+enter newline", "ctrl+e editor", "ctrl+f attach"}
	if hasAttachments {
		hints = append(hints, "ctrl+r delete")
	}
	return fitHints(width, hints)
}

// hintSeparator joins hints; it is also what makes a dropped hint invisible
// rather than leaving a dangling separator behind.
const hintSeparator = " · "

// fitHints joins as many hints as fit, dropping from the end. A single hint too
// wide to fit is truncated with an ellipsis, so the line is never taller than
// one row whatever the terminal size.
func fitHints(width int, hints []string) string {
	if width <= 0 || len(hints) == 0 {
		return ""
	}
	for n := len(hints); n > 0; n-- {
		joined := strings.Join(hints[:n], hintSeparator)
		if ansi.StringWidth(joined) <= width {
			return joined
		}
	}
	return ansi.Truncate(hints[0], width, "…")
}

// isSlashCommand reports whether the composer value is composing a slash/custom
// command (a leading "/" with no whitespace yet).
func isSlashCommand(value string) bool {
	v := strings.TrimLeft(value, " ")
	if !strings.HasPrefix(v, "/") {
		return false
	}
	return !strings.ContainsAny(v, " \n")
}

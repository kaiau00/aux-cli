package chat

import "strings"

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

// composerHint returns the compact shortcut hint line. It reflects send state
// (a clear disabled reason while the agent is working), focus, and whether the
// multiline and attachment affordances are available — so the multiline action
// is discoverable rather than a hidden trailing-backslash convention.
func composerHint(focused, busy, hasAttachments bool) string {
	if busy {
		return "working… · esc cancel"
	}
	if !focused {
		return "type to focus the composer"
	}
	hints := []string{"enter send", "\\+enter newline", "ctrl+e editor", "ctrl+f attach"}
	if hasAttachments {
		hints = append(hints, "ctrl+r delete")
	}
	return strings.Join(hints, " · ")
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

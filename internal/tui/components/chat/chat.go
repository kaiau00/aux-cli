package chat

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
)

type SendMsg struct {
	Text        string
	Attachments []message.Attachment
}

// RunCommandMsg runs a registered command typed in the composer as /ID.
// Args is the text after the command name, trimmed.
type RunCommandMsg struct {
	ID   string
	Args string
}

type SessionSelectedMsg = session.Session

type SessionClearedMsg struct{}

type EditorFocusMsg bool

// ScrollbackPrintedMsg says that part of the conversation has just been handed
// to the terminal. It is the signal that there is now history above the live
// region: before it, the region owns the screen below the startup banner and
// can be as tall as it likes; after it, the region has to sit tight against
// what was printed, or there is a block of blank rows between the two.
type ScrollbackPrintedMsg struct{}

// displayPath shortens a working directory for the startup banner. Rendering it
// raw let lipgloss wrap it, which broke the path across three lines in the
// middle of a directory name. Here the home prefix collapses to "~" and, if it
// still does not fit, leading segments are dropped: the trailing segments name
// the project, and that is the part worth keeping.
func displayPath(path, home string, width int) string {
	if width <= 0 {
		return ""
	}
	if home != "" && home != "/" && strings.HasPrefix(path, home) {
		path = "~" + path[len(home):]
	}
	if ansi.StringWidth(path) <= width {
		return path
	}

	segments := strings.Split(path, string(filepath.Separator))
	for i := 1; i < len(segments); i++ {
		candidate := ellipsis() + "/" + strings.Join(segments[i:], string(filepath.Separator))
		if ansi.StringWidth(candidate) <= width {
			return candidate
		}
	}
	// Even the last segment is too wide: keep its start rather than nothing.
	return ansi.Truncate(segments[len(segments)-1], width, ellipsis())
}

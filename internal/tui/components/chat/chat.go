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

package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/version"
)

type SendMsg struct {
	Text        string
	Attachments []message.Attachment
}

type SessionSelectedMsg = session.Session

type SessionClearedMsg struct{}

type EditorFocusMsg bool

func header(width int) string {
	return lipgloss.JoinVertical(
		lipgloss.Top,
		logo(width),
		repo(width),
		"",
		cwd(width),
	)
}

func lspsConfigured(width int) string {
	cfg := config.Get()
	title := "LSP Configuration"
	title = ansi.Truncate(title, width, "…")

	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	lsps := baseStyle.
		Width(width).
		Foreground(t.Primary()).
		Bold(true).
		Render(title)

	// Get LSP names and sort them for consistent ordering
	var lspNames []string
	for name := range cfg.LSP {
		lspNames = append(lspNames, name)
	}
	sort.Strings(lspNames)

	var lspViews []string
	for _, name := range lspNames {
		lsp := cfg.LSP[name]
		lspName := baseStyle.
			Foreground(t.Text()).
			Render(fmt.Sprintf("• %s", name))

		cmd := lsp.Command
		cmd = ansi.Truncate(cmd, width-lipgloss.Width(lspName)-3, "…")

		lspPath := baseStyle.
			Foreground(t.TextMuted()).
			Render(fmt.Sprintf(" (%s)", cmd))

		lspViews = append(lspViews,
			baseStyle.
				Width(width).
				Render(
					lipgloss.JoinHorizontal(
						lipgloss.Left,
						lspName,
						lspPath,
					),
				),
		)
	}

	return baseStyle.
		Width(width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				lsps,
				lipgloss.JoinVertical(
					lipgloss.Left,
					lspViews...,
				),
			),
		)
}

func logo(width int) string {
	logo := fmt.Sprintf("%s %s", styles.AuxIcon, "Aux")
	t := theme.CurrentTheme()
	baseStyle := styles.BaseStyle()

	versionText := baseStyle.
		Foreground(t.TextMuted()).
		Render(version.Version)

	return baseStyle.
		Bold(true).
		Width(width).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Left,
				logo,
				" ",
				versionText,
			),
		)
}

func repo(width int) string {
	repo := "https://github.com/kaiau00/aux-cli"
	t := theme.CurrentTheme()

	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(repo)
}

func cwd(width int) string {
	t := theme.CurrentTheme()
	const label = "cwd: "
	home, _ := os.UserHomeDir()
	path := displayPath(config.WorkingDirectory(), home, width-len(label))

	return styles.BaseStyle().
		Foreground(t.TextMuted()).
		Width(width).
		Render(label + path)
}

// displayPath shortens a working directory for the splash line. Rendering it
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
		candidate := "…/" + strings.Join(segments[i:], string(filepath.Separator))
		if ansi.StringWidth(candidate) <= width {
			return candidate
		}
	}
	// Even the last segment is too wide: keep its start rather than nothing.
	return ansi.Truncate(segments[len(segments)-1], width, "…")
}

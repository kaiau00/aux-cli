package chat

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/tui/components/contextbudget"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/version"
)

// The banner is printed into the terminal's scrollback once, at startup, and
// never repainted. That is what Claude Code does, measured rather than
// assumed: driving `claude` v2.1.246 in a pty with twelve lines already on the
// screen, it emitted no alternate-screen switch, no ED clear of any form and
// no mouse tracking -- it wrote its mark immediately below the existing
// output and kept a five-row live region under it.
//
// Printing rather than rendering is what makes the mark behave: it scrolls
// with the conversation, the terminal owns it, and it is still on screen after
// Aux exits. A banner drawn inside the live region would instead be repainted
// on every keystroke and would have to be given up the moment a reply needed
// the rows.

// wordmark spells AUX in quadrant blocks. Every glyph is present in both SF
// Mono and Menlo -- checked by parsing the cmap of each font, not by eye --
// and all of them are in the font-safe allowlist that TestIconsAreFontSafe
// enforces. Quadrants were the right primitive: the braille and partial-circle
// glyphs that would give a smoother mark are absent from SF Mono entirely.
var wordmark = []string{
	"▟▀▀▙ █  █ ▜▖▗▛",
	"█▄▄█ █  █  ██ ",
	"█  █ ▜▄▄▛ ▟▘▝▙",
}

// wordmarkWidth is the display width of the mark, plus the gutter between it
// and the information beside it.
const wordmarkGutter = 3

// Banner returns the startup banner for a terminal of the given width: the
// wordmark with Aux's version, the configured model and the working directory
// set beside it.
//
// Below the width the mark needs, the mark is dropped rather than wrapped --
// a wordmark broken across lines reads as corruption, and the information
// lines are the part that carries meaning.
func Banner(width int) string {
	if width <= 0 {
		return ""
	}
	info := bannerInfo(width)
	if !styles.SupportsUnicode() {
		return trimTrailing(strings.Join(info, "\n"))
	}

	markWidth := 0
	for _, line := range wordmark {
		markWidth = max(markWidth, ansi.StringWidth(line))
	}
	if width < markWidth+wordmarkGutter+minBannerInfoWidth {
		return trimTrailing(strings.Join(info, "\n"))
	}

	t := theme.CurrentTheme()
	// No background: the canvas belongs to the terminal (D17).
	mark := lipgloss.NewStyle().
		Foreground(t.Primary()).
		Render(strings.Join(wordmark, "\n"))

	// The information block is re-fitted to the room left beside the mark.
	info = bannerInfo(width - markWidth - wordmarkGutter)
	return trimTrailing(lipgloss.JoinHorizontal(
		lipgloss.Top,
		mark,
		lipgloss.NewStyle().PaddingLeft(wordmarkGutter).Render(strings.Join(info, "\n")),
	))
}

// trimTrailing drops the padding lipgloss adds to the right of each line.
// The banner is printed, so those cells become part of the user's scrollback
// permanently and come back when they select and copy it.
func trimTrailing(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n")
}

// minBannerInfoWidth is the narrowest the information block may be squeezed to
// before the mark is dropped to give it the whole width.
const minBannerInfoWidth = 24

// bannerInfo is the three lines beside the mark: what this is, what model it
// will use, and where it is pointed. Each is a fact the user would otherwise
// have to ask for, and each is read live rather than baked in.
func bannerInfo(width int) []string {
	t := theme.CurrentTheme()
	plain := lipgloss.NewStyle().Foreground(t.Text()).Bold(true)
	muted := lipgloss.NewStyle().Foreground(t.TextMuted())

	name := plain.Render(ansi.Truncate("Aux", width, ""))
	// "unknown" is what the version package reports when the linker stamped
	// nothing and there is no build info to fall back on. Printing it beside
	// the name claims a version called "unknown"; saying nothing is the
	// truthful option, and M3.5 is what makes local builds report properly.
	if v := version.Version; v != "" && v != "unknown" {
		if room := width - ansi.StringWidth("Aux") - 1; room > 0 {
			name = lipgloss.JoinHorizontal(lipgloss.Top, name,
				muted.Render(" "+ansi.Truncate(v, room, ellipsis())))
		}
	}

	lines := []string{name}
	if model := bannerModel(); model != "" {
		lines = append(lines, muted.Render(ansi.Truncate(model, width, ellipsis())))
	}
	home, _ := os.UserHomeDir()
	lines = append(lines, muted.Render(displayPath(config.WorkingDirectory(), home, width)))
	if lsp := bannerLSP(); lsp != "" {
		lines = append(lines, muted.Render(ansi.Truncate(lsp, width, ellipsis())))
	}
	return lines
}

// bannerLSP names the configured language servers. The splash this banner
// replaced listed them, and they are named nowhere else in the TUI, so
// dropping the line would have quietly removed the only place a user could
// see that an LSP was picked up at all.
func bannerLSP() string {
	cfg := config.Get()
	if cfg == nil || len(cfg.LSP) == 0 {
		return ""
	}
	names := make([]string, 0, len(cfg.LSP))
	for name := range cfg.LSP {
		names = append(names, name)
	}
	sort.Strings(names)
	return "LSP: " + strings.Join(names, ", ")
}

// ellipsis and separator keep the banner readable on a terminal that cannot
// render non-ASCII at all. The rest of the TUI spends "·" and "…" freely --
// both are font-safe -- but the banner is printed into the user's scrollback
// and stays there for the session, so it is worth being strict about here.
func ellipsis() string {
	if styles.SupportsUnicode() {
		return "…"
	}
	return "..."
}

func separator() string {
	if styles.SupportsUnicode() {
		return "·"
	}
	return "-"
}

// bannerModel names the coder agent's model and the size of its context
// window. It returns "" when no model is configured -- on a first run, before
// any key is set, there is nothing truthful to say here.
func bannerModel() string {
	cfg := config.Get()
	if cfg == nil {
		return ""
	}
	coder, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return ""
	}
	name := models.NameOf(coder.Model)
	if name == "" {
		return ""
	}
	if m, ok := models.Resolve(coder.Model); ok && m.ContextWindow > 0 {
		return fmt.Sprintf("%s %s %s context", name, separator(), contextbudget.FormatTokens(m.ContextWindow))
	}
	return name
}

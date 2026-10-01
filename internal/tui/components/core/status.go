package core

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/lsp/protocol"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

type StatusCmp interface {
	tea.Model
}

// statusCmp is the bottom status bar. It deliberately does not repeat what
// the task header already shows (project, stage, model, context, cost) --
// it only carries ambient system state the header has no room for:
// diagnostics, the help hint, and transient toast messages. See
// taskheader.go for the task/session state this bar intentionally leaves
// out.
type statusCmp struct {
	info       util.InfoMsg
	width      int
	messageTTL time.Duration
	lspClients map[string]*lsp.Client
}

// clearMessageCmd is a command that clears status messages after a timeout
func (m statusCmp) clearMessageCmd(ttl time.Duration) tea.Cmd {
	return tea.Tick(ttl, func(time.Time) tea.Msg {
		return util.ClearStatusMsg{}
	})
}

func (m statusCmp) Init() tea.Cmd {
	return nil
}

func (m statusCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case util.InfoMsg:
		m.info = msg
		ttl := msg.TTL
		if ttl == 0 {
			ttl = m.messageTTL
		}
		return m, m.clearMessageCmd(ttl)
	case util.ClearStatusMsg:
		m.info = util.InfoMsg{}
	}
	return m, nil
}

// getHelpWidget returns the help hint. It is the least important thing on
// the bar, so it is plain muted text on the bar's shared background rather
// than its own filled pill.
func getHelpWidget() string {
	t := theme.CurrentTheme()
	return styles.Padded().
		Background(t.BackgroundSecondary()).
		Foreground(t.TextMuted()).
		Render("ctrl+? help")
}

// formatTokenCount renders a token count compactly (e.g. 18200 -> "18.2K").
// Shared with taskheader.go, which is the surviving place token/context
// figures are shown.
func formatTokenCount(tokens int64) string {
	var formatted string
	switch {
	case tokens >= 1_000_000:
		formatted = fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		formatted = fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		formatted = fmt.Sprintf("%d", tokens)
	}

	if strings.HasSuffix(formatted, ".0K") {
		formatted = strings.Replace(formatted, ".0K", "K", 1)
	}
	if strings.HasSuffix(formatted, ".0M") {
		formatted = strings.Replace(formatted, ".0M", "M", 1)
	}
	return formatted
}

func (m statusCmp) View() string {
	t := theme.CurrentTheme()
	barBg := styles.Padded().Background(t.BackgroundSecondary())

	help := getHelpWidget()
	diagnostics := m.projectDiagnostics()
	diagnosticsSegment := ""
	if diagnostics != "" {
		diagnosticsSegment = barBg.Render(diagnostics)
	}

	// Budget the fixed segments before rendering any of them: help first
	// (static text, the same key works whether or not it is shown), then
	// diagnostics (a count, visible in full on the diagnostics view).
	fit := fitStatus(m.width, lipgloss.Width(help), lipgloss.Width(diagnosticsSegment))

	status := ""
	usedWidth := 0
	if fit.ShowHelp {
		status += help
		usedWidth += lipgloss.Width(help)
	}
	if !fit.ShowDiagnostics {
		diagnosticsSegment = ""
	}
	usedWidth += lipgloss.Width(diagnosticsSegment)

	availableWidth := max(0, m.width-usedWidth)

	if m.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.Background()).
			Width(availableWidth)

		switch m.info.Type {
		case util.InfoTypeInfo:
			infoStyle = infoStyle.Background(t.Info())
		case util.InfoTypeWarn:
			infoStyle = infoStyle.Background(t.Warning())
		case util.InfoTypeError:
			infoStyle = infoStyle.Background(t.Error())
		}

		infoWidth := availableWidth - 10
		// Truncate message if it's longer than available width
		msg := m.info.Msg
		if len(msg) > infoWidth && infoWidth > 0 {
			msg = msg[:infoWidth] + "..."
		}
		status += infoStyle.Render(msg)
	} else {
		// Nothing to say: fill with the plain bar background instead of a
		// separately-colored blank pill, so an idle bar stays quiet.
		status += barBg.Width(availableWidth).Render("")
	}

	status += diagnosticsSegment
	return status
}

// projectDiagnostics returns the styled diagnostics summary, or "" when
// there is nothing to report -- a clean project shows nothing on the bar
// rather than a permanent "No diagnostics".
func (m *statusCmp) projectDiagnostics() string {
	t := theme.CurrentTheme()
	bg := t.BackgroundSecondary()

	// Check if any LSP server is still initializing
	initializing := false
	for _, client := range m.lspClients {
		if client.GetServerState() == lsp.StateStarting {
			initializing = true
			break
		}
	}

	// If any server is initializing, show that status
	if initializing {
		return lipgloss.NewStyle().
			Background(bg).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s Initializing LSP...", styles.SpinnerIcon))
	}

	errorDiagnostics := []protocol.Diagnostic{}
	warnDiagnostics := []protocol.Diagnostic{}
	hintDiagnostics := []protocol.Diagnostic{}
	infoDiagnostics := []protocol.Diagnostic{}
	for _, client := range m.lspClients {
		for _, d := range client.GetDiagnostics() {
			for _, diag := range d {
				switch diag.Severity {
				case protocol.SeverityError:
					errorDiagnostics = append(errorDiagnostics, diag)
				case protocol.SeverityWarning:
					warnDiagnostics = append(warnDiagnostics, diag)
				case protocol.SeverityHint:
					hintDiagnostics = append(hintDiagnostics, diag)
				case protocol.SeverityInformation:
					infoDiagnostics = append(infoDiagnostics, diag)
				}
			}
		}
	}

	if len(errorDiagnostics) == 0 && len(warnDiagnostics) == 0 && len(hintDiagnostics) == 0 && len(infoDiagnostics) == 0 {
		return ""
	}

	diagnostics := []string{}

	if len(errorDiagnostics) > 0 {
		errStr := lipgloss.NewStyle().
			Background(bg).
			Foreground(t.Error()).
			Render(fmt.Sprintf("%s %d", styles.ErrorIcon, len(errorDiagnostics)))
		diagnostics = append(diagnostics, errStr)
	}
	if len(warnDiagnostics) > 0 {
		warnStr := lipgloss.NewStyle().
			Background(bg).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s %d", styles.WarningIcon, len(warnDiagnostics)))
		diagnostics = append(diagnostics, warnStr)
	}
	if len(hintDiagnostics) > 0 {
		hintStr := lipgloss.NewStyle().
			Background(bg).
			Foreground(t.Text()).
			Render(fmt.Sprintf("%s %d", styles.HintIcon, len(hintDiagnostics)))
		diagnostics = append(diagnostics, hintStr)
	}
	if len(infoDiagnostics) > 0 {
		infoStr := lipgloss.NewStyle().
			Background(bg).
			Foreground(t.Info()).
			Render(fmt.Sprintf("%s %d", styles.InfoIcon, len(infoDiagnostics)))
		diagnostics = append(diagnostics, infoStr)
	}

	return strings.Join(diagnostics, " ")
}

func NewStatusCmp(lspClients map[string]*lsp.Client) StatusCmp {
	return &statusCmp{
		messageTTL: 10 * time.Second,
		lspClients: lspClients,
	}
}

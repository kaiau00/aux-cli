package core

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/lsp/protocol"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

type StatusCmp interface {
	tea.Model
}

type statusCmp struct {
	info       util.InfoMsg
	width      int
	messageTTL time.Duration
	lspClients map[string]*lsp.Client
	session    session.Session
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
	case chat.SessionSelectedMsg:
		m.session = msg
	case chat.SessionClearedMsg:
		m.session = session.Session{}
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent {
			if m.session.ID == msg.Payload.ID {
				m.session = msg.Payload
			}
		}
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

var helpWidget = ""

// getHelpWidget returns the help widget with current theme colors
func getHelpWidget() string {
	t := theme.CurrentTheme()
	helpText := "ctrl+? help"

	return styles.Padded().
		Background(t.TextMuted()).
		Foreground(t.BackgroundDarker()).
		Bold(true).
		Render(helpText)
}

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

func formatTokensAndCost(tokens, contextWindow int64, cost float64) string {
	formattedTokens := formatTokenCount(tokens)
	formattedWindow := formatTokenCount(contextWindow)
	formattedCost := fmt.Sprintf("$%.2f", cost)

	percentage := 0
	if contextWindow > 0 {
		percentage = int((float64(tokens) / float64(contextWindow)) * 100)
	}

	contextPart := fmt.Sprintf("%s / %s (%d%%)", formattedTokens, formattedWindow, percentage)
	if percentage > 80 {
		contextPart = fmt.Sprintf("%s %s", styles.WarningIcon, contextPart)
	}

	return fmt.Sprintf("Context: %s, Cost: %s", contextPart, formattedCost)
}

func (m statusCmp) View() string {
	t := theme.CurrentTheme()
	modelID := config.Get().Agents[config.AgentCoder].Model
	model := models.SupportedModels[modelID]

	help := getHelpWidget()

	tokenInfo := ""
	tokensStyle := styles.Padded().
		Background(t.Text()).
		Foreground(t.BackgroundSecondary())
	if m.session.ID != "" {
		totalTokens := m.session.PromptTokens + m.session.CompletionTokens
		tokenInfo = formatTokensAndCost(totalTokens, model.ContextWindow, m.session.Cost)
		percentage := (float64(totalTokens) / float64(model.ContextWindow)) * 100
		if percentage > 80 {
			tokensStyle = tokensStyle.Background(t.Warning())
		}
	}

	diagnostics := styles.Padded().
		Background(t.BackgroundDarker()).
		Render(m.projectDiagnostics())

	// Budget the fixed segments before rendering any of them. The spacer below
	// clamps at zero, so without this the fixed parts run past the edge of the
	// terminal and it clips whatever is last -- which is the model name.
	fit := fitStatus(
		m.width,
		lipgloss.Width(help),
		lipgloss.Width(tokensStyle.Render(tokenInfo)),
		lipgloss.Width(diagnostics),
		lipgloss.Width(m.model()),
	)

	status := ""
	usedWidth := 0
	if fit.ShowHelp {
		status += help
		usedWidth += lipgloss.Width(help)
	}
	if fit.ShowTokens && tokenInfo != "" {
		rendered := tokensStyle.Render(tokenInfo)
		status += rendered
		usedWidth += lipgloss.Width(rendered)
	}
	if !fit.ShowDiagnostics {
		diagnostics = ""
	}
	modelSegment := m.modelFitted(fit.ModelBudget)
	usedWidth += lipgloss.Width(diagnostics) + lipgloss.Width(modelSegment)

	availableWidht := max(0, m.width-usedWidth)

	if m.info.Msg != "" {
		infoStyle := styles.Padded().
			Foreground(t.Background()).
			Width(availableWidht)

		switch m.info.Type {
		case util.InfoTypeInfo:
			infoStyle = infoStyle.Background(t.Info())
		case util.InfoTypeWarn:
			infoStyle = infoStyle.Background(t.Warning())
		case util.InfoTypeError:
			infoStyle = infoStyle.Background(t.Error())
		}

		infoWidth := availableWidht - 10
		// Truncate message if it's longer than available width
		msg := m.info.Msg
		if len(msg) > infoWidth && infoWidth > 0 {
			msg = msg[:infoWidth] + "..."
		}
		status += infoStyle.Render(msg)
	} else {
		status += styles.Padded().
			Foreground(t.Text()).
			Background(t.BackgroundSecondary()).
			Width(availableWidht).
			Render("")
	}

	status += diagnostics
	status += modelSegment
	return status
}

// modelFitted renders the model segment within a width budget, truncating the
// name rather than the styled string so the escape sequences stay intact.
func (m statusCmp) modelFitted(budget int) string {
	full := m.model()
	if budget <= 0 {
		return ""
	}
	if lipgloss.Width(full) <= budget {
		return full
	}

	t := theme.CurrentTheme()
	name := modelName()
	// Padded() adds one cell either side, and an ellipsis needs one more.
	room := budget - 3
	if room < 1 {
		return ""
	}
	if len(name) > room {
		name = name[:room] + "…"
	}
	return styles.Padded().
		Background(t.Secondary()).
		Foreground(t.Background()).
		Render(name)
}

// modelName is the plain, unstyled name of the coder agent's model.
func modelName() string {
	coder, ok := config.Get().Agents[config.AgentCoder]
	if !ok {
		return "Unknown"
	}
	return models.SupportedModels[coder.Model].Name
}

func (m *statusCmp) projectDiagnostics() string {
	t := theme.CurrentTheme()

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
			Background(t.BackgroundDarker()).
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
		return "No diagnostics"
	}

	diagnostics := []string{}

	if len(errorDiagnostics) > 0 {
		errStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Error()).
			Render(fmt.Sprintf("%s %d", styles.ErrorIcon, len(errorDiagnostics)))
		diagnostics = append(diagnostics, errStr)
	}
	if len(warnDiagnostics) > 0 {
		warnStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Warning()).
			Render(fmt.Sprintf("%s %d", styles.WarningIcon, len(warnDiagnostics)))
		diagnostics = append(diagnostics, warnStr)
	}
	if len(hintDiagnostics) > 0 {
		hintStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Text()).
			Render(fmt.Sprintf("%s %d", styles.HintIcon, len(hintDiagnostics)))
		diagnostics = append(diagnostics, hintStr)
	}
	if len(infoDiagnostics) > 0 {
		infoStr := lipgloss.NewStyle().
			Background(t.BackgroundDarker()).
			Foreground(t.Info()).
			Render(fmt.Sprintf("%s %d", styles.InfoIcon, len(infoDiagnostics)))
		diagnostics = append(diagnostics, infoStr)
	}

	return strings.Join(diagnostics, " ")
}

func (m statusCmp) model() string {
	t := theme.CurrentTheme()

	return styles.Padded().
		Background(t.Secondary()).
		Foreground(t.Background()).
		Render(modelName())
}

func NewStatusCmp(lspClients map[string]*lsp.Client) StatusCmp {
	helpWidget = getHelpWidget()

	return &statusCmp{
		messageTTL: 10 * time.Second,
		lspClients: lspClients,
	}
}

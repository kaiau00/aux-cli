package chat

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

type cacheItem struct {
	width   int
	content []uiMessage
}
type messagesCmp struct {
	app           *app.App
	width, height int
	viewport      viewport.Model
	session       session.Session
	messages      []message.Message
	uiMessages    []uiMessage
	currentMsgID  string
	cachedContent map[string]cacheItem
	spinner       spinner.Model
	rendering     bool
	attachments   viewport.Model

	// expandedThinking tracks which assistant messages should render their
	// full reasoning block. The default is a short preview of the latest
	// lines; Tab toggles the focused message.
	expandedThinking map[string]bool

	// workingLabelIndex advances on spinner ticks to rotate status verbs.
	workingLabelIndex int

	// rerenderPending and rerenderTailActivity coalesce streaming message
	// updates behind rerenderDebounce; see scheduleRerender.
	rerenderPending      bool
	rerenderTailActivity bool
}

// rerenderDebounce caps how often a streaming response forces a full
// conversation rejoin. renderView rejoins and re-wraps the entire
// conversation on every call, not just the changed message; measured at
// 200-700ms per call on a 400-message session. A real turn delivers a
// content delta many times a second, so calling it on every single one blocks
// the whole UI loop for most of a second at a time -- including the user's
// own scroll input, which is what made scrolling during an active turn look
// both frozen and, once the backlog cleared, buggy.
const rerenderDebounce = 100 * time.Millisecond

// rerenderPendingMsg fires the debounced re-render scheduled by
// scheduleRerender.
type rerenderPendingMsg struct{}

// scheduleRerender defers the next renderView to at most once per
// rerenderDebounce. Cheap bookkeeping (updating m.messages, invalidating
// cachedContent) still happens synchronously when an event arrives; only the
// expensive rejoin is coalesced.
func (m *messagesCmp) scheduleRerender() tea.Cmd {
	if m.rerenderPending {
		return nil
	}
	m.rerenderPending = true
	return tea.Tick(rerenderDebounce, func(time.Time) tea.Msg {
		return rerenderPendingMsg{}
	})
}

// workingStatusLabels cycles in the footer while the agent is responding,
// similar to Claude Code's rotating "Working / Searching / …" indicator.
var workingStatusLabels = []string{
	"Working",
	"Thinking",
	"Searching",
	"Finding",
	"Pondering",
	"Considering",
	"Exploring",
	"Drafting",
	"Planning",
	"Reading",
	"Analyzing",
}

type renderFinishedMsg struct{}

type MessageKeys struct {
	PageDown       key.Binding
	PageUp         key.Binding
	HalfPageUp     key.Binding
	HalfPageDown   key.Binding
	ToggleThinking key.Binding
}

var messageKeys = MessageKeys{
	PageDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("f/pgdn", "page down"),
	),
	PageUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("b/pgup", "page up"),
	),
	HalfPageUp: key.NewBinding(
		key.WithKeys("ctrl+u"),
		key.WithHelp("ctrl+u", "½ page up"),
	),
	HalfPageDown: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "½ page down"),
	),
	ToggleThinking: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "toggle reasoning"),
	),
}

func (m *messagesCmp) Init() tea.Cmd {
	return tea.Batch(m.viewport.Init(), m.spinner.Tick)
}

func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case dialog.ThemeChangedMsg:
		m.rerender()
		return m, nil
	case SessionSelectedMsg:
		if msg.ID != m.session.ID {
			cmd := m.SetSession(msg)
			return m, cmd
		}
		return m, nil
	case SessionClearedMsg:
		m.session = session.Session{}
		m.messages = make([]message.Message, 0)
		m.currentMsgID = ""
		m.rendering = false
		return m, nil

	case tea.MouseMsg:
		// Forward the wheel to the viewport. Without this the terminal keeps the
		// wheel to itself and scrolls its own scrollback, so scrolling up in a
		// conversation showed the shell commands from before Aux started rather
		// than the earlier messages.
		u, cmd := m.viewport.Update(msg)
		m.viewport = u
		return m, cmd

	case tea.KeyMsg:
		// Forward scroll keys to the viewport: PgUp/PgDn, Ctrl+U/Ctrl+D,
		// and Up/Down arrows. Without this, only the page-scroll keys work
		// and the user has no way to scroll one line at a time.
		if key.Matches(msg, messageKeys.PageUp) || key.Matches(msg, messageKeys.PageDown) ||
			key.Matches(msg, messageKeys.HalfPageUp) || key.Matches(msg, messageKeys.HalfPageDown) ||
			key.Matches(msg, m.viewport.KeyMap.Up) || key.Matches(msg, m.viewport.KeyMap.Down) {
			u, cmd := m.viewport.Update(msg)
			m.viewport = u
			cmds = append(cmds, cmd)
		}
		if key.Matches(msg, messageKeys.ToggleThinking) {
			if targetID := m.reasoningToggleTargetID(); targetID != "" {
				m.expandedThinking[targetID] = !m.expandedThinking[targetID]
				m.currentMsgID = targetID
				delete(m.cachedContent, targetID)
				m.renderView()
				return m, tea.Batch(cmds...)
			}
		}

	case renderFinishedMsg:
		m.rendering = false
		m.viewport.GotoBottom()
	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.session.ID {
			m.session = msg.Payload
			if m.session.SummaryMessageID == m.currentMsgID {
				delete(m.cachedContent, m.currentMsgID)
				m.renderView()
			}
		}
	case pubsub.Event[message.Message]:
		needsRerender := false
		if msg.Type == pubsub.CreatedEvent {
			if msg.Payload.SessionID == m.session.ID {

				messageExists := false
				for _, v := range m.messages {
					if v.ID == msg.Payload.ID {
						messageExists = true
						break
					}
				}

				if !messageExists {
					if len(m.messages) > 0 {
						lastMsgID := m.messages[len(m.messages)-1].ID
						delete(m.cachedContent, lastMsgID)
					}

					m.messages = append(m.messages, msg.Payload)
					delete(m.cachedContent, m.currentMsgID)
					m.currentMsgID = msg.Payload.ID
					needsRerender = true
				}
			}
			// There are tool calls from the child task
			for _, v := range m.messages {
				for _, c := range v.ToolCalls() {
					if c.ID == msg.Payload.SessionID {
						delete(m.cachedContent, v.ID)
						needsRerender = true
					}
				}
			}
		} else if msg.Type == pubsub.UpdatedEvent && msg.Payload.SessionID == m.session.ID {
			for i, v := range m.messages {
				if v.ID == msg.Payload.ID {
					m.messages[i] = msg.Payload
					delete(m.cachedContent, msg.Payload.ID)
					needsRerender = true
					break
				}
			}
		}
		if needsRerender {
			if len(m.messages) > 0 &&
				((msg.Type == pubsub.CreatedEvent) ||
					(msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == m.messages[len(m.messages)-1].ID)) {
				m.rerenderTailActivity = true
			}
			cmds = append(cmds, m.scheduleRerender())
		}

	case rerenderPendingMsg:
		if !m.rerenderPending {
			return m, nil
		}
		m.rerenderPending = false
		// Captured now, not when the triggering event arrived: a user who
		// scrolled up to read earlier history during the debounce window
		// must not be yanked back down by content that kept streaming in
		// underneath them.
		wasAtBottom := m.viewport.AtBottom()
		m.renderView()
		if m.rerenderTailActivity && wasAtBottom {
			m.viewport.GotoBottom()
		}
		m.rerenderTailActivity = false
		return m, nil
	}

	if _, ok := msg.(spinner.TickMsg); ok && m.IsAgentWorking() {
		m.workingLabelIndex++
	}
	spinner, cmd := m.spinner.Update(msg)
	m.spinner = spinner
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m *messagesCmp) reasoningToggleTargetID() string {
	start := len(m.messages) - 1
	for i, msg := range m.messages {
		if msg.ID == m.currentMsgID {
			start = i
			break
		}
	}
	for i := start; i >= 0; i-- {
		if isPromptReasoningAnchor(m.messages, i) && hasAnyReasoningDetails(promptReasoningMessages(m.messages, i)) {
			return m.messages[i].ID
		}
	}
	return ""
}

func (m *messagesCmp) IsAgentWorking() bool {
	return m.app.CoderAgent.IsSessionBusy(m.session.ID)
}

func formatTimeDifference(unixTime1, unixTime2 int64) string {
	diffSeconds := float64(math.Abs(float64(unixTime2 - unixTime1)))

	if diffSeconds < 60 {
		return fmt.Sprintf("%.1fs", diffSeconds)
	}

	minutes := int(diffSeconds / 60)
	seconds := int(diffSeconds) % 60
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}

func (m *messagesCmp) renderView() {
	m.uiMessages = make([]uiMessage, 0)
	pos := 0
	baseStyle := styles.BaseStyle()

	if m.width == 0 {
		return
	}
	for inx, msg := range m.messages {
		switch msg.Role {
		case message.User:
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == m.width {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			userMsg := renderUserMessage(
				msg,
				msg.ID == m.currentMsgID,
				m.width,
				pos,
			)
			m.uiMessages = append(m.uiMessages, userMsg)
			m.cachedContent[msg.ID] = cacheItem{
				width:   m.width,
				content: []uiMessage{userMsg},
			}
			pos += userMsg.height + 1 // + 1 for spacing
		case message.Assistant:
			if !isPromptReasoningAnchor(m.messages, inx) {
				continue
			}
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == m.width {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			isSummary := m.session.SummaryMessageID == msg.ID
			reasoningMessages := promptReasoningMessages(m.messages, inx)

			assistantMessages := renderAssistantMessage(
				msg,
				inx,
				m.messages,
				reasoningMessages,
				m.app.Messages,
				m.currentMsgID,
				isSummary,
				m.expandedThinking[msg.ID],
				m.spinner.View(),
				m.width,
				pos,
			)
			for _, msg := range assistantMessages {
				m.uiMessages = append(m.uiMessages, msg)
				pos += msg.height + 1 // + 1 for spacing
			}
			m.cachedContent[msg.ID] = cacheItem{
				width:   m.width,
				content: assistantMessages,
			}
		}
	}

	messages := make([]string, 0)
	for _, v := range m.uiMessages {
		messages = append(messages, lipgloss.JoinVertical(lipgloss.Left, v.content),
			baseStyle.
				Width(m.width).
				Render(
					"",
				),
		)
	}

	m.viewport.SetContent(
		baseStyle.
			Width(m.width).
			Render(
				lipgloss.JoinVertical(
					lipgloss.Top,
					messages...,
				),
			),
	)
}

func isPromptReasoningAnchor(messages []message.Message, index int) bool {
	if index < 0 || index >= len(messages) {
		return false
	}
	msg := messages[index]
	if msg.Role != message.Assistant {
		return false
	}
	for i := index + 1; i < len(messages); i++ {
		switch messages[i].Role {
		case message.User:
			return true
		case message.Assistant:
			return false
		}
	}
	return true
}

func promptReasoningMessages(messages []message.Message, anchorIndex int) []message.Message {
	if anchorIndex < 0 || anchorIndex >= len(messages) {
		return nil
	}
	start := anchorIndex
	for start > 0 {
		if messages[start-1].Role == message.User {
			break
		}
		start--
	}

	reasoningMessages := make([]message.Message, 0, anchorIndex-start+1)
	for i := start; i <= anchorIndex; i++ {
		if messages[i].Role == message.Assistant && hasReasoningDetails(messages[i]) {
			reasoningMessages = append(reasoningMessages, messages[i])
		}
	}
	return reasoningMessages
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	// Every branch renders exactly m.height rows. The alternate screen has no
	// slack: one row too many and the whole app shifts up, taking the task
	// header -- model, context budget, cost -- off the top of the screen with
	// no way to scroll back to it.
	fit := func(content string) string {
		return baseStyle.
			Width(m.width).
			Height(m.height).
			MaxHeight(m.height).
			Render(content)
	}

	if m.rendering {
		return fit(lipgloss.JoinVertical(lipgloss.Top, "Loading...", m.working()))
	}

	if len(m.messages) == 0 {
		// One row below is spoken for by the working line. MaxHeight is what
		// stops a greeting taller than the terminal from pushing the rest off
		// the top: Height alone sets a floor, not a ceiling.
		content := baseStyle.
			Width(m.width).
			Height(max(0, m.height-1)).
			MaxHeight(max(1, m.height-1)).
			Render(m.initialScreen())
		return fit(lipgloss.JoinVertical(lipgloss.Top, content, m.working()))
	}

	return fit(lipgloss.JoinVertical(lipgloss.Top, m.viewport.View(), m.working()))
}

func hasToolsWithoutResponse(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	toolResults := make([]message.ToolResult, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
		toolResults = append(toolResults, m.ToolResults()...)
	}

	for _, v := range toolCalls {
		found := false
		for _, r := range toolResults {
			if v.ID == r.ToolCallID {
				found = true
				break
			}
		}
		if !found && v.Finished {
			return true
		}
	}
	return false
}

func hasUnfinishedToolCalls(messages []message.Message) bool {
	toolCalls := make([]message.ToolCall, 0)
	for _, m := range messages {
		toolCalls = append(toolCalls, m.ToolCalls()...)
	}
	for _, v := range toolCalls {
		if !v.Finished {
			return true
		}
	}
	return false
}

// lastToolCallName returns the human-friendly name of the most recent tool
// call across the current session (or any active nested Task session). Used
// by the working footer so users see which tool is currently in flight.
func (m *messagesCmp) lastToolCallName() string {
	for i := len(m.messages) - 1; i >= 0; i-- {
		calls := m.messages[i].ToolCalls()
		if len(calls) > 0 {
			return toolName(calls[len(calls)-1].Name)
		}
	}
	return ""
}

func (m *messagesCmp) workingStatusLabel() string {
	if hasToolsWithoutResponse(m.messages) {
		return "Waiting for tool response..."
	}
	if hasUnfinishedToolCalls(m.messages) {
		if name := m.lastToolCallName(); name != "" {
			return fmt.Sprintf("Calling %s...", name)
		}
		return "Building tool call..."
	}
	idx := (m.workingLabelIndex / 12) % len(workingStatusLabels)
	return workingStatusLabels[idx] + "..."
}

func (m *messagesCmp) working() string {
	if !m.IsAgentWorking() || len(m.messages) == 0 {
		return ""
	}
	task := m.workingStatusLabel()
	if task == "" {
		return ""
	}

	t := theme.CurrentTheme()
	// This line has exactly one reserved row, and a long tool label would
	// otherwise wrap into the row the composer occupies.
	line := ansi.Truncate(fmt.Sprintf("%s %s ", m.spinner.View(), task), m.width, "…")
	return styles.BaseStyle().
		Width(m.width).
		MaxHeight(1).
		Foreground(t.Primary()).
		Bold(true).
		Render(line)
}

func (m *messagesCmp) initialScreen() string {
	baseStyle := styles.BaseStyle()
	t := theme.CurrentTheme()

	greeting := baseStyle.
		Width(m.width).
		Foreground(t.Text()).
		Render("Hello, I am Aux. How can I help?")

	prompt := baseStyle.
		Width(m.width).
		Foreground(t.TextMuted()).
		Render("Ask me to inspect this project, make a change, debug an error, or explain what you are looking at.")

	return baseStyle.Width(m.width).Render(
		lipgloss.JoinVertical(
			lipgloss.Top,
			header(m.width),
			"",
			greeting,
			prompt,
			"",
			lspsConfigured(m.width),
		),
	)
}

func (m *messagesCmp) rerender() {
	for _, msg := range m.messages {
		delete(m.cachedContent, msg.ID)
	}
	m.renderView()
}

func (m *messagesCmp) SetSize(width, height int) tea.Cmd {
	if m.width == width && m.height == height {
		return nil
	}
	m.width = width
	m.height = height
	m.viewport.Width = width
	m.viewport.Height = height - 1
	m.attachments.Width = width + 40
	m.attachments.Height = 3
	m.rerender()
	return nil
}

func (m *messagesCmp) GetSize() (int, int) {
	return m.width, m.height
}

func (m *messagesCmp) SetSession(session session.Session) tea.Cmd {
	if m.session.ID == session.ID {
		return nil
	}
	m.session = session
	messages, err := m.app.Messages.List(context.Background(), session.ID)
	if err != nil {
		return util.ReportError(err)
	}
	m.messages = messages
	if len(m.messages) > 0 {
		m.currentMsgID = m.messages[len(m.messages)-1].ID
	}
	delete(m.cachedContent, m.currentMsgID)
	m.rendering = true
	return func() tea.Msg {
		m.renderView()
		return renderFinishedMsg{}
	}
}

func (m *messagesCmp) BindingKeys() []key.Binding {
	return []key.Binding{
		messageKeys.ToggleThinking,
		m.viewport.KeyMap.PageDown,
		m.viewport.KeyMap.PageUp,
		m.viewport.KeyMap.HalfPageUp,
		m.viewport.KeyMap.HalfPageDown,
	}
}

func NewMessagesCmp(app *app.App) tea.Model {
	s := spinner.New()
	s.Spinner = spinner.Pulse
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		app:              app,
		cachedContent:    make(map[string]cacheItem),
		viewport:         vp,
		spinner:          s,
		attachments:      attachmets,
		expandedThinking: make(map[string]bool),
	}
}

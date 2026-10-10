package chat

import (
	"context"
	"fmt"
	"math"
	"strings"
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

	// rerenderPending and rerenderTailActivity coalesce streaming message
	// updates behind rerenderDebounce; see scheduleRerender.
	rerenderPending      bool
	rerenderTailActivity bool

	// printed records the messages already handed to the terminal's own
	// scrollback. Aux renders only what is still in flight; a finished message
	// is printed once, above the live region, and belongs to the terminal from
	// then on -- so it can be scrolled to, selected and copied with the
	// terminal's own tools, and it survives Aux exiting.
	//
	// The consequence is that a printed line cannot be re-wrapped. Resizing the
	// window re-wraps the live region and leaves history as it was written,
	// which is the same bargain every program that writes to scrollback makes.
	printed map[string]bool
}

// messageIsSettled reports whether the message at index i will not change
// again, and so can be handed to the terminal.
//
// A user message is settled as soon as it exists. An assistant message is
// settled when it carries a finish part -- or when a later message exists,
// because the model has moved on and nothing will be appended to this one.
// Getting this wrong in the optimistic direction prints a half-streamed reply
// and then has no way to take it back.
func messageIsSettled(messages []message.Message, i int) bool {
	if i < 0 || i >= len(messages) {
		return false
	}
	msg := messages[i]
	if msg.Role == message.User {
		return true
	}
	if msg.IsFinished() {
		return true
	}
	return i < len(messages)-1
}

// takeScrollback renders every settled message not yet handed over, marks them
// handed over, and returns the lines to print. Separated from Update so the
// decision of what reaches the terminal is testable without a terminal.
func (m *messagesCmp) takeScrollback() []string {
	if m.width == 0 {
		return nil
	}
	var out []string
	for i, msg := range m.messages {
		if m.printed[msg.ID] || !messageIsSettled(m.messages, i) {
			continue
		}
		rendered := m.renderOne(i, msg)
		m.printed[msg.ID] = true
		if rendered == "" {
			continue
		}
		out = append(out, rendered, "")
	}
	return out
}

// renderOne renders a single message exactly as the transcript would.
func (m *messagesCmp) renderOne(index int, msg message.Message) string {
	switch msg.Role {
	case message.User:
		return renderUserMessage(msg, false, m.width, 0).content
	case message.Assistant:
		var reasoning []message.Message
		if hasReasoningDetails(msg) {
			reasoning = []message.Message{msg}
		}
		parts := renderAssistantMessage(
			msg, index, m.messages, reasoning, m.app.Messages,
			m.currentMsgID, m.session.SummaryMessageID == msg.ID,
			m.expandedThinking[msg.ID], m.spinner.View(), m.width, 0,
		)
		rendered := make([]string, 0, len(parts))
		for _, p := range parts {
			rendered = append(rendered, p.content)
		}
		return strings.Join(rendered, "\n")
	}
	return ""
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

// Update handles the message, then hands over anything that settled as a
// result. Wrapping rather than draining at each return keeps it impossible for
// a path to settle a message and not print it -- a created message, a finished
// stream and a session being opened all arrive differently.
func (m *messagesCmp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.updateInner(msg)
	if lines := m.takeScrollback(); len(lines) > 0 {
		for len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		// The printed messages have to leave the live region, or they show
		// twice until the next render.
		m.renderView()
		cmd = tea.Batch(cmd, tea.Println(strings.Join(lines, "\n")))
	}
	return model, cmd
}

func (m *messagesCmp) updateInner(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		m.printed = make(map[string]bool)
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
	// The nearest assistant message carrying model thinking. It used to be the
	// nearest turn anchor, because a whole turn's reasoning was collapsed into
	// one message; now thinking belongs to the message that produced it.
	for i := start; i >= 0; i-- {
		if m.messages[i].Role == message.Assistant && hasReasoningDetails(m.messages[i]) {
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
		// Settled messages live in the terminal's scrollback; rendering them
		// here too would show everything twice.
		if m.printed[msg.ID] {
			continue
		}
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
			// Every assistant message renders. Only the last one of a turn used
			// to, which folded the prose and tool calls of all the others into a
			// single "reasoning hidden" line.
			if cache, ok := m.cachedContent[msg.ID]; ok && cache.width == m.width {
				m.uiMessages = append(m.uiMessages, cache.content...)
				continue
			}
			isSummary := m.session.SummaryMessageID == msg.ID
			// A message's own thinking, if it has any. It used to be every
			// assistant message in the turn, because the turn collapsed into
			// one rendered message.
			var reasoningMessages []message.Message
			if hasReasoningDetails(msg) {
				reasoningMessages = []message.Message{msg}
			}

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
			// Only cache a message that will not change again. An in-flight
			// reply grows on every delta, and a cached copy of an earlier
			// delta is what the debounced re-render would then show: the
			// stream appeared to stop at whatever arrived first.
			if messageIsSettled(m.messages, inx) {
				m.cachedContent[msg.ID] = cacheItem{
					width:   m.width,
					content: assistantMessages,
				}
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

	content := baseStyle.
		Width(m.width).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Top,
				messages...,
			),
		)
	m.viewport.SetContent(content)

	// Only as tall as the content, up to the allocation. A viewport pads to
	// its height, so a fixed height here is a fixed block of blank rows
	// whenever the live region holds less than its allocation -- which, now
	// that settled messages go to the terminal's scrollback, is most of the
	// time.
	ceiling := max(1, m.height-1)
	if len(m.uiMessages) == 0 {
		m.viewport.Height = 0
		return
	}
	m.viewport.Height = min(ceiling, lipgloss.Height(content))
}

func (m *messagesCmp) View() string {
	baseStyle := styles.BaseStyle()

	// The live region is as tall as what is in it and no taller. Aux manages
	// only the bottom of the screen; the conversation above it belongs to the
	// terminal's scrollback. Padding to the allocation put a block of blank
	// rows between the two -- nine of them on a 30-row terminal.
	//
	// MaxHeight is a ceiling, Height is a floor. This wants the ceiling only.
	fit := func(parts ...string) string {
		content := lipgloss.JoinVertical(lipgloss.Top, parts...)
		if strings.TrimSpace(ansi.Strip(content)) == "" {
			return ""
		}
		return baseStyle.
			Width(m.width).
			MaxHeight(max(1, m.height)).
			Render(content)
	}

	if m.rendering {
		return fit("Loading...", m.working())
	}

	if len(m.messages) == 0 {
		return fit(m.initialScreen(), m.working())
	}

	if m.viewport.Height <= 0 {
		// Nothing in flight: the whole conversation is in scrollback.
		return fit(m.working())
	}
	return fit(m.viewport.View(), m.working())
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
	if m.isWritingResponse() {
		return "Responding..."
	}
	return "Thinking..."
}

// isWritingResponse reports whether the newest assistant message already holds
// text and has not finished, which means the model is streaming prose rather
// than still deciding what to do.
//
// This exists so the label says something observed. It replaces eleven verbs
// ("Working", "Searching", "Reading", ...) that were cycled on a spinner tick:
// the UI claimed to be searching when nothing was being searched, and reading
// when nothing was being read. The two branches above this one were always
// real; now all four are.
func (m *messagesCmp) isWritingResponse() bool {
	for i := len(m.messages) - 1; i >= 0; i-- {
		msg := m.messages[i]
		if msg.Role != message.Assistant {
			continue
		}
		return !msg.IsFinished() && msg.Content().String() != ""
	}
	return false
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
	// The viewport's height is set from its content in renderView, not from
	// the allocation. Fixing it here is what padded the live region out to its
	// full allocation and left a block of blank rows between the conversation
	// in scrollback and the composer.
	m.viewport.Height = max(1, height-1)
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
	// A different session has a different history, so nothing carries over.
	// Opening a session prints it into scrollback, which is how --continue and
	// the picker hand you back what you were doing.
	m.printed = make(map[string]bool)
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
	s.Spinner = spinner.Spinner{
		Frames: styles.WorkingSpinnerFrames,
		FPS:    styles.WorkingSpinnerFPS,
	}
	vp := viewport.New(0, 0)
	attachmets := viewport.New(0, 0)
	vp.KeyMap.PageUp = messageKeys.PageUp
	vp.KeyMap.PageDown = messageKeys.PageDown
	vp.KeyMap.HalfPageUp = messageKeys.HalfPageUp
	vp.KeyMap.HalfPageDown = messageKeys.HalfPageDown
	return &messagesCmp{
		app:              app,
		printed:          make(map[string]bool),
		cachedContent:    make(map[string]cacheItem),
		viewport:         vp,
		spinner:          s,
		attachments:      attachmets,
		expandedThinking: make(map[string]bool),
	}
}

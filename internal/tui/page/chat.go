package page

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/completions"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
	"github.com/kaiau00/aux-cli/internal/tui/layout"
	"github.com/kaiau00/aux-cli/internal/tui/util"
)

var ChatPage PageID = "chat"

// narrowWidthThreshold is the terminal width below which the split layout
// drops the context panel to a single conversation column. Below this width the panel is only reachable via the
// context drawer, never fully lost.
const narrowWidthThreshold = 80

// composerMinHeight is the smallest the editor panel can be and still show
// both of its rules, one row of input, and the shortcut hint.
const composerMinHeight = 4

type chatPage struct {
	app                   *app.App
	editor                layout.Container
	messages              layout.Container
	contextPane           *chat.ContextPaneCmp
	contextPaneContainer  layout.Container
	layout                layout.SplitPaneLayout
	session               session.Session
	completionDialog      dialog.CompletionDialog
	showCompletionDialog  bool
	composer              interface{ Value() string }
	commandCompletion     dialog.CompletionDialog
	showCommandCompletion bool
	showContextDrawer     bool
	// width/height are the live region Aux manages; termWidth/termHeight are
	// the terminal itself, which overlays may use in full.
	width, height         int
	termWidth, termHeight int
}

type ChatKeyMap struct {
	ShowCompletionDialog key.Binding
	NewSession           key.Binding
	Cancel               key.Binding
	ToggleContextDrawer  key.Binding
}

var keyMap = ChatKeyMap{
	ShowCompletionDialog: key.NewBinding(
		key.WithKeys("@"),
		key.WithHelp("@", "Complete"),
	),
	NewSession: key.NewBinding(
		key.WithKeys("ctrl+n"),
		key.WithHelp("ctrl+n", "new session"),
	),
	Cancel: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	ToggleContextDrawer: key.NewBinding(
		key.WithKeys("ctrl+g"),
		key.WithHelp("ctrl+g", "context drawer"),
	),
}

func (p *chatPage) Init() tea.Cmd {
	cmds := []tea.Cmd{
		p.layout.Init(),
		p.completionDialog.Init(),
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	// The page is the only authority on how tall the live region is. The split
	// layout sizes itself from any WindowSizeMsg it receives, so it has to be
	// handed the capped height rather than the terminal's -- otherwise it
	// reclaims the whole screen and there is nothing left for the terminal to
	// scroll.
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.termWidth, p.termHeight = ws.Width, ws.Height
		ws.Height = liveRegionHeight(ws.Height)
		msg = ws
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		cmds = append(cmds, p.layout.SetSize(msg.Width, msg.Height))
	case dialog.CompletionDialogCloseMsg:
		p.showCompletionDialog = false
		p.showCommandCompletion = false
	case chat.SendMsg:
		cmd := p.sendMessage(msg.Text, msg.Attachments)
		if cmd != nil {
			return p, cmd
		}
	case dialog.CommandRunCustomMsg:
		// Check if the agent is busy before executing custom commands
		if p.app.CoderAgent.IsBusy() {
			return p, util.ReportWarn("Agent is busy, please wait before executing a command...")
		}

		// Process the command content with arguments if any
		content := msg.Content
		if msg.Args != nil {
			// Replace all named arguments with their values
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}
		}

		// Handle custom command execution
		cmd := p.sendMessage(content, nil)
		if cmd != nil {
			return p, cmd
		}
	case chat.ExcludePathMsg:
		if p.contextPane == nil {
			return p, nil
		}
		if matched := p.contextPane.ExcludePath(msg.Path); matched == 0 {
			// Reporting the miss matters: a mistyped path would otherwise be
			// indistinguishable from a successful exclusion.
			return p, util.ReportWarn(fmt.Sprintf("%s is not in the current context", msg.Path))
		}
		return p, util.ReportInfo(fmt.Sprintf("Dropped %s from context", msg.Path))
	case chat.SessionSelectedMsg:
		p.session = msg
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keyMap.ShowCompletionDialog) && !p.showCommandCompletion:
			p.showCompletionDialog = true
			// Continue sending keys to layout->chat
		case msg.String() == "/" && !p.showCompletionDialog && p.editor.Focused() && p.composer.Value() == "":
			p.showCommandCompletion = true
		case key.Matches(msg, keyMap.NewSession):
			p.session = session.Session{}
			return p, tea.Batch(
				util.CmdHandler(chat.SessionClearedMsg{}),
			)
		case key.Matches(msg, keyMap.Cancel):
			// Esc closes the topmost thing first. The drawer is checked before
			// cancellation because otherwise, once a session exists, the
			// cancel branch always won and the drawer could only be closed
			// with its own toggle.
			if p.showContextDrawer {
				p.showContextDrawer = false
				return p, nil
			}
			if p.session.ID != "" {
				// Cancel the current session's generation process
				// This allows users to interrupt long-running operations
				p.app.CoderAgent.Cancel(p.session.ID)
				return p, nil
			}
		case key.Matches(msg, keyMap.ToggleContextDrawer):
			p.showContextDrawer = !p.showContextDrawer
			return p, nil
		}
	}
	if p.showCompletionDialog || p.showCommandCompletion {
		active := &p.completionDialog
		if p.showCommandCompletion {
			active = &p.commandCompletion
		}
		context, contextCmd := (*active).Update(msg)
		*active = context.(dialog.CompletionDialog)
		cmds = append(cmds, contextCmd)

		// Doesn't forward event if enter or tab is pressed
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "enter" || keyMsg.String() == "tab" {
				return p, tea.Batch(cmds...)
			}
		}
	}

	u, cmd := p.layout.Update(msg)
	cmds = append(cmds, cmd)
	p.layout = u.(layout.SplitPaneLayout)

	// Sync editor focus to the context pane so its letter hotkeys do not
	// fire while the user is typing into the editor.
	if p.contextPane != nil {
		p.contextPane.SetEditorFocused(p.editor.Focused())
	}

	return p, tea.Batch(cmds...)
}

func (p *chatPage) sendMessage(text string, attachments []message.Attachment) tea.Cmd {
	var cmds []tea.Cmd
	if p.session.ID == "" {
		sess, err := p.app.Sessions.Create(context.Background(), "New Session")
		if err != nil {
			return util.ReportError(err)
		}

		p.session = sess
		cmds = append(cmds, util.CmdHandler(chat.SessionSelectedMsg(sess)))
	}

	_, err := p.app.CoderAgent.Run(context.Background(), p.session.ID, text, attachments...)
	if err != nil {
		return util.ReportError(err)
	}
	return tea.Batch(cmds...)
}

func (p *chatPage) SetSize(width, height int) tea.Cmd {
	p.termWidth, p.termHeight = width, height
	p.width, p.height = width, liveRegionHeight(height)
	return p.layout.SetSize(p.width, p.height)
}

// liveRegionHeight is how many rows Aux manages at the bottom of the screen.
// Everything above belongs to the terminal.
//
// The cap is what makes the conversation visible: without it the live region
// fills the window, the terminal has nothing to scroll, and printing to
// scrollback buys nothing. Below the cap a small terminal gets everything it
// has, because a composer that does not fit is worse than a short history.
func liveRegionHeight(terminalHeight int) int {
	const maxLiveRows = 12
	if terminalHeight <= 0 {
		return 0
	}
	return min(maxLiveRows, terminalHeight)
}

func (p *chatPage) GetSize() (int, int) {
	return p.layout.GetSize()
}

func (p *chatPage) View() string {
	layoutView := p.layout.View()

	// The drawer and the completion popups are placed onto this view and
	// bounded by it. The live region is only a few rows, so without growing it
	// first the drawer renders as a clipped sliver.
	if p.showContextDrawer || p.showCompletionDialog || p.showCommandCompletion {
		// Upwards, for the same reason as the overlays in tui.go: the composer
		// stays on the bottom row it was already on.
		layoutView = lipgloss.NewStyle().
			Height(max(1, p.termHeight)).
			AlignVertical(lipgloss.Bottom).
			Render(layoutView)
	}

	if p.showCompletionDialog || p.showCommandCompletion {
		active := p.completionDialog
		if p.showCommandCompletion {
			active = p.commandCompletion
		}
		_, layoutHeight := p.layout.GetSize()
		editorWidth, editorHeight := p.editor.GetSize()

		active.SetWidth(editorWidth)
		overlay := active.View()

		layoutView = layout.PlaceOverlay(
			0,
			layoutHeight-editorHeight-lipgloss.Height(overlay),
			overlay,
			layoutView,
			false,
		)
	}

	// Below narrowWidthThreshold the split layout drops the context panel
	// entirely; the drawer is what makes it reachable
	// again instead of simply being lost.
	if p.showContextDrawer && p.contextPaneContainer != nil && p.width > 0 {
		const preferredDrawerWidth = 60
		const minUsableDrawerWidth = 20
		drawerWidth := p.width - 4
		if drawerWidth > preferredDrawerWidth {
			drawerWidth = preferredDrawerWidth
		}
		if drawerWidth < minUsableDrawerWidth {
			drawerWidth = p.width
		}
		// The drawer is an overlay, so it may be as tall as the terminal even
		// though the live region is not; opening it grows the managed region.
		p.contextPaneContainer.SetSize(drawerWidth, max(p.height, p.termHeight))
		overlay := p.contextPaneContainer.View()
		layoutView = layout.PlaceOverlay(
			p.width-lipgloss.Width(overlay),
			0,
			overlay,
			layoutView,
			true,
		)
	}

	return layoutView
}

func (p *chatPage) BindingKeys() []key.Binding {
	bindings := layout.KeyMapToSlice(keyMap)
	bindings = append(bindings, p.messages.BindingKeys()...)
	bindings = append(bindings, p.editor.BindingKeys()...)
	// The context pane's hotkeys (cross off, pin, expand, dashboard URL) are
	// live whenever the editor isn't focused, so the help overlay has to list
	// them too — otherwise they work but are undiscoverable.
	if p.contextPane != nil {
		bindings = append(bindings, p.contextPane.BindingKeys()...)
	}
	return bindings
}

func NewChatPage(app *app.App, commands *dialog.CommandRegistry) tea.Model {
	cg := completions.NewFileAndFolderContextGroup()
	completionDialog := dialog.NewCompletionDialogCmp(cg, "No file matches found")
	commandCompletion := dialog.NewCompletionDialogCmp(completions.NewCommandsGroup(commands), "No matching command")

	messagesContainer := layout.NewContainer(
		chat.NewMessagesCmp(app),
		layout.WithPadding(1, 1, 0, 1),
		// Collapses when nothing is in flight, instead of padding out to its
		// allocation and separating the composer from the conversation.
		layout.WithTightHeight(),
	)
	editor := chat.NewEditorCmp(app, commands)
	editorContainer := layout.NewContainer(
		editor,
		// Ruled above and below. The composer is the one thing on screen the
		// user acts through, and a single rule above it left it running into
		// the rows beneath; measuring Claude Code showed the same two-rule
		// frame, which is the shape people recognise.
		layout.WithBorder(true, false, true, false),
	)
	contextPane := chat.NewContextPaneCmp(app)
	contextPaneContainer := layout.NewContainer(
		contextPane,
		layout.WithPadding(1, 1, 1, 1),
	)
	return &chatPage{
		app:                  app,
		editor:               editorContainer,
		messages:             messagesContainer,
		contextPane:          contextPane,
		contextPaneContainer: contextPaneContainer,
		completionDialog:     completionDialog,
		composer:             editor.(interface{ Value() string }),
		commandCompletion:    commandCompletion,
		layout: layout.NewSplitPane(
			layout.WithLeftPanel(messagesContainer),
			layout.WithRightPanel(contextPaneContainer),
			layout.WithBottomPanel(editorContainer),
			// The composer needs four rows: its two rules, one line of input,
			// and the shortcut hint. Below that the vertical ratio alone would
			// under-allocate and the composer would overflow its panel.
			layout.WithMinBottomHeight(composerMinHeight),
			// The context pane is never a column now. The live region is a few
			// rows at the bottom of the screen, which cannot host a full-height
			// panel, so ctrl+g opens it as an overlay instead -- which is what
			// the drawer was built for when narrow terminals dropped it.
			layout.WithCollapseRightBelow(math.MaxInt32),
			// The live region is as tall as the composer plus whatever is in
			// flight, not as tall as the screen.
			layout.WithTightVerticalFit(),
		),
	}
}

package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/app"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/agent"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/tui/components/chat"
	"github.com/kaiau00/aux-cli/internal/tui/components/core"
	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
	"github.com/kaiau00/aux-cli/internal/tui/layout"
	"github.com/kaiau00/aux-cli/internal/tui/page"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/tui/util"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

type keyMap struct {
	Logs          key.Binding
	Quit          key.Binding
	Help          key.Binding
	SwitchSession key.Binding
	Commands      key.Binding
	Filepicker    key.Binding
	Models        key.Binding
	SwitchTheme   key.Binding
}

// excludeCommandID names the command that drops a file from context. It is
// special-cased on submit because, unlike every other argument-taking command,
// it performs a local action instead of sending a prompt.
const excludeCommandID = "exclude"

type startCompactSessionMsg struct{}

// These open overlays from the /help, /model, and /sessions commands, whose
// handlers cannot reach the model state directly.
type (
	toggleHelpMsg      struct{}
	openModelDialogMsg struct{}
)

// OpenSessionDialogMsg opens the session picker. Exported because `--resume`
// with no id sends it from cmd/root.go once the program is running; inside the
// TUI it is what /sessions and Ctrl+S both raise.
type OpenSessionDialogMsg struct{}

const (
	quitKey = "q"
)

var keys = keyMap{
	Logs: key.NewBinding(
		key.WithKeys("ctrl+l"),
		key.WithHelp("ctrl+l", "logs"),
	),

	Quit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("ctrl+_", "ctrl+h"),
		key.WithHelp("ctrl+?", "toggle help"),
	),

	SwitchSession: key.NewBinding(
		key.WithKeys("ctrl+s"),
		key.WithHelp("ctrl+s", "switch session"),
	),

	Commands: key.NewBinding(
		key.WithKeys("ctrl+k"),
		key.WithHelp("ctrl+k", "commands"),
	),
	Filepicker: key.NewBinding(
		key.WithKeys("ctrl+f"),
		key.WithHelp("ctrl+f", "select files to upload"),
	),
	Models: key.NewBinding(
		key.WithKeys("ctrl+o"),
		key.WithHelp("ctrl+o", "model selection"),
	),

	SwitchTheme: key.NewBinding(
		key.WithKeys("ctrl+t"),
		key.WithHelp("ctrl+t", "switch theme"),
	),
}

var helpEsc = key.NewBinding(
	key.WithKeys("?"),
	key.WithHelp("?", "toggle help"),
)

var returnKey = key.NewBinding(
	key.WithKeys("esc"),
	key.WithHelp("esc", "close"),
)

var logsKeyReturnKey = key.NewBinding(
	key.WithKeys("esc", "backspace", quitKey),
	key.WithHelp("esc/q", "go back"),
)

type appModel struct {
	width, height int
	bannerPrinted bool
	bannerRows    int
	// conversationPrinted goes true the first time any of the conversation is
	// handed to the terminal. Until then there is nothing above the live
	// region, so the region can own the whole screen below the banner.
	conversationPrinted bool
	currentPage         page.PageID
	previousPage        page.PageID
	pages               map[page.PageID]tea.Model
	loadedPages         map[page.PageID]bool
	header              core.TaskHeaderCmp
	status              core.StatusCmp
	app                 *app.App
	selectedSession     session.Session

	showPermissions bool
	permissions     dialog.PermissionDialogCmp

	showHelp bool
	help     dialog.HelpCmp

	showQuit bool
	quit     dialog.QuitDialog

	showSessionDialog bool
	sessionDialog     dialog.SessionDialog

	showCommandDialog bool
	commandDialog     dialog.CommandDialog
	commands          *dialog.CommandRegistry

	showModelDialog bool
	modelDialog     dialog.ModelDialog

	showInitDialog bool
	initDialog     dialog.InitDialogCmp

	showFilepicker bool
	filepicker     dialog.FilepickerCmp

	showThemeDialog bool
	themeDialog     dialog.ThemeDialog

	showMultiArgumentsDialog bool
	multiArgumentsDialog     dialog.MultiArgumentsDialogCmp

	isCompacting      bool
	compactingMessage string
}

func (a appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmd := a.pages[a.currentPage].Init()
	a.loadedPages[a.currentPage] = true
	cmds = append(cmds, cmd)
	cmd = a.header.Init()
	cmds = append(cmds, cmd)
	cmd = a.status.Init()
	cmds = append(cmds, cmd)
	cmd = a.quit.Init()
	cmds = append(cmds, cmd)
	cmd = a.help.Init()
	cmds = append(cmds, cmd)
	cmd = a.sessionDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.commandDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.modelDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.initDialog.Init()
	cmds = append(cmds, cmd)
	cmd = a.filepicker.Init()
	cmds = append(cmds, cmd)
	cmd = a.themeDialog.Init()
	cmds = append(cmds, cmd)

	// Check if we should show the init dialog
	cmds = append(cmds, func() tea.Msg {
		shouldShow, err := config.ShouldShowInitDialog()
		if err != nil {
			return util.InfoMsg{
				Type: util.InfoTypeError,
				Msg:  "Failed to check init status: " + err.Error(),
			}
		}
		return dialog.ShowInitDialogMsg{Show: shouldShow}
	})

	return tea.Batch(cmds...)
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case chat.ScrollbackPrintedMsg:
		// There is history above the region now, so it stops owning the screen.
		a.conversationPrinted = true
		return a, nil

	case tea.WindowSizeMsg:
		terminalHeight := msg.Height

		// The banner is measured before anything is sized, because its rows
		// come off the screen the live region has to work with. Printed at
		// the end of this case, once the children know their size.
		var bannerCmd tea.Cmd
		if !a.bannerPrinted && msg.Width > 0 && terminalHeight > 0 {
			a.bannerPrinted = true
			if banner := chat.Banner(msg.Width); banner != "" {
				a.bannerRows = lipgloss.Height(banner)
				bannerCmd = tea.Println(banner)
			}
		}

		// Everything downstream is sized from the region, not from the
		// terminal, so that the page's own growth for the drawer and the
		// completion popups lands on exactly the height this model pads to.
		// When the two disagreed, opening and closing the drawer left the
		// composer four rows -- the banner's height -- above the bottom.
		// Narrowing the window re-wraps every row the previous frame left on
		// screen. Bubble Tea's inline renderer moves its cursor up by the
		// number of *lines* it last rendered rather than the number of rows
		// those lines now occupy, so it lands short, paints over the wrong
		// rows and leaves the ones it missed behind -- a stale copy of the
		// composer above the real one. Reported from a real terminal as a
		// duplicate that depended on the window's size; reproduced in a pty by
		// shrinking 100 columns to 70, where the 30-cell tail of the old rule
		// is still visible above the stale row.
		//
		// The renderer resets its cache on a resize but not its line count, so
		// there is no way to correct the arithmetic from here. Clearing is the
		// one lever available. It costs the conversation its place on screen --
		// it is still in the terminal's scrollback, one scroll up -- and it is
		// done only when the width actually shrinks: widening cannot re-wrap
		// anything, because every line already fitted the narrower width.
		if a.width > 0 && msg.Width < a.width {
			cmds = append(cmds, tea.ClearScreen)
		}

		region := terminalHeight
		if !a.conversationPrinted && a.bannerRows > 0 {
			region = max(1, terminalHeight-a.bannerRows)
		}
		msg.Height = region - 2 // Make space for the task header and status bar
		a.width, a.height = msg.Width, msg.Height

		// Dialogs own the whole region while they are up -- the header and the
		// status line are not drawn under them -- so they are told the
		// region's height rather than the page's. Handing them region-2 made
		// them two rows too tall for the region they were placed on, which
		// PlaceOverlay then expanded past the terminal.
		dialogMsg := msg
		dialogMsg.Height = region

		h, _ := a.header.Update(msg)
		a.header = h.(core.TaskHeaderCmp)

		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		cmds = append(cmds, cmd)

		prm, permCmd := a.permissions.Update(dialogMsg)
		a.permissions = prm.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permCmd)

		help, helpCmd := a.help.Update(dialogMsg)
		a.help = help.(dialog.HelpCmp)
		cmds = append(cmds, helpCmd)

		session, sessionCmd := a.sessionDialog.Update(dialogMsg)
		a.sessionDialog = session.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)

		command, commandCmd := a.commandDialog.Update(dialogMsg)
		a.commandDialog = command.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)

		filepicker, filepickerCmd := a.filepicker.Update(dialogMsg)
		a.filepicker = filepicker.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

		// The model and theme pickers were never given the terminal's size at
		// all, so they rendered at their unsized defaults: the model picker
		// came out 47 cells wide and overflowed every terminal narrower than
		// that. Found by widening the audit's size grid.
		models, modelsCmd := a.modelDialog.Update(dialogMsg)
		a.modelDialog = models.(dialog.ModelDialog)
		cmds = append(cmds, modelsCmd)

		themes, themesCmd := a.themeDialog.Update(dialogMsg)
		a.themeDialog = themes.(dialog.ThemeDialog)
		cmds = append(cmds, themesCmd)

		a.initDialog.SetSize(dialogMsg.Width, dialogMsg.Height)

		// Sized whether or not it is showing. Inside the `if` it only learned
		// the size when the terminal was resized while it was already open,
		// so the first render after opening used the unsized default.
		a.multiArgumentsDialog.SetSize(dialogMsg.Width, dialogMsg.Height)
		if a.showMultiArgumentsDialog {
			args, argsCmd := a.multiArgumentsDialog.Update(dialogMsg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			cmds = append(cmds, argsCmd, a.multiArgumentsDialog.Init())
		}

		if bannerCmd != nil {
			cmds = append(cmds, bannerCmd)
		}

		return a, tea.Batch(cmds...)
	// Status
	case util.InfoMsg:
		s, cmd := a.status.Update(msg)
		a.status = s.(core.StatusCmp)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)
	case pubsub.Event[logging.LogMessage]:
		if msg.Payload.Persist {
			switch msg.Payload.Level {
			case "error":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeError,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			case "info":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)

			case "warn":
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeWarn,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})

				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			default:
				s, cmd := a.status.Update(util.InfoMsg{
					Type: util.InfoTypeInfo,
					Msg:  msg.Payload.Message,
					TTL:  msg.Payload.PersistTime,
				})
				a.status = s.(core.StatusCmp)
				cmds = append(cmds, cmd)
			}
		}
	case util.ClearStatusMsg:
		s, _ := a.status.Update(msg)
		a.status = s.(core.StatusCmp)

	// Permission
	case pubsub.Event[permission.PermissionRequest]:
		a.showPermissions = true
		return a, a.permissions.SetPermissions(msg.Payload)
	case dialog.PermissionResponseMsg:
		var cmd tea.Cmd
		switch msg.Action {
		case dialog.PermissionAllow:
			a.app.Permissions.Grant(msg.Permission)
		case dialog.PermissionAllowForSession:
			a.app.Permissions.GrantPersistant(msg.Permission)
		case dialog.PermissionDeny:
			a.app.Permissions.Deny(msg.Permission)
		}
		a.showPermissions = false
		return a, cmd

	case page.PageChangeMsg:
		return a, a.moveToPage(msg.ID)

	case dialog.CloseQuitMsg:
		a.showQuit = false
		return a, nil

	case dialog.CloseSessionDialogMsg:
		a.showSessionDialog = false
		return a, nil

	case dialog.CloseCommandDialogMsg:
		a.showCommandDialog = false
		return a, nil

	case startCompactSessionMsg:
		// Start compacting the current session
		a.isCompacting = true
		a.compactingMessage = "Starting summarization..."

		if a.selectedSession.ID == "" {
			a.isCompacting = false
			return a, util.ReportWarn("No active session to summarize")
		}

		// Start the summarization process
		return a, func() tea.Msg {
			ctx := context.Background()
			a.app.CoderAgent.Summarize(ctx, a.selectedSession.ID)
			return nil
		}

	case pubsub.Event[agent.AgentEvent]:
		payload := msg.Payload
		if payload.Error != nil {
			a.isCompacting = false
			return a, util.ReportError(payload.Error)
		}

		a.compactingMessage = payload.Progress

		if payload.Done && payload.Type == agent.AgentEventTypeSummarize {
			a.isCompacting = false
			return a, util.ReportInfo("Session summarization complete")
		} else if payload.Done && payload.Type == agent.AgentEventTypeResponse && a.selectedSession.ID != "" {
			model := a.app.CoderAgent.Model()
			contextWindow := model.ContextWindow
			tokens := a.selectedSession.ContextTokens
			if (tokens >= int64(float64(contextWindow)*0.95)) && config.Get().AutoCompact {
				return a, util.CmdHandler(startCompactSessionMsg{})
			}
		}
		// Continue listening for events
		return a, nil

	case dialog.CloseThemeDialogMsg:
		a.showThemeDialog = false
		return a, nil

	case dialog.ThemeChangedMsg:
		a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
		a.showThemeDialog = false
		return a, tea.Batch(cmd, util.ReportInfo("Theme changed to: "+msg.ThemeName))

	case dialog.CloseModelDialogMsg:
		a.showModelDialog = false
		return a, nil

	case dialog.ModelSelectedMsg:
		a.showModelDialog = false

		model, err := a.app.CoderAgent.Update(config.AgentCoder, msg.Model.ID)
		if err != nil {
			return a, util.ReportError(err)
		}

		return a, util.ReportInfo(fmt.Sprintf("Model changed to %s", model.Name))

	case dialog.ShowInitDialogMsg:
		a.showInitDialog = msg.Show
		return a, nil

	case dialog.CloseInitDialogMsg:
		a.showInitDialog = false
		if msg.Initialize {
			// Run the initialization command
			if cmd, ok := a.commands.Find("init"); ok {
				// Mark the project as initialized
				if err := config.MarkProjectInitialized(); err != nil {
					return a, util.ReportError(err)
				}
				return a, cmd.Handler(cmd)
			}
		} else {
			// Mark the project as initialized without running the command
			if err := config.MarkProjectInitialized(); err != nil {
				return a, util.ReportError(err)
			}
		}
		return a, nil

	case chat.SessionSelectedMsg:
		a.selectedSession = msg
		a.sessionDialog.SetSelectedSession(msg.ID)

	case pubsub.Event[session.Session]:
		if msg.Type == pubsub.UpdatedEvent && msg.Payload.ID == a.selectedSession.ID {
			a.selectedSession = msg.Payload
		}
	case dialog.SessionSelectedMsg:
		a.showSessionDialog = false
		if a.currentPage == page.ChatPage {
			return a, util.CmdHandler(chat.SessionSelectedMsg(msg.Session))
		}
		return a, nil

	case chat.RunCommandMsg:
		cmd, ok := a.commands.Find(msg.ID)
		if !ok {
			return a, util.ReportWarn(fmt.Sprintf("unknown command /%s; Ctrl+K lists commands", msg.ID))
		}
		if msg.Args != "" {
			if cmd.ArgHandler == nil {
				return a, util.ReportWarn(fmt.Sprintf("/%s takes no arguments", msg.ID))
			}
			return a, cmd.ArgHandler(msg.Args)
		}
		if cmd.Handler != nil {
			return a, cmd.Handler(cmd)
		}
		return a, nil

	case toggleHelpMsg:
		if !a.showQuit {
			a.showHelp = !a.showHelp
		}
		return a, nil

	case openModelDialogMsg:
		if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions {
			a.showModelDialog = true
		}
		return a, nil

	case OpenSessionDialogMsg:
		return a, a.openSessionDialog()

	case dialog.CommandSelectedMsg:
		a.showCommandDialog = false
		// Execute the command handler if available
		if msg.Command.Handler != nil {
			return a, msg.Command.Handler(msg.Command)
		}
		return a, util.ReportInfo("Command selected: " + msg.Command.Title)

	case dialog.ShowMultiArgumentsDialogMsg:
		// Show multi-arguments dialog
		a.multiArgumentsDialog = dialog.NewMultiArgumentsDialogCmp(msg.CommandID, msg.Content, msg.ArgNames)
		a.showMultiArgumentsDialog = true
		return a, a.multiArgumentsDialog.Init()

	case dialog.CloseMultiArgumentsDialogMsg:
		// Close multi-arguments dialog
		a.showMultiArgumentsDialog = false

		// The exclude command performs a local action rather than sending a
		// prompt, so it never reaches the template-substitution path below.
		if msg.Submit && msg.CommandID == excludeCommandID {
			return a, util.CmdHandler(chat.ExcludePathMsg{Path: msg.Args["path"]})
		}

		// If submitted, replace all named arguments and run the command
		if msg.Submit {
			content := msg.Content

			// Replace each named argument with its value
			for name, value := range msg.Args {
				placeholder := "$" + name
				content = strings.ReplaceAll(content, placeholder, value)
			}

			// Execute the command with arguments
			return a, util.CmdHandler(dialog.CommandRunCustomMsg{
				Content: content,
				Args:    msg.Args,
			})
		}
		return a, nil

	case tea.KeyMsg:
		// If multi-arguments dialog is open, let it handle the key press first
		if a.showMultiArgumentsDialog {
			args, cmd := a.multiArgumentsDialog.Update(msg)
			a.multiArgumentsDialog = args.(dialog.MultiArgumentsDialogCmp)
			return a, cmd
		}

		switch {

		case key.Matches(msg, keys.Quit):
			a.showQuit = !a.showQuit
			if a.showHelp {
				a.showHelp = false
			}
			if a.showSessionDialog {
				a.showSessionDialog = false
			}
			if a.showCommandDialog {
				a.showCommandDialog = false
			}
			if a.showFilepicker {
				a.showFilepicker = false
				a.filepicker.ToggleFilepicker(a.showFilepicker)
			}
			if a.showModelDialog {
				a.showModelDialog = false
			}
			if a.showMultiArgumentsDialog {
				a.showMultiArgumentsDialog = false
			}
			return a, nil
		case key.Matches(msg, keys.SwitchSession):
			if !a.showCommandDialog {
				return a, a.openSessionDialog()
			}
			return a, nil
		case key.Matches(msg, keys.Commands):
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showThemeDialog && !a.showFilepicker {
				// Show commands dialog
				if len(a.commands.Commands()) == 0 {
					return a, util.ReportWarn("No commands available")
				}
				a.commandDialog.SetCommands(a.commands.Commands())
				a.showCommandDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.Models):
			if a.showModelDialog {
				a.showModelDialog = false
				return a, nil
			}
			if a.currentPage == page.ChatPage && !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				a.showModelDialog = true
				return a, nil
			}
			return a, nil
		case key.Matches(msg, keys.SwitchTheme):
			if !a.showQuit && !a.showPermissions && !a.showSessionDialog && !a.showCommandDialog {
				// Show theme switcher dialog
				a.showThemeDialog = true
				// Theme list is dynamically loaded by the dialog component
				return a, a.themeDialog.Init()
			}
			return a, nil
		case key.Matches(msg, returnKey) || key.Matches(msg):
			if msg.String() == quitKey {
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			} else if !a.filepicker.IsCWDFocused() {
				if a.showQuit {
					a.showQuit = !a.showQuit
					return a, nil
				}
				if a.showHelp {
					a.showHelp = !a.showHelp
					return a, nil
				}
				if a.showInitDialog {
					a.showInitDialog = false
					// Mark the project as initialized without running the command
					if err := config.MarkProjectInitialized(); err != nil {
						return a, util.ReportError(err)
					}
					return a, nil
				}
				if a.showFilepicker {
					a.showFilepicker = false
					a.filepicker.ToggleFilepicker(a.showFilepicker)
					return a, nil
				}
				if a.currentPage == page.LogsPage {
					return a, a.moveToPage(page.ChatPage)
				}
			}
		case key.Matches(msg, keys.Logs):
			return a, a.moveToPage(page.LogsPage)
		case key.Matches(msg, keys.Help):
			if a.showQuit {
				return a, nil
			}
			a.showHelp = !a.showHelp
			return a, nil
		case key.Matches(msg, helpEsc):
			if a.app.CoderAgent.IsBusy() {
				if a.showQuit {
					return a, nil
				}
				a.showHelp = !a.showHelp
				return a, nil
			}
		case key.Matches(msg, keys.Filepicker):
			a.showFilepicker = !a.showFilepicker
			a.filepicker.ToggleFilepicker(a.showFilepicker)
			return a, nil
		}
	default:
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)

	}

	if a.showFilepicker {
		f, filepickerCmd := a.filepicker.Update(msg)
		a.filepicker = f.(dialog.FilepickerCmp)
		cmds = append(cmds, filepickerCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showQuit {
		q, quitCmd := a.quit.Update(msg)
		a.quit = q.(dialog.QuitDialog)
		cmds = append(cmds, quitCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}
	if a.showPermissions {
		d, permissionsCmd := a.permissions.Update(msg)
		a.permissions = d.(dialog.PermissionDialogCmp)
		cmds = append(cmds, permissionsCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showSessionDialog {
		d, sessionCmd := a.sessionDialog.Update(msg)
		a.sessionDialog = d.(dialog.SessionDialog)
		cmds = append(cmds, sessionCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showCommandDialog {
		d, commandCmd := a.commandDialog.Update(msg)
		a.commandDialog = d.(dialog.CommandDialog)
		cmds = append(cmds, commandCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showModelDialog {
		d, modelCmd := a.modelDialog.Update(msg)
		a.modelDialog = d.(dialog.ModelDialog)
		cmds = append(cmds, modelCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showInitDialog {
		d, initCmd := a.initDialog.Update(msg)
		a.initDialog = d.(dialog.InitDialogCmp)
		cmds = append(cmds, initCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	if a.showThemeDialog {
		d, themeCmd := a.themeDialog.Update(msg)
		a.themeDialog = d.(dialog.ThemeDialog)
		cmds = append(cmds, themeCmd)
		// Only block key messages send all other messages down
		if _, ok := msg.(tea.KeyMsg); ok {
			return a, tea.Batch(cmds...)
		}
	}

	s, _ := a.status.Update(msg)
	a.status = s.(core.StatusCmp)
	a.pages[a.currentPage], cmd = a.pages[a.currentPage].Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

// headerVM projects the current TUI state into a task-header view model.
// Every field traces to real runtime state — working
// directory, configured model, the coder agent's busy state, and the selected
// session's token/cost totals — never fabricated.
func (a appModel) headerVM() viewmodel.TaskHeaderVM {
	cfg := config.Get()
	modelName := ""
	var ctxLimit int64
	if coder, ok := cfg.Agents[config.AgentCoder]; ok {
		m, _ := models.Resolve(coder.Model)
		modelName = models.NameOf(coder.Model)
		ctxLimit = m.ContextWindow
	}

	stage, state := "idle", viewmodel.StateWaiting
	if a.app != nil && a.app.CoderAgent != nil && a.app.CoderAgent.IsBusy() {
		stage, state = "working", viewmodel.StateActive
	}

	vm := viewmodel.TaskHeaderVM{
		Project:      filepath.Base(config.WorkingDirectory()),
		Objective:    a.selectedSession.Title,
		Stage:        stage,
		State:        state,
		Model:        modelName,
		ContextLimit: ctxLimit,
	}
	if a.selectedSession.ID != "" {
		// Occupancy, not lifetime spend: PromptTokens/CompletionTokens sum
		// every call, so a session that resends the same conversation each turn
		// reads several times over what the window actually holds.
		vm.ContextUsed = a.selectedSession.ContextTokens
		vm.Cost = a.selectedSession.Cost
	}
	return vm
}

// RegisterCommand adds a command to the command dialog and the composer.
func (a *appModel) RegisterCommand(cmd dialog.Command) {
	a.commands.Register(cmd)
}

// openSessionDialog loads the sessions and shows the picker. Ctrl+S and
// /sessions share it.
func (a *appModel) openSessionDialog() tea.Cmd {
	if a.currentPage != page.ChatPage || a.showQuit || a.showPermissions {
		return nil
	}
	sessions, err := a.app.Sessions.List(context.Background())
	if err != nil {
		return util.ReportError(err)
	}
	if len(sessions) == 0 {
		return util.ReportWarn("No sessions available")
	}
	a.sessionDialog.SetSessions(sessions)
	a.showSessionDialog = true
	return nil
}

func (a *appModel) moveToPage(pageID page.PageID) tea.Cmd {
	if a.app.CoderAgent.IsBusy() {
		// For now we don't move to any page if the agent is busy
		return util.ReportWarn("Agent is busy, please wait...")
	}

	var cmds []tea.Cmd
	if _, ok := a.loadedPages[pageID]; !ok {
		cmd := a.pages[pageID].Init()
		cmds = append(cmds, cmd)
		a.loadedPages[pageID] = true
	}
	a.previousPage = a.currentPage
	a.currentPage = pageID
	if sizable, ok := a.pages[a.currentPage].(layout.Sizeable); ok {
		cmd := sizable.SetSize(a.width, a.height)
		cmds = append(cmds, cmd)
	}

	return tea.Batch(cmds...)
}

// hasOverlay reports whether any dialog, picker or status overlay is showing.
// Each is placed onto the assembled view and bounded by it, so the view has to
// be the size of the screen while one is up rather than the size of the live
// region.
func (a appModel) hasOverlay() bool {
	return a.showPermissions ||
		a.showHelp ||
		a.showQuit ||
		a.showSessionDialog ||
		a.showCommandDialog ||
		a.showModelDialog ||
		a.showInitDialog ||
		a.showFilepicker ||
		a.showThemeDialog ||
		a.showMultiArgumentsDialog ||
		a.isCompacting
}

// managedRegionHeight is how tall the live region renders, or 0 to let it be
// as tall as its content.
//
// Until any of the conversation has been printed there is nothing above the
// region, so it owns the whole screen below the banner: the composer sits on
// the terminal's last row and Aux fills the terminal from the moment it
// starts, without the alternate screen. Sizing it here rather than padding
// the banner print means a resize recomputes it, which the padding could not.
//
// It also removes a whole class of defect. An overlay needs most of the
// screen, so with a collapsed region one had to be grown and then shrunk
// again -- and Bubble Tea's inline renderer cannot scroll the terminal back
// down, so closing a dialog left the composer stranded near the top with the
// rest of the screen blank. Found by driving the real binary in a pty. While
// the region already owns the screen there is nothing to grow, so nothing
// drifts.
//
// Once the conversation is printed the region goes tight against it: a floor
// here instead would put a block of blank rows between the history and the
// composer, which is the defect #53 was raised to fix.
func (a appModel) managedRegionHeight() int {
	// a.height was already set from the region, banner subtracted, when the
	// terminal reported its size -- so the region is simply that plus the two
	// rows the header and status line take. Subtracting the banner again here
	// took it off twice and left the bottom four rows of the screen empty.
	region := a.height + 2
	if region <= 0 {
		return 0
	}
	if !a.conversationPrinted && a.bannerRows > 0 {
		return region
	}
	if a.hasOverlay() {
		return region
	}
	return 0
}

// centreOverlay centres a dialog on the managed region.
//
// It refuses to place one the region cannot hold, and says so instead. The
// placement expands the view to fit whatever it is given, so an oversized
// dialog used to push the rendered region past the terminal and lose its own
// title off the top and its buttons off the bottom -- and a prompt whose
// buttons are off the screen cannot be answered. Measured at 40x14, where the
// region is ten rows: the model picker wanted fourteen and the init dialog
// twenty-three.
func centreOverlay(appView, overlay string) string {
	regionHeight := lipgloss.Height(appView)
	regionWidth := lipgloss.Width(appView)
	// PlaceOverlay draws a drop shadow around what it is given, and that costs
	// two rows and a column: the shadow background is built with `i <=
	// fgHeight` and a trailing newline, so it is two lines taller than the
	// dialog. Checking the bare dialog made a 16-row one "fit" a 16-row region
	// and then render 18.
	const shadowRows, shadowCols = 2, 1
	if lipgloss.Height(overlay)+shadowRows > regionHeight ||
		lipgloss.Width(overlay)+shadowCols > regionWidth {
		overlay = tooSmallNotice(regionWidth-shadowCols, regionHeight-shadowRows)
	}
	row := regionHeight/2 - lipgloss.Height(overlay)/2
	col := regionWidth/2 - lipgloss.Width(overlay)/2
	return layout.PlaceOverlay(col, row, overlay, appView, true)
}

// tooSmallNotice is what a dialog becomes when the terminal is too small to
// show it. Saying so is the honest option: a clipped dialog looks like a
// rendering fault, and a clipped prompt cannot be answered at all.
func tooSmallNotice(width, height int) string {
	t := theme.CurrentTheme()
	// Short enough to fit a 40-column terminal without being cut, which is
	// the width this notice exists for.
	const message = "Terminal too small"
	const hint = "Resize or press esc"
	// width-5: the border and the overlay's drop shadow. The padding is
	// counted inside Width by lipgloss, so it is not subtracted again here.
	inner := max(10, min(max(lipgloss.Width(message), lipgloss.Width(hint))+2, width-5))
	// Truncated to the padded content width, or lipgloss wraps the lines
	// instead of cutting them and the notice doubles in height.
	text := max(1, inner-2)
	body := lipgloss.JoinVertical(lipgloss.Center,
		ansi.Truncate(message, text, "\u2026"),
		ansi.Truncate(hint, text, "\u2026"),
	)
	notice := lipgloss.NewStyle().
		Width(inner).
		Padding(0, 1).
		Align(lipgloss.Center).
		Foreground(t.Text()).
		Background(t.Background()).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.BorderFocused()).
		BorderBackground(t.Background()).
		Render(body)
	// Even the notice has to fit; at that point there is nothing to do but
	// show the words without a frame.
	if lipgloss.Height(notice) > height || lipgloss.Width(notice) > width {
		return lipgloss.NewStyle().
			Width(max(1, min(lipgloss.Width(message), width))).
			MaxHeight(max(1, height)).
			Foreground(t.Text()).
			Render(message)
	}
	return notice
}

func (a appModel) baseView() string {
	// The task header projects truthful runtime state:
	// project, active stage, model, context, and cost. It renders only once the
	// terminal width is known.
	a.header.SetVM(a.headerVM())

	// The header sits below the composer, not above it. Claude Code puts its
	// run information -- mode, model, effort -- under the prompt, and measuring
	// it showed why: the composer's rule is the top edge of the live region, so
	// everything Aux repaints is in one block beneath the printed
	// conversation instead of being split either side of it.
	components := []string{a.pages[a.currentPage].View()}
	if header := a.header.View(); header != "" {
		components = append(components, header)
	}
	components = append(components, a.status.View())
	return lipgloss.JoinVertical(lipgloss.Top, components...)
}

func (a appModel) View() string {
	appView := a.baseView()

	// Every overlay below is centred on appView and clipped to it, so the
	// region has to be as tall as the screen before one is placed. A clipped
	// permission prompt gates every tool call and cannot be answered, so this
	// is a correctness rule, not a cosmetic one.
	//
	// Grown upwards: the composer sits on the bottom row, and padding
	// underneath moved it to the top of the screen with the dialog hanging
	// below it.
	if height := a.managedRegionHeight(); height > 0 {
		appView = lipgloss.NewStyle().
			Height(height).
			AlignVertical(lipgloss.Bottom).
			Render(appView)
	}

	// While a dialog is up it has the region to itself. Centring an opaque box
	// over the composer left fragments of the composer's full-width rules
	// showing in the columns the box did not cover, and at 80x24 the box and
	// those rules overlapped outright. Measured in a pty, not reasoned about.
	if a.hasOverlay() {
		appView = lipgloss.NewStyle().
			Width(max(1, a.width)).
			Height(lipgloss.Height(appView)).
			Render("")
	}

	if a.showPermissions {
		overlay := a.permissions.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showFilepicker {
		overlay := a.filepicker.View()
		appView = centreOverlay(appView, overlay)

	}

	// Show compacting status overlay
	if a.isCompacting {
		t := theme.CurrentTheme()
		style := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.BorderFocused()).
			BorderBackground(t.Background()).
			Padding(1, 2).
			Background(t.Background()).
			Foreground(t.Text())

		overlay := style.Render("Summarizing\n" + a.compactingMessage)
		appView = centreOverlay(appView, overlay)
	}

	if a.showHelp {
		bindings := layout.KeyMapToSlice(keys)
		if p, ok := a.pages[a.currentPage].(layout.Bindings); ok {
			bindings = append(bindings, p.BindingKeys()...)
		}
		if a.showPermissions {
			bindings = append(bindings, a.permissions.BindingKeys()...)
		}
		if a.currentPage == page.LogsPage {
			bindings = append(bindings, logsKeyReturnKey)
		}
		if !a.app.CoderAgent.IsBusy() {
			bindings = append(bindings, helpEsc)
		}
		a.help.SetBindings(bindings)

		overlay := a.help.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showQuit {
		overlay := a.quit.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showSessionDialog {
		overlay := a.sessionDialog.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showModelDialog {
		overlay := a.modelDialog.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showCommandDialog {
		overlay := a.commandDialog.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showInitDialog {
		// Through centreOverlay like every other dialog. This one centred
		// itself against a.width/a.height rather than the region, and so
		// skipped the check that the region can hold it: at 40x14 it rendered
		// twenty-three rows into a ten-row region.
		appView = centreOverlay(appView, a.initDialog.View())
	}

	if a.showThemeDialog {
		overlay := a.themeDialog.View()
		appView = centreOverlay(appView, overlay)
	}

	if a.showMultiArgumentsDialog {
		overlay := a.multiArgumentsDialog.View()
		appView = centreOverlay(appView, overlay)
	}

	// Trimmed at the one place everything leaves this model. A line padded to
	// exactly the terminal's width is never cleared by the renderer and wraps
	// the moment the window narrows, which left a stale copy of the composer
	// on screen. See styles.TrimTrailingSpace.
	return styles.TrimTrailingSpace(appView)
}

func New(app *app.App) tea.Model {
	startPage := page.ChatPage
	commands := dialog.NewCommandRegistry()
	model := &appModel{
		currentPage:   startPage,
		loadedPages:   make(map[page.PageID]bool),
		header:        core.NewTaskHeaderCmp(),
		status:        core.NewStatusCmp(app.LSPClients),
		help:          dialog.NewHelpCmp(),
		quit:          dialog.NewQuitCmp(),
		sessionDialog: dialog.NewSessionDialogCmp(),
		commandDialog: dialog.NewCommandDialogCmp(),
		modelDialog:   dialog.NewModelDialogCmp(),
		permissions:   dialog.NewPermissionDialogCmp(),
		initDialog:    dialog.NewInitDialogCmp(),
		themeDialog:   dialog.NewThemeDialogCmp(),
		app:           app,
		commands:      commands,
		pages: map[page.PageID]tea.Model{
			page.ChatPage: page.NewChatPage(app, commands),
			page.LogsPage: page.NewLogsPage(),
		},
		filepicker: dialog.NewFilepickerCmp(app),
	}

	model.RegisterCommand(dialog.Command{
		ID:          "init",
		Title:       "Initialize Project",
		Description: "Create/update the Aux.md memory file",
		Handler: func(cmd dialog.Command) tea.Cmd {
			prompt := `Please analyze this codebase and create an Aux.md file containing:
1. Build/lint/test commands - especially for running a single test
2. Code style guidelines including imports, formatting, types, naming conventions, error handling, etc.

The file you create will be given to agentic coding agents (such as yourself) that operate in this repository. Make it about 20 lines long.
If there's already a aux.md, improve it.
If there are Cursor rules (in .cursor/rules/ or .cursorrules) or Copilot rules (in .github/copilot-instructions.md), make sure to include them.`
			return tea.Batch(
				util.CmdHandler(chat.SendMsg{
					Text: prompt,
				}),
			)
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "compact",
		Title:       "Compact Session",
		Description: "Summarize the current session and create a new one with the summary",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return func() tea.Msg {
				return startCompactSessionMsg{}
			}
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          excludeCommandID,
		Title:       "Drop File From Context",
		Description: "Stop sending an already-read file to the model on later turns",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(dialog.ShowMultiArgumentsDialogMsg{
				CommandID: excludeCommandID,
				ArgNames:  []string{"path"},
			})
		},
		ArgHandler: func(args string) tea.Cmd {
			return util.CmdHandler(chat.ExcludePathMsg{Path: args})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "help",
		Title:       "Toggle Help",
		Description: "Show or hide the keyboard shortcuts",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(toggleHelpMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "model",
		Title:       "Switch Model",
		Description: "Choose the model for this project",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(openModelDialogMsg{})
		},
	})

	model.RegisterCommand(dialog.Command{
		ID:          "sessions",
		Title:       "Switch Session",
		Description: "Open a previous session",
		Handler: func(cmd dialog.Command) tea.Cmd {
			return util.CmdHandler(OpenSessionDialogMsg{})
		},
	})
	// Load custom commands
	customCommands, err := dialog.LoadCustomCommands()
	if err != nil {
		logging.Warn("Failed to load custom commands", "error", err)
	} else {
		for _, cmd := range customCommands {
			model.RegisterCommand(cmd)
		}
	}

	return model
}

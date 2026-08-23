package welcome

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aux-ai/aux-cli/internal/config"
	"github.com/aux-ai/aux-cli/internal/dashboard"
	"github.com/aux-ai/aux-cli/internal/message"
	"github.com/aux-ai/aux-cli/internal/session"
)

const (
	flagFilename = "intro_shown"

	banner = `
 █████╗ ██╗   ██╗██╗  ██╗
██╔══██╗██║   ██║╚██╗██╔╝
███████║██║   ██║ ╚███╔╝
██╔══██║██║   ██║ ██╔██╗
██║  ██║╚██████╔╝██╔╝ ██╗
╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═╝
`
)

// Result describes the outcome of the first-boot welcome flow.
type Result struct {
	// Session is the welcome session to auto-select on startup, or empty
	// if this was not a first boot (or creation failed).
	Session session.Session
	// Shown indicates whether the welcome flow actually created a session.
	Shown bool
}

// ShouldShow returns true if this is the first time aux is starting on
// this machine (no intro flag file exists yet).
func ShouldShow() bool {
	cfg := config.Get()
	if cfg == nil || cfg.Data.Directory == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(cfg.Data.Directory, flagFilename))
	return os.IsNotExist(err)
}

// MaybeShow creates the welcome session and message on first boot.
// It returns a Result describing what happened. Errors are logged but
// never fatal: a failed welcome must not block startup.
func MaybeShow(ctx context.Context, sessions session.Service, messages message.Service, dash *dashboard.Server) Result {
	if !ShouldShow() {
		return Result{}
	}

	body := buildIntroBody(dash)

	title := "Welcome to Aux"
	sess, err := sessions.Create(ctx, title)
	if err != nil {
		return Result{}
	}

	_, err = messages.Create(ctx, sess.ID, message.CreateMessageParams{
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.TextContent{Text: body},
		},
	})
	if err != nil {
		return Result{}
	}

	// Mark the first-boot flag so we never show it again. Use MkdirAll
	// defensively in case the data dir hasn't been created yet.
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" {
		_ = os.MkdirAll(cfg.Data.Directory, 0o755)
		// Best effort: this flag only suppresses the intro on later starts. If it
		// cannot be written the intro shows again, which is mildly annoying and
		// never wrong -- and failing the first run over it would be worse.
		_ = os.WriteFile(filepath.Join(cfg.Data.Directory, flagFilename), []byte{}, 0o644)
	}

	return Result{
		Session: sess,
		Shown:   true,
	}
}

// dataDirForDisplay names where Aux keeps its local state, so the intro can be
// concrete about it rather than vague.
func dataDirForDisplay() string {
	if cfg := config.Get(); cfg != nil && cfg.Data.Directory != "" {
		return cfg.Data.Directory
	}
	return ".aux"
}

func buildIntroBody(dash *dashboard.Server) string {
	var b strings.Builder
	b.WriteString("```\n")
	b.WriteString(strings.TrimPrefix(banner, "\n"))
	b.WriteString("\n```\n\n")
	b.WriteString("Hey, I'm **Aux** — an agentic coding assistant for your terminal.\n\n")

	b.WriteString("**Quick start**\n")
	b.WriteString("- Type a request and press `enter` to send it.\n")
	b.WriteString("- Use `\\` + `enter` to add a new line without sending.\n")
	b.WriteString("- Press `@` to attach files, `ctrl+o` to switch models, `ctrl+t` to switch themes.\n")
	b.WriteString("- Press `ctrl+k` for commands (init, compact, …), `ctrl+s` to switch sessions.\n\n")

	if dash != nil && dash.URL() != "" {
		b.WriteString("**Dashboard**\n")
		b.WriteString(fmt.Sprintf("A live dashboard is available at: %s\n", dash.URL()))
		b.WriteString("Open it in your browser to watch the session unfold in real time.\n\n")
	} else {
		b.WriteString("**Dashboard**\n")
		b.WriteString("The local dashboard is currently disabled. Enable it with `dashboard.enabled: true` in your aux config to get a live web view of this session.\n\n")
	}

	// What a new user's first question actually is. Aux reads their files and
	// sends them to a provider, so saying so plainly on first boot is part of
	// the product rather than an appendix to it.
	b.WriteString("**What I do with your code**\n")
	b.WriteString("- Files I read are sent to your configured model provider as context. The right-hand pane lists everything currently in that context, and `x` drops a file from it.\n")
	b.WriteString("- Running commands, editing files, and fetching URLs each ask first. Approving one command approves that command, not every later one.\n")
	b.WriteString("- Reads outside this project directory ask before they happen.\n")
	b.WriteString(fmt.Sprintf("- Sessions, messages, and costs are stored locally in `%s`. Nothing is sent anywhere else.\n\n", dataDirForDisplay()))

	b.WriteString("Just say hi or describe what you'd like to build — I'll take it from here. ")
	return b.String()
}

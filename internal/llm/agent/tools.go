package agent

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/permission"
)

func CoderAgentTools(
	deps Deps,
	permissions permission.Service,
	history history.Service,
	lspClients map[string]*lsp.Client,
	hookRegistry *hooks.Registry,
	impactSvc *impact.Service,
) []tools.BaseTool {
	ctx := context.Background()
	// MCP tools are deliberately absent here. Discovering them meant waiting
	// for every configured server to answer before the agent could be built --
	// and the TUI is built after the agent, so Aux drew nothing at all until
	// then: nine seconds of a fourteen-second startup on one `npx -y` server.
	// They are resolved per turn instead, through Deps.LateTools.
	var otherTools []tools.BaseTool
	if len(lspClients) > 0 {
		otherTools = append(otherTools, tools.NewDiagnosticsTool(lspClients))
	}
	base := []tools.BaseTool{
		tools.NewBashTool(permissions),
		tools.NewEditTool(lspClients, permissions, history),
		tools.NewFetchTool(permissions),
		tools.NewGlobTool(permissions),
		tools.NewGrepTool(permissions),
		tools.NewLsTool(permissions),
		tools.NewSourcegraphTool(),
		tools.NewViewTool(lspClients, permissions),
		tools.NewPatchTool(lspClients, permissions, history),
		tools.NewWriteTool(lspClients, permissions, history),
		NewAgentTool(deps, hookRegistry, permissions, impactSvc, lspClients),
	}
	// Per-page exclusion is only useful if the party generating the context can
	// reach it, so the tool is offered whenever there is a page store to write
	// to and history to resolve paths against.
	if deps.Pages != nil && deps.Messages != nil {
		base = append(base, tools.NewContextExcludeTool(
			deps.Pages,
			newReadHistory(deps.Messages, tools.ResolveWorkingDir(ctx)),
		))
	}
	return append(base, otherTools...)
}

// TaskAgentTools is the read-only tool set given to subagents. permissions is
// required for the same reason the coder agent needs it: a subagent reading
// outside its working directory must prompt rather than silently succeed.
func TaskAgentTools(lspClients map[string]*lsp.Client, permissions permission.Service) []tools.BaseTool {
	return []tools.BaseTool{
		tools.NewGlobTool(permissions),
		tools.NewGrepTool(permissions),
		tools.NewLsTool(permissions),
		tools.NewSourcegraphTool(),
		tools.NewViewTool(lspClients, permissions),
	}
}

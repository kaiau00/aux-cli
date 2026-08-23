package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/version"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type mcpTool struct {
	mcpName     string
	tool        mcp.Tool
	mcpConfig   config.MCPServer
	permissions permission.Service
}

type MCPClient interface {
	Initialize(
		ctx context.Context,
		request mcp.InitializeRequest,
	) (*mcp.InitializeResult, error)
	ListTools(ctx context.Context, request mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func (b *mcpTool) Info() tools.ToolInfo {
	required := b.tool.InputSchema.Required
	if required == nil {
		required = make([]string, 0)
	}
	return tools.ToolInfo{
		Name:        fmt.Sprintf("%s_%s", b.mcpName, b.tool.Name),
		Description: b.tool.Description,
		Parameters:  b.tool.InputSchema.Properties,
		Required:    required,
	}
}

func runTool(ctx context.Context, c MCPClient, toolName string, input string) (tools.ToolResponse, error) {
	defer c.Close()
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Aux",
		Version: version.Version,
	}

	_, err := c.Initialize(ctx, initRequest)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	toolRequest := mcp.CallToolRequest{}
	toolRequest.Params.Name = toolName
	var args map[string]any
	if err = json.Unmarshal([]byte(input), &args); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	toolRequest.Params.Arguments = args
	result, err := c.CallTool(ctx, toolRequest)
	if err != nil {
		return tools.NewTextErrorResponse(err.Error()), nil
	}

	output := ""
	for _, v := range result.Content {
		if v, ok := v.(mcp.TextContent); ok {
			output = v.Text
		} else {
			output = fmt.Sprintf("%v", v)
		}
	}

	return tools.NewTextResponse(output), nil
}

func (b *mcpTool) Run(ctx context.Context, params tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, messageID := tools.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session ID and message ID are required for creating a new file")
	}
	permissionDescription := fmt.Sprintf("execute %s with the following parameters: %s", b.Info().Name, params.Input)
	p := b.permissions.Request(
		permission.CreatePermissionRequest{
			SessionID:   sessionID,
			Path:        config.WorkingDirectory(),
			ToolName:    b.Info().Name,
			Action:      "execute",
			Description: permissionDescription,
			Params:      params.Input,
		},
	)
	if !p {
		return tools.NewTextErrorResponse("permission denied"), nil
	}

	switch b.mcpConfig.Type {
	case config.MCPStdio:
		c, err := client.NewStdioMCPClient(
			b.mcpConfig.Command,
			b.mcpConfig.Env,
			b.mcpConfig.Args...,
		)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return runTool(ctx, c, b.tool.Name, params.Input)
	case config.MCPSse:
		c, err := client.NewSSEMCPClient(
			b.mcpConfig.URL,
			client.WithHeaders(b.mcpConfig.Headers),
		)
		if err != nil {
			return tools.NewTextErrorResponse(err.Error()), nil
		}
		return runTool(ctx, c, b.tool.Name, params.Input)
	}

	return tools.NewTextErrorResponse("invalid mcp type"), nil
}

func NewMcpTool(name string, tool mcp.Tool, permissions permission.Service, mcpConfig config.MCPServer) tools.BaseTool {
	return &mcpTool{
		mcpName:     name,
		tool:        tool,
		mcpConfig:   mcpConfig,
		permissions: permissions,
	}
}

var (
	mcpTools   []tools.BaseTool
	mcpToolsMu sync.Mutex
)

func getTools(ctx context.Context, name string, m config.MCPServer, permissions permission.Service, c MCPClient) []tools.BaseTool {
	var stdioTools []tools.BaseTool
	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Aux",
		Version: version.Version,
	}

	_, err := c.Initialize(ctx, initRequest)
	if err != nil {
		logging.Error("error initializing mcp client", "error", err)
		return stdioTools
	}
	toolsRequest := mcp.ListToolsRequest{}
	tools, err := c.ListTools(ctx, toolsRequest)
	if err != nil {
		logging.Error("error listing tools", "error", err)
		return stdioTools
	}
	// Sort by tool name so a server that returns its tools in a different order
	// between runs cannot shift the prompt prefix. See serverNames below for why
	// tool ordering is load-bearing rather than cosmetic.
	sorted := make([]mcp.Tool, len(tools.Tools))
	copy(sorted, tools.Tools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, t := range sorted {
		stdioTools = append(stdioTools, NewMcpTool(name, t, permissions, m))
	}
	defer c.Close()
	return stdioTools
}

// serverNames returns the MCP server names in sorted order.
//
// Ranging over the server map directly would inherit Go's randomized map
// iteration, so with two or more MCP servers the tool array — and therefore the
// serialized tool block at the head of every prompt — would differ between
// process starts. Providers key their prompt cache on an exact prefix match, so
// that randomization silently cost a cold cache on every restart while
// StablePrefixID (which sorted before hashing) reported the prefix as unchanged.
//
// Kept as a pure function over the map so the ordering guarantee is testable
// without loading global config.
func serverNames(servers map[string]config.MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func GetMcpTools(ctx context.Context, permissions permission.Service) []tools.BaseTool {
	mcpToolsMu.Lock()
	defer mcpToolsMu.Unlock()
	if len(mcpTools) > 0 {
		return mcpTools
	}
	cfg := config.Get()
	if cfg == nil {
		return nil
	}
	servers := cfg.MCPServers
	for _, name := range serverNames(servers) {
		m := servers[name]
		switch m.Type {
		case config.MCPStdio:
			c, err := client.NewStdioMCPClient(
				m.Command,
				m.Env,
				m.Args...,
			)
			if err != nil {
				logging.Error("error creating mcp client", "error", err)
				continue
			}

			mcpTools = append(mcpTools, getTools(ctx, name, m, permissions, c)...)
		case config.MCPSse:
			c, err := client.NewSSEMCPClient(
				m.URL,
				client.WithHeaders(m.Headers),
			)
			if err != nil {
				logging.Error("error creating mcp client", "error", err)
				continue
			}
			mcpTools = append(mcpTools, getTools(ctx, name, m, permissions, c)...)
		}
	}

	return mcpTools
}

package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/lsp"
)

// stubTool is a tool that exists only to be found in a list.
type stubTool struct{ name string }

func (s stubTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: s.name} }

func (s stubTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.ToolResponse{}, nil
}

// Building the agent's tool set must not wait for MCP servers to answer.
//
// It used to. Discovery ran while the app was being assembled, and the TUI is
// assembled after the agent, so Aux drew nothing until the slowest configured
// server had answered: measured at 9.1s of a 14s startup against one
// `npx -y` server, with a goroutine dump showing main parked in mcp-go.
//
// The server configured here sleeps rather than speaking the protocol, which
// is all this needs: the point is that nobody waits for it.
func TestBuildingTheToolSetDoesNotWaitForMcpServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// Only this server, so the user's own configuration cannot take part.
	if err := os.WriteFile(filepath.Join(home, ".aux.json"), []byte(`{
	  "mcpServers": {
	    "slow": {"type": "stdio", "command": "sleep", "args": ["30"]}
	  }
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	work := t.TempDir()
	if _, err := config.Load(work, false); err != nil {
		t.Skipf("config.Load: %v", err)
	}
	if len(config.Get().MCPServers) == 0 {
		t.Skip("the test's own MCP server did not reach the config")
	}

	done := make(chan int, 1)
	go func() {
		done <- len(CoderAgentTools(Deps{}, nil, nil, map[string]*lsp.Client{},
			hooks.NewRegistry(), nil))
	}()
	select {
	case n := <-done:
		if n == 0 {
			t.Error("no tools were built at all")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("building the tool set is waiting for the MCP server; startup is blocked on it again")
	}
}

// The slow-to-discover tools go after the built-in ones, every turn.
//
// Providers key their prompt cache on an exact prefix of the serialized tool
// block, so where these land is load-bearing: appended at the end, the prefix
// the cache matches on does not move.
func TestLateToolsComeAfterTheBuiltInOnes(t *testing.T) {
	a := &agent{
		tools: []tools.BaseTool{stubTool{"bash"}, stubTool{"view"}},
		lateTools: func(context.Context) []tools.BaseTool {
			return []tools.BaseTool{stubTool{"mcp_memory_read"}}
		},
	}

	var names []string
	for _, tool := range a.currentTools(context.Background()) {
		names = append(names, tool.Info().Name)
	}
	want := []string{"bash", "view", "mcp_memory_read"}
	if len(names) != len(want) {
		t.Fatalf("tool set is %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool set is %v, want %v", names, want)
		}
	}

	// The built-in slice must not have been appended to in place: a turn that
	// grew it would leave the next turn with the late tools listed twice.
	if len(a.tools) != 2 {
		t.Errorf("the built-in tool set grew to %d entries", len(a.tools))
	}
}

// A tool the model was just offered has to be findable when it calls it.
// Looking only at the built-in set would tell the model an MCP tool it had
// been shown does not exist.
func TestALateToolCanBeFound(t *testing.T) {
	a := &agent{
		tools: []tools.BaseTool{stubTool{"bash"}},
		lateTools: func(context.Context) []tools.BaseTool {
			return []tools.BaseTool{stubTool{"mcp_memory_read"}}
		},
	}
	if a.findTool(context.Background(), "mcp_memory_read") == nil {
		t.Error("a late-discovered tool cannot be found when the model calls it")
	}
	if a.findTool(context.Background(), "nothing_like_this") != nil {
		t.Error("a tool that does not exist was found")
	}
}

// With nothing late configured the tool set is exactly the built-in one, and
// no copy is made.
func TestWithoutLateToolsTheSetIsUnchanged(t *testing.T) {
	a := &agent{tools: []tools.BaseTool{stubTool{"bash"}}}
	if got := a.currentTools(context.Background()); len(got) != 1 || got[0].Info().Name != "bash" {
		t.Errorf("tool set is %v", got)
	}
	empty := &agent{
		tools:     []tools.BaseTool{stubTool{"bash"}},
		lateTools: func(context.Context) []tools.BaseTool { return nil },
	}
	if got := empty.currentTools(context.Background()); len(got) != 1 {
		t.Errorf("tool set is %v with a provider that found nothing", got)
	}
}

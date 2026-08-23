package context

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/version"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

const CodebaseMemoryServerName = "codebase_memory"

type memoryClient interface {
	Initialize(context.Context, mcp.InitializeRequest) (*mcp.InitializeResult, error)
	ListTools(context.Context, mcp.ListToolsRequest) (*mcp.ListToolsResult, error)
	CallTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
	Close() error
}

func QueryCodebaseGraph(ctx context.Context, prompt string) (Graph, error) {
	cfg := config.Get()
	if cfg == nil {
		return Graph{}, fmt.Errorf("config not loaded")
	}
	server, ok := cfg.MCPServers[CodebaseMemoryServerName]
	if !ok {
		return Graph{}, fmt.Errorf("codebase memory mcp server is not configured")
	}

	c, err := newMemoryClient(server)
	if err != nil {
		return Graph{}, err
	}
	defer c.Close()

	initRequest := mcp.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcp.Implementation{
		Name:    "Aux",
		Version: version.Version,
	}
	if _, err := c.Initialize(ctx, initRequest); err != nil {
		return Graph{}, err
	}

	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return Graph{}, err
	}

	candidates := graphToolCandidates(toolsResult.Tools)
	if len(candidates) == 0 {
		return Graph{}, fmt.Errorf("no graph-like codebase memory tools discovered")
	}

	args := map[string]any{
		"path":      cfg.WorkingDir,
		"root":      cfg.WorkingDir,
		"workspace": cfg.WorkingDir,
		"query":     prompt,
		"prompt":    prompt,
	}
	var lastErr error
	for _, tool := range candidates {
		graph, err := callGraphTool(ctx, c, tool.Name, args)
		if err == nil && len(graph.Nodes) > 0 {
			return graph, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr != nil {
		return Graph{}, lastErr
	}
	return Graph{}, fmt.Errorf("codebase memory returned no graph nodes")
}

func newMemoryClient(server config.MCPServer) (memoryClient, error) {
	switch server.Type {
	case "", config.MCPStdio:
		return client.NewStdioMCPClient(server.Command, server.Env, server.Args...)
	case config.MCPSse:
		return client.NewSSEMCPClient(server.URL, client.WithHeaders(server.Headers))
	default:
		return nil, fmt.Errorf("unsupported codebase memory MCP type: %s", server.Type)
	}
}

func graphToolCandidates(tools []mcp.Tool) []mcp.Tool {
	candidates := make([]mcp.Tool, 0)
	for _, tool := range tools {
		name := strings.ToLower(tool.Name)
		description := strings.ToLower(tool.Description)
		score := 0
		for _, term := range []string{"graph", "ast", "symbol", "dependency", "codebase", "memory"} {
			if strings.Contains(name, term) {
				score += 2
			}
			if strings.Contains(description, term) {
				score++
			}
		}
		if score > 0 {
			candidates = append(candidates, tool)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return graphToolScore(candidates[i]) > graphToolScore(candidates[j])
	})
	return candidates
}

func graphToolScore(tool mcp.Tool) int {
	text := strings.ToLower(tool.Name + " " + tool.Description)
	score := 0
	for _, term := range []string{"graph", "ast", "symbol", "dependency", "codebase", "memory"} {
		if strings.Contains(text, term) {
			score++
		}
	}
	return score
}

func callGraphTool(ctx context.Context, c memoryClient, toolName string, args map[string]any) (Graph, error) {
	request := mcp.CallToolRequest{}
	request.Params.Name = toolName
	request.Params.Arguments = args
	result, err := c.CallTool(ctx, request)
	if err != nil {
		return Graph{}, err
	}

	for _, content := range result.Content {
		text, ok := content.(mcp.TextContent)
		if !ok {
			continue
		}
		graph, err := ParseGraphResponse(text.Text)
		if err == nil && len(graph.Nodes) > 0 {
			return graph, nil
		}
		if err != nil {
			logging.Debug("failed to parse codebase memory graph response", "tool", toolName, "error", err)
		}
	}
	return Graph{}, fmt.Errorf("tool %s did not return a parseable graph", toolName)
}

func ParseGraphResponse(raw string) (Graph, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return Graph{}, err
	}
	graph := Graph{}
	collectGraph(value, &graph)
	dedupeGraph(&graph)
	return graph, nil
}

func collectGraph(value any, graph *Graph) {
	switch v := value.(type) {
	case map[string]any:
		if arrays, ok := graphArrays(v); ok {
			for _, node := range arrays.nodes {
				if parsed, ok := parseGraphNode(node); ok {
					graph.Nodes = append(graph.Nodes, parsed)
				}
			}
			for _, edge := range arrays.edges {
				if parsed, ok := parseGraphEdge(edge); ok {
					graph.Edges = append(graph.Edges, parsed)
				}
			}
			return
		}
		for _, child := range v {
			collectGraph(child, graph)
		}
	case []any:
		for _, child := range v {
			collectGraph(child, graph)
		}
	}
}

type graphArraySet struct {
	nodes []any
	edges []any
}

func graphArrays(obj map[string]any) (graphArraySet, bool) {
	nodeKeys := []string{"nodes", "symbols", "files"}
	edgeKeys := []string{"edges", "relationships", "dependencies", "links"}
	var set graphArraySet
	for _, key := range nodeKeys {
		if values, ok := obj[key].([]any); ok {
			set.nodes = values
			break
		}
	}
	for _, key := range edgeKeys {
		if values, ok := obj[key].([]any); ok {
			set.edges = values
			break
		}
	}
	return set, len(set.nodes) > 0
}

func parseGraphNode(value any) (GraphNode, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return GraphNode{}, false
	}
	node := GraphNode{
		ID:   firstString(obj, "id", "node_id", "symbol_id", "key"),
		Name: firstString(obj, "name", "symbol", "identifier", "label"),
		Path: firstString(obj, "path", "file", "file_path", "filepath", "uri"),
		Type: firstString(obj, "type", "kind"),
	}
	if node.ID == "" {
		node.ID = nodeIdentity(node)
	}
	return node, node.Path != "" || node.Name != ""
}

func parseGraphEdge(value any) (GraphEdge, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return GraphEdge{}, false
	}
	edge := GraphEdge{
		From: firstString(obj, "from", "source", "source_id", "caller", "importer"),
		To:   firstString(obj, "to", "target", "target_id", "callee", "imported"),
		Type: firstString(obj, "type", "kind", "relation"),
	}
	return edge, edge.From != "" && edge.To != ""
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := obj[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			return typed
		case map[string]any:
			if nested := firstString(typed, "id", "name", "path"); nested != "" {
				return nested
			}
		}
	}
	return ""
}

func dedupeGraph(graph *Graph) {
	nodes := make([]GraphNode, 0, len(graph.Nodes))
	seenNodes := make(map[string]struct{})
	for _, node := range graph.Nodes {
		if node.ID == "" {
			node.ID = nodeIdentity(node)
		}
		if _, ok := seenNodes[node.ID]; ok {
			continue
		}
		seenNodes[node.ID] = struct{}{}
		nodes = append(nodes, node)
	}
	graph.Nodes = nodes

	edges := make([]GraphEdge, 0, len(graph.Edges))
	seenEdges := make(map[string]struct{})
	for _, edge := range graph.Edges {
		key := edge.From + "\x00" + edge.To + "\x00" + edge.Type
		if _, ok := seenEdges[key]; ok {
			continue
		}
		seenEdges[key] = struct{}{}
		edges = append(edges, edge)
	}
	graph.Edges = edges
}

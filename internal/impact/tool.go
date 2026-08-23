package impact

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// AnalyzeToolName is the tool an impact-analyst subagent uses to query the
// deterministic impact graph (roadmapplan.md §11.3, §8.6).
const AnalyzeToolName = "impact_analyze"

type analyzeTool struct {
	svc *Service
}

// NewAnalyzeTool returns the impact-analysis tool: given a set of changed or
// planned-to-change repository-relative paths, it reports affected packages,
// direct dependents, and related tests from the project's deterministic
// impact graph, plus whether validation should broaden.
func NewAnalyzeTool(svc *Service) tools.BaseTool {
	return &analyzeTool{svc: svc}
}

type analyzeParams struct {
	Paths    []string `json:"paths"`
	Revision string   `json:"revision,omitempty"`
}

func (t *analyzeTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name: AnalyzeToolName,
		Description: "Analyze the impact of a set of changed or to-be-changed repository-relative " +
			"file paths: affected packages, direct dependents, and related tests, from the " +
			"project's deterministic impact graph (not an LLM guess). Reports whether validation " +
			"should broaden beyond the affected packages.",
		Parameters: map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Repository-relative paths changed or planned to change",
			},
			"revision": map[string]any{
				"type":        "string",
				"description": "Optional VCS revision to check the graph's freshness against",
			},
		},
		Required: []string{"paths"},
	}
}

func (t *analyzeTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var p analyzeParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}
	if len(p.Paths) == 0 {
		return tools.NewTextErrorResponse("paths is required"), nil
	}

	projectID := tools.CorrelationFromContext(ctx).ProjectID
	if projectID == "" {
		return tools.NewTextErrorResponse("no project is bound to this task"), nil
	}

	res, err := t.svc.Analyze(ctx, projectID, p.Revision, p.Paths)
	if err != nil {
		return tools.ToolResponse{}, fmt.Errorf("failed to analyze impact: %w", err)
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return tools.ToolResponse{}, fmt.Errorf("failed to encode impact result: %w", err)
	}
	return tools.NewTextResponse(string(b)), nil
}

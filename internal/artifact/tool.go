package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// ArtifactToolName is the tool the model uses to retrieve virtualized output.
const ArtifactToolName = "artifact"

type viewTool struct {
	svc *Service
}

// NewViewTool returns the artifact retrieval tool (view range / search).
func NewViewTool(svc *Service) tools.BaseTool {
	return &viewTool{svc: svc}
}

type artifactParams struct {
	ArtifactID string `json:"artifact_id"`
	Action     string `json:"action"`
	Offset     int    `json:"offset"`
	Length     int    `json:"length"`
	Query      string `json:"query"`
	MaxHits    int    `json:"max_hits"`
}

func (t *viewTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name: ArtifactToolName,
		Description: "Retrieve the full content of a stored artifact (large tool output that was " +
			"summarized). Use action 'view' with offset/length to read a byte range, or action " +
			"'search' with a query to find matching lines.",
		Parameters: map[string]any{
			"artifact_id": map[string]any{"type": "string", "description": "The artifact id from a digest handle"},
			"action":      map[string]any{"type": "string", "enum": []string{"view", "search"}, "description": "view a byte range or search lines"},
			"offset":      map[string]any{"type": "integer", "description": "byte offset for view (default 0)"},
			"length":      map[string]any{"type": "integer", "description": "byte length for view (0 = to end)"},
			"query":       map[string]any{"type": "string", "description": "search substring for action=search"},
			"max_hits":    map[string]any{"type": "integer", "description": "max search hits (default 50)"},
		},
		Required: []string{"artifact_id"},
	}
}

func (t *viewTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var p artifactParams
	if err := json.Unmarshal([]byte(call.Input), &p); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid parameters: %s", err)), nil
	}
	if p.ArtifactID == "" {
		return tools.NewTextErrorResponse("artifact_id is required"), nil
	}

	switch p.Action {
	case "search":
		matches, err := t.svc.Search(ctx, p.ArtifactID, p.Query, p.MaxHits)
		if err != nil {
			return tools.NewTextErrorResponse(fmt.Sprintf("artifact search failed: %s", err)), nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d matches for %q:\n", len(matches), p.Query)
		for _, m := range matches {
			fmt.Fprintf(&b, "%d: %s\n", m.Line, m.Content)
		}
		return tools.NewTextResponse(b.String()), nil
	default: // view
		length := p.Length
		if length == 0 {
			length = 8000 // default readable window
		}
		data, err := t.svc.GetRange(ctx, p.ArtifactID, p.Offset, length)
		if err != nil {
			return tools.NewTextErrorResponse(fmt.Sprintf("artifact read failed: %s", err)), nil
		}
		return tools.NewTextResponse(string(data)), nil
	}
}

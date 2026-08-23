package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// Role specializes a subagent's tool set, prompt, and expected output for a
// specific kind of bounded work. The zero value is the
// generic, unspecialized subagent this package already had.
type Role string

const (
	RoleGeneric          Role = ""
	RoleRepoMapper       Role = "repo_mapper"
	RoleImpactAnalyst    Role = "impact_analyst"
	RoleValidationRunner Role = "validation_runner"
	RoleReviewer         Role = "reviewer"
)

// rolePrompt returns a role-specific system-prompt fragment appended to the
// subagent's instructions. The empty string for RoleGeneric preserves the
// original, unspecialized behaviour exactly.
func (r Role) rolePrompt() string {
	switch r {
	case RoleRepoMapper:
		return "You are the repository mapper. Locate the files, symbols, and structures relevant to the objective. Do not propose a solution; report a structured map via the report tool."
	case RoleImpactAnalyst:
		return "You are the impact analyst. Use the impact_analyze tool to determine what changing the relevant files would affect: callers, tests, and related packages. Report your findings via the report tool."
	case RoleValidationRunner:
		return "You are the validation runner. Run the project's validation commands and report pass/fail evidence via the report tool's validation field. Do not make changes beyond what running validation requires."
	case RoleReviewer:
		return "You are the reviewer. Identify correctness, security, and consistency issues in the current changes. Do not make edits; report findings via the report tool."
	default:
		return ""
	}
}

// Effect is one side effect a subagent believes it caused (e.g. a command it
// ran). Subagents today are read-only except for RoleValidationRunner's Bash
// access, so most reports carry no effects.
type Effect struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

// ValidationSummary is a subagent's own account of what it checked. It is not
// proof-of-done evidence by itself — validation.Service.RunIntent recording a
// Run is still the authoritative source — but it lets the parent see what the
// subagent believes it verified without re-deriving it from free text.
type ValidationSummary struct {
	Commands []string `json:"commands,omitempty"`
	Passed   bool     `json:"passed"`
	Notes    string   `json:"notes,omitempty"`
}

// Result is a subagent's structured report: the
// contract between a subagent and its parent, replacing an unstructured final
// message.
type Result struct {
	Findings   []string           `json:"findings,omitempty"`
	Effects    []Effect           `json:"effects,omitempty"`
	PageRefs   []string           `json:"pageRefs,omitempty"`
	Validation *ValidationSummary `json:"validation,omitempty"`
	Risks      []string           `json:"risks,omitempty"`
}

// reportCollector receives at most one report-tool call per subagent run and
// makes it available to the caller once the run finishes. A fresh collector is
// created per subagent invocation, so there is no cross-request state.
type reportCollector struct {
	mu     sync.Mutex
	result *Result
}

func (c *reportCollector) set(r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.result = &r
}

func (c *reportCollector) get() (Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.result == nil {
		return Result{}, false
	}
	return *c.result, true
}

// ReportToolName is the tool a subagent calls to return its structured Result.
const ReportToolName = "report"

type reportTool struct {
	collector *reportCollector
}

func newReportTool(c *reportCollector) tools.BaseTool {
	return &reportTool{collector: c}
}

func (t *reportTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name: ReportToolName,
		Description: "Report your findings back to the parent agent. Call this exactly once, as " +
			"your final action, instead of ending with a free-text summary.",
		Parameters: map[string]any{
			"findings": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "What you found or did, as short bullet points",
			},
			"effects": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":        map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
					},
				},
				"description": "Side effects you caused, if any (e.g. commands run)",
			},
			"pageRefs": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "Stable keys of context pages you consulted",
			},
			"validation": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"passed":   map[string]any{"type": "boolean"},
					"notes":    map[string]any{"type": "string"},
				},
				"description": "What you verified, if you ran validation",
			},
			"risks": map[string]any{
				"type": "array", "items": map[string]any{"type": "string"},
				"description": "Anything the parent should be cautious about",
			},
		},
		Required: []string{"findings"},
	}
}

func (t *reportTool) Run(_ context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var r Result
	if err := json.Unmarshal([]byte(call.Input), &r); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("invalid report: %s", err)), nil
	}
	if len(r.Findings) == 0 {
		return tools.NewTextErrorResponse("findings is required"), nil
	}
	t.collector.set(r)
	return tools.NewTextResponse("report received"), nil
}

// roleTools returns the tool set for a subagent role: the shared read-only
// base plus the report tool, plus whatever the role specifically needs.
// impactSvc/bashTool are only consulted for roles that use them, so a nil
// value simply omits that capability rather than failing.
func roleTools(r Role, base []tools.BaseTool, impactSvc *impact.Service, bashTool tools.BaseTool, collector *reportCollector) []tools.BaseTool {
	out := append(append([]tools.BaseTool{}, base...), newReportTool(collector))
	switch r {
	case RoleImpactAnalyst:
		if impactSvc != nil {
			out = append(out, impact.NewAnalyzeTool(impactSvc))
		}
	case RoleValidationRunner:
		if bashTool != nil {
			out = append(out, bashTool)
		}
	}
	return out
}

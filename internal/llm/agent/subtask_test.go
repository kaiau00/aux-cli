package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

func TestRolePromptsAreDistinctAndGenericIsEmpty(t *testing.T) {
	if RoleGeneric.rolePrompt() != "" {
		t.Fatalf("generic role must have no prompt fragment, got %q", RoleGeneric.rolePrompt())
	}
	seen := map[string]bool{}
	for _, r := range []Role{RoleRepoMapper, RoleImpactAnalyst, RoleValidationRunner, RoleReviewer} {
		p := r.rolePrompt()
		if p == "" {
			t.Fatalf("role %q must have a non-empty prompt fragment", r)
		}
		if seen[p] {
			t.Fatalf("role %q reuses another role's prompt fragment", r)
		}
		seen[p] = true
	}
}

func TestReportToolCollectsStructuredResult(t *testing.T) {
	collector := &reportCollector{}
	tool := newReportTool(collector)

	input, _ := json.Marshal(Result{
		Findings: []string{"found the thing"},
		Risks:    []string{"be careful"},
		Validation: &ValidationSummary{
			Commands: []string{"go test ./..."},
			Passed:   true,
		},
	})
	resp, err := tool.Run(context.Background(), tools.ToolCall{ID: "c1", Name: ReportToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}

	got, ok := collector.get()
	if !ok {
		t.Fatal("expected the collector to have a result")
	}
	if len(got.Findings) != 1 || got.Findings[0] != "found the thing" {
		t.Fatalf("findings not captured: %+v", got)
	}
	if got.Validation == nil || !got.Validation.Passed {
		t.Fatalf("validation summary not captured: %+v", got)
	}
}

func TestReportToolRequiresFindings(t *testing.T) {
	collector := &reportCollector{}
	tool := newReportTool(collector)

	input, _ := json.Marshal(Result{})
	resp, err := tool.Run(context.Background(), tools.ToolCall{ID: "c1", Name: ReportToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected an error response for empty findings")
	}
	if _, ok := collector.get(); ok {
		t.Fatal("collector must not record an invalid report")
	}
}

func TestRoleToolsAddsRoleSpecificToolsOnly(t *testing.T) {
	base := []tools.BaseTool{&scriptedTool{name: "glob"}, &scriptedTool{name: "grep"}}
	collector := &reportCollector{}
	bash := &scriptedTool{name: "bash"}

	hasName := func(ts []tools.BaseTool, name string) bool {
		for _, tl := range ts {
			if tl.Info().Name == name {
				return true
			}
		}
		return false
	}

	generic := roleTools(RoleGeneric, base, nil, bash, collector)
	if hasName(generic, "bash") {
		t.Fatal("generic role must not get bash")
	}
	if !hasName(generic, ReportToolName) {
		t.Fatal("every role must get the report tool")
	}

	runner := roleTools(RoleValidationRunner, base, nil, bash, collector)
	if !hasName(runner, "bash") {
		t.Fatal("validation_runner role must get bash")
	}

	mapper := roleTools(RoleRepoMapper, base, nil, bash, collector)
	if hasName(mapper, "bash") {
		t.Fatal("repo_mapper role must not get bash")
	}
	if len(mapper) != len(base)+1 {
		t.Fatalf("repo_mapper should only add the report tool to the base set, got %d tools", len(mapper))
	}

	// A nil impact service must not crash and must not add a phantom tool.
	analyst := roleTools(RoleImpactAnalyst, base, nil, bash, collector)
	if len(analyst) != len(base)+1 {
		t.Fatalf("impact_analyst with a nil service should only add the report tool, got %d tools", len(analyst))
	}
}

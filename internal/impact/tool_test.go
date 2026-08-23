package impact_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

func TestAnalyzeToolReportsImpactForBoundProject(t *testing.T) {
	svc := newService(t)
	root := writeModule(t)
	ctx := context.Background()

	if _, err := svc.Index(ctx, "proj-1", root, "rev-1"); err != nil {
		t.Fatalf("Index: %v", err)
	}

	tool := impact.NewAnalyzeTool(svc)
	ctx = context.WithValue(ctx, tools.ProjectIDContextKey, "proj-1")

	input, _ := json.Marshal(map[string]any{"paths": []string{"foo/foo.go"}, "revision": "rev-1"})
	resp, err := tool.Run(ctx, tools.ToolCall{ID: "c1", Name: impact.AnalyzeToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.IsError {
		t.Fatalf("unexpected error response: %s", resp.Content)
	}

	var res impact.Result
	if err := json.Unmarshal([]byte(resp.Content), &res); err != nil {
		t.Fatalf("failed to decode result: %v\n%s", err, resp.Content)
	}
	if !contains(res.AffectedPackages, "example.com/proj/foo") {
		t.Fatalf("expected foo package affected, got %+v", res)
	}
	if !contains(res.DirectDependents, "bar/bar.go") {
		t.Fatalf("expected bar/bar.go as a direct dependent, got %+v", res)
	}
}

func TestAnalyzeToolRequiresBoundProject(t *testing.T) {
	svc := newService(t)
	tool := impact.NewAnalyzeTool(svc)

	input, _ := json.Marshal(map[string]any{"paths": []string{"foo/foo.go"}})
	resp, err := tool.Run(context.Background(), tools.ToolCall{ID: "c1", Name: impact.AnalyzeToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected an error response when no project is bound to the context")
	}
}

func TestAnalyzeToolRequiresPaths(t *testing.T) {
	svc := newService(t)
	tool := impact.NewAnalyzeTool(svc)
	ctx := context.WithValue(context.Background(), tools.ProjectIDContextKey, "proj-1")

	input, _ := json.Marshal(map[string]any{"paths": []string{}})
	resp, err := tool.Run(ctx, tools.ToolCall{ID: "c1", Name: impact.AnalyzeToolName, Input: string(input)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected an error response for empty paths")
	}
}

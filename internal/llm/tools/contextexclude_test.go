package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

type fakePageStore struct {
	excluded []string
	err      error
}

func (f *fakePageStore) Exclude(_ context.Context, _, toolCallID string) error {
	if f.err != nil {
		return f.err
	}
	f.excluded = append(f.excluded, toolCallID)
	return nil
}

type fakeReadHistory map[string][]string

func (f fakeReadHistory) ToolCallIDsForPaths(_ context.Context, _ string, paths []string) (map[string][]string, error) {
	out := map[string][]string{}
	for _, p := range paths {
		if ids, ok := f[p]; ok {
			out[p] = ids
		}
	}
	return out, nil
}

func excludeCtx() context.Context {
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	return context.WithValue(ctx, tools.TaskIDContextKey, "task-1")
}

func runExclude(t *testing.T, tool tools.BaseTool, ctx context.Context, input string) tools.ToolResponse {
	t.Helper()
	resp, err := tool.Run(ctx, tools.ToolCall{ID: "c1", Name: tools.ContextExcludeToolName, Input: input})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return resp
}

func TestContextExcludeDropsMatchedPages(t *testing.T) {
	pages := &fakePageStore{}
	history := fakeReadHistory{"a.go": {"call-a1", "call-a2"}, "b.go": {"call-b"}}
	tool := tools.NewContextExcludeTool(pages, history)

	resp := runExclude(t, tool, excludeCtx(), `{"paths":["a.go","b.go"],"reason":"done with these"}`)
	if resp.IsError {
		t.Fatalf("unexpected error response: %q", resp.Content)
	}

	if len(pages.excluded) != 3 {
		t.Fatalf("expected every tool call for both paths to be excluded, got %v", pages.excluded)
	}

	var meta tools.ContextExcludeResponseMetadata
	if err := json.Unmarshal([]byte(resp.Metadata), &meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if len(meta.Excluded) != 2 || len(meta.NotFound) != 0 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.Reason != "done with these" {
		t.Fatalf("reason should be carried through for the UI, got %q", meta.Reason)
	}
}

// Excluding something never read is a normal outcome, not a failure: the model
// should be told plainly rather than shown an error it will try to recover from.
func TestContextExcludeReportsUnknownPathsWithoutFailing(t *testing.T) {
	pages := &fakePageStore{}
	tool := tools.NewContextExcludeTool(pages, fakeReadHistory{"a.go": {"call-a"}})

	resp := runExclude(t, tool, excludeCtx(), `{"paths":["a.go","never-read.go"]}`)
	if resp.IsError {
		t.Fatalf("an unread path should not make the call fail: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "never-read.go") {
		t.Fatalf("the response should name what could not be dropped, got %q", resp.Content)
	}
	if len(pages.excluded) != 1 || pages.excluded[0] != "call-a" {
		t.Fatalf("only the known path should have been excluded, got %v", pages.excluded)
	}
}

func TestContextExcludeRequiresPaths(t *testing.T) {
	tool := tools.NewContextExcludeTool(&fakePageStore{}, fakeReadHistory{})
	if resp := runExclude(t, tool, excludeCtx(), `{"paths":[]}`); !resp.IsError {
		t.Fatal("expected an error response for an empty path list")
	}
	if resp := runExclude(t, tool, excludeCtx(), `not json`); !resp.IsError {
		t.Fatal("expected an error response for malformed input")
	}
}

// Exclusions are scoped to a task, so without one there is nothing to write to.
func TestContextExcludeWithoutTaskIsRejected(t *testing.T) {
	tool := tools.NewContextExcludeTool(&fakePageStore{}, fakeReadHistory{"a.go": {"call-a"}})
	resp := runExclude(t, tool, context.Background(), `{"paths":["a.go"]}`)
	if !resp.IsError {
		t.Fatal("expected an error response when there is no active task")
	}
}

func TestSamePathHandlesRelativeAndAbsoluteForms(t *testing.T) {
	const wd = "/repo"
	cases := []struct {
		a, b string
		want bool
	}{
		{"a.go", "/repo/a.go", true},
		{"./a.go", "/repo/a.go", true},
		{"internal/x.go", "/repo/internal/x.go", true},
		{"/repo/internal/../a.go", "a.go", true},
		{"a.go", "/repo/b.go", false},
		{"a.go", "/elsewhere/a.go", false},
		// A suffix match is not a path match: these are different files.
		{"x.go", "/repo/prefix_x.go", false},
	}
	for _, c := range cases {
		if got := tools.SamePath(wd, c.a, c.b); got != c.want {
			t.Errorf("SamePath(%q, %q, %q) = %v, want %v", wd, c.a, c.b, got, c.want)
		}
	}
}

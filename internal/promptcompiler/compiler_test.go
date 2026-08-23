package promptcompiler_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
)

type fakeTool struct {
	name   string
	desc   string
	params map[string]any
}

func (f fakeTool) Info() tools.ToolInfo {
	return tools.ToolInfo{Name: f.name, Description: f.desc, Parameters: f.params}
}
func (f fakeTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return tools.ToolResponse{}, nil
}

func history() []message.Message {
	return []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "add a feature"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{
			message.TextContent{Text: "working on it"},
			message.ToolCall{ID: "c1", Name: "grep", Input: `{"q":"x"}`},
		}},
		{Role: message.Tool, Parts: []message.ContentPart{
			message.ToolResult{ToolCallID: "c1", Content: "match"},
		}},
	}
}

func TestCompatibilityCompilerParity(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	in := history()
	out := c.Compile(promptcompiler.Input{History: in})

	// Golden: compiled messages equal the input transcript (parity mode).
	if !reflect.DeepEqual(out.Messages, in) {
		t.Fatalf("compatibility compiler must render the transcript unchanged")
	}
	if out.Manifest.TokenEstimate != out.EstimatedTokens {
		t.Fatalf("manifest token estimate must match prompt estimate")
	}
	if len(out.Manifest.Sections) == 0 {
		t.Fatalf("manifest should record at least one section")
	}
}

func TestCompilerDropsEmptyMessages(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	in := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{}}, // empty -> dropped
	}
	out := c.Compile(promptcompiler.Input{History: in})
	if len(out.Messages) != 1 {
		t.Fatalf("empty-part messages should be dropped, got %d", len(out.Messages))
	}
}

func TestCompilerPreservesToolCallResultOrder(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	in := history()
	out := c.Compile(promptcompiler.Input{History: in})

	// The assistant tool_call must precede its tool result in the compiled prompt.
	var callIdx, resultIdx = -1, -1
	for i, m := range out.Messages {
		if len(m.ToolCalls()) > 0 {
			callIdx = i
		}
		if len(m.ToolResults()) > 0 {
			resultIdx = i
		}
	}
	if callIdx == -1 || resultIdx == -1 || callIdx >= resultIdx {
		t.Fatalf("tool_call must precede tool_result: call=%d result=%d", callIdx, resultIdx)
	}
}

func TestCompatibilityCompilerAppliesExclusions(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	out := c.Compile(promptcompiler.Input{History: history(), ExcludedToolCallIDs: map[string]bool{"c1": true}})

	var result message.ToolResult
	found := false
	for _, m := range out.Messages {
		for _, r := range m.ToolResults() {
			if r.ToolCallID == "c1" {
				result, found = r, true
			}
		}
	}
	if !found {
		t.Fatal("excluded tool result should still be present, just stubbed")
	}
	if result.Content == "match" {
		t.Fatal("excluded tool result content must not reach the compiled prompt")
	}
	if len(out.Messages) != len(history()) {
		t.Fatalf("exclusion must stub content, not drop messages: got %d messages, want %d", len(out.Messages), len(history()))
	}
}

func TestCompilerWithoutExclusionsIsUnaffected(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	in := history()
	out := c.Compile(promptcompiler.Input{History: in, ExcludedToolCallIDs: nil})
	if !reflect.DeepEqual(out.Messages, in) {
		t.Fatal("nil exclusions must not alter the compiled prompt")
	}
}

func TestPinnedToolCallOverridesExclusion(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	out := c.Compile(promptcompiler.Input{
		History:             history(),
		ExcludedToolCallIDs: map[string]bool{"c1": true},
		PinnedToolCallIDs:   map[string]bool{"c1": true},
	})

	var result message.ToolResult
	found := false
	for _, m := range out.Messages {
		for _, r := range m.ToolResults() {
			if r.ToolCallID == "c1" {
				result, found = r, true
			}
		}
	}
	if !found {
		t.Fatal("pinned tool result should still be present")
	}
	if result.Content != "match" {
		t.Fatalf("a pinned tool result must not be stubbed even if also excluded, got %q", result.Content)
	}
}

func historyWithRepeatedContent() []message.Message {
	big := ""
	for i := 0; i < 60; i++ {
		big += "duplicate line content padding out past the dedup threshold\n"
	}
	return []message.Message{
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "r1", Name: "view", Input: "{}"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "r1", Content: big}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "r2", Name: "view", Input: "{}"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "r2", Content: big}}},
	}
}

func TestPagingCompilerDedupsRepeatedContentByDefault(t *testing.T) {
	c := promptcompiler.NewPagingCompiler()
	out := c.Compile(promptcompiler.Input{History: historyWithRepeatedContent()})

	var first string
	for _, m := range out.Messages {
		for _, r := range m.ToolResults() {
			if r.ToolCallID == "r1" {
				first = r.Content
			}
		}
	}
	if first == historyWithRepeatedContent()[1].ToolResults()[0].Content {
		t.Fatal("the earlier occurrence of duplicate content should be stubbed by dedup")
	}
}

func TestPinnedToolCallOverridesDedup(t *testing.T) {
	c := promptcompiler.NewPagingCompiler()
	out := c.Compile(promptcompiler.Input{
		History:           historyWithRepeatedContent(),
		PinnedToolCallIDs: map[string]bool{"r1": true},
	})

	var first string
	for _, m := range out.Messages {
		for _, r := range m.ToolResults() {
			if r.ToolCallID == "r1" {
				first = r.Content
			}
		}
	}
	want := historyWithRepeatedContent()[1].ToolResults()[0].Content
	if first != want {
		t.Fatal("a pinned tool result must not be stubbed by dedup even as an earlier duplicate occurrence")
	}
}

func TestPagingCompilerAppliesExclusions(t *testing.T) {
	c := promptcompiler.NewPagingCompiler()
	out := c.Compile(promptcompiler.Input{History: history(), ExcludedToolCallIDs: map[string]bool{"c1": true}})

	for _, m := range out.Messages {
		for _, r := range m.ToolResults() {
			if r.ToolCallID == "c1" && r.Content == "match" {
				t.Fatal("excluded tool result content must not reach the paging-compiled prompt")
			}
		}
	}
}

func TestCompileDecomposesIntoPages(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	in := promptcompiler.Input{
		TaskID:          "task-1",
		History:         history(),
		ProjectManifest: "# Project profile\nLanguages: go\n",
		TaskSpecText:    "Task (implementation): add feature",
	}
	out := c.Compile(in)
	pages := out.Manifest.Pages

	// Three transcript messages -> three resident pages; the tool message is a
	// tool_digest page.
	var resident, available int
	var haveToolDigest, haveManifest, haveSpec bool
	var residentTokens int64
	for _, p := range pages {
		switch p.State {
		case "resident":
			resident++
			residentTokens += p.TokenEstimate
		case "available":
			available++
		}
		if p.Kind == "tool_digest" {
			haveToolDigest = true
		}
		if p.Kind == "project_manifest" {
			haveManifest = true
		}
		if p.Kind == "task_spec" {
			haveSpec = true
		}
	}
	if resident != 3 {
		t.Fatalf("expected 3 resident pages (one per message), got %d", resident)
	}
	if available != 2 || !haveManifest || !haveSpec {
		t.Fatalf("expected project_manifest + task_spec available pages")
	}
	if !haveToolDigest {
		t.Fatalf("tool message should become a tool_digest page")
	}
	// Resident page tokens reconcile with the prompt token estimate within
	// per-page rounding tolerance: each page rounds up independently.
	delta := residentTokens - out.EstimatedTokens
	if delta < 0 {
		delta = -delta
	}
	if delta > int64(resident) {
		t.Fatalf("resident page tokens (%d) do not reconcile with prompt estimate (%d)", residentTokens, out.EstimatedTokens)
	}
}

// The stable prefix identifies the tool block providers serialize at the head of
// every request and key their prompt cache on. It must therefore change whenever
// that block changes — including a reorder, which an earlier implementation
// deliberately hid by sorting names before hashing. Keeping tool order stable is
// the assembler's job (agent.serverNames); this identity's job is to notice when
// it is not.
func TestStablePrefixTracksTheSerializedToolBlock(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	prefix := func(ts ...tools.BaseTool) string {
		return c.Compile(promptcompiler.Input{Tools: ts}).StablePrefixID
	}

	grep := fakeTool{name: "grep", desc: "search files", params: map[string]any{"q": "string"}}
	edit := fakeTool{name: "edit", desc: "edit a file"}

	if prefix(grep, edit) != prefix(grep, edit) {
		t.Fatal("the same tool block must hash identically")
	}
	if prefix(grep, edit) == prefix(edit, grep) {
		t.Fatal("reordering the tool block changes the bytes sent, so it must change the prefix")
	}
	if prefix(grep, edit) == prefix(grep) {
		t.Fatal("a different tool set must produce a different prefix")
	}

	redescribed := grep
	redescribed.desc = "search files, but faster"
	if prefix(grep, edit) == prefix(redescribed, edit) {
		t.Fatal("editing a tool description invalidates the provider cache, so it must change the prefix")
	}

	reschemad := grep
	reschemad.params = map[string]any{"q": "string", "limit": "number"}
	if prefix(grep, edit) == prefix(reschemad, edit) {
		t.Fatal("changing a parameter schema must change the prefix")
	}
}

// Schemas are built from maps, whose Go iteration order is randomized. Equal
// schemas must still hash identically or the prefix would churn for no reason.
func TestStablePrefixIsInsensitiveToSchemaMapOrdering(t *testing.T) {
	c := promptcompiler.NewCompatibilityCompiler()
	first := fakeTool{name: "grep", params: map[string]any{"a": 1, "b": 2, "c": 3, "d": 4}}
	second := fakeTool{name: "grep", params: map[string]any{"d": 4, "c": 3, "b": 2, "a": 1}}

	a := c.Compile(promptcompiler.Input{Tools: []tools.BaseTool{first}}).StablePrefixID
	b := c.Compile(promptcompiler.Input{Tools: []tools.BaseTool{second}}).StablePrefixID
	if a != b {
		t.Fatal("equal schemas built in different key orders must hash identically")
	}
}

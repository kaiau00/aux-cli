package promptcompiler_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
)

// transcript with the same large file content read twice.
func repeatedReadHistory() ([]message.Message, string) {
	bigContent := "package main\n" + strings.Repeat("// a meaningful line of the file\n", 100)
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "show me main.go"}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: "v1", Name: "view", Input: `{"file":"main.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "v1", Content: bigContent}}},
		{Role: message.Assistant, Parts: []message.ContentPart{message.TextContent{Text: "let me look again"}, message.ToolCall{ID: "v2", Name: "view", Input: `{"file":"main.go"}`}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "v2", Content: bigContent}}},
	}
	return msgs, bigContent
}

func TestPagingReducesRepeatedReads(t *testing.T) {
	msgs, bigContent := repeatedReadHistory()

	compat := promptcompiler.NewCompatibilityCompiler().Compile(promptcompiler.Input{History: msgs})
	paging := promptcompiler.NewPagingCompiler().Compile(promptcompiler.Input{History: msgs})

	// Paging sends materially fewer tokens than the full transcript.
	if paging.EstimatedTokens >= compat.EstimatedTokens {
		t.Fatalf("paging (%d) should be smaller than compat (%d)", paging.EstimatedTokens, compat.EstimatedTokens)
	}
	if paging.SavedTokens <= 0 {
		t.Fatalf("expected positive saved tokens, got %d", paging.SavedTokens)
	}

	// Lossless: exactly one full copy of the content remains in the prompt.
	full := 0
	for _, m := range paging.Messages {
		for _, part := range m.Parts {
			if tr, ok := part.(message.ToolResult); ok && tr.Content == bigContent {
				full++
			}
		}
	}
	if full != 1 {
		t.Fatalf("exactly one full copy should remain, found %d", full)
	}

	// The earlier occurrence is a compact reference, not empty (protocol pairing
	// preserved: the tool_result part still exists).
	firstTool := paging.Messages[2].ToolResults()
	if len(firstTool) != 1 {
		t.Fatalf("tool result part must be preserved for protocol pairing")
	}
	if firstTool[0].Content == bigContent {
		t.Fatalf("earlier duplicate should have been replaced with a reference")
	}
	if !strings.Contains(firstTool[0].Content, "omitted") {
		t.Fatalf("reference stub expected, got %q", firstTool[0].Content)
	}
}

func TestPagingNoOpWhenNoDuplicates(t *testing.T) {
	msgs := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "a", Content: strings.Repeat("x", 500)}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "b", Content: strings.Repeat("y", 500)}}},
	}
	out := promptcompiler.NewPagingCompiler().Compile(promptcompiler.Input{History: msgs})
	if out.SavedTokens != 0 {
		t.Fatalf("distinct content should not be deduped, saved=%d", out.SavedTokens)
	}
}

func TestPagingLeavesSmallDuplicatesAlone(t *testing.T) {
	small := "short output"
	msgs := []message.Message{
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "a", Content: small}}},
		{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: "b", Content: small}}},
	}
	out := promptcompiler.NewPagingCompiler().Compile(promptcompiler.Input{History: msgs})
	if out.SavedTokens != 0 {
		t.Fatalf("small duplicates below threshold should be left alone, saved=%d", out.SavedTokens)
	}
}

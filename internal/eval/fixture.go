// Package eval provides the deterministic, network-free evaluation harness for
// comparing control and optimized variants against the same recorded scenario
// (roadmapplan.md §5.1, §12.2 counterfactual replay, §19 PR 12). It never makes
// live provider calls in the default path.
package eval

import (
	"strings"

	"github.com/kaiau00/aux-cli/internal/message"
)

// Kind classifies a fixture scenario.
type Kind string

const (
	KindLocalizedEdit Kind = "localized_edit"
	KindCrossFile     Kind = "cross_file"
	KindRepeatedRead  Kind = "repeated_read"
)

// Fixture is a recorded, provider-neutral task transcript used to compare how
// different prompt compilers assemble the same history.
type Fixture struct {
	Name        string
	Kind        Kind
	Description string
	History     []message.Message
}

func fileContent(header string, lines int) string {
	return header + "\n" + strings.Repeat("// a representative line of source\n", lines)
}

func user(text string) message.Message {
	return message.Message{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: text}}}
}

func toolCall(id, name, input string) message.Message {
	return message.Message{Role: message.Assistant, Parts: []message.ContentPart{message.ToolCall{ID: id, Name: name, Input: input}}}
}

func toolResult(id, content string) message.Message {
	return message.Message{Role: message.Tool, Parts: []message.ContentPart{message.ToolResult{ToolCallID: id, Content: content}}}
}

// BaselineFixtures returns representative scenarios from roadmapplan.md §5.1:
// a localized single-file task, a cross-file task with distinct reads, and a
// repeated-read task where the model re-reads the same large file.
func BaselineFixtures() []Fixture {
	main := fileContent("package main", 120)
	handler := fileContent("package handler", 120)

	return []Fixture{
		{
			Name: "localized-edit", Kind: KindLocalizedEdit,
			Description: "One file read and edited; no repeated content.",
			History: []message.Message{
				user("rename the timeout constant in main.go"),
				toolCall("v1", "view", `{"file":"main.go"}`),
				toolResult("v1", main),
				toolCall("e1", "edit", `{"file":"main.go"}`),
				toolResult("e1", "edit applied"),
			},
		},
		{
			Name: "cross-file", Kind: KindCrossFile,
			Description: "Two distinct files read; discovery across files, no duplicates.",
			History: []message.Message{
				user("thread the request id from handler into main"),
				toolCall("v1", "view", `{"file":"main.go"}`),
				toolResult("v1", main),
				toolCall("v2", "view", `{"file":"handler.go"}`),
				toolResult("v2", handler),
			},
		},
		{
			Name: "repeated-read", Kind: KindRepeatedRead,
			Description: "The same large file is read twice during the task.",
			History: []message.Message{
				user("fix the bug in main.go"),
				toolCall("v1", "view", `{"file":"main.go"}`),
				toolResult("v1", main),
				user("look at main.go again after my note"),
				toolCall("v2", "view", `{"file":"main.go"}`),
				toolResult("v2", main),
			},
		},
	}
}

// Package promptcompiler produces provider-neutral model input from durable
// task/history state, separately from how that history is stored or displayed
// (roadmapplan.md §2 critical constraint, §7.2). Provider adapters translate the
// compiled messages into provider-specific formats but never choose context.
//
// PR 8 ships the compatibility compiler: it renders the stored transcript
// unchanged so history and prompt become distinct code paths with parity. Later
// phases replace the body of Compile with typed pages and demand paging behind
// the same Compiler interface.
package promptcompiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
)

// Section describes one compiled region for the manifest.
type Section struct {
	Kind          string `json:"kind"`
	MessageCount  int    `json:"messageCount,omitempty"`
	TokenEstimate int64  `json:"tokenEstimate"`
}

// PageDescriptor describes one typed page the compiler accounted for: resident
// pages are in the prompt; available pages are known but not loaded (compat
// mode). It carries the rendered content so the caller can persist a
// content-addressed page version.
type PageDescriptor struct {
	Kind          string `json:"kind"`
	StableKey     string `json:"stableKey"`
	ContentHash   string `json:"contentHash"`
	TokenEstimate int64  `json:"tokenEstimate"`
	State         string `json:"state"` // resident | available
	Reason        string `json:"reason,omitempty"`
	Content       string `json:"-"` // not serialized into the manifest
}

// ContextManifest records exactly what the compiler put into a prompt so it can
// be inspected and reconciled with the rendered token count.
type ContextManifest struct {
	TaskID         string           `json:"taskId,omitempty"`
	CallID         string           `json:"callId,omitempty"`
	Sections       []Section        `json:"sections"`
	Pages          []PageDescriptor `json:"pages"`
	ToolCount      int              `json:"toolCount"`
	TokenEstimate  int64            `json:"tokenEstimate"`
	SavedTokens    int64            `json:"savedTokens,omitempty"`
	StablePrefixID string           `json:"stablePrefixId"`
}

// CompiledPrompt is the compiler's output: the messages and tool set to send,
// plus the manifest and a stable-prefix identity for cache reasoning.
type CompiledPrompt struct {
	Messages        []message.Message
	ToolSet         []tools.BaseTool
	Manifest        ContextManifest
	StablePrefixID  string
	EstimatedTokens int64
	// SavedTokens is the estimated uncached input avoided vs. the full transcript.
	SavedTokens int64
}

// Input is the compiler's request.
type Input struct {
	TaskID  string
	CallID  string
	History []message.Message
	Tools   []tools.BaseTool
	// ProjectManifest and TaskSpecText are the compiled project/task knowledge.
	// In compatibility mode they are recorded as available (not-yet-loaded)
	// pages so the manifest shows what is known but not sent.
	ProjectManifest string
	TaskSpecText    string
	// ExcludedToolCallIDs are tool-result parts to stub out of the compiled
	// prompt (roadmapplan.md §13.11): the TUI's cross-off control changes what
	// is actually sent to the model, not just how it's displayed. Keyed by
	// message.ToolResult.ToolCallID. Nil/empty is a no-op.
	ExcludedToolCallIDs map[string]bool
	// PinnedToolCallIDs are tool-result parts whose full content is guaranteed
	// in the compiled prompt (roadmapplan.md §13.11, StatePinned): pinning
	// overrides both ExcludedToolCallIDs (a pinned-and-excluded call is still
	// sent in full) and PagingCompiler's content dedup (a pinned call is never
	// replaced by a dedup reference stub, even as an earlier duplicate
	// occurrence). Keyed by message.ToolResult.ToolCallID. Nil/empty is a no-op.
	PinnedToolCallIDs map[string]bool
}

// Compiler produces a CompiledPrompt from durable state. It is a pure function of
// its input (side effects such as page-access logging happen around it).
type Compiler interface {
	Compile(in Input) CompiledPrompt
}

// CompatibilityCompiler renders the stored transcript unchanged (parity mode).
type CompatibilityCompiler struct{}

// NewCompatibilityCompiler returns the parity-mode compiler.
func NewCompatibilityCompiler() *CompatibilityCompiler { return &CompatibilityCompiler{} }

// Compile returns the cleaned history as the prompt. Empty-part messages are
// dropped exactly as the provider adapters do, so the compiled output equals the
// legacy prompt path.
func (c *CompatibilityCompiler) Compile(in Input) CompiledPrompt {
	msgs := cleanMessages(in.History)
	msgs = applyExclusions(msgs, in.ExcludedToolCallIDs, in.PinnedToolCallIDs)
	est := EstimateMessages(msgs)
	prefix := stablePrefixID(in.Tools)
	pages := decomposePages(in, msgs)
	return CompiledPrompt{
		Messages:       msgs,
		ToolSet:        in.Tools,
		StablePrefixID: prefix,
		Manifest: ContextManifest{
			TaskID:         in.TaskID,
			CallID:         in.CallID,
			ToolCount:      len(in.Tools),
			TokenEstimate:  est,
			StablePrefixID: prefix,
			Pages:          pages,
			Sections: []Section{
				{Kind: "recent_conversation", MessageCount: len(msgs), TokenEstimate: est},
			},
		},
		EstimatedTokens: est,
	}
}

// decomposePages explains the compiled prompt page by page. Each transcript
// message becomes a resident page (a tool message becomes a tool_digest page);
// compiled project/task knowledge becomes available (not-yet-loaded) pages so
// the manifest shows what is known but not sent in compatibility mode.
func decomposePages(in Input, msgs []message.Message) []PageDescriptor {
	pages := make([]PageDescriptor, 0, len(msgs)+2)
	for i, m := range msgs {
		content := renderMessage(m)
		kind := messagePageKind(m)
		key := m.ID
		if key == "" {
			key = fmt.Sprintf("msg-%d", i)
		}
		pages = append(pages, PageDescriptor{
			Kind:          kind,
			StableKey:     "msg:" + key,
			ContentHash:   hashString(content),
			TokenEstimate: EstimateMessages([]message.Message{m}),
			State:         "resident",
			Reason:        "transcript",
			Content:       content,
		})
	}
	if in.ProjectManifest != "" {
		pages = append(pages, PageDescriptor{
			Kind: "project_manifest", StableKey: "project_manifest",
			ContentHash: hashString(in.ProjectManifest), TokenEstimate: estimateText(in.ProjectManifest),
			State: "available", Reason: "known project knowledge not loaded in compatibility mode",
			Content: in.ProjectManifest,
		})
	}
	if in.TaskSpecText != "" {
		pages = append(pages, PageDescriptor{
			Kind: "task_spec", StableKey: "task_spec:" + in.TaskID,
			ContentHash: hashString(in.TaskSpecText), TokenEstimate: estimateText(in.TaskSpecText),
			State: "available", Reason: "compiled task spec not loaded in compatibility mode",
			Content: in.TaskSpecText,
		})
	}
	return pages
}

func messagePageKind(m message.Message) string {
	if m.Role == message.Tool || len(m.ToolResults()) > 0 {
		return "tool_digest"
	}
	return "recent_conversation"
}

func renderMessage(m message.Message) string {
	var b strings.Builder
	b.WriteString(string(m.Role))
	b.WriteByte('\n')
	for _, part := range m.Parts {
		switch p := part.(type) {
		case message.TextContent:
			b.WriteString(p.Text)
		case message.ReasoningContent:
			b.WriteString(p.Thinking)
		case message.ToolCall:
			b.WriteString(p.Name)
			b.WriteString(p.Input)
		case message.ToolResult:
			b.WriteString(p.Content)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func estimateText(s string) int64 { return int64(len(s)+3) / 4 }

// applyExclusions replaces the content of excluded tool-result parts with a
// compact stub, leaving message/part structure (and tool_call/tool_result
// pairing) intact — the same shape dedupRepeatedContent uses for repeated
// content. This is what makes the TUI's cross-off control a real content
// change rather than a display-only checkbox (roadmapplan.md §13.11). A
// pinned call is never stubbed even if also excluded: pin always wins.
func applyExclusions(msgs []message.Message, excluded, pinned map[string]bool) []message.Message {
	if len(excluded) == 0 {
		return msgs
	}
	out := make([]message.Message, len(msgs))
	for mi, m := range msgs {
		newParts := make([]message.ContentPart, len(m.Parts))
		copy(newParts, m.Parts)
		changed := false
		for pi, part := range m.Parts {
			tr, ok := part.(message.ToolResult)
			if !ok || !excluded[tr.ToolCallID] || pinned[tr.ToolCallID] {
				continue
			}
			newParts[pi] = message.ToolResult{
				ToolCallID: tr.ToolCallID,
				Name:       tr.Name,
				Content:    excludedStub(len(tr.Content)),
				Metadata:   tr.Metadata,
				IsError:    tr.IsError,
			}
			changed = true
		}
		if !changed {
			out[mi] = m
			continue
		}
		out[mi] = message.Message{
			ID: m.ID, Role: m.Role, SessionID: m.SessionID, Parts: newParts,
			Model: m.Model, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		}
	}
	return out
}

func excludedStub(n int) string {
	return fmt.Sprintf("[excluded from context by user; %d bytes omitted]", n)
}

// cleanMessages drops messages with no content parts, matching provider behaviour.
func cleanMessages(in []message.Message) []message.Message {
	out := make([]message.Message, 0, len(in))
	for _, m := range in {
		if len(m.Parts) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}

// EstimateMessages returns a rough token estimate (~4 chars/token) over the text
// content of the messages. Providers report exact usage; this is for budgeting.
func EstimateMessages(msgs []message.Message) int64 {
	var chars int64
	for _, m := range msgs {
		for _, part := range m.Parts {
			switch p := part.(type) {
			case message.TextContent:
				chars += int64(len(p.Text))
			case message.ReasoningContent:
				chars += int64(len(p.Thinking))
			case message.ToolCall:
				chars += int64(len(p.Name) + len(p.Input))
			case message.ToolResult:
				chars += int64(len(p.Content))
			}
		}
	}
	return (chars + 3) / 4
}

// stablePrefixID hashes the tool set, which providers serialize at the head of
// every request and key their prompt cache on.
//
// The hash deliberately covers the tool set exactly as it will be sent: in the
// caller's order, including each tool's description and parameter schema. An
// earlier version sorted the names and hashed only those, which made the ID
// stable by construction and therefore useless — it reported an unchanged
// prefix when the tool order had been reshuffled (see agent.serverNames) or a
// description had been edited, both of which invalidate the provider's cache.
// An identity that cannot observe the change it exists to detect is worse than
// none, because it invites trusting it.
//
// Determinism of the *input* is the tool assembler's job, not this function's.
func stablePrefixID(ts []tools.BaseTool) string {
	h := sha256.New()
	for _, t := range ts {
		info := t.Info()
		h.Write([]byte(info.Name))
		h.Write([]byte{0})
		h.Write([]byte(info.Description))
		h.Write([]byte{0})
		// json.Marshal sorts map keys at every level, so an equal schema always
		// serializes identically regardless of how it was built.
		if schema, err := json.Marshal(info.Parameters); err == nil {
			h.Write(schema)
		}
		h.Write([]byte{0})
		for _, r := range info.Required {
			h.Write([]byte(r))
			h.Write([]byte{1})
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

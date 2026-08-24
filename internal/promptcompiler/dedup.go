package promptcompiler

import (
	"fmt"

	"github.com/kaiau00/aux-cli/internal/message"
)

// minDedupBytes is the smallest tool-result content worth deduplicating. Below
// this, the reference stub would not save meaningful tokens.
const minDedupBytes = 200

// DedupCompiler is the reduction that is safe to apply without evidence: when
// the same large tool-result or file content appears more than once in the
// transcript -- the model re-read a file -- only the last occurrence is sent in
// full and earlier identical copies become a compact reference. No information
// leaves the prompt, so the saving cannot cost a re-read.
//
// It is deliberately not demand paging. Nothing here evicts to fit a budget,
// and nothing in Aux does: see TODO.md A4 for why that is gated on being able
// to measure token savings against silent context loss, and A3 for the run that
// would settle this compiler's default.
type DedupCompiler struct{}

// NewDedupCompiler returns the paging compiler.
func NewDedupCompiler() *DedupCompiler { return &DedupCompiler{} }

// Compile deduplicates repeated identical large content, then produces the same
// manifest/page structure as compatibility mode over the reduced messages.
func (c *DedupCompiler) Compile(in Input) CompiledPrompt {
	original := cleanMessages(in.History)
	fullEst := EstimateMessages(original)

	deduped := dedupRepeatedContent(original, in.PinnedToolCallIDs)
	deduped = applyExclusions(deduped, in.ExcludedToolCallIDs, in.PinnedToolCallIDs)
	est := EstimateMessages(deduped)
	prefix := stablePrefixID(in.Tools)

	pagingInput := in
	pagingInput.History = deduped
	pages := decomposePages(pagingInput, deduped)

	saved := fullEst - est
	if saved < 0 {
		saved = 0
	}

	return CompiledPrompt{
		Messages:        deduped,
		ToolSet:         in.Tools,
		StablePrefixID:  prefix,
		EstimatedTokens: est,
		SavedTokens:     saved,
		Manifest: ContextManifest{
			TaskID:         in.TaskID,
			CallID:         in.CallID,
			ToolCount:      len(in.Tools),
			TokenEstimate:  est,
			SavedTokens:    saved,
			StablePrefixID: prefix,
			Pages:          pages,
			Sections: []Section{
				{Kind: "recent_conversation", MessageCount: len(deduped), TokenEstimate: est},
			},
		},
	}
}

// dedupRepeatedContent replaces every non-final occurrence of an identical large
// tool-result content with a reference stub. Messages are copied (immutable
// input); only the duplicated parts are rewritten. Tool_call/tool_result pairing
// is preserved because the result part remains, only its content shortens. A
// pinned call's content is always sent in full, even as an earlier duplicate
// occurrence that would otherwise be stubbed.
func dedupRepeatedContent(msgs []message.Message, pinned map[string]bool) []message.Message {
	// Count occurrences and find the last index (message, part) for each hash.
	type pos struct{ mi, pi int }
	last := map[string]pos{}
	count := map[string]int{}
	for mi, m := range msgs {
		for pi, part := range m.Parts {
			tr, ok := part.(message.ToolResult)
			if !ok || len(tr.Content) < minDedupBytes {
				continue
			}
			h := hashString(tr.Content)
			count[h]++
			last[h] = pos{mi, pi}
		}
	}

	out := make([]message.Message, len(msgs))
	for mi, m := range msgs {
		newParts := make([]message.ContentPart, len(m.Parts))
		copy(newParts, m.Parts)
		for pi, part := range m.Parts {
			tr, ok := part.(message.ToolResult)
			if !ok || len(tr.Content) < minDedupBytes || pinned[tr.ToolCallID] {
				continue
			}
			h := hashString(tr.Content)
			if count[h] > 1 && last[h] != (pos{mi, pi}) {
				newParts[pi] = message.ToolResult{
					ToolCallID: tr.ToolCallID,
					Name:       tr.Name,
					Content:    dedupStub(tr.Content),
					Metadata:   tr.Metadata,
					IsError:    tr.IsError,
				}
			}
		}
		out[mi] = message.Message{
			ID: m.ID, Role: m.Role, SessionID: m.SessionID, Parts: newParts,
			Model: m.Model, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		}
	}
	return out
}

func dedupStub(content string) string {
	return fmt.Sprintf("[identical output repeated later in this task; %d bytes omitted here — the full content appears in the most recent occurrence]", len(content))
}

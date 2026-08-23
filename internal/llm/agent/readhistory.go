package agent

import (
	"context"
	"encoding/json"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
)

// readHistory resolves file paths back to the tool calls that read them, which
// is what makes path-based context exclusion possible.
//
// Context pages are keyed by tool call id, but a model reasons in file paths and
// has no reliable handle on the id of a read it made several turns ago. The
// mapping is recovered the same way the context pane recovers it: by scanning
// the session's tool results for view metadata.
type readHistory struct {
	messages message.Service
	workDir  string
}

func newReadHistory(messages message.Service, workDir string) *readHistory {
	return &readHistory{messages: messages, workDir: workDir}
}

// ToolCallIDsForPaths returns, for each requested path, the ids of the tool
// calls that read it. A path that was never read is absent from the map rather
// than present with an empty slice, so callers can tell the two apart.
func (h *readHistory) ToolCallIDsForPaths(ctx context.Context, sessionID string, paths []string) (map[string][]string, error) {
	if h == nil || h.messages == nil || sessionID == "" || len(paths) == 0 {
		return nil, nil
	}

	msgs, err := h.messages.List(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]string, len(paths))
	for _, msg := range msgs {
		for _, result := range msg.ToolResults() {
			if result.IsError || result.Metadata == "" {
				continue
			}
			var meta tools.ViewResponseMetadata
			if json.Unmarshal([]byte(result.Metadata), &meta) != nil || meta.FilePath == "" {
				continue
			}
			for _, want := range paths {
				if tools.SamePath(h.workDir, want, meta.FilePath) {
					out[want] = appendUnique(out[want], result.ToolCallID)
				}
			}
		}
	}
	return out, nil
}

func appendUnique(ids []string, id string) []string {
	if id == "" {
		return ids
	}
	for _, existing := range ids {
		if existing == id {
			return ids
		}
	}
	return append(ids, id)
}

package artifact

import (
	"context"
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// Mode controls tool-output virtualization (roadmapplan.md §15.2).
type Mode string

const (
	// ModeOff never virtualizes (compatibility default).
	ModeOff Mode = "off"
	// ModeObserve stores the artifact and measures potential savings but returns
	// the response unchanged (no prompt/action change).
	ModeObserve Mode = "observe"
	// ModeOn stores the artifact and returns a compact digest + handle.
	ModeOn Mode = "on"
)

// DefaultThresholdBytes is the size above which output is a virtualization
// candidate. Matches roadmapplan.md §15.1 example.
const DefaultThresholdBytes = 12000

// Virtualizer implements tools.Virtualizer using the artifact service.
type Virtualizer struct {
	svc       *Service
	mode      Mode
	threshold int
}

// NewVirtualizer returns a tool-output virtualizer. A zero threshold uses the
// default. mode "" is treated as off.
func NewVirtualizer(svc *Service, mode Mode, thresholdBytes int) *Virtualizer {
	if mode == "" {
		mode = ModeOff
	}
	if thresholdBytes <= 0 {
		thresholdBytes = DefaultThresholdBytes
	}
	return &Virtualizer{svc: svc, mode: mode, threshold: thresholdBytes}
}

// Virtualize stores large tool output as an artifact and, when enabled, replaces
// the response with a compact digest plus a retrieval handle.
func (v *Virtualizer) Virtualize(ctx context.Context, rec *tools.ExecutionRecord, resp tools.ToolResponse) (tools.ToolResponse, error) {
	if v.mode == ModeOff || len(resp.Content) <= v.threshold {
		return resp, nil
	}

	art, reused, err := v.svc.Put(ctx, []byte(resp.Content), mediaType(resp), OwnerRef{
		Type:     "tool_execution",
		ID:       rec.ID,
		Relation: "produced",
	})
	if err != nil {
		// Never fail the tool because of virtualization; return original output.
		return resp, nil
	}
	rec.ArtifactID = art.ID
	_ = reused

	digest := buildDigest(art.ID, resp)
	if v.mode == ModeObserve {
		// Measure the potential saving without changing the response.
		rec.BytesSaved = int64(len(resp.Content) - len(digest.Content))
		return resp, nil
	}
	rec.BytesSaved = int64(len(resp.Content) - len(digest.Content))
	return digest, nil
}

func mediaType(resp tools.ToolResponse) string {
	if resp.Metadata != "" {
		return "application/json"
	}
	return "text/plain"
}

const (
	digestHeadLines = 20
	digestTailLines = 10
)

// buildDigest renders a compact, model-facing summary that preserves the head
// and tail (important for errors: exit codes and final diagnostics are usually
// at the end) and always surfaces the artifact handle for retrieval.
func buildDigest(artifactID string, resp tools.ToolResponse) tools.ToolResponse {
	lines := strings.Split(resp.Content, "\n")
	total := len(lines)
	var b strings.Builder
	fmt.Fprintf(&b, "[Output stored as artifact %s (%d bytes, %d lines). Showing head and tail; use the artifact tool to view ranges or search.]\n",
		artifactID, len(resp.Content), total)

	if total <= digestHeadLines+digestTailLines {
		b.WriteString(resp.Content)
	} else {
		b.WriteString(strings.Join(lines[:digestHeadLines], "\n"))
		fmt.Fprintf(&b, "\n... [%d lines omitted] ...\n", total-digestHeadLines-digestTailLines)
		b.WriteString(strings.Join(lines[total-digestTailLines:], "\n"))
	}

	return tools.ToolResponse{
		Type:     resp.Type,
		Content:  b.String(),
		Metadata: resp.Metadata, // structured fields preserved for the model
		IsError:  resp.IsError,
	}
}

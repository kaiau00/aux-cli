package cost

import (
	"fmt"

	"github.com/kaiau00/aux-cli/internal/llm/tools"
)

// Waste detectors (roadmapplan.md §9.4). All deterministic. They start as
// observable warnings; interventions are enabled only after replay evidence.

// Warning is a detected inefficiency with enough context to explain it.
type Warning struct {
	Detector string   `json:"detector"`
	Severity string   `json:"severity"` // info | warn
	Detail   string   `json:"detail"`
	Refs     []string `json:"refs,omitempty"`
}

// ArtifactAccess describes a stored artifact and whether it was ever read back.
type ArtifactAccess struct {
	ArtifactID string
	ByteSize   int64
	Accessed   bool
}

// WasteInput is the deterministic evidence the detectors run over.
type WasteInput struct {
	ToolExecutions []tools.ExecutionRecord
	Artifacts      []ArtifactAccess
	// RepeatedValidation counts, keyed by "commandHash|inputFingerprint", of
	// validation runs that re-executed despite unchanged inputs.
	RepeatedValidation map[string]int
}

const largeUnusedArtifactBytes = 8000

// DetectWaste returns deterministic waste warnings.
func DetectWaste(in WasteInput) []Warning {
	var warnings []Warning

	// Same tool + identical canonical input repeated (no relevant state change).
	type key struct{ tool, hash string }
	counts := map[key]int{}
	refs := map[key][]string{}
	for _, e := range in.ToolExecutions {
		if e.InputHash == "" {
			continue
		}
		k := key{e.ToolName, e.InputHash}
		counts[k]++
		refs[k] = append(refs[k], e.ID)
	}
	for k, n := range counts {
		if n > 1 {
			warnings = append(warnings, Warning{
				Detector: "repeated_tool_call",
				Severity: "warn",
				Detail:   fmt.Sprintf("tool %q ran %d times with identical input (no state change between calls)", k.tool, n),
				Refs:     refs[k],
			})
		}
	}

	// Large output artifact never accessed.
	for _, a := range in.Artifacts {
		if !a.Accessed && a.ByteSize >= largeUnusedArtifactBytes {
			warnings = append(warnings, Warning{
				Detector: "unused_large_artifact",
				Severity: "info",
				Detail:   fmt.Sprintf("artifact %s (%d bytes) was stored but never read back", a.ArtifactID, a.ByteSize),
				Refs:     []string{a.ArtifactID},
			})
		}
	}

	// Validation command repeated with unchanged affected inputs.
	for k, n := range in.RepeatedValidation {
		if n > 1 {
			warnings = append(warnings, Warning{
				Detector: "repeated_validation",
				Severity: "warn",
				Detail:   fmt.Sprintf("a validation command re-ran %d times with unchanged inputs (should reuse cached result)", n),
				Refs:     []string{k},
			})
		}
	}

	return warnings
}

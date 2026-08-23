package task

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/profile"
)

// RenderText produces a compact, model-facing summary of a spec for use as an
// available context page.
func (s Spec) RenderText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Task (%s): %s\n", s.Mode, s.Objective)
	if len(s.Constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, c := range s.Constraints {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
	}
	if len(s.AcceptanceCriteria) > 0 {
		b.WriteString("Acceptance criteria:\n")
		for _, c := range s.AcceptanceCriteria {
			fmt.Fprintf(&b, "  - %s\n", c.Description)
		}
	}
	if len(s.ValidationIntents) > 0 {
		b.WriteString("Validation:\n")
		for _, v := range s.ValidationIntents {
			fmt.Fprintf(&b, "  - %s: %s\n", v.Intent, v.Command)
		}
	}
	return b.String()
}

// defaultBudget returns a mode-appropriate budget policy. Values are directional
// defaults; the Cost Governor (Phase 1.4) refines them from measured evidence.
func defaultBudget(mode Mode) Budget {
	b := Budget{Mode: "balanced", MaxInputTokens: 120_000, MaxOutputToken: 16_000, MaxToolCalls: 60, MaxWallMS: 600_000}
	switch mode {
	case ModeResearch:
		b.MaxToolCalls = 30
	case ModeImplementation, ModeBugDiagnosis:
		b.MaxToolCalls = 80
	}
	return b
}

// Compile deterministically turns an objective into a task spec, drawing
// constraints and validation commands from the effective profile. The primary
// model still reasons within its normal call; this is the pre-tool scaffold.
func Compile(objective string, mode Mode, eff profile.Effective) Spec {
	spec := Spec{
		Objective:        strings.TrimSpace(objective),
		Mode:             mode,
		Budget:           defaultBudget(mode),
		ProfileVersionID: eff.VersionSetHash,
	}

	for _, e := range eff.Entries {
		switch e.Type {
		case profile.EntryConstraint, profile.EntryConvention:
			if c := describeEntry(e); c != "" {
				spec.Constraints = append(spec.Constraints, c)
			}
		case profile.EntryValidationCommand:
			cmd := commandOf(e.ValueJSON)
			spec.ValidationIntents = append(spec.ValidationIntents, ValidationIntent{
				ID:      ids.New(),
				Intent:  "run " + e.Key,
				Command: cmd,
				Scope:   scopeOf(e.ValueJSON),
			})
		}
	}

	spec.AcceptanceCriteria = defaultCriteria(mode, len(spec.ValidationIntents) > 0)
	return spec
}

func defaultCriteria(mode Mode, hasValidation bool) []Criterion {
	crit := func(desc string) Criterion {
		return Criterion{ID: ids.New(), Description: desc, State: CriterionUncovered}
	}
	switch mode {
	case ModeResearch, ModeCodeReview:
		return []Criterion{crit("The question is answered with evidence cited from the codebase")}
	case ModeTestAuthoring:
		return []Criterion{
			crit("Tests are added for the described behaviour"),
			crit("The new tests pass"),
		}
	default:
		out := []Criterion{crit("The requested change is implemented in scope")}
		if hasValidation {
			out = append(out, crit("Relevant project validation passes"))
		}
		return out
	}
}

func describeEntry(e profile.EffectiveEntry) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(e.ValueJSON), &m); err == nil {
		if s, ok := m["strategy"].(string); ok {
			return e.Key + ": " + s
		}
		if s, ok := m["text"].(string); ok {
			return s
		}
	}
	return e.Key
}

func commandOf(valueJSON string) string {
	var v struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(valueJSON), &v)
	return v.Command
}

func scopeOf(valueJSON string) string {
	var v struct {
		Scope string `json:"scope"`
	}
	_ = json.Unmarshal([]byte(valueJSON), &v)
	return v.Scope
}

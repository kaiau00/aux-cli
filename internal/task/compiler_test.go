package task_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/task"
)

func sampleEffective() profile.Effective {
	return profile.Effective{
		VersionSetHash: "vhash-123",
		Entries: []profile.EffectiveEntry{
			{Type: profile.EntryLanguage, Key: "go", ValueJSON: `{}`},
			{Type: profile.EntryValidationCommand, Key: "go.test", ValueJSON: `{"command":"go test ./...","scope":"repository"}`},
			{Type: profile.EntryConvention, Key: "validation.strategy", ValueJSON: `{"strategy":"targeted-then-broad"}`},
		},
	}
}

func TestCompileProducesSpec(t *testing.T) {
	spec := task.Compile("Add a caching layer", task.ModeImplementation, sampleEffective())

	if spec.Objective != "Add a caching layer" {
		t.Fatalf("objective = %q", spec.Objective)
	}
	if spec.Mode != task.ModeImplementation {
		t.Fatalf("mode = %q", spec.Mode)
	}
	if spec.ProfileVersionID != "vhash-123" {
		t.Fatalf("profile binding = %q, want vhash-123", spec.ProfileVersionID)
	}
	// Validation intent drawn from the profile command.
	if len(spec.ValidationIntents) != 1 || spec.ValidationIntents[0].Command != "go test ./..." {
		t.Fatalf("validation intents wrong: %+v", spec.ValidationIntents)
	}
	// Constraint drawn from the convention entry.
	found := false
	for _, c := range spec.Constraints {
		if strings.Contains(c, "targeted-then-broad") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected convention constraint, got %+v", spec.Constraints)
	}
	// Acceptance criteria present and uncovered.
	if len(spec.AcceptanceCriteria) == 0 {
		t.Fatalf("expected acceptance criteria")
	}
	for _, cr := range spec.AcceptanceCriteria {
		if cr.State != task.CriterionUncovered {
			t.Fatalf("new criteria should start uncovered, got %q", cr.State)
		}
	}
	// Budget set.
	if spec.Budget.MaxToolCalls == 0 {
		t.Fatalf("expected a tool-call budget")
	}
}

func TestCompileResearchModeCriteria(t *testing.T) {
	spec := task.Compile("Explain the retrieval flow", task.ModeResearch, sampleEffective())
	if len(spec.AcceptanceCriteria) != 1 {
		t.Fatalf("research should have a single evidence criterion, got %d", len(spec.AcceptanceCriteria))
	}
	if !strings.Contains(spec.AcceptanceCriteria[0].Description, "evidence") {
		t.Fatalf("research criterion should require evidence: %q", spec.AcceptanceCriteria[0].Description)
	}
}

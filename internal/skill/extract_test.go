package skill_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/skill"
)

func TestExtractBuildsAValidationSkillFromPassingCommands(t *testing.T) {
	got := skill.Extract(skill.ExtractInput{
		ProjectID:          "proj-1",
		TaskID:             "task-1",
		Objective:          "add a feature",
		SuccessfulCommands: []string{"go test ./...", "go vet ./..."},
	})

	if len(got) != 1 {
		t.Fatalf("expected exactly one candidate, got %d", len(got))
	}
	c := got[0]
	if c.Name == "" || c.Purpose == "" {
		t.Fatalf("a candidate needs a name and purpose, got %+v", c)
	}
	if len(c.Procedure) != 2 {
		t.Fatalf("expected a step per validated command, got %d", len(c.Procedure))
	}
	if len(c.ValidationRequirements) != 2 {
		t.Fatalf("the commands themselves must be recorded, got %v", c.ValidationRequirements)
	}
	// Sorted, so the same commands found in a different order do not look like
	// a different skill version.
	if c.Procedure[0].Action != "go test ./..." || c.Procedure[1].Action != "go vet ./..." {
		t.Fatalf("steps should be in a stable order, got %+v", c.Procedure)
	}
	if len(c.Exclusions) == 0 {
		t.Fatal("a candidate must state what its evidence does not prove")
	}
}

// Nothing validated means there is no evidence, and a skill without evidence is
// exactly the kind that misleads later tasks.
func TestExtractProducesNothingWithoutEvidence(t *testing.T) {
	cases := []struct {
		name string
		in   skill.ExtractInput
	}{
		{"no commands", skill.ExtractInput{ProjectID: "p", TaskID: "t"}},
		{"only blank commands", skill.ExtractInput{ProjectID: "p", TaskID: "t", SuccessfulCommands: []string{"", "  "}}},
		{"no project", skill.ExtractInput{TaskID: "t", SuccessfulCommands: []string{"go test ./..."}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := skill.Extract(c.in); len(got) != 0 {
				t.Fatalf("expected no candidates, got %+v", got)
			}
		})
	}
}

func TestExtractDeduplicatesCommands(t *testing.T) {
	got := skill.Extract(skill.ExtractInput{
		ProjectID:          "p",
		TaskID:             "t",
		SuccessfulCommands: []string{"go test ./...", "go test ./...", " go test ./... ", ""},
	})
	if len(got) != 1 {
		t.Fatalf("expected one candidate, got %d", len(got))
	}
	if len(got[0].Procedure) != 1 {
		t.Fatalf("the same command must not become several steps, got %+v", got[0].Procedure)
	}
}

func TestSourceIDsCarryProvenance(t *testing.T) {
	in := skill.ExtractInput{ProjectID: "p", TaskID: "task-42"}
	ids := skill.SourceIDsFor(in)
	if len(ids) != 1 || ids[0] != "task-42" {
		t.Fatalf("a candidate must record which task produced it, got %v", ids)
	}
	if got := skill.SourceIDsFor(skill.ExtractInput{ProjectID: "p"}); got != nil {
		t.Fatalf("no task means no provenance, got %v", got)
	}
}

func TestDescribeSummarizesACandidate(t *testing.T) {
	c := skill.Extract(skill.ExtractInput{
		ProjectID:          "p",
		TaskID:             "t",
		SuccessfulCommands: []string{"go test ./..."},
	})[0]
	if got := skill.Describe(c); !strings.Contains(got, "1 step") {
		t.Fatalf("unexpected description: %q", got)
	}
}

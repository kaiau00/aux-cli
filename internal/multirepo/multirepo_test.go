package multirepo_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/multirepo"
)

func TestCompileMultipleReposHasCrossRepoCriteria(t *testing.T) {
	plan := multirepo.Compile("wire the new payments field end to end", []multirepo.RepoTarget{
		{ProjectID: "p-api", Name: "api", Mode: "implementation"},
		{ProjectID: "p-web", Name: "web", Mode: "implementation"},
	})
	if len(plan.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(plan.Children))
	}
	// Each child is scoped to its repo and bound to its project.
	if !strings.Contains(plan.Children[0].Objective, "in api") || plan.Children[0].ProjectID != "p-api" {
		t.Fatalf("child not scoped/bound: %+v", plan.Children[0])
	}
	// Cross-repo interface compatibility criterion appears with >1 boundary.
	var hasInterface bool
	for _, c := range plan.AcceptanceCriteria {
		if c.ID == "interface-compatibility" {
			hasInterface = true
		}
	}
	if !hasInterface {
		t.Fatalf("multi-repo plan must carry an interface-compatibility criterion: %+v", plan.AcceptanceCriteria)
	}
}

func TestCompileSingleRepoHasNoInterfaceCriterion(t *testing.T) {
	plan := multirepo.Compile("fix the bug", []multirepo.RepoTarget{
		{ProjectID: "p1", Name: "svc"},
	})
	if len(plan.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(plan.Children))
	}
	if plan.Children[0].Mode != "implementation" {
		t.Fatalf("missing mode should default to implementation, got %q", plan.Children[0].Mode)
	}
	for _, c := range plan.AcceptanceCriteria {
		if c.ID == "interface-compatibility" {
			t.Fatal("a single-repo task has no integration boundary and must not carry an interface criterion")
		}
	}
	// It still requires all children to validate.
	if plan.AcceptanceCriteria[0].ID != "all-children-validated" {
		t.Fatalf("single-repo plan should still require validation: %+v", plan.AcceptanceCriteria)
	}
}

func TestCompileDeduplicatesRepos(t *testing.T) {
	plan := multirepo.Compile("x", []multirepo.RepoTarget{
		{ProjectID: "p1", Name: "a"},
		{ProjectID: "p1", Name: "a-dup"},
		{ProjectID: "", Name: "ignored"},
	})
	if len(plan.Children) != 1 {
		t.Fatalf("duplicate/empty repos must collapse to one child, got %d", len(plan.Children))
	}
}

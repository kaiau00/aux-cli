package workbench

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestChangesHeaderNoChanges(t *testing.T) {
	// §13.8: "no changes yet" is an informative state, not an empty panel.
	if got := ChangesHeaderText(viewmodel.ChangeSummaryVM{}); got != "Changes  no changes yet" {
		t.Fatalf("empty change set header = %q", got)
	}
}

func TestChangesHeaderCounts(t *testing.T) {
	vm := viewmodel.ChangeSummaryVM{
		Files:    []viewmodel.ChangedFileVM{{Path: "a.go", Operation: "add"}},
		Added:    2,
		Modified: 3,
		Deleted:  1,
	}
	got := ChangesHeaderText(vm)
	for _, want := range []string{"+2", "~3", "-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("header %q missing %q", got, want)
		}
	}
}

func TestRenderChangesEmptyRendersNothing(t *testing.T) {
	// Before anything has changed, this is boilerplate ("no changes yet" on
	// every fresh task) rather than useful information.
	if out := RenderChanges(viewmodel.ChangeSummaryVM{}, 40); out != "" {
		t.Fatalf("empty change set should render nothing, got %q", out)
	}
}

func TestRenderChangesFitsWidth(t *testing.T) {
	vm := viewmodel.ChangeSummaryVM{
		Files: []viewmodel.ChangedFileVM{
			{Path: "internal/very/long/path/to/some/file.go", Operation: "modify"},
			{Path: "b.go", Operation: "delete"},
		},
		Modified: 1,
		Deleted:  1,
	}
	const width = 24
	out := RenderChanges(vm, width)
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %q width %d exceeds %d", line, w, width)
		}
	}
}

func TestValidationNeverImpliesSuccessWithoutEvidence(t *testing.T) {
	// A stopped agent with unverified criteria must never render as validated.
	vm := viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{
		{Description: "tests pass"},   // zero state -> unverified
		{Description: "builds clean"}, // zero state -> unverified
	})
	if vm.Overall != viewmodel.StateUnverified {
		t.Fatalf("overall should be unverified, got %q", vm.Overall)
	}
	out := RenderValidation(vm, 60)
	if !strings.Contains(out, "unverified") {
		t.Fatalf("validation surface must show unverified: %q", out)
	}
	if strings.Contains(out, "validated") {
		t.Fatalf("validation surface must not claim validated without evidence: %q", out)
	}
}

func TestValidationEmptyRendersNothing(t *testing.T) {
	// Before there are any acceptance criteria, the section is boilerplate
	// ("unverified" / "no validation has run" on every fresh task) rather
	// than useful information, so it renders nothing until a criterion
	// exists. This must never be confused with claiming success: an
	// unverified criterion, once one exists, is still always shown as
	// unverified (see TestValidationNeverImpliesSuccessWithoutEvidence).
	if out := RenderValidation(viewmodel.BuildValidationSummary(nil), 40); out != "" {
		t.Fatalf("empty validation should render nothing, got %q", out)
	}
}

func TestValidationShowsValidatedWhenEvidenced(t *testing.T) {
	vm := viewmodel.ValidationSummaryVM{
		Criteria: []viewmodel.CriterionVM{{Description: "tests pass", State: viewmodel.StateValidated}},
		Overall:  viewmodel.StateValidated,
	}
	out := RenderValidation(vm, 60)
	if !strings.Contains(out, "validated") {
		t.Fatalf("validated criterion should render as validated: %q", out)
	}
}

func TestCriterionRowText(t *testing.T) {
	cases := map[viewmodel.ComponentState]string{
		viewmodel.StateValidated:  "validated",
		viewmodel.StateBlocked:    "blocked",
		viewmodel.StateFailed:     "failed",
		viewmodel.StateUnverified: "unverified",
	}
	for state, want := range cases {
		row := CriterionRowText(viewmodel.CriterionVM{Description: "x", State: state})
		if !strings.Contains(row, want) {
			t.Fatalf("state %q row %q missing %q", state, row, want)
		}
	}
}

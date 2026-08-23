package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func sampleHeaderVM() viewmodel.TaskHeaderVM {
	return viewmodel.TaskHeaderVM{
		Project:      "/Users/x/dev/aux-cli",
		Branch:       "feat/phase1",
		Objective:    "add responsive task header to the TUI",
		Mode:         "implementation",
		Stage:        "editing",
		State:        viewmodel.StateActive,
		Model:        "Claude Opus 4.8",
		ContextUsed:  18200,
		ContextLimit: 64000,
		Cost:         0.12,
	}
}

func TestPlanTaskHeaderNeverExceedsWidth(t *testing.T) {
	vm := sampleHeaderVM()
	for w := 4; w <= 200; w++ {
		line := planTaskHeader(vm, w)
		if got := lipgloss.Width(line); got > w {
			t.Fatalf("width %d: line width %d exceeds budget: %q", w, got, line)
		}
	}
}

func TestPlanTaskHeaderStageAlwaysVisible(t *testing.T) {
	vm := sampleHeaderVM()
	// The active stage word must survive every collapse step (§13.6). At w>=12
	// the stage fits as "● editing" (9 cols) even after every other field —
	// including project — has been dropped.
	for w := 12; w <= 200; w++ {
		line := planTaskHeader(vm, w)
		if !strings.Contains(line, "editing") {
			t.Fatalf("width %d: stage missing from header: %q", w, line)
		}
	}
}

func TestPlanTaskHeaderProjectAlwaysPresent(t *testing.T) {
	vm := sampleHeaderVM()
	// Project (at least its basename) is non-droppable.
	for w := 20; w <= 200; w++ {
		line := planTaskHeader(vm, w)
		if !strings.Contains(line, "aux-cli") {
			t.Fatalf("width %d: project basename missing: %q", w, line)
		}
	}
}

func TestPlanTaskHeaderFullFormShowsEverything(t *testing.T) {
	vm := sampleHeaderVM()
	line := planTaskHeader(vm, 200)
	for _, want := range []string{"aux-cli", "feat/phase1", "responsive task header", "editing", "active", "Claude Opus 4.8", "$0.12"} {
		if !strings.Contains(line, want) {
			t.Fatalf("full header missing %q: %q", want, line)
		}
	}
	// Context usage present as a token ratio at full width.
	if !strings.Contains(line, "/64K") {
		t.Fatalf("full header missing context ratio: %q", line)
	}
}

func TestPlanTaskHeaderCollapseOrder(t *testing.T) {
	vm := sampleHeaderVM()
	// goneAt returns the widest width at which marker is absent, i.e. how early
	// (at what width) a field drops as the header narrows. A larger value means
	// the field disappears sooner and is therefore less retained.
	goneAt := func(marker string) int {
		for w := 200; w >= 4; w-- {
			if !strings.Contains(planTaskHeader(vm, w), marker) {
				return w
			}
		}
		return 3
	}
	gCost := goneAt("$0.12")
	gModel := goneAt("Claude Opus")
	gTitle := goneAt("responsive")
	gBranch := goneAt("feat/phase1")

	// Reverse-priority retention (§13.6): cost/context collapses first, then the
	// model moves into details, then the task title shortens, and the project
	// (priority 1, including its branch) is retained longest.
	if !(gCost >= gModel && gModel >= gTitle && gTitle >= gBranch) {
		t.Fatalf("collapse order violated: cost=%d model=%d title=%d branch=%d (want non-increasing)", gCost, gModel, gTitle, gBranch)
	}
	if gCost <= gBranch {
		t.Fatalf("cost should drop far earlier than the project branch (cost=%d branch=%d)", gCost, gBranch)
	}
}

func TestPlanTaskHeaderEmptyProjection(t *testing.T) {
	vm := viewmodel.TaskHeaderVM{Stage: "idle", State: viewmodel.StateWaiting}
	line := planTaskHeader(vm, 80)
	if strings.TrimSpace(line) == "" {
		t.Fatalf("empty projection should still render a header, got %q", line)
	}
	if !strings.Contains(line, "idle") {
		t.Fatalf("stage should be present for empty projection: %q", line)
	}
	if !strings.Contains(line, "project") {
		t.Fatalf("placeholder project should be present: %q", line)
	}
	// No fabricated budget/cost when there is no usage.
	if strings.Contains(line, "$") || strings.Contains(line, "%") {
		t.Fatalf("empty projection must not fabricate budget/cost: %q", line)
	}
}

func TestPlanTaskHeaderZeroWidth(t *testing.T) {
	if got := planTaskHeader(sampleHeaderVM(), 0); got != "" {
		t.Fatalf("zero width should render empty, got %q", got)
	}
	if got := planTaskHeader(sampleHeaderVM(), -5); got != "" {
		t.Fatalf("negative width should render empty, got %q", got)
	}
}

func TestBudgetVariantsUnknownCost(t *testing.T) {
	vm := viewmodel.TaskHeaderVM{ContextUsed: 1000, ContextLimit: 64000, CostUnknown: true}
	forms := budgetVariants(vm)
	if len(forms) == 0 {
		t.Fatal("expected budget variants for known context")
	}
	if !strings.Contains(forms[0], "$?") {
		t.Fatalf("unknown cost should render $?, got %q", forms[0])
	}
}

func TestTruncateNeverExceedsMax(t *testing.T) {
	cases := []struct {
		s   string
		max int
	}{
		{"hello world", 5},
		{"hello", 10},
		{"", 4},
		{"x", 1},
		{"abcdef", 1},
		{"abcdef", 0},
	}
	for _, c := range cases {
		got := truncate(c.s, c.max)
		if lipgloss.Width(got) > c.max {
			t.Fatalf("truncate(%q,%d)=%q exceeds max", c.s, c.max, got)
		}
	}
}

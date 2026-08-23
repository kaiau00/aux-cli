// Package visual holds deterministic, cross-component golden snapshots for the
// Phase 2 TUI surfaces (roadmapplan.md §13.19). It renders the view-model
// fixtures the plan enumerates — empty project, active edit, multiple changed
// files, validation pass/failure, tool failure, context pressure, cost warning,
// completed validated/unverified — at wide/medium/narrow widths, strips ANSI so
// the snapshot captures layout/truncation/state rather than color, and asserts
// content/state semantics in addition to the golden text. Browser screenshot
// regression (the §13.19 browser section) is not runnable in this environment.
package visual

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/tui/components/activity"
	"github.com/kaiau00/aux-cli/internal/tui/components/contextbudget"
	"github.com/kaiau00/aux-cli/internal/tui/components/core"
	"github.com/kaiau00/aux-cli/internal/tui/components/workbench"
	"github.com/kaiau00/aux-cli/internal/tui/theme"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
	"github.com/muesli/termenv"
)

var update = flag.Bool("update", false, "update golden files")

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips ANSI styling and trailing whitespace so snapshots are stable
// across themes and editors.
func plain(s string) string {
	lines := strings.Split(ansiRE.ReplaceAllString(s, ""), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

// fixture is a deterministic composition of the Phase 2 view models.
type fixture struct {
	header     viewmodel.TaskHeaderVM
	activity   []viewmodel.ActivityGroupVM
	changes    viewmodel.ChangeSummaryVM
	validation viewmodel.ValidationSummaryVM
	budget     viewmodel.ContextBudgetVM
	// pageList drives the expanded context view (the Expand hotkey). When set,
	// the snapshot covers RenderExpanded instead of the compact budget, so the
	// expanded surface is regression-tested too.
	pageList *viewmodel.ContextPageListVM
}

// render composes every present surface at the given width into one snapshot.
func (f fixture) render(width int) string {
	var b strings.Builder
	b.WriteString(plain(core.RenderTaskHeader(f.header, width)))
	if s := plain(activity.Render(f.activity, width)); s != "" {
		b.WriteString("\n\n")
		b.WriteString(s)
	}
	if s := plain(workbench.RenderChanges(f.changes, width)); s != "" {
		b.WriteString("\n\n")
		b.WriteString(s)
	}
	if s := plain(workbench.RenderValidation(f.validation, width)); s != "" {
		b.WriteString("\n\n")
		b.WriteString(s)
	}
	if f.pageList != nil {
		if s := plain(contextbudget.RenderExpanded(*f.pageList, width)); s != "" {
			b.WriteString("\n\n")
			b.WriteString(s)
		}
	} else if s := plain(contextbudget.Render(f.budget, width)); s != "" {
		b.WriteString("\n\n")
		b.WriteString(s)
	}
	return b.String()
}

func fixtures() map[string]fixture {
	return map[string]fixture{
		// The expanded context view (Expand hotkey), grouped by binding state.
		"context-expanded": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "implementing", State: viewmodel.StateActive, Model: "Opus", ContextLimit: 64000},
			pageList: &viewmodel.ContextPageListVM{
				Resident: []viewmodel.ContextPageEntryVM{
					{PageType: "file_region", StableKey: "file:internal/app/app.go", State: "resident", Tokens: 1200, Reason: "transcript"},
				},
				Pinned: []viewmodel.ContextPageEntryVM{
					{PageType: "file_region", StableKey: "file:internal/task/coordinator.go", State: "pinned", Tokens: 900, Reason: "pinned by user"},
				},
				Available: []viewmodel.ContextPageEntryVM{
					{PageType: "project_manifest", StableKey: "project_manifest", State: "available", Tokens: 300},
				},
				Evicted: []viewmodel.ContextPageEntryVM{
					{PageType: "tool_digest", StableKey: "msg:tool-7", State: "evicted", Tokens: 4200, Reason: "demand paging"},
				},
			},
		},
		"empty-project": {
			header:     viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "idle", State: viewmodel.StateWaiting, Model: "Opus", ContextLimit: 64000},
			validation: viewmodel.BuildValidationSummary(nil),
		},
		"active-edit": {
			header: viewmodel.TaskHeaderVM{
				Project: "aux-cli", Branch: "feat/x", Objective: "add the widget",
				Stage: "editing", State: viewmodel.StateActive, Model: "Opus",
				ContextUsed: 12000, ContextLimit: 64000, Cost: 0.08,
			},
			activity: []viewmodel.ActivityGroupVM{
				{Kind: viewmodel.ActivitySearching, State: viewmodel.StateCompleted, Count: 3, DurationMS: 240},
				{Kind: viewmodel.ActivityEditing, State: viewmodel.StateActive, Count: 2, DurationMS: 1200},
			},
			changes:    viewmodel.ChangeSummaryVM{Files: []viewmodel.ChangedFileVM{{Path: "widget.go", Operation: "modify"}}, Modified: 1},
			validation: viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{{Description: "tests pass"}}),
		},
		"multiple-changed-files": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "editing", State: viewmodel.StateActive, ContextLimit: 64000},
			changes: viewmodel.ChangeSummaryVM{
				Files: []viewmodel.ChangedFileVM{
					{Path: "a.go", Operation: "add"},
					{Path: "b.go", Operation: "modify"},
					{Path: "c.go", Operation: "delete"},
				},
				Added: 1, Modified: 1, Deleted: 1,
			},
		},
		"validation-pass": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "completed", State: viewmodel.StateCompleted, ContextLimit: 64000},
			validation: viewmodel.ValidationSummaryVM{
				Criteria: []viewmodel.CriterionVM{{Description: "tests pass", State: viewmodel.StateValidated}},
				Overall:  viewmodel.StateValidated, Validated: 1,
			},
		},
		"validation-failure": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "validating", State: viewmodel.StateActive, ContextLimit: 64000},
			validation: viewmodel.ValidationSummaryVM{
				Criteria: []viewmodel.CriterionVM{
					{Description: "tests pass", State: viewmodel.StateFailed},
					{Description: "no regressions", State: viewmodel.StateBlocked},
				},
				Overall: viewmodel.StateBlocked, Blocked: 1,
			},
		},
		"tool-failure": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "testing", State: viewmodel.StateActive, ContextLimit: 64000},
			activity: []viewmodel.ActivityGroupVM{
				{Kind: viewmodel.ActivityTesting, State: viewmodel.StateFailed, Count: 2, Errors: 1, DurationMS: 5000},
			},
		},
		"context-pressure": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "thinking", State: viewmodel.StateActive, ContextUsed: 60000, ContextLimit: 64000, Cost: 1.20},
			budget: viewmodel.ContextBudgetVM{
				TotalTokens: 60000, LimitTokens: 64000,
				Categories:    []viewmodel.ContextCategoryVM{{Label: "Active code", Tokens: 40000}, {Label: "Tool results", Tokens: 20000}},
				ResidentPages: 12, PinnedPages: 2, SavedTokens: 8000,
			},
		},
		"cost-warning": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "thinking", State: viewmodel.StateActive, ContextUsed: 30000, ContextLimit: 64000, Cost: 4.75},
		},
		"completed-unverified": {
			header:     viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "completed", State: viewmodel.StateCompleted, ContextLimit: 64000},
			validation: viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{{Description: "tests pass"}, {Description: "builds"}}),
		},
		"completed-validated": {
			header: viewmodel.TaskHeaderVM{Project: "aux-cli", Stage: "completed", State: viewmodel.StateCompleted, ContextLimit: 64000},
			validation: viewmodel.ValidationSummaryVM{
				Criteria: []viewmodel.CriterionVM{
					{Description: "tests pass", State: viewmodel.StateValidated},
					{Description: "builds", State: viewmodel.StateValidated},
				},
				Overall: viewmodel.StateValidated, Validated: 2,
			},
		},
		"permission-waiting": {
			header: viewmodel.TaskHeaderVM{
				Project: "aux-cli", Branch: "feat/x", Objective: "run the migration script",
				Stage: "waiting for permission", State: viewmodel.StateWaiting, Model: "Opus",
				ContextUsed: 8000, ContextLimit: 64000,
			},
			activity: []viewmodel.ActivityGroupVM{
				{Kind: viewmodel.ActivityWaiting, State: viewmodel.StateWaiting, Count: 1},
			},
		},
		"cancelled": {
			header: viewmodel.TaskHeaderVM{
				Project: "aux-cli", Objective: "refactor the parser",
				Stage: "cancelled", State: viewmodel.StateCancelled, ContextUsed: 15000, ContextLimit: 64000, Cost: 0.22,
			},
			validation: viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{{Description: "tests pass"}}),
		},
	}
}

var widths = map[string]int{"wide": 100, "medium": 60, "narrow": 30}

func TestGoldenSnapshots(t *testing.T) {
	for name, f := range fixtures() {
		for wname, w := range widths {
			got := f.render(w)
			golden := filepath.Join("testdata", name+"."+wname+".golden")
			if *update {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden %s (run: go test ./internal/tui/visual -update): %v", golden, err)
			}
			if got != string(want) {
				t.Fatalf("golden %s mismatch — snapshots are reviewed as visual changes, not mechanically accepted:\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
			}
			// Every rendered line must fit within its width budget.
			for _, line := range strings.Split(got, "\n") {
				if len([]rune(line)) > w {
					t.Fatalf("%s@%s: line exceeds width %d: %q", name, wname, w, line)
				}
			}
		}
	}
}

// themeSample is a representative, non-exhaustive subset of registered themes
// (roadmapplan.md §13.19). The golden files themselves are theme-independent
// (plain() strips ANSI/color, and layout does not vary by theme), so this
// does not re-run the golden diff per theme — that would just duplicate
// identical files with no diagnostic value. Instead it proves every sampled
// theme actually renders every fixture without panicking and actually
// applies color (catching a theme whose color application is broken or a
// fixture that crashes under a specific palette).
var themeSample = []string{"aux", "catppuccin", "dracula", "tokyonight"}

func TestFixturesRenderAcrossThemes(t *testing.T) {
	// This test's whole point is to check that color is actually applied, but
	// lipgloss correctly detects a non-terminal test run and disables color
	// output by default — so force a color profile for the duration of this
	// test, restoring it afterward, rather than asserting against an
	// environment-dependent default.
	originalProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(originalProfile) })

	original := theme.CurrentThemeName()
	t.Cleanup(func() {
		if original != "" {
			_ = theme.SetTheme(original)
		}
	})

	fx := fixtures()
	plainBaseline := make(map[string]string, len(fx))
	for fname, f := range fx {
		plainBaseline[fname] = plain(core.RenderTaskHeader(f.header, 100))
	}

	for _, name := range themeSample {
		if theme.GetTheme(name) == nil {
			t.Fatalf("theme sample references unregistered theme %q", name)
		}
		if err := theme.SetTheme(name); err != nil {
			t.Fatalf("SetTheme(%q): %v", name, err)
		}
		for fname, f := range fx {
			raw := core.RenderTaskHeader(f.header, 100)
			if raw == "" {
				t.Fatalf("theme %q fixture %q: header rendered empty", name, fname)
			}
			if !ansiRE.MatchString(raw) {
				t.Fatalf("theme %q fixture %q: header carries no color codes; color application may be broken", name, fname)
			}
			// The plain (stripped) content must match the theme-independent
			// baseline — proving color is the only thing that varies by
			// theme, not layout or content.
			if plain(raw) != plainBaseline[fname] {
				t.Fatalf("theme %q fixture %q: plain content changed under this theme, want layout/content independent of theme", name, fname)
			}
		}
	}
}

// TestSemanticInvariants asserts state semantics independent of the golden text
// so a mechanical golden update can never silently break a safety property.
func TestSemanticInvariants(t *testing.T) {
	f := fixtures()

	// A stopped/unverified task never renders as validated (§13.9).
	unverified := f["completed-unverified"].render(100)
	if strings.Contains(unverified, "validated") {
		t.Fatalf("completed-unverified must not claim validated:\n%s", unverified)
	}
	if !strings.Contains(unverified, "unverified") {
		t.Fatalf("completed-unverified must read unverified:\n%s", unverified)
	}

	// A validation failure surfaces blocked/failed, never validated.
	fail := f["validation-failure"].render(100)
	if strings.Contains(fail, "validated") || !strings.Contains(fail, "failed") {
		t.Fatalf("validation-failure must show failed and never validated:\n%s", fail)
	}

	// A tool failure never hides its error (§13.7).
	if !strings.Contains(f["tool-failure"].render(100), "failed") {
		t.Fatalf("tool-failure must surface the error state")
	}

	// Context pressure shows a high percentage (§13.11).
	if !strings.Contains(f["context-pressure"].render(100), "93%") {
		t.Fatalf("context-pressure header should show ~93%% usage")
	}

	// The cost warning fixture surfaces the real cost.
	if !strings.Contains(f["cost-warning"].render(100), "$4.75") {
		t.Fatalf("cost-warning must show the cost")
	}
}

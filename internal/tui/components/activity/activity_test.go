package activity

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestKindLabelCoversAllKinds(t *testing.T) {
	want := map[viewmodel.ActivityKind]string{
		viewmodel.ActivitySearching: "Searching",
		viewmodel.ActivityReading:   "Reading",
		viewmodel.ActivityEditing:   "Editing",
		viewmodel.ActivityTesting:   "Testing",
		viewmodel.ActivityPlanning:  "Planning",
		viewmodel.ActivityWaiting:   "Waiting",
		viewmodel.ActivityOther:     "Working",
	}
	for kind, label := range want {
		if got := KindLabel(kind); got != label {
			t.Fatalf("KindLabel(%q)=%q want %q", kind, got, label)
		}
	}
}

func TestRowTextShowsLabelCountDuration(t *testing.T) {
	g := viewmodel.ActivityGroupVM{
		Kind:       viewmodel.ActivityEditing,
		State:      viewmodel.StateCompleted,
		Count:      3,
		DurationMS: 1500,
	}
	row := RowText(g)
	for _, want := range []string{"Editing", "3×", "1.5s"} {
		if !strings.Contains(row, want) {
			t.Fatalf("row %q missing %q", row, want)
		}
	}
}

func TestRowTextNeverHidesErrors(t *testing.T) {
	// §13.7: errors must never be collapsed away.
	g := viewmodel.ActivityGroupVM{
		Kind:   viewmodel.ActivityTesting,
		State:  viewmodel.StateFailed,
		Count:  5,
		Errors: 2,
	}
	row := RowText(g)
	if !strings.Contains(row, "2 failed") {
		t.Fatalf("failed group must surface its error count: %q", row)
	}
}

func TestRowTextSingleCountOmitsMultiplier(t *testing.T) {
	g := viewmodel.ActivityGroupVM{Kind: viewmodel.ActivityReading, State: viewmodel.StateCompleted, Count: 1}
	if strings.Contains(RowText(g), "×") {
		t.Fatalf("single-item group should not show a count multiplier: %q", RowText(g))
	}
}

func TestRenderEmptyIsEmpty(t *testing.T) {
	if got := Render(nil, 40); got != "" {
		t.Fatalf("no groups should render nothing, got %q", got)
	}
	if got := Render([]viewmodel.ActivityGroupVM{{Kind: viewmodel.ActivityReading}}, 0); got != "" {
		t.Fatalf("zero width should render nothing, got %q", got)
	}
}

func TestRenderRowsFitWidth(t *testing.T) {
	groups := []viewmodel.ActivityGroupVM{
		{Kind: viewmodel.ActivitySearching, State: viewmodel.StateCompleted, Count: 4, DurationMS: 320},
		{Kind: viewmodel.ActivityEditing, State: viewmodel.StateFailed, Count: 2, Errors: 1, DurationMS: 90_000},
	}
	const width = 30
	out := Render(groups, width)
	if out == "" {
		t.Fatal("expected rendered activity")
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("line %q width %d exceeds %d", line, w, width)
		}
	}
	// The failure must still be present even after truncation to width.
	if !strings.Contains(out, "failed") {
		t.Fatalf("rendered activity dropped the error state: %q", out)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[int64]string{
		0:      "",
		250:    "250ms",
		1500:   "1.5s",
		90_000: "1m30s",
	}
	for ms, want := range cases {
		if got := formatDuration(ms); got != want {
			t.Fatalf("formatDuration(%d)=%q want %q", ms, got, want)
		}
	}
}

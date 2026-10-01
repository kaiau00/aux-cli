package core

import "testing"

// The property that matters: whatever fitStatus decides, the segments it keeps
// must fit the width. Everything else is a preference; this is correctness,
// because overflow is what the terminal silently clips.
func totalWidth(f statusFit, help, diagnostics int) int {
	w := 0
	if f.ShowHelp {
		w += help
	}
	if f.ShowDiagnostics {
		w += diagnostics
	}
	return w
}

func TestFitStatusNeverExceedsTheWidth(t *testing.T) {
	const (
		help = 13 // "ctrl+? help" plus padding
		diag = 8
	)
	for _, width := range []int{200, 120, 100, 80, 70, 60, 50, 45, 40, 30, 20, 10, 5, 1, 0} {
		f := fitStatus(width, help, diag)
		if got := totalWidth(f, help, diag); got > width {
			t.Errorf("width=%d: kept %d cells, overflowing by %d", width, got, got-width)
		}
	}
}

// A wide terminal must lose nothing.
func TestFitStatusKeepsEverythingWhenThereIsRoom(t *testing.T) {
	f := fitStatus(200, 13, 8)
	if !f.ShowHelp || !f.ShowDiagnostics {
		t.Fatalf("a wide bar dropped something: %+v", f)
	}
}

// The order is the whole design: the help hint is the most recoverable thing
// on the bar, so it must be the first thing dropped, and diagnostics must
// outlive it.
func TestFitStatusGivesUpTheHintBeforeDiagnostics(t *testing.T) {
	const (
		help = 13
		diag = 8
	)
	// Wide enough for diagnostics but not the hint.
	f := fitStatus(diag, help, diag)
	if f.ShowHelp {
		t.Fatal("the hint should be the first thing dropped")
	}
	if !f.ShowDiagnostics {
		t.Fatalf("diagnostics should survive alone: %+v", f)
	}
}

// Below the width where even diagnostics fit, both go -- overflow is the one
// outcome this function exists to make impossible.
func TestFitStatusDropsDiagnosticsRatherThanOverflow(t *testing.T) {
	f := fitStatus(5, 13, 8)
	if f.ShowHelp || f.ShowDiagnostics {
		t.Fatalf("nothing fits in 5 cells and both must be dropped: %+v", f)
	}
}

func TestFitStatusHandlesNoRoomAtAll(t *testing.T) {
	for _, w := range []int{0, -1} {
		f := fitStatus(w, 13, 8)
		if f.ShowHelp || f.ShowDiagnostics {
			t.Fatalf("width %d should render nothing, got %+v", w, f)
		}
	}
}

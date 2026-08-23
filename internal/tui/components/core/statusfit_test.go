package core

import "testing"

// The property that matters: whatever fitStatus decides, the segments it keeps
// must fit the width. Everything else is a preference; this is correctness,
// because overflow is what the terminal silently clips.
func totalWidth(f statusFit, help, tokens, diagnostics int) int {
	w := f.ModelBudget
	if f.ShowTokens {
		w += tokens
	}
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
		help  = 13 // "ctrl+? help" plus padding
		diag  = 8
		model = 22 // e.g. " Claude Sonnet 4.5 " plus padding
	)
	for _, width := range []int{200, 120, 100, 80, 70, 60, 50, 45, 40, 30, 20, 10, 5, 1, 0} {
		for _, tokens := range []int{0, 18, 34} {
			f := fitStatus(width, help, tokens, diag, model)
			if got := totalWidth(f, help, tokens, diag); got > width {
				t.Errorf("width=%d tokens=%d: kept %d cells, overflowing by %d", width, tokens, got, got-width)
			}
		}
	}
}

// A wide terminal must lose nothing.
func TestFitStatusKeepsEverythingWhenThereIsRoom(t *testing.T) {
	f := fitStatus(200, 13, 34, 8, 22)
	if !f.ShowHelp || !f.ShowDiagnostics || !f.ShowTokens || f.ModelBudget != 22 {
		t.Fatalf("a wide bar dropped something: %+v", f)
	}
}

// The order is the whole design: the help hint is the most recoverable thing on
// the bar, the model name the least, so the model must outlive both the hint
// and the diagnostics count.
func TestFitStatusGivesUpTheHintBeforeTheModel(t *testing.T) {
	const (
		help   = 13
		diag   = 8
		model  = 22
		tokens = 34
	)
	// Wide enough for everything but the hint.
	f := fitStatus(tokens+diag+model, help, tokens, diag, model)
	if f.ShowHelp {
		t.Fatal("the hint should be the first thing dropped")
	}
	if !f.ShowDiagnostics || f.ModelBudget != model {
		t.Fatalf("dropping the hint alone should have sufficed: %+v", f)
	}

	// Wide enough only for tokens and the model.
	f = fitStatus(tokens+model, help, tokens, diag, model)
	if f.ShowHelp || f.ShowDiagnostics {
		t.Fatalf("hint and diagnostics should both be gone: %+v", f)
	}
	if f.ModelBudget != model {
		t.Fatalf("the model should still be whole: %+v", f)
	}

	// Narrower than that: the model gives ground, tokens do not.
	f = fitStatus(tokens+5, help, tokens, diag, model)
	if f.ModelBudget != 5 {
		t.Fatalf("the model should take exactly the remaining room, got %+v", f)
	}
}

// Below the width where even the token figure fits, it goes too -- overflow is
// the one outcome this function exists to make impossible.
func TestFitStatusDropsTokensRatherThanOverflow(t *testing.T) {
	f := fitStatus(10, 13, 34, 8, 22)
	if f.ShowTokens {
		t.Fatalf("tokens cannot fit in 10 cells and must be dropped: %+v", f)
	}
	if f.ModelBudget != 10 {
		t.Fatalf("the remaining room should go to the model, got %+v", f)
	}
}

func TestFitStatusHandlesNoRoomAtAll(t *testing.T) {
	for _, w := range []int{0, -1} {
		f := fitStatus(w, 13, 34, 8, 22)
		if f.ShowHelp || f.ShowDiagnostics || f.ShowTokens || f.ModelBudget != 0 {
			t.Fatalf("width %d should render nothing, got %+v", w, f)
		}
	}
}

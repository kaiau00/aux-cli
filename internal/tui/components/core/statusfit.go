package core

// statusFit decides which of the status bar's fixed segments fit across the
// terminal, and how much of the model name survives when they do not.
//
// The bar renders help + tokens + a stretched spacer + diagnostics + model. The
// spacer is what absorbs slack, so it clamps to zero and stops helping the
// moment the fixed segments alone exceed the width. Nothing then truncated
// them, so the line ran past the edge and the terminal clipped it -- and since
// the model is rendered last, the clipped piece was the one telling the user
// which model they were talking to.
//
// Segments are dropped in increasing order of how hard they are to recover
// elsewhere: the help hint first (static text, and the same key works whether
// or not it is displayed), then diagnostics (a count, visible in full on the
// diagnostics view), and only then is the model name truncated. Token and cost
// figures outlive all of those, because a budget you cannot see is the one that
// surprises you -- but they are dropped too if they alone cannot fit, since at
// that width the alternative is the overflow this exists to prevent.
type statusFit struct {
	ShowHelp        bool
	ShowDiagnostics bool
	ShowTokens      bool
	// ModelBudget is the width the model segment may occupy, including the
	// padding its style adds. Zero means it does not fit at all.
	ModelBudget int
}

// fitStatus budgets a bar of the given width. Widths are in terminal cells and
// must already account for each segment's own padding.
func fitStatus(width, help, tokens, diagnostics, model int) statusFit {
	fit := statusFit{ShowHelp: true, ShowDiagnostics: true, ShowTokens: true, ModelBudget: model}

	if width <= 0 {
		return statusFit{}
	}

	fits := func(f statusFit) int {
		used := f.ModelBudget
		if f.ShowTokens {
			used += tokens
		}
		if f.ShowHelp {
			used += help
		}
		if f.ShowDiagnostics {
			used += diagnostics
		}
		return used
	}

	if fits(fit) <= width {
		return fit
	}
	fit.ShowHelp = false
	if fits(fit) <= width {
		return fit
	}
	fit.ShowDiagnostics = false
	if fits(fit) <= width {
		return fit
	}
	// The model gives ground next; tokens stay whole while they can.
	if tokens <= width {
		fit.ModelBudget = width - tokens
		return fit
	}
	// Not even the tokens fit. Nothing else would either, so the bar becomes as
	// much of the model name as the terminal can show.
	fit.ShowTokens = false
	fit.ModelBudget = width
	return fit
}

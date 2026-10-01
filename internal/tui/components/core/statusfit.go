package core

// statusFit decides which of the status bar's fixed segments fit across the
// terminal. The bar renders help + a stretched spacer (the transient info
// message, or blank) + diagnostics. The spacer absorbs slack and clamps to
// zero, so the fixed segments must be budgeted first or they would run past
// the edge and get clipped by the terminal instead of dropped cleanly.
//
// Segments are dropped in increasing order of how hard they are to recover
// elsewhere: the help hint first (static text, and the same key works
// whether or not it is displayed), then diagnostics (a count, visible in
// full on the diagnostics view).
type statusFit struct {
	ShowHelp        bool
	ShowDiagnostics bool
}

// fitStatus budgets a bar of the given width. Widths are in terminal cells and
// must already account for each segment's own padding.
func fitStatus(width, help, diagnostics int) statusFit {
	if width <= 0 {
		return statusFit{}
	}

	fit := statusFit{ShowHelp: true, ShowDiagnostics: true}
	if help+diagnostics <= width {
		return fit
	}
	fit.ShowHelp = false
	if diagnostics <= width {
		return fit
	}
	fit.ShowDiagnostics = false
	return fit
}

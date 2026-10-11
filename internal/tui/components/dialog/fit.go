package dialog

// Sizing shared by every dialog.
//
// Each one used to carry its own fixed dimensions, and the clamps that were
// there did not work: `models.go` had a `maxDialogWidth = 40` constant that
// was never compared with the terminal at all, `init.go` clamped its width
// only after it had already rendered its text at the unclamped value,
// `arguments.go` had no clamp, and `session.go`'s clamp sat inside a
// `max(30, ...)` that put the floor back above the terminal's width.
//
// Measured at 40x14 and 60x18, the sizes the audit grid did not reach: the
// model picker rendered 47 cells wide, the init dialog and the multi-argument
// prompt 67, the file picker 48, the session picker 43. A dialog wider than
// the terminal loses its right border and every column of content past the
// edge; one taller than the terminal loses its title off the top and its
// buttons off the bottom, which for a prompt means it cannot be answered.
const (
	// dialogChrome is the columns a dialog spends on its border, its
	// horizontal padding and the overlay's drop shadow.
	dialogChrome = 7
	// dialogVerticalChrome is the rows it spends on its border and its
	// vertical padding.
	dialogVerticalChrome = 5
	// minDialogInner is the narrowest content worth rendering. Below roughly
	// 24 columns no dialog can be made to fit, and clipping is then the
	// terminal's business rather than something to pretend about.
	minDialogInner = 16
	// minDialogRows keeps at least a title and one line of content.
	minDialogRows = 3
)

// fitWidth is the inner width a dialog should render at: what it would prefer,
// reduced to what the terminal can actually show.
func fitWidth(preferred, terminal int) int {
	if terminal <= 0 {
		return preferred
	}
	return max(minDialogInner, min(preferred, terminal-dialogChrome))
}

// fitRows is how many rows of content a dialog may render: what it would
// prefer, reduced to what is left of the terminal's height once its own frame
// is accounted for.
func fitRows(preferred, terminal int) int {
	if terminal <= 0 {
		return preferred
	}
	return max(minDialogRows, min(preferred, terminal-dialogVerticalChrome))
}

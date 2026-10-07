package styles

import "time"

// WorkingSpinnerFrames rotate a dot around the inside of a single cell, using
// the four quadrant blocks. The dot is always in exactly one corner, so the
// cell's ink never changes amount -- only position -- which is what makes it
// read as motion rather than as flicker.
//
// Two things ruled out the obvious alternatives, both measured against the
// cmap of the actual fonts rather than assumed:
//
//   - Braille (the usual choice for terminal spinners, and what
//     spinner.Dot/MiniDot use) is absent from SF Mono, the default font of the
//     default macOS terminal. It would be substituted from another font, which
//     need not honour the cell grid.
//   - The partial circles that would give a smoother orbit -- U+25D0 ◐,
//     U+25D4 ◔, U+25D5 ◕ -- are absent from SF Mono too.
//
// The quadrants are present in both SF Mono and Menlo. See TestIconsAreFontSafe
// and TestWorkingSpinnerFrames.
//
// Order is clockwise from the top left. The sequence is a true cycle, so it
// loops without the snap back to the start that made the previous
// spinner.Pulse (█ ▓ ▒ ░, fading and then jumping to full) read as a flicker.
var WorkingSpinnerFrames = pickFrames(
	[]string{"▘", "▝", "▗", "▖"},
	// The classic ASCII rotation, in the same clockwise order.
	[]string{"|", "/", "-", "\\"},
)

// WorkingSpinnerFPS gives four frames half a second, so the dot makes two
// revolutions a second: fast enough to read as working, slow enough not to
// pull the eye off the transcript.
const WorkingSpinnerFPS = time.Second / 8

func pickFrames(unicode, ascii []string) []string {
	if SupportsUnicode() {
		return unicode
	}
	return ascii
}

package styles

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The working spinner sits inside a line that is clamped to exactly one row and
// truncated to the pane width, so a frame wider than one column shifts the
// label and can push the end of it off the screen. Every frame has to occupy
// one cell, in both the Unicode and the ASCII set.
func TestWorkingSpinnerFramesAreOneCellWide(t *testing.T) {
	for _, set := range []struct {
		name   string
		frames []string
	}{
		{"unicode", []string{"▘", "▝", "▗", "▖"}},
		{"ascii", []string{"|", "/", "-", "\\"}},
	} {
		for i, f := range set.frames {
			if w := ansi.StringWidth(f); w != 1 {
				t.Errorf("%s frame %d (%q) is %d cells wide, want 1", set.name, i, f, w)
			}
		}
	}
}

// The two sets must stay the same length. The spinner's period is frames×FPS,
// so a mismatch would make the animation run at a different speed depending on
// the terminal's locale -- a difference nobody would think to look for.
func TestWorkingSpinnerSetsAgreeOnLength(t *testing.T) {
	unicode := pickFrames([]string{"▘", "▝", "▗", "▖"}, []string{"|", "/", "-", "\\"})
	if len(unicode) != 4 {
		t.Fatalf("pickFrames returned %d frames, want 4", len(unicode))
	}
	if got := len(WorkingSpinnerFrames); got != 4 {
		t.Errorf("WorkingSpinnerFrames has %d frames, want 4", got)
	}
}

// A rotation reads as motion only if no position repeats within a cycle.
// spinner.Pulse's frames fade and then jump back to full, which is why it read
// as a flicker; this asserts the replacement is a genuine cycle.
func TestWorkingSpinnerFramesAreDistinct(t *testing.T) {
	seen := map[string]int{}
	for i, f := range WorkingSpinnerFrames {
		if prev, dup := seen[f]; dup {
			t.Errorf("frame %d repeats frame %d (%q); the cycle stalls there", i, prev, f)
		}
		seen[f] = i
	}
}

func TestPickFramesFollowsUnicodeSupport(t *testing.T) {
	unicode := []string{"▘", "▝"}
	ascii := []string{"|", "/"}

	t.Setenv("AUX_ASCII_ICONS", "1")
	if got := pickFrames(unicode, ascii); got[0] != ascii[0] {
		t.Errorf("with AUX_ASCII_ICONS=1 got %q, want the ASCII set", got)
	}

	t.Setenv("AUX_ASCII_ICONS", "")
	t.Setenv("AUX_UNICODE_ICONS", "1")
	if got := pickFrames(unicode, ascii); got[0] != unicode[0] {
		t.Errorf("with AUX_UNICODE_ICONS=1 got %q, want the Unicode set", got)
	}
}

package styles

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Any SGR run that sets a background: the indexed and 24-bit forms, the plain
// ANSI 40-47 and bright 100-107 ranges, and the 49 reset.
var backgroundCode = regexp.MustCompile(`\x1b\[[0-9;]*?(?:48;|\b(?:4[0-7]|49|10[0-7])\b)[0-9;]*m`)

func hasBackground(s string) bool {
	return backgroundCode.MatchString(s)
}

// The canvas belongs to the terminal. BaseStyle is what nearly every widget
// starts from, so if it paints a background then Aux paints the whole screen:
// before this rule, a 160x44 conversation emitted 4,557 background colour
// sequences against 2,203 foreground ones.
func TestBaseStyleLeavesTheBackgroundAlone(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	got := BaseStyle().Render("x")
	if hasBackground(got) {
		t.Errorf("BaseStyle painted a background: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
	}
	// It must still set a foreground, or text is unstyled rather than themed.
	if !strings.Contains(got, "38;") {
		t.Errorf("BaseStyle set no foreground: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
	}
}

// Glamour emits its own backgrounds, which is what StripBackgrounds is for on
// the canvas. Foregrounds and text attributes have to survive.
func TestStripBackgroundsRemovesOnlyBackgrounds(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"24-bit background", "\x1b[48;2;26;21;18mX\x1b[0m"},
		{"indexed background", "\x1b[48;5;234mX\x1b[0m"},
		{"plain ANSI background", "\x1b[41mX\x1b[0m"},
		{"bright ANSI background", "\x1b[101mX\x1b[0m"},
		{"foreground and background together", "\x1b[38;2;232;220;200;48;2;26;21;18mX\x1b[0m"},
		{"bold, foreground and background", "\x1b[1;38;2;201;138;60;48;2;26;21;18mX\x1b[0m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := StripBackgrounds(tc.input)
			if hasBackground(got) {
				t.Errorf("background survived: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
			}
			if !strings.Contains(got, "X") {
				t.Errorf("content lost: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
			}
		})
	}
}

func TestStripBackgroundsKeepsForegroundAndBold(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	got := StripBackgrounds("\x1b[1;38;2;201;138;60;48;2;26;21;18mX\x1b[0m")
	for _, want := range []string{"38;2;201;138;60", "1;"} {
		if !strings.Contains(got, want) {
			t.Errorf("StripBackgrounds dropped %q: %q", want, strings.ReplaceAll(got, "\x1b", "ESC"))
		}
	}
}

// A surface that has to be opaque -- a dialog over the transcript -- still gets
// a background. The canvas rule is not "never paint".
func TestForceReplaceStillPaintsForSurfaces(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	got := ForceReplaceBackgroundWithLipgloss("\x1b[38;2;1;2;3mX\x1b[0m",
		lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1a1512"})
	if !hasBackground(got) {
		t.Errorf("a surface must still be able to paint: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
	}
}

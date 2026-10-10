package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/kaiau00/aux-cli/internal/tui/styles"
	"github.com/muesli/termenv"
)

// withColorProfile sets the profile for one test and puts back whatever was
// there. lipgloss keeps it in a package-level variable, so a test that sets
// TrueColor and walks away changes how every later test in the package
// renders -- which is how three scrollback tests started failing on text that
// was suddenly broken up by escape sequences.
func withColorProfile(t *testing.T, p termenv.Profile) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// The mark is three lines that have to stack. A ragged line shifts the
// information set beside it and the wordmark reads as corruption, so the
// widths are pinned rather than eyeballed.
func TestWordmarkLinesAreTheSameWidth(t *testing.T) {
	if len(wordmark) == 0 {
		t.Fatal("there is no wordmark")
	}
	want := ansi.StringWidth(wordmark[0])
	for i, line := range wordmark {
		if got := ansi.StringWidth(line); got != want {
			t.Errorf("wordmark line %d is %d cells wide, line 0 is %d", i, got, want)
		}
	}
	if want == 0 {
		t.Error("the wordmark is zero cells wide")
	}
}

// What the banner is for: saying which Aux this is, which model it will use,
// and where it is pointed. All three are read at render time, so a stale
// banner cannot claim the wrong model.
func TestBannerNamesAuxTheModelAndTheDirectory(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.Ascii)

	got := ansi.Strip(Banner(100))
	if !strings.Contains(got, "Aux") {
		t.Errorf("the banner does not name Aux: %q", got)
	}
	if !strings.Contains(got, wordmark[0]) {
		t.Errorf("the banner is missing its mark: %q", got)
	}
	// The working directory is the config's, whatever the test's temp dir is.
	if lines := strings.Split(got, "\n"); len(lines) < 3 {
		t.Errorf("the banner is %d lines, want the mark plus its information: %q", len(lines), got)
	}
}

// A mark that does not fit has to be dropped, not wrapped. Wrapping splits the
// letters across rows, which looks like a rendering fault rather than a logo,
// and the information lines are the part that carries meaning.
func TestNarrowTerminalsDropTheMarkRatherThanWrapIt(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.Ascii)

	markWidth := ansi.StringWidth(wordmark[0])
	for _, width := range []int{1, 10, 20, markWidth, markWidth + 5} {
		got := Banner(width)
		if got == "" {
			t.Errorf("width %d: the banner rendered nothing at all", width)
			continue
		}
		plain := ansi.Strip(got)
		if strings.Contains(plain, wordmark[0]) {
			t.Errorf("width %d: the mark was kept although it needs %d cells", width, markWidth)
		}
		for _, line := range strings.Split(plain, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: a line is %d cells wide, %d over", width, w, w-width)
				break
			}
		}
	}
}

// Every banner line has to fit the terminal it was given. The banner is
// printed, so a line that overflows is wrapped by the terminal itself and
// cannot be taken back.
func TestBannerNeverOverflowsTheTerminal(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.Ascii)

	for _, width := range []int{30, 40, 60, 70, 80, 100, 120, 160, 200} {
		for _, line := range strings.Split(ansi.Strip(Banner(width)), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: %q is %d cells, %d over", width, line, w, w-width)
			}
		}
	}
}

// With Unicode off the mark cannot be drawn at all: the blocks it is built
// from are the glyphs that would be missing. The information still has to
// come through.
func TestAsciiTerminalsGetTheInformationWithoutTheMark(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.Ascii)
	t.Setenv("AUX_ASCII_ICONS", "1")
	if styles.SupportsUnicode() {
		t.Fatal("test setup: AUX_ASCII_ICONS did not take effect")
	}

	got := ansi.Strip(Banner(100))
	if !strings.Contains(got, "Aux") {
		t.Errorf("the ASCII banner does not name Aux: %q", got)
	}
	for _, r := range got {
		if r > 127 && r != '\n' {
			t.Errorf("the ASCII banner contains a non-ASCII rune %q: %q", r, got)
			break
		}
	}
}

// The canvas belongs to the terminal (D17). The banner is printed into the
// user's scrollback and stays there, so a background painted here is a block
// of Aux's colour sitting in their terminal for the rest of the session.
func TestBannerPaintsNoBackground(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.TrueColor)

	got := Banner(100)
	if backgroundCode.MatchString(got) {
		t.Errorf("the banner painted a background: %q", strings.ReplaceAll(got, "\x1b", "ESC"))
	}
}

// The banner is printed, so whatever is on those lines is in the user's
// scrollback for good -- including the padding lipgloss adds to the right of
// every line, which comes back when they select and copy it.
func TestBannerLinesCarryNoTrailingPadding(t *testing.T) {
	loadConfig(t)
	withColorProfile(t, termenv.TrueColor)

	for _, width := range []int{40, 80, 100, 160} {
		for i, line := range strings.Split(ansi.Strip(Banner(width)), "\n") {
			if line != strings.TrimRight(line, " ") {
				t.Errorf("width %d: line %d is padded to %d cells: %q",
					width, i, len(line), line)
			}
		}
	}
}

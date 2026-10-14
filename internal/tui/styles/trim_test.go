package styles

import "testing"

// The padding lipgloss leaves is inside the styled run: the line ends in the
// sequence that closes the run, not in the spaces. A plain strings.TrimRight
// finds nothing to remove, which is how the first version of this silently did
// nothing at all.
func TestTrimTrailingSpaceReachesPastTheClosingEscape(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{
			"padding before the reset",
			"\x1b[38;2;1;2;3mhello    \x1b[0m",
			"\x1b[38;2;1;2;3mhello\x1b[0m",
		},
		{
			"padding with no escapes at all",
			"hello    ",
			"hello",
		},
		{
			"interior spaces are content",
			"\x1b[1mhello there\x1b[0m  ",
			"\x1b[1mhello there\x1b[0m",
		},
		{
			"several lines, each padded",
			"\x1b[1ma  \x1b[0m\n\x1b[1mbb \x1b[0m",
			"\x1b[1ma\x1b[0m\n\x1b[1mbb\x1b[0m",
		},
		{
			"nothing to do",
			"\x1b[1mhello\x1b[0m",
			"\x1b[1mhello\x1b[0m",
		},
		{
			"a line that is only padding keeps its escapes",
			"\x1b[1m   \x1b[0m",
			"\x1b[1m\x1b[0m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := TrimTrailingSpace(tc.in); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

// Whatever is trimmed, the escape sequences have to survive in order, or a
// style is left open and bleeds into the rest of the terminal.
func TestTrimTrailingSpaceKeepsEveryEscape(t *testing.T) {
	in := "\x1b[38;5;2m\x1b[1mword\x1b[22m   \x1b[39m  "
	got := TrimTrailingSpace(in)
	for _, esc := range []string{"\x1b[38;5;2m", "\x1b[1m", "\x1b[22m", "\x1b[39m"} {
		if !contains(got, esc) {
			t.Errorf("escape %q was dropped: %q", esc, got)
		}
	}
	if contains(got, "   ") {
		t.Errorf("trailing padding survived: %q", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

package chat

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"testing"
)

func TestComposerPlaceholder(t *testing.T) {
	cases := []struct {
		followUp, slash bool
		want            string
	}{
		{false, false, "Describe a task for Aux…"},
		{true, false, "Reply or refine the task…"},
		{false, true, "Run a command…"},
		{true, true, "Run a command…"}, // slash wins
	}
	for _, c := range cases {
		if got := composerPlaceholder(c.followUp, c.slash); got != c.want {
			t.Fatalf("composerPlaceholder(%v,%v)=%q want %q", c.followUp, c.slash, got, c.want)
		}
	}
}

func TestComposerHintReflectsState(t *testing.T) {
	// Busy shows a clear disabled reason and cancel affordance.
	if got := composerHint(200, true, true, false); !strings.Contains(got, "working") || !strings.Contains(got, "cancel") {
		t.Fatalf("busy hint should state working + cancel: %q", got)
	}
	// Unfocused invites focus.
	if got := composerHint(200, false, false, false); !strings.Contains(got, "focus") {
		t.Fatalf("unfocused hint should invite focus: %q", got)
	}
	// Focused surfaces send + the multiline affordance (discoverable, not hidden).
	focused := composerHint(200, true, false, false)
	for _, want := range []string{"enter send", "newline", "editor"} {
		if !strings.Contains(focused, want) {
			t.Fatalf("focused hint missing %q: %q", want, focused)
		}
	}
	// Attachment delete affordance only appears when there are attachments.
	if strings.Contains(focused, "delete") {
		t.Fatalf("no-attachment hint should not mention delete: %q", focused)
	}
	if got := composerHint(200, true, false, true); !strings.Contains(got, "delete") {
		t.Fatalf("attachment hint should mention delete: %q", got)
	}
}

func TestIsSlashCommand(t *testing.T) {
	cases := map[string]bool{
		"/init":        true,
		"  /compact":   true,
		"/":            true,
		"/run args":    false, // has whitespace -> composing args, not the command token
		"hello":        false,
		"":             false,
		"a/b":          false,
		"/multi\nline": false,
	}
	for in, want := range cases {
		if got := isSlashCommand(in); got != want {
			t.Fatalf("isSlashCommand(%q)=%v want %v", in, got, want)
		}
	}
}

// The hint occupies exactly one reserved row, so at any terminal width it must
// come back as a single line that fits. Dropping the optional affordances is
// fine; wrapping is not, because the row above it is the conversation and the
// row below it does not exist.
func TestComposerHintAlwaysFitsOnOneLine(t *testing.T) {
	for width := 1; width <= 120; width++ {
		for _, busy := range []bool{false, true} {
			for _, focused := range []bool{false, true} {
				got := composerHint(width, focused, busy, true)
				if strings.Contains(got, "\n") {
					t.Fatalf("width %d: hint wrapped to more than one line: %q", width, got)
				}
				if w := ansi.StringWidth(got); w > width {
					t.Fatalf("width %d: hint is %d columns wide: %q", width, w, got)
				}
			}
		}
	}
}

// Narrowing the terminal must cost the least important affordance first. A
// user who cannot see "enter send" has no way to guess how to send at all.
func TestComposerHintKeepsSendLongestAsItNarrows(t *testing.T) {
	wide := composerHint(120, true, false, false)
	if !strings.Contains(wide, "ctrl+f attach") {
		t.Fatalf("a wide terminal should show every hint, got %q", wide)
	}
	narrow := composerHint(24, true, false, false)
	if !strings.Contains(narrow, "enter send") {
		t.Fatalf("a narrow terminal must still show how to send, got %q", narrow)
	}
	if strings.Contains(narrow, "ctrl+f attach") {
		t.Fatalf("a narrow terminal should have dropped the attach hint, got %q", narrow)
	}
}

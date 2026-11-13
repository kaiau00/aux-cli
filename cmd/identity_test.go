package cmd

import (
	"strings"
	"testing"
)

// `aux --help` is the first thing a stranger reads, and it was OpenCode's text:
// "Aux is a powerful terminal-based AI assistant that helps with software
// development tasks", which says nothing true that is specific to Aux and
// claims nothing about where the model comes from.
//
// M2.7's done-when is that `rg "powerful terminal-based AI assistant"` finds
// nothing. A grep run once is not a guarantee, so it is asserted here.
func TestHelpSaysWhatAuxIs(t *testing.T) {
	long := rootCmd.Long
	short := rootCmd.Short

	for _, phrase := range []string{
		"powerful terminal-based AI assistant",
		"Terminal-based AI assistant for software development",
	} {
		if strings.Contains(long, phrase) || strings.Contains(short, phrase) {
			t.Errorf("the help text still says %q", phrase)
		}
	}

	// The three facts a stranger needs: what it is, whose model it runs, and
	// where it came from.
	for _, want := range []string{
		"open-source",
		"harness",
		"your own API key",
		"OpenCode",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("the help text never says %q:\n%s", want, long)
		}
	}
	if !strings.Contains(short, "harness") {
		t.Errorf("the one-line description does not say what Aux is: %q", short)
	}
}

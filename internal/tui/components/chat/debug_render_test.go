package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/muesli/termenv"
)

// TestRenderedMessageMatchesTerminalColorProfile is a regression test for a
// real incident on Apple's Terminal.app: glamour.NewTermRenderer defaults to
// termenv.TrueColor unconditionally, with no auto-detection of its own, so
// message text (rendered through glamour) was always emitted as raw 24-bit
// color regardless of what the terminal actually supports — while the
// message's background (rendered separately, through lipgloss) had already
// been fixed to respect the detected profile. On a terminal with incomplete
// TrueColor support, that split let text and background diverge silently:
// the background renders as intended in the terminal's own reduced palette
// while the foreground drops or misrenders, since the two were never
// generated for the same profile.
//
// This asserts foreground and background are emitted in the *same* profile
// (both ANSI256, neither one still 24-bit), which is what GetMarkdownRenderer
// pinning to lipgloss.ColorProfile() is responsible for.
func TestRenderedMessageMatchesTerminalColorProfile(t *testing.T) {
	origDark := lipgloss.HasDarkBackground()
	defer lipgloss.SetHasDarkBackground(origDark)
	lipgloss.SetHasDarkBackground(true)
	origProfile := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(origProfile)
	lipgloss.SetColorProfile(termenv.ANSI256)

	msg := message.Message{
		ID:   "m1",
		Role: message.User,
		Parts: []message.ContentPart{
			message.TextContent{Text: "hi there"},
		},
	}
	out := renderUserMessage(msg, false, 80, 0).content

	if strings.Contains(out, "38;2;") {
		t.Fatalf("expected no 24-bit foreground codes at ANSI256 profile, got %q", out)
	}
	if strings.Contains(out, "48;2;") {
		t.Fatalf("expected no 24-bit background codes at ANSI256 profile, got %q", out)
	}
	if !strings.Contains(out, "38;5;") {
		t.Fatalf("expected an ANSI256 foreground code, got %q", out)
	}
	if !strings.Contains(out, "48;5;") {
		t.Fatalf("expected an ANSI256 background code, got %q", out)
	}
}

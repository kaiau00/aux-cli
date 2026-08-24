package chat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The splash renders the working directory on a single line. Left to lipgloss
// it wrapped, splitting a directory name across three rows; whatever it does
// now, the result has to fit on one line and still say which project this is.
func TestDisplayPathFitsOnOneLine(t *testing.T) {
	const home = "/Users/kaiau"
	paths := []string{
		"/Users/kaiau/repos/aux",
		"/var/folders/j_/psxvl1bs5j16mf_6mkt30dd40000gn/T/TestChatPage1804343695/001",
		"/a/b/c",
		"/Users/kaiau/some/very/deeply/nested/project/directory/that/keeps/going/on",
		"/" + strings.Repeat("x", 200),
	}
	for _, p := range paths {
		for width := 4; width <= 90; width++ {
			got := displayPath(p, home, width)
			if strings.Contains(got, "\n") {
				t.Fatalf("displayPath(%q, %d) wrapped: %q", p, width, got)
			}
			if w := ansi.StringWidth(got); w > width {
				t.Fatalf("displayPath(%q, %d) is %d columns wide: %q", p, width, w, got)
			}
		}
	}
}

// Shortening must drop the leading directories, never the trailing ones. The
// last segment is the project name -- losing it makes the line useless.
func TestDisplayPathKeepsTheProjectName(t *testing.T) {
	got := displayPath("/Users/kaiau/some/very/deeply/nested/aux-cli", "/Users/kaiau", 20)
	if !strings.Contains(got, "aux-cli") {
		t.Fatalf("expected the trailing segment to survive shortening, got %q", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("expected an ellipsis to mark the dropped leading segments, got %q", got)
	}
}

// A path under the home directory reads better as "~/...", and it buys back
// columns before any segment has to be dropped.
func TestDisplayPathCollapsesHome(t *testing.T) {
	if got := displayPath("/Users/kaiau/repos/aux", "/Users/kaiau", 40); got != "~/repos/aux" {
		t.Fatalf("got %q, want %q", got, "~/repos/aux")
	}
	// A path outside home keeps its absolute form.
	if got := displayPath("/opt/work/aux", "/Users/kaiau", 40); got != "/opt/work/aux" {
		t.Fatalf("got %q, want %q", got, "/opt/work/aux")
	}
}

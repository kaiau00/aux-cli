package diff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
)

// GenerateDiff reads config.WorkingDirectory, which panics when config has not
// been loaded, so the package needs one loaded before any test runs.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "diff-test-*")
	if err != nil {
		panic(err)
	}
	if _, err := config.Load(dir, false); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// The counts GenerateDiff returns are not decoration: they are the "+N -M"
// shown on the permission dialog that asks whether to apply an edit. If they
// are wrong, someone approves a change whose size they were told incorrectly.
func TestGenerateDiffCountsASubstitution(t *testing.T) {
	_, additions, removals := GenerateDiff("one\ntwo\nthree\n", "one\nTWO\nthree\n", "f.go")

	if additions != 1 || removals != 1 {
		t.Fatalf("replacing one line is +1 -1, got +%d -%d", additions, removals)
	}
}

// The counts describe the *rendered* diff, not the semantic edit, and the two
// are not always the same number: udiff is free to express one appended line as
// a remove-and-add pair when the surrounding context shifts, so appending
// "four" to a three-line file reports +3 -2 rather than +1 -0.
//
// That is correct for what these numbers are for -- they sit beside the diff
// text and must describe the lines on screen. Asserting the relationship rather
// than hard-coding udiff's choices keeps that contract pinned without making the
// test brittle to how the dependency formats a change.
func TestGenerateDiffCountsMatchTheRenderedDiff(t *testing.T) {
	for _, tc := range []struct{ name, before, after string }{
		{"substitution", "one\ntwo\nthree\n", "one\nTWO\nthree\n"},
		{"append", "one\ntwo\nthree\n", "one\ntwo\nthree\nfour\n"},
		{"deletion", "one\ntwo\nthree\n", "one\nthree\n"},
		{"whole file replaced", "a\nb\n", "x\ny\nz\n"},
		{"added to an empty file", "", "first\n"},
		{"emptied", "first\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diffText, additions, removals := GenerateDiff(tc.before, tc.after, "f.go")

			var wantAdd, wantRemove int
			for line := range strings.SplitSeq(diffText, "\n") {
				switch {
				case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
					// File headers, not changed lines.
				case strings.HasPrefix(line, "+"):
					wantAdd++
				case strings.HasPrefix(line, "-"):
					wantRemove++
				}
			}

			if additions != wantAdd || removals != wantRemove {
				t.Fatalf("reported +%d -%d but the diff shows +%d -%d:\n%s", additions, removals, wantAdd, wantRemove, diffText)
			}
			if additions == 0 && removals == 0 {
				t.Fatalf("a real change must be counted as something:\n%s", diffText)
			}
		})
	}
}

// The +++/--- file headers both begin with the characters the counter looks for.
// Counting them would inflate every diff by one each, so this pins the
// exclusion rather than leaving it to the reader of the loop.
func TestGenerateDiffDoesNotCountFileHeaders(t *testing.T) {
	diffText, additions, removals := GenerateDiff("a\n", "b\n", "f.go")

	if !strings.Contains(diffText, "+++") || !strings.Contains(diffText, "---") {
		t.Fatalf("expected a unified diff with both file headers:\n%s", diffText)
	}
	if additions != 1 || removals != 1 {
		t.Fatalf("got +%d -%d, want +1 -1; the ---/+++ headers must not be counted", additions, removals)
	}
}

func TestGenerateDiffOfIdenticalContentIsEmpty(t *testing.T) {
	diffText, additions, removals := GenerateDiff("same\n", "same\n", "f.go")

	if additions != 0 || removals != 0 {
		t.Fatalf("identical content must report no changes, got +%d -%d", additions, removals)
	}
	if diffText != "" {
		t.Fatalf("identical content should produce no diff, got %q", diffText)
	}
}

// Paths are made relative to the working directory so the same change does not
// render differently depending on where the repository happens to live.
func TestGenerateDiffStripsTheWorkingDirectory(t *testing.T) {
	cwd := config.WorkingDirectory()
	absolute := filepath.Join(cwd, "pkg", "thing.go")

	diffText, _, _ := GenerateDiff("a\n", "b\n", absolute)

	if strings.Contains(diffText, cwd) {
		t.Fatalf("the working directory must not appear in the diff:\n%s", diffText)
	}
	if !strings.Contains(diffText, "a/pkg/thing.go") {
		t.Fatalf("expected a path relative to the working directory:\n%s", diffText)
	}
}

// --- parsing --------------------------------------------------------------

func TestParseUnifiedDiffReadsHeadersAndHunks(t *testing.T) {
	input := `--- a/f.go
+++ b/f.go
@@ -1,3 +1,3 @@
 one
-two
+TWO
 three`

	result, err := ParseUnifiedDiff(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.OldFile != "f.go" || result.NewFile != "f.go" {
		t.Fatalf("file names: got %q / %q", result.OldFile, result.NewFile)
	}
	if len(result.Hunks) != 1 {
		t.Fatalf("expected one hunk, got %d", len(result.Hunks))
	}

	var added, removed, context int
	for _, l := range result.Hunks[0].Lines {
		switch l.Kind {
		case LineAdded:
			added++
		case LineRemoved:
			removed++
		case LineContext:
			context++
		}
	}
	if added != 1 || removed != 1 || context != 2 {
		t.Fatalf("got +%d -%d context=%d, want +1 -1 context=2", added, removed, context)
	}
}

// A diff with several hunks must not drop the last one -- hunks are appended
// when the *next* header is seen, so the final hunk depends on the flush after
// the loop.
func TestParseUnifiedDiffKeepsTheFinalHunk(t *testing.T) {
	input := `--- a/f.go
+++ b/f.go
@@ -1,2 +1,2 @@
 one
-two
+TWO
@@ -10,2 +10,2 @@
 ten
-eleven
+ELEVEN`

	result, err := ParseUnifiedDiff(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Hunks) != 2 {
		t.Fatalf("expected both hunks, got %d", len(result.Hunks))
	}
}

// Content before any hunk header has no hunk to belong to and must be ignored
// rather than mis-attributed or panicking on a nil hunk.
func TestParseUnifiedDiffIgnoresLinesBeforeAnyHunk(t *testing.T) {
	input := `--- a/f.go
+++ b/f.go
some preamble
-orphan
@@ -1,1 +1,1 @@
-two
+TWO`

	result, err := ParseUnifiedDiff(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Hunks) != 1 {
		t.Fatalf("expected one hunk, got %d", len(result.Hunks))
	}
	for _, l := range result.Hunks[0].Lines {
		if strings.Contains(l.Content, "orphan") {
			t.Fatal("a line before the first hunk header must not be attributed to a hunk")
		}
	}
}

func TestParseUnifiedDiffOfEmptyInputHasNoHunks(t *testing.T) {
	result, err := ParseUnifiedDiff("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Hunks) != 0 {
		t.Fatalf("expected no hunks, got %d", len(result.Hunks))
	}
}

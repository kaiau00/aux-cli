package completions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaiau00/aux-cli/internal/tui/components/dialog"
)

// TestFileCompletionSurvivesSymlinkLoop reproduces the shape of directory tree
// that previously let file completion (rg -L | fzf, with no timeout or result
// bound) enumerate an effectively unbounded number of paths and exhaust
// system memory: a symlink that loops back to an ancestor directory. It must
// now complete quickly regardless.
func TestFileCompletionSurvivesSymlinkLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(dir, filepath.Join(dir, "loop")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() { _ = os.Chdir(orig) }()

	group := NewFileAndFolderContextGroup()

	type result struct {
		items []dialog.CompletionItemI
		err   error
	}
	done := make(chan result, 1)
	go func() {
		items, err := group.GetChildEntries("real")
		done <- result{items, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("GetChildEntries returned an error instead of bounded results: %v", r.err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("GetChildEntries did not return within 15s — a symlink loop is not bounded")
	}
}

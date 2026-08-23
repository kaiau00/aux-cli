package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetContextFromPaths(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	_, err := config.Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	cfg := config.Get()
	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{
		"file.txt",
		"directory/",
	}
	testFiles := []string{
		"file.txt",
		"directory/file_a.txt",
		"directory/file_b.txt",
		"directory/file_c.txt",
	}

	createTestFiles(t, tmpDir, testFiles)

	context := getContextFromPaths()
	expectedContext := fmt.Sprintf("# From:%s/file.txt\nfile.txt: test content\n# From:%s/directory/file_a.txt\ndirectory/file_a.txt: test content\n# From:%s/directory/file_b.txt\ndirectory/file_b.txt: test content\n# From:%s/directory/file_c.txt\ndirectory/file_c.txt: test content", tmpDir, tmpDir, tmpDir, tmpDir)
	assert.Equal(t, expectedContext, context)
}

func createTestFiles(t *testing.T, tmpDir string, testFiles []string) {
	t.Helper()
	for _, path := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if path[len(path)-1] == '/' {
			err := os.MkdirAll(fullPath, 0755)
			require.NoError(t, err)
		} else {
			dir := filepath.Dir(fullPath)
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(path+": test content"), 0644)
			require.NoError(t, err)
		}
	}
}

// The context block goes into the system prompt, which is the prefix providers
// key their cache on. An earlier implementation collected results from a shared
// channel across one goroutine per path, so the order was whatever order those
// goroutines finished in -- a varying prefix throws away a cache that is
// otherwise served ~99% from hit, and it made this package's tests flaky.
func TestContextPathOrderIsDeterministic(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{
		"first.txt",
		"beta/file_a.txt",
		"beta/file_b.txt",
		"alpha/file_a.txt",
		"last.txt",
	})
	// Deliberately not alphabetical: the output must follow the configured
	// order, not the filesystem's.
	paths := []string{"first.txt", "beta/", "alpha/", "last.txt"}

	want := processContextPaths(tmpDir, paths)
	for i := 0; i < 40; i++ {
		if got := processContextPaths(tmpDir, paths); got != want {
			t.Fatalf("run %d produced a different order:\n got %q\nwant %q", i, got, want)
		}
	}

	// And that order is the configured one.
	firstAt := strings.Index(want, "first.txt")
	betaAt := strings.Index(want, "beta/file_a.txt")
	alphaAt := strings.Index(want, "alpha/file_a.txt")
	lastAt := strings.Index(want, "last.txt")
	if !(firstAt < betaAt && betaAt < alphaAt && alphaAt < lastAt) {
		t.Fatalf("output should follow the configured path order, got:\n%s", want)
	}
}

// A file reachable from two configured paths must appear once.
func TestContextPathsDeduplicate(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"dir/dupe.txt"})

	got := processContextPaths(tmpDir, []string{"dir/", "dir/dupe.txt"})
	if n := strings.Count(got, "dupe.txt: test content"); n != 1 {
		t.Fatalf("expected the file once, got %d occurrences:\n%s", n, got)
	}
}

func TestContextPathsToleratesMissingPaths(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"real.txt"})

	got := processContextPaths(tmpDir, []string{"nope.txt", "real.txt", "missing_dir/"})
	if !strings.Contains(got, "real.txt: test content") {
		t.Fatalf("a missing path must not suppress the ones that exist, got %q", got)
	}
}

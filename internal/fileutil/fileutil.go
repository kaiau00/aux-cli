package fileutil

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/kaiau00/aux-cli/internal/logging"
)

var (
	rgPath  string
	fzfPath string
)

func init() {
	var err error
	rgPath, err = exec.LookPath("rg")
	if err != nil {
		logging.Warn("Ripgrep (rg) not found in $PATH. Some features might be limited or slower.")
		rgPath = ""
	}
	fzfPath, err = exec.LookPath("fzf")
	if err != nil {
		logging.Warn("FZF not found in $PATH. Some features might be limited or slower.")
		fzfPath = ""
	}
}

// GetRgCmd builds a ripgrep file-listing command bound to ctx. It deliberately
// does not pass -L/--follow: following symlinks with no depth or result bound
// can walk into a cloud-sync placeholder mount or a symlink cycle and
// enumerate an effectively unbounded number of paths, which has previously
// exhausted system memory. Callers should give ctx a deadline so a
// pathological directory tree can't hang indefinitely either.
func GetRgCmd(ctx context.Context, globPattern string) *exec.Cmd {
	if rgPath == "" {
		return nil
	}
	rgArgs := []string{
		"--files",
		"--null",
	}
	if globPattern != "" {
		if !filepath.IsAbs(globPattern) && !strings.HasPrefix(globPattern, "/") {
			globPattern = "/" + globPattern
		}
		rgArgs = append(rgArgs, "--glob", globPattern)
	}
	cmd := exec.CommandContext(ctx, rgPath, rgArgs...)
	cmd.Dir = "."
	return cmd
}

// GetFzfCmd builds an fzf filter command bound to ctx; see GetRgCmd for why
// callers should give ctx a deadline.
func GetFzfCmd(ctx context.Context, query string) *exec.Cmd {
	if fzfPath == "" {
		return nil
	}
	fzfArgs := []string{
		"--filter",
		query,
		"--read0",
		"--print0",
	}
	cmd := exec.CommandContext(ctx, fzfPath, fzfArgs...)
	cmd.Dir = "."
	return cmd
}

type FileInfo struct {
	Path    string
	ModTime time.Time
}

func SkipHidden(path string) bool {
	// Check for hidden files (starting with a dot)
	base := filepath.Base(path)
	if base != "." && strings.HasPrefix(base, ".") {
		return true
	}

	commonIgnoredDirs := map[string]bool{
		".aux":             true,
		"node_modules":     true,
		"vendor":           true,
		"dist":             true,
		"build":            true,
		"target":           true,
		".git":             true,
		".idea":            true,
		".vscode":          true,
		"__pycache__":      true,
		"bin":              true,
		"obj":              true,
		"out":              true,
		"coverage":         true,
		"tmp":              true,
		"temp":             true,
		"logs":             true,
		"generated":        true,
		"bower_components": true,
		"jspm_packages":    true,
	}

	parts := strings.Split(path, string(os.PathSeparator))
	for _, part := range parts {
		if commonIgnoredDirs[part] {
			return true
		}
	}
	return false
}

// errLimitReached stops GlobWithDoublestar's walk early once enough matches
// are collected. doublestar's GlobWalk (unlike stdlib fs.WalkDir) does not
// special-case fs.SkipAll from the callback as a clean stop — it propagates
// whatever the callback returns as a genuine walk error — so a private
// sentinel is used instead and unwrapped below rather than surfaced to callers.
var errLimitReached = errors.New("glob limit reached")

func GlobWithDoublestar(pattern, searchPath string, limit int) ([]string, bool, error) {
	fsys := os.DirFS(searchPath)
	relPattern := strings.TrimPrefix(pattern, "/")
	var matches []FileInfo

	err := doublestar.GlobWalk(fsys, relPattern, func(path string, d fs.DirEntry) error {
		if d.IsDir() {
			return nil
		}
		if SkipHidden(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		absPath := path
		if !strings.HasPrefix(absPath, searchPath) && searchPath != "." {
			absPath = filepath.Join(searchPath, absPath)
		} else if !strings.HasPrefix(absPath, "/") && searchPath == "." {
			absPath = filepath.Join(searchPath, absPath) // Ensure relative paths are joined correctly
		}

		matches = append(matches, FileInfo{Path: absPath, ModTime: info.ModTime()})
		if limit > 0 && len(matches) >= limit*2 {
			return errLimitReached
		}
		return nil
	})
	if err != nil && !errors.Is(err, errLimitReached) {
		return nil, false, fmt.Errorf("glob walk error: %w", err)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].ModTime.After(matches[j].ModTime)
	})

	truncated := false
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
		truncated = true
	}

	results := make([]string, len(matches))
	for i, m := range matches {
		results[i] = m.Path
	}
	return results, truncated, nil
}

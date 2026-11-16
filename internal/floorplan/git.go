package floorplan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// gitStateTimeout bounds the git calls that read repository state. These are
// cheap commands on any repository; a timeout this generous only fires when
// something is wrong, and a map without git state is still a map.
const gitStateTimeout = 10 * time.Second

// RepoStatus is the git state of a checkout, as far as the map needs it.
//
// Outside a repository every field is empty. That is an ordinary case, not a
// failure: Aux is useful in a directory that was never a repository, and a
// floor plan of one should still be built.
type RepoStatus struct {
	Head   string   // commit id at HEAD
	Branch string   // checked-out branch, empty when detached
	Remote string   // the "origin" fetch URL
	Dirty  []string // repo-relative paths that differ from HEAD, sorted
}

// RepoState reads the git state of root.
//
// Failure to reach git is reported as an empty state rather than an error. The
// only error returned is for a root that cannot be examined at all.
func RepoState(ctx context.Context, root string) (RepoStatus, error) {
	if _, err := os.Stat(root); err != nil {
		return RepoStatus{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, gitStateTimeout)
	defer cancel()

	var st RepoStatus
	st.Head = gitTrimmed(ctx, root, "rev-parse", "HEAD")
	if st.Head == "" {
		return RepoStatus{}, nil // not a repository, or one with no commits
	}
	if b := gitTrimmed(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); b != "HEAD" {
		st.Branch = b // "HEAD" means detached, which is not a branch name
	}
	st.Remote = gitTrimmed(ctx, root, "remote", "get-url", "origin")
	st.Dirty = dirtyFiles(ctx, root)
	return st, nil
}

// gitRaw runs one git command and returns its output untouched, reporting
// whether it succeeded.
func gitRaw(ctx context.Context, root string, args ...string) (string, bool) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// gitTrimmed runs one git command and returns its trimmed output, or "" if it
// failed for any reason.
//
// Trimming suits commands whose whole output is one value. It must not be used
// on status output, whose first column is a status code that may itself be a
// space -- trimming there eats the first path's leading character.
func gitTrimmed(ctx context.Context, root string, args ...string) string {
	out, ok := gitRaw(ctx, root, args...)
	if !ok {
		return ""
	}
	return strings.TrimSpace(out)
}

// dirtyFiles lists the paths that differ from HEAD, sorted.
func dirtyFiles(ctx context.Context, root string) []string {
	out, ok := gitRaw(ctx, root, "status", "--porcelain", "-z", "--untracked-files=all")
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	var paths []string
	for _, rel := range parseStatus(out) {
		if seen[rel] {
			continue
		}
		seen[rel] = true
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	return paths
}

// parseStatus reads the paths out of `git status --porcelain -z` output.
//
// Each record is two status codes, a space, then the path. A rename or a copy
// is two records in one: the new path, then the old path as a bare field with
// no status codes of its own. Both ends are reported, because a rename changes
// the map in both places, and because consuming the second field is what stops
// it being misread as a record and losing its first three characters.
func parseStatus(out string) []string {
	fields := strings.Split(out, "\x00")
	var paths []string
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue // the trailing empty field, or nothing parseable
		}
		codes, rel := f[:2], filepath.ToSlash(f[3:])
		if rel != "" {
			paths = append(paths, rel)
		}
		if strings.ContainsAny(codes, "RC") && i+1 < len(fields) {
			i++
			if src := filepath.ToSlash(fields[i]); src != "" {
				paths = append(paths, src)
			}
		}
	}
	return paths
}

// Record is this state as the built document carries it.
func (s RepoStatus) Record(root string) Repo {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	return Repo{
		Path:   ".",
		Name:   filepath.Base(abs),
		Branch: s.Branch,
		Head:   s.Head,
		Remote: s.Remote,
	}
}

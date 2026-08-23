// Package worktree creates and removes isolated git worktrees so a subagent's
// file changes land on their own branch and directory rather than directly on
// the parent's working tree. It is a thin wrapper over
// the `git worktree` porcelain — deterministic and testable without a fake,
// the same choice internal/project makes for VCS inspection (see
// internal/project/vcs.go).
package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Worktree is a created git worktree: an isolated working directory checked
// out on its own branch.
type Worktree struct {
	Path   string
	Branch string
}

// Create adds a new git worktree at path, checked out on a new branch created
// from base ("HEAD" when base is empty). repoRoot must be inside a git working
// tree; path must not already exist.
func Create(ctx context.Context, repoRoot, path, branch, base string) (Worktree, error) {
	if base == "" {
		base = "HEAD"
	}
	if err := run(ctx, repoRoot, "worktree", "add", "-b", branch, path, base); err != nil {
		return Worktree{}, fmt.Errorf("failed to create worktree %q on branch %q: %w", path, branch, err)
	}
	return Worktree{Path: path, Branch: branch}, nil
}

// Remove removes a worktree. force also removes one with uncommitted or
// unmerged changes, which is expected once a subagent's result has been
// reported back to the parent and the worktree is being torn down regardless
// of what it left behind.
func Remove(ctx context.Context, repoRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if err := run(ctx, repoRoot, args...); err != nil {
		return fmt.Errorf("failed to remove worktree %q: %w", path, err)
	}
	return nil
}

// DeleteBranch deletes a branch created by Create. `git worktree remove` frees
// the directory but leaves the branch ref behind, so without this every
// subagent run would leave a permanent aux/subagent/* ref in the user's repo.
// force is required because a subagent's branch is normally unmerged, which is
// exactly the state a plain `git branch -d` refuses to delete.
func DeleteBranch(ctx context.Context, repoRoot, branch string) error {
	if branch == "" {
		return nil
	}
	if err := run(ctx, repoRoot, "branch", "-D", branch); err != nil {
		return fmt.Errorf("failed to delete branch %q: %w", branch, err)
	}
	return nil
}

// SyncWorkingTree overlays repoRoot's live working directory onto dst, a
// worktree created via Create (checked out at HEAD or another commit). Create
// alone only reflects the last commit; a subagent working against dst needs
// to see the same uncommitted edits and untracked files the parent is
// currently looking at, or checks like "run the tests" and "review the
// current changes" would silently operate on stale code. The file set to
// copy comes from `git ls-files -c -o --exclude-standard`, i.e. every tracked
// file plus every untracked-but-not-ignored file — the same definition of
// "what's in the working tree" git itself uses, so build artifacts and
// dependency directories that are gitignored are skipped automatically
// rather than needing a hand-maintained exclude list.
func SyncWorkingTree(ctx context.Context, repoRoot, dst string) error {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-files", "-c", "-o", "--exclude-standard", "-z").Output()
	if err != nil {
		return fmt.Errorf("failed to list working tree files in %q: %w", repoRoot, err)
	}
	trimmed := strings.TrimRight(string(out), "\x00")
	if trimmed == "" {
		return nil
	}
	for _, rel := range strings.Split(trimmed, "\x00") {
		srcPath := filepath.Join(repoRoot, rel)
		info, err := os.Lstat(srcPath)
		if err != nil || !info.Mode().IsRegular() {
			// Deleted locally, a symlink, or otherwise not a plain file we can
			// copy: leave whatever Create's checkout already put at dst.
			continue
		}
		dstPath := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("failed to prepare %q: %w", dstPath, err)
		}
		if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
			return fmt.Errorf("failed to sync %q into worktree: %w", rel, err)
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// Snapshot returns a content hash for every regular file under root (relative
// path -> SHA-256 hex digest), skipping .git. It is the baseline half of
// detecting what a subagent's Bash commands changed inside its worktree: take
// a Snapshot right after SyncWorkingTree, run the subagent, Snapshot again,
// and pass both to DiffSnapshots. A plain content hash is used instead of a
// git commit as the baseline so nothing here touches the worktree's git state
// or triggers repo hooks meant for the user's own commits.
func Snapshot(root string) (map[string]string, error) {
	sums := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		sums[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot %q: %w", root, err)
	}
	return sums, nil
}

// DiffSnapshots returns the sorted relative paths that were added, removed,
// or modified between two Snapshot results.
func DiffSnapshots(before, after map[string]string) []string {
	changed := make(map[string]struct{}, len(before)+len(after))
	for path, sum := range after {
		if before[path] != sum {
			changed[path] = struct{}{}
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed[path] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for path := range changed {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func run(ctx context.Context, dir string, args ...string) error {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

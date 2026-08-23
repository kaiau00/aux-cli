package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/worktree"
)

func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestCreateAndRemoveWorktree(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	wtPath := filepath.Join(t.TempDir(), "subagent-1")

	wt, err := worktree.Create(ctx, repo, wtPath, "aux/subagent-1", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if wt.Path != wtPath || wt.Branch != "aux/subagent-1" {
		t.Fatalf("unexpected worktree: %+v", wt)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); err != nil {
		t.Fatalf("expected the worktree to contain the repo's tracked files: %v", err)
	}

	// The worktree is isolated: a file written there does not appear in repo.
	if err := os.WriteFile(filepath.Join(wtPath, "subagent-only.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "subagent-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected the parent repo to be unaffected by the worktree's file, got err=%v", err)
	}

	if err := worktree.Remove(ctx, repo, wtPath, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree path to be removed, got err=%v", err)
	}
}

func TestSyncWorkingTreeOverlaysUncommittedAndUntrackedFiles(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Dirty the parent repo after the commit newTestRepo already made:
	// modify a tracked file (uncommitted change) and add an untracked one.
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("skip me\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	wtPath := filepath.Join(t.TempDir(), "subagent-sync")
	if _, err := worktree.Create(ctx, repo, wtPath, "aux/subagent-sync", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Before syncing, the worktree only has the committed (unmodified) content.
	if got, _ := os.ReadFile(filepath.Join(wtPath, "README.md")); string(got) != "hello\n" {
		t.Fatalf("expected pre-sync worktree to have the committed content, got %q", got)
	}

	if err := worktree.SyncWorkingTree(ctx, repo, wtPath); err != nil {
		t.Fatalf("SyncWorkingTree: %v", err)
	}

	if got, err := os.ReadFile(filepath.Join(wtPath, "README.md")); err != nil || string(got) != "modified\n" {
		t.Fatalf("expected the worktree to pick up the uncommitted edit, got %q err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(wtPath, "untracked.txt")); err != nil || string(got) != "new\n" {
		t.Fatalf("expected the worktree to pick up the untracked file, got %q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected the gitignored file to be skipped, got err=%v", err)
	}
}

func TestDiffSnapshotsDetectsAddedModifiedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	write("unchanged.txt", "same\n")
	write("modified.txt", "before\n")
	write("removed.txt", "gone soon\n")

	before, err := worktree.Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	write("modified.txt", "after\n")
	write("added.txt", "new\n")
	if err := os.Remove(filepath.Join(dir, "removed.txt")); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	after, err := worktree.Snapshot(dir)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	changed := worktree.DiffSnapshots(before, after)
	want := map[string]bool{"modified.txt": true, "added.txt": true, "removed.txt": true}
	if len(changed) != len(want) {
		t.Fatalf("got %v, want paths %v", changed, want)
	}
	for _, p := range changed {
		if !want[p] {
			t.Fatalf("unexpected changed path %q in %v", p, changed)
		}
	}
	for _, p := range changed {
		if p == "unchanged.txt" {
			t.Fatal("unchanged.txt must not be reported as changed")
		}
	}
}

func TestRemoveAndDeleteBranchLeavesNoRefBehind(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	wtPath := filepath.Join(t.TempDir(), "subagent-refs")
	const branch = "aux/subagent/refs"

	if _, err := worktree.Create(ctx, repo, wtPath, branch, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := worktree.Remove(ctx, repo, wtPath, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	branches := func() string {
		t.Helper()
		out, err := exec.Command("git", "-C", repo, "branch", "--list", branch).CombinedOutput()
		if err != nil {
			t.Fatalf("git branch --list: %v\n%s", err, out)
		}
		return string(out)
	}

	// `git worktree remove` on its own leaves the branch ref behind, which is
	// the leak this guards against.
	if !strings.Contains(branches(), "aux/subagent/refs") {
		t.Skip("git removed the branch with the worktree; nothing left to assert")
	}
	if err := worktree.DeleteBranch(ctx, repo, branch); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if got := branches(); strings.Contains(got, "aux/subagent/refs") {
		t.Fatalf("expected the branch ref to be gone, still listed: %q", got)
	}
}

func TestDeleteBranchEmptyNameIsNoOp(t *testing.T) {
	if err := worktree.DeleteBranch(context.Background(), t.TempDir(), ""); err != nil {
		t.Fatalf("empty branch name should be a no-op, got %v", err)
	}
}

func TestCreateFailsOnDuplicateBranch(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	if _, err := worktree.Create(ctx, repo, filepath.Join(t.TempDir(), "wt1"), "aux/dup", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := worktree.Create(ctx, repo, filepath.Join(t.TempDir(), "wt2"), "aux/dup", ""); err == nil {
		t.Fatal("expected an error creating a worktree on an already-checked-out branch")
	}
}

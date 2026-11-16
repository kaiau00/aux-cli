package floorplan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// mustWrite writes one file under root, creating parents.
func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// gitRun runs one git command in root and fails the test if it does not succeed.
func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git %v failed: %v: %s", args, err, out)
	}
}

// commitTree makes root a repository with one commit holding every file in it.
func commitTree(t *testing.T, root string) {
	t.Helper()
	gitInit(t, root)
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "commit", "-q", "-m", "initial")
}

func TestRepoStateReportsHeadAndBranch(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	commitTree(t, root)

	st, err := RepoState(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoState: %v", err)
	}
	if len(st.Head) != 40 {
		t.Errorf("Head = %q, want a 40-character commit id", st.Head)
	}
	if st.Branch == "" {
		t.Error("Branch is empty, want the checked-out branch")
	}
	if len(st.Dirty) != 0 {
		t.Errorf("Dirty = %v, want none in a clean tree", st.Dirty)
	}
}

func TestRepoStateListsDirtyFiles(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	commitTree(t, root)
	mustWrite(t, root, "main.go", "package main\n\nfunc main() {}\n")
	mustWrite(t, root, "added.go", "package main\n")

	st, err := RepoState(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoState: %v", err)
	}
	want := []string{"added.go", "main.go"}
	if len(st.Dirty) != len(want) {
		t.Fatalf("Dirty = %v, want %v", st.Dirty, want)
	}
	for i, p := range want {
		if st.Dirty[i] != p {
			t.Errorf("Dirty[%d] = %q, want %q (sorted)", i, st.Dirty[i], p)
		}
	}
}

func TestRepoStateOutsideARepositoryIsNotAnError(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})

	st, err := RepoState(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoState outside a repository: %v", err)
	}
	if st.Head != "" || st.Branch != "" {
		t.Errorf("got head %q branch %q, want both empty outside a repository", st.Head, st.Branch)
	}
}

func TestRepoRecordNamesTheDirectory(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	commitTree(t, root)

	st, err := RepoState(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoState: %v", err)
	}
	rec := st.Record(root)
	if rec.Name == "" {
		t.Error("Record().Name is empty, want the directory name")
	}
	if rec.Head != st.Head {
		t.Errorf("Record().Head = %q, want %q", rec.Head, st.Head)
	}
}

func TestParseStatusKeepsTheLeadingStatusColumn(t *testing.T) {
	// A modified-but-unstaged file's record begins with a space. Trimming the
	// output before splitting it eats that space and with it the first
	// character of the path.
	got := parseStatus(" M main.go\x00?? added.go\x00")
	want := []string{"main.go", "added.go"}
	if len(got) != len(want) {
		t.Fatalf("parseStatus = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseStatus[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseStatusReportsBothEndsOfARename(t *testing.T) {
	// A rename is one record carrying two NUL-separated paths: the new one,
	// then the old. Reading the second as a record of its own would strip its
	// first three characters.
	got := parseStatus("R  new/place.go\x00old/place.go\x00")
	want := []string{"new/place.go", "old/place.go"}
	if len(got) != len(want) {
		t.Fatalf("parseStatus = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseStatus[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseStatusIgnoresTheTrailingEmptyField(t *testing.T) {
	if got := parseStatus(""); len(got) != 0 {
		t.Errorf("parseStatus(\"\") = %v, want nothing", got)
	}
	if got := parseStatus("\x00"); len(got) != 0 {
		t.Errorf("parseStatus of one empty field = %v, want nothing", got)
	}
}

func TestRepoStateReportsBothEndsOfARename(t *testing.T) {
	root := writeTree(t, map[string]string{"old.go": "package main\n"})
	commitTree(t, root)
	gitRun(t, root, "mv", "old.go", "new.go")

	st, err := RepoState(context.Background(), root)
	if err != nil {
		t.Fatalf("RepoState: %v", err)
	}
	found := map[string]bool{}
	for _, p := range st.Dirty {
		found[p] = true
	}
	if !found["new.go"] || !found["old.go"] {
		t.Errorf("Dirty = %v, want both new.go and old.go", st.Dirty)
	}
}

// bumpModTime moves a file's modification time forward by a second, so a
// stat-based change check sees it regardless of filesystem timestamp
// granularity. Tests write and re-write files far faster than a filesystem
// records, so without this a genuine edit can look unchanged.
func bumpModTime(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat %s: %v", rel, err)
	}
	next := info.ModTime().Add(time.Second)
	if err := os.Chtimes(abs, next, next); err != nil {
		t.Fatalf("chtimes %s: %v", rel, err)
	}
}

// mustRemove deletes a file under root.
func mustRemove(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

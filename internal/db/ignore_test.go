package db

import (
	"os"
	"path/filepath"
	"testing"
)

// The data directory defaults to .aux inside the working directory, which is
// normally the user's own repository, and it holds full session transcripts. If
// nothing marks it ignorable, `git add -A` commits every prompt and every piece
// of tool output the agent was shown.
func TestDataDirectoryIsGitIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := ignoreSelf(dir); err != nil {
		t.Fatalf("ignoreSelf: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("no .gitignore was written: %v", err)
	}
	if string(body) == "" {
		t.Fatal("the .gitignore must actually exclude something")
	}
	found := false
	for _, line := range []string{"*"} {
		if containsLine(string(body), line) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the .gitignore must exclude the whole directory; got:\n%s", body)
	}
}

// A user who edits the file has said something; rewriting it on every start
// would undo that without telling them.
func TestExistingGitIgnoreIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	const edited = "# deliberately empty\n"
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := ignoreSelf(dir); err != nil {
		t.Fatalf("ignoreSelf: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != edited {
		t.Fatalf("a user's edit was overwritten; got:\n%s", body)
	}
}

func containsLine(body, want string) bool {
	start := 0
	for i := 0; i <= len(body); i++ {
		if i == len(body) || body[i] == '\n' {
			if body[start:i] == want {
				return true
			}
			start = i + 1
		}
	}
	return false
}

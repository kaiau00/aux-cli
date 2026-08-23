package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
)

// VCSInfo is deterministic version-control metadata for a directory.
type VCSInfo struct {
	Type      string // "git" | "none"
	Root      string // canonical VCS root, or the input dir when Type == "none"
	Remote    string // raw remote URL, empty when none
	Revision  string // HEAD commit sha, empty when none/unborn
	Branch    string // branch name, empty when detached or none
	DirtyHash string // fingerprint of the working-tree dirt, empty when clean
}

// VCS inspects version-control state. It is an interface so tests can inject a
// deterministic fake instead of shelling out.
type VCS interface {
	Inspect(ctx context.Context, dir string) (VCSInfo, error)
}

// GitVCS inspects git repositories by invoking the git binary. Preferring git
// metadata over reimplementation keeps analysis deterministic (deterministic analysis over LLM/heuristics).
type GitVCS struct{}

func (GitVCS) Inspect(ctx context.Context, dir string) (VCSInfo, error) {
	root, err := gitOutput(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		// Not a git repository (or git unavailable): local-only identity.
		return VCSInfo{Type: "none", Root: dir}, nil
	}
	info := VCSInfo{Type: "git", Root: root}
	info.Remote, _ = gitOutput(ctx, dir, "remote", "get-url", "origin")
	info.Revision, _ = gitOutput(ctx, dir, "rev-parse", "HEAD")
	if branch, _ := gitOutput(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "HEAD" {
		info.Branch = branch
	}
	if status, _ := gitOutput(ctx, dir, "status", "--porcelain"); status != "" {
		sum := sha256.Sum256([]byte(status))
		info.DirtyHash = hex.EncodeToString(sum[:])
	}
	return info, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

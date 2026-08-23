package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kaiau00/aux-cli/internal/permission"
)

// canonicalPath returns path in absolute, symlink-resolved form. A path that
// does not exist yet is resolved through its nearest existing ancestor, so a
// read of a missing file is still judged by where it would live — and a
// symlink pointing outside the working directory can't disguise itself as an
// inside path.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	current, remainder := abs, ""
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			if remainder == "" {
				return resolved
			}
			return filepath.Join(resolved, remainder)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}

// withinDir reports whether target resolves to root or somewhere beneath it.
// Both sides are canonicalized first, so neither `..` traversal nor a symlink
// can smuggle an outside path past the check.
func withinDir(root, target string) bool {
	r, t := canonicalPath(root), canonicalPath(target)
	if r == t {
		return true
	}
	rel, err := filepath.Rel(r, t)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RequireReadAccess gates a filesystem read. Reads inside the call's working
// directory (the project, or a subagent's isolated worktree) are always
// allowed and never prompt, so ordinary work is unaffected. Anything outside
// needs explicit user approval: without this gate, a prompt injection could
// make the agent read ~/.ssh/id_rsa or ~/.aux.json and feed it straight into
// the model's context.
//
// It fails closed. When no permission service is wired, or the call carries no
// session to prompt in, an outside read is denied rather than quietly allowed.
func RequireReadAccess(ctx context.Context, permissions permission.Service, toolName, path string) error {
	root := ResolveWorkingDir(ctx)
	if withinDir(root, path) {
		return nil
	}

	resolved := canonicalPath(path)
	sessionID, _ := GetContextValues(ctx)
	if permissions == nil || sessionID == "" {
		return fmt.Errorf("%w: reading %s outside the working directory requires approval",
			permission.ErrorPermissionDenied, resolved)
	}

	granted := permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolName:    toolName,
		Action:      "read",
		Path:        path,
		Description: fmt.Sprintf("Read %s (outside the working directory)", resolved),
		// Each distinct outside path is approved on its own; approving a read
		// of one file must not authorize reading its neighbours.
		Fingerprint: resolved,
	})
	if !granted {
		return permission.ErrorPermissionDenied
	}
	return nil
}

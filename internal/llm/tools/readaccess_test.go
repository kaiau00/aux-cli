package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kaiau00/aux-cli/internal/permission"
)

// recordingPermissions answers every request with a fixed verdict and records
// what it was asked, so a test can assert whether a prompt happened at all.
type recordingPermissions struct {
	permission.Service
	grant    bool
	requests []permission.CreatePermissionRequest
}

func (r *recordingPermissions) Request(opts permission.CreatePermissionRequest) bool {
	r.requests = append(r.requests, opts)
	return r.grant
}

func ctxWithSession(t *testing.T) context.Context {
	t.Helper()
	ctx := context.WithValue(context.Background(), SessionIDContextKey, "s1")
	return context.WithValue(ctx, MessageIDContextKey, "m1")
}

func TestWithinDirAcceptsInsideAndRejectsOutside(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{"root itself", root, true},
		{"nested dir", filepath.Join(root, "pkg", "sub"), true},
		{"file that does not exist yet", filepath.Join(root, "pkg", "new.go"), true},
		{"parent traversal", filepath.Join(root, "..", "elsewhere"), false},
		{"absolute outside", "/etc/passwd", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := withinDir(root, c.target); got != c.want {
				t.Fatalf("withinDir(%q, %q) = %v, want %v", root, c.target, got, c.want)
			}
		})
	}
}

func TestWithinDirRejectsSymlinkEscape(t *testing.T) {
	// The important case: a symlink that lives inside the working directory but
	// points outside it must be judged by its target, not its location.
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("token"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	link := filepath.Join(root, "innocent.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if withinDir(root, link) {
		t.Fatal("a symlink pointing outside the working directory must not count as inside")
	}
}

func TestRequireReadAccessAllowsInsideWithoutPrompting(t *testing.T) {
	root := t.TempDir()
	ctx := context.WithValue(ctxWithSession(t), WorkingDirContextKey, root)
	perms := &recordingPermissions{grant: false} // would deny if consulted

	if err := RequireReadAccess(ctx, perms, "view", filepath.Join(root, "a.go")); err != nil {
		t.Fatalf("in-workdir read should be allowed without a prompt: %v", err)
	}
	if len(perms.requests) != 0 {
		t.Fatalf("in-workdir read must not prompt, got %d requests", len(perms.requests))
	}
}

func TestRequireReadAccessPromptsOutsideAndHonorsGrant(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	ctx := context.WithValue(ctxWithSession(t), WorkingDirContextKey, root)

	perms := &recordingPermissions{grant: true}
	if err := RequireReadAccess(ctx, perms, "view", outside); err != nil {
		t.Fatalf("granted outside read should succeed: %v", err)
	}
	if len(perms.requests) != 1 {
		t.Fatalf("expected exactly one prompt, got %d", len(perms.requests))
	}
	if perms.requests[0].Action != "read" || perms.requests[0].Fingerprint == "" {
		t.Fatalf("outside read must prompt as a fingerprinted read action: %+v", perms.requests[0])
	}
}

func TestRequireReadAccessDeniedOutsideReturnsPermissionError(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	ctx := context.WithValue(ctxWithSession(t), WorkingDirContextKey, root)

	perms := &recordingPermissions{grant: false}
	err := RequireReadAccess(ctx, perms, "view", outside)
	if err == nil {
		t.Fatal("denied outside read must return an error")
	}
	if !isPermissionDenied(err) {
		t.Fatalf("expected a permission-denied error, got %v", err)
	}
}

func TestRequireReadAccessFailsClosedWithoutPermissionService(t *testing.T) {
	// No permission service wired means there is no way to ask the user, so an
	// outside read must be refused rather than silently allowed.
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	ctx := context.WithValue(ctxWithSession(t), WorkingDirContextKey, root)

	if err := RequireReadAccess(ctx, nil, "view", outside); !isPermissionDenied(err) {
		t.Fatalf("expected fail-closed denial, got %v", err)
	}
	// ...but an inside read still works with no service at all.
	if err := RequireReadAccess(ctx, nil, "view", filepath.Join(root, "a.go")); err != nil {
		t.Fatalf("inside read should not need a permission service: %v", err)
	}
}

func isPermissionDenied(err error) bool {
	return errors.Is(err, permission.ErrorPermissionDenied)
}

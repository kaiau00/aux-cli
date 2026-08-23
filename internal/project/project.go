// Package project resolves a working directory to a stable project identity,
// its roots/worktrees, and its current revision. Identity is keyed by the
// normalized VCS remote where one exists, else by canonical root path, so
// reopening the same repository reuses the same project_id.
package project

// Project is a stable, repository-level identity.
type Project struct {
	ID                  string
	CanonicalName       string
	VCSType             string // "git" | "none"
	CanonicalRemoteHash string // sha256 of normalized remote, empty for local-only
	CreatedAt           int64
	LastOpenedAt        int64
}

// Root is a directory (worktree or package root) belonging to a project.
type Root struct {
	PathHash      string
	ProjectID     string
	CanonicalPath string
	WorkspaceKind string
	CreatedAt     int64
	LastSeenAt    int64
}

// Revision captures the VCS state at a point in time.
type Revision struct {
	ID               string
	ProjectID        string
	VCSRevision      string
	BranchName       string
	DirtyTreeHash    string
	ProfileInputHash string
	CreatedAt        int64
}

// Resolution is the full result of resolving a working directory.
type Resolution struct {
	Project  Project
	Root     Root
	Revision Revision
}

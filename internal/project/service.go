package project

import (
	"context"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/ids"
)

// Service resolves working directories to stable project identities and exposes
// read access to projects, roots, and revisions.
type Service struct {
	store *Store
	vcs   VCS
}

// NewService returns a project service. Pass GitVCS{} for production.
func NewService(store *Store, vcs VCS) *Service {
	return &Service{store: store, vcs: vcs}
}

// Resolve maps a working directory to its project, root, and current revision,
// creating identity records as needed. Reopening the same repository (matched by
// normalized remote, else by root path) reuses the same project.
func (s *Service) Resolve(ctx context.Context, dir string) (Resolution, error) {
	now := time.Now().UnixMilli()
	canonicalDir := CanonicalizePath(dir)

	info, err := s.vcs.Inspect(ctx, canonicalDir)
	if err != nil {
		return Resolution{}, fmt.Errorf("failed to inspect vcs: %w", err)
	}
	root := CanonicalizePath(info.Root)
	if root == "" {
		root = canonicalDir
	}
	rootHash := PathHash(root)
	remoteHash := RemoteHash(info.Remote)

	proj, found, err := s.matchProject(ctx, remoteHash, rootHash)
	if err != nil {
		return Resolution{}, err
	}
	if !found {
		proj = Project{
			ID:                  ids.New(),
			CanonicalName:       nameFromRemoteOrPath(NormalizeRemote(info.Remote), root),
			VCSType:             info.Type,
			CanonicalRemoteHash: remoteHash,
			CreatedAt:           now,
			LastOpenedAt:        now,
		}
		if err := s.store.CreateProject(ctx, proj); err != nil {
			return Resolution{}, err
		}
	} else {
		if err := s.store.TouchProject(ctx, proj.ID, now); err != nil {
			return Resolution{}, err
		}
		proj.LastOpenedAt = now
	}

	rootRow := Root{
		PathHash:      rootHash,
		ProjectID:     proj.ID,
		CanonicalPath: root,
		WorkspaceKind: "primary",
		CreatedAt:     now,
		LastSeenAt:    now,
	}
	if err := s.store.UpsertRoot(ctx, rootRow); err != nil {
		return Resolution{}, err
	}

	rev, err := s.store.GetOrCreateRevision(ctx, Revision{
		ID:            ids.New(),
		ProjectID:     proj.ID,
		VCSRevision:   info.Revision,
		BranchName:    info.Branch,
		DirtyTreeHash: info.DirtyHash,
		CreatedAt:     now,
	})
	if err != nil {
		return Resolution{}, err
	}

	return Resolution{Project: proj, Root: rootRow, Revision: rev}, nil
}

func (s *Service) matchProject(ctx context.Context, remoteHash, rootHash string) (Project, bool, error) {
	// A remote is authoritative identity: match strictly by remote when present.
	// Falling back to path here would wrongly merge two repositories that happen
	// to be checked out at the same path (roadmapplan.md §6.1).
	if remoteHash != "" {
		return s.store.GetProjectByRemoteHash(ctx, remoteHash)
	}
	return s.store.GetProjectByRootPathHash(ctx, rootHash)
}

// Store exposes the underlying store for read-only callers (CLI/dashboard).
func (s *Service) Store() *Store { return s.store }

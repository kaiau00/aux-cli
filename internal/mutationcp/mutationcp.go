// Package mutationcp creates an automatic baseline checkpoint the first time a
// task mutates the working tree, so mid-run state is
// rollback-able rather than only captured at task completion. It reads the
// before/after content the edit/write tools already recorded in history, so the
// snapshot is truthful, and it is idempotent — at most one first-mutation
// checkpoint per task.
package mutationcp

import (
	"context"
	"strings"
	"sync"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/history"
)

// FileLister reads recorded file versions for a session (the history service).
type FileLister interface {
	ListBySession(ctx context.Context, sessionID string) ([]history.File, error)
	ListLatestSessionFiles(ctx context.Context, sessionID string) ([]history.File, error)
}

// CheckpointStore lists existing checkpoints and creates new ones.
type CheckpointStore interface {
	ListByTask(ctx context.Context, taskID string) ([]checkpoint.Checkpoint, error)
	Create(ctx context.Context, taskID, parentID, label, revision string, changes []checkpoint.FileChange) (checkpoint.Checkpoint, error)
}

// StoreAdapter combines a checkpoint store (for lookups) with the service (for
// content-addressed creation) into the CheckpointStore this package needs.
type StoreAdapter struct {
	Store   *checkpoint.Store
	Service *checkpoint.Service
}

// ListByTask lists a task's checkpoints.
func (a StoreAdapter) ListByTask(ctx context.Context, taskID string) ([]checkpoint.Checkpoint, error) {
	return a.Store.ListByTask(ctx, taskID)
}

// Create captures a checkpoint via the service (deduped artifacts).
func (a StoreAdapter) Create(ctx context.Context, taskID, parentID, label, revision string, changes []checkpoint.FileChange) (checkpoint.Checkpoint, error) {
	return a.Service.Create(ctx, taskID, parentID, label, revision, changes)
}

// Checkpointer captures the first-mutation baseline for tasks.
type Checkpointer struct {
	history     FileLister
	checkpoints CheckpointStore

	mu   sync.Mutex
	seen map[string]bool // taskIDs already checkpointed (fast path)
}

// New returns a first-mutation checkpointer. A nil history or checkpoint store
// disables it.
func New(files FileLister, checkpoints CheckpointStore) *Checkpointer {
	return &Checkpointer{history: files, checkpoints: checkpoints, seen: map[string]bool{}}
}

// IsMutatingTool reports whether a tool name mutates the working tree.
func IsMutatingTool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "edit", "write", "patch":
		return true
	default:
		return false
	}
}

// OnToolCompleted creates a first-mutation checkpoint when a mutating tool
// completes for a task that has none yet. It is a no-op for non-mutating tools,
// missing ids, tasks already checkpointed, or when nothing has actually changed.
// Best-effort: errors are returned but never required to be fatal to the caller.
func (c *Checkpointer) OnToolCompleted(ctx context.Context, taskID, sessionID, toolName string) error {
	if c == nil || c.history == nil || c.checkpoints == nil {
		return nil
	}
	if taskID == "" || sessionID == "" || !IsMutatingTool(toolName) {
		return nil
	}

	c.mu.Lock()
	if c.seen[taskID] {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	// Durable double-check: never create a second first-mutation checkpoint.
	if existing, err := c.checkpoints.ListByTask(ctx, taskID); err == nil && len(existing) > 0 {
		c.markSeen(taskID)
		return nil
	}

	all, err := c.history.ListBySession(ctx, sessionID)
	if err != nil || len(all) == 0 {
		return err
	}
	latest, err := c.history.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		return err
	}

	before := make(map[string]string, len(all))
	for _, f := range all {
		if f.Version == history.InitialVersion {
			before[f.Path] = f.Content
		}
	}
	versions := make([]checkpoint.FileVersion, 0, len(latest))
	for _, f := range latest {
		versions = append(versions, checkpoint.FileVersion{Path: f.Path, Content: f.Content})
	}

	changes := checkpoint.ChangesFrom(before, versions)
	if len(changes) == 0 {
		return nil
	}
	if _, err := c.checkpoints.Create(ctx, taskID, "", "first-mutation", "", changes); err != nil {
		return err
	}
	c.markSeen(taskID)
	return nil
}

func (c *Checkpointer) markSeen(taskID string) {
	c.mu.Lock()
	c.seen[taskID] = true
	c.mu.Unlock()
}

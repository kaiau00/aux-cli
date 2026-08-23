package checkpoint

import (
	"context"
	"sort"
	"time"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Service creates and compares checkpoints, storing content via the artifact
// store so identical file content across branches is deduplicated (branches
// share immutable content and store only deltas).
type Service struct {
	store     *Store
	artifacts *artifact.Service
	events    EventSink
}

// NewService returns a checkpoint service.
func NewService(store *Store, artifacts *artifact.Service, events EventSink) *Service {
	return &Service{store: store, artifacts: artifacts, events: events}
}

// Create captures a checkpoint from a set of file changes. Before/after content
// is stored as content-addressed artifacts (deduplicated), so a branch only adds
// artifacts for genuinely new content.
func (s *Service) Create(ctx context.Context, taskID, parentID, label, revision string, changes []FileChange) (Checkpoint, error) {
	c := Checkpoint{ID: ids.New(), TaskID: taskID, ParentID: parentID, Label: label, Revision: revision, CreatedAt: time.Now().UnixMilli()}
	entries := make([]Entry, 0, len(changes))
	for _, ch := range changes {
		e := Entry{ID: ids.New(), CheckpointID: c.ID, Path: ch.Path, Operation: ch.Operation, Mode: ch.Mode}
		if len(ch.Before) > 0 {
			id, err := s.putBlob(ctx, c.ID, ch.Before)
			if err != nil {
				return Checkpoint{}, err
			}
			e.BeforeArtifactID = id
		}
		if len(ch.After) > 0 {
			id, err := s.putBlob(ctx, c.ID, ch.After)
			if err != nil {
				return Checkpoint{}, err
			}
			e.AfterArtifactID = id
		}
		entries = append(entries, e)
	}
	c.TreeHash = treeHash(entries)
	if err := s.store.Insert(ctx, c, entries); err != nil {
		return Checkpoint{}, err
	}
	if s.events != nil {
		_, _ = s.events.Append(ctx, eventstore.Append{
			Type:   eventstore.CheckpointCreated,
			TaskID: taskID,
			Payload: eventstore.CheckpointPayload{
				CheckpointID: c.ID, ParentID: parentID, Label: label, Entries: len(entries),
			},
		})
	}
	return c, nil
}

func (s *Service) putBlob(ctx context.Context, checkpointID string, data []byte) (string, error) {
	if s.artifacts == nil {
		// Without an artifact store, fall back to a content hash as the ref so
		// dedup semantics still hold in tests.
		return artifact.HashBytes(data), nil
	}
	a, _, err := s.artifacts.Put(ctx, data, "text/plain", artifact.OwnerRef{Type: "checkpoint", ID: checkpointID, Relation: "snapshot"})
	if err != nil {
		return "", err
	}
	return a.ID, nil
}

// ChangedPaths returns the paths changed in a checkpoint.
func (s *Service) ChangedPaths(ctx context.Context, checkpointID string) ([]string, error) {
	entries, err := s.store.Entries(ctx, checkpointID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.Path)
	}
	sort.Strings(paths)
	return paths, nil
}

// Compare returns the paths that differ between two checkpoints (by after-blob
// content id), i.e. what changed from a to b.
func (s *Service) Compare(ctx context.Context, a, b string) ([]string, error) {
	ea, err := s.entryMap(ctx, a)
	if err != nil {
		return nil, err
	}
	eb, err := s.entryMap(ctx, b)
	if err != nil {
		return nil, err
	}
	changed := map[string]struct{}{}
	for path, after := range eb {
		if ea[path] != after {
			changed[path] = struct{}{}
		}
	}
	for path := range ea {
		if _, ok := eb[path]; !ok {
			changed[path] = struct{}{}
		}
	}
	out := make([]string, 0, len(changed))
	for p := range changed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Service) entryMap(ctx context.Context, checkpointID string) (map[string]string, error) {
	entries, err := s.store.Entries(ctx, checkpointID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		m[e.Path] = e.AfterArtifactID
	}
	return m, nil
}

// DetectWriteConflicts returns the paths written by both change sets — the
// overlap that must be resolved before automatic integration (roadmapplan.md
// §11.3: detect overlapping write sets before merge).
func DetectWriteConflicts(a, b []string) []string {
	set := make(map[string]struct{}, len(a))
	for _, p := range a {
		set[p] = struct{}{}
	}
	var conflicts []string
	for _, p := range b {
		if _, ok := set[p]; ok {
			conflicts = append(conflicts, p)
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func treeHash(entries []Entry) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, e.Path+":"+e.AfterArtifactID)
	}
	sort.Strings(parts)
	joined := ""
	for _, p := range parts {
		joined += p + "\n"
	}
	return artifact.HashBytes([]byte(joined))
}

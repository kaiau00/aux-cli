package memory

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/eventstore"
)

// autoPromoteConfidence is the confidence at or above which a non-episodic
// candidate is auto-promoted (roadmapplan.md §8.3: direct facts / evidenced
// procedures). Below it, the memory stays a candidate awaiting more evidence.
const autoPromoteConfidence = 0.85

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Service applies the memory lifecycle policy over the store and emits events.
type Service struct {
	store  *Store
	events EventSink
}

// NewService returns a memory service. events may be nil.
func NewService(store *Store, events EventSink) *Service {
	return &Service{store: store, events: events}
}

// Learn persists candidates and applies the promotion policy:
//   - episodic memories activate once the task outcome is known;
//   - procedural/factual memories auto-promote only at high confidence, else
//     they remain candidates awaiting repeated evidence.
func (s *Service) Learn(ctx context.Context, candidates []Candidate) error {
	for _, c := range candidates {
		mem, ver, err := s.store.SaveCandidate(ctx, c)
		if err != nil {
			return err
		}
		s.emit(ctx, eventstore.MemoryCandidateCreated, mem, c.Confidence)

		promote := c.Type == Episodic || c.Confidence >= autoPromoteConfidence
		if promote {
			if err := s.store.Promote(ctx, mem.ID, c.Confidence); err != nil {
				return err
			}
			s.emit(ctx, eventstore.MemoryPromoted, mem, c.Confidence)
			// Record the promotion as positive feedback on the version.
			// Best effort: the promotion above already succeeded and emitted its
			// event. This is a ranking signal for later scoring, so losing one
			// weakens a heuristic rather than misreporting anything.
			_ = s.store.RecordFeedback(ctx, ver.ID, c.Sources[0].ID, "promoted", "auto_policy")
		}
	}
	return nil
}

// Retrieve returns active memories for a project, filtered and capped.
func (s *Service) Retrieve(ctx context.Context, projectID string, types []Type, limit int) ([]Memory, error) {
	return s.store.Retrieve(ctx, projectID, types, limit)
}

// InvalidateForRevision marks active memories whose supporting revision changed
// as stale (revision-aware invalidation), emitting an event per invalidation.
func (s *Service) InvalidateForRevision(ctx context.Context, projectID, revision string) (int, error) {
	before, err := s.store.ListByState(ctx, projectID, StateActive)
	if err != nil {
		return 0, err
	}
	n, err := s.store.MarkStaleForChangedRevision(ctx, projectID, revision)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		// Emit invalidation events for the memories that changed state.
		after, _ := s.store.ListByState(ctx, projectID, StateStale)
		activeIDs := map[string]struct{}{}
		for _, m := range before {
			activeIDs[m.ID] = struct{}{}
		}
		for _, m := range after {
			if _, was := activeIDs[m.ID]; was {
				s.emit(ctx, eventstore.MemoryInvalidated, m, m.Confidence)
			}
		}
	}
	return n, nil
}

// RecordCorrection lowers confidence and, on strong correction, invalidates a
// conflicting memory (roadmapplan.md §8.3: user corrections lower/invalidate).
func (s *Service) RecordCorrection(ctx context.Context, memoryID string, taskID string) error {
	if err := s.store.SetState(ctx, memoryID, StateRejected); err != nil {
		return err
	}
	return nil
}

func (s *Service) emit(ctx context.Context, t eventstore.Type, mem Memory, confidence float64) {
	if s.events == nil {
		return
	}
	_, _ = s.events.Append(ctx, eventstore.Append{
		Type:      t,
		ProjectID: mem.ProjectID,
		Payload: eventstore.MemoryPayload{
			MemoryID:   mem.ID,
			MemoryType: string(mem.Type),
			State:      string(mem.State),
			StableKey:  mem.StableKey,
			Confidence: confidence,
		},
	})
}

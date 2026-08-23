package govpolicy

import (
	"context"
	"errors"

	"github.com/kaiau00/aux-cli/internal/eventstore"
)

// ErrNoEvaluationEvidence is returned when promotion is attempted without a
// passing evaluation — the core safety gate.
var ErrNoEvaluationEvidence = errors.New("policy promotion requires a passing evaluation against a baseline")

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Service manages the governor-policy lifecycle with evaluation-gated promotion.
type Service struct {
	store  *Store
	events EventSink
}

// NewService returns a policy service. events may be nil.
func NewService(store *Store, events EventSink) *Service {
	return &Service{store: store, events: events}
}

// Candidate creates a candidate policy. It never activates a policy.
func (s *Service) Candidate(ctx context.Context, ownerType, ownerID, taskClass, policyJSON string) (Policy, error) {
	return s.store.Create(ctx, Policy{
		OwnerType: ownerType, OwnerID: ownerID, TaskClass: taskClass,
		State: StateCandidate, PolicyJSON: policyJSON,
	})
}

// Evaluate records an evaluation result (candidate vs baseline). Only a passing
// evaluation unlocks promotion.
func (s *Service) Evaluate(ctx context.Context, policyID, baselinePolicyID, evalRunID string, result EvalResult, metricsJSON string) error {
	return s.store.RecordEvaluation(ctx, Evaluation{
		PolicyID: policyID, BaselinePolicyID: baselinePolicyID, EvalRunID: evalRunID,
		Result: result, MetricsJSON: metricsJSON,
	})
}

// Promote activates a policy only if it has a passing evaluation. Returns
// ErrNoEvaluationEvidence otherwise — no autonomous promotion without evidence.
func (s *Service) Promote(ctx context.Context, policyID string) error {
	ok, err := s.store.HasPassingEvaluation(ctx, policyID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoEvaluationEvidence
	}
	if err := s.store.SetState(ctx, policyID, StateActive); err != nil {
		return err
	}
	s.emit(ctx, policyID, StateActive)
	return nil
}

// Rollback demotes an active policy (regression response). History is preserved.
func (s *Service) Rollback(ctx context.Context, policyID string) error {
	if err := s.store.SetState(ctx, policyID, StateRolledBack); err != nil {
		return err
	}
	s.emit(ctx, policyID, StateRolledBack)
	return nil
}

// Active returns the active policies (those the governor may apply).
func (s *Service) Active(ctx context.Context) ([]Policy, error) {
	return s.store.ListByState(ctx, StateActive)
}

// Candidates returns policies awaiting evaluation/promotion.
func (s *Service) Candidates(ctx context.Context) ([]Policy, error) {
	return s.store.ListByState(ctx, StateCandidate)
}

func (s *Service) emit(ctx context.Context, policyID string, state State) {
	if s.events == nil {
		return
	}
	_, _ = s.events.Append(ctx, eventstore.Append{
		Type: eventstore.PolicyPromoted,
		Payload: eventstore.PolicyPayload{
			PolicyID: policyID, State: string(state),
		},
	})
}

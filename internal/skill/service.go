package skill

import (
	"context"
	"errors"

	"github.com/kaiau00/aux-cli/internal/eventstore"
)

// ErrNoEvaluationEvidence is returned when promotion is attempted without a
// passing evaluation — the core safety gate.
var ErrNoEvaluationEvidence = errors.New("skill promotion requires a passing evaluation on the candidate version")

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Service manages the skill lifecycle with evaluation-gated promotion.
type Service struct {
	store  *Store
	events EventSink
}

// NewService returns a skill service. events may be nil.
func NewService(store *Store, events EventSink) *Service {
	return &Service{store: store, events: events}
}

// Candidate creates (or reuses) a skill in the candidate state with a new
// content version. It never activates a skill (that requires evaluation).
func (s *Service) Candidate(ctx context.Context, ownerType, ownerID string, content Content, sourceType string, sourceIDs []string) (Skill, Version, error) {
	sk, err := s.store.CreateSkill(ctx, Skill{OwnerType: ownerType, OwnerID: ownerID, Name: content.Name, Scope: content.Scope, State: StateCandidate})
	if err != nil {
		return Skill{}, Version{}, err
	}
	ver, err := s.store.AddVersion(ctx, Version{SkillID: sk.ID, Content: content, SourceType: sourceType, SourceIDs: sourceIDs})
	if err != nil {
		return Skill{}, Version{}, err
	}
	s.emit(ctx, eventstore.SkillCandidateCreated, sk, ver.ID)
	return sk, ver, nil
}

// Evaluate records an evaluation result for a skill version (baseline vs
// candidate). Only a passing evaluation unlocks promotion.
func (s *Service) Evaluate(ctx context.Context, versionID, baselineVersionID, evalRunID string, result EvalResult, metricsJSON string) error {
	return s.store.RecordEvaluation(ctx, Evaluation{
		SkillVersionID: versionID, BaselineVersion: baselineVersionID, EvalRunID: evalRunID,
		Result: result, MetricsJSON: metricsJSON,
	})
}

// Promote activates a skill only if its version has a passing evaluation. The
// prior version remains as a rollback target. Returns ErrNoEvaluationEvidence
// otherwise — there is no autonomous promotion without evidence.
func (s *Service) Promote(ctx context.Context, skillID, versionID string) error {
	ok, err := s.store.HasPassingEvaluation(ctx, versionID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoEvaluationEvidence
	}
	if err := s.store.SetState(ctx, skillID, StateActive); err != nil {
		return err
	}
	sk, _ := s.store.GetSkill(ctx, skillID)
	s.emit(ctx, eventstore.SkillPromoted, sk, versionID)
	return nil
}

// Rollback demotes an active skill (regression response). The version history is
// preserved so a prior version can be re-promoted with fresh evidence.
func (s *Service) Rollback(ctx context.Context, skillID string) error {
	if err := s.store.SetState(ctx, skillID, StateRolledBack); err != nil {
		return err
	}
	sk, _ := s.store.GetSkill(ctx, skillID)
	s.emit(ctx, eventstore.SkillRolledBack, sk, "")
	return nil
}

// Active returns the active skills (available for retrieval).
func (s *Service) Active(ctx context.Context) ([]Skill, error) {
	return s.store.ListByState(ctx, StateActive)
}

// Candidates returns skills awaiting evaluation/promotion.
func (s *Service) Candidates(ctx context.Context) ([]Skill, error) {
	return s.store.ListByState(ctx, StateCandidate)
}

func (s *Service) emit(ctx context.Context, t eventstore.Type, sk Skill, versionID string) {
	if s.events == nil {
		return
	}
	_, _ = s.events.Append(ctx, eventstore.Append{
		Type: t,
		Payload: eventstore.SkillPayload{
			SkillID: sk.ID, Name: sk.Name, State: string(sk.State), VersionID: versionID,
		},
	})
}

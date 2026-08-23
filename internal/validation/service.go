package validation

import (
	"context"
	"fmt"
	"time"

	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/ids"
)

// CommandResult is the outcome of executing a validation command. Execution
// happens through the caller's Runner (which enforces existing permissions).
type CommandResult struct {
	ExitCode         int
	DurationMS       int64
	OutputArtifactID string
}

// Runner executes a validation command under existing permission controls.
// Tests inject a deterministic fake.
type Runner interface {
	Run(ctx context.Context, command string) (CommandResult, error)
}

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Service plans, runs (via a Runner), caches, and records validation, and
// computes proof-of-done state.
type Service struct {
	store  *Store
	events EventSink
}

// NewService returns a validation service. events may be nil.
func NewService(store *Store, events EventSink) *Service {
	return &Service{store: store, events: events}
}

// RunIntent executes one intent's command through the runner, unless a passing
// result for the same command + input fingerprint is already cached. It
// records the run and attaches executable evidence to the intent's criteria.
func (s *Service) RunIntent(ctx context.Context, taskID string, intent Intent, inputFingerprint string, runner Runner) (Result, error) {
	cmdHash := HashCommand(intent.Command)

	if cached, ok, err := s.store.CachedPass(ctx, cmdHash, inputFingerprint); err != nil {
		return Result{}, err
	} else if ok {
		if eerr := s.attachEvidence(ctx, taskID, intent, cached.ID, "cached pass: "+intent.Command); eerr != nil {
			return Result{}, eerr
		}
		return Result{Run: cached, Cached: true}, nil
	}

	now := time.Now().UnixMilli()
	run := Run{
		ID: ids.New(), TaskID: taskID, IntentID: intent.ID, ValidatorType: intent.ValidatorType,
		Command: intent.Command, CommandHash: cmdHash, InputFingerprint: inputFingerprint,
		Status: StatusRunning, StartedAt: now, CreatedAt: now,
	}
	s.emit(ctx, taskID, eventstore.ValidationStarted, run)

	cr, err := runner.Run(ctx, intent.Command)
	run.FinishedAt = time.Now().UnixMilli()
	run.DurationMS = cr.DurationMS
	run.ExitCode = cr.ExitCode
	run.OutputArtifactID = cr.OutputArtifactID
	switch {
	case err != nil:
		run.Status = StatusFailed
	case cr.ExitCode == 0:
		run.Status = StatusPassed
	default:
		run.Status = StatusFailed
	}
	if serr := s.store.InsertRun(ctx, run); serr != nil {
		return Result{}, serr
	}
	s.emit(ctx, taskID, eventstore.ValidationCompleted, run)

	// Both outcomes are evidence: a pass validates the criterion, a fail blocks
	// it. The agent cannot silently mark a criterion validated without this.
	if eerr := s.attachEvidence(ctx, taskID, intent, run.ID, string(run.Status)+": "+intent.Command); eerr != nil {
		return Result{}, eerr
	}
	return Result{Run: run}, err
}

// attachEvidence records executable evidence for each of the intent's criteria.
//
// The error is returned rather than discarded, and callers must fail on it.
// deriveState resolves failing evidence ahead of passing evidence, so a failing
// run's evidence is the thing that blocks a criterion. If that write is dropped
// while an earlier run passed, the criterion has passing evidence and no
// failing evidence, and proof-of-done reports it Validated — a failed
// validation presented as a successful one, which is the single invariant this
// package exists to hold.
//
// Failing closed is therefore correct: if the evidence cannot be recorded, the
// validation cannot be claimed to have happened.
func (s *Service) attachEvidence(ctx context.Context, taskID string, intent Intent, runID, summary string) error {
	for _, cid := range intent.CriterionIDs {
		if err := s.store.InsertEvidence(ctx, Evidence{
			TaskID: taskID, CriterionID: cid, ValidationRunID: runID,
			EvidenceType: EvidenceExecutable, Summary: summary, CreatedAt: time.Now().UnixMilli(),
		}); err != nil {
			return fmt.Errorf("failed to record validation evidence for criterion %s: %w", cid, err)
		}
	}
	return nil
}

// WaiveCriterion records a user waiver for a criterion (only the user can waive).
func (s *Service) WaiveCriterion(ctx context.Context, taskID, criterionID, reason string) error {
	return s.store.InsertEvidence(ctx, Evidence{
		TaskID: taskID, CriterionID: criterionID, EvidenceType: EvidenceWaiver,
		Summary: reason, CreatedAt: time.Now().UnixMilli(),
	})
}

// ProofOfDone computes each criterion's state from recorded evidence. The agent
// cannot silently mark a criterion validated: only passing executable evidence
// (or a user waiver) advances it.
func (s *Service) ProofOfDone(ctx context.Context, taskID string, criterionIDs []string) (map[string]CriterionState, error) {
	evidence, err := s.store.EvidenceForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	runs, err := s.store.RunsForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	runStatus := map[string]Status{}
	for _, r := range runs {
		runStatus[r.ID] = r.Status
	}

	// Bucket evidence by criterion.
	byCriterion := map[string][]Evidence{}
	for _, e := range evidence {
		byCriterion[e.CriterionID] = append(byCriterion[e.CriterionID], e)
	}

	out := map[string]CriterionState{}
	for _, cid := range criterionIDs {
		out[cid] = deriveState(byCriterion[cid], runStatus)
	}
	return out, nil
}

func deriveState(evidence []Evidence, runStatus map[string]Status) CriterionState {
	if len(evidence) == 0 {
		return Uncovered
	}
	var hasPassing, hasFailing, hasWaiver, hasOther bool
	for _, e := range evidence {
		switch e.EvidenceType {
		case EvidenceWaiver:
			hasWaiver = true
		case EvidenceExecutable:
			switch runStatus[e.ValidationRunID] {
			case StatusPassed:
				hasPassing = true
			case StatusFailed, StatusBlocked:
				hasFailing = true
			default:
				hasOther = true
			}
		default:
			hasOther = true
		}
	}
	switch {
	case hasWaiver:
		return WaivedByUser
	case hasFailing:
		return Blocked
	case hasPassing:
		return Validated
	case hasOther:
		return PartiallyEvidenced
	default:
		return Claimed
	}
}

// SuccessfulCommands returns the distinct commands whose latest run passed for a
// task. This feeds procedural-memory extraction.
func (s *Service) SuccessfulCommands(ctx context.Context, taskID string) ([]string, error) {
	runs, err := s.store.RunsForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	// RunsForTask is newest-first; take the first (latest) status per command.
	latest := map[string]Status{}
	var order []string
	for _, r := range runs {
		if _, seen := latest[r.Command]; !seen {
			latest[r.Command] = r.Status
			order = append(order, r.Command)
		}
	}
	var out []string
	for _, cmd := range order {
		if latest[cmd] == StatusPassed {
			out = append(out, cmd)
		}
	}
	return out, nil
}

func (s *Service) emit(ctx context.Context, taskID string, t eventstore.Type, run Run) {
	if s.events == nil {
		return
	}
	_, _ = s.events.Append(ctx, eventstore.Append{
		Type:   t,
		TaskID: taskID,
		Payload: eventstore.ValidationPayload{
			ValidationRunID: run.ID,
			Command:         run.Command,
			Status:          string(run.Status),
			ExitCode:        run.ExitCode,
			DurationMS:      run.DurationMS,
		},
	})
}

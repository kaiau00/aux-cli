package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// TaskValidator is the part of a task coordinator that validates a finished
// task. The agent checks for it on its TaskCoordinator rather than requiring
// it, so coordinators without validation keep working.
type TaskValidator interface {
	PlanValidation(ctx context.Context, taskID, sessionID string) (validation.TaskPlan, string, error)
	RunValidation(ctx context.Context, taskID string, intent validation.Intent, fingerprint string, runner validation.Runner) (validation.Result, error)
	ProofOfDone(ctx context.Context, taskID string, criterionIDs []string) (map[string]validation.CriterionState, error)
}

// Skip reasons decided here rather than by the coordinator.
const (
	skipDisabled      = "disabled by validation.auto"
	skipNoPermissions = "no permission service to ask"
)

// validateTaskIfNeeded runs the project's validation commands at the end of a
// task that changed files, each behind the normal permission prompt, so the
// deferred Finish can learn procedural memory and skills from what passed. It
// returns a short report for the transcript, or "" when nothing ran.
//
// Best effort throughout: a failing command is information for the user, and
// nothing here can fail the turn.
func (a *agent) validateTaskIfNeeded(ctx context.Context, sessionID, taskID string) string {
	validator, ok := a.coordinator.(TaskValidator)
	if !ok || taskID == "" {
		return ""
	}
	// A subagent's task belongs to its parent, which validates once at its end.
	if parent, _ := ctx.Value(tools.ParentTaskIDContextKey).(string); parent != "" {
		return ""
	}
	if cfg := config.Get(); cfg != nil && !cfg.Validation.Auto {
		a.emitValidationSkipped(ctx, taskID, skipDisabled)
		return ""
	}
	plan, reason, err := validator.PlanValidation(ctx, taskID, sessionID)
	if err != nil {
		logging.Warn("failed to plan task validation", "task", taskID, "error", err)
		return ""
	}
	if reason != "" {
		a.emitValidationSkipped(ctx, taskID, reason)
		return ""
	}
	if a.permissions == nil {
		a.emitValidationSkipped(ctx, taskID, skipNoPermissions)
		return ""
	}

	runner := a.validationRunner(plan.WorkDir, sessionID)
	var b strings.Builder
	b.WriteString("\n\n---\nValidation at task end:\n")
	for _, intent := range plan.Intents {
		res, err := validator.RunValidation(ctx, taskID, intent, plan.Fingerprint, runner)
		var outcome string
		switch {
		case errors.Is(err, permission.ErrorPermissionDenied):
			outcome = "denied"
		case err != nil && res.Run.Status == validation.StatusSkipped:
			outcome = "not run (cancelled)"
		case err != nil:
			outcome = fmt.Sprintf("error (%v)", err)
		case res.Cached:
			outcome = "passed (cached)"
		default:
			outcome = string(res.Run.Status)
		}
		fmt.Fprintf(&b, "- `%s`: %s\n", intent.Command, outcome)
	}
	if states, err := validator.ProofOfDone(ctx, taskID, plan.CriterionIDs); err == nil {
		counts := map[validation.CriterionState]int{}
		for _, s := range states {
			counts[s]++
		}
		fmt.Fprintf(&b, "Criteria: %d validated, %d blocked, %d not covered\n",
			counts[validation.Validated], counts[validation.Blocked], len(states)-counts[validation.Validated]-counts[validation.Blocked])
	}
	return b.String()
}

// validationRunner runs approved commands in a shell; tests replace it.
func (a *agent) validationRunner(workDir, sessionID string) validation.Runner {
	if a.newValidationRunner != nil {
		return a.newValidationRunner(workDir, sessionID, a.permissions)
	}
	return validation.ShellRunner{WorkDir: workDir, SessionID: sessionID, Approver: a.permissions}
}

func (a *agent) emitValidationSkipped(ctx context.Context, taskID, reason string) {
	a.emit(ctx, eventstore.Append{
		Type:    eventstore.ValidationSkipped,
		TaskID:  taskID,
		Payload: eventstore.ValidationPayload{Reason: reason},
	})
}

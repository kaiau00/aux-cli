package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/kaiau00/aux-cli/internal/validation"
)

// Reasons PlanValidation gives for not validating a task.
const (
	SkipNotWired     = "validation is not wired"
	SkipNoCriteria   = "task has no acceptance criteria"
	SkipNoFileChange = "no file changes"
	SkipNoCommands   = "no validation commands in the project profile"
)

// PlanValidation decides whether a finished task should be validated and, if
// so, plans it exactly as `aux validate` does: every profile validation
// command against every acceptance criterion. A non-empty skip reason means
// nothing should run.
//
// Only a task that changed files is validated. Mutations are read from file
// history, which the edit, write, and patch tools record; research, review,
// and explain tasks write nothing and so fall out without a mode check.
func (c *Coordinator) PlanValidation(ctx context.Context, taskID, sessionID string) (validation.TaskPlan, string, error) {
	if c.validations == nil || c.history == nil {
		return validation.TaskPlan{}, SkipNotWired, nil
	}
	spec, ok, err := c.store.LatestSpec(ctx, taskID)
	if err != nil {
		return validation.TaskPlan{}, "", fmt.Errorf("failed to read task spec: %w", err)
	}
	if !ok || len(spec.AcceptanceCriteria) == 0 {
		return validation.TaskPlan{}, SkipNoCriteria, nil
	}
	t, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return validation.TaskPlan{}, "", fmt.Errorf("failed to read task: %w", err)
	}
	changed, err := c.changedDuringTask(ctx, sessionID, t.CreatedAt)
	if err != nil {
		return validation.TaskPlan{}, "", err
	}
	if !changed {
		return validation.TaskPlan{}, SkipNoFileChange, nil
	}

	res, err := c.resolution(ctx)
	if err != nil {
		return validation.TaskPlan{}, "", err
	}
	eff, err := c.profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
	if err != nil {
		return validation.TaskPlan{}, "", fmt.Errorf("failed to compile profile: %w", err)
	}
	var specs []validation.CommandSpec
	for _, cmd := range eff.ValidationCommands() {
		specs = append(specs, validation.CommandSpec{Key: cmd.Key, Command: cmd.Command, ValidatorType: cmd.Type})
	}
	criterionIDs := make([]string, 0, len(spec.AcceptanceCriteria))
	for _, crit := range spec.AcceptanceCriteria {
		criterionIDs = append(criterionIDs, crit.ID)
	}
	intents := validation.PlanIntents(specs, criterionIDs)
	if len(intents) == 0 {
		return validation.TaskPlan{}, SkipNoCommands, nil
	}

	fingerprint, err := c.workingTreeFingerprint(ctx, sessionID, res.Revision.VCSRevision)
	if err != nil {
		return validation.TaskPlan{}, "", err
	}
	return validation.TaskPlan{
		Intents:      intents,
		CriterionIDs: criterionIDs,
		Fingerprint:  fingerprint,
		WorkDir:      res.Root.CanonicalPath,
	}, "", nil
}

// RunValidation runs one planned intent and records its evidence.
func (c *Coordinator) RunValidation(ctx context.Context, taskID string, intent validation.Intent, fingerprint string, runner validation.Runner) (validation.Result, error) {
	return c.validations.RunIntent(ctx, taskID, intent, fingerprint, runner)
}

// ProofOfDone reports each criterion's state from recorded evidence.
func (c *Coordinator) ProofOfDone(ctx context.Context, taskID string, criterionIDs []string) (map[string]validation.CriterionState, error) {
	return c.validations.ProofOfDone(ctx, taskID, criterionIDs)
}

// changedDuringTask reports whether the session recorded a file version since
// the task began. File history is stamped in seconds, tasks in milliseconds.
func (c *Coordinator) changedDuringTask(ctx context.Context, sessionID string, taskCreatedMS int64) (bool, error) {
	files, err := c.history.ListBySession(ctx, sessionID)
	if err != nil {
		return false, fmt.Errorf("failed to read file history: %w", err)
	}
	since := taskCreatedMS / 1000
	for _, f := range files {
		if f.CreatedAt >= since {
			return true, nil
		}
	}
	return false, nil
}

// workingTreeFingerprint is the commit plus the session's latest content for
// every file it touched.
func (c *Coordinator) workingTreeFingerprint(ctx context.Context, sessionID, revision string) (string, error) {
	files, err := c.history.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to read file history: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	h := sha256.New()
	h.Write([]byte(revision))
	for _, f := range files {
		h.Write([]byte{0})
		h.Write([]byte(f.Path))
		h.Write([]byte{0})
		h.Write([]byte(f.Content))
	}
	return revision + ":" + hex.EncodeToString(h.Sum(nil))[:16], nil
}

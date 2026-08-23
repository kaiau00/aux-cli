package memory

// ExtractInput is the deterministic evidence available after a task finalizes.
// Candidate extraction prefers deterministic signals
// before any model-proposed candidates.
type ExtractInput struct {
	ProjectID          string
	TaskID             string
	Objective          string
	Mode               string
	Outcome            string
	SupportingRevision string
	// SuccessfulCommands are validation commands that passed during the task.
	SuccessfulCommands []string
	// ChangedPaths are the files the task changed.
	ChangedPaths []string
}

// Extract produces deterministic memory candidates from a finalized task:
//   - an episodic summary of the task (activates once the outcome is known);
//   - a procedural candidate per validated command that passed.
//
// It never fabricates facts; each candidate carries provenance and confidence.
func Extract(in ExtractInput) []Candidate {
	var candidates []Candidate

	// Episodic: one compact summary per task. Bounded knowledge, not a transcript.
	candidates = append(candidates, Candidate{
		ProjectID: in.ProjectID,
		Type:      Episodic,
		Scope:     "task",
		StableKey: "episode:" + in.TaskID,
		Content: map[string]any{
			"objective":    in.Objective,
			"mode":         in.Mode,
			"outcome":      in.Outcome,
			"changedPaths": in.ChangedPaths,
		},
		Confidence:                 0.9,
		SupportingRevision:         in.SupportingRevision,
		Sources:                    []Source{{Type: "task", ID: in.TaskID, Relation: "summarizes"}},
		InvalidateOnRevisionChange: false, // episodes remain true about the past
	})

	// Procedural: a validated command that passed is a repeatable method.
	for _, cmd := range in.SuccessfulCommands {
		if cmd == "" {
			continue
		}
		candidates = append(candidates, Candidate{
			ProjectID: in.ProjectID,
			Type:      Procedural,
			Scope:     "project",
			StableKey: "validate:" + cmd,
			Content: map[string]any{
				"command": cmd,
				"purpose": "validated successfully during a task",
			},
			// A command that passed validation is one successful validated use,
			// which the promotion policy treats as sufficient for a
			// procedure — so it meets the auto-promote threshold.
			Confidence:                 0.85,
			SupportingRevision:         in.SupportingRevision,
			Sources:                    []Source{{Type: "task", ID: in.TaskID, Relation: "validated"}},
			InvalidateOnRevisionChange: true,
		})
	}
	return candidates
}

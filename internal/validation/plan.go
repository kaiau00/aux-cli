package validation

import "github.com/kaiau00/aux-cli/internal/ids"

// CommandSpec is one runnable validation command discovered for a project,
// usually from its compiled profile (a test or build command).
type CommandSpec struct {
	// Key identifies the command's origin, e.g. "go.test".
	Key string
	// Command is the shell command to execute.
	Command string
	// ValidatorType classifies it, e.g. "test" or "build".
	ValidatorType string
}

// PlanIntents turns the project's known validation commands into intents that
// provide evidence for a task's acceptance criteria (roadmapplan.md §14.1).
//
// Every command is attributed to every criterion on purpose: the project's
// commands are not criterion-specific, so a passing test run is evidence for
// each criterion, and — more importantly — a failing one blocks all of them.
// Attributing a failure too narrowly would let a task look validated while a
// command that covers it is red.
//
// A task with no acceptance criteria yields no intents: there would be nothing
// for the run to be evidence *of*, and recording runs with no criterion would
// produce validation activity that never affects proof-of-done.
func PlanIntents(commands []CommandSpec, criterionIDs []string) []Intent {
	if len(commands) == 0 || len(criterionIDs) == 0 {
		return nil
	}
	intents := make([]Intent, 0, len(commands))
	for _, c := range commands {
		if c.Command == "" {
			continue
		}
		vt := c.ValidatorType
		if vt == "" {
			vt = "command"
		}
		intents = append(intents, Intent{
			ID:            ids.New(),
			ValidatorType: vt,
			Command:       c.Command,
			CriterionIDs:  append([]string(nil), criterionIDs...),
		})
	}
	return intents
}

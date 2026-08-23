package cost

import "github.com/kaiau00/aux-cli/internal/eventstore"

// Trajectory records. A task is compiled into an ordered
// sequence of state/action/outcome steps derived from durable events plus the
// call ledger, so successful and failed paths can be compared by task class.

// StepKind classifies a trajectory step.
type StepKind string

const (
	StepState   StepKind = "state"
	StepAction  StepKind = "action"
	StepOutcome StepKind = "outcome"
)

// TrajectoryStep is one point in a task's trajectory.
type TrajectoryStep struct {
	Sequence int64    `json:"sequence"`
	Kind     StepKind `json:"kind"`
	Type     string   `json:"type"` // the underlying event type
	Detail   string   `json:"detail,omitempty"`
}

// Trajectory is the compiled path of a task.
type Trajectory struct {
	TaskID      string           `json:"taskId"`
	Steps       []TrajectoryStep `json:"steps"`
	ModelCalls  int              `json:"modelCalls"`
	ToolCalls   int              `json:"toolCalls"`
	Failures    int              `json:"failures"`
	Corrections int              `json:"corrections"`
	TotalCost   float64          `json:"totalCost"`
	Totals      Totals           `json:"totals"`
}

// classify maps an event type to a trajectory step kind.
func classify(t eventstore.Type) StepKind {
	switch t {
	case eventstore.TaskCreated, eventstore.TaskCompiled, eventstore.ContextCompiled, eventstore.TurnStarted:
		return StepState
	case eventstore.TaskCompleted, eventstore.TaskFailed, eventstore.TaskCancelled,
		eventstore.ModelCallCompleted, eventstore.ModelCallFailed, eventstore.ToolCompleted,
		eventstore.ToolFailed, eventstore.ValidationCompleted, eventstore.TurnCompleted:
		return StepOutcome
	default:
		return StepAction
	}
}

// CompileTrajectory builds a task's trajectory from its ordered events and the
// aggregated ledger totals.
func CompileTrajectory(taskID string, events []eventstore.Event, totals Totals) Trajectory {
	tr := Trajectory{TaskID: taskID, Totals: totals, TotalCost: totals.Cost}
	for _, e := range events {
		step := TrajectoryStep{Sequence: e.Sequence, Kind: classify(e.Type), Type: string(e.Type)}
		tr.Steps = append(tr.Steps, step)
		switch e.Type {
		case eventstore.ModelCallStarted:
			tr.ModelCalls++
		case eventstore.ToolStarted:
			tr.ToolCalls++
		case eventstore.ModelCallFailed, eventstore.ToolFailed, eventstore.TaskFailed:
			tr.Failures++
		}
	}
	return tr
}

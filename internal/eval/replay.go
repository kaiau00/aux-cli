package eval

import "github.com/kaiau00/aux-cli/internal/eventstore"

// Replay modes (roadmapplan.md §12.2). This file implements deterministic event
// replay: reconstructing a task's runtime state purely from its durable event
// sequence, with no provider calls. It is the basis for testing projections and
// runtime state transitions and for counterfactual analysis.

// ReplayedTask is the state reconstructed from a task's events.
type ReplayedTask struct {
	TaskID     string `json:"taskId"`
	Status     string `json:"status"`
	Turns      int    `json:"turns"`
	ModelCalls int    `json:"modelCalls"`
	ToolCalls  int    `json:"toolCalls"`
	Failures   int    `json:"failures"`
	Validated  bool   `json:"validated"`
	Events     int    `json:"events"`
}

// ReplayTaskState reconstructs a task's final state from an ordered event slice.
// The result is a pure function of the events (deterministic), independent of
// how the events were paged in.
func ReplayTaskState(taskID string, events []eventstore.Event) ReplayedTask {
	rt := ReplayedTask{TaskID: taskID, Status: "unknown", Events: len(events)}
	for _, e := range events {
		switch e.Type {
		case eventstore.TaskCreated:
			rt.Status = "created"
		case eventstore.TaskStarted:
			rt.Status = "running"
		case eventstore.TaskCompleted:
			rt.Status = "completed"
		case eventstore.TaskFailed:
			rt.Status = "failed"
		case eventstore.TaskCancelled:
			rt.Status = "cancelled"
		case eventstore.TurnStarted:
			rt.Turns++
		case eventstore.ModelCallStarted:
			rt.ModelCalls++
		case eventstore.ToolStarted:
			rt.ToolCalls++
		case eventstore.ModelCallFailed, eventstore.ToolFailed:
			rt.Failures++
		case eventstore.ValidationCompleted:
			var vp eventstore.ValidationPayload
			if e.DecodePayload(&vp) == nil && vp.Status == "passed" {
				rt.Validated = true
			}
		}
	}
	return rt
}

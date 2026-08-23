package hooks

import (
	"context"

	"github.com/kaiau00/aux-cli/internal/logging"
)

// RegisterObservability attaches the built-in handlers for every lifecycle
// point (roadmapplan.md §12.3).
//
// Without this, the registry is constructed and threaded everywhere but has no
// handlers registered anywhere in production, so all six dispatch points fire
// into an empty list — real machinery doing nothing. These handlers give the
// hook system a real consumer: a structured debug trace of task, subtask, and
// tool lifecycle that makes "what did the agent actually do, in what order"
// answerable from the logs alone.
//
// They are deliberately observation-only. Returning an error from a ToolPre
// handler vetoes the tool call, so a logging handler must never do that, and
// none of these do.
//
// User-defined hooks (running a shell command on a lifecycle point) are
// intentionally NOT registered here: that is arbitrary code execution driven
// by a config file, and it needs its own design and security review rather
// than riding along with observability.
func RegisterObservability(r *Registry) {
	if r == nil {
		return
	}
	r.Register(TaskBegin, func(_ context.Context, e Event) error {
		logging.Debug("task begin", "task_id", e.TaskID, "session_id", e.SessionID)
		return nil
	})
	r.Register(TaskEnd, func(_ context.Context, e Event) error {
		logging.Debug("task end", "task_id", e.TaskID, "session_id", e.SessionID, "outcome", e.Outcome)
		return nil
	})
	r.Register(SubtaskBegin, func(_ context.Context, e Event) error {
		logging.Debug("subtask begin", "parent_task_id", e.ParentTaskID, "session_id", e.SessionID, "role", e.Data["role"])
		return nil
	})
	r.Register(SubtaskEnd, func(_ context.Context, e Event) error {
		logging.Debug("subtask end", "parent_task_id", e.ParentTaskID, "session_id", e.SessionID,
			"role", e.Data["role"], "outcome", e.Outcome)
		return nil
	})
	r.Register(ToolPre, func(_ context.Context, e Event) error {
		logging.Debug("tool pre", "tool", e.Tool, "task_id", e.TaskID, "session_id", e.SessionID)
		return nil
	})
	r.Register(ToolPost, func(_ context.Context, e Event) error {
		logging.Debug("tool post", "tool", e.Tool, "task_id", e.TaskID, "session_id", e.SessionID, "outcome", e.Outcome)
		return nil
	})
	r.Register(ValidationComplete, func(_ context.Context, e Event) error {
		logging.Debug("validation complete", "task_id", e.TaskID, "outcome", e.Outcome)
		return nil
	})
}

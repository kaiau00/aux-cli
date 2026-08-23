// Package hooks is the local lifecycle-hook mechanism. It
// lets in-process observers and local extensions react at defined runtime points
// — task begin/end, tool pre/post, validation complete — without coupling to the
// core services. It stays dependency-light and deterministic; a nil registry is
// a safe no-op so the runtime can always dispatch.
package hooks

import (
	"context"
	"sync"
)

// Point is a named lifecycle point.
type Point string

const (
	TaskBegin          Point = "task.begin"
	TaskEnd            Point = "task.end"
	ToolPre            Point = "tool.pre"
	ToolPost           Point = "tool.post"
	ValidationComplete Point = "validation.complete"
	// SubtaskBegin/SubtaskEnd bracket one subagent's run within a parent task.
	// They are distinct from TaskBegin/TaskEnd — which
	// still fire once for the subagent's own task record — so a handler can
	// react specifically to subagent lifecycle (e.g. per-subtask checkpointing)
	// without matching on ParentTaskID being non-empty in every TaskBegin/TaskEnd.
	SubtaskBegin Point = "subtask.begin"
	SubtaskEnd   Point = "subtask.end"
)

// Event is dispatched to handlers at a lifecycle point. Fields are best-effort
// context; unused fields are empty.
type Event struct {
	Point Point
	// TaskID is the subject of the event: the task itself for TaskBegin/TaskEnd,
	// or the subagent's own task for SubtaskBegin/SubtaskEnd.
	TaskID string
	// ParentTaskID is set on SubtaskBegin/SubtaskEnd to the task that spawned
	// the subagent.
	ParentTaskID string
	SessionID    string
	Tool         string
	Outcome      string
	Data         map[string]string
}

// Handler reacts to a lifecycle event. Returning an error from a pre-point
// handler (e.g. ToolPre) vetoes the action; post-point errors are advisory and
// the caller decides whether to honor them.
type Handler func(ctx context.Context, e Event) error

// Registry holds handlers per lifecycle point.
type Registry struct {
	mu       sync.RWMutex
	handlers map[Point][]Handler
}

// NewRegistry returns an empty hook registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[Point][]Handler)}
}

// Register adds a handler for a lifecycle point. Handlers run in registration
// order.
func (r *Registry) Register(p Point, h Handler) {
	if r == nil || h == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[p] = append(r.handlers[p], h)
}

// Dispatch runs every handler for the event's point in registration order and
// returns the first error (so a pre-point handler can veto). A nil registry or
// no registered handlers is a no-op returning nil.
func (r *Registry) Dispatch(ctx context.Context, e Event) error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	handlers := append([]Handler(nil), r.handlers[e.Point]...)
	r.mu.RUnlock()
	for _, h := range handlers {
		if err := h(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// Count returns the number of handlers registered for a point (for tests/introspection).
func (r *Registry) Count(p Point) int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[p])
}

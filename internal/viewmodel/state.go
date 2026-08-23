// Package viewmodel builds truthful, event-backed presentation state for the TUI
// and dashboard. View models are pure projections of durable runtime data —
// events, task/validation state, context bindings, and the cost ledger — never
// inferred from rendered strings.
package viewmodel

// Component state vocabulary. The same wording appears in
// events, TUI, dashboard, and docs.
type ComponentState string

const (
	StateQueued     ComponentState = "queued"
	StateActive     ComponentState = "active"
	StateWaiting    ComponentState = "waiting"
	StateCompleted  ComponentState = "completed"
	StateFailed     ComponentState = "failed"
	StateBlocked    ComponentState = "blocked"
	StateCancelled  ComponentState = "cancelled"
	StateStale      ComponentState = "stale"
	StatePinned     ComponentState = "pinned"
	StateCached     ComponentState = "cached"
	StateUnverified ComponentState = "unverified"
	StateValidated  ComponentState = "validated"
)

// ActivityKind groups mechanical operations into user-understandable stages.
type ActivityKind string

const (
	ActivitySearching ActivityKind = "searching"
	ActivityReading   ActivityKind = "reading"
	ActivityEditing   ActivityKind = "editing"
	ActivityTesting   ActivityKind = "testing"
	ActivityPlanning  ActivityKind = "planning"
	ActivityWaiting   ActivityKind = "waiting"
	ActivityOther     ActivityKind = "other"
)

// activityForTool maps a tool name to its activity group.
func activityForTool(tool string) ActivityKind {
	switch tool {
	case "glob", "grep", "ls", "sourcegraph", "agent", "fetch":
		return ActivitySearching
	case "view", "diagnostics", "artifact":
		return ActivityReading
	case "edit", "write", "patch":
		return ActivityEditing
	case "bash":
		return ActivityTesting
	default:
		return ActivityOther
	}
}

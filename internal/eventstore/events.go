// Package eventstore is the durable, ordered domain-event log for Aux. It is the
// authoritative record of what happened at runtime; dashboard read models and
// task replay are derived from it. See ADR 0002.
//
// The in-process pubsub broker (embedded in Service) is only a low-latency
// notification path: notifications are published after the row commits, and
// subscribers may miss them and must recover from the persisted sequence.
package eventstore

import "encoding/json"

// SchemaVersion is the current envelope schema version stamped on new events.
const SchemaVersion = 1

// Type is a domain-event type. The taxonomy below is closed;
// types are declared even before their producing subsystem exists so the
// vocabulary is stable across PRs.
type Type string

const (
	// Task lifecycle.
	TaskCreated   Type = "task.created"
	TaskCompiled  Type = "task.compiled"
	TaskStarted   Type = "task.started"
	TaskCompleted Type = "task.completed"
	TaskFailed    Type = "task.failed"
	TaskCancelled Type = "task.cancelled"

	// Turn lifecycle (one model call + its tool results).
	TurnStarted   Type = "turn.started"
	TurnCompleted Type = "turn.completed"

	// Model call lifecycle.
	ModelCallStarted    Type = "model_call.started"
	ModelCallFirstToken Type = "model_call.first_token"
	ModelCallCompleted  Type = "model_call.completed"
	ModelCallFailed     Type = "model_call.failed"

	// Context lifecycle.
	ContextCompiled    Type = "context.compiled"
	ContextPageBound   Type = "context.page_bound"
	ContextPageEvicted Type = "context.page_evicted"
	ContextPageFault   Type = "context.page_fault"

	// Tool lifecycle.
	ToolStarted   Type = "tool.started"
	ToolCompleted Type = "tool.completed"
	ToolFailed    Type = "tool.failed"

	// Artifact lifecycle.
	ArtifactCreated Type = "artifact.created"
	ArtifactReused  Type = "artifact.reused"

	// Validation lifecycle.
	ValidationStarted   Type = "validation.started"
	ValidationCompleted Type = "validation.completed"

	// Checkpoint lifecycle.
	CheckpointCreated Type = "checkpoint.created"

	// Memory lifecycle.
	MemoryCandidateCreated Type = "memory.candidate_created"
	MemoryPromoted         Type = "memory.promoted"
	MemoryInvalidated      Type = "memory.invalidated"

	// Skill lifecycle.
	SkillCandidateCreated Type = "skill.candidate_created"
	SkillPromoted         Type = "skill.promoted"
	SkillRolledBack       Type = "skill.rolled_back"

	// Budget / governor.
	BudgetAllocated  Type = "budget.allocated"
	BudgetWarning    Type = "budget.warning"
	BudgetExhausted  Type = "budget.exhausted"
	GovernorDecision Type = "governor.decision"

	// Governor policy lifecycle.
	PolicyPromoted   Type = "policy.promoted"
	PolicyRolledBack Type = "policy.rolled_back"
)

// Event is one row of the durable log. Payload holds the marshaled typed payload.
type Event struct {
	ID            string          `json:"id"`
	Sequence      int64           `json:"sequence"`
	Type          Type            `json:"type"`
	SchemaVersion int             `json:"schemaVersion"`
	ProjectID     string          `json:"projectId,omitempty"`
	SessionID     string          `json:"sessionId,omitempty"`
	TaskID        string          `json:"taskId,omitempty"`
	TurnID        string          `json:"turnId,omitempty"`
	OccurredAt    int64           `json:"occurredAt"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// Append describes a new event to record. The store assigns ID, Sequence,
// OccurredAt, and SchemaVersion, and marshals Payload.
type Append struct {
	Type      Type
	ProjectID string
	SessionID string
	TaskID    string
	TurnID    string
	// Payload is any JSON-marshalable struct, or nil.
	Payload any
	// OccurredAt overrides the timestamp (unix millis). Zero means "now".
	OccurredAt int64
}

// DecodePayload unmarshals an event's payload into v.
func (e Event) DecodePayload(v any) error {
	if len(e.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(e.Payload, v)
}

// Filter selects and paginates events for reads.
type Filter struct {
	ProjectID string
	SessionID string
	TaskID    string
	TurnID    string
	Types     []Type
	// AfterSequence returns only events with sequence strictly greater than this.
	AfterSequence int64
	// Limit caps the number of returned events (0 = no limit).
	Limit int
}

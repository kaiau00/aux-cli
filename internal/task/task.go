// Package task turns a user request into a first-class, versioned task
// specification with scope, constraints, acceptance criteria, validation
// intents, and a budget. Storage/UI history stays
// separate from this compiled task state.
package task

// Mode is the task classification. Inference starts deterministic; the primary
// model refines ambiguity within its normal call (no separate classifier).
type Mode string

const (
	ModeImplementation Mode = "implementation"
	ModeBugDiagnosis   Mode = "bug_diagnosis"
	ModeRefactor       Mode = "refactor"
	ModeTestAuthoring  Mode = "test_authoring"
	ModeCodeReview     Mode = "code_review"
	ModeResearch       Mode = "research"
	ModeMaintenance    Mode = "maintenance"
)

// Status is a task's lifecycle state.
type Status string

const (
	StatusCreated   Status = "created"
	StatusCompiled  Status = "compiled"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// CriterionState follows the proof-of-done model.
type CriterionState string

const (
	CriterionUncovered          CriterionState = "uncovered"
	CriterionClaimed            CriterionState = "claimed"
	CriterionPartiallyEvidenced CriterionState = "partially_evidenced"
	CriterionValidated          CriterionState = "validated"
	CriterionBlocked            CriterionState = "blocked"
	CriterionWaived             CriterionState = "waived_by_user"
)

// CompilerVersion identifies the task-compiler behaviour for provenance.
const CompilerVersion = "task-compiler-1"

// Task is the durable task record.
type Task struct {
	ID                string
	ProjectID         string
	SessionID         string
	ProjectRevisionID string
	ProfileVersionSet string
	Objective         string
	Mode              Mode
	Status            Status
	Outcome           string
	CreatedAt         int64
	StartedAt         int64
	FinishedAt        int64
	// ParentTaskID links a child task to the task that spawned it: a multi-repo
	// child spec or a subagent's own task. Empty
	// for an ordinary top-level task.
	ParentTaskID string
}

// Criterion is one acceptance criterion with proof-of-done state.
type Criterion struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	State       CriterionState `json:"state"`
}

// ValidationIntent expresses validation as intent, resolved to a project command.
type ValidationIntent struct {
	ID      string `json:"id"`
	Intent  string `json:"intent"`
	Command string `json:"command,omitempty"`
	Scope   string `json:"scope,omitempty"`
}

// Budget captures the task's cost/token/time/tool ceilings and mode.
type Budget struct {
	Mode           string  `json:"mode"`
	MaxCost        float64 `json:"maxCost"`
	MaxInputTokens int64   `json:"maxInputTokens"`
	MaxOutputToken int64   `json:"maxOutputTokens"`
	MaxWallMS      int64   `json:"maxWallMs"`
	MaxToolCalls   int64   `json:"maxToolCalls"`
}

// Spec is the compiled task specification (persisted as task_specs.content_json).
type Spec struct {
	TaskID             string             `json:"taskId"`
	SpecVersion        int                `json:"specVersion"`
	Objective          string             `json:"objective"`
	Mode               Mode               `json:"mode"`
	InScope            []string           `json:"inScope,omitempty"`
	OutOfScope         []string           `json:"outOfScope,omitempty"`
	Constraints        []string           `json:"constraints,omitempty"`
	AcceptanceCriteria []Criterion        `json:"acceptanceCriteria,omitempty"`
	ValidationIntents  []ValidationIntent `json:"validationIntents,omitempty"`
	Budget             Budget             `json:"budget"`
	ProfileVersionID   string             `json:"profileVersionId,omitempty"`
	Unknowns           []string           `json:"unknowns,omitempty"`
}

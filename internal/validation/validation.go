// Package validation plans and records checks and determines proof-of-done state.
// It never converts a claim into validated state on its
// own: a criterion becomes validated only when appropriate evidence exists, and
// completion policy is task-mode and risk aware.
package validation

// Status is a validation run's status.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
	StatusBlocked Status = "blocked"
)

// EvidenceType classifies acceptance evidence.
const (
	EvidenceExecutable = "executable" // a command ran and passed
	EvidenceInspection = "inspection" // cited inspection (research tasks)
	EvidenceDiff       = "diff"       // a change diff
	EvidenceWaiver     = "user_waiver"
)

// CriterionState is the proof-of-done state of one acceptance criterion.
type CriterionState string

const (
	Uncovered          CriterionState = "uncovered"
	Claimed            CriterionState = "claimed"
	PartiallyEvidenced CriterionState = "partially_evidenced"
	Validated          CriterionState = "validated"
	Blocked            CriterionState = "blocked"
	WaivedByUser       CriterionState = "waived_by_user"
)

// Run is a validation execution record.
type Run struct {
	ID               string
	TaskID           string
	IntentID         string
	ValidatorType    string
	Command          string
	CommandHash      string
	InputFingerprint string
	Status           Status
	StartedAt        int64
	FinishedAt       int64
	ExitCode         int
	DurationMS       int64
	OutputArtifactID string
	CreatedAt        int64
}

// Evidence links a validation run (or inspection) to an acceptance criterion.
type Evidence struct {
	ID              string
	TaskID          string
	CriterionID     string
	ValidationRunID string
	EvidenceType    string
	Summary         string
	CreatedAt       int64
}

// Intent is a validation intent resolved to a concrete command (from the task
// spec + profile + impact recommendation).
type Intent struct {
	ID            string
	ValidatorType string
	Command       string
	// CriterionIDs are the acceptance criteria this intent provides evidence for.
	CriterionIDs []string
}

// Result is the outcome of running a single intent.
type Result struct {
	Run    Run
	Cached bool
}

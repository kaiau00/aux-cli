// Package govpolicy manages learned cost-governor policies with the same
// evaluation-gated promotion discipline as skills. A
// policy is a candidate until a passing evaluation against a baseline promotes it
// to active; there is no autonomous promotion, and every active policy has an
// evidence trail and a rollback path.
package govpolicy

// State is a policy's lifecycle state.
type State string

const (
	StateCandidate  State = "candidate"
	StateActive     State = "active"
	StateRolledBack State = "rolled_back"
	StateRejected   State = "rejected"
)

// EvalResult is a policy evaluation outcome.
type EvalResult string

const (
	Pass         EvalResult = "pass"
	Fail         EvalResult = "fail"
	Inconclusive EvalResult = "inconclusive"
)

// Policy is a learned governor policy for a task class.
type Policy struct {
	ID         string
	OwnerType  string
	OwnerID    string
	TaskClass  string
	State      State
	PolicyJSON string
	CreatedAt  int64
	UpdatedAt  int64
}

// Evaluation is evidence that a policy beat (or did not beat) a baseline.
type Evaluation struct {
	ID               string
	PolicyID         string
	BaselinePolicyID string
	EvalRunID        string
	Result           EvalResult
	MetricsJSON      string
}

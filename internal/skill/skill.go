// Package skill imports, versions, retrieves, evaluates, promotes, and rolls
// back skills — reusable, evaluated procedures compiled from repeated successful
// work. Promotion is evaluation-gated: a skill version can
// become active only with a passing evaluation on record, and the prior version
// is always retained as a rollback target.
package skill

// State is a skill's lifecycle state.
type State string

const (
	StateCandidate  State = "candidate"
	StateActive     State = "active"
	StateRolledBack State = "rolled_back"
	StateRejected   State = "rejected"
	StateArchived   State = "archived"
)

// EvalResult is a skill evaluation outcome.
type EvalResult string

const (
	EvalPass         EvalResult = "pass"
	EvalFail         EvalResult = "fail"
	EvalInconclusive EvalResult = "inconclusive"
)

// Step is one step of a skill procedure with an optional decision point.
type Step struct {
	Title    string `json:"title"`
	Action   string `json:"action,omitempty"`
	Decision string `json:"decision,omitempty"`
}

// Content is the body of a skill version.
type Content struct {
	Name                   string   `json:"name"`
	Purpose                string   `json:"purpose"`
	Scope                  string   `json:"scope,omitempty"`
	Triggers               []string `json:"triggers,omitempty"`
	Exclusions             []string `json:"exclusions,omitempty"`
	RequiredCapabilities   []string `json:"requiredCapabilities,omitempty"`
	Inputs                 []string `json:"inputs,omitempty"`
	Outputs                []string `json:"outputs,omitempty"`
	Procedure              []Step   `json:"procedure,omitempty"`
	ToolRequirements       []string `json:"toolRequirements,omitempty"`
	ContextRequirements    []string `json:"contextRequirements,omitempty"`
	ValidationRequirements []string `json:"validationRequirements,omitempty"`
	FailurePatterns        []string `json:"failurePatterns,omitempty"`
}

// Skill is the durable skill record.
type Skill struct {
	ID        string
	OwnerType string
	OwnerID   string
	Name      string
	Scope     string
	State     State
	CreatedAt int64
	UpdatedAt int64
}

// Version is a content snapshot of a skill.
type Version struct {
	ID         string
	SkillID    string
	Content    Content
	SourceType string
	SourceIDs  []string
	CreatedAt  int64
}

// Evaluation is a baseline-vs-candidate comparison for a skill version.
type Evaluation struct {
	ID              string
	SkillVersionID  string
	EvalRunID       string
	BaselineVersion string
	Result          EvalResult
	MetricsJSON     string
	CreatedAt       int64
}

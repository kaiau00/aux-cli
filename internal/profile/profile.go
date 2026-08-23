// Package profile builds, versions, and stores compiled project profiles from
// deterministic scanners. Each scanner reads specific
// project inputs and emits typed entries with a source and confidence; a profile
// version is content-addressed so unchanged inputs reuse the previous version.
package profile

// Entry types. Kept as string constants so new kinds are additive.
const (
	EntryLanguage          = "language"
	EntryFramework         = "framework"
	EntryValidationCommand = "validation_command"
	EntryBuildCommand      = "build_command"
	EntryConvention        = "convention"
	EntryConstraint        = "constraint"
	EntryInstruction       = "instruction"
	EntryWorkspace         = "workspace"
	EntryArchitecture      = "architecture"
	EntryTool              = "tool"
	EntrySkill             = "skill"
)

// Profile owners, lowest to highest precedence.
const (
	OwnerBuiltin   = "builtin"
	OwnerUser      = "user"
	OwnerOrg       = "org"
	OwnerProject   = "project"
	OwnerWorkspace = "workspace"
	OwnerBranch    = "branch"
	OwnerTaskMode  = "task_mode"
)

// Precedence values matching the owner ordering.
var Precedence = map[string]int{
	OwnerBuiltin:   0,
	OwnerUser:      1,
	OwnerOrg:       2,
	OwnerProject:   3,
	OwnerWorkspace: 4,
	OwnerBranch:    5,
	OwnerTaskMode:  6,
}

// EntryDraft is a scanner's output before persistence. Value is marshaled to JSON.
type EntryDraft struct {
	Type          string
	Key           string
	Value         any
	SourceType    string
	SourceRef     string
	Confidence    float64
	TokenEstimate int
}

// ScanResult is one scanner's contribution: typed entries plus a fingerprint of
// the raw inputs it read (so unchanged inputs reuse prior entries).
type ScanResult struct {
	Entries     []EntryDraft
	Fingerprint string
}

// Profile is a versioned bag of entries owned by some layer.
type Profile struct {
	ID         string
	OwnerType  string
	OwnerID    string
	Name       string
	Precedence int
	Enabled    bool
	CreatedAt  int64
	UpdatedAt  int64
}

// Version is a content-addressed snapshot of a profile's entries.
type Version struct {
	ID              string
	ProfileID       string
	SourceRevision  string
	ContentHash     string
	CompilerVersion string
	CreatedAt       int64
	// Reused is true when Build returned an existing version rather than creating one.
	Reused bool
}

// Entry is a persisted profile entry.
type Entry struct {
	ID               string
	ProfileVersionID string
	Type             string
	Key              string
	ValueJSON        string
	SourceType       string
	SourceRef        string
	Confidence       float64
	TokenEstimate    int
}

// CompilerVersion identifies the profile-compiler behaviour for provenance.
const CompilerVersion = "profile-compiler-1"

// estimateTokens is a cheap heuristic (~4 chars/token).
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}

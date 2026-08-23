// Package memory manages factual, procedural, and episodic project memory with
// provenance and revision-aware invalidation. Memory is
// compiled, bounded knowledge with a reversible version chain; candidates are
// promoted only with evidence, and source changes mark memories stale rather
// than deleting them.
package memory

// Type classifies a memory.
type Type string

const (
	// Factual: verified project facts (architecture, conventions, commands).
	Factual Type = "factual"
	// Procedural: successful project-specific methods.
	Procedural Type = "procedural"
	// Episodic: compact prior-task summaries.
	Episodic Type = "episodic"
)

// State is a memory's lifecycle state.
type State string

const (
	StateCandidate  State = "candidate"
	StateActive     State = "active"
	StateSuperseded State = "superseded"
	StateStale      State = "stale"
	StateRejected   State = "rejected"
	StateArchived   State = "archived"
)

// Memory is the durable memory record.
type Memory struct {
	ID         string
	ProjectID  string
	Type       Type
	Scope      string
	StableKey  string
	State      State
	Confidence float64
	CreatedAt  int64
	UpdatedAt  int64
}

// Version is a content-addressed snapshot of a memory's content.
type Version struct {
	ID                  string
	MemoryID            string
	ContentJSON         string
	ContentHash         string
	SupportingRevision  string
	SupersedesVersionID string
	CreatedAt           int64
}

// Source records where a memory version came from (provenance).
type Source struct {
	Type     string
	ID       string
	Hash     string
	Relation string
}

// Candidate is a proposed memory before persistence, carrying provenance,
// scope, confidence, and an invalidation strategy.
type Candidate struct {
	ProjectID          string
	Type               Type
	Scope              string
	StableKey          string
	Content            any
	Confidence         float64
	SupportingRevision string
	Sources            []Source
	// InvalidateOnRevisionChange marks the memory stale when its supporting
	// revision no longer matches.
	InvalidateOnRevisionChange bool
}

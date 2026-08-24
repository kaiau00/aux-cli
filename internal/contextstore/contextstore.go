// Package contextstore stores typed context pages, their content-addressed
// versions, and the bindings that record what a specific model call held
// resident, available, or evicted. It makes
// every compiled prompt explainable page by page.
package contextstore

// Page kinds. Declared as constants so new kinds are additive.
const (
	KindProjectManifest    = "project_manifest"
	KindTaskSpec           = "task_spec"
	KindPlanState          = "plan_state"
	KindFileRegion         = "file_region"
	KindSymbolDefinition   = "symbol_definition"
	KindSymbolReferences   = "symbol_references"
	KindDependencyNeighbor = "dependency_neighborhood"
	KindInstruction        = "instruction"
	KindMemoryFact         = "memory_fact"
	KindMemoryProcedure    = "memory_procedure"
	KindMemoryEpisode      = "memory_episode"
	KindToolDigest         = "tool_digest"
	KindValidationResult   = "validation_result"
	KindDiffSummary        = "diff_summary"
	KindSkillManifest      = "skill_manifest"
	KindSkillBody          = "skill_body"
	KindRecentConversation = "recent_conversation"
)

// Binding states.
//
// Resident, available and pinned are written by the agent as it binds each
// call's pages. Evicted and faulted are declared but never written: nothing in
// Aux evicts a page to fit a budget or faults one back in, because no context
// budget is enforced anywhere. They are kept because they are part of the state
// model the schema stores, not because the behaviour exists.
const (
	StateResident  = "resident"
	StateAvailable = "available"
	StateEvicted   = "evicted"
	StateFaulted   = "faulted"
	StatePinned    = "pinned"
)

// Page is a stable, typed context unit.
type Page struct {
	ID        string
	ProjectID string
	Type      string
	StableKey string
	Scope     string
	CreatedAt int64
}

// PageVersion is a content-addressed snapshot of a page.
type PageVersion struct {
	ID             string
	PageID         string
	ContentHash    string
	ArtifactID     string
	SourceRevision string
	TokenCount     int64
	MetadataJSON   string
	CreatedAt      int64
}

// Binding records that a page version was in a model call's working set.
type Binding struct {
	ID            string
	TaskID        string
	ModelCallID   string
	PageVersionID string
	State         string
	Rank          int
	Reason        string
	TokenCount    int64
	BoundAt       int64
	EvictedAt     int64
}

// BoundPage is a binding joined with its page identity, for read models.
type BoundPage struct {
	Binding
	PageType  string
	StableKey string
}

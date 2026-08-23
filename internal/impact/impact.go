// Package impact maintains a hybrid code graph and proposes affected symbols,
// packages, and tests for a set of changed paths.
// It prefers deterministic analysis (AST, build manifests, git) over any model
// call, and broadens validation automatically when the graph is stale,
// incomplete, or uncertain.
package impact

// Node types.
const (
	NodeFile      = "file"
	NodeDirectory = "directory"
	NodePackage   = "package"
	NodeModule    = "module"
	NodeSymbol    = "symbol"
	NodeTest      = "test"
	NodeCommand   = "command"
	NodeGenerated = "generated"
)

// Edge types.
const (
	EdgeImports    = "imports"
	EdgeCalls      = "calls"
	EdgeReferences = "references"
	EdgeImplements = "implements"
	EdgeContains   = "contains"
	EdgeBuilds     = "builds"
	EdgeTests      = "tests"
	EdgeCoChanges  = "co_changes"
	EdgeOwns       = "owns"
	EdgeGenerates  = "generates"
)

// Edge sources.
const (
	SourceAST      = "ast"
	SourceLSP      = "lsp"
	SourceManifest = "manifest"
	SourceGit      = "git"
	SourceMCP      = "mcp"
)

// IndexerVersion identifies the indexer behaviour for provenance/staleness.
const IndexerVersion = "impact-indexer-1"

// Node is a graph node.
type Node struct {
	ID             string
	ProjectID      string
	Type           string
	StableKey      string
	DisplayName    string
	SourceRevision string
	MetadataJSON   string
}

// Edge is a typed, weighted relationship with a source.
type Edge struct {
	ID             string
	ProjectID      string
	FromNodeID     string
	ToNodeID       string
	Type           string
	Weight         float64
	Source         string
	SourceRevision string
}

// IndexState records the last indexing run for a project.
type IndexState struct {
	ProjectID      string
	SourceRevision string
	IndexerVersion string
	Status         string
	LastIndexedAt  int64
}

// RiskLevel summarises change blast radius.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// TestRec is a recommended test with the reason it was selected.
type TestRec struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Result is the impact analysis for a set of changed paths.
type Result struct {
	ChangedPaths     []string  `json:"changedPaths"`
	DirectDependents []string  `json:"directDependents"`
	AffectedPackages []string  `json:"affectedPackages"`
	RelatedTests     []TestRec `json:"relatedTests"`
	Risk             RiskLevel `json:"risk"`
	// Uncertainty in [0,1]; higher when the graph is stale, empty, or the change
	// touches paths the graph does not cover.
	Uncertainty float64 `json:"uncertainty"`
	// BroadenValidation is true when impact selection must not be the only basis:
	// the caller should run broader validation.
	BroadenValidation bool     `json:"broadenValidation"`
	Recommended       []string `json:"recommended"`
	Reason            string   `json:"reason"`
}

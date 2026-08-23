// Package checkpoint creates task checkpoints and branch/delta relationships.
// Checkpoints reference content-addressed before/after
// blobs so branches share immutable content and store only deltas, and they form
// an acyclic DAG via parent links. Write conflicts are detected before merge.
package checkpoint

// Operation classifies a checkpoint entry's change.
type Operation string

const (
	OpAdd    Operation = "add"
	OpModify Operation = "modify"
	OpDelete Operation = "delete"
	OpRename Operation = "rename"
)

// Checkpoint is a captured task state.
type Checkpoint struct {
	ID        string
	TaskID    string
	ParentID  string
	Label     string
	Revision  string
	TreeHash  string
	CreatedAt int64
}

// Entry is a single changed path in a checkpoint, referencing content-addressed
// before/after blobs (empty when adding/deleting).
type Entry struct {
	ID               string
	CheckpointID     string
	Path             string
	BeforeArtifactID string
	AfterArtifactID  string
	Operation        Operation
	Mode             string
}

// FileChange is the input describing one changed file for a new checkpoint.
type FileChange struct {
	Path      string
	Before    []byte // nil when the file is new
	After     []byte // nil when the file is deleted
	Operation Operation
	Mode      string
}

package checkpoint

// FileVersion is a path with a content snapshot, used to derive a change set
// from recorded file history without coupling this package to the history store.
type FileVersion struct {
	Path    string
	Content string
}

// ChangesFrom builds the file changes between a before snapshot (path -> initial
// content) and the latest content per path. Net-unchanged paths are skipped, and
// the operation is derived from presence: empty-before is an add, empty-after a
// delete, otherwise a modify. Pure and deterministic — the single source of the
// "recorded history -> checkpoint changes" projection.
func ChangesFrom(before map[string]string, latest []FileVersion) []FileChange {
	var changes []FileChange
	for _, f := range latest {
		b := before[f.Path]
		if b == f.Content {
			continue
		}
		op := OpModify
		switch {
		case b == "":
			op = OpAdd
		case f.Content == "":
			op = OpDelete
		}
		changes = append(changes, FileChange{
			Path: f.Path, Before: []byte(b), After: []byte(f.Content), Operation: op,
		})
	}
	return changes
}

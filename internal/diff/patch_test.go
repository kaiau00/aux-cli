package diff

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

// recorder captures what ApplyCommit did to the filesystem, in order. Order is
// the point of most of these tests: this package is the one that mutates the
// user's files, and the sequence decides whether a failure half-way through
// loses content or merely leaves extra.
type recorder struct {
	ops       []string
	writeErr  error
	removeErr error
}

func (r *recorder) write(path, content string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	r.ops = append(r.ops, "write "+path+"="+content)
	return nil
}

func (r *recorder) remove(path string) error {
	if r.removeErr != nil {
		return r.removeErr
	}
	r.ops = append(r.ops, "remove "+path)
	return nil
}

// A move must write the new path before removing the old one. If it removed
// first and the write then failed, the content would be gone; in this order the
// worst case is a leftover copy, which is recoverable. This ordering is the
// single most important property in the file.
func TestApplyCommitWritesTheNewPathBeforeRemovingTheOld(t *testing.T) {
	r := &recorder{}
	commit := Commit{Changes: map[string]FileChange{
		"old.go": {Type: ActionUpdate, OldContent: ptr("before"), NewContent: ptr("after"), MovePath: ptr("new.go")},
	}}

	if err := ApplyCommit(commit, r.write, r.remove); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"write new.go=after", "remove old.go"}
	if !slices.Equal(r.ops, want) {
		t.Fatalf("a move must write then remove.\n got: %v\nwant: %v", r.ops, want)
	}
}

// The consequence of that ordering, stated as its own test: when the removal
// fails, the new content has already been written, so nothing is lost.
func TestApplyCommitKeepsTheNewContentWhenRemovalFails(t *testing.T) {
	r := &recorder{removeErr: errors.New("permission denied")}
	commit := Commit{Changes: map[string]FileChange{
		"old.go": {Type: ActionUpdate, NewContent: ptr("after"), MovePath: ptr("new.go")},
	}}

	if err := ApplyCommit(commit, r.write, r.remove); err == nil {
		t.Fatal("a failing removal must be reported")
	}
	if !slices.Contains(r.ops, "write new.go=after") {
		t.Fatalf("the new content must survive a failed removal, ops: %v", r.ops)
	}
}

// Nil content is a malformed commit. It must be refused rather than written as
// an empty file, which would silently truncate whatever was there.
func TestApplyCommitRefusesNilContent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change FileChange
	}{
		{"add", FileChange{Type: ActionAdd, NewContent: nil}},
		{"update", FileChange{Type: ActionUpdate, OldContent: ptr("x"), NewContent: nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{}
			commit := Commit{Changes: map[string]FileChange{"f.go": tc.change}}

			if err := ApplyCommit(commit, r.write, r.remove); err == nil {
				t.Fatal("nil content must be an error, not an empty write")
			}
			if len(r.ops) != 0 {
				t.Fatalf("nothing should have been written, got %v", r.ops)
			}
		})
	}
}

func TestApplyCommitAddsAndDeletes(t *testing.T) {
	r := &recorder{}
	commit := Commit{Changes: map[string]FileChange{
		"added.go": {Type: ActionAdd, NewContent: ptr("package main")},
	}}
	if err := ApplyCommit(commit, r.write, r.remove); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(r.ops, []string{"write added.go=package main"}) {
		t.Fatalf("unexpected ops: %v", r.ops)
	}

	r = &recorder{}
	commit = Commit{Changes: map[string]FileChange{
		"gone.go": {Type: ActionDelete, OldContent: ptr("package main")},
	}}
	if err := ApplyCommit(commit, r.write, r.remove); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(r.ops, []string{"remove gone.go"}) {
		t.Fatalf("unexpected ops: %v", r.ops)
	}
}

// A failing write must surface rather than be swallowed; the caller decides
// whether a partially applied commit can be retried.
func TestApplyCommitPropagatesWriteFailure(t *testing.T) {
	r := &recorder{writeErr: errors.New("disk full")}
	commit := Commit{Changes: map[string]FileChange{
		"f.go": {Type: ActionAdd, NewContent: ptr("x")},
	}}
	if err := ApplyCommit(commit, r.write, r.remove); err == nil {
		t.Fatal("a failing write must be reported")
	}
}

// --- building the commit -------------------------------------------------

func TestPatchToCommitUpdateAppliesChunks(t *testing.T) {
	orig := map[string]string{"f.go": "one\ntwo\nthree"}
	patch := Patch{Actions: map[string]PatchAction{
		"f.go": {Type: ActionUpdate, Chunks: []Chunk{
			{OrigIndex: 1, DelLines: []string{"two"}, InsLines: []string{"TWO"}},
		}},
	}}

	commit, err := PatchToCommit(patch, orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := commit.Changes["f.go"]
	if got, want := *change.NewContent, "one\nTWO\nthree"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := *change.OldContent, "one\ntwo\nthree"; got != want {
		t.Fatalf("the original content must be preserved for rollback, got %q want %q", got, want)
	}
}

// Chunks that walk backwards would duplicate or drop lines. They must be
// refused instead of producing silently wrong content.
func TestPatchToCommitRejectsOverlappingChunks(t *testing.T) {
	orig := map[string]string{"f.go": "a\nb\nc\nd"}
	patch := Patch{Actions: map[string]PatchAction{
		"f.go": {Type: ActionUpdate, Chunks: []Chunk{
			{OrigIndex: 2, DelLines: []string{"c"}, InsLines: []string{"C"}},
			{OrigIndex: 1, DelLines: []string{"b"}, InsLines: []string{"B"}},
		}},
	}}

	if _, err := PatchToCommit(patch, orig); err == nil {
		t.Fatal("overlapping chunks must be refused")
	}
}

func TestPatchToCommitRejectsAChunkPastTheEnd(t *testing.T) {
	orig := map[string]string{"f.go": "a\nb"}
	patch := Patch{Actions: map[string]PatchAction{
		"f.go": {Type: ActionUpdate, Chunks: []Chunk{
			{OrigIndex: 99, InsLines: []string{"x"}},
		}},
	}}

	if _, err := PatchToCommit(patch, orig); err == nil {
		t.Fatal("a chunk starting past the end of the file must be refused")
	}
}

// A chunk may sit exactly at end-of-file while claiming to delete more lines
// than remain. The bounds check only guards the chunk's start, so the trailing
// slice is taken at an index past the end -- which must be an error, not a
// panic in the code path that rewrites the user's files.
func TestPatchToCommitRejectsDeletingPastTheEnd(t *testing.T) {
	orig := map[string]string{"f.go": "a\nb"}
	patch := Patch{Actions: map[string]PatchAction{
		"f.go": {Type: ActionUpdate, Chunks: []Chunk{
			{OrigIndex: 2, DelLines: []string{"x", "y", "z"}},
		}},
	}}

	if _, err := PatchToCommit(patch, orig); err == nil {
		t.Fatal("deleting more lines than the file has must be refused")
	}
}

func TestPatchToCommitDeleteCapturesOldContent(t *testing.T) {
	orig := map[string]string{"f.go": "contents"}
	patch := Patch{Actions: map[string]PatchAction{"f.go": {Type: ActionDelete}}}

	commit, err := PatchToCommit(patch, orig)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	change := commit.Changes["f.go"]
	if change.OldContent == nil || *change.OldContent != "contents" {
		t.Fatal("a delete must record what was removed, or it cannot be undone")
	}
}

// --- the patch envelope ---------------------------------------------------

func TestTextToPatchRequiresTheEnvelope(t *testing.T) {
	body := "*** Update File: f.go\n@@\n-a\n+b"
	for _, tc := range []struct{ name, text string }{
		{"no begin", body + "\n*** End Patch"},
		{"no end", "*** Begin Patch\n" + body},
		{"empty", ""},
		{"single line", "*** Begin Patch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := TextToPatch(tc.text, map[string]string{"f.go": "a"}); err == nil {
				t.Fatal("a patch without its envelope must be refused")
			}
		})
	}
}

// --- identifying the files a patch touches --------------------------------

// These decide which files get read before a patch is applied, so a miss means
// applying a patch against content that was never loaded.
func TestIdentifyFilesNeededCoversUpdatesAndDeletes(t *testing.T) {
	text := `*** Begin Patch
*** Update File: a.go
*** Delete File: b.go
*** Add File: c.go
*** End Patch`

	got := IdentifyFilesNeeded(text)
	sort.Strings(got)
	if want := []string{"a.go", "b.go"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v (an added file does not exist yet, so it is not needed)", got, want)
	}

	added := IdentifyFilesAdded(text)
	if want := []string{"c.go"}; !slices.Equal(added, want) {
		t.Fatalf("got %v, want %v", added, want)
	}
}

func TestIdentifyFilesDeduplicates(t *testing.T) {
	text := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: a.go",
		"*** Update File: a.go",
		"*** End Patch",
	}, "\n")

	if got := IdentifyFilesNeeded(text); len(got) != 1 {
		t.Fatalf("the same path listed twice must appear once, got %v", got)
	}
}

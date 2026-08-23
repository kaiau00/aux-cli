package impact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubStore succeeds at everything except the calls a test opts into failing.
type stubStore struct {
	failDeleteEdges bool
	failIndexState  bool
	nodeMissing     bool

	deletedEdges  int
	upsertedEdges int
	states        []string
}

func (s *stubStore) SetIndexState(_ context.Context, st IndexState) error {
	if s.failIndexState {
		return errors.New("state write rejected")
	}
	s.states = append(s.states, st.Status)
	return nil
}

func (s *stubStore) NodeByKey(context.Context, string, string, string) (string, bool, error) {
	if s.nodeMissing {
		return "", false, nil
	}
	return "node-1", true, nil
}

func (s *stubStore) DeleteNodeEdges(context.Context, string) error {
	if s.failDeleteEdges {
		return errors.New("delete rejected")
	}
	s.deletedEdges++
	return nil
}

func (s *stubStore) UpsertNode(context.Context, Node) (string, error) { return "node-1", nil }

func (s *stubStore) UpsertEdge(context.Context, Edge) error {
	s.upsertedEdges++
	return nil
}

// goRepo writes a minimal module with one Go file and returns its root.
func goRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package x\n\nimport \"strings\"\n\nvar _ = strings.TrimSpace\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// Reindex clears a file's edges and then re-adds them. If the clear fails and
// the failure is swallowed, the re-add lands on top of the old edges and the
// graph accumulates dependencies that no longer exist -- which impact analysis
// then reports to the user as real. Reindex must refuse instead.
func TestReindexRefusesWhenStaleEdgesCannotBeCleared(t *testing.T) {
	store := &stubStore{failDeleteEdges: true}
	ix := &Indexer{store: store}

	err := ix.Reindex(context.Background(), "proj-1", goRepo(t), "rev-1", []string{"a.go"})
	if err == nil {
		t.Fatal("a failed edge reset was swallowed; the graph would keep edges that no longer exist")
	}
	if !strings.Contains(err.Error(), "a.go") {
		t.Fatalf("the error must name the file whose edges are now suspect; got: %v", err)
	}
	if store.upsertedEdges != 0 {
		t.Fatalf("no edge should be re-added after a failed reset, got %d", store.upsertedEdges)
	}
	if len(store.states) != 0 {
		t.Fatalf("a failed reindex must not record itself as indexed, got %v", store.states)
	}
}

// The terminal write is the one that claims "the graph is current at this
// revision". Returning success while it fails tells the caller something the
// recorded state contradicts.
func TestReindexRefusesWhenIndexStateCannotBeRecorded(t *testing.T) {
	store := &stubStore{failIndexState: true}
	ix := &Indexer{store: store}

	if err := ix.Reindex(context.Background(), "proj-1", goRepo(t), "rev-1", []string{"a.go"}); err == nil {
		t.Fatal("reindex reported success while the index state write failed")
	}
}

func TestIndexProjectRefusesWhenIndexStateCannotBeRecorded(t *testing.T) {
	store := &stubStore{failIndexState: true}
	ix := &Indexer{store: store}

	if _, err := ix.IndexProject(context.Background(), "proj-1", goRepo(t), "rev-1"); err == nil {
		t.Fatal("index reported success while the index state write failed")
	}
}

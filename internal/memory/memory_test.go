package memory_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/memory"
)

func newService(t *testing.T) (*memory.Service, *memory.Store, eventstore.Service) {
	t.Helper()
	conn := dbtest.New(t)
	store := memory.NewStore(conn)
	events := eventstore.NewService(conn)
	return memory.NewService(store, events), store, events
}

func TestExtractProducesEpisodicAndProcedural(t *testing.T) {
	cands := memory.Extract(memory.ExtractInput{
		ProjectID: "p", TaskID: "t1", Objective: "add cache", Mode: "implementation",
		Outcome: "completed", SupportingRevision: "rev1",
		SuccessfulCommands: []string{"go test ./..."},
	})
	if len(cands) != 2 {
		t.Fatalf("expected episodic + procedural, got %d", len(cands))
	}
	var haveEpisode, haveProc bool
	for _, c := range cands {
		if c.Type == memory.Episodic {
			haveEpisode = true
		}
		if c.Type == memory.Procedural && c.StableKey == "validate:go test ./..." {
			haveProc = true
		}
	}
	if !haveEpisode || !haveProc {
		t.Fatalf("missing expected candidates: %+v", cands)
	}
}

func TestLearnPromotesEpisodicAndHighConfidence(t *testing.T) {
	svc, store, events := newService(t)
	ctx := context.Background()

	err := svc.Learn(ctx, []memory.Candidate{
		{ProjectID: "p", Type: memory.Episodic, StableKey: "episode:t1", Confidence: 0.9,
			Sources: []memory.Source{{Type: "task", ID: "t1"}}},
		// Low-confidence procedural stays a candidate.
		{ProjectID: "p", Type: memory.Procedural, StableKey: "validate:x", Confidence: 0.7,
			Sources: []memory.Source{{Type: "task", ID: "t1"}}},
		// High-confidence factual auto-promotes.
		{ProjectID: "p", Type: memory.Factual, StableKey: "lang:go", Confidence: 0.95,
			Sources: []memory.Source{{Type: "scan", ID: "go.mod"}}},
	})
	if err != nil {
		t.Fatalf("Learn: %v", err)
	}

	active, _ := store.ListByState(ctx, "p", memory.StateActive)
	candidates, _ := store.ListByState(ctx, "p", memory.StateCandidate)
	if len(active) != 2 {
		t.Fatalf("expected 2 active (episodic + high-confidence factual), got %d", len(active))
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 remaining candidate (low-confidence procedural), got %d", len(candidates))
	}

	// Events emitted.
	promoted, _ := events.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.MemoryPromoted}})
	if len(promoted) != 2 {
		t.Fatalf("expected 2 memory.promoted events, got %d", len(promoted))
	}
}

func TestSaveCandidateDedupsAndVersions(t *testing.T) {
	_, store, _ := newService(t)
	ctx := context.Background()

	c := memory.Candidate{ProjectID: "p", Type: memory.Factual, StableKey: "k", Content: map[string]string{"v": "1"}, Confidence: 0.9}
	m1, v1, err := store.SaveCandidate(ctx, c)
	if err != nil {
		t.Fatalf("SaveCandidate: %v", err)
	}
	// Identical content reuses the version.
	m2, v2, _ := store.SaveCandidate(ctx, c)
	if m2.ID != m1.ID || v2.ID != v1.ID {
		t.Fatalf("identical candidate should dedup memory + version")
	}
	// Changed content adds a superseding version.
	c.Content = map[string]string{"v": "2"}
	_, v3, _ := store.SaveCandidate(ctx, c)
	if v3.ID == v1.ID || v3.SupersedesVersionID != v1.ID {
		t.Fatalf("changed content should add a version superseding the previous")
	}
}

func TestRetrieveOnlyActiveByConfidence(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	_ = svc.Learn(ctx, []memory.Candidate{
		{ProjectID: "p", Type: memory.Factual, StableKey: "a", Confidence: 0.9, Sources: []memory.Source{{ID: "s"}}},
		{ProjectID: "p", Type: memory.Factual, StableKey: "b", Confidence: 0.95, Sources: []memory.Source{{ID: "s"}}},
		{ProjectID: "p", Type: memory.Procedural, StableKey: "c", Confidence: 0.7, Sources: []memory.Source{{ID: "s"}}}, // candidate
	})
	got, err := svc.Retrieve(ctx, "p", nil, 10)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 active memories, got %d", len(got))
	}
	// Highest confidence first.
	if got[0].StableKey != "b" {
		t.Fatalf("expected highest-confidence memory first, got %q", got[0].StableKey)
	}
}

func TestInvalidateForRevisionMarksStale(t *testing.T) {
	svc, store, events := newService(t)
	ctx := context.Background()

	// An active memory supported by rev1.
	_ = svc.Learn(ctx, []memory.Candidate{
		{ProjectID: "p", Type: memory.Factual, StableKey: "arch", Confidence: 0.95,
			SupportingRevision: "rev1", Sources: []memory.Source{{ID: "s"}}},
	})

	// The project moves to rev2 -> the memory becomes stale (not deleted).
	n, err := svc.InvalidateForRevision(ctx, "p", "rev2")
	if err != nil {
		t.Fatalf("InvalidateForRevision: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 memory marked stale, got %d", n)
	}
	stale, _ := store.ListByState(ctx, "p", memory.StateStale)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale memory")
	}
	// Not deleted: total memories preserved.
	inval, _ := events.List(ctx, eventstore.Filter{Types: []eventstore.Type{eventstore.MemoryInvalidated}})
	if len(inval) != 1 {
		t.Fatalf("expected 1 memory.invalidated event, got %d", len(inval))
	}

	// Same revision leaves it alone.
	svc2, store2, _ := newService(t)
	_ = svc2.Learn(ctx, []memory.Candidate{
		{ProjectID: "p", Type: memory.Factual, StableKey: "arch", Confidence: 0.95,
			SupportingRevision: "rev1", Sources: []memory.Source{{ID: "s"}}},
	})
	if n, _ := svc2.InvalidateForRevision(ctx, "p", "rev1"); n != 0 {
		t.Fatalf("same revision should not invalidate")
	}
	active, _ := store2.ListByState(ctx, "p", memory.StateActive)
	if len(active) != 1 {
		t.Fatalf("memory should remain active at the same revision")
	}
}

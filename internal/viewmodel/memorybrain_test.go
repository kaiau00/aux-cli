package viewmodel_test

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func TestMemoryBrainViewGroupsByState(t *testing.T) {
	conn := dbtest.New(t)
	ctx := context.Background()
	events := eventstore.NewService(conn)
	memStore := memory.NewStore(conn)
	memSvc := memory.NewService(memStore, events)
	skillSvc := skill.NewService(skill.NewStore(conn), events)

	if err := memSvc.Learn(ctx, []memory.Candidate{{
		ProjectID: "proj-1", Type: memory.Procedural, StableKey: "validate:go test ./...",
		Content: map[string]any{"command": "go test ./..."},
	}}); err != nil {
		t.Fatalf("Learn: %v", err)
	}
	cands, err := memStore.ListByState(ctx, "proj-1", memory.StateCandidate)
	if err != nil {
		t.Fatalf("ListByState(candidate): %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate memory, got %d", len(cands))
	}
	if err := memStore.Promote(ctx, cands[0].ID, 0.9); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	if _, _, err := skillSvc.Candidate(ctx, "project", "proj-1", skill.Content{Name: "run-tests"}, "task", nil); err != nil {
		t.Fatalf("skill Candidate: %v", err)
	}

	stores := viewmodel.MemoryStores{Memories: memStore, Skills: skillSvc}
	view, err := stores.MemoryBrainView(ctx, "proj-1")
	if err != nil {
		t.Fatalf("MemoryBrainView: %v", err)
	}
	if len(view.Active) != 1 || view.Active[0].StableKey != "validate:go test ./..." {
		t.Fatalf("expected the promoted memory active, got %+v", view.Active)
	}
	if len(view.Skills) != 1 || view.Skills[0].Name != "run-tests" {
		t.Fatalf("expected the candidate skill listed, got %+v", view.Skills)
	}
}

func TestMemoryBrainViewWithNilReadersIsEmpty(t *testing.T) {
	view, err := viewmodel.MemoryStores{}.MemoryBrainView(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("MemoryBrainView: %v", err)
	}
	if len(view.Active) != 0 || len(view.Skills) != 0 {
		t.Fatalf("expected empty view with nil readers, got %+v", view)
	}
}

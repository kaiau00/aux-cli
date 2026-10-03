package task_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/task"
)

func memoryCoordinator(t *testing.T) (*task.Coordinator, *memory.Service, *memory.Store) {
	t.Helper()
	conn := dbtest.New(t)
	events := eventstore.NewService(conn)
	memStore := memory.NewStore(conn)
	memSvc := memory.NewService(memStore, events)
	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/tmp/p"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc"},
	}
	coord := task.NewCoordinator(fakeResolver{res}, fakeProfiles{profile.Effective{VersionSetHash: "v"}}, task.NewStore(conn), events, "/tmp/p").
		WithMemory(memSvc)
	return coord, memSvc, memStore
}

func beginManifest(t *testing.T, coord *task.Coordinator) string {
	t.Helper()
	ctx := context.WithValue(context.Background(), tools.SessionIDContextKey, "sess-1")
	newCtx, _, err := coord.Begin(ctx, "sess-1", "add a feature")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	manifest, _ := promptcompiler.ProjectContextFromContext(newCtx)
	return manifest
}

func TestMemorySectionRendersContentNotKeys(t *testing.T) {
	coord, memSvc, memStore := memoryCoordinator(t)
	ctx := context.Background()
	src := []memory.Source{{Type: "task", ID: "task-0"}}
	if err := memSvc.Learn(ctx, []memory.Candidate{
		{ProjectID: "proj-1", Type: memory.Factual, StableKey: "fact:style", Confidence: 0.95, Sources: src,
			Content: map[string]any{"fact": "Errors are wrapped with fmt.Errorf and %w"}},
		{ProjectID: "proj-1", Type: memory.Procedural, StableKey: "validate:go test ./...", Confidence: 0.85, Sources: src,
			SupportingRevision: "0123456789abcdef",
			Content:            map[string]any{"command": "go test ./...", "purpose": "validated successfully during a task"}},
		{ProjectID: "proj-1", Type: memory.Episodic, StableKey: "episode:task-0", Confidence: 0.9, Sources: src,
			Content: map[string]any{"objective": "add Sub", "mode": "implementation", "outcome": "completed", "changedPaths": []string{"main.go"}}},
		// Not active, so it must not appear.
		{ProjectID: "proj-1", Type: memory.Factual, StableKey: "fact:stale", Confidence: 0.95, Sources: src,
			Content: map[string]any{"fact": "STALE FACT"}},
	}); err != nil {
		t.Fatalf("Learn: %v", err)
	}
	stale, err := memSvc.Retrieve(ctx, "proj-1", []memory.Type{memory.Factual}, 0)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	for _, m := range stale {
		if m.StableKey == "fact:stale" {
			if err := memStore.SetState(ctx, m.ID, memory.StateStale); err != nil {
				t.Fatalf("SetState: %v", err)
			}
		}
	}

	manifest := beginManifest(t, coord)
	for _, want := range []string{
		"Errors are wrapped with fmt.Errorf and %w",
		"`go test ./...` — validated in 1 task(s) since 0123456",
		"Earlier task: add Sub → completed (changed main.go)",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("memory section missing %q:\n%s", want, manifest)
		}
	}
	for _, key := range []string{"fact:style", "validate:", "episode:", "[factual]", "[procedural]", "[episodic]", "STALE FACT"} {
		if strings.Contains(manifest, key) {
			t.Fatalf("memory section must render content only, found %q:\n%s", key, manifest)
		}
	}
}

func TestMemorySectionRespectsTokenBudget(t *testing.T) {
	coord, memSvc, _ := memoryCoordinator(t)
	var cands []memory.Candidate
	for i := 0; i < 50; i++ {
		cands = append(cands, memory.Candidate{
			ProjectID: "proj-1", Type: memory.Factual, StableKey: fmt.Sprintf("fact:%d", i), Confidence: 0.95,
			Sources: []memory.Source{{Type: "task", ID: fmt.Sprintf("task-%d", i)}},
			Content: map[string]any{"fact": fmt.Sprintf("fact number %02d: %s", i, strings.Repeat("detail ", 12))},
		})
	}
	if err := memSvc.Learn(context.Background(), cands); err != nil {
		t.Fatalf("Learn: %v", err)
	}

	manifest := beginManifest(t, coord)
	start := strings.Index(manifest, "Prior knowledge")
	if start < 0 {
		t.Fatalf("no memory section:\n%s", manifest)
	}
	section := manifest[start:]
	if tokens := (len(section) + 3) / 4; tokens > 600 {
		t.Fatalf("memory section is ~%d tokens; budget is 600", tokens)
	}
	lines := strings.Count(section, "\n  - ")
	if lines == 0 || lines >= 50 {
		t.Fatalf("expected the budget to cut the 50 memories short, rendered %d", lines)
	}
}

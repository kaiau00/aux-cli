package viewmodel_test

import (
	"encoding/json"
	"testing"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/validation"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

func toolEvent(t *testing.T, typ eventstore.Type, tool string, latency int64, isErr bool) eventstore.Event {
	t.Helper()
	raw, err := json.Marshal(eventstore.ToolPayload{ToolName: tool, LatencyMS: latency, IsError: isErr})
	if err != nil {
		t.Fatal(err)
	}
	return eventstore.Event{Type: typ, Payload: raw}
}

func TestStateForTaskStatus(t *testing.T) {
	cases := map[task.Status]viewmodel.ComponentState{
		task.StatusRunning:   viewmodel.StateActive,
		task.StatusCompleted: viewmodel.StateCompleted,
		task.StatusFailed:    viewmodel.StateFailed,
		task.StatusCancelled: viewmodel.StateCancelled,
		task.StatusCompiled:  viewmodel.StateQueued,
	}
	for status, want := range cases {
		if got := viewmodel.StateForTaskStatus(status); got != want {
			t.Errorf("status %q -> %q, want %q", status, got, want)
		}
	}
}

func TestBuildActivityGroups(t *testing.T) {
	events := []eventstore.Event{
		toolEvent(t, eventstore.ToolCompleted, "grep", 10, false),
		toolEvent(t, eventstore.ToolCompleted, "glob", 5, false), // also searching
		toolEvent(t, eventstore.ToolCompleted, "view", 3, false), // reading
		toolEvent(t, eventstore.ToolFailed, "edit", 8, true),     // editing, error
	}
	groups := viewmodel.BuildActivityGroups(events)

	byKind := map[viewmodel.ActivityKind]viewmodel.ActivityGroupVM{}
	for _, g := range groups {
		byKind[g.Kind] = g
	}
	if byKind[viewmodel.ActivitySearching].Count != 2 {
		t.Fatalf("searching should group grep+glob: %+v", byKind[viewmodel.ActivitySearching])
	}
	if byKind[viewmodel.ActivityReading].Count != 1 {
		t.Fatalf("reading should have view")
	}
	editing := byKind[viewmodel.ActivityEditing]
	if editing.Errors != 1 || editing.State != viewmodel.StateFailed {
		t.Fatalf("editing group should record the failure: %+v", editing)
	}
}

func TestStageFromEvents(t *testing.T) {
	events := []eventstore.Event{
		{Type: eventstore.TaskCompiled},
		{Type: eventstore.ModelCallStarted},
		toolEvent(t, eventstore.ToolStarted, "edit", 0, false),
	}
	if stage := viewmodel.StageFromEvents(events); stage != string(viewmodel.ActivityEditing) {
		t.Fatalf("stage should reflect the latest tool activity, got %q", stage)
	}
}

func TestBuildValidationSummary(t *testing.T) {
	// All validated -> overall validated.
	all := viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{
		{Description: "a", State: validation.Validated},
		{Description: "b", State: validation.Validated},
	})
	if all.Overall != viewmodel.StateValidated || all.Validated != 2 {
		t.Fatalf("all-validated summary wrong: %+v", all)
	}
	// A blocked criterion dominates.
	blocked := viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{
		{Description: "a", State: validation.Validated},
		{Description: "b", State: validation.Blocked},
	})
	if blocked.Overall != viewmodel.StateBlocked {
		t.Fatalf("blocked should dominate overall")
	}
	// Uncovered -> unverified, never validated.
	uncovered := viewmodel.BuildValidationSummary([]viewmodel.CriterionInput{
		{Description: "a", State: validation.Uncovered},
	})
	if uncovered.Overall != viewmodel.StateUnverified {
		t.Fatalf("uncovered must be unverified, not validated")
	}
	// No criteria -> unverified (never implies success).
	if viewmodel.BuildValidationSummary(nil).Overall != viewmodel.StateUnverified {
		t.Fatalf("no criteria must be unverified")
	}
}

func TestBuildChangeSummary(t *testing.T) {
	vm := viewmodel.BuildChangeSummary([]checkpoint.Entry{
		{Path: "a.go", Operation: checkpoint.OpAdd},
		{Path: "b.go", Operation: checkpoint.OpModify},
		{Path: "c.go", Operation: checkpoint.OpDelete},
	})
	if vm.Added != 1 || vm.Modified != 1 || vm.Deleted != 1 || len(vm.Files) != 3 {
		t.Fatalf("change summary counts wrong: %+v", vm)
	}
}

func TestBuildContextBudget(t *testing.T) {
	bindings := []contextstore.BoundPage{
		{Binding: contextstore.Binding{State: contextstore.StateResident, TokenCount: 100}, PageType: contextstore.KindRecentConversation},
		{Binding: contextstore.Binding{State: contextstore.StateResident, TokenCount: 50}, PageType: contextstore.KindFileRegion},
		{Binding: contextstore.Binding{State: contextstore.StateAvailable, TokenCount: 999}, PageType: contextstore.KindProjectManifest},
	}
	vm := viewmodel.BuildContextBudget(bindings, 64000, 20)
	if vm.TotalTokens != 150 {
		t.Fatalf("only resident pages count: got %d, want 150", vm.TotalTokens)
	}
	if vm.ResidentPages != 2 || vm.AvailablePages != 1 {
		t.Fatalf("page counts wrong: %+v", vm)
	}
	if vm.CallTotalTokens != 64000 || vm.SavedTokens != 20 {
		t.Fatalf("limit/saved not carried: %+v", vm)
	}
	// Categories reflect page types.
	labels := map[string]int64{}
	for _, c := range vm.Categories {
		labels[c.Label] = c.Tokens
	}
	if labels["Recent conversation"] != 100 || labels["Active code"] != 50 {
		t.Fatalf("category composition wrong: %+v", vm.Categories)
	}
}

func TestBuildContextPageListGroupsByState(t *testing.T) {
	bindings := []contextstore.BoundPage{
		{Binding: contextstore.Binding{State: contextstore.StateResident, TokenCount: 100, Reason: "current"}, PageType: contextstore.KindFileRegion, StableKey: "file:/a.go"},
		{Binding: contextstore.Binding{State: contextstore.StateAvailable, TokenCount: 50}, PageType: contextstore.KindProjectManifest, StableKey: "project_manifest"},
		{Binding: contextstore.Binding{State: contextstore.StatePinned, TokenCount: 20}, PageType: contextstore.KindTaskSpec, StableKey: "task_spec:1"},
		{Binding: contextstore.Binding{State: contextstore.StateEvicted, TokenCount: 10, Reason: "excluded by user"}, PageType: contextstore.KindFileRegion, StableKey: "file:/b.go"},
		{Binding: contextstore.Binding{State: contextstore.StateFaulted, TokenCount: 5}, PageType: contextstore.KindFileRegion, StableKey: "file:/c.go"},
	}
	vm := viewmodel.BuildContextPageList(bindings)
	if len(vm.Resident) != 1 || vm.Resident[0].StableKey != "file:/a.go" {
		t.Fatalf("resident group wrong: %+v", vm.Resident)
	}
	if len(vm.Available) != 1 || vm.Available[0].StableKey != "project_manifest" {
		t.Fatalf("available group wrong: %+v", vm.Available)
	}
	if len(vm.Pinned) != 1 || vm.Pinned[0].StableKey != "task_spec:1" {
		t.Fatalf("pinned group wrong: %+v", vm.Pinned)
	}
	if len(vm.Evicted) != 1 || vm.Evicted[0].Reason != "excluded by user" {
		t.Fatalf("evicted group wrong: %+v", vm.Evicted)
	}
	if len(vm.Faulted) != 1 || vm.Faulted[0].StableKey != "file:/c.go" {
		t.Fatalf("faulted group wrong: %+v", vm.Faulted)
	}
}

package viewmodel_test

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/viewmodel"
)

// A binding whose state the projection does not handle disappears silently on
// the way to the screen: the page was in the prompt, and the expanded context
// view simply would not mention it. Every state contextstore declares must land
// in a group.
func TestContextPageListCoversEveryDeclaredState(t *testing.T) {
	states := []string{
		contextstore.StateResident,
		contextstore.StateAvailable,
		contextstore.StatePinned,
		contextstore.StateEvicted,
		contextstore.StateFaulted,
	}

	for _, state := range states {
		vm := viewmodel.BuildContextPageList([]contextstore.BoundPage{
			{Binding: contextstore.Binding{State: state, TokenCount: 100}, PageType: "file_region", StableKey: "file:main.go"},
		})
		total := len(vm.Resident) + len(vm.Available) + len(vm.Pinned) + len(vm.Evicted) + len(vm.Faulted)
		if total != 1 {
			t.Errorf("state %q produced %d grouped entries, want 1: the page vanished between the store and the view", state, total)
		}
	}
}

// An unrecognised state must not be silently swallowed either -- but there is
// nothing sensible to do with it, so this records the current behaviour so a
// future state addition is a deliberate choice rather than a surprise.
func TestContextPageListDropsUnknownStates(t *testing.T) {
	vm := viewmodel.BuildContextPageList([]contextstore.BoundPage{
		{Binding: contextstore.Binding{State: "invented", TokenCount: 100}, PageType: "file_region", StableKey: "file:main.go"},
	})
	total := len(vm.Resident) + len(vm.Available) + len(vm.Pinned) + len(vm.Evicted) + len(vm.Faulted)
	if total != 0 {
		t.Errorf("an undeclared state was grouped anyway (%d entries); add it to contextstore first", total)
	}
}

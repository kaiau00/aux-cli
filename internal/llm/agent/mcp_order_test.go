package agent

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
)

// Tool order is load-bearing: providers cache on an exact prefix match of the
// serialized tool block, so a set of servers that iterates in a different order
// between process starts throws away the cache on every restart. Map iteration
// in Go is deliberately randomized, so this needs a real guarantee rather than
// an incidental one.
func TestServerNamesAreSortedAndStable(t *testing.T) {
	servers := map[string]config.MCPServer{
		"zeta":     {},
		"alpha":    {},
		"mid":      {},
		"beta":     {},
		"omega":    {},
		"codebase": {},
	}

	want := []string{"alpha", "beta", "codebase", "mid", "omega", "zeta"}

	// Repeat enough times that randomized map iteration would almost certainly
	// have produced a different order at least once if we were not sorting.
	for i := 0; i < 50; i++ {
		got := serverNames(servers)
		if len(got) != len(want) {
			t.Fatalf("iteration %d: got %d names, want %d", i, len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: order %v, want %v", i, got, want)
			}
		}
	}
}

func TestServerNamesHandlesEmptyAndNil(t *testing.T) {
	if got := serverNames(nil); len(got) != 0 {
		t.Fatalf("nil server map should yield no names, got %v", got)
	}
	if got := serverNames(map[string]config.MCPServer{}); len(got) != 0 {
		t.Fatalf("empty server map should yield no names, got %v", got)
	}
}

// GetMcpTools runs during agent construction, which some commands do before
// config.Load. It must not panic on a nil config.
func TestGetMcpToolsWithoutConfigDoesNotPanic(t *testing.T) {
	if config.Get() != nil {
		t.Skip("config already loaded in this test binary")
	}
	mcpToolsMu.Lock()
	mcpTools = nil
	mcpToolsMu.Unlock()

	if got := GetMcpTools(t.Context(), nil); got != nil {
		t.Fatalf("expected no tools without config, got %d", len(got))
	}
}

package floorplan

import "testing"

// areas used across the assignment tests: a top-level area that hands tests to
// a sibling, a child that claims a subtree of its parent, and a test area.
func testAreas() []Area {
	return []Area{
		{ID: "cli", Label: "Command line", Kind: KindUI, Paths: []string{"main.go", "cmd/**"}, Exclude: []string{"**/*_test.go"}},
		{ID: "cli.schema", Parent: "cli", Label: "Schema generator", Kind: KindScript, Paths: []string{"cmd/schema/**"}},
		{ID: "agent", Label: "Agent", Kind: KindService, Paths: []string{"internal/llm/**"}, Exclude: []string{"**/*_test.go"}},
		{ID: "agent.tools", Parent: "agent", Label: "Tools", Kind: KindFeature, Paths: []string{"internal/llm/tools/**"}},
		{ID: "tests", Label: "Tests", Kind: KindTest, Paths: []string{"**/*_test.go"}},
	}
}

func TestAssignPrefersTheDeepestArea(t *testing.T) {
	a, err := NewAssigner(testAreas())
	if err != nil {
		t.Fatalf("NewAssigner: %v", err)
	}
	cases := map[string]string{
		"main.go":                        "cli",
		"cmd/root.go":                    "cli",
		"cmd/schema/main.go":             "cli.schema", // child beats parent
		"internal/llm/agent/agent.go":    "agent",
		"internal/llm/tools/bash.go":     "agent.tools", // child beats parent
		"internal/llm/tools/nested/x.go": "agent.tools",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := a.Assign(path); got != want {
				t.Fatalf("Assign(%q) = %q; want %q", path, got, want)
			}
		})
	}
}

// An exclude on a top-level area is how a map hands every test file to a
// dedicated area. It has to reach the area's descendants too, or a child's
// broad glob silently claims the tests its parent just released.
func TestExcludeIsInheritedByDescendants(t *testing.T) {
	a, err := NewAssigner(testAreas())
	if err != nil {
		t.Fatalf("NewAssigner: %v", err)
	}
	cases := map[string]string{
		"cmd/root_test.go":                 "tests",
		"cmd/schema/main_test.go":          "tests", // cli.schema inherits cli's exclude
		"internal/llm/tools/bash_test.go":  "tests", // agent.tools inherits agent's exclude
		"internal/llm/agent/agent_test.go": "tests",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := a.Assign(path); got != want {
				t.Fatalf("Assign(%q) = %q; want %q", path, got, want)
			}
		})
	}
}

// Two areas at the same depth can both match. The more specific glob -- the one
// with the longer literal prefix before any wildcard -- is the one that meant
// this path, so it wins regardless of declaration order.
func TestSameDepthPrefersTheLongerLiteralPrefix(t *testing.T) {
	areas := []Area{
		{ID: "broad", Label: "Broad", Kind: KindMisc, Paths: []string{"internal/**"}},
		{ID: "narrow", Label: "Narrow", Kind: KindData, Paths: []string{"internal/db/**"}},
	}
	for _, order := range [][]Area{areas, {areas[1], areas[0]}} {
		a, err := NewAssigner(order)
		if err != nil {
			t.Fatalf("NewAssigner: %v", err)
		}
		if got := a.Assign("internal/db/models.go"); got != "narrow" {
			t.Fatalf("Assign = %q; want narrow (declaration order must not matter)", got)
		}
		if got := a.Assign("internal/other/x.go"); got != "broad" {
			t.Fatalf("Assign = %q; want broad", got)
		}
	}
}

// A path no area claims must come back empty. Guessing an owner would put a
// file in a room it was never placed in, and the unassigned count is the
// signal that the map is incomplete.
func TestUnclaimedPathIsUnassigned(t *testing.T) {
	a, err := NewAssigner(testAreas())
	if err != nil {
		t.Fatalf("NewAssigner: %v", err)
	}
	if got := a.Assign("docs/shipping-plan.md"); got != "" {
		t.Fatalf("Assign(docs/...) = %q; want \"\"", got)
	}
}

// A parent id that does not exist would otherwise be silently treated as a
// root, so the area's depth -- and therefore which area wins -- would be wrong.
func TestNewAssignerRejectsAMissingParent(t *testing.T) {
	_, err := NewAssigner([]Area{{ID: "child", Parent: "nope", Label: "Child", Kind: KindMisc, Paths: []string{"**"}}})
	if err == nil {
		t.Fatal("NewAssigner accepted an area whose parent does not exist")
	}
}

// A parent cycle would make depth computation loop forever.
func TestNewAssignerRejectsACycle(t *testing.T) {
	_, err := NewAssigner([]Area{
		{ID: "a", Parent: "b", Label: "A", Kind: KindMisc},
		{ID: "b", Parent: "a", Label: "B", Kind: KindMisc},
	})
	if err == nil {
		t.Fatal("NewAssigner accepted a parent cycle")
	}
}

// Duplicate ids make every lookup ambiguous.
func TestNewAssignerRejectsDuplicateIDs(t *testing.T) {
	_, err := NewAssigner([]Area{
		{ID: "dup", Label: "One", Kind: KindMisc},
		{ID: "dup", Label: "Two", Kind: KindMisc},
	})
	if err == nil {
		t.Fatal("NewAssigner accepted a duplicate area id")
	}
}

// A glob the matcher cannot parse must be reported, not quietly dropped: a
// dropped glob means files land in the wrong area with no sign anything failed.
func TestNewAssignerRejectsAnInvalidGlob(t *testing.T) {
	_, err := NewAssigner([]Area{{ID: "bad", Label: "Bad", Kind: KindMisc, Paths: []string{"internal/[a-"}}})
	if err == nil {
		t.Fatal("NewAssigner accepted an unparsable glob")
	}
}

func TestLiteralPrefixLen(t *testing.T) {
	cases := map[string]int{
		"internal/**":       len("internal/"),
		"internal/db/**":    len("internal/db/"),
		"main.go":           len("main.go"),
		"**/*_test.go":      0,
		"cmd/schema/**":     len("cmd/schema/"),
		"internal/?/x.go":   len("internal/"),
		"internal/{a,b}/**": len("internal/"),
	}
	for glob, want := range cases {
		if got := literalPrefixLen(glob); got != want {
			t.Errorf("literalPrefixLen(%q) = %d; want %d", glob, got, want)
		}
	}
}

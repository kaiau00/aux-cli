package promptcompiler_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/promptcompiler"
)

const (
	testManifest = "Languages: go\nBuild: go build ./...\nPrior knowledge:\n  - go test ./... passed\n"
	testSpec     = "Task (implementation): add Sub\nAcceptance: tests pass"
)

func compilers() map[string]promptcompiler.Compiler {
	return map[string]promptcompiler.Compiler{
		"compat": promptcompiler.NewCompatibilityCompiler(),
		"dedup":  promptcompiler.NewDedupCompiler(),
	}
}

func TestSystemAddendumCarriesProjectThenTask(t *testing.T) {
	for name, c := range compilers() {
		t.Run(name, func(t *testing.T) {
			out := c.Compile(promptcompiler.Input{History: history(), ProjectManifest: testManifest, TaskSpecText: testSpec})
			a := out.SystemAddendum
			project, task := strings.Index(a, "# Project"), strings.Index(a, "# Task")
			if project != 0 || task < 0 {
				t.Fatalf("addendum must start with # Project and contain # Task:\n%s", a)
			}
			if !strings.Contains(a[:task], "Build: go build ./...") || !strings.Contains(a[task:], "Acceptance: tests pass") {
				t.Fatalf("manifest must sit under # Project and spec under # Task:\n%s", a)
			}
		})
	}
}

func TestSystemAddendumIsCountedInTheEstimate(t *testing.T) {
	for name, c := range compilers() {
		t.Run(name, func(t *testing.T) {
			bare := c.Compile(promptcompiler.Input{History: history()})
			with := c.Compile(promptcompiler.Input{History: history(), ProjectManifest: testManifest, TaskSpecText: testSpec})
			if bare.SystemAddendum != "" {
				t.Fatalf("no project context should mean no addendum, got %q", bare.SystemAddendum)
			}
			grew := with.EstimatedTokens - bare.EstimatedTokens
			want := int64(len(with.SystemAddendum)+3) / 4
			if grew != want {
				t.Fatalf("EstimatedTokens grew by %d; want the addendum's estimate %d", grew, want)
			}
			if with.Manifest.TokenEstimate != with.EstimatedTokens {
				t.Fatalf("manifest estimate %d != EstimatedTokens %d", with.Manifest.TokenEstimate, with.EstimatedTokens)
			}
		})
	}
}

// The addendum follows the cached base system prompt, so a byte that differs
// between identical compiles would defeat the provider's cache for no reason.
func TestSystemAddendumIsDeterministic(t *testing.T) {
	for name, c := range compilers() {
		t.Run(name, func(t *testing.T) {
			in := promptcompiler.Input{History: history(), ProjectManifest: testManifest, TaskSpecText: testSpec}
			first := c.Compile(in).SystemAddendum
			for i := 0; i < 20; i++ {
				if got := c.Compile(in).SystemAddendum; got != first {
					t.Fatalf("compile %d produced a different addendum", i)
				}
			}
		})
	}
}

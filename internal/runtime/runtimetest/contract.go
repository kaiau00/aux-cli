// Package runtimetest provides the reusable conformance contract every runtime
// adapter must satisfy (roadmapplan.md §12.4, §24 platform quality). A new
// adapter (local, remote, replay) proves it honors the Runner seam by calling
// RunnerContract from its own test — so adapters are contract tested rather than
// trusted.
package runtimetest

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/runtime"
)

// RunnerContract exercises the invariants every runtime.Runner must uphold.
// newRunner returns a fresh runner for each check so state does not leak between
// cases. name labels failures.
func RunnerContract(t *testing.T, name string, newRunner func() runtime.Runner) {
	t.Helper()

	t.Run(name+"/empty history is handled", func(t *testing.T) {
		r := newRunner()
		// Must not panic and must return a result or an error for nil history.
		_, err := r.RunTurn(context.Background(), "sess", nil)
		_ = err // either outcome is acceptable; the contract is "no panic / returns".
	})

	t.Run(name+"/cancelled context returns an error", func(t *testing.T) {
		r := newRunner()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.RunTurn(ctx, "sess", nil); err == nil {
			t.Fatalf("%s: a cancelled context must yield a non-nil error, not a successful turn", name)
		}
	})

	t.Run(name+"/sequential calls make progress", func(t *testing.T) {
		r := newRunner()
		hist := []message.Message{{Role: message.User}}
		for i := 0; i < 3; i++ {
			if _, err := r.RunTurn(context.Background(), "sess", hist); err != nil {
				t.Fatalf("%s: call %d returned error: %v", name, i, err)
			}
		}
	})
}

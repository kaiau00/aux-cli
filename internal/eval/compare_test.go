package eval_test

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/eval"
)

func resultFor(results []eval.CompilerResult, name string) (eval.CompilerResult, bool) {
	for _, r := range results {
		if r.Fixture == name {
			return r, true
		}
	}
	return eval.CompilerResult{}, false
}

func TestBaselineComparisonMeasuresHypothesis(t *testing.T) {
	results := eval.RunBaseline()
	if len(results) != 3 {
		t.Fatalf("expected 3 baseline results, got %d", len(results))
	}

	// Every variant must be lossless.
	for _, r := range results {
		if !r.OutcomePreserved {
			t.Fatalf("fixture %q lost content under paging", r.Fixture)
		}
	}

	// Repeated-read: paging must save tokens (the central hypothesis).
	rr, ok := resultFor(results, "repeated-read")
	if !ok {
		t.Fatalf("missing repeated-read fixture")
	}
	if rr.SavedTokens <= 0 || rr.SavedPercent <= 0 {
		t.Fatalf("paging should reduce uncached input on repeated reads, saved=%d", rr.SavedTokens)
	}

	// Localized and cross-file (no duplicates): paging is parity (no change).
	for _, name := range []string{"localized-edit", "cross-file"} {
		r, _ := resultFor(results, name)
		if r.SavedTokens != 0 {
			t.Fatalf("fixture %q should be unchanged by paging, saved=%d", name, r.SavedTokens)
		}
		if r.ControlTokens != r.VariantTokens {
			t.Fatalf("fixture %q control/variant tokens should match", name)
		}
	}
}

func TestRenderReportSupportsHypothesis(t *testing.T) {
	report := eval.RenderReport(eval.RunBaseline())
	if !strings.Contains(report, "repeated-read") {
		t.Fatalf("report should list the repeated-read fixture")
	}
	if !strings.Contains(report, "supported") {
		t.Fatalf("report should conclude the hypothesis is supported, got:\n%s", report)
	}
}

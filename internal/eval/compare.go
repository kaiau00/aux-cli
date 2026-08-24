package eval

import (
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
)

// CompilerResult is the measured comparison of two compilers on one fixture.
type CompilerResult struct {
	Fixture          string
	Kind             Kind
	ControlTokens    int64
	VariantTokens    int64
	SavedTokens      int64
	SavedPercent     float64
	OutcomePreserved bool
}

// CompareCompilers compiles a fixture with a control and a variant compiler using
// the same input and reports the token difference and whether the variant is
// lossless (every distinct large content in the control prompt still appears in
// the variant prompt). This is a counterfactual context replay — no mutations,
// no provider calls.
func CompareCompilers(f Fixture, control, variant promptcompiler.Compiler) CompilerResult {
	in := promptcompiler.Input{TaskID: f.Name, History: f.History}
	c := control.Compile(in)
	v := variant.Compile(in)

	saved := c.EstimatedTokens - v.EstimatedTokens
	if saved < 0 {
		saved = 0
	}
	var pct float64
	if c.EstimatedTokens > 0 {
		pct = float64(saved) / float64(c.EstimatedTokens) * 100
	}

	return CompilerResult{
		Fixture:          f.Name,
		Kind:             f.Kind,
		ControlTokens:    c.EstimatedTokens,
		VariantTokens:    v.EstimatedTokens,
		SavedTokens:      saved,
		SavedPercent:     pct,
		OutcomePreserved: preservesContent(c.Messages, v.Messages),
	}
}

// preservesContent reports whether every distinct large tool-result content in
// control still appears at least once in variant (lossless check).
func preservesContent(control, variant []message.Message) bool {
	const minSize = 200
	variantContents := map[string]struct{}{}
	for _, m := range variant {
		for _, part := range m.Parts {
			if tr, ok := part.(message.ToolResult); ok {
				variantContents[tr.Content] = struct{}{}
			}
		}
	}
	for _, m := range control {
		for _, part := range m.Parts {
			tr, ok := part.(message.ToolResult)
			if !ok || len(tr.Content) < minSize {
				continue
			}
			if _, present := variantContents[tr.Content]; !present {
				return false
			}
		}
	}
	return true
}

// RunBaseline compares the compatibility and paging compilers across all baseline
// fixtures — the central known-project compilation hypothesis measurement.
func RunBaseline() []CompilerResult {
	control := promptcompiler.NewCompatibilityCompiler()
	variant := promptcompiler.NewDedupCompiler()
	var out []CompilerResult
	for _, f := range BaselineFixtures() {
		out = append(out, CompareCompilers(f, control, variant))
	}
	return out
}

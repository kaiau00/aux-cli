package eval

import (
	"fmt"
	"strings"
)

// RenderReport produces a human-readable comparison report of control vs variant
// prompt compilation across fixtures. This is the recorded evidence the plan
// requires before tuning any default.
func RenderReport(results []CompilerResult) string {
	var b strings.Builder
	b.WriteString("Prompt compiler comparison: compatibility (control) vs paging (variant)\n")
	b.WriteString("Same recorded fixtures, no provider calls.\n\n")
	b.WriteString(fmt.Sprintf("%-16s %-14s %10s %10s %10s %8s  %s\n",
		"FIXTURE", "KIND", "CONTROL", "VARIANT", "SAVED", "SAVED%", "LOSSLESS"))
	b.WriteString(strings.Repeat("-", 86) + "\n")

	var totalControl, totalVariant, totalSaved int64
	allLossless := true
	for _, r := range results {
		lossless := "yes"
		if !r.OutcomePreserved {
			lossless = "NO"
			allLossless = false
		}
		b.WriteString(fmt.Sprintf("%-16s %-14s %10d %10d %10d %7.1f%%  %s\n",
			r.Fixture, r.Kind, r.ControlTokens, r.VariantTokens, r.SavedTokens, r.SavedPercent, lossless))
		totalControl += r.ControlTokens
		totalVariant += r.VariantTokens
		totalSaved += r.SavedTokens
	}
	b.WriteString(strings.Repeat("-", 86) + "\n")
	var pct float64
	if totalControl > 0 {
		pct = float64(totalSaved) / float64(totalControl) * 100
	}
	b.WriteString(fmt.Sprintf("%-16s %-14s %10d %10d %10d %7.1f%%  %s\n",
		"TOTAL", "", totalControl, totalVariant, totalSaved, pct, boolLabel(allLossless)))

	b.WriteString("\nHypothesis: demand paging reduces uncached input on repeated-read tasks ")
	b.WriteString("without dropping content. Result: ")
	if totalSaved > 0 && allLossless {
		b.WriteString("supported.\n")
	} else if totalSaved == 0 {
		b.WriteString("no measurable difference on these fixtures.\n")
	} else {
		b.WriteString("REJECTED — content loss detected; do not enable paging by default.\n")
	}
	return b.String()
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return "NO"
}

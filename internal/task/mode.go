package task

import "strings"

// modeSignals maps a task mode to lowercase trigger terms. Ordering in InferMode
// resolves ties deterministically.
var modeSignals = []struct {
	mode  Mode
	terms []string
}{
	{ModeBugDiagnosis, []string{"bug", "fix", "broken", "error", "crash", "panic", "regression", "failing", "debug", "why is", "not working"}},
	{ModeTestAuthoring, []string{"test", "unit test", "coverage", "write tests", "add tests", "table-driven"}},
	{ModeRefactor, []string{"refactor", "rename", "clean up", "cleanup", "restructure", "extract", "simplify", "deduplicate"}},
	{ModeCodeReview, []string{"review", "audit", "critique", "assess the code", "code review"}},
	{ModeMaintenance, []string{"upgrade", "update dependency", "bump", "dependency", "migrate to", "deprecat"}},
	{ModeResearch, []string{"explain", "how does", "what is", "understand", "research", "investigate", "describe", "summarize", "where is"}},
	{ModeImplementation, []string{"add", "implement", "create", "build", "support", "introduce", "feature", "endpoint", "write a"}},
}

// InferMode deterministically classifies a request. It scores each mode by the
// number of matched trigger terms and returns the best; implementation is the
// default when nothing matches. The primary model may
// still refine ambiguity within its normal call.
func InferMode(objective string) Mode {
	lower := strings.ToLower(objective)
	best := ModeImplementation
	bestScore := 0
	for _, sig := range modeSignals {
		score := 0
		for _, term := range sig.terms {
			if strings.Contains(lower, term) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = sig.mode
		}
	}
	return best
}

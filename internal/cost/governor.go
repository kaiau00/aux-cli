package cost

import "fmt"

// GovernorMode gates the governor (off / observe / on).
type GovernorMode string

const (
	GovOff     GovernorMode = "off"
	GovObserve GovernorMode = "observe"
	GovOn      GovernorMode = "on"
)

// Decision is a governor recommendation.
type Decision struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// Assessment is the governor's read on a task's budget and efficiency. In
// observe mode it is advisory; in on mode the caller may enforce it. The
// governor never swaps API keys or changes the model.
type Assessment struct {
	Enabled         bool       `json:"enabled"`
	Pressure        Pressure   `json:"pressure"`
	Exhausted       bool       `json:"exhausted"`
	Warnings        []Warning  `json:"warnings"`
	Decisions       []Decision `json:"decisions"`
	DegradationPlan []string   `json:"degradationPlan,omitempty"`
}

// Governor produces budget/efficiency assessments.
type Governor struct {
	mode GovernorMode
}

// NewGovernor returns a governor. An empty or unknown mode is treated as off.
func NewGovernor(mode GovernorMode) *Governor {
	switch mode {
	case GovObserve, GovOn:
		return &Governor{mode: mode}
	default:
		return &Governor{mode: GovOff}
	}
}

// Mode returns the governor's mode.
func (g *Governor) Mode() GovernorMode { return g.mode }

// warnThreshold is the pressure at which the governor advises action.
const warnThreshold = 0.8

// Assess evaluates a task's budget usage and waste warnings. Off mode returns a
// disabled, empty assessment (no computation, no side effects).
func (g *Governor) Assess(budget Budget, usage Usage, waste []Warning) Assessment {
	if g.mode == GovOff {
		return Assessment{Enabled: false}
	}
	p := budget.Pressure(usage)
	a := Assessment{Enabled: true, Pressure: p, Exhausted: p.Max >= 1.0, Warnings: waste}

	if p.Max >= warnThreshold && !a.Exhausted {
		a.Decisions = append(a.Decisions, Decision{
			Action: "warn_budget",
			Reason: fmt.Sprintf("budget pressure at %.0f%%; prefer known profile commands and stop low-yield search", p.Max*100),
		})
	}
	if a.Exhausted {
		a.Decisions = append(a.Decisions, Decision{
			Action: "degrade",
			Reason: "budget exhausted; degrade in a planned way without corrupting protocol state",
		})
		// Planned degradation order.
		a.DegradationPlan = []string{
			"compress or evict low-value context pages",
			"stop redundant exploration",
			"choose focused (targeted) validation only",
			"reserve remaining budget for repair and final response",
			"clearly report any uncovered acceptance criteria",
		}
	}
	// Waste warnings become efficiency decisions.
	for _, w := range waste {
		switch w.Detector {
		case "repeated_tool_call":
			a.Decisions = append(a.Decisions, Decision{Action: "avoid_repeat", Reason: w.Detail})
		case "repeated_validation":
			a.Decisions = append(a.Decisions, Decision{Action: "reuse_validation_cache", Reason: w.Detail})
		}
	}
	return a
}

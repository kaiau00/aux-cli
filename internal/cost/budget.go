package cost

// Budget model for the One-Key Cost Governor. The governor
// allocates a task's cost/token/time across categories and drives planned
// degradation when a budget is exhausted — it never swaps API keys or changes
// the user's chosen model.

// Mode is a user-facing budget mode.
type Mode string

const (
	ModeEfficient Mode = "efficient"
	ModeBalanced  Mode = "balanced"
	ModeMaximum   Mode = "maximum"
	ModeCapped    Mode = "capped"
	ModeLocal     Mode = "local"
)

// Budget categories the governor allocates across.
const (
	CatProfileContext = "profile_context"
	CatDiscovery      = "discovery"
	CatToolSchemas    = "tool_schemas"
	CatImplementation = "implementation"
	CatRecovery       = "recovery"
	CatValidation     = "validation"
	CatFinal          = "final"
)

// Budget is a task's overall budget plus per-category token allocations. A zero
// ceiling means unbounded for that dimension.
type Budget struct {
	Mode            Mode
	MaxCost         float64
	MaxInputTokens  int64
	MaxOutputTokens int64
	MaxWallMS       int64
	MaxToolCalls    int64
	// Allocations distributes MaxInputTokens across categories (fractions of the
	// input budget). They are advisory guides for the governor, not hard walls.
	Allocations map[string]int64
}

// DefaultBudget returns a budget for a mode with sensible category allocations.
func DefaultBudget(mode Mode) Budget {
	b := Budget{Mode: mode, MaxInputTokens: 120_000, MaxOutputTokens: 16_000, MaxToolCalls: 60, MaxWallMS: 600_000}
	switch mode {
	case ModeEfficient:
		b.MaxInputTokens = 60_000
		b.MaxToolCalls = 30
	case ModeMaximum:
		b.MaxInputTokens = 200_000
		b.MaxToolCalls = 120
	case ModeLocal:
		b.MaxCost = 0 // no monetary ceiling, but still controls waste/latency
	}
	b.Allocations = allocate(b.MaxInputTokens, mode)
	return b
}

// allocate splits the input-token budget across categories. Fractions sum to 1.
func allocate(maxInput int64, mode Mode) map[string]int64 {
	fractions := map[string]float64{
		CatProfileContext: 0.10,
		CatDiscovery:      0.25,
		CatToolSchemas:    0.05,
		CatImplementation: 0.40,
		CatRecovery:       0.10,
		CatValidation:     0.05,
		CatFinal:          0.05,
	}
	if mode == ModeEfficient {
		// Efficient favours implementation over open-ended discovery.
		fractions[CatDiscovery] = 0.15
		fractions[CatImplementation] = 0.50
	}
	out := make(map[string]int64, len(fractions))
	for cat, f := range fractions {
		out[cat] = int64(float64(maxInput) * f)
	}
	return out
}

// Usage is measured consumption against a budget.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	Cost         float64
	WallMS       int64
	ToolCalls    int64
}

// Pressure summarises how close usage is to each ceiling, in [0,1] (0 when a
// ceiling is unbounded).
type Pressure struct {
	Cost   float64
	Input  float64
	Output float64
	Tools  float64
	Wall   float64
	// Max is the highest single-dimension pressure.
	Max float64
}

// Pressure computes budget pressure from usage.
func (b Budget) Pressure(u Usage) Pressure {
	p := Pressure{
		Cost:   ratio(u.Cost, b.MaxCost),
		Input:  ratio(float64(u.InputTokens), float64(b.MaxInputTokens)),
		Output: ratio(float64(u.OutputTokens), float64(b.MaxOutputTokens)),
		Tools:  ratio(float64(u.ToolCalls), float64(b.MaxToolCalls)),
		Wall:   ratio(float64(u.WallMS), float64(b.MaxWallMS)),
	}
	p.Max = max5(p.Cost, p.Input, p.Output, p.Tools, p.Wall)
	return p
}

// Exhausted reports whether any hard ceiling has been reached.
func (b Budget) Exhausted(u Usage) bool {
	return b.Pressure(u).Max >= 1.0
}

func ratio(used, ceiling float64) float64 {
	if ceiling <= 0 {
		return 0
	}
	r := used / ceiling
	if r < 0 {
		return 0
	}
	return r
}

func max5(a, b, c, d, e float64) float64 {
	m := a
	for _, v := range []float64{b, c, d, e} {
		if v > m {
			m = v
		}
	}
	return m
}

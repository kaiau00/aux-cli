package cost

import (
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
)

func TestComputeCost(t *testing.T) {
	priced := models.Model{
		Provider:           models.ProviderAnthropic,
		CostPer1MIn:        3.0,
		CostPer1MOut:       15.0,
		CostPer1MInCached:  3.75,
		CostPer1MOutCached: 0.30,
	}
	tests := []struct {
		name      string
		model     models.Model
		usage     provider.TokenUsage
		wantCost  float64
		wantState CostState
	}{
		{
			name:  "no cache metrics",
			model: priced,
			usage: provider.TokenUsage{InputTokens: 1_000_000, OutputTokens: 1_000_000},
			// 3.0 + 15.0
			wantCost:  18.0,
			wantState: CostKnown,
		},
		{
			name:  "with cache metrics kept separate",
			model: priced,
			usage: provider.TokenUsage{
				InputTokens:         1_000_000,
				OutputTokens:        1_000_000,
				CacheCreationTokens: 1_000_000,
				CacheReadTokens:     1_000_000,
			},
			// 3.75 (cache create) + 0.30 (cache read) + 3.0 (in) + 15.0 (out)
			wantCost:  22.05,
			wantState: CostKnown,
		},
		{
			name:      "local model zero rates is known-free",
			model:     models.Model{Provider: models.ProviderLocal},
			usage:     provider.TokenUsage{InputTokens: 5_000, OutputTokens: 5_000},
			wantCost:  0,
			wantState: CostKnown,
		},
		{
			name:      "unknown pricing for non-local zero-rate model",
			model:     models.Model{Provider: models.ProviderOpenAI},
			usage:     provider.TokenUsage{InputTokens: 5_000, OutputTokens: 5_000},
			wantCost:  0,
			wantState: CostUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCost, gotState := ComputeCost(tt.model, tt.usage)
			if !almostEqual(gotCost, tt.wantCost) {
				t.Errorf("cost = %v, want %v", gotCost, tt.wantCost)
			}
			if gotState != tt.wantState {
				t.Errorf("state = %v, want %v", gotState, tt.wantState)
			}
		})
	}
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	return d < eps && d > -eps
}

// Cached input bills at a fraction of the fresh rate (Anthropic: $0.30 vs
// $3.00 per 1M, a factor of ten). Until the streaming usage fix, cache reads
// were always recorded as zero and the whole prompt was priced as fresh input,
// so a warmed session's cost was overstated several-fold -- and the governor,
// which pauses the agent at a budget ceiling, was measuring against that
// inflated figure.
func TestCachedTokensAreBilledAtTheCacheRate(t *testing.T) {
	model := models.Model{
		CostPer1MIn:        3.00,
		CostPer1MInCached:  3.75,
		CostPer1MOutCached: 0.30,
		CostPer1MOut:       15.00,
	}

	const prompt = 15_000
	cached := provider.TokenUsage{InputTokens: 150, CacheReadTokens: prompt - 150, OutputTokens: 100}
	asFresh := provider.TokenUsage{InputTokens: prompt, OutputTokens: 100}

	cachedCost, state := ComputeCost(model, cached)
	freshCost, _ := ComputeCost(model, asFresh)

	if state == CostUnknown {
		t.Fatal("a model with rates must produce a known cost")
	}
	if cachedCost >= freshCost {
		t.Fatalf("a 99%%-cached prompt (%.4f) must cost less than the same prompt priced as fresh (%.4f)", cachedCost, freshCost)
	}

	// The pre-fix behaviour recorded the cached prompt as entirely fresh, so
	// this ratio is what the bug inflated cost by.
	if ratio := freshCost / cachedCost; ratio < 3 {
		t.Fatalf("expected the mispricing to be several-fold, got %.1fx", ratio)
	}
}

// Cache writes cost more than fresh input, so they must not be quietly treated
// as a saving.
func TestCacheCreationIsBilledAboveFreshInput(t *testing.T) {
	model := models.Model{CostPer1MIn: 3.00, CostPer1MInCached: 3.75}

	writing, _ := ComputeCost(model, provider.TokenUsage{CacheCreationTokens: 1_000_000})
	fresh, _ := ComputeCost(model, provider.TokenUsage{InputTokens: 1_000_000})

	if writing <= fresh {
		t.Fatalf("cache creation (%.2f) should cost more than fresh input (%.2f)", writing, fresh)
	}
}

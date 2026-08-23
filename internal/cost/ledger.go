// Package cost owns the per-call token/cost ledger and (in later phases)
// budgets, forecasts, and governor decisions. See roadmapplan.md §3 and §5.2.
//
// PR 1 scope: a durable per-model-call ledger so that every provider call has
// correlation ids, timing, cache-aware token counts, and a cost with an explicit
// pricing-catalog version. Session/task token and cost totals are DERIVED from
// this ledger rather than overwritten by the latest call.
package cost

import (
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
)

// PriceCatalogVersion identifies the built-in model price catalog used to
// compute a call's estimated cost. It is stored on every ledger row so costs
// remain interpretable if catalog prices change later.
const PriceCatalogVersion = "models-builtin-2026-07"

// CallStatus is the lifecycle state of a model call.
type CallStatus string

const (
	CallStarted   CallStatus = "started"
	CallCompleted CallStatus = "completed"
	CallFailed    CallStatus = "failed"
	CallCancelled CallStatus = "cancelled"
)

// CostState records whether a call's cost is trustworthy. Unknown pricing must
// surface as CostUnknown rather than a misleading zero (roadmapplan.md §5.2).
type CostState string

const (
	CostKnown   CostState = "known"
	CostUnknown CostState = "cost_unknown"
)

// ModelCall is one row of the per-call ledger.
type ModelCall struct {
	ID        string
	ProjectID string
	TaskID    string
	TurnID    string
	SessionID string
	MessageID string

	Provider string
	Model    string
	Status   CallStatus

	// RetryGroup links a call to its retry siblings; empty for singletons.
	RetryGroup          string
	PriceCatalogVersion string
	CostState           CostState

	StartedAt    int64 // unix millis
	FinishedAt   int64 // unix millis, 0 while running
	FirstTokenAt int64 // unix millis, 0 until first token
	LatencyMS    int64
	TTFTMS       int64

	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	EstimatedCost       float64
	ErrorCode           string
}

// Totals is an aggregate over a set of calls (a session or a task).
type Totals struct {
	Calls               int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	// PromptTokens/CompletionTokens keep the legacy session semantics:
	// prompt = fresh input + cache-creation input; completion = output + cache reads.
	PromptTokens     int64
	CompletionTokens int64
	Cost             float64
	// CostUnknown is true when any contributing call had unknown pricing, so
	// the aggregate Cost is a lower bound rather than exact.
	CostUnknown bool
}

// ComputeCost returns the estimated cost of a usage against a model's price
// catalog, and whether that cost is trustworthy.
//
// A model whose four rate fields are all zero is treated as free ("known", cost
// 0) only for providers where that is legitimate (local/mock); for any other
// provider a fully-zero rate table means we lack pricing and the state is
// CostUnknown so the UI never claims a false zero.
func ComputeCost(model models.Model, usage provider.TokenUsage) (float64, CostState) {
	cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
		model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
		model.CostPer1MIn/1e6*float64(usage.InputTokens) +
		model.CostPer1MOut/1e6*float64(usage.OutputTokens)

	ratesAllZero := model.CostPer1MIn == 0 && model.CostPer1MOut == 0 &&
		model.CostPer1MInCached == 0 && model.CostPer1MOutCached == 0
	if ratesAllZero && !isFreeProvider(model.Provider) {
		return 0, CostUnknown
	}
	return cost, CostKnown
}

func isFreeProvider(p models.ModelProvider) bool {
	return p == models.ProviderLocal || p == models.ProviderMock
}

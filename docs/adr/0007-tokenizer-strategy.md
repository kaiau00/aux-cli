# ADR 0007 — Tokenizer strategy for estimates across providers

- Status: accepted
- Date: 2026-08-14

## Decision

Aux uses a single, deterministic, provider-agnostic **character-based
estimate** — `(len(text) + 3) / 4` — everywhere it needs a token count before
a call completes (`internal/promptcompiler.EstimateMessages`/`estimateText`,
budget headroom checks, context-pressure display). It does **not** ship a
real per-provider tokenizer (no `tiktoken`, no Anthropic/Gemini tokenizer
bindings). After a call completes, the *real* usage from the provider's
response (`ProviderResponse.Usage`) is what gets recorded to the cost ledger
and reconciled against — the estimate is only ever used for pre-call
decisions (budget checks, "will this fit," UI pressure bars), never for
billing or reported cost.

## Alternatives considered

- **Bundle a real tokenizer (e.g. tiktoken-go) as the estimate.** Rejected
  for now: real tokenizers are provider- and often model-specific (Anthropic,
  OpenAI, and Gemini all tokenize differently), so "the" tokenizer would
  still be an approximation for at least some configured providers, while
  adding a real dependency and encoding-table maintenance burden. The
  4-chars/token heuristic is deliberately honest about being an estimate
  rather than implying provider-exact precision it can't actually deliver
  for a multi-provider tool.
- **Call each provider's estimate endpoint before every turn.** Rejected:
  adds a network round-trip (and cost, for providers that charge for it) to
  every context-budget check, which needs to run synchronously and cheaply
  on the hot path.

## Evidence

- `internal/cost` never uses the estimate for recorded cost — `FinishCall`
  stores the provider's actual `InputTokens`/`OutputTokens`
  (`internal/cost/ledger.go`), and `Totals` aggregates only real ledger rows.
  The estimate is confined to `promptcompiler` and TUI/dashboard display
  (`viewmodel.ContextBudgetVM`).
- The exit-gate language ("token and cost totals reconcile
  from call to task to session," Phase 1.0) is about the *real* ledger, which
  this design already satisfies independent of estimate accuracy.

## Consequences

- Pre-call budget/pressure numbers are approximate (usually within a
  reasonable margin for English prose, less accurate for dense code or
  non-Latin text) and must never be presented as exact — existing UI copy
  already says "estimate"/uses `~` prefixes where relevant.
- Swapping in a real tokenizer later is additive: `EstimateMessages` is the
  single call site to change; no schema or ledger change is implied since
  actual usage was never derived from it.

## Revisit trigger

Add a real tokenizer (likely per-provider, selected by the configured
model's provider) if estimate error becomes large enough to cause budget
mode (`efficient`/`capped`) to under- or over-throttle in practice —
something only measurable once there's real usage data to compare against.

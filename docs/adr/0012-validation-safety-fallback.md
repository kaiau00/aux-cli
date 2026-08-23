# ADR 0012 — Validation safety fallback

- Status: accepted
- Date: 2026-08-14

## Decision

Validation never trusts impact-graph selection as the sole basis for scoping
what to run. `impact.Service.Analyze` computes `BroadenValidation` (bool) and
sets it `true` whenever **any** of: the graph is empty (never indexed),
stale (source revision doesn't match the current one), or a changed path is
uncovered by the graph (today: any non-Go file, or a Go file the indexer
hasn't seen) — or when computed `Uncertainty >= 0.5`. When
`BroadenValidation` is true, `recommend()` returns the broad commands
(`go build ./...`, `go test ./...`) instead of targeted per-package
commands, with a `Reason` string explaining which condition triggered it.
Proof-of-done is a separate, stricter gate on top of this: a criterion only
reaches `Validated` state with real executable evidence
(`validation.Service.RunIntent` recording a `Run`) — a claim, or a
subagent's self-reported `ValidationSummary` (§11.3), is never sufficient by
itself.

## Alternatives considered

- **Trust the graph once it's been indexed once.** Rejected: a stale graph
  (source revision changed since last index) is actively misleading — it
  would recommend validation scoped to an outdated dependency picture.
  Staleness is checked on every `Analyze` call, not cached as "was ever
  built."
- **Always run broad validation, skip impact-scoped validation entirely.**
  Rejected: defeats the entire purpose of the impact graph (targeted,
  faster validation) — the fallback exists so scoping can be trusted
  *when it's actually trustworthy*, not to avoid trusting it ever.
- **Let a subagent's `ValidationSummary.Passed` (§11.3) count as
  proof-of-done directly.** Explicitly rejected in the subagent design
  itself (`internal/llm/agent/subtask.go`'s doc comment on
  `ValidationSummary`): it's the subagent's own account, not authoritative
  evidence — only `validation.Service.RunIntent` recording a real `Run`
  can validate a criterion.

## Evidence

- `internal/impact/service.go`'s `uncertainty()`/`riskFrom()`/`recommend()`
  and `TestAnalyzeBroadensWhenUncovered`,
  `TestAnalyzeBroadensWhenGraphEmpty`, `TestAnalyzeBroadensWhenStale` cover
  all three broadening conditions independently.
- `internal/validation/validation.go`'s `CriterionState` enum
  (`Uncovered → Claimed → PartiallyEvidenced → Validated`) and
  `EvidenceType` constants (`executable`, `inspection`, `diff`,
  `user_waiver`) show validated state is always evidence-typed, never
  inferred from confidence or self-report.

## Consequences

- A newly-cloned or freshly-touched project always gets broad validation
  until the impact graph is built and fresh — correct-by-default even
  though it's less efficient than scoped validation.
- Any future evidence source (e.g. a subagent's validation-runner role
  output) must be adapted into a real `validation.Run` to count toward
  proof-of-done; it cannot bypass `RunIntent`.

## Revisit trigger

Revisit the `Uncertainty >= 0.5` threshold, or add finer-grained broadening
(e.g. broaden only the affected language's build/test rather than the whole
repo) once multi-language impact coverage (ADR 0011) makes "the whole repo"
too coarse a fallback unit.

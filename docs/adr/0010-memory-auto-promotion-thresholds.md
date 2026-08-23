# ADR 0010 — Memory auto-promotion thresholds

- Status: accepted
- Date: 2026-08-14

## Decision

A memory candidate (`internal/memory.Candidate`) is auto-promoted from
`StateCandidate` to `StateActive` at creation time under exactly two
conditions (`Service.Learn`, `internal/memory/service.go`): its `Type` is
`Episodic` (prior-task summaries are always active — they're inherently
low-risk, bounded, and superseded naturally by newer episodes), **or** its
`Confidence >= autoPromoteConfidence` (`0.85`). Everything else — most
`Factual` and `Procedural` candidates, which shape what gets asserted into
future prompts as "known" project knowledge — starts as a `candidate` and
requires an explicit promotion (`Store.Promote`) before it affects any
compiled prompt. A promotion is recorded as feedback
(`RecordFeedback(..., "promoted", "auto_policy")`) so the provenance of *why*
something became active is never lost, whether it was auto- or
manually-promoted.

## Alternatives considered

- **Auto-promote everything above a lower confidence bar (e.g. 0.5).**
  Rejected: factual/procedural memory becomes part of the manifest every
  task sees (`Coordinator.memorySection`); a wrong "fact" promoted too
  readily is a durable, compounding error, not a one-off mistake — the bar
  is deliberately high (0.85) to bias toward the memory system staying
  trustworthy over being maximally eager (the "memory becomes noisy or
  stale" risk).
- **Never auto-promote; require a human/evaluation gate for every memory.**
  Rejected for episodic memory specifically: episodic summaries are
  low-stakes (they inform, they don't assert durable facts) and require the
  learning loop to actually produce visible value without per-task manual
  curation, which would defeat the "compounding" goal.
- **Type-specific thresholds instead of one shared constant.** Not adopted
  yet — `autoPromoteConfidence` is currently a single package constant, not
  per-type. Left as future work rather than speculative complexity now (no
  evidence yet that Factual and Procedural need different bars).

## Evidence

- `internal/memory/service_test.go` exercises both the auto-promote and
  stays-candidate paths at the threshold boundary.
- `Store.MarkStaleForChangedRevision` (revision-aware invalidation) is the
  other half of this trust model: promotion is deliberately not permanent —
  a promoted memory is marked stale, not deleted, when its supporting
  revision changes, so an aggressive-enough threshold is recoverable rather
  than a one-way door.

## Consequences

- The 0.85 bar is a single tunable constant
  (`internal/memory/service.go:autoPromoteConfidence`) — changing product
  behavior here is a one-line change, not a redesign.
- Any future memory *source* (a new extraction heuristic, a subagent-derived
  candidate) inherits this policy automatically by going through
  `Service.Learn` with an honest `Confidence` value — the burden is on the
  candidate producer to not over-report confidence, not on this gate.

## Revisit trigger

Revisit per-type thresholds, or the 0.85 constant itself, once there's
real promoted-memory accuracy data (e.g. from `RecordCorrection` feedback
volume) to tune against, rather than adjusting it speculatively.

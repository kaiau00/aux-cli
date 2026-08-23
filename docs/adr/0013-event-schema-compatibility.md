# ADR 0013 — Event schema compatibility policy

- Status: accepted
- Date: 2026-08-14

## Decision

Every durable event carries an explicit `SchemaVersion` int, stamped at
append time from a single package constant
(`internal/eventstore.SchemaVersion = 1`, `events.go`). Events are
**append-only** — `Store.Append` never updates or deletes a row — and are
ordered by a monotonic `Sequence`, not wall-clock time, so replay
(`aux eval replay`, `internal/eval/experiment.go`'s `ReplayTaskState`) is
deterministic regardless of clock skew. A payload's shape is versioned
implicitly through `SchemaVersion`: a reader that understands version *N*
must be able to read any event stamped `<= N`; a breaking payload change
requires bumping `SchemaVersion` and giving readers a way to branch on it
(not yet needed in practice — every payload struct so far has only grown
additive, optional fields).

## Alternatives considered

- **Version each event *type* independently instead of one global
  constant.** Rejected for now: a single monotonically-increasing schema
  version is simpler to reason about ("this whole store was written by
  schema N or earlier") and sufficient while every payload change so far has
  been additive; per-type versioning is a strictly more complex scheme with
  no current payload divergent enough to need it.
- **Allow in-place event mutation for corrections.** Rejected: events are
  the durable, replayable source of truth for task reconstruction
  (`aux eval replay`) and dashboard live state (SSE `/events` pipe) — a
  mutable event log would make replay non-deterministic and break the
  "durable event store" exit-gate guarantee from Phase 1.0.
- **Delete/compact old events.** Not adopted: no compaction exists yet
  (same category of deferred-retention decision as ADR 0004's artifact
  retention) — ties to the same "measure real growth before building policy"
  reasoning.

## Evidence

- `internal/eventstore/store.go` inserts `schema_version` on every append
  and never issues an `UPDATE`/`DELETE` against the events table.
- `internal/eval/experiment.go`'s replay functions
  (`ReplayTaskState`, `TestReplayTaskState`, `TestReplayValidated`)
  reconstruct task state purely from the append-only event log, which is
  only possible because the log is immutable and ordered.

## Consequences

- Adding a new event `Type` or a new optional payload field is always
  additive and safe without a version bump.
- A genuinely breaking payload change (renaming/removing a required field)
  must bump `SchemaVersion` and needs an explicit compatibility decision at
  that time — this ADR does not pre-solve that, it only guarantees the
  version marker exists to make the decision possible.

## Revisit trigger

Revisit if/when a payload needs a breaking change — decide then whether
readers branch on `SchemaVersion` or whether a one-time migration rewrites
old rows (the append-only guarantee would need an explicit, deliberate
exception for that migration).

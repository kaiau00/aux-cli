# ADR 0002 — SQLite transaction and event-sequence strategy

- Status: accepted
- Date: 2026-07-24

## Decision

- Durable domain events live in a single `domain_events` table with a per-database
  **monotonic integer `sequence`** allocated inside the same transaction as the write that
  produced them, using `INSERT ... SELECT COALESCE(MAX(sequence),0)+1`.
- Correctness of read models derives from **replaying `sequence` order**, not from receiving
  every in-process pub/sub notification.
- In-process `pubsub` notifications are published **only after the transaction commits**.
  Subscribers may miss notifications and must be able to recover from the event sequence.
- The database (WAL mode, `PRAGMA foreign_keys=ON`, already configured in `internal/db`)
  remains the source of truth for what occurred.

## Alternatives considered

- **SQLite `rowid` as sequence.** Rejected: not guaranteed gap-free/monotonic across vacuum,
  and not an explicit contract.
- **Autoincrement column only.** Works, but an explicit `MAX+1` inside the append transaction
  keeps sequence assignment visible and testable and lets us assert ordering under concurrent
  appends.

## Evidence

- WAL + `synchronous=NORMAL` already used; single-writer SQLite serializes appends so
  `MAX(sequence)+1` inside a transaction is safe.

## Consequences

- `internal/eventstore` owns append/read and the sequence contract.
- Projection/read-model rebuild is always possible from `sequence = 0`.

## Revisit trigger

Move to an external event log or add sharded sequences only if we outgrow single-writer SQLite.

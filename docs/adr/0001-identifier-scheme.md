# ADR 0001 — Application-layer identifier scheme

- Status: accepted
- Date: 2026-07-24

## Decision

Generate all cross-system domain identifiers (`project_id`, `task_id`, `turn_id`,
`model_call_id`, `tool_execution_id`, `event_id`, `artifact_id`, page/memory/skill
IDs, …) in the application layer as **UUIDv7** strings via
`internal/ids`. Database row ids are never used as cross-system identity.

## Alternatives considered

- **ULID.** Also sortable and time-ordered. Rejected only because `github.com/google/uuid`
  (already a dependency) ships a spec-compliant `NewV7`, so UUIDv7 needs no new module and
  interoperates with the existing `uuid`-based session/message ids.
- **Database autoincrement ids.** Rejected: couples identity to one SQLite file, defeats the
  "replaceable harness" goal, and cannot be assigned before insert (needed for event
  correlation and transactional appends).

## Evidence

- UUIDv7 is time-sortable, so it doubles as a coarse creation-order key and indexes well in
  SQLite b-trees.
- Application-assigned ids let us emit correlated events and ledger rows inside one
  transaction before any row exists.

## Consequences

- One helper package `internal/ids` (`ids.New()`), used everywhere.
- Existing `uuid.New()` (v4) call sites for sessions/messages remain valid; migration to v7 is
  additive and non-breaking.

## Revisit trigger

Switch to ULID only if we drop `google/uuid` or need Crockford base32 external compatibility.

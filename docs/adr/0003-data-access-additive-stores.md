# ADR 0003 — Additive raw-SQL stores instead of extending generated sqlc code

- Status: accepted
- Date: 2026-07-24

## Context

The plan (§4.1) suggests keeping raw SQL under `internal/db/sql/<domain>.sql` and regenerating
sqlc. `sqlc` is not available in this environment, and the generated `internal/db` code uses
hand-registered prepared statements (`db.go`, `querier.go`) that would have to be edited by
hand for every new query — exactly the kind of generated-file editing that is error prone.

## Decision

Each new domain package (`internal/cost`, `internal/eventstore`, `internal/project`,
`internal/profile`, `internal/task`, `internal/artifact`, `internal/contextstore`, …) owns a
small store that executes raw parameterized SQL directly against a `*sql.DB`/`db.DBTX`. The
existing sqlc-generated `sessions`/`messages`/`files` access is left untouched and continues to
serve legacy paths.

- Schema still evolves through focused goose migrations (§4.1) embedded in `internal/db`.
- Stores accept the narrow `db.DBTX` interface (already defined by sqlc) so tests inject an
  in-memory SQLite connection.
- All SQL is parameterized; no string interpolation of untrusted input.

## Alternatives considered

- **Install and run sqlc.** Not available; would also require editing generated prepared-
  statement registration. Rejected for now.
- **Reuse the sqlc `Queries` god-struct for new domains.** Rejected: merges unrelated domains
  into one type, opposite of §3.1 package boundaries.

## Consequences

- New domains are decoupled and independently testable, matching §3.2 grouped services.
- If sqlc becomes available later, individual stores can be regenerated without touching others.

## Revisit trigger

Adopt sqlc generation for a domain once sqlc is part of the toolchain and the domain's query set
is stable.

## Orphaned tables

- 2026-10-03: `governor_policies` (migration `20260724000011`) and `governor_policy_evaluations`
  (`20260727000013`) have no reader or writer since `internal/govpolicy` was removed (plan M1.5).
  Migrations only add, so the tables stay; a later store may reuse or drop them.

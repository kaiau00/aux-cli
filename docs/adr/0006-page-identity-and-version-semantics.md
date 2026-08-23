# ADR 0006 — Page identity and version semantics

- Status: accepted
- Date: 2026-08-14

## Decision

A context **page** (`internal/contextstore.Page`) is identified by
`(project_id, page_type, stable_key)` — stable across the page's lifetime
regardless of content changes (e.g. `file_region:internal/foo.go#L1-40`
stays the same page as the file is edited). A page **version**
(`PageVersion`) is a content-addressed snapshot of that page, keyed by
`(page_id, content_hash)`: identical content reuses the same version row
(`Store.UpsertVersion` dedups on hash), so unchanged content never grows the
table.

A **binding** (`Binding`) is the fact that one page *version* was in one
specific model call's working set, with a `State` — `resident`, `available`,
`evicted`, `faulted`, or `pinned` — and a `Reason`. Bindings are immutable,
append-only, per-call history; they are never edited after the call
completes. This three-layer model (page → version → binding) is what makes a
prompt explainable page-by-page after the fact
(`Store.BindingsForCall`) without conflating "what this page currently is"
with "what a specific past call actually received."

## Alternatives considered

- **One row per page, mutated in place with a `content` and `state`
  column.** Rejected: loses history (you can no longer answer "what did
  call X actually receive"), and mutating a resident/available flag
  in-place is exactly the "local-only checkbox" failure mode this schema is
  designed to avoid (see the M6 exclusion work, `internal/contextstore`
  `Exclude`/`Include`/`Exclusions`, which is deliberately a *separate*,
  forward-looking override table rather than a `Binding` mutation, because
  bindings describe the past and overrides describe the next compile).
- **Version pages by wall-clock timestamp instead of content hash.**
  Rejected: would create a new version on every touch even when content is
  byte-identical, defeating dedup and making `Compare` (checkpoint-style
  diffing) noisy.

## Evidence

- `Store.UpsertPage`/`UpsertVersion` both dedup by lookup-before-insert
  (`internal/contextstore/store.go`), confirmed by
  `TestPageAndVersionDedup`.
- `BindingsForCall` joins binding → version → page and orders by
  `(state, rank)`, giving a deterministic, reconstructable manifest per call
  (`TestBindingsForCall`-style coverage in `store_test.go`).

## Consequences

- Any future "what changed" or "why was this evicted" UI reads bindings, not
  pages — pages alone don't carry per-call state.
- A page/version identity scheme change (e.g. adding a new `PageType`) is
  additive: new stable-key formats don't require migrating existing rows.

## Revisit trigger

Revisit if per-call binding volume becomes a storage concern for long-running
tasks with many turns — would need a retention policy analogous to ADR 0004's
artifact retention, not a redesign of the identity model itself.

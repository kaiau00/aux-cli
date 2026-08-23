# ADR 0009 — Profile merge rules

- Status: accepted
- Date: 2026-08-14

## Decision

Effective profiles merge multiple precedence-ordered **layers**
(`internal/profile/effective.go`'s `Compile(projectID, revisionID, taskMode string, layers []LayerInput) Effective`).
Layers are sorted ascending by `Precedence` and applied low → high; for each
`(type, key)` pair, the **highest-precedence layer wins**, and every
lower-precedence entry that contributed the same key becomes recorded
**provenance** (`Overrides []Provenance`) on the winning entry rather than
being silently dropped. An entry is flagged `Conflict: true` only when a
lower layer set the *same key to a different value* — same value from
multiple layers is not a conflict. Today there are two layers: `builtin`
(lowest, `Precedence[OwnerBuiltin]`) and `project`
(`Precedence[OwnerProject]`); the ordering scheme already supports adding
higher layers (e.g. user- or team-level overrides) without changing the
merge algorithm.

Content-addressing applies here too: `Builder.Build` reuses an existing
profile *version* when the scanned content hash is unchanged
(`internal/profile/builder.go`), so re-running `project refresh` on an
untouched repo produces zero new rows — merge determinism plus reuse is what
makes `versionSetHash` (`internal/profile/effective.go`) a stable identity
for "this exact combination of layer versions."

## Alternatives considered

- **First-writer-wins instead of last-writer-wins.** Rejected: would make a
  more-specific, higher-precedence layer (e.g. a future user-level override)
  unable to override a lower one, which is the opposite of the intended
  precedence semantics.
- **Silently drop overridden entries instead of recording provenance.**
  Rejected: the CLI (`aux project show`, `aux profile show --effective`) and
  the dashboard's Project Brain view (`viewmodel.ProfileSummaryVM.Conflicts`)
  both need to explain *why* a value won, not just report the final value —
  provenance is what "explainable" actually depends on here (the general
  explainability requirement, applied to profiles).
- **Union/merge list-valued knowledge (e.g. multiple validation commands)
  instead of last-writer-wins per key.** Not rejected outright — this is
  already how it works in practice, since list-like knowledge is modeled as
  *distinct keys* (e.g. `go.test`, `go.build` are separate
  `EntryValidationCommand` keys), so union happens naturally without a
  separate merge rule (documented in `effective.go`'s `Compile` comment).

## Evidence

- `internal/profile/effective_test.go` covers precedence, provenance, and
  conflict-flagging directly.
- `aux project refresh` (added this session, `cmd/project.go`) surfaces
  `DiffEffective` (added/removed/changed keys between two effective
  profiles) precisely because the merge is deterministic enough for a diff
  to be meaningful rather than noisy.

## Consequences

- Adding a new profile layer (e.g. a user-level `~/.aux` layer) is additive:
  give it a `Precedence` above project, feed it into `Compile`'s `layers`
  slice, and the existing merge/provenance/conflict logic applies unchanged.
- A profile entry's "winner" can change between runs only if the underlying
  scanned content actually changed (content-hash-gated), not due to merge
  non-determinism.

## Revisit trigger

Revisit if a future layer needs partial-field merge within one key (e.g.
merging two partially-specified validation commands) rather than whole-value
override — today's model treats a `(type,key)` value as atomic.

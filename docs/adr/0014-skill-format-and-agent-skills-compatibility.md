# ADR 0014 — Skill format and Agent Skills compatibility

- Status: accepted
- Date: 2026-08-14

## Decision

Aux's internal skill representation (`internal/skill.Content`) is richer than
the public Agent Skills interchange format — it carries Aux-specific fields
(`ToolRequirements`, `ContextRequirements`, `ValidationRequirements`,
`FailurePatterns`) alongside the portable ones (`Name`, `Purpose`, `Scope`,
`Triggers`, `Procedure`). At the import/export boundary,
`internal/skill/agentskills.go` defines a **separate, narrower**
`AgentSkillManifest` type (`Name`, `Description`, `Triggers`, `Steps`) with
explicit `ToAgentSkill`/`FromAgentSkill` converters. Aux-specific metadata
(evaluation state, source trajectories, promotion history) never round-trips
through this boundary — it stays in Aux's own store
(`internal/skill/skill.go`'s `Skill`/`Version` records) and is
regenerated/re-evaluated locally if a skill is re-imported.

## Alternatives considered

- **Make `skill.Content` itself the Agent Skills format (no separate
  manifest type).** Rejected: would force every Aux-specific field to be
  either shoehorned into the public format (polluting it for other Agent
  Skills consumers) or silently dropped on every internal read, rather than
  making the lossy boundary explicit and intentional at one clearly-named
  conversion point.
- **Losslessly round-trip everything via a custom extension namespace in the
  exported format.** Not adopted: adds format complexity for a
  compatibility promise (full fidelity across tools that don't share Aux's
  schema) nothing currently requires — Aux's own promotion/evaluation state
  is meaningless to a non-Aux consumer of an exported skill anyway.
- **Export skills as raw JSON dumps of the internal `Skill`/`Version`
  rows.** Rejected: not portable — any other Agent Skills-compatible tool
  would need to understand Aux's internal schema instead of the shared
  interchange shape.

## Evidence

- `internal/skill/agentskills.go`'s `ToAgentSkill`/`FromAgentSkill` are pure,
  explicit, and total (every `Content` field either maps to
  `AgentSkillManifest` or is dropped by omission — no silent partial
  mapping).
- `internal/bundle` (`aux bundle export|import`, added earlier this
  program) already treats imported skills as **candidates**, never
  directly active — consistent with this ADR's stance that cross-boundary
  data (whether from Agent Skills or a bundle) carries no Aux-internal trust
  state and must be re-evaluated locally.

## Consequences

- A skill exported from Aux and re-imported elsewhere keeps its portable
  content but loses Aux-specific evaluation/trust metadata by design — this
  is expected, not a bug to fix.
- Extending the internal `Content` struct with a new Aux-specific field
  never requires touching `AgentSkillManifest` unless that field is meant to
  be portable.

## Revisit trigger

Revisit if the public Agent Skills spec adds a field Aux's `Content` should
also portably carry (e.g. a standardized requirements/capabilities field) —
extend `AgentSkillManifest` and the converters together, not `Content` alone.

# ADR 0008 — Project identity for forks, worktrees, and remote-less repositories

- Status: accepted
- Date: 2026-08-14

## Decision

A project's identity is keyed by its **normalized VCS remote** when one
exists (`internal/project/vcs.go`'s `GitVCS.Inspect` reads `git remote
get-url origin`, hashed), falling back to the **canonical root path** for
local-only repositories with no remote (`internal/project.CanonicalizePath`
+ root path hash). Reopening the same repository — from the same clone, a
different clone of the same remote, or the same path — resolves to the same
`project_id` (`Service.Resolve`, `internal/project/service.go`). A **root**
(`project.Root`) is a distinct row per physical directory (worktree or
package root) belonging to that one project identity — so a project can have
many roots without having many identities.

This directly extends to the subagent git worktrees added in §11.3
(`internal/worktree`): a worktree created via `worktree.Create` for a
subagent is a sibling working directory of the same clone, on a new branch.
It is **not** a new project — when/if a subagent's worktree root is resolved
via `project.Service.Resolve`, it must land on the *same* `project_id` as
its parent, because it shares the same normalized remote. The worktree gets
its own `project.Root` row (a new canonical path), not a new project.

## Alternatives considered

- **Treat every worktree as its own project.** Rejected: would fragment
  profile compilation, memory, and the impact graph across the parent repo
  and every worktree spawned from it, defeating the entire premise of "Aux
  remembers the project" for exactly the kind of parallel/subagent work
  §11.3 exists to support.
- **Key identity purely on root path, ignoring the remote.** Rejected before
  this ADR (it's the reason remote-hash-first exists): would make a
  re-cloned or moved repository look like a brand-new project, losing all
  accumulated profile/memory/impact history for something that is, from the
  user's perspective, the same codebase.

## Evidence

- `internal/project/vcs.go` and `internal/project/service.go`'s `Resolve`
  already implement remote-first, path-fallback identity, and
  `internal/project/project_test.go` covers reopening via a fake VCS.
- `internal/worktree/worktree.go` (added this session) only manages the
  filesystem/git side of an isolated working directory; it deliberately does
  not touch `project.Store` or mint any identity itself — identity resolution
  for a worktree path goes through the same `Service.Resolve` path as any
  other directory, which is what makes the "same remote → same project"
  guarantee hold without worktree-specific code.

## Consequences

- A subagent worktree's checkpoints, memory candidates, and impact-graph
  contributions all attribute to the parent project's identity, not a
  fragment — this is required for the parent to actually benefit from
  subagent work.
- A truly independent fork (different remote) correctly gets its own
  identity even if it started as a clone of the same upstream, since the
  normalized remote differs.

## Revisit trigger

Revisit if Aux needs to distinguish "my fork" from "upstream" as *related but
distinct* identities for the same logical codebase — that's a job for the
related-project graph (`internal/relatedproject`, ADR-worthy relation type),
not a change to this identity scheme.

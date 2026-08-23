# Live evaluation gates and deferred remainders

This document records how to run the opt-in live evaluation gates (roadmapplan.md
§9, §10.3, §16.7) and lists the work that remains, by exact plan section. It is
the "leave the harness in place and document the exact command" deliverable
for the live gates: this environment has **no provider credentials**, so the
live gates are intentionally not run here (they must never be required for
normal tests or CI — the whole suite is deterministic and offline).

## Deterministic harness already in place

These run offline, with no credentials, and are covered by tests:

- `aux eval compiler` — prompt-compiler evaluation (control vs optimized) on baseline fixtures.
- `aux eval experiment` — runs and persists the compiler experiment (compatibility vs paging); records evidence before any default changes.
- `aux eval replay <task-id>` — deterministically reconstructs a task's state from its durable events (no provider calls).
- `aux cost <task-id>` — a task's budget usage, trajectory, and waste warnings.
- `aux validate <task-id>` — runs the project's own validation commands and records proof-of-done evidence (needs `--yes` to approve them).

Evaluation-gated promotion is already enforced in code and tested: skills refuse
to promote without a passing evaluation (`skill.Service.Promote` →
`ErrNoEvaluationEvidence`), and proof-of-done never marks a criterion validated
without a passing run.

Skill candidates are now produced automatically: a completed task proposes one
from the commands it actually validated (`skill.Extract`, called from
`task.Coordinator.proposeSkills`). Until this landed the candidate pipeline had
no production caller, so no task had ever produced a skill. Extraction is
deterministic and narrow — a command either passed during that task or it does
not appear — and candidates stay inert until the gate above is satisfied.

**What still does not exist is the task-level benchmark.** `aux eval compiler`
compares prompt compilers over fixtures; it does not measure whether a change
helps on real work. Every token and latency claim about this project is
unfalsifiable until a 20–30 task suite with recorded baselines exists, and two
things are explicitly blocked on it: promoting any auto-extracted skill, and
flipping demand paging's default to on.

## Live gates (opt-in, require credentials + budget)

Run these only with a configured provider and an explicit opt-in, on a machine
where spending real budget is acceptable. They compare the **same preferred
model** with a capability on vs off — never per-action multi-model routing.

### §9.6 — Governed vs baseline ("accepted changes per dollar")

The cost governor ships default-off and supports `off` / `observe` / `on`
(`costGovernor.mode` in config; defaults to `off`). To measure the governor's
effect, run a fixed task set twice against the same model and compare accepted
changes per dollar (and task success rate) from the per-task ledger:

1. Baseline: set `costGovernor.mode: off`, run the task set, note each baseline task id.
2. Governed: set `costGovernor.mode: on`, run the identical task set, note each variant task id.
3. Compare: `aux eval ab <baseline-task-id> <variant-task-id>` — this computes
   accepted validated changes per dollar for both runs from durable records
   (ledger, proof-of-done, checkpoints) and reports whether the variant improved.
   It is conservative: an unknown-cost run never counts as an improvement.

The governor must not reduce task success on the fixture set for `on` to become a
default. Until then it stays `observe` (measures, no behavior change).

### §10.3 — Skill vs baseline

For a candidate skill, run its target task set with the skill inactive
(baseline) and active, on the same model, and compare success rate / cost. A
skill is promoted only when the active run beats baseline by the agreed margin —
this is the evaluation evidence `skill.Service.Promote` already requires.

### Note

The **comparison** half of the gate is implemented and tested: `aux eval ab`
computes the metric and improvement decision offline from two recorded runs, and
`govpolicy` / `skill` promotion consume that pass/fail as evidence. The only part
that needs credentials is **driving the two agent runs** with a real provider —
that turnkey `aux eval live` runner is not built here because it cannot be
verified without spending budget.

## Now implemented and wired (this pass)

Everything below is built, tested, **and reachable from a real user path** —
not just present as an isolated, tested package:

- **§9.7 learned policy promotion** — `internal/govpolicy`, evaluation-gated like
  skills, with rollback + evidence trail. Surfaced in the dashboard's
  Optimization view (`/optimization`).
- **§9.6 / §10.3 comparison** — `aux eval ab` (`internal/eval` A/B runner).
  Experiment history surfaced in the dashboard's Optimization view.
- **§11.1 first-mutation checkpointing** — `internal/mutationcp`, in addition to
  completion-time capture. Now also fires for subagent tasks (see §11.3 below),
  since a subagent's task lifecycle runs through the same coordinator.
- **§11.2 related-project graph** — `internal/relatedproject`, now wired into
  `internal/app/app.go` (background derivation from `go.mod` on project
  resolve), `internal/task/coordinator.go` (manifest section for related
  projects), the CLI (`aux project related`), and the dashboard's Project
  Brain view (`/project`). Previously built and tested in isolation only —
  this pass is what made it reachable.
- **§11.3 efficient subagents** — largely built this pass:
  subagents now get real task identity (linked to their parent via
  `tasks.parent_task_id`, `internal/llm/agent/agent-tool.go`), which gives
  them real per-subtask checkpointing and cost attribution "for free" through
  the existing task coordinator lifecycle (`internal/cost.TaskTotals` now
  recursively rolls up descendant tasks). Subagents support 4 specialist
  roles (repo mapper, impact analyst, validation runner, reviewer,
  `internal/llm/agent/subtask.go`) with role-specific tools/prompts, and
  report back through a structured `report` tool instead of free text.
  `SubtaskBegin`/`SubtaskEnd` hooks fire around every subagent invocation.
  Git worktree isolation is now wired: the validation-runner role (the only
  role with Bash, and so the only one that can cause filesystem side effects)
  runs in a real `git worktree`, synced with the parent's live uncommitted
  state so it validates current code rather than the last commit. Tool path
  resolution became per-call via `tools.WorkingDirContextKey` /
  `ResolveWorkingDir`, which is what made this possible. Worktrees are torn
  down with their branch refs after each run. Sibling subagents in the same
  model turn are checked for overlapping write sets
  (`checkpoint.DetectWriteConflicts`), surfaced to the parent as a risk on
  the structured report. Subagent tool sets remain read-only apart from that
  Bash access; note that tool calls execute sequentially, so "siblings" means
  same-turn, not concurrent.
- **§11.4 multi-repo child tasks** — `internal/multirepo` compiler, now wired
  into `internal/task/coordinator.go` (`Coordinator.BeginMultiRepo`) and the
  CLI (`aux task begin --repo <path> ... `, `--auto-related`). Previously
  built and tested in isolation only.
- **§12.3 lifecycle hooks** — `internal/hooks`, dispatched at task and subtask
  boundaries (§11.3) and around every tool execution (`ToolPre`/`ToolPost`,
  from `tools.Executor`; a ToolPre handler can veto the call). Built-in
  observability handlers are registered in `app.New`
  (`hooks.RegisterObservability`), so the dispatch points have a real
  consumer rather than firing into an empty registry. **User-defined shell
  hooks are not planned.** Running commands from a config file is arbitrary
  code execution, and a repository-level config that registers one turns
  cloning a repository into running its code. Shipping that safely needs a
  threat model and a review this project has not done, and the internal
  dispatch points below cover the observability the hooks were wanted for.
  Removed from the roadmap rather than left pending, so it stops reading as
  a capability that is nearly here.
- **§12.4 runtime adapters** — `internal/runtime` Adapter + `runtimetest`
  conformance contract.
- **§12.5 shareable bundles** — `internal/bundle` (`aux bundle export|import`);
  imports arrive as candidates.
- **§13.12 / §13.14 dashboard** — the default route (`/`) now serves the
  task-first workspace (previously the legacy session/log view); the
  decorative "Live Core" panel is gone, replaced by a real Live Activity
  panel (connection state, active-session count, last event) on the
  secondary `/sessions` view. All 6 planned dashboard views now exist:
  Tasks (`/tasks`), Project Brain (`/project`), Memory & skills (`/memory`),
  Impact graph (`/impact`), Optimization (`/optimization`), Sessions
  (`/sessions`) — previously only 2 of 6 existed.
- **§13.11 context controls** — the TUI's `x`/`u`/`c` (cross-off/un-cross/
  clear) keybindings now persist a real per-task exclusion
  (`internal/contextstore.Exclude/Include/ClearExclusions`), consulted by
  both prompt compilers on the task's next turn
  (`promptcompiler.applyExclusions`) — a real content change, not a
  display-only checkbox. An expanded, per-page context view (grouped by
  binding state) is available via a new Expand key (`e`) in the context pane.
  Narrow terminals (<80 cols) that drop the context panel can reach it again
  via a context drawer (`ctrl+g`), an overlay rather than a lost panel.
  Pinning is also real now (`p`, backed by a `context_pins` table): a pinned
  page's full content is guaranteed in the next compile, exempt from both
  exclusion and dedup stubbing. Reload remains the one control from the
  plan's "pin/exclude/reload" phrasing that does not exist.
  The agent can now reach the same exclusion itself
  (`tools.NewContextExcludeTool`, path-based), which matters because it is the
  party generating the context; pages it drops are marked distinctly in the
  pane so the user can see and undo them, and an `exclude` command applies the
  same action by path.
- **§13.18 accessibility** — icons fall back to ASCII when the terminal is
  detected as ASCII-only (`internal/tui/styles.SupportsUnicode`, explicit
  override via `AUX_ASCII_ICONS`). All 9 registered themes now have a
  deterministic, offline contrast test
  (`internal/tui/theme/contrast_test.go`) against the WCAG AA-normal 4.5:1
  floor for text/background — a substitute for the browser-based contrast
  tooling this environment doesn't have.
- **§13.19 visual testing** — golden fixture coverage extended to 12 states ×
  3 widths (was 9): added `permission-waiting`, `cancelled`, and
  `completed-validated` (distinct from `completed-unverified`). Added
  `TestFixturesRenderAcrossThemes`, which forces a real color profile and
  verifies every sampled theme (aux, catppuccin, dracula, tokyonight) renders
  every fixture without panicking and actually applies color, while the
  plain (color-stripped) content stays theme-independent.
- **§21 ADRs** — all 14 topics roadmapplan.md §21 lists are now recorded
  (`docs/adr/0001` through `0015`; `0003` is a bonus decision beyond the
  original 14). Previously only 2 of 14 were written.

## Genuinely remaining (need external resources)

- **§9.6 / §10.3 / §16.7 live A/B *execution*** — driving the two agent runs with
  a real provider to produce the dollar-efficiency *number*. The comparison,
  gates, and promotion that consume it are done; only the credentialed run is not
  (it cannot be verified here without spending budget).
- **§13.19 browser screenshot / contrast / SSE regression** — needs a browser.
  The TUI golden + semantic coverage (`internal/tui/visual`) is in place, the
  dashboard views are DOM/route tested, and theme contrast is now covered by
  a deterministic offline test, but pixel-level regression is still not
  runnable in this environment.
- **Full live multi-repo / cross-repo execution end to end** — the graph
  (§11.2) and multi-repo compiler (§11.4) are now wired into a real user path
  (CLI, coordinator, dashboard); exercising them across several real indexed
  repositories with a live provider is still environment-bound.

## Known, deliberately-scoped gaps (not blocked on external resources)

- **§11.3 git worktree isolation is not wired into live tool execution** —
  see above. `internal/worktree` exists and is tested; making it apply to
  real subagent file edits needs a separate refactor to make tool path
  resolution instance-scoped instead of process-global.
- **Artifact retention/compression** (ADR 0004) and **event-log
  retention** (ADR 0013) are deliberately unimplemented pending real growth
  data from actual usage — not a gap so much as a documented "measure before
  building" decision.

## Security posture (added after the repo-wide audit)

- **Permission grants are per-action.** A session-wide grant is keyed by a
  fingerprint — the command for Bash, the URL for Fetch — so approving one
  command never authorizes a different one. Previously the key was
  (tool, action, directory), and since Bash's directory is constant for a
  session, one approval silently covered every later command.
- **Reads outside the working directory prompt.** view/ls/glob/grep resolve
  through `EvalSymlinks` and compare against the call's working directory;
  outside reads require approval and fail closed when there is no way to ask.
- **Validation commands require approval.** They come from the compiled
  profile, which is derived from repo content, so running them unattended
  would be arbitrary code execution from a checked-out repository.
- **The dashboard token is compared in constant time**, and `~/.aux.json`
  (which holds provider keys) and the debug log are written 0600.

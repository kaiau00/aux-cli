# Aux — the master list

Everything that stands between Aux and a public release a stranger can depend
on. Rewritten 2026-10-02 from an outside audit of `origin/main` at `278b9f1`
(PR #27). The previous version of this file is in git history at `278b9f1`;
its closed-items appendix is carried forward at the bottom.

Everything here was checked by building, running, or reading the code at that
commit — not by re-reading the old file. Where an item cites a file and line,
that is where the evidence is.

**This is the status list.** The detailed execution spec for every item —
goal, files, steps, done-when tests, guardrails, decisions, and the release
checklist — is [`docs/shipping-plan.md`](docs/shipping-plan.md). Items here
map to milestones there (P0→M0, P1→M1, P2→M2, P3→M3, P4→M4). When the two
disagree, the plan wins and this file gets corrected.

---

## Progress (as of 2026-10-07)

Update this section and the plan's §6 checklist in every PR that moves an item.

**Merged to `main`** (one item per PR, squash-merged, all five gates green):

| Item | PR |
| --- | --- |
| M0.1 bash safe-list, M0.1b argument rules | #29, #32 |
| M0.2 `-p` requires `--yes` | #30 |
| M0.3 PR #28 merged, coverage floor 33.8 | #28, #33 |
| M0.4 repository hygiene | #31 |
| M1.1 manifest and task spec as a system addendum | #34 |
| M1.2 manifest and task-spec pages resident | #35 |
| M1.3 memory content in the addendum | #36 |
| M1.4 validation at task end, M1.7 README "How it works" | #37 |
| M1.5 `govpolicy` deleted | #38 |
| M1.6 evicted and faulted states removed | #39 |
| M2.1 model catalog | #40 |
| M2.2 `openai-go` v1.12.0, `anthropic-sdk-go` v1.78.0, `mcp-go` v1.1.1, `genai` v1.72.0 | #41, #42, #43, #44 |
| M2.3 slash commands from the composer | #45 |
| M2.4 `--continue` and `--resume` | #47 |

**Open:** M2.4 `--continue` and `--resume`, #47.

**Next:** M2.5 compaction as a decision, then M2.6–M2.8, then M3.

**Not yet verified** (each is a done-when that has not been run):

- M0.5 **[HUMAN]**: the repository is now `kaiau00/aux-cli`, matching the
  module path. The check that every URL resolves without a redirect has not
  been run.
- Appendix B scratch-repo e2e as one run. M1.4's scratch run is recorded in
  the claims table; the first-run model message and the `context.compiled`
  resident-pages check have not been run against a real provider.
- M2.2: the per-provider live check that cache tokens show in the status bar
  needs real keys. The offline streaming fixtures pass for all four SDKs.

**Gate numbers on `main`:** coverage 36.5% locally against a 33.8% floor
(36.1% before #45; measured 2026-10-07). The floor stays at 33.8 until M3.7
raises it. Dead-code baseline: 47 entries, down from 48 — #45 deleted the
unused `appModel.findCommand`.

---

## Where this actually stands

Aux is a fork of OpenCode's Go agent loop with a serious observability layer
built around it: a per-call ledger, an append-only event store, page bindings,
checkpoints, an impact graph, a project profile, memory, skills, validation,
and a read-only dashboard. The engineering discipline is real — ADRs, three
ratcheting CI gates, a race-clean suite — and the audit confirmed every
mechanical claim it tested (see [Claims](#claims-what-is-defensible-today)).

What the audit also found is that **the product thesis is not wired in**:

- The compiled project manifest, memory, related-project context, and task
  spec were computed on every turn and **never sent to the model**. Both prompt
  compilers marked them `available` and sent the bare transcript. Closed by
  M1.1 and M1.2: they go out as a system addendum and are counted resident.
- The learning loop only learns from validation evidence, and nothing in the
  agent loop produced validation evidence — only `aux validate <task-id>` did.
  Closed by M1.4: a task that changed files runs the profile's validation
  commands before it ends, and memory and a skill candidate follow from what
  passed.
- The bash "safe read-only" fast path was a prefix match with no shell-operator
  check, and its list included `kill`, `timeout`, `time`, `nice`, `nohup`,
  `env`, `go run`, `go test`. `echo hi; rm -rf .` ran without a prompt.
  Closed by M0.1, and the argument-level holes it found by M0.1b.

So: a clean, well-tested, well-instrumented agent whose instrumentation does
not yet feed back into the agent. The distance from here to "the thesis is
mechanically true" is small. The distance to "proven" is a benchmark away.

## Goals (decided 2026-10-02)

These are product decisions, recorded so the ordering below is not relitigated
every time someone opens this file.

| Decision | Choice |
| --- | --- |
| What comes first | **Wire the Project Brain into the prompt.** The thesis is the point; UX parity follows |
| Audience for this phase | **Public release**: tagged, install script works, README accurate |
| Providers that matter | Anthropic, OpenAI, Gemini, OpenRouter, local/OpenAI-compatible. Groq, Azure, Bedrock, Vertex, Copilot, xAI are unmaintained — keep only if free |
| Learning loop | **Agent runs validation itself at task end**; procedural memory and skill candidates accrue automatically |
| Performance claim | **Dropped from the README** until the brain is wired and measured |
| Upstream attribution | **Credit OpenCode plainly** in the README |

## Definition of done

**A tagged release that someone who is not the author installs with one
command, uses on their own repository for a week, and depends on — where the
README's description of what Aux does is true.**

Concretely: P0 through P3 below complete, a tag pushed, and a soak in which
outside findings taper off.

## Claims: what is defensible today

Aux should only say true things about itself. Verified 2026-10-02.

**Defensible now.**

| Claim | Backing |
| --- | --- |
| Builds and vets clean | `go build ./... && go vet ./...` at `278b9f1` |
| Race-clean under `-race` | Full suite, 2m03s, exit 0, locally and on `main`'s own post-merge CI run |
| No unreachable code outside a declared baseline | `scripts/deadcode.sh`, 48 accepted entries, unchanged |
| Coverage cannot regress | `scripts/coverage.sh`: 33.8% on CI against a 33.8% floor (raised from 30.8 in M0.3) |
| Upgrades across schema versions work | Tested from every recorded version with a populated database |
| A database from a newer build is refused | `ensureNotNewer`, tested |
| Reads outside the project need approval | `RequireReadAccess` canonicalizes through symlinks and fails closed |
| Dashboard is loopback-only and token-gated | Random token, constant-time compare, every data route |
| Sessions survive a panic | Deferred teardown, tested both directions |
| A task that changed files is validated before it ends, and what passed is remembered | M1.4: `internal/llm/agent/validate_test.go` drives `processGeneration` with real coordinator, validation, memory, skill, and permission services; scratch repo with a real provider: one edit task ran `go build`/`go test`, both criteria `validated` in `aux task show`, two procedural memories, one skill candidate |
| `aux -p` runs nothing that would prompt unless `--yes` is given | M0.2: `TestRunNonInteractiveDeniesWithoutYes` / `…ApprovesWithYes` (parent and subagent sessions); scratch repo: `touch created.txt` denied and listed on stderr without `--yes`, created with it |
| First run picks a current model and says which | M2.1: `catalog_test.go`, `config/model_test.go`; temp home with only `ANTHROPIC_API_KEY`: announced Claude Sonnet 5.5, wrote it to `~/.aux.json`, second run silent and the file unchanged |
| The context meter reflects what the window holds | Latest call's occupancy from the ledger |
| The TUI fits the terminal it was given | Height invariant asserted across a width×height grid |
| `.aux/` does not leak into commits | Self-ignoring `.gitignore`, verified in a scratch repo |
| A missing API key produces a clear message | Verified: lists every env var and the config path |
| Commands that change files or run programs ask before running | `internal/llm/tools/bash_safety_test.go`. M0.1: every audit exploit plus `\|\|`, backticks, `${`, newline, `>`, `<`, `&`, and each removed wrapper/`go` entry; `Run` prompts on `echo hi; rm -rf .` and refuses `ls && curl x`. M0.1b: mutating arguments to safe-listed commands (`go list -toolexec`, `git log --output`, `git branch -D`, `git ls-remote --upload-pack`, quoting disguises) all prompt. Caveat: `go list`/`go doc` may still fill the module cache |

**Not defensible — do not claim these.**

| Claim | Why not |
| --- | --- |
| "Aux understands how your project works and gives the model only what it needs" | Project knowledge, memory content, and the task spec reach the model since M1.1–M1.3, but the scanners are thin and nothing trims the transcript, so "only what it needs" is not true |
| "Improves every time you use it" / "the tenth task is cheaper" | Memory and skill candidates now accrue with no CLI step (M1.4), but whether that makes later tasks cheaper or better is unmeasured (P4.1). Nothing has ever been promoted to a skill |
| "The prompt is compiled, not just concatenated" | Both compilers send the full transcript. `DedupCompiler` stubs duplicate blobs and defaults to off |
| "Cheaper than opencode" | Python n=5: gap grew 19%→63% as runs were added. TypeScript n=5: p=0.06 and Aux failed 4/25 task-attempts vs 0/25. Decided: dropped until P1 is done |
| "80% test coverage" | 36.5% locally, floor 33.8. 15 packages have no test file |
| "Aux manages the agent's context" | `ContextWindow` appears only in display code. Nothing truncates, evicts, or budgets (the states that implied it were removed in M1.6) |
| "Production ready" | See the definition above |

**A standing rule.** This file has now been wrong about its own symptom ten
times — the SQLite alarm, the panic bullet, the migration item, the first-run
item, the evicted-state entry, the reconciliation line, the race-clean row,
the release-pipeline line, and on 2026-10-02 two more: "commands ask before
running" (the allowlist was never inspected for chaining) and the README's
"compiled, not concatenated" (the compilers were read for what they recorded,
not for what they sent). Every correction came from measuring or running,
never from re-reading. Treat every unmeasured claim here as a hypothesis.

The shape to watch for: claims about a *mechanism* working. Machinery looks
correct when you read it. The two newest were both machinery.

---

## P0 — Trust. Before anything ships to a stranger

Small, mechanical, and each one is currently a false statement somewhere.

### P0.4 Repo name (plan M0.5 **[HUMAN]**)

- The repository is now `kaiau00/aux-cli` (`gh repo view`, 2026-10-05),
  matching the module path and the README links. Still to do: confirm every
  URL in the tree resolves without a redirect (plan M0.5 done-when). See
  [P3.2](#p32-install-paths-that-work).

---

## P1 — Make the thesis true

The product claim is that Aux knows the project and the model benefits. Four
changes make that mechanically true. Measuring whether it *helps* is P4.

### P1.1 Send the project manifest and task spec — closed (M1.1, M1.2)

See the closed appendix. The caveat stands: for a Go project the manifest is
~36 tokens (`Languages: go`, `go build ./...`, `go test ./...`). The profile
scanners are thin, so sending it is necessary, not sufficient. What makes it
worth sending is P1.2 and P1.3.

### P1.2 Render memory content, not keys — closed (M1.3)

See the closed appendix.

### P1.3 Run validation at task end — closed (M1.4)

See the closed appendix. Not done: the impact graph's targeted-vs-broad
decision is not consulted; every profile command runs (see *Opportunistic*).

### P1.4 Wire or delete `govpolicy` — closed (M1.5, deleted)

See the closed appendix. Learned budgets can return in 0.2 with a producer and
an evaluator.

### P1.5 Make the two unwritten context states honest — closed (M1.6)

See the closed appendix.

### P1.6 Rewrite the README's "How it works" to match — closed (M1.7)

See the closed appendix.

---

## P2 — Daily-driver parity

The things that decide whether you reach for Aux or for Claude Code when you
sit down. In order of how often they bite.

### P2.1 The model catalog — closed (M2.1)

See the closed appendix. Hardcoded tables stay as the offline fallback.

### P2.2 Provider SDKs

Done: `openai-go` is v1.12.0, `anthropic-sdk-go` is v1.78.0, `mcp-go` is v1.1.1, and
`google.golang.org/genai` is v1.72.0 (see the appendix). Each streaming adapter has an offline
usage fixture. Live status-bar token checks per provider still need real keys.

### P2.3 Slash commands

Done (see the appendix). `/init`, `/compact`, `/exclude <path>`, `/help`, `/model`,
`/sessions`, and custom commands (`/user:foo`, `/project:bar`) run from the composer
through the registry that Ctrl+K uses. Typing `/` into an empty composer opens a
filtered command list. `/remember` arrives with M2.6.

### P2.4 `--continue` and `--resume` — closed (M2.4)

### P2.5 Compaction as a decision, not an ambush

Auto-compaction fires silently at 95% of the window. The page list and the
corrected meter already exist. Warn approaching the limit, show what is largest
in the window, offer exclude/pin/compact. This is the differentiator A4 reaches
for without A4's risk, and needs no benchmark to justify.

### P2.6 `/remember` and memory UX

`/remember <fact>` writes a factual memory (project scope by default). Then
make memory not a black box: `aux memory list`, delete ("forget that"),
provenance on use, export. Ship this before more memory *features*; a memory
that surprises people is worse than none. Depends on P2.3 for the slash form.

### P2.7 Text that still says the wrong thing

- The OpenAI coder prompt begins "built by OpenAI" (`prompt/coder.go:28`).
- `aux --help` opens with OpenCode's description, not Aux's.
- The instruction file is spelled `Aux.md`, `aux.md`, and `AUX.md` in the
  prompt and `defaultContextPaths`. Pick one, document it, keep the others as
  aliases.
- The dashboard URL breaks mid-address in the intro message (glamour splits at
  `127.0.0.`); print the bare origin and point at `d` in the context pane,
  which also stops the token being written into the transcript.

---

## P3 — Ship it publicly

### P3.1 Tag `v0.1.0`

There are no releases and no tags. `goreleaser check` passes and a snapshot
build produces all four archives (proven 2026-10-01), but nothing has gone
through the real `release` workflow and `./install` has never fetched an
artifact. Tag after P0 and P1.1–P1.3; a release whose README is false is
worse than no release.

### P3.2 Install paths that work

- `./install` fetches `github.com/kaiau00/aux-cli/releases/latest` — fix the
  repo name (P0.4) or the URL, then run the script on a clean machine.
- `go install github.com/kaiau00/aux-cli@latest` requires the module path to
  resolve. Test it after the rename decision.
- Homebrew tap and AUR need owned namespaces; `aux-ai` is taken. Decide whether
  to create `kaiau00/homebrew-tap` or skip both for 0.1.

### P3.3 `aux --version` for local builds

Prints `unknown` for `go build` (verified). The B2 fix made the stamped tag
win, which is right; the fallback should still surface the VCS pseudo-version
when the linker stamped nothing.

### P3.4 README rewrite

After P1 and P2.1: accurate "How it works", no performance claim, OpenCode
credited in the first screen, install section that is true, provider list that
matches what is maintained, model names current. `docs/trying-aux.md` updated
to match.

### P3.5 Coverage, deliberately

36.5% locally (2026-10-07; floor 33.8) against a stated 80%. The ratchet holds
the floor; it does not climb.
15 packages have no test file: `.` (root), `cmd`, `cmd/schema`,
`internal/format`, `internal/history`, `internal/lsp` (+`protocol`, `util`,
`watcher`), `internal/tui/{components/logs,components/util,image,util}`, and
the two test-helper packages. `internal/diff` came off this list in PR #27.
Priority by blast radius: `cmd` (every entry point), then `internal/history`
(file versions back every checkpoint), then the rest. Raise the floor as it
climbs; it is calibrated to CI, which measures ~0.6 points lower than a laptop.

---

## P4 — Evidence. After P1, not before

### P4.1 Suite breadth

`aux eval suite|gate|compare` exists and works. Python: one repository, five
tasks. TypeScript (`bench/suite-ts.json`): five tasks, p=0.06, and Aux was less
reliable (4/25 failures vs 0/25). Both were measured against a build that did
not send the manifest (P1.1), so neither measures the thesis. Re-run both
suites after P1 with n≥10 a side, same model both sides, and report the
reliability number next to the token number every time.

Cost note: `local.MiniMax-M3` resolves via `LOCAL_ENDPOINT` to a hosted API, not
local compute. A suite run is network calls on a subscription, not free.

### P4.2 Decide `--paging`'s default

`DedupCompiler` saves 47.9% on the repeated-read fixture, 0% elsewhere,
deterministically. Whether a reference stub changes model behaviour is
unmeasured and is what P4.1 answers. Flip the default only on evidence.

### P4.3 Tool-result eviction with promotion

The most valuable idea in this file and the most dangerous. Replace consumed
tool results with one-line pointers; promote facts about the project to memory
first; drop turn-local content. Plausibly 40–70% off history on long sessions.
Do not build before P4.1 can tell token savings from silent context loss.

---

## Opportunistic

Real, small, or low-confidence. None of it blocks anything.

- MCP tool curation — a 50-tool server costs ~15K tokens of definitions per
  turn and churns the cache prefix
- Smart window around the search match in `view`
- Trajectory waste detection — "you grepped this three times"
- `--budget strict` preset wiring the existing governor
- `grep` spawns `rg` per call — measure before optimizing
- **`internal/db/models.go` is stale.** Running `sqlc generate` adds 481 lines
  of structs for tables that have migrations but no queries in
  `internal/db/sql` (artifacts, checkpoints, context bindings, and others,
  reached through hand-written SQL instead). Left out of M2.4 as unrelated
  scope; it means the committed file is not what the generator produces, so
  the next person to run it gets a diff they did not ask for
- **`sessions.updated_at` and `created_at` say milliseconds and hold seconds.**
  The column comments in `20250424200609_initial.sql` claim milliseconds;
  `strftime('%s', 'now')` is whole seconds. Only a comment, but it is what
  makes two sessions touched in the same second tie
- `renderView` is O(conversation) per call; debounced to 100ms, still a
  stutter on very long sessions. Incremental rendering would fix it
- Local-build version string: shorten the pseudo-version on the splash
- Four-tier context model as a review principle: saving tokens means moving
  Tier 2 → Tier 3, not deleting Tier 2
- **Bash safe list still reads outside the project without a prompt (by
  reading, found during M0.1b).** `ls ~/.ssh` and `du ~` list names outside
  the working directory, and `printenv` prints every environment variable,
  provider API keys included, into the transcript. Nothing is written or
  sent anywhere new, but it sits awkwardly next to "Reads outside the project
  need approval", which is about the read tools, not bash
- **End-of-task validation runs every profile command.** The impact graph's
  targeted-vs-broad decision is not consulted (the agent has no `Impact`
  dependency). Fine for small repos; slow for a large test suite
- **`aux validate` keys its pass cache on HEAD only** (`cmd/validate.go`), so
  a pass recorded before uncommitted edits can be reused after them. M1.4's
  path uses commit + edited-file content; the CLI should do the same
- **`available` has had no writer since M1.2**: the manifest and task spec
  were the only pages compiled as available. The state, its context-pane group
  and `ContextPayload.AvailablePages` now describe nothing. Same treatment as
  M1.6 when convenient (found during M1.6)
- The addendum nests the profile's own `# Project profile` heading under
  `# Project`. Cosmetic
- `bashDescription()` says commands time out after 30 minutes when no timeout
  is given; `DefaultTimeout` is 1 minute (`bash.go`)
- The banned-command check now matches every word, so `grep -r curl .` or
  `ls links` is refused outright rather than prompting. Conservative by design
  (plan M0.1 guardrails); revisit if it bites

## Decisions still open

- **Memory beyond project scope.** Where does user- or org-level memory live?
  Blocks the `supersede`/`expire`/`scope` primitives in A8 (old numbering).
- **Distribution identity.** `aux-ai` is not yours. Tap and AUR under
  `kaiau00`, or neither for 0.1?
- **Windows.** Not supported; the shell tool is Unix-only. Decide whether
  that is permanent and say so in the README (it currently does).
- **Package maintainer identity** on deb/rpm is `kaiau00 <noreply>`. Change if
  that is not what public packages should carry.

## Sequencing

**P0 this week** — small changes, mostly one-file. Nothing else should merge
ahead of M0.1.

**P1.1 → P1.2 → P1.3 next**, in that order; each is testable alone, and P1.3
is where the loop closes. P1.6 lands with P1.3.

**P2.1 and P2.2 in parallel with P1** — they touch different code and the
model catalog is what makes Aux usable at all day to day.

P0 (except the M0.5 URL check), P1, P2.1, P2.2, and P2.3 are done.
See [Progress](#progress-as-of-2026-10-05).

**P3 after P1 is true.** Tag when the README is honest, not before.

**P4 only after P1.** Measuring the current build measures the wrong thing.

---

## Appendix: what has been closed

| | |
| --- | --- |
| M2.4 `--continue` and `--resume`, 2026-10-07 | `--continue`/`-C` opens the most recently updated session; `--resume`/`-r` takes an id, or opens the picker with none. Both work under `-p`. `GetMostRecentSession` filters `parent_session_id IS NULL`, so the task and title sessions a turn writes constantly are skipped, and breaks a tie on `rowid` because `updated_at` is whole seconds. Two defects found by running the binary rather than reading it: a NUL sentinel for pflag's `NoOptDefVal` printed into `--help` as `string[="\x00picker"]` and broke the column (now the typeable word `ask`), and `aux --resume abc` failed with `unknown command "abc"` because the root command has subcommands and cobra rejects every positional argument — `resumeAwareArgs` admits the one that `--resume <id>` leaves behind and still reports an unknown subcommand. Scratch repo, three seeded sessions and an invalid key: `-p "hello" --continue` left the count at 3 and put both messages in the intended session, skipping the subagent row whose `updated_at` was far newer; `--resume sess-old` put them in that one instead. Tests: `cmd/root_test.go`, the package's first — every flag spelling, the `--continue`/`--resume` conflict, task and title sessions skipped, an empty project is not an error, an unknown id is refused before the TUI starts |
| M2.3 slash commands, 2026-10-04 | `editorCmp.send` turns `/id args` into `chat.RunCommandMsg` when `id` is registered, before the busy check, so `/help` works while the agent runs. A lone unregistered `/word` shows "unknown command /word; Ctrl+K lists commands" and stays in the composer. Text such as `see /etc/hosts` or `/etc/hosts is broken` still goes to the model. `dialog.CommandRegistry` replaces the TUI's command slice and is shared with the page and editor. `Command.ArgHandler` lets `/exclude main.go` skip the arguments dialog; other commands warn "/init takes no arguments". Typing `/` into an empty composer opens a second completion dialog over `completions.NewCommandsGroup`; Tab or Enter accepts through `dialog.CompletionSelectedMsg`. Tests: `TestSlashInitRunsTheCommandAndIsNotSent`, `TestSlashExcludeCarriesItsArgument`, `TestUnknownSlashCommandWarnsAndKeepsTheText`, `TestSlashInsideTextIsNotACommand` (components/chat), `TestSlashOpensCommandCompletionAndRunsTheCommand` (page), `TestRunCommand*` and `TestHelpAndModelCommandsOpenTheirOverlays` (tui), and `TestCommandPopupGolden` (`internal/completions/testdata/command-popup.*.golden`) |
| M2.2 genai v1.72.0, 2026-10-03 | `go get google.golang.org/genai@v1.72.0`. Gemini and Vertex compiled unchanged. `TestGeminiStreamReportsCachedTokens` plays a recorded `streamGenerateContent` SSE response: text `ok`, then a final chunk with `promptTokenCount` 20, `candidatesTokenCount` 3, `cachedContentTokenCount` 15. The completion reports 20 input, 3 output, 15 cache read. Prompt tokens already include the cached content, so input is not reduced by the cache count |
| M2.2 mcp-go v1.1.1, 2026-10-03 | `go get github.com/mark3labs/mcp-go@v1.1.1`. The stdio and SSE call sites compiled unchanged, including `InputSchema.Properties`/`Required` and `mcp.TextContent`. The module requires Go 1.25.5, so `go.mod`'s `go` line moved from 1.24.0; CI reads that line. `TestGetMcpToolsWithoutConfigDoesNotPanic`, `TestParseGraphResponse`, and `TestParseGraphResponseSupportsAlternateKeys` pass. MCP does not stream model tokens, so there is no usage fixture |
| M2.2 anthropic-sdk-go v1.78.0, 2026-10-03 | `go get github.com/anthropics/anthropic-sdk-go@v1.78.0`. The adapter compiled unchanged. `TestAnthropicStreamReportsCacheTokens` plays a recorded Messages stream: message_start reports 10 input, 2 cache creation, 100 cache read; message_delta replaces output with 4. The completion reports those four counts, and the text delta is `ok` |
| M2.2 openai-go v1.12.0, 2026-10-03 | `go get github.com/openai/openai-go@v1.12.0` (the `@latest` of this module path; v2 and v3 are different modules). Request extra fields moved from `WithExtraFields` to `SetExtraFields`; response extra fields use `Field.Valid` instead of `IsPresent`. The stream accumulator still copies only prompt and completion totals, not `prompt_tokens_details`. `TestOpenAIStreamReportsCachedTokensFromTheChunk` plays a recorded SSE fixture (2179 prompt, 2178 cached, 8 completion) and the completion reports 1 fresh input, 2178 cache read, 8 output. Copilot and Azure share this module and still build |
| M0.1b safe-list argument rules, 2026-10-03 | Each safe-list entry has an argument rule: dangerous `git` long options refused with their abbreviations, `-O`/`-f` short flags refused, `git branch`/`tag` listing flags only, `git remote` bare/`-v`/`show`/`get-url`, `go list`/`go env` listed flags only, `date`/`hostname` display only. Quotes, backslashes, braces and `$` force a prompt — each was measured disguising `--output`. 44 rejections and 32 kept read-only forms in `bash_safety_test.go` |
| M0.1 bash safe-list bypass, 2026-10-03 | `isSafeReadOnly` refuses the fast path on any shell operator; wrappers (`env`, `timeout`, `nohup`, `nice`, `time`, `kill`, `killall`, `set`, `unset`, `top`) and code-executing `go` subcommands removed; banned check runs on every word. `bash_safety_test.go`: 29 bypasses rejected, 7 read-only commands kept, `Run` prompts with the full command as fingerprint |
| Audit, 2026-10-02 | Outside read of `278b9f1`: build, vet, `-race` suite, both gates, CLI in a scratch repo, agent loop and compiler traced end to end. Found: manifest never sent, memory renders keys, learning loop has no automatic producer, bash allowlist bypass, catalog 17 months stale, slash commands inert, no resume flag, beta OpenAI SDK. Confirmed every mechanical claim in the previous claims table except "commands ask before running" |
| Task benchmark | `internal/evalsuite`, pinned revisions, command-decided success, exact two-sided rank test. Repetition enforced |
| SQLite concurrency | Pragmas per connection, bounded pool, failures verified at startup |
| Reachability audit | `scripts/deadcode.sh` ratchets against a baseline. 48 accepted entries |
| Coverage gate | Ratchets against `.coverage-floor`, calibrated to CI |
| Cached-token accounting | Streaming accumulator dropped `prompt_tokens_details`; cost was overstated several-fold |
| Validation fail-closed | A dropped evidence write could report a failed criterion as validated |
| Deterministic context order | Goroutine completion order was leaking into the cacheable prefix |
| Panic teardown | A panic wedged the session permanently and silently |
| Upgrade safety | Tested from every recorded version; newer database refused |
| Silent failures | 137 sites triaged; three misreported state |
| First run | `agent coder not found` replaced with a real message; `.aux/` self-ignoring |
| pubsub race | `Publish` sent on closed channels; a 2026-07-04 crash log recorded it |
| Module identity | `github.com/aux-ai/aux-cli` resolved to nothing; renamed |
| Install instructions | Every method in the README was fictional; now says build from source |
| Package attribution | Upstream author's personal address removed from shipped files |
| Skill promotion path (PR #28), 2026-10-02 | Outside tests, `skill.Service.Evaluate` and `Promote` had no callers, so no skill could be promoted. The CLI now exposes the lifecycle it already implemented: `aux skill evaluate <id> --result pass\|fail\|inconclusive` (`--baseline`, `--eval-run`, `--metrics`), `aux skill promote`, `aux skill rollback`; `skill list` shows ids and which candidates are promotable. Found by exercising it: rolled-back skills appeared in no list (`Service.RolledBack` fixes it), and `--result Pass` would have been stored but never unlocked promotion (`ParseEvalResult` rejects it). The result still comes from a run done elsewhere; `deadcode.sh` could not have caught the gap, since a constructed-but-never-invoked service looks reachable |
| M2.1 model catalog, 2026-10-03 | `agents.<name>.model` accepts a hardcoded id or `<provider>/<api-model-id>`. Resolution is hardcoded map, then the models.dev catalog (cached 24h at `$XDG_CACHE_HOME/aux/models.json`, 2s startup budget). No configured coder model: newest non-preview tool-calling catalog model is written to `~/.aux.json` and announced; title and summarizer get the cheapest priced tool-calling model. A catalog entry with no `cost` stays `CostUnknown` (zero rates already mean that, except local/mock). Groq, Azure, Bedrock, Vertex, Copilot, and xAI are marked unmaintained in the picker and README. Startup applies the theme in memory; a theme or model change patches that key instead of remarshaling `Config`, which had been replacing the file just written. Tests: `catalog_test.go` (fixture id resolves, default is newest-then-largest, offline fallback is `claude-4-sonnet`), `config/model_test.go` (`anthropic/fixture-sonnet` validates; a missing id lists catalog ids; a theme patch keeps the picked model). Temp home, only `ANTHROPIC_API_KEY=sk-test`: stderr `Using Claude Sonnet 5.5 (newest for anthropic). Change with Ctrl+O or agents.coder.model.`; `~/.aux.json` coder and task `anthropic/claude-sonnet-5-5`, title and summarizer `anthropic/claude-haiku-4-5-20251001`. Second run printed nothing and left the file byte-identical |
| M1.6 context states, 2026-10-03 | `StateEvicted` and `StateFaulted` removed from `contextstore`, the view model, and the TUI's expanded context view; ADR 0006 amended. The golden fixture showed an "Evicted — demand paging" page and an `available` project manifest, both fiction; it now shows the manifest resident. `rg 'StateEvicted\|StateFaulted'` matches only the plan |
| M1.5 `govpolicy` deleted, 2026-10-03 | No producer, no evaluator, no non-test caller. Removed the package, its app wiring, the dashboard "Governed-cost policies" panel and view-model fields, and policies from bundles (format version 2, so a version-1 bundle is refused by version rather than misreported as tampered). The two tables stay and are noted in ADR 0003. `rg govpolicy` matches only the plan, this file, and the ADR note |
| M1.4 validation at task end, 2026-10-03 | `agent.validateTaskIfNeeded` runs before the deferred `Finish`: skips (with `validation.skipped{reason}`) when `validation.auto` is off, there are no criteria, no file version was recorded during the task, or the profile has no commands, and inside subagents. Otherwise plans as `aux validate` does, runs each command through `validation.ShellRunner` with the session's permission service, and appends the results to the final message. The pass cache is keyed on commit + edited-file content. A denied command is now a `skipped` run with no evidence (before, it was recorded `failed` and blocked every criterion, also via `aux validate` without `--yes`). `aux task show` reports proof of done from evidence instead of the compile-time state. Tests in `agent/validate_test.go` (changed → validated + memory + skill; no change → skipped; denied → skipped, no memory; subagent → not run) and `validation_test.go`. Scratch repo, real provider, `--yes`: both commands passed, both criteria `validated`, two procedural memories, one skill candidate |
| M1.7 README "How it works", 2026-10-03 | Steps 3–6 rewritten to what M1.1–M1.4 do; tagline performance claim removed (D13); "demand paging" removed from README and `--paging` help |
| M1.3 memory content, 2026-10-03 | `memorySection` rendered `[episodic] episode:<task-id>`, the stable key. It now loads each active memory's latest version (`Store.LatestVersion`, `Service.RetrieveWithContent`) and renders one line per type: the fact; `` `command` `` — validated in N task(s) since <rev>; "Earlier task: objective → outcome (changed …)". Newest first, bounded at 600 estimated tokens rather than 5 rows. Tests: one memory of each type renders content and no keys, a stale memory is left out, 50 memories stay under budget. "Since <rev>" not "last at": re-validation reuses the same version, so its revision is when the command was first recorded |
| M1.2 addendum pages resident, 2026-10-03 | `project_manifest` and `task_spec` pages are `resident` (reasons "project knowledge", "compiled task spec") and carry exactly the text sent, headings included, so resident page tokens reconcile with `EstimatedTokens` again. Scratch repo, real provider: `context.compiled` payload `residentPages: 3`, no available pages; bindings show `project_manifest` resident (36 tokens) and `task_spec` resident (85); the model answered "go build ./... / go test ./..." with tools forbidden |
| M1.1 system addendum, 2026-10-03 | Both compilers render `# Project` (manifest + memory + related projects) then `# Task` into `CompiledPrompt.SystemAddendum`, counted in `EstimatedTokens`. The agent attaches it to the provider call's context only; Anthropic sends it as a second system block after the cached base, OpenAI/OpenRouter/local/Copilot/Gemini append it to the system message (Azure, Bedrock, Vertex inherit). Tests: wire requests captured by an `httptest` server for Anthropic and OpenAI, compiler order/estimate/determinism, and an agent turn through the mock provider that also proves tools do not inherit it |
| M0.3 coverage floor, 2026-10-03 | `.coverage-floor` 30.8 → 33.8, the value `scripts/coverage.sh` reported on CI for `main` at `8707a1a` (after #28) |
| M0.4 repo hygiene, 2026-10-03 | `.claude/settings.json` (a dev-session Claude Code bash allowlist) untracked; `.claude/` and `.kilo/` ignored (`.codebase-memory/` already was). `git ls-tree -r HEAD --name-only \| rg '^\.claude\|codebase-memory\|\.kilo'` empty; `git ls-files \| rg '\.log$'` empty |
| Title/turn lost update | Title generation saved a stale session over the turn's totals |
| Dashboard disclosure | Handoff note said "no server" while one started by default |
| Terminal layout | Rendered more rows than the terminal had at nearly every size |
| Model name | `friendlyModelName` ate the end of the ID |
| Font coverage | 12 of 32 glyphs absent from SF Mono; `TestIconsAreFontSafe` |
| Context meter | Summed lifetime spend against the window; auto-compaction fired on it |
| User-defined hooks | Dropped: a config file naming commands turns cloning into executing |
| Eval suite isolation | `.aux` survived `git clean`; tasks inherited each other's databases. Then the held connection read a deleted file; metrics reconnect per read |
| Context pane budget | Relabelled "Pages"; divided by the call's real total, not the model window |
| Trackpad scrolling | Streaming fought scroll-up back to the bottom; full re-render per delta. Debounced, bottom-sticky only when already there |
| Release pipeline | No usable token, deprecated goreleaser keys, version stamping lost to build info. All three fixed and a snapshot proven; a real tag still has not run |
| Permission grant ordering | `GrantPersistant` woke the waiter before recording; two parallel calls could prompt twice. Pinned deterministically |
| `internal/diff` | 1,481 lines mutating files with no tests; PR #27 added them and fixed a panic on a chunk deleting past EOF |
| M0.2 `-p` requires `--yes`, 2026-10-03 | `-p` auto-approved every request (`app.go:430`). Now `DenyAllSession` refuses without blocking and records each denial; `--yes` (hidden alias `--dangerously-skip-permissions`) restores approve-all. Subagent sessions follow the parent via `LinkSession` — before, a subagent prompt under `-p` had no one to answer it and waited forever. Eval harness passes `--yes`. Tests in `internal/app`, `internal/permission`, `internal/evalsuite`; scratch-repo run recorded in the claims table |

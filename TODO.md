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

## Where this actually stands

Aux is a fork of OpenCode's Go agent loop with a serious observability layer
built around it: a per-call ledger, an append-only event store, page bindings,
checkpoints, an impact graph, a project profile, memory, skills, validation,
and a read-only dashboard. The engineering discipline is real — ADRs, three
ratcheting CI gates, a race-clean suite — and the audit confirmed every
mechanical claim it tested (see [Claims](#claims-what-is-defensible-today)).

What the audit also found is that **the product thesis is not wired in**:

- The compiled project manifest, memory, related-project context, and task
  spec are computed on every turn and **never sent to the model**. Both prompt
  compilers mark them `available` and send the bare transcript
  (`internal/promptcompiler/compiler.go:109-132`, `dedup.go:30-67`).
- The learning loop only learns from validation evidence, and nothing in the
  agent loop produces validation evidence — only `aux validate <task-id>` does
  (`cmd/validate.go`). After a normal session: no procedural memory, no skill
  candidates, one episodic memory holding `{objective, mode, outcome}`.
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
| Coverage cannot regress | `scripts/coverage.sh`: 33.8% against a 30.8% floor |
| Upgrades across schema versions work | Tested from every recorded version with a populated database |
| A database from a newer build is refused | `ensureNotNewer`, tested |
| Reads outside the project need approval | `RequireReadAccess` canonicalizes through symlinks and fails closed |
| Dashboard is loopback-only and token-gated | Random token, constant-time compare, every data route |
| Sessions survive a panic | Deferred teardown, tested both directions |
| The context meter reflects what the window holds | Latest call's occupancy from the ledger |
| The TUI fits the terminal it was given | Height invariant asserted across a width×height grid |
| `.aux/` does not leak into commits | Self-ignoring `.gitignore`, verified in a scratch repo |
| A missing API key produces a clear message | Verified: lists every env var and the config path |
| Commands that change files or run programs ask before running | `internal/llm/tools/bash_safety_test.go`. M0.1: every audit exploit plus `\|\|`, backticks, `${`, newline, `>`, `<`, `&`, and each removed wrapper/`go` entry; `Run` prompts on `echo hi; rm -rf .` and refuses `ls && curl x`. M0.1b: mutating arguments to safe-listed commands (`go list -toolexec`, `git log --output`, `git branch -D`, `git ls-remote --upload-pack`, quoting disguises) all prompt. Caveat: `go list`/`go doc` may still fill the module cache |

**Not defensible — do not claim these.**

| Claim | Why not |
| --- | --- |
| "Aux understands how your project works and gives the model only what it needs" | Nothing the profile, memory, or task compiler produces reaches the model. See [P1.1](#p11-send-the-project-manifest-and-task-spec) |
| "Improves every time you use it" / "the tenth task is cheaper" | No automatic evidence producer; see [P1.3](#p13-run-validation-at-task-end). Nothing has ever been promoted to a skill |
| "The prompt is compiled, not just concatenated" | Both compilers send the full transcript. `DedupCompiler` stubs duplicate blobs and defaults to off |
| "Cheaper than opencode" | Python n=5: gap grew 19%→63% as runs were added. TypeScript n=5: p=0.06 and Aux failed 4/25 task-attempts vs 0/25. Decided: dropped until P1 is done |
| "80% test coverage" | 33.8%. 15 packages have no test file |
| "Aux manages the agent's context" | `ContextWindow` appears only in display code. Nothing truncates, evicts, or budgets. `evicted`/`faulted` are written nowhere |
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

### P0.2 Say what `-p` does, where it is seen

`aux -p` calls `AutoApproveSession` (`internal/app/app.go:430`). The README
mentions it once, `docs/trying-aux.md` leads with it — but `aux --help` and
the `-p` flag text say nothing. Put it in the flag help and in the first line
`-p` prints when not `--quiet`.

### P0.3 Merge PR #28

Small, tested, CI green, description accurate. Closes the "no skill can ever be
promoted" dead end by exposing `evaluate`/`promote`/`rollback`. Raise
`.coverage-floor` to 33.8 in the same change or the next.

### P0.4 Repo hygiene

- `.claude/settings.json` is committed: a Claude Code bash allowlist from a
  development session, including `Bash(go run *)` and `Bash(git commit *)`.
  Remove it; add `.claude/` to `.gitignore`.
- `.codebase-memory/` (from `codebase-memory-mcp`) sits untracked in the
  working tree. Ignore it.
- The remote is `kaiau00/Aux`; the module path and every README link say
  `kaiau00/aux-cli`. `gh` follows the redirect; `go install` and the install
  script's `releases/latest` URL may not. Pick one name. See [P3.2](#p32-install-paths-that-work).

---

## P1 — Make the thesis true

The product claim is that Aux knows the project and the model benefits. Four
changes make that mechanically true. Measuring whether it *helps* is P4.

### P1.1 Send the project manifest and task spec

`task.Coordinator.beginWithParent` (`coordinator.go:214-215`) builds
`eff.Manifest + memorySection + relatedSection` and the rendered task spec and
puts them on the context. `agent.streamAndHandleEvents` (`agent.go:466`) reads
them and passes them to `Compile`. `decomposePages` records them as `available`
pages and `Compile` returns `Messages: msgs` — the transcript only.

Fix: render manifest and task spec into the system prompt (or a leading system
section the provider adapters already support), mark those pages `resident`,
and count them in `EstimatedTokens`. Keep it deterministic: this text goes into
the cached prefix, so the order must be stable (the same lesson as
`processContextPaths`).

Caveat worth knowing before measuring: for a Go project the manifest is ~34
tokens — `Languages: go`, `go build ./...`, `go test ./...`. The profile
scanners are thin. Sending it is necessary, not sufficient. What makes the
manifest worth sending is P1.2 and P1.3.

Done when: a test asserts the compiled messages contain the manifest text and
the `project_manifest` page is `resident`; a real session's `ContextCompiled`
event shows `ResidentPages` including it.

### P1.2 Render memory content, not keys

`coordinator.memorySection` (`coordinator.go:273-288`) writes
`[episodic] episode:<task-id>` — the stable key. The content
(`{objective, mode, outcome, changedPaths}` or `{command, purpose}`) is in the
version row and never read here. Zero information even once P1.1 ships.

Fix: load the latest version per memory, render a one-line summary per type
(episodic: objective → outcome; procedural: the command and when it was last
validated; factual: the fact), bound the total by tokens not count.

### P1.3 Run validation at task end

Decided: the agent runs validation itself. Today `validation.Service.RunIntent`
is called only from `cmd/validate.go` and `cmd/eval.go`. `coordinator.Finish`
reads `SuccessfulCommands`, which is therefore always empty in the TUI.

Fix: in `Finish` (or a step the agent takes before yielding), plan intents from
the effective profile's validation commands against the task's acceptance
criteria (the same code `cmd/validate.go:77-90` already runs), execute them
through the permission service with a fingerprint per command, record evidence,
and only then extract memory and skills. Respect the impact graph's
targeted-vs-broad decision. Make it skippable per task and configurable off.

This is what turns "validated commands become procedural memory" from a
sentence into a path that runs. It also closes A5 properly: skill candidates
will exist without a human running three CLI commands.

Done when: a scratch-repo session that edits a file ends with a
`validation.run` event, a procedural memory, and a skill candidate, with no CLI
step.

### P1.4 Wire or delete `govpolicy`

`Policies.Evaluate` and `Policies.Promote` have no non-test callers (PR #28
confirmed). The cost governor defaults `off`, and `on` only stops at a fixed
`DefaultBudget(ModeBalanced)`. Either make learned policies feed the governor's
budget, or delete the package and its dashboard panel. An evaluation-gated
pipeline with no producer and no consumer is the exact shape
`scripts/deadcode.sh` cannot see.

### P1.5 Make the two unwritten context states honest

`contextstore` has five binding states; `evicted` and `faulted` are read and
written nowhere. Both state models now say so in comments. Either implement
A4-style eviction (after P4.1 can measure it) or remove the two states so the
dashboard's "evicted" group stops implying a mechanism.

### P1.6 Rewrite the README's "How it works" to match

Once P1.1–P1.3 land, the section is true. Until then, it isn't — and it is the
first thing a stranger reads. Do this in the same PR as P1.3, not after.

---

## P2 — Daily-driver parity

The things that decide whether you reach for Aux or for Claude Code when you
sit down. In order of how often they bite.

### P2.1 The model catalog

Hardcoded; the newest Anthropic entry is `claude-sonnet-4-20250514`
(`internal/llm/models/anthropic.go:88`), seventeen months old at the time of
the audit. `models.SupportedModels[agentConfig.Model]` must match
(`agent.go:1182`, `config.go:631`), so a newer model ID is unreachable except
through `LOCAL_ENDPOINT`. For a product whose pitch is "pick one model" this is
the most user-hostile gap in the tree.

Fix, in order of leverage:

1. Let `agents.<name>.model` accept any API model ID for a configured
   provider, with limits looked up from models.dev (`catalog.go` already fetches
   it for context limits) and a sane fallback when the lookup fails.
2. Refresh the hardcoded entries for the five providers that matter.
3. Drop or stop maintaining Groq, Azure, Bedrock, Vertex, Copilot, xAI, per
   the decision above. If they stay, say "unmaintained" in the README.

### P2.2 Provider SDKs

`openai-go v0.1.0-beta.2` (current: 1.12), `anthropic-sdk-go v1.4.0` (current:
1.78), `mcp-go v0.17.0` (current: 1.1), `google.golang.org/genai v1.3.0`
(current: 1.72). New model parameters, tool-schema changes, and streaming fixes
all live in the gap. The OpenAI dependency is a *beta*. Bump all four; the
suite is offline and deterministic so this is cheap to verify.

### P2.3 Slash commands

`isSlashCommand` (`composer.go:73`) changes the placeholder to "Run a command…".
On Enter, `editorCmp.send` forwards the text to the model unchanged. `/init`
is sent as the literal string `/init`. Dispatch `/`-prefixed input to the
command registry that Ctrl+K already uses (`tui.go:989-1021`), with completion.
The placeholder is currently a lie; fixing the placeholder alone is not the fix.

### P2.4 `--continue` and `--resume`

No flag resumes a session. `-c` is `--cwd`. Add `--continue` (most recent
session in this project) and `--resume <id|picker>`, matching what people
expect from the tool they are comparing Aux to.

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

33.8% against a stated 80%. The ratchet holds the floor; it does not climb.
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

**P3 after P1 is true.** Tag when the README is honest, not before.

**P4 only after P1.** Measuring the current build measures the wrong thing.

---

## Appendix: what has been closed

| | |
| --- | --- |
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

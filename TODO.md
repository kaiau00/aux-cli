# Aux — the master list

Everything that stands between Aux and being something a stranger can depend on.
Rewritten 2026-08-23, replacing `roadmap.md` and `roadmapplan.md` (both deleted;
their content is in git history at `859109b~1` if it is ever wanted).

Two tracks: **[Track A](#track-a--an-agent-can-do-these)** is work an AI agent can
do unattended. **[Track B](#track-b--only-you-can-do-these)** is work that needs a
human, either because it needs another person or because it is a decision about
what this product is.

---

## Where this actually stands

A strong late alpha. The architecture is coherent, the security posture is real,
the suite is race-clean, and three mechanical gates (tests, reachability,
coverage) run on every push.

It is **not production ready**, and the remaining gap is mostly *evidence*, not
code.

## Definition of done

**Someone who is not the author installs Aux, uses it on their own repository
for a week, and depends on the result.**

Concretely: Track B item 1 done, plus a soak in which outside findings taper
off. That second half is not padding. A checklist written by the person who
wrote the code cannot enumerate what they cannot see, so the signal is not "the
list is empty" — it is **the list growing slower than it is closed**.

## Claims: what is defensible today

The point of this section is that Aux should only say true things about itself.

**Defensible now.**

| Claim | Backing |
| --- | --- |
| Race-clean under `-race` | Full suite, every push |
| No unreachable code outside a declared baseline | `scripts/deadcode.sh`, 49 accepted entries with reasons |
| Coverage cannot regress | `scripts/coverage.sh` against `.coverage-floor` |
| Upgrades across schema versions work | Tested from every recorded version with a populated database |
| A database from a newer build is refused | `ensureNotNewer`, tested |
| Commands ask before running | Permission service, fingerprinted grants |
| Sessions survive a panic | Deferred teardown, tested both directions |

**Not defensible — do not claim these.**

| Claim | Why not |
| --- | --- |
| "Cheaper than opencode" | One repository, five Python tasks, one model. The gap **grew from 19% to 63% when runs were added**, so n=5 may still be too few, and Aux's own spread is 46%. Directionally encouraging, nowhere near a general claim |
| "80% test coverage" | Actual coverage is **32.6%**. 19 of 74 packages have no test file at all |
| "Demand paging saves tokens" | `--paging` defaults to off because nothing has shown it lossless. The one measurement suggesting −26% was inside the noise floor and is void |
| "Aux manages the agent's context" | It does not. `ContextWindow` appears only in display code — nothing truncates, evicts, or budgets. `StateEvicted` is written nowhere. The compiler sends the full history. What actually ships is context *observability* plus manual exclude/pin, which is worth claiming and is not this |
| "Production ready" | See the definition above |

**A standing rule.** Five times now, an item in this file has been wrong about
its own symptom — the SQLite alarm, the panic bullet, the migration item, the
first-run item, the evicted-state entry in A4. Every correction came from measuring or from running the binary,
never from re-reading the file. Treat every unmeasured claim here, including the
confident-sounding ones, as a hypothesis.

---

## Track A — an agent can do these

Ordered by value. Nothing here needs a human decision first.

### A1. Suite breadth — the biggest blocker to a legitimate claim

The benchmark exists and works (`aux eval suite|gate|compare`, `--repeat`, exact
two-sided rank test). It covers one repository, five small Python tasks, one
model. Nothing in it generalises to Go or TypeScript work.

Until a second repository in another language exists, the performance comparison
cannot be stated publicly as more than a single-repository result. **This is the
one item that converts an inadmissible claim into an admissible one.**

Needs: a scratch checkout of a well-tested public repo in another language,
five tasks whose success is decided by that project's own test suite, n≥5 a side.

Note before running: `aux -p` calls `AutoApproveSession`, so non-interactive runs
bypass every permission prompt. Benchmark repositories must be scratch checkouts.

### A2. Coverage, deliberately

32.6% against a stated bar of 80%. The floor ratchet stops it regressing but
does not close the gap. 19 packages have no test file:

`cmd`, `cmd/schema`, `internal/diff`, `internal/format`, `internal/history`,
`internal/lsp` (+`protocol`, `util`, `watcher`), `internal/session`,
`internal/tui` (+`components/logs`, `components/util`, `image`, `util`),
`internal/version`, and the two test-helper packages.

Priority order by blast radius: `internal/session` and `internal/diff` first
(data loss and file mutation), then `cmd` (every entry point), then the rest.
Raise `.coverage-floor` as it climbs — that is the ratchet's whole purpose.

### A3. Decide demand paging's default

`--paging` exists, defaults to off, and the machinery to settle it now exists.
Runs are cheap on a local model. Just needs an adequate n and an honest verdict,
including "no measurable difference" if that is the answer.

Blocked on A1 only in the sense that deciding it against one repository would be
Goodhart. Running it is unblocked.

Two things changed here. The occupancy meter is now honest, so a run can be read
against what the window actually holds rather than against lifetime spend. And
`PagingCompiler` is misnamed: it does not page, it replaces an identical earlier
copy of a large blob with a reference to the later one. That is lossless by
construction, which makes it the cheapest thing in this file to justify turning
on — and a type whose doc comment calls itself "the first demand-paging
compiler" is how an untrue README claim gets born. Rename it `DedupCompiler`.

### A4. Tool-result eviction with promotion

The most valuable idea in this file and the most dangerous. When a tool result
has been consumed, replace its content with a one-line pointer
(`// file.go (1.2K lines) — read, no action needed`), leaving retrieval able to
re-pull it. Plausibly 40–70% off history size on long sessions.

The pairing that makes it safe: eviction must ask **"is this a fact about the
project, or about this turn?"** Facts get promoted to memory; turn-local content
is dropped. Blind eviction is how an agent forgets what it was told.

Do not build this before A1 can measure it, or there is no way to tell token
savings from silent context loss.

Two of contextstore's five binding states, `evicted` and `faulted`, are read and
written nowhere — nothing evicts because nothing enforces a budget. The UI turns
out to be innocent here: `RenderExpanded` skips empty groups, so neither heading
ever reaches the screen. (An earlier draft of this entry claimed it rendered an
empty section. It does not. Five, now.) What was actually untrue was a comment
in `viewmodel/build.go` calling all five states backed rather than aspirational.
Both state models now say plainly which two have no writer.

### A5. Skill promotion path

Candidates are produced automatically from validated commands, and promotion
correctly requires a passing evaluation. Nothing can currently produce that
evaluation, so **no skill can ever be promoted** — the pipeline terminates one
step short. A1 supplies the missing evidence.

Keep the risk note in force: a wrong skill is worse than no skill, and one that
fires on every task is the bad outcome to design against.

### A6. `/remember`

`/remember always use the new auth API, not legacy` writes to `.aux/memory.md`
(project) or `~/.aux/memory.md` (user), loaded next session as Project Brain
input. Default project scope; explicit opt-in for user scope. Closes the "the
agent forgets what I told it" complaint, and it is small.

### A7. Memory UX

Ship before more memory features, not after. Without it memory is a black box
that surprises people, which is worse than no memory:

- **Discoverable** — plain markdown, plus `aux memory list`
- **Editable** — by hand
- **Deletable** — "forget that", "forget session X", "wipe everything"
- **Provenance visible** — when the agent acts on a memory, show which line
- **Portable** — export, import, sync

### A8. Memory primitive gaps

Most of the original list exists (`promote`, `confidence`, `provenance`,
project/task scope, revision-based invalidation). Genuinely missing:

| Primitive | Gap |
| --- | --- |
| `supersede` | versions exist, no explicit supersede |
| `expire` | revision-based only, no date-based expiry |
| `scope` | project/task only — user and org scope needs **[B4](#b4-product-decisions-an-agent-should-not-make-for-you)** |

### A9. Opportunistic

Real, small, or low-confidence. None of it blocks anything.

- **MCP tool curation** — a 50-tool MCP server costs ~15K tokens of definitions
  per turn. Interacts directly with cache stability
- **Smart window around the search match in `view`** (the bash half shipped)
- **Trajectory waste detection** — "you grepped this three times"
- **Cost-aware tool selection** — suggest `--testPathPattern` when budget is low
- **Differential inclusion for edits** — send diffs, not whole files
- **`--budget strict` preset**, wiring existing governor policies
- **PageRank as file-inclusion oracle** — needs a <50ms p99 retriever with
  per-file invalidation first
- **Correction detection** — "propose, never auto-write" remains right; the
  heuristics are ~70% unreliable at telling one-shot from durable
- **`grep` spawns `rg` per call** — measure before optimizing
- **Some turns never reconcile the session** — two top-level sessions in a real
  database hold a completed ~21K call but read `prompt_tokens = 0, cost = 0`.
  Invisible on a local model; under-reports spend on a paid one
- **Four-tier context model** — a principle for reviewing the above, not a task:
  saving tokens means moving Tier 2 → Tier 3, not deleting Tier 2

### A10. Make compaction a decision, not an ambush

Auto-compaction fires silently at 95% of the window. It is the single worst
moment in every agent tool: the thread is lost and the user finds out
afterwards. The page list needed to do better already exists, and the meter
driving it is now correct.

Warn approaching the limit, say **what is largest in the window**, and let the
user drop specific pages or compact deliberately. This is the differentiator A4
is reaching for, without A4's risk of silently discarding something the agent
needed — and unlike eviction, it needs no evidence from A1 to justify.

---

## Track B — only you can do these

### B1. Get it in front of someone else — the only real blocker

Nothing in Track A substitutes for this, and everything in Track A is written by
the same process that keeps being wrong about its own symptoms.

Priority order:

1. **The security surface** — permission fingerprinting, read confinement, the
   dashboard's auth. All of it is weeks old and has never met an adversary
2. **The agent loop** — `RunTurn`, the coalescing write buffer, parallel tool
   execution
3. **One real user on a real repository for a week**

[docs/trying-aux.md](docs/trying-aux.md) exists so this costs ten minutes: it
covers install, what to try, what to report, and leads with the two things that
should never surprise anyone — that `-p` auto-approves every permission, and
that `.aux/` holds their full transcript inside their repository.

### B2. Tag a first release

There are no releases. `goreleaser release` now works (archives, checksums,
deb/rpm) since the unusable Homebrew and AUR blocks were removed. Until a tag
exists, the install script has nothing to download and build-from-source is the
only path.

### B3. Distribution identity

`.goreleaser.yml` no longer publishes to a Homebrew tap or AUR. Re-adding either
means creating and owning them. The `aux-ai` organisation is **not yours** —
registered by someone else in 2021 — so anything under that name is unavailable.

Also note the package maintainer is now `kaiau00 <258971420+kaiau00@users.noreply.github.com>`;
change it if that is not the identity you want on public packages.

### B4. Product decisions an agent should not make for you

- **Memory user/org scope** (blocks part of [A8](#a8-memory-primitive-gaps)) —
  where does memory live when it is not project-local?
- **Whether the performance claim matters enough** to fund [A1](#a1-suite-breadth--the-biggest-blocker-to-a-legitimate-claim)'s
  benchmark runs
- **Whether to credit the upstream project in the README.** `LICENSE` keeps
  `Copyright (c) 2025 Kujtim Hoxha`, which is what MIT requires, but nothing
  user-facing says Aux is derived from that work. Not a legal question; a
  question about how you want to present it

---

## Sequencing

**Start B1 today.** It is a week of wall-clock that has not started, nothing in
Track A shortens it, and every day it does not start is a day added to the end.

Track A in parallel, in order: **A1** (it is what makes a claim admissible), then
**A2**, then **A3**. Do not start A4 before A1 can measure it.

Everything else waits until outside findings start arriving, because those will
reorder this list — which is the point of B1.

## Appendix: what has been closed

| | |
| --- | --- |
| Task benchmark | `internal/evalsuite`, pinned revisions, command-decided success, exact two-sided rank test. Repetition enforced: three runs a side cannot reach p≤0.05 and the tool says so |
| SQLite concurrency | Mostly a false alarm, recorded rather than deleted. `synchronous`/`cache_size` were genuinely per-connection; the pool was unbounded; pragma failures were ignored and are now verified at startup |
| Reachability audit | `scripts/deadcode.sh` ratchets against a baseline. Found a complete unwired onboarding flow in seconds. All 23 debt entries now resolved; 49 accepted remain |
| Coverage gate | Ratchets against `.coverage-floor`, calibrated to CI rather than a laptop |
| Cached-token accounting | The dependency's streaming accumulator dropped `prompt_tokens_details`, so 150 turns recorded zero cache reads and cost was overstated several-fold — with the governor stopping work against the inflated figure |
| Validation fail-closed | A dropped evidence write could report a **failed** criterion as *Validated* |
| Deterministic context order | Goroutine completion order was leaking into the cacheable prompt prefix |
| Panic teardown | Recovery existed; it skipped the session's busy marker, the context and the channel, so a panic wedged the session permanently and silently |
| Upgrade safety | Tested from every recorded version with a populated database; a newer database is refused instead of silently accepted |
| Silent failures | 137 sites triaged; three were misreporting state (`finishMessage`, `scanVersionOpt`, `Reindex`). The rest annotated with why discarding is correct |
| First run | `agent coder not found` for a missing API key, plus a usage dump. And `.aux/` sat uncommitted in the user's repository holding their full transcript — now self-ignoring |
| pubsub race | `Publish` sent on channels a concurrent unsubscribe had closed. A crash log from 2026-07-04 in the repository root had recorded it happening |
| Module identity | `github.com/aux-ai/aux-cli` resolved to nothing. Renamed with the repository. Go rejects `aux` as a path element outright — a reserved Windows device name |
| Install instructions | Every method in the README was fictional |
| Package attribution | Named the upstream author, with his personal address, in files meant to ship to strangers |
| Terminal layout | The screen rendered more rows than the terminal had at nearly every size — three too many at 40 columns, one too many even at 200x20 — so the alt screen scrolled and the task header left the top for good. Hint lines rendered at a fixed width wrap rather than clip; two of them said the same thing; and a 90/10 vertical split starves the composer below 24 rows |
| Model name | `friendlyModelName` let a trailing `.*` eat the end of the ID. `MiniMax-M3` displayed as "MiniMax M", `kimi-k2` as "Kimi K", `Qwen2.5-Coder-32B-Instruct` as "Qwen2" — always the part that says which model it is |
| Font coverage | 12 of 32 non-ASCII glyphs were absent from SF Mono, the default font of the default macOS terminal, the `⌬` logo among them and absent from Menlo too. A substituted glyph need not honour the cell grid, and an emoji-presentation codepoint renders double-width while the layout counts one. `TestIconsAreFontSafe` now walks the tree |
| Context meter | "Context X/Y" summed lifetime spend against the context window, counting the resident conversation once per turn: seven turns over 21K read 148.3K. Auto-compaction fires off the same number, so a long session summarised itself away with the window a third full |
| User-defined hooks | Dropped, not deferred. A config file naming commands to run turns cloning a repository into running its code |

# Aux — Production Readiness

Rewritten 2026-08-16. Everything already shipped has been removed; see
[Appendix: what was removed](#appendix-what-was-removed) for the commits.

## Verdict

Aux is a strong late alpha. The architecture is coherent, the security posture
is real, and the test suite is race-clean. It is **not production ready**, and
the gap is mostly *evidence* rather than *code*.

Three things drive that judgment:

1. **The core claim has never been tested.** Aux's pitch is a cheaper, smarter
   harness. There is no measurement of that on real work — not weak evidence,
   none. Demand paging, arguably the centerpiece, still defaults to off because
   nothing has shown it lossless.
2. **"Built but never wired" has happened four times.** Four inert systems in
   the first audit; the skill pipeline with zero callers in the second; and the
   entire first-run welcome flow, found by the reachability gate the moment it
   was switched on (P0.3). None was found by something failing — tests pass
   perfectly well on code nothing calls. The gate now catches this class
   mechanically.

   A related class the gate does *not* catch: code that runs but reports the
   wrong thing. Cached-token usage was dropped by a dependency, so 150 turns
   recorded zero cache reads and cost was overstated several-fold — with the
   cost governor stopping work against the inflated figure. Reachable,
   exercised, and wrong.
3. **The reviews were self-reviews.** The audits and fixes came from the same
   process. Real bugs were found and tests were verified to fail against broken
   implementations — but this file's own F-caveats name this exact failure mode,
   and that discount applies here.

## Definition of done

"Production ready" means: **someone who is not the author can install Aux, use
it on their own repository for a week, and depend on the result.**

Concretely: all of P0 and P1 closed, **and** a soak period afterwards in which
external findings taper off. The second half is not padding. A checklist
assembled by the people who wrote the code cannot enumerate what they cannot
see, and P1.1 exists specifically to lengthen the list — if outside review
produces no new items, the review was shallow rather than the list complete. So
the signal is not "the list is empty", it is **the rate at which the list grows
falling below the rate it is closed**.

This definition exists so the claim stays falsifiable rather than becoming a
vibe.

---

## P0 — Blocking

Nothing in P2 or P3 should be started before these are closed.

### P0.1 — The task benchmark

Built and run (`aux eval suite`, `aux eval gate`, `aux eval compare`,
`internal/evalsuite`). Suites define tasks with pinned revisions and
command-decided success; the runner isolates each task and reads cost from the
ledger; comparisons use an exact two-sided rank test over repeated runs.

**Repetition is not optional and the tool now enforces it.** Two runs of an
identical configuration differed by 49% in tokens and by one task in success
rate, and single-run comparison produced opposite verdicts from the same data.
Fewer than two runs a side can never conclude; three a side cannot reach
p<=0.05 two-sided (its floor is 0.10); five reaches 0.008.

First real result, five runs a side on `bench/suite.json`
(receipt-pipeline, MiniMax-M3 both sides):

| | aux | opencode |
| --- | --- | --- |
| median tokens | 430,576 | 699,974 |
| range | 394,408–591,400 | 647,809–901,503 |
| median turns | 25 | 32 |
| pass rate | 100% median (one run 80%) | 100% median (one run 80%) |

opencode used 63% more median tokens, p=0.01. Two caveats that matter more than
the headline: the gap **grew from 19% to 63% when runs were added**, so five may
still be too few; and aux's spread is 46%, wider at n=5 than at n=3.

**What the suite still cannot support:** one repository, five small Python
tasks, one model. Nothing here generalises to Go or TypeScript work, and
optimising against it would be Goodhart. Breadth is the open debt, not
precision.

Caveats from the original F still stand and no harness addresses them:
team-written evals test what the team thinks matters; "≥ baseline" assumes the
baseline was correct; and no eval captures knowing when to stop or when to ask.
Pair the suite with a hard policy layer **not** subject to eval outcomes.

Note before running it: `aux -p` calls `AutoApproveSession`, so non-interactive
runs bypass every permission prompt. Benchmark repositories must be scratch
checkouts.

### P0.2 — SQLite concurrency ✅ closed, and mostly a false alarm

**The version of this item written on 2026-08-16 was largely wrong, and
measuring it took about twenty minutes.** Recorded here rather than quietly
deleted, because how it was wrong matters more than the fix.

The claim was that `busy_timeout` was unset and foreign keys were therefore at
risk. Probing the driver directly showed otherwise: `ncruces/go-sqlite3`
defaults `foreign_keys` to on and `busy_timeout` to 60s **on every connection**,
and `journal_mode = WAL` is persisted in the database file rather than being
per-connection. None of the correctness-critical settings were ever at risk.

What was actually true, and is now fixed
([`internal/db/pragma.go`](internal/db/pragma.go)):

- `synchronous` and `cache_size` **are** per-connection, and a `db.Exec` against
  a pooled `*sql.DB` reaches only whichever connection serves it. Those two
  applied to one connection. Now set via the DSN so every connection carries
  them. Note the trap this creates: adding any `_pragma` to the DSN **disables**
  the driver's automatic busy timeout, so it must now be set explicitly — there
  is a test pinning that.
- The pool was unbounded and, at the `database/sql` default of 2 idle
  connections, a burst of parallel work closed and reopened connections rather
  than reusing them. Measured: 6 closures per burst at idle=2, zero at idle=8.
- Pragma failures were logged and ignored. SQLite silently accepts unknown
  pragmas and unparseable values — only a *syntax* error fails the open — so
  `verifyPragmas` now reads the critical settings back at startup and refuses to
  run a database that did not get them.

**The lesson is the reusable part.** This item was escalated to P0 on the
strength of reading code and reasoning about it, which is exactly the habit the
rest of this file warns against. Twenty minutes of measurement would have
prevented writing it. Treat every remaining unmeasured claim in this document,
including the ones that sound confident, as a hypothesis.

Still open, and genuinely unmeasured: **a long agent session with the dashboard
open and polling**, asserting no dropped `tool_executions` records under real
load. The unit tests cover a synthetic burst, not a real session. Folded into
P1.1's soak period rather than blocking.

### P0.3 — Systematic reachability audit ✅ closed, and it paid immediately

[`scripts/deadcode.sh`](scripts/deadcode.sh) runs in CI against
[`.deadcode-baseline`](.deadcode-baseline). It ratchets: the build fails when
something becomes newly unreachable, not merely because unreachable code exists.
Verified in both directions — it flags new dead code, and it flags baseline
entries that have since become reachable so the list tightens rather than rots.

**The first run found a fourth instance of the defect class**, in one command,
in seconds — where the previous three took seven parallel hand-audits:

| Dead | Size |
| --- | --- |
| `internal/welcome` | a complete first-run onboarding flow, never called |
| `chat/sidebar.go` | an entire chat component, 13 functions |
| `diff/patch.go` | a public patch-application API with no callers |

72 unreachable functions overall; 49 are design-system and provider-option
surface and are accepted in the baseline with reasons. The 23 above are recorded
as debt, **not** accepted — see P1.5 and P3.

Known limitation, unchanged: `deadcode` traces from `main` and from tests, so a
service that is constructed, stored on a struct, and never invoked still looks
reachable. That is the shape of the earlier validation and governor findings, so
this narrows the problem rather than closing it. Manual review still matters,
and the "constructed but never invoked" pattern deserves its own check
eventually.

---

## P1 — Required before it runs on someone else's machine

### P1.1 — External review

The most important item after P0.1, and the one this file cannot self-assess.
Priority order for outside eyes:

1. **The security surface** — permission fingerprinting, read confinement, the
   dashboard's auth. All of it is days old and has never met an adversary.
   Correct-on-review is not proven.
2. **The agent loop** — `RunTurn`, the coalescing write buffer, parallel tool
   execution.
3. **One real user on a real repository for a week.** Someone who does not know
   where the sharp edges are will find things no audit will.

### P1.2 — Upgrade and migration safety ✅ closed

`goose.Up` runs on every `Connect`, releases ship via `.goreleaser.yml`, and so
every user walks the upgrade path while nothing tested it. Fresh databases were
covered by every other test in the tree; populated ones by none — which is the
only case a real user has.

Now tested from *every* recorded version, not just the newest, since a user
arrives from whatever build they last ran: seed a populated database at version
N, migrate to current, assert the rows, the added columns and the
trigger-maintained counters all survive. Idempotence is covered too, because
`Connect` re-runs `goose.Up` on every single start.

Two behaviours added rather than merely asserted:

- **A database from a newer build is refused** (`ensureNotNewer`). `goose.Up`
  accepts one happily — there is nothing left to apply, so it *succeeds* — and
  the older binary then runs against a schema it does not know, surfacing much
  later as an unrelated query failing on a missing column. Downgrade is
  deliberately not offered: the Down migrations exist but have never been run
  against real data.
- **Failure says where it stopped and that retrying is safe**
  (`migrationFailure`). Every migration here is transactional, so a failure
  leaves a known-good version rather than a half-applied one; saying so is the
  difference between "my database is corrupt" and "it stopped at a known point".
  The connection also leaked on this path and no longer does.

Recovery is documented in the README's [Upgrading](README.md#upgrading) section.
Both tests were verified to fail against a deliberately broken implementation:
the refusal test against a no-op check, the upgrade test against an upgrade that
silently drops a row.

### P1.3 — Failure behaviour

- **Panic recovery.** ✅ closed, and the claim as written was wrong again.
  Recovery already existed — `logging.RecoverPanic` wraps the agent goroutine,
  the TUI message handler, every subscription and `main` — so a panic never took
  down the TUI. The defect was in what recovery *skipped*. That goroutine also
  owns the session's busy marker, the generation context and the event channel,
  and released all three on its normal path only. So a panic left the session
  permanently busy — the editor refuses input for it for the rest of the process
  — and left any consumer ranging over the channel blocked forever. The recovery
  handler's own send was unbuffered on top of that, so once no consumer
  remained it deadlocked *inside the handler*, wedging the session with no
  visible symptom at all. `Summarize`, twenty lines below in the same file, had
  the deferred form right. Fixed with deferred teardown and a one-slot buffer.

  Found next to it: crash logs went to the process's working directory, so a
  panic dropped `aux-panic-*.log` into the user's repository at 0644. Now
  `.aux/` at 0600, on the same reasoning that made the debug log 0600. Note the
  correction: `.aux/` is itself inside the repository, so these move out of the
  repository *root*, not out of the repository. The original wording overstated
  it.
- **Silent-failure sweep.** ✅ closed. 137 discarded-error sites triaged; three
  were changing what Aux *reported*, which is the class that produced this
  file's worst entries:

  | Site | What it did |
  | --- | --- |
  | `finishMessage` | dropped the write marking a message finished, so it rendered as streaming forever, across restarts |
  | `scanVersionOpt` | discarded both unmarshals, then returned the skill as found and valid with empty content |
  | `Reindex` | discarded the edge reset, then re-added on top — the impact graph accumulated dependencies that no longer exist and went on reporting them |

  The rest were checked and annotated with why discarding is correct, so the
  next dangerous one is visible rather than hidden among identical lines. Each
  fix was verified to fail against the swallowed version first.

- **First-run and misconfiguration.** ✅ closed, and found by *running the
  binary in an empty environment* rather than reading the code — which is how
  all three of these turned out to be worse than the item described.

  With no API key the error was `agent coder not found`, followed by a usage
  dump. Agent defaults are only filled in once a provider exists, so a missing
  key surfaced as a missing agent: an internal concept, several layers from the
  cause, no remedy. Now a provider preflight that names the variables to set.

  The two remaining cases in this bullet were **not** broken, contrary to how it
  was written: a non-git directory already works, and an unwritable `$HOME` is
  irrelevant because the data directory is working-directory-relative.

  That last fact exposed something nobody had written down: `.aux/` is created
  **inside the user's repository** and holds the full session transcript —
  prompts, tool output, everything the agent was shown — with nothing marking it
  ignorable. `git add -A` committed all of it. It now writes its own
  `.gitignore`.

### P1.4 — User-defined hooks ✅ dropped

Decided: **not planned**, removed from the roadmap rather than left pending.

Running commands from a config file is arbitrary code execution, and a
repository-level config that registers one turns *cloning a repository* into
running its code. That needs a threat model this project has not written. The
in-process dispatch points and their observability handlers already cover what
the hooks were wanted for, and they stay.

---

## P2 — Unblocked by P0.1, high value

Ordered by value. None should start before the benchmark exists, because none
can be evaluated without it.

### P2.1 — Tool-result eviction with promotion (was B2)

Still the most valuable idea in this file, and the most dangerous. When a tool
result has been consumed, replace its full content with a one-line pointer
(`// file.go (1.2K lines) — read, no action needed`), leaving PageRank retrieval
able to re-pull it. Plausibly 40–70% off history size on long sessions.

The pairing that makes it safe: eviction must ask **"is this a fact about the
project, or about this turn?"** Facts get promoted to memory; turn-local content
is dropped. Blind eviction is how an agent forgets what it was told.

Built before P0.1 exists, there is no way to tell token savings from silent
context loss. That is the entire reason it has not been built.

### P2.2 — Decide demand paging's default

`--paging` exists and defaults to off. A single-run measurement suggested −26%
tokens; it was inside the noise floor and is void. No trustworthy measurement
exists yet.

The machinery to settle it now exists (`aux eval suite --repeat`), and runs are
cheap on a local model. Unblocked — it just has not been run at adequate n.

### P2.3 — Skill promotion path (was B4)

Candidates are now produced automatically from validated commands, and promotion
correctly requires a passing evaluation. Nothing can currently produce that
evaluation, so no skill can ever be promoted — the pipeline terminates one step
short. P0.1 supplies the missing evidence.

Keep the original risk note in force: a wrong skill is worse than no skill, and
one that fires on every task is the bad outcome to design against.

### P2.4 — `/remember` (was C2)

`/remember always use the new auth API, not legacy` writes to `.aux/memory.md`
(project) or `~/.aux/memory.md` (user), loaded next session as Project Brain
input. Default project scope; explicit opt-in for user scope. Closes the
specific "the agent forgets what I told it" complaint that started this file,
and it is small.

### P2.5 — Memory UX (was E)

Ship before more of C, not after. Without it memory is a black box that
surprises people, which is worse than no memory:

- **Discoverable** — plain markdown, plus `aux memory list`.
- **Editable** — by hand.
- **Deletable** — "forget that", "forget session X", "wipe everything".
- **Provenance visible** — when the agent acts on a memory, show which line.
- **Portable** — export, import, sync.

### P2.6 — Memory primitive gaps (was D)

Most of the original list already exists (`promote`, `confidence`, `provenance`,
project/task scope, revision-based invalidation). Genuinely missing:

| Primitive | Gap |
| --- | --- |
| `supersede` | versions exist, no explicit supersede |
| `expire` | revision-based only, no date-based expiry |
| `scope` | project/task only — no user or org scope |

An afternoon of work, but user/org scope is a real design decision about where
memory lives and should not be inferred from the code.

---

## P3 — Opportunistic

Real, small, or low-confidence. Nothing here blocks production.

- **B3 (view half)** — smart window around the search match in `view`. The bash
  half already shipped.
- **B12 — MCP tool curation.** Worth more than its original position: a 50-tool
  MCP server costs ~15K tokens of definitions per turn. Now that MCP ordering is
  understood to drive cache stability, curation interacts with P0.1 directly.
- **B7 — PageRank as file-inclusion oracle.** Needs a <50ms p99 retriever with
  per-file invalidation first.
- **B8 — trajectory waste detection** ("you grepped this three times").
- **B9 — cost-aware tool selection** (suggest `--testPathPattern` when budget is
  low).
- **B10 — differential inclusion for edits** (send diffs, not whole files).
- **B11 — `--budget strict` preset** wiring existing governor policies.
- **C4 — correction detection.** "Propose, never auto-write" remains right; the
  heuristics are ~70% unreliable at telling one-shot from durable.
- **A8 — grep spawns `rg` per call.** Measure before optimizing.
- **Delete the dead chat sidebar.** `internal/tui/components/chat/sidebar.go` is
  an entire component (13 functions) superseded by the context pane and never
  removed. Listed as debt in `.deadcode-baseline`.
- **Delete or use `diff/patch.go`'s patch API.** `AssembleChanges`, `LoadFiles`,
  `ProcessPatch`, `ValidatePatch` and friends have no callers; the patch tool
  takes a different path. Dead code in a file-mutating package is worse than
  dead code elsewhere, because the next person to touch patching may reasonably
  assume it is the real implementation.
- **C1 — four-tier context model.** A principle for reviewing the above, not a
  task: saving tokens means moving Tier 2 → Tier 3, not deleting Tier 2.

---

## Sequencing

P0 is closed. **P1 is now closed except P1.1**, which is the one item this file
cannot do for itself.

The pattern across everything closed since is worth keeping in view, because it
is the argument for P1.1 rather than a decoration on it. In four of the last six
items the *description was wrong about its own symptom*: P0.2's SQLite alarm was
a false alarm, P1.3's panic bullet described a crash that could not happen,
P1.2's real defect (a newer database accepted silently) was not in the item at
all, and P1.3's first-run bullet named two things that already worked while
missing the one that mattered — session transcripts sitting uncommitted in the
user's repository. Every one was found by measurement or by running the binary,
never by re-reading the checklist that produced them.

So the remaining work is not on this list, and cannot be. It is
[P1.1](#p11--external-review): one outside reader on the security surface, one
real user on a real repository for a week, and then the soak — the list growing
slower than it is closed. [docs/trying-aux.md](docs/trying-aux.md) exists so
handing it to someone costs ten minutes.

P2 stays blocked on suite breadth. Optimising against five tasks in one Python
repository is how a benchmark becomes a target, and the current comparison
against opencode does not generalise past that repository.

## Appendix: what was removed

Shipped 2026-08-16, commits `518c85c`, `d4ec855`, `a9f9a85`:

| Was | Shipped as |
| --- | --- |
| A1 | Coalescing write buffer, with the ordering guard (ADR 18) |
| A2 | Parallel read-only tool execution, capped at 4 (ADR 19) |
| A3 | Denial continues reads, cancels effectful calls |
| A7 | Bounded shell capture — the bug was `os.ReadFile`, not streaming |
| B1 | Deterministic MCP ordering; `stablePrefixID` hashes the tool block as sent |
| B5 | `context_exclude` tool, `exclude` command, agent-drop markers in the pane |
| C3 | Skill candidates extracted from validated commands at task completion |
| — | Permission prompt serialization (found as an A2 prerequisite) |

Closed since (on `main`):

| Was | Outcome |
| --- | --- |
| P0.4 | PR #1 merged; work continues directly on `main` |
| P0.1b | `--repeat`, `Series`, and an exact two-sided rank test; three runs a side cannot reach p<=0.05 and the tool says so |
| P1.6 (platforms) | macOS and Linux stated in the README; the Windows archive rules for a target never built are gone |
| P1.2 (coverage) | `scripts/coverage.sh` ratchets against `.coverage-floor`, starting at the 30.5% the tree actually achieves. Verified to fail on a drop and to ask for the floor to be raised on a gain. The gap to the stated 80% bar is real and unclosed; the gate only stops it widening |
| P1.5 (first run) | `welcome` wired into interactive startup and removed from the reachability baseline; the intro now states what leaves the machine, that commands ask first, and where state is stored |

Struck as invalid after verification:

- **A4** — `Compile` is pure over in-memory history. No repo walk, no PageRank,
  no skill scan. There is no full rebuild to cache.
- **A5** — `RankFiles` is a pure function over a graph handed to it. The cadence
  question belongs to whatever builds the graph.
- **G / `internal/db/embed.go`** — six lines of `//go:embed migrations/*.sql`.
  Go's `embed` package, not vector embeddings.
- **B6** — subagents rebuild from scratch *by design*. Statelessness is the
  contract: it keeps a subagent's exploration out of the parent's context, which
  is the reason to spawn one. Not waste.

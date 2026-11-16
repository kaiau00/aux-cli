# Floor plan — implementation plan

Status at the time of writing: **F1 complete and verified, uncommitted.** Everything
below F1 is unstarted.

## Goals

Decided 2026-11-15:

- **Two audiences, equally weighted: the user and the agent.** The map is for
  orientation and search by someone who already knows the code, and it is also
  something Aux itself reads so a task starts already knowing the shape of the
  repository. It is *not* primarily an onboarding document for newcomers, which is
  what pulls the reference output toward describing all 505 files.
- **Ships in v0.1.0 as the headline feature.** The tag waits for it. The delay was
  raised and accepted.

## What this is

A navigable map of the repository, built by Aux and shown in the dashboard as its
front page, and readable by the agent as context. Two layers, kept strictly apart:

- **Measured.** Files, categories, line counts, import links, git state. Derived from
  what is on disk, repeatable, and always present.
- **Authored.** Area labels, summaries, file descriptions, annotations, flows, the
  orientation tour. Written by the agent after reading the code. Cannot be derived
  from file names.

The separation is the point. A measured map with no prose is useful and honest. A map
with invented prose is worse than no map, because nothing marks the invention. **An
undescribed area renders as undescribed.** Nothing in the UI may imply a summary
exists when it does not.

Schemas are Floorplan's `architecture@1` and `graph@1`
(https://github.com/haydenschmitty/floorplan, MIT, Copyright (c) 2026 Hayden Smith),
so maps are interchangeable with that project's tooling.

## Verified parity (F1 + F2)

Both phases were diffed against Floorplan's own output for this repository, rather
than reviewed by eye:

| | Mine | Reference |
|---|---|---|
| files scanned | 593 | 593 |
| code / other / docs / config / data / lock | 505 / 53 / 24 / 6 / 4 / 1 | identical |
| code lines | 81,567 | 81,567 |
| mapped files / unassigned | 505 / 0 | 505 / 0 |
| areas, and their file and line counts | 70, **zero differences** | 70 |
| links found by the reference and missed here | **0** | — |
| links found here and missed by the reference | 10 | — |
| routes | **18** | 4 |

Where the two now differ, Aux is right and the reference is not:

- **Routes.** The reference reads only patterns with no method, so it finds 4 of the
  18 routes this repository serves and labels them all `ANY`. Go 1.22's ServeMux puts
  the method in the pattern, and the method is the most important thing about a route
  — it separates a page you can read from an endpoint that changes something.
- **The 10 extra links** are calls to the 14 routes the reference never knew existed,
  such as `memory.js` calling `/api/v1/memory`. They were verified individually.

Eight wrong assumptions were caught by diffing rather than by review. Four in F1:
blank lines must not count (an 11% overstatement), `.github` must not be dropped, a
package is represented by the file named after its directory, and a test file does
link to the package it tests. Four more in F2: the authored and built schemas use
different shapes for flow steps (`node` versus a prefixed `ref`), a hostname in a
comment is a mention and not a call, an import path contains route-like paths and
produced 45 false links until excluded, and a naive string tokeniser desynchronises on
a regular expression containing a quote — which hid three endpoints `sessions.js`
really calls.

---

## Phases

Each phase is one PR and must pass all five gates (`gofmt -l .`, `go vet ./...`,
`AUX_UNICODE_ICONS=1 go test -race ./...`, `scripts/deadcode.sh`,
`scripts/coverage.sh`).

**Order as it now stands.** F1 to F4 are done. F5 is parked for want of an API key,
and nothing downstream waits on it, so the order from here is F7, F8, F9, F10, F6,
F11, with F5 rejoining whenever credentials exist. F6 sits late because feeding the
map to the agent is worth much more once there is prose to feed.

### F1 — Scan, classify, assign, link — **S** — done

`internal/floorplan`: `floorplan.go` (both schemas), `scan.go` (git-based listing),
`classify.go`, `assign.go` (glob precedence), `links.go` (AST imports).

### F2 — Build the graph document — **M** — done

Join the scan, the architecture and the links into one `graph@1`.

- Stats roll **up**: a parent's `files`/`loc`/`langs` include every descendant's.
  Verified against the reference (`agent`: 0 direct, 86 inclusive).
- `counts.unassigned` is the honesty metric — files no area claimed.
- Git state per checkout: branch, head, remote.
- Resolve `f:… → n:ext.*` links by matching an external's declared package or host
  prefixes against each file's imports.
- Extract served HTTP routes from Go source (method, path, file, line).
- Health issues: measured only (unassigned files, oversized files, areas with no
  files, dangling flow/tour refs). Never an opinion.
- Dangling flow and tour refs are **reported**, never silently dropped.

Test shape: a fixture repo with a known architecture, asserted as a golden `graph@1`.

### F3 — Store the map, detect staleness — **S** — done

Decision 1 removed most of this phase: with the authored layer in repository
files there is no migration, no sqlc, and no database schema.

- `floorplan/architecture.json` and `floorplan/descriptions/<area>.json`, written
  indented through a temporary file and renamed, so an interrupted write cannot
  replace a good map with half of one.
- One description file per area, so describing one area cannot damage another's prose.
- A missing map and a damaged one are different errors. Reporting a truncated file as
  "no map" would overwrite authored work with an empty document.
- An area id becomes a filename, and an authored document is not a trustworthy source
  of filenames, so ids that could escape the map directory are rejected.
- **Staleness is keyed on a signature, not on content.** The signature hashes what a
  file offers -- its exported names, the routes it serves, its doc comment -- so
  rewriting a body or reformatting does not invalidate a description, and adding an
  exported function does. This is what makes staleness usable at all: with 505
  descriptions, a check keyed on exact content would flag nearly all of them nearly
  all of the time, which is the same as having no check.

Verified by loading the reference's own map: 70 areas, 5 edges, 5 annotations, 3 flows
with 14 steps (none empty), 7 tour stops and all 505 descriptions, named and with
prose.

**One honest limit.** Descriptions written by the reference carry no signature, so
their staleness cannot be judged and they are reported as unknown rather than as
current. Only descriptions Aux writes record one.

### F4 — Build the map on first use — **M** — done

`internal/floorplan/service.go` (lazy build, incremental rescan) and `git.go`
(repository state for the document's `meta.repos`).

The map is built on first use rather than at startup, so nothing that never opens
the floor plan pays the ~235ms to read and parse the repository. Within a session the
document is held and re-served until something on disk moves.

Measured on this repository:

| | |
|---|---|
| cold build — read and parse 618 files | 235 ms |
| warm check — nothing changed | 28 ms |
| &nbsp;&nbsp;of which `git ls-files` | 27 ms |
| &nbsp;&nbsp;of which statting 618 files | 1.2 ms |

**Two things the plan had wrong, both settled by measuring rather than reasoning.**

The cache was to be keyed on git head plus the dirty set. Reading that state costs
four subprocesses and 84ms — *three times* what checking every file costs, and it
says nothing about a directory that is not a repository. Invalidation is stat-based
instead: size and modification time per file, which is what build systems use.

"With an unchanged head the steady state is a few milliseconds" was also wrong. It is
28ms, and the statting is not the reason — one `git ls-files` subprocess is 27ms of
it. Listing from directory mtimes would remove most of that, and is not worth the
complexity until something asks for the map far more often than a page load does.

Stat-based invalidation is wrong in one narrow case, a file rewritten to the same
length inside one filesystem timestamp tick. `Service.Rebuild` is the answer to it.

Still open, and deliberately not done here:

- **No disk persistence.** It would save one 235ms build per process, once. The
  valuable cache is the in-memory one, and persisting the snapshot means a schema, a
  version, and corruption handling for a saving a user notices once per session.
- `internal/impact` still walks the tree and parses every Go file at startup for its
  own graph. That walk and this one should become one.
- `aux floorplan status` reports what is built, described and stale — it needs a
  command to live in, so it lands with F11.
- Nothing calls `Service` outside its tests yet. The wiring lands with F9, when there
  is a page to serve it.

### F5 — The agent writes the prose — **L** — parked

Parked deliberately, not blocked. It is the only phase that spends money, and there is
no API key available at present. Nothing downstream needs it: a map with no prose
renders as undescribed by design, so F6 through F11 can all proceed without it.

A Claude Pro/Max subscription does not open a path here. It covers Claude Code and the
Claude apps; API access is billed separately, and the OAuth token Claude Code stores is
scoped to Claude Code. Lifting it into Aux is outside what the subscription grants and
will not be built. The Copilot provider looks like a counter-example and is not one:
`internal/config/config.go` reads the credential file GitHub's own CLI writes, which
works because GitHub's subscription explicitly covers third-party CLI integrations.
Anthropic's does not.

When it resumes, it splits in two, and this ordering is better than the original:

**F5a — the tool.** In the shape of `internal/impact/tool.go`: writes area labels and
summaries, file names and descriptions, annotations, flows and the tour. Testable end
to end against `ProviderMock` and `launch3.sh` with no credentials and no spend.

**F5b — this repository's descriptions, written by hand.** ~70 area summaries, produced
by an agent reading the code in a session rather than by a billed API call.

F5b should land **first**. It gives F5a the one thing F1 to F3 had and the original
plan did not: reference output to diff against. Three of the eight wrong assumptions in
F1/F2 were caught only because Floorplan had already mapped this repository and its
output was on disk. A generator with nothing to check against but its author's judgment
is the exact trap that caught.

Constraints that still hold whenever the tool runs:

- Runs **by default** after the measured build (chosen deliberately, over the cheaper
  on-demand option).
- **Cost is made visible, never silent.** The reference described 505 files and 70
  areas: roughly the whole repository read once, around 19k tokens of output. In a
  repo this size that is dollars, not cents, on the first run. Show the estimate,
  show progress, and record the spend in the existing cost ledger.
- Scope control: describe only what is undescribed or stale, so the full cost is paid
  once rather than every start. `StaleDescriptions` in `signature.go` already answers
  "what is stale"; it is the input to the scope decision.
- Needs a ceiling and a sane refusal for very large repositories.
- Alternatives for a user without an Anthropic key: a Copilot subscription works today
  via `~/.config/github-copilot/`; `LOCAL_ENDPOINT` against Ollama or LM Studio is free
  and noticeably worse at prose. The known offline-start bug bites the local path.

### F6 — The map as agent context — **M**

Follows directly from the audience decision: the map is for the agent as much as for
the user.

- Expose the measured structure and the area summaries to the agent through the
  existing Project Brain and context-page machinery, rather than only as a web page.
- Token-budgeted: the whole map does not fit in a prompt, so the agent gets the area
  tree and summaries, and queries down into files on demand.
- The useful questions are "what depends on this?" and "which tests cover this
  change?" — the second already has an answer in `internal/impact`, so this phase is
  partly about joining the two rather than inventing anything.
- Area summaries must therefore read well for both a person and a model: compact,
  concrete, no filler.

### F7 — Lay the map out — **L**

A layered layout in Go, server-side.

Floorplan uses ELK for this. ELK is EPL-2.0 and 1.6MB of JavaScript; vendoring it
into a single MIT binary adds copyleft obligations and breaks the dashboard's
zero-dependency, build-free property. So: layer assignment by longest path, crossing
reduction by barycenter ordering, then coordinates, with nested containers for
parent/child areas.

**This will not be pixel-identical to ELK.** It will be deterministic and unit
testable, which ELK in the browser is not.

### F8 — Dashboard design system — **M**

Port Floorplan's token system: neutral `#0f0f0f` / `#f3f3f1` surfaces, per-kind node
colours, per-kind link colours, Inter and JetBrains Mono, large radii, soft shadows,
and a working light theme. Amber survives as the brand and active-state accent.

Restyle all six existing pages (Tasks, Project Brain, Memory, Impact, Optimization,
Sessions) onto the new tokens. Nothing is deleted.

Also fold in the duplication `common.js` already documents: `tasks.js` and
`sessions.js` predate it and keep private copies of `card`, `kv` and the escaping
helper — 40% of the dashboard's JS.

Contrast must meet WCAG AA, keyboard navigation must work, and every animation stays
behind `prefers-reduced-motion`.

### F9 — The floor plan is the front page — **L**

- `GET /floorplan` and `GET /api/v1/floorplan`, plus a viewmodel.
- `/` serves the floor plan; the task workspace moves to `/tasks`; one nav entry added.
- The map view: areas, links, pan and zoom, click an area for its detail panel, file
  list, and search across areas and files.
- Undescribed areas and stale descriptions are visibly marked.
- **Decide what the map shows under each redaction mode.** The dashboard has three
  (`full`, `metadata`, default snippet-plus-label). Agent-written prose about the code
  is a new category of content those modes were not designed for.

### F10 — Flows, tour, recent work, health — **M**

Flow stepping, the orientation tour, recent-work cards, and the health list. These are
what make the map teach rather than merely display.

### F11 — Command, docs, attribution — **S**

- `aux floorplan` — `status`, `rebuild`, `describe`, `open`.
- **Attribution: a NOTICE entry for Floorplan (MIT, Copyright (c) 2026 Hayden Smith).**
  Required, not optional. `LICENSE` keeps `Copyright (c) 2025 Kujtim Hoxha`.
- An ADR for the two-layer design and the layout decision.
- README and shipping-plan entries.

---

## Decisions made

Taken rather than deferred, with the reasoning, so they can be argued with later.

1. **The authored layer lives in the repository, not only in SQLite.**
   `floorplan/architecture.json` and `floorplan/descriptions/*.json` are the source of
   truth; SQLite caches the built graph. A map worth writing is worth reviewing in a
   pull request and sharing with everyone on the project, and prose that cost real
   money to generate should not die with a database file. This also matches the
   reference layout, so the two tools can read each other's maps.

2. **Redaction splits structure from prose.** Area names, file paths, counts and import
   links are always shown: they are derived from code the viewer can already read, and
   the dashboard is bound to 127.0.0.1 behind a per-run token. Authored prose and
   recent-work entries follow the active redaction mode and are withheld under
   `metadata`, because a summary can quote or paraphrase code content and the existing
   modes were not designed with that in mind.

3. **Default describe covers areas, not every file.** Roughly 70 area summaries, not
   505 file descriptions. The area summaries carry nearly all the orientation value
   and cost about a seventh as much; per-file prose is largely an onboarding feature,
   which is explicitly not the primary goal. Full file description stays available as
   one command. A hard ceiling skips default describe on very large repositories with
   a message saying so rather than quietly spending.

4. **Import links are Go-only, and the UI says so.** The scanner is multi-language;
   link derivation is not. A JavaScript project would otherwise appear to have no
   dependencies at all, which is a false claim by omission. TypeScript and Python
   import parsing is a follow-up, not a blocker.

## Known risks

- **Startup latency** is the largest. Two tree walks already exist; a third is not an
  option. F4 must be measured, not assumed.
- **Describe cost** on first run in a large repository.
- **Description churn.** Every edit staleness-flags a file description. With 505 of
  them, the steady state may be mostly stale unless re-describing is cheap and
  incremental.
- **Layout quality** without ELK.

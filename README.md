# ⌬ Aux

**The coding agent that gets cheaper, faster, and more capable the longer it works on your project.**

Aux is a local-first terminal and browser coding agent built around one idea: instead of adding more model providers or another chat window, it builds a persistent, project-specific intelligence layer between you, your codebase, and the one model you already want to use. Add one API key, pick one preferred model, and Aux remembers how your project works so it stops paying to rediscover it on every task.

> Aux understands how your project works, gives your preferred model only what it needs, and improves every time you use it.

## Contents

- [What Aux is](#what-aux-is)
- [Quick start](#quick-start)
- [How it works](#how-it-works)
- [CLI reference](#cli-reference)
- [The dashboard](#the-dashboard)
- [The TUI](#the-tui)
- [Configuration](#configuration)
- [Supported AI models](#supported-ai-models)
- [MCP](#mcp-model-context-protocol)
- [LSP](#lsp-language-server-protocol)
- [Custom commands](#custom-commands)
- [Architecture](#architecture)
- [Upgrading](#upgrading)
- [Development](#development)

## What Aux is

Most coding agents win by adding more model providers or copying features. Aux's bet is different: build a **Project Brain** that compounds. Four systems work together on every task:

1. **Project Brain** — a living, project-specific model of architecture, conventions, decisions, skills, and experience, built automatically from your repo (`go.mod`/`package.json`/`Makefile`/instruction files, layered by precedence).
2. **Context OS** — a token-efficient memory hierarchy that pages in only what the current step needs, instead of resending the whole transcript every turn.
3. **One-Key Cost Governor** — a single efficiency layer (budgets, waste detection, trajectory tracking) for the one model you've chosen, not per-action model routing.
4. **Experience Compiler** — a learning loop that turns validated work and corrections into reusable memory, evaluation-gated skills, and cost policy — so the tenth similar task is cheaper and better than the first.

```text
Understand the project
        ↓
Compile the smallest useful task context
        ↓
Optimize the chosen model's context and budget
        ↓
Execute and validate the change
        ↓
Learn reusable project experience
        ↓
Make the next task cheaper and better
```

Two more things that fall directly out of this design:

- **Nothing is trusted without evidence.** A task criterion only becomes "validated" when a real command actually ran and passed — never from a model's claim or a subagent's self-report. When the impact graph can't confidently scope validation (empty, stale, or the change touches something it doesn't cover), Aux broadens validation automatically instead of guessing narrow.
- **Two control surfaces, same truth.** A fast terminal workbench for execution, and a browser dashboard — Tasks, Project Brain, Memory, Impact, Optimization, Sessions — for everything else: understanding, comparison, and history. Both render the same durable, event-backed state; nothing in either surface is invented for display.

## Quick start

### Requirements

macOS and Linux, on x86_64 or arm64. Windows is not supported: the shell tool
depends on Unix process semantics, so a Windows build would compile and then
fail at the first command it ran.

### Install

Build from source. Requires Go 1.24 or newer.

```bash
git clone https://github.com/kaiau00/aux-cli
cd aux-cli
go build -o aux .
```

There are no published releases yet, so there is nothing to download and no
package to install. The install script, the Homebrew tap and the AUR package
described in `.goreleaser.yml` are prepared but not yet published, and
`go install` does not work because the module path is not the repository path.
Build from source until a release is tagged.

### Run

```bash
# Set at least one provider API key, e.g.:
export ANTHROPIC_API_KEY=sk-...

# Open Aux in a project
cd your-project
aux
```

Aux resolves the project, compiles its profile, and prints a token-gated localhost dashboard URL — open it any time to watch what's happening. Type an objective in the composer and Aux gets to work.

For a one-shot, non-interactive run:

```bash
aux -p "Explain this repository"
aux -p "Explain this repository" -f json   # JSON output
aux -p "Explain this repository" -q        # no spinner, good for scripts
```

## How it works

1. **Open a project.** Aux resolves a stable project identity (by normalized git remote, or root path for local-only repos — the same identity survives re-clones, forks, and worktrees), compiles the effective profile in the background, builds a deterministic AST-derived impact graph, and derives a related-project graph from your dependencies.
2. **You give it an objective.** The Task Compiler infers a mode (implementation, bug diagnosis, refactor, test authoring, code review, research, maintenance), and compiles a versioned task spec — scope, acceptance criteria, validation intents, and a budget — from the objective plus the compiled profile.
3. **The prompt is compiled, not just concatenated.** The Context OS builds a page-by-page manifest instead of resending everything: project manifest, task spec, prior memory, related-project context, transcript — deduplicated, and with real per-page exclude controls (crossing a file off in the TUI actually removes it from the next compile, not just the display). The Cost Governor watches budget mode and ceilings throughout.
4. **The agent works.** It calls tools (`bash`, `edit`, `write`, `patch`, `grep`, `glob`, `view`, `fetch`, `sourcegraph`, `diagnostics`) and can spawn specialist **subagents** — repo mapper, impact analyst, validation runner, reviewer — each with its own task identity linked to the parent, reporting back through a structured contract instead of free text. The first file mutation auto-checkpoints a baseline; every tool call is recorded to a durable ledger and event log.
5. **Validation is evidence-based.** The impact graph decides targeted vs. broad validation and always fails safe. A criterion is only "validated" once a real command actually ran and passed.
6. **The task finishes.** A final checkpoint captures what changed. Validated commands become procedural memory; the task becomes an episodic summary — both available to future tasks automatically, without you re-explaining anything.
7. **You watch it live**, in the TUI or the dashboard — both are projections of the same event-backed state, never a separate narrative.

## CLI reference

Beyond the interactive TUI (`aux`) and one-shot prompts (`aux -p "..."`), Aux ships read-only inspection commands for everything it tracks:

| Command | What it does |
| --- | --- |
| `aux project show [--json]` | Show the current project identity, revision, and compiled profile |
| `aux project refresh [--json]` | Rescan the project and recompile the effective profile, reporting what changed |
| `aux project related [--json]` | List related-project edges derived from the current project's dependencies |
| `aux profile show [--effective] [--json]` | Show the project profile layer, or the merged effective profile |
| `aux task show <task-id> [--json]` | Show a task, its compiled spec, and acceptance criteria |
| `aux task begin <objective> [--repo <path>...] [--auto-related] [--json]` | Begin a task; pass `--repo` more than once (or `--auto-related`) to compile a multi-repository task |
| `aux impact <changed-path>... [--json]` | Show the change impact (dependents, affected packages, tests) for a set of paths |
| `aux cost <task-id> [--json]` | Show a task's budget usage, trajectory, and waste warnings |
| `aux validate <task-id>` | Run the project's validation commands and record proof-of-done evidence (`--yes` to approve them) |
| `aux eval compiler` | Compare compatibility vs. paging prompt compilation on baseline fixtures |
| `aux eval experiment` | Run and persist the compiler experiment (compatibility vs. paging) |
| `aux eval replay <task-id>` | Deterministically reconstruct a task's state from its durable events, offline |
| `aux eval ab <baseline-task-id> <variant-task-id>` | Compare accepted validated changes per dollar for two recorded runs |
| `aux learn` | Record a workflow as a skill candidate (activated only after evaluation) |
| `aux skill list` | List skill candidates and active skills |
| `aux bundle export <file>` | Export active skills and governor policies to a content-addressed bundle |
| `aux bundle import <file>` | Import a bundle; entries arrive as candidates and must be evaluated before use |

### Command-line flags (`aux`)

| Flag | Short | Description |
| --- | --- | --- |
| `--help` | `-h` | Display help information |
| `--debug` | `-d` | Enable debug mode |
| `--cwd` | `-c` | Set current working directory |
| `--prompt` | `-p` | Run a single prompt in non-interactive mode |
| `--output-format` | `-f` | Output format for non-interactive mode (`text`, `json`) |
| `--quiet` | `-q` | Hide spinner in non-interactive mode |
| `--version` | `-v` | Print the version and exit |
| `--paging` | | Prompt compiler for this run: `on` (demand paging) or `off` (compatibility) |

`--debug` and `--cwd` apply to the interactive `aux` command itself; subcommands
resolve the project from the current directory.

## The dashboard

The dashboard starts automatically on every run, bound to `127.0.0.1` on a random free port, and is **read-only** by design — there is no mutation endpoint anywhere in its API. Every route requires a token, generated fresh per run and printed at startup, either as `?token=` or an `X-Aux-Dashboard-Token` header. Static assets (`/css`, `/js`) are the one deliberate exception, since a browser can't attach a token to a stylesheet request — they carry no data.

| Route | View |
| --- | --- |
| `/` (default) and `/tasks` | Active-task workspace — the task-first view: header, changes, validation, context budget, activity |
| `/project` | Project Brain — identity, effective profile (with conflicts), related-project graph |
| `/memory` | Memory & skills — active/candidate/stale memory, skills |
| `/impact` | Impact graph — indexed nodes and edges, with a lightweight diagram |
| `/optimization` | Optimization — experiment history, governed-cost policies |
| `/sessions` | Session/log inspector — the secondary, debugging-oriented view: session tree, live activity, event feed, logs |

Set `dashboard.fullContent: true` if you want the dashboard to show full local prompt/tool content instead of redacted snippets, or `dashboard.enabled: false` to turn it off entirely.

## The TUI

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). A right-hand context pane lists everything currently in the agent's context; a composer at the bottom sends messages; the message list scrolls independently.

### Global

| Shortcut | Action |
| --- | --- |
| `Ctrl+C` | Quit |
| `Ctrl+?` | Toggle help |
| `?` | Toggle help (when not editing) |
| `Ctrl+L` | View logs |
| `Ctrl+S` | Switch session |
| `Ctrl+K` | Command dialog |
| `Ctrl+F` | Select files to upload |
| `Ctrl+O` | Model selection dialog |
| `Ctrl+T` | Switch theme |
| `Esc` | Close current overlay/dialog |

### Chat page

| Shortcut | Action |
| --- | --- |
| `Ctrl+N` | New session |
| `Esc` | Cancel current generation, or close an open context drawer |
| `Ctrl+G` | Toggle the context drawer (reach the context pane when the terminal is too narrow to show it inline) |
| `Tab` | Toggle collapse of the focused reasoning block |
| `i` | Focus editor (when not writing) |

### Context pane

Hotkeys are suppressed while the editor is focused, so `x`/`u`/`c`/`p`/`e`/`d`/`j`/`k` type normally into your prompt; press `Esc` once to leave the editor and the pane takes over.

| Shortcut | Action |
| --- | --- |
| `↑` / `k` | Move selection up |
| `↓` / `j` | Move selection down |
| `x` | Cross off the selected entry — excludes it from the task's next prompt compile, not just the display |
| `u` | Un-cross the selected entry (also restores one the agent dropped itself) |
| `c` | Clear all crossed-off entries |
| `p` | Toggle pin on the selected entry — guarantees its full content on the next compile, exempt from both exclusion and dedup stubbing |
| `e` | Toggle the expanded context view (grouped by resident/available/pinned/evicted/faulted) |
| `d` | Reveal/hide the full dashboard URL (collapsed to a one-line status by default) |

The agent can also drop files from its own context once it is done with them
(the `context_exclude` tool). Those entries appear crossed off with a distinct
marker, so you can see what it discarded and press `u` to put any of it back.
The `exclude` command (via `Ctrl+K`) does the same thing by path, for files that
are not currently on screen.

### Editor

| Shortcut | Action |
| --- | --- |
| `Enter` / `Ctrl+S` | Send message (when editor focused) |
| `Ctrl+E` | Open external editor |
| `Ctrl+R` then a number | Delete attachment at that index |
| `Ctrl+R` then `r` | Delete all attachments |
| `Esc` | Blur editor, focus messages |

### Dialogs

| Dialog | Shortcuts |
| --- | --- |
| Session | `↑/k` `↓/j` move · `Enter` select · `Esc` close |
| Model | `↑/k` `↓/j` move · `←/h` `→/l` switch provider · `Esc` close |
| Permission | `←/→` or `Tab` switch options · `Enter`/`Space` confirm · `a` allow · `A` allow for session · `d` deny |
| Logs page | `Backspace` or `q` return to chat |

## Configuration

Aux looks for configuration in, in order: `$HOME/.aux.json`, `$XDG_CONFIG_HOME/aux/.aux.json`, `./.aux.json`.

### Environment variables

| Variable | Purpose |
| --- | --- |
| `ANTHROPIC_API_KEY` | Claude models |
| `OPENAI_API_KEY` | OpenAI models |
| `GEMINI_API_KEY` | Google Gemini models |
| `GITHUB_TOKEN` | GitHub Copilot models (see [Using GitHub Copilot](#using-github-copilot)) |
| `OPENROUTER_API_KEY` | OpenRouter models |
| `VERTEXAI_PROJECT` / `VERTEXAI_LOCATION` | Google Cloud VertexAI (Gemini) |
| `GROQ_API_KEY` | Groq models |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` | AWS Bedrock (Claude) |
| `AZURE_OPENAI_ENDPOINT` / `AZURE_OPENAI_API_KEY` / `AZURE_OPENAI_API_VERSION` | Azure OpenAI |
| `LOCAL_ENDPOINT` | Self-hosted (OpenAI-compatible) models |
| `SHELL` | Default shell (if not set in config) |

### Configuration file structure

```json
{
  "data": {
    "directory": ".aux"
  },
  "dashboard": {
    "enabled": true,
    "host": "127.0.0.1",
    "port": 0,
    "redaction": "redacted",
    "fullContent": false
  },
  "providers": {
    "openai": { "apiKey": "your-api-key", "disabled": false },
    "anthropic": { "apiKey": "your-api-key", "disabled": false },
    "copilot": { "disabled": false },
    "groq": { "apiKey": "your-api-key", "disabled": false },
    "openrouter": { "apiKey": "your-api-key", "disabled": false }
  },
  "agents": {
    "coder": { "model": "claude-4-sonnet", "maxTokens": 5000 },
    "task": { "model": "claude-4-sonnet", "maxTokens": 5000 },
    "title": { "model": "claude-4-sonnet", "maxTokens": 80 }
  },
  "shell": {
    "path": "/bin/bash",
    "args": ["-l"]
  },
  "mcpServers": {
    "codebase_memory": {
      "type": "stdio",
      "command": "npx",
      "env": [],
      "args": ["-y", "codebase-memory-mcp"]
    }
  },
  "semanticRetrieval": {
    "enabled": true,
    "maxFiles": 8,
    "maxLines": 200,
    "minScore": 0.005,
    "damping": 0.85,
    "maxIterations": 50,
    "epsilon": 0.000001,
    "timeoutSeconds": 15
  },
  "context": {
    "virtualization": "off",
    "artifactThresholdBytes": 0,
    "paging": "off"
  },
  "costGovernor": {
    "mode": "off"
  },
  "ponytail": { "enabled": false },
  "lsp": {
    "go": { "disabled": false, "command": "gopls" }
  },
  "debug": false,
  "debugLSP": false,
  "autoCompact": true
}
```

- **`context.paging`** (`off`/`on`) switches on the demand-paging prompt compiler (dedup of repeated tool output, real per-page exclusion). `context.virtualization` (`off`/`observe`/`on`) controls whether large tool output gets replaced with a compact digest plus an artifact reference.
- **`costGovernor.mode`** (`off`/`observe`/`on`) controls the one-key cost governor. `off` disables it; `observe` measures without changing behavior; `on` actively governs. Compare the effect with `aux eval ab` on a fixed task set run both ways.
- **`autoCompact`** (default `true`) summarizes the conversation automatically at ~95% of the model's context window, starting a new session with the summary instead of hitting a hard context error.
- **`semanticRetrieval`** tunes the AST-backed, PageRank-scored retrieval layer that narrows which files get read into context.

### Local dashboard

See [The dashboard](#the-dashboard) above.

## Supported AI models

### OpenAI
GPT-4.1 family · GPT-4.5 Preview · GPT-4o family · O1 family · O3 family · O4 Mini

### Anthropic
Claude 4 Sonnet · Claude 4 Opus · Claude 3.7 Sonnet · Claude 3.5 Sonnet · Claude 3.5 Haiku · Claude 3 Opus · Claude 3 Haiku

### GitHub Copilot
GPT-3.5 Turbo · GPT-4 · GPT-4o · GPT-4o Mini · GPT-4.1 · Claude 3.5/3.7 Sonnet (+ Thinking) · Claude Sonnet 4 · O1 · O3 Mini · O4 Mini · Gemini 2.0 Flash · Gemini 2.5 Pro

### Google
Gemini 2.5 · Gemini 2.5 Flash · Gemini 2.0 Flash · Gemini 2.0 Flash Lite

### AWS Bedrock
Claude 3.7 Sonnet

### Groq
Llama 4 Maverick (17b-128e-instruct) · Llama 4 Scout (17b-16e-instruct) · Qwen QwQ-32b · DeepSeek R1 Distill Llama 70b · Llama 3.3 70B Versatile

### Azure OpenAI
GPT-4.1 family · GPT-4.5 Preview · GPT-4o family · O1 family · O3 family · O4 Mini

### Google Cloud VertexAI
Gemini 2.5 · Gemini 2.5 Flash

Aux is a **one-key** tool by design: pick one preferred model for your provider of choice and Aux's efficiency systems (Context OS, Cost Governor) work for that model, rather than routing different actions to different models.

## MCP (Model Context Protocol)

Aux extends its tool set through MCP servers — stdio or SSE — configured under `mcpServers`. MCP tools are discovered automatically and follow the same permission model as built-in tools.

```json
{
  "mcpServers": {
    "example": { "type": "stdio", "command": "path/to/mcp-server", "env": [], "args": [] },
    "web-example": {
      "type": "sse",
      "url": "https://example.com/mcp",
      "headers": { "Authorization": "Bearer token" }
    }
  }
}
```

## LSP (Language Server Protocol)

Language servers are configured under `lsp`; Aux connects to them for diagnostics and file-watching. The `diagnostics` tool exposes errors/warnings to the agent. (The client supports the full LSP protocol — completions, hover, definitions — but only diagnostics are currently wired to the agent.)

```json
{
  "lsp": {
    "go": { "disabled": false, "command": "gopls" },
    "typescript": { "disabled": false, "command": "typescript-language-server", "args": ["--stdio"] }
  }
}
```

## Custom commands

Predefined prompts stored as Markdown files:

- **User commands** — `$XDG_CONFIG_HOME/aux/commands/` (typically `~/.config/aux/commands/`) or `$HOME/.aux/commands/`, prefixed `user:`
- **Project commands** — `<project>/.aux/commands/`, prefixed `project:`

The filename (without extension) becomes the command ID; subdirectories nest the ID (`git/commit.md` → `user:git:commit`). Commands can take named arguments with `$NAME` placeholders (uppercase letters/numbers/underscores, starting with a letter) — Aux prompts for each unique placeholder before running.

```markdown
# Fetch Context for Issue $ISSUE_NUMBER

RUN gh issue view $ISSUE_NUMBER --json title,body,comments
RUN git grep --author="$AUTHOR_NAME" -n .
```

Press `Ctrl+K` to open the command dialog, select a command, press Enter.

## Architecture

```text
cmd/                     Cobra CLI: root (TUI/non-interactive), project, profile, task,
                          impact, cost, eval, skill, bundle
internal/app/            Wires every service together; project resolution, LSP startup
internal/config/         Configuration loading (viper) and defaults
internal/db/             Migrations (goose) and raw-SQL stores (ADR 0003)

internal/project/        Stable project identity (VCS remote / root path)
internal/profile/        Scanners + layered, precedence-merged effective profiles
internal/task/            Task Compiler + Coordinator (task lifecycle, multi-repo)
internal/promptcompiler/  Provider-neutral prompt compilation (compatibility + paging)
internal/contextstore/    Typed context pages, versions, bindings, exclusions
internal/artifact/        Content-addressed blob store for large tool output
internal/memory/          Factual/procedural/episodic memory, confidence-gated promotion
internal/impact/          Deterministic AST-derived change-impact graph
internal/relatedproject/  Cross-project dependency graph
internal/multirepo/       Multi-repository task compilation
internal/checkpoint/      Content-addressed, deduplicated change checkpoints
internal/mutationcp/      First-mutation-time auto-checkpoint
internal/validation/      Validation intents, runs, and proof-of-done state
internal/cost/            Per-call ledger, budgets, waste detection, trajectory
internal/skill/           Evaluation-gated learned skills (+ Agent Skills interchange)
internal/govpolicy/       Evaluation-gated learned cost-governor policies
internal/eval/            Offline experiments, replay, and A/B comparison
internal/hooks/           Lifecycle hooks (task/subtask boundaries)
internal/runtime/         Runtime compatibility shell + adapter conformance contract
internal/bundle/          Shareable export/import of skills and policies
internal/worktree/        Git worktree creation for isolated subagent work
internal/eventstore/      Durable, append-only, schema-versioned domain events

internal/llm/             Providers, agent orchestration, tools, prompts, models
internal/dashboard/       Read-only local HTTP dashboard (token-gated)
internal/viewmodel/       Pure, event-backed view-model projections shared by TUI + dashboard
internal/tui/             Bubble Tea terminal UI (components, layout, pages, themes)

internal/session/, message/, history/   Sessions, messages, tracked file versions
internal/lsp/, logging/, permission/, pubsub/   Supporting infrastructure
```

Architectural decisions are recorded as ADRs in [`docs/adr/`](docs/adr/); everything still outstanding, and what may honestly be claimed today, is tracked in [`TODO.md`](TODO.md).

## Using GitHub Copilot

_Experimental._ Requires [Copilot chat](https://github.com/settings/copilot) enabled, plus one of: the VS Code Copilot Chat extension, the `gh` CLI, a Neovim Copilot plugin, or a GitHub token with Copilot permissions. Authenticated tools write a token to `~/.config/github-copilot/[hosts,apps].json`. Alternatively set `GITHUB_TOKEN` or `providers.copilot.apiKey`.

## Using a self-hosted model provider

```bash
LOCAL_ENDPOINT=http://localhost:1235/v1
```

```json
{
  "agents": {
    "coder": { "model": "local.granite-3.3-2b-instruct@q8_0", "reasoningEffort": "high" }
  }
}
```

## Upgrading

Aux keeps everything in one SQLite database at `<data.directory>/aux.db`,
`.aux/aux.db` by default: sessions, messages, tasks, and cost history. Releases
add to its schema, and migrations run automatically the next time you start.

Migrations only ever add, and each one runs inside a transaction. An upgrade
therefore either applies completely or leaves the database exactly where it
was — there is no half-migrated state to repair by hand.

If an upgrade fails, Aux reports the schema version the database is still at and
refuses to start rather than run against a schema it does not understand.
Re-running retries it. If it keeps failing that is a bug worth reporting, with
the error included. As a last resort, moving `aux.db` aside starts a fresh one:
you lose session and cost history, and nothing in your repository.

**Downgrading is not supported.** An older build refuses a database written by a
newer one, instead of opening it and failing later on a column it has never
heard of. Upgrade again, or move the file aside.

## Development

### Prerequisites

- Go 1.24 or higher

### Building from source

```bash
git clone https://github.com/kaiau00/aux-cli.git
cd aux-cli
go build -o aux
./aux
```

### Testing

```bash
go build ./...
go vet ./...
go test ./...          # full suite, deterministic and offline — no provider credentials needed
go test ./... -race
```

Live evaluation gates that need real provider credentials and budget (governed-vs-baseline, skill-vs-baseline) are opt-in via `aux eval ab`; they are never required for normal development.

## Acknowledgments

- [@isaacphi](https://github.com/isaacphi) — for [mcp-language-server](https://github.com/isaacphi/mcp-language-server), the foundation for Aux's LSP client
- [@adamdottv](https://github.com/adamdottv) — for design direction and UI/UX architecture

Thanks to the broader open source community whose tools and libraries make this project possible.

## License

MIT — see [LICENSE](LICENSE).

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please keep tests passing and follow the existing code style.

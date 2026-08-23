# ADR 0005 — Neutral prompt representation and provider adapter boundary

- Status: accepted
- Date: 2026-08-14

## Decision

`internal/promptcompiler` produces a provider-neutral `CompiledPrompt`
(messages + tool set + `ContextManifest`) from durable task/history state
(`internal/promptcompiler/compiler.go`). It never imports a provider package
and never chooses a provider-specific encoding. Provider adapters
(`internal/llm/provider/*`) translate that neutral `[]message.Message` +
`[]tools.BaseTool` into each provider's wire format; they do not participate
in context *selection* — that decision is made once, upstream, by the
compiler (`Compiler` interface: `Compile(Input) CompiledPrompt`).

This keeps a hard boundary: **compilation decides what goes in the prompt;
adapters decide how to say it to a specific API.** The same `CompiledPrompt`
is sent to Anthropic, OpenAI, Gemini, Bedrock, etc. unchanged; only the
adapter differs.

## Alternatives considered

- **Let each provider adapter read history and events directly.** Rejected:
  duplicates context-selection logic per provider and makes the demand-paging
  work (§7.2, §19 PR 11) impossible to apply uniformly — exactly the
  regression this boundary exists to prevent.
- **One monolithic "build the API request" function per provider that also
  does context management.** This was closer to the pre-Phase-1 baseline;
  rejected because it couples token-budget policy to wire format, so
  changing one (e.g. adding demand paging) risks the other.

## Evidence

- Two compilers already exist behind the same `Compiler` interface —
  `CompatibilityCompiler` (renders history unchanged) and `PagingCompiler`
  (dedups + excludes) — and both feed the *same* provider adapters
  unmodified, proving the boundary holds in practice
  (`internal/promptcompiler/compiler.go`, `paging.go`).
- `internal/llm/agent/agent.go`'s turn loop calls `a.compiler.Compile(...)`
  once, then hands the result to `a.provider` — the provider never sees
  `Input` or raw history.

## Consequences

- Adding a new context-selection strategy (e.g. real demand-paging eviction)
  never touches a provider adapter.
- Adding a new provider never touches context selection.
- A behavior that depends on *both* (e.g. provider-specific token limits)
  must be threaded through `Input`/`CompiledPrompt` explicitly rather than
  read directly from provider config inside the compiler.

## Revisit trigger

Revisit if a provider requires context decisions the compiler cannot express
(e.g. a provider-native caching primitive that needs to see raw history) —
extend `CompiledPrompt`/`Input` first rather than breaking the boundary.

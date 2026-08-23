# ADR 0011 — Impact graph engine and parser/LSP/MCP adapter priorities

- Status: accepted
- Date: 2026-08-14

## Decision

The impact graph (`internal/impact`) is built exclusively from **deterministic
Go AST analysis** today (`Source: SourceAST`, `internal/impact/indexer.go`).
The `Edge.Source` enum already defines the intended adapter priority order —
`ast` (implemented) → `lsp` → `manifest` → `git` → `mcp` (all reserved,
unimplemented) — but no adapter beyond AST exists yet. This is a deliberate,
narrow first slice: one language, one deterministic source, proven end to
end (index → analyze → recommend validation), rather than a shallow
multi-source graph with no adapter fully trustworthy.

Crucially, the *consumer* of the graph never assumes completeness: `Analyze`
computes `graphEmpty`/`stale`/`uncovered` and sets `BroadenValidation` (and a
non-zero `Uncertainty`) whenever the graph doesn't cover a changed path —
including every non-Go file today, since only Go is indexed. This is what
lets the graph ship AST-only without silently under-validating non-Go
changes (see ADR 0012, validation safety fallback, for the consumer side of
this same guarantee).

## Alternatives considered

- **Wait for a multi-language/LSP-backed graph before shipping impact
  analysis at all.** Rejected: `BroadenValidation`'s fallback already makes
  a narrow graph *safe* to ship incrementally — every phase after
  Phase 1.0 explicitly builds on "impact analysis broadens validation
  automatically when the graph is absent," so partial coverage was the
  designed-for case from the start, not a stopgap.
- **Use `go/packages` or a full type-checker instead of raw `go/ast`.**
  Not adopted for the first slice: raw AST parsing (imports, declarations,
  test detection) is enough for the currently-shipped edge types
  (`imports`, `contains`, `tests`) without the compilation/build-tag
  complexity `go/packages` would add. Left as a natural upgrade path when a
  edge type needs real type information (e.g. `calls`, `implements`).
- **Prioritize an MCP-based indexer over LSP.** Not adopted: LSP is
  ranked above MCP in the source priority because gopls (already a
  first-class Aux integration, `internal/lsp`) gives symbol-accurate
  `references`/`implements` data directly, whereas an MCP source would be an
  external, less-controlled dependency for the same information.

## Evidence

- `internal/impact/indexer.go` and `impact_test.go` (`TestIndexAndAnalyzeDependents`)
  cover the AST-only path end to end, including `TestAnalyzeBroadensWhenUncovered`,
  which directly proves the fallback for non-Go/uncovered paths.
- The four `Edge`/reserved `Source` constants
  (`SourceAST/SourceLSP/SourceManifest/SourceGit/SourceMCP`,
  `internal/impact/impact.go`) exist precisely so a future adapter slots
  into the existing schema without a migration.

## Consequences

- Any non-Go project currently gets `BroadenValidation: true` for every
  change (graph never covers it) — correct, but means impact-scoped
  (non-broad) validation is Go-only until a second adapter ships.
- Adding an LSP-backed adapter next (per the priority order) should reuse
  `internal/lsp`'s existing gopls client rather than a new language-server
  integration.

## Revisit trigger

Add the LSP adapter once a second language needs impact coverage, or once
Go's AST-only edges (no type info) produce visibly wrong `calls`/`implements`
recommendations in practice.

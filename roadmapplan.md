# Aux End-Product Implementation Plan

## Document purpose

This document translates [`roadmap.md`](roadmap.md) into a codebase-specific engineering plan. It is not another feature wishlist. It defines the architecture, data model, interfaces, migrations, delivery order, tests, observability, rollout controls, and acceptance criteria needed to evolve the current Aux codebase into the intended product.

The target product is a coding agent that becomes materially better and cheaper inside a project over time. A user supplies one API key, chooses one preferred model, and receives the benefits of structured project knowledge, precise context selection, persistent memory, learned workflows, impact-aware validation, and cost governance without manually orchestrating models or rebuilding context every session.

The central promise is:

> Aux remembers the project and avoids paying to rediscover it.

This plan deliberately optimizes for accepted, validated outcomes rather than the number of features shipped or tool calls made.

### Two top-level delivery phases

This implementation plan is organized around two product phases:

1. **Phase 1 — Project intelligence and execution platform.** This contains the runtime, measurement, project profiles, task compilation, Context OS, memory, impact analysis, Cost Governor, Experience Compiler, checkpoints, cross-project work, and Optimization Lab. The existing technical sequence is retained below as Phase 1.0 through Phase 1.7.
2. **Phase 2 — Visual product experience.** This turns Phase 1's state into a coherent TUI workbench and browser workspace. It includes a shared design system, activity visualization, first-class changes and validation, signature context visualization, responsive layouts, accessibility, and visual regression coverage.

Phase 2 is intentionally more than a styling pass. Its implementation depends on Phase 1 exposing truthful task, activity, context, validation, change, and cost state through stable services and events. Visual components must consume those sources rather than infer state from rendered messages.

---

## 1. Final product contract

The finished product must satisfy all of the following:

1. **One-key setup.** The normal path asks for one provider credential and one preferred model. No multi-provider configuration is required.
2. **One primary model.** Aux improves the economics and effectiveness of the user's selected model through better context, caching, retrieval, validation, and reuse. Per-action model routing is not part of the core design.
3. **Automatic project awareness.** Opening a repository activates or creates its profile without requiring the user to curate a large instruction file.
4. **Structured task execution.** Each request becomes an explicit task specification with scope, constraints, likely areas, validation expectations, and budgets.
5. **Demand-paged context.** The model receives the smallest sufficient working set. Large or repeated data is stored as addressable artifacts instead of being resent in full.
6. **Persistent, scoped memory.** Aux retains verified facts, procedures, and prior outcomes with provenance and revision-aware invalidation.
7. **Evidence-based completion.** “Done” means the requested outcome is implemented and the relevant validation evidence is attached to the task.
8. **Measurable cost control.** Aux can explain where tokens, latency, and money were spent and demonstrate savings against the same-model baseline.
9. **Learning with gates.** Learned memories, policies, and skills are candidates until replay or real-world evidence supports promotion.
10. **Replaceable harness.** Project Brain, Context OS, memory, skills, and evaluation are not permanently coupled to one provider client or user interface.

Optional same-key economy modes may be supported later when a provider exposes compatible lower-cost variants, but they must be disabled by default and never be necessary to obtain Aux's core advantages.

---

## 2. Current codebase baseline

Aux already has useful foundations:

- A Go CLI and Bubble Tea TUI.
- A streaming agent loop under `internal/llm/agent`.
- Provider abstraction under `internal/llm/provider`.
- Typed tools with JSON schemas and structured response metadata under `internal/llm/tools`.
- SQLite, goose migrations, and sqlc-generated queries under `internal/db`.
- Persisted sessions, messages, token totals, cost totals, and file versions.
- LSP clients and file watching.
- A semantic retrieval gate with PageRank-style graph scoring.
- Optional codebase-memory MCP discovery.
- Generic in-process pub/sub brokers.
- A token-protected, localhost-only dashboard with SSE updates.
- Existing support for multiple instruction-file conventions and MCP servers.

The main gaps are architectural rather than cosmetic:

| Concern | Current behavior | Required end state |
|---|---|---|
| Session history | Persisted messages are also used directly as the next model prompt | Immutable event/history record compiled into a separate model-facing prompt |
| Task identity | A user message starts generation inside a session | First-class project, task, turn, model-call, tool-execution, and validation records |
| Project knowledge | Instruction files and runtime discovery | Compiled, layered, versioned project profiles |
| Retrieval | Per-prompt in-memory file gate, primarily on `view` | Page-level retrieval across files, symbols, memories, tools, artifacts, skills, and validation evidence |
| Tool output | Usually placed directly into message history | Full artifact stored once; compact typed digest and handle returned to the model |
| Token accounting | Session-level aggregate fields plus provider usage | Per-call ledger including input, output, cache creation/read, effective uncached tokens, cost, and latency |
| Memory | Conversation and file history only | Factual, procedural, and episodic memory with provenance and invalidation |
| File history | Version snapshots scoped to session | Named task checkpoints, branchable restore points, and content-addressed blobs |
| Events | Lossy in-process notification brokers | Durable ordered domain events plus ephemeral UI notifications |
| Validation | Agent-selected commands and textual claims | Planned validation, structured evidence, coverage state, and proof-of-done policy |
| Dashboard | Session/message/tool snapshot | Project, task, context, memory, skill, impact, cost, and evaluation control plane |
| Optimization | No reproducible baseline/replay system | Same-model experiments, deterministic fixtures, trajectory analysis, and guarded promotion |

### Critical architectural constraint

The current generation loop appends messages and tool results to `msgHistory` and then sends that array back to the provider. That means storage history, UI history, and model context are effectively the same object. Persistent memory and context paging will be unreliable until these concerns are separated.

The first hard requirement is therefore an explicit runtime pipeline:

```text
durable task/event history
        |
        v
task compiler -> retrieval plan -> context assembly -> prompt compiler
                                                    |
                                                    v
                                              provider call
                                                    |
                                                    v
tool executor -> artifact store -> task events -> next compilation
```

The database remains the source of truth for what occurred. `PromptCompiler` determines what the model needs to see now.

---

## 3. Target architecture

### 3.1 Domain boundaries

Introduce small packages with explicit ownership. Names can change through an ADR, but responsibilities should not be merged into a single “intelligence” service.

| Package | Responsibility |
|---|---|
| `internal/runtime` | Orchestrates a task turn; contains no provider-specific prompt formatting |
| `internal/eventstore` | Appends and reads durable, ordered domain events |
| `internal/project` | Resolves repository identity, roots, branches, workspaces, and project relationships |
| `internal/profile` | Builds, layers, versions, and activates project profiles |
| `internal/task` | Stores task specifications, steps, statuses, budgets, and completion evidence |
| `internal/promptcompiler` | Produces provider-neutral model input from system policy, task, pages, and selected history |
| `internal/contextstore` | Stores typed context pages, residency, page bindings, and access records |
| `internal/artifact` | Content-addressed storage for large tool outputs and immutable blobs |
| `internal/retrieval` | Ranks context pages and records reasons; absorbs or wraps current `internal/llm/context` logic |
| `internal/memory` | Manages factual, procedural, and episodic memory lifecycle |
| `internal/impact` | Maintains hybrid code graph and proposes affected symbols/tests |
| `internal/validation` | Plans and records checks and determines proof-of-done state |
| `internal/cost` | Maintains budgets, call ledger, forecasts, anomaly detection, and governor decisions |
| `internal/skill` | Imports, versions, retrieves, evaluates, promotes, and rolls back skills |
| `internal/checkpoint` | Creates task checkpoints and branch/delta relationships |
| `internal/eval` | Fixtures, replays, experiment definitions, metrics, and comparisons |

Existing `internal/session`, `internal/message`, `internal/history`, `internal/llm/provider`, `internal/llm/tools`, `internal/lsp`, `internal/dashboard`, and `internal/tui` continue to exist during migration. Do not perform a large package rewrite. Add compatibility adapters, move behavior only when a tested vertical slice requires it, and remove old paths after parity is proven.

### 3.2 Service composition

`internal/app.App` currently constructs and exposes most services. Preserve its public role initially, but introduce a grouped dependency structure so it does not become an untestable god object:

```go
type CoreServices struct {
    Events      *eventstore.Service
    Projects    *project.Service
    Profiles    *profile.Service
    Tasks       *task.Service
    Artifacts   *artifact.Service
    Context     *contextstore.Service
    Memories    *memory.Service
    Impact      *impact.Service
    Validation  *validation.Service
    Cost        *cost.Service
    Skills      *skill.Service
    Checkpoints *checkpoint.Service
}

type AgentServices struct {
    Runtime   *runtime.Service
    Provider  provider.Provider
    Tools     *tools.Registry
    Compiler  *promptcompiler.Compiler
    Retrieval *retrieval.Service
}
```

Construction order must be deterministic: database and artifact backend; durable events; projects/profiles; context and memory; task/cost/validation; tools/provider; runtime; projections; UI.

Every package should accept interfaces for its direct dependencies. Production wiring lives in `internal/app`; tests use in-memory or SQLite-backed fakes.

### 3.3 Stable identifiers

Use sortable UUIDv7 or ULID identifiers generated in the application layer. Do not use database row IDs as cross-system identities.

Required identifiers:

- `project_id`
- `project_revision_id`
- `profile_id` and `profile_version_id`
- `session_id`
- `task_id`
- `turn_id`
- `model_call_id`
- `tool_execution_id`
- `artifact_id`
- `page_id` and `page_version_id`
- `memory_id` and `memory_version_id`
- `skill_id` and `skill_version_id`
- `validation_run_id`
- `checkpoint_id`
- `experiment_id` and `eval_run_id`
- `event_id`

All task-related records must carry `project_id` and `task_id` where applicable. Provider calls, tool calls, pages, validations, costs, and UI events must be correlatable without parsing message JSON.

### 3.4 Runtime contracts

Define provider-neutral contracts before changing provider clients:

```go
type TaskSpec struct {
    Objective          string
    Mode               TaskMode
    Scope              []ScopeRef
    Constraints        []Constraint
    AcceptanceCriteria []Criterion
    ValidationPlan     []ValidationIntent
    Budget             Budget
    ProfileVersionID   string
}

type ContextManifest struct {
    TaskID       string
    CallID       string
    Resident     []PageBinding
    Available    []PageDescriptor
    Evicted      []PageBinding
    TokenEstimate int64
}

type CompiledPrompt struct {
    Messages       []message.Message
    ToolSet        []tools.BaseTool
    Manifest       ContextManifest
    StablePrefixID string
    EstimatedTokens int64
}

type ToolExecutionResult struct {
    Display      tools.ToolResponse
    ModelDigest  string
    ArtifactRefs []ArtifactRef
    PageRefs     []PageRef
    Effects      []Effect
}
```

`PromptCompiler` must be a pure function wherever possible. Given a task snapshot, selected pages, history selection, and tool descriptors, it should produce the same prompt and manifest. Side effects such as page-access logging happen around it, not inside it.

### 3.5 End-to-end task flow

1. Resolve the working directory to a canonical project and revision.
2. Activate or refresh the effective project profile.
3. Create a task and compile the user request into a `TaskSpec`.
4. Establish the initial context working set and cost allocation.
5. Compile the first provider prompt and record every included page/profile entry.
6. Record a `model_call.started` event and stream the response.
7. Convert provider stream events into durable semantic events and ephemeral UI updates.
8. Route tool calls through one execution wrapper.
9. Persist full tool output as an artifact when it exceeds policy thresholds or is reusable.
10. Return a compact typed digest to the model and add/update pages based on effects.
11. Recalculate context residency, remaining budget, and validation state.
12. Compile the next prompt from current task state rather than blindly resending the transcript.
13. Before completion, require the applicable acceptance criteria and validation evidence.
14. Finalize cost and outcome records.
15. Generate memory/skill/policy candidates asynchronously or during idle time.
16. Update dashboard projections and make the complete task replayable.

---

## 4. Persistence and migration plan

### 4.1 Rules for schema evolution

- Add one focused goose migration per domain slice; never ship a single roadmap-wide migration.
- Make migrations forward-only in releases. Test fresh install and upgrade from the oldest supported database.
- Keep raw SQL in `internal/db/sql/<domain>.sql` and regenerate sqlc code.
- Add indexes based on actual query shapes and verify them with `EXPLAIN QUERY PLAN`.
- Store timestamps in UTC and serialize them consistently.
- Use JSON only for genuinely variant payloads. Frequently filtered fields belong in typed columns.
- Content-addressed blobs live outside SQLite by default; SQLite stores identity, metadata, checksums, references, and lifecycle state.
- Foreign keys and uniqueness constraints must enforce scope boundaries.

### 4.2 Migration sequence

#### Migration A: runtime observability

Create:

- `domain_events(event_id, sequence, event_type, schema_version, project_id, session_id, task_id, turn_id, occurred_at, payload_json)`
- `model_calls(model_call_id, project_id, task_id, turn_id, provider, model, status, started_at, finished_at, latency_ms, input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, estimated_cost, error_code)`
- `tool_executions(tool_execution_id, project_id, task_id, turn_id, model_call_id, tool_call_id, tool_name, input_hash, status, started_at, finished_at, latency_ms, is_error, artifact_id, metadata_json)`
- `retrieval_decisions(id, project_id, task_id, model_call_id, candidate_type, candidate_id, score, decision, reason, estimated_tokens, later_used, created_at)`

Indexes:

- domain event sequence, task/sequence, session/sequence, project/event type/time
- model calls by task and start time
- tool executions by task, tool name, input hash
- retrieval decisions by call and candidate

Backfill no fake historical call-level records. Existing session totals remain valid legacy aggregates.

#### Migration B: projects and profiles

Create:

- `projects(project_id, canonical_name, vcs_type, canonical_remote_hash, created_at, last_opened_at)`
- `project_roots(project_id, canonical_path, path_hash, workspace_kind, created_at, last_seen_at)`
- `project_revisions(project_revision_id, project_id, vcs_revision, branch_name, dirty_tree_hash, profile_input_hash, created_at)`
- `profiles(profile_id, owner_type, owner_id, name, precedence, enabled, created_at, updated_at)`
- `profile_versions(profile_version_id, profile_id, source_revision, content_hash, compiler_version, created_at)`
- `profile_entries(entry_id, profile_version_id, entry_type, entry_key, value_json, source_type, source_ref, confidence, token_estimate)`
- `effective_profiles(id, project_id, project_revision_id, task_mode, version_set_hash, compiled_artifact_id, created_at)`

Profile entries must support commands, language/framework facts, architecture summaries, conventions, constraints, tools, skills, validation strategies, instruction excerpts, and workspace relationships.

#### Migration C: first-class tasks

Create:

- `tasks(task_id, project_id, session_id, profile_version_set, objective, mode, status, outcome, created_at, started_at, finished_at)`
- `task_specs(task_id, spec_version, content_json, source_message_id, compiler_version, created_at)`
- `task_steps(step_id, task_id, ordinal, title, status, evidence_json, created_at, updated_at)`
- `task_budgets(task_id, mode, max_cost, max_input_tokens, max_output_tokens, max_wall_ms, max_tool_calls, policy_json)`
- `task_corrections(id, task_id, turn_id, correction_type, content, created_at)`

Keep sessions as a conversation/navigation construct. A session can contain multiple tasks; a task belongs to exactly one project revision at execution time.

#### Migration D: artifact and context stores

Create:

- `artifacts(artifact_id, content_hash, storage_backend, storage_key, media_type, byte_size, compression, sensitivity, created_at, last_accessed_at)`
- `artifact_refs(id, artifact_id, owner_type, owner_id, relation, created_at)`
- `context_pages(page_id, project_id, page_type, stable_key, scope, created_at)`
- `context_page_versions(page_version_id, page_id, content_hash, artifact_id, source_revision, token_count, metadata_json, created_at)`
- `context_bindings(id, task_id, model_call_id, page_version_id, state, rank, reason, token_count, bound_at, evicted_at)`
- `page_accesses(id, task_id, model_call_id, page_version_id, access_type, useful_signal, created_at)`

Uniqueness should deduplicate artifacts by content hash and page versions by `(page_id, content_hash)`.

#### Migration E: memory and impact graph

Create:

- `memories(memory_id, project_id, memory_type, scope, stable_key, state, confidence, created_at, updated_at)`
- `memory_versions(memory_version_id, memory_id, content_json, content_hash, supporting_revision, supersedes_version_id, created_at)`
- `memory_sources(id, memory_version_id, source_type, source_id, source_hash, relation, created_at)`
- `memory_feedback(id, memory_version_id, task_id, outcome, signal, created_at)`
- `graph_nodes(node_id, project_id, node_type, stable_key, display_name, source_revision, metadata_json)`
- `graph_edges(edge_id, project_id, from_node_id, to_node_id, edge_type, weight, source, source_revision)`
- `graph_index_state(project_id, source_revision, indexer_version, status, last_indexed_at)`

Memory state should include `candidate`, `active`, `superseded`, `stale`, `rejected`, and `archived`.

#### Migration F: validation and checkpoints

Create:

- `validation_runs(validation_run_id, task_id, intent_id, validator_type, command_hash, status, started_at, finished_at, exit_code, duration_ms, output_artifact_id)`
- `validation_evidence(id, task_id, criterion_id, validation_run_id, evidence_type, summary, created_at)`
- `checkpoints(checkpoint_id, task_id, parent_checkpoint_id, label, vcs_revision, tree_hash, created_at)`
- `checkpoint_entries(id, checkpoint_id, path, before_artifact_id, after_artifact_id, operation, mode)`

Existing session file versions can remain readable. New task checkpoints reference content-addressed blobs and can later receive an import job for useful legacy history.

#### Migration G: skills, policies, and evaluation

Create:

- `skills(skill_id, owner_type, owner_id, name, scope, state, created_at, updated_at)`
- `skill_versions(skill_version_id, skill_id, content_artifact_id, source_type, source_ids_json, created_at)`
- `skill_evaluations(id, skill_version_id, eval_run_id, baseline_version_id, result, metrics_json, created_at)`
- `governor_policies(policy_id, owner_type, owner_id, task_class, state, policy_json, created_at, updated_at)`
- `experiments(experiment_id, project_id, name, hypothesis, status, config_json, created_at)`
- `eval_cases(eval_case_id, project_id, name, fixture_artifact_id, expected_json, created_at)`
- `eval_runs(eval_run_id, experiment_id, eval_case_id, variant, status, started_at, finished_at, metrics_json, replay_artifact_id)`

Promotion records must identify the baseline, candidate, evaluation set, metric decision, and rollback target.

### 4.3 Artifact filesystem

Use an application data directory, not the repository, with a structure such as:

```text
artifacts/
  sha256/
    ab/
      cd/<full-hash>
```

Writes must be temporary-file plus atomic rename. Verify the hash after write and before first read. Compression policy should be deterministic by media type and size. Garbage collection is mark-and-sweep from `artifact_refs`, page versions, validation runs, skill versions, and checkpoints. Start with manual GC and dry-run reporting; automate only after reference integrity tests exist.

---

## 5. Phase 1.0 — Measurement and runtime foundation

### Objective

Make current behavior observable and introduce the runtime seams required by every later phase without changing user-visible task quality.

### 5.1 Establish baseline fixtures

Create `internal/eval/testdata/baseline/` with small local repositories representing:

- A single-package Go CLI.
- A TypeScript package with scripts and tests.
- A multi-package workspace.
- A repository with explicit instruction files.
- A task requiring one localized edit.
- A task requiring cross-file impact discovery.
- A failed validation and correction loop.
- A large command output and repeated file view.

Each fixture must define objective, initial tree hash, expected changed paths or semantic assertions, required validation, and forbidden out-of-scope effects. Never make live provider calls in the default test suite. Capture provider-neutral replay streams or implement a scripted fake provider.

Record baseline metrics from the existing runtime: total calls, input/output/cache tokens, tool calls, repeated reads, bytes of tool output returned to the model, elapsed time, validation result, and final task success.

### 5.2 Fix accounting semantics

Audit `TrackUsage` and session aggregation before using cost as a product metric. Add per-call records and derive session/task totals from them. Requirements:

- Prompt and completion totals are cumulative, not overwritten by the latest call.
- Cache creation and cache read tokens are retained separately.
- A provider/model price catalog version is stored with cost calculations.
- Unknown pricing produces `cost_unknown`, not zero cost.
- Retries are distinct calls linked by a retry group.
- Cancelled and failed calls retain any usage reported by the provider.
- Streaming latency captures time-to-first-token and total duration.

Add table-driven tests for providers with and without cache metrics.

### 5.3 Durable event store

Implement an append-only event service with:

- Application-assigned event IDs.
- Per-database monotonic sequence.
- Schema version in every event.
- Typed payload structs with a small envelope.
- Transaction-aware append methods.
- Pagination and task/session/project filters.
- Projection checkpoint support for dashboard read models.

The existing generic pub/sub remains the low-latency notification path. Publish only after the database transaction commits. Subscribers may miss notifications and must recover from event sequence; correctness cannot depend on receiving every in-memory event.

Initial event taxonomy:

- `task.created`, `task.compiled`, `task.started`, `task.completed`, `task.failed`, `task.cancelled`
- `turn.started`, `turn.completed`
- `model_call.started`, `model_call.first_token`, `model_call.completed`, `model_call.failed`
- `context.compiled`, `context.page_bound`, `context.page_evicted`, `context.page_fault`
- `tool.started`, `tool.completed`, `tool.failed`
- `artifact.created`, `artifact.reused`
- `validation.started`, `validation.completed`
- `checkpoint.created`
- `memory.candidate_created`, `memory.promoted`, `memory.invalidated`
- `skill.candidate_created`, `skill.promoted`, `skill.rolled_back`
- `budget.allocated`, `budget.warning`, `budget.exhausted`, `governor.decision`

### 5.4 Tool execution wrapper

Add a `tools.Executor` around every `BaseTool.Run` call rather than adding metrics independently to each tool. The executor must:

1. Validate and hash canonical input.
2. Attach project/task/turn/call/execution IDs to context.
3. Emit start/completion/failure events.
4. Measure latency and response size.
5. Preserve existing permission checks inside tools.
6. Store structured metadata.
7. Later delegate large-result virtualization to an artifact policy.
8. Return exactly the legacy response while the feature flag is off.

Extend current context values beyond session/message IDs through a typed execution-context struct. Avoid many private context keys.

### 5.5 Runtime shell

Extract orchestration from the current agent loop behind a `runtime.RunTurn` interface. In Phase 1.0 the compatibility compiler should reproduce the current `msgHistory` behavior exactly. This is a seam, not yet an optimization.

Required tests:

- Existing scripted stream produces identical visible messages.
- Tool calls remain ordered and permission behavior is unchanged.
- Cancellation closes streams and finalizes call status.
- A tool error is persisted and returned to the model.
- Event sequence reconstructs the turn without terminal logs.

### 5.6 Dashboard instrumentation

Add read-only endpoints under `/api/v1` while retaining current routes:

- `/api/v1/tasks`
- `/api/v1/tasks/{id}`
- `/api/v1/tasks/{id}/events`
- `/api/v1/tasks/{id}/calls`
- `/api/v1/tasks/{id}/tools`
- `/api/v1/tasks/{id}/cost`

The dashboard should visualize the existing behavior first. This prevents the team from “optimizing” without a baseline.

### Phase 1.0 exit gate

- A task can be reconstructed from database events.
- Every model and tool call has correlation IDs and duration.
- Token and cost totals reconcile from call to task to session within deterministic rounding.
- Current TUI behavior and baseline success remain unchanged.
- Replay fixtures run in CI without network access.
- Retrieval metrics are persisted rather than only held in memory.

---

## 6. Phase 1.1 — Project Brain and Task Compiler

### Objective

Make repeated work in a known repository begin with structured project knowledge instead of a fresh discovery loop.

### 6.1 Project resolution

Resolution algorithm:

1. Canonicalize the working directory and resolve symlinks.
2. Find the nearest VCS root; if none exists, use the selected workspace root.
3. Normalize the VCS remote without storing embedded credentials.
4. Match an existing project by canonical remote hash, then known root path.
5. Record worktrees as roots of the same project where possible.
6. Record revision, branch, and a bounded dirty-tree fingerprint.
7. Create a local-only project identity when no stable remote exists.

Test nested directories, symlinks, worktrees, remote changes, detached HEAD, no VCS, and monorepo workspace selection.

### 6.2 Profile compiler

Build the profile through independent scanners. Each scanner returns typed entries, sources, confidence, and a source fingerprint. Initial scanners:

- VCS and root layout.
- `go.mod`, `go.work`, `package.json`, workspace manifests, and lockfiles.
- Build/test/lint/typecheck scripts.
- CI workflows.
- Makefiles and task runners.
- Container and deployment manifests.
- Existing Aux instructions.
- `AGENTS.md`, `CLAUDE.md`, `.cursorrules`, Cursor rule directories, and compatible instruction files.
- LSP-supported languages and symbol capabilities.
- MCP availability and tool namespaces.
- Frequently modified areas from bounded git history.

The compiler must not paste all source documents into one prompt. It should produce entries such as:

```json
{
  "type": "validation_command",
  "key": "go.unit",
  "value": {"command": "go test ./...", "scope": "repository"},
  "source": "go.mod+observed_success",
  "confidence": 0.92
}
```

Profile refresh uses source fingerprints. Unchanged scanners reuse their previous entries. Refresh triggers: first open, relevant file watcher event, branch/revision change, explicit command, and a conservative staleness interval.

### 6.3 Layering and precedence

Support this precedence, lowest to highest:

1. Built-in Aux defaults.
2. User profile.
3. Organization profile, when later available.
4. Project profile.
5. Workspace/package profile.
6. Branch profile.
7. Task mode profile.
8. Explicit task overrides.

Every effective entry must retain its contributing source and indicate overrides/conflicts. Lists should have typed merge behavior; scalar values use precedence; prohibitions cannot be silently weakened by a lower layer.

### 6.4 Task modes

Initial modes:

- implementation
- bug diagnosis
- refactor
- test authoring
- code review
- research/explanation
- maintenance/dependency update

Mode inference should start deterministic: request terms, requested output, repository state, and presence of an explicit mode. The model can refine ambiguity within the normal primary-model call; do not add a separate classification call by default.

### 6.5 Task Compiler

The first compiler should combine deterministic extraction with the primary model's existing reasoning. It outputs:

- Normalized objective.
- In-scope and explicitly out-of-scope areas.
- Constraints from effective profile.
- Likely symbols/files/packages as hypotheses, not facts.
- Applicable memories and skills by ID, once those systems exist.
- Proposed steps with dependency order.
- Acceptance criteria.
- Validation intents, escalating from targeted to broad.
- Context and cost budget policy.
- Unknowns that justify exploration.

Store the compiled spec before the first tool call. Later corrections create a new spec version rather than mutating history.

### 6.6 First prompt integration

Add a `profile_enabled` flag and compare:

- Control: current system prompt and transcript.
- Variant: compact effective profile plus stored task spec.

Record profile entries included, tokens consumed, discovery tool calls, first relevant file found, and validation result. A profile does not ship as default merely because it reduces tokens; task success must remain within the agreed threshold.

### 6.7 User interfaces

CLI:

- `aux project show`
- `aux project refresh`
- `aux profile show --effective`
- `aux task show <id>`
- `aux task replay <id> --dry-run`

TUI:

- Show active project, revision, profile version, inferred task mode, and budget mode in a compact status surface.
- Allow opening task spec and source provenance.
- Keep profile generation automatic; do not interrupt normal startup with a wizard.

Dashboard:

- Project list and recent task outcomes.
- Effective profile explorer with layer/source filters.
- Profile version diff.
- Task specification and included-entry view.

### Phase 1.1 exit gate

- Reopening a fixture project reuses its project identity and profile.
- Profile refresh is incremental and deterministic for unchanged inputs.
- Effective profile conflicts are inspectable.
- Known-project tasks use fewer discovery calls than baseline without success regression.
- Users still configure one credential and one primary model.

---

## 7. Phase 1.2 — Context OS and token efficiency

### Objective

Separate durable history from model working memory and minimize uncached input while maintaining outcome quality.

### 7.1 Typed pages

Initial page types:

- `project_manifest`
- `task_spec`
- `plan_state`
- `file_region`
- `symbol_definition`
- `symbol_references`
- `dependency_neighborhood`
- `instruction`
- `memory_fact`
- `memory_procedure`
- `memory_episode`
- `tool_digest`
- `validation_result`
- `diff_summary`
- `skill_manifest`
- `skill_body`

Each type defines stable key, source revision semantics, token estimator, renderer, maximum size, refresh strategy, and usefulness signals. Pages larger than the configured maximum must be split along semantic boundaries, not arbitrary byte offsets when a parser/LSP boundary is available.

### 7.2 Prompt Compiler v1

Stop passing stored messages directly to provider clients. Compile these sections in a stable order:

1. Stable Aux system policy.
2. Stable tool-use policy.
3. Effective project manifest.
4. Task specification and current plan state.
5. Selected factual/procedural pages.
6. Selected code/file/symbol pages.
7. Recent interaction delta and unresolved tool calls.
8. Validation state and remaining budget.

Provider adapters may translate the neutral representation to provider-specific message formats, but they may not choose context.

The compiler records a manifest containing every page version, ordering, rendered token estimate, exclusion reason, and stable-prefix hash. Golden tests should make prompt changes intentional and reviewable.

### 7.3 Minimum resident set

Always resident:

- Current task spec.
- Current plan/step state.
- Applicable hard constraints.
- Files or symbols with uncommitted task edits, represented compactly.
- Unresolved tool call/result pairs required by provider protocol.
- Validation failures not yet addressed.

Evictable:

- Old exploration outputs.
- Superseded file regions.
- Low-value reference lists.
- Completed validation details whose digest is enough.
- Historical conversation already captured in task state.

Pinned pages require a reason and expiry condition. UI pinning is allowed, but unlimited pinning must be prevented by budget feedback.

### 7.4 Retrieval scoring

Start with an explainable weighted function:

```text
score = semantic relevance
      + lexical/path relevance
      + graph proximity
      + task-mode prior
      + recent successful-use prior
      + edited/validation relevance
      - staleness penalty
      - token-cost penalty
      - redundancy penalty
```

Record component scores. Do not train an opaque ranker until the evaluation store contains enough labeled usefulness signals.

Useful signals include later edit, citation in reasoning/result, dependency of an edited symbol, use in validation selection, explicit re-request after eviction, and successful task correlation. “Was loaded” alone is not useful evidence.

### 7.5 Tool-output virtualization

Add an artifact policy to `tools.Executor`:

- Below threshold: return current response.
- Above threshold: persist full output, create a typed digest, and return handle plus relevant excerpts.
- Structured output: preserve fields in metadata and render only task-relevant fields.
- Repeated canonical input with unchanged dependencies: reuse the prior artifact if tool semantics permit caching.
- Errors: preserve diagnostic beginning/end and searchable handle; never hide the exit code.

Add tools or tool actions for artifact search, range retrieval, structured field retrieval, and diff against a previous artifact. Large command output, grep results, test logs, LSP references, and generated manifests are first targets.

### 7.6 Delta-based file pages

File pages are content addressed. If the model already has version A and the file becomes version B:

- Send a bounded diff plus new stable page handle where safe.
- Resend the full semantic region when the diff is ambiguous, too large, or the base page was never resident.
- Track page lineage so the compiler knows which base the model has seen.
- Invalidate symbol pages whose source spans changed.

The current file history service can provide early before/after material, but content identity should move to the artifact store.

### 7.7 Lazy tools and skills

Provider calls currently receive tool definitions. Introduce a tool registry with compact manifests:

- Core tools remain resident.
- MCP namespaces expose compact descriptors first.
- Full schemas are included only when task classification/retrieval selects them.
- Tool selection is logged with reason and token cost.
- A missing tool causes one controlled page/tool-schema fault, not repeated confusion.

Do not lazily hide essential edit/view/validation tools. Measure schema token savings separately from execution savings.

### 7.8 Cache-aware compilation

For providers with prompt caching:

- Keep stable sections byte-identical and ordered first.
- Do not place timestamps or volatile task state in the stable prefix.
- Hash rendered stable prefixes.
- Measure cache creation/read and effective uncached input.
- Detect small compiler changes that destroy a large cache prefix.

Cache optimization must never make provider-specific quirks leak into Project Brain or page storage.

### 7.9 Real context pane

Replace UI-local context bookkeeping with runtime state. Show:

- Resident pages and token estimates.
- Available but unloaded pages.
- Pinned, evicted, stale, and faulted states.
- Why a page was selected.
- Artifact handles and size savings.
- Stable-prefix size and cache-read ratio.

Crossing an item out should update a task-level exclusion or unpin request, subject to minimum-resident safety rules.

### Phase 1.2 exit gate

- Stored transcript and compiled provider input are distinct and inspectable.
- Repeated unchanged file reads do not repeatedly send full content.
- Large tool results are stored once and represented by compact digests.
- Context manifests exactly reconcile with estimated/rendered prompt tokens within provider tokenizer tolerance.
- Same-model fixture success is maintained while uncached input tokens materially decrease.
- Page faults and thrashing are measured and bounded.

---

## 8. Phase 1.3 — Persistent memory and Change Impact Engine

### Objective

Turn completed project work into useful, revision-aware knowledge and improve discovery/validation through a hybrid code graph.

### 8.1 Memory types

**Factual memory** stores verified project facts: architecture, ownership, naming conventions, commands, boundaries, and recurring failure causes.

**Procedural memory** stores successful project-specific methods: how to add an endpoint, run a focused test, update generated code, or validate a migration.

**Episodic memory** stores compact prior-task summaries: objective, changed areas, decisions, failures, correction, validation, and outcome.

Never store an entire raw transcript as a memory. The event store is the transcript source; memory is compiled, bounded knowledge.

### 8.2 Candidate creation

Candidate extraction runs after task finalization or during idle time. Use deterministic extraction first where possible:

- Successful validation command -> procedural candidate.
- Repeated project command -> command-confidence update.
- User correction -> negative memory/candidate invalidation signal.
- Changed symbol plus linked failure/fix -> episodic candidate.
- Repeated profile discovery -> factual candidate.

The selected primary model may propose higher-level candidates as part of the normal completion call or an explicit learning operation. Do not silently create extra paid calls until the Cost Governor can budget them.

Every candidate requires sources, project/revision scope, confidence, and an invalidation strategy.

### 8.3 Promotion and lifecycle

Promotion rules:

- Direct repository fact with current source: may auto-promote at high confidence.
- User-stated preference: promote in the appropriate user/project scope with provenance.
- Inferred fact: require repeated evidence or explicit confirmation.
- Procedure: require at least one successful validated use; higher-risk procedures require more.
- Episode: activate after task outcome is known.

On source changes, mark affected memories stale; do not immediately delete them. Revalidation can create a new version, supersede the old version, or reject it.

Deduplicate by stable key and semantic/content similarity. Consolidation must retain source links and a reversible version chain.

### 8.4 Memory retrieval

Retrieve memory through the same context-page system. Filter by hard scope first, then rank. A suggested initial formula includes task similarity, symbol/workspace overlap, current revision support, prior successful use, confidence, recency, and token cost.

Budgets should cap memory by type and total tokens. A task should usually receive a few strong memories, not a project diary.

### 8.5 Hybrid impact graph

Build graph inputs incrementally:

- Files, directories, packages, modules, symbols, tests, commands, and generated artifacts as nodes.
- Imports, calls/references, implements, contains, builds, tests, co-changes, owns, and generates as typed edges.
- AST/parser data for reliable static relationships.
- LSP definitions/references where available.
- Build/workspace manifests for package relationships.
- Git co-change history as a weak weighted signal.
- Existing codebase-memory MCP graph data behind an adapter.

Keep source and confidence on every edge. Graph refresh should update affected partitions based on changed paths; full rebuild remains a repair operation.

### 8.6 Impact API

Given changed paths/symbols, return:

- Direct dependents.
- Likely affected packages/services.
- Related tests with reason.
- Relevant owners/instructions.
- Risk level and graph uncertainty.
- Recommended targeted validation and conditions requiring broader fallback.

Never use impact selection as the only validation basis until recall is measured. If the graph is stale, incomplete, or uncertain, broaden validation automatically.

### 8.7 UI

Dashboard:

- Memory explorer by type/state/source/revision.
- Memory version and source view.
- “Used in task” and usefulness history.
- Graph neighborhood and change-impact explanation.
- Stale-memory review queue.

TUI:

- Show loaded memory pages and concise reason.
- Show affected-test recommendations.
- Commands to inspect or suppress a bad memory for the current task.

### Phase 1.3 exit gate

- Relevant prior knowledge reduces repeated discovery in held-out repeated tasks.
- Every loaded memory has provenance and current/stale state.
- User corrections lower or invalidate conflicting memory.
- Incremental graph updates are faster than full rebuild on representative projects.
- Impact-selected validation preserves failure detection within the defined recall threshold, with safe fallback.

---

## 9. Phase 1.4 — One-Key Cost Governor and trajectory optimization

### Objective

Reduce total same-model task cost by allocating context, exploration, retries, and validation based on measured value.

### 9.1 Budget model

Support user-facing modes:

- `efficient`: strict context and exploration budgets, targeted validation first.
- `balanced`: default; moderate discovery and validation escalation.
- `maximum`: larger context and broad validation where useful.
- `capped`: explicit money/token/time ceiling.
- `local`: no monetary ceiling assumptions but still controls latency/context waste.

Internally allocate budgets for:

- Initial profile/task context.
- Code discovery.
- Tool schemas.
- Implementation turns.
- Recovery/retry reserve.
- Validation.
- Final explanation.

Budget exhaustion should cause a planned degradation: compress/evict low-value pages, stop redundant exploration, choose focused validation, and clearly report uncovered criteria. It must not abruptly corrupt protocol state.

### 9.2 Governor inputs

- Task mode and estimated complexity.
- Profile maturity and memory confidence.
- Context working-set size.
- Same-model historical cost/success for similar tasks.
- Provider cache behavior.
- Tool latency and result size.
- Current validation/acceptance coverage.
- Remaining cost, token, and time budget.
- Repeated-call, output, and page-fault signals.

### 9.3 Governor actions

- Adjust page/token budget.
- Prefer known profile commands over rediscovery.
- Stop low-yield search branches.
- Expand retrieval after an unexplained failure.
- Preserve or evict pages.
- Reuse safe cached tool artifacts.
- Choose targeted then broader validation.
- Reserve tokens for repair and final response.
- Warn before exceeding an explicit cap.

The Governor never swaps API keys. Any later same-key economy mode is an explicit separate setting and must have independent evaluation.

### 9.4 Waste detectors

Implement deterministic detectors:

- Same tool and canonical input repeated without relevant state change.
- Same file region loaded repeatedly.
- Alternating eviction/page-fault loop.
- Provider call with near-identical compiled prompt and no new evidence.
- Large output artifact never accessed.
- Validation command repeated with unchanged affected inputs.
- Broad repository search after high-confidence target identification.
- Cacheable stable prefix changed by irrelevant volatility.

Start as observable warnings. Enable interventions only after replay tests show improvement.

### 9.5 Speculative context prefetching

Prefetching should hide retrieval latency without bloating prompts. It operates only on page descriptors and the local page/artifact cache; prefetched pages are not automatically made resident or sent to the model.

Candidate signals:

- The current plan's next incomplete step.
- Impact-graph neighbors of a file being edited.
- The test file paired with an implementation file.
- Definition/reference pages implied by an unresolved symbol.
- Validation instructions associated with the current package.
- A page repeatedly faulted after a particular tool or edit action in prior successful trajectories.

Prefetch policy requirements:

- Separate byte/storage budget from model-context budget.
- Cancel low-priority work when the user sends a new instruction or the plan changes.
- Deduplicate by page version/content hash.
- Never run mutating tools or paid model calls as prefetch work.
- Record hit rate, bytes fetched, latency hidden, and unused-prefetch waste.
- Disable or back off when hit rate is poor, filesystem pressure is high, or project revision changes.
- Promote a prefetched page to resident state only through the normal retrieval/budget decision.

Start with deterministic graph/plan prefetch rules. Learned prefetch policies remain observe-only until they beat the no-prefetch baseline on latency without increasing input tokens.

### 9.6 Trajectory records

Compile each task into a trajectory graph:

- State: task spec, resident pages, tree hash, validation state, budget.
- Action: model call, tool call, page operation, edit, validation, correction.
- Outcome: useful evidence, code effect, error, cost, time, acceptance progress.

Compare successful and failed paths by task class. Identify redundant exploration, late retrieval, repeated errors, and validation over/under-spend. Store derived suggestions as policy candidates, not immediate defaults.

### 9.7 Policy learning

Policy scope precedence mirrors profiles: global -> project -> workspace -> task class. Candidate policies require minimum sample size, outcome confidence, and replay comparison. Rollback when moving-window success or cost regresses.

Useful first learned policies:

- Best initial context budget for localized Go fixes.
- Which project test command provides fast early signal.
- When graph-selected validation needs repository-wide fallback.
- Which tool-output thresholds work for the project.
- How many failed searches should trigger retrieval expansion.

### Phase 1.4 exit gate

- Governed runs beat a same-model full-context baseline on accepted changes per dollar.
- Explicit caps are enforced and accounted for.
- Waste detectors have low enough false-positive rates for user-facing explanations.
- At least one project-specific policy beats the global default on held-out fixtures.
- Success remains within the agreed tolerance of maximum mode.

---

## 10. Phase 1.5 — Experience Compiler and self-improving skills

### Objective

Compile repeated successful work into reusable, evaluated procedures without allowing self-generated instructions to silently degrade performance.

### 10.1 Skill representation

A skill version contains:

- Name, purpose, scope, triggers, and exclusions.
- Required profile capabilities.
- Inputs and typed outputs.
- Procedure with decision points.
- Tool and context requirements.
- Validation requirements.
- Known failure/recovery patterns.
- Source trajectories and supporting revisions.
- Evaluation results and promotion state.

Support canonical Agent Skills compatibility at import/export boundaries, while retaining Aux-specific metadata separately.

### 10.2 Candidate discovery

Candidate a skill when trajectories show a repeated, coherent method rather than merely similar user wording. Signals:

- Repeated tool sequence with the same purpose.
- Repeated files/symbol types and validation pattern.
- Same correction or failure recovery across tasks.
- Demonstration through `aux learn`.
- Explicit user-authored workflow.

Avoid skill proliferation by clustering candidates and requiring a minimum value hypothesis.

### 10.3 Evaluation and promotion

For every candidate:

1. Select representative and held-out cases.
2. Run baseline with the current promoted skill set.
3. Run candidate using the same primary model and comparable conditions.
4. Compare success, validation, cost, latency, correction count, and scope violations.
5. Promote only if thresholds pass.
6. Retain the prior version as rollback target.
7. Monitor real usage for regression.

High-variance results remain candidate. A cost win cannot compensate for a meaningful correctness loss unless the user explicitly selects a more aggressive policy.

### 10.4 `aux learn`

Workflow:

1. User names or describes the workflow.
2. Aux records a demonstration task with enhanced trajectory annotation.
3. Aux proposes a structured skill.
4. User can inspect/edit scope and validation.
5. Aux replays against compatible fixtures or historical cases.
6. Skill is activated for project scope when evidence passes.

No second provider key or model configuration is introduced.

### 10.5 Experience outputs beyond skills

The compiler can also propose:

- Memory consolidation.
- Better task-classification heuristics.
- Retrieval priors.
- Tool-output digest rules.
- Validation escalation policies.
- Governor budget allocations.
- Profile corrections.

Each output type uses its own evidence and promotion gate.

### Phase 1.5 exit gate

- A generated or demonstrated skill improves at least one repeated workflow on held-out cases.
- Every promoted skill has baseline comparison and rollback target.
- Skill retrieval adds less context than the workflow rediscovery it replaces.
- Real-world regression can automatically demote or flag a skill.

---

## 11. Phase 1.6 — Checkpoints, cross-project intelligence, and efficient parallel work

### Objective

Reuse context and coordinate related repositories or isolated work branches without multiplying prompt payloads.

### 11.1 Checkpoints and branches

- Create checkpoint before first mutating tool use and at named milestones.
- Store tree/delta references, not duplicate full trees.
- Link task branches as a context DAG.
- Support compare, restore proposal, and branch-from-checkpoint.
- Keep restore explicit and previewable.
- Reuse common page versions between branches; store only new bindings and deltas.

### 11.2 Related-project graph

Derive relationships from workspace files, dependencies, API schemas, deployment configuration, shared remotes, and user declarations. Relationship types include service/client, library/consumer, schema/generator, application/infrastructure, and code/documentation.

Cross-project retrieval must preserve source project and revision. Never merge similarly named symbols from different repositories without identity.

### 11.3 Efficient subagents

Parallelism is valuable only when work is independent. The runtime should:

- Assign a bounded task spec and page manifest.
- Reference shared immutable pages rather than copying text.
- Give each subtask a separate worktree/checkpoint.
- Require structured output: findings, effects, page/artifact refs, validation, unresolved risks.
- Merge result deltas into the parent task.
- Detect overlapping write sets before merge.
- Attribute all calls and costs to parent and child task IDs.

Initial specialist roles: repository mapper, impact analyst, test/validation runner, and independent reviewer. Avoid spawning general agents that repeat the parent’s exploration.

### 11.4 Multi-repository tasks

Compile one product task into repository-specific child specs. Each receives its own effective profile and working set. Cross-repo acceptance criteria live on the parent. Validate interface compatibility at integration boundaries.

### Phase 1.6 exit gate

- Branches share immutable context pages and store only deltas.
- Parallel tasks do not resend shared large context.
- Write conflicts are detected before automatic integration.
- A coordinated fixture change across repositories succeeds with traceable per-repository validation.

---

## 12. Phase 1.7 — Local Optimization Lab and ecosystem

### Objective

Make improvements testable and make Aux's intelligence reusable across runtimes without weakening the one-key normal experience.

### 12.1 Experiment runner

Support comparisons for:

- Prompt compiler versions.
- Retrieval weights.
- Context budgets.
- Artifact thresholds/digest strategies.
- Profile versions.
- Memory/skill inclusion.
- Governor policies.
- Validation strategies.
- Primary-model configurations selected by the user.

Every experiment defines hypothesis, control, variant, fixture set, seeds/repetition policy, metric priority, stopping condition, and artifact retention policy.

### 12.2 Replay modes

- **Deterministic event replay:** no provider calls; tests projections and runtime state transitions.
- **Recorded-provider replay:** feeds captured neutral stream events; tests compilation/tool orchestration.
- **Live evaluation:** makes real calls with explicit cost estimate/approval and tagged results.
- **Counterfactual context replay:** uses recorded tasks to compare page selection without executing mutations.

Never mix live results into deterministic CI baselines.

### 12.3 Stable API and hooks

Version a local API around projects, profiles, tasks, pages, memories, skills, events, cost, and evaluation. Start read-only. Mutation endpoints require explicit permissions and idempotency keys.

Lifecycle hooks:

- project resolved/profile refreshed
- task compiled/started/completed
- before/after model call
- before/after tool execution
- page fault/eviction
- before validation
- memory/skill candidate and promotion

Hooks receive identifiers and artifact references, not secret-rich raw payloads by default.

These are in-process hooks with handlers registered in Go. **User-defined shell hooks -- commands named in a config file and run at these points -- are out of scope and not planned.** A repository-level config that registers one would turn cloning a repository into running its code; that needs a threat model this project has not written, and the observability the hooks were wanted for is covered by the built-in handlers.

### 12.4 Runtime adapters

Define an adapter boundary that can consume Aux task specs/pages and return normalized trajectories. Do not contort core packages around another harness. The native Aux runtime remains first-class; adapters prove that the Project Brain and learning layer are separable.

### 12.5 Shareable profiles and organization packs

After project-local behavior is proven, support exportable bundles containing profile entries, skill manifests, validation intents, and policy defaults. Bundles must declare schema version, required capabilities, source scope, and content hashes. Imported bundles create a new low-precedence layer and cannot silently override repository or explicit task constraints.

Organization profiles add centrally maintained conventions and approved workflows between user and project precedence. Distribution, signing, update channels, and private-registry integration are ecosystem concerns; core profile compilation must continue to work fully offline with local bundles.

### Phase 1.7 exit gate

- An engineer can compare a compiler/retrieval/policy change before making it default.
- Evaluation data identifies compiler, profile, model, and policy versions.
- API versioning and event schema compatibility tests exist.
- A second runtime can consume a project/task manifest without owning Aux's database internals.

---

## 13. Phase 2 — Visual Product Experience

### Objective

Make Aux's usefulness immediately visible. The TUI should become a focused engineering workbench for execution, while the browser dashboard should become the workspace for understanding tasks, context, changes, validation, cost, memory, and optimization.

Phase 2 must answer five questions at a glance:

1. What is Aux doing?
2. What has it discovered?
3. What has it changed?
4. Has it verified the work?
5. How much context, time, and money has it used?

### Phase 2 prerequisites

Do not fabricate these states from display strings. Phase 2 should begin after Phase 1 exposes at least:

- Stable project, session, task, turn, model-call, and tool-execution identifiers.
- Task objective, plan/step state, and task status.
- Ordered model/tool/activity events.
- Changed-file summaries.
- Validation intents, runs, and evidence states.
- Context manifest and token/cost ledger.
- Durable dashboard projections or APIs for the same data.

Individual visual-foundation work can begin earlier, but execution UI must remain backed by truthful runtime state.

### 13.1 Current visual baseline and problems

The codebase already has a meaningful foundation:

- Theme abstraction and multiple terminal themes under `internal/tui/theme`.
- Reusable Lip Gloss styles and Markdown rendering under `internal/tui/styles`.
- Split-pane layout primitives under `internal/tui/layout`.
- Message, editor, context, status, dialog, log, and diff-related components.
- A no-build, embedded dashboard in `internal/dashboard/assets/index.html`.
- SSE updates and token-gated localhost access.

The important visual problems are:

- The TUI is transcript-first, so mechanical operations compete with decisions and outcomes.
- Changed files and validation are not consistently persistent primary surfaces.
- The bottom status bar combines help, context, cost, diagnostics, model, and transient messages in limited space.
- The context pane spends prominent space rendering a long dashboard URL.
- Context is represented mainly as file rows and retrieval scores rather than a comprehensible budget.
- Tool/reasoning detail is available, but its hierarchy is not centered on user-relevant activity stages.
- Narrow terminal behavior depends heavily on squeezing the current split layout.
- The dashboard gives large aggregate cards and an ornamental Live Core more space than the active task.
- Nearly every dashboard surface uses amber/brown, including states that should be semantically distinct.
- The dashboard is one large HTML/CSS/JavaScript file, which will become difficult to evolve and visually test.

### 13.2 Visual direction: warm technical workbench

The intended character is precise, calm, local, and developer-focused. It should not look like another purple AI chat product, and it should not rely on cyberpunk decoration to suggest intelligence.

Use this hierarchy:

- Approximately 90% neutral background, surface, and text roles.
- Approximately 8% muted structural roles such as dividers, labels, and inactive controls.
- Approximately 2% bright Aux amber for brand, focus, and active execution.

Semantic colors remain independent:

- Green: passed, validated, completed successfully.
- Red: failed, destructive, error.
- Yellow: warning, budget pressure, partial validation.
- Blue/cyan: information, context, cached/reused state.
- Amber: Aux identity, current focus, currently active work.
- Gray: queued, inactive, cancelled, unavailable, or historical.

Never encode a state with color alone. Pair it with a label, icon, shape, or pattern.

### 13.3 Shared design tokens

Extend the terminal theme contract from mostly color getters to role-oriented tokens. Preserve backward compatibility while components migrate.

Proposed role groups:

```text
surface.canvas
surface.panel
surface.raised
surface.sunken
surface.selection

text.primary
text.secondary
text.muted
text.inverse

border.default
border.subtle
border.focus

brand.primary
brand.muted

status.active
status.success
status.warning
status.error
status.info
status.inactive

activity.search
activity.read
activity.edit
activity.validate

diff.added
diff.removed
diff.modified
```

Also define:

- Spacing scale for compact terminal and comfortable browser density.
- Border styles for default, focus, selection, warning, and failure.
- Icon vocabulary with ASCII fallbacks.
- Typography roles for browser display, body, label, metadata, code, and numeric data.
- Duration and easing tokens for browser transitions.
- Numeric formatting for tokens, cost, duration, line counts, and percentages.
- Truncation behavior for paths, task titles, model names, and commands.

Browser CSS custom properties should use the same role names. Do not require pixel-identical TUI/browser colors; require identical semantics and hierarchy.

### 13.4 Component state language

Standardize states before redesigning components:

| State | Meaning | Required visual treatment |
|---|---|---|
| queued | Known but not started | Muted icon and label |
| active | Currently executing | Amber focus plus activity indicator |
| waiting | Blocked on provider/tool/user | Distinct waiting label; no fake progress |
| completed | Operation finished | Neutral completion or green when validated |
| failed | Operation failed | Red label/icon with expandable evidence |
| blocked | Cannot continue | Warning/error distinction and reason |
| cancelled | Explicitly stopped | Muted cancelled label |
| stale | Data no longer current | Stale badge and refresh affordance |
| pinned | Must remain resident | Pin icon plus reason |
| cached | Reused without fresh work | Blue/cyan reuse marker |
| unverified | Claimed but not validated | Clear unverified label, never green |
| validated | Acceptance evidence passed | Green validation mark and evidence link |

The same wording should appear in events, TUI, dashboard, screenshots, and documentation.

### 13.5 TUI information architecture

The full-width workbench should contain:

```text
task header
  project / branch / task / active stage / model / context / cost

primary task stream
  user objectives, Aux decisions, grouped activity, results, errors

secondary workbench
  changes / validation / context tabs or stacked summaries

composer
  input, attachments, mode, send/cancel, contextual shortcuts
```

The task stream remains the largest region. Secondary panels expose current state without forcing the user to locate it in the transcript.

#### Proposed TUI components

Add or evolve components along these boundaries:

- `internal/tui/components/task/header.go`
- `internal/tui/components/task/activity.go`
- `internal/tui/components/task/progress.go`
- `internal/tui/components/task/changes.go`
- `internal/tui/components/task/validation.go`
- `internal/tui/components/context/budget.go`
- `internal/tui/components/context/pages.go`
- `internal/tui/components/chat/composer.go`
- `internal/tui/components/navigation/drawer.go`
- `internal/tui/components/core/toast.go`

Do not duplicate domain logic in these components. Introduce projection/view-model structs such as `TaskHeaderVM`, `ActivityGroupVM`, `ChangeSummaryVM`, `ValidationSummaryVM`, and `ContextBudgetVM`. Projection builders subscribe to services/events and produce presentation-ready state.

### 13.6 TUI task header

The header should show, in priority order:

1. Project and branch/worktree.
2. Compact task title.
3. Current task stage and status.
4. Model.
5. Context usage.
6. Current task cost/budget.

On constrained widths, progressively collapse in reverse order:

- Full values become short values.
- Labels become icons with help text.
- Model moves into task details.
- Cost and context become one compact budget indicator.
- Project path becomes repository basename.

The active stage must remain visible. Avoid horizontal status concatenation that produces negative or zero-width content.

### 13.7 Grouped activity stream

Map tool/runtime events into user-understandable groups:

- **Searching:** glob, grep, repository lookup, graph exploration.
- **Reading:** file views, symbol definitions, memory/profile retrieval.
- **Editing:** patch, write, format, generated-file changes.
- **Testing:** tests, lint, typecheck, build, validation inspection.
- **Planning:** task compilation and plan updates.
- **Waiting:** permission, user input, external process, retry backoff.

Default collapsed rows show label, status, meaningful summary, item count, duration, and error state. Expanded rows show exact tool calls, inputs, artifact handles, excerpts, and timestamps.

Rules:

- Consecutive compatible events may group; preserve failures and permissions as distinct rows.
- Never collapse away an error, user decision, destructive operation, or validation result.
- Streaming groups update in place instead of appending noisy new lines.
- Completed mechanical groups become visually quieter than the final response.
- Reasoning visibility remains governed by model/provider capability and existing behavior; activity summaries should not pretend to expose hidden reasoning.

### 13.8 Changes and diff surface

Create a persistent task-level change summary driven by history/checkpoint state:

- Modified, added, deleted, renamed, and generated files.
- Addition/removal counts.
- Uncommitted versus checkpointed state.
- Files with validation coverage.
- Out-of-scope warnings.

Interaction:

- Select a file to open its diff.
- Toggle compact/full diff.
- Jump from activity event to changed file.
- Show binary/generated-file treatment without trying to render invalid text diffs.
- Show “no changes yet” as an informative state, not an empty panel.

Diff colors must preserve conventional green/red semantics rather than remapping both into amber variants.

### 13.9 Validation surface

Display acceptance and validation separately:

- Acceptance criteria coverage.
- Pending/running/passed/failed/skipped/blocked checks.
- Duration and scope of the latest run.
- Whether evidence is current for the latest tree state.
- Clear `unverified` state when no relevant validation has run.

The TUI should never imply successful completion merely because the agent stopped. A green completion treatment requires validated evidence appropriate to the task policy.

### 13.10 Composer redesign

Evolve `internal/tui/components/chat/editor.go` into a distinct composer:

- Visible focused/unfocused boundary.
- Placeholder appropriate to new task versus follow-up.
- Attachment chips with readable removal actions.
- Mode or command indicator when invoking slash/custom commands.
- Send state, cancel/stop state, and disabled reason.
- Compact shortcut hint line that changes with focus/state.
- Multiline affordance that does not depend on discovering a trailing backslash convention.
- External-editor action retained.

Composer growth must cap at a percentage of terminal height and return space to the task stream when empty.

### 13.11 Context as a signature visualization

Replace the file-only mental model with a budget composition:

```text
Context  18.2k / 64k

Task and plan       3.1k
Project knowledge   1.7k
Active code         8.4k
Tool results        2.2k
Memory and skills   1.1k
Recent conversation 1.7k
```

The compact TUI view shows total, pressure, largest categories, pinned count, and fault warning. Expanded view shows pages grouped by type with resident, available, pinned, evicted, stale, rejected, and faulted states.

Each row can expose:

- Human-readable name.
- Token estimate.
- Selection reason.
- Cache/reuse state.
- Source revision/staleness.
- Pin/exclude/reload action when permitted.
- Saved tokens from delta/artifact reuse.

The current `ContextPaneCmp` can serve as a compatibility adapter while runtime-backed page projections are added. Remove local-only “crossed off” behavior once real page controls exist.

### 13.12 Dashboard information architecture

The default dashboard view should prioritize:

1. Active project/task header.
2. Task-stage progress.
3. Changed files, validation, and cost summary.
4. Activity timeline.
5. Context composition and cost trend.
6. Deeper project/session navigation.
7. Raw logs and events.

Recommended desktop layout:

```text
global/project navigation
active task header and stage rail
summary row: changes | validation | context | cost
main: activity timeline / selected detail
secondary: context, diff, evidence, or inspector tabs
```

The current Live Core must either visualize real task-stage/context/validation state with accessible labels or be removed. Lifetime session totals move to analytics rather than occupying the primary active-task row.

### 13.13 Dashboard asset structure

Keep the dashboard dependency-light initially, but split the monolithic asset:

```text
internal/dashboard/assets/
  index.html
  css/tokens.css
  css/base.css
  css/components.css
  js/api.js
  js/state.js
  js/render.js
  js/app.js
```

Continue embedding assets with Go. Add a small internal router/view state only as needed. Do not introduce a JavaScript framework merely for visual polish. Reconsider a framework only when state complexity, component reuse, or testing cost demonstrates a real need.

Add versioned dashboard endpoints/projections for active task summary, timeline, changes, validation, context composition, and cost. SSE messages should carry identifiers and invalidate/refetch or update typed state; avoid injecting raw HTML.

### 13.14 Dashboard views

Deliver views in this order:

1. **Active task:** stage, current activity, changes, validation, context, cost.
2. **Task history:** outcome-oriented list rather than session IDs first.
3. **Task detail:** activity timeline, diff, evidence, prompt/context manifest, cost ledger.
4. **Project Brain:** profile layers, architecture, commands, instructions, revisions.
5. **Memory and skills:** active/candidate/stale states and provenance.
6. **Impact graph:** selected change neighborhood and related validation.
7. **Optimization:** baseline comparisons, Governor decisions, experiment results.

Raw events and logs stay accessible through a diagnostic view. They should not dominate the default experience.

### 13.15 Responsive behavior

TUI modes:

- **Wide:** task stream plus persistent secondary workbench.
- **Medium:** task stream plus one selectable secondary tab.
- **Narrow:** single primary pane; secondary surfaces open as drawers/overlays.
- **Too small:** explicit minimum-size message with essential cancel/quit controls.

Browser breakpoints:

- Wide desktop: multi-column summary and inspector.
- Laptop: two-column main/detail layout.
- Tablet/narrow: single column with sticky task summary and tabbed details.
- Small phone-sized viewport: read-only emergency visibility; no promise of full control until tested.

Persist the user's selected secondary tab per session, but do not persist dimensions that become invalid on another terminal.

### 13.16 Motion and live-state behavior

Use animation only to communicate:

- Active streaming/generation.
- Running tool or validation.
- Connection changes.
- Expansion/collapse or view transition.
- Newly changed state.

Avoid perpetual decorative orbit/pulse animations. Browser animations must honor `prefers-reduced-motion`. Terminal spinners should occupy stable width and stop on cancellation/failure. Do not animate large layout changes during rapid event streams.

### 13.17 Empty, loading, error, and disconnected states

Design explicit states for:

- No project/session/task.
- New task before first response.
- Profile scanning.
- Waiting for provider.
- Waiting for permission/user input.
- Dashboard connecting/reconnecting.
- Stale dashboard projection.
- Redacted content.
- Tool or validation failure.
- Context subsystem unavailable with compatibility fallback.
- Task complete with and without validation.

Each state requires a plain-language explanation and next available action. Avoid empty decorative panels.

### 13.18 Accessibility and terminal compatibility

- Test dark and light themes.
- Test 16-color, 256-color, and true-color terminals where supported.
- Provide ASCII fallbacks for icons whose display width is unreliable.
- Verify keyboard-only operation and predictable focus order.
- Do not reserve single-letter pane actions while the composer is focused.
- Ensure text remains available when panels are hidden in narrow layouts.
- Use browser semantic landmarks, labels, focus-visible styles, and sufficient contrast.
- Do not rely on hover for essential data.
- Respect browser zoom and reduced motion.
- Test long paths, Unicode, wide glyphs, large token counts, unknown model names, and localized number widths.

### 13.19 Visual testing and QA

Add deterministic view-model fixtures for:

- Empty project.
- Active search/read/edit/test.
- Multiple changed files.
- Permission waiting.
- Tool failure.
- Validation pass and failure.
- Context pressure and page fault.
- Cost cap warning.
- Cancelled task.
- Completed validated and completed unverified tasks.
- Disconnected dashboard.

TUI verification:

- Golden snapshots at agreed wide, medium, and narrow sizes.
- Tests for focus, key routing, truncation, scrolling, and responsive mode selection.
- Theme matrix for Aux dark/light plus representative third-party themes.
- Assertions on content/state semantics in addition to snapshots.

Browser verification:

- DOM/state tests for renderers.
- Screenshot regression at standard breakpoints.
- Keyboard/focus tests.
- Automated contrast/accessibility scan where practical.
- SSE disconnect/reconnect and stale-state tests.
- Reduced-motion screenshots or assertions.

Snapshot updates must be reviewed as visual changes, not mechanically accepted.

### 13.20 Phase 2 usability metrics

Measure representative users' ability and time to identify:

- Current task and active operation.
- Which files changed.
- Whether validation passed and remains current.
- Current cost and context pressure.
- Why a context item is loaded.
- Why a task is blocked or failed.
- How to stop work, inspect detail, or continue with a follow-up.

Also track expansion frequency, navigation errors, accidental key actions, time spent locating raw output, and percentage of tasks where the dashboard is opened. These are usability signals, not surveillance requirements; local aggregate measurement is sufficient.

### 13.21 Phase 2 implementation sequence

#### Visual PR 1 — State inventory and design tokens

- Catalog current TUI/dashboard states and components.
- Define shared semantic state language.
- Add role-based terminal tokens and browser CSS variables.
- Preserve existing themes through adapters/defaults.
- Add contrast and terminal palette fixtures.

**Done when:** existing screens render with the new tokens and no behavior change.

#### Visual PR 2 — Event-backed view models

- Add task header, activity, changes, validation, and context projection structs.
- Populate them from Phase 1 services/events.
- Add deterministic fixtures and tests.

**Done when:** both interfaces can consume the same truthful state vocabulary.

#### Visual PR 3 — TUI header and status simplification

- Add task header.
- Reduce the bottom bar to transient status and essential controls.
- Add responsive collapse rules.
- Replace long dashboard URL with compact action/state.

**Done when:** project, task state, context, and cost remain readable at target widths.

#### Visual PR 4 — Grouped activity stream

- Convert runtime/tool events into collapsible activity groups.
- Preserve errors, permissions, and validation evidence.
- Add streaming in-place updates.

**Done when:** routine tool-heavy work is understandable without raw transcript noise.

#### Visual PR 5 — Changes and validation workbench

- Add persistent changes and validation summaries.
- Connect file selection to diff detail.
- Distinguish validated, unverified, stale, failed, and blocked outcomes.

**Done when:** users can determine what changed and whether it works without transcript search.

#### Visual PR 6 — Composer and narrow TUI

- Introduce focused composer treatment and clearer multiline/attachment behavior.
- Add drawer/tab navigation for medium and narrow terminals.
- Verify keyboard routing.

**Done when:** core task operation works at every supported terminal breakpoint.

#### Visual PR 7 — Context budget visualization

- Add category composition and expanded page states.
- Wire pin/exclude/reload to runtime operations.
- Show cache and token-saving signals.

**Done when:** displayed context reconciles with the prompt manifest.

#### Visual PR 8 — Dashboard shell and active-task view

- Split embedded assets.
- Add navigation, active task header, stage rail, summary row, timeline, and inspector.
- Remove or repurpose Live Core.
- Move aggregate telemetry to analytics.

**Done when:** active work is clearly primary and all values trace to runtime projections.

#### Visual PR 9 — Deep dashboard views

- Add task detail, diff/evidence, project profile, memory/skill, graph, and optimization views as APIs permit.
- Add responsive browser navigation.

**Done when:** the browser serves understanding/optimization rather than duplicating the TUI.

#### Visual PR 10 — Visual regression and usability pass

- Add critical screenshot matrices.
- Run task-based usability sessions.
- Fix density, focus, contrast, truncation, motion, and state ambiguity.
- Finalize Aux mark/favicon/README visual treatment.

**Done when:** Phase 2 exit criteria pass.

### Phase 2 exit gate

- A user can answer the five core visual questions without opening raw logs.
- The active task is more prominent than aggregate telemetry.
- Changed files and validation are persistent first-class surfaces.
- Mechanical activity is compact by default and inspectable on demand.
- Context visualization matches actual runtime context and explains selection/cost.
- TUI supports agreed wide, medium, and narrow layouts.
- Browser supports agreed responsive breakpoints, keyboard focus, contrast, and reduced motion.
- Important TUI and dashboard states have deterministic visual regression coverage.
- Aux has a recognizable warm technical identity without using amber for every state.

---

## 14. Validation and proof-of-done architecture

Validation is cross-cutting and should begin in Phase 1.1, then become first-class by Phase 1.3.

### 14.1 Validation intents

Task compilation expresses intent, not a hardcoded command:

- format changed files
- compile affected package
- run related unit tests
- run type checking
- run integration contract
- inspect generated diff
- confirm no out-of-scope changes

The profile resolves intents to project commands. The impact engine narrows scope. The Governor decides escalation within budget. The validation service records evidence.

### 14.2 Proof-of-done state

Each acceptance criterion is:

- `uncovered`
- `claimed`
- `partially_evidenced`
- `validated`
- `blocked`
- `waived_by_user`

The agent cannot silently convert a claim into validated state. Completion policy is task-mode and risk aware. Research tasks may use cited inspection evidence; implementation tasks normally require diff plus relevant executable validation.

### 14.3 Command safety and caching

Validation still passes through existing permission controls. Cache a validation result only when command, environment fingerprint, relevant input tree, and configuration match. Never reuse a passing test result after an affected input changes.

---

## 15. Configuration and feature flags

### 15.1 Configuration evolution

Version `.aux.json` schema. Add typed sections gradually:

```json
{
  "schemaVersion": 2,
  "projectBrain": {"enabled": true, "autoRefresh": true},
  "context": {"mode": "balanced", "artifactThresholdBytes": 12000},
  "memory": {"enabled": true, "autoPromoteVerifiedFacts": true},
  "costGovernor": {"mode": "balanced"},
  "evaluation": {"retainReplaysDays": 30}
}
```

Exact defaults should be derived from benchmarks. Configuration must not require users to understand internal page scores or token allocations.

### 15.2 Rollout flags

Use independently controlled flags:

- durable events
- runtime shell
- project profiles
- task compiler
- prompt compiler
- artifact virtualization
- context paging
- memory retrieval
- impact validation
- cost governor observe-only
- cost governor interventions
- learned skills

Support `off`, `observe`, and `on` where meaningful. Observe mode calculates decisions without changing prompts/actions. Record flag values on every task and replay.

### 15.3 Backward compatibility

- Existing sessions/messages remain readable.
- Legacy sessions can use compatibility compilation.
- New task IDs may be attached lazily to new messages only.
- Existing dashboards routes remain until `/api/v1` reaches parity.
- Existing config works with defaults; schema migration emits clear diagnostics.
- MCP and instruction files remain supported through profile import/adapters.

---

## 16. Testing strategy

### 16.1 Unit tests

- Project identity normalization.
- Profile scanner parsing and incremental fingerprints.
- Layer merge/conflict rules.
- Task-mode inference and task compiler invariants.
- Page keys, splitting, versioning, token estimates, and eviction.
- Retrieval scoring components.
- Artifact threshold, hash, compression, and GC reachability.
- Memory promotion/invalidation/deduplication.
- Impact graph incremental updates.
- Budget allocation and waste detectors.
- Validation coverage transitions.
- Skill promotion/rollback decisions.

### 16.2 Database tests

- Fresh migration to latest.
- Upgrade from each supported schema version.
- Foreign-key and uniqueness enforcement.
- Concurrent event append ordering.
- Transaction rollback does not publish events.
- Projection rebuild from sequence zero.
- Artifact reference integrity and safe GC.

### 16.3 Contract tests

- Every provider maps usage and finish reasons to the neutral contract.
- Every tool works through `tools.Executor` and preserves permissions.
- Prompt compiler output respects provider tool-call/result ordering requirements.
- Dashboard DTOs never expose raw credentials or forbidden sensitive fields.
- Event payloads round-trip for every supported schema version.

### 16.4 Integration tests

- Start project -> compile profile -> create task -> compile prompt -> scripted provider -> tool -> validation -> completion.
- Restart process mid-task and reconstruct state.
- Cancel during provider stream and during tool execution.
- Change a source file and verify page/memory/graph invalidation.
- Repeat a task and verify project knowledge reuse.
- Force artifact/page-store failure and verify safe fallback.
- Run compatibility and optimized paths against the same fixture.

### 16.5 Property and fuzz tests

- Layer merging is deterministic and respects precedence.
- Context selection never exceeds a hard budget except protocol-required minimum.
- Event replay produces the same task state regardless of read page size.
- Artifact content hash always matches retrieved bytes.
- Memory version graph is acyclic.
- Checkpoint DAG is acyclic.
- Malformed tool metadata, MCP schemas, and instruction files cannot panic the runtime.

### 16.6 Performance tests

Measure:

- Profile cold build and incremental refresh by repository size.
- Page creation/retrieval latency.
- Prompt compilation latency and allocation count.
- Graph cold build and one-file incremental update.
- Event append throughput and dashboard projection lag.
- Artifact write/read/compression.
- Database growth across 1,000 tasks and cleanup time.

### 16.7 Live evaluation policy

Live API evaluations are opt-in, budget-capped, tagged, and never required for normal CI. Compare variants with the same provider/model, fixture revision, temperature/settings where controllable, and repeated trials where variance matters.

---

## 17. Observability and success metrics

### 17.1 Primary product metric

**Accepted, validated changes per dollar of model cost.**

Report “accepted” separately from “agent claimed complete.” For local/private use without explicit acceptance, validated completion is a provisional proxy.

### 17.2 Per-task metrics

- Success/outcome and validation coverage.
- Total, input, output, cache creation, and cache read tokens.
- Effective uncached input tokens.
- Cost and pricing-catalog version.
- Wall time, model latency, tool latency, time to first useful edit.
- Tool-call count and repeated-call rate.
- File/page reads, repeated reads, page faults, evictions, and thrashing.
- Context tokens by type.
- Profile/memory/skill hits and later-use signals.
- Artifact bytes stored versus bytes sent to model.
- Targeted versus broad validation time.
- Corrections, retries, reverts, and abandoned tasks.

### 17.3 Compounding metrics

For a project and task class, compare first versus nth task:

- Discovery calls.
- Time to first relevant file.
- Input tokens before first edit.
- Cost to validated completion.
- Reuse of profile, memory, skills, artifacts, and policies.
- Success and correction rates.

### 17.4 Operational targets

Set final numeric SLOs after Phase 1.0 measurement, but initially enforce these directional budgets:

- Runtime/event instrumentation adds negligible visible latency.
- Warm profile activation feels immediate; refresh is background/incremental.
- Prompt compilation is much faster than a network model call.
- Dashboard projection recovers after missed SSE notifications.
- Context-store failure falls back to a bounded compatibility prompt rather than losing the task.
- No optimization can ship without a same-model success comparison.

---

## 18. Phase 1 UI data delivery dependencies

These are the Phase 1 read models and data surfaces that make the richer Phase 2 interface truthful. They may initially use the existing visual shell; Phase 2 owns their final hierarchy and styling.

### Dashboard milestones

1. **Runtime inspector:** calls, tools, tokens, latency, event timeline.
2. **Project Brain:** projects, profile layers, sources, versions, conflicts.
3. **Context inspector:** prompt manifest, resident/evicted pages, artifacts, cache behavior.
4. **Memory and impact:** memory lifecycle, provenance, graph neighborhoods, affected tests.
5. **Cost Governor:** allocations, warnings, interventions, baseline comparison.
6. **Experience Compiler:** candidates, evaluation results, promotions, rollbacks.
7. **Optimization Lab:** experiment setup/results and regression trends.

Keep mutations out of a milestone until its read model is stable. Mutating endpoints need CSRF/origin protections, explicit confirmation, audit events, and idempotency.

### TUI milestones

The TUI should remain task-focused rather than replicate the whole dashboard:

- Persistent compact status: project, mode, task status, cost/budget, validation state.
- Context drawer: resident pages, pins, faults, artifacts.
- Task drawer: spec, plan, criteria, validation evidence.
- Memory/skill indicators with inspect action.
- Checkpoint/branch actions.
- Clear fallback/error states when an optimization subsystem is unavailable.

The browser dashboard is for explanation and comparison; the TUI is for immediate execution control.

---

## 19. Phase 1 implementation program: buildable PR sequence

This is the recommended critical path for the first production-quality vertical slice. Each item should be independently reviewable and leave the main branch working.

### PR 1 — Correlation IDs and per-call ledger

- Add ID generator.
- Add model/tool call tables and sqlc queries.
- Populate call records from current agent loop.
- Reconcile session aggregates.
- Add accounting tests.

**Done when:** current tasks produce complete call ledgers with no prompt behavior change.

### PR 2 — Durable domain events

- Add event table/service and typed envelope.
- Emit task/turn/model/tool lifecycle events.
- Publish notifications after commit.
- Add replay/query tests.

**Done when:** a task timeline is reconstructable after restart.

### PR 3 — Tool executor

- Centralize tool execution, IDs, timing, hashes, and events.
- Route coder and subagent tools through it.
- Preserve permission/history behavior.

**Done when:** all existing tool tests pass and every execution is recorded.

### PR 4 — Runtime compatibility shell

- Extract `RunTurn` orchestration.
- Add scripted fake provider.
- Reproduce current visible messages and tool sequencing.

**Done when:** compatibility replay matches current behavior.

### PR 5 — Project identity and profile schema

- Resolve project/root/revision.
- Add profile tables and services.
- Add basic Go/Node/workspace/instruction scanners.
- Expose read-only CLI/dashboard data.

**Done when:** repeated opens resolve the same project and unchanged inputs reuse a profile version.

### PR 6 — Effective profile compiler

- Implement layer merge/provenance.
- Render compact project manifest.
- Add conflict/diff views.
- Store token estimates.

**Done when:** effective output is deterministic and substantially smaller than raw imported sources on fixtures.

### PR 7 — First-class tasks and task spec

- Add tasks/specs/budgets/criteria representation.
- Add deterministic mode inference.
- Compile user request before tool use.
- Render in UI.

**Done when:** each new user objective has a versioned task spec and profile binding.

### PR 8 — Prompt compiler compatibility mode

- Introduce `CompiledPrompt` and manifest.
- Make provider calls consume compiler output.
- Initially render equivalent transcript context.
- Add golden and protocol-order tests.

**Done when:** history and prompt are separate code paths with parity.

### PR 9 — Artifact store and tool virtualization

- Add content-addressed backend/tables.
- Virtualize large bash/grep/test output.
- Add artifact retrieval tools/actions.
- Measure bytes/tokens saved.

**Done when:** full output is recoverable and the model receives a useful digest/handle.

### PR 10 — Initial typed pages

- Add project manifest, task spec, file region, tool digest, and validation pages.
- Bind pages to calls.
- Show exact manifests in dashboard.

**Done when:** every compiled prompt can be explained page by page.

### PR 11 — Demand paging and delta rereads

- Add retrieval selection, residency, eviction, faults, and version lineage.
- Integrate existing semantic retrieval signals.
- Send file deltas where safe.

**Done when:** repeated-read fixtures reduce uncached input with no success loss.

### PR 12 — First vertical-slice evaluation

- Run control and optimized variants with the same configured model.
- Compare known-project localized and cross-file tasks.
- Publish a dashboard/report view.
- Tune only from recorded evidence.

**Done when:** the central known-project task compilation hypothesis has a measured result.

Do not begin autonomous memory promotion before PR 12. The earlier slices generate the evidence and prompt-control mechanisms memory needs.

---

## 20. Dependency graph and parallel work

Critical path:

```text
call ledger -> durable events -> runtime shell -> prompt compiler
                                  |                 |
project identity -> profiles -> task compiler ------+
                                                    |
artifact store -> typed pages -> demand paging -----+
                                                    v
                                     memory + impact graph
                                                    |
                                                    v
                                      governor + trajectories
                                                    |
                                                    v
                                       experience compiler
```

Safe parallel tracks after contracts stabilize:

- Dashboard projections can follow each domain migration.
- Profile scanners can be developed independently behind one interface.
- Artifact backend and project identity can proceed in parallel.
- Impact graph adapters and memory lifecycle can proceed in parallel after page identity exists.
- TUI surfaces can follow read-only service interfaces.
- Evaluation fixtures should grow continuously, not wait for the final phase.

Avoid parallel edits to the agent loop before the runtime shell and executor interfaces land; that would create conflicting orchestration paths.

### Roadmap traceability

| Product roadmap capability | Primary implementation location in this plan |
|---|---|
| Project Brain and per-project profiles | Phase 1.1; project/profile migrations; profile UI |
| Context OS and demand paging | Phase 1.2; artifact/context migrations; Prompt Compiler |
| Persistent factual/procedural/episodic memory | Phase 1.3; memory lifecycle and retrieval |
| Experience Compiler | Phase 1.5; skill evaluation and broader learned outputs |
| Task Compiler | Phase 1.1; first-class task specification |
| One-Key Cost Governor | Phase 1.4; per-call ledger and budget policies |
| Change Impact Engine | Phase 1.3; hybrid graph and safe validation fallback |
| Speculative context prefetching | Phase 1.4; local page prefetch policy |
| Efficient multi-agent work | Phase 1.6; shared pages, worktrees, structured deltas |
| Validation and proof of done | Cross-cutting Section 14; validation migration/service |
| Trajectory optimization | Phase 1.4; trajectory state/action/outcome graph |
| Checkpoints, branching, and reuse | Phase 1.6; checkpoint DAG and content-addressed deltas |
| Cross-project intelligence | Phase 1.6; related-project graph and child task specs |
| Local Optimization Lab | Phase 1.7; experiment and replay modes |
| Visual system and shared state language | Phase 2.1; design tokens and visual fixtures |
| TUI workbench | Phase 2.2; task/activity/change/validation components |
| Signature context visualization | Phase 2.3; budget composition and page controls |
| Browser workspace | Phase 2.4; active-task hierarchy and deep views |
| Visual QA and usability | Phase 2.5; regression, accessibility, and task studies |
| Shareable skills, profile templates, and organization layers | Phases 1.5 and 1.7 |

---

## 21. Key architectural decisions to record as ADRs

Before or during implementation, add short ADRs for:

1. UUIDv7 versus ULID identifiers.
2. SQLite transaction and event-sequence strategy.
3. Artifact data directory, compression, and retention.
4. Neutral prompt representation and provider adapter boundary.
5. Page identity/version semantics.
6. Tokenizer strategy for estimates across providers.
7. Project identity for forks, worktrees, and remote-less repositories.
8. Profile merge rules.
9. Memory auto-promotion thresholds.
10. Impact graph engine and parser/LSP/MCP adapter priorities.
11. Validation safety fallback.
12. Event schema compatibility policy.
13. Skill format and Agent Skills compatibility.
14. Local API mutation/authentication model.

An ADR should name the decision, alternatives, evidence, consequences, and revisit trigger. Do not delay reversible implementation details waiting for exhaustive design.

---

## 22. Risks and required safeguards

### Prompt compiler causes correctness regression

- Launch in observe/compatibility modes.
- Store control and compiled manifests.
- Use provider protocol contract tests.
- Maintain bounded compatibility fallback.

### Memory becomes stale or noisy

- Require provenance and revision scope.
- Separate candidate from active state.
- Penalize staleness and record corrections.
- Load a bounded number of memories.
- Make promotion and invalidation reversible.

### Paging removes needed information

- Protect minimum resident state.
- Allow page faults and record them.
- Track early retrieval recall and thrashing.
- Broaden context after unexplained failures.

### Cost optimization creates downstream rework

- Optimize validated task cost, not per-call tokens.
- Keep repair/validation reserve.
- Compare against same-model control.
- Do not promote policies on cost alone.

### Graph cost exceeds its value

- Incremental indexing and adapters.
- Track edge source/confidence.
- Use graph as one retrieval signal.
- Fall back when stale or incomplete.

### Self-generated skills degrade behavior

- Candidate state, held-out evaluation, versioning, rollback.
- Limit scope until evidence generalizes.
- Track real-use regressions.

### Database and artifact growth

- Retention classes and reference-aware GC.
- Compressed immutable blobs.
- Projection rebuilds rather than redundant snapshots.
- User-visible storage report before automated deletion.

### Visual state diverges from runtime truth

- Build shared event-backed view models instead of parsing display strings.
- Trace every dashboard metric and status to a durable value.
- Use explicit loading, stale, disconnected, and unverified states.
- Contract-test projection state against task/event replay.
- Do not show green completion without applicable validation evidence.

### Visual density overwhelms the task

- Keep objective, current work, changes, validation, context, and cost in a strict priority order.
- Collapse mechanical detail by default while retaining one-action access.
- Test wide, medium, and narrow layouts with representative task fixtures.
- Use usability timing for the five core interface questions.

---

## 23. What not to build first

- Per-action multi-model routing.
- A marketplace before project-local skills demonstrate value.
- Autonomous skill promotion without replay evidence.
- A vector database before simple hybrid retrieval is measured.
- Complex distributed workers before the local event/runtime model is correct.
- Editable dashboard controls before read models and audit events are stable.
- Broad cross-project memory before revision-aware project memory works.
- Many specialized subagents before shared pages and structured deltas exist.
- A full rewrite of the OpenCode-derived harness.
- Decorative dashboard elements that cannot trace their state or value back to durable runtime data.

---

## 24. Final definition of done

Aux reaches the intended end product when all of these are true:

### Setup and project experience

- A new user can configure one credential and one preferred model.
- Opening a known project activates its current effective profile automatically.
- Switching projects changes context, commands, skills, memory, and policies without manual reconfiguration.

### Execution

- Every request has a first-class task spec and explicit acceptance criteria.
- The model prompt is compiled independently from durable history.
- Context is typed, versioned, demand-paged, and fully inspectable.
- Large/repeated tool outputs use artifact handles and compact digests.
- Relevant code, memories, skills, and validation state are retrieved within an explicit budget.

### Learning

- Verified project facts and procedures survive sessions.
- Stale knowledge is detected from source/revision changes.
- Repeated workflows can produce evaluated skills and project policies.
- Bad memories, skills, or policies have an evidence trail and rollback path.

### Validation

- Completion state distinguishes claim from evidence.
- Impact-aware targeted checks are used where reliable and safely broadened where uncertain.
- Checkpoints make task changes inspectable and branchable.

### Economics

- Every model call has reconciled token, cache, latency, and cost records.
- Users can see why context or validation budget was spent.
- On representative known-project tasks, Aux improves accepted validated changes per dollar against the same model running the compatibility/full-context path.
- Project performance improves measurably as profile, memory, skill, and policy evidence accumulates.

### Platform quality

- Tasks replay from durable events.
- Core services function without the TUI or dashboard.
- Provider/tool/runtime adapters are contract tested.
- Schema upgrades and artifact integrity are tested.
- New optimization policies can be evaluated before becoming defaults.

### Visual experience

- The TUI reads as an active task workbench rather than an undifferentiated transcript.
- The browser prioritizes the active task and outcome over lifetime telemetry.
- Changed files, validation, context pressure, and cost remain visible and understandable.
- Raw activity is compact by default and fully inspectable on demand.
- The design uses restrained amber branding with distinct semantic states.
- Wide, medium, and narrow layouts, keyboard focus, contrast, terminal fallbacks, and reduced motion are verified.
- Critical task states have deterministic visual regression coverage.

When these conditions hold, Aux is no longer merely an OpenCode-derived agent with extra features. It is a project-aware execution and learning system that makes one strong model more useful, more consistent, and less expensive every time it works in the codebase.

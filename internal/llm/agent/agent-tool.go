package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/impact"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/lsp"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/worktree"
)

type agentTool struct {
	deps         Deps
	hooks        *hooks.Registry
	permissions  permission.Service
	impactSvc    *impact.Service
	lspClients   map[string]*lsp.Client
	writeTracker *turnWriteTracker
}

// turnWriteTracker accumulates the file paths each ValidationRunner subagent
// has changed within a single model turn, so a later sibling in the same
// turn can be warned about overlapping writes before its result is trusted
// (detect overlapping write sets before merge). Entries
// are intentionally never evicted: a turn's write-set is a handful of short
// relative paths, and a CLI session's total turn count never grows large
// enough for that to be a real memory concern.
type turnWriteTracker struct {
	mu     sync.Mutex
	byTurn map[string][]string
}

func newTurnWriteTracker() *turnWriteTracker {
	return &turnWriteTracker{byTurn: make(map[string][]string)}
}

// RecordAndCheck returns the paths in paths that overlap with any subtask
// already recorded for turnID, then folds paths into that turn's accumulated
// write-set. An empty turnID or empty paths is a no-op returning nil: with no
// turn correlation there is no sibling scope to compare against.
func (t *turnWriteTracker) RecordAndCheck(turnID string, paths []string) []string {
	if turnID == "" || len(paths) == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	conflicts := checkpoint.DetectWriteConflicts(t.byTurn[turnID], paths)
	t.byTurn[turnID] = append(t.byTurn[turnID], paths...)
	return conflicts
}

const (
	AgentToolName = "agent"
)

type AgentParams struct {
	Prompt string `json:"prompt"`
	// Role specializes the subagent's tool set and prompt. Omit for the generic, unspecialized subagent.
	Role Role `json:"role,omitempty"`
}

func (b *agentTool) Info() tools.ToolInfo {
	return tools.ToolInfo{
		Name:        AgentToolName,
		Description: "Launch a new agent that has access to bounded semantic code exploration tools. The agent should prefer codebase-memory-guided retrieval before broad Glob, Grep, LS, or View usage.\n\nUsage notes:\n1. Agents run one at a time, in the order they appear; issuing several in one message queues them rather than running them in parallel, so prefer one well-scoped agent over several speculative ones\n2. When the agent is done, it reports back via the report tool rather than a free-text message; that structured report is what you receive as this tool's result. To show the user the result, send a text message summarizing it.\n3. Each agent invocation is stateless. You will not be able to send additional messages to the agent, nor will the agent be able to communicate with you outside of its final report. Therefore, your prompt should contain a highly detailed task description for the agent to perform autonomously.\n4. Optionally set role to specialize the agent: repo_mapper (locate relevant files/symbols), impact_analyst (determine blast radius via the impact graph), validation_runner (run validation commands), or reviewer (find issues in current changes). Omit role for a generic exploration agent.\n5. The agent's outputs should generally be trusted\n6. IMPORTANT: Only the validation_runner role can run commands (Bash); no role can Edit or Write files directly. If you want to modify files, do that directly instead of going through the agent.",
		Parameters: map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "The task for the agent to perform",
			},
			"role": map[string]any{
				"type":        "string",
				"enum":        []string{"", string(RoleRepoMapper), string(RoleImpactAnalyst), string(RoleValidationRunner), string(RoleReviewer)},
				"description": "Optional specialist role; omit for a generic exploration agent",
			},
		},
		Required: []string{"prompt"},
	}
}

func (b *agentTool) Run(ctx context.Context, call tools.ToolCall) (tools.ToolResponse, error) {
	var params AgentParams
	if err := json.Unmarshal([]byte(call.Input), &params); err != nil {
		return tools.NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}
	if params.Prompt == "" {
		return tools.NewTextErrorResponse("prompt is required"), nil
	}

	sessionID, messageID := tools.GetContextValues(ctx)
	if sessionID == "" || messageID == "" {
		return tools.ToolResponse{}, fmt.Errorf("session_id and message_id are required")
	}

	parentTaskID := tools.CorrelationFromContext(ctx).TaskID

	// Subagents run with the same task coordinator as the parent: a
	// subagent's own Run(...) call begins and finishes a real task, linked to
	// the parent task via tools.ParentTaskIDContextKey (read by
	// task.Coordinator.Begin). This gives each subagent real checkpointing and
	// cost attribution "for free" through the coordinator's existing lifecycle,
	// rather than opening a fresh top-level task tree.
	subDeps := b.deps
	subCtx := ctx
	if parentTaskID != "" {
		subCtx = context.WithValue(subCtx, tools.ParentTaskIDContextKey, parentTaskID)
	}

	// Only RoleValidationRunner gets an isolated worktree: it is the
	// only role with Bash access and therefore the only one that can cause
	// filesystem side effects a concurrent sibling or the parent could race
	// on. Every other role is read-only and needs to see the parent's actual
	// current state (including uncommitted edits) to do its job — e.g. a
	// Reviewer reviewing "the current changes" — so isolating them would only
	// add git overhead while making their answers less accurate.
	var worktreeDir string
	var worktreeBaseline map[string]string
	var haveWorktree bool
	if params.Role == RoleValidationRunner {
		if dir, before, cleanup, ok := prepareValidationWorktree(ctx, config.WorkingDirectory(), call.ID); ok {
			subCtx = context.WithValue(subCtx, tools.WorkingDirContextKey, dir)
			worktreeDir, worktreeBaseline, haveWorktree = dir, before, true
			defer cleanup()
		}
	}

	collector := &reportCollector{}
	base := TaskAgentTools(b.lspClients, b.permissions)
	var bashTool tools.BaseTool
	if params.Role == RoleValidationRunner && b.permissions != nil {
		bashTool = tools.NewBashTool(b.permissions)
	}
	subTools := roleTools(params.Role, base, b.impactSvc, bashTool, collector)

	subAgent, err := NewAgent(config.AgentTask, subDeps, subTools)
	if err != nil {
		return tools.ToolResponse{}, fmt.Errorf("error creating agent: %s", err)
	}

	session, err := b.deps.Sessions.CreateTaskSession(ctx, call.ID, sessionID, "New Agent Session")
	if err != nil {
		return tools.ToolResponse{}, fmt.Errorf("error creating session: %s", err)
	}

	prompt := params.Prompt
	if fragment := params.Role.rolePrompt(); fragment != "" {
		prompt = fragment + "\n\n" + prompt
	}

	_ = b.hooks.Dispatch(subCtx, hooks.Event{
		Point: hooks.SubtaskBegin, TaskID: parentTaskID, ParentTaskID: parentTaskID,
		SessionID: session.ID, Data: map[string]string{"role": string(params.Role)},
	})

	done, err := subAgent.Run(subCtx, session.ID, prompt)
	if err != nil {
		return tools.ToolResponse{}, fmt.Errorf("error generating agent: %s", err)
	}
	result := <-done

	outcome := "completed"
	if result.Error != nil {
		outcome = "failed"
	}
	_ = b.hooks.Dispatch(subCtx, hooks.Event{
		Point: hooks.SubtaskEnd, TaskID: parentTaskID, ParentTaskID: parentTaskID,
		SessionID: session.ID, Outcome: outcome, Data: map[string]string{"role": string(params.Role)},
	})

	if result.Error != nil {
		return tools.ToolResponse{}, fmt.Errorf("error generating agent: %s", result.Error)
	}

	// If this was an isolated validation run, check what its Bash commands
	// actually changed against every other subtask's write-set already seen
	// in this same model turn (detect overlapping write sets before
	// merge). The comparison scope is deliberately the model turn, not the
	// whole session: siblings the model chose to run together are the ones
	// whose results might get combined, so that's the scope worth flagging.
	var writeConflicts []string
	if haveWorktree {
		if after, err := worktree.Snapshot(worktreeDir); err != nil {
			logging.Warn("failed to snapshot subagent worktree after run", "call_id", call.ID, "error", err)
		} else {
			changed := worktree.DiffSnapshots(worktreeBaseline, after)
			turnID := tools.CorrelationFromContext(ctx).TurnID
			writeConflicts = b.writeTracker.RecordAndCheck(turnID, changed)
		}
	}

	// The parent session's cost is derived from its own ledger plus the cost of
	// direct child (subagent) sessions/tasks in cost.Service.SessionTotals /
	// TaskTotals, so no manual roll-up is needed here.
	if report, ok := collector.get(); ok {
		if len(writeConflicts) > 0 {
			report.Risks = append(report.Risks, fmt.Sprintf(
				"this run touched the same files as another subagent in this turn: %v — check both results before trusting either", writeConflicts))
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return tools.ToolResponse{}, fmt.Errorf("error encoding report: %s", err)
		}
		return tools.NewTextResponse(string(encoded)), nil
	}

	// The model ended without calling the report tool; fall back to its final
	// message so a missed report tool call is never silently empty.
	response := result.Message
	if response.Role != message.Assistant {
		return tools.NewTextErrorResponse("no response"), nil
	}
	return tools.NewTextResponse(response.Content().String()), nil
}

// prepareValidationWorktree creates a git worktree for a RoleValidationRunner
// subagent, checked out at HEAD and then overlaid with repoRoot's live
// working-tree state via worktree.SyncWorkingTree, so validation runs against
// the code actually being worked on rather than the last commit. It fails
// gracefully (ok=false, a no-op cleanup) when repoRoot isn't a git repo or
// worktree creation otherwise fails, so a subagent still runs — just without
// isolation — rather than the tool call failing outright. The returned
// snapshot is the worktree's content hashes immediately after syncing, before
// the subagent's Bash commands run — the baseline Run needs to later compute
// what those commands actually changed via worktree.DiffSnapshots.
func prepareValidationWorktree(ctx context.Context, repoRoot, callID string) (dir string, before map[string]string, cleanup func(), ok bool) {
	noop := func() {}
	id := ids.New()
	path := filepath.Join(os.TempDir(), "aux-subagent-"+id)
	branch := "aux/subagent/" + id

	wt, err := worktree.Create(ctx, repoRoot, path, branch, "")
	if err != nil {
		logging.Warn("subagent worktree unavailable, running without isolation", "call_id", callID, "error", err)
		return "", nil, noop, false
	}
	if err := worktree.SyncWorkingTree(ctx, repoRoot, wt.Path); err != nil {
		logging.Warn("failed to sync working tree into subagent worktree, running without isolation", "call_id", callID, "error", err)
		_ = worktree.Remove(context.Background(), repoRoot, wt.Path, true)
		return "", nil, noop, false
	}
	snap, err := worktree.Snapshot(wt.Path)
	if err != nil {
		logging.Warn("failed to snapshot subagent worktree baseline, running without isolation", "call_id", callID, "error", err)
		_ = worktree.Remove(context.Background(), repoRoot, wt.Path, true)
		return "", nil, noop, false
	}
	return wt.Path, snap, func() {
		cleanupCtx := context.Background()
		if err := worktree.Remove(cleanupCtx, repoRoot, wt.Path, true); err != nil {
			logging.Warn("failed to remove subagent worktree", "path", wt.Path, "error", err)
			return
		}
		// Removing the worktree frees the directory but leaves the branch ref,
		// so without this every subagent run would accumulate one.
		if err := worktree.DeleteBranch(cleanupCtx, repoRoot, wt.Branch); err != nil {
			logging.Warn("failed to delete subagent branch", "branch", wt.Branch, "error", err)
		}
	}, true
}

func NewAgentTool(deps Deps, hookRegistry *hooks.Registry, permissions permission.Service, impactSvc *impact.Service, lspClients map[string]*lsp.Client) tools.BaseTool {
	return &agentTool{
		deps:         deps,
		hooks:        hookRegistry,
		permissions:  permissions,
		impactSvc:    impactSvc,
		lspClients:   lspClients,
		writeTracker: newTurnWriteTracker(),
	}
}

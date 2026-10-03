package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/kaiau00/aux-cli/internal/artifact"
	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/kaiau00/aux-cli/internal/task"
	"github.com/kaiau00/aux-cli/internal/toolexec"
	"github.com/kaiau00/aux-cli/internal/validation"
)

type staticResolver struct{ res project.Resolution }

func (s staticResolver) Resolve(context.Context, string) (project.Resolution, error) {
	return s.res, nil
}

type staticProfiles struct{ eff profile.Effective }

func (s staticProfiles) CompileEffective(context.Context, string, string, string, string, string) (profile.Effective, error) {
	return s.eff, nil
}

// approvingRunner stands in for the shell: it asks the approver exactly as
// validation.ShellRunner does and reports success without running anything.
type approvingRunner struct {
	mu       sync.Mutex
	approver validation.Approver
	session  string
	ran      []string
}

func (r *approvingRunner) Run(_ context.Context, command string) (validation.CommandResult, error) {
	if !r.approver.Request(permission.CreatePermissionRequest{
		SessionID: r.session, ToolName: "validation", Action: "execute", Path: "/repo", Fingerprint: command,
	}) {
		return validation.CommandResult{}, permission.ErrorPermissionDenied
	}
	r.mu.Lock()
	r.ran = append(r.ran, command)
	r.mu.Unlock()
	return validation.CommandResult{ExitCode: 0}, nil
}

// writeTool records a file version the way edit/write/patch do.
type writeTool struct{ files history.Service }

func (w writeTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: "write"} }
func (w writeTool) Run(ctx context.Context, _ tools.ToolCall) (tools.ToolResponse, error) {
	sessionID, _ := tools.GetContextValues(ctx)
	if _, err := w.files.Create(ctx, sessionID, "/repo/main.go", "package main\n"); err != nil {
		return tools.ToolResponse{}, err
	}
	return tools.NewTextResponse("written"), nil
}

type validationHarness struct {
	agent       *agent
	sessionID   string
	events      eventstore.Service
	memories    *memory.Service
	skills      *skill.Service
	validations *validation.Store
	runner      *approvingRunner
	perms       permission.Service
}

func newValidationHarness(t *testing.T, turns ...[]provider.ProviderEvent) *validationHarness {
	t.Helper()
	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	events := eventstore.NewService(conn)
	files := history.NewService(q, conn)
	memories := memory.NewService(memory.NewStore(conn), events)
	valStore := validation.NewStore(conn)
	skills := skill.NewService(skill.NewStore(conn), events)
	checkpoints := checkpoint.NewService(checkpoint.NewStore(conn),
		artifact.NewService(artifact.NewFSBackend(t.TempDir()), artifact.NewStore(conn)), events)

	res := project.Resolution{
		Project:  project.Project{ID: "proj-1"},
		Root:     project.Root{CanonicalPath: "/repo"},
		Revision: project.Revision{ID: "rev-1", VCSRevision: "abc1234"},
	}
	eff := profile.Effective{VersionSetHash: "v", Entries: []profile.EffectiveEntry{
		{Type: profile.EntryValidationCommand, Key: "go.test", ValueJSON: `{"command":"go test ./..."}`},
	}}
	coord := task.NewCoordinator(staticResolver{res}, staticProfiles{eff}, task.NewStore(conn), events, "/repo").
		WithMemory(memories).WithValidation(validation.NewService(valStore, events)).
		WithSkills(skills).WithCheckpoints(files, checkpoints)

	perms := permission.NewPermissionService()
	runner := &approvingRunner{approver: perms}
	a := &agent{
		Broker:      pubsub.NewBroker[AgentEvent](),
		sessions:    sessions,
		messages:    messages,
		ledger:      cost.NewService(conn),
		events:      events,
		coordinator: coord,
		compiler:    promptcompiler.NewCompatibilityCompiler(),
		pages:       contextstore.NewStore(conn),
		permissions: perms,
		executor:    tools.NewExecutor(toolexec.NewRecorder(toolexec.NewStore(conn), events), nil),
		tools:       []tools.BaseTool{writeTool{files: files}},
		provider:    provider.NewMockProvider(mockModel(), turns...),
		newValidationRunner: func(_, sessionID string, _ validation.Approver) validation.Runner {
			runner.session = sessionID
			return runner
		},
	}
	sess, err := sessions.Create(context.Background(), "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &validationHarness{agent: a, sessionID: sess.ID, events: events, memories: memories,
		skills: skills, validations: valStore, runner: runner, perms: perms}
}

func editThenFinish() [][]provider.ProviderEvent {
	usage := provider.TokenUsage{InputTokens: 5, OutputTokens: 2}
	return [][]provider.ProviderEvent{
		provider.ToolCallTurn(usage, message.ToolCall{ID: "w1", Name: "write", Input: "{}", Finished: true}),
		provider.TextTurn("Added Sub.", usage),
	}
}

func (h *validationHarness) run(t *testing.T) AgentEvent {
	t.Helper()
	return h.runCtx(t, context.Background())
}

func (h *validationHarness) runCtx(t *testing.T, ctx context.Context) AgentEvent {
	t.Helper()
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, h.sessionID)
	result := h.agent.processGeneration(ctx, h.sessionID, "Add a Sub function with a test", nil)
	if result.Error != nil {
		t.Fatalf("processGeneration: %v", result.Error)
	}
	return result
}

func (h *validationHarness) taskID(t *testing.T) string {
	t.Helper()
	evs, err := h.events.List(context.Background(), eventstore.Filter{Types: []eventstore.Type{eventstore.TaskCreated}})
	if err != nil || len(evs) != 1 {
		t.Fatalf("task.created events = %d, err %v", len(evs), err)
	}
	return evs[0].TaskID
}

func (h *validationHarness) skipReasons(t *testing.T) []string {
	t.Helper()
	evs, err := h.events.List(context.Background(), eventstore.Filter{Types: []eventstore.Type{eventstore.ValidationSkipped}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var out []string
	for _, e := range evs {
		var p eventstore.ValidationPayload
		_ = json.Unmarshal(e.Payload, &p)
		out = append(out, p.Reason)
	}
	return out
}

func TestTaskThatChangedFilesIsValidatedAndLearnedFrom(t *testing.T) {
	h := newValidationHarness(t, editThenFinish()...)
	h.perms.AutoApproveSession(h.sessionID)
	result := h.run(t)
	taskID := h.taskID(t)

	if len(h.runner.ran) != 1 || h.runner.ran[0] != "go test ./..." {
		t.Fatalf("runner ran %v; want go test ./... once", h.runner.ran)
	}
	runs, _ := h.validations.RunsForTask(context.Background(), taskID)
	evidence, _ := h.validations.EvidenceForTask(context.Background(), taskID)
	if len(runs) != 1 || runs[0].Status != validation.StatusPassed || len(evidence) == 0 {
		t.Fatalf("runs %+v evidence %d; want one passed run with evidence", runs, len(evidence))
	}
	if !strings.Contains(result.Message.Content().Text, "`go test ./...`: passed") {
		t.Fatalf("transcript is missing the validation report:\n%s", result.Message.Content().Text)
	}

	procs, _ := h.memories.Retrieve(context.Background(), "proj-1", []memory.Type{memory.Procedural}, 0)
	if len(procs) != 1 || procs[0].StableKey != "validate:go test ./..." {
		t.Fatalf("procedural memories = %+v; want one for go test ./...", procs)
	}
	cands, _ := h.skills.Candidates(context.Background())
	if len(cands) != 1 {
		t.Fatalf("skill candidates = %d; want 1", len(cands))
	}
}

func TestTaskWithoutFileChangesSkipsValidation(t *testing.T) {
	h := newValidationHarness(t, provider.TextTurn("It adds numbers.", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}))
	h.perms.AutoApproveSession(h.sessionID)
	h.run(t)

	if len(h.runner.ran) != 0 {
		t.Fatalf("runner ran %v for a task that changed nothing", h.runner.ran)
	}
	if got := h.skipReasons(t); len(got) != 1 || got[0] != task.SkipNoFileChange {
		t.Fatalf("validation.skipped reasons = %v; want [%q]", got, task.SkipNoFileChange)
	}
}

func TestSubagentTaskIsNotValidated(t *testing.T) {
	h := newValidationHarness(t, editThenFinish()...)
	h.perms.AutoApproveSession(h.sessionID)
	h.runCtx(t, context.WithValue(context.Background(), tools.ParentTaskIDContextKey, "parent-task"))
	if len(h.runner.ran) != 0 {
		t.Fatalf("a subagent ran validation %v; the parent validates", h.runner.ran)
	}
}

func TestDeniedValidationIsRecordedAsSkippedNotFailed(t *testing.T) {
	h := newValidationHarness(t, editThenFinish()...)
	h.perms.DenyAllSession(h.sessionID)
	result := h.run(t)
	taskID := h.taskID(t)

	runs, _ := h.validations.RunsForTask(context.Background(), taskID)
	if len(runs) != 1 || runs[0].Status != validation.StatusSkipped {
		t.Fatalf("runs = %+v; want one skipped run", runs)
	}
	evidence, _ := h.validations.EvidenceForTask(context.Background(), taskID)
	if len(evidence) != 0 {
		t.Fatalf("a denied command must attach no evidence, got %d", len(evidence))
	}
	if !strings.Contains(result.Message.Content().Text, "`go test ./...`: denied") {
		t.Fatalf("transcript should say the command was denied:\n%s", result.Message.Content().Text)
	}
	procs, _ := h.memories.Retrieve(context.Background(), "proj-1", []memory.Type{memory.Procedural}, 0)
	if len(procs) != 0 {
		t.Fatalf("a denied command must not become procedural memory: %+v", procs)
	}
	if denied := h.perms.Denied(h.sessionID); len(denied) != 1 || denied[0].ToolName != "validation" {
		t.Fatalf("permission denials = %+v; want the one validation request", denied)
	}
}

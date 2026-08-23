package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kaiau00/aux-cli/internal/checkpoint"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/history"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/ids"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/memory"
	"github.com/kaiau00/aux-cli/internal/multirepo"
	"github.com/kaiau00/aux-cli/internal/profile"
	"github.com/kaiau00/aux-cli/internal/project"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/relatedproject"
	"github.com/kaiau00/aux-cli/internal/skill"
	"github.com/kaiau00/aux-cli/internal/validation"
)

// RelatedProjectReader looks up outgoing related-project edges for a project.
type RelatedProjectReader interface {
	From(ctx context.Context, projectID string) ([]relatedproject.Relation, error)
}

// FileLister lists the recorded file versions for a session (history service).
type FileLister interface {
	ListBySession(ctx context.Context, sessionID string) ([]history.File, error)
	ListLatestSessionFiles(ctx context.Context, sessionID string) ([]history.File, error)
}

// CheckpointCreator captures a checkpoint from a set of file changes.
type CheckpointCreator interface {
	Create(ctx context.Context, taskID, parentID, label, revision string, changes []checkpoint.FileChange) (checkpoint.Checkpoint, error)
}

// ProjectResolver resolves a working directory to a project identity.
type ProjectResolver interface {
	Resolve(ctx context.Context, dir string) (project.Resolution, error)
}

// ProfileCompiler compiles a project's effective profile.
type ProfileCompiler interface {
	CompileEffective(ctx context.Context, projectID, revisionID, root, sourceRevision, taskMode string) (profile.Effective, error)
}

// EventSink appends domain events.
type EventSink interface {
	Append(ctx context.Context, in eventstore.Append) (eventstore.Event, error)
}

// Coordinator turns each new user objective into a compiled, versioned task
// bound to the current project revision and effective profile, before any tool
// runs (roadmapplan.md §3.5, §6.5). It caches project resolution per session.
type Coordinator struct {
	resolver    ProjectResolver
	profiles    ProfileCompiler
	store       *Store
	events      EventSink
	memories    *memory.Service
	skills      *skill.Service
	validations *validation.Service
	history     FileLister
	checkpoints CheckpointCreator
	hooks       *hooks.Registry
	related     RelatedProjectReader
	workdir     string

	mu     sync.Mutex
	cached *project.Resolution
}

// NewCoordinator builds a task coordinator. events may be nil.
func NewCoordinator(resolver ProjectResolver, profiles ProfileCompiler, store *Store, events EventSink, workdir string) *Coordinator {
	return &Coordinator{resolver: resolver, profiles: profiles, store: store, events: events, workdir: workdir}
}

// WithMemory attaches a memory service so completed tasks produce episodic
// memory candidates and active memories surface as available context. Optional.
func (c *Coordinator) WithMemory(m *memory.Service) *Coordinator {
	c.memories = m
	return c
}

// WithSkills attaches a skill service so a completed task proposes skill
// candidates from the commands it validated. Candidates are inert until they
// pass an evaluation and are promoted, so this never changes agent behaviour on
// its own. Optional.
func (c *Coordinator) WithSkills(s *skill.Service) *Coordinator {
	c.skills = s
	return c
}

// WithValidation attaches a validation service so procedural memory can be
// extracted from commands that validated successfully during a task. Optional.
func (c *Coordinator) WithValidation(v *validation.Service) *Coordinator {
	c.validations = v
	return c
}

// WithCheckpoints wires history + checkpoint capture so a completed task
// automatically records what it changed (roadmapplan.md §11.1). Both are
// required for capture; either nil disables it. Optional.
func (c *Coordinator) WithCheckpoints(files FileLister, checkpoints CheckpointCreator) *Coordinator {
	c.history = files
	c.checkpoints = checkpoints
	return c
}

// WithHooks wires a lifecycle-hook registry so local extensions can observe task
// begin/end (roadmapplan.md §12.3). Optional; nil dispatch is a no-op.
func (c *Coordinator) WithHooks(r *hooks.Registry) *Coordinator {
	c.hooks = r
	return c
}

// WithRelatedProjects attaches the related-project graph so a task's manifest
// includes the projects it depends on or is consumed by (roadmapplan.md §11.2).
// Optional.
func (c *Coordinator) WithRelatedProjects(r RelatedProjectReader) *Coordinator {
	c.related = r
	return c
}

func (c *Coordinator) resolution(ctx context.Context) (project.Resolution, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil {
		return *c.cached, nil
	}
	res, err := c.resolver.Resolve(ctx, c.workdir)
	if err != nil {
		return project.Resolution{}, err
	}
	c.cached = &res
	return res, nil
}

// Begin resolves the project, compiles the effective profile and a task spec,
// persists the task/spec/budget, emits task lifecycle events, and returns a
// context carrying the task and project ids for downstream correlation. When
// ctx carries a tools.ParentTaskIDContextKey (set by the agent tool before
// spawning a subagent, roadmapplan.md §11.3), the new task is linked as that
// task's child rather than starting a fresh top-level task tree.
func (c *Coordinator) Begin(ctx context.Context, sessionID, objective string) (context.Context, string, error) {
	res, err := c.resolution(ctx)
	if err != nil {
		return ctx, "", err
	}
	parentTaskID, _ := ctx.Value(tools.ParentTaskIDContextKey).(string)
	return c.beginWithParent(ctx, sessionID, objective, res, parentTaskID)
}

// beginWithParent is Begin's implementation, generalized to (a) an explicit
// project resolution rather than the coordinator's own cached one, and (b) an
// optional parent task id. It backs both Begin (res = the coordinator's own
// project, no parent) and BeginMultiRepo (one call per child repository,
// parent = the multi-repo parent task).
func (c *Coordinator) beginWithParent(ctx context.Context, sessionID, objective string, res project.Resolution, parentTaskID string) (context.Context, string, error) {
	eff, err := c.profiles.CompileEffective(ctx, res.Project.ID, res.Revision.ID, res.Root.CanonicalPath, res.Revision.VCSRevision, "")
	if err != nil {
		return ctx, "", err
	}

	mode := InferMode(objective)
	spec := Compile(objective, mode, eff)
	taskID := ids.New()
	spec.TaskID = taskID
	spec.SpecVersion = 1
	now := time.Now().UnixMilli()

	t := Task{
		ID:                taskID,
		ProjectID:         res.Project.ID,
		SessionID:         sessionID,
		ProjectRevisionID: res.Revision.ID,
		ProfileVersionSet: eff.VersionSetHash,
		Objective:         spec.Objective,
		Mode:              mode,
		Status:            StatusCompiled,
		CreatedAt:         now,
		ParentTaskID:      parentTaskID,
	}
	if err := c.store.CreateTask(ctx, t); err != nil {
		return ctx, "", err
	}
	if err := c.store.SaveSpec(ctx, spec, "", now); err != nil {
		return ctx, "", err
	}
	if err := c.store.SaveBudget(ctx, taskID, spec.Budget); err != nil {
		return ctx, "", err
	}
	if err := c.store.SetStatus(ctx, taskID, StatusRunning, "", now, 0); err != nil {
		return ctx, "", err
	}

	c.emit(ctx, res.Project.ID, sessionID, eventstore.TaskCreated, eventPayload(t, eff.VersionSetHash))
	c.emit(ctx, res.Project.ID, sessionID, eventstore.TaskCompiled, eventPayload(t, eff.VersionSetHash))
	c.emit(ctx, res.Project.ID, sessionID, eventstore.TaskStarted, eventPayload(t, eff.VersionSetHash))

	ctx = context.WithValue(ctx, tools.TaskIDContextKey, taskID)
	ctx = context.WithValue(ctx, tools.ProjectIDContextKey, res.Project.ID)
	_ = c.hooks.Dispatch(ctx, hooks.Event{Point: hooks.TaskBegin, TaskID: taskID, SessionID: sessionID})
	// Offer the compiled project manifest (plus any relevant prior memory and
	// related-project context) and the task spec to the prompt compiler as
	// available context pages (§7.1, §8.4, §11.2).
	manifest := eff.Manifest + c.memorySection(ctx, res.Project.ID) + c.relatedSection(ctx, res.Project.ID)
	ctx = promptcompiler.WithProjectContext(ctx, manifest, spec.RenderText())
	return ctx, taskID, nil
}

// BeginMultiRepo compiles a product objective into a parent task (in the
// coordinator's own project) plus one child task per repository target, each
// bound to its own project so it resolves that project's own profile and
// working set (roadmapplan.md §11.4). Children are linked to the parent via
// ParentTaskID. A target with no resolvable root, or a child that fails to
// begin, is recorded as an error but does not stop the remaining children —
// the caller sees exactly which repositories are ready to work and which are
// not, rather than losing the whole multi-repo task to one bad target.
func (c *Coordinator) BeginMultiRepo(ctx context.Context, sessionID, objective string, targets []multirepo.RepoTarget) (multirepo.ParentPlan, []Task, error) {
	plan := multirepo.Compile(objective, targets)

	parentRes, err := c.resolution(ctx)
	if err != nil {
		return plan, nil, fmt.Errorf("failed to resolve parent project: %w", err)
	}
	_, parentTaskID, err := c.beginWithParent(ctx, sessionID, objective, parentRes, "")
	if err != nil {
		return plan, nil, fmt.Errorf("failed to begin parent task: %w", err)
	}

	rootByProject := make(map[string]string, len(targets))
	for _, tgt := range targets {
		rootByProject[tgt.ProjectID] = tgt.Root
	}

	var children []Task
	var errs []error
	for _, child := range plan.Children {
		root, ok := rootByProject[child.ProjectID]
		if !ok || root == "" {
			errs = append(errs, fmt.Errorf("no root configured for target project %s", child.ProjectID))
			continue
		}
		childRes, err := c.resolver.Resolve(ctx, root)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to resolve child project at %s: %w", root, err))
			continue
		}
		_, childTaskID, err := c.beginWithParent(ctx, sessionID, child.Objective, childRes, parentTaskID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to begin child task for project %s: %w", child.ProjectID, err))
			continue
		}
		childTask, err := c.store.GetTask(ctx, childTaskID)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to load child task %s: %w", childTaskID, err))
			continue
		}
		children = append(children, childTask)
	}
	return plan, children, errors.Join(errs...)
}

// memorySection renders a bounded set of active memories for the manifest, so
// prior project knowledge is available without re-discovery (roadmapplan.md §8.4).
func (c *Coordinator) memorySection(ctx context.Context, projectID string) string {
	if c.memories == nil {
		return ""
	}
	mems, err := c.memories.Retrieve(ctx, projectID, nil, 5)
	if err != nil || len(mems) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Prior knowledge:\n")
	for _, m := range mems {
		fmt.Fprintf(&b, "  - [%s] %s\n", m.Type, m.StableKey)
	}
	return b.String()
}

// relatedSection renders the projects this project depends on (or is consumed
// by), so cross-project work can be recognized without rediscovering the graph
// each time (roadmapplan.md §11.2). Each edge preserves the related project's
// own identity; nothing here merges symbols across projects.
func (c *Coordinator) relatedSection(ctx context.Context, projectID string) string {
	if c.related == nil {
		return ""
	}
	rels, err := c.related.From(ctx, projectID)
	if err != nil || len(rels) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Related projects:\n")
	for _, r := range rels {
		fmt.Fprintf(&b, "  - [%s] %s (via %s)\n", r.RelationType, r.ToProject, r.Source)
	}
	return b.String()
}

// Finish marks a task completed and emits task.completed.
func (c *Coordinator) Finish(ctx context.Context, taskID, outcome string) {
	if taskID == "" {
		return
	}
	now := time.Now().UnixMilli()
	if err := c.store.SetStatus(ctx, taskID, StatusCompleted, outcome, 0, now); err != nil {
		return
	}
	projectID, _ := ctx.Value(tools.ProjectIDContextKey).(string)
	sessionID, _ := ctx.Value(tools.SessionIDContextKey).(string)
	c.emit(context.Background(), projectID, sessionID, eventstore.TaskCompleted, eventstore.TaskPayload{
		TaskID: taskID, Status: string(StatusCompleted), Outcome: outcome,
	})
	c.captureCheckpoint(context.Background(), taskID, sessionID)
	c.learnFromTask(context.Background(), taskID, outcome)
	_ = c.hooks.Dispatch(context.Background(), hooks.Event{
		Point: hooks.TaskEnd, TaskID: taskID, SessionID: sessionID, Outcome: outcome,
	})
}

// captureCheckpoint records what a completed task changed by snapshotting the
// session's recorded file versions into a checkpoint (roadmapplan.md §11.1). The
// before/after content comes from the history the edit/write tools already wrote,
// so the change set is truthful — never inferred. Best-effort: any failure is
// swallowed so it never affects task completion.
func (c *Coordinator) captureCheckpoint(ctx context.Context, taskID, sessionID string) {
	if c.checkpoints == nil || c.history == nil || sessionID == "" {
		return
	}
	all, err := c.history.ListBySession(ctx, sessionID)
	if err != nil || len(all) == 0 {
		return
	}
	latest, err := c.history.ListLatestSessionFiles(ctx, sessionID)
	if err != nil {
		return
	}

	changes := checkpoint.ChangesFrom(initialContentByPath(all), toFileVersions(latest))
	if len(changes) == 0 {
		return
	}

	rev := ""
	if c.cached != nil {
		rev = c.cached.Revision.VCSRevision
	}
	_, _ = c.checkpoints.Create(ctx, taskID, "", "task-complete", rev, changes)
}

// initialContentByPath maps each path to its earliest ("initial") recorded
// content — the "before" snapshot for a change set.
func initialContentByPath(all []history.File) map[string]string {
	before := make(map[string]string, len(all))
	for _, f := range all {
		if f.Version == history.InitialVersion {
			before[f.Path] = f.Content
		}
	}
	return before
}

// toFileVersions adapts history files to the checkpoint change-set input.
func toFileVersions(files []history.File) []checkpoint.FileVersion {
	out := make([]checkpoint.FileVersion, 0, len(files))
	for _, f := range files {
		out = append(out, checkpoint.FileVersion{Path: f.Path, Content: f.Content})
	}
	return out
}

// learnFromTask extracts deterministic memory candidates from a completed task
// (roadmapplan.md §8.2). This is safe to run after PR 12; earlier slices
// generated the evidence memory needs.
func (c *Coordinator) learnFromTask(ctx context.Context, taskID, outcome string) {
	if c.memories == nil && c.skills == nil {
		return
	}
	t, err := c.store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	rev := ""
	if c.cached != nil {
		rev = c.cached.Revision.VCSRevision
	}
	// Commands that validated successfully become procedural-memory candidates.
	var successfulCommands []string
	if c.validations != nil {
		successfulCommands, _ = c.validations.SuccessfulCommands(ctx, t.ID)
	}
	if c.memories != nil {
		_ = c.memories.Learn(ctx, memory.Extract(memory.ExtractInput{
			ProjectID:          t.ProjectID,
			TaskID:             t.ID,
			Objective:          t.Objective,
			Mode:               string(t.Mode),
			Outcome:            outcome,
			SupportingRevision: rev,
			SuccessfulCommands: successfulCommands,
		}))
	}

	c.proposeSkills(ctx, t, successfulCommands)
}

// proposeSkills records skill candidates from what the task actually validated.
//
// The candidate pipeline (candidate -> evaluate -> promote, gated on
// HasPassingEvaluation) already existed and was already correct; nothing in the
// product ever called it, so no task had ever produced a skill. This is that
// call site.
//
// Failures are swallowed on purpose: proposing a skill is an optimization, and
// it must never be able to fail a task that has otherwise succeeded.
func (c *Coordinator) proposeSkills(ctx context.Context, t Task, successfulCommands []string) {
	if c.skills == nil {
		return
	}
	in := skill.ExtractInput{
		ProjectID:          t.ProjectID,
		TaskID:             t.ID,
		Objective:          t.Objective,
		SuccessfulCommands: successfulCommands,
	}
	for _, content := range skill.Extract(in) {
		if _, _, err := c.skills.Candidate(ctx, "project", t.ProjectID, content, "task", skill.SourceIDsFor(in)); err != nil {
			logging.Debug("skill candidate not recorded", "task", t.ID, "skill", content.Name, "error", err)
		}
	}
}

// Fail marks a task failed or cancelled and emits the corresponding event.
func (c *Coordinator) Fail(ctx context.Context, taskID string, cause error) {
	if taskID == "" {
		return
	}
	status := StatusFailed
	evType := eventstore.TaskFailed
	if errors.Is(cause, context.Canceled) {
		status = StatusCancelled
		evType = eventstore.TaskCancelled
	}
	now := time.Now().UnixMilli()
	if err := c.store.SetStatus(ctx, taskID, status, cause.Error(), 0, now); err != nil {
		return
	}
	projectID, _ := ctx.Value(tools.ProjectIDContextKey).(string)
	sessionID, _ := ctx.Value(tools.SessionIDContextKey).(string)
	c.emit(context.Background(), projectID, sessionID, evType, eventstore.TaskPayload{
		TaskID: taskID, Status: string(status), Outcome: cause.Error(),
	})
}

func (c *Coordinator) emit(ctx context.Context, projectID, sessionID string, evType eventstore.Type, payload eventstore.TaskPayload) {
	if c.events == nil {
		return
	}
	_, _ = c.events.Append(ctx, eventstore.Append{
		Type:      evType,
		ProjectID: projectID,
		SessionID: sessionID,
		TaskID:    payload.TaskID,
		Payload:   payload,
	})
}

func eventPayload(t Task, profileVersionID string) eventstore.TaskPayload {
	return eventstore.TaskPayload{
		TaskID:           t.ID,
		Objective:        t.Objective,
		Mode:             string(t.Mode),
		Status:           string(t.Status),
		ProfileVersionID: profileVersionID,
	}
}

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/hooks"
	"github.com/kaiau00/aux-cli/internal/ids"
	llmcontext "github.com/kaiau00/aux-cli/internal/llm/context"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/prompt"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/permission"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/runtime"
	"github.com/kaiau00/aux-cli/internal/session"
)

// agent implements the runtime.Runner turn seam.
var _ runtime.Runner = (*agent)(nil)

// Common errors
var (
	ErrRequestCancelled = errors.New("request cancelled by user")
	ErrSessionBusy      = errors.New("session is currently processing another request")
)

type AgentEventType string

const (
	AgentEventTypeError     AgentEventType = "error"
	AgentEventTypeResponse  AgentEventType = "response"
	AgentEventTypeSummarize AgentEventType = "summarize"
)

type AgentEvent struct {
	Type    AgentEventType
	Message message.Message
	Error   error

	// When summarizing
	SessionID string
	Progress  string
	Done      bool
}

type Service interface {
	pubsub.Suscriber[AgentEvent]
	Model() models.Model
	Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error)
	Cancel(sessionID string)
	IsSessionBusy(sessionID string) bool
	IsBusy() bool
	Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error)
	Summarize(ctx context.Context, sessionID string) error
}

// TaskCoordinator turns a user objective into a first-class task before tool use
// and finalizes it afterward. It is optional (only the top-level agent uses it).
type TaskCoordinator interface {
	Begin(ctx context.Context, sessionID, objective string) (context.Context, string, error)
	Finish(ctx context.Context, taskID, outcome string)
	Fail(ctx context.Context, taskID string, cause error)
}

// Deps groups the runtime services an agent needs, so wiring stays readable as
// more services are added.
type Deps struct {
	Sessions     session.Service
	Messages     message.Service
	Ledger       cost.Service
	Events       eventstore.Service
	Recorder     tools.Recorder
	Coordinator  TaskCoordinator         // optional; top-level agent only
	Compiler     promptcompiler.Compiler // optional; defaults to compatibility mode
	Virtualizer  tools.Virtualizer       // optional; large tool-output virtualization
	Pages        *contextstore.Store     // optional; records page bindings per call
	GovernorMode cost.GovernorMode       // optional; off/observe/on, default off
	Hooks        *hooks.Registry         // optional; dispatches ToolPre/ToolPost around tool execution
	Permissions  permission.Service      // optional; asks the user to approve continuing past a budget ceiling
}

type agent struct {
	*pubsub.Broker[AgentEvent]
	sessions    session.Service
	messages    message.Service
	ledger      cost.Service
	events      eventstore.Service
	coordinator TaskCoordinator
	compiler    promptcompiler.Compiler
	pages       *contextstore.Store
	governor    *cost.Governor
	permissions permission.Service
	executor    *tools.Executor

	tools    []tools.BaseTool
	provider provider.Provider

	titleProvider     provider.Provider
	summarizeProvider provider.Provider

	activeRequests sync.Map
}

func NewAgent(
	agentName config.AgentName,
	deps Deps,
	agentTools []tools.BaseTool,
) (Service, error) {
	agentProvider, err := createAgentProvider(agentName)
	if err != nil {
		return nil, err
	}
	var titleProvider provider.Provider
	// Only generate titles for the coder agent
	if agentName == config.AgentCoder {
		titleProvider, err = createAgentProvider(config.AgentTitle)
		if err != nil {
			return nil, err
		}
	}
	var summarizeProvider provider.Provider
	if agentName == config.AgentCoder {
		summarizeProvider, err = createAgentProvider(config.AgentSummarizer)
		if err != nil {
			return nil, err
		}
	}

	compiler := deps.Compiler
	if compiler == nil {
		compiler = promptcompiler.NewCompatibilityCompiler()
	}

	agent := &agent{
		Broker:            pubsub.NewBroker[AgentEvent](),
		provider:          agentProvider,
		messages:          deps.Messages,
		sessions:          deps.Sessions,
		ledger:            deps.Ledger,
		events:            deps.Events,
		coordinator:       deps.Coordinator,
		compiler:          compiler,
		pages:             deps.Pages,
		governor:          cost.NewGovernor(deps.GovernorMode),
		permissions:       deps.Permissions,
		executor:          tools.NewExecutor(deps.Recorder, deps.Virtualizer).WithHooks(deps.Hooks),
		tools:             agentTools,
		titleProvider:     titleProvider,
		summarizeProvider: summarizeProvider,
		activeRequests:    sync.Map{},
	}

	return agent, nil
}

func (a *agent) Model() models.Model {
	return a.provider.Model()
}

func (a *agent) Cancel(sessionID string) {
	// Cancel regular requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Request cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}

	// Also check for summarize requests
	if cancelFunc, exists := a.activeRequests.LoadAndDelete(sessionID + "-summarize"); exists {
		if cancel, ok := cancelFunc.(context.CancelFunc); ok {
			logging.InfoPersist(fmt.Sprintf("Summarize cancellation initiated for session: %s", sessionID))
			cancel()
		}
	}
}

func (a *agent) IsBusy() bool {
	busy := false
	a.activeRequests.Range(func(key, value interface{}) bool {
		if cancelFunc, ok := value.(context.CancelFunc); ok {
			if cancelFunc != nil {
				busy = true
				return false // Stop iterating
			}
		}
		return true // Continue iterating
	})
	return busy
}

func (a *agent) IsSessionBusy(sessionID string) bool {
	_, busy := a.activeRequests.Load(sessionID)
	return busy
}

func (a *agent) generateTitle(ctx context.Context, sessionID string, content string) error {
	if content == "" {
		return nil
	}
	if a.titleProvider == nil {
		return nil
	}
	if _, err := a.sessions.Get(ctx, sessionID); err != nil {
		return err
	}
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	parts := []message.ContentPart{message.TextContent{Text: content}}
	response, err := a.titleProvider.SendMessages(
		ctx,
		[]message.Message{
			{
				Role:  message.User,
				Parts: parts,
			},
		},
		make([]tools.BaseTool, 0),
	)
	if err != nil {
		return err
	}

	title := strings.TrimSpace(strings.ReplaceAll(response.Content, "\n", " "))
	if title == "" {
		return nil
	}

	// Write the title alone. This runs in a goroutine started on the session's
	// first message, and the turn that triggered it has been reconciling the
	// session's token and cost totals while the title model call was in flight.
	// Saving a Session struct read before that call would carry the pre-turn
	// zeros back over them -- which is how first turns ended up recorded with
	// no tokens and no cost at all.
	_, err = a.sessions.SetTitle(ctx, sessionID, title)
	return err
}

func (a *agent) err(err error) AgentEvent {
	return AgentEvent{
		Type:  AgentEventTypeError,
		Error: err,
	}
}

func (a *agent) Run(ctx context.Context, sessionID string, content string, attachments ...message.Attachment) (<-chan AgentEvent, error) {
	if !a.provider.Model().SupportsAttachments && attachments != nil {
		attachments = nil
	}
	// Buffered by one so the final hand-off never blocks on a consumer that has
	// already walked away. This goroutine owns the session's busy marker, so a
	// send that blocks forever would keep the session busy forever.
	events := make(chan AgentEvent, 1)
	if a.IsSessionBusy(sessionID) {
		return nil, ErrSessionBusy
	}

	genCtx, cancel := context.WithCancel(ctx)

	a.activeRequests.Store(sessionID, cancel)
	go func() {
		logging.Debug("Request started", "sessionID", sessionID)
		// The normal path below tears down in its own order, which subscribers
		// to the published event depend on. These defers are the safety net for
		// the abnormal one: without them a panic leaves the busy marker set and
		// the channel open, so the session accepts no further message and any
		// consumer ranging over the channel blocks forever. Delete and cancel
		// are both idempotent, so running twice on the normal path is harmless.
		//
		// Order matters. Defers run last-registered-first, so RecoverPanic
		// delivers its error event first, and the busy marker is released
		// before the close that tells a consumer the turn is over -- otherwise
		// an immediate retry on the same session would race and see it busy.
		defer close(events)
		defer a.activeRequests.Delete(sessionID)
		defer cancel()
		defer logging.RecoverPanic("agent.Run", func() {
			events <- a.err(fmt.Errorf("panic while running the agent"))
		})
		var attachmentParts []message.ContentPart
		for _, attachment := range attachments {
			attachmentParts = append(attachmentParts, message.BinaryContent{Path: attachment.FilePath, MIMEType: attachment.MimeType, Data: attachment.Content})
		}
		result := a.processGeneration(genCtx, sessionID, content, attachmentParts)
		if result.Error != nil && !errors.Is(result.Error, ErrRequestCancelled) && !errors.Is(result.Error, context.Canceled) {
			logging.ErrorPersist(result.Error.Error())
		}
		logging.Debug("Request completed", "sessionID", sessionID)
		a.activeRequests.Delete(sessionID)
		cancel()
		a.Publish(pubsub.CreatedEvent, result)
		events <- result
	}()
	return events, nil
}

func (a *agent) processGeneration(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (result AgentEvent) {
	cfg := config.Get()

	// Turn this user objective into a first-class, versioned task bound to the
	// current project revision and effective profile, before any tool runs. The
	// task is finalized (completed/failed/cancelled) when generation returns.
	var taskID string
	if a.coordinator != nil {
		if taskCtx, id, err := a.coordinator.Begin(ctx, sessionID, content); err != nil {
			logging.Warn("failed to begin task", "error", err)
		} else {
			ctx = taskCtx
			taskID = id
			defer func() {
				if result.Error != nil {
					a.coordinator.Fail(ctx, taskID, result.Error)
				} else {
					a.coordinator.Finish(ctx, taskID, "completed")
				}
			}()
		}
	}

	// List existing messages; if none, start title generation asynchronously.
	msgs, err := a.messages.List(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to list messages: %w", err))
	}
	if len(msgs) == 0 {
		go func() {
			defer logging.RecoverPanic("agent.Run", func() {
				logging.ErrorPersist("panic while generating title")
			})
			titleErr := a.generateTitle(context.Background(), sessionID, content)
			if titleErr != nil {
				logging.ErrorPersist(fmt.Sprintf("failed to generate title: %v", titleErr))
			}
		}()
	}
	session, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return a.err(fmt.Errorf("failed to get session: %w", err))
	}
	if session.SummaryMessageID != "" {
		summaryMsgInex := -1
		for i, msg := range msgs {
			if msg.ID == session.SummaryMessageID {
				summaryMsgInex = i
				break
			}
		}
		if summaryMsgInex != -1 {
			msgs = msgs[summaryMsgInex:]
			msgs[0].Role = message.User
		}
	}

	userMsg, err := a.createUserMessage(ctx, sessionID, content, attachmentParts)
	if err != nil {
		return a.err(fmt.Errorf("failed to create user message: %w", err))
	}
	ctx = llmcontext.WithState(ctx, llmcontext.NewState(content))
	// Append the new user message to the conversation history.
	msgHistory := append(msgs, userMsg)

	for {
		// Check for cancellation before each iteration
		select {
		case <-ctx.Done():
			return a.err(ctx.Err())
		default:
			// Continue processing
		}
		if stop, reason := a.budgetStop(ctx, sessionID); stop {
			return a.err(fmt.Errorf("stopped by the cost governor: %s", reason))
		}
		turn, err := a.RunTurn(ctx, sessionID, msgHistory)
		agentMessage, toolResults := turn.Assistant, turn.ToolResults
		if err != nil {
			if errors.Is(err, context.Canceled) {
				agentMessage.AddFinish(message.FinishReasonCanceled)
				a.messages.Update(context.Background(), agentMessage)
				return a.err(ErrRequestCancelled)
			}
			return a.err(fmt.Errorf("failed to process events: %w", err))
		}
		if cfg.Debug {
			seqId := (len(msgHistory) + 1) / 2
			toolResultFilepath := logging.WriteToolResultsJson(sessionID, seqId, toolResults)
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", "{}", "filepath", toolResultFilepath)
		} else {
			logging.Info("Result", "message", agentMessage.FinishReason(), "toolResults", toolResults)
		}
		if (agentMessage.FinishReason() == message.FinishReasonToolUse) && toolResults != nil {
			// We are not done, we need to respond with the tool response
			msgHistory = append(msgHistory, agentMessage, *toolResults)
			continue
		}
		return AgentEvent{
			Type:    AgentEventTypeResponse,
			Message: agentMessage,
			Done:    true,
		}
	}
}

func (a *agent) createUserMessage(ctx context.Context, sessionID, content string, attachmentParts []message.ContentPart) (message.Message, error) {
	parts := []message.ContentPart{message.TextContent{Text: content}}
	parts = append(parts, attachmentParts...)
	return a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.User,
		Parts: parts,
	})
}

// RunTurn executes one turn (one model call plus its tool results) behind the
// runtime.Runner seam. In compatibility mode it passes the stored history to the
// provider unchanged; later phases substitute the prompt compiler here.
func (a *agent) RunTurn(ctx context.Context, sessionID string, history []message.Message) (runtime.TurnResult, error) {
	assistant, toolResults, err := a.streamAndHandleEvents(ctx, sessionID, history)
	return runtime.TurnResult{Assistant: assistant, ToolResults: toolResults}, err
}

func (a *agent) streamAndHandleEvents(ctx context.Context, sessionID string, msgHistory []message.Message) (message.Message, *message.Message, error) {
	// A turn is one iteration of the agent loop: one model call plus the tool
	// results it produced. Correlation IDs let every downstream record (model
	// call, tool execution, event) be reconstructed without parsing message JSON.
	turnID := ids.New()
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	ctx = context.WithValue(ctx, tools.TurnIDContextKey, turnID)
	a.emit(ctx, eventstore.Append{
		Type:    eventstore.TurnStarted,
		TurnID:  turnID,
		Payload: eventstore.TurnPayload{TurnID: turnID},
	})

	assistantMsg, err := a.messages.Create(ctx, sessionID, message.CreateMessageParams{
		Role:  message.Assistant,
		Parts: []message.ContentPart{},
		Model: a.provider.Model().ID,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create assistant message: %w", err)
	}

	// Add the session and message ID into the context if needed by tools.
	ctx = context.WithValue(ctx, tools.MessageIDContextKey, assistantMsg.ID)

	// Open the per-call ledger record and expose its id to tools for correlation.
	tracker := a.startCall(ctx, sessionID, turnID, assistantMsg.ID)
	ctx = context.WithValue(ctx, tools.ModelCallIDContextKey, tracker.id)

	// Compile the model-facing prompt from durable history, separately from how
	// that history is stored/displayed. In compatibility mode the compiled
	// messages equal the cleaned transcript, so behaviour is unchanged; the
	// manifest is recorded for inspection and reconciliation.
	corr := tools.CorrelationFromContext(ctx)
	projectManifest, taskSpecText := promptcompiler.ProjectContextFromContext(ctx)
	var excluded, pinned map[string]bool
	if a.pages != nil && corr.TaskID != "" {
		excluded, _ = a.pages.Exclusions(ctx, corr.TaskID)
		pinned, _ = a.pages.Pins(ctx, corr.TaskID)
	}
	compiled := a.compiler.Compile(promptcompiler.Input{
		TaskID:              corr.TaskID,
		CallID:              tracker.id,
		History:             msgHistory,
		Tools:               a.tools,
		ProjectManifest:     projectManifest,
		TaskSpecText:        taskSpecText,
		ExcludedToolCallIDs: excluded,
		PinnedToolCallIDs:   pinned,
	})
	resident, available := a.bindPages(ctx, corr.TaskID, tracker.id, compiled)
	a.emit(ctx, eventstore.Append{
		Type:   eventstore.ContextCompiled,
		TurnID: turnID,
		Payload: eventstore.ContextPayload{
			CallID:         tracker.id,
			MessageCount:   len(compiled.Messages),
			ToolCount:      compiled.Manifest.ToolCount,
			TokenEstimate:  compiled.EstimatedTokens,
			StablePrefixID: compiled.StablePrefixID,
			ResidentPages:  resident,
			AvailablePages: available,
			SavedTokens:    compiled.SavedTokens,
		},
	})
	eventChan := a.provider.StreamResponse(ctx, compiled.Messages, compiled.ToolSet)

	// Process each event in the stream.
	for event := range eventChan {
		if processErr := a.processEvent(ctx, sessionID, tracker, &assistantMsg, event); processErr != nil {
			a.finishMessage(ctx, &assistantMsg, message.FinishReasonCanceled)
			a.abortCall(ctx, tracker, processErr)
			return assistantMsg, nil, processErr
		}
		if ctx.Err() != nil {
			a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
			a.abortCall(ctx, tracker, ctx.Err())
			return assistantMsg, nil, ctx.Err()
		}
	}
	// Safety net: if the stream closed without an explicit completion event, close
	// the ledger record so it never remains in the `started` state.
	a.finalizeCallIfOpen(context.Background(), tracker, sessionID)
	// Same reasoning for the transcript: EventComplete normally supersedes any
	// coalesced streaming write, but a stream that ends without one would leave
	// the last window of deltas unwritten.
	_ = a.messages.FlushStreamed(context.Background(), assistantMsg.ID)

	toolResults, toolsCancelled := a.executeToolCalls(ctx, assistantMsg.ToolCalls())
	if toolsCancelled {
		a.finishMessage(context.Background(), &assistantMsg, message.FinishReasonCanceled)
	}

	a.emit(ctx, eventstore.Append{
		Type:   eventstore.TurnCompleted,
		TurnID: turnID,
		Payload: eventstore.TurnPayload{
			TurnID:       turnID,
			MessageID:    assistantMsg.ID,
			ToolCalls:    len(assistantMsg.ToolCalls()),
			FinishReason: string(assistantMsg.FinishReason()),
		},
	})
	a.assessBudget(ctx, sessionID)
	if len(toolResults) == 0 {
		return assistantMsg, nil, nil
	}
	parts := make([]message.ContentPart, 0)
	for _, tr := range toolResults {
		parts = append(parts, tr)
	}
	msg, err := a.messages.Create(context.Background(), assistantMsg.SessionID, message.CreateMessageParams{
		Role:  message.Tool,
		Parts: parts,
	})
	if err != nil {
		return assistantMsg, nil, fmt.Errorf("failed to create cancelled tool message: %w", err)
	}

	return assistantMsg, &msg, err
}

// finishMessage stamps the finish marker on a message and persists it.
//
// The write is not allowed to fail silently. Every call site is already
// unwinding with a more important error, so this one cannot change control
// flow -- but a finish marker that never lands leaves the message rendering as
// though it were still streaming, permanently, across restarts, with nothing
// anywhere to say why. Losing the error is what makes that undiagnosable.
func (a *agent) finishMessage(ctx context.Context, msg *message.Message, finishReson message.FinishReason) {
	msg.AddFinish(finishReson)
	if err := a.messages.Update(ctx, *msg); err != nil {
		logging.ErrorPersist(fmt.Sprintf("failed to record finish marker for message %s: %v", msg.ID, err))
	}
}

func (a *agent) processEvent(ctx context.Context, sessionID string, tracker *callTracker, assistantMsg *message.Message, event provider.ProviderEvent) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing.
	}

	switch event.Type {
	case provider.EventThinkingDelta:
		thinking := event.Thinking
		if thinking == "" {
			thinking = event.Content
		}
		if thinking == "" {
			return nil
		}
		a.onFirstToken(ctx, tracker)
		assistantMsg.AppendReasoningContent(thinking)
		return a.messages.UpdateStreamed(ctx, *assistantMsg)
	case provider.EventContentDelta:
		a.onFirstToken(ctx, tracker)
		assistantMsg.AppendContent(event.Content)
		return a.messages.UpdateStreamed(ctx, *assistantMsg)
	case provider.EventToolUseStart:
		a.onFirstToken(ctx, tracker)
		assistantMsg.AddToolCall(*event.ToolCall)
		return a.messages.UpdateStreamed(ctx, *assistantMsg)
	// TODO: see how to handle this
	// case provider.EventToolUseDelta:
	// 	tm := time.Unix(assistantMsg.UpdatedAt, 0)
	// 	assistantMsg.AppendToolCallInput(event.ToolCall.ID, event.ToolCall.Input)
	// 	if time.Since(tm) > 1000*time.Millisecond {
	// 		err := a.messages.Update(ctx, *assistantMsg)
	// 		assistantMsg.UpdatedAt = time.Now().Unix()
	// 		return err
	// 	}
	case provider.EventToolUseStop:
		assistantMsg.FinishToolCall(event.ToolCall.ID)
		return a.messages.Update(ctx, *assistantMsg)
	case provider.EventError:
		if errors.Is(event.Error, context.Canceled) {
			logging.InfoPersist(fmt.Sprintf("Event processing canceled for session: %s", sessionID))
			return context.Canceled
		}
		logging.ErrorPersist(event.Error.Error())
		return event.Error
	case provider.EventComplete:
		assistantMsg.SetToolCalls(event.Response.ToolCalls)
		assistantMsg.AddFinish(event.Response.FinishReason)
		if err := a.messages.Update(ctx, *assistantMsg); err != nil {
			return fmt.Errorf("failed to update message: %w", err)
		}
		return a.completeCall(ctx, tracker, sessionID, event.Response.Usage)
	}

	return nil
}

// callTracker holds the mutable timing state for one in-flight model call so the
// ledger row can record time-to-first-token and total latency.
type callTracker struct {
	id           string
	model        models.Model
	startedAt    time.Time
	firstTokenAt time.Time
	finalized    bool
}

// markFirstToken records the first-token time and reports whether this call was
// the transition (so a model_call.first_token event is emitted exactly once).
func (t *callTracker) markFirstToken() bool {
	if t != nil && t.firstTokenAt.IsZero() {
		t.firstTokenAt = time.Now()
		return true
	}
	return false
}

// emit appends a durable domain event, filling in the session/turn correlation
// from context. It no-ops when no event store is configured and never blocks the
// agent on event-store failures.
func (a *agent) emit(ctx context.Context, ev eventstore.Append) {
	if a.events == nil {
		return
	}
	corr := tools.CorrelationFromContext(ctx)
	if ev.SessionID == "" {
		ev.SessionID = corr.SessionID
	}
	if ev.TurnID == "" {
		ev.TurnID = corr.TurnID
	}
	if ev.TaskID == "" {
		ev.TaskID = corr.TaskID
	}
	if ev.ProjectID == "" {
		ev.ProjectID = corr.ProjectID
	}
	// Append with a detached context so events are still recorded when the
	// request context has been cancelled (e.g. model_call.failed on cancel).
	if _, err := a.events.Append(context.Background(), ev); err != nil {
		logging.Error("failed to append domain event", "type", ev.Type, "error", err)
	}
}

// startCall opens a ledger record for the model call about to stream and returns
// a tracker. The tracker always carries a fresh id (even when no ledger is
// configured) so tool executions can be correlated to the call.
func (a *agent) startCall(ctx context.Context, sessionID, turnID, messageID string) *callTracker {
	model := a.provider.Model()
	t := &callTracker{
		id:        ids.New(),
		model:     model,
		startedAt: time.Now(),
	}
	if a.ledger == nil {
		return t
	}
	corr := tools.CorrelationFromContext(ctx)
	if _, err := a.ledger.StartCall(ctx, cost.ModelCall{
		ID:        t.id,
		ProjectID: corr.ProjectID,
		TaskID:    corr.TaskID,
		TurnID:    turnID,
		SessionID: sessionID,
		MessageID: messageID,
		Provider:  string(model.Provider),
		Model:     string(model.ID),
		Status:    cost.CallStarted,
		StartedAt: t.startedAt.UnixMilli(),
	}); err != nil {
		logging.Error("failed to record model call start", "error", err)
	}
	a.emit(ctx, eventstore.Append{
		Type:   eventstore.ModelCallStarted,
		TurnID: turnID,
		Payload: eventstore.ModelCallPayload{
			ModelCallID: t.id,
			Provider:    string(model.Provider),
			Model:       string(model.ID),
			Status:      string(cost.CallStarted),
		},
	})
	return t
}

// bindPages persists the compiled prompt's page descriptors as durable pages,
// versions, and per-call bindings so the prompt can be explained page by page.
// Failures are logged, never fatal.
func (a *agent) bindPages(ctx context.Context, taskID, callID string, compiled promptcompiler.CompiledPrompt) (resident, available int) {
	if a.pages == nil {
		return 0, 0
	}
	corr := tools.CorrelationFromContext(ctx)
	for i, pd := range compiled.Manifest.Pages {
		page, err := a.pages.UpsertPage(ctx, corr.ProjectID, pd.Kind, pd.StableKey, "")
		if err != nil {
			logging.Error("failed to upsert context page", "error", err)
			continue
		}
		ver, err := a.pages.UpsertVersion(ctx, page.ID, pd.ContentHash, "", pd.TokenEstimate)
		if err != nil {
			logging.Error("failed to upsert page version", "error", err)
			continue
		}
		if err := a.pages.Bind(ctx, contextstore.Binding{
			TaskID:        taskID,
			ModelCallID:   callID,
			PageVersionID: ver.ID,
			State:         pd.State,
			Rank:          i,
			Reason:        pd.Reason,
			TokenCount:    pd.TokenEstimate,
		}); err != nil {
			logging.Error("failed to bind page", "error", err)
			continue
		}
		if pd.State == contextstore.StateResident {
			resident++
		} else {
			available++
		}
	}
	return resident, available
}

// onFirstToken records the first-token time once and emits a
// model_call.first_token event on the transition.
func (a *agent) onFirstToken(ctx context.Context, tracker *callTracker) {
	if tracker.markFirstToken() {
		a.emit(ctx, eventstore.Append{
			Type: eventstore.ModelCallFirstToken,
			Payload: eventstore.ModelCallPayload{
				ModelCallID: tracker.id,
				TTFTMS:      tracker.firstTokenAt.Sub(tracker.startedAt).Milliseconds(),
			},
		})
	}
}

// completeCall finalizes the ledger record with usage/cost and then re-derives
// the session token and cost totals from the ledger (never overwriting with a
// single call's usage).
func (a *agent) completeCall(ctx context.Context, tracker *callTracker, sessionID string, usage provider.TokenUsage) error {
	if tracker != nil && !tracker.finalized && a.ledger != nil {
		tracker.finalized = true
		now := time.Now()
		estCost, state := cost.ComputeCost(tracker.model, usage)
		mc := cost.ModelCall{
			ID:                  tracker.id,
			Status:              cost.CallCompleted,
			CostState:           state,
			PriceCatalogVersion: cost.PriceCatalogVersion,
			FinishedAt:          now.UnixMilli(),
			LatencyMS:           now.Sub(tracker.startedAt).Milliseconds(),
			InputTokens:         usage.InputTokens,
			OutputTokens:        usage.OutputTokens,
			CacheCreationTokens: usage.CacheCreationTokens,
			CacheReadTokens:     usage.CacheReadTokens,
			EstimatedCost:       estCost,
		}
		if !tracker.firstTokenAt.IsZero() {
			mc.FirstTokenAt = tracker.firstTokenAt.UnixMilli()
			mc.TTFTMS = tracker.firstTokenAt.Sub(tracker.startedAt).Milliseconds()
		}
		if err := a.ledger.FinishCall(ctx, mc); err != nil {
			logging.Error("failed to finalize model call", "error", err)
		}
		a.emit(ctx, eventstore.Append{
			Type: eventstore.ModelCallCompleted,
			Payload: eventstore.ModelCallPayload{
				ModelCallID:         tracker.id,
				Status:              string(cost.CallCompleted),
				InputTokens:         usage.InputTokens,
				OutputTokens:        usage.OutputTokens,
				CacheCreationTokens: usage.CacheCreationTokens,
				CacheReadTokens:     usage.CacheReadTokens,
				EstimatedCost:       estCost,
				CostState:           string(state),
				LatencyMS:           mc.LatencyMS,
				TTFTMS:              mc.TTFTMS,
			},
		})
	} else if tracker != nil {
		tracker.finalized = true
	}
	return a.reconcileSession(ctx, sessionID)
}

// abortCall records a failed or cancelled model call. The ctx is used only to
// read correlation; the ledger/event writes use a detached context because the
// request context is usually already cancelled here.
func (a *agent) abortCall(ctx context.Context, tracker *callTracker, cause error) {
	if tracker == nil || tracker.finalized {
		return
	}
	tracker.finalized = true
	now := time.Now()
	status := cost.CallFailed
	errCode := "error"
	if errors.Is(cause, context.Canceled) {
		status = cost.CallCancelled
		errCode = "cancelled"
	}
	latency := now.Sub(tracker.startedAt).Milliseconds()
	if a.ledger != nil {
		mc := cost.ModelCall{
			ID:         tracker.id,
			Status:     status,
			CostState:  cost.CostKnown,
			FinishedAt: now.UnixMilli(),
			LatencyMS:  latency,
			ErrorCode:  errCode,
		}
		if !tracker.firstTokenAt.IsZero() {
			mc.FirstTokenAt = tracker.firstTokenAt.UnixMilli()
			mc.TTFTMS = tracker.firstTokenAt.Sub(tracker.startedAt).Milliseconds()
		}
		if err := a.ledger.FinishCall(context.Background(), mc); err != nil {
			logging.Error("failed to record aborted model call", "error", err)
		}
	}
	a.emit(ctx, eventstore.Append{
		Type: eventstore.ModelCallFailed,
		Payload: eventstore.ModelCallPayload{
			ModelCallID: tracker.id,
			Status:      string(status),
			ErrorCode:   errCode,
			LatencyMS:   latency,
		},
	})
}

// finalizeCallIfOpen closes a ledger record for a stream that ended cleanly but
// never emitted an explicit completion event.
func (a *agent) finalizeCallIfOpen(ctx context.Context, tracker *callTracker, sessionID string) {
	if tracker == nil || tracker.finalized {
		return
	}
	if err := a.completeCall(ctx, tracker, sessionID, provider.TokenUsage{}); err != nil {
		logging.Error("failed to close open model call", "error", err)
	}
}

// assessBudget runs the Cost Governor in observe mode over the current task/
// session totals and emits budget/governor events. It changes no prompt or
// action (observe); enforcement ("on") is a separate, evaluated step. No-op when
// the governor is off or no ledger is configured.
// currentAssessment computes the governor's read on the task/session budget.
// A disabled assessment means the governor is off or the totals are unknown.
func (a *agent) currentAssessment(ctx context.Context, sessionID string) (cost.Assessment, cost.Totals) {
	if a.governor == nil || a.governor.Mode() == cost.GovOff || a.ledger == nil {
		return cost.Assessment{}, cost.Totals{}
	}
	corr := tools.CorrelationFromContext(ctx)
	var totals cost.Totals
	var err error
	if corr.TaskID != "" {
		totals, err = a.ledger.TaskTotals(ctx, corr.TaskID)
	} else {
		totals, err = a.ledger.SessionTotals(ctx, sessionID)
	}
	if err != nil {
		return cost.Assessment{}, cost.Totals{}
	}
	budget := cost.DefaultBudget(cost.ModeBalanced)
	usage := cost.Usage{InputTokens: totals.PromptTokens, OutputTokens: totals.CompletionTokens, Cost: totals.Cost}
	return a.governor.Assess(budget, usage, nil), totals
}

// budgetStop enforces the governor in "on" mode. When a
// task has exhausted its budget it pauses before spending more and asks the
// user whether to keep going; declining ends the task with a clear reason
// rather than silently burning the remaining budget.
//
// "observe" mode never stops — it only emits the events assessBudget already
// records — so the two modes finally differ in behaviour, which is the whole
// point of having both.
func (a *agent) budgetStop(ctx context.Context, sessionID string) (stop bool, reason string) {
	if a.governor == nil || a.governor.Mode() != cost.GovOn {
		return false, ""
	}
	assessment, totals := a.currentAssessment(ctx, sessionID)
	if !assessment.Enabled || !assessment.Exhausted {
		return false, ""
	}
	spent := fmt.Sprintf("$%.4f over %d input / %d output tokens",
		totals.Cost, totals.PromptTokens, totals.CompletionTokens)

	// Without a way to ask, stopping is the safe answer: "on" mode exists
	// precisely so an unattended run cannot spend without bound.
	if a.permissions == nil {
		return true, "budget exhausted (" + spent + ") and no approval channel is available"
	}
	approved := a.permissions.Request(permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolName:    "governor",
		Action:      "continue",
		Path:        tools.ResolveWorkingDir(ctx),
		Description: "Budget exhausted: " + spent + ". Continue this task anyway?",
		// Approving once must not silently authorize every later overrun; the
		// fingerprint changes as spend does, so each new ceiling asks again.
		Fingerprint: spent,
	})
	if approved {
		return false, ""
	}
	return true, "budget exhausted (" + spent + ") and continuing was declined"
}

func (a *agent) assessBudget(ctx context.Context, sessionID string) {
	assessment, _ := a.currentAssessment(ctx, sessionID)
	if !assessment.Enabled {
		return
	}
	if assessment.Exhausted {
		a.emit(ctx, eventstore.Append{Type: eventstore.BudgetExhausted, Payload: eventstore.BudgetPayload{Pressure: assessment.Pressure.Max, Exhausted: true}})
	} else if assessment.Pressure.Max >= 0.8 {
		a.emit(ctx, eventstore.Append{Type: eventstore.BudgetWarning, Payload: eventstore.BudgetPayload{Pressure: assessment.Pressure.Max}})
	}
	for _, d := range assessment.Decisions {
		a.emit(ctx, eventstore.Append{Type: eventstore.GovernorDecision, Payload: eventstore.BudgetPayload{Action: d.Action, Reason: d.Reason}})
	}
}

// reconcileSession recomputes the session's token and cost totals from the
// durable call ledger so they always reconcile with the underlying records.
func (a *agent) reconcileSession(ctx context.Context, sessionID string) error {
	if a.ledger == nil {
		return nil
	}
	totals, err := a.ledger.SessionTotals(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to compute session totals: %w", err)
	}
	sess, err := a.sessions.Get(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	contextTokens, err := a.ledger.SessionContextTokens(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to compute session context tokens: %w", err)
	}
	sess.PromptTokens = totals.PromptTokens
	sess.CompletionTokens = totals.CompletionTokens
	sess.ContextTokens = contextTokens
	sess.Cost = totals.Cost
	if _, err := a.sessions.Save(ctx, sess); err != nil {
		return fmt.Errorf("failed to save session totals: %w", err)
	}
	return nil
}

func (a *agent) Update(agentName config.AgentName, modelID models.ModelID) (models.Model, error) {
	if a.IsBusy() {
		return models.Model{}, fmt.Errorf("cannot change model while processing requests")
	}

	if err := config.UpdateAgentModel(agentName, modelID); err != nil {
		return models.Model{}, fmt.Errorf("failed to update config: %w", err)
	}

	provider, err := createAgentProvider(agentName)
	if err != nil {
		return models.Model{}, fmt.Errorf("failed to create provider for model %s: %w", modelID, err)
	}

	a.provider = provider

	return a.provider.Model(), nil
}

func (a *agent) Summarize(ctx context.Context, sessionID string) error {
	if a.summarizeProvider == nil {
		return fmt.Errorf("summarize provider not available")
	}

	// Check if session is busy
	if a.IsSessionBusy(sessionID) {
		return ErrSessionBusy
	}

	// Create a new context with cancellation
	summarizeCtx, cancel := context.WithCancel(ctx)

	// Store the cancel function in activeRequests to allow cancellation
	a.activeRequests.Store(sessionID+"-summarize", cancel)

	go func() {
		defer a.activeRequests.Delete(sessionID + "-summarize")
		defer cancel()
		event := AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Starting summarization...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		// Get all messages from the session
		msgs, err := a.messages.List(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to list messages: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		summarizeCtx = context.WithValue(summarizeCtx, tools.SessionIDContextKey, sessionID)

		if len(msgs) == 0 {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("no messages to summarize"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Analyzing conversation...",
		}
		a.Publish(pubsub.CreatedEvent, event)

		// Add a system message to guide the summarization
		summarizePrompt := "Provide a detailed but concise summary of our conversation above. Focus on information that would be helpful for continuing the conversation, including what we did, what we're doing, which files we're working on, and what we're going to do next."

		// Create a new message with the summarize prompt
		promptMsg := message.Message{
			Role:  message.User,
			Parts: []message.ContentPart{message.TextContent{Text: summarizePrompt}},
		}

		// Append the prompt to the messages
		msgsWithPrompt := append(msgs, promptMsg)

		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Generating summary...",
		}

		a.Publish(pubsub.CreatedEvent, event)

		// Send the messages to the summarize provider
		response, err := a.summarizeProvider.SendMessages(
			summarizeCtx,
			msgsWithPrompt,
			make([]tools.BaseTool, 0),
		)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to summarize: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}

		summary := strings.TrimSpace(response.Content)
		if summary == "" {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("empty summary returned"),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		event = AgentEvent{
			Type:     AgentEventTypeSummarize,
			Progress: "Creating new session...",
		}

		a.Publish(pubsub.CreatedEvent, event)
		oldSession, err := a.sessions.Get(summarizeCtx, sessionID)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to get session: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		// Create a message in the new session with the summary
		msg, err := a.messages.Create(summarizeCtx, oldSession.ID, message.CreateMessageParams{
			Role: message.Assistant,
			Parts: []message.ContentPart{
				message.TextContent{Text: summary},
				message.Finish{
					Reason: message.FinishReasonEndTurn,
					Time:   time.Now().Unix(),
				},
			},
			Model: a.summarizeProvider.Model().ID,
		})
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to create summary message: %w", err),
				Done:  true,
			}

			a.Publish(pubsub.CreatedEvent, event)
			return
		}
		oldSession.SummaryMessageID = msg.ID
		oldSession.CompletionTokens = response.Usage.OutputTokens
		oldSession.PromptTokens = 0
		// The transcript has been replaced by its summary, so the window now
		// holds only that summary. Leaving the pre-compaction occupancy behind
		// would keep the meter pinned high and re-trigger auto-compaction.
		oldSession.ContextTokens = response.Usage.OutputTokens
		model := a.summarizeProvider.Model()
		usage := response.Usage
		cost := model.CostPer1MInCached/1e6*float64(usage.CacheCreationTokens) +
			model.CostPer1MOutCached/1e6*float64(usage.CacheReadTokens) +
			model.CostPer1MIn/1e6*float64(usage.InputTokens) +
			model.CostPer1MOut/1e6*float64(usage.OutputTokens)
		oldSession.Cost += cost
		_, err = a.sessions.Save(summarizeCtx, oldSession)
		if err != nil {
			event = AgentEvent{
				Type:  AgentEventTypeError,
				Error: fmt.Errorf("failed to save session: %w", err),
				Done:  true,
			}
			a.Publish(pubsub.CreatedEvent, event)
		}

		event = AgentEvent{
			Type:      AgentEventTypeSummarize,
			SessionID: oldSession.ID,
			Progress:  "Summary complete",
			Done:      true,
		}
		a.Publish(pubsub.CreatedEvent, event)
		// Send final success event with the new session ID
	}()

	return nil
}

func createAgentProvider(agentName config.AgentName) (provider.Provider, error) {
	cfg := config.Get()
	agentConfig, ok := cfg.Agents[agentName]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", agentName)
	}
	model, ok := models.SupportedModels[agentConfig.Model]
	if !ok {
		return nil, fmt.Errorf("model %s not supported", agentConfig.Model)
	}

	providerCfg, ok := cfg.Providers[model.Provider]
	if !ok {
		return nil, fmt.Errorf("provider %s not supported", model.Provider)
	}
	if providerCfg.Disabled {
		return nil, fmt.Errorf("provider %s is not enabled", model.Provider)
	}
	maxTokens := model.DefaultMaxTokens
	if agentConfig.MaxTokens > 0 {
		maxTokens = agentConfig.MaxTokens
	}
	opts := []provider.ProviderClientOption{
		provider.WithAPIKey(providerCfg.APIKey),
		provider.WithModel(model),
		provider.WithSystemMessage(prompt.GetAgentPrompt(agentName, model.Provider)),
		provider.WithMaxTokens(maxTokens),
	}
	if model.Provider == models.ProviderOpenAI || model.Provider == models.ProviderLocal && model.CanReason {
		opts = append(
			opts,
			provider.WithOpenAIOptions(
				provider.WithReasoningEffort(agentConfig.ReasoningEffort),
			),
		)
	} else if model.Provider == models.ProviderAnthropic && model.CanReason && agentName == config.AgentCoder {
		opts = append(
			opts,
			provider.WithAnthropicOptions(
				provider.WithAnthropicShouldThinkFn(provider.DefaultShouldThinkFn),
			),
		)
	}
	agentProvider, err := provider.NewProvider(
		model.Provider,
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("could not create provider: %v", err)
	}

	return agentProvider, nil
}

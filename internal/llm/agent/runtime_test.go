package agent

import (
	"context"
	"testing"

	"github.com/kaiau00/aux-cli/internal/contextstore"
	"github.com/kaiau00/aux-cli/internal/cost"
	"github.com/kaiau00/aux-cli/internal/db"
	"github.com/kaiau00/aux-cli/internal/db/dbtest"
	"github.com/kaiau00/aux-cli/internal/eventstore"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/provider"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/kaiau00/aux-cli/internal/promptcompiler"
	"github.com/kaiau00/aux-cli/internal/pubsub"
	"github.com/kaiau00/aux-cli/internal/session"
	"github.com/kaiau00/aux-cli/internal/toolexec"
)

// scriptedTool is a deterministic BaseTool for turn tests.
type scriptedTool struct {
	name string
	resp tools.ToolResponse
}

func (s *scriptedTool) Info() tools.ToolInfo { return tools.ToolInfo{Name: s.name} }
func (s *scriptedTool) Run(context.Context, tools.ToolCall) (tools.ToolResponse, error) {
	return s.resp, nil
}

func mockModel() models.Model {
	return models.Model{
		ID:           models.ModelID("mock"),
		Provider:     models.ProviderAnthropic,
		CostPer1MIn:  3.0,
		CostPer1MOut: 15.0,
	}
}

func newTurnAgent(t *testing.T, p provider.Provider, toolset ...tools.BaseTool) (*agent, eventstore.Service, cost.Service, string) {
	t.Helper()
	conn := dbtest.New(t)
	q := db.New(conn)
	sessions := session.NewService(q)
	messages := message.NewService(q)
	ledger := cost.NewService(conn)
	events := eventstore.NewService(conn)
	recorder := toolexec.NewRecorder(toolexec.NewStore(conn), events)

	a := &agent{
		Broker:   pubsub.NewBroker[AgentEvent](),
		sessions: sessions,
		messages: messages,
		ledger:   ledger,
		events:   events,
		compiler: promptcompiler.NewCompatibilityCompiler(),
		pages:    contextstore.NewStore(conn),
		executor: tools.NewExecutor(recorder, nil),
		tools:    toolset,
		provider: p,
	}

	sess, err := sessions.Create(context.Background(), "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return a, events, ledger, sess.ID
}

func eventTypes(t *testing.T, events eventstore.Service) []eventstore.Type {
	t.Helper()
	evs, err := events.List(context.Background(), eventstore.Filter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	types := make([]eventstore.Type, len(evs))
	for i, e := range evs {
		types[i] = e.Type
	}
	return types
}

func TestRunTurnTextResponseIsVisibleAndAccounted(t *testing.T) {
	p := provider.NewMockProvider(mockModel(),
		provider.TextTurn("hello world", provider.TokenUsage{InputTokens: 100, OutputTokens: 20}))
	a, events, ledger, sessID := newTurnAgent(t, p)
	ctx := context.Background()

	turn, err := a.RunTurn(ctx, sessID, nil)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if turn.Assistant.Content().Text != "hello world" {
		t.Fatalf("assistant content = %q", turn.Assistant.Content().Text)
	}
	if turn.Assistant.FinishReason() != message.FinishReasonEndTurn {
		t.Fatalf("finish reason = %q", turn.Assistant.FinishReason())
	}
	if turn.ToolResults != nil {
		t.Fatalf("no tool results expected for a text turn")
	}

	// Ledger records the call with usage.
	calls, err := ledger.ListCallsBySession(ctx, sessID)
	if err != nil {
		t.Fatalf("list calls: %v", err)
	}
	if len(calls) != 1 || calls[0].Status != cost.CallCompleted {
		t.Fatalf("expected one completed call, got %+v", calls)
	}
	if calls[0].InputTokens != 100 || calls[0].OutputTokens != 20 {
		t.Fatalf("usage not recorded: %+v", calls[0])
	}

	// Event sequence reconstructs the turn without any terminal logs.
	got := eventTypes(t, events)
	want := []eventstore.Type{
		eventstore.TurnStarted,
		eventstore.ModelCallStarted,
		eventstore.ContextCompiled,
		eventstore.ModelCallFirstToken,
		eventstore.ModelCallCompleted,
		eventstore.TurnCompleted,
	}
	assertTypeOrder(t, got, want)
}

func TestRunTurnToolCallsOrderedAndErrorReturned(t *testing.T) {
	okTool := &scriptedTool{name: "ok_tool", resp: tools.NewTextResponse("ok result")}
	errTool := &scriptedTool{name: "err_tool", resp: tools.NewTextErrorResponse("bad result")}

	p := provider.NewMockProvider(mockModel(),
		provider.ToolCallTurn(provider.TokenUsage{InputTokens: 50, OutputTokens: 10},
			message.ToolCall{ID: "c1", Name: "ok_tool", Input: "{}"},
			message.ToolCall{ID: "c2", Name: "err_tool", Input: "{}"},
		))
	a, events, _, sessID := newTurnAgent(t, p, okTool, errTool)
	ctx := context.Background()

	turn, err := a.RunTurn(ctx, sessID, nil)
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if turn.ToolResults == nil {
		t.Fatalf("expected tool results message")
	}
	results := turn.ToolResults.ToolResults()
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}
	// Ordering preserved: c1 then c2.
	if results[0].ToolCallID != "c1" || results[1].ToolCallID != "c2" {
		t.Fatalf("tool result ordering wrong: %+v", results)
	}
	// The error tool's result is returned to the model as an error.
	if results[0].IsError {
		t.Fatalf("ok tool should not be an error")
	}
	if !results[1].IsError || results[1].Content != "bad result" {
		t.Fatalf("error tool result not returned to model: %+v", results[1])
	}

	got := eventTypes(t, events)
	want := []eventstore.Type{
		eventstore.TurnStarted,
		eventstore.ModelCallStarted,
		eventstore.ContextCompiled,
		eventstore.ModelCallFirstToken,
		eventstore.ModelCallCompleted,
		eventstore.ToolStarted,
		eventstore.ToolCompleted,
		eventstore.ToolStarted,
		eventstore.ToolFailed,
		eventstore.TurnCompleted,
	}
	assertTypeOrder(t, got, want)
}

func TestRunTurnCancellationFinalizesCall(t *testing.T) {
	p := provider.NewMockProvider(mockModel(), provider.CanceledTurn())
	a, _, ledger, sessID := newTurnAgent(t, p)
	ctx := context.Background()

	_, err := a.RunTurn(ctx, sessID, nil)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}

	calls, lerr := ledger.ListCallsBySession(ctx, sessID)
	if lerr != nil {
		t.Fatalf("list calls: %v", lerr)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one call, got %d", len(calls))
	}
	if calls[0].Status != cost.CallCancelled {
		t.Fatalf("call status = %q, want cancelled", calls[0].Status)
	}
}

func TestRunTurnBindsPagesToCall(t *testing.T) {
	p := provider.NewMockProvider(mockModel(),
		provider.TextTurn("ack", provider.TokenUsage{InputTokens: 5, OutputTokens: 2}))
	a, _, ledger, sessID := newTurnAgent(t, p)
	ctx := context.Background()

	history := []message.Message{
		{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "please help"}}},
	}
	if _, err := a.RunTurn(ctx, sessID, history); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	calls, err := ledger.ListCallsBySession(ctx, sessID)
	if err != nil || len(calls) != 1 {
		t.Fatalf("expected one call, err=%v", err)
	}
	bound, err := a.pages.BindingsForCall(ctx, calls[0].ID)
	if err != nil {
		t.Fatalf("BindingsForCall: %v", err)
	}
	// The single history message must be bound as a resident page to this call,
	// so the compiled prompt is explainable page by page.
	if len(bound) != 1 || bound[0].State != contextstore.StateResident {
		t.Fatalf("expected one resident page bound to the call, got %+v", bound)
	}
}

func assertTypeOrder(t *testing.T, got, want []eventstore.Type) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event count = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q\n got: %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}

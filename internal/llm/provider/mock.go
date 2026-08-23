package provider

import (
	"context"
	"sync"

	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/llm/tools"
	"github.com/kaiau00/aux-cli/internal/message"
)

// MockProvider is a scripted provider for deterministic, network-free tests of
// the runtime/agent orchestration. Each call to
// StreamResponse replays the next scripted turn's events in order; SendMessages
// returns that turn's terminal EventComplete response.
type MockProvider struct {
	model models.Model

	mu    sync.Mutex
	turns [][]ProviderEvent
	idx   int
}

// NewMockProvider returns a scripted provider. Each element of turns is the
// ordered event stream for one StreamResponse/SendMessages call.
func NewMockProvider(model models.Model, turns ...[]ProviderEvent) *MockProvider {
	return &MockProvider{model: model, turns: turns}
}

func (m *MockProvider) next() []ProviderEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.idx >= len(m.turns) {
		return []ProviderEvent{{Type: EventComplete, Response: &ProviderResponse{FinishReason: message.FinishReasonEndTurn}}}
	}
	t := m.turns[m.idx]
	m.idx++
	return t
}

// Model returns the configured model.
func (m *MockProvider) Model() models.Model { return m.model }

// StreamResponse replays the next scripted turn, honouring context cancellation.
func (m *MockProvider) StreamResponse(ctx context.Context, _ []message.Message, _ []tools.BaseTool) <-chan ProviderEvent {
	events := m.next()
	ch := make(chan ProviderEvent)
	go func() {
		defer close(ch)
		for _, ev := range events {
			select {
			case <-ctx.Done():
				select {
				case ch <- ProviderEvent{Type: EventError, Error: ctx.Err()}:
				default:
				}
				return
			case ch <- ev:
			}
		}
	}()
	return ch
}

// SendMessages returns the terminal response of the next scripted turn.
func (m *MockProvider) SendMessages(_ context.Context, _ []message.Message, _ []tools.BaseTool) (*ProviderResponse, error) {
	for _, ev := range m.next() {
		if ev.Type == EventComplete && ev.Response != nil {
			return ev.Response, nil
		}
		if ev.Type == EventError {
			return nil, ev.Error
		}
	}
	return &ProviderResponse{FinishReason: message.FinishReasonEndTurn}, nil
}

// --- scripted-turn builders ---

// TextTurn scripts a plain text response that ends the turn.
func TextTurn(content string, usage TokenUsage) []ProviderEvent {
	return []ProviderEvent{
		{Type: EventContentDelta, Content: content},
		{Type: EventComplete, Response: &ProviderResponse{
			Content:      content,
			FinishReason: message.FinishReasonEndTurn,
			Usage:        usage,
		}},
	}
}

// ToolCallTurn scripts a response that requests one or more tool calls.
func ToolCallTurn(usage TokenUsage, calls ...message.ToolCall) []ProviderEvent {
	events := make([]ProviderEvent, 0, len(calls)+1)
	for i := range calls {
		tc := calls[i]
		events = append(events, ProviderEvent{Type: EventToolUseStart, ToolCall: &tc})
	}
	events = append(events, ProviderEvent{Type: EventComplete, Response: &ProviderResponse{
		ToolCalls:    calls,
		FinishReason: message.FinishReasonToolUse,
		Usage:        usage,
	}})
	return events
}

// CanceledTurn scripts a stream that reports cancellation, exercising the
// call-finalization path deterministically.
func CanceledTurn() []ProviderEvent {
	return []ProviderEvent{{Type: EventError, Error: context.Canceled}}
}

package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/message"
)

// Recorded shape of a Messages stream. message_start carries the input and
// cache counts; message_delta replaces output_tokens with the final total.
const anthropicStreamFixture = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"x","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":100}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4}}

event: message_stop
data: {"type":"message_stop"}

`

func TestAnthropicStreamReportsCacheTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, anthropicStreamFixture)
	}))
	t.Cleanup(srv.Close)

	c := &anthropicClient{
		providerOptions: providerClientOptions{
			systemMessage: "base",
			maxTokens:     16,
			model:         models.Model{APIModel: "x", Provider: models.ProviderAnthropic},
		},
		client: anthropic.NewClient(
			option.WithBaseURL(srv.URL),
			option.WithAPIKey("k"),
			option.WithMaxRetries(0),
		),
	}

	var complete *ProviderResponse
	var content strings.Builder
	for event := range c.stream(context.Background(), []message.Message{{
		Role:  message.User,
		Parts: []message.ContentPart{message.TextContent{Text: "hi"}},
	}}, nil) {
		switch event.Type {
		case EventContentDelta:
			content.WriteString(event.Content)
		case EventComplete:
			complete = event.Response
		case EventError:
			t.Fatalf("stream: %v", event.Error)
		}
	}
	if complete == nil {
		t.Fatal("stream ended without a completion")
	}
	if content.String() != "ok" {
		t.Fatalf("content = %q", content.String())
	}
	got := complete.Usage
	if got.InputTokens != 10 || got.OutputTokens != 4 || got.CacheCreationTokens != 2 || got.CacheReadTokens != 100 {
		t.Fatalf("usage = %+v; want 10 input, 4 output, 2 cache creation, 100 cache read", got)
	}
}

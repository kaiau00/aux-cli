package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Recorded shape of a chat completion stream whose final chunk carries
// prompt_tokens_details. The SDK accumulator adds prompt and completion
// totals and leaves cached_tokens at zero; the adapter must bill from the chunk.
const openAIStreamFixture = `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"x","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2179,"completion_tokens":8,"total_tokens":2187,"prompt_tokens_details":{"cached_tokens":2178}}}

data: [DONE]

`

func TestOpenAIStreamReportsCachedTokensFromTheChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, openAIStreamFixture)
	}))
	t.Cleanup(srv.Close)

	c := &openaiClient{
		providerOptions: providerClientOptions{
			systemMessage: "base",
			maxTokens:     16,
			model:         models.Model{APIModel: "x", Provider: models.ProviderOpenAI},
		},
		client: openai.NewClient(
			option.WithBaseURL(srv.URL+"/"),
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
	if complete.Usage.InputTokens != 1 || complete.Usage.CacheReadTokens != 2178 || complete.Usage.OutputTokens != 8 {
		t.Fatalf("usage = %+v; want 1 fresh input, 2178 cache read, 8 output", complete.Usage)
	}
}

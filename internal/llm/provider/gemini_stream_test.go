package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/message"
	"google.golang.org/genai"
)

// Recorded shape of a streamGenerateContent SSE response. The text arrives
// first; the last chunk carries finishReason and usageMetadata, which is the
// response the adapter bills from.
const geminiStreamFixture = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]}}]}\n\n" +
	"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"\"}]},\"finishReason\":\"STOP\"}]," +
	"\"usageMetadata\":{\"promptTokenCount\":20,\"candidatesTokenCount\":3,\"cachedContentTokenCount\":15,\"totalTokenCount\":23}}\n\n"

func TestGeminiStreamReportsCachedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, geminiStreamFixture)
	}))
	t.Cleanup(srv.Close)

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:      "k",
		Backend:     genai.BackendGeminiAPI,
		HTTPClient:  srv.Client(),
		HTTPOptions: genai.HTTPOptions{BaseURL: srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}

	c := &geminiClient{
		providerOptions: providerClientOptions{
			systemMessage: "base",
			maxTokens:     16,
			model:         models.Model{APIModel: "gemini-test", Provider: models.ProviderGemini},
		},
		client: client,
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
	// PromptTokenCount already includes cached content. The adapter records it
	// as input and also records CachedContentTokenCount as cache read.
	if got.InputTokens != 20 || got.OutputTokens != 3 || got.CacheReadTokens != 15 {
		t.Fatalf("usage = %+v; want 20 input, 3 output, 15 cache read", got)
	}
}

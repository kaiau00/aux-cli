package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/models"
	"github.com/kaiau00/aux-cli/internal/message"
	"github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
)

// The adapters read config for debug logging, which panics unless loaded.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "provider-test-*")
	if err != nil {
		panic(err)
	}
	if _, err := config.Load(dir, false); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

const (
	baseSystem = "BASE SYSTEM PROMPT"
	addendum   = "# Project\n\nLanguages: go"
)

// recordingServer answers every request with body and keeps the request
// bodies it saw.
type recordingServer struct {
	*httptest.Server
	mu     sync.Mutex
	bodies []string
}

func newRecordingServer(t *testing.T, body string) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rs.mu.Lock()
		rs.bodies = append(rs.bodies, string(b))
		rs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(rs.Close)
	return rs
}

func (rs *recordingServer) only(t *testing.T) string {
	t.Helper()
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.bodies) != 1 {
		t.Fatalf("server saw %d requests; want 1", len(rs.bodies))
	}
	return rs.bodies[0]
}

func userTurn() []message.Message {
	return []message.Message{{Role: message.User, Parts: []message.ContentPart{message.TextContent{Text: "hi"}}}}
}

func assertBaseThenAddendum(t *testing.T, body string) {
	t.Helper()
	base, add := strings.Index(body, baseSystem), strings.Index(body, "Languages: go")
	if base < 0 || add < 0 {
		t.Fatalf("request is missing the base system prompt or the addendum:\n%s", body)
	}
	if add < base {
		t.Fatalf("addendum must come after the base system prompt:\n%s", body)
	}
}

func TestAnthropicRequestCarriesSystemAddendum(t *testing.T) {
	srv := newRecordingServer(t, `{"id":"m","type":"message","role":"assistant","model":"x",
		"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":1,"output_tokens":1}}`)
	c := &anthropicClient{
		providerOptions: providerClientOptions{systemMessage: baseSystem, maxTokens: 16, model: models.Model{APIModel: "x"}},
		client:          anthropic.NewClient(anthropicoption.WithBaseURL(srv.URL), anthropicoption.WithAPIKey("k"), anthropicoption.WithMaxRetries(0)),
	}
	if _, err := c.send(WithSystemAddendum(context.Background(), addendum), userTurn(), nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	body := srv.only(t)
	assertBaseThenAddendum(t, body)
	// One cache breakpoint on the base block only; Anthropic allows four and
	// the request already uses them.
	if n := strings.Count(body, `"cache_control"`); n != 2 {
		t.Fatalf("cache_control count = %d; want 2 (base system block + last user message)", n)
	}
}

func TestOpenAIRequestCarriesSystemAddendum(t *testing.T) {
	srv := newRecordingServer(t, `{"id":"c","object":"chat.completion","created":1,"model":"x",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	c := &openaiClient{
		providerOptions: providerClientOptions{systemMessage: baseSystem, maxTokens: 16, model: models.Model{APIModel: "x"}},
		client:          openai.NewClient(openaioption.WithBaseURL(srv.URL+"/"), openaioption.WithAPIKey("k"), openaioption.WithMaxRetries(0)),
	}
	if _, err := c.send(WithSystemAddendum(context.Background(), addendum), userTurn(), nil); err != nil {
		t.Fatalf("send: %v", err)
	}
	assertBaseThenAddendum(t, srv.only(t))
}

func TestNoAddendumLeavesSystemPromptUnchanged(t *testing.T) {
	if got := systemPrompt(context.Background(), baseSystem); got != baseSystem {
		t.Fatalf("systemPrompt without an addendum = %q", got)
	}
	if got := systemPrompt(WithSystemAddendum(context.Background(), addendum), baseSystem); got != baseSystem+"\n\n"+addendum {
		t.Fatalf("systemPrompt with an addendum = %q", got)
	}
}

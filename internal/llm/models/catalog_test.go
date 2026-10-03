package models

import (
	"os"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := UseCatalogForTest(data); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resetModelsCatalogForTest)
}

func TestResolveCatalogIDNotInHardcodedTable(t *testing.T) {
	loadFixture(t)
	model, ok := Resolve("anthropic/claude-new")
	if !ok {
		t.Fatal("expected anthropic/claude-new to resolve from the catalog")
	}
	if model.APIModel != "claude-new" || model.Provider != ProviderAnthropic {
		t.Fatalf("resolved = %+v", model)
	}
	if model.CostPer1MIn != 3 || model.CostPer1MOut != 15 {
		t.Fatalf("cost = in %v out %v", model.CostPer1MIn, model.CostPer1MOut)
	}
	if _, ok := SupportedModels["anthropic/claude-new"]; ok {
		t.Fatal("catalog id must not be required in the hardcoded table")
	}
	if _, ok := Resolve("claude-4-sonnet"); !ok {
		t.Fatal("hardcoded id should still resolve")
	}
}

func TestSelectDefaultPicksNewestToolCallingModel(t *testing.T) {
	loadFixture(t)
	model, ok := SelectDefault(ProviderAnthropic)
	if !ok {
		t.Fatal("expected a default")
	}
	if model.APIModel != "claude-new-big" {
		t.Fatalf("default = %s, want claude-new-big (newest date, then largest context; preview excluded)", model.APIModel)
	}
	cheap, ok := SelectCheapest(ProviderAnthropic)
	if !ok || cheap.APIModel != "claude-cheap" {
		t.Fatalf("cheapest = %+v", cheap)
	}
}

func TestSelectDefaultFallsBackOffline(t *testing.T) {
	resetModelsCatalogForTest()
	catalogMu.Lock()
	catalogLoaded = true
	catalogEntries = map[ModelProvider][]Model{}
	catalogToolCall = map[ModelID]bool{}
	catalogMu.Unlock()
	t.Cleanup(resetModelsCatalogForTest)

	model, ok := SelectDefault(ProviderAnthropic)
	if !ok || model.ID != Claude4Sonnet {
		t.Fatalf("offline default = %+v, want %s", model, Claude4Sonnet)
	}
	if _, ok := Resolve("anthropic/claude-new"); ok {
		t.Fatal("a catalog id must not resolve when the catalog is empty")
	}
	err := UnsupportedError("anthropic/claude-new")
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmaintainedProviders(t *testing.T) {
	for _, provider := range []ModelProvider{ProviderGROQ, ProviderAzure, ProviderBedrock, ProviderVertexAI, ProviderCopilot, ProviderXAI} {
		if !UnmaintainedProvider(provider) {
			t.Fatalf("%s should be unmaintained", provider)
		}
	}
	if UnmaintainedProvider(ProviderAnthropic) || UnmaintainedProvider(ProviderOpenAI) || UnmaintainedProvider(ProviderGemini) || UnmaintainedProvider(ProviderOpenRouter) || UnmaintainedProvider(ProviderLocal) {
		t.Fatal("maintained provider was marked unmaintained")
	}
	if !SupportedModels[CopilotGPT4o].Unmaintained {
		t.Fatal("hardcoded copilot models should be marked unmaintained")
	}
}

func TestInferCatalogProvider(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"https://api.minimax.io/v1", "minimax"},
		{"http://localhost:1234/v1", ""},
		{"https://api.openai.com/v1", "openai"},
	}

	for _, tt := range tests {
		if got := inferCatalogProvider(tt.endpoint); got != tt.want {
			t.Errorf("inferCatalogProvider(%q) = %q, want %q", tt.endpoint, got, tt.want)
		}
	}
}

func TestResolveLocalModelLimitsUsesCatalog(t *testing.T) {
	resetModelsCatalogForTest()
	catalogByID = map[string]catalogLimits{
		"MiniMax-M3": {Context: 1_000_000, Output: 128_000},
	}
	catalogByProvider = map[string]map[string]catalogLimits{
		"minimax": {
			"MiniMax-M3": {Context: 1_000_000, Output: 128_000},
		},
	}
	catalogLoaded = true

	contextWindow, defaultMaxTokens := resolveLocalModelLimits(localModel{ID: "MiniMax-M3"}, "https://api.minimax.io/v1")
	if contextWindow != 1_000_000 {
		t.Fatalf("contextWindow = %d, want 1000000", contextWindow)
	}
	if defaultMaxTokens != 128_000 {
		t.Fatalf("defaultMaxTokens = %d, want 128000", defaultMaxTokens)
	}
}

func TestResolveLocalModelLimitsPrefersLMStudio(t *testing.T) {
	resetModelsCatalogForTest()
	catalogByID = map[string]catalogLimits{
		"qwen3": {Context: 1_000_000, Output: 128_000},
	}
	catalogLoaded = true

	contextWindow, defaultMaxTokens := resolveLocalModelLimits(localModel{
		ID:                  "qwen3",
		LoadedContextLength: 65_536,
		MaxContextLength:    131_072,
	}, "http://localhost:1234/v1")

	if contextWindow != 65_536 {
		t.Fatalf("contextWindow = %d, want 65536", contextWindow)
	}
	if defaultMaxTokens != 65_536 {
		t.Fatalf("defaultMaxTokens = %d, want 65536", defaultMaxTokens)
	}
}

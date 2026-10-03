package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/llm/models"
)

func TestValidateAgentAcceptsCatalogModelID(t *testing.T) {
	t.Cleanup(models.ResetCatalogForTest)
	if err := models.UseCatalogForTest([]byte(`{
	  "anthropic": {
	    "models": {
	      "fixture-sonnet": {
	        "name": "Fixture Sonnet",
	        "tool_call": true,
	        "release_date": "2026-02-02",
	        "limit": {"context": 200000, "output": 8192},
	        "cost": {"input": 3, "output": 15}
	      }
	    }
	  }
	}`)); err != nil {
		t.Fatal(err)
	}

	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &Config{
		Providers: map[models.ModelProvider]Provider{
			models.ProviderAnthropic: {APIKey: "test"},
		},
		Agents: map[AgentName]Agent{},
	}

	agent := Agent{Model: "anthropic/fixture-sonnet"}
	if err := validateAgent(cfg, AgentCoder, agent); err != nil {
		t.Fatalf("catalog model id should validate: %v", err)
	}

	err := validateAgent(cfg, AgentCoder, Agent{Model: "anthropic/missing"})
	if err == nil || !strings.Contains(err.Error(), "anthropic/fixture-sonnet") {
		t.Fatalf("error = %v; want the provider's catalog ids", err)
	}
}

func TestPersistPickedModelsFillsEmptyAgentsOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := filepath.Join(home, ".aux.json")
	if err := os.WriteFile(path, []byte("{\n  \"debug\": true,\n  \"agents\": {\"title\": {\"model\": \"keep-me\"}}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := persistPickedModels(map[AgentName]models.ModelID{
		AgentCoder: "anthropic/fixture-sonnet",
		AgentTitle: "anthropic/cheap",
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(mustRead(t, path), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["debug"] != true {
		t.Fatalf("debug was rewritten: %#v", raw["debug"])
	}
	agents := raw["agents"].(map[string]any)
	title := agents["title"].(map[string]any)
	if title["model"] != "keep-me" {
		t.Fatalf("existing title model = %v", title["model"])
	}
	coder := agents["coder"].(map[string]any)
	if coder["model"] != "anthropic/fixture-sonnet" {
		t.Fatalf("coder model = %v", coder["model"])
	}
}

func TestUpdateThemeKeepsPickedModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	cfg = &Config{}

	if err := persistPickedModels(map[AgentName]models.ModelID{
		AgentCoder: "anthropic/fixture-sonnet",
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateTheme("aux"); err != nil {
		t.Fatal(err)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(mustRead(t, filepath.Join(home, ".aux.json")), &raw); err != nil {
		t.Fatal(err)
	}
	agents := raw["agents"].(map[string]any)
	coder := agents["coder"].(map[string]any)
	if coder["model"] != "anthropic/fixture-sonnet" {
		t.Fatalf("theme write dropped the model: %#v", raw)
	}
	tui := raw["tui"].(map[string]any)
	if tui["theme"] != "aux" {
		t.Fatalf("theme = %v", tui["theme"])
	}
	if _, ok := raw["validation"]; ok {
		t.Fatal("theme write added validation")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

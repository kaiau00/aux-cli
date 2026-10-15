package prompt

import (
	"strings"
	"testing"

	"github.com/kaiau00/aux-cli/internal/config"
	"github.com/kaiau00/aux-cli/internal/llm/models"
)

// getEnvironmentInfo reads the configured working directory, so the prompts
// cannot be built without a loaded config.
func loadConfig(t *testing.T) {
	t.Helper()
	if config.Get() != nil {
		return
	}
	if _, err := config.Load(t.TempDir(), false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
}

// Every provider's prompt has to say the same true thing about what Aux is and
// what the model is.
//
// The OpenAI variant said Aux was "built by OpenAI" and that it "wraps OpenAI
// models" -- inherited from the fork and false on both counts. The Anthropic
// variant opened "You are Aux", which is the other half of the confusion: the
// model is not the harness. A model that believes either will tell the user
// its provider wrote this tool, or answer for the harness's behaviour as if it
// were its own.
func TestEveryCoderPromptSaysWhatAuxIs(t *testing.T) {
	loadConfig(t)
	providers := []models.ModelProvider{
		models.ProviderOpenAI,
		models.ProviderAnthropic,
		models.ProviderGemini,
		models.ProviderOpenRouter,
		models.ProviderGROQ,
		models.ProviderXAI,
		models.ProviderBedrock,
		models.ProviderAzure,
		models.ProviderLocal,
	}
	for _, provider := range providers {
		prompt := CoderPrompt(provider)
		for _, want := range []string{
			"open-source",      // what Aux is
			"harness",          // not a model
			"forked from",      // where it came from
			"not a product of", // whose it is not
			"API key",          // whose model it is, and who pays
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s: the prompt never says %q", provider, want)
			}
		}
	}
}

// No prompt may claim a model provider built Aux, or that Aux is tied to one
// provider's models. This is the done-when M2.7 states.
func TestNoPromptClaimsAProviderBuiltAux(t *testing.T) {
	loadConfig(t)
	bad := []string{
		"built by OpenAI",
		"wraps OpenAI models",
		"built by Anthropic",
		"powerful terminal-based AI assistant",
	}
	prompts := map[string]string{
		"coder/openai":    CoderPrompt(models.ProviderOpenAI),
		"coder/anthropic": CoderPrompt(models.ProviderAnthropic),
		"task":            TaskPrompt(models.ProviderAnthropic),
		"summarizer":      SummarizerPrompt(models.ProviderAnthropic),
		"title":           TitlePrompt(models.ProviderAnthropic),
	}
	for name, prompt := range prompts {
		for _, phrase := range bad {
			if strings.Contains(prompt, phrase) {
				t.Errorf("%s says %q", name, phrase)
			}
		}
	}
}

// The model is told not to confuse itself with the harness. "You are Aux"
// invites exactly that, so it may not come back.
func TestThePromptDoesNotTellTheModelItIsAux(t *testing.T) {
	loadConfig(t)
	for _, provider := range []models.ModelProvider{models.ProviderOpenAI, models.ProviderAnthropic} {
		prompt := CoderPrompt(provider)
		if strings.Contains(prompt, "You are Aux") {
			t.Errorf("%s: the prompt says \"You are Aux\"; the model is not the harness", provider)
		}
		if !strings.Contains(prompt, "You are not Aux") {
			t.Errorf("%s: the prompt never separates the model from the harness", provider)
		}
	}
}

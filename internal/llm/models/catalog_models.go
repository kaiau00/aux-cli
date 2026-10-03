package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kaiau00/aux-cli/internal/logging"
)

const catalogTTL = 24 * time.Hour

// catalogFetchBudget is the longest startup waits on models.dev. A slow
// response keeps going in the background and fills the cache for next time.
const catalogFetchBudget = 2 * time.Second

// Maintained providers whose model lists come from models.dev. Local is
// maintained too, but its models are discovered from the endpoint, not the
// catalog. Everyone else is an unmaintained fallback (D4).
func CatalogKey(provider ModelProvider) (string, bool) {
	switch provider {
	case ProviderAnthropic:
		return "anthropic", true
	case ProviderOpenAI:
		return "openai", true
	case ProviderGemini:
		return "google", true
	case ProviderOpenRouter:
		return "openrouter", true
	default:
		return "", false
	}
}

func UnmaintainedProvider(provider ModelProvider) bool {
	switch provider {
	case ProviderGROQ, ProviderAzure, ProviderBedrock, ProviderVertexAI, ProviderCopilot, ProviderXAI:
		return true
	default:
		return false
	}
}

// CatalogModels returns the catalog's models for a maintained provider,
// newest release first. Empty when the provider is unmaintained or the
// catalog could not be loaded.
func CatalogModels(provider ModelProvider) []Model {
	if _, ok := CatalogKey(provider); !ok {
		return nil
	}
	ensureModelsCatalog()
	entries := catalogEntries[provider]
	out := slices.Clone(entries)
	slices.SortFunc(out, func(a, b Model) int {
		if a.ReleaseDate != b.ReleaseDate {
			if a.ReleaseDate < b.ReleaseDate {
				return 1
			}
			return -1
		}
		if a.ContextWindow != b.ContextWindow {
			if a.ContextWindow < b.ContextWindow {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// Resolve finds a model by a hardcoded id or by "<provider>/<api-model-id>".
func Resolve(id ModelID) (Model, bool) {
	if model, ok := SupportedModels[id]; ok {
		return model, true
	}
	providerName, apiID, ok := strings.Cut(string(id), "/")
	if !ok || providerName == "" || apiID == "" {
		return Model{}, false
	}
	provider := ModelProvider(providerName)
	for _, model := range CatalogModels(provider) {
		if model.APIModel == apiID || model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}

// UnsupportedError describes a model id Resolve rejected, including the
// provider's catalog ids when the id names a maintained provider.
func UnsupportedError(id ModelID) error {
	providerName, _, ok := strings.Cut(string(id), "/")
	if !ok {
		return fmt.Errorf("model %s is not supported; use a known id or <provider>/<api-model-id>", id)
	}
	ids := catalogAPIIds(ModelProvider(providerName))
	if len(ids) == 0 {
		return fmt.Errorf("model %s is not supported", id)
	}
	const maxListed = 40
	listed := ids
	extra := ""
	if len(ids) > maxListed {
		listed = ids[:maxListed]
		extra = fmt.Sprintf(", … and %d more", len(ids)-maxListed)
	}
	return fmt.Errorf("model %s is not supported; %s models: %s%s", id, providerName, strings.Join(listed, ", "), extra)
}

func catalogAPIIds(provider ModelProvider) []string {
	models := CatalogModels(provider)
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, string(model.ID))
	}
	slices.Sort(ids)
	return ids
}

// NameOf returns the display name for an id, or the id itself when it does
// not resolve.
func NameOf(id ModelID) string {
	if model, ok := Resolve(id); ok && model.Name != "" {
		return model.Name
	}
	return string(id)
}

// SelectDefault picks the newest tool-calling model for a maintained
// provider that is not marked preview, beta, or experimental. Ties go to
// the larger context window. With no catalog, it returns the hardcoded
// fallback for that provider.
func SelectDefault(provider ModelProvider) (Model, bool) {
	if _, ok := CatalogKey(provider); !ok {
		return Model{}, false
	}
	eligible := toolCalling(CatalogModels(provider))
	if len(eligible) == 0 {
		fallback, ok := SupportedModels[hardcodedDefault[provider]]
		return fallback, ok
	}
	return eligible[0], true
}

// SelectCheapest picks the lowest input-priced tool-calling model. A model
// with no catalog price is not "free"; it is skipped. With nothing priced,
// the default model is returned.
func SelectCheapest(provider ModelProvider) (Model, bool) {
	def, ok := SelectDefault(provider)
	if !ok {
		return Model{}, false
	}
	var best Model
	found := false
	for _, model := range toolCalling(CatalogModels(provider)) {
		if model.CostPer1MIn == 0 && model.CostPer1MOut == 0 {
			continue
		}
		if !found || model.CostPer1MIn < best.CostPer1MIn {
			best = model
			found = true
		}
	}
	if !found {
		return def, true
	}
	return best, true
}

func toolCalling(models []Model) []Model {
	out := make([]Model, 0, len(models))
	for _, model := range models {
		if !catalogToolCall[model.ID] || markedExperimental(model) {
			continue
		}
		out = append(out, model)
	}
	return out
}

// catalogToolCall records tool_call from the catalog. Hardcoded fallbacks
// are treated as tool-capable via hardcodedDefault, not this map.
var catalogToolCall = map[ModelID]bool{}

func markedExperimental(model Model) bool {
	text := strings.ToLower(model.Name + " " + model.APIModel + " " + string(model.ID))
	return strings.Contains(text, "preview") ||
		strings.Contains(text, "beta") ||
		strings.Contains(text, "experimental")
}

var hardcodedDefault = map[ModelProvider]ModelID{
	ProviderAnthropic:  Claude4Sonnet,
	ProviderOpenAI:     GPT41,
	ProviderGemini:     Gemini25,
	ProviderOpenRouter: OpenRouterClaude37Sonnet,
}

func ingestCatalog(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	byID := make(map[string]catalogLimits)
	byProvider := make(map[string]map[string]catalogLimits)
	entries := make(map[ModelProvider][]Model)
	toolCall := make(map[ModelID]bool)

	for _, provider := range []ModelProvider{ProviderAnthropic, ProviderOpenAI, ProviderGemini, ProviderOpenRouter} {
		key, _ := CatalogKey(provider)
		providerRaw, ok := raw[key]
		if !ok {
			continue
		}
		var parsed struct {
			Models map[string]catalogRawModel `json:"models"`
		}
		if err := json.Unmarshal(providerRaw, &parsed); err != nil || len(parsed.Models) == 0 {
			continue
		}
		limits := make(map[string]catalogLimits, len(parsed.Models))
		for apiID, rawModel := range parsed.Models {
			model := rawModel.toModel(provider, apiID)
			entries[provider] = append(entries[provider], model)
			toolCall[model.ID] = rawModel.ToolCall
			if rawModel.Limit.Context <= 0 && rawModel.Limit.Output <= 0 {
				continue
			}
			lim := catalogLimits{Context: rawModel.Limit.Context, Output: rawModel.Limit.Output}
			limits[apiID] = lim
			if _, exists := byID[apiID]; !exists {
				byID[apiID] = lim
			}
		}
		if len(limits) > 0 {
			byProvider[key] = limits
		}
	}
	// Keep limit lookups for providers the local resolver infers (minimax, …)
	// even though they are not one of the five maintained providers.
	for providerID, providerRaw := range raw {
		if _, already := byProvider[providerID]; already {
			continue
		}
		var parsed struct {
			Models map[string]catalogRawModel `json:"models"`
		}
		if err := json.Unmarshal(providerRaw, &parsed); err != nil {
			continue
		}
		limits := make(map[string]catalogLimits)
		for apiID, rawModel := range parsed.Models {
			if rawModel.Limit.Context <= 0 && rawModel.Limit.Output <= 0 {
				continue
			}
			lim := catalogLimits{Context: rawModel.Limit.Context, Output: rawModel.Limit.Output}
			limits[apiID] = lim
			if _, exists := byID[apiID]; !exists {
				byID[apiID] = lim
			}
		}
		if len(limits) > 0 {
			byProvider[providerID] = limits
		}
	}

	catalogByID = byID
	catalogByProvider = byProvider
	catalogEntries = entries
	catalogToolCall = toolCall
	return nil
}

type catalogRawModel struct {
	Name        string `json:"name"`
	Attachment  bool   `json:"attachment"`
	Reasoning   bool   `json:"reasoning"`
	ToolCall    bool   `json:"tool_call"`
	ReleaseDate string `json:"release_date"`
	Limit       struct {
		Context int64 `json:"context"`
		Output  int64 `json:"output"`
	} `json:"limit"`
	Cost *struct {
		Input      float64 `json:"input"`
		Output     float64 `json:"output"`
		CacheRead  float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"cost"`
}

func (raw catalogRawModel) toModel(provider ModelProvider, apiID string) Model {
	model := Model{
		ID:                  ModelID(string(provider) + "/" + apiID),
		Name:                raw.Name,
		Provider:            provider,
		APIModel:            apiID,
		ContextWindow:       raw.Limit.Context,
		DefaultMaxTokens:    raw.Limit.Output,
		CanReason:           raw.Reasoning,
		SupportsAttachments: raw.Attachment,
		ReleaseDate:         raw.ReleaseDate,
	}
	if model.Name == "" {
		model.Name = apiID
	}
	if raw.Cost != nil {
		model.CostPer1MIn = raw.Cost.Input
		model.CostPer1MOut = raw.Cost.Output
		model.CostPer1MInCached = raw.Cost.CacheWrite
		model.CostPer1MOutCached = raw.Cost.CacheRead
	}
	return model
}

// ResetCatalogForTest drops a fixture so the next lookup reads the cache again.
func ResetCatalogForTest() { resetModelsCatalogForTest() }

// UseCatalogForTest installs a fixture and skips the network.
func UseCatalogForTest(data []byte) error {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if err := ingestCatalog(data); err != nil {
		return err
	}
	catalogLoaded = true
	return nil
}

func readCatalogWithinBudget() []byte {
	cached, fresh := readCatalogCache()
	if fresh {
		return cached
	}
	fetched := make(chan []byte, 1)
	go func() {
		body, err := fetchCatalog()
		if err != nil {
			logging.Debug("Failed to fetch models.dev catalog", "error", err)
			fetched <- nil
			return
		}
		writeCatalogCache(body)
		fetched <- body
	}()
	select {
	case body := <-fetched:
		if len(body) > 0 {
			return body
		}
		return cached
	case <-time.After(catalogFetchBudget):
		go func() {
			body := <-fetched
			if len(body) > 0 {
				writeCatalogCache(body)
			}
		}()
		return cached
	}
}

func fetchCatalog() ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get(modelsDevAPI)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

func catalogCachePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "aux", "models.json")
}

func readCatalogCache() (data []byte, fresh bool) {
	path := catalogCachePath()
	if path == "" {
		return nil, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, time.Since(info.ModTime()) < catalogTTL
}

func writeCatalogCache(data []byte) {
	path := catalogCachePath()
	if path == "" || len(data) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

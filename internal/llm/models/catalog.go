package models

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/kaiau00/aux-cli/internal/logging"
)

const modelsDevAPI = "https://models.dev/api.json"

type catalogLimits struct {
	Context int64
	Output  int64
}

var (
	catalogMu         sync.Mutex
	catalogLoaded     bool
	catalogByID       map[string]catalogLimits
	catalogByProvider map[string]map[string]catalogLimits
)

func ensureModelsCatalog() {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	if catalogLoaded {
		return
	}
	loadModelsCatalog()
	catalogLoaded = true
}

func resetModelsCatalogForTest() {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	catalogLoaded = false
	catalogByID = nil
	catalogByProvider = nil
}

func loadModelsCatalog() {
	catalogByID = make(map[string]catalogLimits)
	catalogByProvider = make(map[string]map[string]catalogLimits)

	res, err := http.Get(modelsDevAPI)
	if err != nil {
		logging.Debug("Failed to fetch models.dev catalog", "error", err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		logging.Debug("Failed to fetch models.dev catalog", "status", res.StatusCode)
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&raw); err != nil {
		logging.Debug("Failed to decode models.dev catalog", "error", err)
		return
	}

	for providerID, providerRaw := range raw {
		var provider struct {
			Models map[string]struct {
				Limit struct {
					Context int64 `json:"context"`
					Output  int64 `json:"output"`
				} `json:"limit"`
			} `json:"models"`
		}
		if err := json.Unmarshal(providerRaw, &provider); err != nil || len(provider.Models) == 0 {
			continue
		}

		byModel := make(map[string]catalogLimits, len(provider.Models))
		for modelID, model := range provider.Models {
			if model.Limit.Context <= 0 && model.Limit.Output <= 0 {
				continue
			}
			limits := catalogLimits{
				Context: model.Limit.Context,
				Output:  model.Limit.Output,
			}
			byModel[modelID] = limits
			if _, exists := catalogByID[modelID]; !exists {
				catalogByID[modelID] = limits
			}
		}
		if len(byModel) > 0 {
			catalogByProvider[providerID] = byModel
		}
	}

	logging.Debug("Loaded models.dev catalog",
		"models", len(catalogByID),
		"providers", len(catalogByProvider),
	)
}

func inferCatalogProvider(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}

	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "minimax"):
		return "minimax"
	case strings.Contains(host, "openai.com"):
		return "openai"
	case strings.Contains(host, "anthropic"):
		return "anthropic"
	case strings.Contains(host, "groq"):
		return "groq"
	case strings.Contains(host, "x.ai"):
		return "xai"
	case strings.Contains(host, "openrouter"):
		return "openrouter"
	case strings.Contains(host, "google"):
		return "google"
	case strings.Contains(host, "deepseek"):
		return "deepseek"
	default:
		return ""
	}
}

func lookupModelsDevLimits(endpoint, modelID string) (catalogLimits, bool) {
	ensureModelsCatalog()
	if len(catalogByID) == 0 {
		return catalogLimits{}, false
	}

	if providerID := inferCatalogProvider(endpoint); providerID != "" {
		if models, ok := catalogByProvider[providerID]; ok {
			if limits, ok := models[modelID]; ok {
				return limits, true
			}
		}
	}

	if limits, ok := catalogByID[modelID]; ok {
		return limits, true
	}

	return catalogLimits{}, false
}

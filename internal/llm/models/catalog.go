package models

import (
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
	catalogEntries    map[ModelProvider][]Model
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
	catalogEntries = nil
}

func loadModelsCatalog() {
	data := readCatalogWithinBudget()
	if len(data) == 0 {
		catalogByID = map[string]catalogLimits{}
		catalogByProvider = map[string]map[string]catalogLimits{}
		catalogEntries = map[ModelProvider][]Model{}
		return
	}
	if err := ingestCatalog(data); err != nil {
		logging.Debug("Failed to decode models.dev catalog", "error", err)
		catalogByID = map[string]catalogLimits{}
		catalogByProvider = map[string]map[string]catalogLimits{}
		catalogEntries = map[ModelProvider][]Model{}
		return
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

package models

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/kaiau00/aux-cli/internal/logging"
	"github.com/spf13/viper"
)

const (
	ProviderLocal ModelProvider = "local"

	localModelsPath        = "v1/models"
	lmStudioBetaModelsPath = "api/v0/models"
)

func init() {
	// Best-effort model discovery at package load. The authoritative call
	// happens after viper is configured (see LoadLocalModels); this init
	// only catches the case where LOCAL_ENDPOINT is set in the process env
	// before config.Load runs.
	if endpoint := os.Getenv("LOCAL_ENDPOINT"); endpoint != "" {
		_, _ = loadLocalModelsFromEndpoint(endpoint)
	}
}

// LoadLocalModels discovers models at the configured LOCAL_ENDPOINT (read
// from viper, which merges config file, env, and CLI flags) and registers
// them as supported. Safe to call multiple times — re-discovery is
// idempotent because loadLocalModels rewrites SupportedModels entries by
// the same key.
func LoadLocalModels() {
	// Viper stores keys lowercase; the env-var key replacer maps
	// LOCAL_ENDPOINT -> "local_endpoint" automatically.
	endpoint := viper.GetString("local_endpoint")
	if endpoint == "" {
		endpoint = os.Getenv("LOCAL_ENDPOINT")
	}
	if endpoint == "" {
		return
	}
	_, _ = loadLocalModelsFromEndpoint(endpoint)
}

func loadLocalModelsFromEndpoint(endpoint string) ([]localModel, error) {
	localEndpoint, err := url.Parse(endpoint)
	if err != nil {
		logging.Debug("Failed to parse local endpoint", "error", err, "endpoint", endpoint)
		return nil, err
	}

	load := func(url *url.URL, path string) []localModel {
		url.Path = path
		return listLocalModels(url.String())
	}

	models := load(localEndpoint, lmStudioBetaModelsPath)
	if len(models) == 0 {
		models = load(localEndpoint, localModelsPath)
	}
	if len(models) == 0 {
		logging.Debug("No local models found", "endpoint", endpoint)
		return nil, nil
	}

	loadLocalModels(models, endpoint)
	viper.SetDefault("providers.local.apiKey", "dummy")
	ProviderPopularity[ProviderLocal] = 0
	return models, nil
}

type localModelList struct {
	Data []localModel `json:"data"`
}

type localModel struct {
	ID                  string `json:"id"`
	Object              string `json:"object"`
	Type                string `json:"type"`
	Publisher           string `json:"publisher"`
	Arch                string `json:"arch"`
	CompatibilityType   string `json:"compatibility_type"`
	Quantization        string `json:"quantization"`
	State               string `json:"state"`
	MaxContextLength    int64  `json:"max_context_length"`
	LoadedContextLength int64  `json:"loaded_context_length"`
	ContextLength       int64  `json:"context_length"`
	ContextWindow       int64  `json:"context_window"`
}

func listLocalModels(modelsEndpoint string) []localModel {
	req, err := http.NewRequest(http.MethodGet, modelsEndpoint, nil)
	if err != nil {
		logging.Debug("Failed to build local-models request", "error", err)
		return []localModel{}
	}
	// local.go init() runs before viper is configured, so read the API
	// key from the env var (viper prefix AUX_ + underscored path).
	if apiKey := os.Getenv("AUX_PROVIDERS_LOCAL_APIKEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		logging.Debug("Failed to list local models",
			"error", err,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		logging.Debug("Failed to list local models",
			"status", res.StatusCode,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}

	var modelList localModelList
	if err = json.NewDecoder(res.Body).Decode(&modelList); err != nil {
		logging.Debug("Failed to list local models",
			"error", err,
			"endpoint", modelsEndpoint,
		)
		return []localModel{}
	}

	var supportedModels []localModel
	for _, model := range modelList.Data {
		if strings.HasSuffix(modelsEndpoint, lmStudioBetaModelsPath) {
			if model.Object != "model" || model.Type != "llm" {
				logging.Debug("Skipping unsupported LMStudio model",
					"endpoint", modelsEndpoint,
					"id", model.ID,
					"object", model.Object,
					"type", model.Type,
				)

				continue
			}
		}

		supportedModels = append(supportedModels, model)
	}

	return supportedModels
}

func loadLocalModels(models []localModel, endpoint string) {
	for i, m := range models {
		model := convertLocalModel(m, endpoint)
		SupportedModels[model.ID] = model

		if i == 0 || m.State == "loaded" {
			viper.SetDefault("agents.coder.model", model.ID)
			viper.SetDefault("agents.summarizer.model", model.ID)
			viper.SetDefault("agents.task.model", model.ID)
			viper.SetDefault("agents.title.model", model.ID)
		}
	}
}

func convertLocalModel(model localModel, endpoint string) Model {
	contextWindow, defaultMaxTokens := resolveLocalModelLimits(model, endpoint)
	return Model{
		ID:                  ModelID("local." + model.ID),
		Name:                friendlyModelName(model.ID),
		Provider:            ProviderLocal,
		APIModel:            model.ID,
		ContextWindow:       contextWindow,
		DefaultMaxTokens:    defaultMaxTokens,
		CanReason:           true,
		SupportsAttachments: true,
	}
}

func resolveLocalModelLimits(model localModel, endpoint string) (contextWindow, defaultMaxTokens int64) {
	contextWindow = cmp.Or(
		model.LoadedContextLength,
		model.MaxContextLength,
		model.ContextLength,
		model.ContextWindow,
	)

	catalogFound := false
	var catalog catalogLimits
	if limits, ok := lookupModelsDevLimits(endpoint, model.ID); ok {
		catalogFound = true
		catalog = limits
		if contextWindow <= 0 && limits.Context > 0 {
			contextWindow = limits.Context
		}
	}

	if contextWindow <= 0 {
		contextWindow = 4096
	}

	defaultMaxTokens = cmp.Or(model.LoadedContextLength, model.MaxContextLength)
	if defaultMaxTokens <= 0 && catalogFound && catalog.Output > 0 {
		defaultMaxTokens = catalog.Output
	}
	if defaultMaxTokens <= 0 {
		defaultMaxTokens = 4096
	}

	return contextWindow, defaultMaxTokens
}

var modelInfoRegex = regexp.MustCompile(`(?i)^([a-z0-9]+)(?:[-_]?([rv]?\d[\.\d]*))?(?:[-_]?([a-z]+))?.*`)

func friendlyModelName(modelID string) string {
	mainID := modelID
	tag := ""

	if slash := strings.LastIndex(mainID, "/"); slash != -1 {
		mainID = mainID[slash+1:]
	}

	if at := strings.Index(modelID, "@"); at != -1 {
		mainID = modelID[:at]
		tag = modelID[at+1:]
	}

	match := modelInfoRegex.FindStringSubmatch(mainID)
	if match == nil {
		return modelID
	}

	capitalize := func(s string) string {
		if s == "" {
			return ""
		}
		runes := []rune(s)
		runes[0] = unicode.ToUpper(runes[0])
		return string(runes)
	}

	family := capitalize(match[1])
	version := ""
	label := ""

	if len(match) > 2 && match[2] != "" {
		version = strings.ToUpper(match[2])
	}

	if len(match) > 3 && match[3] != "" {
		label = capitalize(match[3])
	}

	var parts []string
	if family != "" {
		parts = append(parts, family)
	}
	if version != "" {
		parts = append(parts, version)
	}
	if label != "" {
		parts = append(parts, label)
	}
	if tag != "" {
		parts = append(parts, tag)
	}

	return strings.Join(parts, " ")
}

package modelcatalog

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/state"
)

const remoteCatalogRefreshTTL = 10 * time.Minute
const remoteCatalogRefreshFailureCooldown = 90 * time.Second

var remoteCatalogRefreshFailures sync.Map

type remoteCatalogTarget struct {
	ProviderID   string
	ProviderName string
	BaseURL      string
	Token        string
	Headers      http.Header
}

type remoteCatalogEnvelope struct {
	Data   json.RawMessage `json:"data"`
	Models json.RawMessage `json:"models"`
}

type remoteCatalogModel struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	DisplayName         string   `json:"display_name"`
	Description         string   `json:"description"`
	ContextLen          int      `json:"context_length"`
	ContextWin          int      `json:"context_window"`
	MaxContext          int      `json:"max_context_length"`
	InputTokens         int      `json:"input_token_limit"`
	MaxInputTokens      int      `json:"max_input_tokens"`
	SupportedParameters []string `json:"supported_parameters"`
	Capabilities struct {
		Reasoning         bool `json:"reasoning"`
		ParallelToolCalls bool `json:"parallel_tool_calls"`
	} `json:"capabilities"`
	TopProvider struct {
		ContextLength int `json:"context_length"`
	} `json:"top_provider"`
}

func shouldAttemptLiveCatalogRefresh(ctx RuntimeContext, snapshot Snapshot) bool {
	if strings.TrimSpace(ctx.ProviderID) == "" {
		return false
	}
	if refreshBlockedByFailureCooldown(remoteCatalogRefreshKey(ctx, snapshot)) {
		return false
	}
	if len(snapshot.Models) == 0 {
		return true
	}
	if snapshot.FetchedAt.IsZero() {
		return true
	}
	return time.Since(snapshot.FetchedAt) >= remoteCatalogRefreshTTL
}

func refreshRuntimeCatalogSnapshot(config state.Config, ctx RuntimeContext, codexHome string, current Snapshot) (Snapshot, bool) {
	refreshKey := remoteCatalogRefreshKey(ctx, current)
	target, ok := resolveRemoteCatalogTarget(config, ctx, codexHome)
	if !ok {
		return Snapshot{}, false
	}
	endpoint, ok := modelsEndpointURL(target.BaseURL)
	if !ok {
		return Snapshot{}, false
	}
	models, etag, ok := fetchRemoteCatalogModels(endpoint, target, current)
	if !ok || len(models) == 0 {
		rememberRemoteCatalogRefreshFailure(refreshKey)
		return Snapshot{}, false
	}

	snapshot := Snapshot{
		FetchedAt:    time.Now().UTC(),
		ETag:         etag,
		ProviderID:   firstNonEmptyTrimmed(target.ProviderID, current.ProviderID, ctx.ProviderID),
		ProviderName: firstNonEmptyTrimmed(target.ProviderName, current.ProviderName, ctx.ProviderName),
		ProfileName:  firstNonEmptyTrimmed(current.ProfileName, ctx.ProfileName),
		Models:       models,
	}
	switch {
	case ctx.HasProfile:
		_ = SaveProfileSnapshot(ctx.Profile, codexHome, snapshot)
	case strings.TrimSpace(ctx.SidecarPath) != "":
		_ = SaveSnapshot(ctx.SidecarPath, snapshot)
	}
	clearRemoteCatalogRefreshFailure(refreshKey)
	return snapshot, true
}

func remoteCatalogRefreshKey(ctx RuntimeContext, snapshot Snapshot) string {
	return firstNonEmptyTrimmed(
		ctx.SidecarPath,
		ctx.ProfileName,
		ctx.ProviderID,
		ctx.ProviderName,
		snapshot.ProfileName,
		snapshot.ProviderID,
		snapshot.ProviderName,
	)
}

func refreshBlockedByFailureCooldown(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	raw, ok := remoteCatalogRefreshFailures.Load(key)
	if !ok {
		return false
	}
	attemptedAt, ok := raw.(time.Time)
	if !ok {
		remoteCatalogRefreshFailures.Delete(key)
		return false
	}
	if time.Since(attemptedAt) >= remoteCatalogRefreshFailureCooldown {
		remoteCatalogRefreshFailures.Delete(key)
		return false
	}
	return true
}

func rememberRemoteCatalogRefreshFailure(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	remoteCatalogRefreshFailures.Store(key, time.Now().UTC())
}

func clearRemoteCatalogRefreshFailure(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	remoteCatalogRefreshFailures.Delete(key)
}

func resolveRemoteCatalogTarget(config state.Config, ctx RuntimeContext, codexHome string) (remoteCatalogTarget, bool) {
	providerID := NormalizeProviderID(firstNonEmptyTrimmed(ctx.ProviderID))
	providerName := firstNonEmptyTrimmed(ctx.ProviderName, providerID)
	builtinAlias := normalizeBuiltinOpenAIProviderAlias(firstNonEmptyTrimmed(ctx.ProviderName, ctx.ProviderID))
	baseURL := ""
	token := ""
	headers := make(http.Header)

	if ctx.HasProvider {
		providerName = firstNonEmptyTrimmed(ctx.Provider.DisplayName(), ctx.Provider.Name, providerName)
		if displayID := NormalizeProviderID(ctx.Provider.DisplayName()); displayID != "" {
			providerID = displayID
		}
		if providerID == "" {
			providerID = NormalizeProviderID(providerName)
		}
		baseURL = strings.TrimSpace(ctx.Provider.BaseURL)
		token = strings.TrimSpace(ctx.Provider.BearerToken())
		if builtinAlias == "" {
			builtinAlias = normalizeBuiltinOpenAIProviderAlias(firstNonEmptyTrimmed(providerName, ctx.Provider.DisplayName(), ctx.ProviderID))
		}
		if token == "" && looksLikeBuiltinOpenAIProvider(providerName, ctx.Provider) {
			token = loadBuiltinOpenAIToken(codexHome)
		}
	}

	if builtinAlias != "" {
		providerID = "openai"
	}
	if providerID == "" {
		providerID = NormalizeProviderID(firstNonEmptyTrimmed(providerName, ctx.ProviderName))
	}
	if providerID == "" {
		providerID = "openai"
	}
	if baseURL == "" {
		baseURL = builtinProviderBaseURL(providerID)
	}
	if token == "" && (isBuiltinOpenAIProviderID(providerID) || builtinAlias != "") {
		token = loadBuiltinOpenAIToken(codexHome)
	}
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(token) == "" {
		return remoteCatalogTarget{}, false
	}

	headers.Set("Accept", "application/json")
	if providerID == "anthropic" {
		headers.Set("x-api-key", token)
		headers.Set("anthropic-version", "2023-06-01")
	} else {
		headers.Set("Authorization", "Bearer "+token)
	}

	return remoteCatalogTarget{
		ProviderID:   providerID,
		ProviderName: providerName,
		BaseURL:      baseURL,
		Token:        token,
		Headers:      headers,
	}, true
}

func builtinProviderBaseURL(providerID string) string {
	switch NormalizeProviderID(providerID) {
	case "openai":
		return "https://api.openai.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "xai":
		return "https://api.x.ai/v1"
	case "together":
		return "https://api.together.xyz/v1"
	case "fireworks":
		return "https://api.fireworks.ai/inference/v1"
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "sambanova":
		return "https://api.sambanova.ai/v1"
	case "perplexity":
		return "https://api.perplexity.ai"
	case "ollama":
		return "http://127.0.0.1:11434/v1"
	default:
		return ""
	}
}

func loadBuiltinOpenAIToken(codexHome string) string {
	if value := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); value != "" {
		return value
	}
	authPath := apphome.NewLayout(codexHome).AuthPath()
	value, err := state.LoadOpenAIAPIKey(authPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func modelsEndpointURL(baseURL string) (string, bool) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", false
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", false
	}
	pathValue := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(pathValue, "/chat/completions"):
		pathValue = strings.TrimSuffix(pathValue, "/chat/completions")
	case strings.HasSuffix(pathValue, "/responses"):
		pathValue = strings.TrimSuffix(pathValue, "/responses")
	case strings.HasSuffix(pathValue, "/models"):
		pathValue = strings.TrimSuffix(pathValue, "/models")
	}
	if pathValue == "" {
		pathValue = "/v1"
	}
	parsed.Path = strings.TrimRight(pathValue, "/") + "/models"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), true
}

func fetchRemoteCatalogModels(endpoint string, target remoteCatalogTarget, current Snapshot) ([]Model, string, bool) {
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", false
	}
	for key, values := range target.Headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", false
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, "", false
	}
	entries := parseRemoteCatalogEntries(body)
	if len(entries) == 0 {
		return nil, "", false
	}
	return mergeFetchedCatalogModels(target.ProviderID, current, entries), strings.TrimSpace(response.Header.Get("ETag")), true
}

func parseRemoteCatalogEntries(body []byte) []remoteCatalogModel {
	var envelope remoteCatalogEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil {
		if models, ok := decodeRemoteCatalogModelList(envelope.Data); ok {
			return models
		}
		if models, ok := decodeRemoteCatalogModelList(envelope.Models); ok {
			return models
		}
	}
	if models, ok := decodeRemoteCatalogModelList(json.RawMessage(body)); ok {
		return models
	}
	return nil
}

func decodeRemoteCatalogModelList(raw json.RawMessage) ([]remoteCatalogModel, bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}

	models := make([]remoteCatalogModel, 0, len(items))
	for _, item := range items {
		var model remoteCatalogModel
		if err := json.Unmarshal(item, &model); err == nil {
			model.ID = strings.TrimSpace(model.ID)
			model.Name = strings.TrimSpace(model.Name)
			if model.ID != "" || model.Name != "" {
				models = append(models, model)
				continue
			}
		}

		var slug string
		if err := json.Unmarshal(item, &slug); err == nil {
			slug = strings.TrimSpace(slug)
			if slug != "" {
				models = append(models, remoteCatalogModel{ID: slug})
			}
		}
	}
	return models, true
}

func mergeFetchedCatalogModels(providerID string, current Snapshot, entries []remoteCatalogModel) []Model {
	existing := NewCatalog(providerID, current)
	seen := make(map[string]struct{}, len(entries))
	models := make([]Model, 0, len(entries))
	for _, entry := range entries {
		model := remoteCatalogModelToModel(providerID, entry)
		if strings.TrimSpace(model.Slug) == "" {
			continue
		}
		if _, ok := seen[model.Slug]; ok {
			continue
		}
		seen[model.Slug] = struct{}{}
		if currentModel, ok := existing.Lookup(model.Slug); ok {
			model = mergeCatalogModelMetadata(model, currentModel)
		} else {
			model = inferCatalogModelMetadata(providerID, model, entry)
		}
		models = append(models, model)
	}
	sortModels(models)
	return models
}

func remoteCatalogModelToModel(providerID string, entry remoteCatalogModel) Model {
	slug := firstNonEmptyTrimmed(entry.ID, entry.Name)
	if NormalizeProviderID(providerID) == "gemini" && slug != "" && !strings.HasPrefix(strings.ToLower(slug), "models/") && !strings.Contains(slug, "/") {
		slug = "models/" + slug
	}
	displayName := firstNonEmptyTrimmed(entry.DisplayName)
	if displayName == "" {
		name := strings.TrimSpace(entry.Name)
		if name != "" && !strings.EqualFold(name, slug) {
			displayName = name
		}
	}
	contextWindow := firstNonZero(entry.ContextWin, entry.ContextLen, entry.MaxContext, entry.InputTokens, entry.MaxInputTokens, entry.TopProvider.ContextLength)
	return Model{
		Slug:                  slug,
		DisplayName:           displayName,
		Description:           strings.TrimSpace(entry.Description),
		ContextWindow:         contextWindow,
		SupportsParallelTools: entry.Capabilities.ParallelToolCalls || supportsParallelParameters(entry.SupportedParameters),
	}
}

func mergeCatalogModelMetadata(fetched Model, existing Model) Model {
	if strings.TrimSpace(fetched.DisplayName) == "" {
		fetched.DisplayName = strings.TrimSpace(existing.DisplayName)
	}
	if strings.TrimSpace(fetched.Description) == "" {
		fetched.Description = strings.TrimSpace(existing.Description)
	}
	if fetched.ContextWindow == 0 {
		fetched.ContextWindow = existing.ContextWindow
	}
	if strings.TrimSpace(fetched.DefaultReasoningLevel) == "" {
		fetched.DefaultReasoningLevel = strings.TrimSpace(existing.DefaultReasoningLevel)
	}
	if len(fetched.SupportedReasoningLevels) == 0 && len(existing.SupportedReasoningLevels) > 0 {
		fetched.SupportedReasoningLevels = append([]ReasoningLevel(nil), existing.SupportedReasoningLevels...)
	}
	if fetched.Priority == 0 {
		fetched.Priority = existing.Priority
	}
	fetched.SupportsParallelTools = fetched.SupportsParallelTools || existing.SupportsParallelTools
	fetched.SupportsReasoningSummary = fetched.SupportsReasoningSummary || existing.SupportsReasoningSummary
	return fetched
}

func inferCatalogModelMetadata(providerID string, model Model, entry remoteCatalogModel) Model {
	switch NormalizeProviderID(providerID) {
	case "openai":
		lower := strings.ToLower(strings.TrimSpace(model.Slug))
		if strings.HasPrefix(lower, "gpt-5") || strings.Contains(lower, "codex") {
			model.DefaultReasoningLevel = "medium"
			model.SupportedReasoningLevels = []ReasoningLevel{
				{Effort: "low"},
				{Effort: "medium"},
				{Effort: "high"},
				{Effort: "xhigh"},
			}
		}
	case "mistral":
		if entry.Capabilities.Reasoning {
			model.DefaultReasoningLevel = "high"
			model.SupportedReasoningLevels = []ReasoningLevel{
				{Effort: "none"},
				{Effort: "high"},
			}
		}
	}
	return model
}

func isBuiltinOpenAIProviderID(providerID string) bool {
	switch NormalizeProviderID(providerID) {
	case "openai", "codex-oauth":
		return true
	default:
		return false
	}
}

func looksLikeBuiltinOpenAIProvider(providerName string, provider state.ProviderConfig) bool {
	name := strings.ToLower(strings.TrimSpace(providerName))
	display := strings.ToLower(strings.TrimSpace(provider.DisplayName()))
	baseURL := strings.ToLower(strings.TrimSpace(provider.BaseURL))
	return strings.Contains(name, "openai") || strings.Contains(display, "openai") || strings.Contains(baseURL, "api.openai.com")
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func normalizeBuiltinOpenAIProviderAlias(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "openai", "openai api", "openai-api", "openai_api":
		return "openai"
	case "codex_oauth", "codex-oauth", "oauth", "chatgpt", "openai-oauth", "codex":
		return "codex_oauth"
	default:
		return ""
	}
}

func supportsParallelParameters(values []string) bool {
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "parallel_tool_calls", "parallel-tools":
			return true
		}
	}
	return false
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

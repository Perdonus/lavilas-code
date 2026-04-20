package accountprofiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/provider/responsesapi"
	"github.com/Perdonus/lavilas-code/internal/state"
)

type ProviderSpec struct {
	ID                string
	NameEN            string
	NameRU            string
	BaseURL           string
	WireAPI           string
	APIKeyOptional    bool
	BuiltinProviderID string
	RequiresBaseURL   bool
	DefaultModel      string
	DefaultReasoning  string
	SeedModels        []modelcatalog.Model
}

type StoredProfile struct {
	Provider                string `json:"provider"`
	Name                    string `json:"name"`
	BaseURL                 string `json:"base_url,omitempty"`
	Model                   string `json:"model"`
	ModelCatalogJSON        string `json:"model_catalog_json,omitempty"`
	ConfigProfile           string `json:"config_profile,omitempty"`
	ModelProviderID         string `json:"model_provider_id,omitempty"`
	ExperimentalBearerToken string `json:"experimental_bearer_token,omitempty"`
}

type StoredProfileRecord struct {
	Key     string
	Path    string
	Profile StoredProfile
}

type StoredProfileStatus struct {
	RecordKey       string
	ConfigProfile   string
	ProviderKey     string
	CatalogPath     string
	HasSavedAPIKey  bool
	BuiltinProvider bool
}

var providerSpecs = []ProviderSpec{
	{
		ID:                "codex_oauth",
		NameEN:            "Codex OAuth",
		NameRU:            "Codex OAuth",
		BaseURL:           responsesapi.DefaultBaseURL + "/v1",
		WireAPI:           "responses",
		APIKeyOptional:    true,
		BuiltinProviderID: "openai",
		DefaultModel:      "gpt-5.3-codex",
		DefaultReasoning:  "medium",
		SeedModels: []modelcatalog.Model{
			seedModel("gpt-5.3-codex", "GPT-5.3 Codex", "medium", []string{"low", "medium", "high", "xhigh"}),
			seedModel("gpt-5.4", "GPT-5.4", "high", []string{"low", "medium", "high", "xhigh"}),
		},
	},
	{
		ID:               "openai",
		NameEN:           "OpenAI API",
		NameRU:           "OpenAI API",
		BaseURL:          openai.DefaultBaseURL + "/v1",
		WireAPI:          "responses",
		DefaultModel:     "gpt-5.3-codex",
		DefaultReasoning: "medium",
		SeedModels: []modelcatalog.Model{
			seedModel("gpt-5.3-codex", "GPT-5.3 Codex", "medium", []string{"low", "medium", "high", "xhigh"}),
			seedModel("gpt-5.4", "GPT-5.4", "high", []string{"low", "medium", "high", "xhigh"}),
		},
	},
	{
		ID:               "openrouter",
		NameEN:           "OpenRouter",
		NameRU:           "OpenRouter",
		BaseURL:          "https://openrouter.ai/api/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "openai/gpt-5.3-codex",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("openai/gpt-5.3-codex", "GPT-5.3 Codex", "none", nil),
			seedModel("anthropic/claude-sonnet-4", "Claude Sonnet 4", "none", nil),
		},
	},
	{
		ID:               "anthropic",
		NameEN:           "Anthropic",
		NameRU:           "Anthropic",
		BaseURL:          "https://api.anthropic.com/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "claude-sonnet-4-0",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("claude-sonnet-4-0", "Claude Sonnet 4", "none", nil),
			seedModel("claude-opus-4-1", "Claude Opus 4.1", "none", nil),
		},
	},
	{
		ID:               "gemini",
		NameEN:           "Gemini",
		NameRU:           "Gemini",
		BaseURL:          "https://generativelanguage.googleapis.com/v1beta/openai",
		WireAPI:          "chat_completions",
		DefaultModel:     "gemini-2.5-pro",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("models/gemini-2.5-pro", "Gemini 2.5 Pro", "none", nil),
			seedModel("models/gemini-2.5-flash", "Gemini 2.5 Flash", "none", nil),
		},
	},
	{
		ID:               "mistral",
		NameEN:           "Mistral",
		NameRU:           "Mistral",
		BaseURL:          "https://api.mistral.ai/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "mistral-vibe-cli-with-tools",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("mistral-vibe-cli-with-tools", "Mistral Vibe CLI Tools", "none", nil),
			seedModel("mistral-small-latest", "Mistral Small", "high", []string{"none", "high"}),
		},
	},
	{
		ID:               "groq",
		NameEN:           "Groq",
		NameRU:           "Groq",
		BaseURL:          "https://api.groq.com/openai/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "llama-3.3-70b-versatile",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("llama-3.3-70b-versatile", "Llama 3.3 70B Versatile", "none", nil),
		},
	},
	{
		ID:               "deepseek",
		NameEN:           "DeepSeek",
		NameRU:           "DeepSeek",
		BaseURL:          "https://api.deepseek.com/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "deepseek-chat",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("deepseek-chat", "DeepSeek Chat", "none", nil),
			seedModel("deepseek-reasoner", "DeepSeek Reasoner", "none", nil),
		},
	},
	{
		ID:               "xai",
		NameEN:           "xAI",
		NameRU:           "xAI",
		BaseURL:          "https://api.x.ai/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "grok-4",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("grok-4", "Grok 4", "none", nil),
			seedModel("grok-3-mini", "Grok 3 Mini", "none", nil),
		},
	},
	{
		ID:               "together",
		NameEN:           "Together AI",
		NameRU:           "Together AI",
		BaseURL:          "https://api.together.xyz/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "meta-llama/Llama-3.3-70B-Instruct-Turbo",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("meta-llama/Llama-3.3-70B-Instruct-Turbo", "Llama 3.3 70B Turbo", "none", nil),
		},
	},
	{
		ID:               "fireworks",
		NameEN:           "Fireworks AI",
		NameRU:           "Fireworks AI",
		BaseURL:          "https://api.fireworks.ai/inference/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "accounts/fireworks/models/llama-v3p3-70b-instruct",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("accounts/fireworks/models/llama-v3p3-70b-instruct", "Llama 3.3 70B Instruct", "none", nil),
		},
	},
	{
		ID:               "cerebras",
		NameEN:           "Cerebras",
		NameRU:           "Cerebras",
		BaseURL:          "https://api.cerebras.ai/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "llama-4-scout-17b-16e-instruct",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("llama-4-scout-17b-16e-instruct", "Llama 4 Scout", "none", nil),
		},
	},
	{
		ID:               "sambanova",
		NameEN:           "SambaNova",
		NameRU:           "SambaNova",
		BaseURL:          "https://api.sambanova.ai/v1",
		WireAPI:          "chat_completions",
		DefaultModel:     "Meta-Llama-3.1-70B-Instruct",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("Meta-Llama-3.1-70B-Instruct", "Llama 3.1 70B Instruct", "none", nil),
		},
	},
	{
		ID:               "perplexity",
		NameEN:           "Perplexity",
		NameRU:           "Perplexity",
		BaseURL:          "https://api.perplexity.ai",
		WireAPI:          "chat_completions",
		DefaultModel:     "sonar-pro",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("sonar-pro", "Sonar Pro", "none", nil),
		},
	},
	{
		ID:               "ollama",
		NameEN:           "Ollama",
		NameRU:           "Ollama",
		BaseURL:          "http://127.0.0.1:11434/v1",
		WireAPI:          "chat_completions",
		APIKeyOptional:   true,
		DefaultModel:     "qwen2.5-coder:latest",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("qwen2.5-coder:latest", "Qwen 2.5 Coder", "none", nil),
		},
	},
	{
		ID:               "custom",
		NameEN:           "Custom provider",
		NameRU:           "Свой провайдер",
		WireAPI:          "chat_completions",
		RequiresBaseURL:  true,
		DefaultModel:     "custom-model",
		DefaultReasoning: "none",
		SeedModels: []modelcatalog.Model{
			seedModel("custom-model", "Custom Model", "none", nil),
		},
	},
}

func seedModel(slug, displayName, defaultReasoning string, reasoning []string) modelcatalog.Model {
	levels := make([]modelcatalog.ReasoningLevel, 0, len(reasoning))
	for _, effort := range reasoning {
		levels = append(levels, modelcatalog.ReasoningLevel{Effort: effort, Description: effort})
	}
	return modelcatalog.Model{Slug: slug, DisplayName: displayName, DefaultReasoningLevel: defaultReasoning, SupportedReasoningLevels: levels, Priority: 1, SupportsParallelTools: true}
}

func SupportedProviders() []ProviderSpec {
	out := make([]ProviderSpec, len(providerSpecs))
	copy(out, providerSpecs)
	return out
}

func Provider(provider string) (ProviderSpec, bool) {
	normalized := normalizeProviderAlias(provider)
	for _, spec := range providerSpecs {
		if spec.ID == normalized {
			return spec, true
		}
	}
	return ProviderSpec{}, false
}

func ProviderDisplayName(provider string, russian bool) string {
	spec, ok := Provider(provider)
	if !ok {
		return strings.TrimSpace(provider)
	}
	if russian {
		return spec.NameRU
	}
	return spec.NameEN
}

func DefaultProfileModel(provider string) string {
	if spec, ok := Provider(provider); ok && strings.TrimSpace(spec.DefaultModel) != "" {
		return spec.DefaultModel
	}
	return "custom-model"
}

func DefaultReasoning(provider string) string {
	if spec, ok := Provider(provider); ok {
		return strings.TrimSpace(spec.DefaultReasoning)
	}
	return ""
}

func SanitizeProfileKey(profileName, provider string) string {
	normalized := strings.TrimSpace(profileName)
	if normalized == "" {
		normalized = normalizeProviderAlias(provider) + "-profile"
	}
	var builder strings.Builder
	for _, ch := range normalized {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9', ch == '-', ch == '_':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-_")
	result = strings.ToLower(strings.TrimSpace(result))
	if result == "" {
		return normalizeProviderAlias(provider) + "-profile"
	}
	return result
}

func ProfilesDir(codexHome string) string {
	return apphome.NewLayout(codexHome).ProfilesDir()
}

func StoredProfilePath(codexHome, profileKey string) string {
	return filepath.Join(ProfilesDir(codexHome), strings.TrimSpace(profileKey)+".json")
}

func DefaultSidecarPath(codexHome, profileKey string) string {
	return filepath.Join(ProfilesDir(codexHome), strings.TrimSpace(profileKey)+".models.json")
}

func derivedSidecarPath(profilePath string) string {
	if strings.TrimSpace(profilePath) == "" {
		return ""
	}
	base := filepath.Dir(profilePath)
	filename := filepath.Base(profilePath)
	if filename == "." {
		return ""
	}
	stem := strings.TrimSpace(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if strings.TrimSpace(stem) == "" {
		return ""
	}
	if base == "." || base == "" {
		return stem + ".models.json"
	}
	return filepath.Join(base, stem+".models.json")
}

func ResolveSidecarPath(codexHome, profilePath string, stored StoredProfile) string {
	if custom := strings.TrimSpace(stored.ModelCatalogJSON); custom != "" {
		if filepath.IsAbs(custom) {
			return filepath.Clean(custom)
		}
		base := filepath.Dir(profilePath)
		if base == "." || base == "" {
			base = ProfilesDir(codexHome)
		}
		return filepath.Clean(filepath.Join(base, custom))
	}
	if derived := derivedSidecarPath(profilePath); strings.TrimSpace(derived) != "" {
		return filepath.Clean(derived)
	}
	if configProfile := StoredProfileConfigKey("", stored); configProfile != "" && strings.TrimSpace(codexHome) != "" {
		return DefaultSidecarPath(codexHome, configProfile)
	}
	return ""
}

func StoredProfileConfigKey(profileKey string, stored StoredProfile) string {
	return firstNonEmpty(strings.TrimSpace(stored.ConfigProfile), strings.TrimSpace(profileKey))
}

func StoredProfileProviderKey(profileKey string, stored StoredProfile) string {
	normalized := normalizeStoredProfile(profileKey, stored)
	if spec, ok := Provider(normalized.Provider); ok && strings.TrimSpace(spec.BuiltinProviderID) != "" {
		return spec.BuiltinProviderID
	}
	return firstNonEmpty(
		strings.TrimSpace(normalized.ModelProviderID),
		strings.TrimSpace(profileKey)+"-provider",
		StoredProfileConfigKey(profileKey, normalized)+"-provider",
	)
}

func StoredProfileCatalogPath(codexHome, profileKey string, stored StoredProfile) string {
	normalized := normalizeStoredProfile(profileKey, stored)
	path := ResolveSidecarPath(codexHome, StoredProfilePath(codexHome, profileKey), normalized)
	if strings.TrimSpace(path) != "" {
		return path
	}
	if strings.TrimSpace(profileKey) != "" {
		return DefaultSidecarPath(codexHome, profileKey)
	}
	return ""
}

func StoredProfileHasSavedKey(stored StoredProfile) bool {
	if strings.TrimSpace(stored.ExperimentalBearerToken) != "" {
		return true
	}
	spec, ok := Provider(stored.Provider)
	return ok && spec.APIKeyOptional
}

func StoredProfileStatusFor(codexHome, profileKey string, stored StoredProfile) StoredProfileStatus {
	normalized := normalizeStoredProfile(profileKey, stored)
	spec, ok := Provider(normalized.Provider)
	return StoredProfileStatus{
		RecordKey:       strings.TrimSpace(profileKey),
		ConfigProfile:   StoredProfileConfigKey(profileKey, normalized),
		ProviderKey:     StoredProfileProviderKey(profileKey, normalized),
		CatalogPath:     StoredProfileCatalogPath(codexHome, profileKey, normalized),
		HasSavedAPIKey:  StoredProfileHasSavedKey(normalized),
		BuiltinProvider: ok && strings.TrimSpace(spec.BuiltinProviderID) != "",
	}
}

func normalizeStoredProfile(profileKey string, stored StoredProfile) StoredProfile {
	normalized := StoredProfile{
		Provider:                strings.TrimSpace(stored.Provider),
		Name:                    strings.TrimSpace(stored.Name),
		BaseURL:                 strings.TrimSpace(stored.BaseURL),
		Model:                   strings.TrimSpace(stored.Model),
		ModelCatalogJSON:        strings.TrimSpace(stored.ModelCatalogJSON),
		ConfigProfile:           strings.TrimSpace(stored.ConfigProfile),
		ModelProviderID:         strings.TrimSpace(stored.ModelProviderID),
		ExperimentalBearerToken: strings.TrimSpace(stored.ExperimentalBearerToken),
	}
	if normalized.ConfigProfile == "" {
		normalized.ConfigProfile = strings.TrimSpace(profileKey)
	}
	if normalized.Name == "" {
		normalized.Name = firstNonEmpty(normalized.ConfigProfile, strings.TrimSpace(profileKey))
	}
	if spec, ok := Provider(normalized.Provider); ok {
		normalized.Provider = spec.ID
		if normalized.Model == "" {
			normalized.Model = DefaultProfileModel(firstNonEmpty(spec.BuiltinProviderID, spec.ID))
		}
		if strings.TrimSpace(spec.BuiltinProviderID) != "" {
			normalized.BaseURL = ""
			normalized.ExperimentalBearerToken = ""
			normalized.ModelProviderID = spec.BuiltinProviderID
		}
		return normalized
	}
	if normalized.Model == "" {
		normalized.Model = DefaultProfileModel(normalized.Provider)
	}
	return normalized
}

func CreateOrUpdateStoredProfile(codexHome, provider, profileName string, baseURL, apiKey *string) (string, StoredProfile, string, error) {
	spec, ok := Provider(provider)
	if !ok {
		return "", StoredProfile{}, "", fmt.Errorf("unsupported provider %q", provider)
	}
	if spec.RequiresBaseURL && strings.TrimSpace(deref(baseURL)) == "" {
		return "", StoredProfile{}, "", fmt.Errorf("provider %q requires base_url", provider)
	}
	if !spec.APIKeyOptional && spec.BuiltinProviderID == "" && strings.TrimSpace(deref(apiKey)) == "" {
		return "", StoredProfile{}, "", fmt.Errorf("provider %q requires an API key", provider)
	}
	profileKey := SanitizeProfileKey(profileName, spec.ID)
	modelSeedProvider := firstNonEmpty(strings.TrimSpace(spec.BuiltinProviderID), spec.ID)
	stored := StoredProfile{
		Provider:         spec.ID,
		Name:             strings.TrimSpace(profileName),
		Model:            DefaultProfileModel(modelSeedProvider),
		ModelCatalogJSON: DefaultSidecarPath(codexHome, profileKey),
		ConfigProfile:    profileKey,
	}
	if spec.BuiltinProviderID != "" {
		stored.ModelProviderID = spec.BuiltinProviderID
	} else {
		stored.BaseURL = strings.TrimSpace(firstNonEmpty(deref(baseURL), spec.BaseURL))
		stored.ModelProviderID = profileKey + "-provider"
		stored.ExperimentalBearerToken = strings.TrimSpace(deref(apiKey))
	}
	stored = normalizeStoredProfile(profileKey, stored)
	path, err := SaveStoredProfile(codexHome, profileKey, stored)
	if err != nil {
		return "", StoredProfile{}, "", err
	}
	if err := ensureSeedSidecar(codexHome, path, stored, spec); err != nil {
		return "", StoredProfile{}, "", err
	}
	return profileKey, stored, path, nil
}

func SaveStoredProfile(codexHome, profileKey string, stored StoredProfile) (string, error) {
	path := StoredProfilePath(codexHome, profileKey)
	stored = normalizeStoredProfile(profileKey, stored)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func LoadStoredProfile(path string) (StoredProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return StoredProfile{}, err
	}
	var stored StoredProfile
	if err := json.Unmarshal(data, &stored); err != nil {
		return StoredProfile{}, err
	}
	profileKey := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	stored = normalizeStoredProfile(profileKey, stored)
	if strings.TrimSpace(stored.ModelCatalogJSON) == "" {
		stored.ModelCatalogJSON = ResolveSidecarPath("", path, stored)
	}
	return stored, nil
}

func DeleteStoredProfile(codexHome, profileKey string) error {
	profilePath := StoredProfilePath(codexHome, profileKey)
	stored, _ := LoadStoredProfile(profilePath)
	paths := []string{profilePath, DefaultSidecarPath(codexHome, profileKey)}
	if custom := strings.TrimSpace(stored.ModelCatalogJSON); custom != "" {
		paths = append(paths, ResolveSidecarPath(codexHome, profilePath, stored))
	}
	var firstErr error
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func ListStoredProfiles(codexHome string) ([]StoredProfileRecord, error) {
	entries, err := os.ReadDir(ProfilesDir(codexHome))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	records := make([]StoredProfileRecord, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || strings.HasSuffix(entry.Name(), ".models.json") || entry.Name() == "settings.json" {
			continue
		}
		path := filepath.Join(ProfilesDir(codexHome), entry.Name())
		stored, err := LoadStoredProfile(path)
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(entry.Name(), ".json")
		records = append(records, StoredProfileRecord{Key: key, Path: path, Profile: stored})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	return records, nil
}

func ApplyStoredProfile(config *state.Config, codexHome string, profileKey string, stored StoredProfile) error {
	stored = normalizeStoredProfile(profileKey, stored)
	spec, ok := Provider(stored.Provider)
	if !ok {
		return fmt.Errorf("unsupported provider %q", stored.Provider)
	}
	status := StoredProfileStatusFor(codexHome, profileKey, stored)
	providerName := status.ProviderKey
	profileName := status.ConfigProfile
	if profileName == "" {
		return fmt.Errorf("stored profile %q resolved an empty config profile", profileKey)
	}
	if providerName == "" {
		return fmt.Errorf("stored profile %q resolved an empty provider key", profileKey)
	}

	profileConfig, found := config.Profile(profileName)
	if !found {
		profileConfig = state.ProfileConfig{Name: profileName}
	}
	profileConfig.Name = profileName
	profileConfig.SetProvider(providerName)
	profileConfig.SetModel(firstNonEmpty(strings.TrimSpace(stored.Model), spec.DefaultModel))
	profileConfig.SetReasoningEffort(firstNonEmpty(strings.TrimSpace(spec.DefaultReasoning), strings.TrimSpace(profileConfig.ReasoningEffort)))
	if status.BuiltinProvider {
		profileConfig.SetCatalogPath("")
	} else {
		profileConfig.SetCatalogPath(status.CatalogPath)
	}
	config.UpsertProfile(profileConfig)

	if !status.BuiltinProvider {
		providerConfig, found := config.Provider(providerName)
		if !found {
			providerConfig = state.ProviderConfig{Name: providerName}
		}
		providerConfig.Name = providerName
		providerConfig.BaseURL = firstNonEmpty(strings.TrimSpace(stored.BaseURL), strings.TrimSpace(spec.BaseURL))
		providerConfig.WireAPI = firstNonEmpty(strings.TrimSpace(spec.WireAPI), "chat_completions")
		providerConfig.APIKeyEnv = ""
		if providerConfig.Fields == nil {
			providerConfig.Fields = make(state.ConfigFields)
		}
		providerConfig.Fields.Set("name", state.StringConfigValue(ProviderDisplayName(spec.ID, false)))
		providerConfig.Fields.Set("requires_openai_auth", state.BoolConfigValue(false))
		providerConfig.Fields.Set("supports_websockets", state.BoolConfigValue(false))
		providerConfig.Fields.Delete("env_key_instructions")
		providerConfig.Fields.Delete("auth")
		if token := strings.TrimSpace(stored.ExperimentalBearerToken); token != "" {
			providerConfig.Fields.Set("experimental_bearer_token", state.StringConfigValue(token))
		} else {
			providerConfig.Fields.Delete("experimental_bearer_token")
		}
		config.UpsertProvider(providerConfig)
	}

	config.SetActiveProfile(profileName)
	config.SetModel(profileConfig.Model)
	config.SetReasoningEffort(profileConfig.ReasoningEffort)
	config.Model.Fields.Delete("model_catalog_json")
	return nil
}

func CleanStoredProfileFromConfig(config *state.Config, profileKey string) {
	if config == nil {
		return
	}
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return
	}

	matchedProfiles := make(map[string]state.ProfileConfig)
	for _, profile := range config.Profiles {
		if profileMatchesStoredProfileKey(profile, profileKey) {
			matchedProfiles[strings.TrimSpace(profile.Name)] = profile
		}
	}

	deletedProfiles := make(map[string]struct{}, len(matchedProfiles))
	for name := range matchedProfiles {
		if name == "" {
			continue
		}
		if config.DeleteProfile(name) {
			deletedProfiles[name] = struct{}{}
		}
	}
	if _, ok := deletedProfiles[profileKey]; !ok {
		clearActiveProfileReference(config, profileKey)
	}
	for name := range matchedProfiles {
		if _, ok := deletedProfiles[name]; ok {
			continue
		}
		clearActiveProfileReference(config, name)
	}

	providerCandidates := map[string]struct{}{
		profileKey + "-provider": {},
	}
	for name, profile := range matchedProfiles {
		if name != "" {
			providerCandidates[name+"-provider"] = struct{}{}
		}
		if providerName := strings.TrimSpace(profile.EffectiveProviderName()); providerName != "" {
			providerCandidates[providerName] = struct{}{}
		}
	}

	deletedProviders := make(map[string]struct{})
	for providerName := range providerCandidates {
		if !shouldDeleteStoredProfileProvider(*config, providerName, profileKey, deletedProfiles) {
			continue
		}
		if config.DeleteProvider(providerName) {
			deletedProviders[providerName] = struct{}{}
			continue
		}
		deletedProviders[providerName] = struct{}{}
	}

	rootProvider := strings.TrimSpace(config.Model.Fields.Text("model_provider"))
	if _, ok := deletedProviders[rootProvider]; ok {
		config.Model.Fields.Delete("model_provider")
	}
	if catalogPathMatchesProfileKey(config.Model.Fields.Text("model_catalog_json"), profileKey) {
		config.Model.Fields.Delete("model_catalog_json")
	}
}

func normalizeProviderAlias(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case "codex", "codex-oauth", "oauth", "chatgpt", "openai-oauth":
		return "codex_oauth"
	case "custom-openai", "custom-api", "custom-openai-compatible":
		return "custom"
	default:
		return normalized
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ensureSeedSidecar(codexHome, profilePath string, stored StoredProfile, spec ProviderSpec) error {
	sidecarPath := ResolveSidecarPath(codexHome, profilePath, stored)
	if _, err := os.Stat(sidecarPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	snapshot := modelcatalog.Snapshot{
		ProviderID:   modelcatalog.NormalizeProviderID(firstNonEmpty(strings.TrimSpace(spec.BuiltinProviderID), spec.ID)),
		ProviderName: ProviderDisplayName(spec.ID, false),
		ProfileName:  StoredProfileConfigKey("", stored),
		Models:       append([]modelcatalog.Model(nil), spec.SeedModels...),
	}
	return modelcatalog.SaveSnapshot(sidecarPath, snapshot)
}

func clearActiveProfileReference(config *state.Config, profileName string) {
	profileName = strings.TrimSpace(profileName)
	if profileName == "" || strings.TrimSpace(config.ActiveProfileName()) != profileName {
		return
	}
	if _, ok := config.Profile(profileName); !ok {
		config.UpsertProfile(state.ProfileConfig{Name: profileName})
	}
	config.DeleteProfile(profileName)
}

func profileMatchesStoredProfileKey(profile state.ProfileConfig, profileKey string) bool {
	profileKey = strings.TrimSpace(profileKey)
	if profileKey == "" {
		return false
	}
	if strings.TrimSpace(profile.Name) == profileKey {
		return true
	}
	if providerName := strings.TrimSpace(profile.EffectiveProviderName()); providerName != "" {
		if providerName == profileKey || providerName == profileKey+"-provider" {
			return true
		}
	}
	return catalogPathMatchesProfileKey(profile.CatalogPath(), profileKey)
}

func shouldDeleteStoredProfileProvider(config state.Config, providerName, profileKey string, deletedProfiles map[string]struct{}) bool {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return false
	}
	for _, profile := range config.Profiles {
		if strings.TrimSpace(profile.EffectiveProviderName()) == providerName {
			return false
		}
	}
	if providerName == profileKey+"-provider" {
		return true
	}
	for name := range deletedProfiles {
		if name != "" && providerName == name+"-provider" {
			return true
		}
	}
	_, standard := Provider(providerName)
	return !standard
}

func catalogPathMatchesProfileKey(path, profileKey string) bool {
	path = strings.TrimSpace(path)
	profileKey = strings.TrimSpace(profileKey)
	if path == "" || profileKey == "" {
		return false
	}
	return filepath.Base(path) == profileKey+".models.json"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

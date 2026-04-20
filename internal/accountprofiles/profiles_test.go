package accountprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	"github.com/Perdonus/lavilas-code/internal/state"
)

func TestSanitizeProfileKey(t *testing.T) {
	if got := SanitizeProfileKey("My Profile!!", "mistral"); got != "my-profile" {
		t.Fatalf("sanitize = %q", got)
	}
	if got := SanitizeProfileKey("", "openrouter"); got != "openrouter-profile" {
		t.Fatalf("fallback sanitize = %q", got)
	}
}

func TestCreateAndLoadStoredProfile(t *testing.T) {
	home := t.TempDir()
	apiKey := "secret"
	profileKey, stored, path, err := CreateOrUpdateStoredProfile(home, "openrouter", "Main Router", nil, &apiKey)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	if profileKey != "main-router" {
		t.Fatalf("profile key = %q", profileKey)
	}
	if filepath.Base(path) != "main-router.json" {
		t.Fatalf("path = %q", path)
	}
	loaded, err := LoadStoredProfile(path)
	if err != nil {
		t.Fatalf("LoadStoredProfile: %v", err)
	}
	if loaded.Provider != "openrouter" {
		t.Fatalf("provider = %q", loaded.Provider)
	}
	if loaded.ExperimentalBearerToken != "secret" {
		t.Fatalf("token = %q", loaded.ExperimentalBearerToken)
	}
	if loaded.ModelProviderID != "main-router-provider" {
		t.Fatalf("provider id = %q", loaded.ModelProviderID)
	}
	if stored.ModelCatalogJSON == "" {
		t.Fatal("expected sidecar path")
	}
	if !StoredProfileHasSavedKey(loaded) {
		t.Fatal("expected saved API key status")
	}
}

func TestCreateAndLoadStoredProfileBuiltinProviderDoesNotPersistCustomFields(t *testing.T) {
	home := t.TempDir()
	baseURL := "https://override.example/v1"
	apiKey := "ignored-token"
	profileKey, stored, path, err := CreateOrUpdateStoredProfile(home, "codex_oauth", "Codex Main", &baseURL, &apiKey)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	if stored.BaseURL != "" {
		t.Fatalf("base_url = %q", stored.BaseURL)
	}
	if stored.ExperimentalBearerToken != "" {
		t.Fatalf("token = %q", stored.ExperimentalBearerToken)
	}
	if stored.ModelProviderID != "openai" {
		t.Fatalf("provider id = %q", stored.ModelProviderID)
	}

	loaded, err := LoadStoredProfile(path)
	if err != nil {
		t.Fatalf("LoadStoredProfile: %v", err)
	}
	if loaded.BaseURL != "" {
		t.Fatalf("loaded base_url = %q", loaded.BaseURL)
	}
	if loaded.ExperimentalBearerToken != "" {
		t.Fatalf("loaded token = %q", loaded.ExperimentalBearerToken)
	}
	status := StoredProfileStatusFor(home, profileKey, loaded)
	if !status.BuiltinProvider {
		t.Fatal("expected builtin provider status")
	}
	if !status.HasSavedAPIKey {
		t.Fatal("expected builtin profile to report saved access")
	}
	if status.ProviderKey != "openai" {
		t.Fatalf("provider key = %q", status.ProviderKey)
	}
	if status.CatalogPath != DefaultSidecarPath(home, profileKey) {
		t.Fatalf("catalog path = %q", status.CatalogPath)
	}

	snapshot, err := modelcatalog.LoadSnapshot(DefaultSidecarPath(home, profileKey))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snapshot.ProviderID != "openai" {
		t.Fatalf("snapshot.ProviderID = %q", snapshot.ProviderID)
	}
}

func TestLoadStoredProfileDerivesMissingSidecarPathFromProfilePath(t *testing.T) {
	home := t.TempDir()
	profilePath := filepath.Join(home, "Profiles", "derived.json")
	if err := os.MkdirAll(filepath.Dir(profilePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	payload := StoredProfile{
		Provider:      "mistral",
		Name:          "Derived",
		Model:         "mistral-vibe-cli-with-tools",
		ConfigProfile: "friendly",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(profilePath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loaded, err := LoadStoredProfile(profilePath)
	if err != nil {
		t.Fatalf("LoadStoredProfile: %v", err)
	}
	if loaded.ModelCatalogJSON != filepath.Join(home, "Profiles", "derived.models.json") {
		t.Fatalf("model catalog path = %q", loaded.ModelCatalogJSON)
	}
}

func TestStoredProfileStatusUsesRecordKeyForFallbackProviderAndCatalogPath(t *testing.T) {
	home := t.TempDir()
	status := StoredProfileStatusFor(home, "record-key", StoredProfile{
		Provider:      "custom",
		Name:          "Friendly",
		BaseURL:       "https://example.invalid/v1",
		Model:         "custom-model",
		ConfigProfile: "friendly",
	})
	if status.ConfigProfile != "friendly" {
		t.Fatalf("config profile = %q", status.ConfigProfile)
	}
	if status.ProviderKey != "record-key-provider" {
		t.Fatalf("provider key = %q", status.ProviderKey)
	}
	if status.CatalogPath != filepath.Join(home, "Profiles", "record-key.models.json") {
		t.Fatalf("catalog path = %q", status.CatalogPath)
	}
	if status.HasSavedAPIKey {
		t.Fatal("expected missing API key status")
	}
}

func TestApplyStoredProfileUpdatesCustomConfig(t *testing.T) {
	home := t.TempDir()
	apiKey := "secret"
	profileKey, stored, _, err := CreateOrUpdateStoredProfile(home, "mistral", "Mistral 1", nil, &apiKey)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	var config state.Config
	config.Model.Fields = make(state.ConfigFields)
	config.Model.Fields.Set("model_catalog_json", state.StringConfigValue("stale.models.json"))
	config.UpsertProvider(state.ProviderConfig{
		Name:      profileKey + "-provider",
		APIKeyEnv: "MISTRAL_KEY",
		Fields: state.ConfigFields{
			"auth":                 state.StringConfigValue("legacy"),
			"env_key_instructions": state.StringConfigValue("legacy"),
		},
	})
	if err := ApplyStoredProfile(&config, home, profileKey, stored); err != nil {
		t.Fatalf("ApplyStoredProfile: %v", err)
	}
	if config.ActiveProfileName() != profileKey {
		t.Fatalf("active profile = %q", config.ActiveProfileName())
	}
	profile, ok := config.Profile(profileKey)
	if !ok {
		t.Fatal("profile missing")
	}
	if profile.Model == "" || profile.Provider == "" {
		t.Fatalf("profile not hydrated: %+v", profile)
	}
	if profile.CatalogPath() != DefaultSidecarPath(home, profileKey) {
		t.Fatalf("catalog path = %q", profile.CatalogPath())
	}
	if got := config.Model.Fields.Text("model_catalog_json"); got != "" {
		t.Fatalf("root model_catalog_json = %q", got)
	}
	provider, ok := config.Provider(profile.Provider)
	if !ok {
		t.Fatal("provider missing")
	}
	if provider.Fields.Text("experimental_bearer_token") != "secret" {
		t.Fatalf("provider token = %q", provider.Fields.Text("experimental_bearer_token"))
	}
	if provider.APIKeyEnv != "" {
		t.Fatalf("env_key = %q", provider.APIKeyEnv)
	}
	if got := provider.Fields.Text("auth"); got != "" {
		t.Fatalf("auth field = %q", got)
	}
	if got := provider.Fields.Text("env_key_instructions"); got != "" {
		t.Fatalf("env_key_instructions = %q", got)
	}
}

func TestApplyStoredProfileBuiltinProviderSkipsProviderUpsert(t *testing.T) {
	home := t.TempDir()
	profileKey, stored, _, err := CreateOrUpdateStoredProfile(home, "codex_oauth", "Codex Main", nil, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	var config state.Config
	config.Model.Fields = make(state.ConfigFields)
	config.Model.Fields.Set("model_catalog_json", state.StringConfigValue("stale.models.json"))
	if err := ApplyStoredProfile(&config, home, profileKey, stored); err != nil {
		t.Fatalf("ApplyStoredProfile: %v", err)
	}
	profile, ok := config.Profile(profileKey)
	if !ok {
		t.Fatal("profile missing")
	}
	if profile.Provider != "openai" {
		t.Fatalf("profile provider = %q", profile.Provider)
	}
	if profile.CatalogPath() != "" {
		t.Fatalf("catalog path = %q", profile.CatalogPath())
	}
	if _, ok := config.Provider("openai"); ok {
		t.Fatal("builtin apply should not create provider config")
	}
	if got := config.Model.Fields.Text("model_catalog_json"); got != "" {
		t.Fatalf("root model_catalog_json = %q", got)
	}
}

func TestApplyStoredProfileBuiltinProviderLeavesExistingProviderConfigUntouched(t *testing.T) {
	home := t.TempDir()
	profileKey, stored, _, err := CreateOrUpdateStoredProfile(home, "codex_oauth", "Codex Main", nil, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	var config state.Config
	config.UpsertProvider(state.ProviderConfig{
		Name:    "openai",
		BaseURL: "https://existing.example/v1",
		WireAPI: "responses",
		Fields: state.ConfigFields{
			"name":                      state.StringConfigValue("Existing OpenAI"),
			"experimental_bearer_token": state.StringConfigValue("keep-me"),
		},
	})
	if err := ApplyStoredProfile(&config, home, profileKey, stored); err != nil {
		t.Fatalf("ApplyStoredProfile: %v", err)
	}
	provider, ok := config.Provider("openai")
	if !ok {
		t.Fatal("expected existing provider to remain")
	}
	if provider.BaseURL != "https://existing.example/v1" {
		t.Fatalf("base_url = %q", provider.BaseURL)
	}
	if provider.Fields.Text("experimental_bearer_token") != "keep-me" {
		t.Fatalf("token = %q", provider.Fields.Text("experimental_bearer_token"))
	}
}

func TestCleanStoredProfileFromConfigRemovesAliasedProfileAndOwnedProvider(t *testing.T) {
	temp := t.TempDir()
	recordKey := "record-key"
	catalogPath := filepath.Join(temp, "Profiles", recordKey+".models.json")

	var config state.Config
	config.SetActiveProfile("friendly")
	config.SetModelProvider("corp-prod")
	config.Model.Fields.Set("model_catalog_json", state.StringConfigValue(catalogPath))

	aliased := state.ProfileConfig{Name: "friendly"}
	aliased.SetProvider("corp-prod")
	aliased.SetModel("custom-model")
	aliased.SetCatalogPath(catalogPath)
	config.UpsertProfile(aliased)
	config.UpsertProvider(state.ProviderConfig{Name: "corp-prod", BaseURL: "https://corp.example/v1"})

	shared := state.ProfileConfig{Name: "shared"}
	shared.SetProvider("shared-provider")
	config.UpsertProfile(shared)
	config.UpsertProvider(state.ProviderConfig{Name: "shared-provider", BaseURL: "https://shared.example/v1"})

	CleanStoredProfileFromConfig(&config, recordKey)

	if config.ActiveProfileName() != "" {
		t.Fatalf("active profile = %q", config.ActiveProfileName())
	}
	if _, ok := config.Profile("friendly"); ok {
		t.Fatal("expected aliased profile to be removed")
	}
	if _, ok := config.Provider("corp-prod"); ok {
		t.Fatal("expected owned provider to be removed")
	}
	if got := config.Model.Fields.Text("model_provider"); got != "" {
		t.Fatalf("root model_provider = %q", got)
	}
	if got := config.Model.Fields.Text("model_catalog_json"); got != "" {
		t.Fatalf("root model_catalog_json = %q", got)
	}
	if _, ok := config.Profile("shared"); !ok {
		t.Fatal("expected unrelated profile to remain")
	}
	if _, ok := config.Provider("shared-provider"); !ok {
		t.Fatal("expected unrelated provider to remain")
	}
}

func TestCleanStoredProfileFromConfigKeepsStandardProviderEntries(t *testing.T) {
	temp := t.TempDir()
	recordKey := "codex-main"
	catalogPath := filepath.Join(temp, "Profiles", recordKey+".models.json")

	var config state.Config
	profile := state.ProfileConfig{Name: recordKey}
	profile.SetProvider("openai")
	profile.SetCatalogPath(catalogPath)
	config.UpsertProfile(profile)
	config.UpsertProvider(state.ProviderConfig{Name: "openai", BaseURL: "https://existing.example/v1"})

	CleanStoredProfileFromConfig(&config, recordKey)

	if _, ok := config.Profile(recordKey); ok {
		t.Fatal("expected profile to be removed")
	}
	if _, ok := config.Provider("openai"); !ok {
		t.Fatal("expected standard provider entry to remain")
	}
}

package accountprofiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	if stored.ModelCatalogJSON == "" {
		t.Fatal("expected sidecar path")
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
		ConfigProfile: "derived",
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

func TestApplyStoredProfileUpdatesConfig(t *testing.T) {
	home := t.TempDir()
	apiKey := "secret"
	profileKey, stored, _, err := CreateOrUpdateStoredProfile(home, "mistral", "Mistral 1", nil, &apiKey)
	if err != nil {
		t.Fatalf("CreateOrUpdateStoredProfile: %v", err)
	}
	var config state.Config
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
	provider, ok := config.Provider(profile.Provider)
	if !ok {
		t.Fatal("provider missing")
	}
	if provider.Fields.Text("experimental_bearer_token") != "secret" {
		t.Fatalf("provider token = %q", provider.Fields.Text("experimental_bearer_token"))
	}
}

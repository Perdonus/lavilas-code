package modelcatalog

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/state"
)

func TestNormalizeProviderID(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"OpenAI API":         "openai",
		"OpenRouter.ai":      "openrouter",
		"Google AI Studio":   "gemini",
		"generativelanguage": "gemini",
		"Mistral AI":         "mistral",
		"Anthropic":          "anthropic",
	}

	for input, want := range cases {
		if got := NormalizeProviderID(input); got != want {
			t.Fatalf("NormalizeProviderID(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeAndCanonicalizeModelSlug(t *testing.T) {
	t.Parallel()

	if got := NormalizeModelSlug("gemini", "models/gemini-flash-latest"); got != "models/"+canonicalGeminiFlashModel {
		t.Fatalf("NormalizeModelSlug(gemini) = %q", got)
	}

	if got := CanonicalizeProviderModelSlug("mistral-vibe-cli-with-tools"); got != mistralCanonicalMedium {
		t.Fatalf("CanonicalizeProviderModelSlug(mistral tools) = %q", got)
	}

	if got := CanonicalizeProviderModelSlug("vendor/mistral-vibe-cli-fast"); got != "vendor/"+mistralCanonicalFast {
		t.Fatalf("CanonicalizeProviderModelSlug(namespaced fast) = %q", got)
	}
}

func TestCatalogLookupPrefersNormalizedAndTailMatches(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog("gemini", Snapshot{
		Models: []Model{
			{Slug: "models/gemini-2.5-flash", DisplayName: "Gemini Flash", Priority: 2},
			{Slug: "models/gemini-2.5-pro", DisplayName: "Gemini Pro", Priority: 1},
		},
	})

	for _, query := range []string{"models/gemini-flash-latest", "gemini-2.5-flash"} {
		model, ok := catalog.Lookup(query)
		if !ok {
			t.Fatalf("Lookup(%q) failed", query)
		}
		if model.Slug != "models/gemini-2.5-flash" {
			t.Fatalf("Lookup(%q) returned %q", query, model.Slug)
		}
	}
}

func TestCatalogLookupUsesCanonicalTailForMistralAliases(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog("mistral", Snapshot{
		Models: []Model{
			{Slug: "mistral-small-latest", DisplayName: "Mistral Small", Priority: 2},
			{Slug: "mistral-medium-latest", DisplayName: "Mistral Medium", Priority: 1},
		},
	})

	model, ok := catalog.Lookup("mistral-vibe-cli-with-tools")
	if !ok {
		t.Fatal("Lookup(mistral-vibe-cli-with-tools) failed")
	}
	if model.Slug != "mistral-medium-latest" {
		t.Fatalf("Lookup returned %q", model.Slug)
	}
}

func TestFamilyAndPresetGroups(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog("openrouter", Snapshot{
		Models: []Model{
			{Slug: "anthropic/claude-haiku-4", Priority: 2},
			{Slug: "anthropic/claude-opus-4", Priority: 1},
			{Slug: "google/gemini-2.5-flash", Priority: 3},
			{Slug: "openai/gpt-4.1", Priority: 4},
		},
	})

	families := catalog.FamilyGroups()
	if len(families) != 4 {
		t.Fatalf("FamilyGroups() len = %d, want 4", len(families))
	}

	presets := catalog.PresetGroups()
	if len(presets) != 3 {
		t.Fatalf("PresetGroups() len = %d, want 3", len(presets))
	}
	if presets[0].Key != "fast" || presets[1].Key != "balanced" || presets[2].Key != "power" {
		t.Fatalf("PresetGroups order = %#v", presets)
	}
}

func TestResolveAndPersistProfileSidecar(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	profile := state.ProfileConfig{
		Name:     "mistral-profile",
		Provider: "Mistral",
		Fields: state.ConfigFields{
			"model_catalog_json": state.StringConfigValue("Profiles/custom.models.json"),
		},
	}

	path := ResolveProfileSidecarPath(profile, tempDir)
	wantPath := filepath.Join(tempDir, "Profiles", "custom.models.json")
	if path != wantPath {
		t.Fatalf("ResolveProfileSidecarPath() = %q, want %q", path, wantPath)
	}

	snapshot := Snapshot{
		Models: []Model{{Slug: "mistral-medium-latest", DisplayName: "Mistral Medium"}},
	}
	if err := SaveProfileSnapshot(profile, tempDir, snapshot); err != nil {
		t.Fatalf("SaveProfileSnapshot() error = %v", err)
	}

	loaded, err := LoadProfileSnapshot(profile, tempDir)
	if err != nil {
		t.Fatalf("LoadProfileSnapshot() error = %v", err)
	}
	if loaded.ProfileName != "mistral-profile" {
		t.Fatalf("loaded.ProfileName = %q", loaded.ProfileName)
	}
	if loaded.ProviderID != "mistral" {
		t.Fatalf("loaded.ProviderID = %q", loaded.ProviderID)
	}
	if !reflect.DeepEqual(loaded.Models, snapshot.Models) {
		t.Fatalf("loaded models = %#v, want %#v", loaded.Models, snapshot.Models)
	}
}

func TestParseSnapshotSupportsBareArray(t *testing.T) {
	t.Parallel()

	snapshot, err := ParseSnapshot([]byte(`[{"slug":"gpt-5.4","display_name":"GPT-5.4"}]`))
	if err != nil {
		t.Fatalf("ParseSnapshot() error = %v", err)
	}
	if len(snapshot.Models) != 1 || snapshot.Models[0].Slug != "gpt-5.4" {
		t.Fatalf("ParseSnapshot() = %#v", snapshot)
	}
}

func TestLoadSnapshotReturnsCatalogNotFound(t *testing.T) {
	t.Parallel()

	_, err := LoadSnapshot(filepath.Join(t.TempDir(), "missing.models.json"))
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("LoadSnapshot() error = %v, want ErrCatalogNotFound", err)
	}
}

func TestDiscoverSidecars(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	profilesDir := filepath.Join(tempDir, "Profiles")
	if err := os.MkdirAll(profilesDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, name := range []string{"b.models.json", "a.models.json", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(profilesDir, name), []byte("{}"), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", name, err)
		}
	}

	refs, err := DiscoverSidecars(tempDir)
	if err != nil {
		t.Fatalf("DiscoverSidecars() error = %v", err)
	}

	want := []SidecarRef{
		{ProfileName: "a", Path: filepath.Join(profilesDir, "a.models.json")},
		{ProfileName: "b", Path: filepath.Join(profilesDir, "b.models.json")},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("DiscoverSidecars() = %#v, want %#v", refs, want)
	}
}

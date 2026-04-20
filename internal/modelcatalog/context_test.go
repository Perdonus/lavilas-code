package modelcatalog

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/state"
)

func TestResolveRuntimeContextLoadsProfileSidecar(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	profile := state.ProfileConfig{
		Name:     "mistral-profile",
		Provider: "mistral-provider",
		Model:    "mistral-vibe-cli-with-tools",
	}
	snapshot := Snapshot{
		ProviderID:  "mistral",
		ProfileName: profile.Name,
		Models:      []Model{{Slug: "mistral-medium-latest", DisplayName: "Mistral Medium"}},
	}
	if err := SaveProfileSnapshot(profile, codexHome, snapshot); err != nil {
		t.Fatalf("SaveProfileSnapshot() error = %v", err)
	}

	config := state.Config{Model: state.ModelConfig{Profile: profile.Name}, Profiles: []state.ProfileConfig{profile}}
	ctx, err := ResolveRuntimeContext(config, codexHome, "", "")
	if err != nil {
		t.Fatalf("ResolveRuntimeContext() error = %v", err)
	}
	if !ctx.HasProfile || !ctx.SidecarFound {
		t.Fatalf("ResolveRuntimeContext() = %#v", ctx)
	}
	if ctx.ProviderID != "mistral" {
		t.Fatalf("ctx.ProviderID = %q", ctx.ProviderID)
	}
	model, ok := ctx.Catalog.Lookup("mistral-vibe-cli-with-tools")
	if !ok || model.Slug != "mistral-medium-latest" {
		t.Fatalf("catalog lookup = %#v, %v", model, ok)
	}
}

func TestEffectivePresetChoicePrefersSettingsOverride(t *testing.T) {
	t.Parallel()

	catalog := NewCatalog("openrouter", Snapshot{Models: []Model{{Slug: "openai/gpt-4.1-mini", DisplayName: "GPT 4.1 Mini"}}})
	settings := state.Settings{}
	settings.SetModelPreset("openrouter", "fast", state.ModelPresetConfig{
		Name:      "Cheap Fast",
		Model:     "openai/gpt-4.1-mini",
		Reasoning: "low",
	})

	preset, ok := EffectivePresetChoice(catalog, settings, "openrouter", "fast")
	if !ok {
		t.Fatal("EffectivePresetChoice() failed")
	}
	if preset.Source != "settings" {
		t.Fatalf("preset.Source = %q", preset.Source)
	}
	if preset.Model.DisplayName != "Cheap Fast" {
		t.Fatalf("preset.Model.DisplayName = %q", preset.Model.DisplayName)
	}
	if preset.Reasoning != "low" {
		t.Fatalf("preset.Reasoning = %q", preset.Reasoning)
	}
}

func TestDeleteProfileSnapshotRemovesSidecar(t *testing.T) {
	t.Parallel()

	codexHome := t.TempDir()
	profile := state.ProfileConfig{
		Name: "gemini-profile",
		Fields: state.ConfigFields{
			"model_catalog_json": state.StringConfigValue("Profiles/custom.models.json"),
		},
	}
	if err := SaveProfileSnapshot(profile, codexHome, Snapshot{Models: []Model{{Slug: "models/gemini-2.5-flash"}}}); err != nil {
		t.Fatalf("SaveProfileSnapshot() error = %v", err)
	}
	if err := DeleteProfileSnapshot(profile, codexHome); err != nil {
		t.Fatalf("DeleteProfileSnapshot() error = %v", err)
	}
	_, err := LoadSnapshot(filepath.Join(codexHome, "Profiles", "custom.models.json"))
	if !errors.Is(err, ErrCatalogNotFound) {
		t.Fatalf("LoadSnapshot() error = %v, want ErrCatalogNotFound", err)
	}
}

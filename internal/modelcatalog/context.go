package modelcatalog

import (
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/state"
)

var orderedPresetGroups = []struct {
	Key   string
	Label string
}{
	{Key: "fast", Label: "Fast"},
	{Key: "balanced", Label: "Balanced"},
	{Key: "power", Label: "Power"},
}

type RuntimeContext struct {
	ProfileName  string               `json:"profile_name,omitempty"`
	HasProfile   bool                 `json:"has_profile"`
	Profile      state.ProfileConfig  `json:"profile,omitempty"`
	ProviderName string               `json:"provider_name,omitempty"`
	ProviderID   string               `json:"provider_id,omitempty"`
	HasProvider  bool                 `json:"has_provider"`
	Provider     state.ProviderConfig `json:"provider,omitempty"`
	SidecarPath  string               `json:"sidecar_path,omitempty"`
	SidecarFound bool                 `json:"sidecar_found"`
	Snapshot     Snapshot             `json:"snapshot,omitempty"`
	Catalog      Catalog              `json:"-"`
}

type EffectivePreset struct {
	Key       string `json:"key"`
	Label     string `json:"label,omitempty"`
	Source    string `json:"source,omitempty"`
	Model     Model  `json:"model"`
	Reasoning string `json:"reasoning,omitempty"`
}

type SidecarSummary struct {
	ProfileName string    `json:"profile_name,omitempty"`
	Path        string    `json:"path,omitempty"`
	ProviderID  string    `json:"provider_id,omitempty"`
	Found       bool      `json:"found"`
	FetchedAt   time.Time `json:"fetched_at,omitempty"`
	ModelCount  int       `json:"model_count"`
}

func ResolveRuntimeContext(config state.Config, codexHome, profileName, providerName string) (RuntimeContext, error) {
	ctx := RuntimeContext{}
	profileName = strings.TrimSpace(profileName)
	providerName = strings.TrimSpace(providerName)

	if profileName == "" {
		profileName = strings.TrimSpace(config.ActiveProfileName())
	}
	if profileName != "" {
		profile, ok := config.Profile(profileName)
		if ok {
			ctx.HasProfile = true
			ctx.ProfileName = profile.Name
			ctx.Profile = profile
		}
	}

	if providerName == "" && ctx.HasProfile {
		providerName = strings.TrimSpace(ctx.Profile.EffectiveProviderName())
	}
	if providerName == "" {
		providerName = strings.TrimSpace(config.EffectiveProviderName())
	}

	if providerName != "" {
		if provider, ok := config.Provider(providerName); ok {
			ctx.HasProvider = true
			ctx.Provider = provider
			ctx.ProviderName = provider.Name
		} else {
			ctx.ProviderName = providerName
		}
	}
	if ctx.ProviderName == "" && ctx.HasProfile {
		ctx.ProviderName = strings.TrimSpace(ctx.Profile.EffectiveProviderName())
	}
	ctx.ProviderID = NormalizeProviderID(ctx.ProviderName)
	if ctx.ProviderID == "" && ctx.HasProvider {
		ctx.ProviderID = NormalizeProviderID(ctx.Provider.DisplayName())
	}

	var snapshot Snapshot
	var err error
	if ctx.HasProfile {
		ctx.SidecarPath = ResolveProfileSidecarPath(ctx.Profile, codexHome)
		snapshot, err = LoadProfileSnapshot(ctx.Profile, codexHome)
	} else if path, ok := ResolveConfigSidecarPath(config, profileName, codexHome); ok {
		ctx.SidecarPath = path
		snapshot, err = LoadSnapshot(path)
	}
	if err != nil {
		if !errors.Is(err, ErrCatalogNotFound) {
			return RuntimeContext{}, err
		}
		ctx.SidecarFound = false
		return ctx, nil
	}

	ctx.SidecarFound = true
	if snapshot.ProfileName == "" {
		snapshot.ProfileName = ctx.ProfileName
	}
	if snapshot.ProviderID == "" {
		snapshot.ProviderID = ctx.ProviderID
	}
	if ctx.ProviderID == "" {
		ctx.ProviderID = NormalizeProviderID(snapshot.ProviderID)
	}
	ctx.Snapshot = snapshot
	ctx.Catalog = NewCatalog(ctx.ProviderID, snapshot)
	return ctx, nil
}

func ResolveModelChoice(providerID string, catalog Catalog, slug string) (Model, bool) {
	if model, ok := catalog.Lookup(slug); ok {
		return model, true
	}

	normalized := NormalizeModelSlug(providerID, slug)
	if normalized == "" {
		return Model{}, false
	}
	canonical := CanonicalizeProviderModelSlug(normalized)
	if canonical == "" {
		canonical = normalized
	}
	return Model{
		Slug:        canonical,
		DisplayName: displayName(Model{Slug: canonical}),
		Description: "manual",
		Priority:    999,
	}, false
}

func EffectivePresetChoices(catalog Catalog, settings state.Settings, providerID string) []EffectivePreset {
	result := make([]EffectivePreset, 0, len(orderedPresetGroups))
	for _, entry := range orderedPresetGroups {
		preset, ok := EffectivePresetChoice(catalog, settings, providerID, entry.Key)
		if !ok {
			continue
		}
		result = append(result, preset)
	}
	return result
}

func EffectivePresetChoice(catalog Catalog, settings state.Settings, providerID, key string) (EffectivePreset, bool) {
	providerID = NormalizeProviderID(providerID)
	key = strings.TrimSpace(strings.ToLower(key))
	if providerID == "" || key == "" {
		return EffectivePreset{}, false
	}
	label := presetLabel(key)

	if settings.ModelPresets.Enabled {
		if preset, ok := settings.ModelPreset(providerID, key); ok && strings.TrimSpace(preset.Model) != "" {
			model, _ := ResolveModelChoice(providerID, catalog, preset.Model)
			if strings.TrimSpace(preset.Name) != "" {
				model.DisplayName = strings.TrimSpace(preset.Name)
			}
			reasoning := strings.TrimSpace(preset.Reasoning)
			if reasoning == "" {
				reasoning = strings.TrimSpace(model.DefaultReasoningLevel)
			}
			return EffectivePreset{
				Key:       key,
				Label:     label,
				Source:    "settings",
				Model:     model,
				Reasoning: reasoning,
			}, true
		}
	}

	for _, group := range catalog.PresetGroups() {
		if group.Key != key || len(group.Models) == 0 {
			continue
		}
		model := group.Models[0]
		return EffectivePreset{
			Key:       key,
			Label:     label,
			Source:    "catalog",
			Model:     model,
			Reasoning: strings.TrimSpace(model.DefaultReasoningLevel),
		}, true
	}

	return EffectivePreset{}, false
}

func DeleteProfileSnapshot(profile state.ProfileConfig, codexHome string) error {
	path := ResolveProfileSidecarPath(profile, codexHome)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func SidecarSummaryForProfile(profile state.ProfileConfig, codexHome string) (SidecarSummary, error) {
	summary := SidecarSummary{
		ProfileName: profile.Name,
		Path:        ResolveProfileSidecarPath(profile, codexHome),
		ProviderID:  ProviderIDFromProfile(profile),
	}
	snapshot, err := LoadProfileSnapshot(profile, codexHome)
	if err != nil {
		if errors.Is(err, ErrCatalogNotFound) {
			return summary, nil
		}
		return SidecarSummary{}, err
	}
	summary.Found = true
	summary.FetchedAt = snapshot.FetchedAt
	summary.ModelCount = len(snapshot.Models)
	if summary.ProviderID == "" {
		summary.ProviderID = NormalizeProviderID(snapshot.ProviderID)
	}
	return summary, nil
}

func presetLabel(key string) string {
	for _, entry := range orderedPresetGroups {
		if entry.Key == key {
			return entry.Label
		}
	}
	return titleSlugPart(key)
}

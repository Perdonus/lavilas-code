package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

type editablePreset struct {
	Key       string
	Label     string
	Model     string
	Reasoning string
	Source    string
	Saved     bool
}

func (m *Model) openModelPresetsSettingsPalette(pushCurrent bool) tea.Cmd {
	return m.applyPaletteScreen(PaletteModeModelPresets, m.modelPresetsSettingsItems(), "", pushCurrent)
}

func (m *Model) reopenModelPresetsSettingsPalette() tea.Cmd {
	return m.replacePaletteRoot(PaletteModeModelPresets, m.modelPresetsSettingsItems(), "")
}

func (m *Model) modelPresetsSettingsItems() []PaletteItem {
	settings, _ := loadSettingsOptional(m.layout.SettingsPath())
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	ctx, _ := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")

	providerLabel := firstNonEmpty(strings.TrimSpace(ctx.ProviderName), strings.TrimSpace(ctx.ProviderID), localizedUnsetTUI(m.language))
	statusLabel := m.localize("disabled", "выключены")
	if settings.ModelPresets.Enabled {
		statusLabel = m.localize("enabled", "включены")
	}

	return []PaletteItem{
		{
			Key:         "model_presets.toggle",
			Title:       m.localize("Quick Presets", "Быстрые пресеты"),
			Description: statusLabel,
			Keywords:    []string{"presets", "toggle", "quick", "пресеты", "переключить"},
		},
		{
			Key:         "model_presets.provider",
			Title:       m.localize("Current Provider", "Текущий провайдер"),
			Description: providerLabel,
			Keywords:    []string{"provider", "current", "active", "провайдер", "текущий"},
		},
	}
}

func (m *Model) toggleModelPresetsEnabled() tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SetModelPresetsEnabled(!settings.ModelPresets.Enabled)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	if settings.ModelPresets.Enabled {
		m.state.Footer = m.localize("Quick presets enabled", "Быстрые пресеты включены")
	} else {
		m.state.Footer = m.localize("Quick presets disabled", "Быстрые пресеты выключены")
	}
	return m.reopenModelPresetsSettingsPalette()
}

func (m *Model) openCurrentProviderPresetEditor(pushCurrent bool) tea.Cmd {
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	items := []PaletteItem{{
		Key:         "preset.add",
		Title:       m.localize("Add Preset", "Добавить пресет"),
		Description: m.localize("Create a provider preset", "Создать пресет провайдера"),
		Keywords:    []string{"add", "preset", "create", "добавить", "пресет"},
	}}
	for _, preset := range m.currentProviderEditablePresets(ctx, settings, providerID) {
		descriptionParts := []string{firstNonEmpty(strings.TrimSpace(preset.Model), localizedUnsetTUI(m.language))}
		if reasoning := strings.TrimSpace(preset.Reasoning); reasoning != "" {
			descriptionParts = append(descriptionParts, reasoning)
		}
		if source := strings.TrimSpace(preset.Source); source != "" {
			descriptionParts = append(descriptionParts, source)
		}
		items = append(items, PaletteItem{
			Key:         "preset.entry",
			Title:       firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Key)),
			Description: strings.Join(descriptionParts, " · "),
			Value:       preset.Key,
			Keywords:    []string{preset.Key, preset.Label, preset.Model, preset.Reasoning, preset.Source},
		})
	}
	footer := fmt.Sprintf("%s: %s", m.localize("Provider", "Провайдер"), firstNonEmpty(strings.TrimSpace(ctx.ProviderName), providerID))
	return m.applyPaletteScreen(PaletteModePresetEditor, items, footer, pushCurrent)
}

func (m *Model) reopenCurrentProviderPresetEditor() tea.Cmd {
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	items := []PaletteItem{{
		Key:         "preset.add",
		Title:       m.localize("Add Preset", "Добавить пресет"),
		Description: m.localize("Create a provider preset", "Создать пресет провайдера"),
		Keywords:    []string{"add", "preset", "create", "добавить", "пресет"},
	}}
	for _, preset := range m.currentProviderEditablePresets(ctx, settings, providerID) {
		descriptionParts := []string{firstNonEmpty(strings.TrimSpace(preset.Model), localizedUnsetTUI(m.language))}
		if reasoning := strings.TrimSpace(preset.Reasoning); reasoning != "" {
			descriptionParts = append(descriptionParts, reasoning)
		}
		if source := strings.TrimSpace(preset.Source); source != "" {
			descriptionParts = append(descriptionParts, source)
		}
		items = append(items, PaletteItem{
			Key:         "preset.entry",
			Title:       firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Key)),
			Description: strings.Join(descriptionParts, " · "),
			Value:       preset.Key,
			Keywords:    []string{preset.Key, preset.Label, preset.Model, preset.Reasoning, preset.Source},
		})
	}
	footer := fmt.Sprintf("%s: %s", m.localize("Provider", "Провайдер"), firstNonEmpty(strings.TrimSpace(ctx.ProviderName), providerID))
	return m.replacePaletteRoot(PaletteModePresetEditor, items, footer)
}

func (m *Model) openCurrentProviderPresetActions(presetKey string, pushCurrent bool) tea.Cmd {
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	preset, ok := m.lookupEditablePreset(ctx, settings, providerID, presetKey)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Preset not found", "Пресет не найден"), presetKey)
		return nil
	}
	items := []PaletteItem{
		{
			Key:         "preset.rename",
			Title:       m.localize("Rename", "Переименовать"),
			Description: firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Key)),
			Value:       preset.Key,
			Keywords:    []string{"rename", "name", "переименовать", "имя"},
		},
		{
			Key:         "preset.model",
			Title:       m.localize("Change Model", "Сменить модель"),
			Description: firstNonEmpty(strings.TrimSpace(preset.Model), localizedUnsetTUI(m.language)),
			Value:       preset.Key,
			Keywords:    []string{"model", "change", "сменить", "модель"},
		},
	}
	if preset.Saved {
		items = append(items, PaletteItem{
			Key:         "preset.delete",
			Title:       m.localize("Delete", "Удалить"),
			Description: m.localize("Remove the saved override", "Удалить сохранённый оверрайд"),
			Value:       preset.Key,
			Keywords:    []string{"delete", "remove", "удалить"},
		})
	}
	return m.applyPaletteScreen(PaletteModePresetActions, items, "", pushCurrent)
}

func (m *Model) openCurrentProviderPresetModelPicker(presetKey string, pushCurrent bool) tea.Cmd {
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	models := ctx.Catalog.Models()
	if len(models) == 0 {
		m.state.Footer = m.localize("No models found for the active provider", "Для активного провайдера модели не найдены")
		return nil
	}
	sort.SliceStable(models, func(i, j int) bool {
		left := firstNonEmpty(strings.TrimSpace(models[i].DisplayName), strings.TrimSpace(models[i].Slug))
		right := firstNonEmpty(strings.TrimSpace(models[j].DisplayName), strings.TrimSpace(models[j].Slug))
		if models[i].Priority == models[j].Priority {
			return left < right
		}
		if models[i].Priority == 0 {
			return false
		}
		if models[j].Priority == 0 {
			return true
		}
		return models[i].Priority < models[j].Priority
	})

	currentModel := ""
	if presetKey != "" {
		if preset, ok := m.lookupEditablePreset(ctx, settings, providerID, presetKey); ok {
			currentModel = strings.TrimSpace(preset.Model)
		}
	}

	items := make([]PaletteItem, 0, len(models))
	for _, model := range models {
		descriptionParts := []string{}
		if text := strings.TrimSpace(model.Description); text != "" {
			descriptionParts = append(descriptionParts, text)
		}
		if reasoning := strings.TrimSpace(model.DefaultReasoningLevel); reasoning != "" {
			descriptionParts = append(descriptionParts, reasoning)
		}
		if currentModel != "" && strings.TrimSpace(model.Slug) == currentModel {
			descriptionParts = append(descriptionParts, m.localize("current", "текущая"))
		}
		items = append(items, PaletteItem{
			Key:         "preset.model.entry",
			Title:       firstNonEmpty(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.Slug)),
			Description: strings.Join(descriptionParts, " · "),
			Value:       strings.Join([]string{presetKey, model.Slug}, "\n"),
			Keywords:    []string{presetKey, model.Slug, model.DisplayName, model.Description},
		})
	}

	return m.applyPaletteScreen(PaletteModePresetModels, items, "", pushCurrent)
}

func (m *Model) openCurrentProviderPresetRenamePrompt(presetKey string) tea.Cmd {
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	preset, ok := m.lookupEditablePreset(ctx, settings, providerID, presetKey)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Preset not found", "Пресет не найден"), presetKey)
		return nil
	}
	return m.openPresetNamePrompt(providerID, preset.Key, preset.Model, firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Model)))
}

func (m *Model) applyCurrentProviderPresetModelSelection(value string) tea.Cmd {
	presetKey, modelSlug := splitPresetModelSelectionValue(value)
	if strings.TrimSpace(modelSlug) == "" {
		return nil
	}
	_, settings, ctx, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	model, _ := modelcatalog.ResolveModelChoice(providerID, ctx.Catalog, modelSlug)
	if strings.TrimSpace(presetKey) == "" {
		generatedKey := uniquePresetKey(settings, providerID, firstNonEmpty(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.Slug)))
		return m.openPresetNamePrompt(providerID, generatedKey, model.Slug, firstNonEmpty(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.Slug)))
	}
	preset, ok := m.lookupEditablePreset(ctx, settings, providerID, presetKey)
	if !ok {
		preset = editablePreset{Key: presetKey, Label: presetKey}
	}
	settingsPath := m.layout.SettingsPath()
	settings.SetModelPreset(providerID, preset.Key, appstate.ModelPresetConfig{
		Name:      firstNonEmpty(strings.TrimSpace(preset.Label), preset.Key),
		Model:     model.Slug,
		Reasoning: firstNonEmpty(strings.TrimSpace(preset.Reasoning), strings.TrimSpace(model.DefaultReasoningLevel)),
	})
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Preset model updated", "Модель пресета обновлена"), model.Slug)
	return m.reopenCurrentProviderPresetEditor()
}

func (m *Model) deleteCurrentProviderPreset(presetKey string) tea.Cmd {
	_, settings, _, providerID, err := m.resolvePresetContext()
	if err != nil {
		m.state.Footer = err.Error()
		return nil
	}
	settingsPath := m.layout.SettingsPath()
	settings.DeleteModelPreset(providerID, presetKey)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Preset deleted", "Пресет удалён"), presetKey)
	return m.reopenCurrentProviderPresetEditor()
}

func (m *Model) resolvePresetContext() (appstate.Config, appstate.Settings, modelcatalog.RuntimeContext, string, error) {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		return appstate.Config{}, appstate.Settings{}, modelcatalog.RuntimeContext{}, "", fmt.Errorf("%s: %w", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
	}
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		return appstate.Config{}, appstate.Settings{}, modelcatalog.RuntimeContext{}, "", fmt.Errorf("%s: %w", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
	}
	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
	if err != nil {
		return appstate.Config{}, appstate.Settings{}, modelcatalog.RuntimeContext{}, "", fmt.Errorf("%s: %w", m.localize("Failed to load provider catalog", "Не удалось загрузить каталог провайдера"), err)
	}
	providerID := firstNonEmpty(strings.TrimSpace(ctx.ProviderID), modelcatalog.NormalizeProviderID(ctx.ProviderName))
	if providerID == "" {
		return appstate.Config{}, appstate.Settings{}, modelcatalog.RuntimeContext{}, "", fmt.Errorf("%s", m.localize("No active provider", "Нет активного провайдера"))
	}
	return config, settings, ctx, providerID, nil
}

func (m *Model) currentProviderEditablePresets(ctx modelcatalog.RuntimeContext, settings appstate.Settings, providerID string) []editablePreset {
	presets := make(map[string]editablePreset)
	for _, preset := range modelcatalog.EffectivePresetChoices(ctx.Catalog, settings, providerID) {
		_, saved := settings.ModelPreset(providerID, preset.Key)
		presets[preset.Key] = editablePreset{
			Key:       preset.Key,
			Label:     firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Model.DisplayName), strings.TrimSpace(preset.Model.Slug)),
			Model:     strings.TrimSpace(preset.Model.Slug),
			Reasoning: strings.TrimSpace(preset.Reasoning),
			Source:    strings.TrimSpace(preset.Source),
			Saved:     saved,
		}
	}

	if providerPresets, ok := settings.ModelPresets.Providers[providerID]; ok {
		for key, preset := range providerPresets.Presets {
			model, _ := modelcatalog.ResolveModelChoice(providerID, ctx.Catalog, preset.Model)
			existing := presets[key]
			existing.Key = key
			existing.Label = firstNonEmpty(strings.TrimSpace(preset.Name), strings.TrimSpace(existing.Label), strings.TrimSpace(model.DisplayName), key)
			existing.Model = firstNonEmpty(strings.TrimSpace(model.Slug), strings.TrimSpace(preset.Model), strings.TrimSpace(existing.Model))
			existing.Reasoning = firstNonEmpty(strings.TrimSpace(preset.Reasoning), strings.TrimSpace(existing.Reasoning), strings.TrimSpace(model.DefaultReasoningLevel))
			existing.Source = m.localize("saved", "сохранён")
			existing.Saved = true
			presets[key] = existing
		}
	}

	keys := make([]string, 0, len(presets))
	for key := range presets {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left := presetSortRank(keys[i])
		right := presetSortRank(keys[j])
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})

	result := make([]editablePreset, 0, len(keys))
	for _, key := range keys {
		result = append(result, presets[key])
	}
	return result
}

func (m *Model) lookupEditablePreset(ctx modelcatalog.RuntimeContext, settings appstate.Settings, providerID, presetKey string) (editablePreset, bool) {
	for _, preset := range m.currentProviderEditablePresets(ctx, settings, providerID) {
		if preset.Key == presetKey {
			return preset, true
		}
	}
	return editablePreset{}, false
}

func presetSortRank(key string) int {
	switch strings.TrimSpace(strings.ToLower(key)) {
	case "fast":
		return 0
	case "balanced":
		return 1
	case "power":
		return 2
	default:
		return 10
	}
}

func uniquePresetKey(settings appstate.Settings, providerID string, seed string) string {
	key := sanitizePresetKey(seed)
	if key == "" {
		key = "preset"
	}
	if _, ok := settings.ModelPreset(providerID, key); !ok {
		return key
	}
	for index := 2; ; index++ {
		candidate := fmt.Sprintf("%s-%d", key, index)
		if _, ok := settings.ModelPreset(providerID, candidate); !ok {
			return candidate
		}
	}
}

func sanitizePresetKey(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var builder strings.Builder
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
		default:
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}

func splitPresetModelSelectionValue(value string) (string, string) {
	parts := strings.SplitN(value, "\n", 2)
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return "", strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

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

func (m *Model) openModelSettingsPalette(pushCurrent bool) tea.Cmd {
	return m.applyPaletteScreen(PaletteModeModelSettings, m.modelSettingsPaletteItems(), "", pushCurrent)
}

func (m *Model) openModelPickerPalette(pushCurrent bool) tea.Cmd {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load provider catalog", "Не удалось загрузить каталог провайдера"), err)
		return nil
	}

	presets := modelcatalog.EffectivePresetChoices(ctx.Catalog, settings, ctx.ProviderID)
	if !settings.ModelPresets.Enabled || len(presets) == 0 {
		return m.openAllModelsPalette(pushCurrent)
	}

	items := make([]PaletteItem, 0, len(presets)+1)
	currentModel := strings.TrimSpace(config.EffectiveModel())
	for _, preset := range presets {
		title := firstNonEmpty(strings.TrimSpace(preset.Label), strings.TrimSpace(preset.Model.DisplayName), strings.TrimSpace(preset.Model.Slug))
		descriptionParts := []string{
			firstNonEmpty(strings.TrimSpace(preset.Model.DisplayName), strings.TrimSpace(preset.Model.Slug)),
		}
		if reasoning := strings.TrimSpace(preset.Reasoning); reasoning != "" {
			descriptionParts = append(descriptionParts, reasoning)
		}
		if preset.Source != "" {
			descriptionParts = append(descriptionParts, preset.Source)
		}
		if currentModel == preset.Model.Slug {
			descriptionParts = append(descriptionParts, m.localize("current", "текущая"))
		}
		items = append(items, PaletteItem{
			Key:         "model.preset",
			Title:       title,
			Description: strings.Join(descriptionParts, " · "),
			Value:       strings.Join([]string{preset.Model.Slug, preset.Reasoning, ctx.ProviderName}, "\n"),
			Keywords: []string{
				preset.Key,
				preset.Label,
				preset.Model.Slug,
				preset.Model.DisplayName,
				preset.Reasoning,
			},
		})
	}
	items = append(items, PaletteItem{
		Key:         "model.catalog",
		Title:       m.localize("All Models", "Все модели"),
		Description: m.localize("Open the full provider catalog", "Открыть полный каталог провайдера"),
		Keywords:    []string{"all", "catalog", "models", "full", "все", "каталог", "модели"},
	})
	return m.applyPaletteScreen(PaletteModeModel, items, "", pushCurrent)
}

func (m *Model) openAllModelsPalette(pushCurrent bool) tea.Cmd {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load provider catalog", "Не удалось загрузить каталог провайдера"), err)
		return nil
	}
	models := ctx.Catalog.Models()
	if len(models) == 0 {
		m.state.Footer = m.localize("No models found for the active provider", "Для активного провайдера модели не найдены")
		return nil
	}
	sort.SliceStable(models, func(left, right int) bool {
		if models[left].Priority == models[right].Priority {
			return firstNonEmpty(models[left].DisplayName, models[left].Slug) < firstNonEmpty(models[right].DisplayName, models[right].Slug)
		}
		if models[left].Priority == 0 {
			return false
		}
		if models[right].Priority == 0 {
			return true
		}
		return models[left].Priority < models[right].Priority
	})

	currentModel := strings.TrimSpace(config.EffectiveModel())
	items := make([]PaletteItem, 0, len(models))
	for _, model := range models {
		descriptionParts := []string{}
		if text := strings.TrimSpace(model.Description); text != "" {
			descriptionParts = append(descriptionParts, text)
		}
		if reasoning := strings.TrimSpace(model.DefaultReasoningLevel); reasoning != "" {
			descriptionParts = append(descriptionParts, reasoning)
		}
		if currentModel == model.Slug {
			descriptionParts = append(descriptionParts, m.localize("current", "текущая"))
		}
		items = append(items, PaletteItem{
			Key:         "model.catalog.entry",
			Title:       firstNonEmpty(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.Slug)),
			Description: strings.Join(descriptionParts, " · "),
			Value:       strings.Join([]string{model.Slug, ctx.ProviderName}, "\n"),
			Keywords: []string{
				model.Slug,
				model.DisplayName,
				model.Description,
				model.DefaultReasoningLevel,
			},
		})
	}
	return m.applyPaletteScreen(PaletteModeModelCatalog, items, "", pushCurrent)
}

func (m *Model) openReasoningPalette(model modelcatalog.Model, providerName string, pushCurrent bool) tea.Cmd {
	choices := normalizeReasoningOptions(model)
	if len(choices) <= 1 {
		effort := ""
		if len(choices) == 1 {
			effort = choices[0].Effort
		}
		return m.applyModelSelection(model, providerName, effort)
	}
	items := make([]PaletteItem, 0, len(choices))
	for _, choice := range choices {
		items = append(items, PaletteItem{
			Key:         "model.reasoning.entry",
			Title:       m.reasoningLabel(choice.Effort),
			Description: firstNonEmpty(strings.TrimSpace(choice.Description), strings.TrimSpace(choice.Effort)),
			Value:       strings.Join([]string{model.Slug, providerName, choice.Effort}, "\n"),
			Keywords:    []string{choice.Effort, choice.Description, model.Slug, model.DisplayName},
		})
	}
	return m.applyPaletteScreen(PaletteModeReasoning, items, "", pushCurrent)
}

func (m *Model) modelSettingsPaletteItems() []PaletteItem {
	return []PaletteItem{
		{
			Key:         "model_settings.models",
			Title:       m.localize("Models", "Модели"),
			Description: m.localize("Choose model and reasoning", "Выбор модели и размышлений"),
			Aliases:     []string{"/model", "/модель"},
			Keywords:    []string{"model", "models", "reasoning", "модель", "модели", "размышления"},
		},
		{
			Key:         "model_settings.profiles",
			Title:       m.localize("Profiles", "Профили"),
			Description: m.localize("Switch or delete saved accounts", "Переключение и удаление аккаунтов"),
			Aliases:     []string{"/profiles", "/профили"},
			Keywords:    []string{"profiles", "accounts", "keys", "профили", "аккаунты", "ключи"},
		},
		{
			Key:         "model_settings.providers",
			Title:       m.localize("Providers", "Провайдеры"),
			Description: m.localize("Inspect and manage providers", "Просмотр и удаление провайдеров"),
			Aliases:     []string{"/providers", "/провайдеры"},
			Keywords:    []string{"providers", "api", "base_url", "wire_api", "провайдеры"},
		},
	}
}

func (m *Model) profilesManagerItems() []PaletteItem {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		return []PaletteItem{{
			Key:         "profiles.error",
			Title:       m.localize("Profiles unavailable", "Профили недоступны"),
			Description: err.Error(),
		}}
	}
	if len(config.Profiles) == 0 {
		return []PaletteItem{{
			Key:         "profiles.empty",
			Title:       m.localize("No profiles", "Нет профилей"),
			Description: m.localize("The config has no saved profiles", "В конфиге нет сохранённых профилей"),
		}}
	}

	activeProfile := strings.TrimSpace(config.ActiveProfileName())
	items := make([]PaletteItem, 0, len(config.Profiles))
	for _, profile := range config.Profiles {
		descriptionParts := []string{
			firstNonEmpty(strings.TrimSpace(profile.Model), localizedUnsetTUI(m.language)),
			firstNonEmpty(strings.TrimSpace(profile.EffectiveProviderName()), localizedUnsetTUI(m.language)),
			firstNonEmpty(strings.TrimSpace(profile.ReasoningEffort), localizedUnsetTUI(m.language)),
		}
		if strings.TrimSpace(profile.Name) == activeProfile {
			descriptionParts = append(descriptionParts, m.localize("active", "активен"))
		}
		items = append(items, PaletteItem{
			Key:         "profiles.entry",
			Title:       profile.Name,
			Description: strings.Join(descriptionParts, " · "),
			Value:       profile.Name,
			Keywords: []string{
				profile.Name,
				profile.Model,
				profile.EffectiveProviderName(),
				profile.ReasoningEffort,
			},
		})
	}
	return items
}

func (m *Model) openProfileActionsPalette(profileName string, pushCurrent bool) tea.Cmd {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	profile, ok := config.Profile(profileName)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Profile not found", "Профиль не найден"), profileName)
		return nil
	}
	items := []PaletteItem{
		{
			Key:         "profile.activate",
			Title:       m.localize("Activate", "Активировать"),
			Description: m.localize("Make this profile active", "Сделать этот профиль активным"),
			Value:       profile.Name,
			Keywords:    []string{"activate", "switch", "активировать", "переключить"},
		},
		{
			Key:         "profile.delete",
			Title:       m.localize("Delete", "Удалить"),
			Description: m.localize("Delete profile and sidecar snapshot", "Удалить профиль и sidecar-снимок"),
			Value:       profile.Name,
			Keywords:    []string{"delete", "remove", "удалить"},
		},
	}
	return m.applyPaletteScreen(PaletteModeProfileActions, items, "", pushCurrent)
}

func (m *Model) providersManagerItems() []PaletteItem {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		return []PaletteItem{{
			Key:         "providers.error",
			Title:       m.localize("Providers unavailable", "Провайдеры недоступны"),
			Description: err.Error(),
		}}
	}
	if len(config.ModelProviders) == 0 {
		return []PaletteItem{{
			Key:         "providers.empty",
			Title:       m.localize("No providers", "Нет провайдеров"),
			Description: m.localize("The config has no configured providers", "В конфиге нет настроенных провайдеров"),
		}}
	}

	activeProvider := strings.TrimSpace(config.EffectiveProviderName())
	items := make([]PaletteItem, 0, len(config.ModelProviders))
	for _, provider := range config.ModelProviders {
		descriptionParts := []string{
			firstNonEmpty(strings.TrimSpace(provider.BaseURL), localizedUnsetTUI(m.language)),
			firstNonEmpty(strings.TrimSpace(provider.WireAPI), "chat_completions"),
		}
		if provider.Name == activeProvider {
			descriptionParts = append(descriptionParts, m.localize("current", "текущий"))
		}
		items = append(items, PaletteItem{
			Key:         "providers.entry",
			Title:       provider.Name,
			Description: strings.Join(descriptionParts, " · "),
			Value:       provider.Name,
			Keywords: []string{
				provider.Name,
				provider.DisplayName(),
				provider.BaseURL,
				provider.WireAPI,
			},
		})
	}
	return items
}

func (m *Model) openProviderActionsPalette(providerName string, pushCurrent bool) tea.Cmd {
	config, err := loadConfigOptional(m.layout.ConfigPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	provider, ok := config.Provider(providerName)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Provider not found", "Провайдер не найден"), providerName)
		return nil
	}
	items := []PaletteItem{
		{
			Key:         "provider.activate",
			Title:       m.localize("Set Global Provider", "Сделать глобальным провайдером"),
			Description: m.localize("Update the root provider in config", "Обновить корневой провайдер в конфиге"),
			Value:       provider.Name,
			Keywords:    []string{"activate", "global", "provider", "активировать"},
		},
		{
			Key:         "provider.delete",
			Title:       m.localize("Delete", "Удалить"),
			Description: m.localize("Delete provider from config", "Удалить провайдера из конфига"),
			Value:       provider.Name,
			Keywords:    []string{"delete", "remove", "удалить"},
		},
	}
	return m.applyPaletteScreen(PaletteModeProviderActions, items, "", pushCurrent)
}

func (m *Model) applyModelSelection(model modelcatalog.Model, providerName string, reasoning string) tea.Cmd {
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	targetProfile := applyModelSelectionForTUI(&config, "", providerName, model, reasoning)
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	if targetProfile != "" {
		m.state.Footer = fmt.Sprintf("%s: %s · %s", m.localize("Model updated for profile", "Модель обновлена для профиля"), targetProfile, model.Slug)
	} else {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Model updated", "Модель обновлена"), model.Slug)
	}
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return nil
}

func (m *Model) activateProfile(profileName string) tea.Cmd {
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	if _, ok := config.Profile(profileName); !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Profile not found", "Профиль не найден"), profileName)
		return nil
	}
	config.SetActiveProfile(profileName)
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Active profile", "Активный профиль"), profileName)
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return nil
}

func (m *Model) deleteProfile(profileName string) tea.Cmd {
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	profile, ok := config.Profile(profileName)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Profile not found", "Профиль не найден"), profileName)
		return nil
	}
	if !config.DeleteProfile(profileName) {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Profile not found", "Профиль не найден"), profileName)
		return nil
	}
	if err := modelcatalog.DeleteProfileSnapshot(profile, apphome.CodexHome()); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to remove profile snapshot", "Не удалось удалить снимок профиля"), err)
		return nil
	}
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Deleted profile", "Профиль удалён"), profileName)
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return nil
}

func (m *Model) activateProvider(providerName string) tea.Cmd {
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	if _, ok := config.Provider(providerName); !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Provider not found", "Провайдер не найден"), providerName)
		return nil
	}
	config.SetModelProvider(providerName)
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Global provider", "Глобальный провайдер"), providerName)
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return nil
}

func (m *Model) deleteProvider(providerName string) tea.Cmd {
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	if !config.DeleteProvider(providerName) {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Provider not found", "Провайдер не найден"), providerName)
		return nil
	}
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Provider deleted", "Провайдер удалён"), providerName)
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return nil
}

func (m *Model) syncConfigState(config appstate.Config) {
	m.state.Model = strings.TrimSpace(config.EffectiveModel())
	m.state.Provider = strings.TrimSpace(config.EffectiveProviderName())
	m.state.Profile = strings.TrimSpace(config.ActiveProfileName())
	m.state.Reasoning = strings.TrimSpace(config.EffectiveReasoningEffort())

	m.options.Model = m.state.Model
	m.options.Provider = m.state.Provider
	m.options.Profile = m.state.Profile
	m.options.ReasoningEffort = m.state.Reasoning
	m.updateStatus()
}

func normalizeReasoningOptions(model modelcatalog.Model) []modelcatalog.ReasoningLevel {
	options := append([]modelcatalog.ReasoningLevel{}, model.SupportedReasoningLevels...)
	if len(options) == 0 && strings.TrimSpace(model.DefaultReasoningLevel) != "" {
		options = append(options, modelcatalog.ReasoningLevel{
			Effort:      model.DefaultReasoningLevel,
			Description: model.DefaultReasoningLevel,
		})
	}
	if len(options) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]modelcatalog.ReasoningLevel, 0, len(options))
	for _, option := range options {
		effort := strings.TrimSpace(option.Effort)
		if effort == "" {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		result = append(result, option)
	}
	return result
}

func (m *Model) reasoningLabel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none":
		return m.localize("No reasoning", "Без размышлений")
	case "minimal":
		return m.localize("Minimal", "Минимальный")
	case "low":
		return m.localize("Low", "Лёгкий")
	case "medium":
		return m.localize("Medium", "Стандартный")
	case "high":
		return m.localize("High", "Глубокий")
	case "xhigh":
		return m.localize("Extra high", "Максимальный")
	default:
		if effort == "" {
			return localizedUnsetTUI(m.language)
		}
		return effort
	}
}

func applyModelSelectionForTUI(config *appstate.Config, explicitProfile, providerName string, model modelcatalog.Model, reasoning string) string {
	targetProfile := strings.TrimSpace(explicitProfile)
	if targetProfile == "" {
		targetProfile = strings.TrimSpace(config.ActiveProfileName())
	}

	if targetProfile != "" {
		profile, ok := config.Profile(targetProfile)
		if !ok {
			profile = appstate.ProfileConfig{Name: targetProfile}
		}
		profile.Name = targetProfile
		profile.SetModel(model.Slug)
		if providerName = strings.TrimSpace(providerName); providerName != "" {
			profile.SetProvider(providerName)
			config.SetModelProvider(providerName)
		}
		if reasoning = strings.TrimSpace(reasoning); reasoning != "" {
			profile.SetReasoningEffort(reasoning)
		} else if strings.TrimSpace(profile.ReasoningEffort) == "" && strings.TrimSpace(model.DefaultReasoningLevel) != "" {
			profile.SetReasoningEffort(model.DefaultReasoningLevel)
		}
		config.UpsertProfile(profile)
		if strings.TrimSpace(explicitProfile) != "" || strings.TrimSpace(config.ActiveProfileName()) == "" {
			config.SetActiveProfile(targetProfile)
		}
		return targetProfile
	}

	config.SetModel(model.Slug)
	if providerName = strings.TrimSpace(providerName); providerName != "" {
		config.SetModelProvider(providerName)
	}
	if reasoning = strings.TrimSpace(reasoning); reasoning != "" {
		config.SetReasoningEffort(reasoning)
	} else if strings.TrimSpace(config.EffectiveReasoningEffort()) == "" && strings.TrimSpace(model.DefaultReasoningLevel) != "" {
		config.SetReasoningEffort(model.DefaultReasoningLevel)
	}
	return ""
}

func splitModelSelectionValue(value string) (slug string, reasoning string, providerName string) {
	parts := strings.SplitN(value, "\n", 3)
	if len(parts) > 0 {
		slug = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		reasoning = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		providerName = strings.TrimSpace(parts[2])
	}
	return slug, reasoning, providerName
}

func splitModelCatalogValue(value string) (slug string, providerName string) {
	parts := strings.SplitN(value, "\n", 2)
	if len(parts) > 0 {
		slug = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		providerName = strings.TrimSpace(parts[1])
	}
	return slug, providerName
}

func splitReasoningSelectionValue(value string) (slug string, providerName string, effort string) {
	parts := strings.SplitN(value, "\n", 3)
	if len(parts) > 0 {
		slug = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		providerName = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		effort = strings.TrimSpace(parts[2])
	}
	return slug, providerName, effort
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

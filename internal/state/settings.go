package state

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type SettingsSummary struct {
	Language                 string   `json:"language"`
	CommandPrefix            string   `json:"command_prefix"`
	HiddenCommands           []string `json:"hidden_commands"`
	SelectionHighlightPreset string   `json:"selection_highlight_preset"`
	SelectionHighlightFill   bool     `json:"selection_highlight_fill"`
	SelectionHighlightColor  string   `json:"selection_highlight_color"`
	ListPrimaryColor         string   `json:"list_primary_color"`
	ListSecondaryColor       string   `json:"list_secondary_color"`
	ReplyTextColor           string   `json:"reply_text_color"`
	CommandTextColor         string   `json:"command_text_color"`
	ReasoningTextColor       string   `json:"reasoning_text_color"`
	CommandOutputTextColor   string   `json:"command_output_text_color"`
	ModelPresetsEnabled      bool     `json:"model_presets_enabled"`
	ModelPresetProviders     []string `json:"model_preset_providers,omitempty"`
	ModelPresetCount         int      `json:"model_preset_count"`
}

type Settings struct {
	Path               string                     `json:"-"`
	Language           string                     `json:"-"`
	CommandPrefix      string                     `json:"-"`
	HiddenCommands     []string                   `json:"-"`
	SelectionHighlight SelectionHighlightSettings `json:"-"`
	Colors             SettingsColors             `json:"-"`
	ModelPresets       ModelPresetSettings        `json:"-"`
	Extras             map[string]json.RawMessage `json:"-"`
}

type SelectionHighlightSettings struct {
	Preset string
	Fill   bool
	Color  string
}

type SettingsColors struct {
	ListPrimary   string
	ListSecondary string
	ReplyText     string
	CommandText   string
	ReasoningText string
	CommandOutput string
}

type ModelPresetSettings struct {
	Enabled   bool
	Providers map[string]ProviderPresetSettings
}

type ProviderPresetSettings struct {
	Presets map[string]ModelPresetConfig
}

type ModelPresetConfig struct {
	Name      string `json:"name,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type settingsFile struct {
	Language                 string   `json:"language"`
	CommandPrefix            string   `json:"command_prefix"`
	HiddenCommands           []string `json:"hidden_commands"`
	SelectionHighlightPreset string   `json:"selection_highlight_preset"`
	SelectionHighlightFill   bool     `json:"selection_highlight_fill"`
	SelectionHighlightColor  string   `json:"selection_highlight_color"`
	ListPrimaryColor         string   `json:"list_primary_color"`
	ListSecondaryColor       string   `json:"list_secondary_color"`
	ReplyTextColor           string   `json:"reply_text_color"`
	CommandTextColor         string   `json:"command_text_color"`
	ReasoningTextColor       string   `json:"reasoning_text_color"`
	CommandOutputTextColor   string   `json:"command_output_text_color"`
}

func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}, err
	}

	settings, err := ParseSettings(data)
	if err != nil {
		return Settings{}, err
	}
	settings.Path = path
	return settings, nil
}

func ParseSettings(data []byte) (Settings, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Settings{}, err
	}

	var file settingsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Settings{}, err
	}

	extras := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		extras[key] = cloneRawMessage(value)
	}
	for _, key := range []string{
		"language",
		"command_prefix",
		"hidden_commands",
		"selection_highlight_preset",
		"selection_highlight_fill",
		"selection_highlight_color",
		"list_primary_color",
		"list_secondary_color",
		"reply_text_color",
		"command_text_color",
		"reasoning_text_color",
		"command_output_text_color",
		"model_presets_enabled",
		"model_presets",
	} {
		delete(extras, key)
	}

	modelPresets := ModelPresetSettings{}
	if rawValue, ok := raw["model_presets_enabled"]; ok {
		_ = json.Unmarshal(rawValue, &modelPresets.Enabled)
	}
	if rawValue, ok := raw["model_presets"]; ok {
		var providers map[string]map[string]ModelPresetConfig
		if err := json.Unmarshal(rawValue, &providers); err == nil {
			modelPresets.Providers = make(map[string]ProviderPresetSettings, len(providers))
			for provider, presets := range providers {
				if provider == "" || len(presets) == 0 {
					continue
				}
				cleaned := make(map[string]ModelPresetConfig, len(presets))
				for key, preset := range presets {
					if key == "" {
						continue
					}
					cleaned[key] = preset
				}
				if len(cleaned) == 0 {
					continue
				}
				modelPresets.Providers[provider] = ProviderPresetSettings{Presets: cleaned}
			}
		}
	}

	return Settings{
		Language:       file.Language,
		CommandPrefix:  file.CommandPrefix,
		HiddenCommands: cloneStrings(file.HiddenCommands),
		SelectionHighlight: SelectionHighlightSettings{
			Preset: file.SelectionHighlightPreset,
			Fill:   file.SelectionHighlightFill,
			Color:  file.SelectionHighlightColor,
		},
		Colors: SettingsColors{
			ListPrimary:   file.ListPrimaryColor,
			ListSecondary: file.ListSecondaryColor,
			ReplyText:     file.ReplyTextColor,
			CommandText:   file.CommandTextColor,
			ReasoningText: file.ReasoningTextColor,
			CommandOutput: file.CommandOutputTextColor,
		},
		ModelPresets: modelPresets,
		Extras:       extras,
	}, nil
}

func LoadSettingsSummary(path string) (SettingsSummary, error) {
	settings, err := LoadSettings(path)
	if err != nil {
		return SettingsSummary{}, err
	}
	return settings.Summary(), nil
}

func SaveSettings(path string, settings Settings) error {
	data, err := settings.MarshalIndent()
	if err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	return os.WriteFile(path, data, 0o644)
}

func (s Settings) Clone() Settings {
	extras := make(map[string]json.RawMessage, len(s.Extras))
	for key, value := range s.Extras {
		extras[key] = cloneRawMessage(value)
	}
	return Settings{
		Path:           s.Path,
		Language:       s.Language,
		CommandPrefix:  s.CommandPrefix,
		HiddenCommands: cloneStrings(s.HiddenCommands),
		SelectionHighlight: SelectionHighlightSettings{
			Preset: s.SelectionHighlight.Preset,
			Fill:   s.SelectionHighlight.Fill,
			Color:  s.SelectionHighlight.Color,
		},
		Colors: SettingsColors{
			ListPrimary:   s.Colors.ListPrimary,
			ListSecondary: s.Colors.ListSecondary,
			ReplyText:     s.Colors.ReplyText,
			CommandText:   s.Colors.CommandText,
			ReasoningText: s.Colors.ReasoningText,
			CommandOutput: s.Colors.CommandOutput,
		},
		ModelPresets: s.ModelPresets.Clone(),
		Extras:       extras,
	}
}

func (s Settings) Summary() SettingsSummary {
	return SettingsSummary{
		Language:                 s.Language,
		CommandPrefix:            s.CommandPrefix,
		HiddenCommands:           cloneStrings(s.HiddenCommands),
		SelectionHighlightPreset: s.SelectionHighlight.Preset,
		SelectionHighlightFill:   s.SelectionHighlight.Fill,
		SelectionHighlightColor:  s.SelectionHighlight.Color,
		ListPrimaryColor:         s.Colors.ListPrimary,
		ListSecondaryColor:       s.Colors.ListSecondary,
		ReplyTextColor:           s.Colors.ReplyText,
		CommandTextColor:         s.Colors.CommandText,
		ReasoningTextColor:       s.Colors.ReasoningText,
		CommandOutputTextColor:   s.Colors.CommandOutput,
		ModelPresetsEnabled:      s.ModelPresets.Enabled,
		ModelPresetProviders:     s.ModelPresets.ProviderNames(),
		ModelPresetCount:         s.ModelPresets.Count(),
	}
}

func (s Settings) HasHiddenCommand(name string) bool {
	for _, hidden := range s.HiddenCommands {
		if hidden == name {
			return true
		}
	}
	return false
}

func (s *Settings) SetLanguage(value string) {
	s.Language = value
}

func (s *Settings) SetCommandPrefix(value string) {
	s.CommandPrefix = value
}

func (s *Settings) HideCommand(name string) {
	if s.HasHiddenCommand(name) {
		return
	}
	s.HiddenCommands = append(s.HiddenCommands, name)
}

func (s *Settings) ShowCommand(name string) {
	filtered := s.HiddenCommands[:0]
	for _, hidden := range s.HiddenCommands {
		if hidden != name {
			filtered = append(filtered, hidden)
		}
	}
	s.HiddenCommands = filtered
}

func (s *Settings) SetModelPresetsEnabled(value bool) {
	s.ModelPresets.Enabled = value
}

func (s Settings) ModelPreset(provider, key string) (ModelPresetConfig, bool) {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" || key == "" {
		return ModelPresetConfig{}, false
	}
	providerPresets, ok := s.ModelPresets.Providers[provider]
	if !ok {
		return ModelPresetConfig{}, false
	}
	preset, ok := providerPresets.Presets[key]
	return preset, ok
}

func (s *Settings) SetModelPreset(provider, key string, preset ModelPresetConfig) {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" || key == "" {
		return
	}
	s.ModelPresets.Enabled = true
	if s.ModelPresets.Providers == nil {
		s.ModelPresets.Providers = make(map[string]ProviderPresetSettings)
	}
	providerPresets := s.ModelPresets.Providers[provider]
	if providerPresets.Presets == nil {
		providerPresets.Presets = make(map[string]ModelPresetConfig)
	}
	providerPresets.Presets[key] = preset
	s.ModelPresets.Providers[provider] = providerPresets
}

func (s *Settings) DeleteModelPreset(provider, key string) {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" || key == "" {
		return
	}
	providerPresets, ok := s.ModelPresets.Providers[provider]
	if !ok || len(providerPresets.Presets) == 0 {
		return
	}
	delete(providerPresets.Presets, key)
	if len(providerPresets.Presets) == 0 {
		delete(s.ModelPresets.Providers, provider)
		return
	}
	s.ModelPresets.Providers[provider] = providerPresets
}

func (s Settings) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.marshalMap())
}

func (s Settings) MarshalIndent() ([]byte, error) {
	return json.MarshalIndent(s.marshalMap(), "", "  ")
}

func (s Settings) marshalMap() map[string]any {
	result := make(map[string]any, len(s.Extras)+12)
	for key, value := range s.Extras {
		result[key] = cloneRawMessage(value)
	}
	result["language"] = s.Language
	result["command_prefix"] = s.CommandPrefix
	result["hidden_commands"] = cloneStrings(s.HiddenCommands)
	result["selection_highlight_preset"] = s.SelectionHighlight.Preset
	result["selection_highlight_fill"] = s.SelectionHighlight.Fill
	result["selection_highlight_color"] = s.SelectionHighlight.Color
	result["list_primary_color"] = s.Colors.ListPrimary
	result["list_secondary_color"] = s.Colors.ListSecondary
	result["reply_text_color"] = s.Colors.ReplyText
	result["command_text_color"] = s.Colors.CommandText
	result["reasoning_text_color"] = s.Colors.ReasoningText
	result["command_output_text_color"] = s.Colors.CommandOutput
	result["model_presets_enabled"] = s.ModelPresets.Enabled
	result["model_presets"] = s.ModelPresets.marshalMap()
	return result
}

func (s ModelPresetSettings) Clone() ModelPresetSettings {
	result := ModelPresetSettings{
		Enabled: s.Enabled,
	}
	if len(s.Providers) == 0 {
		return result
	}
	result.Providers = make(map[string]ProviderPresetSettings, len(s.Providers))
	for provider, presets := range s.Providers {
		result.Providers[provider] = presets.Clone()
	}
	return result
}

func (s ModelPresetSettings) Count() int {
	total := 0
	for _, provider := range s.Providers {
		total += len(provider.Presets)
	}
	return total
}

func (s ModelPresetSettings) ProviderNames() []string {
	if len(s.Providers) == 0 {
		return nil
	}
	result := make([]string, 0, len(s.Providers))
	for provider := range s.Providers {
		result = append(result, provider)
	}
	sort.Strings(result)
	return result
}

func (s ModelPresetSettings) marshalMap() map[string]any {
	result := make(map[string]any, len(s.Providers))
	for _, provider := range s.ProviderNames() {
		providerPresets := s.Providers[provider]
		result[provider] = providerPresets.marshalMap()
	}
	return result
}

func (p ProviderPresetSettings) Clone() ProviderPresetSettings {
	if len(p.Presets) == 0 {
		return ProviderPresetSettings{}
	}
	result := ProviderPresetSettings{Presets: make(map[string]ModelPresetConfig, len(p.Presets))}
	for key, preset := range p.Presets {
		result.Presets[key] = preset
	}
	return result
}

func (p ProviderPresetSettings) marshalMap() map[string]any {
	result := make(map[string]any, len(p.Presets))
	keys := make([]string, 0, len(p.Presets))
	for key := range p.Presets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result[key] = p.Presets[key]
	}
	return result
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	result := make(json.RawMessage, len(value))
	copy(result, value)
	return result
}

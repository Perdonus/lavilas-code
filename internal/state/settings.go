package state

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/tooling"
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
	ToolApprovalMode         string   `json:"tool_approval_mode"`
	AllowedToolsCount        int      `json:"allowed_tools_count"`
	BlockedToolsCount        int      `json:"blocked_tools_count"`
	BlockMutatingTools       bool     `json:"block_mutating_tools"`
	BlockShellCommands       bool     `json:"block_shell_commands"`
	ToolParallelEnabled      bool     `json:"tool_parallel_enabled"`
	ToolParallelism          int      `json:"tool_parallelism"`
}

type Settings struct {
	Path               string                     `json:"-"`
	Language           string                     `json:"-"`
	CommandPrefix      string                     `json:"-"`
	HiddenCommands     []string                   `json:"-"`
	SelectionHighlight SelectionHighlightSettings `json:"-"`
	Colors             SettingsColors             `json:"-"`
	TextFormats        SettingsTextFormats        `json:"-"`
	ModelPresets       ModelPresetSettings        `json:"-"`
	ToolPolicy         tooling.ToolPolicy         `json:"-"`
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

type SettingsTextFormats struct {
	SelectionHighlight TextFormats
	ListPrimary        TextFormats
	ListSecondary      TextFormats
	Reply              TextFormats
	Reasoning          TextFormats
	Command            TextFormats
	CommandOutput      TextFormats
}

type TextFormats struct {
	Bold        bool
	Italic      bool
	Underlined  bool
	CrossedOut  bool
}

func (f TextFormats) IsEmpty() bool {
	return !f.Underlined && !f.CrossedOut
}

func (f TextFormats) Contains(code string) bool {
	switch normalizeTextFormatCode(code) {
	case "bold":
		return f.Bold
	case "italic":
		return f.Italic
	case "underlined":
		return f.Underlined
	case "crossed_out":
		return f.CrossedOut
	default:
		return false
	}
}

func (f TextFormats) Toggle(code string) TextFormats {
	switch normalizeTextFormatCode(code) {
	case "bold":
		f.Bold = !f.Bold
	case "italic":
		f.Italic = !f.Italic
	case "underlined":
		f.Underlined = !f.Underlined
	case "crossed_out":
		f.CrossedOut = !f.CrossedOut
	}
	return f
}

func (f TextFormats) SettingValue() []string {
	values := make([]string, 0, 2)
	if f.Underlined {
		values = append(values, "underlined")
	}
	if f.CrossedOut {
		values = append(values, "crossed_out")
	}
	return values
}

type ModelPresetSettings struct {
	Enabled   bool
	Providers map[string]ProviderPresetSettings
}

type ProviderPresetSettings struct {
	Order   []string
	Presets map[string]ModelPresetConfig
}

type ModelPresetConfig struct {
	Name      string `json:"name,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type storedModelPresetJSON struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
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
	ToolPolicy               tooling.ToolPolicy `json:"tool_policy"`
}

func DefaultSettings() Settings {
	return Settings{
		Language:      "ru",
		CommandPrefix: ".",
		SelectionHighlight: SelectionHighlightSettings{
			Preset: "light",
			Fill:   true,
			Color:  "light",
		},
		ModelPresets: ModelPresetSettings{Enabled: false},
		ToolPolicy:   cloneToolPolicy(tooling.DefaultToolPolicy()),
		Extras:       map[string]json.RawMessage{},
	}
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
		"selection_highlight_text_formats",
		"list_text_formats",
		"list_primary_text_formats",
		"list_secondary_text_formats",
		"reply_text_formats",
		"reasoning_text_formats",
		"command_text_formats",
		"command_output_text_formats",
		"tool_policy",
		"model_presets_enabled",
		"model_presets",
	} {
		delete(extras, key)
	}

	selectionHighlightPreset := "light"
	if rawValue, ok := raw["selection_highlight_preset"]; ok {
		var value string
		if err := json.Unmarshal(rawValue, &value); err == nil {
			if normalized := normalizeSelectionPreset(value); normalized != "" {
				selectionHighlightPreset = normalized
			}
		}
	}

	selectionHighlightFill := true
	if rawValue, ok := raw["selection_highlight_fill"]; ok {
		_ = json.Unmarshal(rawValue, &selectionHighlightFill)
	}

	selectionHighlightColor := selectionHighlightPreset
	if rawValue, ok := raw["selection_highlight_color"]; ok {
		_ = json.Unmarshal(rawValue, &selectionHighlightColor)
	}

	legacyListFormats := parseTextFormatsRaw(raw["list_text_formats"])
	listPrimaryFormats := parseTextFormatsRaw(raw["list_primary_text_formats"])
	if listPrimaryFormats.IsEmpty() {
		listPrimaryFormats = legacyListFormats
	}
	listSecondaryFormats := parseTextFormatsRaw(raw["list_secondary_text_formats"])
	if listSecondaryFormats.IsEmpty() {
		listSecondaryFormats = legacyListFormats
	}

	modelPresets := ModelPresetSettings{Enabled: false}
	if rawValue, ok := raw["model_presets"]; ok {
		modelPresets = parseModelPresets(rawValue)
	} else {
		if rawValue, ok := raw["model_presets_enabled"]; ok {
			_ = json.Unmarshal(rawValue, &modelPresets.Enabled)
		}
		if rawValue, ok := raw["model_presets"]; ok {
			modelPresets = parseLegacyModelPresets(rawValue, modelPresets.Enabled)
		}
	}

	settings := DefaultSettings()
	settings.Language = firstNonEmptyTrimmed(file.Language, settings.Language)
	settings.CommandPrefix = firstNonEmptyTrimmed(file.CommandPrefix, settings.CommandPrefix)
	settings.HiddenCommands = cloneStrings(file.HiddenCommands)
	settings.SelectionHighlight = SelectionHighlightSettings{
		Preset: selectionHighlightPreset,
		Fill:   selectionHighlightFill,
		Color:  firstNonEmptyTrimmed(selectionHighlightColor, selectionHighlightPreset),
	}
	settings.Colors = SettingsColors{
		ListPrimary:   file.ListPrimaryColor,
		ListSecondary: file.ListSecondaryColor,
		ReplyText:     file.ReplyTextColor,
		CommandText:   file.CommandTextColor,
		ReasoningText: file.ReasoningTextColor,
		CommandOutput: file.CommandOutputTextColor,
	}
	settings.TextFormats = SettingsTextFormats{
		SelectionHighlight: parseTextFormatsRaw(raw["selection_highlight_text_formats"]),
		ListPrimary:        listPrimaryFormats,
		ListSecondary:      listSecondaryFormats,
		Reply:              parseTextFormatsRaw(raw["reply_text_formats"]),
		Reasoning:          parseTextFormatsRaw(raw["reasoning_text_formats"]),
		Command:            parseTextFormatsRaw(raw["command_text_formats"]),
		CommandOutput:      parseTextFormatsRaw(raw["command_output_text_formats"]),
	}
	settings.ModelPresets = modelPresets
	settings.ToolPolicy = cloneToolPolicy(file.ToolPolicy)
	settings.Extras = extras
	return settings, nil
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
		TextFormats: SettingsTextFormats{
			SelectionHighlight: s.TextFormats.SelectionHighlight,
			ListPrimary:        s.TextFormats.ListPrimary,
			ListSecondary:      s.TextFormats.ListSecondary,
			Reply:              s.TextFormats.Reply,
			Reasoning:          s.TextFormats.Reasoning,
			Command:            s.TextFormats.Command,
			CommandOutput:      s.TextFormats.CommandOutput,
		},
		ModelPresets: s.ModelPresets.Clone(),
		ToolPolicy:   cloneToolPolicy(s.ToolPolicy),
		Extras:       extras,
	}
}

func (s Settings) Summary() SettingsSummary {
	policy := tooling.NormalizeToolPolicy(s.ToolPolicy)
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
		ToolApprovalMode:         string(policy.ApprovalMode),
		AllowedToolsCount:        len(policy.AllowedTools),
		BlockedToolsCount:        len(policy.BlockedTools),
		BlockMutatingTools:       policy.BlockMutatingTools,
		BlockShellCommands:       policy.BlockShellCommands,
		ToolParallelEnabled:      policy.Planning.AllowParallel,
		ToolParallelism:          policy.Planning.MaxParallelCalls,
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

func (s *Settings) SetToolPolicy(policy tooling.ToolPolicy) {
	s.ToolPolicy = cloneToolPolicy(policy)
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
	if s.ModelPresets.Providers == nil {
		s.ModelPresets.Providers = make(map[string]ProviderPresetSettings)
	}
	providerPresets := s.ModelPresets.Providers[provider]
	if providerPresets.Presets == nil {
		providerPresets.Presets = make(map[string]ModelPresetConfig)
	}
	if _, exists := providerPresets.Presets[key]; !exists {
		providerPresets.Order = append(providerPresets.Order, key)
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
	providerPresets.Order = removeString(providerPresets.Order, key)
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
	result["selection_highlight_text_formats"] = s.TextFormats.SelectionHighlight.SettingValue()
	result["list_primary_text_formats"] = s.TextFormats.ListPrimary.SettingValue()
	result["list_secondary_text_formats"] = s.TextFormats.ListSecondary.SettingValue()
	result["reply_text_formats"] = s.TextFormats.Reply.SettingValue()
	result["reasoning_text_formats"] = s.TextFormats.Reasoning.SettingValue()
	result["command_text_formats"] = s.TextFormats.Command.SettingValue()
	result["command_output_text_formats"] = s.TextFormats.CommandOutput.SettingValue()
	result["tool_policy"] = tooling.NormalizeToolPolicy(s.ToolPolicy)
	result["model_presets"] = map[string]any{
		"enabled":   s.ModelPresets.Enabled,
		"providers": s.ModelPresets.marshalMap(),
	}
	return result
}

func normalizeSelectionPreset(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "graphite":
		return "graphite"
	case "amber":
		return "amber"
	case "mint":
		return "mint"
	case "rose":
		return "rose"
	default:
		return "light"
	}
}

func normalizeTextFormatCode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "bold":
		return "bold"
	case "italic":
		return "italic"
	case "underlined":
		return "underlined"
	case "crossed_out", "crossedout":
		return "crossed_out"
	default:
		return ""
	}
}

func parseTextFormatsRaw(raw json.RawMessage) TextFormats {
	if len(raw) == 0 {
		return TextFormats{}
	}

	var array []string
	if err := json.Unmarshal(raw, &array); err == nil {
		return parseTextFormatsStrings(array)
	}

	var object map[string]bool
	if err := json.Unmarshal(raw, &object); err == nil {
		var result TextFormats
		for key, enabled := range object {
			if !enabled {
				continue
			}
			result = result.Toggle(key)
		}
		return result
	}

	return TextFormats{}
}

func parseTextFormatsStrings(values []string) TextFormats {
	var result TextFormats
	for _, value := range values {
		result = result.Toggle(value)
	}
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
		return ProviderPresetSettings{Order: cloneStrings(p.Order)}
	}
	result := ProviderPresetSettings{
		Order:   cloneStrings(p.Order),
		Presets: make(map[string]ModelPresetConfig, len(p.Presets)),
	}
	for key, preset := range p.Presets {
		result.Presets[key] = preset
	}
	return result
}

func (p ProviderPresetSettings) marshalMap() []map[string]any {
	keys := orderedPresetKeys(p)
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		preset, ok := p.Presets[key]
		if !ok {
			continue
		}
		result = append(result, map[string]any{
			"id":    key,
			"name":  preset.Name,
			"model": preset.Model,
		})
	}
	return result
}

func parseModelPresets(rawValue json.RawMessage) ModelPresetSettings {
	type nestedModelPresets struct {
		Enabled   *bool                      `json:"enabled"`
		Providers map[string]json.RawMessage `json:"providers"`
	}
	settings := ModelPresetSettings{Enabled: false}
	var nested nestedModelPresets
	if err := json.Unmarshal(rawValue, &nested); err != nil {
		return parseLegacyModelPresets(rawValue, settings.Enabled)
	}
	if nested.Enabled != nil {
		settings.Enabled = *nested.Enabled
	}
	if len(nested.Providers) == 0 {
		return settings
	}
	settings.Providers = make(map[string]ProviderPresetSettings, len(nested.Providers))
	for provider, value := range nested.Providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		var stored []storedModelPresetJSON
		if err := json.Unmarshal(value, &stored); err == nil {
			settings.Providers[provider] = providerPresetSettingsFromStored(stored)
			continue
		}
		settings.Providers[provider] = parseLegacyProviderPresets(value)
	}
	return settings
}

func parseLegacyModelPresets(rawValue json.RawMessage, enabled bool) ModelPresetSettings {
	var providers map[string]map[string]ModelPresetConfig
	settings := ModelPresetSettings{Enabled: enabled}
	if err := json.Unmarshal(rawValue, &providers); err != nil {
		return settings
	}
	if len(providers) == 0 {
		return settings
	}
	settings.Providers = make(map[string]ProviderPresetSettings, len(providers))
	for provider, presets := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		current := ProviderPresetSettings{Presets: make(map[string]ModelPresetConfig, len(presets))}
		keys := make([]string, 0, len(presets))
		for key, preset := range presets {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			current.Presets[key] = preset
			keys = append(keys, key)
		}
		sort.Strings(keys)
		current.Order = append(current.Order, keys...)
		settings.Providers[provider] = current
	}
	return settings
}

func parseLegacyProviderPresets(rawValue json.RawMessage) ProviderPresetSettings {
	var presets map[string]ModelPresetConfig
	if err := json.Unmarshal(rawValue, &presets); err != nil {
		return ProviderPresetSettings{}
	}
	result := ProviderPresetSettings{Presets: make(map[string]ModelPresetConfig, len(presets))}
	keys := make([]string, 0, len(presets))
	for key, preset := range presets {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		result.Presets[key] = preset
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result.Order = append(result.Order, keys...)
	return result
}

func providerPresetSettingsFromStored(stored []storedModelPresetJSON) ProviderPresetSettings {
	result := ProviderPresetSettings{Presets: make(map[string]ModelPresetConfig, len(stored))}
	for _, preset := range stored {
		key := strings.TrimSpace(preset.ID)
		if key == "" {
			continue
		}
		if _, exists := result.Presets[key]; exists {
			continue
		}
		result.Order = append(result.Order, key)
		result.Presets[key] = ModelPresetConfig{
			Name:  strings.TrimSpace(preset.Name),
			Model: strings.TrimSpace(preset.Model),
		}
	}
	return result
}

func cloneToolPolicy(policy tooling.ToolPolicy) tooling.ToolPolicy {
	return tooling.ToolPolicy{
		Planning:           policy.Planning,
		ApprovalMode:       policy.ApprovalMode,
		AllowedTools:       cloneStrings(policy.AllowedTools),
		BlockedTools:       cloneStrings(policy.BlockedTools),
		BlockMutatingTools: policy.BlockMutatingTools,
		BlockShellCommands: policy.BlockShellCommands,
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func orderedPresetKeys(p ProviderPresetSettings) []string {
	if len(p.Presets) == 0 {
		return cloneStrings(p.Order)
	}
	keys := make([]string, 0, len(p.Presets))
	seen := make(map[string]struct{}, len(p.Presets))
	for _, key := range p.Order {
		if _, ok := p.Presets[key]; !ok {
			continue
		}
		keys = append(keys, key)
		seen[key] = struct{}{}
	}
	extra := make([]string, 0, len(p.Presets))
	for key := range p.Presets {
		if _, ok := seen[key]; ok {
			continue
		}
		extra = append(extra, key)
	}
	sort.Strings(extra)
	keys = append(keys, extra...)
	return keys
}

func removeString(values []string, target string) []string {
	if len(values) == 0 {
		return nil
	}
	filtered := values[:0]
	for _, value := range values {
		if value != target {
			filtered = append(filtered, value)
		}
	}
	return filtered
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

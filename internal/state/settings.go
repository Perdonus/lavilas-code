package state

import (
	"encoding/json"
	"os"
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
}

type Settings struct {
	Path               string                     `json:"-"`
	Language           string                     `json:"-"`
	CommandPrefix      string                     `json:"-"`
	HiddenCommands     []string                   `json:"-"`
	SelectionHighlight SelectionHighlightSettings `json:"-"`
	Colors             SettingsColors             `json:"-"`
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
	} {
		delete(extras, key)
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
		Extras: extras,
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
		Extras: extras,
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

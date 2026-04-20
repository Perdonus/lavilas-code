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

func LoadSettingsSummary(path string) (SettingsSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SettingsSummary{}, err
	}

	var result SettingsSummary
	if err := json.Unmarshal(data, &result); err != nil {
		return SettingsSummary{}, err
	}
	return result, nil
}

package tui

import (
	"strings"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	runtimeapi "github.com/Perdonus/lavilas-code/internal/runtime"
)

type PaneFocus string

type PaletteMode string
type ModelSettingsNavigationOrigin string

const (
	FocusStatus     PaneFocus = "status"
	FocusTranscript PaneFocus = "transcript"
	FocusInput      PaneFocus = "input"
	FocusPalette    PaneFocus = "palette"
)

const (
	ModelSettingsNavigationOriginCommand  ModelSettingsNavigationOrigin = "command"
	ModelSettingsNavigationOriginSettings ModelSettingsNavigationOrigin = "settings"
)

const (
	PaletteModeRoot            PaletteMode = "root"
	PaletteModeResume          PaletteMode = "resume"
	PaletteModeFork            PaletteMode = "fork"
	PaletteModeStatus          PaletteMode = "status"
	PaletteModeModel           PaletteMode = "model"
	PaletteModeModelSettings   PaletteMode = "model_settings"
	PaletteModeModelCatalog    PaletteMode = "model_catalog"
	PaletteModeReasoning       PaletteMode = "reasoning"
	PaletteModeProfiles        PaletteMode = "profiles"
	PaletteModeProfileActions  PaletteMode = "profile_actions"
	PaletteModeAddAccount      PaletteMode = "add_account"
	PaletteModeProviders       PaletteMode = "providers"
	PaletteModeProviderActions PaletteMode = "provider_actions"
	PaletteModeModelPresets    PaletteMode = "model_presets"
	PaletteModePresetEditor    PaletteMode = "preset_editor"
	PaletteModePresetActions   PaletteMode = "preset_actions"
	PaletteModePresetModels    PaletteMode = "preset_models"
	PaletteModeSettings        PaletteMode = "settings"
	PaletteModeCustomization   PaletteMode = "customization"
	PaletteModeLanguage        PaletteMode = "language"
	PaletteModeCommandPrefix   PaletteMode = "command_prefix"
	PaletteModePopupCommands   PaletteMode = "popup_commands"
	PaletteModePermissions     PaletteMode = "permissions"
)

type State struct {
	Language    string
	Title       string
	Status      []StatusItem
	Transcript  []TranscriptEntry
	LiveTurn    *LiveTurnState
	InputDraft  string
	Palette     PaletteState
	Focus       PaneFocus
	Footer      string
	Busy        bool
	SessionPath string
	CWD         string
	Model       string
	Provider    string
	Profile     string
	Reasoning   string
}

type StatusItem struct {
	Label string
	Value string
}

type TranscriptEntry struct {
	Role string
	Body string
}

type LiveTurnState struct {
	Prompt        string
	Round         int
	AssistantText string
	ToolCalls     []runtimeapi.ToolCall
	Notes         []string
}

type PaletteState struct {
	Visible       bool
	Mode          PaletteMode
	Query         string
	Items         []PaletteItem
	Selected      int
	Context       PaletteContext
	SelectedToken string
	Stack         []PaletteView
}

type PaletteView struct {
	Mode          PaletteMode
	Query         string
	Items         []PaletteItem
	Selected      int
	Context       PaletteContext
	SelectedToken string
	Footer        string
}

type PaletteContext struct {
	BackTitle       string
	BackDescription string
	BackHint        string
	ReturnFocus     PaneFocus
}

type PaletteItem struct {
	Key         string
	Title       string
	Subtitle    string
	Description string
	Meta        string
	Value       string
	Aliases     []string
	Keywords    []string
}

func DefaultState() State {
	language := commandcatalog.CatalogLanguageEnglish
	return State{
		Language: "en",
		Title:    "Go Lavilas",
		Status: []StatusItem{
			{Label: "Mode", Value: "alpha"},
			{Label: "Session", Value: "fresh"},
		},
		Transcript: nil,
		Palette: PaletteState{
			Mode:    PaletteModeRoot,
			Items:   defaultPaletteItemsForLanguage(language),
			Context: defaultPaletteContextForLanguage(language),
		},
		Focus:  FocusInput,
		Footer: "",
	}
}

func defaultPaletteItems() []PaletteItem {
	return defaultPaletteItemsForLanguage(commandcatalog.CatalogLanguageEnglish)
}

func defaultPaletteItemsForLanguage(language commandcatalog.CatalogLanguage) []PaletteItem {
	return defaultPaletteCatalog().RootItems(language, "")
}

func (s State) clone() State {
	language := normalizeTUILanguage(s.Language)
	cloned := State{
		Language:    strings.TrimSpace(s.Language),
		Title:       strings.TrimSpace(s.Title),
		Status:      cloneStatusItems(s.Status),
		Transcript:  cloneTranscriptEntries(s.Transcript),
		LiveTurn:    cloneLiveTurnState(s.LiveTurn),
		InputDraft:  s.InputDraft,
		Focus:       normalizeFocus(s.Focus),
		Footer:      s.Footer,
		Busy:        s.Busy,
		SessionPath: s.SessionPath,
		CWD:         s.CWD,
		Model:       s.Model,
		Provider:    s.Provider,
		Profile:     s.Profile,
		Reasoning:   s.Reasoning,
		Palette: PaletteState{
			Visible:       s.Palette.Visible,
			Mode:          normalizePaletteMode(s.Palette.Mode),
			Query:         s.Palette.Query,
			Items:         clonePaletteItems(s.Palette.Items),
			Selected:      s.Palette.Selected,
			Context:       normalizePaletteContextForLanguage(s.Palette.Context, language),
			SelectedToken: s.Palette.SelectedToken,
			Stack:         clonePaletteViews(s.Palette.Stack),
		},
	}

	if cloned.Title == "" {
		cloned.Title = DefaultState().Title
	}
	if cloned.Language == "" {
		cloned.Language = DefaultState().Language
	}
	language = normalizeTUILanguage(cloned.Language)
	if len(cloned.Palette.Items) == 0 {
		cloned.Palette.Items = defaultPaletteItemsForLanguage(language)
	}
	return cloned
}

func defaultPaletteContext() PaletteContext {
	return defaultPaletteContextForLanguage(commandcatalog.CatalogLanguageEnglish)
}

func defaultPaletteContextForLanguage(language commandcatalog.CatalogLanguage) PaletteContext {
	localize := func(english string, russian string) string {
		if normalizeTUILanguage(string(language)) == commandcatalog.CatalogLanguageRussian {
			return russian
		}
		return english
	}
	return PaletteContext{
		BackTitle:       localize("Back to Chat", "Назад в чат"),
		BackDescription: localize("Return to transcript", "Вернуться к диалогу"),
		BackHint:        localize("Enter select · Esc close", "Enter выбрать · Esc закрыть"),
		ReturnFocus:     FocusInput,
	}
}

func normalizeFocus(value PaneFocus) PaneFocus {
	switch value {
	case FocusStatus, FocusTranscript, FocusInput, FocusPalette:
		return value
	default:
		return FocusInput
	}
}

func normalizePaletteMode(value PaletteMode) PaletteMode {
	switch value {
	case PaletteModeRoot,
		PaletteModeResume,
		PaletteModeFork,
		PaletteModeStatus,
		PaletteModeModel,
		PaletteModeModelSettings,
		PaletteModeModelCatalog,
		PaletteModeReasoning,
		PaletteModeProfiles,
		PaletteModeProfileActions,
		PaletteModeAddAccount,
		PaletteModeProviders,
		PaletteModeProviderActions,
		PaletteModeModelPresets,
		PaletteModePresetEditor,
		PaletteModePresetActions,
		PaletteModePresetModels,
		PaletteModeSettings,
		PaletteModeCustomization,
		PaletteModeLanguage,
		PaletteModeCommandPrefix,
		PaletteModePopupCommands,
		PaletteModePermissions:
		return value
	default:
		return PaletteModeRoot
	}
}

func normalizePaletteContext(value PaletteContext) PaletteContext {
	return normalizePaletteContextForLanguage(value, commandcatalog.CatalogLanguageEnglish)
}

func normalizePaletteContextForLanguage(value PaletteContext, language commandcatalog.CatalogLanguage) PaletteContext {
	defaults := defaultPaletteContextForLanguage(language)
	normalized := PaletteContext{
		BackTitle:       strings.TrimSpace(value.BackTitle),
		BackDescription: strings.TrimSpace(value.BackDescription),
		BackHint:        strings.TrimSpace(value.BackHint),
		ReturnFocus:     normalizeFocus(value.ReturnFocus),
	}
	if normalized.BackTitle == "" {
		normalized.BackTitle = defaults.BackTitle
	}
	if normalized.BackDescription == "" {
		normalized.BackDescription = defaults.BackDescription
	}
	if normalized.BackHint == "" {
		normalized.BackHint = defaults.BackHint
	}
	if normalized.ReturnFocus == FocusPalette {
		normalized.ReturnFocus = FocusInput
	}
	return normalized
}

func cloneStatusItems(items []StatusItem) []StatusItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]StatusItem, len(items))
	copy(cloned, items)
	return cloned
}

func cloneTranscriptEntries(items []TranscriptEntry) []TranscriptEntry {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]TranscriptEntry, len(items))
	copy(cloned, items)
	return cloned
}

func clonePaletteItems(items []PaletteItem) []PaletteItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]PaletteItem, len(items))
	for index, item := range items {
		cloned[index] = PaletteItem{
			Key:         item.Key,
			Title:       item.Title,
			Subtitle:    item.Subtitle,
			Description: item.Description,
			Meta:        item.Meta,
			Value:       item.Value,
			Aliases:     cloneStrings(item.Aliases),
			Keywords:    cloneStrings(item.Keywords),
		}
	}
	return cloned
}

func clonePaletteViews(items []PaletteView) []PaletteView {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]PaletteView, len(items))
	for index, item := range items {
		cloned[index] = PaletteView{
			Mode:          normalizePaletteMode(item.Mode),
			Query:         item.Query,
			Items:         clonePaletteItems(item.Items),
			Selected:      item.Selected,
			Context:       normalizePaletteContext(item.Context),
			SelectedToken: item.SelectedToken,
			Footer:        item.Footer,
		}
	}
	return cloned
}

func cloneLiveTurnState(value *LiveTurnState) *LiveTurnState {
	if value == nil {
		return nil
	}
	cloned := &LiveTurnState{
		Prompt:        value.Prompt,
		Round:         value.Round,
		AssistantText: value.AssistantText,
		ToolCalls:     cloneRuntimeToolCalls(value.ToolCalls),
		Notes:         cloneStrings(value.Notes),
	}
	return cloned
}

func cloneRuntimeToolCalls(calls []runtimeapi.ToolCall) []runtimeapi.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	cloned := make([]runtimeapi.ToolCall, len(calls))
	for index, call := range calls {
		cloned[index] = call
		if len(call.Function.Arguments) > 0 {
			args := make([]byte, len(call.Function.Arguments))
			copy(args, call.Function.Arguments)
			cloned[index].Function.Arguments = args
		}
	}
	return cloned
}

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

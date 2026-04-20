package tui

import "strings"

type PaneFocus string

type PaletteMode string

const (
	FocusStatus     PaneFocus = "status"
	FocusTranscript PaneFocus = "transcript"
	FocusInput      PaneFocus = "input"
	FocusPalette    PaneFocus = "palette"
)

const (
	PaletteModeRoot      PaletteMode = "root"
	PaletteModeResume    PaletteMode = "resume"
	PaletteModeFork      PaletteMode = "fork"
	PaletteModeModel     PaletteMode = "model"
	PaletteModeProfiles  PaletteMode = "profiles"
	PaletteModeProviders PaletteMode = "providers"
	PaletteModeSettings  PaletteMode = "settings"
)

type State struct {
	Title       string
	Status      []StatusItem
	Transcript  []TranscriptEntry
	InputDraft  string
	Palette     PaletteState
	Focus       PaneFocus
	Footer      string
	Busy        bool
	SessionPath string
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
	Description string
	Value       string
	Aliases     []string
	Keywords    []string
}

func DefaultState() State {
	return State{
		Title: "Go Lavilas",
		Status: []StatusItem{
			{Label: "Mode", Value: "alpha"},
			{Label: "Session", Value: "fresh"},
		},
		Transcript: []TranscriptEntry{
			{Role: "system", Body: "Go Lavilas alpha TUI loaded."},
			{Role: "assistant", Body: "Type a prompt and press Enter. Ctrl+P opens the command palette."},
		},
		Palette: PaletteState{
			Mode:    PaletteModeRoot,
			Items:   defaultPaletteItems(),
			Context: defaultPaletteContext(),
		},
		Focus:  FocusInput,
		Footer: "Enter submit · Ctrl+P palette · Tab focus · Esc close",
	}
}

func defaultPaletteItems() []PaletteItem {
	return defaultPaletteCatalog().RootItems()
}

func (s State) clone() State {
	cloned := State{
		Title:       strings.TrimSpace(s.Title),
		Status:      cloneStatusItems(s.Status),
		Transcript:  cloneTranscriptEntries(s.Transcript),
		InputDraft:  s.InputDraft,
		Focus:       normalizeFocus(s.Focus),
		Footer:      s.Footer,
		Busy:        s.Busy,
		SessionPath: s.SessionPath,
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
			Context:       normalizePaletteContext(s.Palette.Context),
			SelectedToken: s.Palette.SelectedToken,
			Stack:         clonePaletteViews(s.Palette.Stack),
		},
	}

	if cloned.Title == "" {
		cloned.Title = DefaultState().Title
	}
	if len(cloned.Palette.Items) == 0 {
		cloned.Palette.Items = defaultPaletteItems()
	}
	return cloned
}

func defaultPaletteContext() PaletteContext {
	return PaletteContext{
		BackTitle:       "Back to Chat",
		BackDescription: "Return to transcript",
		BackHint:        "Enter select · Esc close",
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
	case PaletteModeRoot, PaletteModeResume, PaletteModeFork, PaletteModeModel, PaletteModeProfiles, PaletteModeProviders, PaletteModeSettings:
		return value
	default:
		return PaletteModeRoot
	}
}

func normalizePaletteContext(value PaletteContext) PaletteContext {
	normalized := PaletteContext{
		BackTitle:       strings.TrimSpace(value.BackTitle),
		BackDescription: strings.TrimSpace(value.BackDescription),
		BackHint:        strings.TrimSpace(value.BackHint),
		ReturnFocus:     normalizeFocus(value.ReturnFocus),
	}
	if normalized.BackTitle == "" {
		normalized.BackTitle = "Back to Chat"
	}
	if normalized.BackDescription == "" {
		normalized.BackDescription = "Return to transcript"
	}
	if normalized.BackHint == "" {
		normalized.BackHint = "Enter select · Esc close"
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
			Description: item.Description,
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

func cloneStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]string, len(items))
	copy(cloned, items)
	return cloned
}

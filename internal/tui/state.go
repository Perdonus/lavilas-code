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
	Visible  bool
	Mode     PaletteMode
	Query    string
	Items    []PaletteItem
	Selected int
}

type PaletteItem struct {
	Key         string
	Title       string
	Description string
	Value       string
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
			Mode:  PaletteModeRoot,
			Items: defaultPaletteItems(),
		},
		Focus:  FocusInput,
		Footer: "Enter submit · Ctrl+P palette · Tab focus · Esc close",
	}
}

func defaultPaletteItems() []PaletteItem {
	return []PaletteItem{
		{Key: "new", Title: "New Session", Description: "Clear transcript and start fresh"},
		{Key: "resume_latest", Title: "Resume Latest", Description: "Load the latest saved session"},
		{Key: "fork_latest", Title: "Fork Latest", Description: "Load the latest session as a new branch"},
		{Key: "sessions_resume", Title: "Sessions", Description: "Browse saved sessions to resume"},
		{Key: "sessions_fork", Title: "Fork Session", Description: "Browse saved sessions to fork"},
		{Key: "model", Title: "Model", Description: "Inspect active model and reasoning"},
		{Key: "profiles", Title: "Profiles", Description: "Inspect configured profiles"},
		{Key: "providers", Title: "Providers", Description: "Inspect configured providers"},
		{Key: "settings", Title: "Settings", Description: "Inspect saved UI settings"},
		{Key: "status", Title: "Status", Description: "Show runtime status summary"},
		{Key: "help", Title: "Help", Description: "Show keyboard and slash commands"},
	}
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
			Visible:  s.Palette.Visible,
			Mode:     normalizePaletteMode(s.Palette.Mode),
			Query:    s.Palette.Query,
			Items:    clonePaletteItems(s.Palette.Items),
			Selected: s.Palette.Selected,
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
	copy(cloned, items)
	return cloned
}

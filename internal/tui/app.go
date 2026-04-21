package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Perdonus/lavilas-code/internal/taskrun"
)

type Options struct {
	TaskOptions    taskrun.Options
	PaletteCatalog PaletteCommandCatalog
	Startup        StartupOptions
}

type StartupMode string

const (
	StartupModeNone         StartupMode = ""
	StartupModeResumePicker StartupMode = "resume_picker"
	StartupModeForkPicker   StartupMode = "fork_picker"
	StartupModeResumeLatest StartupMode = "resume_latest"
	StartupModeForkLatest   StartupMode = "fork_latest"
	StartupModeResumeSelect StartupMode = "resume_select"
	StartupModeForkSelect   StartupMode = "fork_select"
	StartupModeResumePath   StartupMode = "resume_path"
	StartupModeForkPath     StartupMode = "fork_path"
	StartupModeModel        StartupMode = "model"
	StartupModeModelPresets StartupMode = "model_presets"
	StartupModeProfiles     StartupMode = "profiles"
	StartupModeProviders    StartupMode = "providers"
	StartupModeSettings     StartupMode = "settings"
	StartupModeLanguage     StartupMode = "language"
	StartupModePermissions  StartupMode = "permissions"
)

type StartupOptions struct {
	Mode            StartupMode
	SessionPath     string
	SessionSelector string
	InitialPrompt   string
	ShowAll         bool
	TaskOptions     taskrun.Options
}

func Run(options Options) int {
	model, err := newModel(options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}

	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}

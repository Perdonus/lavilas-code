package tui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Perdonus/lavilas-code/internal/taskrun"
)

type Options struct {
	TaskOptions taskrun.Options
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

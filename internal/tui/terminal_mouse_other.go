//go:build !windows

package tui

import tea "github.com/charmbracelet/bubbletea"

func releaseTerminalMouseCmd() tea.Cmd {
	return nil
}

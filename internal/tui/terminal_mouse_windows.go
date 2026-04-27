//go:build windows

package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/windows"
)

func releaseTerminalMouseCmd() tea.Cmd {
	return func() tea.Msg {
		releaseTerminalMouse()
		return nil
	}
}

func releaseTerminalMouse() {
	handle, err := windows.GetStdHandle(windows.STD_INPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return
	}
	mode &^= windows.ENABLE_MOUSE_INPUT
	mode |= windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_QUICK_EDIT_MODE
	_ = windows.SetConsoleMode(handle, mode)
}

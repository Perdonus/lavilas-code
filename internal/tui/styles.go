package tui

import "github.com/charmbracelet/lipgloss"

type styles struct {
	app           lipgloss.Style
	pane          lipgloss.Style
	paneActive    lipgloss.Style
	paneTitle     lipgloss.Style
	sectionTitle  lipgloss.Style
	label         lipgloss.Style
	value         lipgloss.Style
	muted         lipgloss.Style
	body          lipgloss.Style
	selected      lipgloss.Style
	helpKey       lipgloss.Style
	helpDesc      lipgloss.Style
	roleUser      lipgloss.Style
	roleAssistant lipgloss.Style
	roleSystem    lipgloss.Style
	roleTool      lipgloss.Style
	busy          lipgloss.Style
}

func newStyles() styles {
	const (
		borderColor = "#4A5565"
		activeColor = "#8FB8FF"
		titleColor  = "#F4EBD0"
		textColor   = "#E7ECEF"
		mutedColor  = "#9AA4B2"
		userColor   = "#F6C177"
		assistColor = "#8BD5CA"
		systemColor = "#F28FAD"
		toolColor   = "#B7BDF8"
		selectBG    = "#243042"
		busyColor   = "#FFD166"
	)

	pane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1)

	paneActive := pane.BorderForeground(lipgloss.Color(activeColor))

	return styles{
		app:           lipgloss.NewStyle(),
		pane:          pane,
		paneActive:    paneActive,
		paneTitle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)),
		sectionTitle:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(activeColor)),
		label:         lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		value:         lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		muted:         lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		body:          lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		selected:      lipgloss.NewStyle().Background(lipgloss.Color(selectBG)).Foreground(lipgloss.Color(textColor)).Bold(true),
		helpKey:       lipgloss.NewStyle().Foreground(lipgloss.Color(titleColor)).Bold(true),
		helpDesc:      lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		roleUser:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(userColor)),
		roleAssistant: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(assistColor)),
		roleSystem:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(systemColor)),
		roleTool:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(toolColor)),
		busy:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(busyColor)),
	}
}

func applyPaneFocus(base, active lipgloss.Style, focused bool) lipgloss.Style {
	if focused {
		return active
	}
	return base
}

func innerWidth(style lipgloss.Style, total int) int {
	return maxInt(0, total-style.GetHorizontalFrameSize())
}

func innerHeight(style lipgloss.Style, total int) int {
	return maxInt(0, total-style.GetVerticalFrameSize())
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clampInt(value, low, high int) int {
	if high < low {
		high = low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

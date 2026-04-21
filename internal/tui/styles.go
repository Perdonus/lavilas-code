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
	selectedSecondary lipgloss.Style
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
		borderColor = "8"
		activeColor = "8"
		selectBG    = "15"
		busyColor   = "3"
		mutedColor  = "8"
		textColor   = "7"
		secondaryOnSelection = "8"
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
		paneTitle:     lipgloss.NewStyle().Bold(true),
		sectionTitle:  lipgloss.NewStyle().Bold(true),
		label:         lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		value:         lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		muted:         lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		body:          lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		selected:      lipgloss.NewStyle().Background(lipgloss.Color(selectBG)).Foreground(lipgloss.Color("0")),
		selectedSecondary: lipgloss.NewStyle().Background(lipgloss.Color(selectBG)).Foreground(lipgloss.Color(secondaryOnSelection)),
		helpKey:       lipgloss.NewStyle().Bold(true),
		helpDesc:      lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
		roleUser:      lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		roleAssistant: lipgloss.NewStyle().Foreground(lipgloss.Color(textColor)),
		roleSystem:    lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)).Italic(true),
		roleTool:      lipgloss.NewStyle().Foreground(lipgloss.Color(mutedColor)),
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

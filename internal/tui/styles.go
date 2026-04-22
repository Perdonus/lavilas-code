package tui

import "github.com/charmbracelet/lipgloss"

type styles struct {
	app               lipgloss.Style
	pane              lipgloss.Style
	paneActive        lipgloss.Style
	paneTitle         lipgloss.Style
	sectionTitle      lipgloss.Style
	label             lipgloss.Style
	value             lipgloss.Style
	muted             lipgloss.Style
	body              lipgloss.Style
	selected          lipgloss.Style
	selectedSecondary lipgloss.Style
	helpKey           lipgloss.Style
	helpDesc          lipgloss.Style
	roleUser          lipgloss.Style
	roleAssistant     lipgloss.Style
	roleSystem        lipgloss.Style
	roleTool          lipgloss.Style
	busy              lipgloss.Style
}

type codexThemePalette struct {
	border            string
	borderActive      string
	text              string
	muted             string
	busy              string
	selectionBG       string
	selectionFG       string
	selectionMuted    string
	userMessageBG     string
}

func newStyles() styles {
	ensureTerminalRendererConfigured()
	palette := defaultCodexThemePalette()

	pane := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(palette.border)).
		Padding(0, 1)

	paneActive := pane.BorderForeground(lipgloss.Color(palette.borderActive))

	return styles{
		app:               lipgloss.NewStyle(),
		pane:              pane,
		paneActive:        paneActive,
		paneTitle:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.text)),
		sectionTitle:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.text)),
		label:             lipgloss.NewStyle().Foreground(lipgloss.Color(palette.muted)),
		value:             lipgloss.NewStyle().Foreground(lipgloss.Color(palette.text)),
		muted:             lipgloss.NewStyle().Foreground(lipgloss.Color(palette.muted)),
		body:              lipgloss.NewStyle().Foreground(lipgloss.Color(palette.text)),
		selected:          lipgloss.NewStyle().Background(lipgloss.Color(palette.selectionBG)).Foreground(lipgloss.Color(palette.selectionFG)),
		selectedSecondary: lipgloss.NewStyle().Background(lipgloss.Color(palette.selectionBG)).Foreground(lipgloss.Color(palette.selectionMuted)),
		helpKey:           lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.text)),
		helpDesc:          lipgloss.NewStyle().Foreground(lipgloss.Color(palette.muted)),
		roleUser:          lipgloss.NewStyle().Foreground(lipgloss.Color(palette.text)).Background(lipgloss.Color(palette.userMessageBG)),
		roleAssistant:     lipgloss.NewStyle().Foreground(lipgloss.Color(palette.text)),
		roleSystem:        lipgloss.NewStyle().Foreground(lipgloss.Color(palette.muted)).Italic(true),
		roleTool:          lipgloss.NewStyle().Foreground(lipgloss.Color(palette.muted)),
		busy:              lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(palette.busy)),
	}
}

func defaultCodexThemePalette() codexThemePalette {
	background := defaultTerminalBackgroundRGB()
	foreground := defaultTerminalForegroundRGB()
	muted := rgbHex(visibleTerminalRGB(blendRGB(foreground, background, 0.42)))
	border := rgbHex(visibleTerminalRGB(blendRGB(foreground, background, 0.64)))
	borderActive := rgbHex(visibleTerminalRGB(blendRGB(foreground, background, 0.34)))
	if isLightRGB(background) {
		return codexThemePalette{
			border:         border,
			borderActive:   borderActive,
			text:           rgbHex(foreground),
			muted:          muted,
			busy:           rgbHex(visibleTerminalRGB([3]float64{154, 107, 13})),
			selectionBG:    "#111827",
			selectionFG:    "#ffffff",
			selectionMuted: rgbHex(blendRGB([3]float64{255, 255, 255}, [3]float64{17, 24, 39}, 0.18)),
			userMessageBG:  rgbHex(blendRGB(background, [3]float64{0, 0, 0}, 0.04)),
		}
	}
	return codexThemePalette{
		border:         border,
		borderActive:   borderActive,
		text:           rgbHex(foreground),
		muted:          muted,
		busy:           rgbHex(visibleTerminalRGB([3]float64{227, 198, 120})),
		selectionBG:    "#ffffff",
		selectionFG:    "#000000",
		selectionMuted: rgbHex(blendRGB([3]float64{0, 0, 0}, [3]float64{255, 255, 255}, 0.32)),
		userMessageBG:  rgbHex(blendRGB(background, [3]float64{255, 255, 255}, 0.12)),
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

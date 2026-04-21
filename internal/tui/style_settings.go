package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

func (m *Model) reloadStyleSettings() {
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		m.applyStyleSettings(appstate.Settings{})
		return
	}
	m.applyStyleSettings(settings)
}

func (m *Model) applyStyleSettings(settings appstate.Settings) {
	styles := newStyles()

	if color := normalizeUIColor(settings.Colors.ListPrimary); color != "" {
		styles.value = styles.value.Foreground(lipgloss.Color(color))
		styles.body = styles.body.Foreground(lipgloss.Color(color))
	}
	if color := normalizeUIColor(settings.Colors.ListSecondary); color != "" {
		styles.muted = styles.muted.Foreground(lipgloss.Color(color))
		styles.label = styles.label.Foreground(lipgloss.Color(color))
		styles.helpDesc = styles.helpDesc.Foreground(lipgloss.Color(color))
	}
	if color := normalizeUIColor(settings.Colors.ReplyText); color != "" {
		styles.roleAssistant = styles.roleAssistant.Foreground(lipgloss.Color(color))
	}
	if color := normalizeUIColor(settings.Colors.CommandText); color != "" {
		styles.sectionTitle = styles.sectionTitle.Foreground(lipgloss.Color(color))
		styles.helpKey = styles.helpKey.Foreground(lipgloss.Color(color))
	}
	if color := normalizeUIColor(settings.Colors.ReasoningText); color != "" {
		styles.roleSystem = styles.roleSystem.Foreground(lipgloss.Color(color))
	}
	if color := normalizeUIColor(settings.Colors.CommandOutput); color != "" {
		styles.roleTool = styles.roleTool.Foreground(lipgloss.Color(color))
	}

	selectionColor := normalizeUIColor(settings.SelectionHighlight.Color)
	if selectionColor == "" {
		selectionColor = "#8FB8FF"
	}
	fillEnabled := effectiveSelectionHighlightFill(settings)
	if fillEnabled {
		styles.selected = lipgloss.NewStyle().
			Background(lipgloss.Color(selectionColor)).
			Foreground(lipgloss.Color(contrastForeground(selectionColor))).
			Bold(true)
	} else {
		styles.selected = lipgloss.NewStyle().
			Foreground(lipgloss.Color(selectionColor)).
			Bold(true)
	}

	m.styles = styles
	m.input.TextStyle = styles.value
	m.input.PlaceholderStyle = styles.muted
	m.input.PromptStyle = styles.sectionTitle
	m.input.Cursor.Style = styles.paneTitle
	m.paletteInput.TextStyle = styles.value
	m.paletteInput.PlaceholderStyle = styles.muted
	m.paletteInput.Cursor.Style = styles.paneTitle
}

func normalizeUIColor(value string) string {
	return strings.TrimSpace(value)
}

func effectiveSelectionHighlightFill(settings appstate.Settings) bool {
	mode := strings.ToLower(strings.TrimSpace(settings.SelectionHighlight.Preset))
	if mode == "text" {
		return false
	}
	if settings.SelectionHighlight.Fill {
		return true
	}
	if mode == "fill" {
		return true
	}
	return strings.TrimSpace(settings.SelectionHighlight.Color) == "" && mode == ""
}

func contrastForeground(value string) string {
	if !strings.HasPrefix(value, "#") {
		return "#111111"
	}
	hex := strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(hex) == 3 {
		hex = strings.Repeat(string(hex[0]), 2) + strings.Repeat(string(hex[1]), 2) + strings.Repeat(string(hex[2]), 2)
	}
	if len(hex) != 6 {
		return "#111111"
	}
	r, errR := strconv.ParseInt(hex[0:2], 16, 64)
	g, errG := strconv.ParseInt(hex[2:4], 16, 64)
	b, errB := strconv.ParseInt(hex[4:6], 16, 64)
	if errR != nil || errG != nil || errB != nil {
		return "#111111"
	}
	luminance := (299*r + 587*g + 114*b) / 1000
	if luminance >= 160 {
		return "#111111"
	}
	return "#F8FAFC"
}

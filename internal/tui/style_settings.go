package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

type uiColorChoiceKind string

const (
	uiColorChoiceAuto   uiColorChoiceKind = "auto"
	uiColorChoicePreset uiColorChoiceKind = "preset"
	uiColorChoiceCustom uiColorChoiceKind = "custom"
)

type uiColorChoice struct {
	kind   uiColorChoiceKind
	preset string
	hex    string
}

type selectionPalette struct {
	fillBG                string
	fillSecondaryBG       string
	fillFG                string
	fillSecondaryFG       string
	textFG                string
	textFGEmphasis        string
	textSecondaryFG       string
	textSecondaryEmphasis string
	monoTextFG            string
	monoTextSecondaryFG   string
}

func (m *Model) reloadStyleSettings() {
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		m.applyStyleSettings(appstate.DefaultSettings())
		return
	}
	m.applyStyleSettings(settings)
}

func (m *Model) applyStyleSettings(settings appstate.Settings) {
	ensureTerminalRendererConfigured()
	styles := newStyles()
	fallbackPreset := normalizedSelectionPreset(settings.SelectionHighlight.Preset)
	if fallbackPreset == "" {
		fallbackPreset = "light"
	}

	styles.value = applyColorAndFormats(styles.value, settings.Colors.ListPrimary, fallbackPreset, settings.TextFormats.ListPrimary, false)
	styles.body = applyColorAndFormats(styles.body, settings.Colors.ListPrimary, fallbackPreset, settings.TextFormats.ListPrimary, false)
	styles.roleUser = applyColorAndFormats(styles.roleUser, settings.Colors.ListPrimary, fallbackPreset, settings.TextFormats.ListPrimary, false)

	styles.muted = applyColorAndFormats(styles.muted, settings.Colors.ListSecondary, fallbackPreset, settings.TextFormats.ListSecondary, true)
	styles.label = applyColorAndFormats(styles.label, settings.Colors.ListSecondary, fallbackPreset, settings.TextFormats.ListSecondary, true)
	styles.helpDesc = applyColorAndFormats(styles.helpDesc, settings.Colors.ListSecondary, fallbackPreset, settings.TextFormats.ListSecondary, true)

	styles.roleAssistant = applyColorAndFormats(styles.roleAssistant, settings.Colors.ReplyText, fallbackPreset, settings.TextFormats.Reply, false)
	styles.roleSystem = applyColorAndFormats(styles.roleSystem, settings.Colors.ReasoningText, fallbackPreset, settings.TextFormats.Reasoning, false)
	styles.sectionTitle = applyColorAndFormats(styles.sectionTitle, settings.Colors.CommandText, fallbackPreset, settings.TextFormats.Command, false)
	styles.helpKey = applyColorAndFormats(styles.helpKey, settings.Colors.CommandText, fallbackPreset, settings.TextFormats.Command, false)
	styles.roleTool = applyColorAndFormats(styles.roleTool, settings.Colors.CommandOutput, fallbackPreset, settings.TextFormats.CommandOutput, false)

	selectionChoice := parseUIColorChoice(settings.SelectionHighlight.Color, fallbackPreset)
	palette := selectionPaletteForChoice(selectionChoice, fallbackPreset)
	fillEnabled := effectiveSelectionHighlightFill(settings)
	styles.selected = selectionHighlightStyle(selectionChoice, palette, fillEnabled, false, fallbackPreset, settings.TextFormats.SelectionHighlight)
	styles.selectedSecondary = selectionHighlightStyle(selectionChoice, palette, fillEnabled, true, fallbackPreset, settings.TextFormats.SelectionHighlight)

	m.styles = styles
	m.input.TextStyle = styles.value
	m.input.PlaceholderStyle = styles.muted
	m.input.PromptStyle = styles.sectionTitle
	m.input.Cursor.Style = styles.paneTitle
	m.paletteInput.TextStyle = styles.value
	m.paletteInput.PlaceholderStyle = styles.muted
	m.paletteInput.Cursor.Style = styles.paneTitle
}

func selectionHighlightStyle(choice uiColorChoice, palette selectionPalette, fill bool, secondary bool, fallbackPreset string, formats appstate.TextFormats) lipgloss.Style {
	style := lipgloss.NewStyle()
	if fill {
		bg := palette.fillBG
		fg := palette.fillFG
		if secondary {
			bg = palette.fillSecondaryBG
			fg = palette.fillSecondaryFG
		}
		style = style.Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(fg))
	} else {
		fg := ""
		if choice.kind != uiColorChoiceAuto {
			fg = resolveTextColorChoiceHex(choice, secondary, fallbackPreset)
		}
		if fg == "" {
			if secondary {
				fg = palette.textSecondaryEmphasis
			} else {
				fg = palette.textFGEmphasis
			}
		}
		style = style.Foreground(lipgloss.Color(fg)).Background(lipgloss.NoColor{})
	}
	return applyTextFormats(style, formats)
}

func effectiveSelectionHighlightFill(settings appstate.Settings) bool {
	return settings.SelectionHighlight.Fill
}

func normalizedSelectionPreset(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "graphite":
		return "graphite"
	case "amber":
		return "amber"
	case "mint":
		return "mint"
	case "rose":
		return "rose"
	default:
		return "light"
	}
}

func parseUIColorChoice(raw string, fallbackPreset string) uiColorChoice {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" || normalized == "auto" {
		return uiColorChoice{kind: uiColorChoiceAuto, preset: normalizedSelectionPreset(fallbackPreset)}
	}
	if hex := legacyGradientBaseHex(normalized); hex != "" {
		return uiColorChoice{kind: uiColorChoiceCustom, preset: normalizedSelectionPreset(fallbackPreset), hex: hex}
	}
	if preset := normalizedSelectionPreset(normalized); preset != "light" || normalized == "light" {
		return uiColorChoice{kind: uiColorChoicePreset, preset: preset}
	}
	if hex := normalizeHexColor(normalized); hex != "" {
		return uiColorChoice{kind: uiColorChoiceCustom, preset: normalizedSelectionPreset(fallbackPreset), hex: hex}
	}
	return uiColorChoice{kind: uiColorChoiceAuto, preset: normalizedSelectionPreset(fallbackPreset)}
}

func legacyGradientBaseHex(raw string) string {
	legacy := strings.TrimSpace(raw)
	if strings.HasPrefix(legacy, "gradient:") {
		legacy = strings.TrimPrefix(legacy, "gradient:")
	}
	for _, separator := range []string{"->", "→", ",", ";", "|", ":"} {
		if start, _, ok := strings.Cut(legacy, separator); ok {
			if hex := normalizeHexColor(start); hex != "" {
				return hex
			}
		}
	}
	return ""
}

func selectionPaletteForChoice(choice uiColorChoice, fallbackPreset string) selectionPalette {
	switch choice.kind {
	case uiColorChoiceCustom:
		if base, ok := parseHexRGB(choice.hex); ok {
			return selectionPaletteFromBase(base)
		}
		return selectionPaletteForPreset(fallbackPreset)
	case uiColorChoicePreset:
		return selectionPaletteForPreset(choice.preset)
	default:
		return selectionPaletteForPreset(fallbackPreset)
	}
}

func selectionPaletteForPreset(preset string) selectionPalette {
	switch normalizedSelectionPreset(preset) {
	case "graphite":
		return selectionPalette{
			fillBG:                "#494e57",
			fillSecondaryBG:       "#494e57",
			fillFG:                "#ffffff",
			fillSecondaryFG:       "#dfe3ea",
			textFG:                "#cdd2dc",
			textFGEmphasis:        "#e4e8ef",
			textSecondaryFG:       "#a3aab7",
			textSecondaryEmphasis: "#bcc3cf",
			monoTextFG:            "#dadee4",
			monoTextSecondaryFG:   "#bdc2c9",
		}
	case "amber":
		return selectionPalette{
			fillBG:                "#f8e5bf",
			fillSecondaryBG:       "#f8e5bf",
			fillFG:                "#000000",
			fillSecondaryFG:       "#4e3d23",
			textFG:                "#f7e0a6",
			textFGEmphasis:        "#ffd58a",
			textSecondaryFG:       "#ddc181",
			textSecondaryEmphasis: "#eccd8e",
			monoTextFG:            "#e4d6b5",
			monoTextSecondaryFG:   "#ccbb97",
		}
	case "mint":
		return selectionPalette{
			fillBG:                "#d4f1df",
			fillSecondaryBG:       "#d4f1df",
			fillFG:                "#000000",
			fillSecondaryFG:       "#224937",
			textFG:                "#beeed0",
			textFGEmphasis:        "#a3e4bb",
			textSecondaryFG:       "#99d0b1",
			textSecondaryEmphasis: "#abdcbf",
			monoTextFG:            "#c0e2cc",
			monoTextSecondaryFG:   "#a1c6b1",
		}
	case "rose":
		return selectionPalette{
			fillBG:                "#f6d5e0",
			fillSecondaryBG:       "#f6d5e0",
			fillFG:                "#000000",
			fillSecondaryFG:       "#573343",
			textFG:                "#f4c6d9",
			textFGEmphasis:        "#ecb2ca",
			textSecondaryFG:       "#dca5bd",
			textSecondaryEmphasis: "#e7b5ca",
			monoTextFG:            "#e4c8d3",
			monoTextSecondaryFG:   "#cdafbc",
		}
	default:
		return selectionPalette{
			fillBG:                "#ffffff",
			fillSecondaryBG:       "#ffffff",
			fillFG:                "#000000",
			fillSecondaryFG:       "#3e3e3e",
			textFG:                "#ffffff",
			textFGEmphasis:        "#f5f5f5",
			textSecondaryFG:       "#d6d6d6",
			textSecondaryEmphasis: "#e6e6e6",
			monoTextFG:            "#efefef",
			monoTextSecondaryFG:   "#cecece",
		}
	}
}

func selectionPaletteFromBase(base [3]float64) selectionPalette {
	fillFG := [3]float64{250, 250, 250}
	if isLightRGB(base) {
		fillFG = [3]float64{15, 15, 15}
	}
	fillSecondaryFG := [3]float64{220, 223, 228}
	if isLightRGB(base) {
		fillSecondaryFG = [3]float64{58, 58, 58}
	}

	textBase := [3]float64{245, 245, 245}
	if rgbHex(base) != "#ffffff" {
		target := [3]float64{240, 240, 240}
		weight := 0.92
		if isLightRGB(base) {
			target = [3]float64{255, 255, 255}
			weight = 0.78
		}
		textBase = blendRGB(base, target, weight)
	}
	textEmphasis := blendRGB(base, [3]float64{255, 255, 255}, 0.97)
	if isLightRGB(base) {
		textEmphasis = blendRGB(base, [3]float64{255, 255, 255}, 0.88)
	}
	textSecondary := blendRGB(base, [3]float64{180, 185, 195}, 0.82)
	if isLightRGB(base) {
		textSecondary = blendRGB(base, [3]float64{70, 70, 70}, 0.72)
	}
	textSecondaryEmphasis := blendRGB(base, [3]float64{220, 225, 230}, 0.90)
	if isLightRGB(base) {
		textSecondaryEmphasis = blendRGB(base, [3]float64{55, 55, 55}, 0.80)
	}
	monoFG := blendRGB(textBase, [3]float64{128, 214, 255}, 0.36)
	monoSecondary := blendRGB(textSecondary, [3]float64{128, 214, 255}, 0.30)
	if isLightRGB(base) {
		monoFG = blendRGB(textBase, [3]float64{92, 64, 42}, 0.62)
		monoSecondary = blendRGB(textSecondary, [3]float64{92, 64, 42}, 0.56)
	}

	return selectionPalette{
		fillBG:                rgbHex(base),
		fillSecondaryBG:       rgbHex(base),
		fillFG:                rgbHex(fillFG),
		fillSecondaryFG:       rgbHex(fillSecondaryFG),
		textFG:                rgbHex(textBase),
		textFGEmphasis:        rgbHex(textEmphasis),
		textSecondaryFG:       rgbHex(textSecondary),
		textSecondaryEmphasis: rgbHex(textSecondaryEmphasis),
		monoTextFG:            rgbHex(monoFG),
		monoTextSecondaryFG:   rgbHex(monoSecondary),
	}
}

func applyColorAndFormats(style lipgloss.Style, rawChoice string, fallbackPreset string, formats appstate.TextFormats, secondary bool) lipgloss.Style {
	choice := parseUIColorChoice(rawChoice, fallbackPreset)
	if choice.kind != uiColorChoiceAuto {
		style = style.Foreground(lipgloss.Color(resolveColorChoiceHex(choice, fallbackPreset)))
	}
	return applyTextFormats(style, formats)
}

func applyTextFormats(style lipgloss.Style, formats appstate.TextFormats) lipgloss.Style {
	if formats.Bold {
		style = emphasizeForeground(style, 0.82)
		style = style.Bold(true)
	}
	if formats.Italic {
		style = italicizeForeground(style)
		style = style.Italic(true)
	}
	if formats.Underlined {
		style = style.Underline(true)
	}
	if formats.CrossedOut {
		style = style.Strikethrough(true)
	}
	return style
}

func resolveColorChoiceHex(choice uiColorChoice, fallbackPreset string) string {
	switch choice.kind {
	case uiColorChoicePreset:
		return selectionPresetHex(choice.preset)
	case uiColorChoiceCustom:
		if choice.hex != "" {
			return choice.hex
		}
		return selectionPresetHex(fallbackPreset)
	default:
		return selectionPresetHex(fallbackPreset)
	}
}

func resolveTextColorChoiceHex(choice uiColorChoice, secondary bool, fallbackPreset string) string {
	_ = secondary
	if choice.kind == uiColorChoiceAuto {
		return ""
	}
	return resolveColorChoiceHex(choice, fallbackPreset)
}

func selectionPresetHex(preset string) string {
	switch normalizedSelectionPreset(preset) {
	case "graphite":
		return "#495057"
	case "amber":
		return "#f0d6a2"
	case "mint":
		return "#bfe4ce"
	case "rose":
		return "#efc1d2"
	default:
		return "#ffffff"
	}
}

func normalizeHexColor(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "#") {
		trimmed = "#" + trimmed
	}
	hex := strings.TrimPrefix(trimmed, "#")
	if len(hex) == 3 {
		hex = strings.Repeat(string(hex[0]), 2) + strings.Repeat(string(hex[1]), 2) + strings.Repeat(string(hex[2]), 2)
	}
	if len(hex) != 6 {
		return ""
	}
	for _, ch := range hex {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return ""
		}
	}
	return "#" + strings.ToLower(hex)
}

func parseHexRGB(value string) ([3]float64, bool) {
	hex := normalizeHexColor(value)
	if hex == "" {
		return [3]float64{}, false
	}
	r, errR := strconv.ParseInt(hex[1:3], 16, 64)
	g, errG := strconv.ParseInt(hex[3:5], 16, 64)
	b, errB := strconv.ParseInt(hex[5:7], 16, 64)
	if errR != nil || errG != nil || errB != nil {
		return [3]float64{}, false
	}
	return [3]float64{float64(r), float64(g), float64(b)}, true
}

func rgbHex(rgb [3]float64) string {
	return fmt.Sprintf("#%02x%02x%02x", clampColor(rgb[0]), clampColor(rgb[1]), clampColor(rgb[2]))
}

func clampColor(value float64) uint8 {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return uint8(value + 0.5)
}

func blendRGB(a [3]float64, b [3]float64, alpha float64) [3]float64 {
	return [3]float64{
		a[0]*(1-alpha) + b[0]*alpha,
		a[1]*(1-alpha) + b[1]*alpha,
		a[2]*(1-alpha) + b[2]*alpha,
	}
}

func rgbLuminance(rgb [3]float64) float64 {
	channel := func(value float64) float64 {
		srgb := value / 255.0
		if srgb <= 0.04045 {
			return srgb / 12.92
		}
		return math.Pow((srgb+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(rgb[0]) + 0.7152*channel(rgb[1]) + 0.0722*channel(rgb[2])
}

func isLightRGB(rgb [3]float64) bool {
	return rgbLuminance(rgb) >= 0.55
}

func rgbFromTerminalColor(color lipgloss.TerminalColor) ([3]float64, bool) {
	if color == nil {
		return [3]float64{}, false
	}
	if _, ok := color.(lipgloss.NoColor); ok {
		return [3]float64{}, false
	}
	r, g, b, _ := color.RGBA()
	return [3]float64{float64(r >> 8), float64(g >> 8), float64(b >> 8)}, true
}

func defaultTerminalBackgroundRGB() [3]float64 {
	ensureTerminalRendererConfigured()
	if lipgloss.HasDarkBackground() {
		return [3]float64{12, 12, 12}
	}
	return [3]float64{245, 245, 245}
}

func defaultTerminalForegroundRGB() [3]float64 {
	ensureTerminalRendererConfigured()
	if lipgloss.HasDarkBackground() {
		return [3]float64{234, 234, 234}
	}
	return [3]float64{26, 26, 26}
}

func contrastDelta(foreground [3]float64, background [3]float64) float64 {
	return math.Abs(rgbLuminance(foreground) - rgbLuminance(background))
}

func visibleTerminalRGB(rgb [3]float64) [3]float64 {
	background := defaultTerminalBackgroundRGB()
	if contrastDelta(rgb, background) >= 0.28 {
		return rgb
	}
	target := [3]float64{245, 245, 245}
	if rgbLuminance(background) >= 0.52 {
		target = [3]float64{18, 18, 18}
	}
	for _, weight := range []float64{0.16, 0.30, 0.46, 0.62, 0.80, 1.0} {
		candidate := blendRGB(rgb, target, weight)
		if contrastDelta(candidate, background) >= 0.28 {
			return candidate
		}
	}
	return target
}

func adjustForegroundForBackgroundRGB(foreground [3]float64, background [3]float64, secondary bool) [3]float64 {
	minimumDelta := 0.34
	if secondary {
		minimumDelta = 0.24
	}
	if contrastDelta(foreground, background) >= minimumDelta {
		return visibleTerminalRGB(foreground)
	}
	target := [3]float64{245, 245, 245}
	if rgbLuminance(background) >= 0.62 {
		target = [3]float64{12, 12, 12}
	}
	for _, weight := range []float64{0.18, 0.32, 0.48, 0.64, 0.82, 1.0} {
		candidate := blendRGB(foreground, target, weight)
		if contrastDelta(candidate, background) >= minimumDelta {
			return visibleTerminalRGB(candidate)
		}
	}
	return visibleTerminalRGB(target)
}

func ensureVisibleTextHex(hex string, secondary bool) string {
	rgb, ok := parseHexRGB(hex)
	if !ok {
		return hex
	}
	return rgbHex(adjustForegroundForBackgroundRGB(rgb, defaultTerminalBackgroundRGB(), secondary))
}

func styleForegroundRGB(style lipgloss.Style) [3]float64 {
	if rgb, ok := rgbFromTerminalColor(style.GetForeground()); ok {
		return rgb
	}
	return defaultTerminalForegroundRGB()
}

func emphasisTargetRGB(rgb [3]float64) [3]float64 {
	if isLightRGB(rgb) {
		return [3]float64{12, 12, 12}
	}
	return [3]float64{245, 245, 245}
}

func italicTargetRGB(rgb [3]float64) [3]float64 {
	if isLightRGB(rgb) {
		return [3]float64{78, 96, 148}
	}
	return [3]float64{166, 190, 255}
}

func emphasizeForeground(style lipgloss.Style, weight float64) lipgloss.Style {
	rgb := styleForegroundRGB(style)
	next := adjustForegroundForBackgroundRGB(blendRGB(rgb, emphasisTargetRGB(rgb), weight), defaultTerminalBackgroundRGB(), false)
	return style.Foreground(lipgloss.Color(rgbHex(next)))
}

func italicizeForeground(style lipgloss.Style) lipgloss.Style {
	rgb := styleForegroundRGB(style)
	next := adjustForegroundForBackgroundRGB(blendRGB(rgb, italicTargetRGB(rgb), 0.72), defaultTerminalBackgroundRGB(), false)
	return style.Foreground(lipgloss.Color(rgbHex(next)))
}

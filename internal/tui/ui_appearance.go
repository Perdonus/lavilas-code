package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

type namedColor struct {
	NameRU string
	NameEN string
	Hex    string
}

type popupColorTarget string

type popupFormatTarget string

const (
	popupColorTargetSelection     popupColorTarget = "selection"
	popupColorTargetListPrimary   popupColorTarget = "list_primary"
	popupColorTargetListSecondary popupColorTarget = "list_secondary"
	popupColorTargetReply         popupColorTarget = "reply"
	popupColorTargetReasoning     popupColorTarget = "reasoning"
	popupColorTargetCommand       popupColorTarget = "command"
	popupColorTargetCommandOutput popupColorTarget = "command_output"
)

const (
	popupFormatTargetSelection     popupFormatTarget = "selection"
	popupFormatTargetListPrimary   popupFormatTarget = "list_primary"
	popupFormatTargetListSecondary popupFormatTarget = "list_secondary"
	popupFormatTargetReply         popupFormatTarget = "reply"
	popupFormatTargetReasoning     popupFormatTarget = "reasoning"
	popupFormatTargetCommand       popupFormatTarget = "command"
	popupFormatTargetCommandOutput popupFormatTarget = "command_output"
)

var allNamedColors = []namedColor{
	{NameRU: "Белый", NameEN: "White", Hex: "#ffffff"},
	{NameRU: "Фарфор", NameEN: "Porcelain", Hex: "#f7f6f2"},
	{NameRU: "Жемчужный", NameEN: "Pearl", Hex: "#f0ece4"},
	{NameRU: "Серебро", NameEN: "Silver", Hex: "#d8dce3"},
	{NameRU: "Дымчатый", NameEN: "Smoke", Hex: "#8d96a3"},
	{NameRU: "Стальной", NameEN: "Steel", Hex: "#6a7280"},
	{NameRU: "Сланцевый", NameEN: "Slate", Hex: "#58616d"},
	{NameRU: "Тень", NameEN: "Shadow", Hex: "#414952"},
	{NameRU: "Графит", NameEN: "Graphite", Hex: "#495057"},
	{NameRU: "Антрацит", NameEN: "Anthracite", Hex: "#2f3438"},
	{NameRU: "Чернильный", NameEN: "Ink", Hex: "#263042"},
	{NameRU: "Оникс", NameEN: "Onyx", Hex: "#202428"},
	{NameRU: "Уголь", NameEN: "Coal", Hex: "#17191c"},
	{NameRU: "Глубокий графит", NameEN: "Deep Graphite", Hex: "#30363d"},
	{NameRU: "Тёмный шифер", NameEN: "Dark Slate", Hex: "#37404a"},
	{NameRU: "Базальт", NameEN: "Basalt", Hex: "#2b3138"},
	{NameRU: "Вулканический", NameEN: "Volcanic", Hex: "#23282d"},
	{NameRU: "Сумеречный", NameEN: "Dusk", Hex: "#3d4357"},
	{NameRU: "Тёмная хвоя", NameEN: "Dark Pine", Hex: "#223a34"},
	{NameRU: "Тёмный мох", NameEN: "Dark Moss", Hex: "#364637"},
	{NameRU: "Ночной бордо", NameEN: "Night Bordeaux", Hex: "#4c2933"},
	{NameRU: "Тёмная слива", NameEN: "Dark Plum", Hex: "#432b49"},
	{NameRU: "Полночный", NameEN: "Midnight", Hex: "#1f2a44"},
	{NameRU: "Ночной синий", NameEN: "Navy", Hex: "#24324f"},
	{NameRU: "Петрольный", NameEN: "Petrol", Hex: "#335b63"},
	{NameRU: "Глубокая волна", NameEN: "Deep Sea", Hex: "#22485e"},
	{NameRU: "Лесной", NameEN: "Forest", Hex: "#355a46"},
	{NameRU: "Моховой", NameEN: "Moss", Hex: "#51684a"},
	{NameRU: "Хвойный", NameEN: "Pine", Hex: "#28453b"},
	{NameRU: "Песочный", NameEN: "Sand", Hex: "#e9ddc8"},
	{NameRU: "Кремовый", NameEN: "Cream", Hex: "#f2dfb4"},
	{NameRU: "Янтарный", NameEN: "Amber", Hex: "#f0d6a2"},
	{NameRU: "Медовый", NameEN: "Honey", Hex: "#e3c678"},
	{NameRU: "Персиковый", NameEN: "Peach", Hex: "#efc5ae"},
	{NameRU: "Коралловый", NameEN: "Coral", Hex: "#e89c91"},
	{NameRU: "Розовый", NameEN: "Rose", Hex: "#efc1d2"},
	{NameRU: "Пудровый", NameEN: "Powder Rose", Hex: "#e3afc2"},
	{NameRU: "Лиловый", NameEN: "Lilac", Hex: "#d8c2e8"},
	{NameRU: "Лавандовый", NameEN: "Lavender", Hex: "#c7b4e5"},
	{NameRU: "Небесный", NameEN: "Sky", Hex: "#bfd8ef"},
	{NameRU: "Ледяной", NameEN: "Ice", Hex: "#d7ecf3"},
	{NameRU: "Океанский", NameEN: "Ocean", Hex: "#5a8fb3"},
	{NameRU: "Сапфировый", NameEN: "Sapphire", Hex: "#326f9c"},
	{NameRU: "Мятный", NameEN: "Mint", Hex: "#bfe4ce"},
	{NameRU: "Шалфей", NameEN: "Sage", Hex: "#9cc4ac"},
	{NameRU: "Изумрудный", NameEN: "Emerald", Hex: "#4f8c73"},
	{NameRU: "Лаймовый", NameEN: "Lime", Hex: "#bfdc7a"},
	{NameRU: "Бирюзовый", NameEN: "Turquoise", Hex: "#81d2cf"},
	{NameRU: "Голубой", NameEN: "Azure", Hex: "#75b8dd"},
	{NameRU: "Индиго", NameEN: "Indigo", Hex: "#5261a8"},
	{NameRU: "Сливовый", NameEN: "Plum", Hex: "#875b8d"},
	{NameRU: "Баклажановый", NameEN: "Aubergine", Hex: "#5c3b63"},
	{NameRU: "Бордовый", NameEN: "Bordeaux", Hex: "#6a3946"},
	{NameRU: "Шёлковица", NameEN: "Mulberry", Hex: "#4b324f"},
	{NameRU: "Какао", NameEN: "Cocoa", Hex: "#8a6d5a"},
	{NameRU: "Шоколадный", NameEN: "Chocolate", Hex: "#6d4c41"},
	{NameRU: "Ржавчина", NameEN: "Rust", Hex: "#7b4d3d"},
	{NameRU: "Красный", NameEN: "Red", Hex: "#d35b62"},
}

func popupColorTargets() []popupColorTarget {
	return []popupColorTarget{
		popupColorTargetSelection,
		popupColorTargetListPrimary,
		popupColorTargetListSecondary,
		popupColorTargetReply,
		popupColorTargetReasoning,
		popupColorTargetCommand,
		popupColorTargetCommandOutput,
	}
}

func popupFormatTargets() []popupFormatTarget {
	return []popupFormatTarget{
		popupFormatTargetSelection,
		popupFormatTargetListPrimary,
		popupFormatTargetListSecondary,
		popupFormatTargetReply,
		popupFormatTargetReasoning,
		popupFormatTargetCommand,
		popupFormatTargetCommandOutput,
	}
}

func popupColorTargetLabel(target popupColorTarget, language commandcatalog.CatalogLanguage) string {
	switch target {
	case popupColorTargetSelection:
		return localizedTextTUI(language, "Selection", "Выделение")
	case popupColorTargetListPrimary:
		return localizedTextTUI(language, "Primary text", "Основной текст")
	case popupColorTargetListSecondary:
		return localizedTextTUI(language, "Secondary text", "Текст описания")
	case popupColorTargetReply:
		return localizedTextTUI(language, "Replies", "Ответы")
	case popupColorTargetReasoning:
		return localizedTextTUI(language, "Reasoning", "Раздумья")
	case popupColorTargetCommand:
		return localizedTextTUI(language, "Commands", "Команды")
	case popupColorTargetCommandOutput:
		return localizedTextTUI(language, "Output", "Вывод")
	default:
		return localizedTextTUI(language, "Selection", "Выделение")
	}
}

func popupFormatTargetLabel(target popupFormatTarget, language commandcatalog.CatalogLanguage) string {
	return popupColorTargetLabel(popupColorTarget(target), language)
}

func renderStateTag(enabled bool) string {
	text := " ✕ "
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("1")).Bold(true)
	if enabled {
		text = " ✓ "
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("2")).Bold(true)
	}
	return style.Render(text)
}

func namedColorLabel(color namedColor, language commandcatalog.CatalogLanguage) string {
	if language == commandcatalog.CatalogLanguageRussian {
		return color.NameRU
	}
	return color.NameEN
}

func findNamedColorByHex(hex string) (namedColor, bool) {
	normalized := normalizeHexColor(hex)
	if normalized == "" {
		return namedColor{}, false
	}
	for _, color := range allNamedColors {
		if strings.EqualFold(color.Hex, normalized) {
			return color, true
		}
	}
	return namedColor{}, false
}

func closestNamedColorLabel(hex string, language commandcatalog.CatalogLanguage) string {
	rgb, ok := parseHexRGB(hex)
	if !ok {
		return localizedTextTUI(language, "Custom color", "Свой цвет")
	}
	best := allNamedColors[0]
	bestDistance := 1e18
	for _, candidate := range allNamedColors {
		candidateRGB, ok := parseHexRGB(candidate.Hex)
		if !ok {
			continue
		}
		dr := rgb[0] - candidateRGB[0]
		dg := rgb[1] - candidateRGB[1]
		db := rgb[2] - candidateRGB[2]
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			bestDistance = distance
			best = candidate
		}
	}
	return namedColorLabel(best, language)
}

func renderColorBadge(hex string) string {
	normalized := normalizeHexColor(hex)
	if normalized == "" {
		normalized = "#ffffff"
	}
	fg := "#000000"
	if rgb, ok := parseHexRGB(normalized); ok && !isLightRGB(rgb) {
		fg = "#ffffff"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(normalized)).Bold(true).Render("  ")
}

func renderColoredLabel(label string, hex string) string {
	normalized := normalizeHexColor(hex)
	if normalized == "" {
		normalized = "#ffffff"
	}
	return renderColorBadge(normalized) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ensureVisibleTextHex(normalized, false))).Bold(true).Render(label)
}

func colorPreviewDescriptionChoice(choice uiColorChoice, fallbackPreset string, language commandcatalog.CatalogLanguage) string {
	switch choice.kind {
	case uiColorChoiceAuto:
		return localizedTextTUI(language, "Auto · ", "Авто · ") + selectionPresetDisplayName(fallbackPreset, language)
	case uiColorChoicePreset:
		return selectionPresetDisplayName(choice.preset, language) + " · " + strings.ToUpper(selectionPresetHex(choice.preset))
	case uiColorChoiceCustom:
		return closestNamedColorLabel(choice.hex, language) + " · " + strings.ToUpper(choice.hex)
	default:
		return localizedTextTUI(language, "Auto", "Авто")
	}
}

func colorChoiceSummary(choice string, fallbackPreset string, language commandcatalog.CatalogLanguage) string {
	parsed := parseUIColorChoice(choice, fallbackPreset)
	switch parsed.kind {
	case uiColorChoicePreset:
		return selectionPresetDisplayName(parsed.preset, language)
	case uiColorChoiceCustom:
		return closestNamedColorLabel(parsed.hex, language) + " " + strings.ToUpper(parsed.hex)
	default:
		return localizedTextTUI(language, "Auto", "Авто")
	}
}

func selectionPresetDisplayName(preset string, language commandcatalog.CatalogLanguage) string {
	switch normalizedSelectionPreset(preset) {
	case "graphite":
		return localizedTextTUI(language, "Graphite", "Графит")
	case "amber":
		return localizedTextTUI(language, "Amber", "Янтарь")
	case "mint":
		return localizedTextTUI(language, "Mint", "Мята")
	case "rose":
		return localizedTextTUI(language, "Rose", "Роза")
	default:
		return localizedTextTUI(language, "Light", "Светлый")
	}
}

func renderPresetChoiceLabel(preset string, language commandcatalog.CatalogLanguage) string {
	return renderColoredLabel(selectionPresetDisplayName(preset, language), selectionPresetHex(preset))
}

func renderNamedColorChoiceLabel(color namedColor, language commandcatalog.CatalogLanguage) string {
	return renderColoredLabel(namedColorLabel(color, language), color.Hex)
}

func popupColorTargetChoice(settings appstate.Settings, target popupColorTarget) string {
	switch target {
	case popupColorTargetSelection:
		return strings.TrimSpace(settings.SelectionHighlight.Color)
	case popupColorTargetListPrimary:
		return strings.TrimSpace(settings.Colors.ListPrimary)
	case popupColorTargetListSecondary:
		return strings.TrimSpace(settings.Colors.ListSecondary)
	case popupColorTargetReply:
		return strings.TrimSpace(settings.Colors.ReplyText)
	case popupColorTargetReasoning:
		return strings.TrimSpace(settings.Colors.ReasoningText)
	case popupColorTargetCommand:
		return strings.TrimSpace(settings.Colors.CommandText)
	case popupColorTargetCommandOutput:
		return strings.TrimSpace(settings.Colors.CommandOutput)
	default:
		return ""
	}
}

func popupFormatTargetValue(settings appstate.Settings, target popupFormatTarget) appstate.TextFormats {
	switch target {
	case popupFormatTargetSelection:
		return settings.TextFormats.SelectionHighlight
	case popupFormatTargetListPrimary:
		return settings.TextFormats.ListPrimary
	case popupFormatTargetListSecondary:
		return settings.TextFormats.ListSecondary
	case popupFormatTargetReply:
		return settings.TextFormats.Reply
	case popupFormatTargetReasoning:
		return settings.TextFormats.Reasoning
	case popupFormatTargetCommand:
		return settings.TextFormats.Command
	case popupFormatTargetCommandOutput:
		return settings.TextFormats.CommandOutput
	default:
		return appstate.TextFormats{}
	}
}

func formatCodeLabel(code string, language commandcatalog.CatalogLanguage) string {
	switch code {
	case "bold":
		return localizedTextTUI(language, "Bold", "Жирный")
	case "italic":
		return localizedTextTUI(language, "Italic", "Курсив")
	case "underlined":
		return localizedTextTUI(language, "Underlined", "Подчёркнутый")
	case "crossed_out":
		return localizedTextTUI(language, "Crossed out", "Зачёркнутый")
	default:
		return code
	}
}

func renderFormatChoiceLabel(code string, language commandcatalog.CatalogLanguage) string {
	label := formatCodeLabel(code, language)
	preview := lipgloss.NewStyle().Foreground(lipgloss.Color(ensureVisibleTextHex("#e8ecf2", false)))
	prefix := ""
	switch code {
	case "bold":
		preview = applyTextFormats(preview, appstate.TextFormats{Bold: true})
		prefix = "B "
	case "italic":
		preview = applyTextFormats(preview, appstate.TextFormats{Italic: true})
		prefix = "/ "
	case "underlined":
		preview = applyTextFormats(preview, appstate.TextFormats{Underlined: true})
		prefix = "_ "
	case "crossed_out":
		preview = applyTextFormats(preview, appstate.TextFormats{CrossedOut: true})
		prefix = "~ "
	}
	return preview.Render(prefix + label)
}

func formatValueSummary(formats appstate.TextFormats, language commandcatalog.CatalogLanguage) string {
	labels := make([]string, 0, 4)
	for _, code := range []string{"bold", "italic", "underlined", "crossed_out"} {
		if formats.Contains(code) {
			labels = append(labels, formatCodeLabel(code, language))
		}
	}
	if len(labels) == 0 {
		return localizedTextTUI(language, "Off", "Выключено")
	}
	return strings.Join(labels, ", ")
}

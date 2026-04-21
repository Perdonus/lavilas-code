package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	appstate "github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/version"
)

const (
	headerCardPreferredWidth = 57
	commandPopupMaxRows      = 8
	sessionPickerListPadding = 1
)

func (m *Model) renderCodexScreen() string {
	if m.isSessionPickerMode() && m.state.Focus == FocusPalette {
		return m.styles.app.Render(m.renderSessionPickerScreen())
	}

	header := m.renderSessionHeaderBox()
	composer := m.renderComposerPane()
	popup := ""
	if m.state.Palette.Visible {
		popup = m.renderCommandPopup(commandPopupMaxRows + 4)
	}
	aux := make([]string, 0, 3)
	if m.approval != nil {
		aux = append(aux, m.renderApprovalPane())
	}
	if m.cwdPrompt != nil {
		aux = append(aux, m.renderCWDPromptPane())
	}
	if m.formPrompt != nil {
		aux = append(aux, m.renderFormPromptPane())
	}

	reserved := lipgloss.Height(header) + lipgloss.Height(composer)
	if popup != "" {
		reserved += lipgloss.Height(popup)
	}
	for _, block := range aux {
		if strings.TrimSpace(block) != "" {
			reserved += lipgloss.Height(block)
		}
	}
	bodyHeight := maxInt(0, m.height-reserved)
	parts := []string{header, m.renderConversationArea(bodyHeight)}
	parts = append(parts, aux...)
	if popup != "" {
		parts = append(parts, popup)
	}
	parts = append(parts, composer)
	return m.styles.app.Width(m.width).Render(strings.Join(parts, "\n"))
}

func (m *Model) isSessionPickerMode() bool {
	return m.state.Palette.Visible && (m.state.Palette.Mode == PaletteModeResume || m.state.Palette.Mode == PaletteModeFork)
}

func (m *Model) isInlineCommandPaletteActive() bool {
	return m.state.Palette.Visible && m.state.Palette.Mode == PaletteModeRoot && m.state.Focus == FocusInput
}

func (m *Model) renderSessionHeaderBox() string {
	cardWidth := minInt(maxInt(36, m.width-2), headerCardPreferredWidth)
	modelValue := strings.TrimSpace(m.state.Model)
	if reasoning := strings.TrimSpace(m.headerReasoningLabel()); reasoning != "" {
		if modelValue == "" {
			modelValue = reasoning
		} else {
			modelValue += " " + reasoning
		}
	}
	if modelValue == "" {
		modelValue = localizedUnsetTUI(m.language)
	}
	labelModel := m.localize("model:", "модель:")
	labelDirectory := m.localize("directory:", "каталог:")
	changeHint := fmt.Sprintf("%s%s %s", commandPrefix(m.layout.SettingsPath()), m.localize("model", "модель"), m.localize("to change", "для смены"))
	rows := []string{
		fmt.Sprintf(">_ Go Lavilas (v%s)", version.Version),
		"",
		fmt.Sprintf("%-14s %s", labelModel, joinHeaderValue(modelValue, changeHint)),
		fmt.Sprintf("%-14s %s", labelDirectory, compactPathForUI(strings.TrimSpace(m.effectiveWorkingDirectory()))),
	}
	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5D6675")).
		Padding(0, 1).
		Render(strings.Join(rows, "\n"))
}

func joinHeaderValue(value string, hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return value
	}
	return value + "   " + hint
}

func compactPathForUI(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "~"
	}
	home := appstate.ComparableSessionCWD(apphomeHomeDir())
	clean := appstate.ComparableSessionCWD(path)
	if clean == "" {
		clean = path
	}
	if home != "" && clean == home {
		return "~"
	}
	if home != "" && strings.HasPrefix(clean, home+"/") {
		return "~" + strings.TrimPrefix(clean, home)
	}
	return clean
}

func apphomeHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func (m *Model) headerReasoningLabel() string {
	switch strings.ToLower(strings.TrimSpace(m.state.Reasoning)) {
	case "xhigh", "extra-high":
		return m.localize("xhigh", "максимальный")
	case "high":
		return m.localize("high", "высокий")
	case "medium":
		return m.localize("medium", "средний")
	case "low":
		return m.localize("low", "низкий")
	case "none", "off":
		return m.localize("no reasoning", "без размышлений")
	default:
		return strings.TrimSpace(m.state.Reasoning)
	}
}

func (m *Model) renderComposerPane() string {
	meta := m.renderComposerMeta()
	parts := []string{m.input.View()}
	if strings.TrimSpace(meta) != "" {
		parts = append(parts, m.styles.muted.Render(meta))
	}
	return strings.Join(parts, "\n")
}

func (m *Model) renderComposerMeta() string {
	cwd := compactPathForUI(strings.TrimSpace(m.effectiveWorkingDirectory()))
	parts := make([]string, 0, 3)
	modelValue := strings.TrimSpace(m.state.Model)
	if reasoning := strings.TrimSpace(m.headerReasoningLabel()); reasoning != "" {
		if modelValue == "" {
			modelValue = reasoning
		} else {
			modelValue += " " + reasoning
		}
	}
	if modelValue != "" {
		parts = append(parts, modelValue)
	}
	if m.state.Busy {
		parts = append(parts, m.localize("running", "выполняется"))
	} else {
		parts = append(parts, m.localize("100% left", "100% осталось"))
	}
	if cwd != "" {
		parts = append(parts, cwd)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + strings.Join(parts, " · ")
}

func (m *Model) renderConversationArea(height int) string {
	if height <= 0 {
		return ""
	}
	width := maxInt(1, m.width)
	m.viewport.Width = width
	m.viewport.Height = maxInt(1, height)
	m.refreshViewport()
	body := m.viewport.View()
	if strings.TrimSpace(body) == "" {
		return lipgloss.NewStyle().Width(width).Height(height).Render("")
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(body)
}

func (m *Model) renderCommandPopup(maxHeight int) string {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, 0, commandPopupMaxRows+4)
	if m.state.Palette.Mode != PaletteModeRoot || m.state.Focus == FocusPalette {
		title := strings.TrimSpace(m.paletteTitle())
		if title != "" {
			lines = append(lines, title)
		}
	}
	if m.state.Focus == FocusPalette && m.state.Palette.Mode != PaletteModeRoot {
		lines = append(lines, m.paletteInput.View())
	}
	reserved := len(lines)
	rowLimit := maxInt(1, minInt(commandPopupMaxRows, maxHeight-reserved-1))
	selected := clampInt(m.state.Palette.Selected, 0, maxInt(0, len(items)-1))
	start, end := paletteVisibleRange(len(items), selected, rowLimit)
	for index := start; index < end; index++ {
		lines = append(lines, m.renderCommandPopupRow(items[index], index == selected))
	}
	if m.state.Focus == FocusPalette {
		if hint := strings.TrimSpace(m.paletteHint()); hint != "" {
			lines = append(lines, m.styles.muted.Render(hint))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderCommandPopupRow(item PaletteItem, selected bool) string {
	prefix := commandPrefix(m.layout.SettingsPath())
	left := strings.TrimSpace(item.Title)
	if m.state.Palette.Mode == PaletteModeRoot {
		left = prefix + strings.TrimSpace(item.Title)
	}
	description := strings.TrimSpace(item.Description)
	row := "  " + left
	if description != "" {
		padding := maxInt(2, 18-lipgloss.Width(left))
		row += strings.Repeat(" ", padding) + description
	}
	style := m.styles.body
	if selected {
		style = m.styles.selected
	}
	return style.Render(row)
}

func (m *Model) renderSessionPickerScreen() string {
	headerHeight := 1
	searchHeight := 1
	columnsHeight := 1
	hintHeight := 1
	listHeight := maxInt(1, m.height-headerHeight-searchHeight-columnsHeight-hintHeight-(sessionPickerListPadding*2))

	title := m.localize("Resume Previous Session", "Продолжить предыдущую сессию")
	if m.state.Palette.Mode == PaletteModeFork {
		title = m.localize("Fork Previous Session", "Форк предыдущей сессии")
	}
	header := fmt.Sprintf(
		"%s  %s: %s",
		title,
		m.localize("Sort", "Сортировка"),
		m.sessionSortLabel(),
	)
	search := m.localize("Type to search", "Введите запрос")
	if query := strings.TrimSpace(m.state.Palette.Query); query != "" {
		search = query
	}
	columns := fmt.Sprintf(
		"  %-16s  %-16s  %-8s  %s",
		m.localize("Created", "Создано"),
		m.localize("Updated", "Обновлено"),
		m.localize("Branch", "Ветка"),
		m.localize("Dialog", "Диалог"),
	)
	entries, _, err := m.sessionPickerEntries()
	list := m.renderSessionPickerList(m.filteredPaletteItems(), entries, listHeight, err)
	hint := strings.Join([]string{
		m.localize("enter to continue", "enter чтобы продолжить"),
		m.localize("esc to start new", "esc чтобы начать новую"),
		m.localize("ctrl + c to exit", "ctrl + c чтобы выйти"),
		m.localize("tab to change sort", "tab чтобы переключить сортировку"),
		m.localize("↑/↓ to move", "↑/↓ для прокрутки"),
	}, "     ")
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join([]string{
		header,
		search,
		columns,
		list,
		hint,
	}, "\n"))
}

func (m *Model) sessionSortLabel() string {
	if m.sessionSort == sessionSortCreated {
		return m.localize("Created", "Дата создания")
	}
	return m.localize("Updated", "Дата обновления")
}

func (m *Model) sessionPickerEntries() ([]appstate.SessionEntry, bool, error) {
	entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 0)
	if err != nil {
		return nil, false, err
	}
	entries, showingAllDirectories := m.filterSessionEntriesWithFallback(entries)
	m.sortSessionEntries(entries)
	entries = m.filterSessionPickerEntries(entries, m.state.Palette.Query)
	if len(entries) > 50 {
		entries = entries[:50]
	}
	return entries, showingAllDirectories, nil
}

func (m *Model) filterSessionPickerEntries(entries []appstate.SessionEntry, query string) []appstate.SessionEntry {
	tokens := tokenizePaletteQuery(query)
	if len(tokens) == 0 {
		return entries
	}
	filtered := make([]appstate.SessionEntry, 0, len(entries))
	for _, entry := range entries {
		haystack := normalizePaletteSearchText(strings.Join([]string{entry.Name, entry.RelPath, entry.CWD, entry.Branch, entry.Preview}, " "))
		matched := true
		for _, token := range tokens {
			if !strings.Contains(haystack, token) {
				matched = false
				break
			}
		}
		if matched {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (m *Model) renderSessionPickerList(items []PaletteItem, entries []appstate.SessionEntry, height int, err error) string {
	if err != nil {
		return lipgloss.NewStyle().Width(m.width).Height(height).Render(err.Error())
	}
	if len(entries) == 0 {
		return lipgloss.NewStyle().Width(m.width).Height(height).Render(m.localize("No sessions yet", "Сессий пока нет"))
	}
	entryByPath := make(map[string]appstate.SessionEntry, len(entries))
	for _, entry := range entries {
		entryByPath[strings.TrimSpace(entry.Path)] = entry
	}
	selectedPath := ""
	if item, ok := m.selectedPaletteItem(); ok {
		selectedPath = strings.TrimSpace(item.Value)
	}
	rows := make([]string, 0, minInt(height, len(items)))
	for _, item := range items {
		entry, ok := entryByPath[strings.TrimSpace(item.Value)]
		if !ok {
			continue
		}
		row := fmt.Sprintf(
			"  %-16s  %-16s  %-8s  %s",
			formatSessionPickerTime(entry.Created),
			formatSessionPickerTime(entry.ModTime),
			truncateForPicker(firstNonEmpty(strings.TrimSpace(entry.Branch), "-"), 8),
			truncateForPicker(firstNonEmpty(strings.TrimSpace(entry.Preview), strings.TrimSpace(entry.RelPath), strings.TrimSpace(entry.Name)), maxInt(12, m.width-48)),
		)
		style := m.styles.body
		if strings.TrimSpace(entry.Path) == selectedPath {
			style = m.styles.selected
		}
		rows = append(rows, style.Render(row))
		if len(rows) == height {
			break
		}
	}
	return lipgloss.NewStyle().Width(m.width).Height(height).Render(strings.Join(rows, "\n"))
}

func formatSessionPickerTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func truncateForPicker(value string, width int) string {
	value = strings.TrimSpace(value)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	return lipgloss.NewStyle().MaxWidth(width - 1).Render(value) + "…"
}

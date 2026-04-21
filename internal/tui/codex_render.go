package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/version"
)

const (
	headerCardPreferredWidth = 57
	commandPopupMaxRows      = 8
	sessionPickerListPadding = 1
)

func (m *Model) renderCodexScreen() string {
	switch {
	case m.state.Focus == FocusPalette && m.isSessionPickerMode():
		return m.styles.app.Render(m.renderSessionPickerScreen())
	case m.state.Focus == FocusPalette && m.state.Palette.Visible && m.state.Palette.Mode == PaletteModeStatus:
		return m.styles.app.Render(m.renderStatusScreen())
	case m.state.Focus == FocusPalette && m.state.Palette.Visible:
		return m.styles.app.Render(m.renderPaletteModalScreen())
	}

	header := m.renderSessionHeaderBox()
	composer := m.renderComposerPane()
	popup := ""
	if m.isInlineCommandPaletteActive() {
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
		BorderForeground(lipgloss.Color("8")).
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
	footer := strings.TrimSpace(m.state.Footer)
	if footer == "" {
		footer = m.localize("? for hints", "? для подсказок")
	}
	parts := []string{m.input.View()}
	if strings.TrimSpace(meta) != "" {
		parts = append(parts, m.styles.muted.Render(meta))
	}
	if footer != "" {
		parts = append(parts, m.styles.muted.Render("  "+footer))
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
	lines := make([]string, 0, commandPopupMaxRows+3)
	rowLimit := maxInt(1, minInt(commandPopupMaxRows, maxHeight-1))
	selected := clampInt(m.state.Palette.Selected, 0, maxInt(0, len(items)-1))
	start, end := paletteVisibleRange(len(items), selected, rowLimit)
	for index := start; index < end; index++ {
		lines = append(lines, m.renderCommandPopupRow(items[index], index == selected))
	}
	if hint := strings.TrimSpace(m.localize("Enter — run, Esc — close", "Enter — открыть, Esc — закрыть")); hint != "" {
		lines = append(lines, m.styles.muted.Render(hint))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderCommandPopupRow(item PaletteItem, selected bool) string {
	prefix := commandPrefix(m.layout.SettingsPath())
	left := prefix + strings.TrimSpace(item.Title)
	right := strings.TrimSpace(item.Description)
	row := padBetween(left, right, maxInt(24, m.width-2))
	style := m.styles.body
	if selected {
		style = m.styles.selected
		row = "› " + strings.TrimLeft(row, " ")
	} else {
		row = "  " + strings.TrimLeft(row, " ")
	}
	return style.Render(row)
}

func (m *Model) renderStatusScreen() string {
	return m.statusCardBox(maxInt(40, m.width))
}

func (m *Model) renderStatusCardInline() string {
	return m.statusCardBox(minInt(maxInt(40, m.width-2), 86))
}

func (m *Model) statusCardBox(width int) string {
	lines := []string{
		fmt.Sprintf(" >_ Go Lavilas (v%s)", version.Version),
		"",
		m.localize(" Limits and credits are shown from the active account snapshot.", " Лимиты и кредиты отображаются по данным текущего аккаунта."),
		m.localize(" If values lag, reopen /status in a few seconds.", " Если значения обновляются не сразу, откройте /status повторно через несколько секунд."),
		"",
	}
	for _, field := range m.statusCardFields() {
		lines = append(lines, formatStatusField(field[0], field[1]))
	}
	return lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

func (m *Model) statusCardFields() [][2]string {
	tokenSummary := m.localize("0 total  (0 input + 0 output)", "0 всего  (0 вход + 0 выход)")
	fields := make([][2]string, 0, 10)
	fields = append(fields,
		[2]string{m.localize("Model", "Модель"), firstNonEmpty(strings.TrimSpace(m.state.Model), localizedUnsetTUI(m.language))},
	)
	if provider := strings.TrimSpace(m.state.Provider); provider != "" {
		fields = append(fields, [2]string{m.localize("Model Provider", "Провайдер модели"), provider})
	}
	fields = append(fields,
		[2]string{m.localize("Directory", "Каталог"), compactPathForUI(strings.TrimSpace(m.effectiveWorkingDirectory()))},
		[2]string{m.localize("Permissions", "Доступ"), m.toolPolicySummary()},
		[2]string{"AGENTS.md", m.statusAgentsPath()},
		[2]string{m.localize("Account", "Аккаунт"), m.statusAccountValue()},
		[2]string{m.localize("Collaboration Mode", "Режим совместной работы"), "Default"},
		[2]string{m.localize("Session", "Сессия"), m.statusSessionValue()},
		[2]string{m.localize("Tokens", "Токены"), tokenSummary},
		[2]string{m.localize("Limits", "Лимиты"), m.localize("data not available yet", "данные пока недоступны")},
	)
	return fields
}

func formatStatusField(label string, value string) string {
	return fmt.Sprintf("  %-26s %s", label+":", value)
}

func (m *Model) statusAgentsPath() string {
	cwd := strings.TrimSpace(m.effectiveWorkingDirectory())
	if cwd == "" {
		return m.localize("<none>", "<нет>")
	}
	path := filepath.Join(cwd, "AGENTS.md")
	if _, err := os.Stat(path); err == nil {
		return "AGENTS.md"
	}
	return m.localize("<none>", "<нет>")
}

func (m *Model) statusAccountValue() string {
	if profile := strings.TrimSpace(m.state.Profile); profile != "" {
		return fmt.Sprintf("%s (%s)", m.localize("Saved profile configured", "Сохранён сохранённый профиль"), profile)
	}
	if _, err := os.Stat(m.layout.AuthPath()); err == nil {
		return m.localize("API key configured (run lvls login to save a profile)", "API-ключ настроен (выполните lvls login, чтобы сохранить профиль)")
	}
	return m.localize("Not configured", "Не настроен")
}

func (m *Model) statusSessionValue() string {
	if sessionPath := strings.TrimSpace(m.state.SessionPath); sessionPath != "" {
		base := strings.TrimSuffix(filepath.Base(sessionPath), filepath.Ext(sessionPath))
		if base != "" {
			return base
		}
		return sessionPath
	}
	return m.localize("<none>", "<нет>")
}

func (m *Model) renderPaletteModalScreen() string {
	title := m.paletteTitle()
	subtitle := m.paletteSubtitle()
	footer := m.paletteFooterHint()
	bodyWidth := maxInt(24, m.width-4)
	bodyLines := make([]string, 0, 32)
	if subtitle != "" {
		bodyLines = append(bodyLines, m.styles.muted.Render(subtitle))
	}
	bodyLines = append(bodyLines, m.styles.muted.Render(strings.Repeat("─", maxInt(8, bodyWidth))))
	bodyLines = append(bodyLines, m.renderPaletteSearchRow(bodyWidth, m.paletteModalResultCount()))
	bodyLines = append(bodyLines, m.renderPaletteRows(bodyWidth, m.paletteModalListHeight(bodyWidth))...)
	return m.renderFramedScreen(title, bodyLines, footer)
}

func (m *Model) renderSessionPickerScreen() string {
	title := m.localize("Resume Previous Session", "Продолжить предыдущую сессию")
	if m.state.Palette.Mode == PaletteModeFork {
		title = m.localize("Fork Previous Session", "Форк предыдущей сессии")
	}
	header := fmt.Sprintf("%s  %s: %s", title, m.localize("Sort", "Сортировка"), m.sessionSortLabel())
	bodyWidth := maxInt(24, m.width-4)
	entries, _, err := m.sessionPickerEntries()
	bodyLines := []string{m.renderPaletteSearchRow(bodyWidth, len(entries))}
	bodyLines = append(bodyLines, m.styles.muted.Render(fmt.Sprintf("  %-16s  %-16s  %-8s  %s",
		m.localize("Created", "Создано"),
		m.localize("Updated", "Обновлено"),
		m.localize("Branch", "Ветка"),
		m.localize("Dialog", "Диалог"),
	)))
	bodyLines = append(bodyLines, m.renderSessionPickerRows(entries, err, m.paletteModalListHeight(bodyWidth))...)
	footer := strings.Join([]string{
		m.localize("enter to continue", "enter чтобы продолжить"),
		m.localize("esc to start new", "esc чтобы начать новую"),
		m.localize("ctrl + c to exit", "ctrl + c чтобы выйти"),
		m.localize("tab to change sort", "tab чтобы переключить сортировку"),
		m.localize("↑/↓ to move", "↑/↓ для списка"),
	}, "     ")
	return m.renderFramedScreen(header, bodyLines, footer)
}

func (m *Model) renderFramedScreen(title string, bodyLines []string, footer string) string {
	pane := lipgloss.NewStyle().
		Width(maxInt(40, m.width)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1)
	content := []string{title}
	content = append(content, bodyLines...)
	rendered := pane.Render(strings.Join(content, "\n"))
	if strings.TrimSpace(footer) == "" {
		return rendered
	}
	return rendered + "\n" + m.styles.muted.Render("  "+footer)
}

func (m *Model) paletteSubtitle() string {
	prefix := commandPrefix(m.layout.SettingsPath())
	switch m.state.Palette.Mode {
	case PaletteModeModelCatalog:
		config, err := loadConfigOptional(m.layout.ConfigPath())
		if err == nil {
			ctx, resolveErr := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
			if resolveErr == nil {
				return fmt.Sprintf("%s · %s", firstNonEmpty(strings.TrimSpace(ctx.ProviderName), strings.TrimSpace(m.state.Provider)), ctx.ProviderID)
			}
		}
		return firstNonEmpty(strings.TrimSpace(m.state.Provider), localizedUnsetTUI(m.language))
	case PaletteModeProfiles:
		if m.language == "ru" {
			return fmt.Sprintf("Папка профилей: %s. Быстрый вызов: %sprofiles", m.layout.ProfilesDir(), prefix)
		}
		return fmt.Sprintf("Profiles folder: %s. Quick command: %sprofiles", m.layout.ProfilesDir(), prefix)
	case PaletteModeCustomization,
		PaletteModeCustomizationColor,
		PaletteModeCustomizationFormatting:
		return ""
	default:
		return ""
	}
}

func (m *Model) paletteFooterHint() string {
	switch m.state.Palette.Mode {
	case PaletteModeModelCatalog:
		return m.localize("Enter opens the reasoning picker, Esc closes the menu.", "Enter открывает выбор размышлений для модели, Esc закрывает окно.")
	case PaletteModeProfiles:
		return m.localize("Press enter to open profile actions, esc to go back", "Нажмите enter чтобы открыть действия профиля, esc для возврата")
	case PaletteModeStatus:
		return m.localize("Esc — back", "Esc — назад")
	default:
		return m.localize("Enter — select, click — open, Esc — back", "Enter — выбрать, клик — открыть, Esc — назад")
	}
}

func (m *Model) paletteSearchPlaceholder() string {
	switch m.state.Palette.Mode {
	case PaletteModeModelCatalog:
		return m.localize("Find a model in the catalog", "Найти модель в каталоге")
	case PaletteModeProfiles:
		return m.localize("Find a profile, model, or provider", "Найти профиль, модель или провайдера")
	case PaletteModeSettings:
		return m.localize("Find a settings section", "Найти раздел настроек")
	case PaletteModeCustomization:
		return m.localize("Find a customization section", "Найти раздел кастомизации")
	case PaletteModeCustomizationColor:
		return m.localize("Find a text color target", "Найти цель цвета текста")
	case PaletteModeCustomizationColorChoice:
		return m.localize("Find a color or HEX", "Найти цвет или HEX")
	case PaletteModeCustomizationFormatting:
		return m.localize("Find a formatting target", "Найти цель форматирования")
	case PaletteModeCustomizationFormattingTarget:
		return m.localize("Find a formatting option", "Найти форматирование")
	case PaletteModeModelSettings:
		return m.localize("Find model settings", "Найти раздел моделей")
	case PaletteModeProviders:
		return m.localize("Find a provider", "Найти провайдера")
	case PaletteModeModelPresets:
		return m.localize("Find a preset", "Найти пресет")
	case PaletteModeRoot:
		return m.localize("Type a command", "Введите команду")
	default:
		return localizedTextTUI(m.language, "Type to filter items", "Введите запрос")
	}
}

func (m *Model) paletteModalResultCount() int {
	items := m.filteredPaletteItems()
	if _, rest, hasBack := splitPaletteBackItem(items); hasBack {
		return len(rest)
	}
	return len(items)
}

func (m *Model) renderPaletteSearchRow(width int, count int) string {
	query := strings.TrimSpace(m.state.Palette.Query)
	placeholder := m.paletteSearchPlaceholder()
	left := query
	if left == "" {
		left = placeholder
	}
	left = "⌕  " + left
	right := fmt.Sprintf("%d", count)
	return m.styles.body.Render(padBetween(left, right, maxInt(12, width)))
}

func (m *Model) paletteModalListHeight(width int) int {
	paneFrame := 2
	bodyLines := 4
	if subtitle := m.paletteSubtitle(); strings.TrimSpace(subtitle) != "" {
		bodyLines++
	}
	footerLines := 1
	available := m.height - paneFrame - bodyLines - footerLines - (sessionPickerListPadding * 2)
	if available < 4 {
		available = 4
	}
	return available
}

func (m *Model) renderPaletteRows(width int, height int) []string {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		return []string{lipgloss.NewStyle().Width(width).Height(height).Render(m.localize("No items match the current filter.", "Ничего не найдено по текущему фильтру."))}
	}
	selected := clampInt(m.state.Palette.Selected, 0, maxInt(0, len(items)-1))
	start, end := paletteVisibleRange(len(items), selected, maxInt(1, minInt(len(items), height)))
	lines := make([]string, 0, height)
	for index := start; index < end; index++ {
		rowLines := m.renderPaletteRowLines(items[index], index == selected, width)
		for _, line := range rowLines {
			if len(lines) == height {
				return lines
			}
			lines = append(lines, line)
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func (m *Model) renderPaletteRowLines(item PaletteItem, selected bool, width int) []string {
	mode := m.state.Palette.Mode
	prefix := "  "
	if selected {
		prefix = "› "
	}
	compact := mode == PaletteModeSettings || mode == PaletteModeCustomization || mode == PaletteModeCustomizationColor || mode == PaletteModeCustomizationColorChoice || mode == PaletteModeCustomizationFormatting || mode == PaletteModeCustomizationFormattingTarget || mode == PaletteModeModelSettings || mode == PaletteModeLanguage || mode == PaletteModeCommandPrefix || mode == PaletteModePopupCommands || mode == PaletteModePermissions || mode == PaletteModeRoot
	style := m.styles.body
	descStyle := m.styles.muted
	if selected {
		style = m.styles.selected
		descStyle = m.styles.selectedSecondary
	}
	title := pickerDisplayText(item.DisplayTitle, item.Title)
	description := pickerDisplayText(item.DisplayDescription, item.Description)
	if compact {
		left := title
		if description != "" {
			left = padBetween(title, description, maxInt(10, width-lipgloss.Width(prefix)-maxInt(0, lipgloss.Width(item.Meta)+1)))
		}
		line := padBetween(left, strings.TrimSpace(item.Meta), maxInt(10, width-lipgloss.Width(prefix)))
		return []string{style.Render(prefix + line)}
	}
	left := title
	if subtitle := strings.TrimSpace(item.Subtitle); subtitle != "" {
		left = left + "  " + subtitle
	}
	lineOne := padBetween(left, strings.TrimSpace(item.Meta), maxInt(10, width-lipgloss.Width(prefix)))
	lines := []string{style.Render(prefix + lineOne)}
	if description != "" {
		descPrefix := strings.Repeat(" ", lipgloss.Width(prefix)) + "  "
		lines = append(lines, descStyle.Render(descPrefix+truncateForPicker(description, maxInt(10, width-lipgloss.Width(descPrefix)))))
	}
	return lines
}

func (m *Model) renderSessionPickerRows(entries []appstate.SessionEntry, err error, height int) []string {
	if err != nil {
		return []string{err.Error()}
	}
	if len(entries) == 0 {
		return []string{m.localize("Loading sessions…", "Загрузка сессий…")}
	}
	entryByPath := make(map[string]appstate.SessionEntry, len(entries))
	for _, entry := range entries {
		entryByPath[strings.TrimSpace(entry.Path)] = entry
	}
	items := m.filteredPaletteItems()
	selectedPath := ""
	if item, ok := m.selectedPaletteItem(); ok {
		selectedPath = strings.TrimSpace(item.Value)
	}
	rows := make([]string, 0, height)
	for _, item := range items {
		entry, ok := entryByPath[strings.TrimSpace(item.Value)]
		if !ok {
			continue
		}
		row := fmt.Sprintf("  %-16s  %-16s  %-8s  %s",
			formatSessionPickerTime(entry.Created),
			formatSessionPickerTime(entry.ModTime),
			truncateForPicker(firstNonEmpty(strings.TrimSpace(entry.Branch), "-"), 8),
			truncateForPicker(firstNonEmpty(strings.TrimSpace(entry.Preview), strings.TrimSpace(entry.RelPath), strings.TrimSpace(entry.Name)), maxInt(12, m.width-48)),
		)
		style := m.styles.body
		if strings.TrimSpace(entry.Path) == selectedPath {
			style = m.styles.selected
			row = "›" + strings.TrimPrefix(row, " ")
		}
		rows = append(rows, style.Render(row))
		if len(rows) == height {
			break
		}
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return rows
}

func padBetween(left string, right string, width int) string {
	left = normalizePickerText(left)
	right = normalizePickerText(right)
	if width <= 0 {
		if right == "" {
			return left
		}
		return left + " " + right
	}
	if right == "" {
		return truncateForPicker(left, width)
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	if leftWidth+2+rightWidth > width {
		left = truncateForPicker(left, maxInt(1, width-rightWidth-2))
		leftWidth = lipgloss.Width(left)
	}
	padding := width - leftWidth - rightWidth
	if padding < 2 {
		padding = 2
	}
	return left + strings.Repeat(" ", padding) + right
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

func formatSessionPickerTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func truncateForPicker(value string, width int) string {
	value = normalizePickerText(value)
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

func pickerDisplayText(display string, fallback string) string {
	display = normalizePickerText(display)
	if display != "" {
		return display
	}
	return normalizePickerText(fallback)
}

func normalizePickerText(value string) string {
	if strings.Contains(value, "\x1b[") {
		return value
	}
	return strings.TrimSpace(value)
}

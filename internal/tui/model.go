package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
	runtimeapi "github.com/Perdonus/lavilas-code/internal/runtime"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
	"github.com/Perdonus/lavilas-code/internal/tooling"
	"github.com/Perdonus/lavilas-code/internal/version"
)

const (
	layoutGap          = 1
	statusPaneMinWidth = 24
	statusPaneMaxWidth = 34
	inputPaneHeight    = 4
	palettePaneHeight  = 10
)

type taskFinishedMsg struct {
	Prompt      string
	Result      taskrun.Result
	SessionPath string
	Warn        error
	Err         error
}

type taskProgressMsg struct {
	Update taskrun.ProgressUpdate
}

type taskEventMsg struct {
	Inner tea.Msg
	Next  <-chan tea.Msg
}

type approvalRequestedMsg struct {
	Request    tooling.ApprovalRequest
	DecisionCh chan taskrun.ApprovalDecision
}

type sessionLoadedMsg struct {
	Entry    appstate.SessionEntry
	Meta     appstate.SessionMeta
	Messages []runtimeapi.Message
	Fork     bool
	Err      error
}

type paletteItemsMsg struct {
	Mode        PaletteMode
	Items       []PaletteItem
	Footer      string
	PushCurrent bool
	Err         error
}

type approvalPromptState struct {
	Request    tooling.ApprovalRequest
	DecisionCh chan taskrun.ApprovalDecision
}

type cwdSelection int

const (
	cwdSelectionSession cwdSelection = iota
	cwdSelectionCurrent
)

type cwdPromptState struct {
	Pending    sessionLoadedMsg
	CurrentCWD string
	SessionCWD string
	Selection  cwdSelection
}

type Model struct {
	state        State
	keys         KeyMap
	styles       styles
	viewport     viewport.Model
	input        textinput.Model
	paletteInput textinput.Model
	width        int
	height       int
	ready        bool
	statusWidth  int
	mainWidth    int
	layout       apphome.Layout
	options      taskrun.Options
	startup      StartupOptions
	history      []runtimeapi.Message
	catalog      PaletteCommandCatalog
	language     commandcatalog.CatalogLanguage
	approval     *approvalPromptState
	cwdPrompt    *cwdPromptState
}

func New() *Model {
	model, err := newModel(Options{})
	if err != nil {
		fallback := NewModel(DefaultState())
		return fallback
	}
	return model
}

func NewModel(state State) *Model {
	clonedState := state.clone()
	language := normalizeTUILanguage(clonedState.Language)
	styles := newStyles()

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = localizedTextTUI(language, "Send a message to the Go alpha session", "Введите сообщение в Go Lavilas alpha")
	input.TextStyle = styles.value
	input.PlaceholderStyle = styles.muted
	input.PromptStyle = styles.sectionTitle
	input.Cursor.Style = styles.paneTitle
	input.SetValue(clonedState.InputDraft)

	paletteInput := textinput.New()
	paletteInput.Placeholder = localizedTextTUI(language, "Type to filter items", "Введите запрос для фильтрации")
	paletteInput.TextStyle = styles.value
	paletteInput.PlaceholderStyle = styles.muted
	paletteInput.Cursor.Style = styles.paneTitle
	paletteInput.SetValue(clonedState.Palette.Query)

	model := &Model{
		state:        clonedState,
		keys:         DefaultKeyMap(),
		styles:       styles,
		viewport:     viewport.New(0, 0),
		input:        input,
		paletteInput: paletteInput,
		layout:       apphome.DefaultLayout(),
		catalog:      defaultPaletteCatalog(),
		language:     language,
	}
	model.state.Language = string(language)
	model.state.Palette.Context = normalizePaletteContextForLanguage(model.state.Palette.Context, language)
	if len(model.state.Palette.Items) == 0 {
		model.state.Palette.Items = model.rootPaletteItems()
	}
	model.refreshViewport()
	return model
}

func newModel(options Options) (*Model, error) {
	layout := apphome.DefaultLayout()
	config, _ := loadConfigOptional(layout.ConfigPath())
	settings, _ := loadSettingsOptional(layout.SettingsPath())
	language := normalizeTUILanguage(settings.Language)
	state := defaultStateForLanguage(language)
	state.Title = fmt.Sprintf("Go Lavilas %s", version.Version)
	state.Footer = buildFooter(settings, language)
	state.Model = fallback(options.TaskOptions.Model, config.EffectiveModel())
	state.Provider = fallback(options.TaskOptions.Provider, config.EffectiveProviderName())
	state.Profile = fallback(options.TaskOptions.Profile, config.ActiveProfileName())
	state.Reasoning = fallback(options.TaskOptions.ReasoningEffort, config.EffectiveReasoningEffort())
	state.CWD = fallback(strings.TrimSpace(options.TaskOptions.CWD), currentWorkingDirectory())
	state.Status = buildStatusItems(layout, state, 0)
	model := NewModel(state)
	model.layout = layout
	model.options = options.TaskOptions
	model.startup = options.Startup
	model.language = language
	if options.PaletteCatalog != nil {
		model.catalog = options.PaletteCatalog
	}
	model.state.Palette.Items = model.rootPaletteItems()
	return model, nil
}

func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.state.Palette.Visible {
		cmds = append(cmds, m.setFocus(FocusPalette))
	} else {
		cmds = append(cmds, m.setFocus(FocusInput))
	}
	if startupCmd := m.startupCmd(); startupCmd != nil {
		cmds = append(cmds, startupCmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.ready = true
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.refreshViewport()
		return m, nil
	case taskFinishedMsg:
		m.state.Busy = false
		m.state.LiveTurn = nil
		m.approval = nil
		if msg.Err != nil {
			m.appendTranscript("system", fmt.Sprintf("%s: %v", m.localize("Run failed", "Сбой запуска"), msg.Err))
			m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Last error", "Последняя ошибка"), msg.Err)
			m.updateStatus()
			return m, nil
		}
		m.applyTaskResult(msg)
		return m, nil
	case taskEventMsg:
		switch inner := msg.Inner.(type) {
		case taskProgressMsg:
			m.applyTaskProgress(inner.Update)
		case taskFinishedMsg:
			m.state.Busy = false
			m.state.LiveTurn = nil
			m.approval = nil
			if inner.Err != nil {
				m.appendTranscript("system", fmt.Sprintf("%s: %v", m.localize("Run failed", "Сбой запуска"), inner.Err))
				m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Last error", "Последняя ошибка"), inner.Err)
			} else {
				m.applyTaskResult(inner)
			}
			m.updateStatus()
		}
		if msg.Next != nil {
			return m, waitTaskEventCmd(msg.Next)
		}
		return m, nil
	case sessionLoadedMsg:
		if msg.Err != nil {
			m.appendTranscript("system", msg.Err.Error())
			m.state.Footer = msg.Err.Error()
			m.updateStatus()
			return m, nil
		}
		if m.beginCWDPrompt(msg) {
			return m, nil
		}
		m.applyLoadedSession(msg)
		return m, nil
	case approvalRequestedMsg:
		m.approval = &approvalPromptState{Request: msg.Request, DecisionCh: msg.DecisionCh}
		m.state.Footer = m.localize("Approval required", "Нужно подтверждение")
		return m, nil
	case paletteItemsMsg:
		if msg.Err != nil {
			m.appendTranscript("system", msg.Err.Error())
			m.state.Footer = msg.Err.Error()
			m.updateStatus()
			return m, nil
		}
		return m, m.applyPaletteScreen(msg.Mode, msg.Items, msg.Footer, msg.PushCurrent)
	case tea.KeyMsg:
		if m.approval != nil {
			return m, m.updateApproval(msg)
		}
		if m.cwdPrompt != nil {
			return m, m.updateCWDPrompt(msg)
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.TogglePalette):
			return m, m.togglePalette()
		}

		if m.state.Palette.Visible {
			return m, m.updatePalette(msg)
		}

		switch {
		case key.Matches(msg, m.keys.NextFocus):
			return m, m.cycleFocus(1)
		case key.Matches(msg, m.keys.PrevFocus):
			return m, m.cycleFocus(-1)
		case key.Matches(msg, m.keys.Submit) && m.state.Focus == FocusInput:
			return m, m.submitInput()
		}
	}

	switch m.state.Focus {
	case FocusInput:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.state.InputDraft = m.input.Value()
		return m, cmd
	case FocusTranscript:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	default:
		return m, nil
	}
}

func (m *Model) View() string {
	if !m.ready {
		return m.styles.app.Render(
			m.styles.pane.Width(60).Render(
				lipgloss.JoinVertical(
					lipgloss.Left,
					m.styles.paneTitle.Render(m.state.Title),
					m.styles.muted.Render(m.localize("Waiting for the first window size event...", "Ожидание первого события размера окна...")),
				),
			),
		)
	}

	statusPane := m.renderStatusPane()
	mainSections := make([]string, 0, 3)
	if m.approval != nil {
		mainSections = append(mainSections, m.renderApprovalPane())
	}
	if m.cwdPrompt != nil {
		mainSections = append(mainSections, m.renderCWDPromptPane())
	}
	if m.state.Palette.Visible {
		mainSections = append(mainSections, m.renderPalettePane())
	}
	mainSections = append(mainSections, m.renderTranscriptPane(), m.renderInputPane())
	mainColumn := lipgloss.JoinVertical(lipgloss.Left, mainSections...)

	if strings.TrimSpace(statusPane) == "" {
		return m.styles.app.Render(mainColumn)
	}
	return m.styles.app.Render(lipgloss.JoinHorizontal(lipgloss.Top, statusPane, strings.Repeat(" ", layoutGap)+mainColumn))
}

func (m *Model) State() State {
	return m.state.clone()
}

func (m *Model) SetState(state State) {
	m.state = state.clone()
	m.language = normalizeTUILanguage(m.state.Language)
	m.state.Language = string(m.language)
	m.state.Palette.Context = normalizePaletteContextForLanguage(m.state.Palette.Context, m.language)
	m.input.Placeholder = localizedTextTUI(m.language, "Send a message to the Go alpha session", "Введите сообщение в Go Lavilas alpha")
	m.paletteInput.Placeholder = localizedTextTUI(m.language, "Type to filter items", "Введите запрос для фильтрации")
	m.input.SetValue(m.state.InputDraft)
	m.paletteInput.SetValue(m.state.Palette.Query)
	if len(m.state.Palette.Items) == 0 {
		m.state.Palette.Items = defaultPaletteItemsForLanguage(m.language)
	}
	m.applyFocusState()
	m.resize()
	m.refreshViewport()
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	statusWidth := clampInt(m.width/4, statusPaneMinWidth, statusPaneMaxWidth)
	if m.width < 100 {
		statusWidth = clampInt(m.width/3, 22, 30)
	}
	mainWidth := m.width - statusWidth - layoutGap
	if mainWidth < 30 {
		mainWidth = maxInt(1, m.width-layoutGap)
		statusWidth = maxInt(0, m.width-mainWidth-layoutGap)
	}

	m.statusWidth = statusWidth
	m.mainWidth = mainWidth
	m.input.Width = maxInt(1, innerWidth(m.styles.pane, m.mainWidth)-2)
	m.paletteInput.Width = maxInt(1, innerWidth(m.styles.pane, m.mainWidth)-1)

	transcriptHeight := m.height - inputPaneHeight - layoutGap
	if m.state.Palette.Visible {
		transcriptHeight -= palettePaneHeight + layoutGap
	}
	transcriptHeight = maxInt(3, transcriptHeight)

	viewportWidth := maxInt(1, innerWidth(m.styles.pane, m.mainWidth))
	viewportHeight := maxInt(3, transcriptHeight-innerHeight(m.styles.pane, transcriptHeight)-1)
	m.viewport.Width = viewportWidth
	m.viewport.Height = viewportHeight
}

func (m *Model) renderStatusPane() string {
	if m.statusWidth <= 0 {
		return ""
	}
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusStatus).Width(m.statusWidth)
	content := []string{
		m.styles.paneTitle.Render(m.state.Title),
		m.styles.muted.Render(m.localize("Standalone alpha runtime", "Независимый alpha-контур")),
		"",
		m.styles.sectionTitle.Render(m.localize("Status", "Статус")),
		m.renderStatusItems(),
		"",
		m.styles.sectionTitle.Render(m.localize("Focus", "Фокус")),
		m.styles.value.Render(m.focusLabel()),
		m.styles.label.Render(m.localize("messages", "сообщения")) + " " + m.styles.value.Render(fmt.Sprintf("%d", len(m.state.Transcript))),
	}
	if m.state.Busy {
		content = append(content, m.styles.label.Render(m.localize("turn", "ход"))+" "+m.styles.busy.Render(m.localize("running", "выполняется")))
	}
	content = append(content,
		"",
		m.styles.sectionTitle.Render(m.localize("Keys", "Клавиши")),
		m.renderBindings(m.keys.ShortHelp()),
	)
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderTranscriptPane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusTranscript).Width(m.mainWidth)
	header := m.styles.paneTitle.Render(m.localize("Transcript", "Диалог")) + " " + m.styles.muted.Render(fmt.Sprintf(m.localize("%d entries", "%d записей"), len(m.state.Transcript)))
	body := m.viewport.View()
	if strings.TrimSpace(body) == "" {
		body = m.styles.muted.Render(m.localize("Transcript viewport is empty.", "Диалог пока пуст."))
	}
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m *Model) renderInputPane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusInput).Width(m.mainWidth)
	footer := m.state.Footer
	if strings.TrimSpace(footer) == "" {
		footer = m.localize("Enter submit · Ctrl+P palette", "Enter отправить · Ctrl+P палитра")
	}
	if m.state.Busy {
		footer = m.localize("Running turn...", "Выполняется ход...")
	}
	content := []string{
		m.styles.paneTitle.Render(m.localize("Input", "Ввод")),
		m.input.View(),
		m.styles.muted.Render(footer),
	}
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderApprovalPane() string {
	if m.approval == nil {
		return ""
	}
	request := m.approval.Request
	pane := m.styles.paneActive.Width(m.mainWidth)
	content := []string{
		m.styles.paneTitle.Render(m.localize("Approval Required", "Нужно подтверждение")),
		m.styles.label.Render(m.localize("tool", "инструмент")) + " " + m.styles.value.Render(request.Name),
	}
	if summary := strings.TrimSpace(request.Summary); summary != "" {
		content = append(content, m.styles.label.Render(m.localize("summary", "сводка"))+" "+m.styles.value.Render(summary))
	}
	if details := strings.TrimSpace(request.Details); details != "" {
		content = append(content, m.styles.label.Render(m.localize("details", "детали"))+" "+m.styles.value.Render(details))
	}
	if reason := strings.TrimSpace(request.Reason); reason != "" {
		content = append(content, m.styles.label.Render(m.localize("reason", "причина"))+" "+m.styles.value.Render(reason))
	}
	content = append(content,
		"",
		m.styles.helpKey.Render("Y")+" "+m.styles.helpDesc.Render(m.localize("allow once", "разрешить один раз")),
		m.styles.helpKey.Render("A")+" "+m.styles.helpDesc.Render(m.localize("allow for session", "разрешить на сессию")),
		m.styles.helpKey.Render("N")+" "+m.styles.helpDesc.Render(m.localize("deny", "запретить")),
	)
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderCWDPromptPane() string {
	if m.cwdPrompt == nil {
		return ""
	}
	prompt := m.cwdPrompt
	pane := m.styles.paneActive.Width(m.mainWidth)
	sessionSelected := prompt.Selection == cwdSelectionSession
	currentSelected := prompt.Selection == cwdSelectionCurrent
	lines := []string{
		m.styles.paneTitle.Render(m.localize("Choose Working Directory", "Выберите рабочую папку")),
		m.styles.muted.Render(m.localize("Session = last directory from the saved session", "Сессия = последняя папка из сохранённой сессии")),
		m.styles.muted.Render(m.localize("Current = the directory of the active chat", "Текущая = папка активного чата")),
		"",
		m.cwdPromptRow(prompt.SessionCWD, sessionSelected, m.localize("Use session directory", "Использовать папку из сессии")),
		m.cwdPromptRow(prompt.CurrentCWD, currentSelected, m.localize("Use current directory", "Использовать текущую папку")),
		"",
		m.styles.helpKey.Render("Enter") + " " + m.styles.helpDesc.Render(m.localize("continue", "продолжить")),
		m.styles.helpKey.Render("Esc") + " " + m.styles.helpDesc.Render(m.localize("use session directory", "использовать папку сессии")),
	}
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) cwdPromptRow(path string, selected bool, label string) string {
	prefix := "[ ]"
	if selected {
		prefix = "[x]"
	}
	style := m.styles.body
	if selected {
		style = m.styles.selected
	}
	return style.Render(fmt.Sprintf("%s %s (%s)", prefix, label, fallback(strings.TrimSpace(path), localizedUnsetTUI(m.language))))
}

func (m *Model) renderPalettePane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusPalette).Width(m.mainWidth)
	items := m.filteredPaletteItems()
	content := []string{
		m.styles.paneTitle.Render(m.paletteTitle()),
		m.paletteInput.View(),
	}
	if len(items) == 0 {
		content = append(content, m.styles.muted.Render(m.localize("No items match the current filter.", "Ничего не найдено по текущему фильтру.")))
	} else {
		entryWidth := maxInt(1, innerWidth(m.styles.pane, m.mainWidth))
		selected := clampInt(m.state.Palette.Selected, 0, maxInt(0, len(items)-1))
		if backItem, rest, hasBack := splitPaletteBackItem(items); hasBack {
			content = append(content, m.renderPaletteEntry(backItem, entryWidth, selected == 0))
			limit := maxInt(1, m.paletteListLimit()-1)
			start, end := paletteVisibleRange(len(rest), selected-1, limit)
			for index := start; index < end; index++ {
				content = append(content, m.renderPaletteEntry(rest[index], entryWidth, selected == index+1))
			}
		} else {
			start, end := paletteVisibleRange(len(items), selected, m.paletteListLimit())
			for index := start; index < end; index++ {
				content = append(content, m.renderPaletteEntry(items[index], entryWidth, selected == index))
			}
		}
	}
	content = append(content, m.styles.muted.Render(m.paletteHint()))
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderPaletteEntry(item PaletteItem, width int, selected bool) string {
	entry := item.Title
	if description := strings.TrimSpace(item.Description); description != "" {
		entry += "  " + description
	}
	style := m.styles.body.Width(width)
	if selected {
		style = m.styles.selected.Width(width)
	}
	return style.Render(entry)
}

func (m *Model) renderStatusItems() string {
	if len(m.state.Status) == 0 {
		return m.styles.muted.Render(m.localize("No status items yet.", "Пока нет элементов статуса."))
	}
	lines := make([]string, 0, len(m.state.Status))
	for _, item := range m.state.Status {
		label := item.Label
		if strings.TrimSpace(label) == "" {
			label = m.localize("Item", "Элемент")
		}
		lines = append(lines, m.styles.label.Render(strings.ToLower(label))+" "+m.styles.value.Render(item.Value))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderBindings(bindings []key.Binding) string {
	lines := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		if help.Key == "" && help.Desc == "" {
			continue
		}
		lines = append(lines, m.styles.helpKey.Render(help.Key)+" "+m.styles.helpDesc.Render(help.Desc))
	}
	if len(lines) == 0 {
		return m.styles.muted.Render(m.localize("No key bindings exposed yet.", "Пока нет доступных клавиш."))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderTranscriptContent(width int) string {
	if len(m.state.Transcript) == 0 && m.state.LiveTurn == nil {
		return m.styles.muted.Render(m.localize("Transcript is empty.", "Диалог пуст."))
	}
	bodyStyle := m.styles.body.Width(maxInt(1, width))
	blocks := make([]string, 0, len(m.state.Transcript)+1)
	for _, entry := range m.state.Transcript {
		role := strings.TrimSpace(entry.Role)
		if role == "" {
			role = "event"
		}
		body := strings.TrimSpace(entry.Body)
		if body == "" {
			body = "..."
		}
		blocks = append(blocks, lipgloss.JoinVertical(
			lipgloss.Left,
			m.roleStyle(role).Render(strings.ToUpper(m.displayRole(role))),
			bodyStyle.Render(body),
		))
	}
	if m.state.LiveTurn != nil {
		live := m.state.LiveTurn
		notes := make([]string, 0, len(live.Notes)+1)
		if strings.TrimSpace(live.Prompt) != "" {
			blocks = append(blocks, lipgloss.JoinVertical(
				lipgloss.Left,
				m.roleStyle("user").Render(strings.ToUpper(m.localize("user", "пользователь"))),
				bodyStyle.Render(strings.TrimSpace(live.Prompt)),
			))
		}
		if strings.TrimSpace(live.AssistantText) != "" {
			blocks = append(blocks, lipgloss.JoinVertical(
				lipgloss.Left,
				m.roleStyle("assistant").Render(strings.ToUpper(m.localize("assistant", "ассистент"))),
				bodyStyle.Render(strings.TrimSpace(live.AssistantText)),
			))
		}
		for _, call := range live.ToolCalls {
			line := fmt.Sprintf("%s %s", fallback(call.ID, "<id>"), call.Function.Name)
			if args := strings.TrimSpace(call.Function.ArgumentsString()); args != "" {
				line += "\n" + args
			}
			notes = append(notes, line)
		}
		notes = append(notes, live.Notes...)
		if len(notes) > 0 {
			blocks = append(blocks, lipgloss.JoinVertical(
				lipgloss.Left,
				m.roleStyle("tool").Render(strings.ToUpper(m.localize("tool", "инструмент"))),
				bodyStyle.Render(strings.Join(notes, "\n\n")),
			))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func (m *Model) roleStyle(role string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return m.styles.roleUser
	case "assistant":
		return m.styles.roleAssistant
	case "system":
		return m.styles.roleSystem
	default:
		return m.styles.roleTool
	}
}

func (m *Model) displayRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return m.localize("user", "пользователь")
	case "assistant":
		return m.localize("assistant", "ассистент")
	case "system":
		return m.localize("system", "система")
	case "tool":
		return m.localize("tool", "инструмент")
	default:
		return fallback(strings.TrimSpace(role), m.localize("event", "событие"))
	}
}

func (m *Model) refreshViewport() {
	width := m.viewport.Width
	if width <= 0 {
		width = 72
	}
	stickToBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscriptContent(width))
	if stickToBottom || len(m.state.Transcript) <= 2 {
		m.viewport.GotoBottom()
	}
}

func (m *Model) submitInput() tea.Cmd {
	if m.state.Busy {
		return nil
	}
	draft := strings.TrimSpace(m.input.Value())
	if draft == "" {
		return nil
	}
	m.input.Reset()
	m.state.InputDraft = ""

	prefix := commandPrefix(m.layout.SettingsPath())
	if strings.HasPrefix(draft, prefix) {
		return m.dispatchSlash(draft, prefix)
	}

	m.state.Busy = true
	m.state.LiveTurn = &LiveTurnState{Prompt: draft}
	m.state.Footer = m.localize("Running turn...", "Выполняется ход...")
	m.updateStatus()
	m.refreshViewport()
	return m.runPromptCmd(draft)
}

func (m *Model) runPromptCmd(prompt string) tea.Cmd {
	history := cloneRuntimeMessages(m.history)
	options := m.options
	options.Prompt = prompt
	options.History = history
	existingSessionPath := m.state.SessionPath
	layout := m.layout

	return startTaskCmd(func(eventCh chan<- tea.Msg) {
		progress := newTaskProgressBridge(eventCh)
		defer progress.Close()
		options.OnProgress = progress.Handle
		options.OnApproval = func(ctx context.Context, request tooling.ApprovalRequest) (taskrun.ApprovalDecision, error) {
			decisionCh := make(chan taskrun.ApprovalDecision, 1)
			select {
			case <-ctx.Done():
				return taskrun.ApprovalDecisionDeny, ctx.Err()
			case eventCh <- approvalRequestedMsg{Request: request, DecisionCh: decisionCh}:
			}
			select {
			case <-ctx.Done():
				return taskrun.ApprovalDecisionDeny, ctx.Err()
			case decision := <-decisionCh:
				return decision, nil
			}
		}
		result, err := taskrun.Run(context.Background(), options)
		if err != nil {
			eventCh <- taskFinishedMsg{Prompt: prompt, Err: err}
			return
		}
		sessionPath, warn := persistTurn(layout, existingSessionPath, result)
		eventCh <- taskFinishedMsg{Prompt: prompt, Result: result, SessionPath: sessionPath, Warn: warn}
	})
}

func (m *Model) dispatchSlash(line string, prefix string) tea.Cmd {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if len(fields) == 0 {
		m.appendTranscript("system", m.localize("Empty slash command.", "Пустая слэш-команда."))
		return nil
	}
	command, ok := m.resolveSlashCommand(fields[0])
	if !ok {
		m.appendTranscript("system", fmt.Sprintf("%s: %s%s", m.localize("Unknown slash command", "Неизвестная слэш-команда"), prefix, fields[0]))
		return nil
	}
	return m.executePaletteCommand(command)
}

func (m *Model) applyTaskResult(msg taskFinishedMsg) {
	history := cloneRuntimeMessages(msg.Result.FullHistory())
	m.history = history
	m.state.LiveTurn = nil
	m.state.Transcript = transcriptFromMessages(history, m.language)
	m.state.SessionPath = msg.SessionPath
	m.approval = nil
	m.state.Model = fallback(msg.Result.Model, m.state.Model)
	m.state.Provider = fallback(msg.Result.ProviderName, m.state.Provider)
	m.state.Profile = fallback(msg.Result.Profile, m.state.Profile)
	m.state.Reasoning = fallback(msg.Result.Reasoning, m.state.Reasoning)
	m.state.CWD = fallback(strings.TrimSpace(msg.Result.CWD), m.state.CWD)
	m.options.CWD = m.state.CWD
	m.state.Footer = m.localize("Turn complete", "Ход завершён")
	if msg.Warn != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Turn complete with warning", "Ход завершён с предупреждением"), msg.Warn)
	}
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) applyLoadedSession(msg sessionLoadedMsg) {
	m.history = cloneRuntimeMessages(msg.Messages)
	m.state.LiveTurn = nil
	m.state.Transcript = transcriptFromMessages(msg.Messages, m.language)
	m.approval = nil
	m.state.Model = msg.Meta.Model
	m.state.Provider = msg.Meta.Provider
	m.state.Profile = msg.Meta.Profile
	m.state.Reasoning = msg.Meta.Reasoning
	m.state.CWD = fallback(strings.TrimSpace(msg.Meta.CWD), m.state.CWD)
	m.options.CWD = m.state.CWD
	if msg.Fork {
		m.state.SessionPath = ""
		m.state.Footer = fmt.Sprintf("%s %s", m.localize("Forked from", "Ответвлено от"), msg.Entry.RelPath)
	} else {
		m.state.SessionPath = msg.Entry.Path
		m.state.Footer = fmt.Sprintf("%s %s", m.localize("Loaded", "Загружено"), msg.Entry.RelPath)
	}
	m.state.Palette = PaletteState{
		Mode:    PaletteModeRoot,
		Items:   defaultPaletteItemsForLanguage(m.language),
		Context: defaultPaletteContextForLanguage(m.language),
	}
	m.paletteInput.Reset()
	m.state.Focus = FocusInput
	m.applyFocusState()
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) beginCWDPrompt(msg sessionLoadedMsg) bool {
	sessionCWD := strings.TrimSpace(msg.Meta.CWD)
	currentCWD := strings.TrimSpace(m.effectiveWorkingDirectory())
	if sessionCWD == "" || currentCWD == "" || sessionCWD == currentCWD {
		return false
	}
	m.cwdPrompt = &cwdPromptState{
		Pending:    msg,
		CurrentCWD: currentCWD,
		SessionCWD: sessionCWD,
		Selection:  cwdSelectionSession,
	}
	m.state.Footer = m.localize("Choose the directory for this session", "Выберите папку для этой сессии")
	return true
}

func (m *Model) updateCWDPrompt(keyMsg tea.KeyMsg) tea.Cmd {
	if m.cwdPrompt == nil {
		return nil
	}
	switch {
	case key.Matches(keyMsg, m.keys.Close):
		return m.confirmCWDPrompt(cwdSelectionSession)
	case key.Matches(keyMsg, m.keys.Up):
		m.cwdPrompt.Selection = cwdSelectionSession
		return nil
	case key.Matches(keyMsg, m.keys.Down):
		m.cwdPrompt.Selection = cwdSelectionCurrent
		return nil
	case key.Matches(keyMsg, m.keys.Submit):
		return m.confirmCWDPrompt(m.cwdPrompt.Selection)
	}
	switch strings.ToLower(strings.TrimSpace(keyMsg.String())) {
	case "1":
		return m.confirmCWDPrompt(cwdSelectionSession)
	case "2":
		return m.confirmCWDPrompt(cwdSelectionCurrent)
	}
	return nil
}

func (m *Model) confirmCWDPrompt(selection cwdSelection) tea.Cmd {
	if m.cwdPrompt == nil {
		return nil
	}
	prompt := m.cwdPrompt
	m.cwdPrompt = nil
	msg := prompt.Pending
	switch selection {
	case cwdSelectionCurrent:
		msg.Meta.CWD = prompt.CurrentCWD
	default:
		msg.Meta.CWD = prompt.SessionCWD
	}
	m.applyLoadedSession(msg)
	return nil
}

func (m *Model) openPaletteMode(mode PaletteMode, pushCurrent bool) tea.Cmd {
	return m.applyPaletteScreen(mode, m.paletteItemsForMode(mode), "", pushCurrent)
}

func (m *Model) applyPaletteScreen(mode PaletteMode, items []PaletteItem, footer string, pushCurrent bool) tea.Cmd {
	context := m.nextPaletteContext(mode, pushCurrent)
	if pushCurrent && m.state.Palette.Visible {
		m.pushPaletteView()
	}
	m.state.Palette.Visible = true
	m.state.Palette.Mode = mode
	m.state.Palette.Query = ""
	m.state.Palette.Selected = 0
	m.state.Palette.Context = context
	m.state.Palette.Items = m.decoratePaletteItems(mode, items)
	m.state.Palette.SelectedToken = ""
	m.paletteInput.Reset()
	if strings.TrimSpace(footer) != "" {
		m.state.Footer = footer
	}
	m.syncPaletteSelection()
	m.resize()
	return m.setFocus(FocusPalette)
}

func (m *Model) pushPaletteView() {
	m.state.Palette.Stack = append(m.state.Palette.Stack, PaletteView{
		Mode:          m.state.Palette.Mode,
		Query:         m.state.Palette.Query,
		Items:         clonePaletteItems(m.state.Palette.Items),
		Selected:      m.state.Palette.Selected,
		Context:       m.state.Palette.Context,
		SelectedToken: m.state.Palette.SelectedToken,
		Footer:        m.state.Footer,
	})
}

func (m *Model) navigatePaletteBack() tea.Cmd {
	if !m.state.Palette.Visible {
		return nil
	}
	if len(m.state.Palette.Stack) == 0 {
		return m.closePalette()
	}
	last := m.state.Palette.Stack[len(m.state.Palette.Stack)-1]
	m.state.Palette.Stack = m.state.Palette.Stack[:len(m.state.Palette.Stack)-1]
	m.state.Palette.Visible = true
	m.state.Palette.Mode = last.Mode
	m.state.Palette.Query = last.Query
	m.state.Palette.Items = clonePaletteItems(last.Items)
	m.state.Palette.Selected = last.Selected
	m.state.Palette.Context = last.Context
	m.state.Palette.SelectedToken = last.SelectedToken
	m.paletteInput.SetValue(last.Query)
	m.state.Footer = last.Footer
	m.syncPaletteSelection()
	m.resize()
	return m.setFocus(FocusPalette)
}

func (m *Model) paletteItemsForMode(mode PaletteMode) []PaletteItem {
	switch mode {
	case PaletteModeSettings:
		return m.settingsPaletteItems()
	case PaletteModeModel:
		return m.modelPaletteItems()
	case PaletteModeProfiles:
		return m.profilesPaletteItems()
	case PaletteModeProviders:
		return m.providersPaletteItems()
	default:
		return m.rootPaletteItems()
	}
}

func (m *Model) settingsPaletteItems() []PaletteItem {
	settings, _ := loadSettingsOptional(m.layout.SettingsPath())
	summary := settings.Summary()
	return []PaletteItem{
		{Key: "model", Title: m.localize("Model", "Модель"), Description: m.localize("Inspect active model", "Открыть активную модель"), Aliases: []string{"/model", "/модель"}, Keywords: []string{"reasoning", "provider", "profile", "модель", "провайдер"}},
		{Key: "profiles", Title: m.localize("Profiles", "Профили"), Description: m.localize("Inspect configured profiles", "Открыть профили"), Aliases: []string{"/profiles", "/профили"}, Keywords: []string{"accounts", "profile", "config", "аккаунты", "профиль"}},
		{Key: "providers", Title: m.localize("Providers", "Провайдеры"), Description: m.localize("Inspect configured providers", "Открыть провайдеры"), Aliases: []string{"/providers", "/провайдеры"}, Keywords: []string{"api", "wire_api", "base_url", "провайдер"}},
		{Key: "settings.language", Title: m.localize("Language", "Язык"), Description: fallback(summary.Language, localizedUnsetTUI(m.language)), Keywords: []string{"locale", "translation", "язык"}},
		{Key: "settings.command_prefix", Title: m.localize("Command Prefix", "Префикс команд"), Description: fallback(summary.CommandPrefix, localizedUnsetTUI(m.language)), Keywords: []string{"slash", "prefix", "commands", "префикс"}},
		{Key: "settings.hidden_commands", Title: m.localize("Hidden Commands", "Скрытые команды"), Description: fmt.Sprintf("%d", len(summary.HiddenCommands)), Keywords: []string{"visibility", "commands", "скрытые"}},
	}
}

func (m *Model) modelPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	return []PaletteItem{
		{Key: "model.value", Title: m.localize("Model", "Модель"), Description: fallback(m.state.Model, config.EffectiveModel()), Keywords: []string{"active", "slug", "model", "модель"}},
		{Key: "model.provider", Title: m.localize("Provider", "Провайдер"), Description: fallback(m.state.Provider, config.EffectiveProviderName()), Keywords: []string{"api", "provider", "провайдер"}},
		{Key: "model.profile", Title: m.localize("Profile", "Профиль"), Description: fallback(m.state.Profile, config.ActiveProfileName()), Keywords: []string{"account", "profile", "профиль"}},
		{Key: "model.reasoning", Title: m.localize("Reasoning", "Размышления"), Description: fallback(m.state.Reasoning, config.EffectiveReasoningEffort()), Keywords: []string{"effort", "thinking", "reasoning", "размышления"}},
	}
}

func (m *Model) profilesPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	items := []PaletteItem{}
	if len(config.Profiles) == 0 {
		return append(items, PaletteItem{Key: "profiles.empty", Title: m.localize("No Profiles", "Нет профилей"), Description: m.localize("No configured profiles found", "Профили не настроены")})
	}
	for _, profile := range config.Profiles {
		description := localizedTextTUI(
			m.language,
			"model=%s provider=%s reasoning=%s",
			"модель=%s провайдер=%s размышления=%s",
			fallback(profile.Model, localizedUnsetTUI(m.language)),
			fallback(profile.Provider, localizedUnsetTUI(m.language)),
			fallback(profile.ReasoningEffort, localizedUnsetTUI(m.language)),
		)
		if profile.Name == config.ActiveProfileName() {
			description += m.localize(" · active", " · активен")
		}
		items = append(items, PaletteItem{Key: "profiles.entry", Title: profile.Name, Description: description, Keywords: []string{profile.Provider, profile.Model, profile.ReasoningEffort}})
	}
	return items
}

func (m *Model) providersPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	items := []PaletteItem{}
	if len(config.ModelProviders) == 0 {
		return append(items, PaletteItem{Key: "providers.empty", Title: m.localize("No Providers", "Нет провайдеров"), Description: m.localize("No configured providers found", "Провайдеры не настроены")})
	}
	for _, provider := range config.ModelProviders {
		items = append(items, PaletteItem{
			Key:         "providers.entry",
			Title:       provider.Name,
			Description: fmt.Sprintf("base_url=%s wire_api=%s", fallback(provider.BaseURL, localizedUnsetTUI(m.language)), fallback(provider.WireAPI, "chat_completions")),
			Keywords:    []string{provider.BaseURL, provider.WireAPI},
		})
	}
	return items
}

func (m *Model) paletteBackItem() PaletteItem {
	context := normalizePaletteContextForLanguage(m.state.Palette.Context, m.language)
	return PaletteItem{Key: "__back", Title: context.BackTitle, Description: context.BackDescription, Keywords: []string{"back", "close", "return", "назад", "закрыть"}}
}

func (m *Model) paletteHint() string {
	return normalizePaletteContextForLanguage(m.state.Palette.Context, m.language).BackHint
}

func (m *Model) rootPaletteItems() []PaletteItem {
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	return m.catalog.RootItems(m.language, "")
}

func (m *Model) decoratePaletteItems(mode PaletteMode, items []PaletteItem) []PaletteItem {
	decorated := clonePaletteItems(items)
	if mode == PaletteModeRoot {
		return decorated
	}
	if len(decorated) > 0 && decorated[0].Key == "__back" {
		decorated[0] = m.paletteBackItem()
		return decorated
	}
	return append([]PaletteItem{m.paletteBackItem()}, decorated...)
}

func (m *Model) nextPaletteContext(mode PaletteMode, pushCurrent bool) PaletteContext {
	context := defaultPaletteContextForLanguage(m.language)
	if m.state.Palette.Visible {
		context.ReturnFocus = normalizePaletteContextForLanguage(m.state.Palette.Context, m.language).ReturnFocus
	} else {
		context.ReturnFocus = normalizeFocus(m.state.Focus)
		if context.ReturnFocus == FocusPalette {
			context.ReturnFocus = FocusInput
		}
	}
	if !m.state.Palette.Visible || !pushCurrent {
		return context
	}
	context.BackTitle, context.BackDescription = paletteBackCopyForMode(m.state.Palette.Mode, m.language)
	context.BackHint = m.localize("Enter select · Esc back", "Enter выбрать · Esc назад")
	return context
}

func paletteBackCopyForMode(mode PaletteMode, language commandcatalog.CatalogLanguage) (string, string) {
	localize := func(english string, russian string) string {
		if language == commandcatalog.CatalogLanguageRussian {
			return russian
		}
		return english
	}
	switch mode {
	case PaletteModeSettings:
		return localize("Back to Settings", "Назад к настройкам"), localize("Return to settings", "Вернуться к настройкам")
	case PaletteModeRoot:
		return localize("Back to Palette", "Назад к палитре"), localize("Return to command palette", "Вернуться к палитре команд")
	case PaletteModeModel:
		return localize("Back to Model", "Назад к модели"), localize("Return to model details", "Вернуться к модели")
	case PaletteModeProfiles:
		return localize("Back to Profiles", "Назад к профилям"), localize("Return to profiles", "Вернуться к профилям")
	case PaletteModeProviders:
		return localize("Back to Providers", "Назад к провайдерам"), localize("Return to providers", "Вернуться к провайдерам")
	case PaletteModeResume:
		return localize("Back to Resume", "Назад к продолжению"), localize("Return to saved sessions", "Вернуться к сохранённым сессиям")
	case PaletteModeFork:
		return localize("Back to Fork", "Назад к ответвлению"), localize("Return to fork browser", "Вернуться к списку ответвлений")
	default:
		return localize("Back to Chat", "Назад в чат"), localize("Return to transcript", "Вернуться к диалогу")
	}
}

func (m *Model) paletteListLimit() int {
	return maxInt(3, innerHeight(m.styles.pane, palettePaneHeight)-3)
}

func (m *Model) palettePageSize() int {
	return maxInt(1, m.paletteListLimit()-1)
}

type scoredPaletteItem struct {
	Item  PaletteItem
	Score int
	Index int
}

func splitPaletteBackItem(items []PaletteItem) (PaletteItem, []PaletteItem, bool) {
	if len(items) == 0 || items[0].Key != "__back" {
		return PaletteItem{}, items, false
	}
	return items[0], items[1:], true
}

func paletteVisibleRange(total int, selected int, limit int) (int, int) {
	if total <= 0 {
		return 0, 0
	}
	limit = clampInt(limit, 1, total)
	selected = clampInt(selected, 0, total-1)
	start := selected - limit/2
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > total {
		end = total
		start = maxInt(0, end-limit)
	}
	return start, end
}

func tokenizePaletteQuery(query string) []string {
	normalized := normalizePaletteSearchText(query)
	if normalized == "" {
		return nil
	}
	return strings.Fields(normalized)
}

func normalizePaletteSearchText(value string) string {
	replacer := strings.NewReplacer("/", " ", "-", " ", "_", " ", ".", " ", ":", " ", "=", " ", "·", " ", ",", " ")
	return strings.ToLower(strings.TrimSpace(replacer.Replace(value)))
}

func scorePaletteItem(item PaletteItem, tokens []string) (int, bool) {
	title := normalizePaletteSearchText(item.Title)
	description := normalizePaletteSearchText(item.Description)
	value := normalizePaletteSearchText(item.Value)
	aliases := make([]string, 0, len(item.Aliases))
	for _, alias := range item.Aliases {
		aliases = append(aliases, normalizePaletteSearchText(alias))
	}
	keywords := make([]string, 0, len(item.Keywords))
	for _, keyword := range item.Keywords {
		keywords = append(keywords, normalizePaletteSearchText(keyword))
	}
	score := 0
	for _, token := range tokens {
		tokenScore, ok := scorePaletteToken(token, title, description, value, aliases, keywords)
		if !ok {
			return 0, false
		}
		score += tokenScore
	}
	return score, true
}

func scorePaletteToken(token string, title string, description string, value string, aliases []string, keywords []string) (int, bool) {
	switch {
	case title == token || listContainsExact(aliases, token):
		return 0, true
	case strings.HasPrefix(title, token) || listHasPrefix(aliases, token):
		return 1, true
	case strings.Contains(title, token) || listContainsToken(keywords, token):
		return 2, true
	case strings.Contains(value, token) || listHasPrefix(keywords, token):
		return 3, true
	case strings.Contains(description, token) || listContainsToken(aliases, token):
		return 4, true
	default:
		return 0, false
	}
}

func listContainsExact(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func listHasPrefix(items []string, needle string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, needle) {
			return true
		}
	}
	return false
}

func listContainsToken(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(item, needle) {
			return true
		}
	}
	return false
}

func paletteItemToken(item PaletteItem) string {
	return item.Key + "\x00" + item.Title + "\x00" + item.Value
}

func (m *Model) executePaletteCommand(command PaletteCommandSpec) tea.Cmd {
	switch command.Action {
	case PaletteActionOpenPalette:
		if m.state.Palette.Visible {
			return nil
		}
		return m.openPaletteMode(PaletteModeRoot, false)
	case PaletteActionNewSession:
		m.resetConversation()
		if m.state.Palette.Visible {
			return m.closePalette()
		}
		return nil
	case PaletteActionResumeLatest:
		return m.loadLatestSessionCmd(false)
	case PaletteActionForkLatest:
		return m.loadLatestSessionCmd(true)
	case PaletteActionBrowseResume:
		return m.openSessionsPalette(false, m.state.Palette.Visible)
	case PaletteActionBrowseFork:
		return m.openSessionsPalette(true, m.state.Palette.Visible)
	case PaletteActionShowStatus:
		m.appendTranscript("system", m.statusSummary())
		if m.state.Palette.Visible {
			return m.closePalette()
		}
		return nil
	case PaletteActionShowHelp:
		m.appendTranscript("system", m.helpText())
		m.state.Footer = m.localize("Help opened", "Открыта помощь")
		if m.state.Palette.Visible {
			return m.closePalette()
		}
		return nil
	case PaletteActionQuit:
		return tea.Quit
	case PaletteActionOpenMode:
		return m.openPaletteMode(command.Mode, m.state.Palette.Visible)
	default:
		return nil
	}
}

func (m *Model) togglePalette() tea.Cmd {
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	return m.openPaletteMode(PaletteModeRoot, false)
}

func (m *Model) closePalette() tea.Cmd {
	returnFocus := normalizePaletteContextForLanguage(m.state.Palette.Context, m.language).ReturnFocus
	if returnFocus == FocusPalette {
		returnFocus = FocusInput
	}
	m.state.Palette.Visible = false
	m.state.Palette.Mode = PaletteModeRoot
	m.state.Palette.Query = ""
	m.state.Palette.Selected = 0
	m.state.Palette.Items = m.rootPaletteItems()
	m.state.Palette.Context = defaultPaletteContextForLanguage(m.language)
	m.state.Palette.SelectedToken = ""
	m.state.Palette.Stack = nil
	m.paletteInput.Reset()
	m.resize()
	return m.setFocus(returnFocus)
}

func (m *Model) updatePalette(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, m.keys.Close):
			return m.navigatePaletteBack()
		case key.Matches(keyMsg, m.keys.Up):
			m.movePaletteSelection(-1)
			return nil
		case key.Matches(keyMsg, m.keys.Down):
			m.movePaletteSelection(1)
			return nil
		case key.Matches(keyMsg, m.keys.PageUp):
			m.movePaletteSelection(-m.palettePageSize())
			return nil
		case key.Matches(keyMsg, m.keys.PageDown):
			m.movePaletteSelection(m.palettePageSize())
			return nil
		case key.Matches(keyMsg, m.keys.Submit):
			return m.activatePaletteSelection()
		}
	}

	var cmd tea.Cmd
	previousToken := m.state.Palette.SelectedToken
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.state.Palette.Query = m.paletteInput.Value()
	if m.state.Palette.Mode == PaletteModeRoot {
		m.state.Palette.Items = m.rootPaletteItemsForQuery(m.state.Palette.Query)
	}
	m.state.Palette.SelectedToken = previousToken
	m.syncPaletteSelection()
	return cmd
}

func (m *Model) activatePaletteSelection() tea.Cmd {
	item, ok := m.selectedPaletteItem()
	if !ok {
		return nil
	}

	switch m.state.Palette.Mode {
	case PaletteModeResume:
		return m.loadSessionCmd(item.Value, false)
	case PaletteModeFork:
		return m.loadSessionCmd(item.Value, true)
	case PaletteModeSettings:
		switch item.Key {
		case "settings.language":
			return m.toggleSettingsLanguage()
		case "settings.command_prefix":
			return m.cycleCommandPrefix()
		}
		return nil
	default:
		if item.Key != "__back" {
			if command, ok := m.catalog.LookupByKey(item.Key); ok {
				return m.executePaletteCommand(command)
			}
		}
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		}
		return nil
	}
}

func (m *Model) selectedPaletteItem() (PaletteItem, bool) {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		return PaletteItem{}, false
	}
	index := clampInt(m.state.Palette.Selected, 0, len(items)-1)
	return items[index], true
}

func (m *Model) filteredPaletteItems() []PaletteItem {
	items := m.state.Palette.Items
	if len(items) == 0 {
		items = m.rootPaletteItems()
	}
	if m.state.Palette.Mode == PaletteModeRoot {
		items = m.rootPaletteItemsForQuery(m.state.Palette.Query)
	}
	tokens := tokenizePaletteQuery(m.state.Palette.Query)
	if len(tokens) == 0 {
		return items
	}
	filtered := make([]PaletteItem, 0, len(items))
	scored := make([]scoredPaletteItem, 0, len(items))
	for _, item := range items {
		if item.Key == "__back" {
			filtered = append(filtered, item)
			continue
		}
		if score, ok := scorePaletteItem(item, tokens); ok {
			scored = append(scored, scoredPaletteItem{Item: item, Score: score, Index: len(scored)})
		}
	}
	sort.SliceStable(scored, func(left, right int) bool {
		if scored[left].Score == scored[right].Score {
			if scored[left].Index == scored[right].Index {
				return scored[left].Item.Title < scored[right].Item.Title
			}
			return scored[left].Index < scored[right].Index
		}
		return scored[left].Score < scored[right].Score
	})
	for _, item := range scored {
		filtered = append(filtered, item.Item)
	}
	return filtered
}

func (m *Model) movePaletteSelection(delta int) {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		m.state.Palette.Selected = 0
		m.state.Palette.SelectedToken = ""
		return
	}
	m.state.Palette.Selected = clampInt(m.state.Palette.Selected+delta, 0, len(items)-1)
	m.state.Palette.SelectedToken = paletteItemToken(items[m.state.Palette.Selected])
}

func (m *Model) syncPaletteSelection() {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		m.state.Palette.Selected = 0
		m.state.Palette.SelectedToken = ""
		return
	}
	if token := strings.TrimSpace(m.state.Palette.SelectedToken); token != "" {
		for index, item := range items {
			if paletteItemToken(item) == token {
				m.state.Palette.Selected = index
				m.state.Palette.SelectedToken = token
				return
			}
		}
	}
	m.state.Palette.Selected = clampInt(m.state.Palette.Selected, 0, len(items)-1)
	m.state.Palette.SelectedToken = paletteItemToken(items[m.state.Palette.Selected])
}

func (m *Model) cycleFocus(delta int) tea.Cmd {
	order := []PaneFocus{FocusStatus, FocusTranscript, FocusInput}
	current := 0
	for index, pane := range order {
		if pane == m.state.Focus {
			current = index
			break
		}
	}
	current = (current + delta + len(order)) % len(order)
	return m.setFocus(order[current])
}

func (m *Model) setFocus(next PaneFocus) tea.Cmd {
	m.state.Focus = normalizeFocus(next)
	return m.applyFocusState()
}

func (m *Model) applyFocusState() tea.Cmd {
	if m.state.Palette.Visible {
		m.state.Focus = FocusPalette
	}
	var cmds []tea.Cmd
	switch m.state.Focus {
	case FocusInput:
		cmds = append(cmds, m.input.Focus())
		m.paletteInput.Blur()
	case FocusPalette:
		m.input.Blur()
		cmds = append(cmds, m.paletteInput.Focus())
	default:
		m.input.Blur()
		m.paletteInput.Blur()
	}
	return tea.Batch(cmds...)
}

func (m *Model) focusLabel() string {
	switch m.state.Focus {
	case FocusStatus:
		return m.localize("status pane", "панель статуса")
	case FocusTranscript:
		return m.localize("transcript viewport", "область диалога")
	case FocusPalette:
		return m.localize("command palette", "палитра команд")
	default:
		return m.localize("input area", "область ввода")
	}
}

func (m *Model) paletteTitle() string {
	switch m.state.Palette.Mode {
	case PaletteModeResume:
		return m.localize("Resume Session", "Продолжить сессию")
	case PaletteModeFork:
		return m.localize("Fork Session", "Ответвить сессию")
	case PaletteModeModel:
		return m.localize("Model", "Модель")
	case PaletteModeProfiles:
		return m.localize("Profiles", "Профили")
	case PaletteModeProviders:
		return m.localize("Providers", "Провайдеры")
	case PaletteModeSettings:
		return m.localize("Settings", "Настройки")
	default:
		return m.localize("Command Palette", "Палитра команд")
	}
}

func (m *Model) loadLatestSessionCmd(fork bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 1)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return sessionLoadedMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
			}
			return sessionLoadedMsg{Err: err}
		}
		if len(entries) == 0 {
			return sessionLoadedMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
		}
		entry := entries[0]
		meta, messages, err := appstate.LoadSession(entry.Path)
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) loadSessionCmd(path string, fork bool) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(path) == "" {
			return sessionLoadedMsg{Err: errors.New(m.localize("session path is empty", "путь к сессии пуст"))}
		}
		entryInfo, err := os.Stat(path)
		if err != nil {
			return sessionLoadedMsg{Err: err}
		}
		entry := buildSessionEntry(m.layout.SessionsDir(), path, entryInfo)
		meta, messages, err := appstate.LoadSession(path)
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) openSessionsPalette(fork bool, pushCurrent bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 20)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
			}
			return paletteItemsMsg{Err: err}
		}
		if len(entries) == 0 {
			return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
		}
		entries = m.filterSessionEntries(entries)
		if len(entries) == 0 {
			return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
		}
		items := make([]PaletteItem, 0, len(entries))
		for _, entry := range entries {
			description := entry.RelPath
			if m.startup.ShowAll && strings.TrimSpace(entry.CWD) != "" {
				description = fmt.Sprintf("%s · %s", entry.RelPath, entry.CWD)
			}
			items = append(items, PaletteItem{
				Key:         "session",
				Title:       entry.Name,
				Description: description,
				Value:       entry.Path,
			})
		}
		mode := PaletteModeResume
		footer := m.localize("Select a saved session to resume", "Выберите сохранённую сессию для продолжения")
		if fork {
			mode = PaletteModeFork
			footer = m.localize("Select a saved session to fork", "Выберите сохранённую сессию для ответвления")
		}
		return paletteItemsMsg{Mode: mode, Items: items, Footer: footer, PushCurrent: pushCurrent}
	}
}

func (m *Model) filterSessionEntries(entries []appstate.SessionEntry) []appstate.SessionEntry {
	if m.startup.ShowAll {
		return entries
	}
	currentCWD := strings.TrimSpace(m.effectiveWorkingDirectory())
	if currentCWD == "" {
		return entries
	}
	filtered := make([]appstate.SessionEntry, 0, len(entries))
	for _, entry := range entries {
		entryCWD := strings.TrimSpace(entry.CWD)
		if entryCWD == "" || entryCWD == currentCWD {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (m *Model) startupCmd() tea.Cmd {
	switch m.startup.Mode {
	case StartupModeResumePicker:
		return m.openSessionsPalette(false, false)
	case StartupModeForkPicker:
		return m.openSessionsPalette(true, false)
	case StartupModeResumeLatest:
		return m.loadLatestSessionCmd(false)
	case StartupModeForkLatest:
		return m.loadLatestSessionCmd(true)
	case StartupModeResumePath:
		if strings.TrimSpace(m.startup.SessionPath) == "" {
			return nil
		}
		return m.loadSessionCmd(m.startup.SessionPath, false)
	case StartupModeForkPath:
		if strings.TrimSpace(m.startup.SessionPath) == "" {
			return nil
		}
		return m.loadSessionCmd(m.startup.SessionPath, true)
	case StartupModeModel:
		return m.openPaletteMode(PaletteModeModel, false)
	case StartupModeProfiles:
		return m.openPaletteMode(PaletteModeProfiles, false)
	case StartupModeProviders:
		return m.openPaletteMode(PaletteModeProviders, false)
	case StartupModeSettings:
		return m.openPaletteMode(PaletteModeSettings, false)
	default:
		return nil
	}
}

func (m *Model) resetConversation() {
	m.history = nil
	m.state.LiveTurn = nil
	m.state.Transcript = defaultStateForLanguage(m.language).Transcript
	m.state.SessionPath = ""
	m.state.Footer = m.localize("Started a new session", "Начата новая сессия")
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) updateStatus() {
	m.state.Status = buildStatusItems(m.layout, m.state, len(m.history))
}

func (m *Model) appendTranscript(role string, body string) {
	m.state.Transcript = append(m.state.Transcript, TranscriptEntry{Role: role, Body: body})
	m.refreshViewport()
}

func (m *Model) helpText() string {
	return m.catalog.HelpText(commandPrefix(m.layout.SettingsPath()), m.language, m.state.Palette.Query)
}

func (m *Model) statusSummary() string {
	lines := []string{m.localize("Runtime status:", "Состояние рантайма:")}
	for _, item := range m.state.Status {
		lines = append(lines, fmt.Sprintf("- %s: %s", item.Label, item.Value))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) modelSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	lines := []string{m.localize("Model summary:", "Сводка по модели:"), fmt.Sprintf("- %s: %s", m.localize("model", "модель"), fallback(m.state.Model, config.EffectiveModel())), fmt.Sprintf("- %s: %s", m.localize("provider", "провайдер"), fallback(m.state.Provider, config.EffectiveProviderName())), fmt.Sprintf("- %s: %s", m.localize("profile", "профиль"), fallback(m.state.Profile, config.ActiveProfileName())), fmt.Sprintf("- %s: %s", m.localize("reasoning", "размышления"), fallback(m.state.Reasoning, config.EffectiveReasoningEffort()))}
	return strings.Join(lines, "\n")
}

func (m *Model) profilesSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	if len(config.Profiles) == 0 {
		return m.localize("Profiles: none", "Профили: нет")
	}
	lines := []string{m.localize("Profiles:", "Профили:")}
	for _, profile := range config.Profiles {
		suffix := ""
		if profile.Name == config.ActiveProfileName() {
			suffix = m.localize(" (active)", " (активен)")
		}
		lines = append(lines, localizedTextTUI(
			m.language,
			"- %s%s model=%s provider=%s reasoning=%s",
			"- %s%s модель=%s провайдер=%s размышления=%s",
			profile.Name,
			suffix,
			fallback(profile.Model, localizedUnsetTUI(m.language)),
			fallback(profile.Provider, localizedUnsetTUI(m.language)),
			fallback(profile.ReasoningEffort, localizedUnsetTUI(m.language)),
		))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) providersSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	if len(config.ModelProviders) == 0 {
		return m.localize("Providers: none", "Провайдеры: нет")
	}
	lines := []string{m.localize("Providers:", "Провайдеры:")}
	for _, provider := range config.ModelProviders {
		lines = append(lines, fmt.Sprintf("- %s base_url=%s wire_api=%s", provider.Name, fallback(provider.BaseURL, localizedUnsetTUI(m.language)), fallback(provider.WireAPI, "chat_completions")))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) settingsSummary() string {
	settings, _ := loadSettingsOptional(m.layout.SettingsPath())
	summary := settings.Summary()
	lines := []string{
		m.localize("Settings:", "Настройки:"),
		fmt.Sprintf("- %s: %s", m.localize("language", "язык"), fallback(summary.Language, localizedUnsetTUI(m.language))),
		fmt.Sprintf("- %s: %s", m.localize("command prefix", "префикс команд"), fallback(summary.CommandPrefix, localizedUnsetTUI(m.language))),
		fmt.Sprintf("- %s: %d", m.localize("hidden commands", "скрытые команды"), len(summary.HiddenCommands)),
	}
	return strings.Join(lines, "\n")
}

func buildStatusItems(layout apphome.Layout, state State, historyLen int) []StatusItem {
	language := normalizeTUILanguage(state.Language)
	localize := func(english string, russian string) string {
		if language == commandcatalog.CatalogLanguageRussian {
			return russian
		}
		return english
	}
	sessionValue := localize("fresh", "новая")
	if strings.TrimSpace(state.SessionPath) != "" {
		sessionValue = filepath.Base(state.SessionPath)
	}
	return []StatusItem{
		{Label: localize("Model", "Модель"), Value: fallback(state.Model, localizedUnsetTUI(language))},
		{Label: localize("Provider", "Провайдер"), Value: fallback(state.Provider, localizedUnsetTUI(language))},
		{Label: localize("Profile", "Профиль"), Value: fallback(state.Profile, localizedUnsetTUI(language))},
		{Label: localize("Reasoning", "Размышления"), Value: fallback(state.Reasoning, localizedUnsetTUI(language))},
		{Label: localize("CWD", "Папка"), Value: fallback(state.CWD, localizedUnsetTUI(language))},
		{Label: localize("Session", "Сессия"), Value: sessionValue},
		{Label: localize("History", "История"), Value: fmt.Sprintf("%d", historyLen)},
		{Label: localize("Home", "Каталог"), Value: layout.CodexHome()},
	}
}

func buildFooter(settings appstate.Settings, language commandcatalog.CatalogLanguage) string {
	return localizedTextTUI(language, "Enter submit · Ctrl+P palette · %shelp slash help", "Enter отправить · Ctrl+P палитра · %sпомощь слэш-команды", commandPrefixFromSettings(settings))
}

func commandPrefix(settingsPath string) string {
	settings, _ := loadSettingsOptional(settingsPath)
	return commandPrefixFromSettings(settings)
}

func commandPrefixFromSettings(settings appstate.Settings) string {
	prefix := settings.CommandPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "/"
	}
	return prefix
}

func transcriptFromMessages(messages []runtimeapi.Message, language commandcatalog.CatalogLanguage) []TranscriptEntry {
	if len(messages) == 0 {
		return nil
	}
	entries := make([]TranscriptEntry, 0, len(messages)*2)
	for _, message := range messages {
		if text := strings.TrimSpace(message.Text()); text != "" {
			entries = append(entries, TranscriptEntry{Role: string(message.Role), Body: text})
		}
		for _, call := range message.ToolCalls {
			body := fmt.Sprintf("%s %s %s", localizedTextTUI(language, "tool call", "вызов инструмента"), fallback(call.ID, "<id>"), call.Function.Name)
			arguments := strings.TrimSpace(call.Function.ArgumentsString())
			if arguments != "" {
				body += "\n" + arguments
			}
			entries = append(entries, TranscriptEntry{Role: "tool", Body: body})
		}
	}
	return entries
}

func persistTurn(layout apphome.Layout, sessionPath string, result taskrun.Result) (string, error) {
	history := cloneRuntimeMessages(result.FullHistory())
	if strings.TrimSpace(sessionPath) == "" {
		entry, err := appstate.CreateSession(layout.SessionsDir(), appstate.SessionMeta{
			Model:     result.Model,
			Provider:  result.ProviderName,
			Profile:   result.Profile,
			Reasoning: result.Reasoning,
			CWD:       result.CWD,
		}, history)
		if err != nil {
			return "", err
		}
		return entry.Path, nil
	}

	if err := appstate.AppendSessionHistory(sessionPath, appstate.SessionMeta{
		Model:     result.Model,
		Provider:  result.ProviderName,
		Profile:   result.Profile,
		Reasoning: result.Reasoning,
		CWD:       result.CWD,
	}, history); err != nil {
		return sessionPath, err
	}
	return sessionPath, nil
}

func cloneRuntimeMessages(messages []runtimeapi.Message) []runtimeapi.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]runtimeapi.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

func hasPersistableMessage(message runtimeapi.Message) bool {
	return strings.TrimSpace(message.Text()) != "" || len(message.ToolCalls) > 0 || strings.TrimSpace(message.Refusal) != ""
}

func fallback(value string, fallbackValue string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallbackValue
	}
	return value
}

func (m *Model) localize(english string, russian string) string {
	if m.language == commandcatalog.CatalogLanguageRussian {
		return russian
	}
	return english
}

func localizedTextTUI(language commandcatalog.CatalogLanguage, english string, russian string, args ...any) string {
	template := english
	if language == commandcatalog.CatalogLanguageRussian {
		template = russian
	}
	return fmt.Sprintf(template, args...)
}

func localizedUnsetTUI(language commandcatalog.CatalogLanguage) string {
	return localizedTextTUI(language, "<unset>", "<не задано>")
}

func normalizeTUILanguage(value string) commandcatalog.CatalogLanguage {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "ru", "русский":
		return commandcatalog.CatalogLanguageRussian
	case "en", "english":
		return commandcatalog.CatalogLanguageEnglish
	default:
		return commandcatalog.CatalogLanguageEnglish
	}
}

func defaultStateForLanguage(language commandcatalog.CatalogLanguage) State {
	state := DefaultState()
	state.Language = string(language)
	state.Title = "Go Lavilas"
	state.Transcript = []TranscriptEntry{
		{Role: "system", Body: localizedTextTUI(language, "Go Lavilas alpha TUI loaded.", "Загружен Go Lavilas alpha TUI.")},
		{Role: "assistant", Body: localizedTextTUI(language, "Type a prompt and press Enter. Ctrl+P opens the command palette.", "Введите запрос и нажмите Enter. Ctrl+P открывает палитру команд.")},
	}
	state.Palette.Items = defaultPaletteItemsForLanguage(language)
	state.Palette.Context = defaultPaletteContextForLanguage(language)
	state.Footer = localizedTextTUI(language, "Enter submit · Ctrl+P palette · Tab focus · Esc close", "Enter отправить · Ctrl+P палитра · Tab фокус · Esc закрыть")
	return state
}

func startTaskCmd(run func(chan<- tea.Msg)) tea.Cmd {
	eventCh := make(chan tea.Msg, 128)
	go func() {
		defer close(eventCh)
		run(eventCh)
	}()
	return waitTaskEventCmd(eventCh)
}

const taskProgressSnapshotThrottle = 40 * time.Millisecond

type taskProgressBridge struct {
	eventCh chan<- tea.Msg
	done    chan struct{}
	wg      sync.WaitGroup
	mu      sync.Mutex
	latest  *taskrun.ProgressUpdate
}

func newTaskProgressBridge(eventCh chan<- tea.Msg) *taskProgressBridge {
	bridge := &taskProgressBridge{
		eventCh: eventCh,
		done:    make(chan struct{}),
	}
	bridge.wg.Add(1)
	go bridge.loop()
	return bridge
}

func (b *taskProgressBridge) Handle(update taskrun.ProgressUpdate) {
	if update.Kind != taskrun.ProgressKindAssistantSnapshot {
		b.eventCh <- taskProgressMsg{Update: update}
		return
	}
	copy := update
	b.mu.Lock()
	b.latest = &copy
	b.mu.Unlock()
}

func (b *taskProgressBridge) Close() {
	close(b.done)
	b.wg.Wait()
}

func (b *taskProgressBridge) loop() {
	defer b.wg.Done()
	ticker := time.NewTicker(taskProgressSnapshotThrottle)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.flush()
		case <-b.done:
			b.flush()
			return
		}
	}
}

func (b *taskProgressBridge) flush() {
	b.mu.Lock()
	latest := b.latest
	b.latest = nil
	b.mu.Unlock()
	if latest == nil {
		return
	}
	b.eventCh <- taskProgressMsg{Update: *latest}
}

func waitTaskEventCmd(eventCh <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		message, ok := <-eventCh
		if !ok {
			return nil
		}
		return taskEventMsg{Inner: message, Next: eventCh}
	}
}

func (m *Model) applyTaskProgress(update taskrun.ProgressUpdate) {
	if m.state.LiveTurn == nil {
		m.state.LiveTurn = &LiveTurnState{}
	}
	live := m.state.LiveTurn
	if update.Round > 0 {
		live.Round = update.Round
	}
	if strings.TrimSpace(update.Prompt) != "" {
		live.Prompt = update.Prompt
	}
	switch update.Kind {
	case taskrun.ProgressKindTurnStarted:
		if update.Round <= 1 && strings.TrimSpace(update.Prompt) != "" {
			live.Prompt = update.Prompt
		}
		m.state.Footer = m.localize("Running turn...", "Выполняется ход...")
	case taskrun.ProgressKindAssistantSnapshot:
		live.AssistantText = update.Snapshot.Text
		live.ToolCalls = cloneRuntimeToolCalls(update.Snapshot.ToolCalls)
	case taskrun.ProgressKindToolPlanned:
		if update.ToolPlan != nil {
			live.Notes = append(live.Notes, fmt.Sprintf("%s %d / %d", m.localize("Tool batches:", "Пакеты инструментов:"), update.ToolPlan.Summary.BatchCount, update.ToolPlan.Summary.CallCount))
		}
	case taskrun.ProgressKindApprovalRequired:
		if update.ApprovalRequest != nil {
			live.Notes = append(live.Notes, fmt.Sprintf("%s %s (%s)", m.localize("Approval status for", "Статус подтверждения для"), update.ApprovalRequest.Name, localizedToolStatusTUI(m.language, string(update.ApprovalRequest.Status))))
		}
	case taskrun.ProgressKindToolResult:
		if update.ToolResult != nil {
			live.Notes = append(live.Notes, fmt.Sprintf("%s %s -> %s", m.localize("Tool", "Инструмент"), update.ToolResult.Name, localizedToolStatusTUI(m.language, string(update.ToolResult.Status))))
		}
	case taskrun.ProgressKindRetryScheduled:
		if update.RetryAfter > 0 {
			live.Notes = append(live.Notes, fmt.Sprintf("%s %s", m.localize("Retry after", "Повтор через"), update.RetryAfter))
		}
	}
	m.state.LiveTurn = live
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) updateApproval(keyMsg tea.KeyMsg) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(keyMsg.String())) {
	case "y":
		return m.resolveApproval(taskrun.ApprovalDecisionApprove)
	case "a":
		return m.resolveApproval(taskrun.ApprovalDecisionApproveForSession)
	case "n":
		return m.resolveApproval(taskrun.ApprovalDecisionDeny)
	}
	if key.Matches(keyMsg, m.keys.Close) {
		return m.resolveApproval(taskrun.ApprovalDecisionDeny)
	}
	return nil
}

func (m *Model) resolveApproval(decision taskrun.ApprovalDecision) tea.Cmd {
	if m.approval == nil {
		return nil
	}
	request := m.approval.Request
	decisionCh := m.approval.DecisionCh
	m.approval = nil
	select {
	case decisionCh <- decision:
	default:
	}
	switch decision {
	case taskrun.ApprovalDecisionApproveForSession:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Approved for session", "Разрешено на сессию"), request.Name)
	case taskrun.ApprovalDecisionApprove:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Approved", "Разрешено"), request.Name)
	default:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Denied", "Запрещено"), request.Name)
	}
	m.refreshViewport()
	return nil
}

func localizedToolStatusTUI(language commandcatalog.CatalogLanguage, status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded":
		return localizedTextTUI(language, "succeeded", "успешно")
	case "failed":
		return localizedTextTUI(language, "failed", "ошибка")
	case "approval_required":
		return localizedTextTUI(language, "approval required", "нужно подтверждение")
	case "denied":
		return localizedTextTUI(language, "denied", "запрещено")
	default:
		return fallback(strings.TrimSpace(status), localizedTextTUI(language, "unknown", "неизвестно"))
	}
}

func (m *Model) toggleSettingsLanguage() tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	next := "ru"
	if normalizeTUILanguage(settings.Language) == commandcatalog.CatalogLanguageRussian {
		next = "en"
	}
	settings.SetLanguage(next)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.language = normalizeTUILanguage(next)
	m.state.Language = string(m.language)
	m.state.Title = "Go Lavilas"
	m.input.Placeholder = localizedTextTUI(m.language, "Send a message to the Go alpha session", "Введите сообщение в Go Lavilas alpha")
	m.paletteInput.Placeholder = localizedTextTUI(m.language, "Type to filter items", "Введите запрос для фильтрации")
	m.state.Palette.Items = m.settingsPaletteItems()
	m.state.Palette.Context = normalizePaletteContextForLanguage(m.state.Palette.Context, m.language)
	m.state.Footer = localizedTextTUI(m.language, "Language updated", "Язык обновлён")
	m.syncPaletteSelection()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) cycleCommandPrefix() tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	current := commandPrefixFromSettings(settings)
	next := "."
	if current == "." {
		next = "/"
	}
	settings.SetCommandPrefix(next)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Palette.Items = m.settingsPaletteItems()
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Command prefix updated", "Префикс команд обновлён"), next)
	m.syncPaletteSelection()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) rootPaletteItemsForQuery(query string) []PaletteItem {
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	return m.catalog.RootItems(m.language, query)
}

func (m *Model) resolveSlashCommand(name string) (PaletteCommandSpec, bool) {
	needle := strings.TrimSpace(name)
	if needle == "" {
		return PaletteCommandSpec{}, false
	}
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	return m.catalog.LookupBySlash(needle)
}

func buildSessionEntry(root string, path string, info os.FileInfo) appstate.SessionEntry {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = filepath.Base(path)
	}
	name := filepath.Base(path)
	return appstate.SessionEntry{
		ID:      strings.TrimSuffix(name, filepath.Ext(name)),
		Name:    name,
		Path:    path,
		RelPath: relPath,
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
}

func loadConfigOptional(path string) (appstate.Config, error) {
	config, err := appstate.LoadConfig(path)
	if err == nil {
		return config, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return appstate.Config{}, nil
	}
	return appstate.Config{}, err
}

func loadSettingsOptional(path string) (appstate.Settings, error) {
	settings, err := appstate.LoadSettings(path)
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return appstate.Settings{}, nil
	}
	return appstate.Settings{}, err
}

func currentWorkingDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func (m *Model) effectiveWorkingDirectory() string {
	if cwd := strings.TrimSpace(m.options.CWD); cwd != "" {
		return cwd
	}
	if cwd := strings.TrimSpace(m.state.CWD); cwd != "" {
		return cwd
	}
	return currentWorkingDirectory()
}

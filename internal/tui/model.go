package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strconv"
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
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
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
	busyTickInterval   = 200 * time.Millisecond
)

var busySpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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

type busyTickMsg struct{}

type approvalRequestedMsg struct {
	Request    tooling.ApprovalRequest
	DecisionCh chan taskrun.ApprovalDecision
}

type sessionLoadedMsg struct {
	Entry      appstate.SessionEntry
	Meta       appstate.SessionMeta
	Messages   []runtimeapi.Message
	Fork       bool
	StartFresh bool
	Err        error
}

type paletteItemsMsg struct {
	Mode        PaletteMode
	Items       []PaletteItem
	Footer      string
	PushCurrent bool
	StartFresh  bool
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

type sessionSortKey string

const (
	sessionSortUpdated sessionSortKey = "updated"
	sessionSortCreated sessionSortKey = "created"
)

type cwdPromptState struct {
	Pending    sessionLoadedMsg
	CurrentCWD string
	SessionCWD string
	Selection  cwdSelection
}

type Model struct {
	state                         State
	keys                          KeyMap
	styles                        styles
	viewport                      viewport.Model
	input                         textinput.Model
	paletteInput                  textinput.Model
	width                         int
	height                        int
	ready                         bool
	statusWidth                   int
	mainWidth                     int
	layout                        apphome.Layout
	options                       taskrun.Options
	startup                       StartupOptions
	history                       []runtimeapi.Message
	catalog                       PaletteCommandCatalog
	language                      commandcatalog.CatalogLanguage
	approval                      *approvalPromptState
	cwdPrompt                     *cwdPromptState
	formPrompt                    *formPromptState
	modelSettingsNavigationOrigin ModelSettingsNavigationOrigin
	sessionSort                   sessionSortKey
	sessionPaletteStartup         bool
	approvalStore                 *taskrun.ApprovalSessionStore
	customizationColorTarget      popupColorTarget
	customizationFormatTarget     popupFormatTarget
	taskCancel                    context.CancelFunc
	inputHistory                  *inputHistory
}

var composerPlaceholders = map[commandcatalog.CatalogLanguage][]string{
	commandcatalog.CatalogLanguageRussian: {
		"Объясни этот кодовый проект",
		"Кратко перескажи недавние коммиты",
		"Реализуй {feature}",
		"Найди и исправь баг в @filename",
		"Напиши тесты для @filename",
		"Улучши документацию в @filename",
		"Запусти /review для моих текущих изменений",
		"Используй /skills, чтобы показать доступные навыки",
	},
	commandcatalog.CatalogLanguageEnglish: {
		"Explain this codebase",
		"Summarize recent commits",
		"Implement {feature}",
		"Find and fix a bug in @filename",
		"Write tests for @filename",
		"Improve documentation in @filename",
		"Run /review on my current changes",
		"Use /skills to show available skills",
	},
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
	input.Prompt = "› "
	input.Placeholder = composerPlaceholder(language)
	input.TextStyle = styles.value
	input.PlaceholderStyle = styles.muted
	input.PromptStyle = styles.sectionTitle
	input.Cursor.Style = styles.paneTitle
	input.SetValue(clonedState.InputDraft)

	paletteInput := textinput.New()
	paletteInput.Prompt = "⌕  "
	paletteInput.Placeholder = localizedTextTUI(language, "Type to filter items", "Введите запрос для фильтрации")
	paletteInput.TextStyle = styles.value
	paletteInput.PlaceholderStyle = styles.muted
	paletteInput.Cursor.Style = styles.paneTitle
	paletteInput.SetValue(clonedState.Palette.Query)

	model := &Model{
		state:                         clonedState,
		keys:                          DefaultKeyMap(),
		styles:                        styles,
		viewport:                      viewport.New(0, 0),
		input:                         input,
		paletteInput:                  paletteInput,
		layout:                        apphome.DefaultLayout(),
		catalog:                       defaultPaletteCatalog(),
		language:                      language,
		sessionSort:                   sessionSortUpdated,
		approvalStore:                 taskrun.NewApprovalSessionStore(),
		modelSettingsNavigationOrigin: ModelSettingsNavigationOriginCommand,
		inputHistory:                  newInputHistory(apphome.DefaultLayout()),
	}
	model.state.Language = string(language)
	model.state.Palette.Context = normalizePaletteContextForLanguage(model.state.Palette.Context, language)
	if len(model.state.Palette.Items) == 0 {
		model.state.Palette.Items = model.rootPaletteItems()
	}
	model.reloadStyleSettings()
	model.refreshViewport()
	return model
}

func newModel(options Options) (*Model, error) {
	layout := apphome.DefaultLayout()
	config, _ := loadConfigOptional(layout.ConfigPath())
	settings, _ := loadSettingsOptional(layout.SettingsPath())
	if tooling.IsZeroToolPolicy(options.TaskOptions.ToolPolicy) {
		options.TaskOptions.ToolPolicy = settings.ToolPolicy
	}
	options.TaskOptions.ToolPolicy = tooling.NormalizeToolPolicy(options.TaskOptions.ToolPolicy)
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
	model.inputHistory = newInputHistory(layout)
	model.options = options.TaskOptions
	model.startup = options.Startup
	model.language = language
	if options.PaletteCatalog != nil {
		model.catalog = options.PaletteCatalog
	}
	model.applyStyleSettings(settings)
	model.state.Palette.Items = model.rootPaletteItems()
	return model, nil
}

func composerPlaceholder(language commandcatalog.CatalogLanguage) string {
	language = normalizeTUILanguage(string(language))
	samples := composerPlaceholders[language]
	if len(samples) == 0 {
		samples = composerPlaceholders[commandcatalog.CatalogLanguageEnglish]
	}
	if len(samples) == 0 {
		return ""
	}
	index := int(time.Now().UnixNano()%int64(len(samples)))
	if index < 0 || index >= len(samples) {
		index = 0
	}
	return samples[index]
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
		m.flushLiveTurnEntries()
		m.state.LiveTurn = nil
		m.taskCancel = nil
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
			m.flushLiveTurnEntries()
			m.state.LiveTurn = nil
			m.taskCancel = nil
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
	case busyTickMsg:
		if !m.state.Busy || m.state.LiveTurn == nil {
			return m, nil
		}
		m.state.LiveTurn.SpinnerFrame++
		m.refreshViewport()
		return m, busyTickCmd()
	case sessionLoadedMsg:
		if msg.Err != nil {
			m.appendTranscript("system", msg.Err.Error())
			m.state.Footer = msg.Err.Error()
			m.updateStatus()
			return m, nil
		}
		if msg.StartFresh {
			m.resetConversation()
			return m, m.consumeStartupPrompt()
		}
		if m.beginCWDPrompt(msg) {
			return m, nil
		}
		m.applyLoadedSession(msg)
		return m, m.consumeStartupPrompt()
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
		if msg.StartFresh {
			m.resetConversation()
			return m, m.consumeStartupPrompt()
		}
		return m, m.applyPaletteScreen(msg.Mode, msg.Items, msg.Footer, msg.PushCurrent)
	case tea.KeyMsg:
		if m.approval != nil {
			return m, m.updateApproval(msg)
		}
		if m.cwdPrompt != nil {
			return m, m.updateCWDPrompt(msg)
		}
		if m.formPrompt != nil {
			return m, m.updateFormPrompt(msg)
		}
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.TogglePalette):
			return m, m.togglePalette()
		}

		if m.state.Palette.Visible && m.state.Focus == FocusPalette {
			return m, m.updatePalette(msg)
		}
		if !m.isInlineCommandPaletteActive() {
			switch {
			case key.Matches(msg, m.keys.Close) && m.state.Busy && m.taskCancel != nil:
				m.taskCancel()
				m.state.Footer = m.localize("Cancellation requested", "Запрошена остановка хода")
				return m, nil
			case key.Matches(msg, m.keys.PageUp):
				if m.scrollTranscriptPage(-1) {
					return m, nil
				}
			case key.Matches(msg, m.keys.PageDown):
				if m.scrollTranscriptPage(1) {
					return m, nil
				}
			}
		}

		switch {
		case key.Matches(msg, m.keys.NextFocus):
			return m, m.cycleFocus(1)
		case key.Matches(msg, m.keys.PrevFocus):
			return m, m.cycleFocus(-1)
		case key.Matches(msg, m.keys.Submit) && m.state.Focus == FocusInput && !m.isInlineCommandPaletteActive():
			return m, m.submitInput()
		}
	case tea.MouseMsg:
		if m.handleTranscriptMouse(msg) {
			return m, nil
		}
	}

	switch m.state.Focus {
	case FocusInput:
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if m.handleInputHistoryNavigation(keyMsg) {
				return m, nil
			}
			if cmd := m.updateInlineCommandPalette(keyMsg); cmd != nil {
				return m, cmd
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.state.InputDraft = m.input.Value()
		if m.inputHistory != nil {
			m.inputHistory.syncDraft(m.state.InputDraft)
		}
		m.syncInlinePaletteWithDraft()
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

	return m.renderCodexScreen()
}

func (m *Model) State() State {
	return m.state.clone()
}

func (m *Model) SetState(state State) {
	m.state = state.clone()
	m.language = normalizeTUILanguage(m.state.Language)
	m.state.Language = string(m.language)
	m.state.Palette.Context = normalizePaletteContextForLanguage(m.state.Palette.Context, m.language)
	m.reloadStyleSettings()
	m.input.Placeholder = composerPlaceholder(m.language)
	m.paletteInput.Prompt = "⌕  "
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

	m.statusWidth = 0
	m.mainWidth = maxInt(1, m.width)
	m.input.Width = maxInt(1, m.mainWidth-2)
	m.paletteInput.Width = maxInt(1, m.mainWidth-2)
	m.viewport.Width = maxInt(1, m.mainWidth)
	m.viewport.Height = maxInt(1, m.height-6)
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
	return m.renderComposerPane()
}

func (m *Model) renderApprovalPane() string {
	if m.approval == nil {
		return ""
	}
	request := m.approval.Request
	pane := m.styles.paneActive.Width(m.mainWidth)
	allowOnce, allowSession, denyLabel := m.approvalActionLabels(request)
	content := []string{
		m.styles.paneTitle.Render(m.approvalTitle(request)),
		m.styles.label.Render(m.localize("tool", "инструмент")) + " " + m.styles.value.Render(request.Name),
	}
	if hint := m.approvalKindHint(request); hint != "" {
		content = append(content, m.styles.muted.Render(hint))
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
	if len(request.Metadata.RequestedWritableRoots) > 0 {
		content = append(content, m.styles.label.Render(m.localize("writable roots", "доступные для записи пути"))+" "+m.styles.value.Render(strings.Join(request.Metadata.RequestedWritableRoots, ", ")))
	}
	if cwd := strings.TrimSpace(request.Metadata.WorkingDirectory); cwd != "" {
		content = append(content, m.styles.label.Render("cwd")+" "+m.styles.value.Render(cwd))
	}
	if targets := m.approvalResourcePreview(request); targets != "" {
		content = append(content, m.styles.label.Render(m.localize("targets", "цели"))+" "+m.styles.value.Render(targets))
	}
	content = append(content,
		"",
		m.styles.helpKey.Render("Y")+" "+m.styles.helpDesc.Render(allowOnce),
		m.styles.helpKey.Render("A")+" "+m.styles.helpDesc.Render(allowSession),
		m.styles.helpKey.Render("N")+" "+m.styles.helpDesc.Render(denyLabel),
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
	if len(m.state.Transcript) == 0 && len(m.state.Transient) == 0 && m.state.LiveTurn == nil {
		return ""
	}
	blocks := make([]string, 0, len(m.state.Transcript)+len(m.state.Transient)+4)
	for _, entry := range m.state.Transcript {
		if block := m.renderTranscriptEntry(entry, width); strings.TrimSpace(block) != "" {
			blocks = append(blocks, block)
		}
	}
	for _, entry := range m.state.Transient {
		if block := m.renderTranscriptEntry(entry, width); strings.TrimSpace(block) != "" {
			blocks = append(blocks, block)
		}
	}
	if m.state.LiveTurn != nil {
		live := m.state.LiveTurn
		if strings.TrimSpace(live.Prompt) != "" {
			blocks = append(blocks, m.renderTranscriptEntry(TranscriptEntry{Role: "user", Body: strings.TrimSpace(live.Prompt)}, width))
		}
		for _, entry := range live.Entries {
			if block := m.renderTranscriptEntry(entry, width); strings.TrimSpace(block) != "" {
				blocks = append(blocks, block)
			}
		}
		if strings.TrimSpace(live.AssistantText) != "" {
			blocks = append(blocks, m.renderTranscriptEntry(TranscriptEntry{Role: "assistant", Body: strings.TrimSpace(live.AssistantText)}, width))
		}
		for _, note := range m.renderLiveTurnNotes(live) {
			blocks = append(blocks, m.renderTranscriptEntry(TranscriptEntry{Role: "tool", Body: note}, width))
		}
	}
	return strings.Join(blocks, "\n\n")
}

func (m *Model) renderTranscriptEntry(entry TranscriptEntry, width int) string {
	role := strings.ToLower(strings.TrimSpace(entry.Role))
	body := strings.TrimSpace(entry.Body)
	if body == "" {
		return ""
	}
	if role == "card" {
		return body
	}
	prefix := "■"
	style := m.styles.body
	switch role {
	case "user":
		prefix = "›"
		style = m.styles.roleUser
	case "assistant":
		prefix = "•"
		style = m.styles.roleAssistant
	case "tool":
		prefix = "•"
		style = m.styles.roleTool
	case "system":
		prefix = "■"
		style = m.styles.roleSystem
	}
	bodyWidth := maxInt(1, width-lipgloss.Width(prefix)-1)
	rendered := style.Width(bodyWidth).Render(body)
	lines := strings.Split(rendered, "\n")
	indent := strings.Repeat(" ", lipgloss.Width(prefix)+1)
	for index, line := range lines {
		if index == 0 {
			lines[index] = prefix + " " + line
			continue
		}
		lines[index] = indent + line
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderLiveTurnNotes(live *LiveTurnState) []string {
	if live == nil {
		return nil
	}
	notes := make([]string, 0, len(live.Notes)+1)
	if status := m.liveTurnStatusLine(live); status != "" {
		notes = append(notes, status)
	}
	notes = append(notes, visibleLiveTurnNotes(live, m.language)...)
	return notes
}

func (m *Model) liveTurnStatusLine(live *LiveTurnState) string {
	if live == nil || !m.state.Busy {
		return ""
	}
	spinner := "•"
	if len(busySpinnerFrames) > 0 {
		spinner = busySpinnerFrames[live.SpinnerFrame%len(busySpinnerFrames)]
	}
	elapsed := 0
	if !live.StartedAt.IsZero() {
		elapsed = int(time.Since(live.StartedAt).Round(time.Second) / time.Second)
		if elapsed < 0 {
			elapsed = 0
		}
	}
	return localizedTextTUI(
		m.language,
		"%s Working (%ds • esc to cancel)",
		"%s В работе (%ds • esc чтобы прервать)",
		spinner,
		elapsed,
	)
}

func (m *Model) handleInputHistoryNavigation(keyMsg tea.KeyMsg) bool {
	if m.state.Focus != FocusInput || m.isInlineCommandPaletteActive() || m.inputHistory == nil {
		return false
	}
	if !key.Matches(keyMsg, m.keys.Up) && !key.Matches(keyMsg, m.keys.Down) {
		return false
	}
	if !m.inputHistory.shouldHandleNavigation(m.input.Value(), m.input.Position()) {
		return false
	}

	var (
		value string
		ok    bool
	)
	if key.Matches(keyMsg, m.keys.Up) {
		value, ok = m.inputHistory.navigateUp()
	} else {
		value, ok = m.inputHistory.navigateDown()
	}
	if !ok {
		return false
	}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.state.InputDraft = value
	m.syncInlinePaletteWithDraft()
	m.refreshViewport()
	return true
}

func (m *Model) recordInputHistory(text string) {
	if m.inputHistory == nil {
		return
	}
	m.inputHistory.record(text, m.historySessionID())
}

func (m *Model) historySessionID() string {
	if base := strings.TrimSpace(filepath.Base(strings.TrimSpace(m.state.SessionPath))); base != "" && base != "." && base != string(filepath.Separator) {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return "interactive"
}

func (m *Model) updateInlineCommandPalette(keyMsg tea.KeyMsg) tea.Cmd {
	if !m.isInlineCommandPaletteActive() {
		return nil
	}
	switch {
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
	case key.Matches(keyMsg, m.keys.Close):
		m.input.Reset()
		m.state.InputDraft = ""
		m.dismissInlinePalette()
		return nil
	case key.Matches(keyMsg, m.keys.Submit):
		query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m.input.Value()), commandPrefix(m.layout.SettingsPath())))
		if strings.Contains(query, " ") {
			return m.submitInput()
		}
		if _, ok := m.selectedPaletteItem(); !ok {
			return m.submitInput()
		}
		m.input.Reset()
		m.state.InputDraft = ""
		return m.activatePaletteSelection()
	}
	return nil
}

func (m *Model) syncInlinePaletteWithDraft() {
	if m.state.Focus != FocusInput || m.state.Busy {
		return
	}
	prefix := commandPrefix(m.layout.SettingsPath())
	draft := strings.TrimSpace(m.input.Value())
	if strings.HasPrefix(draft, prefix) {
		query := strings.TrimSpace(strings.TrimPrefix(draft, prefix))
		m.state.Palette.Visible = true
		m.state.Palette.Mode = PaletteModeRoot
		m.state.Palette.Query = query
		m.state.Palette.Items = m.rootPaletteItemsForQuery(query)
		m.state.Palette.Context = defaultPaletteContextForLanguage(m.language)
		if strings.TrimSpace(m.state.Palette.SelectedToken) == "" {
			m.state.Palette.Selected = 0
		}
		m.syncPaletteSelection()
		return
	}
	if m.isInlineCommandPaletteActive() {
		m.dismissInlinePalette()
	}
}

func (m *Model) dismissInlinePalette() {
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
	case "card":
		return m.localize("card", "карточка")
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

func (m *Model) scrollTranscriptPage(direction int) bool {
	if m.state.Palette.Visible && !m.isInlineCommandPaletteActive() {
		return false
	}
	if m.viewport.Height <= 0 {
		return false
	}
	if direction < 0 {
		m.viewport.ViewUp()
		return true
	}
	if direction > 0 {
		m.viewport.ViewDown()
		return true
	}
	return false
}

func (m *Model) handleTranscriptMouse(msg tea.MouseMsg) bool {
	if m.state.Palette.Visible && !m.isInlineCommandPaletteActive() {
		return false
	}
	if msg.Action != tea.MouseActionPress {
		return false
	}
	step := maxInt(1, m.viewport.Height/4)
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.viewport.LineUp(step)
		return true
	case tea.MouseButtonWheelDown:
		m.viewport.LineDown(step)
		return true
	default:
		return false
	}
}

func (m *Model) submitInput() tea.Cmd {
	draft := strings.TrimSpace(m.input.Value())
	if draft == "" {
		return nil
	}
	m.recordInputHistory(draft)
	prefix := commandPrefix(m.layout.SettingsPath())
	if strings.HasPrefix(draft, prefix) {
		if m.isInlineCommandPaletteActive() {
			m.dismissInlinePalette()
		}
		m.input.Reset()
		m.state.InputDraft = ""
		return m.dispatchSlash(draft, prefix)
	}
	if m.state.Busy {
		m.appendTranscript("system", m.localize("The current turn is still running. Wait for it to finish before sending the next prompt.", "Текущий ход ещё выполняется. Дождитесь завершения перед следующим запросом."))
		m.refreshViewport()
		return nil
	}
	m.input.Reset()
	m.state.InputDraft = ""

	m.appendTranscript("user", draft)
	m.state.Busy = true
	m.state.LiveTurn = &LiveTurnState{
		StartedAt: time.Now(),
	}
	m.state.Footer = m.localize("Running turn...", "Выполняется ход...")
	m.updateStatus()
	m.refreshViewport()
	return tea.Batch(m.runPromptCmd(draft), busyTickCmd())
}

func (m *Model) runPromptCmd(prompt string) tea.Cmd {
	history := cloneRuntimeMessages(m.history)
	options := m.options
	options.Prompt = prompt
	options.History = history
	options.ApprovalStore = m.approvalStore
	if goruntime.GOOS == "windows" {
		options.DisableStreaming = true
	}
	existingSessionPath := m.state.SessionPath
	layout := m.layout
	ctx, cancel := context.WithCancel(context.Background())
	m.taskCancel = cancel

	return startTaskCmd(func(eventCh chan<- tea.Msg) {
		defer cancel()
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
		result, err := taskrun.Run(ctx, options)
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
	if m.state.Busy && !command.AvailableDuringTask {
		m.appendTranscript("system", fmt.Sprintf("%s: %s%s", m.localize("Command unavailable while a turn is running", "Команда недоступна, пока выполняется ход"), prefix, fields[0]))
		return nil
	}
	return m.executePaletteCommand(command, fields[1:])
}

func (m *Model) applyTaskResult(msg taskFinishedMsg) {
	history := cloneRuntimeMessages(msg.Result.FullHistory())
	m.history = history
	m.state.LiveTurn = nil
	m.state.SessionPath = msg.SessionPath
	m.approval = nil
	assistantText := visibleTranscriptBody(msg.Result.AssistantMessage)
	if assistantText == "" {
		assistantText = normalizeTranscriptBody(msg.Result.Text)
	}
	if assistantText != "" {
		m.state.Transcript = appendTranscriptEntry(m.state.Transcript, "assistant", assistantText)
	}
	m.state.Model = fallback(msg.Result.Model, m.state.Model)
	m.state.Provider = fallback(msg.Result.ProviderName, m.state.Provider)
	m.state.Profile = fallback(msg.Result.Profile, m.state.Profile)
	m.state.Reasoning = fallback(msg.Result.Reasoning, m.state.Reasoning)
	m.state.CWD = fallback(strings.TrimSpace(msg.Result.CWD), m.state.CWD)
	m.options.Model = m.state.Model
	m.options.Provider = m.state.Provider
	m.options.Profile = m.state.Profile
	m.options.ReasoningEffort = m.state.Reasoning
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
	m.approvalStore = taskrun.NewApprovalSessionStore()
	m.state.LiveTurn = nil
	m.state.Transient = nil
	m.state.Transcript = transcriptFromMessages(msg.Messages, m.language)
	m.approval = nil
	m.state.Model = msg.Meta.Model
	m.state.Provider = msg.Meta.Provider
	m.state.Profile = msg.Meta.Profile
	m.state.Reasoning = msg.Meta.Reasoning
	m.state.CWD = fallback(strings.TrimSpace(msg.Meta.CWD), m.state.CWD)
	m.options.Model = m.state.Model
	m.options.Provider = m.state.Provider
	m.options.Profile = m.state.Profile
	m.options.ReasoningEffort = m.state.Reasoning
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
	m.sessionPaletteStartup = false
	m.startup.Mode = StartupModeNone
	m.paletteInput.Reset()
	m.state.Focus = FocusInput
	m.applyFocusState()
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) beginCWDPrompt(msg sessionLoadedMsg) bool {
	sessionCWD := strings.TrimSpace(msg.Meta.CWD)
	currentCWD := strings.TrimSpace(m.effectiveWorkingDirectory())
	if sessionCWD == "" || currentCWD == "" {
		return false
	}
	if appstate.ComparableSessionCWD(sessionCWD) == appstate.ComparableSessionCWD(currentCWD) {
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
	return m.consumeStartupPrompt()
}

func (m *Model) openPaletteMode(mode PaletteMode, pushCurrent bool) tea.Cmd {
	return m.applyPaletteScreen(mode, m.paletteItemsForMode(mode), "", pushCurrent)
}

func (m *Model) applyPaletteScreen(mode PaletteMode, items []PaletteItem, footer string, pushCurrent bool) tea.Cmd {
	context := m.nextPaletteContext(mode, pushCurrent)
	if !pushCurrent && len(m.state.Palette.Stack) == 0 {
		context = m.directPaletteContext(mode)
	}
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
		if cmd := m.navigatePaletteBranchBack(); cmd != nil {
			return cmd
		}
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

func (m *Model) setModelSettingsNavigationOrigin(origin ModelSettingsNavigationOrigin) {
	m.modelSettingsNavigationOrigin = origin
}

func (m *Model) replacePaletteRoot(mode PaletteMode, items []PaletteItem, footer string) tea.Cmd {
	m.state.Palette.Stack = nil
	return m.applyPaletteScreen(mode, items, footer, false)
}

func (m *Model) modelSettingsBackName() string {
	if m.modelSettingsNavigationOrigin == ModelSettingsNavigationOriginSettings {
		return m.localize("Back to Model Settings", "Назад к настройкам моделей")
	}
	return m.localize("Back to Chat", "Назад в чат")
}

func (m *Model) directPaletteContext(mode PaletteMode) PaletteContext {
	localize := m.localize
	switch mode {
	case PaletteModeModelSettings:
		if m.modelSettingsNavigationOrigin == ModelSettingsNavigationOriginSettings {
			return PaletteContext{
				BackTitle:       localize("Back to Settings", "Назад к настройкам"),
				BackDescription: localize("Return to settings", "Вернуться к настройкам"),
				BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
				ReturnFocus:     FocusInput,
			}
		}
	case PaletteModeModel, PaletteModeModelCatalog, PaletteModeProfiles, PaletteModeProviders, PaletteModeModelPresets:
		if m.modelSettingsNavigationOrigin == ModelSettingsNavigationOriginSettings {
			return PaletteContext{
				BackTitle:       m.modelSettingsBackName(),
				BackDescription: localize("Return to model settings", "Вернуться к настройкам моделей"),
				BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
				ReturnFocus:     FocusInput,
			}
		}
	case PaletteModeReasoning:
		return PaletteContext{
			BackTitle:       localize("Back to Model Picker", "Назад к выбору модели"),
			BackDescription: localize("Return to the model picker", "Вернуться к выбору модели"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModeProfileActions, PaletteModeAddAccount:
		return PaletteContext{
			BackTitle:       localize("Back to Profiles", "Назад к профилям"),
			BackDescription: localize("Return to profiles", "Вернуться к профилям"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModeProviderActions:
		return PaletteContext{
			BackTitle:       localize("Back to Providers", "Назад к провайдерам"),
			BackDescription: localize("Return to providers", "Вернуться к провайдерам"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModePresetEditor:
		return PaletteContext{
			BackTitle:       localize("Back to Presets", "Назад к пресетам"),
			BackDescription: localize("Return to model presets", "Вернуться к пресетам моделей"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModePresetActions, PaletteModePresetModels:
		return PaletteContext{
			BackTitle:       localize("Back to Preset List", "Назад к списку пресетов"),
			BackDescription: localize("Return to the preset list", "Вернуться к списку пресетов"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModeCustomization, PaletteModeCustomizationColor, PaletteModeCustomizationFormatting, PaletteModeLanguage, PaletteModeCommandPrefix, PaletteModePopupCommands, PaletteModePermissions:
		return PaletteContext{
			BackTitle:       localize("Back to Settings", "Назад к настройкам"),
			BackDescription: localize("Return to settings", "Вернуться к настройкам"),
			BackHint:        localize("Enter select · Esc back", "Enter выбрать · Esc назад"),
			ReturnFocus:     FocusInput,
		}
	case PaletteModeStatus:
		return PaletteContext{
			BackTitle:       localize("Back to Chat", "Назад в чат"),
			BackDescription: localize("Return to transcript", "Вернуться к диалогу"),
			BackHint:        localize("Esc back", "Esc назад"),
			ReturnFocus:     FocusInput,
		}
	}
	return defaultPaletteContextForLanguage(m.language)
}

func (m *Model) navigatePaletteBranchBack() tea.Cmd {
	switch m.state.Palette.Mode {
	case PaletteModeModelSettings:
		if m.modelSettingsNavigationOrigin == ModelSettingsNavigationOriginSettings {
			return m.openPaletteMode(PaletteModeSettings, false)
		}
	case PaletteModeModel, PaletteModeModelCatalog, PaletteModeProfiles, PaletteModeProviders, PaletteModeModelPresets:
		if m.modelSettingsNavigationOrigin == ModelSettingsNavigationOriginSettings {
			return m.reopenModelSettingsPalette()
		}
	case PaletteModeReasoning:
		return m.reopenModelPickerPalette()
	case PaletteModeProfileActions, PaletteModeAddAccount:
		return m.reopenProfilesPalette()
	case PaletteModeProviderActions:
		return m.reopenProvidersPalette()
	case PaletteModePresetEditor:
		return m.reopenModelPresetsSettingsPalette()
	case PaletteModePresetActions, PaletteModePresetModels:
		return m.reopenCurrentProviderPresetEditor()
	case PaletteModeCustomization, PaletteModeCustomizationColor, PaletteModeCustomizationFormatting, PaletteModeLanguage, PaletteModeCommandPrefix, PaletteModePopupCommands, PaletteModePermissions:
		return m.openPaletteMode(PaletteModeSettings, false)
	case PaletteModeStatus:
		return m.closePalette()
	}
	return nil
}

func (m *Model) paletteItemsForMode(mode PaletteMode) []PaletteItem {
	switch mode {
	case PaletteModeSettings:
		return m.settingsPaletteItems()
	case PaletteModeCustomization:
		return m.customizationPaletteItems()
	case PaletteModeCustomizationColor:
		return m.customizationColorItems()
	case PaletteModeCustomizationColorChoice:
		return m.customizationColorChoiceItems()
	case PaletteModeCustomizationFormatting:
		return m.customizationFormattingItems()
	case PaletteModeCustomizationFormattingTarget:
		return m.customizationFormattingTargetItems()
	case PaletteModeStatus:
		return nil
	case PaletteModeLanguage:
		return m.languagePaletteItems()
	case PaletteModeCommandPrefix:
		return m.commandPrefixPaletteItems()
	case PaletteModePopupCommands:
		return m.popupCommandPaletteItems()
	case PaletteModePermissions:
		return m.permissionsPaletteItems()
	case PaletteModeModel:
		return nil
	case PaletteModeModelSettings:
		return m.modelSettingsPaletteItems()
	case PaletteModeModelCatalog:
		return nil
	case PaletteModeReasoning:
		return nil
	case PaletteModeProfiles:
		return m.profilesManagerItems()
	case PaletteModeProfileActions:
		return nil
	case PaletteModeProviders:
		return m.providersManagerItems()
	case PaletteModeProviderActions:
		return nil
	case PaletteModeModelPresets:
		return m.modelPresetsSettingsItems()
	case PaletteModePresetEditor:
		return nil
	case PaletteModePresetActions:
		return nil
	case PaletteModePresetModels:
		return nil
	default:
		return m.rootPaletteItems()
	}
}

func (m *Model) settingsPaletteItems() []PaletteItem {
	settings, _ := loadSettingsOptional(m.layout.SettingsPath())
	summary := settings.Summary()
	return []PaletteItem{
		{Key: "settings.customization", Title: m.localize("Customization", "Кастомизация"), Description: m.localize("Colors and formatting", "Цвета и форматирование"), Keywords: []string{"customization", "colors", "formatting", "кастомизация", "цвета", "форматирование"}},
		{Key: "settings.model_settings", Title: m.localize("Model Settings", "Настройки моделей"), Description: m.localize("Models, profiles, presets", "Модели, профили, пресеты"), Aliases: []string{"/model", "/profiles", "/providers", "/модель", "/профили", "/провайдеры"}, Keywords: []string{"reasoning", "provider", "profile", "models", "profiles", "providers", "модель", "провайдер", "профиль"}},
		{Key: "settings.language", Title: m.localize("Interface Language", "Язык интерфейса"), Description: fallback(summary.Language, localizedUnsetTUI(m.language)), Keywords: []string{"locale", "translation", "язык", "language"}},
		{Key: "settings.command_prefix", Title: m.localize("Command Prefix", "Префикс команд"), Description: fallback(summary.CommandPrefix, localizedUnsetTUI(m.language)), Keywords: []string{"slash", "prefix", "commands", "префикс"}},
		{Key: "settings.hidden_commands", Title: m.localize("Popup Commands", "Команды во всплывающем списке"), Description: fmt.Sprintf("%d %s", len(summary.HiddenCommands), m.localize("hidden", "скрыто")), Keywords: []string{"visibility", "commands", "popup", "hidden", "скрытые"}},
		{Key: "settings.permissions", Title: m.localize("Access and Approvals", "Доступ и подтверждения"), Description: m.toolPolicySummary(), Keywords: []string{"permissions", "approvals", "tools", "sandbox", "разрешения", "подтверждения"}},
	}
}

func (m *Model) customizationPaletteItems() []PaletteItem {
	settings := m.settingsForUI()
	fillValue := m.localize("Background", "Фон")
	if !effectiveSelectionHighlightFill(settings) {
		fillValue = m.localize("Text", "Текст")
	}
	return []PaletteItem{
		{
			Key:         "customization.fill",
			Title:       m.localize("Fill", "Заливка"),
			Description: fillValue,
			Meta:        renderStateTag(effectiveSelectionHighlightFill(settings)),
			Keywords:    []string{"fill", "background", "selection", "заливка", "фон", "выделение"},
		},
		{
			Key:      "customization.color",
			Title:    m.localize("Color", "Цвет"),
			Keywords: []string{"color", "palette", "reply", "command", "цвет", "палитра", "ответ", "команда"},
		},
		{
			Key:      "customization.formatting",
			Title:    m.localize("Formatting", "Форматирование"),
			Keywords: []string{"formatting", "bold", "italic", "underline", "strike", "форматирование", "жирный", "курсив", "подчёркивание"},
		},
	}
}

func (m *Model) selectionHighlightColorSummary(settings appstate.Settings) string {
	return colorChoiceSummary(settings.SelectionHighlight.Color, settings.SelectionHighlight.Preset, m.language)
}

func (m *Model) selectionPresetLabel(preset string) string {
	return selectionPresetDisplayName(preset, m.language)
}

func (m *Model) selectionHighlightFormattingSummary(settings appstate.Settings) string {
	return formatValueSummary(settings.TextFormats.SelectionHighlight, m.language)
}

func (m *Model) popupColorTargetSummary(settings appstate.Settings, target popupColorTarget) string {
	switch target {
	case popupColorTargetSelection:
		return m.selectionHighlightColorSummary(settings)
	default:
		return colorChoiceSummary(popupColorTargetChoice(settings, target), settings.SelectionHighlight.Preset, m.language)
	}
}

func (m *Model) popupFormatTargetSummary(settings appstate.Settings, target popupFormatTarget) string {
	return formatValueSummary(popupFormatTargetValue(settings, target), m.language)
}

func (m *Model) customizationColorItems() []PaletteItem {
	settings := m.settingsForUI()
	items := []PaletteItem{m.paletteBackItem()}
	for _, target := range popupColorTargets() {
		label := popupColorTargetLabel(target, m.language)
		items = append(items, PaletteItem{
			Key:                "customization.color.target",
			Title:              label,
			DisplayTitle:       label,
			Description:        m.popupColorTargetSummary(settings, target),
			DisplayDescription: m.popupColorTargetSummary(settings, target),
			Value:              string(target),
			Keywords:           []string{label, "color", "цвет"},
		})
	}
	return items
}

func (m *Model) customizationColorChoiceItems() []PaletteItem {
	settings := m.settingsForUI()
	target := m.customizationColorTarget
	if target == "" {
		target = popupColorTargetSelection
	}
	items := []PaletteItem{m.paletteBackItem()}
	currentChoice := parseUIColorChoice(popupColorTargetChoice(settings, target), settings.SelectionHighlight.Preset)
	if target != popupColorTargetSelection {
		label := localizedTextTUI(m.language, "Auto", "Авто")
		items = append(items, PaletteItem{
			Key:                "customization.color.choice",
			Title:              label,
			DisplayTitle:       label,
			Description:        colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoiceAuto, preset: normalizedSelectionPreset(settings.SelectionHighlight.Preset)}, settings.SelectionHighlight.Preset, m.language),
			DisplayDescription: colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoiceAuto, preset: normalizedSelectionPreset(settings.SelectionHighlight.Preset)}, settings.SelectionHighlight.Preset, m.language),
			Value:              "auto",
			Keywords:           []string{"auto", "system", "авто", "системный"},
			Meta:               renderStateTag(currentChoice.kind == uiColorChoiceAuto),
		})
	}
	if target == popupColorTargetSelection {
		for _, preset := range []string{"light", "graphite", "amber", "mint", "rose"} {
			items = append(items, PaletteItem{
				Key:                "customization.color.choice",
				Title:              m.selectionPresetLabel(preset),
				DisplayTitle:       renderPresetChoiceLabel(preset, m.language),
				Description:        colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoicePreset, preset: preset}, settings.SelectionHighlight.Preset, m.language),
				DisplayDescription: colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoicePreset, preset: preset}, settings.SelectionHighlight.Preset, m.language),
				Value:              preset,
				Keywords:           []string{preset, "preset", "color", "пресет", "цвет"},
				Meta:               renderStateTag(currentChoice.kind == uiColorChoicePreset && normalizedSelectionPreset(currentChoice.preset) == normalizedSelectionPreset(preset)),
			})
		}
	}
	for _, named := range allNamedColors {
		if target == popupColorTargetSelection && strings.EqualFold(named.Hex, selectionPresetHex("light")) {
			continue
		}
		if target == popupColorTargetSelection && strings.EqualFold(named.Hex, selectionPresetHex("graphite")) {
			continue
		}
		if target == popupColorTargetSelection && strings.EqualFold(named.Hex, selectionPresetHex("amber")) {
			continue
		}
		if target == popupColorTargetSelection && strings.EqualFold(named.Hex, selectionPresetHex("mint")) {
			continue
		}
		if target == popupColorTargetSelection && strings.EqualFold(named.Hex, selectionPresetHex("rose")) {
			continue
		}
		items = append(items, PaletteItem{
			Key:                "customization.color.choice",
			Title:              namedColorLabel(named, m.language),
			DisplayTitle:       renderNamedColorChoiceLabel(named, m.language),
			Description:        colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoiceCustom, hex: strings.ToLower(named.Hex)}, settings.SelectionHighlight.Preset, m.language),
			DisplayDescription: colorPreviewDescriptionChoice(uiColorChoice{kind: uiColorChoiceCustom, hex: strings.ToLower(named.Hex)}, settings.SelectionHighlight.Preset, m.language),
			Value:              named.Hex,
			Keywords:           []string{namedColorLabel(named, m.language), named.Hex, "color", "цвет"},
			Meta:               renderStateTag(currentChoice.kind == uiColorChoiceCustom && strings.EqualFold(currentChoice.hex, named.Hex)),
		})
	}
	customHex := popupColorTargetChoice(settings, target)
	if parseUIColorChoice(customHex, settings.SelectionHighlight.Preset).kind != uiColorChoiceCustom {
		customHex = "#f7dce5"
	}
	items = append(items, PaletteItem{
		Key:                "customization.color.custom",
		Title:              localizedTextTUI(m.language, "Custom color", "Свой цвет"),
		DisplayTitle:       renderColoredLabel(localizedTextTUI(m.language, "Custom color", "Свой цвет"), customHex),
		Description:        localizedTextTUI(m.language, "Enter HEX", "Введите HEX"),
		DisplayDescription: func() string {
			parsed := parseUIColorChoice(popupColorTargetChoice(settings, target), settings.SelectionHighlight.Preset)
			if parsed.kind == uiColorChoiceCustom {
				return colorPreviewDescriptionChoice(parsed, settings.SelectionHighlight.Preset, m.language)
			}
			return localizedTextTUI(m.language, "Enter HEX", "Введите HEX")
		}(),
		Value:              string(target),
		Keywords:           []string{"custom", "hex", "свой", "hex"},
		Meta:               renderStateTag(parseUIColorChoice(popupColorTargetChoice(settings, target), settings.SelectionHighlight.Preset).kind == uiColorChoiceCustom && !strings.EqualFold(customHex, "#f7dce5")),
	})
	return items
}

func (m *Model) customizationFormattingItems() []PaletteItem {
	settings := m.settingsForUI()
	items := []PaletteItem{m.paletteBackItem()}
	for _, target := range popupFormatTargets() {
		label := popupFormatTargetLabel(target, m.language)
		formats := popupFormatTargetValue(settings, target)
		items = append(items, PaletteItem{
			Key:                "customization.format.target",
			Title:              label,
			DisplayTitle:       label,
			Description:        m.popupFormatTargetSummary(settings, target),
			DisplayDescription: m.popupFormatTargetSummary(settings, target),
			Value:              string(target),
			Keywords:           []string{label, "format", "formatting", "формат", "форматирование"},
			Meta:               renderStateTag(!formats.IsEmpty()),
		})
	}
	return items
}

func (m *Model) customizationFormattingTargetItems() []PaletteItem {
	settings := m.settingsForUI()
	target := m.customizationFormatTarget
	if target == "" {
		target = popupFormatTargetSelection
	}
	formats := popupFormatTargetValue(settings, target)
	items := []PaletteItem{m.paletteBackItem()}
	for _, code := range []string{"underlined", "crossed_out"} {
		label := formatCodeLabel(code, m.language)
		items = append(items, PaletteItem{
			Key:                "customization.format.option",
			Title:              label,
			DisplayTitle:       renderFormatChoiceLabel(code, m.language),
			Value:              code,
			Keywords:           []string{code, label, "formatting", "форматирование"},
			Meta:               renderStateTag(formats.Contains(code)),
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
	return m.filterHiddenPaletteCommands(m.catalog.RootItems(m.language, ""))
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
	case PaletteModeLanguage:
		return localize("Back to Language", "Назад к языку"), localize("Return to language choices", "Вернуться к выбору языка")
	case PaletteModeCommandPrefix:
		return localize("Back to Command Prefix", "Назад к префиксу команд"), localize("Return to prefix choices", "Вернуться к выбору префикса")
	case PaletteModePopupCommands:
		return localize("Back to Popup Commands", "Назад к списку команд"), localize("Return to popup commands", "Вернуться к списку команд")
	case PaletteModePermissions:
		return localize("Back to Permissions", "Назад к разрешениям"), localize("Return to permissions", "Вернуться к разрешениям")
	case PaletteModeRoot:
		return localize("Back to Commands", "Назад к командам"), localize("Return to commands", "Вернуться к командам")
	case PaletteModeModelSettings:
		return localize("Back to Model Settings", "Назад к настройкам моделей"), localize("Return to model settings", "Вернуться к настройкам моделей")
	case PaletteModeModel:
		return localize("Back to Model Picker", "Назад к выбору модели"), localize("Return to the model picker", "Вернуться к выбору модели")
	case PaletteModeModelCatalog:
		return localize("Back to Model Catalog", "Назад к каталогу моделей"), localize("Return to the model catalog", "Вернуться к каталогу моделей")
	case PaletteModeReasoning:
		return localize("Back to Reasoning", "Назад к размышлениям"), localize("Return to reasoning choices", "Вернуться к выбору размышлений")
	case PaletteModeProfiles:
		return localize("Back to Profiles", "Назад к профилям"), localize("Return to profiles", "Вернуться к профилям")
	case PaletteModeProfileActions:
		return localize("Back to Profile Actions", "Назад к действиям профиля"), localize("Return to profile actions", "Вернуться к действиям профиля")
	case PaletteModeAddAccount:
		return localize("Back to Add Account", "Назад к добавлению аккаунта"), localize("Return to add-account providers", "Вернуться к выбору провайдера")
	case PaletteModeProviders:
		return localize("Back to Providers", "Назад к провайдерам"), localize("Return to providers", "Вернуться к провайдерам")
	case PaletteModeProviderActions:
		return localize("Back to Provider Actions", "Назад к действиям провайдера"), localize("Return to provider actions", "Вернуться к действиям провайдера")
	case PaletteModeModelPresets:
		return localize("Back to Presets", "Назад к пресетам"), localize("Return to model presets", "Вернуться к пресетам моделей")
	case PaletteModePresetEditor:
		return localize("Back to Preset Editor", "Назад к редактору пресетов"), localize("Return to the current provider preset list", "Вернуться к списку пресетов провайдера")
	case PaletteModePresetActions:
		return localize("Back to Preset Actions", "Назад к действиям пресета"), localize("Return to preset actions", "Вернуться к действиям пресета")
	case PaletteModePresetModels:
		return localize("Back to Preset Models", "Назад к моделям пресета"), localize("Return to the preset model picker", "Вернуться к выбору модели пресета")
	case PaletteModeCustomization:
		return localize("Back to Customization", "Назад к кастомизации"), localize("Return to customization", "Вернуться к кастомизации")
	case PaletteModeCustomizationColor:
		return localize("Back to Customization", "Назад к кастомизации"), localize("Return to customization", "Вернуться к кастомизации")
	case PaletteModeCustomizationColorChoice:
		return localize("Back to Color", "Назад к цвету"), localize("Return to color targets", "Вернуться к целям цвета")
	case PaletteModeCustomizationFormatting:
		return localize("Back to Customization", "Назад к кастомизации"), localize("Return to customization", "Вернуться к кастомизации")
	case PaletteModeCustomizationFormattingTarget:
		return localize("Back to Formatting", "Назад к форматированию"), localize("Return to formatting targets", "Вернуться к целям форматирования")
	case PaletteModeResume:
		return localize("Back to Resume", "Назад к продолжению"), localize("Return to saved sessions", "Вернуться к сохранённым сессиям")
	case PaletteModeFork:
		return localize("Back to Fork", "Назад к ответвлению"), localize("Return to fork browser", "Вернуться к списку ответвлений")
	case PaletteModeStatus:
		return localize("Back to Chat", "Назад в чат"), localize("Return to transcript", "Вернуться к диалогу")
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

func (m *Model) executePaletteCommand(command PaletteCommandSpec, args []string) tea.Cmd {
	if cmd := m.executeInlineSlashCommand(command, args); cmd != nil {
		return cmd
	}
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
	case PaletteActionForkCurrent:
		if len(m.history) == 0 && strings.TrimSpace(m.state.SessionPath) == "" {
			m.state.Footer = m.localize("No active chat to fork", "Нет активного чата для форка")
			if m.state.Palette.Visible {
				return m.closePalette()
			}
			return nil
		}
		m.state.SessionPath = ""
		m.approvalStore = taskrun.NewApprovalSessionStore()
		m.state.Footer = m.localize("Forked current chat", "Текущий чат ответвлён")
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
	case PaletteActionClearTranscript:
		m.clearTranscriptView()
		if m.state.Palette.Visible {
			return m.closePalette()
		}
		return nil
	case PaletteActionShowStatus:
		m.updateStatus()
		m.appendTranscript("card", m.renderStatusCardInline())
		m.refreshViewport()
		if m.state.Palette.Visible {
			return m.closePalette()
		}
		return nil
	case PaletteActionQuit:
		return tea.Quit
	case PaletteActionOpenMode:
		switch command.Mode {
		case PaletteModeModel:
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
			return m.openModelPickerPalette(m.state.Palette.Visible)
		case PaletteModeProfiles:
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
			return m.openProfilesPalette(m.state.Palette.Visible)
		case PaletteModeModelPresets:
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
			return m.openModelPresetsSettingsPalette(m.state.Palette.Visible)
		case PaletteModeProviders:
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
			return m.openProvidersPalette(m.state.Palette.Visible)
		case PaletteModeSettings:
			return m.openPaletteMode(PaletteModeSettings, m.state.Palette.Visible)
		default:
			return m.openPaletteMode(command.Mode, m.state.Palette.Visible)
		}
	default:
		return nil
	}
}

func (m *Model) executeInlineSlashCommand(command PaletteCommandSpec, args []string) tea.Cmd {
	args = normalizeInlineSlashArgs(args)
	if len(args) == 0 {
		return nil
	}
	key := normalizePaletteCommandName(firstNonEmpty(command.CatalogCommand, command.Key))
	switch key {
	case "setlang":
		return m.executeInlineSetlang(args)
	case "permissions":
		return m.executeInlinePermissions(args)
	default:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Inline arguments are not supported for this command", "У этой команды inline-аргументы не поддерживаются"), strings.Join(args, " "))
		return nil
	}
}

func normalizeInlineSlashArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	result := make([]string, 0, len(args))
	for _, arg := range args {
		if trimmed := strings.TrimSpace(arg); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (m *Model) executeInlineSetlang(args []string) tea.Cmd {
	if len(args) == 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "ru", "russian", "русский":
		return m.setSettingsLanguage("ru")
	case "en", "english", "английский":
		return m.setSettingsLanguage("en")
	default:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Unsupported language", "Неподдерживаемый язык"), args[0])
		return nil
	}
}

func (m *Model) executeInlinePermissions(args []string) tea.Cmd {
	if len(args) == 0 {
		return nil
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	switch action {
	case "auto", "авто":
		policy.ApprovalMode = tooling.ToolApprovalModeAuto
		return m.saveToolPolicy(policy, fmt.Sprintf("%s: %s", m.localize("Approval mode", "Режим подтверждений"), m.approvalModeLabel(policy.ApprovalMode)))
	case "require", "требовать":
		policy.ApprovalMode = tooling.ToolApprovalModeRequire
		return m.saveToolPolicy(policy, fmt.Sprintf("%s: %s", m.localize("Approval mode", "Режим подтверждений"), m.approvalModeLabel(policy.ApprovalMode)))
	case "deny", "запретить":
		policy.ApprovalMode = tooling.ToolApprovalModeDeny
		return m.saveToolPolicy(policy, fmt.Sprintf("%s: %s", m.localize("Approval mode", "Режим подтверждений"), m.approvalModeLabel(policy.ApprovalMode)))
	case "block-mutating", "block-mutating-tools", "блок-записи":
		policy.BlockMutatingTools = true
		return m.saveToolPolicy(policy, m.localize("Mutating tools blocked", "Изменяющие инструменты запрещены"))
	case "allow-mutating", "разрешить-запись":
		policy.BlockMutatingTools = false
		return m.saveToolPolicy(policy, m.localize("Mutating tools allowed", "Изменяющие инструменты разрешены"))
	case "block-shell", "block-shell-tools", "блок-shell":
		policy.BlockShellCommands = true
		return m.saveToolPolicy(policy, m.localize("Shell commands blocked", "Shell-команды запрещены"))
	case "allow-shell", "разрешить-shell":
		policy.BlockShellCommands = false
		return m.saveToolPolicy(policy, m.localize("Shell commands allowed", "Shell-команды разрешены"))
	case "enable-parallel", "включить-параллель":
		policy.Planning.AllowParallel = true
		if policy.Planning.MaxParallelCalls < 2 {
			policy.Planning.MaxParallelCalls = maxInt(2, tooling.DefaultPlanningPolicy().MaxParallelCalls)
		}
		return m.saveToolPolicy(policy, m.localize("Parallel tools enabled", "Параллельные инструменты включены"))
	case "disable-parallel", "выключить-параллель":
		policy.Planning.AllowParallel = false
		policy.Planning.MaxParallelCalls = 1
		return m.saveToolPolicy(policy, m.localize("Parallel tools disabled", "Параллельные инструменты выключены"))
	case "parallelism", "параллелизм":
		if len(args) < 2 {
			m.state.Footer = m.localize("parallelism requires a number", "параллелизм требует число")
			return nil
		}
		value, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || value < 1 {
			m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Invalid parallelism", "Некорректный параллелизм"), args[1])
			return nil
		}
		policy.Planning.MaxParallelCalls = value
		policy.Planning.AllowParallel = value > 1
		return m.saveToolPolicy(policy, fmt.Sprintf("%s: %d", m.localize("Parallelism", "Параллелизм"), value))
	default:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Unsupported permissions action", "Неподдерживаемое действие permissions"), args[0])
		return nil
	}
}

func (m *Model) togglePalette() tea.Cmd {
	if m.state.Palette.Visible {
		if m.isInlineCommandPaletteActive() {
			m.input.Reset()
			m.state.InputDraft = ""
			m.dismissInlinePalette()
			return m.setFocus(FocusInput)
		}
		return m.closePalette()
	}
	prefix := commandPrefix(m.layout.SettingsPath())
	m.state.Focus = FocusInput
	m.input.SetValue(prefix)
	m.state.InputDraft = prefix
	m.syncInlinePaletteWithDraft()
	return m.setFocus(FocusInput)
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
		case keyMsg.Type == tea.KeyTab && (m.state.Palette.Mode == PaletteModeResume || m.state.Palette.Mode == PaletteModeFork):
			return m.toggleSessionSort()
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
		return m.loadSessionCmdWithOptions(item.Value, false, m.sessionPaletteStartup)
	case PaletteModeFork:
		return m.loadSessionCmdWithOptions(item.Value, true, m.sessionPaletteStartup)
	case PaletteModeSettings:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "settings.customization":
			return m.openPaletteMode(PaletteModeCustomization, true)
		case "settings.model_settings":
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
			return m.openModelSettingsPalette(true)
		case "settings.language":
			return m.openPaletteMode(PaletteModeLanguage, true)
		case "settings.command_prefix":
			return m.openCommandPrefixPrompt()
		case "settings.hidden_commands":
			return m.openPaletteMode(PaletteModePopupCommands, true)
		case "settings.permissions":
			return m.openPaletteMode(PaletteModePermissions, true)
		}
		return nil
	case PaletteModeCustomization:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "customization.fill":
			return m.toggleSelectionHighlightFill()
		case "customization.color":
			return m.openPaletteMode(PaletteModeCustomizationColor, true)
		case "customization.formatting":
			return m.openPaletteMode(PaletteModeCustomizationFormatting, true)
		}
		return nil
	case PaletteModeCustomizationColor:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "customization.color.target":
			m.customizationColorTarget = popupColorTarget(item.Value)
			return m.openPaletteMode(PaletteModeCustomizationColorChoice, true)
		}
		return nil
	case PaletteModeCustomizationColorChoice:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "customization.color.choice":
			return m.setPopupColorChoice(m.customizationColorTarget, item.Value)
		case "customization.color.custom":
			return m.openCustomColorPrompt(m.customizationColorTarget)
		}
		return nil
	case PaletteModeCustomizationFormatting:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "customization.format.target":
			m.customizationFormatTarget = popupFormatTarget(item.Value)
			return m.openPaletteMode(PaletteModeCustomizationFormattingTarget, true)
		}
		return nil
	case PaletteModeCustomizationFormattingTarget:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "customization.format.option":
			return m.togglePopupTextFormat(m.customizationFormatTarget, item.Value)
		}
		return nil
	case PaletteModeLanguage:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "settings.language.option":
			return m.setSettingsLanguage(item.Value)
		}
		return nil
	case PaletteModeCommandPrefix:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "settings.command_prefix.option":
			return m.setSettingsCommandPrefix(item.Value)
		}
		return nil
	case PaletteModePopupCommands:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "settings.hidden_commands.entry":
			return m.togglePopupCommandVisibility(item.Value)
		}
		return nil
	case PaletteModePermissions:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "settings.permissions.approval_mode":
			return m.cycleApprovalMode()
		case "settings.permissions.block_mutating":
			return m.toggleBlockMutatingTools()
		case "settings.permissions.block_shell":
			return m.toggleBlockShellCommands()
		case "settings.permissions.parallel":
			return m.toggleParallelTools()
		case "settings.permissions.parallelism":
			return m.cycleToolParallelism()
		}
		return nil
	case PaletteModeModelSettings:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "model_settings.models":
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
			return m.openModelPickerPalette(true)
		case "model_settings.profiles":
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
			return m.openProfilesPalette(true)
		case "model_settings.presets":
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
			return m.openModelPresetsSettingsPalette(true)
		case "model_settings.providers":
			m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
			return m.openProvidersPalette(true)
		}
		return nil
	case PaletteModeModel:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "model.catalog":
			return m.openAllModelsPalette(true)
		case "model.preset":
			slug, reasoning, providerName := splitModelSelectionValue(item.Value)
			if strings.TrimSpace(slug) == "" {
				return nil
			}
			return m.openReasoningPalette(modelcatalog.Model{
				Slug:                  slug,
				DisplayName:           firstNonEmpty(strings.TrimSpace(item.Title), strings.TrimSpace(slug)),
				DefaultReasoningLevel: reasoning,
			}, providerName, true)
		}
		return nil
	case PaletteModeModelCatalog:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "model.catalog.entry":
			slug, providerName := splitModelCatalogValue(item.Value)
			if strings.TrimSpace(slug) == "" {
				return nil
			}
			config, err := loadConfigOptional(m.layout.ConfigPath())
			if err != nil {
				m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
				return nil
			}
			ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", providerName)
			if err != nil {
				m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load provider catalog", "Не удалось загрузить каталог провайдера"), err)
				return nil
			}
			model, _ := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, slug)
			return m.openReasoningPalette(model, firstNonEmpty(providerName, ctx.ProviderName), true)
		}
		return nil
	case PaletteModeReasoning:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "model.reasoning.entry":
			slug, providerName, effort := splitReasoningSelectionValue(item.Value)
			if strings.TrimSpace(slug) == "" {
				return nil
			}
			return m.applyModelSelection(modelcatalog.Model{
				Slug:                  slug,
				DisplayName:           firstNonEmpty(strings.TrimSpace(item.Title), strings.TrimSpace(slug)),
				DefaultReasoningLevel: effort,
			}, providerName, effort)
		}
		return nil
	case PaletteModeProfiles:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "profiles.add_account":
			return m.openAddAccountProviderPalette(true)
		case "profiles.entry":
			return m.openProfileActionsPalette(item.Value, true)
		}
		return nil
	case PaletteModeProfileActions:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "profile.activate":
			return m.activateProfile(item.Value)
		case "profile.delete":
			return m.deleteProfile(item.Value)
		}
		return nil
	case PaletteModeAddAccount:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "profiles.add_provider":
			return m.openAddAccountDetailsPrompt(item.Value, "")
		}
		return nil
	case PaletteModeProviders:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "providers.entry":
			return m.openProviderActionsPalette(item.Value, true)
		}
		return nil
	case PaletteModeProviderActions:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "provider.activate":
			return m.activateProvider(item.Value)
		case "provider.delete":
			return m.deleteProvider(item.Value)
		}
		return nil
	case PaletteModeModelPresets:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "model_presets.toggle":
			return m.toggleModelPresetsEnabled()
		case "model_presets.provider":
			return m.openCurrentProviderPresetEditor(true)
		}
		return nil
	case PaletteModePresetEditor:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "preset.add":
			return m.openCurrentProviderPresetModelPicker("", true)
		case "preset.entry":
			return m.openCurrentProviderPresetActions(item.Value, true)
		}
		return nil
	case PaletteModePresetActions:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "preset.rename":
			return m.openCurrentProviderPresetRenamePrompt(item.Value)
		case "preset.model":
			return m.openCurrentProviderPresetModelPicker(item.Value, true)
		case "preset.delete":
			return m.deleteCurrentProviderPreset(item.Value)
		}
		return nil
	case PaletteModePresetModels:
		switch item.Key {
		case "__back":
			return m.navigatePaletteBack()
		case "preset.model.entry":
			return m.applyCurrentProviderPresetModelSelection(item.Value)
		}
		return nil
	default:
		if item.Key != "__back" {
			if command, ok := m.catalog.LookupByKey(item.Key); ok {
				return m.executePaletteCommand(command, nil)
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
	order := []PaneFocus{FocusTranscript, FocusInput}
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
	if m.formPrompt != nil {
		m.state.Focus = FocusInput
	} else if m.state.Palette.Visible && !(m.state.Palette.Mode == PaletteModeRoot && m.state.Focus == FocusInput) {
		m.state.Focus = FocusPalette
	}
	var cmds []tea.Cmd
	if m.formPrompt != nil {
		m.input.Blur()
		m.paletteInput.Blur()
		if m.formPrompt != nil {
			cmds = append(cmds, m.formPrompt.Input.Focus())
		}
		return tea.Batch(cmds...)
	}
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
	case FocusTranscript:
		return m.localize("chat", "чат")
	case FocusPalette:
		return m.localize("command menu", "меню команд")
	default:
		return m.localize("input", "ввод")
	}
}

func (m *Model) paletteTitle() string {
	switch m.state.Palette.Mode {
	case PaletteModeResume:
		return m.localize("Resume Session", "Продолжить сессию")
	case PaletteModeFork:
		return m.localize("Fork Session", "Ответвить сессию")
	case PaletteModeStatus:
		return m.localize("Status", "Статус")
	case PaletteModeModel:
		return m.localize("Choose Model", "Выбор модели")
	case PaletteModeModelSettings:
		return m.localize("Model Settings", "Настройки моделей")
	case PaletteModeModelCatalog:
		return m.localize("All Models", "Все модели")
	case PaletteModeReasoning:
		return m.localize("Reasoning", "Размышления")
	case PaletteModeProfiles:
		return m.localize("Account Profiles", "Профили аккаунтов")
	case PaletteModeProfileActions:
		return m.localize("Profile Actions", "Действия профиля")
	case PaletteModeProviders:
		return m.localize("Model Providers", "Провайдеры моделей")
	case PaletteModeProviderActions:
		return m.localize("Provider Actions", "Действия провайдера")
	case PaletteModeAddAccount:
		return m.localize("Add Account", "Добавить аккаунт")
	case PaletteModeModelPresets:
		return m.localize("Model Presets", "Пресеты моделей")
	case PaletteModePresetEditor:
		return m.localize("Preset Editor", "Редактор пресетов")
	case PaletteModePresetActions:
		return m.localize("Preset Actions", "Действия пресета")
	case PaletteModePresetModels:
		return m.localize("Choose Preset Model", "Выбор модели для пресета")
	case PaletteModeSettings:
		return m.localize("Settings", "Настройки")
	case PaletteModeCustomization:
		return m.localize("Customization", "Кастомизация")
	case PaletteModeCustomizationColor:
		return m.localize("Color", "Цвет")
	case PaletteModeCustomizationColorChoice:
		return popupColorTargetLabel(m.customizationColorTarget, m.language)
	case PaletteModeCustomizationFormatting:
		return m.localize("Formatting", "Форматирование")
	case PaletteModeCustomizationFormattingTarget:
		return popupFormatTargetLabel(m.customizationFormatTarget, m.language)
	case PaletteModeLanguage:
		return m.localize("Interface Language", "Язык интерфейса")
	case PaletteModeCommandPrefix:
		return m.localize("Command Prefix", "Префикс команд")
	case PaletteModePopupCommands:
		return m.localize("Popup Commands", "Команды во всплывающем списке")
	case PaletteModePermissions:
		return m.localize("Access and Approvals", "Доступ и подтверждения")
	default:
		return m.localize("Commands", "Команды")
	}
}

func (m *Model) loadLatestSessionCmd(fork bool) tea.Cmd {
	return m.loadLatestSessionCmdWithOptions(fork, false)
}

func (m *Model) loadLatestSessionCmdWithOptions(fork bool, useStartupOverrides bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 0)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return sessionLoadedMsg{StartFresh: true}
			}
			return sessionLoadedMsg{Err: err}
		}
		if !fork {
			entries = m.filterSessionEntries(entries)
		}
		if len(entries) == 0 {
			return sessionLoadedMsg{StartFresh: true}
		}
		entry := entries[0]
		meta, messages, err := appstate.LoadSession(entry.Path)
		if useStartupOverrides {
			meta = m.applyStartupSessionOverrides(meta)
		}
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) loadSessionCmd(path string, fork bool) tea.Cmd {
	return m.loadSessionCmdWithOptions(path, fork, false)
}

func (m *Model) loadSessionCmdWithOptions(path string, fork bool, useStartupOverrides bool) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(path) == "" {
			return sessionLoadedMsg{Err: errors.New(m.localize("session path is empty", "путь к сессии пуст"))}
		}
		entry, err := appstate.ResolveSessionEntry(m.layout.SessionsDir(), path)
		if err != nil {
			return sessionLoadedMsg{Err: err}
		}
		meta, messages, err := appstate.LoadSession(entry.Path)
		if useStartupOverrides {
			meta = m.applyStartupSessionOverrides(meta)
		}
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) loadSelectedSessionCmd(selector string, fork bool) tea.Cmd {
	return m.loadSelectedSessionCmdWithOptions(selector, fork, false)
}

func (m *Model) loadSelectedSessionCmdWithOptions(selector string, fork bool, useStartupOverrides bool) tea.Cmd {
	return func() tea.Msg {
		entry, err := appstate.ResolveSessionEntry(m.layout.SessionsDir(), selector)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return sessionLoadedMsg{Err: errors.New(m.localize("saved session not found", "сохранённая сессия не найдена"))}
			}
			return sessionLoadedMsg{Err: err}
		}
		meta, messages, err := appstate.LoadSession(entry.Path)
		if useStartupOverrides {
			meta = m.applyStartupSessionOverrides(meta)
		}
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) openSessionsPalette(fork bool, pushCurrent bool) tea.Cmd {
	m.sessionPaletteStartup = false
	return m.openSessionsPaletteWithOptions(fork, pushCurrent, false)
}

func (m *Model) openStartupSessionsPalette(fork bool) tea.Cmd {
	m.sessionPaletteStartup = true
	return m.openSessionsPaletteWithOptions(fork, false, true)
}

func (m *Model) openSessionsPaletteWithOptions(fork bool, pushCurrent bool, startFreshOnEmpty bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 0)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if startFreshOnEmpty {
					return paletteItemsMsg{StartFresh: true}
				}
				return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
			}
			return paletteItemsMsg{Err: err}
		}
		if len(entries) == 0 {
			if startFreshOnEmpty {
				return paletteItemsMsg{StartFresh: true}
			}
			return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
		}
		entries, showingAllDirectories := m.filterSessionEntriesWithFallback(entries)
		if len(entries) == 0 {
			if startFreshOnEmpty {
				return paletteItemsMsg{StartFresh: true}
			}
			return paletteItemsMsg{Err: errors.New(m.localize("no saved sessions found", "сохранённые сессии не найдены"))}
		}
		m.sortSessionEntries(entries)
		if len(entries) > 50 {
			entries = entries[:50]
		}
		items := make([]PaletteItem, 0, len(entries))
		for _, entry := range entries {
			items = append(items, PaletteItem{
				Key:         "session",
				Title:       entry.Name,
				Description: m.sessionPaletteDescription(entry, showingAllDirectories),
				Value:       entry.Path,
				Aliases:     []string{entry.Name, entry.RelPath, entry.CWD, entry.Branch, entry.Preview},
				Keywords:    []string{entry.Name, entry.RelPath, entry.CWD, entry.Branch, entry.Preview},
			})
		}
		mode := PaletteModeResume
		footer := m.sessionPaletteFooter(false, showingAllDirectories)
		if fork {
			mode = PaletteModeFork
			footer = m.sessionPaletteFooter(true, showingAllDirectories)
		}
		return paletteItemsMsg{Mode: mode, Items: items, Footer: footer, PushCurrent: pushCurrent}
	}
}

func (m *Model) filterSessionEntries(entries []appstate.SessionEntry) []appstate.SessionEntry {
	if m.startup.ShowAll {
		return entries
	}
	currentCWD := appstate.ComparableSessionCWD(m.effectiveWorkingDirectory())
	if currentCWD == "" {
		return entries
	}
	filtered := make([]appstate.SessionEntry, 0, len(entries))
	for _, entry := range entries {
		entryCWD := appstate.ComparableSessionCWD(entry.CWD)
		if entryCWD == "" || entryCWD == currentCWD {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func (m *Model) filterSessionEntriesWithFallback(entries []appstate.SessionEntry) ([]appstate.SessionEntry, bool) {
	filtered := m.filterSessionEntries(entries)
	if len(filtered) > 0 || m.startup.ShowAll {
		return filtered, m.startup.ShowAll
	}
	currentCWD := strings.TrimSpace(m.effectiveWorkingDirectory())
	if currentCWD == "" || len(entries) == 0 {
		return filtered, false
	}
	return entries, true
}

func (m *Model) sortSessionEntries(entries []appstate.SessionEntry) {
	sort.SliceStable(entries, func(left, right int) bool {
		lhs := entries[left]
		rhs := entries[right]
		switch m.sessionSort {
		case sessionSortCreated:
			if lhs.Created.Equal(rhs.Created) {
				if lhs.ModTime.Equal(rhs.ModTime) {
					return lhs.RelPath < rhs.RelPath
				}
				return lhs.ModTime.After(rhs.ModTime)
			}
			return lhs.Created.After(rhs.Created)
		default:
			if lhs.ModTime.Equal(rhs.ModTime) {
				return lhs.RelPath < rhs.RelPath
			}
			return lhs.ModTime.After(rhs.ModTime)
		}
	})
}

func (m *Model) sessionPaletteDescription(entry appstate.SessionEntry, showingAllDirectories bool) string {
	parts := []string{entry.RelPath}
	switch m.sessionSort {
	case sessionSortCreated:
		parts = append(parts, fmt.Sprintf("%s %s", m.localize("created", "создан"), formatSessionPaletteTime(entry.Created)))
	default:
		parts = append(parts, fmt.Sprintf("%s %s", m.localize("updated", "обновлён"), formatSessionPaletteTime(entry.ModTime)))
	}
	if showingAllDirectories && strings.TrimSpace(entry.CWD) != "" {
		parts = append(parts, entry.CWD)
	}
	return strings.Join(parts, " · ")
}

func (m *Model) sessionPaletteFooter(fork bool, showingAllDirectories bool) string {
	action := m.localize("Select a saved session to resume", "Выберите сохранённую сессию для продолжения")
	if fork {
		action = m.localize("Select a saved session to fork", "Выберите сохранённую сессию для ответвления")
	}
	sortLabel := m.localize("updated", "обновление")
	if m.sessionSort == sessionSortCreated {
		sortLabel = m.localize("created", "создание")
	}
	scope := m.localize("current directory", "текущий каталог")
	if showingAllDirectories {
		scope = m.localize("all directories", "все каталоги")
	}
	return fmt.Sprintf("%s · %s: %s · Tab %s", action, m.localize("sort", "сортировка"), sortLabel, scope)
}

func (m *Model) startupCmd() tea.Cmd {
	switch m.startup.Mode {
	case StartupModeResumePicker:
		return m.openStartupSessionsPalette(false)
	case StartupModeForkPicker:
		return m.openStartupSessionsPalette(true)
	case StartupModeResumeLatest:
		return m.loadLatestSessionCmdWithOptions(false, true)
	case StartupModeForkLatest:
		return m.loadLatestSessionCmdWithOptions(true, true)
	case StartupModeResumeSelect:
		if strings.TrimSpace(m.startup.SessionSelector) == "" {
			return nil
		}
		return m.loadSelectedSessionCmdWithOptions(m.startup.SessionSelector, false, true)
	case StartupModeForkSelect:
		if strings.TrimSpace(m.startup.SessionSelector) == "" {
			return nil
		}
		return m.loadSelectedSessionCmdWithOptions(m.startup.SessionSelector, true, true)
	case StartupModeResumePath:
		if strings.TrimSpace(m.startup.SessionPath) == "" {
			return nil
		}
		return m.loadSessionCmdWithOptions(m.startup.SessionPath, false, true)
	case StartupModeForkPath:
		if strings.TrimSpace(m.startup.SessionPath) == "" {
			return nil
		}
		return m.loadSessionCmdWithOptions(m.startup.SessionPath, true, true)
	case StartupModeModel:
		m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
		return m.openModelPickerPalette(false)
	case StartupModeModelPresets:
		m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
		return m.openModelPresetsSettingsPalette(false)
	case StartupModeProfiles:
		m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
		return m.openProfilesPalette(false)
	case StartupModeProviders:
		m.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)
		return m.openProvidersPalette(false)
	case StartupModeSettings:
		return m.openPaletteMode(PaletteModeSettings, false)
	case StartupModeLanguage:
		return m.openPaletteMode(PaletteModeLanguage, false)
	case StartupModePermissions:
		return m.openPaletteMode(PaletteModePermissions, false)
	default:
		return nil
	}
}

func (m *Model) resetConversation() {
	m.history = nil
	m.approvalStore = taskrun.NewApprovalSessionStore()
	m.sessionPaletteStartup = false
	m.state.LiveTurn = nil
	m.state.Transient = nil
	m.state.Transcript = defaultStateForLanguage(m.language).Transcript
	m.state.SessionPath = ""
	m.state.Footer = m.localize("Started a new session", "Начата новая сессия")
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) clearTranscriptView() {
	m.state.Transcript = nil
	m.state.Transient = nil
	m.state.LiveTurn = nil
	m.state.Footer = m.localize("Screen cleared", "Экран очищен")
	m.refreshViewport()
}

func (m *Model) consumeStartupPrompt() tea.Cmd {
	prompt := strings.TrimSpace(m.startup.InitialPrompt)
	if prompt == "" {
		return nil
	}
	m.startup.InitialPrompt = ""
	m.input.SetValue(prompt)
	m.state.InputDraft = prompt
	return m.submitInput()
}

func (m *Model) updateStatus() {
	m.state.Status = buildStatusItems(m.layout, m.state, len(m.history))
}

func (m *Model) appendTranscript(role string, body string) {
	m.state.Transcript = appendTranscriptEntry(m.state.Transcript, role, body)
	m.refreshViewport()
}

func (m *Model) appendTransientTranscript(entry TranscriptEntry) {
	m.state.Transient = appendTranscriptEntryDedup(m.state.Transient, entry)
	m.refreshViewport()
}

func (m *Model) flushLiveTurnEntries() {
	if m.state.LiveTurn == nil {
		return
	}
	for _, entry := range m.state.LiveTurn.Entries {
		m.state.Transcript = appendTranscriptEntryDedup(m.state.Transcript, entry)
	}
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
		fmt.Sprintf("- %s: %s", m.localize("approval mode", "режим подтверждений"), m.approvalModeLabel(tooling.ToolApprovalMode(summary.ToolApprovalMode))),
		fmt.Sprintf("- %s: %t", m.localize("parallel tools", "параллельные инструменты"), summary.ToolParallelEnabled),
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
	state.Transcript = nil
	state.Palette.Items = defaultPaletteItemsForLanguage(language)
	state.Palette.Context = defaultPaletteContextForLanguage(language)
	state.Footer = ""
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

func busyTickCmd() tea.Cmd {
	return tea.Tick(busyTickInterval, func(time.Time) tea.Msg {
		return busyTickMsg{}
	})
}

func (m *Model) applyTaskProgress(update taskrun.ProgressUpdate) {
	if m.state.LiveTurn == nil {
		m.state.LiveTurn = &LiveTurnState{StartedAt: time.Now()}
	}
	live := m.state.LiveTurn
	if live.StartedAt.IsZero() {
		live.StartedAt = time.Now()
	}
	if update.Round > 0 {
		live.Round = update.Round
	}
	if strings.TrimSpace(update.Prompt) != "" {
		if !m.hasTrailingTranscriptEntry("user", update.Prompt) {
			live.Prompt = update.Prompt
		} else {
			live.Prompt = ""
		}
	}
	switch update.Kind {
	case taskrun.ProgressKindTurnStarted:
		m.state.Footer = m.localize("Running turn...", "Выполняется ход...")
	case taskrun.ProgressKindAssistantSnapshot:
		live.AssistantText = update.Snapshot.Text
		live.ToolCalls = cloneRuntimeToolCalls(update.Snapshot.ToolCalls)
	case taskrun.ProgressKindToolPlanned:
		if update.ToolPlan != nil {
			live.Entries = appendTranscriptEntryDedup(live.Entries, renderToolPlanEntry(m.language, update.ToolPlan))
		}
	case taskrun.ProgressKindApprovalRequired:
		if update.ApprovalRequest != nil {
			live.Entries = appendTranscriptEntryDedup(live.Entries, renderApprovalEntry(m.language, update.ApprovalRequest))
		}
	case taskrun.ProgressKindToolResult:
		if update.ToolResult != nil {
			live.Entries = appendTranscriptEntryDedup(live.Entries, renderToolResultEntry(m.language, update.ToolResult))
		}
	case taskrun.ProgressKindRetryScheduled:
		if update.RetryAfter > 0 || update.Err != nil {
			live.Entries = appendTranscriptEntryDedup(live.Entries, renderRetryEntry(m.language, update.RetryAfter, update.Err))
		}
	}
	m.state.LiveTurn = live
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) hasTrailingTranscriptEntry(role string, body string) bool {
	role = strings.TrimSpace(strings.ToLower(role))
	body = normalizeTranscriptBody(body)
	if role == "" || body == "" || len(m.state.Transcript) == 0 {
		return false
	}
	last := m.state.Transcript[len(m.state.Transcript)-1]
	return strings.EqualFold(strings.TrimSpace(last.Role), role) && normalizeTranscriptBody(last.Body) == body
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
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Approved for session", "Разрешено на сессию"), m.approvalDecisionSubject(request))
	case taskrun.ApprovalDecisionApprove:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Approved", "Разрешено"), m.approvalDecisionSubject(request))
	default:
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Denied", "Запрещено"), m.approvalDecisionSubject(request))
	}
	m.refreshViewport()
	return nil
}

func (m *Model) approvalTitle(request tooling.ApprovalRequest) string {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return m.localize("Additional Permissions Requested", "Запрошены дополнительные разрешения")
	case tooling.ApprovalKindShellCommand:
		return m.localize("Shell Command Requires Approval", "Команда shell требует подтверждения")
	case tooling.ApprovalKindApplyPatch:
		return m.localize("Patch Requires Approval", "Патч требует подтверждения")
	case tooling.ApprovalKindWorkspaceWrite:
		return m.localize("Write Requires Approval", "Запись требует подтверждения")
	case tooling.ApprovalKindReadOnly:
		return m.localize("Tool Requires Approval", "Инструмент требует подтверждения")
	default:
		return m.localize("Approval Required", "Нужно подтверждение")
	}
}

func (m *Model) approvalKindHint(request tooling.ApprovalRequest) string {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return m.localize("The model is asking for extra write access before continuing.", "Модель просит дополнительный доступ на запись перед продолжением.")
	case tooling.ApprovalKindShellCommand:
		return m.localize("This will start a subprocess and may change files.", "Это запустит подпроцесс и может изменить файлы.")
	case tooling.ApprovalKindApplyPatch:
		return m.localize("This will edit files through an inline patch.", "Это изменит файлы через встроенный патч.")
	case tooling.ApprovalKindWorkspaceWrite:
		return m.localize("This tool will write directly into the workspace.", "Этот инструмент будет писать прямо в рабочую папку.")
	case tooling.ApprovalKindReadOnly:
		return m.localize("This tool is read-only but is still gated by the current policy.", "Этот инструмент только читает, но всё ещё ограничен текущей политикой.")
	default:
		return ""
	}
}

func (m *Model) approvalActionLabels(request tooling.ApprovalRequest) (string, string, string) {
	switch tooling.ApprovalKindForRequest(request) {
	case tooling.ApprovalKindPermissionRequest:
		return m.localize("grant for this turn", "дать доступ на этот ход"), m.localize("grant for this session", "дать доступ на всю сессию"), m.localize("deny", "запретить")
	case tooling.ApprovalKindShellCommand:
		return m.localize("run once", "запустить один раз"), m.localize("allow shell for session", "разрешить shell на сессию"), m.localize("deny", "запретить")
	case tooling.ApprovalKindApplyPatch:
		return m.localize("apply once", "применить один раз"), m.localize("allow patches for session", "разрешить патчи на сессию"), m.localize("deny", "запретить")
	case tooling.ApprovalKindWorkspaceWrite:
		return m.localize("write once", "записать один раз"), m.localize("allow writes for session", "разрешить запись на сессию"), m.localize("deny", "запретить")
	default:
		return m.localize("approve once", "разрешить один раз"), m.localize("approve for session", "разрешить на сессию"), m.localize("deny", "запретить")
	}
}

func (m *Model) approvalResourcePreview(request tooling.ApprovalRequest) string {
	if len(request.Metadata.ResourceKeys) == 0 {
		return ""
	}
	preview := make([]string, 0, minInt(len(request.Metadata.ResourceKeys), 3))
	for _, value := range request.Metadata.ResourceKeys {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		for _, prefix := range []string{"file:", "dir:", "tree:", "cwd:", "tool:", "writable_root:"} {
			value = strings.TrimPrefix(value, prefix)
		}
		if value == "" {
			continue
		}
		preview = append(preview, value)
		if len(preview) == 3 {
			break
		}
	}
	return strings.Join(preview, ", ")
}

func (m *Model) approvalDecisionSubject(request tooling.ApprovalRequest) string {
	if summary := strings.TrimSpace(request.Summary); summary != "" {
		return summary
	}
	if strings.TrimSpace(request.Name) != "" {
		return request.Name
	}
	return m.localize("tool request", "запрос инструмента")
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

func (m *Model) refreshCurrentPaletteItems() {
	m.reloadStyleSettings()
	if !m.state.Palette.Visible {
		return
	}
	if m.state.Palette.Mode == PaletteModeRoot {
		m.state.Palette.Items = m.rootPaletteItemsForQuery(m.state.Palette.Query)
	} else {
		m.state.Palette.Items = m.decoratePaletteItems(m.state.Palette.Mode, m.paletteItemsForMode(m.state.Palette.Mode))
	}
	m.state.Palette.Context = normalizePaletteContextForLanguage(m.state.Palette.Context, m.language)
	m.syncPaletteSelection()
	m.resize()
}

func (m *Model) settingsForUI() appstate.Settings {
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		return appstate.DefaultSettings()
	}
	return settings
}

func (m *Model) filterHiddenPaletteCommands(items []PaletteItem) []PaletteItem {
	settings := m.settingsForUI()
	if len(settings.HiddenCommands) == 0 || m.catalog == nil {
		return items
	}
	filtered := make([]PaletteItem, 0, len(items))
	for _, item := range items {
		spec, ok := m.catalog.LookupByKey(item.Key)
		if !ok {
			filtered = append(filtered, item)
			continue
		}
		if settings.HasHiddenCommand(paletteCommandVisibilityKey(spec)) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func (m *Model) languagePaletteItems() []PaletteItem {
	current := normalizeTUILanguage(m.settingsForUI().Language)
	return []PaletteItem{
		{
			Key:         "settings.language.option",
			Title:       "English",
			Description: m.activeChoiceLabel(current == commandcatalog.CatalogLanguageEnglish),
			Value:       "en",
			Keywords:    []string{"english", "en", "language", "английский"},
		},
		{
			Key:         "settings.language.option",
			Title:       "Русский",
			Description: m.activeChoiceLabel(current == commandcatalog.CatalogLanguageRussian),
			Value:       "ru",
			Keywords:    []string{"russian", "ru", "language", "русский"},
		},
	}
}

func (m *Model) commandPrefixPaletteItems() []PaletteItem {
	current := commandPrefixFromSettings(m.settingsForUI())
	return []PaletteItem{
		{
			Key:         "settings.command_prefix.option",
			Title:       "/",
			Description: m.activeChoiceLabel(current == "/"),
			Value:       "/",
			Keywords:    []string{"slash", "/", "prefix", "команды"},
		},
		{
			Key:         "settings.command_prefix.option",
			Title:       ".",
			Description: m.activeChoiceLabel(current == "."),
			Value:       ".",
			Keywords:    []string{"dot", ".", "prefix", "команды"},
		},
	}
}

func (m *Model) popupCommandPaletteItems() []PaletteItem {
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	settings := m.settingsForUI()
	base := m.catalog.RootItems(m.language, "")
	items := make([]PaletteItem, 0, len(base))
	for _, item := range base {
		spec, ok := m.catalog.LookupByKey(item.Key)
		if !ok {
			continue
		}
		visibilityKey := paletteCommandVisibilityKey(spec)
		stateLabel := m.localize("visible", "показана")
		if settings.HasHiddenCommand(visibilityKey) {
			stateLabel = m.localize("hidden", "скрыта")
		}
		description := strings.TrimSpace(item.Description)
		if description != "" {
			description = stateLabel + " · " + description
		} else {
			description = stateLabel
		}
		keywords := append([]string{visibilityKey, stateLabel}, item.Keywords...)
		items = append(items, PaletteItem{
			Key:         "settings.hidden_commands.entry",
			Title:       item.Title,
			Description: description,
			Value:       visibilityKey,
			Aliases:     append([]string{}, item.Aliases...),
			Keywords:    keywords,
		})
	}
	return items
}

func (m *Model) permissionsPaletteItems() []PaletteItem {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	items := []PaletteItem{
		{
			Key:         "settings.permissions.approval_mode",
			Title:       m.localize("Approval Mode", "Режим подтверждений"),
			Description: m.approvalModeLabel(policy.ApprovalMode),
			Keywords:    []string{"approval", "mode", "approvals", "подтверждения"},
		},
		{
			Key:         "settings.permissions.block_mutating",
			Title:       m.localize("Mutating Tools", "Изменяющие инструменты"),
			Description: m.onOffLabel(!policy.BlockMutatingTools, m.localize("allowed", "разрешены"), m.localize("blocked", "запрещены")),
			Keywords:    []string{"mutating", "write", "tools", "изменение", "запись"},
		},
		{
			Key:         "settings.permissions.block_shell",
			Title:       m.localize("Shell Commands", "Shell-команды"),
			Description: m.onOffLabel(!policy.BlockShellCommands, m.localize("allowed", "разрешены"), m.localize("blocked", "запрещены")),
			Keywords:    []string{"shell", "commands", "bash", "sh"},
		},
		{
			Key:         "settings.permissions.parallel",
			Title:       m.localize("Parallel Tools", "Параллельные инструменты"),
			Description: m.onOffLabel(policy.Planning.AllowParallel, m.localize("enabled", "включены"), m.localize("disabled", "выключены")),
			Keywords:    []string{"parallel", "planning", "tools", "параллельно"},
		},
		{
			Key:         "settings.permissions.parallelism",
			Title:       m.localize("Parallelism", "Параллелизм"),
			Description: fmt.Sprintf("%d", maxInt(1, policy.Planning.MaxParallelCalls)),
			Keywords:    []string{"parallel", "planning", "count", "число", "параллельно"},
		},
	}
	items = append(items, m.toolListPaletteItem("settings.permissions.allowed_tools", m.localize("Allowed Tools", "Разрешённые инструменты"), policy.AllowedTools)...)
	items = append(items, m.toolListPaletteItem("settings.permissions.blocked_tools", m.localize("Blocked Tools", "Запрещённые инструменты"), policy.BlockedTools)...)
	return items
}

func (m *Model) toolListPaletteItem(key string, title string, values []string) []PaletteItem {
	if len(values) == 0 {
		return []PaletteItem{{
			Key:         key,
			Title:       title,
			Description: localizedUnsetTUI(m.language),
			Keywords:    []string{title},
		}}
	}
	return []PaletteItem{{
		Key:         key,
		Title:       title,
		Description: strings.Join(values, ", "),
		Keywords:    append([]string{title}, values...),
	}}
}

func (m *Model) activeChoiceLabel(active bool) string {
	if active {
		return m.localize("current", "текущая")
	}
	return m.localize("available", "доступна")
}

func (m *Model) onOffLabel(enabled bool, enabledText string, disabledText string) string {
	if enabled {
		return enabledText
	}
	return disabledText
}

func (m *Model) toolPolicySummary() string {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	mode := m.approvalModeLabel(policy.ApprovalMode)
	parts := []string{mode}
	if policy.BlockMutatingTools {
		parts = append(parts, m.localize("no writes", "без записи"))
	}
	if policy.BlockShellCommands {
		parts = append(parts, m.localize("no shell", "без shell"))
	}
	if !policy.Planning.AllowParallel {
		parts = append(parts, m.localize("sequential", "без параллели"))
	} else {
		parts = append(parts, fmt.Sprintf("%s %d", m.localize("parallel", "параллель"), maxInt(1, policy.Planning.MaxParallelCalls)))
	}
	return strings.Join(parts, " · ")
}

func (m *Model) setSettingsLanguage(next string) tea.Cmd {
	next = strings.ToLower(strings.TrimSpace(next))
	if next != "ru" {
		next = "en"
	}
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SetLanguage(next)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.language = normalizeTUILanguage(next)
	m.state.Language = string(m.language)
	m.state.Title = "Go Lavilas"
	m.applyStyleSettings(settings)
	m.input.Placeholder = composerPlaceholder(m.language)
	m.paletteInput.Placeholder = localizedTextTUI(m.language, "Type to filter items", "Введите запрос для фильтрации")
	m.state.Footer = localizedTextTUI(m.language, "Language updated", "Язык обновлён")
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) approvalModeLabel(mode tooling.ToolApprovalMode) string {
	switch tooling.NormalizeToolPolicy(tooling.ToolPolicy{ApprovalMode: mode}).ApprovalMode {
	case tooling.ToolApprovalModeRequire:
		return m.localize("require", "требовать")
	case tooling.ToolApprovalModeDeny:
		return m.localize("deny", "запретить")
	default:
		return m.localize("auto", "авто")
	}
}

func (m *Model) saveToolPolicy(policy tooling.ToolPolicy, footer string) tea.Cmd {
	policy = tooling.NormalizeToolPolicy(policy)
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SetToolPolicy(policy)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.options.ToolPolicy = policy
	if strings.TrimSpace(footer) != "" {
		m.state.Footer = footer
	}
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) cycleApprovalMode() tea.Cmd {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	switch policy.ApprovalMode {
	case tooling.ToolApprovalModeAuto:
		policy.ApprovalMode = tooling.ToolApprovalModeRequire
	case tooling.ToolApprovalModeRequire:
		policy.ApprovalMode = tooling.ToolApprovalModeDeny
	default:
		policy.ApprovalMode = tooling.ToolApprovalModeAuto
	}
	return m.saveToolPolicy(policy, fmt.Sprintf("%s: %s", m.localize("Approval mode", "Режим подтверждений"), m.approvalModeLabel(policy.ApprovalMode)))
}

func (m *Model) toggleBlockMutatingTools() tea.Cmd {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	policy.BlockMutatingTools = !policy.BlockMutatingTools
	footer := m.localize("Mutating tools allowed", "Изменяющие инструменты разрешены")
	if policy.BlockMutatingTools {
		footer = m.localize("Mutating tools blocked", "Изменяющие инструменты запрещены")
	}
	return m.saveToolPolicy(policy, footer)
}

func (m *Model) toggleBlockShellCommands() tea.Cmd {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	policy.BlockShellCommands = !policy.BlockShellCommands
	footer := m.localize("Shell commands allowed", "Shell-команды разрешены")
	if policy.BlockShellCommands {
		footer = m.localize("Shell commands blocked", "Shell-команды запрещены")
	}
	return m.saveToolPolicy(policy, footer)
}

func (m *Model) toggleParallelTools() tea.Cmd {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	policy.Planning.AllowParallel = !policy.Planning.AllowParallel
	if policy.Planning.MaxParallelCalls < 1 {
		policy.Planning.MaxParallelCalls = tooling.DefaultPlanningPolicy().MaxParallelCalls
	}
	footer := m.localize("Parallel tools enabled", "Параллельные инструменты включены")
	if !policy.Planning.AllowParallel {
		footer = m.localize("Parallel tools disabled", "Параллельные инструменты выключены")
	}
	return m.saveToolPolicy(policy, footer)
}

func (m *Model) cycleToolParallelism() tea.Cmd {
	policy := tooling.NormalizeToolPolicy(m.options.ToolPolicy)
	current := maxInt(1, policy.Planning.MaxParallelCalls)
	switch current {
	case 1:
		policy.Planning.MaxParallelCalls = 2
	case 2:
		policy.Planning.MaxParallelCalls = 4
	case 4:
		policy.Planning.MaxParallelCalls = 8
	default:
		policy.Planning.MaxParallelCalls = 1
	}
	policy.Planning.AllowParallel = policy.Planning.MaxParallelCalls > 1
	return m.saveToolPolicy(policy, fmt.Sprintf("%s: %d", m.localize("Parallelism", "Параллелизм"), policy.Planning.MaxParallelCalls))
}

func (m *Model) toggleSettingsLanguage() tea.Cmd {
	next := "ru"
	if normalizeTUILanguage(m.settingsForUI().Language) == commandcatalog.CatalogLanguageRussian {
		next = "en"
	}
	return m.setSettingsLanguage(next)
}

func (m *Model) setSettingsCommandPrefix(next string) tea.Cmd {
	next = strings.TrimSpace(next)
	if next == "" {
		next = "/"
	}
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SetCommandPrefix(next)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Command prefix updated", "Префикс команд обновлён"), next)
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) cycleCommandPrefix() tea.Cmd {
	current := commandPrefixFromSettings(m.settingsForUI())
	next := "."
	if current == "." {
		next = "/"
	}
	return m.setSettingsCommandPrefix(next)
}

func (m *Model) setSelectionHighlightPreset(preset string) tea.Cmd {
	return m.setPopupColorChoice(popupColorTargetSelection, preset)
}

func (m *Model) setPopupColorChoice(target popupColorTarget, raw string) tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	choice := parseUIColorChoice(raw, settings.SelectionHighlight.Preset)
	value := strings.TrimSpace(raw)
	switch choice.kind {
	case uiColorChoicePreset:
		value = normalizedSelectionPreset(choice.preset)
	case uiColorChoiceCustom:
		value = choice.hex
	default:
		value = "auto"
	}
	switch target {
	case popupColorTargetSelection:
		if choice.kind == uiColorChoicePreset {
			settings.SelectionHighlight.Preset = normalizedSelectionPreset(choice.preset)
		}
		settings.SelectionHighlight.Color = value
	case popupColorTargetListPrimary:
		settings.Colors.ListPrimary = value
	case popupColorTargetListSecondary:
		settings.Colors.ListSecondary = value
	case popupColorTargetReply:
		settings.Colors.ReplyText = value
	case popupColorTargetReasoning:
		settings.Colors.ReasoningText = value
	case popupColorTargetCommand:
		settings.Colors.CommandText = value
	case popupColorTargetCommandOutput:
		settings.Colors.CommandOutput = value
	}
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Color updated", "Цвет обновлён"), m.popupColorTargetSummary(settings, target))
	m.reloadStyleSettings()
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) toggleSelectionHighlightTextFormat(code string) tea.Cmd {
	return m.togglePopupTextFormat(popupFormatTargetSelection, code)
}

func (m *Model) togglePopupTextFormat(target popupFormatTarget, code string) tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	switch target {
	case popupFormatTargetSelection:
		settings.TextFormats.SelectionHighlight = settings.TextFormats.SelectionHighlight.Toggle(code)
	case popupFormatTargetListPrimary:
		settings.TextFormats.ListPrimary = settings.TextFormats.ListPrimary.Toggle(code)
	case popupFormatTargetListSecondary:
		settings.TextFormats.ListSecondary = settings.TextFormats.ListSecondary.Toggle(code)
	case popupFormatTargetReply:
		settings.TextFormats.Reply = settings.TextFormats.Reply.Toggle(code)
	case popupFormatTargetReasoning:
		settings.TextFormats.Reasoning = settings.TextFormats.Reasoning.Toggle(code)
	case popupFormatTargetCommand:
		settings.TextFormats.Command = settings.TextFormats.Command.Toggle(code)
	case popupFormatTargetCommandOutput:
		settings.TextFormats.CommandOutput = settings.TextFormats.CommandOutput.Toggle(code)
	}
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", popupFormatTargetLabel(target, m.language), m.popupFormatTargetSummary(settings, target))
	m.reloadStyleSettings()
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) toggleSelectionHighlightFill() tea.Cmd {
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SelectionHighlight.Fill = !effectiveSelectionHighlightFill(settings)
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	if settings.SelectionHighlight.Fill {
		m.state.Footer = m.localize("Selection fill enabled", "Заливка выделения включена")
	} else {
		m.state.Footer = m.localize("Selection text mode enabled", "Текстовый режим выделения включён")
	}
	m.reloadStyleSettings()
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) cycleSelectionHighlightColor() tea.Cmd {
	settings, err := loadSettingsOptional(m.layout.SettingsPath())
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	palette := []string{"light", "graphite", "amber", "mint", "rose"}
	current := normalizedSelectionPreset(settings.SelectionHighlight.Preset)
	index := 0
	for i, candidate := range palette {
		if strings.EqualFold(candidate, current) {
			index = (i + 1) % len(palette)
			break
		}
	}
	return m.setPopupColorChoice(popupColorTargetSelection, palette[index])
}

func (m *Model) togglePopupCommandVisibility(name string) tea.Cmd {
	name = normalizePaletteCommandName(name)
	if name == "" {
		return nil
	}
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	if settings.HasHiddenCommand(name) {
		settings.ShowCommand(name)
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Shown in popup", "Показана во всплывающем списке"), name)
	} else {
		settings.HideCommand(name)
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Hidden from popup", "Скрыта из всплывающего списка"), name)
	}
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.refreshCurrentPaletteItems()
	m.updateStatus()
	m.refreshViewport()
	return nil
}

func (m *Model) rootPaletteItemsForQuery(query string) []PaletteItem {
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	return m.filterHiddenPaletteCommands(m.catalog.RootItems(m.language, query))
}

func (m *Model) resolveSlashCommand(name string) (PaletteCommandSpec, bool) {
	needle := strings.TrimSpace(name)
	if needle == "" {
		return PaletteCommandSpec{}, false
	}
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	command, ok := m.catalog.LookupBySlash(needle)
	if !ok {
		return PaletteCommandSpec{}, false
	}
	settings := m.settingsForUI()
	if settings.HasHiddenCommand(paletteCommandVisibilityKey(command)) {
		return PaletteCommandSpec{}, false
	}
	return command, true
}

func buildSessionEntry(root string, path string, info os.FileInfo) appstate.SessionEntry {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		relPath = filepath.Base(path)
	}
	name := filepath.Base(path)
	meta, _ := appstate.LoadSessionMeta(path)
	createdAt := info.ModTime()
	if !meta.CreatedAt.IsZero() {
		createdAt = meta.CreatedAt.UTC()
	}
	modTime := info.ModTime()
	if !meta.UpdatedAt.IsZero() {
		modTime = meta.UpdatedAt.UTC()
	}
	return appstate.SessionEntry{
		ID:      strings.TrimSuffix(name, filepath.Ext(name)),
		Name:    name,
		Path:    path,
		RelPath: relPath,
		CWD:     meta.CWD,
		Created: createdAt,
		ModTime: modTime,
		Size:    info.Size(),
	}
}

func (m *Model) toggleSessionSort() tea.Cmd {
	if m.sessionSort == sessionSortUpdated {
		m.sessionSort = sessionSortCreated
	} else {
		m.sessionSort = sessionSortUpdated
	}
	items, showingAllDirectories, err := m.currentSessionPaletteItems()
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to refresh sessions", "Не удалось обновить сессии"), err)
		return nil
	}
	currentToken := strings.TrimSpace(m.state.Palette.SelectedToken)
	m.state.Palette.Items = m.decoratePaletteItems(m.state.Palette.Mode, items)
	m.state.Palette.SelectedToken = currentToken
	m.state.Footer = m.sessionPaletteFooter(m.state.Palette.Mode == PaletteModeFork, showingAllDirectories)
	m.syncPaletteSelection()
	m.refreshViewport()
	return nil
}

func (m *Model) currentSessionPaletteItems() ([]PaletteItem, bool, error) {
	entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 0)
	if err != nil {
		return nil, false, err
	}
	entries, showingAllDirectories := m.filterSessionEntriesWithFallback(entries)
	m.sortSessionEntries(entries)
	if len(entries) > 50 {
		entries = entries[:50]
	}
	items := make([]PaletteItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, PaletteItem{
			Key:         "session",
			Title:       entry.Name,
			Description: m.sessionPaletteDescription(entry, showingAllDirectories),
			Value:       entry.Path,
			Aliases:     []string{entry.Name, entry.RelPath, entry.CWD, entry.Branch, entry.Preview},
			Keywords:    []string{entry.Name, entry.RelPath, entry.CWD, entry.Branch, entry.Preview},
		})
	}
	return items, showingAllDirectories, nil
}

func formatSessionPaletteTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
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
		return appstate.DefaultSettings(), nil
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

func (m *Model) applyStartupSessionOverrides(meta appstate.SessionMeta) appstate.SessionMeta {
	options := m.startup.TaskOptions
	if strings.TrimSpace(options.Model) != "" {
		meta.Model = strings.TrimSpace(options.Model)
	}
	if strings.TrimSpace(options.Profile) != "" {
		meta.Profile = strings.TrimSpace(options.Profile)
	}
	if strings.TrimSpace(options.Provider) != "" {
		meta.Provider = strings.TrimSpace(options.Provider)
	}
	if strings.TrimSpace(options.ReasoningEffort) != "" {
		meta.Reasoning = strings.TrimSpace(options.ReasoningEffort)
	}
	if strings.TrimSpace(options.CWD) != "" {
		meta.CWD = strings.TrimSpace(options.CWD)
	}
	return meta
}

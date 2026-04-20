package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	runtimeapi "github.com/Perdonus/lavilas-code/internal/runtime"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
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

type sessionLoadedMsg struct {
	Entry    appstate.SessionEntry
	Meta     appstate.SessionMeta
	Messages []runtimeapi.Message
	Fork     bool
	Err      error
}

type paletteItemsMsg struct {
	Mode   PaletteMode
	Items  []PaletteItem
	Footer string
	Err    error
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
	history      []runtimeapi.Message
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
	styles := newStyles()

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Send a message to the Go alpha session"
	input.TextStyle = styles.value
	input.PlaceholderStyle = styles.muted
	input.PromptStyle = styles.sectionTitle
	input.Cursor.Style = styles.paneTitle
	input.SetValue(state.InputDraft)

	paletteInput := textinput.New()
	paletteInput.Placeholder = "Type to filter items"
	paletteInput.TextStyle = styles.value
	paletteInput.PlaceholderStyle = styles.muted
	paletteInput.Cursor.Style = styles.paneTitle
	paletteInput.SetValue(state.Palette.Query)

	model := &Model{
		state:        state.clone(),
		keys:         DefaultKeyMap(),
		styles:       styles,
		viewport:     viewport.New(0, 0),
		input:        input,
		paletteInput: paletteInput,
		layout:       apphome.DefaultLayout(),
	}
	model.refreshViewport()
	return model
}

func newModel(options Options) (*Model, error) {
	layout := apphome.DefaultLayout()
	config, _ := loadConfigOptional(layout.ConfigPath())
	settings, _ := loadSettingsOptional(layout.SettingsPath())
	state := DefaultState()
	state.Title = fmt.Sprintf("Go Lavilas %s", version.Version)
	state.Footer = buildFooter(settings)
	state.Model = fallback(options.TaskOptions.Model, config.EffectiveModel())
	state.Provider = fallback(options.TaskOptions.Provider, config.EffectiveProviderName())
	state.Profile = fallback(options.TaskOptions.Profile, config.ActiveProfileName())
	state.Reasoning = fallback(options.TaskOptions.ReasoningEffort, config.EffectiveReasoningEffort())
	state.Status = buildStatusItems(layout, state, 0)
	model := NewModel(state)
	model.layout = layout
	model.options = options.TaskOptions
	return model, nil
}

func (m *Model) Init() tea.Cmd {
	if m.state.Palette.Visible {
		return m.setFocus(FocusPalette)
	}
	return m.setFocus(FocusInput)
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
		if msg.Err != nil {
			m.appendTranscript("system", fmt.Sprintf("run failed: %v", msg.Err))
			m.state.Footer = fmt.Sprintf("Last error: %v", msg.Err)
			m.updateStatus()
			return m, nil
		}
		m.applyTaskResult(msg)
		return m, nil
	case sessionLoadedMsg:
		if msg.Err != nil {
			m.appendTranscript("system", msg.Err.Error())
			m.state.Footer = msg.Err.Error()
			m.updateStatus()
			return m, nil
		}
		m.applyLoadedSession(msg)
		return m, nil
	case paletteItemsMsg:
		if msg.Err != nil {
			m.appendTranscript("system", msg.Err.Error())
			m.state.Footer = msg.Err.Error()
			m.updateStatus()
			return m, nil
		}
		m.state.Palette.Visible = true
		m.state.Palette.Mode = msg.Mode
		m.state.Palette.Query = ""
		m.state.Palette.Items = clonePaletteItems(msg.Items)
		m.state.Palette.Selected = 0
		m.paletteInput.Reset()
		m.state.Footer = msg.Footer
		m.resize()
		return m, m.setFocus(FocusPalette)
	case tea.KeyMsg:
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
					m.styles.paneTitle.Render("Go Lavilas"),
					m.styles.muted.Render("Waiting for the first window size event..."),
				),
			),
		)
	}

	statusPane := m.renderStatusPane()
	mainSections := make([]string, 0, 3)
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
	m.input.SetValue(m.state.InputDraft)
	m.paletteInput.SetValue(m.state.Palette.Query)
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
		m.styles.muted.Render("Standalone alpha runtime"),
		"",
		m.styles.sectionTitle.Render("Status"),
		m.renderStatusItems(),
		"",
		m.styles.sectionTitle.Render("Focus"),
		m.styles.value.Render(m.focusLabel()),
		m.styles.label.Render("messages") + " " + m.styles.value.Render(fmt.Sprintf("%d", len(m.state.Transcript))),
	}
	if m.state.Busy {
		content = append(content, m.styles.label.Render("turn")+" "+m.styles.busy.Render("running"))
	}
	content = append(content,
		"",
		m.styles.sectionTitle.Render("Keys"),
		m.renderBindings(m.keys.ShortHelp()),
	)
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderTranscriptPane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusTranscript).Width(m.mainWidth)
	header := m.styles.paneTitle.Render("Transcript") + " " + m.styles.muted.Render(fmt.Sprintf("%d entries", len(m.state.Transcript)))
	body := m.viewport.View()
	if strings.TrimSpace(body) == "" {
		body = m.styles.muted.Render("Transcript viewport is empty.")
	}
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, header, body))
}

func (m *Model) renderInputPane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusInput).Width(m.mainWidth)
	footer := m.state.Footer
	if strings.TrimSpace(footer) == "" {
		footer = "Enter submit · Ctrl+P palette"
	}
	if m.state.Busy {
		footer = "Running turn..."
	}
	content := []string{
		m.styles.paneTitle.Render("Input"),
		m.input.View(),
		m.styles.muted.Render(footer),
	}
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderPalettePane() string {
	pane := applyPaneFocus(m.styles.pane, m.styles.paneActive, m.state.Focus == FocusPalette).Width(m.mainWidth)
	items := m.filteredPaletteItems()
	content := []string{
		m.styles.paneTitle.Render(m.paletteTitle()),
		m.paletteInput.View(),
	}
	if len(items) == 0 {
		content = append(content, m.styles.muted.Render("No items match the current filter."))
	} else {
		limit := minInt(len(items), 6)
		selected := clampInt(m.state.Palette.Selected, 0, maxInt(0, len(items)-1))
		entryWidth := maxInt(1, innerWidth(m.styles.pane, m.mainWidth))
		for index := 0; index < limit; index++ {
			entry := fmt.Sprintf("%s  %s", items[index].Title, items[index].Description)
			style := m.styles.body.Width(entryWidth)
			if index == selected {
				style = m.styles.selected.Width(entryWidth)
			}
			content = append(content, style.Render(entry))
		}
	}
	content = append(content, m.styles.muted.Render("Enter select · Esc close"))
	return pane.Render(lipgloss.JoinVertical(lipgloss.Left, content...))
}

func (m *Model) renderStatusItems() string {
	if len(m.state.Status) == 0 {
		return m.styles.muted.Render("No status items yet.")
	}
	lines := make([]string, 0, len(m.state.Status))
	for _, item := range m.state.Status {
		label := item.Label
		if strings.TrimSpace(label) == "" {
			label = "Item"
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
		return m.styles.muted.Render("No key bindings exposed yet.")
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderTranscriptContent(width int) string {
	if len(m.state.Transcript) == 0 {
		return m.styles.muted.Render("Transcript is empty.")
	}
	bodyStyle := m.styles.body.Width(maxInt(1, width))
	blocks := make([]string, 0, len(m.state.Transcript))
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
			m.roleStyle(role).Render(strings.ToUpper(role)),
			bodyStyle.Render(body),
		))
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

	if strings.HasPrefix(draft, "/") {
		return m.dispatchSlash(draft)
	}

	m.state.Busy = true
	m.appendTranscript("user", draft)
	m.appendTranscript("system", "Running turn...")
	m.state.Footer = "Running turn..."
	m.updateStatus()
	return m.runPromptCmd(draft)
}

func (m *Model) runPromptCmd(prompt string) tea.Cmd {
	history := cloneRuntimeMessages(m.history)
	options := m.options
	options.Prompt = prompt
	options.History = history
	existingSessionPath := m.state.SessionPath
	existingHistoryLen := len(m.history)
	layout := m.layout

	return func() tea.Msg {
		result, err := taskrun.Run(context.Background(), options)
		if err != nil {
			return taskFinishedMsg{Prompt: prompt, Err: err}
		}
		sessionPath, warn := persistTurn(layout, existingSessionPath, existingHistoryLen, result)
		return taskFinishedMsg{Prompt: prompt, Result: result, SessionPath: sessionPath, Warn: warn}
	}
}

func (m *Model) dispatchSlash(line string) tea.Cmd {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "/")))
	if len(fields) == 0 {
		m.appendTranscript("system", "Empty slash command.")
		return nil
	}

	switch fields[0] {
	case "help":
		m.appendTranscript("system", m.helpText())
		m.state.Footer = "Help opened"
		return nil
	case "exit", "quit":
		return tea.Quit
	case "palette":
		return m.togglePalette()
	case "new":
		m.resetConversation()
		return nil
	case "resume":
		return m.loadLatestSessionCmd(false)
	case "fork":
		return m.loadLatestSessionCmd(true)
	case "sessions":
		return m.openSessionsPalette(false)
	case "status":
		m.appendTranscript("system", m.statusSummary())
		return nil
	case "model":
		m.appendTranscript("system", m.modelSummary())
		return nil
	case "profiles":
		m.appendTranscript("system", m.profilesSummary())
		return nil
	case "providers":
		m.appendTranscript("system", m.providersSummary())
		return nil
	case "settings":
		m.appendTranscript("system", m.settingsSummary())
		return nil
	default:
		m.appendTranscript("system", fmt.Sprintf("Unknown slash command: /%s", fields[0]))
		return nil
	}
}

func (m *Model) applyTaskResult(msg taskFinishedMsg) {
	history := cloneRuntimeMessages(msg.Result.RequestMessages)
	if hasPersistableMessage(msg.Result.AssistantMessage) {
		history = append(history, msg.Result.AssistantMessage)
	}
	m.history = history
	m.state.Transcript = transcriptFromMessages(history)
	m.state.SessionPath = msg.SessionPath
	m.state.Model = fallback(msg.Result.Model, m.state.Model)
	m.state.Provider = fallback(msg.Result.ProviderName, m.state.Provider)
	m.state.Profile = fallback(msg.Result.Profile, m.state.Profile)
	m.state.Reasoning = fallback(msg.Result.Reasoning, m.state.Reasoning)
	m.state.Footer = "Turn complete"
	if msg.Warn != nil {
		m.state.Footer = fmt.Sprintf("Turn complete with warning: %v", msg.Warn)
	}
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) applyLoadedSession(msg sessionLoadedMsg) {
	m.history = cloneRuntimeMessages(msg.Messages)
	m.state.Transcript = transcriptFromMessages(msg.Messages)
	m.state.Model = msg.Meta.Model
	m.state.Provider = msg.Meta.Provider
	m.state.Profile = msg.Meta.Profile
	m.state.Reasoning = msg.Meta.Reasoning
	if msg.Fork {
		m.state.SessionPath = ""
		m.state.Footer = fmt.Sprintf("Forked from %s", msg.Entry.RelPath)
	} else {
		m.state.SessionPath = msg.Entry.Path
		m.state.Footer = fmt.Sprintf("Loaded %s", msg.Entry.RelPath)
	}
	m.updateStatus()
	m.refreshViewport()
}

func (m *Model) togglePalette() tea.Cmd {
	if m.state.Palette.Visible {
		return m.closePalette()
	}
	m.state.Palette.Visible = true
	m.state.Palette.Mode = PaletteModeRoot
	m.state.Palette.Items = defaultPaletteItems()
	m.state.Palette.Query = ""
	m.state.Palette.Selected = 0
	m.paletteInput.Reset()
	m.resize()
	return m.setFocus(FocusPalette)
}

func (m *Model) closePalette() tea.Cmd {
	m.state.Palette.Visible = false
	m.state.Palette.Mode = PaletteModeRoot
	m.state.Palette.Query = ""
	m.state.Palette.Selected = 0
	m.state.Palette.Items = defaultPaletteItems()
	m.paletteInput.Reset()
	m.resize()
	return m.setFocus(FocusInput)
}

func (m *Model) updatePalette(msg tea.Msg) tea.Cmd {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, m.keys.Close):
			return m.closePalette()
		case key.Matches(keyMsg, m.keys.Up):
			m.movePaletteSelection(-1)
			return nil
		case key.Matches(keyMsg, m.keys.Down):
			m.movePaletteSelection(1)
			return nil
		case key.Matches(keyMsg, m.keys.Submit):
			return m.activatePaletteSelection()
		}
	}

	var cmd tea.Cmd
	m.paletteInput, cmd = m.paletteInput.Update(msg)
	m.state.Palette.Query = m.paletteInput.Value()
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
	default:
		switch item.Key {
		case "new":
			m.resetConversation()
			return m.closePalette()
		case "resume_latest":
			return m.loadLatestSessionCmd(false)
		case "fork_latest":
			return m.loadLatestSessionCmd(true)
		case "sessions_resume":
			return m.openSessionsPalette(false)
		case "sessions_fork":
			return m.openSessionsPalette(true)
		case "model":
			m.appendTranscript("system", m.modelSummary())
		case "profiles":
			m.appendTranscript("system", m.profilesSummary())
		case "providers":
			m.appendTranscript("system", m.providersSummary())
		case "settings":
			m.appendTranscript("system", m.settingsSummary())
		case "status":
			m.appendTranscript("system", m.statusSummary())
		case "help":
			m.appendTranscript("system", m.helpText())
		}
		return m.closePalette()
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
		items = defaultPaletteItems()
	}
	query := strings.ToLower(strings.TrimSpace(m.state.Palette.Query))
	if query == "" {
		return items
	}
	filtered := make([]PaletteItem, 0, len(items))
	for _, item := range items {
		candidate := strings.ToLower(item.Title + " " + item.Description + " " + item.Value)
		if strings.Contains(candidate, query) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (m *Model) movePaletteSelection(delta int) {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		m.state.Palette.Selected = 0
		return
	}
	m.state.Palette.Selected = clampInt(m.state.Palette.Selected+delta, 0, len(items)-1)
}

func (m *Model) syncPaletteSelection() {
	items := m.filteredPaletteItems()
	if len(items) == 0 {
		m.state.Palette.Selected = 0
		return
	}
	m.state.Palette.Selected = clampInt(m.state.Palette.Selected, 0, len(items)-1)
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
		return "status pane"
	case FocusTranscript:
		return "transcript viewport"
	case FocusPalette:
		return "command palette"
	default:
		return "input area"
	}
}

func (m *Model) paletteTitle() string {
	switch m.state.Palette.Mode {
	case PaletteModeResume:
		return "Resume Session"
	case PaletteModeFork:
		return "Fork Session"
	default:
		return "Command Palette"
	}
}

func (m *Model) loadLatestSessionCmd(fork bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 1)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return sessionLoadedMsg{Err: fmt.Errorf("no saved sessions found")}
			}
			return sessionLoadedMsg{Err: err}
		}
		if len(entries) == 0 {
			return sessionLoadedMsg{Err: fmt.Errorf("no saved sessions found")}
		}
		entry := entries[0]
		meta, messages, err := appstate.LoadSession(entry.Path)
		return sessionLoadedMsg{Entry: entry, Meta: meta, Messages: messages, Fork: fork, Err: err}
	}
}

func (m *Model) loadSessionCmd(path string, fork bool) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(path) == "" {
			return sessionLoadedMsg{Err: fmt.Errorf("session path is empty")}
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

func (m *Model) openSessionsPalette(fork bool) tea.Cmd {
	return func() tea.Msg {
		entries, err := appstate.LoadSessions(m.layout.SessionsDir(), 20)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return paletteItemsMsg{Err: fmt.Errorf("no saved sessions found")}
			}
			return paletteItemsMsg{Err: err}
		}
		if len(entries) == 0 {
			return paletteItemsMsg{Err: fmt.Errorf("no saved sessions found")}
		}
		items := make([]PaletteItem, 0, len(entries))
		for _, entry := range entries {
			items = append(items, PaletteItem{
				Key:         "session",
				Title:       entry.Name,
				Description: entry.RelPath,
				Value:       entry.Path,
			})
		}
		mode := PaletteModeResume
		footer := "Select a saved session to resume"
		if fork {
			mode = PaletteModeFork
			footer = "Select a saved session to fork"
		}
		return paletteItemsMsg{Mode: mode, Items: items, Footer: footer}
	}
}

func (m *Model) resetConversation() {
	m.history = nil
	m.state.Transcript = DefaultState().Transcript
	m.state.SessionPath = ""
	m.state.Footer = "Started a new session"
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
	return strings.Join([]string{
		"Slash commands:",
		"/help      show command help",
		"/palette   open the command palette",
		"/new       start a fresh session",
		"/resume    load the latest session",
		"/fork      fork the latest session",
		"/sessions  browse saved sessions",
		"/status    show runtime summary",
		"/model     show active model summary",
		"/profiles  show configured profiles",
		"/providers show configured providers",
		"/settings  show saved UI settings",
		"/exit      quit the TUI",
	}, "\n")
}

func (m *Model) statusSummary() string {
	lines := []string{"Runtime status:"}
	for _, item := range m.state.Status {
		lines = append(lines, fmt.Sprintf("- %s: %s", item.Label, item.Value))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) modelSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	lines := []string{"Model summary:", fmt.Sprintf("- model: %s", fallback(m.state.Model, config.EffectiveModel())), fmt.Sprintf("- provider: %s", fallback(m.state.Provider, config.EffectiveProviderName())), fmt.Sprintf("- profile: %s", fallback(m.state.Profile, config.ActiveProfileName())), fmt.Sprintf("- reasoning: %s", fallback(m.state.Reasoning, config.EffectiveReasoningEffort()))}
	return strings.Join(lines, "\n")
}

func (m *Model) profilesSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	if len(config.Profiles) == 0 {
		return "Profiles: none"
	}
	lines := []string{"Profiles:"}
	for _, profile := range config.Profiles {
		suffix := ""
		if profile.Name == config.ActiveProfileName() {
			suffix = " (active)"
		}
		lines = append(lines, fmt.Sprintf("- %s%s model=%s provider=%s reasoning=%s", profile.Name, suffix, fallback(profile.Model, "<unset>"), fallback(profile.Provider, "<unset>"), fallback(profile.ReasoningEffort, "<unset>")))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) providersSummary() string {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	if len(config.ModelProviders) == 0 {
		return "Providers: none"
	}
	lines := []string{"Providers:"}
	for _, provider := range config.ModelProviders {
		lines = append(lines, fmt.Sprintf("- %s base_url=%s wire_api=%s", provider.Name, fallback(provider.BaseURL, "<unset>"), fallback(provider.WireAPI, "chat_completions")))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) settingsSummary() string {
	settings, _ := loadSettingsOptional(m.layout.SettingsPath())
	summary := settings.Summary()
	lines := []string{"Settings:", fmt.Sprintf("- language: %s", fallback(summary.Language, "<unset>")), fmt.Sprintf("- command_prefix: %s", fallback(summary.CommandPrefix, "<unset>")), fmt.Sprintf("- hidden_commands: %d", len(summary.HiddenCommands))}
	return strings.Join(lines, "\n")
}

func buildStatusItems(layout apphome.Layout, state State, historyLen int) []StatusItem {
	sessionValue := "fresh"
	if strings.TrimSpace(state.SessionPath) != "" {
		sessionValue = filepath.Base(state.SessionPath)
	}
	return []StatusItem{
		{Label: "Model", Value: fallback(state.Model, "<unset>")},
		{Label: "Provider", Value: fallback(state.Provider, "<unset>")},
		{Label: "Profile", Value: fallback(state.Profile, "<unset>")},
		{Label: "Reasoning", Value: fallback(state.Reasoning, "<unset>")},
		{Label: "Session", Value: sessionValue},
		{Label: "History", Value: fmt.Sprintf("%d", historyLen)},
		{Label: "Home", Value: layout.CodexHome()},
	}
}

func buildFooter(settings appstate.Settings) string {
	prefix := settings.CommandPrefix
	if strings.TrimSpace(prefix) == "" {
		prefix = "/"
	}
	return fmt.Sprintf("Enter submit · Ctrl+P palette · %shelp slash help", prefix)
}

func transcriptFromMessages(messages []runtimeapi.Message) []TranscriptEntry {
	if len(messages) == 0 {
		return nil
	}
	entries := make([]TranscriptEntry, 0, len(messages)*2)
	for _, message := range messages {
		if text := strings.TrimSpace(message.Text()); text != "" {
			entries = append(entries, TranscriptEntry{Role: string(message.Role), Body: text})
		}
		for _, call := range message.ToolCalls {
			body := fmt.Sprintf("tool call %s %s", fallback(call.ID, "<id>"), call.Function.Name)
			arguments := strings.TrimSpace(call.Function.ArgumentsString())
			if arguments != "" {
				body += "\n" + arguments
			}
			entries = append(entries, TranscriptEntry{Role: "tool", Body: body})
		}
	}
	return entries
}

func persistTurn(layout apphome.Layout, sessionPath string, existingHistoryLen int, result taskrun.Result) (string, error) {
	if strings.TrimSpace(sessionPath) == "" {
		messages := cloneRuntimeMessages(result.RequestMessages)
		if hasPersistableMessage(result.AssistantMessage) {
			messages = append(messages, result.AssistantMessage)
		}
		entry, err := appstate.CreateSession(layout.SessionsDir(), appstate.SessionMeta{
			Model:     result.Model,
			Provider:  result.ProviderName,
			Profile:   result.Profile,
			Reasoning: result.Reasoning,
		}, messages)
		if err != nil {
			return "", err
		}
		return entry.Path, nil
	}

	delta := cloneRuntimeMessages(result.RequestMessages)
	if existingHistoryLen < len(delta) {
		delta = delta[existingHistoryLen:]
	} else {
		delta = nil
	}
	if hasPersistableMessage(result.AssistantMessage) {
		delta = append(delta, result.AssistantMessage)
	}
	if err := appstate.AppendSession(sessionPath, delta...); err != nil {
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

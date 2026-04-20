package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Mode        PaletteMode
	Items       []PaletteItem
	Footer      string
	PushCurrent bool
	Err         error
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
	catalog      PaletteCommandCatalog
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
		catalog:      defaultPaletteCatalog(),
	}
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
	if options.PaletteCatalog != nil {
		model.catalog = options.PaletteCatalog
	}
	model.state.Palette.Items = model.rootPaletteItems()
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
		return m, m.applyPaletteScreen(msg.Mode, msg.Items, msg.Footer, msg.PushCurrent)
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
	layout := m.layout

	return func() tea.Msg {
		result, err := taskrun.Run(context.Background(), options)
		if err != nil {
			return taskFinishedMsg{Prompt: prompt, Err: err}
		}
		sessionPath, warn := persistTurn(layout, existingSessionPath, result)
		return taskFinishedMsg{Prompt: prompt, Result: result, SessionPath: sessionPath, Warn: warn}
	}
}

func (m *Model) dispatchSlash(line string) tea.Cmd {
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "/")))
	if len(fields) == 0 {
		m.appendTranscript("system", "Empty slash command.")
		return nil
	}
	command, ok := m.catalog.LookupBySlash(fields[0])
	if !ok {
		m.appendTranscript("system", fmt.Sprintf("Unknown slash command: /%s", fields[0]))
		return nil
	}
	return m.executePaletteCommand(command)
}

func (m *Model) applyTaskResult(msg taskFinishedMsg) {
	history := cloneRuntimeMessages(msg.Result.FullHistory())
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
		{Key: "model", Title: "Model", Description: "Inspect active model", Aliases: []string{"/model"}, Keywords: []string{"reasoning", "provider", "profile"}},
		{Key: "profiles", Title: "Profiles", Description: "Inspect configured profiles", Aliases: []string{"/profiles"}, Keywords: []string{"accounts", "profile", "config"}},
		{Key: "providers", Title: "Providers", Description: "Inspect configured providers", Aliases: []string{"/providers"}, Keywords: []string{"api", "wire_api", "base_url"}},
		{Key: "settings.language", Title: "Language", Description: fallback(summary.Language, "<unset>"), Keywords: []string{"locale", "translation"}},
		{Key: "settings.command_prefix", Title: "Command Prefix", Description: fallback(summary.CommandPrefix, "<unset>"), Keywords: []string{"slash", "prefix", "commands"}},
		{Key: "settings.hidden_commands", Title: "Hidden Commands", Description: fmt.Sprintf("%d", len(summary.HiddenCommands)), Keywords: []string{"visibility", "commands"}},
	}
}

func (m *Model) modelPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	return []PaletteItem{
		{Key: "model.value", Title: "Model", Description: fallback(m.state.Model, config.EffectiveModel()), Keywords: []string{"active", "slug", "model"}},
		{Key: "model.provider", Title: "Provider", Description: fallback(m.state.Provider, config.EffectiveProviderName()), Keywords: []string{"api", "provider"}},
		{Key: "model.profile", Title: "Profile", Description: fallback(m.state.Profile, config.ActiveProfileName()), Keywords: []string{"account", "profile"}},
		{Key: "model.reasoning", Title: "Reasoning", Description: fallback(m.state.Reasoning, config.EffectiveReasoningEffort()), Keywords: []string{"effort", "thinking", "reasoning"}},
	}
}

func (m *Model) profilesPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	items := []PaletteItem{}
	if len(config.Profiles) == 0 {
		return append(items, PaletteItem{Key: "profiles.empty", Title: "No Profiles", Description: "No configured profiles found"})
	}
	for _, profile := range config.Profiles {
		description := fmt.Sprintf("model=%s provider=%s reasoning=%s", fallback(profile.Model, "<unset>"), fallback(profile.Provider, "<unset>"), fallback(profile.ReasoningEffort, "<unset>"))
		if profile.Name == config.ActiveProfileName() {
			description += " · active"
		}
		items = append(items, PaletteItem{Key: "profiles.entry", Title: profile.Name, Description: description, Keywords: []string{profile.Provider, profile.Model, profile.ReasoningEffort}})
	}
	return items
}

func (m *Model) providersPaletteItems() []PaletteItem {
	config, _ := loadConfigOptional(m.layout.ConfigPath())
	items := []PaletteItem{}
	if len(config.ModelProviders) == 0 {
		return append(items, PaletteItem{Key: "providers.empty", Title: "No Providers", Description: "No configured providers found"})
	}
	for _, provider := range config.ModelProviders {
		items = append(items, PaletteItem{
			Key:         "providers.entry",
			Title:       provider.Name,
			Description: fmt.Sprintf("base_url=%s wire_api=%s", fallback(provider.BaseURL, "<unset>"), fallback(provider.WireAPI, "chat_completions")),
			Keywords:    []string{provider.BaseURL, provider.WireAPI},
		})
	}
	return items
}

func (m *Model) paletteBackItem() PaletteItem {
	context := normalizePaletteContext(m.state.Palette.Context)
	return PaletteItem{Key: "__back", Title: context.BackTitle, Description: context.BackDescription, Keywords: []string{"back", "close", "return"}}
}

func (m *Model) paletteHint() string {
	return normalizePaletteContext(m.state.Palette.Context).BackHint
}

func (m *Model) rootPaletteItems() []PaletteItem {
	if m.catalog == nil {
		m.catalog = defaultPaletteCatalog()
	}
	return clonePaletteItems(m.catalog.RootItems())
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
	context := defaultPaletteContext()
	if m.state.Palette.Visible {
		context.ReturnFocus = normalizePaletteContext(m.state.Palette.Context).ReturnFocus
	} else {
		context.ReturnFocus = normalizeFocus(m.state.Focus)
		if context.ReturnFocus == FocusPalette {
			context.ReturnFocus = FocusInput
		}
	}
	if !m.state.Palette.Visible || !pushCurrent {
		return context
	}
	context.BackTitle, context.BackDescription = paletteBackCopyForMode(m.state.Palette.Mode)
	context.BackHint = "Enter select · Esc back"
	return context
}

func paletteBackCopyForMode(mode PaletteMode) (string, string) {
	switch mode {
	case PaletteModeSettings:
		return "Back to Settings", "Return to settings"
	case PaletteModeRoot:
		return "Back to Palette", "Return to command palette"
	case PaletteModeModel:
		return "Back to Model", "Return to model details"
	case PaletteModeProfiles:
		return "Back to Profiles", "Return to profiles"
	case PaletteModeProviders:
		return "Back to Providers", "Return to providers"
	case PaletteModeResume:
		return "Back to Resume", "Return to saved sessions"
	case PaletteModeFork:
		return "Back to Fork", "Return to fork browser"
	default:
		return "Back to Chat", "Return to transcript"
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
		m.state.Footer = "Help opened"
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
	returnFocus := normalizePaletteContext(m.state.Palette.Context).ReturnFocus
	if returnFocus == FocusPalette {
		returnFocus = FocusInput
	}
	m.state.Palette.Visible = false
	m.state.Palette.Mode = PaletteModeRoot
	m.state.Palette.Query = ""
	m.state.Palette.Selected = 0
	m.state.Palette.Items = m.rootPaletteItems()
	m.state.Palette.Context = defaultPaletteContext()
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
	case PaletteModeModel:
		return "Model"
	case PaletteModeProfiles:
		return "Profiles"
	case PaletteModeProviders:
		return "Providers"
	case PaletteModeSettings:
		return "Settings"
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

func (m *Model) openSessionsPalette(fork bool, pushCurrent bool) tea.Cmd {
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
		return paletteItemsMsg{Mode: mode, Items: items, Footer: footer, PushCurrent: pushCurrent}
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
	return m.catalog.HelpText(commandPrefix(m.layout.SettingsPath()))
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
	return fmt.Sprintf("Enter submit · Ctrl+P palette · %shelp slash help", commandPrefixFromSettings(settings))
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

func persistTurn(layout apphome.Layout, sessionPath string, result taskrun.Result) (string, error) {
	history := cloneRuntimeMessages(result.FullHistory())
	if strings.TrimSpace(sessionPath) == "" {
		entry, err := appstate.CreateSession(layout.SessionsDir(), appstate.SessionMeta{
			Model:     result.Model,
			Provider:  result.ProviderName,
			Profile:   result.Profile,
			Reasoning: result.Reasoning,
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

package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Perdonus/lavilas-code/internal/accountprofiles"
	"github.com/Perdonus/lavilas-code/internal/apphome"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

const (
	formFieldProfileName = "profile_name"
	formFieldAPIKey      = "api_key"
	formFieldBaseURL     = "base_url"
	formFieldPresetName  = "model_preset_name"
	formFieldCommandPrefix = "command_prefix"
)

type formPromptKind string

const (
	formPromptAddAccount formPromptKind = "add_account"
	formPromptPresetName formPromptKind = "preset_name"
	formPromptCommandPrefix formPromptKind = "command_prefix"
)

type formPromptField struct {
	ID          string
	Header      string
	Prompt      string
	Secret      bool
	Suggested   string
	Required    bool
	Placeholder string
}

type formPromptState struct {
	Kind     formPromptKind
	Title    string
	Subtitle string
	Fields   []formPromptField
	Index    int
	Answers  map[string]string
	Meta     map[string]string
	Input    textinput.Model
}

func (m *Model) newPromptInput(field formPromptField) textinput.Model {
	input := textinput.New()
	input.Prompt = "> "
	input.PromptStyle = m.styles.sectionTitle
	input.TextStyle = m.styles.value
	input.PlaceholderStyle = m.styles.muted
	input.Cursor.Style = m.styles.paneTitle
	input.Width = maxInt(1, innerWidth(m.styles.pane, m.mainWidth)-2)
	input.Placeholder = strings.TrimSpace(field.Placeholder)
	input.SetValue(strings.TrimSpace(field.Suggested))
	if field.Secret {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}
	return input
}

func (m *Model) showFormPrompt(prompt *formPromptState) tea.Cmd {
	if prompt == nil || len(prompt.Fields) == 0 {
		return nil
	}
	prompt.Index = clampInt(prompt.Index, 0, len(prompt.Fields)-1)
	if prompt.Answers == nil {
		prompt.Answers = make(map[string]string, len(prompt.Fields))
	}
	prompt.Input = m.newPromptInput(prompt.Fields[prompt.Index])
	m.formPrompt = prompt
	m.state.Footer = strings.TrimSpace(prompt.Subtitle)
	return m.applyFocusState()
}

func (m *Model) renderFormPromptPane() string {
	if m.formPrompt == nil {
		return ""
	}
	prompt := m.formPrompt
	field := prompt.Fields[prompt.Index]
	pane := m.styles.paneActive.Width(m.mainWidth)
	lines := []string{m.styles.paneTitle.Render(strings.TrimSpace(prompt.Title))}
	if subtitle := strings.TrimSpace(prompt.Subtitle); subtitle != "" {
		lines = append(lines, m.styles.muted.Render(subtitle))
	}
	lines = append(lines,
		"",
		m.styles.sectionTitle.Render(field.Header),
		m.styles.body.Render(strings.TrimSpace(field.Prompt)),
		prompt.Input.View(),
	)
	if len(prompt.Answers) > 0 {
		lines = append(lines, "", m.styles.sectionTitle.Render(m.localize("Entered", "Заполнено")))
		for _, item := range prompt.Fields[:prompt.Index] {
			value := prompt.Answers[item.ID]
			if item.Secret && strings.TrimSpace(value) != "" {
				value = strings.Repeat("•", minInt(8, len([]rune(value))))
			}
			if strings.TrimSpace(value) == "" {
				value = localizedUnsetTUI(m.language)
			}
			lines = append(lines, m.styles.label.Render(item.Header)+" "+m.styles.value.Render(value))
		}
	}
	lines = append(lines, "", m.styles.muted.Render(m.localize("Enter continue · Esc cancel", "Enter продолжить · Esc отмена")))
	return pane.Render(strings.Join(lines, "\n"))
}

func (m *Model) updateFormPrompt(msg tea.Msg) tea.Cmd {
	if m.formPrompt == nil {
		return nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(keyMsg, m.keys.Close):
			m.formPrompt = nil
			return m.applyFocusState()
		case key.Matches(keyMsg, m.keys.Submit):
			return m.advanceFormPrompt()
		}
	}
	var cmd tea.Cmd
	m.formPrompt.Input, cmd = m.formPrompt.Input.Update(msg)
	return cmd
}

func (m *Model) advanceFormPrompt() tea.Cmd {
	if m.formPrompt == nil {
		return nil
	}
	prompt := m.formPrompt
	field := prompt.Fields[prompt.Index]
	value := strings.TrimSpace(prompt.Input.Value())
	if value == "" {
		value = strings.TrimSpace(field.Suggested)
	}
	if field.Required && value == "" {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Required field", "Обязательное поле"), field.Header)
		return nil
	}
	prompt.Answers[field.ID] = value
	if prompt.Index < len(prompt.Fields)-1 {
		prompt.Index++
		prompt.Input = m.newPromptInput(prompt.Fields[prompt.Index])
		m.state.Footer = strings.TrimSpace(prompt.Fields[prompt.Index].Header)
		return m.applyFocusState()
	}
	m.formPrompt = nil
	return m.submitFormPrompt(prompt)
}

func (m *Model) submitFormPrompt(prompt *formPromptState) tea.Cmd {
	switch prompt.Kind {
	case formPromptAddAccount:
		return m.submitAddAccountPrompt(prompt)
	case formPromptPresetName:
		return m.submitPresetNamePrompt(prompt)
	case formPromptCommandPrefix:
		return m.submitCommandPrefixPrompt(prompt)
	default:
		return m.applyFocusState()
	}
}

func (m *Model) openAddAccountDetailsPrompt(provider string, suggestedProfileName string) tea.Cmd {
	spec, ok := accountprofiles.Provider(provider)
	if !ok {
		m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Unsupported provider", "Неподдерживаемый провайдер"), provider)
		return nil
	}
	fields := []formPromptField{
		{
			ID:        formFieldProfileName,
			Header:    m.localize("Profile", "Профиль"),
			Prompt:    fmt.Sprintf(m.localize("Profile name. Leave empty to use `%s`.", "Название профиля. Оставьте пустым, чтобы использовать `%s`."), firstNonEmpty(suggestedProfileName, accountprofiles.SanitizeProfileKey("", spec.ID))),
			Suggested: strings.TrimSpace(suggestedProfileName),
		},
		{
			ID:       formFieldAPIKey,
			Header:   m.localize("API key", "API-ключ"),
			Prompt:   m.addAccountKeyPrompt(spec),
			Secret:   true,
			Required: !spec.APIKeyOptional && spec.BuiltinProviderID == "",
		},
	}
	if spec.RequiresBaseURL {
		fields = append(fields, formPromptField{
			ID:          formFieldBaseURL,
			Header:      m.localize("Base URL", "Базовый URL"),
			Prompt:      m.localize("Enter the OpenAI-compatible base URL for this provider.", "Введите базовый OpenAI-совместимый URL для этого провайдера."),
			Suggested:   spec.BaseURL,
			Required:    true,
			Placeholder: spec.BaseURL,
		})
	}
	return m.showFormPrompt(&formPromptState{
		Kind:     formPromptAddAccount,
		Title:    m.localize("Add Account", "Добавить аккаунт"),
		Subtitle: accountprofiles.ProviderDisplayName(spec.ID, m.language == "ru"),
		Fields:   fields,
		Meta: map[string]string{
			"provider": spec.ID,
		},
	})
}

func (m *Model) addAccountKeyPrompt(spec accountprofiles.ProviderSpec) string {
	switch {
	case spec.BuiltinProviderID != "" && m.language == "ru":
		return "API-ключ не нужен. Оставьте поле пустым, чтобы использовать стандартный вход Codex/OpenAI."
	case spec.BuiltinProviderID != "":
		return "No API key is needed. Leave this empty to use the standard Codex/OpenAI sign-in."
	case spec.APIKeyOptional && m.language == "ru":
		return "Введите API-ключ. Для локальных провайдеров вроде Ollama поле можно оставить пустым."
	case spec.APIKeyOptional:
		return "Enter the API key. For local providers like Ollama you can leave it empty."
	case m.language == "ru":
		return fmt.Sprintf("Введите API-ключ для %s.", accountprofiles.ProviderDisplayName(spec.ID, true))
	default:
		return fmt.Sprintf("Enter the API key for %s.", accountprofiles.ProviderDisplayName(spec.ID, false))
	}
}

func (m *Model) submitAddAccountPrompt(prompt *formPromptState) tea.Cmd {
	provider := strings.TrimSpace(prompt.Meta["provider"])
	profileName := strings.TrimSpace(prompt.Answers[formFieldProfileName])
	apiKey := strings.TrimSpace(prompt.Answers[formFieldAPIKey])
	baseURL := strings.TrimSpace(prompt.Answers[formFieldBaseURL])
	var apiKeyPtr *string
	if apiKey != "" {
		apiKeyPtr = &apiKey
	}
	var baseURLPtr *string
	if baseURL != "" {
		baseURLPtr = &baseURL
	}
	profileKey, stored, _, err := accountprofiles.CreateOrUpdateStoredProfile(apphome.CodexHome(), provider, profileName, baseURLPtr, apiKeyPtr)
	if err != nil {
		m.state.Footer = err.Error()
		return m.openAddAccountDetailsPrompt(provider, profileName)
	}
	configPath := m.layout.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load config", "Не удалось загрузить конфиг"), err)
		return nil
	}
	if err := accountprofiles.ApplyStoredProfile(&config, apphome.CodexHome(), profileKey, stored); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to activate profile", "Не удалось активировать профиль"), err)
		return nil
	}
	if err := appstate.SaveConfig(configPath, config); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save config", "Не удалось сохранить конфиг"), err)
		return nil
	}
	m.syncConfigState(config)
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Account added", "Аккаунт добавлен"), profileKey)
	return m.reopenProfilesPalette()
}

func (m *Model) openPresetNamePrompt(providerID string, presetKey string, model string, suggestedName string) tea.Cmd {
	meta := map[string]string{
		"provider_id": strings.TrimSpace(providerID),
		"preset_key":  strings.TrimSpace(presetKey),
		"model":       strings.TrimSpace(model),
	}
	return m.showFormPrompt(&formPromptState{
		Kind:  formPromptPresetName,
		Title: m.localize("Preset Name", "Название пресета"),
		Fields: []formPromptField{{
			ID:        formFieldPresetName,
			Header:    m.localize("Preset", "Пресет"),
			Prompt:    fmt.Sprintf(m.localize("Enter a preset name. Leave empty to use `%s`.", "Введите название пресета. Оставьте пустым, чтобы использовать `%s`."), suggestedName),
			Suggested: suggestedName,
			Required:  true,
		}},
		Meta: meta,
	})
}

func (m *Model) submitPresetNamePrompt(prompt *formPromptState) tea.Cmd {
	providerID := strings.TrimSpace(prompt.Meta["provider_id"])
	presetKey := strings.TrimSpace(prompt.Meta["preset_key"])
	model := strings.TrimSpace(prompt.Meta["model"])
	name := strings.TrimSpace(prompt.Answers[formFieldPresetName])
	if name == "" {
		name = model
	}
	settingsPath := m.layout.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to load settings", "Не удалось загрузить настройки"), err)
		return nil
	}
	settings.SetModelPreset(providerID, presetKey, appstate.ModelPresetConfig{Name: name, Model: model})
	if err := appstate.SaveSettings(settingsPath, settings); err != nil {
		m.state.Footer = fmt.Sprintf("%s: %v", m.localize("Failed to save settings", "Не удалось сохранить настройки"), err)
		return nil
	}
	m.state.Footer = fmt.Sprintf("%s: %s", m.localize("Preset saved", "Пресет сохранён"), name)
	return m.reopenCurrentProviderPresetEditor()
}

func (m *Model) openCommandPrefixPrompt() tea.Cmd {
	current := commandPrefixFromSettings(m.settingsForUI())
	return m.showFormPrompt(&formPromptState{
		Kind:  formPromptCommandPrefix,
		Title: m.localize("Command Prefix", "Префикс команд"),
		Fields: []formPromptField{{
			ID:          formFieldCommandPrefix,
			Header:      m.localize("Prefix", "Префикс"),
			Prompt:      m.localize("Enter a single ASCII character without spaces.", "Введите один ASCII-символ без пробелов."),
			Suggested:   current,
			Required:    true,
			Placeholder: current,
		}},
	})
}

func (m *Model) submitCommandPrefixPrompt(prompt *formPromptState) tea.Cmd {
	prefix := strings.TrimSpace(prompt.Answers[formFieldCommandPrefix])
	if prefix == "" {
		prefix = commandPrefixFromSettings(m.settingsForUI())
	}
	if len(prefix) != 1 || !isASCII(prefix) || strings.ContainsAny(prefix, " \t\r\n") {
		m.state.Footer = m.localize("Prefix must be one ASCII character without spaces", "Префикс должен быть одним ASCII-символом без пробелов")
		return m.openCommandPrefixPrompt()
	}
	return m.setSettingsCommandPrefix(prefix)
}

func isASCII(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

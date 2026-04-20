package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
)

type PaletteCommandAction string

const (
	PaletteActionOpenMode     PaletteCommandAction = "open_mode"
	PaletteActionOpenPalette  PaletteCommandAction = "open_palette"
	PaletteActionNewSession   PaletteCommandAction = "new_session"
	PaletteActionResumeLatest PaletteCommandAction = "resume_latest"
	PaletteActionForkLatest   PaletteCommandAction = "fork_latest"
	PaletteActionBrowseResume PaletteCommandAction = "browse_resume"
	PaletteActionBrowseFork   PaletteCommandAction = "browse_fork"
	PaletteActionShowStatus   PaletteCommandAction = "show_status"
	PaletteActionShowHelp     PaletteCommandAction = "show_help"
	PaletteActionQuit         PaletteCommandAction = "quit"
)

type PaletteCommandLocale struct {
	Slash       string
	Title       string
	Description string
	Aliases     []string
	Keywords    []string
}

type PaletteCommandSpec struct {
	Key               string
	CatalogCommand    string
	PresentationOrder int
	English           PaletteCommandLocale
	Russian           PaletteCommandLocale
	Action            PaletteCommandAction
	Mode              PaletteMode
	ShowInRoot        bool
	ShowInHelp        bool
}

type paletteCommandPresentation struct {
	Label       string
	Description string
	InsertSlash string
	Aliases     []string
	Keywords    []string
}

type PaletteCommandCatalog interface {
	RootItems(language commandcatalog.CatalogLanguage, query string) []PaletteItem
	LookupByKey(key string) (PaletteCommandSpec, bool)
	LookupBySlash(name string) (PaletteCommandSpec, bool)
	HelpText(prefix string, language commandcatalog.CatalogLanguage, query string) string
}

type staticPaletteCommandCatalog struct {
	commands []PaletteCommandSpec
}

func defaultPaletteCatalog() PaletteCommandCatalog {
	return staticPaletteCommandCatalog{commands: defaultPaletteCommandSpecs()}
}

func defaultPaletteCommandSpecs() []PaletteCommandSpec {
	return []PaletteCommandSpec{
		{
			Key:               "new",
			PresentationOrder: 40,
			English: PaletteCommandLocale{
				Slash:       "new",
				Title:       "New Session",
				Description: "Clear transcript and start fresh",
				Aliases:     []string{"reset", "fresh"},
				Keywords:    []string{"new", "session", "clear", "reset"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "новая",
				Title:       "Новая сессия",
				Description: "Очистить диалог и начать заново",
				Aliases:     []string{"новый", "сброс"},
				Keywords:    []string{"новая", "сессия", "очистить", "сброс"},
			},
			Action:     PaletteActionNewSession,
			ShowInRoot: true,
			ShowInHelp: true,
		},
		{
			Key:               "resume_latest",
			CatalogCommand:    "resume",
			PresentationOrder: 50,
			English: PaletteCommandLocale{
				Slash:       "resume",
				Title:       "Resume Latest",
				Description: "Load the latest saved session",
				Aliases:     []string{"continue"},
				Keywords:    []string{"latest", "session", "resume"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "продолжить",
				Title:       "Продолжить последнюю",
				Description: "Загрузить последнюю сохранённую сессию",
				Aliases:     []string{"последняя"},
				Keywords:    []string{"последняя", "сессия", "продолжить"},
			},
			Action:     PaletteActionResumeLatest,
			ShowInRoot: true,
			ShowInHelp: true,
		},
		{
			Key:               "fork_latest",
			CatalogCommand:    "fork",
			PresentationOrder: 60,
			English: PaletteCommandLocale{
				Slash:       "fork",
				Title:       "Fork Latest",
				Description: "Load the latest session as a new branch",
				Aliases:     []string{"branch"},
				Keywords:    []string{"fork", "latest", "session", "branch"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "форк",
				Title:       "Форк последней",
				Description: "Загрузить последнюю сессию как новую ветку",
				Aliases:     []string{"ветка"},
				Keywords:    []string{"форк", "последняя", "сессия", "ветка"},
			},
			Action:     PaletteActionForkLatest,
			ShowInRoot: true,
			ShowInHelp: true,
		},
		{
			Key:               "sessions_resume",
			PresentationOrder: 70,
			English: PaletteCommandLocale{
				Slash:       "sessions",
				Title:       "Sessions",
				Description: "Browse saved sessions to resume",
				Aliases:     []string{"history"},
				Keywords:    []string{"sessions", "history", "resume"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "сессии",
				Title:       "Сессии",
				Description: "Открыть сохранённые сессии для продолжения",
				Aliases:     []string{"история"},
				Keywords:    []string{"сессии", "история", "продолжить"},
			},
			Action:     PaletteActionBrowseResume,
			ShowInRoot: true,
			ShowInHelp: true,
		},
		{
			Key:               "sessions_fork",
			PresentationOrder: 80,
			English: PaletteCommandLocale{
				Title:       "Fork Session",
				Description: "Browse saved sessions to fork",
				Keywords:    []string{"history", "saved", "fork", "branch"},
			},
			Russian: PaletteCommandLocale{
				Title:       "Форк сессии",
				Description: "Открыть сохранённые сессии для форка",
				Keywords:    []string{"история", "сохранённые", "форк", "ветка"},
			},
			Action:     PaletteActionBrowseFork,
			ShowInRoot: true,
		},
		{
			Key:               "model",
			CatalogCommand:    "model",
			PresentationOrder: 10,
			Action:            PaletteActionOpenMode,
			Mode:              PaletteModeModel,
			ShowInRoot:        true,
			ShowInHelp:        true,
		},
		{
			Key:               "profiles",
			CatalogCommand:    "profiles",
			PresentationOrder: 20,
			Action:            PaletteActionOpenMode,
			Mode:              PaletteModeProfiles,
			ShowInRoot:        true,
			ShowInHelp:        true,
		},
		{
			Key:               "providers",
			CatalogCommand:    "providers",
			PresentationOrder: 90,
			Action:            PaletteActionOpenMode,
			Mode:              PaletteModeProviders,
			ShowInRoot:        true,
			ShowInHelp:        true,
		},
		{
			Key:               "settings",
			CatalogCommand:    "settings",
			PresentationOrder: 30,
			Action:            PaletteActionOpenMode,
			Mode:              PaletteModeSettings,
			ShowInRoot:        true,
			ShowInHelp:        true,
		},
		{
			Key:               "status",
			CatalogCommand:    "status",
			PresentationOrder: 100,
			Action:            PaletteActionShowStatus,
			ShowInRoot:        true,
			ShowInHelp:        true,
		},
		{
			Key:               "help",
			PresentationOrder: 110,
			English: PaletteCommandLocale{
				Slash:       "help",
				Title:       "Help",
				Description: "Show keyboard and slash commands",
				Aliases:     []string{"?"},
				Keywords:    []string{"help", "keys", "commands"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "помощь",
				Title:       "Помощь",
				Description: "Показать клавиши и слэш-команды",
				Aliases:     []string{"?"},
				Keywords:    []string{"помощь", "клавиши", "команды"},
			},
			Action:     PaletteActionShowHelp,
			ShowInRoot: true,
			ShowInHelp: true,
		},
		{
			Key:               "palette",
			PresentationOrder: 130,
			English: PaletteCommandLocale{
				Slash:       "palette",
				Title:       "Palette",
				Description: "Open the command palette",
				Keywords:    []string{"palette", "menu", "commands"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "палитра",
				Title:       "Палитра",
				Description: "Открыть палитру команд",
				Keywords:    []string{"палитра", "меню", "команды"},
			},
			Action:     PaletteActionOpenPalette,
			ShowInHelp: true,
		},
		{
			Key:               "exit",
			PresentationOrder: 120,
			English: PaletteCommandLocale{
				Slash:       "exit",
				Title:       "Exit",
				Description: "Quit the TUI",
				Aliases:     []string{"quit"},
				Keywords:    []string{"quit", "exit", "close"},
			},
			Russian: PaletteCommandLocale{
				Slash:       "выход",
				Title:       "Выход",
				Description: "Закрыть TUI",
				Aliases:     []string{"выйти"},
				Keywords:    []string{"выход", "выйти", "закрыть"},
			},
			Action:     PaletteActionQuit,
			ShowInHelp: true,
		},
	}
}

func (catalog staticPaletteCommandCatalog) RootItems(language commandcatalog.CatalogLanguage, query string) []PaletteItem {
	preferred := normalizePaletteLanguage(language)
	items := make([]PaletteItem, 0, len(catalog.commands))
	for _, command := range catalog.orderedCommands() {
		if !command.ShowInRoot {
			continue
		}
		items = append(items, catalog.commandToItem(command, preferred, query))
	}
	return items
}

func (catalog staticPaletteCommandCatalog) LookupByKey(key string) (PaletteCommandSpec, bool) {
	needle := strings.TrimSpace(strings.ToLower(key))
	if needle == "" {
		return PaletteCommandSpec{}, false
	}
	for _, command := range catalog.orderedCommands() {
		if strings.ToLower(command.Key) == needle {
			return command, true
		}
	}
	return PaletteCommandSpec{}, false
}

func (catalog staticPaletteCommandCatalog) LookupBySlash(name string) (PaletteCommandSpec, bool) {
	needle := normalizePaletteCommandName(name)
	if needle == "" {
		return PaletteCommandSpec{}, false
	}
	for _, command := range catalog.orderedCommands() {
		for _, alias := range catalog.commandSlashKeys(command) {
			if normalizePaletteCommandName(alias) == needle {
				return command, true
			}
		}
	}
	return PaletteCommandSpec{}, false
}

func (catalog staticPaletteCommandCatalog) HelpText(prefix string, language commandcatalog.CatalogLanguage, query string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/"
	}
	preferred := normalizePaletteLanguage(language)
	lines := []string{localizedPaletteText(preferred, "Slash commands:", "Слэш-команды:")}
	width := 0
	commands := make([]PaletteCommandSpec, 0, len(catalog.commands))
	for _, command := range catalog.orderedCommands() {
		if !command.ShowInHelp {
			continue
		}
		commands = append(commands, command)
		label := catalog.commandHelpLabel(command, prefix, preferred, query)
		if len(label) > width {
			width = len(label)
		}
	}
	for _, command := range commands {
		presentation := catalog.commandPresentation(command, preferred, query)
		label := prefix + presentation.Label
		description := presentation.Description
		lines = append(lines, fmt.Sprintf("%-*s %s", width, label, description))
	}
	return strings.Join(lines, "\n")
}

func (catalog staticPaletteCommandCatalog) commandToItem(command PaletteCommandSpec, preferred commandcatalog.CatalogLanguage, query string) PaletteItem {
	presentation := catalog.commandPresentation(command, preferred, query)
	return PaletteItem{
		Key:         command.Key,
		Title:       presentation.Label,
		Description: presentation.Description,
		Value:       presentation.InsertSlash,
		Aliases:     presentation.Aliases,
		Keywords:    presentation.Keywords,
	}
}

func (catalog staticPaletteCommandCatalog) commandHelpLabel(command PaletteCommandSpec, prefix string, preferred commandcatalog.CatalogLanguage, query string) string {
	return prefix + catalog.commandPresentation(command, preferred, query).Label
}

func (catalog staticPaletteCommandCatalog) commandLocale(command PaletteCommandSpec, language commandcatalog.CatalogLanguage) PaletteCommandLocale {
	language = normalizePaletteLanguage(language)
	if command.CatalogCommand != "" {
		if entry, ok := commandcatalog.Catalog().Find(command.CatalogCommand); ok {
			locale := entry.Locale(language)
			return PaletteCommandLocale{
				Slash:       locale.Name,
				Title:       locale.Name,
				Description: locale.Description,
				Aliases:     clonePaletteStrings(locale.Aliases),
				Keywords:    clonePaletteStrings(entry.Tags),
			}
		}
	}
	if language == commandcatalog.CatalogLanguageRussian {
		return clonePaletteLocale(command.Russian)
	}
	return clonePaletteLocale(command.English)
}

func (catalog staticPaletteCommandCatalog) commandSlashKeys(command PaletteCommandSpec) []string {
	keys := make([]string, 0, 12)
	for _, locale := range []PaletteCommandLocale{catalog.commandLocale(command, commandcatalog.CatalogLanguageEnglish), catalog.commandLocale(command, commandcatalog.CatalogLanguageRussian)} {
		if slash := strings.TrimSpace(locale.Slash); slash != "" {
			keys = append(keys, slash)
		}
		keys = append(keys, locale.Aliases...)
	}
	return uniquePaletteStrings(keys)
}

func (catalog staticPaletteCommandCatalog) orderedCommands() []PaletteCommandSpec {
	commands := append([]PaletteCommandSpec(nil), catalog.commands...)
	sort.SliceStable(commands, func(i, j int) bool {
		left := commands[i].PresentationOrder
		right := commands[j].PresentationOrder
		switch {
		case left == 0 && right == 0:
			return false
		case left == 0:
			return false
		case right == 0:
			return true
		default:
			return left < right
		}
	})
	return commands
}

func (catalog staticPaletteCommandCatalog) commandPresentation(command PaletteCommandSpec, preferred commandcatalog.CatalogLanguage, query string) paletteCommandPresentation {
	if command.CatalogCommand != "" {
		if item, ok := commandcatalog.Catalog().Present(command.CatalogCommand, preferred, query); ok {
			aliases := make([]string, 0, 16)
			aliases = append(aliases, item.Name, item.MirrorName, item.InsertName)
			aliases = append(aliases, item.Aliases...)
			aliases = append(aliases, item.MirrorAliases...)
			keywords := make([]string, 0, 16)
			keywords = append(keywords, item.Tags...)
			keywords = append(keywords, item.Name, item.MirrorName, item.DisplayName, item.Command)
			return paletteCommandPresentation{
				Label:       fallbackPaletteLabel(item.DisplayName, item.Name),
				Description: item.Description,
				InsertSlash: fallbackPaletteLabel(item.InsertName, item.Name),
				Aliases:     expandPaletteSlashAliases(aliases...),
				Keywords:    uniquePaletteStrings(keywords),
			}
		}
	}

	display := paletteDisplayLanguage(preferred, query)
	preferred = normalizePaletteLanguage(preferred)
	if preferred == commandcatalog.CatalogLanguageUnknown {
		preferred = display
	}
	locale := catalog.commandLocale(command, display)
	mirror := catalog.commandLocale(command, oppositePaletteLanguage(display))
	label := fallbackPaletteSlash(locale)
	mirrorLabel := fallbackPaletteSlash(mirror)
	if display != preferred && mirrorLabel != "" && mirrorLabel != label {
		label = fmt.Sprintf("%s (%s)", label, mirrorLabel)
	}
	keywords := make([]string, 0, 16)
	for _, locale := range []PaletteCommandLocale{catalog.commandLocale(command, commandcatalog.CatalogLanguageEnglish), catalog.commandLocale(command, commandcatalog.CatalogLanguageRussian)} {
		keywords = append(keywords, locale.Keywords...)
		keywords = append(keywords, locale.Title, locale.Slash)
	}
	aliases := make([]string, 0, 16)
	aliases = append(aliases, fallbackPaletteSlash(locale), fallbackPaletteSlash(mirror), locale.Title, mirror.Title)
	for _, locale := range []PaletteCommandLocale{catalog.commandLocale(command, commandcatalog.CatalogLanguageEnglish), catalog.commandLocale(command, commandcatalog.CatalogLanguageRussian)} {
		aliases = append(aliases, locale.Aliases...)
	}
	return paletteCommandPresentation{
		Label:       label,
		Description: locale.Description,
		InsertSlash: fallbackPaletteSlash(locale),
		Aliases:     expandPaletteSlashAliases(aliases...),
		Keywords:    uniquePaletteStrings(keywords),
	}
}

func normalizePaletteCommandName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "/")
	return value
}

func normalizePaletteLanguage(language commandcatalog.CatalogLanguage) commandcatalog.CatalogLanguage {
	switch strings.ToLower(strings.TrimSpace(string(language))) {
	case "ru":
		return commandcatalog.CatalogLanguageRussian
	case "en":
		return commandcatalog.CatalogLanguageEnglish
	default:
		return commandcatalog.CatalogLanguageEnglish
	}
}

func paletteDisplayLanguage(preferred commandcatalog.CatalogLanguage, query string) commandcatalog.CatalogLanguage {
	display := preferred
	if queryLanguage := commandcatalog.DetectCatalogLanguage(query); queryLanguage == commandcatalog.CatalogLanguageEnglish || queryLanguage == commandcatalog.CatalogLanguageRussian {
		display = queryLanguage
	}
	return normalizePaletteLanguage(display)
}

func oppositePaletteLanguage(language commandcatalog.CatalogLanguage) commandcatalog.CatalogLanguage {
	if normalizePaletteLanguage(language) == commandcatalog.CatalogLanguageRussian {
		return commandcatalog.CatalogLanguageEnglish
	}
	return commandcatalog.CatalogLanguageRussian
}

func localizedPaletteText(language commandcatalog.CatalogLanguage, english string, russian string) string {
	if normalizePaletteLanguage(language) == commandcatalog.CatalogLanguageRussian {
		return russian
	}
	return english
}

func fallbackPaletteSlash(locale PaletteCommandLocale) string {
	if value := strings.TrimSpace(locale.Slash); value != "" {
		return value
	}
	if value := strings.TrimSpace(locale.Title); value != "" {
		return value
	}
	return ""
}

func fallbackPaletteLabel(value string, fallbackValue string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return strings.TrimSpace(fallbackValue)
}

func clonePaletteLocale(locale PaletteCommandLocale) PaletteCommandLocale {
	locale.Aliases = clonePaletteStrings(locale.Aliases)
	locale.Keywords = clonePaletteStrings(locale.Keywords)
	return locale
}

func clonePaletteStrings(values []string) []string {
	return append([]string(nil), values...)
}

func uniquePaletteStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := strings.ToLower(trimmed)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func expandPaletteSlashAliases(values ...string) []string {
	expanded := make([]string, 0, len(values)*2)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		expanded = append(expanded, trimmed)
		if !strings.HasPrefix(trimmed, "/") && !strings.ContainsAny(trimmed, " \t") {
			expanded = append(expanded, "/"+trimmed)
		}
	}
	return uniquePaletteStrings(expanded)
}

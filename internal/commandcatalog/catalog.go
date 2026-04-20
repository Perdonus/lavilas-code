package commandcatalog

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type CatalogLanguage string

const (
	CatalogLanguageAuto    CatalogLanguage = "auto"
	CatalogLanguageEnglish CatalogLanguage = "en"
	CatalogLanguageRussian CatalogLanguage = "ru"
	CatalogLanguageUnknown CatalogLanguage = ""
)

type CatalogLocale struct {
	Name        string
	Description string
	Aliases     []string
}

type CatalogEntry struct {
	PresentationOrder  int
	Name               string
	Description        string
	Category           string
	Tags               []string
	EnglishName        string
	EnglishDescription string
	EnglishAliases     []string
	RussianName        string
	RussianDescription string
	RussianAliases     []string
}

type CatalogMatch struct {
	Command           string
	MatchedText       string
	MatchedLanguage   CatalogLanguage
	MatchedIsPrimary  bool
	PreferredLanguage CatalogLanguage
	QueryLanguage     CatalogLanguage
}

type CatalogListOptions struct {
	Language CatalogLanguage
	Query    string
	Category string
	Tag      string
}

type CatalogListItem struct {
	Command            string
	Name               string
	MirrorName         string
	DisplayName        string
	InsertName         string
	DisplayMirrorName  string
	DisplayShowsMirror bool
	Description        string
	MirrorDescription  string
	Category           string
	CategoryLabel      string
	Aliases            []string
	MirrorAliases      []string
	Tags               []string
	PresentationOrder  int
	PreferredLanguage  CatalogLanguage
	QueryLanguage      CatalogLanguage
	DisplayLanguage    CatalogLanguage
	Match              *CatalogMatch
}

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Category    string
}

type CommandCatalog struct {
	entries []CatalogEntry
	byName  map[string]int
	byKey   map[string]catalogKeyMatch
}

type catalogKeyMatch struct {
	Index     int
	Text      string
	Language  CatalogLanguage
	IsPrimary bool
}

type catalogListMatch struct {
	score int
	match CatalogMatch
}

func Catalog() *CommandCatalog {
	defaultCatalogOnce.Do(func() {
		defaultCatalog = NewCommandCatalog([]CatalogEntry{
			{
				PresentationOrder:  10,
				Name:               "model",
				Category:           "config",
				Tags:               []string{"model", "reasoning"},
				EnglishAliases:     []string{"models"},
				EnglishDescription: "Show or update the active model",
				RussianName:        "модель",
				RussianAliases:     []string{"модели"},
				RussianDescription: "Показать или сменить активную модель"},
			{
				PresentationOrder:  20,
				Name:               "profiles",
				Category:           "account",
				Tags:               []string{"profiles", "accounts"},
				EnglishAliases:     []string{"accounts", "prof"},
				EnglishDescription: "Manage saved profiles",
				RussianName:        "профили",
				RussianAliases:     []string{"аккаунты"},
				RussianDescription: "Управлять сохранёнными профилями"},
			{
				PresentationOrder:  30,
				Name:               "review",
				Category:           "interactive",
				Tags:               []string{"review", "code"},
				EnglishAliases:     []string{"rev"},
				EnglishDescription: "Run non-interactive review",
				RussianName:        "ревью",
				RussianDescription: "Запустить ревью без интерактивного режима"},
			{
				PresentationOrder:  40,
				Name:               "resume",
				Category:           "interactive",
				Tags:               []string{"sessions", "history"},
				EnglishAliases:     []string{"r", "continue"},
				EnglishDescription: "Resume or inspect saved sessions",
				RussianName:        "продолжить",
				RussianDescription: "Продолжить или открыть сохранённые сессии"},
			{
				PresentationOrder:  50,
				Name:               "fork",
				Category:           "interactive",
				Tags:               []string{"sessions", "branch"},
				EnglishAliases:     []string{"branch-chat"},
				EnglishDescription: "Fork a previous session",
				RussianName:        "форк",
				RussianDescription: "Ответвить предыдущую сессию"},
			{
				PresentationOrder:  60,
				Name:               "status",
				Category:           "account",
				Tags:               []string{"status", "runtime"},
				EnglishAliases:     []string{"info", "whoami"},
				EnglishDescription: "Show active runtime and account state",
				RussianName:        "статус",
				RussianDescription: "Показать активное состояние рантайма и аккаунта"},
			{
				PresentationOrder:  70,
				Name:               "logout",
				Category:           "account",
				Tags:               []string{"auth", "token"},
				EnglishAliases:     []string{"unauth"},
				EnglishDescription: "Remove saved token or profile",
				RussianName:        "выход",
				RussianAliases:     []string{"выйти-аккаунт"},
				RussianDescription: "Удалить сохранённый токен или профиль"},
			{
				PresentationOrder:  80,
				Name:               "settings",
				Category:           "config",
				Tags:               []string{"settings", "prefs", "ui"},
				EnglishAliases:     []string{"prefs", "config"},
				EnglishDescription: "Show saved UI settings",
				RussianName:        "настройки",
				RussianAliases:     []string{"параметры"},
				RussianDescription: "Показать сохранённые настройки интерфейса"},
			{
				PresentationOrder:  90,
				Name:               "providers",
				Category:           "account",
				Tags:               []string{"providers", "models"},
				EnglishAliases:     []string{"provider", "prov"},
				EnglishDescription: "Manage model providers",
				RussianName:        "провайдеры",
				RussianAliases:     []string{"провайдер"},
				RussianDescription: "Управлять провайдерами моделей"},
			{
				PresentationOrder:  100,
				Name:               "chat",
				Category:           "interactive",
				Tags:               []string{"interactive", "repl", "tui"},
				EnglishAliases:     []string{"interactive", "repl"},
				EnglishDescription: "Open interactive chat mode",
				RussianName:        "чат",
				RussianDescription: "Открыть интерактивный чат"},
			{
				PresentationOrder:  110,
				Name:               "run",
				Category:           "interactive",
				Tags:               []string{"task", "one-shot"},
				EnglishAliases:     []string{"exec", "ask"},
				EnglishDescription: "Execute a one-shot task",
				RussianName:        "запуск",
				RussianDescription: "Выполнить разовую задачу"},
			{
				PresentationOrder:  120,
				Name:               "apply",
				Category:           "interactive",
				Tags:               []string{"patch", "stdin"},
				EnglishAliases:     []string{"patch"},
				EnglishDescription: "Apply a patch from stdin or file",
				RussianName:        "применить",
				RussianDescription: "Применить патч из stdin или файла"},
			{
				PresentationOrder:  130,
				Name:               "login",
				Category:           "account",
				Tags:               []string{"auth", "token"},
				EnglishAliases:     []string{"auth"},
				EnglishDescription: "Save provider token and profile",
				RussianName:        "вход",
				RussianDescription: "Сохранить токен провайдера и профиль"},
			{
				PresentationOrder:  140,
				Name:               "completion",
				Category:           "config",
				Tags:               []string{"shell", "completion"},
				EnglishAliases:     []string{"completions"},
				EnglishDescription: "Generate shell completion scripts",
				RussianName:        "автодополнение",
				RussianDescription: "Сгенерировать скрипты автодополнения shell"},
			{
				PresentationOrder:  150,
				Name:               "features",
				Category:           "config",
				Tags:               []string{"features", "flags"},
				EnglishAliases:     []string{"flags"},
				EnglishDescription: "Show the alpha feature matrix",
				RussianName:        "фичи",
				RussianDescription: "Показать матрицу alpha-функций"},
			{
				PresentationOrder:  160,
				Name:               "doctor",
				Category:           "runtime",
				Tags:               []string{"diagnostics", "env"},
				EnglishAliases:     []string{"diag"},
				EnglishDescription: "Inspect the local environment",
				RussianName:        "диагностика",
				RussianDescription: "Проверить локальное окружение"},
		})
	})
	return defaultCatalog
}

func (entry CatalogEntry) English() CatalogLocale {
	name := strings.TrimSpace(entry.EnglishName)
	if name == "" {
		name = strings.TrimSpace(entry.Name)
	}
	description := strings.TrimSpace(entry.EnglishDescription)
	if description == "" {
		description = strings.TrimSpace(entry.Description)
	}
	return CatalogLocale{
		Name:        name,
		Description: description,
		Aliases:     cloneStrings(entry.EnglishAliases),
	}
}

func (entry CatalogEntry) Russian() CatalogLocale {
	name := strings.TrimSpace(entry.RussianName)
	if name == "" {
		name = strings.TrimSpace(entry.Name)
	}
	description := strings.TrimSpace(entry.RussianDescription)
	if description == "" {
		description = strings.TrimSpace(entry.Description)
	}
	return CatalogLocale{
		Name:        name,
		Description: description,
		Aliases:     cloneStrings(entry.RussianAliases),
	}
}

func (entry CatalogEntry) Locale(language CatalogLanguage) CatalogLocale {
	switch normalizeCatalogLanguage(language) {
	case CatalogLanguageRussian:
		return entry.Russian()
	default:
		return entry.English()
	}
}

func (entry CatalogEntry) MirrorLocale(language CatalogLanguage) CatalogLocale {
	switch normalizeCatalogLanguage(language) {
	case CatalogLanguageRussian:
		return entry.English()
	default:
		return entry.Russian()
	}
}

func (entry CatalogEntry) Aliases() []string {
	aliases := make([]string, 0, 1+len(entry.EnglishAliases)+len(entry.RussianAliases))
	aliases = append(aliases, entry.English().Aliases...)
	if russianName := strings.TrimSpace(entry.Russian().Name); russianName != "" && normalizeCatalogKey(russianName) != normalizeCatalogKey(entry.Name) {
		aliases = append(aliases, russianName)
	}
	aliases = append(aliases, entry.Russian().Aliases...)
	return uniqueCatalogStrings(aliases)
}

func (entry CatalogEntry) Keys() []string {
	keys := []string{entry.Name}
	english := entry.English()
	russian := entry.Russian()
	if english.Name != "" {
		keys = append(keys, english.Name)
	}
	if russian.Name != "" {
		keys = append(keys, russian.Name)
	}
	keys = append(keys, english.Aliases...)
	keys = append(keys, russian.Aliases...)
	return uniqueCatalogStrings(keys)
}

func (entry CatalogEntry) Command() Command {
	english := entry.English()
	return Command{
		Name:        fallback(english.Name, entry.Name),
		Aliases:     entry.Aliases(),
		Description: english.Description,
		Category:    entry.Category,
	}
}

func (catalog *CommandCatalog) Entries() []CatalogEntry {
	entries := make([]CatalogEntry, len(catalog.entries))
	for index, entry := range catalog.entries {
		entries[index] = cloneCatalogEntry(entry)
	}
	return entries
}

func (catalog *CommandCatalog) Commands() []Command {
	commands := make([]Command, len(catalog.entries))
	for index, entry := range catalog.entries {
		commands[index] = entry.Command()
	}
	return commands
}

func (catalog *CommandCatalog) Present(command string, preferred CatalogLanguage, query string) (CatalogListItem, bool) {
	entry, ok := catalog.Find(command)
	if !ok {
		return CatalogListItem{}, false
	}
	displayLanguage, queryLanguage := catalogDisplayContext(preferred, query)
	return catalogListItem(entry, displayLanguage, preferred, queryLanguage), true
}

func (catalog *CommandCatalog) Find(name string) (CatalogEntry, bool) {
	index, ok := catalog.byName[normalizeCatalogKey(name)]
	if !ok {
		return CatalogEntry{}, false
	}
	return cloneCatalogEntry(catalog.entries[index]), true
}

func (catalog *CommandCatalog) Resolve(nameOrAlias string) (CatalogEntry, bool) {
	match, ok := catalog.ResolveMatch(nameOrAlias, CatalogLanguageAuto)
	if !ok {
		return CatalogEntry{}, false
	}
	return match.Entry(), true
}

func (catalog *CommandCatalog) ResolveMatch(nameOrAlias string, preferred CatalogLanguage) (CatalogMatchResult, bool) {
	key := normalizeCatalogKey(nameOrAlias)
	match, ok := catalog.byKey[key]
	if !ok {
		return CatalogMatchResult{}, false
	}
	entry := cloneCatalogEntry(catalog.entries[match.Index])
	queryLanguage := DetectCatalogLanguage(nameOrAlias)
	if queryLanguage == CatalogLanguageUnknown {
		queryLanguage = normalizeCatalogLanguage(preferred)
	}
	return CatalogMatchResult{
		CatalogMatch: CatalogMatch{
			Command:           entry.Name,
			MatchedText:       match.Text,
			MatchedLanguage:   match.Language,
			MatchedIsPrimary:  match.IsPrimary,
			PreferredLanguage: normalizeCatalogLanguage(preferred),
			QueryLanguage:     queryLanguage,
		},
		entry: entry,
	}, true
}

type CatalogMatchResult struct {
	CatalogMatch
	entry CatalogEntry
}

func (result CatalogMatchResult) Entry() CatalogEntry {
	return cloneCatalogEntry(result.entry)
}

func (catalog *CommandCatalog) List(options CatalogListOptions) []CatalogListItem {
	preferred := normalizeCatalogLanguage(options.Language)
	query := strings.TrimSpace(options.Query)
	displayLanguage, queryLanguage := catalogDisplayContext(preferred, query)

	type scoredEntry struct {
		entry CatalogEntry
		item  CatalogListItem
		score int
		order int
	}

	items := make([]scoredEntry, 0, len(catalog.entries))
	for index, originalEntry := range catalog.entries {
		entry := cloneCatalogEntry(originalEntry)
		if !catalogEntryMatchesFilters(entry, options.Category, options.Tag) {
			continue
		}

		match, ok := catalog.matchEntry(entry, query, preferred, displayLanguage, queryLanguage)
		if query != "" && !ok {
			continue
		}

		item := catalogListItem(entry, displayLanguage, preferred, queryLanguage)
		if ok {
			copy := match.match
			item.Match = &copy
		}
		items = append(items, scoredEntry{
			entry: entry,
			item:  item,
			score: match.score,
			order: index,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if items[i].item.PresentationOrder != items[j].item.PresentationOrder {
			return items[i].item.PresentationOrder < items[j].item.PresentationOrder
		}
		return items[i].order < items[j].order
	})

	result := make([]CatalogListItem, len(items))
	for index, item := range items {
		result[index] = item.item
	}
	return result
}

func CatalogCategoryLabel(category string, language CatalogLanguage) string {
	switch normalizeCatalogLanguage(language) {
	case CatalogLanguageRussian:
		switch category {
		case "interactive":
			return "Интерактив"
		case "account":
			return "Аккаунт"
		case "config":
			return "Настройки"
		case "automation":
			return "Автоматизация"
		case "runtime":
			return "Рантайм"
		case "debug":
			return "Отладка"
		default:
			return category
		}
	default:
		switch category {
		case "interactive":
			return "Interactive"
		case "account":
			return "Account"
		case "config":
			return "Config"
		case "automation":
			return "Automation"
		case "runtime":
			return "Runtime"
		case "debug":
			return "Debug"
		default:
			return category
		}
	}
}

func DetectCatalogLanguage(value string) CatalogLanguage {
	hasLatin := false
	hasCyrillic := false
	for _, r := range value {
		switch {
		case unicode.In(r, unicode.Cyrillic):
			hasCyrillic = true
		case unicode.In(r, unicode.Latin):
			hasLatin = true
		}
		if hasLatin && hasCyrillic {
			return CatalogLanguageUnknown
		}
	}
	switch {
	case hasCyrillic:
		return CatalogLanguageRussian
	case hasLatin:
		return CatalogLanguageEnglish
	default:
		return CatalogLanguageUnknown
	}
}

var (
	defaultCatalog     *CommandCatalog
	defaultCatalogOnce sync.Once
)

func NewCommandCatalog(entries []CatalogEntry) *CommandCatalog {
	orderedEntries := make([]CatalogEntry, len(entries))
	for index, entry := range entries {
		orderedEntries[index] = cloneCatalogEntry(entry)
	}
	sort.SliceStable(orderedEntries, func(i, j int) bool {
		left := orderedEntries[i].PresentationOrder
		right := orderedEntries[j].PresentationOrder
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

	catalog := &CommandCatalog{
		entries: make([]CatalogEntry, len(orderedEntries)),
		byName:  make(map[string]int, len(orderedEntries)),
		byKey:   make(map[string]catalogKeyMatch, len(orderedEntries)*6),
	}
	for index, entry := range orderedEntries {
		nameKey := normalizeCatalogKey(entry.Name)
		if nameKey == "" {
			panic("command catalog: empty command name")
		}
		if _, exists := catalog.byName[nameKey]; exists {
			panic(fmt.Sprintf("command catalog: duplicate command name %q", entry.Name))
		}
		catalog.entries[index] = entry
		catalog.byName[nameKey] = index
		for _, match := range catalogEntryKeys(entry) {
			normalized := normalizeCatalogKey(match.Text)
			if normalized == "" {
				continue
			}
			if existing, exists := catalog.byKey[normalized]; exists {
				panic(fmt.Sprintf(
					"command catalog: duplicate lookup key %q for %q and %q",
					match.Text,
					catalog.entries[existing.Index].Name,
					entry.Name,
				))
			}
			match.Index = index
			catalog.byKey[normalized] = match
		}
	}
	return catalog
}

func catalogEntryKeys(entry CatalogEntry) []catalogKeyMatch {
	keys := []catalogKeyMatch{
		{Text: entry.Name, Language: CatalogLanguageEnglish, IsPrimary: true},
	}
	english := entry.English()
	russian := entry.Russian()
	if normalizeCatalogKey(english.Name) != normalizeCatalogKey(entry.Name) {
		keys = append(keys, catalogKeyMatch{
			Text:      english.Name,
			Language:  CatalogLanguageEnglish,
			IsPrimary: true,
		})
	}
	if russian.Name != "" {
		keys = append(keys, catalogKeyMatch{
			Text:      russian.Name,
			Language:  CatalogLanguageRussian,
			IsPrimary: true,
		})
	}
	for _, alias := range english.Aliases {
		keys = append(keys, catalogKeyMatch{
			Text:      alias,
			Language:  CatalogLanguageEnglish,
			IsPrimary: false,
		})
	}
	for _, alias := range russian.Aliases {
		keys = append(keys, catalogKeyMatch{
			Text:      alias,
			Language:  CatalogLanguageRussian,
			IsPrimary: false,
		})
	}
	return dedupeCatalogKeyMatches(keys)
}

func catalogEntryMatchesFilters(entry CatalogEntry, category string, tag string) bool {
	if normalized := normalizeCatalogKey(category); normalized != "" && normalizeCatalogKey(entry.Category) != normalized {
		return false
	}
	if normalized := normalizeCatalogKey(tag); normalized != "" {
		for _, entryTag := range entry.Tags {
			if normalizeCatalogKey(entryTag) == normalized {
				return true
			}
		}
		return false
	}
	return true
}

func catalogListItem(
	entry CatalogEntry,
	displayLanguage CatalogLanguage,
	preferred CatalogLanguage,
	queryLanguage CatalogLanguage,
) CatalogListItem {
	locale := entry.Locale(displayLanguage)
	mirror := entry.MirrorLocale(displayLanguage)
	displayName := locale.Name
	displayMirrorName := ""
	displayShowsMirror := false
	normalizedPreferred := normalizeCatalogLanguage(preferred)
	if normalizedPreferred == CatalogLanguageUnknown {
		normalizedPreferred = displayLanguage
	}
	if displayLanguage != normalizedPreferred && mirror.Name != "" && normalizeCatalogKey(mirror.Name) != normalizeCatalogKey(locale.Name) {
		displayMirrorName = mirror.Name
		displayShowsMirror = true
		displayName = fmt.Sprintf("%s (%s)", locale.Name, mirror.Name)
	}
	return CatalogListItem{
		Command:            entry.Name,
		Name:               locale.Name,
		MirrorName:         mirror.Name,
		DisplayName:        displayName,
		InsertName:         locale.Name,
		DisplayMirrorName:  displayMirrorName,
		DisplayShowsMirror: displayShowsMirror,
		Description:        locale.Description,
		MirrorDescription:  mirror.Description,
		Category:           entry.Category,
		CategoryLabel:      CatalogCategoryLabel(entry.Category, displayLanguage),
		Aliases:            cloneStrings(locale.Aliases),
		MirrorAliases:      cloneStrings(mirror.Aliases),
		Tags:               cloneStrings(entry.Tags),
		PresentationOrder:  entry.PresentationOrder,
		PreferredLanguage:  normalizeCatalogLanguage(preferred),
		QueryLanguage:      queryLanguage,
		DisplayLanguage:    displayLanguage,
	}
}

func catalogDisplayContext(preferred CatalogLanguage, query string) (CatalogLanguage, CatalogLanguage) {
	queryLanguage := DetectCatalogLanguage(query)
	displayLanguage := normalizeCatalogLanguage(preferred)
	if queryLanguage == CatalogLanguageEnglish || queryLanguage == CatalogLanguageRussian {
		displayLanguage = queryLanguage
	}
	if displayLanguage == CatalogLanguageUnknown {
		displayLanguage = CatalogLanguageEnglish
	}
	return displayLanguage, queryLanguage
}

func (catalog *CommandCatalog) matchEntry(
	entry CatalogEntry,
	query string,
	preferred CatalogLanguage,
	displayLanguage CatalogLanguage,
	queryLanguage CatalogLanguage,
) (catalogListMatch, bool) {
	if strings.TrimSpace(query) == "" {
		return catalogListMatch{}, true
	}
	normalizedQuery := normalizeCatalogKey(query)
	if normalizedQuery == "" {
		return catalogListMatch{}, true
	}

	keys := make([]catalogKeyMatch, 0, 8)
	displayKeys := filterCatalogKeyLanguage(catalogEntryKeys(entry), displayLanguage)
	mirrorKeys := filterCatalogKeyLanguage(catalogEntryKeys(entry), oppositeCatalogLanguage(displayLanguage))
	keys = append(keys, displayKeys...)
	keys = append(keys, mirrorKeys...)

	best := catalogListMatch{score: -1}
	for _, key := range keys {
		score, ok := scoreCatalogLookupKey(normalizedQuery, key, preferred, queryLanguage, displayLanguage)
		if !ok || score <= best.score {
			continue
		}
		best = catalogListMatch{
			score: score,
			match: CatalogMatch{
				Command:           entry.Name,
				MatchedText:       key.Text,
				MatchedLanguage:   key.Language,
				MatchedIsPrimary:  key.IsPrimary,
				PreferredLanguage: normalizeCatalogLanguage(preferred),
				QueryLanguage:     queryLanguage,
			},
		}
	}
	return best, best.score >= 0
}

func scoreCatalogLookupKey(
	query string,
	key catalogKeyMatch,
	preferred CatalogLanguage,
	queryLanguage CatalogLanguage,
	displayLanguage CatalogLanguage,
) (int, bool) {
	candidate := normalizeCatalogKey(key.Text)
	switch {
	case candidate == query:
		score := 400
		if key.IsPrimary {
			score += 40
		}
		if key.Language == displayLanguage {
			score += 20
		}
		if queryLanguage != CatalogLanguageUnknown && key.Language == queryLanguage {
			score += 20
		}
		if preferred != CatalogLanguageUnknown && key.Language == preferred {
			score += 10
		}
		return score, true
	case strings.HasPrefix(candidate, query):
		score := 260
		if key.IsPrimary {
			score += 25
		}
		if key.Language == displayLanguage {
			score += 20
		}
		if queryLanguage != CatalogLanguageUnknown && key.Language == queryLanguage {
			score += 20
		}
		if preferred != CatalogLanguageUnknown && key.Language == preferred {
			score += 10
		}
		return score, true
	case strings.Contains(candidate, query):
		score := 140
		if key.IsPrimary {
			score += 10
		}
		if key.Language == displayLanguage {
			score += 10
		}
		if queryLanguage != CatalogLanguageUnknown && key.Language == queryLanguage {
			score += 10
		}
		return score, true
	default:
		return 0, false
	}
}

func filterCatalogKeyLanguage(matches []catalogKeyMatch, language CatalogLanguage) []catalogKeyMatch {
	if language == CatalogLanguageUnknown {
		return matches
	}
	filtered := make([]catalogKeyMatch, 0, len(matches))
	for _, match := range matches {
		if match.Language == language {
			filtered = append(filtered, match)
		}
	}
	return filtered
}

func dedupeCatalogKeyMatches(matches []catalogKeyMatch) []catalogKeyMatch {
	seen := make(map[string]struct{}, len(matches))
	result := make([]catalogKeyMatch, 0, len(matches))
	for _, match := range matches {
		key := normalizeCatalogKey(match.Text)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, match)
	}
	return result
}

func cloneCatalogEntry(entry CatalogEntry) CatalogEntry {
	entry.Tags = cloneStrings(entry.Tags)
	entry.EnglishAliases = cloneStrings(entry.EnglishAliases)
	entry.RussianAliases = cloneStrings(entry.RussianAliases)
	return entry
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func uniqueCatalogStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		key := normalizeCatalogKey(trimmed)
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

func normalizeCatalogLanguage(language CatalogLanguage) CatalogLanguage {
	switch strings.ToLower(strings.TrimSpace(string(language))) {
	case "ru", "rus", "russian":
		return CatalogLanguageRussian
	case "en", "eng", "english":
		return CatalogLanguageEnglish
	case "", "auto":
		return CatalogLanguageUnknown
	default:
		return CatalogLanguageUnknown
	}
}

func oppositeCatalogLanguage(language CatalogLanguage) CatalogLanguage {
	switch normalizeCatalogLanguage(language) {
	case CatalogLanguageRussian:
		return CatalogLanguageEnglish
	default:
		return CatalogLanguageRussian
	}
}

func normalizeCatalogKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func catalogDisplayNames(items []CatalogListItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func catalogLookupAliases(item CatalogListItem) []string {
	aliases := append([]string{}, item.Aliases...)
	aliases = append(aliases, item.MirrorAliases...)
	return slices.Clip(uniqueCatalogStrings(aliases))
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallbackValue
}

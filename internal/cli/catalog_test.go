package cli

import (
	"reflect"
	"testing"
)

func TestRegistryUsesSharedCatalogAliases(t *testing.T) {
	lookup := buildLookup(registry())
	tests := map[string]string{
		"chat":        "chat",
		"чат":         "chat",
		"interactive": "chat",
		"resume":      "resume",
		"продолжить":  "resume",
		"profiles":    "profiles",
		"аккаунты":    "profiles",
	}

	for key, want := range tests {
		command, ok := lookup[key]
		if !ok {
			t.Fatalf("lookup[%q] missing", key)
		}
		if command.Name != want {
			t.Fatalf("lookup[%q].Name = %q, want %q", key, command.Name, want)
		}
	}
}

func TestCommandAndAliasNamesIncludeBilingualCatalogValues(t *testing.T) {
	got := commandAndAliasNames(registry())
	for _, want := range []string{"chat", "чат", "interactive", "resume", "продолжить", "profiles", "аккаунты"} {
		if !containsString(got, want) {
			t.Fatalf("commandAndAliasNames(registry()) missing %q in %#v", want, got)
		}
	}
}

func TestCatalogResolveCanonicalAndAliases(t *testing.T) {
	catalog := Catalog()
	tests := []struct {
		key            string
		preferred      CatalogLanguage
		wantCommand    string
		wantLanguage   CatalogLanguage
		wantPrimaryKey bool
	}{
		{key: "chat", preferred: CatalogLanguageEnglish, wantCommand: "chat", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: true},
		{key: "interactive", preferred: CatalogLanguageEnglish, wantCommand: "chat", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: false},
		{key: "чат", preferred: CatalogLanguageRussian, wantCommand: "chat", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: true},
		{key: "продолжить", preferred: CatalogLanguageRussian, wantCommand: "resume", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: true},
		{key: "accounts", preferred: CatalogLanguageEnglish, wantCommand: "profiles", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: false},
		{key: "АККАУНТЫ", preferred: CatalogLanguageRussian, wantCommand: "profiles", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: false},
		{key: "provider", preferred: CatalogLanguageEnglish, wantCommand: "providers", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: false},
		{key: "ПРОВАЙДЕРЫ", preferred: CatalogLanguageRussian, wantCommand: "providers", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: true},
	}

	for _, tt := range tests {
		match, ok := catalog.ResolveMatch(tt.key, tt.preferred)
		if !ok {
			t.Fatalf("ResolveMatch(%q) did not find a command", tt.key)
		}
		if match.Command != tt.wantCommand {
			t.Fatalf("ResolveMatch(%q).Command = %q, want %q", tt.key, match.Command, tt.wantCommand)
		}
		if match.MatchedLanguage != tt.wantLanguage {
			t.Fatalf("ResolveMatch(%q).MatchedLanguage = %q, want %q", tt.key, match.MatchedLanguage, tt.wantLanguage)
		}
		if match.MatchedIsPrimary != tt.wantPrimaryKey {
			t.Fatalf("ResolveMatch(%q).MatchedIsPrimary = %v, want %v", tt.key, match.MatchedIsPrimary, tt.wantPrimaryKey)
		}
	}
}

func TestCatalogAliasesPreserveLanguageBuckets(t *testing.T) {
	entry, ok := Catalog().Find("settings")
	if !ok {
		t.Fatal("Find(settings) = false, want true")
	}

	if !reflect.DeepEqual(entry.EnglishAliases, []string{"prefs", "config"}) {
		t.Fatalf("EnglishAliases = %#v", entry.EnglishAliases)
	}
	if !reflect.DeepEqual(entry.RussianAliases, []string(nil)) {
		t.Fatalf("RussianAliases = %#v", entry.RussianAliases)
	}
	if !reflect.DeepEqual(entry.Aliases(), []string{"prefs", "config", "настройки"}) {
		t.Fatalf("Aliases() = %#v", entry.Aliases())
	}
}

func TestCatalogReturnsClonedEntries(t *testing.T) {
	entry, ok := Catalog().Find("model")
	if !ok {
		t.Fatal("Find(model) = false, want true")
	}
	entry.Tags[0] = "broken"
	entry.EnglishAliases[0] = "broken"
	entry.RussianAliases[0] = "сломано"

	again, ok := Catalog().Find("model")
	if !ok {
		t.Fatal("Find(model) second lookup = false, want true")
	}
	if reflect.DeepEqual(entry.Tags, again.Tags) {
		t.Fatalf("catalog entry tags were mutated: %#v", again.Tags)
	}
	if reflect.DeepEqual(entry.EnglishAliases, again.EnglishAliases) {
		t.Fatalf("catalog entry aliases were mutated: %#v", again.EnglishAliases)
	}
	if reflect.DeepEqual(entry.RussianAliases, again.RussianAliases) {
		t.Fatalf("catalog entry russian aliases were mutated: %#v", again.RussianAliases)
	}
}

func TestCatalogListUsesPreferredLanguageUntilQuerySwitchesScript(t *testing.T) {
	catalog := Catalog()

	russianList := catalog.List(CatalogListOptions{
		Language: CatalogLanguageRussian,
	})
	chatItem, ok := findCatalogListItem(russianList, "chat")
	if !ok {
		t.Fatalf("List(ru) did not include chat: %#v", catalogDisplayNames(russianList))
	}
	if got, want := chatItem.DisplayLanguage, CatalogLanguageRussian; got != want {
		t.Fatalf("List(ru).DisplayLanguage = %q, want %q", got, want)
	}
	if got, want := chatItem.Name, "чат"; got != want {
		t.Fatalf("List(ru).Name = %q, want %q", got, want)
	}
	if got, want := chatItem.MirrorName, "chat"; got != want {
		t.Fatalf("List(ru).MirrorName = %q, want %q", got, want)
	}

	englishQueryList := catalog.List(CatalogListOptions{
		Language: CatalogLanguageRussian,
		Query:    "rev",
	})
	if len(englishQueryList) != 1 {
		t.Fatalf("List(ru, rev) len = %d, want 1", len(englishQueryList))
	}
	if got, want := englishQueryList[0].DisplayLanguage, CatalogLanguageEnglish; got != want {
		t.Fatalf("List(ru, rev).DisplayLanguage = %q, want %q", got, want)
	}
	if got, want := englishQueryList[0].Name, "review"; got != want {
		t.Fatalf("List(ru, rev).Name = %q, want %q", got, want)
	}
	if got, want := englishQueryList[0].MirrorName, "ревью"; got != want {
		t.Fatalf("List(ru, rev).MirrorName = %q, want %q", got, want)
	}

	russianQueryList := catalog.List(CatalogListOptions{
		Language: CatalogLanguageEnglish,
		Query:    "прод",
	})
	if len(russianQueryList) != 1 {
		t.Fatalf("List(en, прод) len = %d, want 1", len(russianQueryList))
	}
	if got, want := russianQueryList[0].DisplayLanguage, CatalogLanguageRussian; got != want {
		t.Fatalf("List(en, про).DisplayLanguage = %q, want %q", got, want)
	}
	if got, want := russianQueryList[0].Name, "продолжить"; got != want {
		t.Fatalf("List(en, про).Name = %q, want %q", got, want)
	}
	if got, want := russianQueryList[0].MirrorName, "resume"; got != want {
		t.Fatalf("List(en, про).MirrorName = %q, want %q", got, want)
	}
}

func TestCatalogListSupportsCategoryAndTagFilters(t *testing.T) {
	catalog := Catalog()

	items := catalog.List(CatalogListOptions{
		Language: CatalogLanguageEnglish,
		Category: "account",
		Tag:      "profiles",
	})
	if got, want := catalogDisplayNames(items), []string{"profiles"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List(category=account, tag=profiles) = %#v, want %#v", got, want)
	}
	if got, want := items[0].CategoryLabel, "Account"; got != want {
		t.Fatalf("CategoryLabel = %q, want %q", got, want)
	}
}

func TestCatalogLookupAliasesExposeBothLanguages(t *testing.T) {
	items := Catalog().List(CatalogListOptions{
		Language: CatalogLanguageRussian,
		Query:    "model",
	})
	if len(items) != 1 {
		t.Fatalf("List(model) len = %d, want 1", len(items))
	}
	if got, want := catalogLookupAliases(items[0]), []string{"models", "модели"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogLookupAliases = %#v, want %#v", got, want)
	}
}

func TestDetectCatalogLanguage(t *testing.T) {
	tests := []struct {
		value string
		want  CatalogLanguage
	}{
		{value: "resume", want: CatalogLanguageEnglish},
		{value: "прод", want: CatalogLanguageRussian},
		{value: "resume 123", want: CatalogLanguageEnglish},
		{value: "прод 123", want: CatalogLanguageRussian},
		{value: "resume прод", want: CatalogLanguageUnknown},
		{value: "123", want: CatalogLanguageUnknown},
	}
	for _, tt := range tests {
		if got := DetectCatalogLanguage(tt.value); got != tt.want {
			t.Fatalf("DetectCatalogLanguage(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestNewCommandCatalogRejectsDuplicateLookupKeys(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("newCommandCatalog() panic = nil, want duplicate key panic")
		}
	}()

	newCommandCatalog([]CatalogEntry{
		{Name: "first", EnglishAliases: []string{"shared"}},
		{Name: "second", RussianAliases: []string{"shared"}},
	})
}

func findCatalogListItem(items []CatalogListItem, command string) (CatalogListItem, bool) {
	for _, item := range items {
		if item.Command == command {
			return item, true
		}
	}
	return CatalogListItem{}, false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

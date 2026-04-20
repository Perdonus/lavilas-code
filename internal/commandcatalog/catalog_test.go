package commandcatalog

import (
	"reflect"
	"testing"
)

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
		{key: "language", preferred: CatalogLanguageEnglish, wantCommand: "setlang", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: false},
		{key: "язык", preferred: CatalogLanguageRussian, wantCommand: "setlang", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: true},
		{key: "approvals", preferred: CatalogLanguageEnglish, wantCommand: "permissions", wantLanguage: CatalogLanguageEnglish, wantPrimaryKey: false},
		{key: "подтверждения", preferred: CatalogLanguageRussian, wantCommand: "permissions", wantLanguage: CatalogLanguageRussian, wantPrimaryKey: false},
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

func TestCatalogListSwitchesDisplayLanguageByQueryScript(t *testing.T) {
	catalog := Catalog()

	russianList := catalog.List(CatalogListOptions{Language: CatalogLanguageRussian})
	chatItem, ok := findCatalogListItem(russianList, "chat")
	if !ok {
		t.Fatalf("List(ru) did not include chat: %#v", listNames(russianList))
	}
	if got, want := chatItem.DisplayLanguage, CatalogLanguageRussian; got != want {
		t.Fatalf("List(ru).DisplayLanguage = %q, want %q", got, want)
	}
	if got, want := chatItem.Name, "чат"; got != want {
		t.Fatalf("List(ru).Name = %q, want %q", got, want)
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
	if got, want := englishQueryList[0].MirrorName, "ревью"; got != want {
		t.Fatalf("List(ru, rev).MirrorName = %q, want %q", got, want)
	}
	if got, want := englishQueryList[0].DisplayName, "review (ревью)"; got != want {
		t.Fatalf("List(ru, rev).DisplayName = %q, want %q", got, want)
	}
	if got, want := englishQueryList[0].InsertName, "review"; got != want {
		t.Fatalf("List(ru, rev).InsertName = %q, want %q", got, want)
	}
}

func TestCatalogPresentUsesMirrorDisplayWithoutChangingInsertName(t *testing.T) {
	catalog := Catalog()

	item, ok := catalog.Present("resume", CatalogLanguageEnglish, "прод")
	if !ok {
		t.Fatal("Present(resume) = false, want true")
	}
	if got, want := item.DisplayLanguage, CatalogLanguageRussian; got != want {
		t.Fatalf("DisplayLanguage = %q, want %q", got, want)
	}
	if got, want := item.DisplayName, "продолжить (resume)"; got != want {
		t.Fatalf("DisplayName = %q, want %q", got, want)
	}
	if got, want := item.InsertName, "продолжить"; got != want {
		t.Fatalf("InsertName = %q, want %q", got, want)
	}
	if got, want := item.DisplayMirrorName, "resume"; got != want {
		t.Fatalf("DisplayMirrorName = %q, want %q", got, want)
	}
	if got, want := item.DisplayShowsMirror, true; got != want {
		t.Fatalf("DisplayShowsMirror = %v, want %v", got, want)
	}
}

func TestCatalogListKeepsPresentationOrder(t *testing.T) {
	items := Catalog().List(CatalogListOptions{Language: CatalogLanguageEnglish})
	if len(items) < 10 {
		t.Fatalf("List(en) len = %d, want >= 10", len(items))
	}
	got := []string{
		items[0].Command,
		items[1].Command,
		items[2].Command,
		items[3].Command,
		items[4].Command,
		items[5].Command,
		items[6].Command,
		items[7].Command,
	}
	want := []string{"model", "profiles", "review", "resume", "fork", "status", "logout", "settings"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List(en) leading order = %#v, want %#v", got, want)
	}
}

func TestCatalogLookupAliasesIncludePrimaryAndMirrorNames(t *testing.T) {
	items := Catalog().List(CatalogListOptions{
		Language: CatalogLanguageRussian,
		Query:    "rev",
	})
	if len(items) != 1 {
		t.Fatalf("List(ru, rev) len = %d, want 1", len(items))
	}
	if got, want := catalogLookupAliases(items[0]), []string{"rev", "review", "ревью"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("catalogLookupAliases = %#v, want %#v", got, want)
	}
}

func TestCatalogCommandsExposeAliasesAndDescriptions(t *testing.T) {
	commands := Catalog().Commands()
	var settings Command
	found := false
	for _, command := range commands {
		if command.Name == "settings" {
			settings = command
			found = true
			break
		}
	}
	if !found {
		t.Fatal("settings command missing")
	}
	if got, want := settings.Description, "Show saved UI settings"; got != want {
		t.Fatalf("settings.Description = %q, want %q", got, want)
	}
	if got, want := settings.Aliases, []string{"prefs", "config", "настройки"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("settings.Aliases = %#v, want %#v", got, want)
	}
}

func TestNewCommandCatalogRejectsDuplicateLookupKeys(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewCommandCatalog() panic = nil, want duplicate key panic")
		}
	}()

	NewCommandCatalog([]CatalogEntry{
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

func listNames(items []CatalogListItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

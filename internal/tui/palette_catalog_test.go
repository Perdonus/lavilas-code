package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/commandcatalog"
)

func TestDefaultPaletteCatalogRootOrderFollowsPresentationOrder(t *testing.T) {
	catalog := defaultPaletteCatalog()
	items := catalog.RootItems(commandcatalog.CatalogLanguageEnglish, "")
	if len(items) < 10 {
		t.Fatalf("RootItems(en) len = %d, want >= 10", len(items))
	}
	got := []string{
		items[0].Key,
		items[1].Key,
		items[2].Key,
		items[3].Key,
		items[4].Key,
		items[5].Key,
		items[6].Key,
		items[7].Key,
	}
	want := []string{"model", "profiles", "settings", "new", "resume_latest", "fork_latest", "sessions_resume", "sessions_fork"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RootItems(en) leading order = %#v, want %#v", got, want)
	}
}

func TestDefaultPaletteCatalogMirrorsDisplayAndInsertByQueryLanguage(t *testing.T) {
	catalog := defaultPaletteCatalog()
	items := catalog.RootItems(commandcatalog.CatalogLanguageRussian, "model")

	if len(items) == 0 {
		t.Fatal("RootItems(ru, model) returned no items")
	}
	model := items[0]
	if got, want := model.Key, "model"; got != want {
		t.Fatalf("items[0].Key = %q, want %q", got, want)
	}
	if got, want := model.Title, "model (модель)"; got != want {
		t.Fatalf("items[0].Title = %q, want %q", got, want)
	}
	if got, want := model.Value, "model"; got != want {
		t.Fatalf("items[0].Value = %q, want %q", got, want)
	}
	if !containsPaletteAlias(model.Aliases, "/модели") {
		t.Fatalf("items[0].Aliases missing /модели: %#v", model.Aliases)
	}
}

func TestDefaultPaletteCatalogLookupBySlashSupportsMirrorAliases(t *testing.T) {
	catalog := defaultPaletteCatalog()
	tests := map[string]string{
		"model":         "model",
		"модели":        "model",
		"accounts":      "profiles",
		"аккаунты":      "profiles",
		"параметры":     "settings",
		"выйти-аккаунт": "logout",
		"provider":      "providers",
		"провайдер":     "providers",
	}
	for needle, want := range tests {
		command, ok := catalog.LookupBySlash(needle)
		if !ok {
			t.Fatalf("LookupBySlash(%q) = false, want true", needle)
		}
		if got := command.CatalogCommand; got != want {
			t.Fatalf("LookupBySlash(%q).CatalogCommand = %q, want %q", needle, got, want)
		}
	}
}

func TestDefaultPaletteCatalogHelpTextUsesMirrorLabelForOppositeQueryScript(t *testing.T) {
	catalog := defaultPaletteCatalog()
	help := catalog.HelpText("/", commandcatalog.CatalogLanguageRussian, "rev")
	if !strings.Contains(help, "/review (ревью)") {
		t.Fatalf("HelpText missing mirrored review label: %q", help)
	}
	if !strings.Contains(help, "Run non-interactive review") {
		t.Fatalf("HelpText missing english review description: %q", help)
	}
}

func containsPaletteAlias(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	appstate "github.com/Perdonus/lavilas-code/internal/state"
)

func newIsolatedModel(t *testing.T) *Model {
	t.Helper()
	model := NewModel(DefaultState())
	model.layout = apphome.NewLayout(t.TempDir())
	return model
}

func saveTestSettings(t *testing.T, model *Model, settings appstate.Settings) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(model.layout.SettingsPath()), 0o755); err != nil {
		t.Fatalf("MkdirAll(settings dir): %v", err)
	}
	if err := appstate.SaveSettings(model.layout.SettingsPath(), settings); err != nil {
		t.Fatalf("SaveSettings(): %v", err)
	}
}

func TestPaletteBackContextAndReturnFocus(t *testing.T) {
	model := NewModel(DefaultState())
	model.state.Focus = FocusTranscript

	model.openPaletteMode(PaletteModeRoot, false)
	if got := model.state.Palette.Context.BackTitle; got != "Back to Chat" {
		t.Fatalf("root back title = %q, want %q", got, "Back to Chat")
	}
	if got := model.state.Palette.Context.ReturnFocus; got != FocusTranscript {
		t.Fatalf("root return focus = %q, want %q", got, FocusTranscript)
	}

	model.openPaletteMode(PaletteModeSettings, true)
	if got := model.paletteBackItem().Title; got != "Back to Palette" {
		t.Fatalf("settings back title = %q, want %q", got, "Back to Palette")
	}

	model.openPaletteMode(PaletteModeModel, true)
	if got := model.paletteBackItem().Title; got != "Back to Settings" {
		t.Fatalf("model back title = %q, want %q", got, "Back to Settings")
	}

	model.navigatePaletteBack()
	if got := model.state.Palette.Mode; got != PaletteModeSettings {
		t.Fatalf("mode after first back = %q, want %q", got, PaletteModeSettings)
	}

	model.navigatePaletteBack()
	if got := model.state.Palette.Mode; got != PaletteModeRoot {
		t.Fatalf("mode after second back = %q, want %q", got, PaletteModeRoot)
	}

	model.closePalette()
	if got := model.state.Focus; got != FocusTranscript {
		t.Fatalf("focus after close = %q, want %q", got, FocusTranscript)
	}
}

func TestFilteredPaletteItemsUseAliasesAndKeywords(t *testing.T) {
	model := NewModel(DefaultState())
	model.state.Palette.Mode = PaletteModeSettings
	model.state.Palette.Context = PaletteContext{
		BackTitle:       "Back to Chat",
		BackDescription: "Return to transcript",
		BackHint:        "Enter select · Esc close",
		ReturnFocus:     FocusInput,
	}
	model.state.Palette.Items = model.decoratePaletteItems(PaletteModeSettings, []PaletteItem{
		{Key: "model", Title: "Model", Description: "Inspect active model", Aliases: []string{"/model"}, Keywords: []string{"reasoning", "provider"}},
		{Key: "providers", Title: "Providers", Description: "Inspect configured providers", Aliases: []string{"/providers"}, Keywords: []string{"wire_api"}},
	})

	model.state.Palette.Query = "/providers"
	items := model.filteredPaletteItems()
	if len(items) != 2 {
		t.Fatalf("alias filter length = %d, want 2", len(items))
	}
	if got := items[1].Key; got != "providers" {
		t.Fatalf("alias filter key = %q, want %q", got, "providers")
	}

	model.state.Palette.Query = "reasoning"
	items = model.filteredPaletteItems()
	if len(items) != 2 {
		t.Fatalf("keyword filter length = %d, want 2", len(items))
	}
	if got := items[1].Key; got != "model" {
		t.Fatalf("keyword filter key = %q, want %q", got, "model")
	}
}

func TestSettingsModelSettingsBackFlow(t *testing.T) {
	model := NewModel(DefaultState())

	model.openPaletteMode(PaletteModeSettings, false)
	model.state.Palette.Selected = 1
	if cmd := model.activatePaletteSelection(); cmd == nil {
		t.Fatal("activatePaletteSelection(settings.model_settings) returned nil")
	}

	if got := model.state.Palette.Mode; got != PaletteModeModelSettings {
		t.Fatalf("mode after opening model settings = %q, want %q", got, PaletteModeModelSettings)
	}
	if got := model.paletteBackItem().Title; got != "Back to Settings" {
		t.Fatalf("model settings back title = %q, want %q", got, "Back to Settings")
	}

	if cmd := model.navigatePaletteBack(); cmd == nil {
		t.Fatal("navigatePaletteBack() returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeSettings {
		t.Fatalf("mode after navigating back = %q, want %q", got, PaletteModeSettings)
	}
}

func TestProfilesOpenedFromModelSettingsReturnBackToModelSettings(t *testing.T) {
	model := NewModel(DefaultState())

	model.openPaletteMode(PaletteModeSettings, false)
	model.state.Palette.Selected = 1
	_ = model.activatePaletteSelection()
	model.state.Palette.Selected = 2
	if cmd := model.activatePaletteSelection(); cmd == nil {
		t.Fatal("activatePaletteSelection(model_settings.profiles) returned nil")
	}

	if got := model.state.Palette.Mode; got != PaletteModeProfiles {
		t.Fatalf("mode after opening profiles = %q, want %q", got, PaletteModeProfiles)
	}
	if got := model.paletteBackItem().Title; got != "Back to Model Settings" {
		t.Fatalf("profiles back title = %q, want %q", got, "Back to Model Settings")
	}

	if cmd := model.navigatePaletteBack(); cmd == nil {
		t.Fatal("navigatePaletteBack() returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeModelSettings {
		t.Fatalf("mode after backing out of profiles = %q, want %q", got, PaletteModeModelSettings)
	}
}

func TestProfilesReopenedFromSettingsReturnBackToModelSettings(t *testing.T) {
	model := NewModel(DefaultState())
	model.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)

	if cmd := model.reopenProfilesPalette(); cmd == nil {
		t.Fatal("reopenProfilesPalette() returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeProfiles {
		t.Fatalf("mode after reopening profiles = %q, want %q", got, PaletteModeProfiles)
	}
	if got := model.paletteBackItem().Title; got != "Back to Model Settings" {
		t.Fatalf("profiles back title = %q, want %q", got, "Back to Model Settings")
	}

	if cmd := model.navigatePaletteBack(); cmd == nil {
		t.Fatal("navigatePaletteBack() returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeModelSettings {
		t.Fatalf("mode after backing out of reopened profiles = %q, want %q", got, PaletteModeModelSettings)
	}
}

func TestProfilesOpenedFromCommandReturnBackToChat(t *testing.T) {
	model := NewModel(DefaultState())
	model.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginCommand)

	if cmd := model.openProfilesPalette(false); cmd == nil {
		t.Fatal("openProfilesPalette(false) returned nil")
	}
	if got := model.paletteBackItem().Title; got != "Back to Chat" {
		t.Fatalf("profiles back title = %q, want %q", got, "Back to Chat")
	}

	if cmd := model.navigatePaletteBack(); cmd == nil {
		t.Fatal("navigatePaletteBack() returned nil")
	}
	if model.state.Palette.Visible {
		t.Fatal("palette should be closed after command-origin profiles back")
	}
}

func TestPresetEditorReopenedReturnsToModelPresetsSettings(t *testing.T) {
	model := NewModel(DefaultState())
	model.setModelSettingsNavigationOrigin(ModelSettingsNavigationOriginSettings)
	model.state.Palette.Visible = true
	model.state.Palette.Mode = PaletteModePresetEditor
	model.state.Palette.Context = model.directPaletteContext(PaletteModePresetEditor)
	model.state.Palette.Stack = nil

	if cmd := model.navigatePaletteBack(); cmd == nil {
		t.Fatal("navigatePaletteBack() returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeModelPresets {
		t.Fatalf("mode after preset editor back = %q, want %q", got, PaletteModeModelPresets)
	}
	if got := model.paletteBackItem().Title; got != "Back to Model Settings" {
		t.Fatalf("model presets back title = %q, want %q", got, "Back to Model Settings")
	}
}

func TestSettingsLanguagePaletteUpdatesConfiguredLanguage(t *testing.T) {
	model := newIsolatedModel(t)
	saveTestSettings(t, model, appstate.Settings{Language: "en"})

	model.openPaletteMode(PaletteModeSettings, false)
	model.state.Palette.Selected = 2
	if cmd := model.activatePaletteSelection(); cmd == nil {
		t.Fatal("activatePaletteSelection(settings.language) returned nil")
	}
	if got := model.state.Palette.Mode; got != PaletteModeLanguage {
		t.Fatalf("mode after opening language picker = %q, want %q", got, PaletteModeLanguage)
	}

	model.state.Palette.Selected = 2
	if cmd := model.activatePaletteSelection(); cmd == nil {
		t.Fatal("activatePaletteSelection(settings.language.option) returned nil")
	}

	settings, err := loadSettingsOptional(model.layout.SettingsPath())
	if err != nil {
		t.Fatalf("loadSettingsOptional(): %v", err)
	}
	if got, want := settings.Language, "ru"; got != want {
		t.Fatalf("settings.Language = %q, want %q", got, want)
	}
	if got, want := model.language, normalizeTUILanguage("ru"); got != want {
		t.Fatalf("model.language = %q, want %q", got, want)
	}
}

func TestRootPaletteFiltersHiddenCommands(t *testing.T) {
	model := newIsolatedModel(t)
	saveTestSettings(t, model, appstate.Settings{HiddenCommands: []string{"model"}})

	items := model.rootPaletteItemsForQuery("")
	for _, item := range items {
		if item.Key == "model" {
			t.Fatalf("root palette still contains hidden command: %#v", items)
		}
	}
}

func TestPopupCommandsPaletteShowsHiddenState(t *testing.T) {
	model := newIsolatedModel(t)
	saveTestSettings(t, model, appstate.Settings{HiddenCommands: []string{"model"}})

	items := model.popupCommandPaletteItems()
	for _, item := range items {
		if item.Value != "model" {
			continue
		}
		if got := item.Key; got != "settings.hidden_commands.entry" {
			t.Fatalf("popup item key = %q, want %q", got, "settings.hidden_commands.entry")
		}
		if got := item.Description; got == "" {
			t.Fatal("popup command description should expose visibility state")
		}
		return
	}
	t.Fatal("popup commands palette missing model entry")
}

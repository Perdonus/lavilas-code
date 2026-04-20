package tui

import "testing"

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

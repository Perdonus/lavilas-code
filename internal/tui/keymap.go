package tui

import "github.com/charmbracelet/bubbles/key"

type KeyMap struct {
	Quit          key.Binding
	TogglePalette key.Binding
	NextFocus     key.Binding
	PrevFocus     key.Binding
	Submit        key.Binding
	Close         key.Binding
	Up            key.Binding
	Down          key.Binding
	PageUp        key.Binding
	PageDown      key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "q"),
			key.WithHelp("q", "quit"),
		),
		TogglePalette: key.NewBinding(
			key.WithKeys("ctrl+p", ":"),
			key.WithHelp("ctrl+p", "palette"),
		),
		NextFocus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next focus"),
		),
		PrevFocus: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("shift+tab", "prev focus"),
		),
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "submit"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("up", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("down", "move down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.TogglePalette, k.NextFocus, k.Submit, k.Quit}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.TogglePalette, k.Submit, k.Close},
		{k.NextFocus, k.PrevFocus},
		{k.Up, k.Down, k.PageUp, k.PageDown},
		{k.Quit},
	}
}

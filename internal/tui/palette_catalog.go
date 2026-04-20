package tui

import (
	"fmt"
	"strings"
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

type PaletteCommandSpec struct {
	Key         string
	Slash       string
	Aliases     []string
	Title       string
	Description string
	Keywords    []string
	Action      PaletteCommandAction
	Mode        PaletteMode
	ShowInRoot  bool
	ShowInHelp  bool
}

type PaletteCommandCatalog interface {
	RootItems() []PaletteItem
	LookupByKey(key string) (PaletteCommandSpec, bool)
	LookupBySlash(name string) (PaletteCommandSpec, bool)
	HelpText(prefix string) string
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
			Key:         "new",
			Slash:       "new",
			Title:       "New Session",
			Description: "Clear transcript and start fresh",
			Keywords:    []string{"fresh", "reset", "clear"},
			Action:      PaletteActionNewSession,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "resume_latest",
			Slash:       "resume",
			Title:       "Resume Latest",
			Description: "Load the latest saved session",
			Keywords:    []string{"continue", "latest", "session"},
			Action:      PaletteActionResumeLatest,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "fork_latest",
			Slash:       "fork",
			Title:       "Fork Latest",
			Description: "Load the latest session as a new branch",
			Keywords:    []string{"branch", "copy", "session"},
			Action:      PaletteActionForkLatest,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "sessions_resume",
			Slash:       "sessions",
			Title:       "Sessions",
			Description: "Browse saved sessions to resume",
			Keywords:    []string{"history", "saved", "resume"},
			Action:      PaletteActionBrowseResume,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "sessions_fork",
			Title:       "Fork Session",
			Description: "Browse saved sessions to fork",
			Keywords:    []string{"history", "saved", "fork", "branch"},
			Action:      PaletteActionBrowseFork,
			ShowInRoot:  true,
		},
		{
			Key:         "model",
			Slash:       "model",
			Title:       "Model",
			Description: "Inspect active model and reasoning",
			Keywords:    []string{"reasoning", "provider", "profile"},
			Action:      PaletteActionOpenMode,
			Mode:        PaletteModeModel,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "profiles",
			Slash:       "profiles",
			Title:       "Profiles",
			Description: "Inspect configured profiles",
			Keywords:    []string{"accounts", "profile", "config"},
			Action:      PaletteActionOpenMode,
			Mode:        PaletteModeProfiles,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "providers",
			Slash:       "providers",
			Title:       "Providers",
			Description: "Inspect configured providers",
			Keywords:    []string{"api", "base_url", "wire_api"},
			Action:      PaletteActionOpenMode,
			Mode:        PaletteModeProviders,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "settings",
			Slash:       "settings",
			Title:       "Settings",
			Description: "Inspect saved UI settings",
			Keywords:    []string{"preferences", "ui", "config"},
			Action:      PaletteActionOpenMode,
			Mode:        PaletteModeSettings,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "status",
			Slash:       "status",
			Title:       "Status",
			Description: "Show runtime status summary",
			Keywords:    []string{"runtime", "summary", "health"},
			Action:      PaletteActionShowStatus,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "help",
			Slash:       "help",
			Aliases:     []string{"?"},
			Title:       "Help",
			Description: "Show keyboard and slash commands",
			Keywords:    []string{"keys", "shortcuts", "commands"},
			Action:      PaletteActionShowHelp,
			ShowInRoot:  true,
			ShowInHelp:  true,
		},
		{
			Key:         "palette",
			Slash:       "palette",
			Title:       "Palette",
			Description: "Open the command palette",
			Keywords:    []string{"commands", "menu"},
			Action:      PaletteActionOpenPalette,
			ShowInHelp:  true,
		},
		{
			Key:         "exit",
			Slash:       "exit",
			Aliases:     []string{"quit"},
			Title:       "Exit",
			Description: "Quit the TUI",
			Keywords:    []string{"quit", "close"},
			Action:      PaletteActionQuit,
			ShowInHelp:  true,
		},
	}
}

func (catalog staticPaletteCommandCatalog) RootItems() []PaletteItem {
	items := make([]PaletteItem, 0, len(catalog.commands))
	for _, command := range catalog.commands {
		if !command.ShowInRoot {
			continue
		}
		items = append(items, catalog.commandToItem(command))
	}
	return items
}

func (catalog staticPaletteCommandCatalog) LookupByKey(key string) (PaletteCommandSpec, bool) {
	needle := strings.TrimSpace(strings.ToLower(key))
	if needle == "" {
		return PaletteCommandSpec{}, false
	}
	for _, command := range catalog.commands {
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
	for _, command := range catalog.commands {
		if normalizePaletteCommandName(command.Slash) == needle {
			return command, true
		}
		for _, alias := range command.Aliases {
			if normalizePaletteCommandName(alias) == needle {
				return command, true
			}
		}
	}
	return PaletteCommandSpec{}, false
}

func (catalog staticPaletteCommandCatalog) HelpText(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/"
	}
	lines := []string{"Slash commands:"}
	width := 0
	commands := make([]PaletteCommandSpec, 0, len(catalog.commands))
	for _, command := range catalog.commands {
		if !command.ShowInHelp || strings.TrimSpace(command.Slash) == "" {
			continue
		}
		commands = append(commands, command)
		labelWidth := len(prefix) + len(command.Slash)
		if labelWidth > width {
			width = labelWidth
		}
	}
	for _, command := range commands {
		lines = append(lines, fmt.Sprintf("%-*s %s", width, prefix+command.Slash, command.Description))
	}
	return strings.Join(lines, "\n")
}

func (catalog staticPaletteCommandCatalog) commandToItem(command PaletteCommandSpec) PaletteItem {
	aliases := make([]string, 0, len(command.Aliases)+2)
	if strings.TrimSpace(command.Slash) != "" {
		aliases = append(aliases, command.Slash, "/"+command.Slash)
	}
	for _, alias := range command.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		aliases = append(aliases, alias)
		if !strings.HasPrefix(alias, "/") {
			aliases = append(aliases, "/"+alias)
		}
	}
	return PaletteItem{
		Key:         command.Key,
		Title:       command.Title,
		Description: command.Description,
		Aliases:     aliases,
		Keywords:    command.Keywords,
	}
}

func normalizePaletteCommandName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "/")
	return value
}

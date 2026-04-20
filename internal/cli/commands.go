package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/doctor"
	"github.com/Perdonus/lavilas-code/internal/state"
)

func registry() []Command {
	stub := func(name string) func([]string) int {
		return func(args []string) int {
			fmt.Printf("%s is not implemented in alpha yet.\\n", name)
			return 2
		}
	}

	return []Command{
		{Name: "resume", Aliases: []string{"r", "continue", "продолжить"}, Description: "Show recent sessions from ~/.codex", Category: "interactive", Run: runResume},
		{Name: "fork", Aliases: []string{"branch-chat", "форк"}, Description: "Fork a previous session", Category: "interactive", Run: stub("fork")},
		{Name: "run", Aliases: []string{"exec", "ask", "запуск"}, Description: "Execute a one-shot task", Category: "interactive", Run: stub("run")},
		{Name: "review", Aliases: []string{"rev", "ревью"}, Description: "Run non-interactive review", Category: "interactive", Run: stub("review")},
		{Name: "apply", Aliases: []string{"patch", "применить"}, Description: "Apply latest agent patch", Category: "interactive", Run: stub("apply")},

		{Name: "login", Aliases: []string{"auth", "вход"}, Description: "Configure account access", Category: "account", Run: stub("login")},
		{Name: "logout", Aliases: []string{"unauth", "выход"}, Description: "Remove saved account access", Category: "account", Run: stub("logout")},
		{Name: "profiles", Aliases: []string{"accounts", "prof", "профили", "аккаунты"}, Description: "List saved profiles from config", Category: "account", Run: runProfiles},

		{Name: "model", Aliases: []string{"models", "модель", "модели"}, Description: "Show active model and reasoning", Category: "config", Run: runModel},
		{Name: "settings", Aliases: []string{"prefs", "config", "настройки"}, Description: "Show saved UI settings", Category: "config", Run: runSettings},
		{Name: "completion", Aliases: []string{"completions", "автодополнение"}, Description: "Generate shell completions", Category: "config", Run: stub("completion")},
		{Name: "features", Aliases: []string{"flags", "фичи"}, Description: "Inspect feature toggles", Category: "config", Run: stub("features")},

		{Name: "mcp", Aliases: []string{"tools", "инструменты"}, Description: "Manage MCP tools", Category: "automation", Run: stub("mcp")},
		{Name: "mcp-server", Aliases: []string{"server", "сервер"}, Description: "Run stdio MCP server", Category: "automation", Run: stub("mcp-server")},
		{Name: "cloud", Aliases: []string{"cloud-tasks", "облако"}, Description: "Inspect remote cloud tasks", Category: "automation", Run: stub("cloud")},

		{Name: "sandbox", Aliases: []string{"isolate", "песочница"}, Description: "Run inside Go Lavilas sandbox", Category: "runtime", Run: stub("sandbox")},
		{Name: "update", Aliases: []string{"upgrade", "обновить"}, Description: "Check for updates", Category: "runtime", Run: stub("update")},
		{Name: "doctor", Aliases: []string{"diag", "диагностика"}, Description: "Inspect local environment", Category: "runtime", Run: runDoctor},

		{Name: "debug", Aliases: []string{"dbg", "дебаг"}, Description: "Debug helpers", Category: "debug", Run: stub("debug")},
	}
}

func runDoctor(args []string) int {
	return doctor.Run()
}

func runModel(args []string) int {
	config, err := state.LoadConfigSummary(apphome.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	fmt.Printf("model: %s\n", fallback(config.Model, "<unset>"))
	fmt.Printf("reasoning: %s\n", fallback(config.Reasoning, "<unset>"))
	fmt.Printf("profiles: %d\n", len(config.Profiles))
	fmt.Printf("providers: %d\n", len(config.ModelProviders))
	return 0
}

func runProfiles(args []string) int {
	config, err := state.LoadConfigSummary(apphome.ConfigPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	if len(config.Profiles) == 0 {
		fmt.Println("No stored profiles found in config.")
		return 0
	}

	fmt.Println("Profiles:")
	for _, name := range config.Profiles {
		fmt.Printf("  - %s\n", name)
	}
	return 0
}

func runSettings(args []string) int {
	settings, err := state.LoadSettingsSummary(apphome.SettingsPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read settings: %v\n", err)
		return 1
	}

	fmt.Printf("language: %s\n", fallback(settings.Language, "<unset>"))
	fmt.Printf("command_prefix: %s\n", fallback(settings.CommandPrefix, "<unset>"))
	fmt.Printf("hidden_commands: %d\n", len(settings.HiddenCommands))
	fmt.Printf("selection_fill: %t\n", settings.SelectionHighlightFill)
	fmt.Printf("selection_preset: %s\n", fallback(settings.SelectionHighlightPreset, "<unset>"))
	fmt.Printf("selection_color: %s\n", fallback(settings.SelectionHighlightColor, "<unset>"))
	fmt.Printf("list_primary_color: %s\n", fallback(settings.ListPrimaryColor, "<unset>"))
	fmt.Printf("list_secondary_color: %s\n", fallback(settings.ListSecondaryColor, "<unset>"))
	fmt.Printf("reply_text_color: %s\n", fallback(settings.ReplyTextColor, "<unset>"))
	fmt.Printf("command_text_color: %s\n", fallback(settings.CommandTextColor, "<unset>"))
	fmt.Printf("reasoning_text_color: %s\n", fallback(settings.ReasoningTextColor, "<unset>"))
	fmt.Printf("command_output_text_color: %s\n", fallback(settings.CommandOutputTextColor, "<unset>"))
	return 0
}

func runResume(args []string) int {
	sessions, err := state.LoadSessions(apphome.SessionsDir(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read sessions: %v\n", err)
		return 1
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return 0
	}

	lastOnly := false
	for _, arg := range args {
		if arg == "--last" {
			lastOnly = true
		}
	}

	if lastOnly {
		fmt.Println(sessions[0].Path)
		return 0
	}

	fmt.Println("Recent sessions:")
	for _, session := range sessions {
		fmt.Printf(
			"  - %s  %s  %s\n",
			session.ModTime.Format(time.RFC3339),
			humanSize(session.Size),
			session.RelPath,
		)
	}
	return 0
}

func fallback(value string, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}

func humanSize(size int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case size >= mb:
		return fmt.Sprintf("%.1fMB", float64(size)/float64(mb))
	case size >= kb:
		return fmt.Sprintf("%.1fKB", float64(size)/float64(kb))
	default:
		return fmt.Sprintf("%dB", size)
	}
}

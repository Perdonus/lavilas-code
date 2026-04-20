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
		{Name: "resume", Aliases: []string{"r"}, Description: "Show recent sessions from ~/.codex", Run: runResume},
		{Name: "run", Aliases: []string{"exec"}, Description: "Execute a one-shot task", Run: stub("run")},
		{Name: "login", Description: "Configure account access", Run: stub("login")},
		{Name: "logout", Description: "Remove saved account access", Run: stub("logout")},
		{Name: "model", Description: "Show active model and reasoning", Run: runModel},
		{Name: "profiles", Description: "List saved profiles from config", Run: runProfiles},
		{Name: "settings", Description: "Show saved UI settings", Run: runSettings},
		{Name: "update", Description: "Check for updates", Run: stub("update")},
		{Name: "doctor", Description: "Inspect local environment", Run: runDoctor},
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

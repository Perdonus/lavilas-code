package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/doctor"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
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
		{Name: "run", Aliases: []string{"exec", "ask", "запуск"}, Description: "Execute a one-shot task", Category: "interactive", Run: runTask},
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
	return doctor.Run(hasFlag(args, "--json"))
}

func runTask(args []string) int {
	options, err := parseRunOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		return 2
	}

	result, err := taskrun.Run(contextBackground(), options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		return 1
	}

	if options.JSON {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "run: failed to encode json: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
		return 0
	}

	if err := taskrun.Print(result); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		return 1
	}
	return 0
}

func runModel(args []string) int {
	configPath := apphome.ConfigPath()
	config, err := state.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	if len(args) > 0 && args[0] == "set" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "model: set requires a model name")
			return 2
		}
		modelName := args[1]
		config.SetModel(modelName)
		for index := 2; index < len(args); index++ {
			switch args[index] {
			case "--reasoning":
				value, next, err := takeFlagValue(args, index, "--reasoning")
				if err != nil {
					fmt.Fprintf(os.Stderr, "model: %v\n", err)
					return 2
				}
				config.SetReasoningEffort(value)
				index = next
			case "--profile":
				value, next, err := takeFlagValue(args, index, "--profile")
				if err != nil {
					fmt.Fprintf(os.Stderr, "model: %v\n", err)
					return 2
				}
				config.SetActiveProfile(value)
				index = next
			default:
				fmt.Fprintf(os.Stderr, "model: unknown flag %q\n", args[index])
				return 2
			}
		}
		if err := state.SaveConfig(configPath, config); err != nil {
			fmt.Fprintf(os.Stderr, "model: failed to save config: %v\n", err)
			return 1
		}
		fmt.Printf("model updated: %s\n", modelName)
		return 0
	}

	if hasFlag(args, "--json") {
		payload := map[string]any{
			"model":            fallback(config.EffectiveModel(), ""),
			"reasoning":        fallback(config.EffectiveReasoningEffort(), ""),
			"active_profile":   fallback(config.ActiveProfileName(), ""),
			"active_provider":  fallback(config.EffectiveProviderName(), ""),
			"profiles":         config.ProfileNames(),
			"providers":        config.ModelProviderNames(),
		}
		return printJSON(payload)
	}

	fmt.Printf("model: %s\n", fallback(config.EffectiveModel(), "<unset>"))
	fmt.Printf("reasoning: %s\n", fallback(config.EffectiveReasoningEffort(), "<unset>"))
	fmt.Printf("active_profile: %s\n", fallback(config.ActiveProfileName(), "<unset>"))
	fmt.Printf("active_provider: %s\n", fallback(config.EffectiveProviderName(), "<unset>"))
	fmt.Printf("profiles: %d\n", len(config.Profiles))
	fmt.Printf("providers: %d\n", len(config.ModelProviders))
	return 0
}

func runProfiles(args []string) int {
	configPath := apphome.ConfigPath()
	config, err := state.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	if len(args) > 0 {
		switch args[0] {
		case "activate":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "profiles: activate requires a profile name")
				return 2
			}
			if _, ok := config.Profile(args[1]); !ok {
				fmt.Fprintf(os.Stderr, "profiles: profile %q not found\n", args[1])
				return 1
			}
			config.SetActiveProfile(args[1])
			if err := state.SaveConfig(configPath, config); err != nil {
				fmt.Fprintf(os.Stderr, "profiles: failed to save config: %v\n", err)
				return 1
			}
			fmt.Printf("active profile: %s\n", args[1])
			return 0
		case "delete":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "profiles: delete requires a profile name")
				return 2
			}
			if !config.DeleteProfile(args[1]) {
				fmt.Fprintf(os.Stderr, "profiles: profile %q not found\n", args[1])
				return 1
			}
			if err := state.SaveConfig(configPath, config); err != nil {
				fmt.Fprintf(os.Stderr, "profiles: failed to save config: %v\n", err)
				return 1
			}
			fmt.Printf("deleted profile: %s\n", args[1])
			return 0
		}
	}

	if hasFlag(args, "--json") {
		type profileView struct {
			Name      string `json:"name"`
			Model     string `json:"model,omitempty"`
			Provider  string `json:"provider,omitempty"`
			Reasoning string `json:"reasoning,omitempty"`
			Active    bool   `json:"active"`
		}
		type providerView struct {
			Name      string `json:"name"`
			BaseURL   string `json:"base_url,omitempty"`
			WireAPI   string `json:"wire_api,omitempty"`
			APIKeyEnv string `json:"api_key_env,omitempty"`
		}

		profiles := make([]profileView, 0, len(config.Profiles))
		for _, profile := range config.Profiles {
			profiles = append(profiles, profileView{
				Name:      profile.Name,
				Model:     profile.Model,
				Provider:  profile.Provider,
				Reasoning: profile.ReasoningEffort,
				Active:    profile.Name == config.ActiveProfileName(),
			})
		}
		providers := make([]providerView, 0, len(config.ModelProviders))
		for _, provider := range config.ModelProviders {
			providers = append(providers, providerView{
				Name:      provider.Name,
				BaseURL:   provider.BaseURL,
				WireAPI:   provider.WireAPI,
				APIKeyEnv: provider.APIKeyEnv,
			})
		}

		payload := map[string]any{
			"active_profile": config.ActiveProfileName(),
			"profiles":       profiles,
			"providers":      providers,
		}
		return printJSON(payload)
	}

	if len(config.Profiles) == 0 {
		fmt.Println("No stored profiles found in config.")
		return 0
	}

	fmt.Println("Profiles:")
	for _, profile := range config.Profiles {
		suffix := ""
		if profile.Name == config.ActiveProfileName() {
			suffix = " (active)"
		}
		fmt.Printf(
			"  - %s%s  model=%s provider=%s reasoning=%s\n",
			profile.Name,
			suffix,
			fallback(profile.Model, "<unset>"),
			fallback(profile.Provider, "<unset>"),
			fallback(profile.ReasoningEffort, "<unset>"),
		)
	}
	return 0
}

func runSettings(args []string) int {
	settingsPath := apphome.SettingsPath()
	settings, err := state.LoadSettings(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read settings: %v\n", err)
		return 1
	}

	if len(args) > 0 {
		switch args[0] {
		case "language":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "settings: language requires a value")
				return 2
			}
			settings.SetLanguage(args[1])
			if err := state.SaveSettings(settingsPath, settings); err != nil {
				fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
				return 1
			}
			fmt.Printf("language updated: %s\n", args[1])
			return 0
		case "prefix":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "settings: prefix requires a value")
				return 2
			}
			settings.SetCommandPrefix(args[1])
			if err := state.SaveSettings(settingsPath, settings); err != nil {
				fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
				return 1
			}
			fmt.Printf("command prefix updated: %s\n", args[1])
			return 0
		case "hide-command":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "settings: hide-command requires a command key")
				return 2
			}
			settings.HideCommand(args[1])
			if err := state.SaveSettings(settingsPath, settings); err != nil {
				fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
				return 1
			}
			fmt.Printf("hidden command added: %s\n", args[1])
			return 0
		case "show-command":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "settings: show-command requires a command key")
				return 2
			}
			settings.ShowCommand(args[1])
			if err := state.SaveSettings(settingsPath, settings); err != nil {
				fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
				return 1
			}
			fmt.Printf("hidden command removed: %s\n", args[1])
			return 0
		}
	}

	if hasFlag(args, "--json") {
		return printJSON(settings.Summary())
	}

	summary := settings.Summary()
	fmt.Printf("language: %s\n", fallback(summary.Language, "<unset>"))
	fmt.Printf("command_prefix: %s\n", fallback(summary.CommandPrefix, "<unset>"))
	fmt.Printf("hidden_commands: %d\n", len(summary.HiddenCommands))
	fmt.Printf("selection_fill: %t\n", summary.SelectionHighlightFill)
	fmt.Printf("selection_preset: %s\n", fallback(summary.SelectionHighlightPreset, "<unset>"))
	fmt.Printf("selection_color: %s\n", fallback(summary.SelectionHighlightColor, "<unset>"))
	fmt.Printf("list_primary_color: %s\n", fallback(summary.ListPrimaryColor, "<unset>"))
	fmt.Printf("list_secondary_color: %s\n", fallback(summary.ListSecondaryColor, "<unset>"))
	fmt.Printf("reply_text_color: %s\n", fallback(summary.ReplyTextColor, "<unset>"))
	fmt.Printf("command_text_color: %s\n", fallback(summary.CommandTextColor, "<unset>"))
	fmt.Printf("reasoning_text_color: %s\n", fallback(summary.ReasoningTextColor, "<unset>"))
	fmt.Printf("command_output_text_color: %s\n", fallback(summary.CommandOutputTextColor, "<unset>"))
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

	if hasFlag(args, "--json") {
		return printJSON(sessions)
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

func hasFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func printJSON(value any) int {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode json: %v\n", err)
		return 1
	}
	fmt.Println(string(data))
	return 0
}

func parseRunOptions(args []string) (taskrun.Options, error) {
	var options taskrun.Options
	var promptParts []string

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch arg {
		case "--json":
			options.JSON = true
		case "--no-stream":
			options.DisableStreaming = true
		case "--stream":
			options.DisableStreaming = false
		case "--model":
			value, next, err := takeFlagValue(args, index, arg)
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Model = value
			index = next
		case "--profile":
			value, next, err := takeFlagValue(args, index, arg)
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, arg)
			if err != nil {
				return taskrun.Options{}, err
			}
			options.Provider = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, arg)
			if err != nil {
				return taskrun.Options{}, err
			}
			options.ReasoningEffort = value
			index = next
		case "--system":
			value, next, err := takeFlagValue(args, index, arg)
			if err != nil {
				return taskrun.Options{}, err
			}
			options.SystemPrompt = value
			index = next
		default:
			if strings.HasPrefix(arg, "--") {
				return taskrun.Options{}, fmt.Errorf("unknown flag %q", arg)
			}
			promptParts = append(promptParts, arg)
		}
	}

	options.Prompt = strings.TrimSpace(strings.Join(promptParts, " "))
	if options.Prompt == "" {
		return taskrun.Options{}, fmt.Errorf("prompt is required")
	}
	return options, nil
}

func takeFlagValue(args []string, index int, flag string) (string, int, error) {
	next := index + 1
	if next >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	return args[next], next, nil
}

func contextBackground() context.Context {
	return context.Background()
}

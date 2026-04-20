package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/provider/responsesapi"
	"github.com/Perdonus/lavilas-code/internal/state"
)

func runLogin(args []string) int {
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "login: failed to read config: %v\n", err)
		return 1
	}

	input, err := parseLoginArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "login: %v\n", err)
		return 2
	}

	providerName := fallback(input.ProviderName, "openai")
	profileName := fallback(input.ProfileName, providerName)
	displayName := fallback(input.DisplayName, defaultDisplayName(providerName))
	wireAPI := fallback(input.WireAPI, "responses")
	baseURL := strings.TrimSpace(input.BaseURL)
	if baseURL == "" {
		if wireAPI == "chat_completions" {
			baseURL = openai.DefaultBaseURL + "/v1"
		} else {
			baseURL = responsesapi.DefaultBaseURL + "/v1"
		}
	}

	providerConfig, ok := config.Provider(providerName)
	if !ok {
		providerConfig = state.ProviderConfig{Name: providerName}
	}
	providerConfig.Name = providerName
	providerConfig.BaseURL = baseURL
	providerConfig.WireAPI = wireAPI
	if input.EnvKey != "" {
		providerConfig.APIKeyEnv = input.EnvKey
	}
	if providerConfig.Fields == nil {
		providerConfig.Fields = make(state.ConfigFields)
	}
	providerConfig.Fields.Set("name", state.StringConfigValue(displayName))

	token := strings.TrimSpace(input.Token)
	if input.TokenFromStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "login: failed to read token from stdin: %v\n", err)
			return 1
		}
		token = strings.TrimSpace(string(data))
	}
	if token != "" {
		providerConfig.Fields.Set("experimental_bearer_token", state.StringConfigValue(token))
	}

	profile, ok := config.Profile(profileName)
	if !ok {
		profile = state.ProfileConfig{Name: profileName}
	}
	profile.Name = profileName
	profile.Provider = providerName
	if input.Model != "" {
		profile.Model = input.Model
	}
	if input.Reasoning != "" {
		profile.ReasoningEffort = input.Reasoning
	}

	config.UpsertProvider(providerConfig)
	config.UpsertProfile(profile)
	if input.Activate {
		config.SetActiveProfile(profileName)
		config.SetModelProvider(providerName)
	}
	if err := state.SaveConfig(configPath, config); err != nil {
		fmt.Fprintf(os.Stderr, "login: failed to save config: %v\n", err)
		return 1
	}

	fmt.Printf("saved provider: %s\n", providerName)
	fmt.Printf("saved profile: %s\n", profileName)
	if input.Activate {
		fmt.Printf("active profile: %s\n", profileName)
	}
	return 0
}

func runLogout(args []string) int {
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logout: failed to read config: %v\n", err)
		return 1
	}

	input, err := parseLogoutArgs(config, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logout: %v\n", err)
		return 2
	}

	if input.ProfileName != "" && !input.KeepProfile {
		if profile, ok := config.Profile(input.ProfileName); ok {
			if err := modelcatalog.DeleteProfileSnapshot(profile, apphome.CodexHome()); err != nil {
				fmt.Fprintf(os.Stderr, "logout: failed to remove sidecar: %v\n", err)
				return 1
			}
		}
		config.DeleteProfile(input.ProfileName)
	}

	if input.ProviderName != "" {
		if input.KeepProvider {
			if providerConfig, ok := config.Provider(input.ProviderName); ok {
				providerConfig.Fields.Delete("experimental_bearer_token")
				config.UpsertProvider(providerConfig)
			}
		} else {
			config.DeleteProvider(input.ProviderName)
		}
	}

	if err := state.SaveConfig(configPath, config); err != nil {
		fmt.Fprintf(os.Stderr, "logout: failed to save config: %v\n", err)
		return 1
	}

	fmt.Printf("cleared provider: %s\n", fallback(input.ProviderName, "<unset>"))
	if input.ProfileName != "" && !input.KeepProfile {
		fmt.Printf("deleted profile: %s\n", input.ProfileName)
	}
	return 0
}

func runStatus(args []string) int {
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: failed to read config: %v\n", err)
		return 1
	}
	settings, err := loadSettingsOptional(apphome.SettingsPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: failed to read settings: %v\n", err)
		return 1
	}

	providerConfig, _ := config.EffectiveProvider()
	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: failed to resolve model catalog: %v\n", err)
		return 1
	}
	model, matched := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, config.EffectiveModel())
	payload := map[string]any{
		"model":              fallback(config.EffectiveModel(), ""),
		"model_display_name": fallback(model.DisplayName, ""),
		"model_matched":      matched,
		"reasoning":          fallback(config.EffectiveReasoningEffort(), ""),
		"active_profile":     fallback(config.ActiveProfileName(), ""),
		"active_provider":    fallback(config.EffectiveProviderName(), ""),
		"provider_name":      fallback(providerConfig.DisplayName(), ""),
		"provider_base_url":  fallback(providerConfig.BaseURL, ""),
		"provider_wire_api":  fallback(providerConfig.WireAPI, "chat_completions"),
		"provider_has_token": strings.TrimSpace(providerConfig.BearerToken()) != "",
		"provider_id":        fallback(ctx.ProviderID, ""),
		"catalog_path":       fallback(ctx.SidecarPath, ""),
		"catalog_found":      ctx.SidecarFound,
		"catalog_models":     len(ctx.Snapshot.Models),
		"language":           fallback(settings.Language, ""),
		"command_prefix":     fallback(settings.CommandPrefix, ""),
		"config_path":        configPath,
		"settings_path":      apphome.SettingsPath(),
		"sessions_dir":       apphome.SessionsDir(),
	}

	if hasFlag(args, "--json") {
		return printJSON(payload)
	}

	fmt.Printf("model: %s\n", fallback(payload["model"].(string), "<unset>"))
	fmt.Printf("model_display_name: %s\n", fallback(payload["model_display_name"].(string), "<unset>"))
	fmt.Printf("model_matched: %t\n", payload["model_matched"].(bool))
	fmt.Printf("reasoning: %s\n", fallback(payload["reasoning"].(string), "<unset>"))
	fmt.Printf("active_profile: %s\n", fallback(payload["active_profile"].(string), "<unset>"))
	fmt.Printf("active_provider: %s\n", fallback(payload["active_provider"].(string), "<unset>"))
	fmt.Printf("provider_name: %s\n", fallback(payload["provider_name"].(string), "<unset>"))
	fmt.Printf("provider_id: %s\n", fallback(payload["provider_id"].(string), "<unset>"))
	fmt.Printf("provider_base_url: %s\n", fallback(payload["provider_base_url"].(string), "<unset>"))
	fmt.Printf("provider_wire_api: %s\n", fallback(payload["provider_wire_api"].(string), "<unset>"))
	fmt.Printf("provider_has_token: %t\n", payload["provider_has_token"].(bool))
	fmt.Printf("catalog_path: %s\n", fallback(payload["catalog_path"].(string), "<unset>"))
	fmt.Printf("catalog_found: %t\n", payload["catalog_found"].(bool))
	fmt.Printf("catalog_models: %d\n", payload["catalog_models"].(int))
	fmt.Printf("language: %s\n", fallback(payload["language"].(string), "<unset>"))
	fmt.Printf("command_prefix: %s\n", fallback(payload["command_prefix"].(string), "<unset>"))
	fmt.Printf("config_path: %s\n", payload["config_path"].(string))
	fmt.Printf("settings_path: %s\n", payload["settings_path"].(string))
	fmt.Printf("sessions_dir: %s\n", payload["sessions_dir"].(string))
	return 0
}

type loginInput struct {
	ProviderName   string
	ProfileName    string
	DisplayName    string
	BaseURL        string
	WireAPI        string
	EnvKey         string
	Model          string
	Reasoning      string
	Token          string
	TokenFromStdin bool
	Activate       bool
}

func parseLoginArgs(args []string) (loginInput, error) {
	input := loginInput{Activate: true}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return loginInput{}, err
			}
			input.ProviderName = value
			index = next
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return loginInput{}, err
			}
			input.ProfileName = value
			index = next
		case "--display-name", "--name":
			value, next, err := takeFlagValue(args, index, args[index])
			if err != nil {
				return loginInput{}, err
			}
			input.DisplayName = value
			index = next
		case "--base-url":
			value, next, err := takeFlagValue(args, index, "--base-url")
			if err != nil {
				return loginInput{}, err
			}
			input.BaseURL = value
			index = next
		case "--wire-api":
			value, next, err := takeFlagValue(args, index, "--wire-api")
			if err != nil {
				return loginInput{}, err
			}
			switch strings.TrimSpace(value) {
			case "", "chat_completions", "responses":
				input.WireAPI = strings.TrimSpace(value)
			default:
				return loginInput{}, fmt.Errorf("unsupported wire api %q", value)
			}
			index = next
		case "--env-key":
			value, next, err := takeFlagValue(args, index, "--env-key")
			if err != nil {
				return loginInput{}, err
			}
			input.EnvKey = value
			index = next
		case "--model":
			value, next, err := takeFlagValue(args, index, "--model")
			if err != nil {
				return loginInput{}, err
			}
			input.Model = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				return loginInput{}, err
			}
			input.Reasoning = value
			index = next
		case "--token":
			value, next, err := takeFlagValue(args, index, "--token")
			if err != nil {
				return loginInput{}, err
			}
			input.Token = value
			index = next
		case "--token-stdin":
			input.TokenFromStdin = true
		case "--no-activate":
			input.Activate = false
		case "--activate":
			input.Activate = true
		default:
			return loginInput{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}
	return input, nil
}

type logoutInput struct {
	ProviderName string
	ProfileName  string
	KeepProvider bool
	KeepProfile  bool
}

func parseLogoutArgs(config state.Config, args []string) (logoutInput, error) {
	input := logoutInput{
		ProfileName:  config.ActiveProfileName(),
		ProviderName: config.EffectiveProviderName(),
	}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return logoutInput{}, err
			}
			input.ProviderName = value
			index = next
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return logoutInput{}, err
			}
			input.ProfileName = value
			index = next
		case "--keep-provider":
			input.KeepProvider = true
		case "--keep-profile":
			input.KeepProfile = true
		default:
			return logoutInput{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}
	return input, nil
}

func defaultDisplayName(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "openai":
		return "OpenAI"
	case "openrouter":
		return "OpenRouter"
	case "anthropic":
		return "Anthropic"
	case "mistral":
		return "Mistral"
	case "gemini":
		return "Gemini"
	default:
		return providerName
	}
}

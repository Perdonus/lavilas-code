package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/doctor"
	"github.com/Perdonus/lavilas-code/internal/modelcatalog"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/taskrun"
	"github.com/Perdonus/lavilas-code/internal/tooling"
	"github.com/Perdonus/lavilas-code/internal/tui"
)

func runDoctor(args []string) int {
	return doctor.Run(hasFlag(args, "--json"))
}

func shouldOpenInteractiveConfig(args []string) bool {
	return isInteractiveTerminal() && len(args) == 0 && !hasFlag(args, "--json")
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

	if entry, err := persistNewSession(result); err == nil {
		result.SessionPath = entry.Path
	} else {
		fmt.Fprintf(os.Stderr, "run: warning: failed to save session: %v\n", err)
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
	if shouldOpenInteractiveConfig(args) {
		return tui.Run(tui.Options{Startup: tui.StartupOptions{Mode: tui.StartupModeModel}})
	}
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}
	settings, err := loadSettingsOptional(apphome.SettingsPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read settings: %v\n", err)
		return 1
	}

	action := ""
	if len(args) > 0 {
		action = normalizeModelSubcommand(args[0])
	}
	switch {
	case action == "set":
		input, err := parseModelSetArgs(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "model: %v\n", err)
			return 2
		}
		ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), input.Profile, input.Provider)
		if err != nil {
			fmt.Fprintf(os.Stderr, "model: %v\n", err)
			return 1
		}
		providerName := firstNonEmpty(input.Provider, ctx.ProviderName)
		model, matched := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, input.Model)
		if strings.TrimSpace(model.Slug) == "" {
			fmt.Fprintf(os.Stderr, "model: unable to resolve %q\n", input.Model)
			return 1
		}
		reasoning := strings.TrimSpace(input.Reasoning)
		targetProfile := applyModelSelection(&config, input.Profile, providerName, model, reasoning)
		if err := state.SaveConfig(configPath, config); err != nil {
			fmt.Fprintf(os.Stderr, "model: failed to save config: %v\n", err)
			return 1
		}

		payload := buildModelPayload(config, settings, modelcatalog.RuntimeContext{
			ProfileName:  firstNonEmpty(targetProfile, config.ActiveProfileName()),
			ProviderName: firstNonEmpty(providerName, config.EffectiveProviderName()),
			ProviderID:   ctx.ProviderID,
			SidecarPath:  ctx.SidecarPath,
			SidecarFound: ctx.SidecarFound,
			Snapshot:     ctx.Snapshot,
			Catalog:      ctx.Catalog,
		})
		payload["updated"] = true
		payload["matched_catalog"] = matched
		payload["target_profile"] = targetProfile
		if hasFlag(args, "--json") {
			return printJSON(payload)
		}
		fmt.Printf("model updated: %s\n", model.Slug)
		if targetProfile != "" {
			fmt.Printf("target_profile: %s\n", targetProfile)
		}
		fmt.Printf("provider: %s\n", fallback(providerName, "<unset>"))
		fmt.Printf("matched_catalog: %t\n", matched)
		return 0
	case action == "list":
		options, err := parseRuntimeTargetArgs(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "model: %v\n", err)
			return 2
		}
		ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), options.Profile, options.Provider)
		if err != nil {
			fmt.Fprintf(os.Stderr, "model: %v\n", err)
			return 1
		}
		payload := buildModelPayload(config, settings, ctx)
		if options.JSON {
			return printJSON(payload)
		}
		printModelPayload(payload)
		return 0
	case action == "preset":
		return runModelPreset(config, settings, configPath, args[1:])
	}

	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), "", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "model: %v\n", err)
		return 1
	}
	payload := buildModelPayload(config, settings, ctx)
	if hasFlag(args, "--json") {
		return printJSON(payload)
	}
	printModelPayload(payload)
	return 0
}

func runProfiles(args []string) int {
	if shouldOpenInteractiveConfig(args) {
		return tui.Run(tui.Options{Startup: tui.StartupOptions{Mode: tui.StartupModeProfiles}})
	}
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	if len(args) > 0 {
		switch normalizeProfilesSubcommand(args[0]) {
		case "set":
			settings, err := loadSettingsOptional(apphome.SettingsPath())
			if err != nil {
				fmt.Fprintf(os.Stderr, "profiles: failed to read settings: %v\n", err)
				return 1
			}
			profile, activate, err := parseProfileSetArgs(config, settings, args[1:])
			if err != nil {
				fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
				return 2
			}
			config.UpsertProfile(profile)
			if activate {
				config.SetActiveProfile(profile.Name)
			}
			if err := state.SaveConfig(configPath, config); err != nil {
				fmt.Fprintf(os.Stderr, "profiles: failed to save config: %v\n", err)
				return 1
			}
			fmt.Printf("profile updated: %s\n", profile.Name)
			return 0
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
			profile, ok := config.Profile(args[1])
			if !ok {
				fmt.Fprintf(os.Stderr, "profiles: profile %q not found\n", args[1])
				return 1
			}
			if !config.DeleteProfile(args[1]) {
				fmt.Fprintf(os.Stderr, "profiles: profile %q not found\n", args[1])
				return 1
			}
			if err := modelcatalog.DeleteProfileSnapshot(profile, apphome.CodexHome()); err != nil {
				fmt.Fprintf(os.Stderr, "profiles: failed to remove sidecar: %v\n", err)
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
		settings, err := loadSettingsOptional(apphome.SettingsPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "profiles: failed to read settings: %v\n", err)
			return 1
		}
		type profileView struct {
			Name             string                         `json:"name"`
			Model            string                         `json:"model,omitempty"`
			ModelDisplayName string                         `json:"model_display_name,omitempty"`
			Provider         string                         `json:"provider,omitempty"`
			ProviderID       string                         `json:"provider_id,omitempty"`
			Reasoning        string                         `json:"reasoning,omitempty"`
			Active           bool                           `json:"active"`
			Sidecar          modelcatalog.SidecarSummary    `json:"sidecar"`
			Presets          []modelcatalog.EffectivePreset `json:"presets,omitempty"`
		}

		profiles := make([]profileView, 0, len(config.Profiles))
		for _, profile := range config.Profiles {
			ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), profile.Name, profile.EffectiveProviderName())
			if err != nil {
				fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
				return 1
			}
			resolvedModel, _ := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, profile.Model)
			sidecar, err := modelcatalog.SidecarSummaryForProfile(profile, apphome.CodexHome())
			if err != nil {
				fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
				return 1
			}
			profiles = append(profiles, profileView{
				Name:             profile.Name,
				Model:            profile.Model,
				ModelDisplayName: resolvedModel.DisplayName,
				Provider:         profile.EffectiveProviderName(),
				ProviderID:       ctx.ProviderID,
				Reasoning:        profile.ReasoningEffort,
				Active:           profile.Name == config.ActiveProfileName(),
				Sidecar:          sidecar,
				Presets:          modelcatalog.EffectivePresetChoices(ctx.Catalog, settings, ctx.ProviderID),
			})
		}

		payload := map[string]any{
			"active_profile": config.ActiveProfileName(),
			"profiles":       profiles,
		}
		return printJSON(payload)
	}

	if len(config.Profiles) == 0 {
		fmt.Println("No stored profiles found in config.")
		return 0
	}

	fmt.Println("Profiles:")
	for _, profile := range config.Profiles {
		ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), profile.Name, profile.EffectiveProviderName())
		if err != nil {
			fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
			return 1
		}
		sidecar, err := modelcatalog.SidecarSummaryForProfile(profile, apphome.CodexHome())
		if err != nil {
			fmt.Fprintf(os.Stderr, "profiles: %v\n", err)
			return 1
		}
		resolvedModel, _ := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, profile.Model)
		suffix := ""
		if profile.Name == config.ActiveProfileName() {
			suffix = " (active)"
		}
		fmt.Printf(
			"  - %s%s  model=%s provider=%s reasoning=%s sidecar=%s models=%d\n",
			profile.Name,
			suffix,
			fallback(firstNonEmpty(resolvedModel.DisplayName, profile.Model), "<unset>"),
			fallback(profile.EffectiveProviderName(), "<unset>"),
			fallback(profile.ReasoningEffort, "<unset>"),
			fallback(sidecar.Path, "<unset>"),
			sidecar.ModelCount,
		)
	}
	return 0
}

func runProviders(args []string) int {
	if shouldOpenInteractiveConfig(args) {
		return tui.Run(tui.Options{Startup: tui.StartupOptions{Mode: tui.StartupModeProviders}})
	}
	configPath := apphome.ConfigPath()
	config, err := loadConfigOptional(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read config: %v\n", err)
		return 1
	}

	if len(args) > 0 {
		switch normalizeProvidersSubcommand(args[0]) {
		case "set":
			providerConfig, err := parseProviderSetArgs(config, args[1:])
			if err != nil {
				fmt.Fprintf(os.Stderr, "providers: %v\n", err)
				return 2
			}
			config.UpsertProvider(providerConfig)
			if err := state.SaveConfig(configPath, config); err != nil {
				fmt.Fprintf(os.Stderr, "providers: failed to save config: %v\n", err)
				return 1
			}
			fmt.Printf("provider updated: %s\n", providerConfig.Name)
			return 0
		case "delete":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "providers: delete requires a provider name")
				return 2
			}
			if !config.DeleteProvider(args[1]) {
				fmt.Fprintf(os.Stderr, "providers: provider %q not found\n", args[1])
				return 1
			}
			if err := state.SaveConfig(configPath, config); err != nil {
				fmt.Fprintf(os.Stderr, "providers: failed to save config: %v\n", err)
				return 1
			}
			fmt.Printf("deleted provider: %s\n", args[1])
			return 0
		}
	}

	if hasFlag(args, "--json") {
		type providerView struct {
			Name        string                        `json:"name"`
			DisplayName string                        `json:"display_name,omitempty"`
			ProviderID  string                        `json:"provider_id,omitempty"`
			BaseURL     string                        `json:"base_url,omitempty"`
			WireAPI     string                        `json:"wire_api,omitempty"`
			APIKeyEnv   string                        `json:"api_key_env,omitempty"`
			HasToken    bool                          `json:"has_token"`
			ProfileRefs []string                      `json:"profiles,omitempty"`
			Sidecars    []modelcatalog.SidecarSummary `json:"sidecars,omitempty"`
		}
		providers := make([]providerView, 0, len(config.ModelProviders))
		for _, current := range config.ModelProviders {
			profiles := config.ProfilesForProvider(current.Name)
			sidecars := make([]modelcatalog.SidecarSummary, 0, len(profiles))
			profileRefs := make([]string, 0, len(profiles))
			for _, profile := range profiles {
				profileRefs = append(profileRefs, profile.Name)
				sidecar, err := modelcatalog.SidecarSummaryForProfile(profile, apphome.CodexHome())
				if err != nil {
					fmt.Fprintf(os.Stderr, "providers: %v\n", err)
					return 1
				}
				sidecars = append(sidecars, sidecar)
			}
			providers = append(providers, providerView{
				Name:        current.Name,
				DisplayName: current.DisplayName(),
				ProviderID:  modelcatalog.NormalizeProviderID(firstNonEmpty(current.Name, current.DisplayName())),
				BaseURL:     current.BaseURL,
				WireAPI:     current.WireAPI,
				APIKeyEnv:   current.APIKeyEnv,
				HasToken:    strings.TrimSpace(current.BearerToken()) != "",
				ProfileRefs: profileRefs,
				Sidecars:    sidecars,
			})
		}
		return printJSON(map[string]any{"providers": providers})
	}

	if len(config.ModelProviders) == 0 {
		fmt.Println("No stored providers found in config.")
		return 0
	}

	fmt.Println("Providers:")
	for _, current := range config.ModelProviders {
		profiles := config.ProfilesForProvider(current.Name)
		sidecarCount := 0
		modelCount := 0
		for _, profile := range profiles {
			sidecar, err := modelcatalog.SidecarSummaryForProfile(profile, apphome.CodexHome())
			if err != nil {
				fmt.Fprintf(os.Stderr, "providers: %v\n", err)
				return 1
			}
			if sidecar.Found {
				sidecarCount++
				modelCount += sidecar.ModelCount
			}
		}
		tokenState := "no-token"
		if strings.TrimSpace(current.BearerToken()) != "" {
			tokenState = "token"
		}
		fmt.Printf(
			"  - %s  id=%s name=%s base_url=%s wire_api=%s profiles=%d sidecars=%d models=%d env_key=%s %s\n",
			current.Name,
			fallback(modelcatalog.NormalizeProviderID(firstNonEmpty(current.Name, current.DisplayName())), "<unset>"),
			fallback(current.DisplayName(), "<unset>"),
			fallback(current.BaseURL, "<unset>"),
			fallback(current.WireAPI, "chat_completions"),
			len(profiles),
			sidecarCount,
			modelCount,
			fallback(current.APIKeyEnv, "<unset>"),
			tokenState,
		)
	}
	return 0
}

func runSettings(args []string) int {
	if shouldOpenInteractiveConfig(args) {
		return tui.Run(tui.Options{Startup: tui.StartupOptions{Mode: tui.StartupModeSettings}})
	}
	settingsPath := apphome.SettingsPath()
	settings, err := loadSettingsOptional(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read settings: %v\n", err)
		return 1
	}

	if len(args) > 0 {
		switch normalizeSettingsSubcommand(args[0]) {
		case "presets":
			return runSettingsPresets(settings, settingsPath, args[1:])
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
	fmt.Printf("model_presets_enabled: %t\n", summary.ModelPresetsEnabled)
	fmt.Printf("model_preset_providers: %d\n", len(summary.ModelPresetProviders))
	fmt.Printf("model_preset_count: %d\n", summary.ModelPresetCount)
	return 0
}

func runResume(args []string) int {
	input, err := parseResumeArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 2
	}
	if shouldRunInteractiveResume(input) {
		return runResumeTUI(input, false)
	}

	sessionEntry, err := resolveResumeSession(apphome.SessionsDir(), input.Selector, input.Last)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No sessions found.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}

	meta, messages, err := state.LoadSession(sessionEntry.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: failed to load session: %v\n", err)
		return 1
	}

	if strings.TrimSpace(input.Prompt) == "" {
		if input.JSONOutput {
			return printJSON(map[string]any{
				"session":  sessionEntry,
				"meta":     meta,
				"messages": messages,
			})
		}
		fmt.Printf("session: %s\n", sessionEntry.Path)
		for _, message := range messages {
			text := strings.TrimSpace(message.Text())
			if text == "" && len(message.ToolCalls) == 0 {
				continue
			}
			if text != "" {
				fmt.Printf("[%s] %s\n", message.Role, text)
				continue
			}
			for _, call := range message.ToolCalls {
				fmt.Printf("[%s] tool_call %s %s %s\n", message.Role, call.ID, call.Function.Name, call.Function.ArgumentsString())
			}
		}
		return 0
	}

	result, err := taskrun.Run(contextBackground(), taskrun.Options{
		Prompt:          input.Prompt,
		Model:           meta.Model,
		Profile:         meta.Profile,
		Provider:        meta.Provider,
		ReasoningEffort: meta.Reasoning,
		History:         messages,
		JSON:            input.JSONOutput,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}

	if err := appendSessionTurn(sessionEntry.Path, result); err != nil {
		fmt.Fprintf(os.Stderr, "resume: warning: failed to append session: %v\n", err)
	}
	result.SessionPath = sessionEntry.Path

	if input.JSONOutput {
		return printJSON(result)
	}
	if err := taskrun.Print(result); err != nil {
		fmt.Fprintf(os.Stderr, "resume: %v\n", err)
		return 1
	}
	return 0
}

func runFork(args []string) int {
	input, err := parseResumeArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork: %v\n", err)
		return 2
	}
	if shouldRunInteractiveResume(input) {
		return runResumeTUI(input, true)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		fmt.Fprintln(os.Stderr, "fork: prompt is required")
		return 2
	}

	sessionEntry, err := resolveResumeSession(apphome.SessionsDir(), input.Selector, input.Last)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Println("No sessions found.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "fork: %v\n", err)
		return 1
	}

	meta, messages, err := state.LoadSession(sessionEntry.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork: failed to load session: %v\n", err)
		return 1
	}

	result, err := taskrun.Run(contextBackground(), taskrun.Options{
		Prompt:          input.Prompt,
		Model:           meta.Model,
		Profile:         meta.Profile,
		Provider:        meta.Provider,
		ReasoningEffort: meta.Reasoning,
		History:         messages,
		JSON:            input.JSONOutput,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork: %v\n", err)
		return 1
	}

	entry, err := persistNewSession(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fork: warning: failed to save forked session: %v\n", err)
	} else {
		result.SessionPath = entry.Path
	}

	if input.JSONOutput {
		return printJSON(result)
	}
	if err := taskrun.Print(result); err != nil {
		fmt.Fprintf(os.Stderr, "fork: %v\n", err)
		return 1
	}
	return 0
}

func persistNewSession(result taskrun.Result) (state.SessionEntry, error) {
	return state.CreateSession(apphome.SessionsDir(), sessionMetaFromResult(result), result.FullHistory())
}

func appendSessionTurn(path string, result taskrun.Result) error {
	history := result.FullHistory()
	if len(history) > 0 {
		return state.AppendSessionHistory(path, sessionMetaFromResult(result), history)
	}

	messages := clonePersistedMessages(result.RequestMessages)
	if hasPersistableMessage(result.AssistantMessage) {
		messages = append(messages, result.AssistantMessage)
	}
	return state.AppendSession(path, messages...)
}

func sessionMetaFromResult(result taskrun.Result) state.SessionMeta {
	return state.SessionMeta{
		Model:     result.Model,
		Provider:  result.ProviderName,
		Profile:   result.Profile,
		Reasoning: result.Reasoning,
		CWD:       result.CWD,
	}
}

func clonePersistedMessages(messages []runtime.Message) []runtime.Message {
	if len(messages) == 0 {
		return nil
	}
	result := make([]runtime.Message, len(messages))
	copy(result, messages)
	return result
}

func hasPersistableMessage(message runtime.Message) bool {
	return strings.TrimSpace(message.Text()) != "" || len(message.ToolCalls) > 0 || strings.TrimSpace(message.Refusal) != ""
}

type resumeInput struct {
	Selector   string
	Prompt     string
	JSONOutput bool
	Last       bool
	ShowAll    bool
}

func parseResumeArgs(args []string) (resumeInput, error) {
	var input resumeInput
	var promptParts []string

	for _, arg := range args {
		switch arg {
		case "--json":
			input.JSONOutput = true
		case "--last":
			input.Last = true
		case "--all":
			input.ShowAll = true
		default:
			if strings.HasPrefix(arg, "--") {
				return resumeInput{}, fmt.Errorf("unknown flag %q", arg)
			}
			if input.Selector == "" && looksLikeSessionTarget(arg) {
				input.Selector = arg
				continue
			}
			promptParts = append(promptParts, arg)
		}
	}

	input.Prompt = strings.TrimSpace(strings.Join(promptParts, " "))
	return input, nil
}

func looksLikeSessionTarget(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.HasSuffix(value, ".jsonl") {
		return true
	}
	if strings.ContainsRune(value, os.PathSeparator) {
		return true
	}
	return false
}

func resolveResumeSession(root string, selector string, last bool) (state.SessionEntry, error) {
	if strings.TrimSpace(selector) != "" {
		candidate := selector
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return state.SessionEntry{
				ID:      strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate)),
				Name:    filepath.Base(candidate),
				Path:    candidate,
				RelPath: selector,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			}, nil
		}
		return state.SessionEntry{}, os.ErrNotExist
	}

	_ = last
	sessions, err := state.LoadSessions(root, 1)
	if err != nil {
		return state.SessionEntry{}, err
	}
	if len(sessions) == 0 {
		return state.SessionEntry{}, os.ErrNotExist
	}
	return sessions[0], nil
}

func shouldRunInteractiveResume(input resumeInput) bool {
	return isInteractiveTerminal() && !input.JSONOutput && strings.TrimSpace(input.Prompt) == ""
}

func runResumeTUI(input resumeInput, fork bool) int {
	startup := tui.StartupOptions{ShowAll: input.ShowAll}
	switch {
	case strings.TrimSpace(input.Selector) != "":
		entry, err := resolveResumeSession(apphome.SessionsDir(), input.Selector, input.Last)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Println("No sessions found.")
				return 0
			}
			label := "resume"
			if fork {
				label = "fork"
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
			return 1
		}
		startup.SessionPath = entry.Path
		if fork {
			startup.Mode = tui.StartupModeForkPath
		} else {
			startup.Mode = tui.StartupModeResumePath
		}
	case input.Last:
		if fork {
			startup.Mode = tui.StartupModeForkLatest
		} else {
			startup.Mode = tui.StartupModeResumeLatest
		}
	default:
		if fork {
			startup.Mode = tui.StartupModeForkPicker
		} else {
			startup.Mode = tui.StartupModeResumePicker
		}
	}
	return tui.Run(tui.Options{Startup: startup})
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
		if next, handled, err := consumeToolPolicyFlag(&options, args, index); handled {
			if err != nil {
				return taskrun.Options{}, err
			}
			index = next
			continue
		}
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

func consumeToolPolicyFlag(options *taskrun.Options, args []string, index int) (int, bool, error) {
	switch args[index] {
	case "--tool-approval":
		value, next, err := takeFlagValue(args, index, "--tool-approval")
		if err != nil {
			return index, true, err
		}
		mode := tooling.ToolApprovalMode(strings.TrimSpace(strings.ToLower(value)))
		switch mode {
		case tooling.ToolApprovalModeAuto, tooling.ToolApprovalModeRequire, tooling.ToolApprovalModeDeny:
			options.ToolPolicy.ApprovalMode = mode
			return next, true, nil
		default:
			return index, true, fmt.Errorf("--tool-approval must be one of auto, require, deny")
		}
	case "--allow-tool":
		value, next, err := takeFlagValue(args, index, "--allow-tool")
		if err != nil {
			return index, true, err
		}
		options.ToolPolicy.AllowedTools = append(options.ToolPolicy.AllowedTools, value)
		return next, true, nil
	case "--deny-tool":
		value, next, err := takeFlagValue(args, index, "--deny-tool")
		if err != nil {
			return index, true, err
		}
		options.ToolPolicy.BlockedTools = append(options.ToolPolicy.BlockedTools, value)
		return next, true, nil
	case "--block-mutating-tools":
		options.ToolPolicy.BlockMutatingTools = true
		return index, true, nil
	case "--block-shell-tools":
		options.ToolPolicy.BlockShellCommands = true
		return index, true, nil
	case "--no-parallel-tools":
		options.ToolPolicy.Planning.AllowParallel = false
		return index, true, nil
	case "--tool-parallelism":
		value, next, err := takeFlagValue(args, index, "--tool-parallelism")
		if err != nil {
			return index, true, err
		}
		parallelism, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || parallelism < 1 {
			return index, true, fmt.Errorf("--tool-parallelism requires a positive integer")
		}
		options.ToolPolicy.Planning.MaxParallelCalls = parallelism
		return next, true, nil
	default:
		return index, false, nil
	}
}

func normalizeModelSubcommand(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "set", "установить":
		return "set"
	case "list", "список":
		return "list"
	case "preset", "presets", "пресет", "пресеты":
		return "preset"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeProfilesSubcommand(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "set", "установить":
		return "set"
	case "activate", "active", "активировать":
		return "activate"
	case "delete", "remove", "удалить":
		return "delete"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeProvidersSubcommand(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "set", "установить":
		return "set"
	case "delete", "remove", "удалить":
		return "delete"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeSettingsSubcommand(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "presets", "preset", "пресеты", "пресет":
		return "presets"
	case "language", "lang", "язык":
		return "language"
	case "prefix", "префикс":
		return "prefix"
	case "hide-command", "скрыть-команду":
		return "hide-command"
	case "show-command", "показать-команду":
		return "show-command"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeSettingsPresetAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enable", "включить":
		return "enable"
	case "disable", "выключить":
		return "disable"
	case "set", "установить":
		return "set"
	case "delete", "remove", "удалить":
		return "delete"
	default:
		return strings.TrimSpace(value)
	}
}

func takeFlagValue(args []string, index int, flag string) (string, int, error) {
	next := index + 1
	if next >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", flag)
	}
	return args[next], next, nil
}

func parseProfileSetArgs(config state.Config, settings state.Settings, args []string) (state.ProfileConfig, bool, error) {
	if len(args) == 0 {
		return state.ProfileConfig{}, false, fmt.Errorf("set requires a profile name")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return state.ProfileConfig{}, false, fmt.Errorf("profile name is required")
	}

	profile, ok := config.Profile(name)
	if !ok {
		profile = state.ProfileConfig{Name: name}
	}

	activate := false
	presetKey := ""
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--model":
			value, next, err := takeFlagValue(args, index, "--model")
			if err != nil {
				return state.ProfileConfig{}, false, err
			}
			profile.SetModel(value)
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return state.ProfileConfig{}, false, err
			}
			profile.SetProvider(value)
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				return state.ProfileConfig{}, false, err
			}
			profile.SetReasoningEffort(value)
			index = next
		case "--catalog-json", "--sidecar":
			value, next, err := takeFlagValue(args, index, args[index])
			if err != nil {
				return state.ProfileConfig{}, false, err
			}
			profile.SetCatalogPath(value)
			index = next
		case "--preset":
			value, next, err := takeFlagValue(args, index, "--preset")
			if err != nil {
				return state.ProfileConfig{}, false, err
			}
			presetKey = value
			index = next
		case "--activate":
			activate = true
		default:
			return state.ProfileConfig{}, false, fmt.Errorf("unknown flag %q", args[index])
		}
	}

	if presetKey != "" {
		tempConfig := config.Clone()
		tempConfig.UpsertProfile(profile)
		if activate || strings.TrimSpace(tempConfig.ActiveProfileName()) == "" {
			tempConfig.SetActiveProfile(profile.Name)
		}
		ctx, err := modelcatalog.ResolveRuntimeContext(tempConfig, apphome.CodexHome(), profile.Name, profile.EffectiveProviderName())
		if err != nil {
			return state.ProfileConfig{}, false, err
		}
		preset, ok := modelcatalog.EffectivePresetChoice(ctx.Catalog, settings, ctx.ProviderID, presetKey)
		if !ok {
			return state.ProfileConfig{}, false, fmt.Errorf("preset %q not available for provider %q", presetKey, ctx.ProviderID)
		}
		profile.SetModel(preset.Model.Slug)
		if strings.TrimSpace(profile.ReasoningEffort) == "" && strings.TrimSpace(preset.Reasoning) != "" {
			profile.SetReasoningEffort(preset.Reasoning)
		}
	}

	return profile, activate, nil
}

func parseProviderSetArgs(config state.Config, args []string) (state.ProviderConfig, error) {
	if len(args) == 0 {
		return state.ProviderConfig{}, fmt.Errorf("set requires a provider name")
	}

	name := strings.TrimSpace(args[0])
	if name == "" {
		return state.ProviderConfig{}, fmt.Errorf("provider name is required")
	}

	providerConfig, ok := config.Provider(name)
	if !ok {
		providerConfig = state.ProviderConfig{Name: name}
	}

	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--type":
			value, next, err := takeFlagValue(args, index, "--type")
			if err != nil {
				return state.ProviderConfig{}, err
			}
			providerConfig.Type = value
			index = next
		case "--display-name", "--name":
			value, next, err := takeFlagValue(args, index, args[index])
			if err != nil {
				return state.ProviderConfig{}, err
			}
			providerConfig.Fields.Set("name", state.StringConfigValue(value))
			index = next
		case "--base-url":
			value, next, err := takeFlagValue(args, index, "--base-url")
			if err != nil {
				return state.ProviderConfig{}, err
			}
			providerConfig.BaseURL = value
			index = next
		case "--env-key":
			value, next, err := takeFlagValue(args, index, "--env-key")
			if err != nil {
				return state.ProviderConfig{}, err
			}
			providerConfig.APIKeyEnv = value
			index = next
		case "--wire-api":
			value, next, err := takeFlagValue(args, index, "--wire-api")
			if err != nil {
				return state.ProviderConfig{}, err
			}
			value = strings.TrimSpace(value)
			switch value {
			case "", "chat_completions", "responses":
				providerConfig.WireAPI = value
			default:
				return state.ProviderConfig{}, fmt.Errorf("unsupported wire api %q", value)
			}
			index = next
		case "--token":
			value, next, err := takeFlagValue(args, index, "--token")
			if err != nil {
				return state.ProviderConfig{}, err
			}
			providerConfig.Fields.Set("experimental_bearer_token", state.StringConfigValue(value))
			index = next
		default:
			return state.ProviderConfig{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}

	return providerConfig, nil
}

type runtimeTargetOptions struct {
	Profile  string
	Provider string
	JSON     bool
}

type modelSetInput struct {
	Model     string
	Profile   string
	Provider  string
	Reasoning string
	JSON      bool
}

type modelPresetInput struct {
	Action   string
	Key      string
	Profile  string
	Provider string
	JSON     bool
}

type settingsPresetInput struct {
	Action    string
	Provider  string
	PresetKey string
	Profile   string
	Model     string
	Name      string
	Reasoning string
	JSON      bool
}

func parseRuntimeTargetArgs(args []string) (runtimeTargetOptions, error) {
	var options runtimeTargetOptions
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			options.JSON = true
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return runtimeTargetOptions{}, err
			}
			options.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return runtimeTargetOptions{}, err
			}
			options.Provider = value
			index = next
		default:
			return runtimeTargetOptions{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}
	return options, nil
}

func parseModelSetArgs(args []string) (modelSetInput, error) {
	if len(args) == 0 {
		return modelSetInput{}, fmt.Errorf("set requires a model name")
	}
	input := modelSetInput{Model: strings.TrimSpace(args[0])}
	if input.Model == "" {
		return modelSetInput{}, fmt.Errorf("model name is required")
	}
	for index := 1; index < len(args); index++ {
		switch args[index] {
		case "--json":
			input.JSON = true
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return modelSetInput{}, err
			}
			input.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return modelSetInput{}, err
			}
			input.Provider = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				return modelSetInput{}, err
			}
			input.Reasoning = value
			index = next
		default:
			return modelSetInput{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}
	return input, nil
}

func parseModelPresetArgs(args []string) (modelPresetInput, error) {
	input := modelPresetInput{Action: "list"}
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			input.JSON = true
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return modelPresetInput{}, err
			}
			input.Profile = value
			index = next
		case "--provider":
			value, next, err := takeFlagValue(args, index, "--provider")
			if err != nil {
				return modelPresetInput{}, err
			}
			input.Provider = value
			index = next
		case "apply", "применить":
			input.Action = "apply"
			next := index + 1
			if next >= len(args) {
				return modelPresetInput{}, fmt.Errorf("preset key is required")
			}
			input.Key = args[next]
			index = next
		default:
			if strings.HasPrefix(args[index], "--") {
				return modelPresetInput{}, fmt.Errorf("unknown flag %q", args[index])
			}
			input.Action = "apply"
			input.Key = args[index]
		}
	}
	return input, nil
}

func parseSettingsPresetArgs(args []string) (settingsPresetInput, error) {
	input := settingsPresetInput{Action: "list"}
	if len(args) == 0 {
		return input, nil
	}
	switch normalizeSettingsPresetAction(args[0]) {
	case "enable", "disable":
		input.Action = normalizeSettingsPresetAction(args[0])
		args = args[1:]
	case "set":
		input.Action = "set"
		args = args[1:]
		if len(args) < 2 {
			return settingsPresetInput{}, fmt.Errorf("set requires <provider> <preset>")
		}
		input.Provider = args[0]
		input.PresetKey = args[1]
		args = args[2:]
	case "delete":
		input.Action = "delete"
		args = args[1:]
		if len(args) < 2 {
			return settingsPresetInput{}, fmt.Errorf("delete requires <provider> <preset>")
		}
		input.Provider = args[0]
		input.PresetKey = args[1]
		args = args[2:]
	default:
		args = args
	}

	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--json":
			input.JSON = true
		case "--profile":
			value, next, err := takeFlagValue(args, index, "--profile")
			if err != nil {
				return settingsPresetInput{}, err
			}
			input.Profile = value
			index = next
		case "--model":
			value, next, err := takeFlagValue(args, index, "--model")
			if err != nil {
				return settingsPresetInput{}, err
			}
			input.Model = value
			index = next
		case "--name":
			value, next, err := takeFlagValue(args, index, "--name")
			if err != nil {
				return settingsPresetInput{}, err
			}
			input.Name = value
			index = next
		case "--reasoning":
			value, next, err := takeFlagValue(args, index, "--reasoning")
			if err != nil {
				return settingsPresetInput{}, err
			}
			input.Reasoning = value
			index = next
		default:
			return settingsPresetInput{}, fmt.Errorf("unknown flag %q", args[index])
		}
	}

	return input, nil
}

func applyModelSelection(config *state.Config, explicitProfile, providerName string, model modelcatalog.Model, reasoning string) string {
	targetProfile := strings.TrimSpace(explicitProfile)
	if targetProfile == "" {
		targetProfile = strings.TrimSpace(config.ActiveProfileName())
	}

	if targetProfile != "" {
		profile, ok := config.Profile(targetProfile)
		if !ok {
			profile = state.ProfileConfig{Name: targetProfile}
		}
		profile.Name = targetProfile
		profile.SetModel(model.Slug)
		if providerName = strings.TrimSpace(providerName); providerName != "" {
			profile.SetProvider(providerName)
			config.SetModelProvider(providerName)
		}
		if reasoning = strings.TrimSpace(reasoning); reasoning != "" {
			profile.SetReasoningEffort(reasoning)
		} else if strings.TrimSpace(profile.ReasoningEffort) == "" && strings.TrimSpace(model.DefaultReasoningLevel) != "" {
			profile.SetReasoningEffort(model.DefaultReasoningLevel)
		}
		config.UpsertProfile(profile)
		if strings.TrimSpace(explicitProfile) != "" || strings.TrimSpace(config.ActiveProfileName()) == "" {
			config.SetActiveProfile(targetProfile)
		}
		return targetProfile
	}

	config.SetModel(model.Slug)
	if providerName = strings.TrimSpace(providerName); providerName != "" {
		config.SetModelProvider(providerName)
	}
	if reasoning = strings.TrimSpace(reasoning); reasoning != "" {
		config.SetReasoningEffort(reasoning)
	} else if strings.TrimSpace(config.EffectiveReasoningEffort()) == "" && strings.TrimSpace(model.DefaultReasoningLevel) != "" {
		config.SetReasoningEffort(model.DefaultReasoningLevel)
	}
	return ""
}

func buildModelPayload(config state.Config, settings state.Settings, ctx modelcatalog.RuntimeContext) map[string]any {
	modelSlug := strings.TrimSpace(config.EffectiveModel())
	reasoning := strings.TrimSpace(config.EffectiveReasoningEffort())
	if ctx.HasProfile {
		if value := strings.TrimSpace(ctx.Profile.Model); value != "" {
			modelSlug = value
		}
		if value := strings.TrimSpace(ctx.Profile.ReasoningEffort); value != "" {
			reasoning = value
		}
	}
	model, matched := modelcatalog.ResolveModelChoice(ctx.ProviderID, ctx.Catalog, modelSlug)
	presets := modelcatalog.EffectivePresetChoices(ctx.Catalog, settings, ctx.ProviderID)

	providerPayload := map[string]any{
		"name":      firstNonEmpty(ctx.ProviderName, config.EffectiveProviderName()),
		"id":        ctx.ProviderID,
		"base_url":  "",
		"wire_api":  "",
		"has_token": false,
	}
	if ctx.HasProvider {
		providerPayload["name"] = ctx.Provider.Name
		providerPayload["display_name"] = ctx.Provider.DisplayName()
		providerPayload["base_url"] = ctx.Provider.BaseURL
		providerPayload["wire_api"] = ctx.Provider.WireAPI
		providerPayload["api_key_env"] = ctx.Provider.APIKeyEnv
		providerPayload["has_token"] = strings.TrimSpace(ctx.Provider.BearerToken()) != ""
	}

	return map[string]any{
		"model": map[string]any{
			"slug":                       model.Slug,
			"display_name":               model.DisplayName,
			"description":                model.Description,
			"default_reasoning":          model.DefaultReasoningLevel,
			"supported_reasoning_levels": model.SupportedReasoningLevels,
			"context_window":             model.ContextWindow,
			"supports_parallel_tools":    model.SupportsParallelTools,
			"supports_reasoning_summary": model.SupportsReasoningSummary,
			"matched_catalog":            matched,
		},
		"reasoning":       reasoning,
		"active_profile":  config.ActiveProfileName(),
		"target_profile":  ctx.ProfileName,
		"active_provider": config.EffectiveProviderName(),
		"provider":        providerPayload,
		"catalog": map[string]any{
			"path":        ctx.SidecarPath,
			"found":       ctx.SidecarFound,
			"fetched_at":  ctx.Snapshot.FetchedAt,
			"model_count": len(ctx.Snapshot.Models),
			"profile":     ctx.Snapshot.ProfileName,
			"provider_id": firstNonEmpty(ctx.Snapshot.ProviderID, ctx.ProviderID),
		},
		"presets":   presets,
		"profiles":  config.ProfileNames(),
		"providers": config.ModelProviderNames(),
	}
}

func printModelPayload(payload map[string]any) {
	modelMap, _ := payload["model"].(map[string]any)
	providerMap, _ := payload["provider"].(map[string]any)
	catalogMap, _ := payload["catalog"].(map[string]any)
	presets, _ := payload["presets"].([]modelcatalog.EffectivePreset)

	fmt.Printf("model: %s\n", fallback(asString(modelMap["slug"]), "<unset>"))
	fmt.Printf("model_display_name: %s\n", fallback(asString(modelMap["display_name"]), "<unset>"))
	fmt.Printf("reasoning: %s\n", fallback(asString(payload["reasoning"]), "<unset>"))
	fmt.Printf("active_profile: %s\n", fallback(asString(payload["active_profile"]), "<unset>"))
	fmt.Printf("target_profile: %s\n", fallback(asString(payload["target_profile"]), "<unset>"))
	fmt.Printf("provider: %s\n", fallback(asString(providerMap["name"]), "<unset>"))
	fmt.Printf("provider_id: %s\n", fallback(asString(providerMap["id"]), "<unset>"))
	fmt.Printf("catalog_path: %s\n", fallback(asString(catalogMap["path"]), "<unset>"))
	fmt.Printf("catalog_found: %t\n", asBool(catalogMap["found"]))
	fmt.Printf("catalog_models: %d\n", asInt(catalogMap["model_count"]))
	fmt.Printf("presets: %d\n", len(presets))
	for _, preset := range presets {
		fmt.Printf("  - %s  model=%s source=%s reasoning=%s\n", preset.Key, fallback(firstNonEmpty(preset.Model.DisplayName, preset.Model.Slug), "<unset>"), fallback(preset.Source, "<unset>"), fallback(preset.Reasoning, "<unset>"))
	}
}

func runModelPreset(config state.Config, settings state.Settings, configPath string, args []string) int {
	input, err := parseModelPresetArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model: %v\n", err)
		return 2
	}
	ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), input.Profile, input.Provider)
	if err != nil {
		fmt.Fprintf(os.Stderr, "model: %v\n", err)
		return 1
	}

	if input.Action == "list" {
		payload := map[string]any{
			"profile":       firstNonEmpty(input.Profile, ctx.ProfileName),
			"provider":      firstNonEmpty(input.Provider, ctx.ProviderName),
			"provider_id":   ctx.ProviderID,
			"catalog_path":  ctx.SidecarPath,
			"catalog_found": ctx.SidecarFound,
			"presets":       modelcatalog.EffectivePresetChoices(ctx.Catalog, settings, ctx.ProviderID),
		}
		if input.JSON {
			return printJSON(payload)
		}
		fmt.Printf("provider: %s\n", fallback(asString(payload["provider"]), "<unset>"))
		fmt.Printf("provider_id: %s\n", fallback(asString(payload["provider_id"]), "<unset>"))
		fmt.Printf("catalog_path: %s\n", fallback(asString(payload["catalog_path"]), "<unset>"))
		fmt.Printf("catalog_found: %t\n", asBool(payload["catalog_found"]))
		for _, preset := range payload["presets"].([]modelcatalog.EffectivePreset) {
			fmt.Printf("  - %s  model=%s source=%s reasoning=%s\n", preset.Key, fallback(firstNonEmpty(preset.Model.DisplayName, preset.Model.Slug), "<unset>"), fallback(preset.Source, "<unset>"), fallback(preset.Reasoning, "<unset>"))
		}
		return 0
	}

	preset, ok := modelcatalog.EffectivePresetChoice(ctx.Catalog, settings, ctx.ProviderID, input.Key)
	if !ok {
		fmt.Fprintf(os.Stderr, "model: preset %q not available for provider %q\n", input.Key, ctx.ProviderID)
		return 1
	}
	targetProfile := applyModelSelection(&config, input.Profile, firstNonEmpty(input.Provider, ctx.ProviderName), preset.Model, preset.Reasoning)
	if err := state.SaveConfig(configPath, config); err != nil {
		fmt.Fprintf(os.Stderr, "model: failed to save config: %v\n", err)
		return 1
	}

	payload := map[string]any{
		"applied":        true,
		"preset":         preset,
		"target_profile": targetProfile,
		"provider":       firstNonEmpty(input.Provider, ctx.ProviderName),
	}
	if input.JSON {
		return printJSON(payload)
	}
	fmt.Printf("preset applied: %s\n", preset.Key)
	fmt.Printf("model: %s\n", preset.Model.Slug)
	if targetProfile != "" {
		fmt.Printf("target_profile: %s\n", targetProfile)
	}
	return 0
}

func runSettingsPresets(settings state.Settings, settingsPath string, args []string) int {
	input, err := parseSettingsPresetArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "settings: %v\n", err)
		return 2
	}

	switch input.Action {
	case "enable":
		settings.SetModelPresetsEnabled(true)
		if err := state.SaveSettings(settingsPath, settings); err != nil {
			fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
			return 1
		}
		fmt.Println("model presets enabled")
		return 0
	case "disable":
		settings.SetModelPresetsEnabled(false)
		if err := state.SaveSettings(settingsPath, settings); err != nil {
			fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
			return 1
		}
		fmt.Println("model presets disabled")
		return 0
	case "set":
		config, err := loadConfigOptional(apphome.ConfigPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "settings: failed to read config: %v\n", err)
			return 1
		}
		ctx, err := modelcatalog.ResolveRuntimeContext(config, apphome.CodexHome(), input.Profile, input.Provider)
		if err != nil {
			fmt.Fprintf(os.Stderr, "settings: %v\n", err)
			return 1
		}
		providerID := modelcatalog.NormalizeProviderID(firstNonEmpty(input.Provider, ctx.ProviderID, ctx.ProviderName))
		if providerID == "" {
			fmt.Fprintln(os.Stderr, "settings: provider is required")
			return 2
		}
		modelName := strings.TrimSpace(input.Model)
		name := strings.TrimSpace(input.Name)
		reasoning := strings.TrimSpace(input.Reasoning)
		if modelName != "" {
			model, _ := modelcatalog.ResolveModelChoice(providerID, ctx.Catalog, modelName)
			modelName = model.Slug
			if name == "" {
				name = firstNonEmpty(model.DisplayName, modelName)
			}
			if reasoning == "" {
				reasoning = model.DefaultReasoningLevel
			}
		}
		settings.SetModelPreset(providerID, strings.ToLower(strings.TrimSpace(input.PresetKey)), state.ModelPresetConfig{
			Name:      name,
			Model:     modelName,
			Reasoning: reasoning,
		})
		if err := state.SaveSettings(settingsPath, settings); err != nil {
			fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
			return 1
		}
		fmt.Printf("model preset updated: %s/%s\n", providerID, strings.ToLower(strings.TrimSpace(input.PresetKey)))
		return 0
	case "delete":
		providerID := modelcatalog.NormalizeProviderID(input.Provider)
		if providerID == "" {
			fmt.Fprintln(os.Stderr, "settings: provider is required")
			return 2
		}
		settings.DeleteModelPreset(providerID, strings.ToLower(strings.TrimSpace(input.PresetKey)))
		if err := state.SaveSettings(settingsPath, settings); err != nil {
			fmt.Fprintf(os.Stderr, "settings: failed to save settings: %v\n", err)
			return 1
		}
		fmt.Printf("model preset removed: %s/%s\n", providerID, strings.ToLower(strings.TrimSpace(input.PresetKey)))
		return 0
	}

	providers := make(map[string]any, len(settings.ModelPresets.Providers))
	for _, provider := range settings.ModelPresets.ProviderNames() {
		presets := settings.ModelPresets.Providers[provider]
		current := make(map[string]state.ModelPresetConfig, len(presets.Presets))
		for key, preset := range presets.Presets {
			current[key] = preset
		}
		providers[provider] = current
	}
	payload := map[string]any{
		"enabled":   settings.ModelPresets.Enabled,
		"providers": providers,
	}
	if input.JSON {
		return printJSON(payload)
	}
	fmt.Printf("model_presets_enabled: %t\n", settings.ModelPresets.Enabled)
	for _, provider := range settings.ModelPresets.ProviderNames() {
		fmt.Printf("provider: %s\n", provider)
		presets := settings.ModelPresets.Providers[provider]
		for _, key := range []string{"fast", "balanced", "power"} {
			preset, ok := presets.Presets[key]
			if !ok {
				continue
			}
			fmt.Printf("  - %s  model=%s reasoning=%s name=%s\n", key, fallback(preset.Model, "<unset>"), fallback(preset.Reasoning, "<unset>"), fallback(preset.Name, "<unset>"))
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func asString(value any) string {
	switch current := value.(type) {
	case string:
		return current
	default:
		return ""
	}
}

func asBool(value any) bool {
	current, _ := value.(bool)
	return current
}

func asInt(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	default:
		return 0
	}
}

func contextBackground() context.Context {
	return context.Background()
}

func loadConfigOptional(path string) (state.Config, error) {
	config, err := state.LoadConfig(path)
	if err == nil {
		return config, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return state.Config{}, nil
	}
	return state.Config{}, err
}

func loadSettingsOptional(path string) (state.Settings, error) {
	settings, err := state.LoadSettings(path)
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return state.Settings{}, nil
	}
	return state.Settings{}, err
}

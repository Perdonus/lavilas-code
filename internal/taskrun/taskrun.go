package taskrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/state"
)

type Options struct {
	Prompt            string
	SystemPrompt      string
	Model             string
	Profile           string
	Provider          string
	ReasoningEffort   string
	JSON              bool
	DisableStreaming  bool
}

type Result struct {
	ProviderName string            `json:"provider_name"`
	Model        string            `json:"model"`
	Reasoning    string            `json:"reasoning,omitempty"`
	Profile      string            `json:"profile,omitempty"`
	Response     *runtime.Response `json:"response,omitempty"`
	Events       []runtime.StreamEvent `json:"events,omitempty"`
	Text         string            `json:"text,omitempty"`
}

func Run(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.Prompt) == "" {
		return Result{}, fmt.Errorf("prompt is required")
	}

	config, err := state.LoadConfig(apphome.ConfigPath())
	if err != nil {
		return Result{}, fmt.Errorf("load config: %w", err)
	}

	resolved, err := resolveRequest(config, options)
	if err != nil {
		return Result{}, err
	}

	client, err := openai.NewClient(resolved.ProviderConfig)
	if err != nil {
		return Result{}, err
	}

	request := runtime.Request{
		Model:           resolved.Model,
		ReasoningEffort: resolved.ReasoningEffort,
		Messages:        resolved.Messages,
	}

	result := Result{
		ProviderName: resolved.ProviderName,
		Model:        resolved.Model,
		Reasoning:    resolved.ReasoningEffort,
		Profile:      resolved.ProfileName,
	}

	if options.JSON || options.DisableStreaming {
		response, err := client.Create(ctx, request)
		if err != nil {
			return Result{}, err
		}
		result.Response = response
		result.Text = responseText(response)
		return result, nil
	}

	stream, err := client.Stream(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer stream.Close()

	var builder strings.Builder
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Result{}, err
		}
		result.Events = append(result.Events, event)
		if event.Type == runtime.StreamEventTypeDelta {
			text := event.Delta.Text()
			if text != "" {
				builder.WriteString(text)
			}
		}
		if event.Type == runtime.StreamEventTypeDone {
			break
		}
	}
	result.Text = builder.String()
	return result, nil
}

func Print(result Result) error {
	if result.Response != nil {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	if strings.TrimSpace(result.Text) != "" {
		fmt.Println(result.Text)
		return nil
	}

	if len(result.Events) == 0 {
		fmt.Println("<no output>")
		return nil
	}

	data, err := json.MarshalIndent(result.Events, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

type resolvedRequest struct {
	ProviderName     string
	ProviderConfig   openai.Config
	Model            string
	ReasoningEffort  string
	ProfileName      string
	Messages         []runtime.Message
}

func resolveRequest(config state.Config, options Options) (resolvedRequest, error) {
	profileName := strings.TrimSpace(options.Profile)
	if profileName == "" {
		profileName = config.ActiveProfileName()
	}

	var profile state.ProfileConfig
	if profileName != "" {
		selected, ok := config.Profile(profileName)
		if !ok {
			return resolvedRequest{}, fmt.Errorf("profile %q not found", profileName)
		}
		profile = selected
	}

	model := strings.TrimSpace(options.Model)
	if model == "" && strings.TrimSpace(profile.Model) != "" {
		model = strings.TrimSpace(profile.Model)
	}
	if model == "" {
		model = config.EffectiveModel()
	}
	if model == "" {
		return resolvedRequest{}, fmt.Errorf("model is not configured")
	}

	reasoning := strings.TrimSpace(options.ReasoningEffort)
	if reasoning == "" && strings.TrimSpace(profile.ReasoningEffort) != "" {
		reasoning = strings.TrimSpace(profile.ReasoningEffort)
	}
	if reasoning == "" {
		reasoning = config.EffectiveReasoningEffort()
	}

	providerName := strings.TrimSpace(options.Provider)
	if providerName == "" && strings.TrimSpace(profile.Provider) != "" {
		providerName = strings.TrimSpace(profile.Provider)
	}
	if providerName == "" {
		providerName = config.EffectiveProviderName()
	}

	providerConfig, resolvedProviderName, err := resolveProviderConfig(config, providerName)
	if err != nil {
		return resolvedRequest{}, err
	}

	messages := make([]runtime.Message, 0, 2)
	if strings.TrimSpace(options.SystemPrompt) != "" {
		messages = append(messages, runtime.TextMessage(runtime.RoleSystem, options.SystemPrompt))
	}
	messages = append(messages, runtime.TextMessage(runtime.RoleUser, options.Prompt))

	return resolvedRequest{
		ProviderName:    resolvedProviderName,
		ProviderConfig:  providerConfig,
		Model:           model,
		ReasoningEffort: reasoning,
		ProfileName:     profileName,
		Messages:        messages,
	}, nil
}

func resolveProviderConfig(config state.Config, providerName string) (openai.Config, string, error) {
	if strings.TrimSpace(providerName) == "" {
		apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if apiKey == "" {
			return openai.Config{}, "", fmt.Errorf("provider is not configured and OPENAI_API_KEY is empty")
		}
		return openai.Config{
			Name:   "OpenAI",
			APIKey: apiKey,
		}, "openai", nil
	}

	provider, ok := config.Provider(providerName)
	if !ok {
		return openai.Config{}, "", fmt.Errorf("provider %q not found", providerName)
	}
	if wireAPI := strings.TrimSpace(provider.WireAPI); wireAPI != "" && wireAPI != "chat_completions" {
		return openai.Config{}, "", fmt.Errorf("provider %q uses unsupported wire_api %q", providerName, wireAPI)
	}

	token := provider.BearerToken()
	if token == "" {
		return openai.Config{}, "", fmt.Errorf("provider %q has no bearer token or env key value", providerName)
	}

	baseURL, chatPath, err := providerEndpoint(provider.BaseURL)
	if err != nil {
		return openai.Config{}, "", fmt.Errorf("provider %q: %w", providerName, err)
	}

	return openai.Config{
		Name:                provider.DisplayName(),
		BaseURL:             baseURL,
		ChatCompletionsPath: chatPath,
		APIKey:              token,
	}, providerName, nil
}

func providerEndpoint(baseURL string) (string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return openai.DefaultBaseURL, openai.DefaultChatCompletionsPath, nil
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", "", fmt.Errorf("parse provider base_url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("provider base_url must include scheme and host")
	}

	path := strings.TrimRight(parsed.Path, "/")
	switch {
	case strings.HasSuffix(path, "/responses"):
		return "", "", fmt.Errorf("responses endpoint is not implemented in Go alpha yet")
	case strings.HasSuffix(path, "/chat/completions"):
		parsed.Path = strings.TrimSuffix(path, "/chat/completions")
		if parsed.Path == "/" {
			parsed.Path = ""
		}
		return parsed.String(), "/chat/completions", nil
	case path == "":
		return parsed.String(), "/v1/chat/completions", nil
	default:
		return parsed.String(), "/chat/completions", nil
	}
}

func responseText(response *runtime.Response) string {
	if response == nil {
		return ""
	}
	var parts []string
	for _, choice := range response.Choices {
		text := strings.TrimSpace(choice.Message.Text())
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

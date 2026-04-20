package taskrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/provider/responsesapi"
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
	History           []runtime.Message
}

type Result struct {
	ProviderName string                `json:"provider_name"`
	Model        string                `json:"model"`
	Reasoning    string                `json:"reasoning,omitempty"`
	Profile      string                `json:"profile,omitempty"`
	SessionPath  string                `json:"session_path,omitempty"`
	Response     *runtime.Response     `json:"response,omitempty"`
	Events       []runtime.StreamEvent `json:"events,omitempty"`
	Text         string                `json:"text,omitempty"`
	RequestMessages  []runtime.Message `json:"-"`
	AssistantMessage runtime.Message   `json:"-"`
}

func Run(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.Prompt) == "" {
		return Result{}, fmt.Errorf("prompt is required")
	}

	config, err := state.LoadConfig(apphome.ConfigPath())
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Result{}, fmt.Errorf("load config: %w", err)
		}
		config = state.Config{}
	}

	resolved, err := resolveRequest(config, options)
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
		RequestMessages: cloneMessages(resolved.Messages),
	}

	if options.JSON || options.DisableStreaming {
		response, err := resolved.Client.Create(ctx, request)
		if err != nil {
			return Result{}, err
		}
		result.Response = response
		result.Text = responseText(response)
		if response != nil && len(response.Choices) > 0 {
			result.AssistantMessage = response.Choices[0].Message
		} else if strings.TrimSpace(result.Text) != "" {
			result.AssistantMessage = runtime.TextMessage(runtime.RoleAssistant, result.Text)
		}
		return result, nil
	}

	stream, err := resolved.Client.Stream(ctx, request)
	if err != nil {
		return Result{}, err
	}
	defer stream.Close()

	var builder strings.Builder
	assistant := runtime.Message{Role: runtime.RoleAssistant}
	toolCalls := map[int]*runtime.ToolCall{}
	toolOrder := []int{}
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
			for _, toolCall := range event.Delta.ToolCalls {
				current, ok := toolCalls[toolCall.Index]
				if !ok {
					toolType := toolCall.Type
					if toolType == "" {
						toolType = runtime.ToolTypeFunction
					}
					current = &runtime.ToolCall{Type: toolType}
					toolCalls[toolCall.Index] = current
					toolOrder = append(toolOrder, toolCall.Index)
				}
				if strings.TrimSpace(toolCall.ID) != "" {
					current.ID = toolCall.ID
				}
				if toolCall.Type != "" {
					current.Type = toolCall.Type
				}
				if toolCall.NameDelta != "" {
					current.Function.Name += toolCall.NameDelta
				}
				if toolCall.ArgumentsDelta != "" {
					current.Function.Arguments = append(current.Function.Arguments, []byte(toolCall.ArgumentsDelta)...)
				}
			}
		}
		if event.Type == runtime.StreamEventTypeDone {
			break
		}
	}
	result.Text = builder.String()
	if strings.TrimSpace(result.Text) != "" {
		assistant.Content = []runtime.ContentPart{runtime.TextPart(result.Text)}
	}
	if len(toolOrder) > 0 {
		for _, index := range toolOrder {
			if current, ok := toolCalls[index]; ok {
				assistant.ToolCalls = append(assistant.ToolCalls, *current)
			}
		}
	}
	result.AssistantMessage = assistant
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
	Client           provider.Client
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

	client, resolvedProviderName, err := resolveProviderClient(config, providerName)
	if err != nil {
		return resolvedRequest{}, err
	}

	messages := cloneMessages(options.History)
	if len(messages) == 0 {
		messages = append(messages, runtime.TextMessage(runtime.RoleSystem, resolveSystemPrompt(options.SystemPrompt)))
	} else if strings.TrimSpace(options.SystemPrompt) != "" && messages[0].Role == runtime.RoleSystem {
		messages[0] = runtime.TextMessage(runtime.RoleSystem, resolveSystemPrompt(options.SystemPrompt))
	}
	messages = append(messages, runtime.TextMessage(runtime.RoleUser, options.Prompt))

	return resolvedRequest{
		ProviderName:    resolvedProviderName,
		Client:          client,
		Model:           model,
		ReasoningEffort: reasoning,
		ProfileName:     profileName,
		Messages:        messages,
	}, nil
}

func cloneMessages(messages []runtime.Message) []runtime.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]runtime.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

func resolveProviderClient(config state.Config, providerName string) (provider.Client, string, error) {
	if strings.TrimSpace(providerName) == "" {
		apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		if apiKey == "" {
			return nil, "", fmt.Errorf("provider is not configured and OPENAI_API_KEY is empty")
		}
		client, err := responsesapi.NewClient(responsesapi.Config{
			Name:   "OpenAI",
			APIKey: apiKey,
		})
		if err != nil {
			return nil, "", err
		}
		return client, "openai", nil
	}

	providerConfig, ok := config.Provider(providerName)
	if !ok {
		return nil, "", fmt.Errorf("provider %q not found", providerName)
	}

	token := providerConfig.BearerToken()
	if token == "" {
		return nil, "", fmt.Errorf("provider %q has no bearer token or env key value", providerName)
	}

	wireAPI := strings.TrimSpace(providerConfig.WireAPI)
	switch wireAPI {
	case "", "chat_completions":
		baseURL, chatPath, err := providerEndpoint(providerConfig.BaseURL)
		if err != nil {
			return nil, "", fmt.Errorf("provider %q: %w", providerName, err)
		}
		client, err := openai.NewClient(openai.Config{
			Name:                providerConfig.DisplayName(),
			BaseURL:             baseURL,
			ChatCompletionsPath: chatPath,
			APIKey:              token,
		})
		if err != nil {
			return nil, "", err
		}
		return client, providerName, nil
	case "responses":
		baseURL, responsesPath, err := responsesEndpoint(providerConfig.BaseURL)
		if err != nil {
			return nil, "", fmt.Errorf("provider %q: %w", providerName, err)
		}
		client, err := responsesapi.NewClient(responsesapi.Config{
			Name:          providerConfig.DisplayName(),
			BaseURL:       baseURL,
			ResponsesPath: responsesPath,
			APIKey:        token,
		})
		if err != nil {
			return nil, "", err
		}
		return client, providerName, nil
	default:
		return nil, "", fmt.Errorf("provider %q uses unsupported wire_api %q", providerName, wireAPI)
	}
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
		return "", "", fmt.Errorf("provider base_url points to responses endpoint")
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

func responsesEndpoint(baseURL string) (string, string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return responsesapi.DefaultBaseURL, responsesapi.DefaultResponsesPath, nil
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
		parsed.Path = strings.TrimSuffix(path, "/responses")
		if parsed.Path == "/" {
			parsed.Path = ""
		}
		return parsed.String(), "/responses", nil
	case path == "":
		return parsed.String(), "/v1/responses", nil
	default:
		return parsed.String(), "/responses", nil
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

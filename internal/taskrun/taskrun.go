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
	"time"

	"github.com/Perdonus/lavilas-code/internal/apphome"
	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/provider/openai"
	"github.com/Perdonus/lavilas-code/internal/provider/responsesapi"
	"github.com/Perdonus/lavilas-code/internal/runtime"
	"github.com/Perdonus/lavilas-code/internal/state"
	"github.com/Perdonus/lavilas-code/internal/tooling"
)

type Options struct {
	Prompt           string
	SystemPrompt     string
	Model            string
	Profile          string
	Provider         string
	ReasoningEffort  string
	JSON             bool
	DisableStreaming bool
	History          []runtime.Message
}

type Result struct {
	ProviderName     string                `json:"provider_name"`
	Model            string                `json:"model"`
	Reasoning        string                `json:"reasoning,omitempty"`
	Profile          string                `json:"profile,omitempty"`
	SessionPath      string                `json:"session_path,omitempty"`
	Response         *runtime.Response     `json:"response,omitempty"`
	Events           []runtime.StreamEvent `json:"events,omitempty"`
	Text             string                `json:"text,omitempty"`
	History          []runtime.Message     `json:"-"`
	RequestMessages  []runtime.Message     `json:"-"`
	AssistantMessage runtime.Message       `json:"-"`
}

const (
	maxProviderAttempts = 4
	baseProviderBackoff = time.Second
	maxProviderBackoff  = 30 * time.Second
)

func (result Result) FullHistory() []runtime.Message {
	if len(result.History) > 0 {
		return cloneMessages(result.History)
	}
	history := cloneMessages(result.RequestMessages)
	if hasPersistableMessage(result.AssistantMessage) {
		history = append(history, result.AssistantMessage)
	}
	return history
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
	if resolved.Client.Capabilities().Tools {
		request.Tools = tooling.Definitions()
		request.ToolChoice = runtime.ToolChoice{Mode: runtime.ToolChoiceModeAuto}
		parallel := true
		request.ParallelToolCalls = &parallel
	}

	result := Result{
		ProviderName:    resolved.ProviderName,
		Model:           resolved.Model,
		Reasoning:       resolved.ReasoningEffort,
		Profile:         resolved.ProfileName,
		RequestMessages: cloneMessages(resolved.Messages),
	}
	preferStreaming := !options.JSON && !options.DisableStreaming

	if len(request.Tools) > 0 {
		history, requestMessages, response, assistantMessage, events, err := runWithToolLoop(ctx, resolved.Client, request, preferStreaming)
		if err != nil {
			return Result{}, err
		}
		result.History = cloneMessages(history)
		result.RequestMessages = cloneMessages(requestMessages)
		result.Response = response
		result.Events = append(result.Events, events...)
		result.Text = strings.TrimSpace(assistantMessage.Text())
		if result.Text == "" {
			result.Text = responseText(response)
		}
		result.AssistantMessage = assistantMessage
		return result, nil
	}

	if !preferStreaming {
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
		result.History = append(cloneMessages(request.Messages), result.AssistantMessage)
		return result, nil
	}

	response, events, assistantMessage, err := runSingleTurn(ctx, resolved.Client, request, true)
	if err != nil {
		return Result{}, err
	}
	result.Response = response
	result.Events = append(result.Events, events...)
	result.AssistantMessage = assistantMessage
	result.Text = strings.TrimSpace(assistantMessage.Text())
	if result.Text == "" {
		result.Text = responseText(response)
	}
	result.History = append(cloneMessages(request.Messages), assistantMessage)
	return result, nil
}

func runWithToolLoop(ctx context.Context, client provider.Client, request runtime.Request, preferStreaming bool) ([]runtime.Message, []runtime.Message, *runtime.Response, runtime.Message, []runtime.StreamEvent, error) {
	const maxRounds = 8

	messages := cloneMessages(request.Messages)
	baseRequest := request
	var events []runtime.StreamEvent

	for round := 0; round < maxRounds; round++ {
		currentRequest := baseRequest
		currentRequest.Messages = cloneMessages(messages)

		response, roundEvents, assistantMessage, err := runSingleTurn(ctx, client, currentRequest, preferStreaming)
		if err != nil {
			return nil, nil, nil, runtime.Message{}, nil, err
		}
		events = append(events, roundEvents...)
		if len(assistantMessage.ToolCalls) == 0 {
			fullHistory := cloneMessages(messages)
			if hasPersistableMessage(assistantMessage) {
				fullHistory = append(fullHistory, assistantMessage)
			}
			return fullHistory, currentRequest.Messages, response, assistantMessage, events, nil
		}

		messages = append(messages, assistantMessage)
		messages = append(messages, tooling.ExecuteCalls(ctx, assistantMessage.ToolCalls)...)
	}

	return nil, nil, nil, runtime.Message{}, nil, fmt.Errorf("tool loop exceeded %d rounds", maxRounds)
}

func runSingleTurn(ctx context.Context, client provider.Client, request runtime.Request, preferStreaming bool) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	if preferStreaming && client.Capabilities().Streaming {
		return collectStreamTurn(ctx, client, request)
	}

	response, err := createWithRetry(ctx, client, request)
	if err != nil {
		return nil, nil, runtime.Message{}, err
	}
	if response == nil || len(response.Choices) == 0 {
		return nil, nil, runtime.Message{}, fmt.Errorf("provider returned no choices")
	}
	return response, nil, response.Choices[0].Message, nil
}

func collectStreamTurn(ctx context.Context, client provider.Client, request runtime.Request) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	stream, err := streamWithRetry(ctx, client, request)
	if err != nil {
		return nil, nil, runtime.Message{}, err
	}
	defer stream.Close()
	return collectStreamResponse(stream, client.Name(), request.Model)
}

func collectStreamResponse(stream runtime.Stream, providerName string, fallbackModel string) (*runtime.Response, []runtime.StreamEvent, runtime.Message, error) {
	events := make([]runtime.StreamEvent, 0, 16)
	accumulator := newTurnAccumulator(providerName, fallbackModel)
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, runtime.Message{}, err
		}
		events = append(events, event)
		accumulator.Apply(event)
		if event.Type == runtime.StreamEventTypeDone {
			break
		}
	}

	response := accumulator.Response()
	if response == nil || len(response.Choices) == 0 {
		return nil, nil, runtime.Message{}, fmt.Errorf("provider returned no streamed choices")
	}
	return response, events, response.Choices[0].Message, nil
}

func createWithRetry(ctx context.Context, client provider.Client, request runtime.Request) (*runtime.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxProviderAttempts; attempt++ {
		response, err := client.Create(ctx, request)
		if err == nil {
			return response, nil
		}
		lastErr = err
		delay, ok := providerRetryDelay(err, attempt)
		if !ok {
			return nil, err
		}
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func streamWithRetry(ctx context.Context, client provider.Client, request runtime.Request) (runtime.Stream, error) {
	var lastErr error
	for attempt := 0; attempt < maxProviderAttempts; attempt++ {
		stream, err := client.Stream(ctx, request)
		if err == nil {
			return stream, nil
		}
		lastErr = err
		delay, ok := providerRetryDelay(err, attempt)
		if !ok {
			return nil, err
		}
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func providerRetryDelay(err error, attempt int) (time.Duration, bool) {
	if attempt >= maxProviderAttempts-1 {
		return 0, false
	}
	var providerErr *provider.Error
	if !errors.As(err, &providerErr) || !providerErr.Retryable {
		return 0, false
	}
	if providerErr.RetryAfter > 0 {
		return providerErr.RetryAfter, true
	}
	delay := baseProviderBackoff * time.Duration(1<<attempt)
	if delay > maxProviderBackoff {
		delay = maxProviderBackoff
	}
	return delay, true
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type turnAccumulator struct {
	provider      string
	fallbackModel string
	responseID    string
	model         string
	createdAt     time.Time
	message       runtime.Message
	finishReason  runtime.FinishReason
	usage         runtime.Usage
	toolOrder     []string
	toolCalls     map[string]*runtime.ToolCall
}

func newTurnAccumulator(providerName string, fallbackModel string) *turnAccumulator {
	return &turnAccumulator{
		provider:      providerName,
		fallbackModel: strings.TrimSpace(fallbackModel),
		message:       runtime.Message{Role: runtime.RoleAssistant},
		toolCalls:     map[string]*runtime.ToolCall{},
	}
}

func (a *turnAccumulator) Apply(event runtime.StreamEvent) {
	if strings.TrimSpace(event.ResponseID) != "" {
		a.responseID = event.ResponseID
	}
	if strings.TrimSpace(event.Model) != "" {
		a.model = event.Model
	}
	if !event.CreatedAt.IsZero() {
		a.createdAt = event.CreatedAt
	}

	switch event.Type {
	case runtime.StreamEventTypeDelta:
		if event.Delta.Role != "" {
			a.message.Role = event.Delta.Role
		}
		for _, part := range event.Delta.Content {
			if part.Type == runtime.ContentPartTypeText && part.Text != "" {
				a.appendText(part.Text)
			}
		}
		for _, toolCall := range event.Delta.ToolCalls {
			a.applyToolCallDelta(toolCall)
		}
	case runtime.StreamEventTypeChoiceDone:
		if event.FinishReason != "" {
			a.finishReason = event.FinishReason
		}
	case runtime.StreamEventTypeUsage:
		if event.Usage != nil {
			a.usage = *event.Usage
		}
	}
}

func (a *turnAccumulator) Response() *runtime.Response {
	if a.message.Role == "" {
		a.message.Role = runtime.RoleAssistant
	}
	if len(a.toolOrder) > 0 {
		a.message.ToolCalls = make([]runtime.ToolCall, 0, len(a.toolOrder))
		for _, key := range a.toolOrder {
			if current, ok := a.toolCalls[key]; ok {
				a.message.ToolCalls = append(a.message.ToolCalls, *current)
			}
		}
	}

	finishReason := a.finishReason
	if finishReason == "" {
		if len(a.message.ToolCalls) > 0 {
			finishReason = runtime.FinishReasonToolCalls
		} else {
			finishReason = runtime.FinishReasonStop
		}
	}

	model := a.model
	if strings.TrimSpace(model) == "" {
		model = a.fallbackModel
	}
	createdAt := a.createdAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	return &runtime.Response{
		ID:        a.responseID,
		Model:     model,
		Provider:  a.provider,
		CreatedAt: createdAt,
		Choices: []runtime.Choice{{
			Index:        0,
			Message:      a.message,
			FinishReason: finishReason,
		}},
		Usage: a.usage,
	}
}

func (a *turnAccumulator) appendText(text string) {
	if text == "" {
		return
	}
	if len(a.message.Content) > 0 {
		last := &a.message.Content[len(a.message.Content)-1]
		if last.Type == runtime.ContentPartTypeText {
			last.Text += text
			return
		}
	}
	a.message.Content = append(a.message.Content, runtime.TextPart(text))
}

func (a *turnAccumulator) applyToolCallDelta(delta runtime.ToolCallDelta) {
	key := streamToolKey(delta)
	current, ok := a.toolCalls[key]
	if !ok {
		toolType := delta.Type
		if toolType == "" {
			toolType = runtime.ToolTypeFunction
		}
		current = &runtime.ToolCall{Type: toolType}
		a.toolCalls[key] = current
		a.toolOrder = append(a.toolOrder, key)
	}
	if strings.TrimSpace(delta.ID) != "" {
		current.ID = delta.ID
	}
	if delta.Type != "" {
		current.Type = delta.Type
	}
	if delta.NameDelta != "" {
		current.Function.Name += delta.NameDelta
	}
	if delta.ArgumentsDelta != "" {
		current.Function.Arguments = append(current.Function.Arguments, []byte(delta.ArgumentsDelta)...)
	}
}

func streamToolKey(delta runtime.ToolCallDelta) string {
	if strings.TrimSpace(delta.ID) != "" {
		return "id:" + strings.TrimSpace(delta.ID)
	}
	return fmt.Sprintf("index:%d", delta.Index)
}

func hasPersistableMessage(message runtime.Message) bool {
	return strings.TrimSpace(message.Text()) != "" || len(message.ToolCalls) > 0 || strings.TrimSpace(message.Refusal) != ""
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
	ProviderName    string
	Client          provider.Client
	Model           string
	ReasoningEffort string
	ProfileName     string
	Messages        []runtime.Message
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

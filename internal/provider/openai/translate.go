package openai

import (
	"fmt"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func requestFromRuntime(req runtime.Request, cfg Config, stream bool) (ChatCompletionRequest, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	if model == "" {
		return ChatCompletionRequest{}, fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return ChatCompletionRequest{}, fmt.Errorf("at least one message is required")
	}

	messages := make([]Message, 0, len(req.Messages))
	for idx, message := range req.Messages {
		if err := message.Validate(); err != nil {
			return ChatCompletionRequest{}, fmt.Errorf("message %d: %w", idx, err)
		}
		converted, err := messageFromRuntime(message)
		if err != nil {
			return ChatCompletionRequest{}, fmt.Errorf("message %d: %w", idx, err)
		}
		messages = append(messages, converted)
	}

	tools := make([]Tool, 0, len(req.Tools))
	for idx, tool := range req.Tools {
		if err := tool.Validate(); err != nil {
			return ChatCompletionRequest{}, fmt.Errorf("tool %d: %w", idx, err)
		}
		tools = append(tools, toolFromRuntime(tool))
	}

	reasoningEffort := strings.TrimSpace(req.ReasoningEffort)
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(cfg.DefaultReasoningEffort)
	}

	payload := ChatCompletionRequest{
		Model:             model,
		ReasoningEffort:   reasoningEffort,
		Messages:          messages,
		Tools:             tools,
		ParallelToolCalls: req.ParallelToolCalls,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		Stop:              req.Stop,
		Metadata:          req.Metadata,
		User:              req.User,
		Stream:            stream,
	}
	if req.MaxOutputTokens > 0 {
		payload.MaxTokens = req.MaxOutputTokens
	}
	if !req.ToolChoice.IsZero() {
		payload.ToolChoice = toolChoiceFromRuntime(req.ToolChoice)
	}
	if stream {
		payload.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	return payload, nil
}

func messageFromRuntime(message runtime.Message) (Message, error) {
	content, err := contentFromRuntime(message.Content)
	if err != nil {
		return Message{}, err
	}
	toolCalls := make([]ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		toolCalls = append(toolCalls, toolCallFromRuntime(call))
	}
	return Message{
		Role:       string(message.Role),
		Content:    content,
		Name:       message.Name,
		ToolCallID: message.ToolCallID,
		ToolCalls:  toolCalls,
		Refusal:    message.Refusal,
	}, nil
}

func contentFromRuntime(parts []runtime.ContentPart) (MessageContent, error) {
	if len(parts) == 0 {
		return MessageContent{}, nil
	}
	converted := make([]ContentPart, 0, len(parts))
	for idx, part := range parts {
		switch part.Type {
		case runtime.ContentPartTypeText:
			converted = append(converted, ContentPart{Type: ContentTypeText, Text: part.Text})
		case runtime.ContentPartTypeImageURL:
			if strings.TrimSpace(part.URL) == "" {
				return MessageContent{}, fmt.Errorf("content part %d: image url is required", idx)
			}
			converted = append(converted, ContentPart{Type: ContentTypeImageURL, ImageURL: &ImageURL{URL: part.URL, Detail: part.Detail}})
		case runtime.ContentPartTypeInputAudio:
			if strings.TrimSpace(part.AudioData) == "" {
				return MessageContent{}, fmt.Errorf("content part %d: audio data is required", idx)
			}
			converted = append(converted, ContentPart{Type: ContentTypeInputAudio, InputAudio: &InputAudio{Data: part.AudioData, Format: part.AudioFormat}})
		default:
			return MessageContent{}, fmt.Errorf("content part %d: unsupported type %q", idx, part.Type)
		}
	}
	return MessageContent{Parts: converted}, nil
}

func toolFromRuntime(tool runtime.ToolDefinition) Tool {
	toolType := tool.Type
	if toolType == "" {
		toolType = runtime.ToolTypeFunction
	}
	return Tool{
		Type: string(toolType),
		Function: FunctionDefinition{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
			Strict:      tool.Function.Strict,
		},
	}
}

func toolChoiceFromRuntime(choice runtime.ToolChoice) *ToolChoice {
	if strings.TrimSpace(choice.Name) != "" {
		return &ToolChoice{Mode: ToolChoiceModeFunction, FunctionName: choice.Name}
	}
	switch choice.Mode {
	case runtime.ToolChoiceModeNone:
		return &ToolChoice{Mode: ToolChoiceModeNone}
	case runtime.ToolChoiceModeRequired:
		return &ToolChoice{Mode: ToolChoiceModeRequired}
	case runtime.ToolChoiceModeNamed:
		return &ToolChoice{Mode: ToolChoiceModeFunction, FunctionName: choice.Name}
	default:
		return &ToolChoice{Mode: ToolChoiceModeAuto}
	}
}

func toolCallFromRuntime(call runtime.ToolCall) ToolCall {
	toolType := call.Type
	if toolType == "" {
		toolType = runtime.ToolTypeFunction
	}
	return ToolCall{
		ID:   call.ID,
		Type: string(toolType),
		Function: FunctionCall{
			Name:      call.Function.Name,
			Arguments: call.Function.ArgumentsString(),
		},
	}
}

func responseToRuntime(providerName string, response ChatCompletionResponse) (*runtime.Response, error) {
	result := &runtime.Response{
		ID:        response.ID,
		Model:     response.Model,
		Provider:  providerName,
		CreatedAt: time.Unix(response.Created, 0).UTC(),
		Choices:   make([]runtime.Choice, 0, len(response.Choices)),
	}
	if response.Usage != nil {
		result.Usage = usageToRuntime(*response.Usage)
	}
	for _, choice := range response.Choices {
		message, err := messageToRuntime(choice.Message)
		if err != nil {
			return nil, err
		}
		result.Choices = append(result.Choices, runtime.Choice{
			Index:        choice.Index,
			Message:      message,
			FinishReason: runtime.FinishReason(choice.FinishReason),
		})
	}
	return result, nil
}

func messageToRuntime(message Message) (runtime.Message, error) {
	content := make([]runtime.ContentPart, 0, len(message.Content.Parts))
	for idx, part := range message.Content.Parts {
		switch part.Type {
		case ContentTypeText:
			content = append(content, runtime.ContentPart{Type: runtime.ContentPartTypeText, Text: part.Text})
		case ContentTypeImageURL:
			if part.ImageURL == nil {
				return runtime.Message{}, fmt.Errorf("content part %d: missing image payload", idx)
			}
			content = append(content, runtime.ContentPart{Type: runtime.ContentPartTypeImageURL, URL: part.ImageURL.URL, Detail: part.ImageURL.Detail})
		case ContentTypeInputAudio:
			if part.InputAudio == nil {
				return runtime.Message{}, fmt.Errorf("content part %d: missing audio payload", idx)
			}
			content = append(content, runtime.ContentPart{Type: runtime.ContentPartTypeInputAudio, AudioData: part.InputAudio.Data, AudioFormat: part.InputAudio.Format})
		default:
			return runtime.Message{}, fmt.Errorf("content part %d: unsupported type %q", idx, part.Type)
		}
	}
	toolCalls := make([]runtime.ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		toolType := runtime.ToolType(call.Type)
		if toolType == "" {
			toolType = runtime.ToolTypeFunction
		}
		toolCalls = append(toolCalls, runtime.ToolCall{
			ID:   call.ID,
			Type: toolType,
			Function: runtime.FunctionCall{
				Name:      call.Function.Name,
				Arguments: []byte(call.Function.Arguments),
			},
		})
	}
	return runtime.Message{
		Role:       runtime.Role(message.Role),
		Content:    content,
		Name:       message.Name,
		ToolCallID: message.ToolCallID,
		ToolCalls:  toolCalls,
		Refusal:    message.Refusal,
	}, nil
}

func chunkToRuntimeEvents(chunk ChatCompletionChunk) []runtime.StreamEvent {
	createdAt := time.Unix(chunk.Created, 0).UTC()
	events := make([]runtime.StreamEvent, 0, len(chunk.Choices)*2+1)
	for _, choice := range chunk.Choices {
		delta := runtime.MessageDelta{}
		if strings.TrimSpace(choice.Delta.Role) != "" {
			delta.Role = runtime.Role(choice.Delta.Role)
		}
		if choice.Delta.Content != "" {
			delta.Content = append(delta.Content, runtime.ContentPartDelta{Type: runtime.ContentPartTypeText, Text: choice.Delta.Content})
		}
		for _, toolCall := range choice.Delta.ToolCalls {
			toolType := runtime.ToolType(toolCall.Type)
			if toolType == "" && (toolCall.Function.Name != "" || toolCall.Function.Arguments != "") {
				toolType = runtime.ToolTypeFunction
			}
			delta.ToolCalls = append(delta.ToolCalls, runtime.ToolCallDelta{
				Index:          toolCall.Index,
				ID:             toolCall.ID,
				Type:           toolType,
				NameDelta:      toolCall.Function.Name,
				ArgumentsDelta: toolCall.Function.Arguments,
			})
		}
		if !delta.Empty() {
			events = append(events, runtime.StreamEvent{
				Type:        runtime.StreamEventTypeDelta,
				ResponseID:  chunk.ID,
				Model:       chunk.Model,
				CreatedAt:   createdAt,
				ChoiceIndex: choice.Index,
				Delta:       delta,
			})
		}
		if choice.FinishReason != "" {
			events = append(events, runtime.StreamEvent{
				Type:         runtime.StreamEventTypeChoiceDone,
				ResponseID:   chunk.ID,
				Model:        chunk.Model,
				CreatedAt:    createdAt,
				ChoiceIndex:  choice.Index,
				FinishReason: runtime.FinishReason(choice.FinishReason),
			})
		}
	}
	if chunk.Usage != nil {
		usage := usageToRuntime(*chunk.Usage)
		events = append(events, runtime.StreamEvent{
			Type:       runtime.StreamEventTypeUsage,
			ResponseID: chunk.ID,
			Model:      chunk.Model,
			CreatedAt:  createdAt,
			Usage:      &usage,
		})
	}
	return events
}

func usageToRuntime(usage Usage) runtime.Usage {
	return runtime.Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
	}
}

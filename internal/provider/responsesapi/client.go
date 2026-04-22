package responsesapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Perdonus/lavilas-code/internal/provider"
	"github.com/Perdonus/lavilas-code/internal/runtime"
)

type Client struct {
	cfg        Config
	httpClient *http.Client
	endpoint   string
}

func NewClient(cfg Config) (*Client, error) {
	normalized := cfg.withDefaults()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		cfg:        normalized,
		httpClient: normalized.HTTPClientOrDefault(),
		endpoint:   normalized.Endpoint(),
	}, nil
}

func (c *Client) Name() string {
	return c.cfg.Name
}

func (c *Client) Capabilities() provider.Capabilities {
	return provider.Capabilities{
		Streaming: true,
		Tools:     true,
	}
}

func (c *Client) Create(ctx context.Context, req runtime.Request) (*runtime.Response, error) {
	payload, err := requestFromRuntime(req, false)
	if err != nil {
		return nil, err
	}
	httpReq, err := c.newRequest(ctx, payload, "application/json")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := c.checkResponse(resp); err != nil {
		return nil, err
	}

	var response Response
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return responseToRuntime(c.Name(), response), nil
}

func (c *Client) Stream(ctx context.Context, req runtime.Request) (runtime.Stream, error) {
	payload, err := requestFromRuntime(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := c.newRequest(ctx, payload, "text/event-stream")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if err := c.checkResponse(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return &streamReader{
		body:   resp.Body,
		reader: bufio.NewReader(resp.Body),
	}, nil
}

func (c *Client) newRequest(ctx context.Context, payload Request, accept string) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, values := range c.cfg.BuildHeaders(accept) {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	return request, nil
}

func (c *Client) checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	trimmed := strings.TrimSpace(string(body))

	var parsed ErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return &provider.Error{
			Provider:   c.Name(),
			StatusCode: resp.StatusCode,
			Code:       parsed.Error.Code,
			Message:    parsed.Error.Message,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}
	if trimmed == "" {
		trimmed = resp.Status
	}
	return &provider.Error{
		Provider:   c.Name(),
		StatusCode: resp.StatusCode,
		Message:    trimmed,
		Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		RetryAfter: provider.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
	}
}

func requestFromRuntime(req runtime.Request, stream bool) (Request, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return Request{}, fmt.Errorf("model is required")
	}
	if len(req.Messages) == 0 {
		return Request{}, fmt.Errorf("at least one message is required")
	}

	input := make([]InputItem, 0, len(req.Messages))
	var instructions []string
	for _, message := range req.Messages {
		if err := message.Validate(); err != nil {
			return Request{}, err
		}
		switch message.Role {
		case runtime.RoleSystem:
			text := strings.TrimSpace(message.Text())
			if text != "" {
				instructions = append(instructions, text)
			}
		case runtime.RoleUser:
			input = append(input, marshalInputTextMessage(string(message.Role), message.Text()))
		case runtime.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				for _, call := range message.ToolCalls {
					callID := strings.TrimSpace(call.ID)
					if callID == "" {
						callID = strings.TrimSpace(call.Function.Name)
					}
					input = append(input, InputItem{
						Type:      "function_call",
						CallID:    callID,
						Name:      call.Function.Name,
						Arguments: call.Function.ArgumentsString(),
					})
				}
			}
			if text := strings.TrimSpace(message.Text()); text != "" {
				input = append(input, marshalOutputTextMessage(string(message.Role), text))
			}
		case runtime.RoleTool:
			text := strings.TrimSpace(message.Text())
			input = append(input, InputItem{
				Type:   "function_call_output",
				CallID: message.ToolCallID,
				Output: text,
			})
		}
	}

	payload := Request{
		Model:  model,
		Input:  input,
		Stream: stream,
	}
	if len(instructions) > 0 {
		payload.Instructions = strings.Join(instructions, "\n\n")
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		payload.Reasoning = &Reasoning{Effort: effort}
	}
	if len(req.Tools) > 0 {
		payload.Tools = make([]Tool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			if err := tool.Validate(); err != nil {
				return Request{}, err
			}
			payload.Tools = append(payload.Tools, Tool{
				Type:        "function",
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  runtime.NormalizeStrictJSONSchema(tool.Function.Parameters),
				Strict:      tool.Function.Strict,
			})
		}
	}
	if len(req.Metadata) > 0 {
		payload.Metadata = req.Metadata
	}
	return payload, nil
}

func responseToRuntime(providerName string, response Response) *runtime.Response {
	message := runtime.Message{
		Role: runtime.RoleAssistant,
	}
	finishReason := runtime.FinishReasonStop
	for _, item := range response.Output {
		switch item.Type {
		case "message":
			text := outputItemText(item)
			if strings.TrimSpace(text) != "" {
				message.Content = append(message.Content, runtime.TextPart(text))
			}
		case "function_call", "custom_tool_call":
			finishReason = runtime.FinishReasonToolCalls
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				callID = strings.TrimSpace(item.ID)
			}
			message.ToolCalls = append(message.ToolCalls, runtime.ToolCall{
				ID:   callID,
				Type: runtime.ToolTypeFunction,
				Function: runtime.FunctionCall{
					Name:      item.Name,
					Arguments: []byte(item.Arguments),
				},
			})
		}
	}
	result := &runtime.Response{
		ID:        response.ID,
		Model:     response.Model,
		Provider:  providerName,
		CreatedAt: time.Now().UTC(),
		Choices: []runtime.Choice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
	}
	if response.Usage != nil {
		result.Usage = runtime.Usage{
			InputTokens:  response.Usage.InputTokens,
			OutputTokens: response.Usage.OutputTokens,
			TotalTokens:  response.Usage.TotalTokens,
		}
	}
	return result
}

type streamReader struct {
	body                  io.ReadCloser
	reader                *bufio.Reader
	queue                 []runtime.StreamEvent
	done                  bool
	sawAssistantTextDelta bool
}

func (s *streamReader) Recv() (runtime.StreamEvent, error) {
	if len(s.queue) > 0 {
		event := s.queue[0]
		s.queue = s.queue[1:]
		return event, nil
	}
	if s.done {
		return runtime.StreamEvent{}, io.EOF
	}

	for {
		payload, err := readSSEPayload(s.reader)
		if err != nil {
			return runtime.StreamEvent{}, err
		}
		if len(payload) == 0 {
			continue
		}
		if bytes.Equal(payload, []byte("[DONE]")) {
			s.done = true
			return runtime.StreamEvent{Type: runtime.StreamEventTypeDone}, nil
		}

		var event Event
		if err := json.Unmarshal(payload, &event); err != nil {
			return runtime.StreamEvent{}, fmt.Errorf("decode stream event: %w", err)
		}
		s.queue = append(s.queue, s.eventToRuntime(event)...)
		if len(s.queue) == 0 {
			continue
		}
		next := s.queue[0]
		s.queue = s.queue[1:]
		if next.Type == runtime.StreamEventTypeDone {
			s.done = true
		}
		return next, nil
	}
}

func (s *streamReader) Close() error {
	if s.body == nil {
		return nil
	}
	err := s.body.Close()
	s.body = nil
	return err
}

func (s *streamReader) eventToRuntime(event Event) []runtime.StreamEvent {
	switch event.Type {
	case "response.output_text.delta":
		if strings.TrimSpace(event.Delta) == "" {
			return nil
		}
		s.sawAssistantTextDelta = true
		return []runtime.StreamEvent{{
			Type: runtime.StreamEventTypeDelta,
			Delta: runtime.MessageDelta{
				Content: []runtime.ContentPartDelta{{
					Type: runtime.ContentPartTypeText,
					Text: event.Delta,
				}},
			},
		}}
	case "response.output_item.done":
		if event.Item == nil {
			return nil
		}
		switch event.Item.Type {
		case "message":
			if s.sawAssistantTextDelta {
				return nil
			}
			text := strings.TrimSpace(outputItemText(*event.Item))
			if text == "" {
				return nil
			}
			s.sawAssistantTextDelta = true
			return []runtime.StreamEvent{{
				Type: runtime.StreamEventTypeDelta,
				Delta: runtime.MessageDelta{
					Content: []runtime.ContentPartDelta{{
						Type: runtime.ContentPartTypeText,
						Text: text,
					}},
				},
			}}
		case "function_call", "custom_tool_call":
			callID := strings.TrimSpace(event.Item.CallID)
			if callID == "" {
				callID = strings.TrimSpace(event.Item.ID)
			}
			return []runtime.StreamEvent{{
				Type: runtime.StreamEventTypeDelta,
				Delta: runtime.MessageDelta{
					ToolCalls: []runtime.ToolCallDelta{{
						ID:             callID,
						Type:           runtime.ToolTypeFunction,
						NameDelta:      event.Item.Name,
						ArgumentsDelta: event.Item.Arguments,
					}},
				},
			}, {
				Type:         runtime.StreamEventTypeChoiceDone,
				FinishReason: runtime.FinishReasonToolCalls,
			}}
		}
	case "response.completed":
		events := []runtime.StreamEvent{}
		if event.Response != nil && event.Response.Usage != nil {
			usage := runtime.Usage{
				InputTokens:  event.Response.Usage.InputTokens,
				OutputTokens: event.Response.Usage.OutputTokens,
				TotalTokens:  event.Response.Usage.TotalTokens,
			}
			events = append(events, runtime.StreamEvent{
				Type:  runtime.StreamEventTypeUsage,
				Usage: &usage,
			})
		}
		events = append(events, runtime.StreamEvent{Type: runtime.StreamEventTypeDone})
		return events
	}
	return nil
}

func marshalInputTextMessage(role string, text string) InputItem {
	return InputItem{
		Type: "message",
		Role: role,
		Content: []InputContent{{
			Type: "input_text",
			Text: text,
		}},
	}
}

func marshalOutputTextMessage(role string, text string) InputItem {
	return InputItem{
		Type: "message",
		Role: role,
		Content: []InputContent{{
			Type: "output_text",
			Text: text,
		}},
	}
}

func outputItemText(item OutputItem) string {
	var parts []string
	for _, content := range item.Content {
		if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
			parts = append(parts, content.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func readSSEPayload(reader *bufio.Reader) ([]byte, error) {
	var data bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "data:") {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
		}
		if trimmed == "" {
			if data.Len() == 0 {
				if err == io.EOF {
					return nil, io.EOF
				}
				continue
			}
			return bytes.TrimSpace(data.Bytes()), nil
		}
		if err == io.EOF {
			if data.Len() == 0 {
				return nil, io.EOF
			}
			return bytes.TrimSpace(data.Bytes()), nil
		}
	}
}

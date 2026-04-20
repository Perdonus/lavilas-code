package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type ChatCompletionRequest struct {
	Model             string            `json:"model"`
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`
	Messages          []Message         `json:"messages"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolChoice        *ToolChoice       `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	Temperature       *float64          `json:"temperature,omitempty"`
	TopP              *float64          `json:"top_p,omitempty"`
	MaxTokens         int               `json:"max_tokens,omitempty"`
	Stop              []string          `json:"stop,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	User              string            `json:"user,omitempty"`
	Stream            bool              `json:"stream,omitempty"`
	StreamOptions     *StreamOptions    `json:"stream_options,omitempty"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Message struct {
	Role       string         `json:"role"`
	Content    MessageContent `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	Refusal    string         `json:"refusal,omitempty"`
}

type MessageContent struct {
	Parts []ContentPart
}

func TextContent(text string) MessageContent {
	return MessageContent{Parts: []ContentPart{{Type: ContentTypeText, Text: text}}}
}

func (c MessageContent) MarshalJSON() ([]byte, error) {
	if len(c.Parts) == 0 {
		return []byte("null"), nil
	}
	if len(c.Parts) == 1 && c.Parts[0].Type == ContentTypeText && c.Parts[0].ImageURL == nil && c.Parts[0].InputAudio == nil {
		return json.Marshal(c.Parts[0].Text)
	}
	return json.Marshal(c.Parts)
}

func (c *MessageContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		c.Parts = nil
		return nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return err
		}
		c.Parts = []ContentPart{{Type: ContentTypeText, Text: text}}
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return fmt.Errorf("decode message content: %w", err)
	}
	c.Parts = parts
	return nil
}

type ContentType string

const (
	ContentTypeText       ContentType = "text"
	ContentTypeImageURL   ContentType = "image_url"
	ContentTypeInputAudio ContentType = "input_audio"
)

type ContentPart struct {
	Type       ContentType `json:"type"`
	Text       string      `json:"text,omitempty"`
	ImageURL   *ImageURL   `json:"image_url,omitempty"`
	InputAudio *InputAudio `json:"input_audio,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type InputAudio struct {
	Data   string `json:"data"`
	Format string `json:"format"`
}

type Tool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeNone     ToolChoiceMode = "none"
	ToolChoiceModeRequired ToolChoiceMode = "required"
	ToolChoiceModeFunction ToolChoiceMode = "function"
)

type ToolChoice struct {
	Mode         ToolChoiceMode
	FunctionName string
}

func (c ToolChoice) MarshalJSON() ([]byte, error) {
	if strings.TrimSpace(c.FunctionName) != "" || c.Mode == ToolChoiceModeFunction {
		if strings.TrimSpace(c.FunctionName) == "" {
			return nil, errors.New("tool choice function name is required")
		}
		payload := struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}{Type: string(ToolChoiceModeFunction)}
		payload.Function.Name = c.FunctionName
		return json.Marshal(payload)
	}
	mode := c.Mode
	if mode == "" {
		mode = ToolChoiceModeAuto
	}
	return json.Marshal(string(mode))
}

func (c *ToolChoice) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*c = ToolChoice{}
		return nil
	}
	if trimmed[0] == '"' {
		var mode string
		if err := json.Unmarshal(trimmed, &mode); err != nil {
			return err
		}
		*c = ToolChoice{Mode: ToolChoiceMode(mode)}
		return nil
	}
	var payload struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return err
	}
	*c = ToolChoice{Mode: ToolChoiceModeFunction, FunctionName: payload.Function.Name}
	return nil
}

type ChatCompletionResponse struct {
	ID                string       `json:"id"`
	Object            string       `json:"object,omitempty"`
	Created           int64        `json:"created"`
	Model             string       `json:"model"`
	Choices           []Choice     `json:"choices"`
	Usage             *Usage       `json:"usage,omitempty"`
	SystemFingerprint string       `json:"system_fingerprint,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type ChatCompletionChunk struct {
	ID                string        `json:"id"`
	Object            string        `json:"object,omitempty"`
	Created           int64         `json:"created"`
	Model             string        `json:"model"`
	Choices           []ChunkChoice `json:"choices"`
	Usage             *Usage        `json:"usage,omitempty"`
	SystemFingerprint string        `json:"system_fingerprint,omitempty"`
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        Delta      `json:"delta"`
	FinishReason string     `json:"finish_reason,omitempty"`
}

type Delta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ToolCallDelta `json:"tool_calls,omitempty"`
}

type ToolCallDelta struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function FunctionCallDelta `json:"function,omitempty"`
}

type FunctionCallDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

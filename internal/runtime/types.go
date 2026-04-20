package runtime

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

func (r Role) Valid() bool {
	switch r {
	case RoleSystem, RoleUser, RoleAssistant, RoleTool:
		return true
	default:
		return false
	}
}

type ContentPartType string

const (
	ContentPartTypeText       ContentPartType = "text"
	ContentPartTypeImageURL   ContentPartType = "image_url"
	ContentPartTypeInputAudio ContentPartType = "input_audio"
)

type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeNone     ToolChoiceMode = "none"
	ToolChoiceModeRequired ToolChoiceMode = "required"
	ToolChoiceModeNamed    ToolChoiceMode = "named"
)

type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
)

type Request struct {
	Model              string
	ReasoningEffort    string
	Messages           []Message
	Tools              []ToolDefinition
	ToolChoice         ToolChoice
	Temperature        *float64
	TopP               *float64
	MaxOutputTokens    int
	Stop               []string
	Metadata           map[string]string
	User               string
	ParallelToolCalls  *bool
}

type Message struct {
	Role       Role
	Name       string
	Content    []ContentPart
	ToolCallID string
	ToolCalls  []ToolCall
	Refusal    string
}

func TextMessage(role Role, text string) Message {
	return Message{
		Role:    role,
		Content: []ContentPart{TextPart(text)},
	}
}

func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartTypeText, Text: text}
}

func (m Message) Validate() error {
	if !m.Role.Valid() {
		return errors.New("message role is required")
	}
	if m.Role == RoleTool && strings.TrimSpace(m.ToolCallID) == "" {
		return errors.New("tool messages require a tool call id")
	}
	if len(m.Content) == 0 && len(m.ToolCalls) == 0 && strings.TrimSpace(m.Refusal) == "" {
		return errors.New("message requires content, tool calls, or refusal text")
	}
	return nil
}

func (m Message) Text() string {
	var parts []string
	for _, part := range m.Content {
		if part.Type == ContentPartTypeText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, part.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type ContentPart struct {
	Type        ContentPartType
	Text        string
	URL         string
	Detail      string
	AudioData   string
	AudioFormat string
	MIMEType    string
}

type ToolDefinition struct {
	Type     ToolType
	Function FunctionDefinition
}

func (t ToolDefinition) Validate() error {
	toolType := t.Type
	if toolType == "" {
		toolType = ToolTypeFunction
	}
	if toolType != ToolTypeFunction {
		return errors.New("unsupported tool type")
	}
	if strings.TrimSpace(t.Function.Name) == "" {
		return errors.New("tool function name is required")
	}
	return nil
}

type FunctionDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	Strict      bool
}

type ToolChoice struct {
	Mode ToolChoiceMode
	Name string
}

func (c ToolChoice) IsZero() bool {
	return c.Mode == "" && strings.TrimSpace(c.Name) == ""
}

type ToolCall struct {
	ID       string
	Type     ToolType
	Function FunctionCall
}

type FunctionCall struct {
	Name      string
	Arguments json.RawMessage
}

func (c FunctionCall) ArgumentsString() string {
	return strings.TrimSpace(string(c.Arguments))
}

type Response struct {
	ID        string
	Model     string
	Provider  string
	CreatedAt time.Time
	Choices   []Choice
	Usage     Usage
	Metadata  map[string]string
}

type Choice struct {
	Index        int
	Message      Message
	FinishReason FinishReason
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func (u Usage) IsZero() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0
}

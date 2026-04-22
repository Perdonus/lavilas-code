package responsesapi

import (
	"encoding/json"
	"strings"
)

type Request struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Input        []InputItem       `json:"input"`
	Tools        []Tool            `json:"tools,omitempty"`
	Stream       bool              `json:"stream,omitempty"`
	Reasoning    *Reasoning        `json:"reasoning,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type Reasoning struct {
	Effort string `json:"effort,omitempty"`
}

type InputItem struct {
	Type    string        `json:"type"`
	Role    string        `json:"role,omitempty"`
	Content []InputContent `json:"content,omitempty"`
	CallID  string        `json:"call_id,omitempty"`
	Name    string        `json:"name,omitempty"`
	Arguments string      `json:"arguments,omitempty"`
	Output  string        `json:"output,omitempty"`
}

type InputContent struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
}

type Tool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type Response struct {
	ID     string       `json:"id"`
	Output []OutputItem `json:"output,omitempty"`
	Usage  *Usage       `json:"usage,omitempty"`
	Model  string       `json:"model,omitempty"`
}

type OutputItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id,omitempty"`
	Role      string          `json:"role,omitempty"`
	Content   []OutputContent `json:"content,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
}

type OutputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type Event struct {
	Type     string          `json:"type"`
	Delta    string          `json:"delta,omitempty"`
	Item     *OutputItem     `json:"item,omitempty"`
	Response *Response       `json:"response,omitempty"`
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

func MarshalInputText(role string, text string) InputItem {
	return InputItem{
		Type: "message",
		Role: role,
		Content: []InputContent{{
			Type: "input_text",
			Text: text,
		}},
	}
}

func MarshalOutputText(role string, text string) InputItem {
	return InputItem{
		Type: "message",
		Role: role,
		Content: []InputContent{{
			Type: "output_text",
			Text: text,
		}},
	}
}

func (r Response) OutputText() string {
	var parts []string
	for _, item := range r.Output {
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	return json.Marshal(alias(e))
}

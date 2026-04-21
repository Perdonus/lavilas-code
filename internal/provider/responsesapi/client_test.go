package responsesapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perdonus/lavilas-code/internal/runtime"
)

func TestRequestFromRuntimePreservesInstructionsAndToolHistory(t *testing.T) {
	payload, err := requestFromRuntime(runtime.Request{
		Model: "gpt-test",
		Messages: []runtime.Message{
			runtime.TextMessage(runtime.RoleSystem, "inspect the repo first"),
			runtime.TextMessage(runtime.RoleUser, "check the weather"),
			{
				Role: runtime.RoleAssistant,
				ToolCalls: []runtime.ToolCall{{
					ID:   "call_1",
					Type: runtime.ToolTypeFunction,
					Function: runtime.FunctionCall{
						Name:      "lookup_weather",
						Arguments: []byte(`{"city":"Moscow"}`),
					},
				}},
			},
			{
				Role:       runtime.RoleTool,
				ToolCallID: "call_1",
				Content:    []runtime.ContentPart{runtime.TextPart(`{"temperature":"3C"}`)},
			},
		},
	}, false)
	if err != nil {
		t.Fatalf("requestFromRuntime: %v", err)
	}

	if payload.Instructions != "inspect the repo first" {
		t.Fatalf("unexpected instructions: %q", payload.Instructions)
	}
	if len(payload.Input) != 3 {
		t.Fatalf("unexpected input item count: %d", len(payload.Input))
	}
	if payload.Input[0].Type != "message" || payload.Input[0].Role != "user" {
		t.Fatalf("unexpected first input: %+v", payload.Input[0])
	}
	if payload.Input[1].Type != "function_call" || payload.Input[1].CallID != "call_1" {
		t.Fatalf("unexpected second input: %+v", payload.Input[1])
	}
	if payload.Input[2].Type != "function_call_output" || payload.Input[2].CallID != "call_1" {
		t.Fatalf("unexpected third input: %+v", payload.Input[2])
	}
}

func TestRequestFromRuntimeNormalizesStrictToolSchema(t *testing.T) {
	payload, err := requestFromRuntime(runtime.Request{
		Model:    "gpt-test",
		Messages: []runtime.Message{runtime.TextMessage(runtime.RoleUser, "hi")},
		Tools: []runtime.ToolDefinition{{
			Function: runtime.FunctionDefinition{
				Name:        "run_shell_command",
				Description: "Run shell",
				Strict:      true,
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"cmd": map[string]any{"type": "string"},
						"permissions": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"writable_roots": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
							},
						},
					},
					"required": []string{"cmd"},
				},
			},
		}},
	}, false)
	if err != nil {
		t.Fatalf("requestFromRuntime: %v", err)
	}
	if len(payload.Tools) != 1 {
		t.Fatalf("unexpected tools: %+v", payload.Tools)
	}
	root := payload.Tools[0].Parameters
	if got, ok := root["additionalProperties"].(bool); !ok || got {
		t.Fatalf("root additionalProperties = %#v, want false", root["additionalProperties"])
	}
	properties, ok := root["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing from normalized schema: %+v", root)
	}
	permissions, ok := properties["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("nested permissions schema missing: %+v", properties)
	}
	if got, ok := permissions["additionalProperties"].(bool); !ok || got {
		t.Fatalf("nested additionalProperties = %#v, want false", permissions["additionalProperties"])
	}
}

func TestClientCreateMapsFunctionCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload Request
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.Model != "gpt-test" {
			t.Fatalf("unexpected model: %s", payload.Model)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id":"resp_123",
			"model":"gpt-test",
			"output":[
				{"type":"function_call","call_id":"call_1","name":"lookup_weather","arguments":"{\"city\":\"Moscow\"}"}
			],
			"usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19}
		}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, ResponsesPath: "/", APIKey: "secret"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	response, err := client.Create(context.Background(), runtime.Request{
		Model:    "gpt-test",
		Messages: []runtime.Message{runtime.TextMessage(runtime.RoleUser, "What is the weather?")},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if len(response.Choices) != 1 {
		t.Fatalf("unexpected choice count: %d", len(response.Choices))
	}
	choice := response.Choices[0]
	if choice.FinishReason != runtime.FinishReasonToolCalls {
		t.Fatalf("unexpected finish reason: %s", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("unexpected tool calls: %+v", choice.Message.ToolCalls)
	}
	if choice.Message.ToolCalls[0].ID != "call_1" {
		t.Fatalf("unexpected tool call id: %s", choice.Message.ToolCalls[0].ID)
	}
}

func TestClientStreamDoesNotDuplicateMessageDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`data: {"type":"response.output_text.delta","delta":"Hello "}`,
			`data: {"type":"response.output_item.done","item":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello world"}]}}`,
			`data: {"type":"response.output_text.delta","delta":"world"}`,
			`data: {"type":"response.completed","response":{"id":"resp_123","model":"gpt-test","usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19}}}`,
		}
		_, _ = io.WriteString(w, strings.Join(chunks, "\n\n")+"\n\n")
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, ResponsesPath: "/", APIKey: "secret"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	stream, err := client.Stream(context.Background(), runtime.Request{
		Model:    "gpt-test",
		Messages: []runtime.Message{runtime.TextMessage(runtime.RoleUser, "hi")},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.Close()

	var text strings.Builder
	var usageSeen bool
	var doneSeen bool
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if event.Type == runtime.StreamEventTypeDelta {
			text.WriteString(event.Delta.Text())
		}
		if event.Type == runtime.StreamEventTypeUsage {
			usageSeen = event.Usage != nil && event.Usage.TotalTokens == 19
		}
		if event.Type == runtime.StreamEventTypeDone {
			doneSeen = true
			break
		}
	}

	if got := text.String(); got != "Hello world" {
		t.Fatalf("unexpected streamed text: %q", got)
	}
	if !usageSeen {
		t.Fatalf("expected usage event")
	}
	if !doneSeen {
		t.Fatalf("expected done event")
	}
}
